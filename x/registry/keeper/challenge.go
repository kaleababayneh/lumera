package keeper

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// disputeResolutionWindow is how long an adjudicator has to rule before a
// challenge can be auto-rejected on expiry (the expiry sweep is a later slice).
func disputeResolutionWindow(p types.Params) time.Duration {
	secs := p.ChallengeResolutionDeadlineSeconds
	if secs <= 0 {
		secs = 86400
	}
	return time.Duration(secs) * time.Second
}

// Receipt disputes (Step 3 ⊗ Step 4): a challenger disputes a SuperNode-attested
// receipt within its dispute window, escrowing a stake and locking an equal slice
// of the publisher's bond. An adjudicator (an active SuperNode in this slice;
// production = a disjoint quorum / governance) then UPHOLDS the challenge — the
// locked bond is slashed (restitution-routed) and the receipt is invalidated —
// or it is rejected. Rejection-on-expiry (EndBlocker) + the challenger bonus are
// the next slice; this one delivers the punishment path. See
// docs/STEP4_PROOF_OF_SERVICE.md §7.

// SetChallenge stores a challenge and indexes it by receipt id.
func (k Keeper) SetChallenge(ctx sdk.Context, c *types.Challenge) error {
	if c == nil || c.ChallengeId == "" {
		return fmt.Errorf("SetChallenge: challenge missing id")
	}
	if err := k.challenges.Set(ctx, c.ChallengeId, c); err != nil {
		return err
	}
	if c.ReceiptId != "" {
		return k.challengeByReceipt.Set(ctx, c.ReceiptId, c.ChallengeId)
	}
	return nil
}

// GetChallenge retrieves a challenge by id.
func (k Keeper) GetChallenge(ctx sdk.Context, challengeID string) (*types.Challenge, bool) {
	c, err := k.challenges.Get(ctx, challengeID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(ctx).Error("failed to load challenge", "challenge_id", challengeID, "error", err)
		return nil, false
	}
	return c, true
}

// GetChallengeByReceipt returns the (latest) challenge filed against a receipt.
func (k Keeper) GetChallengeByReceipt(ctx sdk.Context, receiptID string) (*types.Challenge, bool) {
	cid, err := k.challengeByReceipt.Get(ctx, receiptID)
	if err != nil || cid == "" {
		return nil, false
	}
	return k.GetChallenge(ctx, cid)
}

// GetAllChallenges returns all challenges (genesis export).
func (k Keeper) GetAllChallenges(ctx sdk.Context) []*types.Challenge {
	out := make([]*types.Challenge, 0)
	if err := k.challenges.Walk(ctx, nil, func(_ string, c *types.Challenge) (bool, error) {
		if c != nil {
			out = append(out, c)
		}
		return false, nil
	}); err != nil {
		k.Logger(ctx).Error("failed to iterate challenges", "error", err)
	}
	return out
}

// generateChallengeID derives a deterministic, canonical challenge id from the
// receipt id and block height (one challenge per receipt per height).
func generateChallengeID(receiptID string, height int64) string {
	sum := sha256.Sum256([]byte(receiptID + "|" + strconv.FormatInt(height, 10)))
	return "ch1" + hex.EncodeToString(sum[:12])
}

