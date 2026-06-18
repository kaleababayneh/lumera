//go:build cosmos && cosmos_full

package keeper

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// newMsgSrv creates a credits MsgServer backed by setupCreditsKeeper infrastructure.
type msgSrvFixture struct {
	ctx        sdk.Context
	keeper     *Keeper
	bank       *mockBankKeeper
	moduleAddr sdk.AccAddress
	accKeeper  *mockAccountKeeper
	srv        types.MsgServer
}

func newMsgSrvFixture(t *testing.T) *msgSrvFixture {
	t.Helper()
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)
	ctx = ctx.WithBlockTime(time.Now().UTC())
	return &msgSrvFixture{
		ctx:        ctx,
		keeper:     keeper,
		bank:       bank,
		moduleAddr: moduleAddr,
		accKeeper:  accKeeper,
		srv:        NewMsgServerImpl(keeper),
	}
}

func newMsgSrvFixtureWithRegistry(t *testing.T) (*msgSrvFixture, *mockRegistryKeeper) {
	t.Helper()
	registry := &mockRegistryKeeper{
		publishers: make(map[string]sdk.AccAddress),
	}
	ctx, keeper, bank, moduleAddr, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registry,
	})
	ctx = ctx.WithBlockTime(time.Now().UTC())
	return &msgSrvFixture{
		ctx:        ctx,
		keeper:     keeper,
		bank:       bank,
		moduleAddr: moduleAddr,
		accKeeper:  accKeeper,
		srv:        NewMsgServerImpl(keeper),
	}, registry
}

func protoCoin(denom string, amount string) *basev1beta1.Coin {
	return &basev1beta1.Coin{Denom: denom, Amount: amount}
}

func validKeeperSettleOverdraftMsg(router string) *types.MsgSettleOverdraft {
	return &types.MsgSettleOverdraft{
		Router:                  router,
		CreditLineId:            "credit-line-1",
		SettlementBatchId:       "batch-1",
		CreditLimit:             protoCoin(types.DefaultCreditDenom, "100000"),
		LiquidationThresholdBps: 8000,
		PolicyVersion:           "policy-v1",
		Entries: []*types.OverdraftSettlementEntry{
			{
				RequestId:         "request-1",
				QuoteId:           "quote-1",
				ProvisionalLockId: "overdraft-lock-1",
				ReceiptId:         "receipt-1",
				ToolId:            "tool-1",
				QuotedCost:        protoCoin(types.DefaultCreditDenom, "5000"),
				ActualCost:        protoCoin(types.DefaultCreditDenom, "4000"),
				RefundAmount:      protoCoin(types.DefaultCreditDenom, "1000"),
				InsuranceAmount:   protoCoin(types.DefaultCreditDenom, "0"),
				BurnAmount:        protoCoin(types.DefaultCreditDenom, "0"),
				Splits: []*types.OverdraftSettlementSplit{
					{Role: "publisher", Address: router, Amount: protoCoin(types.DefaultCreditDenom, "4000")},
				},
			},
		},
	}
}

// ============================================================
// LockCredits
// ============================================================

func TestMsgServer_LockCredits_NilMsg(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.LockCredits(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "empty request")
}

func TestMsgServer_LockCredits_InvalidRouter(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router: "not-bech32",
		Amount: protoCoin("ulac", "1000"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid router address")
}

func TestMsgServer_LockCredits_NilAmount(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	resp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-nil-amount",
		ToolId:    "tool-1",
		Amount:    nil,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "amount is required")
}

func TestMsgServer_LockCredits_ZeroAmount(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	resp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-zero-amount",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "0"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "positive")
}

func TestMsgServer_LockCredits_WrongDenom(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	resp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-wrong-denom",
		ToolId:    "tool-1",
		Amount:    protoCoin("uatom", "1000"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "expected denom")
}

func TestMsgServer_LockCredits_InsufficientFunds(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	// Don't fund the account

	resp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-1",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "1000"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "insufficient funds")
}

func TestMsgServer_LockCredits_HappyPath(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))

	resp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:        routerAddr.String(),
		SessionId:     "session-1",
		ToolId:        "tool-1",
		QuoteId:       "quote-1",
		PolicyVersion: "v1",
		IntentHash:    "hash-1",
		Amount:        protoCoin("ulac", "5000"),
		TtlSeconds:    600,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.LockId)
	assert.Greater(t, resp.ExpiresAt, int64(0))

	// Verify funds moved to module
	assert.Equal(t, sdk.NewInt64Coin("ulac", 5000), f.bank.GetBalance(f.ctx, routerAddr, "ulac"))
	assert.Equal(t, sdk.NewInt64Coin("ulac", 5000), f.bank.GetBalance(f.ctx, f.moduleAddr, "ulac"))
}

