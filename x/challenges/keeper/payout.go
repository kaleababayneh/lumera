package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

// protoToSDKCoin converts a protobuf Coin to an sdk.Coin, returning an
// error for malformed denom or negative amount rather than panicking.
// Called on challenge fields (PrizePool, EntryFee, PrizeAmount) whose
// original source can be user-supplied transaction payloads or genesis
// import data. MsgCreateChallenge performs stateless PrizePool validation,
// but keeper-level conversion still returns errors instead of panicking so
// stored legacy or imported malformed coins fail cleanly.
func protoToSDKCoin(c sdk.Coin) (sdk.Coin, error) {
	if c.Denom == "" {
		return sdk.Coin{}, fmt.Errorf("nil coin")
	}
	if err := sdk.ValidateDenom(c.Denom); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid coin denom: %w", err)
	}
	if c.Amount.IsNil() {
		return sdk.Coin{}, fmt.Errorf("nil coin amount")
	}
	if c.Amount.IsNegative() {
		return sdk.Coin{}, fmt.Errorf("negative coin amount: %s", c.Amount.String())
	}
	return c, nil
}

// sdkToProtoCoin is an identity helper retained at call sites after the
// gogoproto migration; challenge coin fields are stored as sdk.Coin.
func sdkToProtoCoin(c sdk.Coin) sdk.Coin { return c }

// isProtoCoinZero reports whether a stored coin is unset or zero. A nil amount
// (zero-value sdk.Coin) counts as zero so callers skip empty escrow/refunds.
func isProtoCoinZero(c sdk.Coin) bool {
	return c.Amount.IsNil() || c.IsZero()
}

