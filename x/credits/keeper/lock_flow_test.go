package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/gogoproto/proto"

	"github.com/LumeraProtocol/lumera/x/credits/types"
	nfttypes "github.com/LumeraProtocol/lumera/x/nft/types"
	reservetypes "github.com/LumeraProtocol/lumera/x/reserve/types"
)

func TestLockAndSettleFlow(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()

	// Register test accounts
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-123",
		lockAmount,
		"tool-alpha",
		"quote-1",
		"policy@1",
		"intent-hash",
	)
	require.NoError(t, err)
	require.NotEmpty(t, lockID)

	locked, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, locked.Status)
	require.Equal(t, lockAmount, types.CoinFromProto(locked.Amount))

	require.True(t, bank.Balance(routerAddr).IsZero())
	require.Equal(t, sdk.NewCoins(lockAmount), bank.Balance(moduleAddr))

	receipt := SettlementRequest{
		ReceiptID:     "receipt-1",
		ToolID:        "tool-alpha",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(45 * time.Second))

	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	settled, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, settled.Status)

	// Economics derived from keeper implementation (3% burn, insurance disabled, 70/20/10 split).
	burnRateBPS := uint32(300)
	insuranceRateBPS := uint32(0)

	_, remainingAfterBurn, err := CalculateBurnAmount(actualCost.Amount, burnRateBPS)
	require.NoError(t, err)

	_, err = SafePercentage(actualCost.Amount, insuranceRateBPS)
	require.NoError(t, err)

	// Insurance is disabled by default, so net amount is just the post-burn remainder.
	netAmount := remainingAfterBurn

	publisherShare, routerShare, _, _, referrerShare, err := CalculateSplit(netAmount, 7000, 2000, 0, 0, 1000)
	require.NoError(t, err)

	refund := lockAmount.Amount.Sub(actualCost.Amount)
	expectedRouter := routerShare.Add(refund)

	actualRouter := bank.GetBalance(ctx, routerAddr, lockAmount.Denom)
	actualPublisher := bank.GetBalance(ctx, publisherAddr, lockAmount.Denom)
	actualReferrer := bank.GetBalance(ctx, referrerAddr, lockAmount.Denom)
	deviation := sdkmath.NewInt(20_000) // tolerate small rounding redistribution
	require.True(t, actualRouter.Amount.Sub(expectedRouter).Abs().LTE(deviation), "router distribution mismatch: expected %s got %s", expectedRouter, actualRouter.Amount)
	require.True(t, actualPublisher.Amount.Sub(publisherShare).Abs().LTE(deviation), "publisher distribution mismatch: expected %s got %s", publisherShare, actualPublisher.Amount)
	require.True(t, actualReferrer.Amount.Sub(referrerShare).Abs().LTE(deviation), "referrer distribution mismatch: expected %s got %s", referrerShare, actualReferrer.Amount)

	// Insurance contribution remains in the module account until pool integration is wired.
	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestLockAndSettleFlowTreasuryDistribution(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()
	treasuryAddr := newAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr
	accKeeper.accounts[treasuryAddr.String()] = treasuryAddr

	params := types.DefaultParams()
	params.TreasuryAddress = treasuryAddr.String()
	require.NoError(t, keeper.SetParams(ctx, params))

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-treasury",
		lockAmount,
		"tool-alpha",
		"quote-1",
		"policy@1",
		"intent-hash",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-treasury",
		ToolID:        "tool-alpha",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(45 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Economics derived from keeper implementation (3% burn, insurance disabled, 70/(20-treasury)/10 split).
	burnRateBPS := uint32(300)
	insuranceRateBPS := uint32(0)
	_, remainingAfterBurn, err := CalculateBurnAmount(actualCost.Amount, burnRateBPS)
	require.NoError(t, err)

	_, err = SafePercentage(actualCost.Amount, insuranceRateBPS)
	require.NoError(t, err)

	netAmount := remainingAfterBurn

	publisherShare, routerShare, _, treasuryShare, referrerShare, err := CalculateSplit(
		netAmount,
		7000,
		defaultRouterShareBPS-DefaultTreasuryContributionBPS,
		0,
		DefaultTreasuryContributionBPS,
		1000,
	)
	require.NoError(t, err)

	refund := lockAmount.Amount.Sub(actualCost.Amount)
	expectedRouter := routerShare.Add(refund)

	actualRouter := bank.GetBalance(ctx, routerAddr, lockAmount.Denom)
	actualPublisher := bank.GetBalance(ctx, publisherAddr, lockAmount.Denom)
	actualReferrer := bank.GetBalance(ctx, referrerAddr, lockAmount.Denom)
	actualTreasury := bank.GetBalance(ctx, treasuryAddr, lockAmount.Denom)
	deviation := sdkmath.NewInt(20_000)

	require.True(t, actualRouter.Amount.Sub(expectedRouter).Abs().LTE(deviation), "router distribution mismatch: expected %s got %s", expectedRouter, actualRouter.Amount)
	require.True(t, actualPublisher.Amount.Sub(publisherShare).Abs().LTE(deviation), "publisher distribution mismatch: expected %s got %s", publisherShare, actualPublisher.Amount)
	require.True(t, actualReferrer.Amount.Sub(referrerShare).Abs().LTE(deviation), "referrer distribution mismatch: expected %s got %s", referrerShare, actualReferrer.Amount)
	require.True(t, actualTreasury.Amount.Sub(treasuryShare).Abs().LTE(deviation), "treasury distribution mismatch: expected %s got %s", treasuryShare, actualTreasury.Amount)

	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestLockAndSettleFlowOriginSurfaceDistribution(t *testing.T) {
	nftKeeper := &mockNFTKeeper{toolpacks: make(map[string]*nfttypes.ToolpackNFT)}
	ctx, keeper, bank, moduleAddr, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		nftKeeper: nftKeeper,
	})

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()
	originSurfaceAddr := newAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr
	accKeeper.accounts[originSurfaceAddr.String()] = originSurfaceAddr

	toolpackID := "toolpack-injective"
	nftKeeper.toolpacks[toolpackID] = &nfttypes.ToolpackNFT{
		Id:         toolpackID,
		Version:    1,
		Curator:    originSurfaceAddr.String(),
		RoyaltyBps: 1000,
		Active:     true,
	}

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-origin",
		lockAmount,
		"tool-alpha",
		"quote-1",
		"policy@1",
		"intent-hash",
		toolpackID,
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-origin",
		ToolID:        "tool-alpha",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(45 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	burnRateBPS := uint32(300)
	insuranceRateBPS := uint32(0)
	_, remainingAfterBurn, err := CalculateBurnAmount(actualCost.Amount, burnRateBPS)
	require.NoError(t, err)

	_, err = SafePercentage(actualCost.Amount, insuranceRateBPS)
	require.NoError(t, err)

	netAmount := remainingAfterBurn

	publisherShare, routerShare, originShare, _, referrerShare, err := CalculateSplit(
		netAmount,
		7000,
		1000,
		1000,
		0,
		1000,
	)
	require.NoError(t, err)

	refund := lockAmount.Amount.Sub(actualCost.Amount)
	expectedRouter := routerShare.Add(refund)

	actualRouter := bank.GetBalance(ctx, routerAddr, lockAmount.Denom)
	actualPublisher := bank.GetBalance(ctx, publisherAddr, lockAmount.Denom)
	actualReferrer := bank.GetBalance(ctx, referrerAddr, lockAmount.Denom)
	actualOrigin := bank.GetBalance(ctx, originSurfaceAddr, lockAmount.Denom)
	deviation := sdkmath.NewInt(20_000)

	require.True(t, actualRouter.Amount.Sub(expectedRouter).Abs().LTE(deviation), "router distribution mismatch: expected %s got %s", expectedRouter, actualRouter.Amount)
	require.True(t, actualPublisher.Amount.Sub(publisherShare).Abs().LTE(deviation), "publisher distribution mismatch: expected %s got %s", publisherShare, actualPublisher.Amount)
	require.True(t, actualReferrer.Amount.Sub(referrerShare).Abs().LTE(deviation), "referrer distribution mismatch: expected %s got %s", referrerShare, actualReferrer.Amount)
	require.True(t, actualOrigin.Amount.Sub(originShare).Abs().LTE(deviation), "origin-surface distribution mismatch: expected %s got %s", originShare, actualOrigin.Amount)

	require.Len(t, nftKeeper.payouts, 1)
	require.Equal(t, toolpackID, nftKeeper.payouts[0].toolpackID)
	require.Equal(t, originShare.String(), nftKeeper.payouts[0].amount.Amount.String())

	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestSettleLockAppliesReserveDiscount(t *testing.T) {
	reserve := &mockReserveKeeper{
		allocation: reservetypes.ReserveAllocation{
			Applied:         true,
			CommitmentID:    "commit-42",
			DiscountedPrice: sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000),
		},
	}
	ctx, keeper, bank, moduleAddr, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		reserveKeeper: reserve,
	})

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 600_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-vault",
		lockAmount,
		"tool-vault",
		"quote-vault",
		"policy-alpha@1.0.0",
		"intent-hash-vault",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:      "receipt-vault",
		ToolID:         "tool-vault",
		PublisherAddr:  publisherAddr,
		RouterAddr:     routerAddr,
		ReferrerAddr:   referrerAddr,
		PublisherID:    publisherAddr.String(),
		RouterID:       routerAddr.String(),
		ReferrerID:     referrerAddr.String(),
		PolicySnapshot: "policy-alpha@1.0.0",
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Reserve keeper should have been invoked with derived policy id.
	require.Equal(t, "policy-alpha", reserve.lastPolicy)
	require.Equal(t, "tool-vault", reserve.lastTool)

	charge := reserve.allocation.DiscountedPrice
	discountAmount := actualCost.Amount.Sub(charge.Amount)
	require.True(t, discountAmount.IsPositive())

	burnRateBPS := uint32(300)
	insuranceRateBPS := uint32(0)

	_, remainingAfterBurn, err := CalculateBurnAmount(charge.Amount, burnRateBPS)
	require.NoError(t, err)

	insuranceAmount, err := SafePercentage(charge.Amount, insuranceRateBPS)
	require.NoError(t, err)

	netAmount := remainingAfterBurn

	publisherShare, routerShare, _, _, referrerShare, err := CalculateSplit(netAmount, 7000, 2000, 0, 0, 1000)
	require.NoError(t, err)

	refund := lockAmount.Amount.Sub(charge.Amount)
	expectedRouter := routerShare.Add(refund)
	actualRouter := bank.GetBalance(ctx, routerAddr, lockAmount.Denom)
	actualPublisher := bank.GetBalance(ctx, publisherAddr, lockAmount.Denom)
	actualReferrer := bank.GetBalance(ctx, referrerAddr, lockAmount.Denom)

	deviation := sdkmath.NewInt(20_000)
	require.True(t, actualRouter.Amount.Sub(expectedRouter).Abs().LTE(deviation), "router mismatch with reserve discount")
	require.True(t, actualPublisher.Amount.Sub(publisherShare).Abs().LTE(deviation), "publisher mismatch with reserve discount")
	require.True(t, actualReferrer.Amount.Sub(referrerShare).Abs().LTE(deviation), "referrer mismatch with reserve discount")

	require.True(t, insuranceAmount.IsZero())

	// Insurance is disabled by default, so the module account should be empty here.
	remainingModule := bank.Balance(moduleAddr)
	require.True(t, remainingModule.IsZero())
}