func TestMsgServer_LockCredits_ReusesActiveQuoteLock(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))

	msg := &types.MsgLockCredits{
		Router:        routerAddr.String(),
		SessionId:     "session-idempotent",
		ToolId:        "tool-1",
		QuoteId:       "quote-idempotent",
		PolicyVersion: "v1",
		IntentHash:    "hash-idempotent",
		Amount:        protoCoin("ulac", "3000"),
		TtlSeconds:    600,
	}

	first, err := f.srv.LockCredits(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := f.srv.LockCredits(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, second)

	require.Equal(t, first.LockId, second.LockId,
		"MsgServer lock path must preserve keeper quote-idempotency semantics")
	assert.Equal(t, sdk.NewInt64Coin("ulac", 7000), f.bank.GetBalance(f.ctx, routerAddr, "ulac"))
	assert.Equal(t, sdk.NewInt64Coin("ulac", 3000), f.bank.GetBalance(f.ctx, f.moduleAddr, "ulac"))
}

func TestMsgServer_LockCredits_RejectsMismatchedActiveQuoteReplay(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*types.MsgLockCredits, sdk.AccAddress)
		assertions func(*testing.T, *msgSrvFixture, sdk.AccAddress, sdk.AccAddress)
	}{
		{
			name: "different router",
			mutate: func(msg *types.MsgLockCredits, otherRouter sdk.AccAddress) {
				msg.Router = otherRouter.String()
			},
			assertions: func(t *testing.T, f *msgSrvFixture, routerAddr sdk.AccAddress, otherRouter sdk.AccAddress) {
				t.Helper()
				assert.Equal(t, sdk.NewInt64Coin("ulac", 7000), f.bank.GetBalance(f.ctx, routerAddr, "ulac"))
				assert.Equal(t, sdk.NewInt64Coin("ulac", 10_000), f.bank.GetBalance(f.ctx, otherRouter, "ulac"))
				assert.Equal(t, sdk.NewInt64Coin("ulac", 3000), f.bank.GetBalance(f.ctx, f.moduleAddr, "ulac"))
			},
		},
		{
			name: "different amount",
			mutate: func(msg *types.MsgLockCredits, _ sdk.AccAddress) {
				msg.Amount = protoCoin("ulac", "4000")
			},
			assertions: func(t *testing.T, f *msgSrvFixture, routerAddr sdk.AccAddress, _ sdk.AccAddress) {
				t.Helper()
				assert.Equal(t, sdk.NewInt64Coin("ulac", 7000), f.bank.GetBalance(f.ctx, routerAddr, "ulac"))
				assert.Equal(t, sdk.NewInt64Coin("ulac", 3000), f.bank.GetBalance(f.ctx, f.moduleAddr, "ulac"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMsgSrvFixture(t)
			routerAddr := newAccAddress()
			otherRouter := newAccAddress()
			f.accKeeper.accounts[routerAddr.String()] = routerAddr
			f.accKeeper.accounts[otherRouter.String()] = otherRouter
			f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))
			f.bank.FundAccount(otherRouter, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))

			msg := &types.MsgLockCredits{
				Router:        routerAddr.String(),
				SessionId:     "session-idempotent",
				ToolId:        "tool-1",
				QuoteId:       "quote-idempotent",
				PolicyVersion: "v1",
				IntentHash:    "hash-idempotent",
				Amount:        protoCoin("ulac", "3000"),
				TtlSeconds:    600,
			}

			first, err := f.srv.LockCredits(f.ctx, msg)
			require.NoError(t, err)
			require.NotNil(t, first)

			replay := *msg
			tt.mutate(&replay, otherRouter)

			second, err := f.srv.LockCredits(f.ctx, &replay)
			require.Error(t, err)
			require.Nil(t, second)
			require.ErrorContains(t, err, "already bound to a different active lock request")
			tt.assertions(t, f, routerAddr, otherRouter)
		})
	}
}

func TestMsgServer_LockCredits_RejectsPaddedIDs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*types.MsgLockCredits)
	}{
		{
			name: "session id",
			edit: func(msg *types.MsgLockCredits) { msg.SessionId = " session-1" },
		},
		{
			name: "tool id",
			edit: func(msg *types.MsgLockCredits) { msg.ToolId = "tool-1 " },
		},
		{
			name: "toolpack id",
			edit: func(msg *types.MsgLockCredits) { msg.ToolpackId = " toolpack-1 " },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMsgSrvFixture(t)
			routerAddr := newAccAddress()
			f.accKeeper.accounts[routerAddr.String()] = routerAddr
			f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))

			msg := &types.MsgLockCredits{
				Router:    routerAddr.String(),
				SessionId: "session-1",
				ToolId:    "tool-1",
				Amount:    protoCoin("ulac", "1000"),
			}
			tc.edit(msg)

			resp, err := f.srv.LockCredits(f.ctx, msg)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), "whitespace")
		})
	}
}

