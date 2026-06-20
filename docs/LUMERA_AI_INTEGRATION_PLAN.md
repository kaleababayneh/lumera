# Lumera AI → Lumera L1 Integration — Master Plan

> Self-contained plan to integrate the **lumera_ai** chain modules into the **Lumera node
> (`lumerad`)** as native gogoproto modules, then ship to **testnet** and **mainnet**.
> Written so any contributor or new session can pick it up with no prior context.
> Companion to the live status in `CLAUDE.md` ("Lumera AI Module Port — Progress Log").
> Last updated: 2026-06-20.

---

## 0. End goal & definition of done

Integrate **all** lumera_ai chain (`x/`) modules into `lumerad` as native, **gogoproto** Cosmos
SDK modules wired through the chain's depinject app, then:

1. Build + boot a local node that includes the modules and processes their txs.
2. Ship to **testnet** (`lumera-testnet-2`) via an on-chain governance software-upgrade and pass
   full end-to-end flows (the agent loop: discover → quote → invoke → settle, with on-chain
   credits settlement + signed receipts).
3. Security-audit, then ship to **mainnet** (`lumera-mainnet-1`) via a governance upgrade.

**Done =** mainnet runs the modules; **no stubs remain**; build/lint/test/sim are green; an
external agent can exercise the settlement loop on-chain.

---

## 1. Repos, environment, conventions

- **Target repo (this one):** the Lumera node. Module path `github.com/LumeraProtocol/lumera`.
  Cosmos SDK v0.53.x, **gogoproto**, **depinject** app wiring, EVM (`cosmos/evm` v0.6.0) + wasmd
  + IBC v10. Existing custom modules: `action, audit, claim, erc20policy, evmigration, lumeraid,
  supernode` (+ EVM: vm/feemarket/precisebank/erc20).
- **Source repo:** lumera_ai (the product). Module path `github.com/LumeraProtocol/lumera-ai`.
  Cosmos SDK v0.54, **protobuf-go**, IBC v11. Local clone expected at a sibling path (`../lumera_ai`).
- **Target version stack** (verified live on **both** mainnet and testnet, app `v1.12.0`, on
  2026-06-19; `main`/v1.20 line shown on the right):

  | Component | mainnet/testnet (v1.12.0) | main (v1.20 line) |
  | --------- | ------------------------- | ----------------- |
  | Cosmos SDK | v0.53.5 | v0.53.6 |
  | ibc-go | v10.5.0 | v10.5.0 |
  | CometBFT | v0.38.21 | v0.38.21 |
  | wasmd / wasmvm | v0.61.6 / v3.0.2 | v0.61.6 / v3.0.3 |
  | Go | 1.25.9 | 1.26.2 |

- **Chain identity:** denom `ulume` (6 dp; EVM extended denom `alume`, 18 dp), **coin type 60**
  (Ethereum-compatible — NOT 118), key type `eth_secp256k1`, EVM chain ID `76857769`,
  min-gas `0.025ulume`, bech32 prefix `lumera`. Chain IDs: `lumera-mainnet-1`, `lumera-testnet-2`.
- **Git policy:** all work → the **fork (`origin`)** only. **Never** push to / open PRs against
  upstream `LumeraProtocol/lumera`. Commits are made by a human reviewer, not automation.
- **Toolchain:**
  - `make build` fails on macOS GNU make 3.81 (`$(strip $(shell …))` parse error). Use **GNU
    make 4+ (`gmake`)** or build on **Linux**. Portable compile: `go build -o /tmp/lumerad ./cmd/lumera`.
  - `buf` via `go install github.com/bufbuild/buf/cmd/buf@v1.50.0` (lumera pins none).
  - Full build needs cgo + libwasmvm (static on darwin; install `libwasmvm` on Linux).

---

## 2. The core finding — why this is a *conversion*, not a *port*