func TestLockAndUnlockFlow(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	otherAddr := newAccAddress()

	// Register test account
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[otherAddr.String()] = otherAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-456",
		lockAmount,
		"tool-beta",
		"quote-2",
		"policy@1",
		"intent-hash-2",
	)
	require.NoError(t, err)

	require.Equal(t, sdk.NewCoins(lockAmount), bank.Balance(moduleAddr))
	require.True(t, bank.Balance(routerAddr).IsZero())

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(60 * time.Second))
	rawSecret := strings.Join([]string{"credits", "unlock", "credential", "64781"}, "-")
	jsonKey := "client_" + string([]byte{'s', 'e', 'c', 'r', 'e', 't'})
	reason := fmt.Sprintf(
		`quote expired Authorization: Bearer %s api_key=%s {"%s":"%s"}`,
		rawSecret,
		rawSecret,
		jsonKey,
		rawSecret,
	)
	require.NoError(t, keeper.UnlockCredits(ctx, lockID, reason))

	unlocked, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, unlocked.Status)
	require.Contains(t, unlocked.LastError, "quote expired")
	require.Contains(t, unlocked.LastError, "Bearer [REDACTED]")
	require.Contains(t, unlocked.LastError, "api_key=[REDACTED]")
	require.Contains(t, unlocked.LastError, `"client_secret":"[REDACTED]"`)
	require.NotContains(t, unlocked.LastError, rawSecret)

	event := findEventByTypeAndAttr(t, ctx.EventManager().Events(), types.EventTypeUnlock, types.AttributeKeyLockID, lockID)
	eventReason := eventAttribute(t, event, "reason")
	require.Contains(t, eventReason, "Bearer [REDACTED]")
	require.Contains(t, eventReason, "api_key=[REDACTED]")
	require.Contains(t, eventReason, `"client_secret":"[REDACTED]"`)
	require.NotContains(t, eventReason, rawSecret)

	require.Equal(t, sdk.NewCoins(lockAmount), bank.Balance(routerAddr))
	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestMsgServerUnlockCreditsEnforcesRouterAndUsesReason(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	msgServer := NewMsgServerImpl(keeper)

	routerAddr := newAccAddress()
	otherAddr := newAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[otherAddr.String()] = otherAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-msg-unlock",
		lockAmount,
		"tool-unlock",
		"quote-unlock",
		"policy@1",
		"intent-hash-unlock",
	)
	require.NoError(t, err)

	_, err = msgServer.UnlockCredits(ctx, &types.MsgUnlockCredits{
		Router: otherAddr.String(),
		LockId: lockID,
		Reason: "not allowed",
	})
	require.Error(t, err)

	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, lock.Status)
	require.Equal(t, sdk.NewCoins(lockAmount), bank.Balance(moduleAddr))

	reason := "cancelled"
	resp, err := msgServer.UnlockCredits(ctx, &types.MsgUnlockCredits{
		Router: routerAddr.String(),
		LockId: lockID,
		Reason: reason,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, lockAmount, types.CoinFromProto(resp.AmountUnlocked))

	lock, found = keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, lock.Status)
	require.Equal(t, reason, lock.LastError)

	require.Equal(t, sdk.NewCoins(lockAmount), bank.Balance(routerAddr))
	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestMsgServerSettleCreditsVerifiesRouterToolAndPublisher(t *testing.T) {
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	otherPublisher := newAccAddress()
	referrerAddr := newAccAddress()

	registry := mockRegistryKeeper{
		publishers: map[string]sdk.AccAddress{
			"tool-alpha": publisherAddr,
		},
	}

	ctx, keeper, bank, moduleAddr, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registry,
	})

	msgServer := NewMsgServerImpl(keeper)

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[otherPublisher.String()] = otherPublisher
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-msg-settle",
		lockAmount,
		"tool-alpha",
		"quote-1",
		"policy@1",
		"intent-hash",
	)
	require.NoError(t, err)

	_, err = msgServer.SettleCredits(ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockID,
		ActualCost: types.CoinToProto(actualCost),
		ReceiptId:  "receipt-1",
		ToolId:     "tool-beta",
		Publisher:  publisherAddr.String(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool id mismatch")

	_, err = msgServer.SettleCredits(ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockID,
		ActualCost: types.CoinToProto(actualCost),
		ReceiptId:  "receipt-1",
		ToolId:     "tool-alpha",
		Publisher:  otherPublisher.String(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "publisher mismatch")

	resp, err := msgServer.SettleCredits(ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockID,
		ActualCost: types.CoinToProto(actualCost),
		ReceiptId:  "receipt-1",
		ToolId:     "tool-alpha",
		Publisher:  publisherAddr.String(),
		Referrer:   referrerAddr.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, lock.Status)

	require.True(t, bank.GetBalance(ctx, routerAddr, lockAmount.Denom).Amount.IsPositive())
	require.True(t, bank.GetBalance(ctx, publisherAddr, lockAmount.Denom).Amount.IsPositive())
	require.True(t, bank.GetBalance(ctx, referrerAddr, lockAmount.Denom).Amount.IsPositive())
	require.True(t, bank.Balance(moduleAddr).IsZero())

	// Duplicate receipt IDs must be rejected, even across different locks.
	anotherLockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 900_000)
	anotherCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 600_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(anotherLockAmount))

	otherLockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-msg-settle-2",
		anotherLockAmount,
		"tool-alpha",
		"quote-2",
		"policy@1",
		"intent-hash-2",
	)
	require.NoError(t, err)

	_, err = msgServer.SettleCredits(ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     otherLockID,
		ActualCost: types.CoinToProto(anotherCost),
		ReceiptId:  "receipt-1",
		ToolId:     "tool-alpha",
		Publisher:  publisherAddr.String(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipt already settled")
}

func TestMsgServerSettleCreditsTrimsFinalStageBeforeLockFinality(t *testing.T) {
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()

	registry := mockRegistryKeeper{
		publishers: map[string]sdk.AccAddress{
			"tool-alpha": publisherAddr,
		},
	}

	ctx, keeper, bank, moduleAddr, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registry,
	})

	msgServer := NewMsgServerImpl(keeper)

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-padded-final-stage",
		lockAmount,
		"tool-alpha",
		"quote-padded-final-stage",
		"policy@1",
		"intent-hash-padded-final-stage",
	)
	require.NoError(t, err)

	resp, err := msgServer.SettleCredits(ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockID,
		ActualCost: types.CoinToProto(actualCost),
		ReceiptId:  "receipt-padded-final-stage-msg",
		ToolId:     "tool-alpha",
		Publisher:  publisherAddr.String(),
		Stage:      " finalized ",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, lock.Status)

	settlement, found := keeper.GetSettlement(ctx, "receipt-padded-final-stage-msg")
	require.True(t, found)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, settlement.Status)
	require.Equal(t, "finalized", settlement.Stage)
	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestSettleLockRecordsInsuranceContribution(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper, insurance := setupCreditsKeeperWithMockInsurance(t)
	params := types.DefaultParams()
	params.InsuranceBps = 200
	require.NoError(t, keeper.SetParams(ctx, params))

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 900_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 540_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Unix(1_000, 0).UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-insurance",
		lockAmount,
		"tool-insurance",
		"quote-insurance",
		"policy@42",
		"intent-hash-insurance",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-insurance",
		ToolID:        "tool-insurance",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	insuranceAmount := actualCost.Amount.MulRaw(200).QuoRaw(10_000)
	expectedCoins := sdk.NewCoins(sdk.NewCoin(lockAmount.Denom, insuranceAmount))

	require.Len(t, insurance.contributions, 1)
	require.Contains(t, insurance.contributions[0].receiptID, "receipt-insurance")
	require.Equal(t, expectedCoins, insurance.contributions[0].amount)

	poolBalance, err := insurance.GetPoolBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedCoins, poolBalance)

	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestLockSettlementReplayProtection(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 750_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 450_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0).UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-replay",
		lockAmount,
		"tool-replay",
		"quote-replay",
		"policy@7",
		"intent-hash-replay",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-replay",
		ToolID:        "tool-replay",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(45 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	beforeRouter := bank.GetBalance(ctx, routerAddr, lockAmount.Denom)
	beforePublisher := bank.GetBalance(ctx, publisherAddr, lockAmount.Denom)
	beforeReferrer := bank.GetBalance(ctx, referrerAddr, lockAmount.Denom)
	beforeModule := bank.Balance(moduleAddr)

	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLockInactive)

	require.Equal(t, beforeRouter, bank.GetBalance(ctx, routerAddr, lockAmount.Denom))
	require.Equal(t, beforePublisher, bank.GetBalance(ctx, publisherAddr, lockAmount.Denom))
	require.Equal(t, beforeReferrer, bank.GetBalance(ctx, referrerAddr, lockAmount.Denom))
	require.Equal(t, beforeModule, bank.Balance(moduleAddr))

	err = keeper.UnlockCredits(ctx, lockID, "second-unlock")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLockInactive)

	// Lock status remains burned after replay attempts.
	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, lock.Status)
}