func TestMsgServer_LockCredits_TTLCapped(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))

	// Request TTL much larger than max (default 3600s)
	resp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:     routerAddr.String(),
		SessionId:  "session-ttl",
		ToolId:     "tool-1",
		Amount:     protoCoin("ulac", "1000"),
		TtlSeconds: 999_999,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// ExpiresAt should be within max TTL of now
	maxExpires := f.ctx.BlockTime().Add(time.Duration(types.DefaultMaxLockTTLSeconds) * time.Second)
	assert.LessOrEqual(t, resp.ExpiresAt, maxExpires.Unix())
}

// ============================================================
// UnlockCredits
// ============================================================

func TestMsgServer_UnlockCredits_NilMsg(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.UnlockCredits(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_UnlockCredits_EmptyLockID(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.UnlockCredits(f.ctx, &types.MsgUnlockCredits{
		Router: newAccAddress().String(),
		LockId: "",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "lock_id is required")
}

func TestMsgServer_UnlockCredits_LockNotFound(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.UnlockCredits(f.ctx, &types.MsgUnlockCredits{
		Router: newAccAddress().String(),
		LockId: "nonexistent-lock",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "not found")
}

func TestMsgServer_UnlockCredits_WrongRouter(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))

	// Lock credits
	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-unlock",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "5000"),
	})
	require.NoError(t, err)

	// Try to unlock with wrong router
	otherRouter := newAccAddress()
	resp, err := f.srv.UnlockCredits(f.ctx, &types.MsgUnlockCredits{
		Router: otherRouter.String(),
		LockId: lockResp.LockId,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "cannot unlock")
}

func TestMsgServer_UnlockCredits_HappyPath(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))

	// Lock
	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-unlock-ok",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "3000"),
	})
	require.NoError(t, err)

	// Unlock
	resp, err := f.srv.UnlockCredits(f.ctx, &types.MsgUnlockCredits{
		Router: routerAddr.String(),
		LockId: lockResp.LockId,
		Reason: "cancellation",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.AmountUnlocked)
}

func TestMsgServer_UnlockCredits_RejectsPaddedLockID(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))

	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-unlock-padded",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "5000"),
	})
	require.NoError(t, err)

	resp, err := f.srv.UnlockCredits(f.ctx, &types.MsgUnlockCredits{
		Router: routerAddr.String(),
		LockId: " " + lockResp.LockId,
		Reason: "cancelled",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "whitespace")
}

// ============================================================
// SettleCredits
// ============================================================

func TestMsgServer_SettleCredits_NilMsg(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SettleCredits(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_SettleCredits_NilActualCost(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     newAccAddress().String(),
		LockId:     "lock-1",
		ReceiptId:  "receipt-1",
		ToolId:     "tool-1",
		Publisher:  newAccAddress().String(),
		ActualCost: nil,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "actual_cost is required")
}

func TestMsgServer_SettleCredits_EmptyLockID(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     newAccAddress().String(),
		LockId:     "",
		ReceiptId:  "receipt-1",
		ToolId:     "tool-1",
		ActualCost: protoCoin("ulac", "100"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "lock_id is required")
}

func TestMsgServer_SettleCredits_EmptyReceiptID(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     newAccAddress().String(),
		LockId:     "lock-1",
		ReceiptId:  "",
		ToolId:     "tool-1",
		ActualCost: protoCoin("ulac", "100"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "receipt_id is required")
}

func TestMsgServer_SettleCredits_EmptyToolID(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     newAccAddress().String(),
		LockId:     "lock-1",
		ReceiptId:  "receipt-1",
		ToolId:     "",
		ActualCost: protoCoin("ulac", "100"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "tool_id is required")
}

func TestMsgServer_SettleCredits_LockNotFound(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     newAccAddress().String(),
		LockId:     "nonexistent-lock",
		ReceiptId:  "receipt-1",
		ToolId:     "tool-1",
		Publisher:  newAccAddress().String(),
		ActualCost: protoCoin("ulac", "100"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "not found")
}

func TestMsgServer_SettleCredits_WrongRouter(t *testing.T) {
	f, registry := newMsgSrvFixtureWithRegistry(t)
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))
	registry.publishers["tool-1"] = publisherAddr

	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-settle-wrong",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "5000"),
	})
	require.NoError(t, err)

	otherRouter := newAccAddress()
	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     otherRouter.String(),
		LockId:     lockResp.LockId,
		ReceiptId:  "receipt-1",
		ToolId:     "tool-1",
		Publisher:  publisherAddr.String(),
		ActualCost: protoCoin("ulac", "3000"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "cannot settle")
}

