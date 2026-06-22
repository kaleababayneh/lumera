package main

// store.go is the persistence layer: an embedded bbolt database (pure-Go,
// no daemon, no CGO, already in lumerad's module graph) holding every indexed
// block, transaction, message and event. It survives explorer restarts. Because
// the localnet wipes its state on every boot, the store stamps a per-chain
// marker (height-1 block hash) and auto-resets when it sees a new chain
// instance, so stale data from a previous run never leaks in.
//
// bbolt is a key/value store, so ordered feeds and filters are served by
// hand-maintained index buckets (height-ordered keys) and a counters bucket,
// all updated transactionally with each block write.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// errNotFound is returned by point lookups when the key is absent.
var errNotFound = errors.New("not found")

type Store struct {
	db *bolt.DB
}

// TxFilter parameterises the transaction feed query.
type TxFilter struct {
	Module  string
	MsgType string
	Status  string // "success" | "failed" | ""
	Address string
	Limit   int
	Offset  int
}

// EventFilter parameterises the event feed query.
type EventFilter struct {
	Module string
	Type   string
	Limit  int
}

var (
	bMeta    = []byte("meta")
	bBlocks  = []byte("blocks")   // be(height)            -> BlockRecord JSON
	bTxs     = []byte("txs")      // hash                  -> TxRecord JSON (full)
	bTxOrder = []byte("tx_order") // be(height)+be32(idx)  -> hash
	bIxMod   = []byte("ix_mod")   // module\x00+orderkey   -> hash
	bIxType  = []byte("ix_type")  // type\x00+orderkey     -> hash
	bIxAddr  = []byte("ix_addr")  // address\x00+orderkey  -> hash
	bEvents  = []byte("events")   // be(seq)               -> EventRow JSON
	bCounts  = []byte("counts")   // mtx:<m>|mmsg:<m>|mev:<m>|total:* -> be(uint64)

	dataBuckets = [][]byte{bBlocks, bTxs, bTxOrder, bIxMod, bIxType, bIxAddr, bEvents, bCounts}
	allBuckets  = append([][]byte{bMeta}, dataBuckets...)
)

func openStore(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range allBuckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s, nil
}

// ResetForChain wipes all indexed data if the chain marker differs from the one
// stored (i.e. a fresh localnet booted since the last run). Returns true when an
// existing dataset was actually cleared.
func (s *Store) ResetForChain(marker string) (bool, error) {
	reset := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		// An unknown marker (RPC was unreachable) must never drive a destructive
		// wipe — only reset when we positively know it's a different chain.
		if marker == "" {
			return nil
		}
		meta := tx.Bucket(bMeta)
		stored := ""
		if v := meta.Get([]byte("chain_marker")); v != nil {
			stored = string(v)
		}
		if stored == marker {
			return nil
		}
		reset = stored != ""
		for _, name := range dataBuckets {
			_ = tx.DeleteBucket(name) // buckets always exist; recreate fresh
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		return meta.Put([]byte("chain_marker"), []byte(marker))
	})
	return reset, err
}

func (s *Store) Has(height int64) bool {
	found := false
	_ = s.db.View(func(tx *bolt.Tx) error {
		found = tx.Bucket(bBlocks).Get(u64b(uint64(height))) != nil
		return nil
	})
	return found
}

func (s *Store) MaxHeight() int64 {
	var h int64
	_ = s.db.View(func(tx *bolt.Tx) error {
		k, _ := tx.Bucket(bBlocks).Cursor().Last()
		if k != nil {
			h = int64(binary.BigEndian.Uint64(k))
		}
		return nil
	})
	return h
}