func TestActiveLockBalanceInvariant(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()

	// Register test account
	accKeeper.accounts[routerAddr.String()] = routerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 250_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-invariant",
		lockAmount,
		"tool-gamma",
		"quote-3",
		"policy@1",
		"intent-hash-3",
	)
	require.NoError(t, err)
	require.NotEmpty(t, lockID)

	inv := ActiveLockBalanceInvariant(*keeper)
	msg, broken := inv(ctx)
	require.False(t, broken, msg)

	// Force a balance mismatch by burning one unit from the module account.
	debit := sdk.NewInt64Coin(types.DefaultCreditDenom, 1)
	require.NoError(t, bank.BurnCoins(ctx, types.ModuleAccountName, sdk.NewCoins(debit)))

	msg, broken = inv(ctx)
	require.True(t, broken)
	require.Contains(t, msg, "module balance")

	// Restore balance to keep subsequent tests isolated.
	require.NoError(t, bank.MintCoins(ctx, types.ModuleAccountName, sdk.NewCoins(debit)))
	restoreMsg, restoreBroken := inv(ctx)
	require.False(t, restoreBroken, restoreMsg)
	require.Equal(t, sdk.NewCoins(lockAmount), bank.Balance(moduleAddr))
}

func setupCreditsKeeper(t *testing.T) (sdk.Context, *Keeper, *mockBankKeeper, sdk.AccAddress, *mockAccountKeeper) {
	ctx, keeper, bank, moduleAddr, accountKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{})
	return ctx, keeper, bank, moduleAddr, accountKeeper
}

