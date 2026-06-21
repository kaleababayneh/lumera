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
- **`oracle` — PORTED + WIRED (2026-06-20).** Standalone full module (keeper + msg server + JSON
  genesis + query gateway), gogo-converted, `module/depinject.go` + `proto/lumera/oracle/module/
  module.proto`, registered in `app/app_config.go` (no module account — no coin handling) and held in
  `app/app.go` (`OracleKeeper`). Node boots + produces blocks; `lumerad query oracle params` returns
  defaults (asset pairs LAC/USD, ETH/USD, BTC/USD). **REVIEW NOTE:** the lumera_ai oracle proto
  carried `(gogoproto.customtype)=LegacyDec/Int` annotations that protobuf-go silently ignored;
  rather than rewrite the entire string-based keeper to `LegacyDec`, the conversion **stripped those
  annotations back to `string`** (wire-compatible; the keeper parses via `LegacyNewDecFromStr`). If
  the intended Go type was `LegacyDec`, restore the annotations and convert the keeper later.
  **oracle also uses ABCI vote extensions** (`vote_extension.go`) — the module boots, but the vote
  extensions are NOT yet wired into the app's ABCI++ handlers (`ExtendVote`/`VerifyVoteExtension`/
  `PrepareProposal`), so price feeds via vote extensions won't populate until that app wiring is added.
- **`policies` — PORTED + WIRED (2026-06-20).** Standalone full module (oracle twin:
  `NewKeeper(cdc, store, authority)`, timestamps-only, `collPtrValue` collections, no query gateway —
  queries over gRPC), gogo-converted, `module/depinject.go` + appconfig proto, registered in
  `app/app_config.go` (no module account) + held in `app/app.go`. Node boots + produces blocks;
  `lumerad query policies params` returns defaults.

### Modules integrated so far (node builds + boots + produces blocks + serves queries)
**4 full modules** — `credits`, `insurance`, `oracle`, `policies` — plus **5 type modules**
(`reserve, passport, nft, cac, registry`) credits depends on. **9 lumera_ai modules total.** All
converted via the §4 recipe + §8 gotchas, each verified to build/boot/query. 1 of 4 credits stubs
removed (insurance).

### The remaining modules are the INTERDEPENDENT CORE — sequencing matters
Every clean *standalone* module (`NewKeeper` needing only bank/account/authority) is now done. Each
remaining module needs another module's **keeper** at construction (checked via their `NewKeeper`):
`incentives`→registry+router · `vaults`→reserve · `auction`→priority+reserve ·
`payment_rails`→credits+nft+oracle+reserve · `router`/`workflows`→credits+registry ·
`challenges`→credits · `registry` itself is ~45k lines / 5 deps. (`priority` is a library, not an
app module.) **Next, in order:** (1) port the **reserve, nft, registry keepers** (currently
types-only) to remove credits' 3 remaining stubs and satisfy the dependents' `NewKeeper` —
reserve/nft are self-contained (~5k each); registry is the big one. (2) `router` + `payment_rails`
(the agent loop). (3) the rest + the **test port** (validate the money paths before testnet).

- **`registry` — KEEPER SLICE PORTED + WIRED (2026-06-21).** Rather than wholesale-port the legacy
  ~17k-line keeper (raw store keys + deprecated param subspace + sparse deps on
  challenges/insurance/passport), we built a **focused, modern registry keeper** on Lumera's real
  wiring (KVStoreService + collections + `collPtrValue`, using the already-converted `registry/types`):
  ToolCard registration + lookup + **`GetToolPublisher`** — the exact method credits settlement needs
  to pay publishers. `MsgRegisterTool` + `GetTool` query + minimal CLI implemented; the remaining
  registry RPCs (bonds, disputes, SLA/SLO, receipts, anchors, watchers) are no-op'd via the generated
  `UnimplementedMsgServer/QueryServer` and ported as later slices. Verified e2e on a localnet:
  `tx registry register-tool` (code 0) → `query registry get-tool` returns the owner/publisher.
  **The real registry keeper replaces `stubRegistryKeeper` in credits — 2 of 4 stubs now removed
  (insurance + registry).** This unblocks `MsgSettleCredits` publisher payout (thesis "will someone pay").