// SaveBlock persists a block and its transactions atomically, updating every
// index and counter in the same write transaction.
func (s *Store) SaveBlock(br *BlockRecord, txs []*TxRecord) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		blocks := tx.Bucket(bBlocks)
		hkey := u64b(uint64(br.Height))
		if blocks.Get(hkey) != nil {
			return nil // idempotent
		}
		bj, err := json.Marshal(br)
		if err != nil {
			return err
		}
		if err := blocks.Put(hkey, bj); err != nil {
			return err
		}

		counts := tx.Bucket(bCounts)
		incr(counts, "total:blocks", 1)
		events := tx.Bucket(bEvents)

		// block (begin/end) events
		for _, ev := range br.BlockEvents {
			if err := putEvent(events, EventRow{Type: ev.Type, Module: ev.Module, Attrs: ev.Attrs,
				Height: br.Height, Time: br.Time, Source: "block"}); err != nil {
				return err
			}
			incr(counts, "mev:"+ev.Module, 1)
			incr(counts, "total:events", 1)
		}

		txsB := tx.Bucket(bTxs)
		order := tx.Bucket(bTxOrder)
		ixMod := tx.Bucket(bIxMod)
		ixType := tx.Bucket(bIxType)
		ixAddr := tx.Bucket(bIxAddr)

		for _, r := range txs {
			rj, err := json.Marshal(r)
			if err != nil {
				return err
			}
			hb := []byte(r.Hash)
			if err := txsB.Put(hb, rj); err != nil {
				return err
			}
			ok := orderKey(r.Height, r.Index)
			if err := order.Put(ok, hb); err != nil {
				return err
			}
			incr(counts, "total:txs", 1)

			for _, m := range r.Modules {
				if err := ixMod.Put(idxKey(m, ok), hb); err != nil {
					return err
				}
				incr(counts, "mtx:"+m, 1)
			}
			for _, m := range r.Msgs {
				if err := ixType.Put(idxKey(m.Type, ok), hb); err != nil {
					return err
				}
				incr(counts, "mmsg:"+m.Module, 1)
			}
			for a := range addressesOf(r) {
				if err := ixAddr.Put(idxKey(a, ok), hb); err != nil {
					return err
				}
			}
			for _, ev := range r.Events {
				if err := putEvent(events, EventRow{Type: ev.Type, Module: ev.Module, Attrs: ev.Attrs,
					TxHash: r.Hash, Height: r.Height, Time: r.Time, Source: "tx"}); err != nil {
					return err
				}
				incr(counts, "mev:"+ev.Module, 1)
				incr(counts, "total:events", 1)
			}
		}
		return nil
	})
}

// ---- reads -----------------------------------------------------------------

func (s *Store) Totals() (blocks, txs, events int) {
	_ = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bCounts)
		blocks = int(getU64(c, "total:blocks"))
		txs = int(getU64(c, "total:txs"))
		events = int(getU64(c, "total:events"))
		return nil
	})
	return
}

func (s *Store) RecentBlocks(limit int, before int64) ([]*BlockRecord, error) {
	out := []*BlockRecord{}
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bBlocks).Cursor()
		var k, v []byte
		if before > 0 {
			k, _ = c.Seek(u64b(uint64(before)))
			if k == nil {
				k, v = c.Last()
			} else {
				k, v = c.Prev() // strictly < before
			}
		} else {
			k, v = c.Last()
		}
		for ; k != nil && len(out) < limit; k, v = c.Prev() {
			b := &BlockRecord{}
			if err := json.Unmarshal(v, b); err != nil {
				return err
			}
			// list view: drop heavy payloads
			b.TxHashes = nil
			b.BlockEvents = nil
			out = append(out, b)
		}
		return nil
	})
	return out, err
}

func (s *Store) Block(height int64) (*BlockRecord, error) {
	var b *BlockRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bBlocks).Get(u64b(uint64(height)))
		if v == nil {
			return errNotFound
		}
		b = &BlockRecord{}
		return json.Unmarshal(v, b)
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Store) Txs(f TxFilter) ([]*TxRecord, error) {
	out := []*TxRecord{}
	err := s.db.View(func(tx *bolt.Tx) error {
		txsB := tx.Bucket(bTxs)
		// Iterate the narrowest available index; any remaining filters are still
		// enforced per-row by txPredicate so combined filters are an intersection.
		var bucket *bolt.Bucket
		var prefix []byte
		chosen := ""
		switch {
		case f.Module != "":
			bucket, prefix, chosen = tx.Bucket(bIxMod), prefixKey(f.Module), "module"
		case f.Address != "":
			bucket, prefix, chosen = tx.Bucket(bIxAddr), prefixKey(f.Address), "address"
		case f.MsgType != "":
			bucket, prefix, chosen = tx.Bucket(bIxType), prefixKey(f.MsgType), "type"
		default:
			bucket = tx.Bucket(bTxOrder)
		}
		skipped := 0
		iterReverse(bucket, prefix, func(_, hash []byte) bool {
			if len(out) >= f.Limit {
				return false
			}
			v := txsB.Get(hash)
			if v == nil {
				return true
			}
			r := &TxRecord{}
			if err := json.Unmarshal(v, r); err != nil {
				return true
			}
			if !txPredicate(r, f, chosen) {
				return true
			}
			if skipped < f.Offset {
				skipped++
				return true
			}
			r.Raw = nil
			out = append(out, r)
			return len(out) < f.Limit
		})
		return nil
	})
	return out, err
}

