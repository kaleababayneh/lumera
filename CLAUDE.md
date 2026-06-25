# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Lumera is a Cosmos SDK blockchain (v0.53.6) built with Ignite CLI, supporting CosmWasm smart contracts, IBC cross-chain messaging, and four custom modules. The binary is `lumerad`, the native token denom is `ulume`, and addresses use the `lumera` Bech32 prefix.

## Build & Development Commands

```bash
# Build
make build                    # Build lumerad binary -> build/lumerad
make build-debug              # Build with debug symbols
make build-proto              # Regenerate protobuf files (cleans first)
make install-tools            # Install all dev tools (buf, golangci-lint, goimports, etc.)

# Lint
make lint                     # golangci-lint run ./... --timeout=5m

# Tests
make unit-tests               # go test ./x/... -v -coverprofile=coverage.out
make integration-tests        # go test ./tests/integration/... -v
make system-tests             # go test -tags=system ./tests/system/... -v
make systemex-tests           # cd tests/systemtests && go test -tags=system_test -v .
make simulation-tests         # ignite chain simulate

# Run a single test
go test ./x/claim/... -v -run TestClaimRecord
go test -tags=integration ./tests/integration/... -v -run TestMsgClaim
cd tests/systemtests && go test -tags=system_test -v . -run 'TestSupernodeMetricsE2E'

# evmigration integration tests REQUIRE -tags='integration test'
# (without 'test', the cosmos-evm chainConfig guard makes every subtest
# silently skip). The package's TestMain fails fast when the tag is missing.
go test -tags='integration test' ./tests/integration/evmigration/... -v

# EVM-specific
make openrpc                  # Regenerate OpenRPC spec -> docs/openrpc.json + app/openrpc/openrpc.json.gz

# EVM integration tests (under tests/integration/evm/)
# Most EVM suites use -tags='integration test'; IBC ERC20 suite uses -tags='test'
go test -tags='integration test' ./tests/integration/evm/contracts/... -v -timeout 10m
go test -tags='integration test' ./tests/integration/evm/jsonrpc/... -v -timeout 10m
go test -tags='integration test' ./tests/integration/evm/feemarket/... -v -timeout 10m
go test -tags='integration test' ./tests/integration/evm/mempool/... -v -timeout 10m
go test -tags='integration test' ./tests/integration/evm/precompiles/... -v -timeout 10m
go test -tags='integration test' ./tests/integration/evm/precisebank/... -v -timeout 10m
go test -tags='integration test' ./tests/integration/evm/vm/... -v -timeout 10m
go test -tags='integration test' ./tests/integration/evm/ante/... -v -timeout 10m
go test -tags='test' ./tests/integration/evm/ibc/... -v -timeout 5m
# All EVM integration tests at once:
go test -tags='integration test' ./tests/integration/evm/... -v -timeout 15m

# Devnet (local Docker testnet with 5 validators + Hermes relayer)
make devnet-new               # Full clean rebuild + start
make devnet-build-default     # Build devnet from default config
make devnet-up                # Start containers (attached)
make devnet-up-detach         # Start containers (detached)
make devnet-down              # Stop and remove containers
make devnet-stop              # Stop containers (keep state)
make devnet-start             # Start stopped containers
make devnet-clean             # Remove all devnet data (/tmp/lumera-devnet-1/)
make devnet-refresh-bin       # Build lumerad + copy it (and libwasmvm) into devnet/bin/
make devnet-update-binaries   # Stage already-built ${BUILD_DIR}/lumerad (+libwasmvm) into /shared/release and restart containers (START_MODE=run, preserves chain state; errors if lumerad is missing — run `make build` first)
make devnet-update-binaries-default # Stage an already-built devnet/bin/ into containers + restart (run devnet-refresh-bin first)
make devnet-update-scripts    # Update devnet scripts in containers
make devnet-reset             # Reset chain state, keep config
make devnet-evm-upgrade       # Run EVM upgrade on devnet
```

### Current shared devnet host notes

For the shared EVM migration/devnet environment, SSH via:

```bash
ssh lumera-devnet
```

Important paths and containers:

- Host devnet root: `/tmp/lumera-devnet-1`
- Shared host path: `/tmp/lumera-devnet-1/shared`
- Shared container path: `/shared`
- Accounts file: `/tmp/lumera-devnet-1/shared/release/accounts-devnet.json` on the host, `/shared/release/accounts-devnet.json` in containers
- Validator containers: `lumera-supernova_validator_1` through `lumera-supernova_validator_5`
- Hermes container: `lumera-hermes`
- Container shell: `docker exec -it lumera-supernova_validator_1 bash`
- Validator data bind mounts: `/tmp/lumera-devnet-1/supernova_validator_N-data` on the host maps to `/root/.lumera` in `lumera-supernova_validator_N`
- Migration scripts in validator containers: `/root/scripts/migration/migrate-account.sh`, `/root/scripts/migration/migrate-validator.sh`, `/root/scripts/migration/migrate-multisig.sh`

For manual multisig migration work, run from inside a validator container that has the relevant keyring entries. The generated devnet multisig records in `accounts-devnet.json` list the composite and member key names, while the actual keyring entries live under the validator container's `/root/.lumera`. Save full transcripts and proof artifacts under `/shared/release/migration-transcripts/<account>-<timestamp>/` so they appear on the host under `/tmp/lumera-devnet-1/shared/release/migration-transcripts/`.

**Note**: `claims.csv` is only needed if genesis `TotalClaimableAmount > 0` (claiming period ended 2025-01-01; default is now 0).

**Rule**: After completing any multi-file code change, run `make lint` and fix any issues before considering the task done. Lint must pass cleanly (0 issues).

## Architecture

### Cosmos SDK App (depinject wiring)

The app uses Cosmos SDK's **depinject** for module wiring. Configuration is declarative in `app/app_config.go` (module list, genesis order, begin/end blocker ordering). The main `App` struct with all keeper fields is in `app/app.go`. Chain upgrades are registered in `app/upgrades/` with version-specific handlers.

### Custom Modules (`x/`)

| Module | Path | Purpose |
|--------|------|---------|
| **action** | `x/action/v1/` | Distributed action processing for GPU compute jobs |
| **claim** | `x/claim/` | Token claim distribution (Bitcoin-to-Cosmos bridge) |
| **lumeraid** | `x/lumeraid/` | Identity management (Lumera ID / PastelID) |
| **supernode** | `x/supernode/v1/` | Supernode registration, governance, metrics, and evidence |

Each module follows standard Cosmos SDK layout:
- `keeper/` - State management and message server implementation
- `module/` - Module definition, depinject providers, AppModule interface
- `types/` - Message types, params, errors, keys, protobuf-generated code
- `simulation/` - Simulation parameters
- `mocks/` - Generated mocks (go.uber.org/mock)

### IBC Stack

IBC v10 with: core IBC, transfer, interchain accounts (host + controller), packet-forward-middleware. Light clients: Tendermint (07-tendermint), Solo Machine (06-solomachine). IBC router and middleware wiring is in `app/app.go` (search for `ibcRouter`).

### Protobuf

Proto definitions live in `proto/lumera/`. Code generation uses `buf` with two templates:
- `proto/buf.gen.gogo.yaml` - Go message/gRPC code
- `proto/buf.gen.swagger.yaml` - OpenAPI specs

Generated files land in `x/*/types/` as `*.pb.go`, `*_pb.gw.go`, `*.pulsar.go`.

### Ante Handlers

Custom ante handler in `ante/delayed_claim_fee_decorator.go` - a fee decorator specific to claim transactions. Dual-route EVM ante handler in `app/evm/ante.go` routes Ethereum extension txs to the EVM path and all others to the Cosmos path.

### EVM Stack (Cosmos EVM v0.6.0)

Four EVM modules wired in `app/evm.go`:

| Module | Purpose |
| -------- | ------- |
| `x/vm` | Core EVM execution, JSON-RPC, receipts/logs |
| `x/feemarket` | EIP-1559 dynamic base fee |
| `x/precisebank` | 6-decimal `ulume` ↔ 18-decimal `alume` bridge |
| `x/erc20` | STRv2 token pair registration, IBC ERC20 middleware |

Key files:

- `app/evm.go` - Keeper wiring, circular dependency resolution (`&app.Erc20Keeper` pointer)
- `app/evm/ante.go` - Dual-route ante handler (EVM vs Cosmos path)
- `app/evm/precompiles.go` - Static precompiles (bank, staking, distribution, gov, ics20, bech32, p256, slashing)
- `app/evm_mempool.go` - EVM-aware app-side mempool wiring + wrapped CheckTx rejection counter
- `app/evm_mempool_metrics.go` - Prometheus collector (gauges: size, pending, queued, broadcast_queue_depth; labeled counter: rejections_total{source,reason})
- `app/evm_broadcast.go` - Async broadcast queue (prevents mempool deadlock)
- `app/evm_runtime.go` - RegisterTxService/Close overrides for EVM lifecycle
- `app/ibc.go` - IBC router with ERC20 middleware for v1 and v2 transfer stacks
- `config/evm.go` - Chain ID, base fee, consensus max gas constants
- `app/openrpc/` - Gzip-compressed embedded OpenRPC spec served via `rpc_discover` and `/openrpc.json`; POST proxy for playground compatibility

EVM integration tests live in `tests/integration/evm/` with subpackages: ante, contracts, feemarket, ibc, jsonrpc, mempool, precisebank, precompiles, vm. Most use `//go:build integration` tag; the IBC ERC20 tests use `//go:build test`.

**Rule**: When adding or modifying EVM tests, update `docs/evm-integration/tests.md` — add new tests to the appropriate table (Unit Tests, Integration Tests, or Devnet Tests) and reference them from the related bug entry in `docs/evm-integration/bugs.md` if applicable.

**Rule**: When making significant changes to EVM code (precompile ABI changes, new module integrations, ante handler updates, new precompiles), update the relevant docs in `docs/evm-integration/` — especially `precompiles/*.md` for precompile changes and `main.md` for architectural changes.

### Test Utilities

`testutil/` provides:
- `keeper/` - Per-module keeper test setup helpers (action, claim, supernode, pastelid)
- `sample/` - Sample data generators for test fixtures
- `network/` - Test network configuration
- `mocks/` - Keyring mocks

### Key Configuration

- Go toolchain: 1.26.1
- Bech32 prefixes defined in `config/config.go` (lumera, lumeravaloper, lumeravalcons)
- Chain denom: `ulume` (coin type 60 / Ethereum-compatible, EVM extended denom `alume` at 18 decimals)
- EVM chain ID: `76857769`, key type: `eth_secp256k1`
- CosmWasm: wasmd v0.61.6 with wasmvm v3.0.3 (requires `libwasmvm.x86_64.so` at runtime)
- Ignite scaffolding comments (`# stargate/app/...`) mark extension points - preserve these when editing

---

# Lumera AI Module Port — Progress Log

**Authoritative plan:** `docs/LUMERA_AI_INTEGRATION_PLAN.md` — end goal (all lumera_ai chain
modules → testnet → mainnet), the repeatable per-module recipe, dependency sequence, deploy
steps, and the module tracker. **Read it first.** This section is the live status only.

Porting lumera_ai's core modules onto this chain (target: **SDK v0.53.6 / IBC v10.5.0 /
CometBFT v0.38.21 / Go 1.26.2**). Approach: land each module with its not-yet-ported
dependency keepers **stubbed** (every stub marked `// TEMPORARY: replace with real <module>
keeper before mainnet`), then replace each stub with the real module **one-by-one** until it's
real end-to-end. **Production target — no stubs may ship to mainnet.**

## VISION COMPLETE end-to-end (2026-06-21)
The full agentic economy runs on a local node: **discover → meter → execute → prove → settle**.
- **On-chain trust graph + flywheel** (all verified e2e, no stubs): registry (tool discovery + bonds +
  PoS receipts + dispute→slash), credits (settlement gated on PoS), incentives (reputation badges),
  insurance/oracle/policies/reserve/nft, supernode (PoS attestor, read-only). 5 thesis primitives
  on-chain except Composable Intelligence (`workflows`, deferred as adjacent/redundant). **Hilt reached.**
- **Agent layer** (`poc/`): a web PoC (`poc/web`) visualizing the flywheel, and an **MCP router**
  (`poc/mcp-router`) — a Model Context Protocol server so real AI agents discover + call on-chain tools,
  each call metered → executed → **proven (BLAKE3 PoS receipt)** → settled (publisher paid). Verified:
  an agent over MCP called `pubtool`, got the result + on-chain proof, publisher paid 800000ulac.