lumera_ai modules are generated with **protobuf-go** (`protoc-gen-go` + `protoc-gen-go-grpc`:
separate `*_grpc.pb.go`, `timestamppb.Timestamp`, `basev1beta1.Coin`, `ProtoReflect`,
`Msg_ServiceDesc`/`rawDescGZIP`). **Lumera is gogoproto-only** for anything that touches the
codec or consensus (core team confirmed: `protoc-gen-gocosmos` + grpc-gateway; 50 `.pb.go` /
0 `.pulsar.go`; protobuf-go exists only for wiring/CLI/signer metadata).

⇒ **Each module must be converted from protobuf-go to gogoproto.** Copying the generated
`.pb.go` does not work (they panic at descriptor registration under lumera's protobuf-go
runtime). This conversion — regenerate + rewrite the hand-written code that used the protobuf-go
API — is the dominant, repeated cost of this project.

---

## 3. Module inventory & disposition (22 lumera_ai `x/` modules)

| lumera_ai module | Disposition | Notes / dependencies |
| --- | --- | --- |
| `credits` | **Port** | Core settlement loop. Deps: cac, reserve, nft, registry, passport, insurance |
| `registry` | Port | ToolCards / bonds / slashing / receipts. Dep: passport |
| `router` | Port | discover/quote/invoke (on-chain side). Deps: credits, registry |
| `payment_rails` | Port | deposits/withdraw + IBC settlement. Dep: credits |
| `passport` | Port | reputation (distinct from `lumeraid`) |
| `reserve` / `nft` / `cac` | Port | credits type-deps (reserve alloc / toolpack NFT / cache royalties) |
| `insurance` | Port | credits dep (insurance pool) |
| `incentives`, `auction`, `challenges`, `oracle`, `policies`, `priority`, `vaults`, `workflows` | Port (later) | Non-core; sequence after the loop works on-chain |
| `ibc_action` | Port (Phase 2) | IBC v11 → v10 |
| `lumeraid` | **Reconcile → use lumera's, drop lumera_ai's** | Both have it; lumera's is the canonical identity module (Ed448) |
| `feemarket` | **Reconcile / skip** | lumera already has `cosmos/evm` feemarket; assess whether lumera_ai's is a different (router) fee market → rename or drop |
| `wasm` | **Reconcile / skip** | lumera has wasmd; assess lumera_ai `x/wasm` |
| `ibc` | **Reconcile** | lumera has IBC v10 core; lumera_ai `x/ibc` is an app-level router → fold into lumera's IBC wiring |

**Off-chain (NOT node modules — separate track):** the MCP router daemon, storefront, explorer,
SDKs, and adapters. These talk to `lumerad` over gRPC/REST; point them at the node after the
chain side lands. Out of scope for node integration.

---

## 4. The repeatable per-module recipe

Do this for each module, in dependency order (§5). Keep a row per module in the tracker (§10).

1. **Copy** the module's Go (`x/<m>/`) and protos (`proto/lumera/<m>/`) from lumera_ai into
   lumera. Rewrite import paths: `github.com/LumeraProtocol/lumera-ai` → `github.com/LumeraProtocol/lumera`.
2. **Proto → gogo:**
   - Set each `.proto` `option go_package` to the **relative** form (`x/<m>/types`), NOT the full
     module path (a full path makes buf write into a stray `./github.com/...` dir).
   - Add gogo annotations matching lumera's conventions: `(gogoproto.customtype)` for `sdk.Coin`/
     `sdk.Coins`/`sdkmath.Int`, `(gogoproto.stdtime) = true` for timestamps, `(gogoproto.nullable)
     = false` where lumera does. (Compare against an existing lumera module's `.proto`, e.g.
     `proto/lumera/supernode/...`.)
   - Generate: `buf generate --template proto/buf.gen.gogo.yaml --path proto/lumera/<m>`
     (do **not** run `make build-proto` — it runs `clean-proto` first and wipes generated code).
   - **Delete** the leftover protobuf-go `x/<m>/types/*_grpc.pb.go` (gogo emits gRPC inline).