- **`reserve` + `nft` — KEEPERS PORTED + WIRED (2026-06-21). ALL 4 CREDITS STUBS REMOVED;
  `x/credits/module/stubs.go` is DELETED.**
  - **reserve** uses modern construction already (KVStoreService) and stores commitments/params as
    JSON, so it was copied + de-tagged + minimally fixed (log/v2→log; the query_server + CLI off the
    protobuf-go API). Full module: `AllocateReserve` (the credits discount hook), `CreateCommitment`,
    `ReleaseExpired`, queries, CLI. Verified e2e: `tx reserve create-commitment` (code 0) →
    `query reserve commitments-by-policy` returns the commitment (bronze tier, discount_bps=250).
  - **nft** built as a focused slice (Toolpack NFT registry: `GetToolpack`, `RecordRoyaltyPayout`,
    `MintToolpack`, query + CLI); history/curator-index/cumulative-royalty-stats are later slices,
    no-op'd via `UnimplementedMsgServer/QueryServer`.
  - **The full settlement loop still works with all real keepers** (re-verified: swap→register→lock→
    settle pays the publisher 543,200 ulac; reserve `AllocateReserve` runs for real, returns
    Applied=false with no matching commitment, settlement proceeds).
- **NO STUBS REMAIN in credits.** All four dependency keepers (insurance, registry, reserve, nft) are
  real and wired. Remaining work toward testnet: port the deferred *test suites* (verify money paths),
  finish the registry sub-slices (disputes/SLA/SLO), and the inference Proof-of-Service track.