- **Deferred (not on the critical path):** `priority`/`auction` (router-support libraries, inert until
  the router is wired), `workflows` (protobuf-go messages with no proto source — needs reconstruction),
  `router` (capstone); incentives Phase-2 self-feed (metrics from PoS receipts + disputes);
  reject-on-expiry EndBlocker for disputes; SGX `EnclaveQuote` verification; the deferred test suites.

## Module-port wave 2 — 5 more modules integrated + e2e-verified (2026-06-23)
Continuing the lumera_ai → lumera port with the §4 recipe; each builds green, boots on a single-node
localnet, and is verified end-to-end (all committed, no stubs):
- **`x/vaults`** — prepaid-capacity vaults over the reserve keeper (tiered prepaid discounts).
  Verified: create vault tier bronze, 1,000,000 ulac escrowed, 250 bps discount.
- **`x/passport`** — agent identity / stake (a thesis trust primitive). maccPerms Burner (slash burns
  stake). Verified: register passport (100 LUME staked) → query → by-agent → top-up (+50 LUME). Found &
  fixed a real `proto.Clone` panic (gogo math.Int customtype) in the query server, same class as
  credits/insurance — now `deepCopyProto` marshal/unmarshal.
- **`x/cac`** — content-addressable cache (BLAKE3), cache-hit royalty split. Added
  `registry.IsDeterministicTool` (reads ToolCard CachePolicy.Deterministic). Verified: cache-store →
  stats/entry/lookup/tool-entries; LegacyDec hit_rate + stdtime timestamps work.
- **`x/challenges`** — grand-challenge / tournament (escrowed prize pools, entry fees, scored
  submissions, ranked payouts). Registry adapter for category eligibility; SLO-probe/identity-attestation
  challenge types deferred (registry SLO state + lumeraid nonce not yet present). Verified: create
  (5,000,000 ulac prize escrow) → join×2 (entry fees) → activate (min-participants enforced).
- **`x/payment_rails`** — programmable settlement (deposit a bridged asset → oracle-priced LAC mint;
  withdraw/refund). Setter-injected bank+credits+oracle. IBC settlement is inert local state (packet
  hooks deferred). Verified full money path: deposit 1,000,000 usdc (USDC/USD oracle price seeded in
  genesis) → 997,000 ulac minted (1,000,000 − 30bps acq fee), 1,000,000 usdc escrowed.

## Module 1: `x/credits`

- [x] Copy `x/credits` + dependency-cluster type pkgs (`reserve`, `nft`, `registry`, `cac`,
      `passport` types) + `internal/{logging,moneyguard}`; import paths rewritten
      `lumera-ai` → `lumera`.
- [x] v0.54→v0.53 changes so far: **`cosmossdk.io/log/v2` → `cosmossdk.io/log`** (only one yet);
      added deps `gowebpki/jcs`, `shopspring/decimal`, `lib/pq`.
- [x] De-tagged for the real tagless build (removed `//go:build cosmos*`; deleted `!cosmos`
      lite variants). `x/credits/ibc/` left gated → **deferred to Phase 2 (IBC v11→v10)**.
- [x] **Tagless `go build` of credits (non-ibc) + type pkgs: GREEN** — compiles in lumera's
      real config.
- [x] depinject wiring: `proto/lumera/credits/module/module.proto`,
      `x/credits/module/depinject.go`, 4 **stub** keepers (Insurance/Registry/Reserve/NFT),
      registered in `app/app_config.go` + `&app.CreditsKeeper` in `app/app.go`.
- [x] **`go build` of full `lumerad` with credits wired: GREEN** (191 MB binary).
      (`make build` itself fails on macOS GNU make 3.81 — `$(strip $(shell …))` parse error;
      use `gmake` or build on Linux. `go build -o … ./cmd/lumera` works.)