func TestMsgServer_SettleCredits_ToolIDMismatch(t *testing.T) {
	f, registry := newMsgSrvFixtureWithRegistry(t)
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))
	registry.publishers["tool-1"] = publisherAddr

	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-settle-mismatch",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "5000"),
	})
	require.NoError(t, err)

	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockResp.LockId,
		ReceiptId:  "receipt-1",
		ToolId:     "tool-WRONG",
		Publisher:  publisherAddr.String(),
		ActualCost: protoCoin("ulac", "3000"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "tool id mismatch")
}

func TestMsgServer_SettleCredits_MissingPublisher(t *testing.T) {
	f, registry := newMsgSrvFixtureWithRegistry(t)
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))
	registry.publishers["tool-1"] = publisherAddr

	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-settle-nopub",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "5000"),
	})
	require.NoError(t, err)

	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockResp.LockId,
		ReceiptId:  "receipt-1",
		ToolId:     "tool-1",
		Publisher:  "", // missing
		ActualCost: protoCoin("ulac", "3000"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid publisher address")
}

func TestMsgServer_SettleCredits_CacheHitMissingOrigin(t *testing.T) {
	f, registry := newMsgSrvFixtureWithRegistry(t)
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))
	registry.publishers["tool-1"] = publisherAddr

	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-cache-miss",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "5000"),
	})
	require.NoError(t, err)

	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:       routerAddr.String(),
		LockId:       lockResp.LockId,
		ReceiptId:    "receipt-cache",
		ToolId:       "tool-1",
		Publisher:    publisherAddr.String(),
		ActualCost:   protoCoin("ulac", "3000"),
		CacheHit:     true,
		OriginToolId: "", // missing origin for cache hit
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "origin_tool_id is required")
}

func TestMsgServer_SettleCredits_CacheHitSameOrigin(t *testing.T) {
	f, registry := newMsgSrvFixtureWithRegistry(t)
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000)))
	registry.publishers["tool-1"] = publisherAddr

	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-cache-same",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "5000"),
	})
	require.NoError(t, err)

	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:       routerAddr.String(),
		LockId:       lockResp.LockId,
		ReceiptId:    "receipt-cache-same",
		ToolId:       "tool-1",
		Publisher:    publisherAddr.String(),
		ActualCost:   protoCoin("ulac", "3000"),
		CacheHit:     true,
		OriginToolId: "tool-1", // same as tool_id
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "must differ")
}

func TestMsgServer_SettleCredits_HappyPath(t *testing.T) {
	f, registry := newMsgSrvFixtureWithRegistry(t)
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.accKeeper.accounts[publisherAddr.String()] = publisherAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 100_000)))
	registry.publishers["tool-1"] = publisherAddr

	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-settle-ok",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "50000"),
	})
	require.NoError(t, err)

	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockResp.LockId,
		ReceiptId:  "receipt-settle-ok",
		ToolId:     "tool-1",
		Publisher:  publisherAddr.String(),
		ActualCost: protoCoin("ulac", "30000"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// All amount fields should be populated
	assert.NotNil(t, resp.PublisherAmount)
	assert.NotNil(t, resp.BurnAmount)
}

func TestMsgServer_SettleCredits_ZeroActualCostCompletesSettlement(t *testing.T) {
	f, registry := newMsgSrvFixtureWithRegistry(t)
	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	f.accKeeper.accounts[routerAddr.String()] = routerAddr
	f.accKeeper.accounts[publisherAddr.String()] = publisherAddr
	f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 100_000)))
	registry.publishers["tool-1"] = publisherAddr

	lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-settle-zero",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "50000"),
	})
	require.NoError(t, err)

	resp, err := f.srv.SettleCredits(f.ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockResp.LockId,
		ReceiptId:  "receipt-settle-zero",
		ToolId:     "tool-1",
		Publisher:  publisherAddr.String(),
		ActualCost: protoCoin("ulac", "0"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "50000", resp.RefundAmount.Amount)

	lock, found := f.keeper.GetLock(f.ctx, lockResp.LockId)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, lock.Status)

	settlement, found := f.keeper.GetSettlement(f.ctx, "receipt-settle-zero")
	require.True(t, found)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, settlement.Status)
	require.True(t, f.bank.Balance(f.moduleAddr).IsZero())
	require.Equal(t, sdk.NewInt64Coin("ulac", 100_000), f.bank.GetBalance(f.ctx, routerAddr, "ulac"))
}