func (s *Store) TxsByHashes(hashes []string) ([]*TxRecord, error) {
	out := make([]*TxRecord, 0, len(hashes))
	err := s.db.View(func(tx *bolt.Tx) error {
		txsB := tx.Bucket(bTxs)
		for _, h := range hashes {
			v := txsB.Get([]byte(h))
			if v == nil {
				continue
			}
			r := &TxRecord{}
			if err := json.Unmarshal(v, r); err != nil {
				continue
			}
			r.Raw = nil
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

func (s *Store) Tx(hash string, withRaw bool) (*TxRecord, error) {
	var r *TxRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bTxs).Get([]byte(hash))
		if v == nil {
			return errNotFound
		}
		r = &TxRecord{}
		return json.Unmarshal(v, r)
	})
	if err != nil {
		return nil, err
	}
	if !withRaw {
		r.Raw = nil
	}
	return r, nil
}

func (s *Store) Events(f EventFilter) ([]EventRow, error) {
	out := []EventRow{}
	const maxScan = 50000
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bEvents).Cursor()
		scanned := 0
		for k, v := c.Last(); k != nil && len(out) < f.Limit && scanned < maxScan; k, v = c.Prev() {
			scanned++
			var e EventRow
			if err := json.Unmarshal(v, &e); err != nil {
				continue
			}
			if f.Module != "" && e.Module != f.Module {
				continue
			}
			if f.Type != "" && e.Type != f.Type {
				continue
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}

func (s *Store) ModuleCounts() ([]ModuleCount, error) {
	m := map[string]*ModuleCount{}
	get := func(name string) *ModuleCount {
		if m[name] == nil {
			m[name] = &ModuleCount{Module: name}
		}
		return m[name]
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bCounts).ForEach(func(k, v []byte) error {
			key := string(k)
			val := int(binary.BigEndian.Uint64(v))
			switch {
			case strings.HasPrefix(key, "mtx:"):
				get(key[4:]).Txs = val
			case strings.HasPrefix(key, "mmsg:"):
				get(key[5:]).Msgs = val
			case strings.HasPrefix(key, "mev:"):
				get(key[4:]).Events = val
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	out := make([]ModuleCount, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}
	return out, nil
}

// ---- key / counter helpers -------------------------------------------------

func u64b(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func u32b(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func orderKey(height int64, idx uint32) []byte {
	return append(u64b(uint64(height)), u32b(idx)...)
}

// idxKey builds "<field>\x00<orderKey>" so a prefix scan of "<field>\x00"
// returns all matching txs in height order.
func idxKey(field string, order []byte) []byte {
	k := append([]byte(field), 0x00)
	return append(k, order...)
}

func prefixKey(field string) []byte {
	return append([]byte(field), 0x00)
}

func putEvent(events *bolt.Bucket, e EventRow) error {
	seq, _ := events.NextSequence()
	ej, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return events.Put(u64b(seq), ej)
}

func incr(b *bolt.Bucket, key string, delta uint64) {
	_ = b.Put([]byte(key), u64b(getU64(b, key)+delta))
}

func getU64(b *bolt.Bucket, key string) uint64 {
	if v := b.Get([]byte(key)); v != nil {
		return binary.BigEndian.Uint64(v)
	}
	return 0
}

// iterReverse walks a bucket newest-first. With a nil/empty prefix it walks the
// whole bucket; otherwise only keys starting with prefix. fn returns false to stop.
func iterReverse(b *bolt.Bucket, prefix []byte, fn func(k, v []byte) bool) {
	c := b.Cursor()
	var k, v []byte
	if len(prefix) == 0 {
		k, v = c.Last()
	} else {
		// position just past the prefix range, then step back into it
		seek := append(append([]byte{}, prefix...), 0xFF)
		k, _ = c.Seek(seek)
		if k == nil {
			k, v = c.Last()
		} else {
			k, v = c.Prev()
		}
	}
	for ; k != nil; k, v = c.Prev() {
		if len(prefix) > 0 && !bytes.HasPrefix(k, prefix) {
			break
		}
		if !fn(k, v) {
			break
		}
	}
}

// txPredicate enforces every set filter except the one already satisfied by the
// chosen iteration index ("module" | "address" | "type" | ""), so combined
// filters yield a correct intersection rather than a superset.
func txPredicate(r *TxRecord, f TxFilter, chosen string) bool {
	if f.Status == "success" && !r.Success {
		return false
	}
	if f.Status == "failed" && r.Success {
		return false
	}
	if f.Module != "" && chosen != "module" && !slices.Contains(r.Modules, f.Module) {
		return false
	}
	if f.Address != "" && chosen != "address" && !addressesOf(r)[f.Address] {
		return false
	}
	if f.MsgType != "" && chosen != "type" && !hasMsgType(r, f.MsgType) {
		return false
	}
	return true
}

func hasMsgType(r *TxRecord, t string) bool {
	for _, m := range r.Msgs {
		if m.Type == t {
			return true
		}
	}
	return false
}

// addressesOf collects the distinct bech32 addresses a tx touched (signer plus
// any event attribute value that looks like a lumera address) for fast filtering.
func addressesOf(r *TxRecord) map[string]bool {
	set := map[string]bool{}
	if strings.HasPrefix(r.Signer, "lumera") {
		set[r.Signer] = true
	}
	for _, ev := range r.Events {
		for _, a := range ev.Attrs {
			if strings.HasPrefix(a.Value, "lumera1") && len(a.Value) >= 39 {
				set[a.Value] = true
			}
		}
	}
	return set
}
