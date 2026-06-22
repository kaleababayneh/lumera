// Command lumera-explorer is a live block explorer for any Lumera node.
//
// It is a node-level tool, not a PoC artifact: point it at any CometBFT RPC
// endpoint (localnet, devnet, or mainnet) with --node and it backfills the
// chain from genesis and then tails it in real time, decoding every block,
// transaction, message and event in-process with lumerad's own codec — so
// every module's activity (registry, credits, insurance, supernode, oracle,
// EVM, IBC, CosmWasm, …) is tracked and labelled with zero per-module code.
//
// Everything it serves is real on-chain data. There is no seeding, mocking or
// simulation. The browser talks only to this server's same-origin /api/*
// endpoints plus a /api/stream Server-Sent-Events feed for live updates.
//
// Usage:
//
//	lumera-explorer --node tcp://localhost:26657 --listen :8090
//	LUMERA_NODE=tcp://host:26657 LISTEN=:8090 lumera-explorer
package main

import (
	"context"
	_ "embed"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	cmthttp "github.com/cometbft/cometbft/rpc/client/http"
)

//go:embed index.html
var indexHTML []byte

func logf(format string, a ...interface{}) { log.Printf("[explorer] "+format, a...) }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	listen := flag.String("listen", envOr("LISTEN", ":8090"), "HTTP listen address")
	node := flag.String("node", envOr("LUMERA_NODE", "tcp://localhost:26657"), "CometBFT RPC endpoint")
	dbPath := flag.String("db", envOr("EXPLORER_DB", "/tmp/lumera-explorer.db"), "bbolt database file")
	flag.Parse()

	if dir := filepath.Dir(*dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("[explorer] db dir: %v", err)
		}
	}
	store, err := openStore(*dbPath)
	if err != nil {
		log.Fatalf("[explorer] open store (%s): %v", *dbPath, err)
	}

	rpc, err := cmthttp.New(*node, "/websocket")
	if err != nil {
		log.Fatalf("[explorer] rpc client (%s): %v", *node, err)
	}

	ix := newIndexer(rpc, store)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Build the codec and start indexing in the background so the HTTP server —
	// and thus the UI — is reachable immediately. Codec init takes a few seconds;
	// the store is already open, so the API serves right away and data fills in.
	go func() {
		clientCtx, err := newClientCtx()
		if err != nil {
			logf("codec init failed (decoding disabled): %v", err)
			return
		}
		ix.clientCtx = clientCtx
		ix.Run(ctx)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("/api/status", ix.handleStatus)
	mux.HandleFunc("/api/blocks", ix.handleBlocks)
	mux.HandleFunc("/api/block/", ix.handleBlock)
	mux.HandleFunc("/api/txs", ix.handleTxs)
	mux.HandleFunc("/api/tx/", ix.handleTx)
	mux.HandleFunc("/api/events", ix.handleEvents)
	mux.HandleFunc("/api/modules", ix.handleModules)
	mux.HandleFunc("/api/search", ix.handleSearch)
	mux.HandleFunc("/api/stream", ix.handleStream)

	srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sh, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = srv.Shutdown(sh)
	}()

	logf("serving %s  →  node %s  ·  db %s", *listen, *node, *dbPath)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[explorer] serve: %v", err)
	}
}