func TestMsgServer_SettleCredits_RejectsPaddedIDs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*types.MsgSettleCredits, string)
		want string
	}{
		{
			name: "lock id",
			edit: func(msg *types.MsgSettleCredits, lockID string) { msg.LockId = " " + lockID },
			want: "whitespace",
		},
		{
			name: "receipt id",
			edit: func(msg *types.MsgSettleCredits, _ string) { msg.ReceiptId = "receipt-settle-padded " },
			want: "whitespace",
		},
		{
			name: "tool id",
			edit: func(msg *types.MsgSettleCredits, _ string) { msg.ToolId = "\ttool-1" },
			want: "whitespace",
		},
		{
			name: "toolpack id",
			edit: func(msg *types.MsgSettleCredits, _ string) { msg.ToolpackId = " toolpack-1 " },
			want: "whitespace",
		},
		{
			name: "publisher",
			edit: func(msg *types.MsgSettleCredits, _ string) { msg.Publisher = " " + msg.Publisher },
			want: "invalid publisher address",
		},
		{
			name: "referrer",
			edit: func(msg *types.MsgSettleCredits, _ string) { msg.Referrer += " " },
			want: "invalid referrer address",
		},
		{
			name: "origin tool id",
			edit: func(msg *types.MsgSettleCredits, _ string) {
				msg.CacheHit = true
				msg.OriginToolId = " origin-tool"
			},
			want: "whitespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, registry := newMsgSrvFixtureWithRegistry(t)
			routerAddr := newAccAddress()
			publisherAddr := newAccAddress()
			referrerAddr := newAccAddress()
			f.accKeeper.accounts[routerAddr.String()] = routerAddr
			f.accKeeper.accounts[publisherAddr.String()] = publisherAddr
			f.accKeeper.accounts[referrerAddr.String()] = referrerAddr
			f.bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 100_000)))
			registry.publishers["tool-1"] = publisherAddr

			lockResp, err := f.srv.LockCredits(f.ctx, &types.MsgLockCredits{
				Router:    routerAddr.String(),
				SessionId: "session-settle-padded",
				ToolId:    "tool-1",
				Amount:    protoCoin("ulac", "50000"),
			})
			require.NoError(t, err)

			msg := &types.MsgSettleCredits{
				Router:     routerAddr.String(),
				LockId:     lockResp.LockId,
				ReceiptId:  "receipt-settle-padded",
				ToolId:     "tool-1",
				Publisher:  publisherAddr.String(),
				Referrer:   referrerAddr.String(),
				ActualCost: protoCoin("ulac", "30000"),
			}
			tc.edit(msg, lockResp.LockId)

			resp, err := f.srv.SettleCredits(f.ctx, msg)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// ============================================================
// SettleOverdraft
// ============================================================

func TestMsgServer_SettleOverdraft_NilMsg(t *testing.T) {
	f := newMsgSrvFixture(t)

	resp, err := f.srv.SettleOverdraft(f.ctx, nil)

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "empty request")
}

func TestMsgServer_SettleOverdraft_DisabledParamsFailClosed(t *testing.T) {
	f := newMsgSrvFixture(t)
	routerAddr := newAccAddress()

	resp, err := f.srv.SettleOverdraft(f.ctx, validKeeperSettleOverdraftMsg(routerAddr.String()))

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "overdraft settlement disabled")
	require.Contains(t, err.Error(), "lock-per-call")
}

func TestMsgServer_SettleOverdraft_StaleThresholdFailsClosed(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()
	routerAddr := newAccAddress()
	_, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:                        authority,
		OverdraftMaxCreditLineToBondBps:  5000,
		OverdraftLiquidationThresholdBps: 7000,
	})
	require.NoError(t, err)

	resp, err := f.srv.SettleOverdraft(f.ctx, validKeeperSettleOverdraftMsg(routerAddr.String()))

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "stale overdraft liquidation threshold")
}

func TestMsgServer_SettleOverdraft_EnabledParamsStillFailClosedUntilExecutionLands(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()
	routerAddr := newAccAddress()
	_, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:                        authority,
		OverdraftMaxCreditLineToBondBps:  5000,
		OverdraftLiquidationThresholdBps: 8000,
	})
	require.NoError(t, err)

	resp, err := f.srv.SettleOverdraft(f.ctx, validKeeperSettleOverdraftMsg(routerAddr.String()))

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "overdraft settlement execution not wired")
	require.Contains(t, err.Error(), "lock-per-call")
}

// ============================================================
// UpdateParams
// ============================================================