// DistributePrizes distributes the prize pool to ranked winners according to
// the challenge's PrizeDistribution configuration.
//
// The method:
//  1. Verifies the challenge is COMPLETED and payouts haven't been processed.
//  2. Computes each winner's share from the prize distribution BPS.
//  3. Deducts the platform fee.
//  4. Sends funds from the module account to each winner's publisher address.
//  5. Marks rankings as claimed and the challenge as payouts_complete.
//
// Requires the keeper to be constructed with a BankKeeper.
func (k *Keeper) DistributePrizes(ctx context.Context, challengeID string) error {
	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_COMPLETED {
		return fmt.Errorf("%w: challenge must be COMPLETED for payout", ErrInvalidStatus)
	}
	if ch.PayoutsComplete {
		return fmt.Errorf("payouts already processed for challenge %s", challengeID)
	}

	if isProtoCoinZero(ch.PrizePool) {
		ch.PayoutsComplete = true
		return k.UpdateChallenge(ctx, ch)
	}

	if k.bankKeeper == nil {
		return fmt.Errorf("bank keeper not configured; payouts require escrow integration")
	}

	dist := ch.PrizeDistribution
	if dist == nil || len(dist.WinnerSharesBps) == 0 {
		return fmt.Errorf("no prize distribution configured for challenge %s", challengeID)
	}

	totalPrize, err := protoToSDKCoin(ch.PrizePool)
	if err != nil {
		return fmt.Errorf("invalid prize pool: %w", err)
	}

	rankings, err := k.GetRankings(ctx, challengeID)
	if err != nil {
		return fmt.Errorf("fetch rankings: %w", err)
	}

	rankMap := make(map[uint32]*types.Ranking, len(rankings))
	for _, r := range rankings {
		rankMap[r.Rank] = r
	}

	params := k.GetParams(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()

	platformFeeAmt := totalPrize.Amount.MulRaw(int64(params.PlatformFeeBps)).QuoRaw(10_000)
	distributableAmt := totalPrize.Amount.Sub(platformFeeAmt)

	var totalBps uint64
	for _, bps := range dist.WinnerSharesBps {
		totalBps += uint64(bps)
	}
	if totalBps == 0 {
		return fmt.Errorf("winner shares total 0 bps; nothing to distribute")
	}
	if totalBps > 10_000 {
		return fmt.Errorf("winner shares total %d bps exceeds 10000", totalBps)
	}
	if totalBps < 5_000 {
		sdkCtx.Logger().With("module", "x/challenges").Warn("DistributePrizes: winner shares significantly below 10000 bps, possible misconfiguration",
			"challenge_id", challengeID, "total_bps", totalBps)
	}

	// Pre-scan to identify eligible winners and build payout plan.
	// WinnerSharesBps are absolute basis points of the distributable amount,
	// so unconfigured or ineligible shares intentionally remain in escrow.
	type payoutEntry struct {
		rank     uint32
		ranking  *types.Ranking
		addr     sdk.AccAddress
		shareBps uint32
		prizeAmt math.Int
	}
	var payouts []payoutEntry
	var eligibleBps uint32
	for i, shareBps := range dist.WinnerSharesBps {
		rank := uint32(i + 1)
		r, ok := rankMap[rank]
		if !ok {
			continue
		}
		if r.PrizeClaimed {
			continue
		}
		p, pErr := k.GetParticipant(ctx, challengeID, r.ToolId)
		if pErr != nil || p == nil || p.PublisherId == "" {
			reason := "participant not found or has no publisher"
			if pErr != nil {
				reason = fmt.Sprintf("participant lookup failed: %v", pErr)
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypePayoutSkipped,
				sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
				sdk.NewAttribute(types.AttributeKeyToolID, r.ToolId),
				sdk.NewAttribute(types.AttributeKeyReason, reason),
			))
			continue
		}
		recipientAddr, addrErr := sdk.AccAddressFromBech32(p.PublisherId)
		if addrErr != nil {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypePayoutSkipped,
				sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
				sdk.NewAttribute(types.AttributeKeyToolID, r.ToolId),
				sdk.NewAttribute(types.AttributeKeyReason, fmt.Sprintf("invalid publisher address: %s", p.PublisherId)),
			))
			continue
		}
		prizeAmt := distributableAmt.MulRaw(int64(shareBps)).QuoRaw(10_000)
		if prizeAmt.IsZero() {
			continue
		}
		eligibleBps += shareBps
		payouts = append(payouts, payoutEntry{
			rank: rank, ranking: r, addr: recipientAddr,
			shareBps: shareBps, prizeAmt: prizeAmt,
		})
	}

	// Assign integer-division dust to the last eligible winner, but only for
	// the total share represented by eligible winners. Shares below 10000 bps
	// or skipped rankings intentionally leave their unallocated funds in the
	// module account.
	if len(payouts) > 0 {
		sumDistributed := math.ZeroInt()
		for _, pe := range payouts {
			sumDistributed = sumDistributed.Add(pe.prizeAmt)
		}
		targetAllocated := distributableAmt.MulRaw(int64(eligibleBps)).QuoRaw(10_000)
		dust := targetAllocated.Sub(sumDistributed)
		if dust.IsPositive() {
			payouts[len(payouts)-1].prizeAmt = payouts[len(payouts)-1].prizeAmt.Add(dust)
		}
	}

	for _, pe := range payouts {
		prizeCoin := sdk.NewCoin(totalPrize.Denom, pe.prizeAmt)
		pe.ranking.PrizeAmount = sdkToProtoCoin(prizeCoin)

		if err := k.bankKeeper.SendCoinsFromModuleToAccount(
			ctx, types.ModuleName, pe.addr, sdk.NewCoins(prizeCoin),
		); err != nil {
			return fmt.Errorf("payout to rank %d (%s): %w", pe.rank, pe.ranking.ToolId, err)
		}

		pe.ranking.PrizeClaimed = true
		pe.ranking.ClaimedAt = now
		if err := k.SetRanking(ctx, pe.ranking); err != nil {
			return fmt.Errorf("update ranking for %s: %w", pe.ranking.ToolId, err)
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypePrizePaid,
			sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
			sdk.NewAttribute(types.AttributeKeyToolID, pe.ranking.ToolId),
			sdk.NewAttribute(types.AttributeKeyRank, fmt.Sprintf("%d", pe.rank)),
			sdk.NewAttribute(types.AttributeKeyAmount, prizeCoin.String()),
		))
	}

	if platformFeeAmt.IsPositive() {
		feeCoin := sdk.NewCoin(totalPrize.Denom, platformFeeAmt)
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx, types.ModuleName, authtypes.FeeCollectorName, sdk.NewCoins(feeCoin),
		); err != nil {
			return fmt.Errorf("transfer platform fee to fee collector: %w", err)
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypePlatformFee,
			sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
			sdk.NewAttribute(types.AttributeKeyAmount, feeCoin.String()),
		))
	}

	ch.PayoutsComplete = true
	return k.UpdateChallenge(ctx, ch)
}