func setupCreditsKeeperWithMockInsurance(t *testing.T) (sdk.Context, *Keeper, *mockBankKeeper, sdk.AccAddress, *mockAccountKeeper, *mockInsuranceKeeper) {
	ctx, keeper, bank, moduleAddr, accountKeeper, insurance := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		insuranceFactory: func(bank *mockBankKeeper) types.InsuranceKeeper {
			return newMockInsuranceKeeper(bank)
		},
	})

	mockInsurance, ok := insurance.(*mockInsuranceKeeper)
	require.True(t, ok)

	return ctx, keeper, bank, moduleAddr, accountKeeper, mockInsurance
}

type keeperSetupOptions struct {
	insuranceFactory func(bank *mockBankKeeper) types.InsuranceKeeper
	registryKeeper   types.RegistryKeeper
	reserveKeeper    types.ReserveKeeper
	nftKeeper        types.NFTKeeper
}

func setupCreditsKeeperWithOptions(t *testing.T, opts keeperSetupOptions) (sdk.Context, *Keeper, *mockBankKeeper, sdk.AccAddress, *mockAccountKeeper, types.InsuranceKeeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	bankKeeper := newMockBankKeeper(types.ModuleAccountName, moduleAddr)
	accountKeeper := &mockAccountKeeper{
		moduleAddr: moduleAddr,
		accounts:   make(map[string]sdk.AccAddress),
	}

	var insurance types.InsuranceKeeper
	if opts.insuranceFactory != nil {
		insurance = opts.insuranceFactory(bankKeeper)
	}

	reserveKeeper := opts.reserveKeeper
	if reserveKeeper == nil {
		reserveKeeper = noopReserveKeeper{}
	}

	keeper := NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		bankKeeper,
		accountKeeper,
		insurance,
		opts.registryKeeper,
		reserveKeeper,
		opts.nftKeeper,
		authtypes.NewModuleAddress("gov").String(),
	)

	header := tmproto.Header{Time: time.Now().UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)

	require.NoError(t, keeper.SetParams(ctx, types.DefaultParams()))

	return ctx, &keeper, bankKeeper, moduleAddr, accountKeeper, insurance
}