func TestMsgServer_UpdateParams_NilMsg(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.UpdateParams(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_UpdateParams_WrongAuthority(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:         "ulumera1wrong",
		MaxLockTtlSeconds: 7200,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid authority")
}

func TestMsgServer_UpdateParams_HappyPath(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:                        authority,
		MaxLockTtlSeconds:                7200,
		BurnRateSpendBps:                 500,
		BurnRateAcqBps:                   200,
		OverdraftMaxCreditLineToBondBps:  5000,
		OverdraftLiquidationThresholdBps: 8000,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify params updated
	params := f.keeper.GetParams(f.ctx)
	assert.Equal(t, uint32(7200), params.MaxLockTtlSeconds)
	assert.Equal(t, uint32(500), params.BurnRateSpendBps)
	assert.Equal(t, uint32(200), params.BurnRateAcqBps)
	assert.Equal(t, uint32(5000), params.OverdraftMaxCreditLineToBondBps)
	assert.Equal(t, uint32(8000), params.OverdraftLiquidationThresholdBps)
}

func TestMsgServer_UpdateParams_PartialUpdate(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()

	// Get original params
	originalParams := f.keeper.GetParams(f.ctx)

	// Only update burn rate
	resp, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:        authority,
		BurnRateSpendBps: 999,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Burn rate updated, others unchanged
	params := f.keeper.GetParams(f.ctx)
	assert.Equal(t, uint32(999), params.BurnRateSpendBps)
	assert.Equal(t, originalParams.MaxLockTtlSeconds, params.MaxLockTtlSeconds)
}

func TestMsgServer_UpdateParams_RejectsInvalidOverdraftParamsBeforePersist(t *testing.T) {
	tests := []struct {
		name string
		msg  types.MsgUpdateParams
		want string
	}{
		{
			name: "credit line ratio without liquidation threshold",
			msg: types.MsgUpdateParams{
				OverdraftMaxCreditLineToBondBps: 5000,
			},
			want: "overdraft liquidation threshold",
		},
		{
			name: "liquidation threshold without credit line ratio",
			msg: types.MsgUpdateParams{
				OverdraftLiquidationThresholdBps: 8000,
			},
			want: "overdraft max credit line",
		},
		{
			name: "excessive credit line ratio",
			msg: types.MsgUpdateParams{
				OverdraftMaxCreditLineToBondBps:  types.MaxBasisPoints + 1,
				OverdraftLiquidationThresholdBps: 8000,
			},
			want: "overdraft max credit line",
		},
		{
			name: "excessive liquidation threshold",
			msg: types.MsgUpdateParams{
				OverdraftMaxCreditLineToBondBps:  5000,
				OverdraftLiquidationThresholdBps: types.MaxBasisPoints + 1,
			},
			want: "overdraft liquidation threshold",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMsgSrvFixture(t)
			authority := authtypes.NewModuleAddress("gov").String()
			before := f.keeper.GetParams(f.ctx)
			msg := tc.msg
			msg.Authority = authority

			resp, err := f.srv.UpdateParams(f.ctx, &msg)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), tc.want)

			after := f.keeper.GetParams(f.ctx)
			require.Equal(t, before.OverdraftMaxCreditLineToBondBps, after.OverdraftMaxCreditLineToBondBps)
			require.Equal(t, before.OverdraftLiquidationThresholdBps, after.OverdraftLiquidationThresholdBps)
		})
	}
}

func TestMsgServer_UpdateParams_DisableOverdraft(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()

	// Enable overdraft first.
	_, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:                        authority,
		OverdraftMaxCreditLineToBondBps:  5000,
		OverdraftLiquidationThresholdBps: 8000,
	})
	require.NoError(t, err)

	// Zero-valued numeric fields mean "leave unchanged", so the explicit
	// disable flag is the only way back to the disabled (0, 0) state.
	resp, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:        authority,
		DisableOverdraft: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	params := f.keeper.GetParams(f.ctx)
	assert.Equal(t, uint32(0), params.OverdraftMaxCreditLineToBondBps)
	assert.Equal(t, uint32(0), params.OverdraftLiquidationThresholdBps)
}

func TestMsgServer_UpdateParams_DisableOverdraftLeavesOtherParamsUntouched(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()

	_, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:                        authority,
		BurnRateSpendBps:                 500,
		OverdraftMaxCreditLineToBondBps:  5000,
		OverdraftLiquidationThresholdBps: 8000,
	})
	require.NoError(t, err)

	_, err = f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:        authority,
		DisableOverdraft: true,
	})
	require.NoError(t, err)

	params := f.keeper.GetParams(f.ctx)
	assert.Equal(t, uint32(500), params.BurnRateSpendBps)
	assert.Equal(t, uint32(0), params.OverdraftMaxCreditLineToBondBps)
	assert.Equal(t, uint32(0), params.OverdraftLiquidationThresholdBps)
}

func TestMsgServer_UpdateParams_DisableOverdraftRejectsConflictingValues(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()
	before := f.keeper.GetParams(f.ctx)

	resp, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:                        authority,
		DisableOverdraft:                 true,
		OverdraftMaxCreditLineToBondBps:  5000,
		OverdraftLiquidationThresholdBps: 8000,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "disable_overdraft")

	after := f.keeper.GetParams(f.ctx)
	require.Equal(t, before.OverdraftMaxCreditLineToBondBps, after.OverdraftMaxCreditLineToBondBps)
	require.Equal(t, before.OverdraftLiquidationThresholdBps, after.OverdraftLiquidationThresholdBps)
}