// OpenChallenge files a dispute against a receipt: escrows the challenger's
// stake, locks an equal slice of the publisher's bond, and marks the receipt
// disputed (so it can no longer settle).
func (k Keeper) OpenChallenge(ctx sdk.Context, challenger sdk.AccAddress, receiptID, reason string, evidenceHash []byte, stake sdk.Coins) (*types.Challenge, error) {
	receipt, found := k.GetUsageReceipt(ctx, receiptID)
	if !found {
		return nil, types.ErrReceiptNotFound.Wrapf("receipt %s not found", receiptID)
	}
	if receipt.Status != "attested" {
		return nil, types.ErrInvalidState.Wrapf("receipt %s is %q, not open to challenge", receiptID, receipt.Status)
	}
	if ctx.BlockTime().After(receipt.ExpiresAt) {
		return nil, types.ErrDisputeExpired.Wrapf("dispute window for receipt %s closed at %s", receiptID, receipt.ExpiresAt)
	}
	if existing, ok := k.GetChallengeByReceipt(ctx, receiptID); ok && existing.Status == types.ChallengeStatusPending {
		return nil, types.ErrChallengeActive.Wrapf("receipt %s already has an open challenge", receiptID)
	}

	cleanStake, err := sanitizeBondCoins(stake)
	if err != nil {
		return nil, types.ErrInsufficientStake.Wrap(err.Error())
	}

	// Escrow the challenger's stake and lock an equal slice of the publisher's
	// bond — both parties now have skin in the outcome.
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, challenger, types.ModuleName, cleanStake); err != nil {
		return nil, err
	}
	if err := k.LockBond(ctx, receipt.ToolId, cleanStake); err != nil {
		// Refund the escrowed stake if the bond cannot back the dispute.
		if rerr := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, challenger, cleanStake); rerr != nil {
			return nil, rerr
		}
		return nil, err
	}

	receipt.Status = "disputed"
	if err := k.SetUsageReceipt(ctx, receipt); err != nil {
		return nil, err
	}

	now := ctx.BlockTime()
	deadline := now.Add(disputeResolutionWindow(k.GetParams(ctx)))
	c := &types.Challenge{
		ChallengeId:       generateChallengeID(receiptID, ctx.BlockHeight()),
		ReceiptId:         receiptID,
		ChallengerAddress: challenger.String(),
		ChallengerStake:   cleanStake,
		Reason:            reason,
		EvidenceHash:      evidenceHash,
		Status:            types.ChallengeStatusPending,
		ChallengedAt:      now,
		DeadlineAt:        deadline,
	}
	if err := k.SetChallenge(ctx, c); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeReceiptChallenged,
		sdk.NewAttribute("challenge_id", c.ChallengeId),
		sdk.NewAttribute("receipt_id", receiptID),
		sdk.NewAttribute("tool_id", receipt.ToolId),
		sdk.NewAttribute("challenger", challenger.String()),
		sdk.NewAttribute("stake", cleanStake.String()),
	))
	return c, nil
}

// UpholdChallenge resolves a pending challenge in the challenger's favour: it
// unlocks then slashes the at-risk bond (restitution-routed), refunds the
// challenger's stake, invalidates the receipt, and closes the challenge.
func (k Keeper) UpholdChallenge(ctx sdk.Context, receiptID string) (*types.Challenge, sdk.Coins, error) {
	receipt, found := k.GetUsageReceipt(ctx, receiptID)
	if !found {
		return nil, nil, types.ErrReceiptNotFound.Wrapf("receipt %s not found", receiptID)
	}
	c, ok := k.GetChallengeByReceipt(ctx, receiptID)
	if !ok {
		return nil, nil, types.ErrChallengeNotFound.Wrapf("no challenge for receipt %s", receiptID)
	}
	if c.Status != types.ChallengeStatusPending {
		return nil, nil, types.ErrInvalidState.Wrapf("challenge %s is %q, not pending", c.ChallengeId, c.Status)
	}

	stake := c.ChallengerStake
	// Free the locked bond so it becomes slashable, then slash it.
	if err := k.UnlockBond(ctx, receipt.ToolId, stake); err != nil {
		return nil, nil, err
	}
	slashed, err := k.SlashBond(ctx, receipt.ToolId, stake, "dispute_upheld:"+c.ChallengeId, hex.EncodeToString(c.EvidenceHash))
	if err != nil {
		return nil, nil, err
	}

	// The challenger was right — return their stake.
	challengerAddr, err := sdk.AccAddressFromBech32(c.ChallengerAddress)
	if err != nil {
		return nil, nil, err
	}
	if !stake.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, challengerAddr, stake); err != nil {
			return nil, nil, err
		}
	}

	receipt.Status = "invalid"
	if err := k.SetUsageReceipt(ctx, receipt); err != nil {
		return nil, nil, err
	}

	now := ctx.BlockTime()
	c.Status = types.ChallengeStatusUpheld
	c.Outcome = "valid"
	c.Resolution = "upheld: receipt invalidated, bond slashed"
	c.ResolvedAt = &now
	if err := k.SetChallenge(ctx, c); err != nil {
		return nil, nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeChallengeResolved,
		sdk.NewAttribute("challenge_id", c.ChallengeId),
		sdk.NewAttribute("receipt_id", receiptID),
		sdk.NewAttribute("outcome", "upheld"),
		sdk.NewAttribute("slashed", slashed.String()),
	))
	return c, slashed, nil
}