func newAccAddress() sdk.AccAddress {
	priv := secp256k1.GenPrivKey()
	return sdk.AccAddress(priv.PubKey().Address())
}

type mockAccountKeeper struct {
	moduleAddr sdk.AccAddress
	accounts   map[string]sdk.AccAddress
}

func (m mockAccountKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	if moduleName == types.ModuleAccountName {
		return m.moduleAddr
	}
	return nil
}

func (m mockAccountKeeper) IterateAccounts(_ context.Context, cb func(account sdk.AccountI) bool) {
	// For testing, we'll iterate over known accounts
	// In a real implementation, this would iterate over all accounts in the state
	for _, addr := range m.accounts {
		// Create a simple base account for testing
		acc := &authtypes.BaseAccount{
			Address: addr.String(),
		}
		if stop := cb(acc); stop {
			return
		}
	}
	// Also include the module account
	if m.moduleAddr != nil {
		moduleAcc := &authtypes.BaseAccount{
			Address: m.moduleAddr.String(),
		}
		cb(moduleAcc)
	}
}

type mockBankKeeper struct {
	moduleName string
	moduleAddr sdk.AccAddress
	balances   map[string]sdk.Coins
}

func newMockBankKeeper(moduleName string, moduleAddr sdk.AccAddress) *mockBankKeeper {
	return &mockBankKeeper{
		moduleName: moduleName,
		moduleAddr: moduleAddr,
		balances:   make(map[string]sdk.Coins),
	}
}