func TestMsgServer_UpdateParams_DisableBurnRateAdjustment(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()

	_, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:               authority,
		BurnRateAdjustmentEpoch: 200,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(200), f.keeper.GetParams(f.ctx).BurnRateAdjustmentEpoch)

	// Zero epoch means "leave unchanged", so the explicit flag is the only
	// way to switch the adaptive burn controller off.
	_, err = f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:                 authority,
		DisableBurnRateAdjustment: true,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(0), f.keeper.GetParams(f.ctx).BurnRateAdjustmentEpoch)
}

func TestMsgServer_UpdateParams_ResetDisputeWindow(t *testing.T) {
	f := newMsgSrvFixture(t)
	authority := authtypes.NewModuleAddress("gov").String()

	_, err := f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:          authority,
		DisputeWindowHours: 48,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(48), f.keeper.GetParams(f.ctx).DisputeWindowHours)

	// Zero hours means "leave unchanged", so the explicit flag is the only
	// way back to deferring to the registry's canonical dispute window.
	_, err = f.srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority:          authority,
		ResetDisputeWindow: true,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(0), f.keeper.GetParams(f.ctx).DisputeWindowHours)
}

func TestMsgServer_UpdateParams_DisableFlagsRejectConflictingValues(t *testing.T) {
	tests := []struct {
		name string
		msg  types.MsgUpdateParams
		want string
	}{
		{
			name: "disable burn rate adjustment with positive epoch",
			msg: types.MsgUpdateParams{
				DisableBurnRateAdjustment: true,
				BurnRateAdjustmentEpoch:   100,
			},
			want: "disable_burn_rate_adjustment",
		},
		{
			name: "reset dispute window with positive hours",
			msg: types.MsgUpdateParams{
				ResetDisputeWindow: true,
				DisputeWindowHours: 24,
			},
			want: "reset_dispute_window",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMsgSrvFixture(t)
			before := f.keeper.GetParams(f.ctx)
			msg := tc.msg
			msg.Authority = authtypes.NewModuleAddress("gov").String()

			resp, err := f.srv.UpdateParams(f.ctx, &msg)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), tc.want)

			after := f.keeper.GetParams(f.ctx)
			require.Equal(t, before.BurnRateAdjustmentEpoch, after.BurnRateAdjustmentEpoch)
			require.Equal(t, before.DisputeWindowHours, after.DisputeWindowHours)
		})
	}
}

// ============================================================
// SwapLUMEtoLAC
// ============================================================