3. **Strip build tags:** remove `//go:build cosmos` / `cosmos && cosmos_full` lines; delete the
   `!cosmos` / `cosmos && !cosmos_full` lite/stub variant files; leave `generate_goldens` /
   `future_migration` / `ignore`-tagged files gated; defer the `ibc/` subpackage (keep gated) to
   Phase 2.
4. **Rewrite hand-written code (protobuf-go → gogo API)** — the real work. Iterate
   `go build ./x/<m>/...` until green. Common fixes:
   - `*basev1beta1.Coin` → `sdk.Coin`; `*timestamppb.Timestamp` → `time.Time`.
   - Remove `.AsTime()` and `!= nil` checks on now-value `time.Time` fields.
   - Drop references to `Msg_ServiceDesc`, `file_*_rawDescGZIP`, `ProtoReflect`; use gogo's
     `msgservice.RegisterMsgServiceDesc` / `RegisterInterfaces` style (mirror a lumera module's
     `types/codec.go`).
   - `cosmossdk.io/log/v2` → `cosmossdk.io/log` (v0.54→v0.53).
5. **depinject wiring** (mirror `x/lumeraid/module/`):
   - `proto/lumera/<m>/module/module.proto` (config message; relative `go_package`); regen.
   - `x/<m>/module/depinject.go`: `init() { appmodule.Register(&Module{}, appmodule.Provide(ProvideModule)) }`,
     `ModuleInputs` (StoreService, Cdc, Config, + dep keepers), `ModuleOutputs` (Keeper + AppModule),
     `ProvideModule`. Stub not-yet-ported dep keepers (mark `// TEMPORARY`).
   - `app/app_config.go`: add to `Modules`, `moduleAccPerms` (minter/burner if it mints/burns),
     and the begin/end-block + `genesisModuleOrder` lists (only the ones the module implements).
   - `app/app.go`: add the keeper field + `&app.<M>Keeper` to `depinject.Inject`.
6. **Replace stubs:** once a dependency module is ported, swap its stub in dependents' `ProvideModule`
   for the real keeper.
7. **Build + boot:** `go build -o /tmp/lumerad ./cmd/lumera`; `lumerad init <m> --chain-id test
   --home /tmp/<m>` (this runs `depinject.Inject` → proves the graph resolves + writes genesis);
   then a localnet smoke tx/query.
8. **Tests:** port `*_test.go` + simulation hooks; `go test ./x/<m>/...`, `make sim-test`. Get
   lint/test/sim/proto green (CI gate).
9. **DoD per module:** builds, boots, a tx works on a localnet, tests green, **no stubs for this
   module**, `make lint` clean.

---

## 5. Dependency-ordered sequence

1. **Type-dep cluster (credits prerequisites):** `passport` → `reserve` → `nft` → `cac` →
   `registry` (registry imports passport) — convert each to gogo (recipe §4 steps 1–4 are enough
   for the *types* packages credits needs; full wiring when each becomes a standalone module).
2. **`insurance`** (credits pool dep).
3. **`credits`** — replace its 4 stubs (insurance/registry/reserve/nft) with the real keepers as
   each lands. First module fully booted + smoke-tested = the integration is proven.
4. **`router`** (needs credits + registry), **`payment_rails`** (needs credits).
5. **Remaining core/economic:** `incentives`, `auction`, `challenges`, `oracle`, `policies`,
   `priority`, `vaults`, `workflows`.
6. **IBC track (Phase 2):** `ibc_action`, credits fee-split middleware, `payment_rails` real IBC
   send — all on IBC v10.

---

## 6. Cross-cutting integration

- **lumeraid:** drop lumera_ai's; point identity-dependent code at lumera's existing `lumeraid`.
- **supernode:** wire tool publishers / SLA / receipts through lumera's `supernode` (it's built
  for validator-operated services).
- **claim / vesting:** ensure credit purchases respect lumera's `claim`/vesting constraints.
- **Token model (DECISION NEEDED):** is LAC a separate on-chain denom or a mapping to `ulume`?
  Who may mint/burn it (credits module account has minter/burner)? How does it interact with
  `claim`/vesting? Resolve before payments are final.
