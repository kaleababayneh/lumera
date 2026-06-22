package main

// indexer.go ingests the chain into the SQLite store. It waits for the node to
// come up, detects a fresh localnet (resetting the store), backfills history
// (every historical tx via /tx_search plus recent block headers) and then polls
// the node, decoding each new block + transaction with the in-process codec.
// Everything it stores is REAL on-chain data — no seeding, mocking or simulation.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmthttp "github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
)

const (
	backfillBlocks  = 1500 // recent blocks fetched for the stream on startup
	pollInterval    = 800 * time.Millisecond
	txSearchPerPage = 100
)

// ---------------------------------------------------------------------------
// Records served by the API (persisted as JSON columns in the store)
// ---------------------------------------------------------------------------

type EventAttr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type EventInfo struct {
	Type   string      `json:"type"`
	Module string      `json:"module"`
	Attrs  []EventAttr `json:"attrs"`
}

type MsgInfo struct {
	Type   string          `json:"type"`
	Module string          `json:"module"`
	Group  string          `json:"group"`
	Label  string          `json:"label"`
	JSON   json.RawMessage `json:"json"`
}

type TxRecord struct {
	Hash      string          `json:"hash"`
	Height    int64           `json:"height"`
	Index     uint32          `json:"index"`
	Time      time.Time       `json:"time"`
	Code      uint32          `json:"code"`
	Success   bool            `json:"success"`
	Codespace string          `json:"codespace,omitempty"`
	Log       string          `json:"log,omitempty"`
	GasWanted int64           `json:"gasWanted"`
	GasUsed   int64           `json:"gasUsed"`
	Fee       string          `json:"fee"`
	Memo      string          `json:"memo,omitempty"`
	Signer    string          `json:"signer,omitempty"`
	Modules   []string        `json:"modules"`
	Summary   string          `json:"summary"`
	Msgs      []MsgInfo       `json:"msgs"`
	Events    []EventInfo     `json:"events"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

type BlockRecord struct {
	Height      int64       `json:"height"`
	Time        time.Time   `json:"time"`
	Hash        string      `json:"hash"`
	Proposer    string      `json:"proposer"`
	NumTxs      int         `json:"numTxs"`
	TxHashes    []string    `json:"txHashes,omitempty"`
	BlockEvents []EventInfo `json:"blockEvents,omitempty"`
}

// ---------------------------------------------------------------------------
// Indexer
// ---------------------------------------------------------------------------

type Indexer struct {
	rpc       *cmthttp.HTTP
	clientCtx client.Context
	store     *Store

	mu         sync.RWMutex
	chainID    string
	moniker    string
	nodeVer    string
	catchingUp bool
	tip        int64 // node's latest block height
	latest     int64 // highest height indexed
	lastTime   time.Time

	subs map[chan sse]struct{}
}

type sse struct {
	Kind string      `json:"kind"`
	Data interface{} `json:"data"`
}

// newIndexer creates an indexer. clientCtx is assigned later (before Run) so the
// HTTP server can come up before the multi-second codec init completes.
func newIndexer(rpc *cmthttp.HTTP, store *Store) *Indexer {
	return &Indexer{rpc: rpc, store: store, subs: map[chan sse]struct{}{}}
}

// Run waits for the node, resets the store on a new chain, backfills, then polls.
func (ix *Indexer) Run(ctx context.Context) {
	if !ix.waitForNode(ctx) {
		return
	}
	marker := ix.chainMarker(ctx)
	if reset, err := ix.store.ResetForChain(marker); err != nil {
		logf("store reset check failed: %v", err)
	} else if reset {
		logf("new chain instance detected — store reset")
	}

	ix.backfill(ctx)

	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			ix.poll(ctx)
		}
	}
}

func (ix *Indexer) waitForNode(ctx context.Context) bool {
	for {
		if _, err := ix.rpc.Status(ctx); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
}

// chainMarker identifies a chain instance by its height-1 block hash so the
// store can tell a fresh localnet from a continuation of the same one.
func (ix *Indexer) chainMarker(ctx context.Context) string {
	one := int64(1)
	if blk, err := ix.rpc.Block(ctx, &one); err == nil && blk.Block != nil {
		return strings.ToUpper(hex.EncodeToString(blk.Block.Hash()))
	}
	if st, err := ix.rpc.Status(ctx); err == nil {
		return fmt.Sprintf("%s:%d", st.NodeInfo.Network, st.SyncInfo.EarliestBlockHeight)
	}
	return ""
}

func (ix *Indexer) backfill(ctx context.Context) {
	st, err := ix.rpc.Status(ctx)
	if err != nil {
		return
	}
	ix.applyStatus(st)
	latest := st.SyncInfo.LatestBlockHeight
	earliest := st.SyncInfo.EarliestBlockHeight
	if earliest < 1 {
		earliest = 1
	}

	heights := ix.txBearingHeights(ctx, earliest)
	logf("backfill: %d tx-bearing height(s) since genesis", len(heights))
	for _, h := range heights {
		if ctx.Err() != nil {
			return
		}
		ix.indexHeight(ctx, h, false)
	}

	start := latest - backfillBlocks
	if start < earliest {
		start = earliest
	}
	for h := start; h <= latest; h++ {
		if ctx.Err() != nil {
			return
		}
		ix.indexHeight(ctx, h, false)
	}
	blocks, txs, _ := ix.store.Totals()
	logf("backfill complete @ height %d (%d blocks, %d tx in store)", latest, blocks, txs)
}

func (ix *Indexer) txBearingHeights(ctx context.Context, from int64) []int64 {
	seen := map[int64]bool{}
	page := 1
	per := txSearchPerPage
	q := fmt.Sprintf("tx.height >= %d", from)
	for {
		res, err := ix.rpc.TxSearch(ctx, q, false, &page, &per, "asc")
		if err != nil {
			break
		}
		for _, r := range res.Txs {
			seen[r.Height] = true
		}
		if page*per >= res.TotalCount || len(res.Txs) == 0 {
			break
		}
		page++
		if page > 1000 {
			break
		}
	}
	out := make([]int64, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (ix *Indexer) poll(ctx context.Context) {
	st, err := ix.rpc.Status(ctx)
	if err != nil {
		return
	}
	ix.applyStatus(st)
	latest := st.SyncInfo.LatestBlockHeight

	ix.mu.RLock()
	last := ix.latest
	ix.mu.RUnlock()
	if last == 0 {
		last = ix.store.MaxHeight()
	}

	for h := last + 1; h <= latest; h++ {
		if ctx.Err() != nil {
			return
		}
		ix.indexHeight(ctx, h, true)
	}
	ix.broadcast(sse{Kind: "status", Data: ix.Status()})
}

func (ix *Indexer) applyStatus(st *coretypes.ResultStatus) {
	ix.mu.Lock()
	ix.chainID = st.NodeInfo.Network
	ix.moniker = st.NodeInfo.Moniker
	ix.nodeVer = st.NodeInfo.Version
	ix.catchingUp = st.SyncInfo.CatchingUp
	ix.tip = st.SyncInfo.LatestBlockHeight
	ix.lastTime = st.SyncInfo.LatestBlockTime
	ix.mu.Unlock()
}

// indexHeight fetches a block + results, decodes every tx, and persists it.
func (ix *Indexer) indexHeight(ctx context.Context, h int64, live bool) {
	if h < 1 || ix.store.Has(h) {
		return
	}
	blk, err := ix.rpc.Block(ctx, &h)
	if err != nil || blk.Block == nil {
		return
	}
	res, err := ix.rpc.BlockResults(ctx, &h)
	if err != nil || res == nil {
		return
	}

	br := &BlockRecord{
		Height:      h,
		Time:        blk.Block.Time,
		Hash:        strings.ToUpper(hex.EncodeToString(blk.Block.Hash())),
		Proposer:    strings.ToUpper(hex.EncodeToString(blk.Block.ProposerAddress)),
		NumTxs:      len(blk.Block.Data.Txs),
		BlockEvents: convertEvents(res.FinalizeBlockEvents),
	}

	var newTxs []*TxRecord
	for i, raw := range blk.Block.Data.Txs {
		var r *abci.ExecTxResult
		if i < len(res.TxsResults) {
			r = res.TxsResults[i]
		}
		rec := ix.decodeTx(raw, h, uint32(i), blk.Block.Time, r)
		br.TxHashes = append(br.TxHashes, rec.Hash)
		newTxs = append(newTxs, rec)
	}

	if err := ix.store.SaveBlock(br, newTxs); err != nil {
		logf("save block %d: %v", h, err)
		return
	}
	ix.mu.Lock()
	if h > ix.latest {
		ix.latest = h
	}
	ix.mu.Unlock()

	if live {
		ix.broadcast(sse{Kind: "block", Data: br})
		for _, rec := range newTxs {
			ix.broadcast(sse{Kind: "tx", Data: rec.listView()})
		}
	}
}

func (ix *Indexer) decodeTx(raw cmttypes.Tx, height int64, index uint32, t time.Time, r *abci.ExecTxResult) *TxRecord {
	sum := sha256.Sum256(raw)
	rec := &TxRecord{
		Hash:   strings.ToUpper(hex.EncodeToString(sum[:])),
		Height: height,
		Index:  index,
		Time:   t,
	}
	if r != nil {
		rec.Code = r.Code
		rec.Success = r.Code == 0
		rec.Codespace = r.Codespace
		rec.Log = r.Log
		rec.GasWanted = r.GasWanted
		rec.GasUsed = r.GasUsed
		rec.Events = convertEvents(r.Events)
		rec.Signer = senderFromEvents(rec.Events)
	}

	sdkTx, err := ix.clientCtx.TxConfig.TxDecoder()(raw)
	if err != nil {
		rec.Summary = "undecodable tx"
		return rec
	}
	if rawJSON, err := ix.clientCtx.TxConfig.TxJSONEncoder()(sdkTx); err == nil {
		rec.Raw = rawJSON
		ix.fillFromTxJSON(rec, rawJSON)
	}

	modset := map[string]bool{}
	for _, m := range rec.Msgs {
		modset[m.Module] = true
	}
	rec.Modules = sortedKeys(modset)
	rec.Summary = summarize(rec.Msgs)
	return rec
}

func (ix *Indexer) fillFromTxJSON(rec *TxRecord, rawJSON []byte) {
	var t struct {
		Body struct {
			Messages []json.RawMessage `json:"messages"`
			Memo     string            `json:"memo"`
		} `json:"body"`
		AuthInfo struct {
			Fee struct {
				Amount []struct {
					Denom  string `json:"denom"`
					Amount string `json:"amount"`
				} `json:"amount"`
			} `json:"fee"`
		} `json:"auth_info"`
	}
	if err := json.Unmarshal(rawJSON, &t); err != nil {
		return
	}
	rec.Memo = t.Body.Memo
	var feeParts []string
	for _, a := range t.AuthInfo.Fee.Amount {
		feeParts = append(feeParts, a.Amount+a.Denom)
	}
	rec.Fee = strings.Join(feeParts, ",")
	for _, m := range t.Body.Messages {
		var head struct {
			Type string `json:"@type"`
		}
		_ = json.Unmarshal(m, &head)
		rec.Msgs = append(rec.Msgs, MsgInfo{
			Type:   head.Type,
			Module: moduleOf(head.Type),
			Group:  groupOf(head.Type),
			Label:  labelOf(head.Type),
			JSON:   m,
		})
	}
}

// ---------------------------------------------------------------------------
// SSE fan-out
// ---------------------------------------------------------------------------

func (ix *Indexer) subscribe() chan sse {
	ch := make(chan sse, 128)
	ix.mu.Lock()
	ix.subs[ch] = struct{}{}
	ix.mu.Unlock()
	return ch
}

func (ix *Indexer) unsubscribe(ch chan sse) {
	ix.mu.Lock()
	delete(ix.subs, ch)
	ix.mu.Unlock()
	close(ch)
}

func (ix *Indexer) broadcast(m sse) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	for ch := range ix.subs {
		select {
		case ch <- m:
		default:
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func convertEvents(evs []abci.Event) []EventInfo {
	out := make([]EventInfo, 0, len(evs))
	for _, e := range evs {
		ei := EventInfo{Type: e.Type}
		for _, a := range e.Attributes {
			ei.Attrs = append(ei.Attrs, EventAttr{Key: a.Key, Value: a.Value})
		}
		ei.Module = eventModule(e.Type, ei.Attrs)
		out = append(out, ei)
	}
	return out
}

func senderFromEvents(evs []EventInfo) string {
	for _, e := range evs {
		if e.Type == "message" {
			for _, a := range e.Attrs {
				if a.Key == "sender" && a.Value != "" {
					return a.Value
				}
			}
		}
	}
	for _, e := range evs {
		if e.Type == "tx" {
			for _, a := range e.Attrs {
				if a.Key == "fee_payer" && a.Value != "" {
					return a.Value
				}
			}
		}
	}
	return ""
}

func summarize(msgs []MsgInfo) string {
	if len(msgs) == 0 {
		return "no messages"
	}
	head := fmt.Sprintf("%s · %s", msgs[0].Module, msgs[0].Label)
	if len(msgs) > 1 {
		head += fmt.Sprintf("  +%d", len(msgs)-1)
	}
	return head
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *TxRecord) listView() *TxRecord {
	c := *r
	c.Raw = nil
	return &c
}