func TestMsgServer_SwapLUMEtoLAC_NilMsg(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SwapLUMEtoLAC(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_SwapLUMEtoLAC_InvalidSender(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SwapLUMEtoLAC(f.ctx, &types.MsgSwapLUMEtoLAC{
		Sender:     "not-bech32",
		LumeAmount: protoCoin("ulume", "1000"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid sender address")
}

func TestMsgServer_SwapLUMEtoLAC_NilAmount(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SwapLUMEtoLAC(f.ctx, &types.MsgSwapLUMEtoLAC{
		Sender:     newAccAddress().String(),
		LumeAmount: nil,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "lume_amount is required")
}

func TestMsgServer_SwapLUMEtoLAC_WrongDenom(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SwapLUMEtoLAC(f.ctx, &types.MsgSwapLUMEtoLAC{
		Sender:     newAccAddress().String(),
		LumeAmount: protoCoin("ulac", "1000"), // LAC not LUME
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "expected LUME token")
}

func TestMsgServer_SwapLUMEtoLAC_HappyPath(t *testing.T) {
	f := newMsgSrvFixture(t)
	userAddr := newAccAddress()
	f.accKeeper.accounts[userAddr.String()] = userAddr
	f.bank.FundAccount(userAddr, sdk.NewCoins(sdk.NewInt64Coin("ulume", 10_000)))

	resp, err := f.srv.SwapLUMEtoLAC(f.ctx, &types.MsgSwapLUMEtoLAC{
		Sender:     userAddr.String(),
		LumeAmount: protoCoin("ulume", "5000"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.LacReceived)
	assert.NotNil(t, resp.BurnAmount)
	assert.NotEmpty(t, resp.ExchangeRate)
}

func TestMsgServer_SwapLUMEtoLAC_MinLacOutTooHigh(t *testing.T) {
	f := newMsgSrvFixture(t)
	userAddr := newAccAddress()
	f.accKeeper.accounts[userAddr.String()] = userAddr
	f.bank.FundAccount(userAddr, sdk.NewCoins(sdk.NewInt64Coin("ulume", 10_000)))
	beforeLume := f.bank.GetBalance(f.ctx, userAddr, "ulume")
	beforeLac := f.bank.GetBalance(f.ctx, userAddr, "ulac")

	resp, err := f.srv.SwapLUMEtoLAC(f.ctx, &types.MsgSwapLUMEtoLAC{
		Sender:     userAddr.String(),
		LumeAmount: protoCoin("ulume", "5000"),
		MinLacOut:  "5001",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "minimum LAC output not met")
	assert.True(t, beforeLume.Equal(f.bank.GetBalance(f.ctx, userAddr, "ulume")))
	assert.True(t, beforeLac.Equal(f.bank.GetBalance(f.ctx, userAddr, "ulac")))
}

// ============================================================
// SwapLACtoLUME
// ============================================================

func TestMsgServer_SwapLACtoLUME_NilMsg(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SwapLACtoLUME(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_SwapLACtoLUME_InvalidSender(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SwapLACtoLUME(f.ctx, &types.MsgSwapLACtoLUME{
		Sender:    "not-bech32",
		LacAmount: protoCoin("ulac", "1000"),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid sender address")
}

func TestMsgServer_SwapLACtoLUME_NilAmount(t *testing.T) {
	f := newMsgSrvFixture(t)
	resp, err := f.srv.SwapLACtoLUME(f.ctx, &types.MsgSwapLACtoLUME{
		Sender:    newAccAddress().String(),
		LacAmount: nil,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "lac_amount is required")
}

func TestMsgServer_SwapLACtoLUME_HappyPath(t *testing.T) {
	f := newMsgSrvFixture(t)
	userAddr := newAccAddress()
	f.accKeeper.accounts[userAddr.String()] = userAddr

	// First swap LUME to LAC to get some LAC
	f.bank.FundAccount(userAddr, sdk.NewCoins(sdk.NewInt64Coin("ulume", 10_000)))
	swapResp, err := f.srv.SwapLUMEtoLAC(f.ctx, &types.MsgSwapLUMEtoLAC{
		Sender:     userAddr.String(),
		LumeAmount: protoCoin("ulume", "5000"),
	})
	require.NoError(t, err)
	require.NotNil(t, swapResp)

	// Now swap LAC back to LUME
	lacBalance := f.bank.GetBalance(f.ctx, userAddr, "ulac")
	if lacBalance.IsPositive() {
		resp, err := f.srv.SwapLACtoLUME(f.ctx, &types.MsgSwapLACtoLUME{
			Sender:    userAddr.String(),
			LacAmount: protoCoin("ulac", lacBalance.Amount.String()),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotNil(t, resp.LumeReceived)
		assert.NotNil(t, resp.BurnAmount)
		assert.NotEmpty(t, resp.ExchangeRate)
	}
}

func TestMsgServer_SwapLACtoLUME_MinLumeOutTooHigh(t *testing.T) {
	f := newMsgSrvFixture(t)
	userAddr := newAccAddress()
	f.accKeeper.accounts[userAddr.String()] = userAddr
	f.bank.FundAccount(userAddr, sdk.NewCoins(sdk.NewInt64Coin("ulume", 10_000)))
	swapResp, err := f.srv.SwapLUMEtoLAC(f.ctx, &types.MsgSwapLUMEtoLAC{
		Sender:     userAddr.String(),
		LumeAmount: protoCoin("ulume", "5000"),
	})
	require.NoError(t, err)
	require.NotNil(t, swapResp)
	lacBalance := f.bank.GetBalance(f.ctx, userAddr, "ulac")
	lumeBalance := f.bank.GetBalance(f.ctx, userAddr, "ulume")

	resp, err := f.srv.SwapLACtoLUME(f.ctx, &types.MsgSwapLACtoLUME{
		Sender:     userAddr.String(),
		LacAmount:  protoCoin("ulac", lacBalance.Amount.String()),
		MinLumeOut: lacBalance.Amount.AddRaw(1).String(),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "minimum LUME output not met")
	assert.True(t, lacBalance.Equal(f.bank.GetBalance(f.ctx, userAddr, "ulac")))
	assert.True(t, lumeBalance.Equal(f.bank.GetBalance(f.ctx, userAddr, "ulume")))
}

// ============================================================
// formatRatioFixed4
// ============================================================

func TestFormatRatioFixed4(t *testing.T) {
	tests := []struct {
		name string
		num  int64
		den  int64
		want string
	}{
		{"1:1", 10000, 10000, "1.0000"},
		{"1:2", 5000, 10000, "0.5000"},
		{"zero denominator", 1000, 0, "0.0000"},
		{"zero numerator", 0, 1000, "0.0000"},
		{"negative numerator", -1, 1000, "0.0000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			num := sdkmath.NewInt(tt.num)
			den := sdkmath.NewInt(tt.den)
			got := formatRatioFixed4(num, den)
			assert.Equal(t, tt.want, got)
		})
	}
}