- **Chain identity:** `ulume` (6 dp), coin type 60, EVM chain ID `76857769`, `eth_secp256k1`,
  min-gas `0.025ulume`, bech32 `lumera`. (lumera_ai's POC defaults — `stake`, `lumera-routerd` —
  must not leak in.)
- **IBC (Phase 2):** down-level `ibc_action` + credits fee-split middleware v11 → v10; make
  `payment_rails` actually `SendPacket` over a real channel (today it only writes state + emits an
  event); handle `ibc/<hash>` denom-trace for bridged USDC/INJ; decide ICA/ICQ; configure the
  relayer + channels to the chains actually used.

---

## 7. Phase 3 — deploy (testnet → audit → mainnet)

- **Genesis + upgrade handler:** add a new entry under `app/upgrades/` (follow the existing
  `v1_20_0` pattern) whose `StoreUpgrades.Added` lists every new module store key and whose
  handler runs each new module's `InitGenesis`. Define default params for each module.
- **Local rehearsal:** `make devnet-new` (5-validator Docker net) → run an in-place upgrade
  rehearsal.
- **Testnet:** deploy the upgrade on `lumera-testnet-2`; run the full agent loop end-to-end; fix.
- **Security audit:** audit the new modules + all credit/settlement/money paths (real funds).
- **Mainnet:** submit a governance software-upgrade proposal; coordinate validators; execute at
  the agreed height.

---

## 8. Current state (2026-06-20)

- **Phase 0 done:** target stack locked; fork cloned; baseline `lumerad` builds; decision
  resolved (gogoproto / Option A).
- **`credits` + type cluster (`reserve`/`passport`/`nft`/`cac`/`registry`) — gogo-converted, BUILDS,
  BOOTS, and RUNS.** A single-node localnet produces blocks and `lumerad query credits params`
  returns live state over gRPC. The protobuf-go→gogo conversion blocker is RESOLVED.
  - All six modules: gogo `.proto` annotated (nullable/stdtime/castrepeated/amino), regenerated with
    `protoc-gen-gocosmos`, stale `*_grpc.pb.go` + HTTP-less `*.pb.gw.go` deleted, hand-written code
    rewritten off the protobuf-go API (`timestamppb`→`time.Time`, `basev1beta1.Coin`→`sdk.Coin`,
    `Msg_ServiceDesc`→`Msg_serviceDesc`, dropped `rawDescGZIP`/manual `RegisterType`).
  - **credits depinject wiring complete:** `proto/lumera/credits/module/module.proto`,
    `x/credits/module/depinject.go`, 4 temporary stub keepers (`x/credits/module/stubs.go`),
    registered in `app/app_config.go` + `app/app.go`.
- **Run recipe (single-node localnet):** `go build -o /tmp/lumerad ./cmd/lumera` (cgo+libwasmvm,
  ~1min, 200 MB) → `lumerad init` → `keys add --algo eth_secp256k1` → `genesis add-genesis-account`
  (denom `ulume`) → `gentx`/`collect-gentxs` → `start --minimum-gas-prices=0ulume`.
- **Gotchas discovered (apply to every future module port):**
  1. lumera uses **grpc-gateway v1** — it does NOT regenerate a `*.pb.gw.go` for a `.proto` lacking
     `google.api.http`; the stale protobuf-go/v2 gw file must be deleted (else `missing ProtoReflect`).
     For credits, GET annotations were added to the `Query` service + `module.go` reverted to the
     standard v1 `RegisterQueryHandlerClient`.
  2. Every `service Msg` needs `option (cosmos.msg.v1.service) = true;` AND every request `Msg` needs
     `option (cosmos.msg.v1.signer) = "<field>";` — gogo+baseapp PANIC at `start` otherwise (the
     lumera_ai protobuf-go protos omitted these; passport/nft were fixed).
  3. The keeper's collections stored `*types.X` via `codec.CollValueV2` (protobuf-go). gogo's
     `codec.CollValue[T]` returns a **value** codec; to keep the keeper's pointer semantics use the
     `collPtrValue[T]` adapter in `x/credits/keeper/collections_codec.go` (a gogo pointer ValueCodec).
  4. Optional timestamps (code treats as "unset") → `(gogoproto.stdtime)=true` only (`*time.Time`);
     required → add `(gogoproto.nullable)=false` (`time.Time`). Drive the choice off the existing
     accessor/validator signatures.