- [x] ✅ **UNBLOCKED — gogo conversion done; node boots, runs, and serves credits queries.**
      Converted credits + its type cluster (reserve/passport/nft/cac/registry) from protobuf-go to
      gogoproto (annotated protos, regenerated with `protoc-gen-gocosmos`, deleted stale
      `*_grpc.pb.go` + HTTP-less `*.pb.gw.go`, rewrote the hand-written API: `timestamppb`→`time.Time`,
      `basev1beta1.Coin`→`sdk.Coin`, `Msg_ServiceDesc`→`Msg_serviceDesc`). `go build ./x/credits/...`
      GREEN; full `lumerad` builds; **`lumerad init` boots the depinject graph and a single-node
      localnet produces blocks**; `lumerad query credits params` returns live state over gRPC.
      Three additional fixes were required for `start` (see the plan §8 "Gotchas"): add
      `cosmos.msg.v1.service` + `cosmos.msg.v1.signer` proto options (passport/nft); a gogo pointer
      `collPtrValue[T]` ValueCodec to keep the keeper's `*types.X` collection semantics
      (`x/credits/keeper/collections_codec.go`); v1 grpc-gateway query registration in `module.go`.
- [x] **Smoke test on a localnet: PASS** (block production + `query credits params`).
- [ ] Port unit tests (deferred — `*_test.go` still on the old protobuf-go API; non-test build green).
- [ ] Replace the 4 stub keepers — needs full *keeper* ports of insurance/registry/reserve/nft
      (those four are currently types-only in lumera).

### ✅ Resolved (core team, 2026-06-19) — gogoproto only → Option A
Core team: **lumerad cannot host protobuf-go module state/messages.** Module message/state
types MUST be gogoproto (`protoc-gen-gocosmos` + `grpc-gateway`; 50 `.pb.go` / 0 `.pulsar.go`).
protobuf-go coexists only for wiring/CLI/signer metadata — never anything hitting the codec or
consensus. **Option B is ruled out. Path = Option A: convert each ported module to gogoproto:**
1. Add gogo annotations to the `.proto` (`(gogoproto.customtype)` `sdk.Coin`, `(gogoproto.stdtime)`,
   `(gogoproto.nullable)=false`, etc.) to match lumera's conventions.
2. Regenerate with `protoc-gen-gocosmos` + `grpc-gateway`; delete the protobuf-go `*_grpc.pb.go`.
3. Rewrite hand-written code that used the protobuf-go API → gogo (`basev1beta1.Coin` → `sdk.Coin`;
   `timestamppb` → `time.Time`; drop `ProtoReflect`/`rawDescGZIP`; gogo-style grpc registration).
   **This is the real porting cost, per module.**
Order: cac/reserve/nft/registry/passport types → credits → (later) registry/router/payment_rails.

### Stubs — ALL REMOVED ✅
`x/credits/module/stubs.go` is **DELETED**. All four credits dependency keepers are real + wired in
`ProvideModule`: **insurance, registry, reserve, nft**. The full settlement loop runs end-to-end on a
localnet (swap LUME→LAC → register tool → lock → settle → publisher paid 543,200 ulac; 3% burn;
reserve discount commitments creatable). Modules 3–7 (`registry, oracle, policies, nft` + `reserve`
keeper) ported + wired + e2e-verified. Remaining toward testnet: port the deferred test suites, the
registry sub-slices (bonds/disputes/SLA/SLO), and the inference Proof-of-Service track.

## Module 3: `x/registry` — keeper slice ported + wired + e2e-verified (+ bond slice)
Built a **focused, modern registry keeper** (ToolCard registry + `GetToolPublisher`, on
KVStoreService + collections + `collPtrValue`) instead of wholesale-porting the legacy ~17k-line
keeper. `MsgRegisterTool` + `GetTool` query + minimal CLI; the rest of the registry RPCs no-op via
`UnimplementedMsgServer/QueryServer` and port as later slices. Verified e2e: `tx registry
register-tool` (code 0) → `query registry get-tool` returns the owner/publisher. Replaces
`stubRegistryKeeper` in credits → unblocks `MsgSettleCredits` publisher payout. (Modules 4/5: `oracle`,
`policies` — standalone full modules, ported + wired + running.)

