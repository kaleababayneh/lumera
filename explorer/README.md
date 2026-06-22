# Lumera On-Chain Explorer

A live block explorer for a local Lumera node. It indexes **every block,
transaction, message and event across all modules** into a local embedded
database and serves a real-time web UI. Everything it shows is real on-chain
data decoded from the node — there is no seeding, mocking, or simulation.

Built for iterating on the chain itself: add or change a module, rebuild and
restart the node, and the explorer shows exactly what your module is doing
on-chain — tx hashes, decoded messages, emitted events, fees, gas, success/fail.

## Why it decodes everything for free

The explorer imports lumerad's own codec (`app.AppConfig` + the IBC/EVM/Wasm
interface registrations, exactly like `cmd/lumera/cmd/root.go`). That means it
decodes any message type compiled into the binary — including **new modules you
add** — with zero per-module code. When you add a module and rebuild, just
rebuild the explorer (the localnet script does this automatically) and your new
`Msg` types and events are decoded and labelled.

## Run it

It starts automatically with the localnet:

```bash
./poc/web/run-localnet.sh      # boots the node AND the explorer on :8090
# → open http://localhost:8090
```

Or run it standalone against any local node:

```bash
make explorer-run              # build + run on :8090
# or
./explorer/run.sh
# or
make explorer && ./build/lumera-explorer --node tcp://localhost:26657 --listen :8090
```

### Flags / env

| Flag | Env | Default | Meaning |
|------|-----|---------|---------|
| `--node` | `LUMERA_NODE` | `tcp://localhost:26657` | CometBFT RPC endpoint |
| `--listen` | `LISTEN` | `:8090` | HTTP listen address |
| `--db` | `EXPLORER_DB` | `/tmp/lumera-explorer.db` | bbolt database file |

## What you get

- **Overview** — live height/tx/event counters, latest blocks + transactions
  (streaming in real time), and a per-module activity breakdown.
- **Blocks** — every block; click for its transactions and begin/end-block
  (finalize) events — so EndBlocker emissions (e.g. `insurance_pool_metrics_updated`,
  `reward_distribution`, settlement-on-finalize) are visible too.
- **Transactions** — live feed filterable by module, message type, success/failed,
  or address. Click a tx for the full decode: status + code, fee, gas, signer,
  memo, every message as pretty JSON with its `@type`, and every event grouped by
  type with its attributes.
- **Events** — a searchable feed of every event, colour-coded and labelled by module.
- **Modules** — a card per module compiled into the node (registry, credits,
  insurance, supernode, incentives, oracle, policies, nft, reserve, action, audit,
  cac, claim, lumeraid, evmigration, plus bank/staking/gov/EVM/IBC/Wasm) with its
  message types, event types, and live counts.
- **Search** — tx hash, block height, or `lumera1…` address.

## How it works

1. On startup it waits for the node, then identifies the chain instance by its
   height-1 block hash. The localnet wipes its state on every boot, so the
   explorer auto-**resets** its database whenever it sees a new chain — stale data
   from a previous run never leaks in.
2. It **backfills** from genesis: every historical transaction (via `/tx_search`)
   plus recent block headers. So even though it starts after the node, it captures
   everything from height 1.
3. It then **polls** the node (~0.8s) and ingests each new block, decoding txs and
   events with the in-process codec and persisting them.
4. The browser loads from the same origin and receives live updates over a
   Server-Sent-Events stream (`/api/stream`).

The node's RPC has no CORS and REST is off on the localnet, so — like `poc/web` —
the browser talks only to this Go server, which holds the RPC connection.

## Storage

Embedded **bbolt** (pure Go, no daemon, no external server, already in lumerad's
module graph). The database is a single file you can point any bolt tool at. It's
keyed/indexed for the feeds and filters (height-ordered indexes per module / type
/ address, plus maintained counters). To wipe it, delete the file (or just reboot
the localnet — it auto-resets).

## Files

| File | Role |
|------|------|
| `main.go` | flags, HTTP routing, embedded UI |
| `decoder.go` | in-process codec (decodes every module) + module/event attribution |
| `indexer.go` | node polling, backfill, block/tx decode pipeline, SSE fan-out |
| `store.go` | bbolt persistence + ordered indexes + counters |
| `api.go` | JSON API + SSE endpoint |
| `catalog.go` | static per-module catalog (titles, blurbs, msg/event types) |
| `index.html` | the single-file web UI |