- **`insurance` — PORTED + WIRED (2026-06-20).** Full module (keeper + msg server + begin/end
  blockers + JSON genesis) gogo-converted (same recipe as credits, incl. the `collPtrValue` codec),
  given a `module/depinject.go` + `proto/lumera/insurance/module/module.proto`, and registered in
  `app/app_config.go` (init/begin/end order, `Burner` module account, Modules list). The **real
  insurance keeper now replaces `stubInsuranceKeeper`** in `x/credits/module/depinject.go` (credits
  takes `insurancekeeper.Keeper` as a depinject input — no cycle: insurance imports only
  `credits/types`). Node boots + produces blocks with insurance's begin/end blockers running; credits
  still serves. **1 of 4 credits stubs removed.**
- **Stubs remaining (TEMPORARY — remove before testnet):** `stubRegistryKeeper`, `stubReserveKeeper`,
  `stubNFTKeeper` in `x/credits/module/stubs.go`. Replacing them requires porting full *keepers* for
  registry/reserve/nft (those three are currently **types-only** in lumera — credits imports only
  their type structs).
- **Tests deferred:** `*_test.go` across the cluster still reference the old protobuf-go API / not-yet
  ported modules and won't compile; the non-test build is green. Port tests in a later pass.

---

## 9. Open decisions & risks

- **Effort:** the per-module gogo conversion is the dominant cost — credits + its cluster ≈ days;
  all core modules ≈ multi-week; all 22 ≈ a substantial program. Plan/resource accordingly.
- **Token model** (LAC ↔ `ulume`, mint authority) — see §6.
- **feemarket / wasm / ibc** reconciliation vs lumera's existing EVM-feemarket / wasmd / IBC v10.
- **module-config proto:** confirm the gogo-generated appconfig `Module` registers correctly with
  depinject (credits' did compile; verify at boot).
- **Module interdependence:** lumera_ai modules assume each other; convert the dependency cluster
  together, not in isolation.

---

## 10. Per-module tracker

Legend: ☐ todo · ◐ in progress · ☑ done (builds + boots + tx + tests + no stubs + lint).

| Module | gogo proto | code rewrite | depinject wired | builds | boots | tx works | tests | stubs removed | Status |
| --- | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: |
| credits | ☑ | ☑ | ☑ | ☑ | ☑ | ◐ (query ✓) | ☐ | ☐ | ◐ |
| passport | ☑ | ☑ | — (types-only) | ☑ | ☑ | — | ☐ | — | ◐ |
| reserve | ☑ | ☑ | — (types-only) | ☑ | ☑ | — | ☐ | — | ◐ |
| nft | ☑ | ☑ | — (types-only) | ☑ | ☑ | — | ☐ | — | ◐ |
| cac | ☑ | ☑ | — (types-only) | ☑ | ☑ | — | ☐ | — | ◐ |
| registry | ☑ | ☑ | — (types-only) | ☑ | ☑ | — | ☐ | — | ◐ |
| insurance | ☑ | ☑ | ☑ | ☑ | ☑ | ◐ | ☐ | n/a | ◐ |
| router | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| payment_rails | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| incentives | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| auction | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| challenges | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| oracle | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| policies | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| priority | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| vaults | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| workflows | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| ibc_action | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ (Phase 2) |
| lumeraid | — | — | — | — | — | — | — | — | reconcile (use lumera's) |
| feemarket | — | — | — | — | — | — | — | — | reconcile/skip |
| wasm | — | — | — | — | — | — | — | — | reconcile/skip |
| ibc | — | — | — | — | — | — | — | — | reconcile |
