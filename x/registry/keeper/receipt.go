package keeper

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// Proof-of-Service receipts (Step 4 — Verifiable Execution).
//
// A SuperNode that executed an inference anchors a UsageReceipt whose receipt_id
// is the content-addressed digest of the work — `pos1<hex(BLAKE3(input,model,
// output))>`. Credits settlement (x/credits SettleCredits) then refuses to pay
// unless its receipt_id resolves to a stored receipt whose tool matches the
// lock. This binds payment to a verifiable, on-chain proof attested by a
// currently-active SuperNode. See docs/STEP4_PROOF_OF_SERVICE.md.
//
// Slice 1 scope: store + verify (submitter is an active SuperNode; receipt_id
// binds to the trace digest; tool exists; idempotent). Embedded SGX/publisher
// signature verification (AttestationProof/EnclaveQuote), dispute windows,
// settlement records and bundle anchoring are later slices — their fields exist
// on UsageReceipt and are left as provided / zero.

// receiptIDPrefix tags content-addressed proof-of-service receipt ids.
const receiptIDPrefix = "pos1"

// ReceiptIDFromTrace derives the canonical receipt_id from a trace digest.
// The off-chain SuperNode computes trace = BLAKE3(request_hash‖model‖output_hash);
// the id is a stable, verifiable function of that digest.
func ReceiptIDFromTrace(trace []byte) string {
	return receiptIDPrefix + hex.EncodeToString(trace)
}

// SetUsageReceipt stores a usage receipt keyed by its receipt id.
func (k Keeper) SetUsageReceipt(ctx sdk.Context, r *types.UsageReceipt) error {
	if r == nil {
		return fmt.Errorf("SetUsageReceipt: receipt cannot be nil")
	}
	if r.ReceiptId == "" {
		return fmt.Errorf("SetUsageReceipt: receipt missing id")
	}
	return k.usageReceipts.Set(ctx, r.ReceiptId, r)
}

// GetUsageReceipt retrieves a usage receipt by id.
func (k Keeper) GetUsageReceipt(ctx sdk.Context, receiptID string) (*types.UsageReceipt, bool) {
	r, err := k.usageReceipts.Get(ctx, receiptID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(ctx).Error("failed to load usage receipt", "receipt_id", receiptID, "error", err)
		return nil, false
	}
	return r, true
}

// GetAllReceipts returns all stored usage receipts (genesis export).
func (k Keeper) GetAllReceipts(ctx sdk.Context) []*types.UsageReceipt {
	out := make([]*types.UsageReceipt, 0)
	if err := k.usageReceipts.Walk(ctx, nil, func(_ string, r *types.UsageReceipt) (bool, error) {
		if r != nil {
			out = append(out, r)
		}
		return false, nil
	}); err != nil {
		k.Logger(ctx).Error("failed to iterate usage receipts", "error", err)
	}
	return out
}

// SubmitReceipt anchors a Proof-of-Service receipt. `attestor` is the message
// signer; it must be the account of a currently-active SuperNode.
func (k Keeper) SubmitReceipt(ctx sdk.Context, attestor string, receipt *types.UsageReceipt) error {
	if receipt == nil {
		return fmt.Errorf("receipt is required")
	}
	if strings.TrimSpace(receipt.ToolId) == "" {
		return fmt.Errorf("receipt tool_id is required")
	}
	if _, found := k.GetToolCard(ctx, receipt.ToolId); !found {
		return types.ErrToolNotFound.Wrapf("tool %s not found", receipt.ToolId)
	}

	// The receipt_id must be the content-addressed digest of the trace, so the
	// id itself is verifiable and replay-safe.
	if len(receipt.TraceHash) == 0 {
		return fmt.Errorf("receipt trace_hash is required (the BLAKE3(input,model,output) digest)")
	}
	wantID := ReceiptIDFromTrace(receipt.TraceHash)
	if receipt.ReceiptId == "" {
		receipt.ReceiptId = wantID
	} else if receipt.ReceiptId != wantID {
		return fmt.Errorf("receipt_id %q does not match trace digest %q", receipt.ReceiptId, wantID)
	}

	// The submitter must be a legitimate, currently-active SuperNode.
	if err := k.requireActiveSupernode(ctx, attestor); err != nil {
		return err
	}

	// Idempotent: an identical re-submission of the same content-addressed work
	// is a no-op success; a different body under the same id is rejected.
	if existing, found := k.GetUsageReceipt(ctx, receipt.ReceiptId); found {
		if existing.ToolId == receipt.ToolId && hex.EncodeToString(existing.TraceHash) == hex.EncodeToString(receipt.TraceHash) {
			return nil
		}
		return types.ErrReceiptExists.Wrapf("receipt %s already submitted with different content", receipt.ReceiptId)
	}

	now := ctx.BlockTime()
	if receipt.Timestamp.IsZero() {
		receipt.Timestamp = now
	}
	if receipt.ExpiresAt.IsZero() {
		window := k.GetParams(ctx).DisputeWindowSeconds
		if window < 0 {
			window = 0
		}
		receipt.ExpiresAt = now.Add(time.Duration(window) * time.Second)
	}
	if strings.TrimSpace(receipt.Status) == "" {
		receipt.Status = "attested"
	}
	// Record the attesting SuperNode account on the receipt.
	receipt.RouterAddress = attestor

	if err := k.SetUsageReceipt(ctx, receipt); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"receipt_submitted",
		sdk.NewAttribute("receipt_id", receipt.ReceiptId),
		sdk.NewAttribute("tool_id", receipt.ToolId),
		sdk.NewAttribute("supernode", attestor),
		sdk.NewAttribute("trace_hash", hex.EncodeToString(receipt.TraceHash)),
	))
	return nil
}

// ValidateReceipt is the gate consumed by credits settlement: it returns nil iff
// a receipt with receiptID exists and was anchored for toolID.
func (k Keeper) ValidateReceipt(ctx sdk.Context, receiptID, toolID string) error {
	receipt, found := k.GetUsageReceipt(ctx, receiptID)
	if !found {
		return types.ErrReceiptNotFound.Wrapf("no proof-of-service receipt for id %s", receiptID)
	}
	if receipt.ToolId != toolID {
		return types.ErrUnauthorized.Wrapf("receipt %s is for tool %s, not %s", receiptID, receipt.ToolId, toolID)
	}
	// A receipt under dispute, or already invalidated by an upheld challenge,
	// must not settle — payment is blocked until the dispute clears.
	switch receipt.Status {
	case "disputed":
		return types.ErrDisputeActive.Wrapf("receipt %s is under dispute", receiptID)
	case "invalid":
		return types.ErrInvalidState.Wrapf("receipt %s was invalidated by an upheld challenge", receiptID)
	}
	return nil
}

// requireActiveSupernode checks that `account` is the account of a currently
// active SuperNode (the legitimacy of the attestor).
func (k Keeper) requireActiveSupernode(ctx sdk.Context, account string) error {
	if k.supernodeKeeper == nil {
		return fmt.Errorf("supernode keeper unavailable")
	}
	sn, found, err := k.supernodeKeeper.GetSuperNodeByAccount(ctx, account)
	if err != nil {
		return err
	}
	if !found {
		return types.ErrUnauthorized.Wrapf("submitter %s is not a registered supernode", account)
	}
	valAddr, err := sdk.ValAddressFromBech32(sn.ValidatorAddress)
	if err != nil {
		return err
	}
	if !k.supernodeKeeper.IsSuperNodeActive(ctx, valAddr) {
		return types.ErrUnauthorized.Wrapf("supernode %s is not active", account)
	}
	return nil
}