// EscrowPrizePool transfers the prize pool from the creator's account to the
// module account when a challenge is created. This ensures funds are locked
// before the challenge can be activated.
func (k *Keeper) EscrowPrizePool(ctx context.Context, challengeID string) error {
	if k.bankKeeper == nil {
		return fmt.Errorf("bank keeper not configured; escrow requires bank integration")
	}

	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	if isProtoCoinZero(ch.PrizePool) {
		return nil
	}

	creatorAddr, err := sdk.AccAddressFromBech32(ch.Creator)
	if err != nil {
		return fmt.Errorf("invalid creator address: %w", err)
	}

	sdkCoin, err := protoToSDKCoin(ch.PrizePool)
	if err != nil {
		return fmt.Errorf("invalid prize pool coin: %w", err)
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(
		ctx, creatorAddr, types.ModuleName, sdk.NewCoins(sdkCoin),
	); err != nil {
		return fmt.Errorf("escrow prize pool: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePrizeEscrowed,
		sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
		sdk.NewAttribute(types.AttributeKeyAmount, sdkCoin.String()),
		sdk.NewAttribute(types.AttributeKeyCreator, ch.Creator),
	))

	return nil
}

// RefundPrizePool returns the escrowed prize pool back to the creator when a
// challenge is cancelled. Only callable when the challenge status is CANCELLED.
func (k *Keeper) RefundPrizePool(ctx context.Context, challengeID string) error {
	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_CANCELLED {
		return fmt.Errorf("%w: refund only available for CANCELLED challenges", ErrInvalidStatus)
	}

	if k.bankKeeper == nil {
		return fmt.Errorf("bank keeper not configured")
	}

	if isProtoCoinZero(ch.PrizePool) {
		return nil
	}

	creatorAddr, err := sdk.AccAddressFromBech32(ch.Creator)
	if err != nil {
		return fmt.Errorf("invalid creator address: %w", err)
	}

	sdkCoin, err := protoToSDKCoin(ch.PrizePool)
	if err != nil {
		return fmt.Errorf("invalid prize pool coin: %w", err)
	}

	// Subtract any prizes already distributed (guards against partial payouts
	// if cancellation occurs after some rankings have been claimed).
	rankings, err := k.GetRankings(ctx, challengeID)
	if err != nil {
		return fmt.Errorf("failed to load rankings for refund: %w", err)
	}
	for _, r := range rankings {
		if r.PrizeClaimed && !isProtoCoinZero(r.PrizeAmount) {
			claimed, cErr := protoToSDKCoin(r.PrizeAmount)
			if cErr == nil && claimed.Denom == sdkCoin.Denom {
				sdkCoin.Amount = sdkCoin.Amount.Sub(claimed.Amount)
			}
		}
	}

	if !sdkCoin.Amount.IsPositive() {
		return nil
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx, types.ModuleName, creatorAddr, sdk.NewCoins(sdkCoin),
	); err != nil {
		return fmt.Errorf("refund prize pool: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePrizeRefunded,
		sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
		sdk.NewAttribute(types.AttributeKeyAmount, sdkCoin.String()),
		sdk.NewAttribute(types.AttributeKeyCreator, ch.Creator),
	))

	return nil
}

// RefundEntryFees returns the collected entry fees back to the participants
// when a challenge is cancelled. Only callable when the challenge status is CANCELLED.
func (k *Keeper) RefundEntryFees(ctx context.Context, challengeID string) error {
	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_CANCELLED {
		return fmt.Errorf("%w: refund only available for CANCELLED challenges", ErrInvalidStatus)
	}

	if k.bankKeeper == nil {
		return fmt.Errorf("bank keeper not configured")
	}

	if isProtoCoinZero(ch.EntryFee) {
		return nil
	}

	sdkFee, err := protoToSDKCoin(ch.EntryFee)
	if err != nil {
		return fmt.Errorf("invalid entry fee coin: %w", err)
	}

	participants, err := k.GetParticipants(ctx, challengeID)
	if err != nil {
		return fmt.Errorf("failed to fetch participants: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, p := range participants {
		if isProtoCoinZero(p.EntryFeePaid) {
			continue
		}

		paidCoin, err := protoToSDKCoin(p.EntryFeePaid)
		if err != nil || paidCoin.Denom != sdkFee.Denom {
			continue // skip malformed records, but process the rest
		}

		publisherAddr, err := sdk.AccAddressFromBech32(p.PublisherId)
		if err != nil {
			continue
		}

		if err := k.bankKeeper.SendCoinsFromModuleToAccount(
			ctx, types.ModuleName, publisherAddr, sdk.NewCoins(paidCoin),
		); err != nil {
			sdkCtx.Logger().With("module", "x/challenges").Error("failed to refund entry fee",
				"challenge_id", challengeID, "tool_id", p.ToolId, "publisher", p.PublisherId, "error", err)
			continue
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypePrizeRefunded, // Using PrizeRefunded or creating EntryFeeRefunded
			sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
			sdk.NewAttribute(types.AttributeKeyAmount, paidCoin.String()),
			sdk.NewAttribute("refundee", p.PublisherId),
			sdk.NewAttribute("refund_type", "entry_fee"),
		))
	}

	return nil
}
