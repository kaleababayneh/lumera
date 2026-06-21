# Step 4 — SuperNode Proof-of-Service: verifiable inference receipts that gate settlement

> **Status:** design (2026-06-21). The thesis #1 primitive — *Verifiable Execution* — wired to
> *Economic Coordination*. This is a **build**, not a port: it grounds lumera_ai's receipt concept in
> Lumera's **real** SuperNode architecture (`x/supernode`, `x/action`) and the already-ported
> `x/registry` receipt types, and makes `x/credits` settlement contingent on a verifiable,
> on-chain, SuperNode-attested proof-of-service.
>
> Companion to `docs/LUMERA_AI_INTEGRATION_PLAN.md` (§ Step 4) and `CLAUDE.md`.

---

## 1. Goal & why this is the catalyst

Today the settlement loop pays a publisher when a router asserts `--receipt-id rXYZ`. That `receipt_id`
is a **free-form string**: `x/credits/keeper/msg_server.go` only checks it is canonical and
not-already-settled (`msg_server.go:199-237`). Nothing on-chain proves the work was actually done.

The thesis (`thesis.md`, primitive #1 *Verifiable Execution* / Sense + Proof-of-Service) says the moat
is **trust you can verify**: an agent should pay only against a receipt that proves *"a legitimate
SuperNode ran THIS model on THIS input and produced THIS output."* That receipt is the
content-addressed digest **`BLAKE3(input, model, output)`**, attested by a currently-active SuperNode.

Closing this turns the loop from *"someone asserts a call happened"* into *"the chain verified a call
happened before releasing payment."* That is the unlock that makes the LUME settlement economy
trustworthy — and it is the on-chain anchor the off-chain MCP router + the SGX enclave attestation
(`sgx-anoncreds-issuer`) plug into later.

---

## 2. What exists today (grounded survey)

### 2.1 `x/action` (Lumera native) — SuperNodes already finalize signed work
- Actions are **Sense** (fingerprint/detection) or **Cascade** (storage) jobs:
  `x/action/v1/types/action_type.pb.go:26-34`.
- Lifecycle Pending → Processing → Done → Approved (`action_state.pb.go:26-43`).
- `MsgFinalizeAction` is **signed by a SuperNode** and carries the result metadata; the keeper checks
  the SuperNode is in the **top-10 active set** for the action's block before accepting
  (`keeper/action.go:165-196`, `msg_server_finalize_action.go`).
- **But:** there is **no `inference` action type**, and `x/action` has **no link to `x/credits`**. Its
  finalize path is built around Kademlia IDs / Merkle chunk proofs / fingerprint signatures, not a
  lightweight `BLAKE3(input,model,output)` inference digest, and not settlement.

### 2.2 `x/supernode` (Lumera native) — the attestor identity
- A `SuperNode` has `validator_address` (`lumeravaloper…`) and `supernode_account` (`lumera…`, the
  signing account): `x/supernode/v1/types/super_node.pb.go:27-37`.
- State enum Active/Disabled/Stopped/Penalized/Postponed/StorageFull
  (`supernode_state.pb.go:29-48`).
- Keeper methods other modules consume (`x/action/v1/types/expected_keepers.go:44-50`):
  `IsSuperNodeActive(ctx, valAddr)`, `QuerySuperNode(ctx, valAddr)`,
  `GetSuperNodeByAccount(ctx, account) (SuperNode, bool, error)`, `SetSuperNode`.
- **Attestor check** = `GetSuperNodeByAccount(submitter)` → latest state `Active`. (Tx-signature
  authenticity comes free from the antehandler; an embedded enclave/publisher signature is a later
  slice via the receipt's `AttestationProof`/`EnclaveQuote` fields.)

### 2.3 `x/registry` (ported) — the receipt types are already here, just no-op'd
- `UsageReceipt` (30 fields) is fully gogo-ported: `ReceiptId, ToolId, RequestId, RequestHash,
  …, TraceHash, AttestationProof, EnclaveQuote, SessionId, QuoteId, LockId, IntentHash, Timestamp,
  ExpiresAt, Status, … TrustClass` (`types/types.pb.go:1961-1994`).
- `MsgSubmitReceipt{Router string, Receipt *UsageReceipt}` (signer = `router`),
  `QueryGetReceipt{ReceiptId}→{Receipt,Status}`, store key `ReceiptPrefix=0x03`.
- **Currently `SubmitReceipt`/`GetReceipt` are no-op'd** via `UnimplementedMsgServer/QueryServer`
  (the focused-slice pattern). Only `RegisterTool`/`CreateBond`/`WithdrawBond` are implemented.

### 2.4 `x/credits` — the settlement gate point
- `MsgSettleCredits{lock_id, receipt_id, tool_id, actual_cost, publisher, …}`; `receipt_id` only
  format/replay-checked (`msg_server.go:199-237`), then `SettleLock`→`ProcessSettlement` pays out
  (70% publisher / 20% router / 10% referrer, minus burn + insurance + CAC royalty).
- The lock carries `tool_id, session_id, quote_id, policy_version, intent_hash`
  (`types/credits.pb.go:476-490`) — these are what a receipt cross-checks against.
- Credits already holds a `RegistryKeeper` interface — **today just
  `GetToolPublisher(ctx, toolID)`** (`types/expected_keepers.go:34-37`). This is exactly where the
  receipt-verification method is added.

---

## 3. Design decision — receipts live in `x/registry`, attested by `x/supernode`, gating `x/credits`

**Chosen: Option A (registry receipts).** Reject Option B (new `x/action` inference type).

| | Option A — registry `SubmitReceipt` (CHOSEN) | Option B — extend `x/action` |
|---|---|---|
| Reuses already-ported types | ✅ `UsageReceipt`/`MsgSubmitReceipt`/`ReceiptPrefix` | ❌ new proto + regen |
| Connects to settlement | ✅ credits already imports `RegistryKeeper` | ❌ action has no credits link |
| Fits inference (vs storage) | ✅ lightweight content digest | ❌ action is Sense/Cascade-shaped |
| SuperNode attestation | ✅ via `x/supernode` keeper | ✅ (but heavier finalize flow) |
| Change to working money path | ✅ one gate method | ❌ large |

So: a SuperNode submits a `UsageReceipt` to `x/registry`; `x/credits.SettleCredits` refuses to pay
unless `receipt_id` resolves to a stored receipt whose `tool_id` matches the lock. `x/registry`
verifies the submitter is an active SuperNode via `x/supernode`. `x/action` is **left untouched** in
this slice (it remains the home of the heavier Sense/Cascade storage proofs; a future
`inference`-flavoured finalize can feed registry receipts if we want validator-set consensus on the
digest).

### 3.1 The receipt content & the `BLAKE3(input, model, output)` digest
The attesting SuperNode computes, off-chain:
```
request_hash = BLAKE3_256(canonical_input_bytes)          # input commitment  → UsageReceipt.RequestHash
output_hash  = BLAKE3_256(canonical_output_bytes)
trace_hash   = BLAKE3_256(request_hash ‖ model_id ‖ output_hash)   # the proof  → UsageReceipt.TraceHash
receipt_id   = "pos1" + hex(trace_hash)                    # content-addressed, deterministic, idempotent
```
`receipt_id` **is** the proof digest, so it is verifiable and replay-safe by construction (unlike
lumera_ai's UUID). BLAKE3 is already a Lumera dependency (`x/supernode` uses it for XOR distance), so
no new module dep. The same `receipt_id` is passed to `MsgSettleCredits`, binding payment to proof.

### 3.2 Registry `SubmitReceipt` rules (slice 1)
1. `msg.Receipt.ToolId` must be a registered tool (`GetToolCard`).
2. `msg.Router` (the signer/attestor) must be a **currently-active SuperNode**
   (`supernodeKeeper.GetSuperNodeByAccount` → latest state `Active`).
3. `receipt_id` non-empty, canonical, and **not already stored** (idempotent; content-addressed so a
   re-submit of the identical work is a no-op success, a *different* body under the same id is
   rejected).
4. Default `Timestamp = blockTime`, `ExpiresAt = blockTime + DisputeWindowSeconds` (param already
   exists), `Status = "attested"`.
5. Store under `ReceiptPrefix=0x03` keyed by `receipt_id`; index by tool. Emit
   `receipt_submitted{receipt_id, tool_id, supernode, trace_hash}`.

Deferred to later slices (fields already exist, left zero): embedded `AttestationProof`/`EnclaveQuote`
(SGX) signature verification, `PublisherSig` co-signing, dispute/challenge window enforcement,
`SettlementRecord` mirroring, bundle anchoring, settled-receipt pruning.

### 3.3 Credits settlement gate (slice 1)
- Extend the credits `RegistryKeeper` interface:
  `ValidateReceipt(ctx, receiptID, toolID string) error` — `nil` iff a stored receipt with that id
  exists and its `tool_id == toolID`; typed error otherwise.
- In `SettleCredits`, **after** the replay check and **after** the lock is loaded (so `tool_id` is
  known), call `registryKeeper.ValidateReceipt(receiptID, lockToolID)`; on error, fail settlement
  before any burn/payout. Emit `receipt_verified{receipt_id, tool_id}` on success.
- **Enforcement policy (decision):** *always-on* in this slice. Settlement now **requires** a
  proof-of-service receipt — that is the point of Step 4. The canonical loop gains a `submit-receipt`
  step. (Rationale: deterministic, secure, no proto regen. A governance `ProofOfServiceRequired`
  param for graceful per-network rollout is **Phase 2**, folded into the next registry proto pass —
  `Params` has no spare bool today.)

---

## 4. The evolved settlement loop

```
agent: swap-lume-to-lac
publisher: register-tool  (escrows bond — Step 3)
router:    lock-credits   (quote → lock LAC, returns lock_id, tool_id)
SuperNode: submit-receipt  ← NEW: anchors BLAKE3(input,model,output) as receipt_id=pos1<hex>
router:    settle-credits --receipt-id pos1<hex>   ← now GATED: pays only if receipt verifies
                                                       publisher paid; receipt_verified emitted
```

---

## 5. Build plan (files)

- **`x/registry/keeper/receipt.go`** (new): `SetUsageReceipt/GetUsageReceipt/HasReceipt/GetAllReceipts`
  (collections.Map `*UsageReceipt` on `ReceiptPrefix`, `collPtrValue`), `SubmitReceipt` keeper logic,
  `ValidateReceipt(ctx, receiptID, toolID) error`, `blake3` digest helpers.
- **`x/registry/keeper/keeper.go`**: add `usageReceipts` collection + a `supernodeKeeper` field.
- **`x/registry/types/expected_keepers.go`**: add `SupernodeKeeper` interface
  (`GetSuperNodeByAccount`, optionally `IsSuperNodeActive`).
- **`x/registry/keeper/msg_server.go` / `query_server.go`**: implement `SubmitReceipt` / `GetReceipt`
  (remove from the Unimplemented no-op set).
- **`x/registry/module/depinject.go` + app wiring**: inject the supernode keeper into registry.
- **`x/registry/keeper/genesis.go`**: import/export `Receipts`.
- **`x/registry/client/cli/{tx,query}.go`**: `submit-receipt`, `get-receipt`.
- **`x/credits/types/expected_keepers.go`**: add `ValidateReceipt` to `RegistryKeeper`.
- **`x/credits/keeper/msg_server.go`**: the settlement gate + `receipt_verified` event.
- (No proto regen. No `x/action`/`x/supernode` changes — read-only consumption of the supernode keeper.)

### Wiring note
`x/registry` will now depend on `x/supernode`'s keeper. `x/supernode` initialises **before** registry
already (it is part of the base Lumera app); registry is appended later in `app_config.go`, so the
ordering is safe. The registry keeper takes the supernode keeper as a depinject input (read-only).

---

## 6. Test plan (e2e on localnet)

Extend the bond e2e. Register a SuperNode (`val` is already a validator → register it as a supernode),
then:

1. **Positive:** active SuperNode `submit-receipt pos1<hex>` for `pubtool` → `settle-credits
   --receipt-id pos1<hex>` → publisher paid; `get-receipt` shows status `attested`;
   `receipt_verified` emitted.
2. **Negative — no proof:** `settle-credits --receipt-id pos1deadbeef` (never submitted) → **fails**
   ("receipt not found"); publisher **not** paid.
3. **Negative — not a SuperNode:** `submit-receipt` signed by `pub` (not a supernode) → **fails**
   ("submitter is not an active supernode").
4. **Negative — tool mismatch:** receipt for `toolA`, settle a lock for `toolB` → **fails**.
5. **Regression:** bond escrow + the swap→register→lock path all still green.

Acceptance: all five hold, node boots, no consensus panic.

---

## 7. Open questions / Phase-2

- **Governance toggle** (`ProofOfServiceRequired`) for staged rollout → next registry proto pass.
- **Embedded attestation**: verify `AttestationProof`/`EnclaveQuote` signatures (SGX enclave quote
  from `sgx-anoncreds-issuer`) and/or publisher co-sign, rather than relying on the tx signer alone.
- **Validator-set consensus on the digest**: route inference through an `x/action`
  `inference` type so N SuperNodes attest the same `trace_hash` (threshold), for high-value calls.
- **Dispute window**: enforce `ExpiresAt`, `ChallengeReceipt`, and slash the publisher bond (Step 3)
  on a proven bad receipt — this is where Step 3 and Step 4 compose into the full trust graph.
- **Session binding**: also cross-check `receipt.SessionId == lock.SessionId` (stricter; deferred so
  the router needn't thread session into the receipt in slice 1).
```