- **`registry` bond slice — BUILT + WIRED + VERIFIED (2026-06-21). Step 3 (publisher skin-in-the-game).**
  A focused first bond slice on the registry keeper (`x/registry/keeper/bond.go`, native `sdk.Coins`/
  `time.Time` — the gogo `BondRecord` already carries them, so NO proto-coin helpers): `register-tool`
  now escrows the publisher's bond into the registry module account, enforcing the params
  `MinBondAmount` floor (`BondDenom=ulume`, default 1,000,000). New surface: keeper `CreateBond`
  (escrow/top-up) + `WithdrawBond` (reclaim **only the excess** above the minimum while registered) +
  `Get/Set/RemoveBondRecord`/`GetAllBonds`; `MsgCreateBond`/`MsgWithdrawBond` handlers (the gogo
  msgs were already signer-annotated `owner`); `GetBond` query; CLI `register-tool --bond` /
  `create-bond` / `withdraw-bond` / `get-bond`; genesis import/export of `BondRecords`. **app wiring:**
  added `{Account: registrytypes.ModuleName}` to maccPerms (no mint/burn — pure bond custody) so the
  module account exists to hold escrow. The slash/lock/SLA/restitution/badge/metrics machinery from
  lumera_ai's full `bond.go` is intentionally NOT ported (later slices); `BondRecord`'s extra fields
  are zero-initialised for forward-compat. Verified e2e on a fresh localnet: register escrows 1,000,000
  ulume (pub balance + registry module account both move exactly), `get-bond`=1,000,000, top-up→
  1,500,000, withdraw 500k→1,000,000, full withdraw **rejected code 8** ("would violate minimum
  requirement"), and the swap→lock→settle loop still pays the publisher 543,200 ulac with bonds live.
- **Tests deferred:** `*_test.go` across the cluster still reference the old protobuf-go API / not-yet
  ported modules and won't compile; the non-test build is green. Port tests in a later pass.

### Scope decision — the 7 remaining "core/economic" modules: SKIP/DEFER (2026-06-21)
A 7-way parallel code-grounded assessment (router/payment_rails/incentives/auction/challenges/vaults/
workflows) against the thesis flywheel test (§XII "say no to peripheral") concluded: **build none of
them now; go straight to the trust + Proof-of-Service primitives.** Verdicts:
- **`vaults` → SKIP.** Thin ownership wrapper over `reserve` — its only Msg (`CreateVault`) just calls
  `reserveKeeper.CreateCommitment` and re-hydrates economics from that commitment. No new signal.
- **`router` → DEFER.** Whitepaper meta-tool, but its on-chain settlement *is* the verified credits
  loop, its discovery *is* the registry slice, and its Msg surface is authority-only telemetry. Needs
  registry `SubmitReceipt`/`GetToolMetrics` (not yet exposed). Becomes the natural **integration
  surface after** bonds + PoS land — it then *consumes* trust/intelligence signal instead of
  re-packaging settled functionality. (~6k keeper LOC.)
- **`payment_rails` → DEFER.** Modern cross-chain on-ramp (escrow external INJ/USDC/USDT → oracle-price
  → mint LAC; burn → withdraw; IBC settlement). Deps (credits, oracle) ported, so not blocked — but
  it's *liquidity / go-to-market*, not moat; IBC piece is Phase-2. Revisit once trust/PoS ships.
- **`incentives` → DEFER (flagged "core").** The genuine **trust-graph engine**: metrics →
  `RequestEvaluation` → tiered, expiring **badges** → bps multipliers (`GetRoutingMultiplier`/
  `GetInsuranceDiscount`/`GetLACBonus`). Modern; hard deps (registry/bank/account) ported; only
  *soft*-blocked on `router`. This is the **tool-reputation** complement to `passport`'s
  agent-reputation — strongest follow-up candidate, queue **right after Step 3 (bonds)** as part of
  building out the trust graph.
- **`auction` → DEFER.** Well-built bid market, but not a wired module and hard-blocked on unported
  `priority`. Adjacent, not core now.
- **`challenges` → DEFER.** *Sounds* trust-graph (benchmarks/ranks tools), but in code the scores never
  write back to registry/passport reputation and `min_badge_tier` is never enforced — so it isn't a
  trust signal today. Needs unported registry SLO-probe hooks + `lumeraid` nonce verify.
- **`workflows` → DEFER.** Modern bundle orchestration (composable-intelligence primitive); deps
  (credits/registry) ported, but its money path is redundant with the verified credits loop. Revisit
  after the inference primitive exists.

**Net path forward:** Step 3 = **registry bonds slice** (publisher stake → skin-in-the-game, the trust
primitive). Step 4 = **SuperNode Proof-of-Service inference receipts** (the catalyst). Then revisit
`incentives` (trust graph) and `router` (integration surface) on top of that signal.

### Step 4 — SuperNode Proof-of-Service: BUILT + WIRED + VERIFIED (2026-06-21)
The thesis #1 primitive (*Verifiable Execution*) wired to *Economic Coordination*. Design doc:
`docs/STEP4_PROOF_OF_SERVICE.md`. A SuperNode that ran an inference anchors a `UsageReceipt` whose
`receipt_id` is the content-addressed digest `pos1<hex(BLAKE3(BLAKE3(input)‖model‖BLAKE3(output)))>`;
`x/credits` `SettleCredits` now **refuses to pay** unless that `receipt_id` resolves to a stored
receipt whose tool matches the lock. Grounded in Lumera's real architecture (no `x/action`/`x/supernode`
changes — read-only consumption):
- **`x/registry`**: implemented `SubmitReceipt` (was no-op) — validates tool exists, the submitter is a
  **currently-active SuperNode** (`supernodeKeeper.GetSuperNodeByAccount` + `IsSuperNodeActive`), and
  `receipt_id` binds to `TraceHash`; idempotent; stores under `ReceiptPrefix=0x03`; emits
  `receipt_submitted`. Added `ValidateReceipt(ctx, receiptID, toolID)`, `GetReceipt` query, CLI
  `submit-receipt`/`get-receipt` (the digest is computed client-side with `lukechampine.com/blake3`),
  genesis import/export of `Receipts`. Wired the supernode keeper into the registry keeper via
  depinject (`sntypes.SupernodeKeeper`).
