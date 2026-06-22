package main

// api.go exposes the SQLite index over a same-origin HTTP API plus a live
// Server-Sent-Events stream. The browser talks only to this server, exactly
// mirroring how poc/web already proxies the chain.

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- API view types --------------------------------------------------------

type ModuleCount struct {
	Module string `json:"module"`
	Txs    int    `json:"txs"`
	Msgs   int    `json:"msgs"`
	Events int    `json:"events"`
}

type StatusView struct {
	ChainID     string        `json:"chainId"`
	Moniker     string        `json:"moniker"`
	NodeVersion string        `json:"nodeVersion"`
	CatchingUp  bool          `json:"catchingUp"`
	Height      int64         `json:"height"`
	BlockTime   time.Time     `json:"blockTime"`
	TotalTxs    int           `json:"totalTxs"`
	TotalBlocks int           `json:"totalBlocks"`
	TotalEvents int           `json:"totalEvents"`
	Modules     []ModuleCount `json:"modules"`
}

type EventRow struct {
	Type   string      `json:"type"`
	Module string      `json:"module"`
	Attrs  []EventAttr `json:"attrs"`
	TxHash string      `json:"txHash,omitempty"`
	Height int64       `json:"height"`
	Time   time.Time   `json:"time"`
	Source string      `json:"source"`
}

// Status snapshots the chain + index for the header and overview.
func (ix *Indexer) Status() StatusView {
	ix.mu.RLock()
	sv := StatusView{
		ChainID:     ix.chainID,
		Moniker:     ix.moniker,
		NodeVersion: ix.nodeVer,
		CatchingUp:  ix.catchingUp,
		Height:      ix.tip,
		BlockTime:   ix.lastTime,
	}
	ix.mu.RUnlock()

	b, t, e := ix.store.Totals()
	sv.TotalBlocks, sv.TotalTxs, sv.TotalEvents = b, t, e
	mc, _ := ix.store.ModuleCounts()
	sort.Slice(mc, func(i, j int) bool {
		if mc[i].Txs != mc[j].Txs {
			return mc[i].Txs > mc[j].Txs
		}
		if mc[i].Events != mc[j].Events {
			return mc[i].Events > mc[j].Events
		}
		return mc[i].Module < mc[j].Module
	})
	sv.Modules = mc
	return sv
}

// ---- handlers --------------------------------------------------------------

func (ix *Indexer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, ix.Status())
}

func (ix *Indexer) handleBlocks(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50, 500)
	before := int64Param(r, "before", 0)
	blocks, err := ix.store.RecentBlocks(limit, before)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"blocks": blocks})
}

func (ix *Indexer) handleBlock(w http.ResponseWriter, r *http.Request) {
	hs := strings.TrimPrefix(r.URL.Path, "/api/block/")
	h, err := strconv.ParseInt(hs, 10, 64)
	if err != nil {
		http.Error(w, "bad height", http.StatusBadRequest)
		return
	}
	b, err := ix.store.Block(h)
	if err != nil {
		http.Error(w, "block not indexed", http.StatusNotFound)
		return
	}
	txs, _ := ix.store.TxsByHashes(b.TxHashes)
	writeJSON(w, map[string]interface{}{"block": b, "txs": txs})
}

func (ix *Indexer) handleTxs(w http.ResponseWriter, r *http.Request) {
	f := TxFilter{
		Module:  r.URL.Query().Get("module"),
		MsgType: r.URL.Query().Get("type"),
		Status:  r.URL.Query().Get("status"),
		Address: strings.TrimSpace(r.URL.Query().Get("address")),
		Limit:   intParam(r, "limit", 50, 500),
		Offset:  intParam(r, "offset", 0, 1<<30),
	}
	txs, err := ix.store.Txs(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"txs": txs, "count": len(txs)})
}

func (ix *Indexer) handleTx(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/api/tx/"))
	hash = strings.TrimPrefix(hash, "0X")
	t, err := ix.store.Tx(hash, true)
	if err != nil {
		http.Error(w, "tx not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"tx": t})
}

func (ix *Indexer) handleEvents(w http.ResponseWriter, r *http.Request) {
	f := EventFilter{
		Module: r.URL.Query().Get("module"),
		Type:   r.URL.Query().Get("type"),
		Limit:  intParam(r, "limit", 80, 1000),
	}
	evs, err := ix.store.Events(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"events": evs})
}

func (ix *Indexer) handleModules(w http.ResponseWriter, _ *http.Request) {
	mc, _ := ix.store.ModuleCounts()
	counts := map[string]ModuleCount{}
	for _, c := range mc {
		counts[c.Module] = c
	}

	type modView struct {
		Name       string   `json:"name"`
		Group      string   `json:"group"`
		Title      string   `json:"title"`
		Blurb      string   `json:"blurb"`
		MsgTypes   []string `json:"msgTypes"`
		EventTypes []string `json:"eventTypes"`
		Txs        int      `json:"txs"`
		Msgs       int      `json:"msgs"`
		Events     int      `json:"events"`
	}
	out := []modView{}
	known := map[string]bool{}
	for _, m := range moduleCatalog {
		known[m.Name] = true
		c := counts[m.Name]
		out = append(out, modView{
			Name: m.Name, Group: m.Group, Title: m.Title, Blurb: m.Blurb,
			MsgTypes: m.MsgTypes, EventTypes: m.EventTypes,
			Txs: c.Txs, Msgs: c.Msgs, Events: c.Events,
		})
	}
	for name, c := range counts {
		if !known[name] {
			out = append(out, modView{Name: name, Group: "cosmos", Title: name, Txs: c.Txs, Msgs: c.Msgs, Events: c.Events})
		}
	}
	writeJSON(w, map[string]interface{}{"modules": out})
}

// handleSearch resolves a query to a tx hash, block height, or address.
func (ix *Indexer) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, map[string]interface{}{"kind": "none"})
		return
	}
	if n, err := strconv.ParseInt(q, 10, 64); err == nil {
		if ix.store.Has(n) {
			writeJSON(w, map[string]interface{}{"kind": "block", "target": n})
		} else {
			writeJSON(w, map[string]interface{}{"kind": "block", "target": n, "note": "not indexed yet"})
		}
		return
	}
	up := strings.ToUpper(strings.TrimPrefix(strings.TrimPrefix(q, "0x"), "0X"))
	if _, err := ix.store.Tx(up, false); err == nil {
		writeJSON(w, map[string]interface{}{"kind": "tx", "target": up})
		return
	}
	if strings.HasPrefix(q, "lumera") {
		writeJSON(w, map[string]interface{}{"kind": "address", "target": q})
		return
	}
	writeJSON(w, map[string]interface{}{"kind": "unknown", "target": q})
}

// handleStream is the Server-Sent-Events endpoint pushing live blocks/txs.
func (ix *Indexer) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := ix.subscribe()
	defer ix.unsubscribe(ch)

	if b, err := json.Marshal(sse{Kind: "status", Data: ix.Status()}); err == nil {
		writeSSE(w, flusher, b)
	}

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case m, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(m)
			if err != nil {
				continue
			}
			writeSSE(w, flusher, b)
		}
	}
}

func writeSSE(w http.ResponseWriter, f http.Flusher, b []byte) {
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
	f.Flush()
}

// ---- helpers ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func intParam(r *http.Request, key string, def, max int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func int64Param(r *http.Request, key string, def int64) int64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