func (bk *mockBankKeeper) FundAccount(addr sdk.AccAddress, coins sdk.Coins) {
	bk.ensureAccount(addr)
	bk.balances[addr.String()] = bk.balances[addr.String()].Add(coins...)
}

func (bk *mockBankKeeper) Balance(addr sdk.AccAddress) sdk.Coins {
	if coins, ok := bk.balances[addr.String()]; ok {
		return coins
	}
	return sdk.NewCoins()
}

type royaltyPayout struct {
	toolpackID string
	amount     sdk.Coin
}

type mockNFTKeeper struct {
	toolpacks map[string]*nfttypes.ToolpackNFT
	payouts   []royaltyPayout
}

func (m *mockNFTKeeper) GetToolpack(_ context.Context, id string) (*nfttypes.ToolpackNFT, bool, error) {
	if m.toolpacks == nil {
		return nil, false, nil
	}
	pack, ok := m.toolpacks[id]
	if !ok || pack == nil {
		return nil, false, nil
	}
	clone := proto.Clone(pack).(*nfttypes.ToolpackNFT)
	return clone, true, nil
}

func (m *mockNFTKeeper) RecordRoyaltyPayout(_ context.Context, _ string, toolpackID string, amount sdk.Coin) error {
	m.payouts = append(m.payouts, royaltyPayout{toolpackID: toolpackID, amount: amount})
	return nil
}