- **`x/credits`**: extended the `RegistryKeeper` interface with `ValidateReceipt`; added the
  proof-of-service gate in `SettleCredits` (after the replay check, before payout) + a
  `receipt_verified` event. **Enforcement is always-on** (deterministic, no proto regen); a governance
  `ProofOfServiceRequired` toggle for staged rollout is Phase-2 (next registry proto pass).
- **Verified e2e** on a fresh localnet: register `val` as an active SuperNode → settle with a
  never-submitted id **rejected** (publisher unpaid) → `submit-receipt` (active SN) →
  `get-receipt`=attested → settle with the verified id **pays the publisher 543,200 ulac**
  (`receipt_verified` emitted) → `submit-receipt` from a non-supernode **rejected** (code 19).
- Deferred (fields exist, later slices): embedded SGX `EnclaveQuote`/`AttestationProof` + publisher
  co-sign verification, settlement records / bundle anchors, `x/action` `inference`-type validator-set
  consensus on the digest.

### Step 3 ⊗ Step 4 — dispute → slash: BUILT + WIRED + VERIFIED (2026-06-21)
The full trust loop: **bond → proof → dispute → slash → restitution**. No proto regen — the gogo
`Challenge`/`MsgChallengeReceipt`/`MsgSettleReceipt`/`PendingSlash` types, `ChallengePrefix=0x10`, the
dispute-window params, and the `BondRecord.LockedAmount`/`TotalSlashed` fields were all already
generated. Added (all in `x/registry`):
- **bond.go**: `LockBond`/`UnlockBond`/`SlashBond` + `splitSlashCoins` — the restitution split imports
  the immutable bps from `x/insurance/types` (5% burn / 25% reserve + 60% user-credit → 85% insurance /
  10% `fee_collector` treasury). Routed via `BurnCoins` + `SendCoinsFromModuleToModule`.
- **challenge.go**: `Challenge` CRUD (+ receipt index), `OpenChallenge` (escrow challenger stake, lock
  an equal bond slice, receipt→`disputed`), `UpholdChallenge` (unlock→slash, refund stake,
  receipt→`invalid`, challenge→`upheld`), deterministic challenge ids.
