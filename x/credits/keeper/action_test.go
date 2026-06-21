package keeper

import (
	"testing"
	"time"

	"github.com/LumeraProtocol/lumera/x/credits/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestActionLifecycle(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	// 1. Lock
	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"action-session",
		lockAmount,
		"tool-action",
		"quote-action",
		"policy@1",
		"intent-action",
	)
	require.NoError(t, err)

	actionID := "action-123"

	// 2. Record Partial Fill (Fill 1)
	fill1 := FillInfo{
		FillID:           "fill-1",
		FillAmount:       sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
		FillPrice:        "1.0",
		CumulativeFilled: sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
		Timestamp:        time.Now().UTC(),
	}

	req1 := SettlementRequest{
		ReceiptID:     actionID, // Use ActionID as ReceiptID
		ActionID:      actionID,
		ToolID:        "tool-action",
		TotalAmount:   sdk.NewCoins(fill1.FillAmount), // Delta for accumulation
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	err = keeper.RecordPartialFill(ctx, req1)
	require.NoError(t, err)

	// Verify state
	rec, found := keeper.GetSettlement(ctx, actionID)
	require.True(t, found)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, rec.Status)
	settlementEvent := findEventByTypeAndAttr(
		t,
		ctx.EventManager().Events(),
		types.EventTypeSettlement,
		types.AttributeKeyStatus,
		types.SettlementStatus_SETTLEMENT_STATUS_PENDING.String(),
	)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING.String(), eventAttribute(t, settlementEvent, types.AttributeKeyStatus))
	require.Equal(t, uint64(1), rec.FillCount)

	totalCost := types.CoinsFromProto(rec.TotalCost)
	require.Equal(t, sdk.NewCoins(fill1.FillAmount), totalCost)

	// 3. Record Partial Fill (Fill 2)
	fill2 := FillInfo{
		FillID:           "fill-2",
		FillAmount:       sdk.NewInt64Coin(types.DefaultCreditDenom, 200_000),
		FillPrice:        "1.0",
		CumulativeFilled: sdk.NewInt64Coin(types.DefaultCreditDenom, 300_000),
		Timestamp:        time.Now().UTC(),
	}

	req2 := SettlementRequest{
		ReceiptID:   actionID,
		ActionID:    actionID,
		TotalAmount: sdk.NewCoins(fill2.FillAmount), // Delta
	}

	err = keeper.RecordPartialFill(ctx, req2)
	require.NoError(t, err)

	rec, found = keeper.GetSettlement(ctx, actionID)
	require.True(t, found)
	require.Equal(t, uint64(2), rec.FillCount)
	totalCost = types.CoinsFromProto(rec.TotalCost)
	require.Equal(t, sdk.NewCoins(fill1.FillAmount.Add(fill2.FillAmount)), totalCost) // 300k

	// 4. Finalize
	res, err := keeper.FinalizeAction(ctx, actionID, lockID)
	require.NoError(t, err)

	// Verify refund
	refund := lockAmount.Sub(fill1.FillAmount.Add(fill2.FillAmount))
	require.Equal(t, sdk.NewCoins(refund), res.RefundAmount)

	rec, found = keeper.GetSettlement(ctx, actionID)
	require.True(t, found)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, rec.Status)
	require.NotNil(t, rec.CompletedAt)
	settlementEvent = findEventByTypeAndAttr(
		t,
		ctx.EventManager().Events(),
		types.EventTypeSettlement,
		types.AttributeKeyStatus,
		types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED.String(),
	)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED.String(), eventAttribute(t, settlementEvent, types.AttributeKeyStatus))

	// Verify lock burned
	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, lock.Status)
}

func findEventByTypeAndAttr(t *testing.T, events sdk.Events, eventType, key, value string) sdk.Event {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != eventType {
			continue
		}
		for _, attr := range events[i].Attributes {
			if attr.Key == key && attr.Value == value {
				return events[i]
			}
		}
	}
	require.Failf(t, "missing event", "event type %s with %s=%s not found", eventType, key, value)
	return sdk.Event{}
}

func eventAttribute(t *testing.T, event sdk.Event, key string) string {
	t.Helper()
	for _, attr := range event.Attributes {
		if attr.Key == key {
			return attr.Value
		}
	}
	require.Failf(t, "missing event attribute", "key %s not found in event %s", key, event.Type)
	return ""
}