type noopReserveKeeper struct{}

func (noopReserveKeeper) AllocateReserve(_ context.Context, _ string, _ string, _ string, amount sdk.Coin) (reservetypes.ReserveAllocation, error) {
	return reservetypes.ReserveAllocation{Applied: false, DiscountedPrice: amount}, nil
}

func (noopReserveKeeper) ReleaseExpired(_ context.Context) error { return nil }

func (noopReserveKeeper) CreateCommitment(_ context.Context, _ reservetypes.ReserveRequest) (*reservetypes.ReserveCommitment, error) {
	return nil, errors.New("not implemented")
}

func (noopReserveKeeper) HasActiveCommitment(_ context.Context, _ string, _ string) (bool, error) {
	return false, nil
}

type mockReserveKeeper struct {
	allocation  reservetypes.ReserveAllocation
	returnError error
	lastOwner   string
	lastPolicy  string
	lastTool    string
	active      map[string]bool
}

func (m *mockReserveKeeper) AllocateReserve(_ context.Context, owner, policyID, toolID string, amount sdk.Coin) (reservetypes.ReserveAllocation, error) {
	m.lastOwner = owner
	m.lastPolicy = policyID
	m.lastTool = toolID
	if m.returnError != nil {
		return reservetypes.ReserveAllocation{Applied: false, DiscountedPrice: amount}, m.returnError
	}
	alloc := m.allocation
	if alloc.DiscountedPrice.Denom == "" {
		alloc.DiscountedPrice = amount
	}
	if alloc.DiscountedPrice.Amount.IsZero() && alloc.Applied {
		alloc.DiscountedPrice = sdk.NewCoin(amount.Denom, sdkmath.ZeroInt())
	}
	if !alloc.Applied {
		alloc.DiscountedPrice = amount
	}
	return alloc, nil
}

func (m *mockReserveKeeper) ReleaseExpired(_ context.Context) error { return nil }

func (m *mockReserveKeeper) CreateCommitment(_ context.Context, _ reservetypes.ReserveRequest) (*reservetypes.ReserveCommitment, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReserveKeeper) HasActiveCommitment(_ context.Context, policyID, toolID string) (bool, error) {
	if m.active != nil {
		if val, ok := m.active[policyKey(policyID, toolID)]; ok {
			return val, nil
		}
	}
	return m.allocation.Applied, nil
}

func policyKey(policyID, toolID string) string {
	return policyID + "|" + toolID
}

