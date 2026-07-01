// wave3.go exposes the orchestration-layer modules (priority, auction,
// workflows) and a whole-stack overview in the PoC.
//
// priority/auction/workflows are genesis-configured infrastructure with no live
// tx/query service of their own, so their config is read from the node's
// genesis (it does not change post-genesis). The stack overview proxies the
// on-chain explorer's live per-module counts so the dashboard can show the
// entire integrated economy in one view.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---- genesis app_state reader (cached) --------------------------------------

var genesisCache struct {
	sync.Mutex
	app    map[string]any
	loaded bool
}

func genesisAppState() map[string]any {
	genesisCache.Lock()
	defer genesisCache.Unlock()
	if genesisCache.loaded && genesisCache.app != nil {
		return genesisCache.app
	}
	raw, err := os.ReadFile(filepath.Join(cfg.Home, "config", "genesis.json"))
	if err != nil {
		return map[string]any{}
	}
	var g struct {
		AppState map[string]any `json:"app_state"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return map[string]any{}
	}
	genesisCache.app = g.AppState
	genesisCache.loaded = true
	return g.AppState
}

func explorerURL() string { return envOr("LUMERA_EXPLORER", "http://localhost:8090") }

// ---- handlers ---------------------------------------------------------------

func registerWave3APIs(mux *http.ServeMux) {
	// Orchestration layer config: priority lanes, spot auctions, workflows.
	mux.HandleFunc("/api/orchestration", func(w http.ResponseWriter, _ *http.Request) {
		app := genesisAppState()
		out := map[string]any{"ok": true}
		if p, ok := app["priority"].(map[string]any); ok {
			out["priority"] = p
		}
		if a, ok := app["auction"].(map[string]any); ok {
			out["auction"] = map[string]any{
				"params":             a["params"],
				"activeAuctionCount": a["active_auction_count"],
			}
		}
		if wf, ok := app["workflows"].(map[string]any); ok {
			out["workflows"] = map[string]any{"params": wf["params"]}
		}
		writeJSON(w, out)
	})

	// Whole-stack overview: every catalogued module on the chain with its title,
	// blurb, and live tx/msg/event counts, proxied from the on-chain explorer's
	// catalog-merged module list.
	mux.HandleFunc("/api/stack", func(w http.ResponseWriter, _ *http.Request) {
		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Get(explorerURL() + "/api/modules")
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "explorer unreachable at " + explorerURL(), "explorer": explorerURL()})
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if err != nil {
			fail(w, err)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			fail(w, err)
			return
		}
		payload["ok"] = true
		payload["explorer"] = explorerURL()
		writeJSON(w, payload)
	})
}
