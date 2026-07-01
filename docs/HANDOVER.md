# Lumera AI — integration handover

Snapshot of the lumera_ai → lumera module integration for handover. The
authoritative living detail is `CLAUDE.md` (progress log) and
`docs/LUMERA_AI_INTEGRATION_PLAN.md` (the plan); this is the executive summary.

## Status: all non-IBC lumera_ai modules are integrated

Every lumera_ai chain module is now ported into lumera as a real Cosmos SDK
(v0.53, gogoproto) module — built, wired into the app via depinject, booting on
a local node, and verified. The on-chain agentic economy runs end-to-end:
**discover → meter → execute → prove → settle**, plus the orchestration layer.

| Module | Role | Surface | Verified |
|---|---|---|---|
| registry | tool discovery, bonds, PoS receipts, disputes/slash | tx + query | e2e |
| credits | LUME↔LAC, metered locks, PoS-gated settlement | tx + query | e2e |
| supernode | PoS attestation | (read-only gate) | e2e |
| incentives | reputation badge engine | tx + query | e2e |
| insurance / oracle / policies / reserve / nft | economic substrate | varies | e2e |
| **vaults** | prepaid-capacity discount vaults | tx + query | e2e |
| **passport** | stake-backed agent identity (slashable) | tx + query | e2e |
| **cac** | content-addressed cache + royalties | tx + query | e2e |
| **challenges** | prize-pool tournaments | tx + query | e2e |
| **payment_rails** | bridged-asset → oracle-priced LAC mint | tx + query | e2e (full money path) |
| **router** | routing telemetry / metrics | query CLI + owner-activation tx; gRPC | e2e |
| **priority** | latency/queue tier definitions | genesis-only | tests + boot |
| **auction** | spot-call auction economics | genesis-only | tests + boot |
| **workflows** | Composable Intelligence (workflow cards, author bonds, signed step receipts) | tx + query (author lifecycle) + begin/end-block | e2e (publish→upgrade→deactivate→bond) |

(Bold = integrated in this effort. Plus the lumera-native stack: EVM
(vm/feemarket/precisebank/erc20), IBC v10, CosmWasm, claim, lumeraid, action,
audit, evmigration, erc20policy.)

## Verification

- **Build:** `go build ./...` and the full `lumerad` binary are green. (`make
  build` itself trips macOS GNU make 3.81's `$(strip $(shell …))`; use `gmake`
  or `go build -o build/lumerad ./cmd/lumera`.)
- **Tests:** the ported modules' unit/keeper/simulation suites pass (incl. the
  workflows cross-language canonical-JSON conformance golden). Run e.g.
  `go test ./x/{vaults,passport,cac,challenges,payment_rails,router,priority,auction,workflows}/...`.
- **Lint:** `golangci-lint run` reports **0 issues** across all the integrated
  modules + the PoC + the explorer.
- **End-to-end:** a single-node localnet boots with all modules in genesis and
  runs the full PoC flows (see below).

## How to run it

```sh
# 1. build the node (Linux, or macOS without `make`):
go build -o /tmp/lumerad ./cmd/lumera

# 2. boot a prepared localnet (node + explorer + seeded marketplace & modules):
LUMERAD=/tmp/lumerad bash poc/web/run-localnet.sh
#    → node on :26657, explorer on http://localhost:8090

# 3. start the dashboard:
go build -o /tmp/lumera-poc-web ./poc/web
LUMERA_HOME=/tmp/lumera-web /tmp/lumera-poc-web   # → http://localhost:8787
```

- **Dashboard** (`poc/web`): every module driven natively over real on-chain
  calls — the core loop (swap → register+bond → lock → prove → settle →
  reputation → dispute → slash) plus Identity (passport), Capacity (vaults),
  On-ramp (payment_rails), Tournaments (challenges), Routing (router), Cache
  (cac), Orchestration (priority/auction/workflows config), and a Full-stack
  view of every module live.
- **MCP router** (`poc/mcp-router`): a Model Context Protocol server — any MCP
  agent discovers + calls on-chain tools; each call runs lock → (cache lookup) →
  execute → Proof-of-Service → settle, with content-cache royalties.
- **Explorer** (`explorer/`): indexes every block/tx/event across all modules.

## Deferred — for the testnet phase (genuine, not blockers)

- **SGX/TEE** — intentionally out of scope for now. PoS receipts anchor a
  BLAKE3(input,model,output) proof today; the `EnclaveQuote` (TEE attestation)
  verification path is Phase-2 (surfaced as `PHASE-2` in the PoC receipt view).
- **IBC track** — `ibc_action` is not ported, and the IBC settlement packet
  paths in `payment_rails` (and the credits IBC variant) ship as inert local
  state. This is the IBC v11→v10 Phase-2 track.
- **priority / auction** — these ship as genesis-configured state modules
  (params + state on-chain, consumed by the routing layer). Their upstream
  tx/query *services* are not defined (`RegisterServices` is empty upstream), so
  they have no live user-facing tx/query surface yet. Wiring those services is the
  natural next step if they need a direct user surface.
- **workflows** — the **author-facing lifecycle is now live**: a gogo-generated
  `Msg` service (`publish-workflow` / `upgrade-workflow` / `deactivate-workflow` /
  `top-up-bond` / `withdraw-bond`) + `Query` service (`params` / `workflow` /
  `author-bond`), CLI, and a PoC Workflows panel that drives them. Verified e2e:
  publish (bond escrowed in state + locked) → upgrade (semver-compatible new
  version) → deactivate (releases the bond lock) → withdraw (refused while locked,
  succeeds once unlocked). Two pieces remain for testnet:
  - **Bond bank-escrow.** The author bond is tracked in module state
    (`AuthorBondRecord`) but the keeper holds **no BankKeeper**, so the bond is
    not yet actually moved from the author's balance. Wire
    `SendCoinsFromAccountToModule` (+ a workflows module account, refund on
    withdraw, route slashes) before mainnet.
  - **Quote / invoke (router-daemon surface).** `QuoteWorkflow` and
    `InvokeWorkflow` exist in the keeper but require the **router's private key**
    (signed quotes) and run credits settlement with ed448 step receipts — they
    belong to the off-chain router daemon, not a generic CLI/PoC. They land with
    that daemon tier, not here.
- **router** — the `Record*` telemetry txs are module-authority-gated and driven
  by the off-chain router over gRPC; only the tool-owner `record-activation`
  path and the read queries are exposed on the CLI.

## Known PoC conveniences (not production patterns)

- The PoC daemons shell out to `lumerad` with the **test keyring** (server-side
  signing) — fine for a local demo, not production key management.
- Tool *execution* is a placeholder transform (real tools plug into
  `mcp-router/executeTool`); `oracle-feed` does a genuine live price fetch.
- `run-localnet.sh` applies demo shortcuts in genesis (widened supernode +
  payment_rails oracle staleness so seeded values persist; a seeded USDC/USD
  oracle price; a lowered passport min-stake) — all clearly commented.