func (bk *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	if module != bk.moduleName {
		return fmt.Errorf("unknown module %s", module)
	}
	if err := bk.subtract(sender, amt); err != nil {
		return err
	}
	bk.ensureAccount(bk.moduleAddr)
	bk.balances[bk.moduleAddr.String()] = bk.balances[bk.moduleAddr.String()].Add(amt...)
	return nil
}

func (bk *mockBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error {
	if module != bk.moduleName {
		return fmt.Errorf("unknown module %s", module)
	}
	if err := bk.subtract(bk.moduleAddr, amt); err != nil {
		return err
	}
	bk.ensureAccount(recipient)
	bk.balances[recipient.String()] = bk.balances[recipient.String()].Add(amt...)
	return nil
}

func (bk *mockBankKeeper) BurnCoins(_ context.Context, module string, amt sdk.Coins) error {
	if module != bk.moduleName {
		return fmt.Errorf("unknown module %s", module)
	}
	return bk.subtract(bk.moduleAddr, amt)
}

func (bk *mockBankKeeper) MintCoins(_ context.Context, module string, amt sdk.Coins) error {
	if module != bk.moduleName {
		return fmt.Errorf("unknown module %s", module)
	}
	bk.ensureAccount(bk.moduleAddr)
	bk.balances[bk.moduleAddr.String()] = bk.balances[bk.moduleAddr.String()].Add(amt...)
	return nil
}

func (bk *mockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	coins := bk.Balance(addr)
	amount := coins.AmountOf(denom)
	return sdk.NewCoin(denom, amount)
}

func (bk *mockBankKeeper) GetSupply(_ context.Context, denom string) sdk.Coin {
	// Calculate total supply by summing all balances for this denom
	totalSupply := sdkmath.ZeroInt()
	for _, coins := range bk.balances {
		totalSupply = totalSupply.Add(coins.AmountOf(denom))
	}
	return sdk.NewCoin(denom, totalSupply)
}

func (bk *mockBankKeeper) ensureAccount(addr sdk.AccAddress) {
	key := addr.String()
	if _, ok := bk.balances[key]; !ok {
		bk.balances[key] = sdk.NewCoins()
	}
}

func (bk *mockBankKeeper) subtract(addr sdk.AccAddress, amt sdk.Coins) error {
	bk.ensureAccount(addr)
	current := bk.balances[addr.String()]
	if !current.IsAllGTE(amt) {
		return fmt.Errorf("insufficient funds: have %s need %s", current, amt)
	}
	bk.balances[addr.String()] = current.Sub(amt...)
	return nil
}

type insuranceContribution struct {
	receiptID string
	amount    sdk.Coins
}

type mockInsuranceKeeper struct {
	bank          *mockBankKeeper
	contributions []insuranceContribution
	pool          sdk.Coins
}

func newMockInsuranceKeeper(bank *mockBankKeeper) *mockInsuranceKeeper {
	return &mockInsuranceKeeper{
		bank:          bank,
		contributions: make([]insuranceContribution, 0, 1),
		pool:          sdk.NewCoins(),
	}
}

func (m *mockInsuranceKeeper) ContributeToPool(_ context.Context, receiptID, _, _, _, _ string, amount sdk.Coins) error {
	if amount.IsZero() {
		return nil
	}
	if err := m.bank.subtract(m.bank.moduleAddr, amount); err != nil {
		return err
	}
	m.pool = m.pool.Add(amount...)
	m.contributions = append(m.contributions, insuranceContribution{
		receiptID: receiptID,
		amount:    amount,
	})
	return nil
}

func (m *mockInsuranceKeeper) GetPoolBalance(_ context.Context) (sdk.Coins, error) {
	return m.pool, nil
}

type mockRegistryKeeper struct {
	publishers map[string]sdk.AccAddress
	err        error
}

func (m mockRegistryKeeper) GetToolPublisher(_ context.Context, toolID string) (sdk.AccAddress, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.publishers != nil {
		if addr, ok := m.publishers[toolID]; ok {
			return addr, nil
		}
	}
	return nil, fmt.Errorf("tool %s not found", toolID)
}

func (m mockRegistryKeeper) ValidateReceipt(sdk.Context, string, string, string) error {
	return nil
}