- **msg_server.go**: `ChallengeReceipt` (anyone, within the receipt's dispute window) +
  `SettleReceipt` = adjudicate-uphold (gated to an **active SuperNode**; production = disjoint quorum /
  governance). `ValidateReceipt` now refuses `disputed`/`invalid` receipts, so a disputed receipt
  cannot settle.
- **app wiring**: registry maccPerms gains `Burner` (the 5% slash burn). CLI `challenge-receipt` /
  `resolve-dispute` / `get-challenge`; genesis import/export of `Challenges`.
- **Verified e2e** (fresh localnet, challenger/publisher/supernode keys): challenge → receipt
  `disputed` + bond `locked=500000` → settle **blocked** → supernode uphold → slash event
  `burned=25000 insurance=425000 treasury=50000`, `bonded 2,000,000→1,500,000`, `total_slashed=500000`,
  receipt `invalid`, challenge `upheld`, challenger stake refunded, insurance reserve **+425,000**,
  settlement still blocked. **Reject-on-expiry added + verified (2026-06-21):** disputes are now
  bilateral — a registry **EndBlocker** (`ProcessExpiredChallenges`, `RejectChallenge`) auto-rejects a
  challenge whose `ChallengeResolutionDeadlineSeconds` passes without an uphold: the locked bond is
  **released (not slashed)**, the challenger's stake is **forfeited to insurance**, and the receipt
  returns to `attested` (settleable). Verified e2e: challenge → expire → bond `bonded=2,000,000`
  unchanged + `locked=0`, challenger stake `500000`→insurance, challenge `rejected`, then settlement
  succeeds. **Deferred:** the challenger bonus on uphold, and a disjoint-quorum adjudicator.

### `incentives` (trust-graph reputation engine) — FULL MODULE PORT: BUILT + WIRED + VERIFIED (2026-06-21)
The reward side of the trust graph (complements dispute→slash, the punishment side). First **full new
module** ported (not a slice): proto-gen + ~1,400-LOC keeper + module wiring, all green + booting.
- **proto-gen**: copied lumera_ai `incentives.proto`/`tx.proto` → `proto/lumera/incentives/v1/`, added
  gogo `stdtime` annotations + `go_package`, generated gogo types with `buf generate
  --template proto/buf.gen.gogo.yaml --path ...` (the **`--path` flag scopes generation to incentives
  only — no other module's `.pb.go` is touched**). Same gogo template generated the appconfig
  `module.pb.go` from a copied `module.proto`. This proves the new-module proto-gen path end-to-end.
- **keeper conversion**: copied `types/` + `keeper/` (stripped `//go:build cosmos` tags, rewrote
  `lumera-ai`→`lumera`). The collections are pointer-typed (`Map[string, *types.Badge]`), so the codec
  swap was `codec.CollValueV2[T]()` → `collPtrValue[T](cdc)` (the same adapter registry uses).
  `cosmossdk.io/log/v2`→`log`; `timestamppb.New(t)`→`t`, `*timestamppb.Timestamp`→`time.Time`,
  `.AsTime()` dropped, `!= nil`→`.IsZero()`; `protobuf-go proto.Clone`→gogo. Hand-wrote `codec.go`
  (registry style), keeper `genesis.go`, `module.go` (+EndBlock `ProcessExpiredBadges`), `module/
  depinject.go`, and minimal CLI.
- **router dep**: passed `nil` to `NewKeeper` — the badge path never invokes it (metrics come from
  authority / Proof-of-Service, not router). Added registry `IsToolRegistered` (the one method the
  incentives `RegistryKeeper` interface needed beyond `GetToolPublisher`).
- **app wiring**: import + all 3 order lists + module config + `IncentivesKeeper` field + depinject
  `&app.IncentivesKeeper`. No maccPerms (holds no coins).
- **Verified e2e**: node boots with incentives wired → `query incentives params` ok → seed a
  high-quality metric snapshot in genesis → register tool → `query incentives score` = **9607 →
  eligible PLATINUM** → publisher `request-evaluation` → **badge awarded `BADGE_TIER_PLATINUM`**.
- **Phase 2 — the thesis self-feed: DONE + VERIFIED (2026-06-21).** The trust graph now feeds itself
  from real on-chain conduct, no genesis seeding, no router. registry tracks per-tool usage on its
  bond record (`bumpToolStats`: +1 successful on each PoS `SubmitReceipt`, +1 dispute on each upheld
  `UpholdChallenge`) and exposes it via `GetToolUsage`; incentives folds it into the metric snapshot's
  invocation / receipt-validity / dispute dimensions (`refreshUsageMetrics`, called from
  `RequestEvaluation`) before scoring, preserving the off-chain-reported dimensions. **Verified e2e:**
  3 receipts → `request-evaluation` earns **PLATINUM (9607)**; a call disputed + upheld → re-evaluate
  → grace period (tier held) → after grace → **GOLD (8837)**. Receipts raise reputation, upheld
  disputes erode it. `GetRoutingMultiplier`/`GetInsuranceDiscount`/`GetLACBonus` remain ready for
  router/insurance/credits to consume.

### HILT REACHED — the on-chain flywheel is complete (2026-06-21)
After incentives, the on-chain core is done. The 5 thesis primitives are on-chain: Durable Memory
(Cascade, native lumera), Verifiable Execution (PoS receipts), Trust/Identity (bonds + incentives +
passport), Economic Coordination (credits settlement), and the only remaining primitive — Composable
Intelligence (`workflows`) — is **deferred on purpose**: the grounded assessment rates it *adjacent*
(not core), it is **7,823 keeper LOC**, and its money path is *redundant* with the verified credits
settlement. Every other unported module is peripheral (`vaults` = thin reserve wrapper [SKIP];
`payment_rails` = cross-chain on-ramp, IBC Phase-2), blocked (`auction` → unported `priority`),
orthogonal (`challenges` = benchmarking that never writes back to reputation), or **the pivot itself
(`router`)**. Per the thesis ("say no to peripheral") we stop on-chain integration here and move to the
agent-facing layer where the *vision* completes:
1. **Web PoC** (`poc/web/`) — visualize the live flywheel against a local node. **DONE + verified.**
2. **`router` + MCP daemon** — the off-chain agent layer that lets real AI agents use the chain.
   **DONE + verified.**

### `router` + MCP daemon — BUILT + VERIFIED (2026-06-21) — the vision realized
`poc/mcp-router/` is an **MCP (Model Context Protocol) server** (JSON-RPC 2.0 over stdio) — the
router pivot in its real, off-chain form. Any MCP-compatible AI agent (Claude Desktop, an Agent SDK
app) connects and uses the on-chain economy through the standard protocol:
- `tools/list` → **discovers tools from the chain** via the registry `ListTools` query (implemented
  this pass, plus a `list-tools` CLI), annotating each with its on-chain publisher + reputation badge.
- `tools/call` → runs the full loop per call: ensure credits (auto-swap) → **lock** → execute the
  tool off-chain (`executeTool`, a placeholder transform — real tools plug in here) → **submit a
  SuperNode Proof-of-Service receipt** (`BLAKE3(input,model,output)`) → **settle** (gated on the
  receipt → pays the publisher) → return the result **plus its on-chain proof** (`receipt_id`,
  publisher paid, "settlement gated on the receipt").
- **Verified e2e**: fed `initialize`/`tools/list`/`tools/call` over stdio against a live node →
  discovered `pubtool` → call `"hello from an AI agent"` → output `HELLO FROM AN AI AGENT` proven by
  receipt `pos1cb914…`, publisher paid `800000ulac`. See `poc/mcp-router/README.md` (incl. a
  Claude-Desktop MCP config).

**The agentic economy is complete end-to-end: discover → meter → execute → prove → settle**, driven by
a real AI agent over MCP. On-chain trust graph + economic settlement + off-chain agent layer all green.

### Security hardening — adversarial audit + fixes (2026-06-21)
A 5-target adversarial audit (refute-to-confirm) of the money paths found 9 confirmed bugs (5 refuted).
Fixed the value-at-risk ones; verified by `poc/security_test.sh` (the legitimate `poc/demo.sh` still
passes):
- **Bond theft (HIGH) — fixed.** A single SuperNode could self-file *and* self-uphold a challenge to
  drain any publisher's bond at ~zero cost. Now `UpholdChallenge` requires the **adjudicator to be
  disjoint** from both the challenger and the publisher, and `OpenChallenge` forbids a publisher
  challenging its own tool. (Verified: self-adjudication rejected, bond intact.)
- **Cheap griefing (MED) — fixed.** 1-ulume challenges could freeze settlements. Now a **minimum
  challenger stake** floor (¼ of MinBondAmount) is enforced.
- **Cross-settlement (MED) — fixed.** A receipt could settle any tool-matching lock. `ValidateReceipt`
  now **binds the receipt to its lock** (`receipt.LockId == lockID`), so a receipt anchored for one
  lock cannot be replayed against another.
- **Reputation double-count (LOW) — fixed.** An upheld dispute now moves a call from success→dispute
  (no longer counted as both), so dispute-rate/success-rate math is correct.
- **Accepted / Phase-2 (documented):** the PoS receipt is a **single-active-SuperNode trust
  assumption, not cryptographic proof** — `trace_hash` is attestor-supplied (no work verification); the
  real fix is SGX `EnclaveQuote`/publisher co-sign + a disjoint quorum (Phase-2). The lock-binding fix
  narrows the minting surface (a fabricated receipt cannot be settled without a matching lock).
  On-demand `RequestEvaluation` re-scoring is intentional (gas-metered, self-only); stake-based slash
  sizing (bounded below by the stake floor, above by available bond) is by design.

### Deferred on-chain modules (revisit only if the flywheel demands them)
`workflows` (composable intelligence, 7.8k LOC, redundant money path) · `payment_rails` (on-ramp,
Phase-2 IBC) · `auction` (blocked on `priority`) · `challenges` (orthogonal benchmarking) · `vaults`
(SKIP). `incentives` Phase-2 self-feed (metrics from PoS receipts + disputes) is the highest-value
on-chain follow-up once the agent layer exists.
Grounded assessment done: modern KVStoreService keeper; hard deps (registry/bank/account) all ported;
the `router` dep is **soft** (stored, never invoked in the badge path → pass nil). Focused slice =
the badge engine (RecordMetrics → RequestEvaluation → tiered badges → GetRoutingMultiplier/
InsuranceDiscount/LACBonus) + EndBlocker ProcessExpiredBadges. Metrics can be fed by the PoS receipts /
dispute outcomes (so the trust graph self-feeds without router). Then continue until `router` = the
**maximum-pivot** point → build the web PoC (`poc/web/` is already scaffolded + parked).

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
| reserve | ☑ | ☑ | ☑ | ☑ | ☑ | ☑ (commitment ✓) | ☐ | n/a | ◐ |
| nft | ☑ | ☑ | ☑ | ☑ | ☑ | ☑ (mint/royalty ✓) | ☐ | n/a | ◐ |
| cac | ☑ | ☑ | — (types-only) | ☑ | ☑ | — | ☐ | — | ◐ |
| registry | ☑ | ◐ (tool+bond+receipt+dispute) | ☑ | ☑ | ☑ | ☑ (register/bond/receipt/slash ✓) | ☐ | n/a | ◐ |
| insurance | ☑ | ☑ | ☑ | ☑ | ☑ | ◐ | ☐ | n/a | ◐ |
| router | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | DEFER (integration surface, post-PoS) |
| payment_rails | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | DEFER (on-ramp/liquidity, Phase-2 IBC) |
| incentives | ☑ | ☑ | ☑ | ☑ | ☑ | ☑ (badge award ✓) | ☐ | n/a | ◐ BUILT+WIRED+VERIFIED |
| auction | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | DEFER (blocked on priority) |
| challenges | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | DEFER (no real trust signal today) |
| oracle | ☑ | ☑ | ☑ | ☑ | ☑ | ☑ (query ✓) | ☐ | n/a | ◐ |
| policies | ☑ | ☑ | ☑ | ☑ | ☑ | ☑ (query ✓) | ☐ | n/a | ◐ |
| priority | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ |
| vaults | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | SKIP (thin wrapper over reserve) |
| workflows | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | DEFER (composable-intel, post-PoS) |
| ibc_action | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | ☐ | — | ☐ (Phase 2) |
| lumeraid | — | — | — | — | — | — | — | — | reconcile (use lumera's) |
| feemarket | — | — | — | — | — | — | — | — | reconcile/skip |
| wasm | — | — | — | — | — | — | — | — | reconcile/skip |
| ibc | — | — | — | — | — | — | — | — | reconcile |