**Bond slice (Step 3 — publisher skin-in-the-game, 2026-06-21):** `register-tool` now escrows the
publisher's bond (`MinBondAmount` floor, `BondDenom=ulume`) into the registry module account.
`x/registry/keeper/bond.go` (native `sdk.Coins`/`time.Time`): `CreateBond` (escrow/top-up),
`WithdrawBond` (reclaim only the excess above the minimum while registered), bond record CRUD,
`MsgCreateBond`/`MsgWithdrawBond` handlers, `GetBond` query, CLI (`--bond`/`create-bond`/
`withdraw-bond`/`get-bond`), genesis import/export. Added `{Account: registry}` to maccPerms (custody,
no mint/burn). Slash/lock/SLA/restitution are later slices. Verified e2e: register escrows 1,000,000
ulume → top-up → partial withdraw → full-withdraw rejected (code 8, below minimum) → settlement loop
still pays the publisher 543,200 ulac.

**Proof-of-Service slice (Step 4 — Verifiable Execution, 2026-06-21):** settlement now requires a
SuperNode-attested inference receipt. Design: `docs/STEP4_PROOF_OF_SERVICE.md`. `x/registry`
implements `SubmitReceipt` (submitter must be an **active SuperNode** via the injected
`sntypes.SupernodeKeeper`; `receipt_id = pos1<hex(BLAKE3(input,model,output))>` binds to `TraceHash`;
idempotent; `ReceiptPrefix=0x03`) + `ValidateReceipt` + `GetReceipt` query + `submit-receipt`/
`get-receipt` CLI (`lukechampine.com/blake3`) + genesis. `x/credits` `SettleCredits` gates on
`registryKeeper.ValidateReceipt(receiptID, lockToolID)` (always-on; `receipt_verified` event).
Verified e2e: register supernode → settle-without-receipt **rejected** → submit-receipt (active SN) →
settle-with-receipt **pays 543,200 ulac** → submit-receipt from a non-supernode **rejected** (code 19).
No `x/action`/`x/supernode` changes (read-only). SGX `EnclaveQuote` verification and a governance
`ProofOfServiceRequired` toggle are Phase-2.

**Dispute → slash slice (Step 3 ⊗ Step 4, 2026-06-21):** the full trust loop bond→proof→dispute→slash.
No proto regen (Challenge/MsgChallengeReceipt/MsgSettleReceipt/PendingSlash + ChallengePrefix already
generated). `x/registry/keeper/bond.go` gains `LockBond`/`UnlockBond`/`SlashBond` + `splitSlashCoins`
(restitution: 5% burn / 85% insurance / 10% fee_collector, bps imported from `x/insurance/types`).
`keeper/challenge.go`: `OpenChallenge` (escrow stake + lock equal bond + receipt→disputed),
`UpholdChallenge` (unlock→slash→refund stake→receipt invalid). `ChallengeReceipt` (anyone, in window) +
`SettleReceipt`=adjudicate-uphold (active-supernode-gated). `ValidateReceipt` refuses disputed/invalid
receipts. registry maccPerms += Burner. CLI challenge-receipt/resolve-dispute/get-challenge. Verified
e2e: challenge→disputed+locked→settle blocked→uphold slashes 500,000 (burn 25k/ins 425k/treasury 50k),
bond 2M→1.5M, receipt invalid, stake refunded, insurance +425k. Reject-on-expiry EndBlocker = next.

## Module 2: `x/insurance` — DONE (ported + wired + booting)
Full module (keeper/msg-server/begin+end-blockers/JSON genesis) gogo-converted with the credits
recipe (incl. the `collPtrValue` collections codec), `module/depinject.go` +
`proto/lumera/insurance/module/module.proto` created, registered in `app/app_config.go` (Burner
module account), and **replaces `stubInsuranceKeeper` in credits**. Node boots + produces blocks with
insurance begin/end blockers running. (`keeper/claims.go`+`keeper/bonds.go` stay `todo_*`-gated.)

## Remaining modules (same recipe, dependency order)
To remove the 3 remaining credits stubs, port the full **keepers** for `reserve`, `nft`, `registry`
(currently types-only) and wire each into credits' `ProvideModule`. Then the agent loop: `router`
(needs credits+registry) and `payment_rails` (needs credits). Then the non-core economic modules
(`incentives, auction, challenges, oracle, policies, priority, vaults, workflows`) and the IBC track
(`ibc_action`, Phase 2). Port `*_test.go` to verify money paths before testnet. Each port = the
recipe in `docs/LUMERA_AI_INTEGRATION_PLAN.md` §4 + the §8 gotchas.
