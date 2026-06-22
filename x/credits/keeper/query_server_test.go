package keeper

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// ---------------------------------------------------------------------------
// Params query tests (supplements coverage_boost_test.go TestQueryServer_Params)
// ---------------------------------------------------------------------------

// skipCloneLockGap skips tests whose only failure is the production cloneLock /
// cloneLocks path in query_server.go panicking after the gogoproto migration.
// proto.Clone (gogo table_merge) cannot merge a Lock once its sdk.Coin.Amount
// (a math.Int / big.Int customtype) has been through a state Save->Get
// round-trip: the unmarshalled Amount holds a non-nil big.Int whose internal
// []big.Word slice has no registered gogo merger, so proto.Clone panics with
// "merger not found for type:big.Word". This is a production gap (cloneLock
// should deep-copy via codec marshal/unmarshal, not proto.Clone); it is NOT
// test drift, so per the porting rules these tests are skipped rather than
// weakened or fixed in production code.
func skipCloneLockGap(t *testing.T) {
	t.Helper()
	// FIXED: cloneLock/cloneLocks now deep-copy via a marshal/unmarshal round-trip
	// (deepCopyProto) instead of proto.Clone, so a state-round-tripped Lock with a
	// populated sdk.Coin.Amount no longer panics. These tests are now active.
}

func TestQueryServer_Params_NilRequest(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	// Params ignores the request, should still work with nil
	resp, err := qs.Params(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, resp.Params)
}

func TestQueryServer_Params_MatchesKeeperDirectly(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	direct := keeper.GetParams(ctx)
	resp, err := qs.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, direct.CreditDenom, resp.Params.CreditDenom)
}

func TestQueryServer_Params_CloneSafety(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	params := types.DefaultParams()
	params.CreditDenom = "ulac"
	params.DefaultLockTtlSeconds = 123
	require.NoError(t, keeper.SetParams(ctx, params))

	resp, err := qs.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.Params)

	resp.Params.CreditDenom = "uatom"
	resp.Params.DefaultLockTtlSeconds = 456

	fresh, err := qs.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, "ulac", fresh.Params.CreditDenom)
	require.Equal(t, uint32(123), fresh.Params.DefaultLockTtlSeconds)
}

func TestQueryServer_RejectsNilKeeperBeforeSDKContext(t *testing.T) {
	qs := NewQueryServer(nil)
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "params",
			call: func() error {
				_, err := qs.Params(ctx, &types.QueryParamsRequest{})
				return err
			},
		},
		{
			name: "lock",
			call: func() error {
				_, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: "lock-valid"})
				return err
			},
		},
		{
			name: "hold",
			call: func() error {
				_, err := qs.Hold(ctx, &types.QueryHoldRequest{HoldId: "hold-valid"})
				return err
			},
		},
		{
			name: "locks",
			call: func() error {
				_, err := qs.Locks(ctx, nil)
				return err
			},
		},
		{
			name: "holds",
			call: func() error {
				_, err := qs.Holds(ctx, nil)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.ErrorContains(t, err, "credits keeper not initialized")
		})
	}
}

func TestQueryServer_InvalidRequestsBeforeNilKeeper(t *testing.T) {
	qs := NewQueryServer(nil)
	ctx := context.Background()

	lockCases := []struct {
		name    string
		req     *types.QueryLockRequest
		wantErr string
	}{
		{
			name:    "nil lock request",
			req:     nil,
			wantErr: "lock_id is required",
		},
		{
			name:    "blank lock id",
			req:     &types.QueryLockRequest{LockId: "\t "},
			wantErr: "lock_id is required",
		},
		{
			name:    "padded lock id",
			req:     &types.QueryLockRequest{LockId: " lock-valid"},
			wantErr: "lock_id must not contain leading or trailing whitespace",
		},
	}
	for _, tc := range lockCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := qs.Lock(ctx, tc.req)
			require.ErrorContains(t, err, tc.wantErr)
			require.NotContains(t, err.Error(), "credits keeper not initialized")
		})
	}

	holdCases := []struct {
		name    string
		req     *types.QueryHoldRequest
		wantErr string
	}{
		{
			name:    "nil hold request",
			req:     nil,
			wantErr: "hold_id is required",
		},
		{
			name:    "blank hold id",
			req:     &types.QueryHoldRequest{HoldId: "\n "},
			wantErr: "hold_id is required",
		},
		{
			name:    "padded hold id",
			req:     &types.QueryHoldRequest{HoldId: "hold-valid "},
			wantErr: "hold_id must not contain leading or trailing whitespace",
		},
	}
	for _, tc := range holdCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := qs.Hold(ctx, tc.req)
			require.ErrorContains(t, err, tc.wantErr)
			require.NotContains(t, err.Error(), "credits keeper not initialized")
		})
	}

	_, err := qs.Locks(ctx, &types.QueryLocksRequest{
		Pagination: &query.PageRequest{Key: []byte("lock-1"), Offset: 1},
	})
	require.ErrorContains(t, err, "pagination key and offset are mutually exclusive")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Locks(ctx, &types.QueryLocksRequest{
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.ErrorContains(t, err, "reverse pagination not supported")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Locks(ctx, &types.QueryLocksRequest{Router: " "})
	require.ErrorContains(t, err, "router must not be blank when provided")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Locks(ctx, &types.QueryLocksRequest{Router: " router-a"})
	require.ErrorContains(t, err, "router must not contain leading or trailing whitespace")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Holds(ctx, &types.QueryHoldsRequest{
		Pagination: &query.PageRequest{Key: []byte("hold-1"), Offset: 1},
	})
	require.ErrorContains(t, err, "pagination key and offset are mutually exclusive")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Holds(ctx, &types.QueryHoldsRequest{
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.ErrorContains(t, err, "reverse pagination not supported")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Holds(ctx, &types.QueryHoldsRequest{Router: "\t"})
	require.ErrorContains(t, err, "router must not be blank when provided")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Holds(ctx, &types.QueryHoldsRequest{Router: "router-a "})
	require.ErrorContains(t, err, "router must not contain leading or trailing whitespace")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Holds(ctx, &types.QueryHoldsRequest{SessionId: "\n"})
	require.ErrorContains(t, err, "session_id must not be blank when provided")
	require.NotContains(t, err.Error(), "credits keeper not initialized")

	_, err = qs.Holds(ctx, &types.QueryHoldsRequest{SessionId: " session-a"})
	require.ErrorContains(t, err, "session_id must not contain leading or trailing whitespace")
	require.NotContains(t, err.Error(), "credits keeper not initialized")
}

// ---------------------------------------------------------------------------
// Lock (single) query tests (supplements coverage_boost_test.go tests)
// ---------------------------------------------------------------------------

func TestQueryServer_Lock_DifferentStatuses(t *testing.T) {
	skipCloneLockGap(t)
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	statuses := []struct {
		id     string
		status types.LockStatus
	}{
		{"lock-status-active", types.LockStatus_LOCK_STATUS_ACTIVE},
		{"lock-status-released", types.LockStatus_LOCK_STATUS_RELEASED},
		{"lock-status-burned", types.LockStatus_LOCK_STATUS_BURNED},
		{"lock-status-expired", types.LockStatus_LOCK_STATUS_EXPIRED},
	}

	for _, s := range statuses {
		lock := &types.Lock{
			LockId:    s.id,
			Router:    "lumera1router",
			SessionId: "session-1",
			Amount:    protoCoin("lac", "1000"),
			Status:    s.status,
		}
		require.NoError(t, keeper.SaveLock(ctx, lock))
	}

	for _, s := range statuses {
		resp, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: s.id})
		require.NoError(t, err, "lock %s", s.id)
		require.Equal(t, s.status, resp.Lock.Status, "lock %s status", s.id)
	}
}

func TestQueryServer_Lock_FieldIntegrity(t *testing.T) {
	skipCloneLockGap(t)
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	lock := &types.Lock{
		LockId:    "lock-full-fields",
		Router:    "lumera1routerxyz",
		SessionId: "session-42",
		Amount:    protoCoin("lac", "12345"),
		Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	resp, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: "lock-full-fields"})
	require.NoError(t, err)
	require.Equal(t, "lumera1routerxyz", resp.Lock.Router)
	require.Equal(t, "session-42", resp.Lock.SessionId)
	require.Equal(t, "12345", resp.Lock.Amount.Amount.String())
	require.Equal(t, "lac", resp.Lock.Amount.Denom)
}

func TestQueryServer_Lock_CloneSafety(t *testing.T) {
	skipCloneLockGap(t)
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	lock := &types.Lock{
		LockId:    "lock-clone-safe",
		Router:    "lumera1routerclone",
		SessionId: "session-clone",
		Amount:    protoCoin("lac", "777"),
		Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	resp, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: lock.LockId})
	require.NoError(t, err)
	require.NotNil(t, resp.Lock)

	resp.Lock.Router = "lumera1mutated"
	resp.Lock.Status = types.LockStatus_LOCK_STATUS_BURNED
	resp.Lock.Amount.Amount = sdkmath.NewInt(999)

	fresh, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: lock.LockId})
	require.NoError(t, err)
	require.Equal(t, "lumera1routerclone", fresh.Lock.Router)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, fresh.Lock.Status)
	require.Equal(t, "777", fresh.Lock.Amount.Amount.String())
}

// ---------------------------------------------------------------------------
// Locks (list) query tests — empty and nil edge cases
// ---------------------------------------------------------------------------

func TestQueryServer_Locks_Empty(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	resp, err := qs.Locks(ctx, &types.QueryLocksRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Locks)
}

func TestQueryServer_Locks_NilRequest(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	resp, err := qs.Locks(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// ---------------------------------------------------------------------------
// Locks (list) query tests — pagination
// ---------------------------------------------------------------------------

func TestQueryServer_Locks_Pagination(t *testing.T) {
	skipCloneLockGap(t)
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	// Create 10 test locks with deterministic ordering
	for i := 1; i <= 10; i++ {
		lock := &types.Lock{
			LockId:    fmt.Sprintf("lock-%02d", i), // Zero-padded for consistent ordering
			Router:    "lumera1router",
			SessionId: fmt.Sprintf("session-%d", i),
			Amount:    protoCoin("lac", "1000"),
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		}
		require.NoError(t, keeper.SaveLock(ctx, lock))
	}

	t.Run("default pagination", func(t *testing.T) {
		resp, err := qs.Locks(ctx, &types.QueryLocksRequest{})
		require.NoError(t, err)
		require.Len(t, resp.Locks, 10)
	})

	t.Run("limit pagination", func(t *testing.T) {
		resp, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Limit: 3},
		})
		require.NoError(t, err)
		require.Len(t, resp.Locks, 3)
		require.Equal(t, "lock-01", resp.Locks[0].LockId)
		require.Equal(t, "lock-02", resp.Locks[1].LockId)
		require.Equal(t, "lock-03", resp.Locks[2].LockId)
		require.NotNil(t, resp.Pagination)
		require.NotEmpty(t, resp.Pagination.NextKey) // Should have next page
	})

	t.Run("offset pagination", func(t *testing.T) {
		resp, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Offset: 5, Limit: 3},
		})
		require.NoError(t, err)
		require.Len(t, resp.Locks, 3)
		require.Equal(t, "lock-06", resp.Locks[0].LockId)
		require.Equal(t, "lock-07", resp.Locks[1].LockId)
		require.Equal(t, "lock-08", resp.Locks[2].LockId)
	})

	t.Run("key-based pagination", func(t *testing.T) {
		// Get first page
		resp1, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Limit: 3},
		})
		require.NoError(t, err)
		require.Len(t, resp1.Locks, 3)
		require.Equal(t, "lock-01", resp1.Locks[0].LockId)
		require.Equal(t, "lock-02", resp1.Locks[1].LockId)
		require.Equal(t, "lock-03", resp1.Locks[2].LockId)
		require.NotEmpty(t, resp1.Pagination.NextKey)
		require.Equal(t, "lock-03", string(resp1.Pagination.NextKey))

		// Get second page using key (starts after lock-03)
		resp2, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Key: resp1.Pagination.NextKey, Limit: 3},
		})
		require.NoError(t, err)
		require.Len(t, resp2.Locks, 3)
		require.Equal(t, "lock-04", resp2.Locks[0].LockId)
		require.Equal(t, "lock-05", resp2.Locks[1].LockId)
		require.Equal(t, "lock-06", resp2.Locks[2].LockId)

		// Get third page
		resp3, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Key: resp2.Pagination.NextKey, Limit: 3},
		})
		require.NoError(t, err)
		require.Len(t, resp3.Locks, 3)
		require.Equal(t, "lock-07", resp3.Locks[0].LockId)
		require.Equal(t, "lock-08", resp3.Locks[1].LockId)
		require.Equal(t, "lock-09", resp3.Locks[2].LockId)

		// Get fourth page (only 1 item left)
		resp4, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Key: resp3.Pagination.NextKey, Limit: 3},
		})
		require.NoError(t, err)
		require.Len(t, resp4.Locks, 1)
		require.Equal(t, "lock-10", resp4.Locks[0].LockId)
		require.Empty(t, resp4.Pagination.NextKey) // No more pages
	})

	t.Run("reject key with offset", func(t *testing.T) {
		_, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Key: []byte("lock-03"), Offset: 1, Limit: 3},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "key and offset")
	})

	t.Run("reject reverse pagination", func(t *testing.T) {
		_, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Reverse: true, Limit: 3},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "reverse pagination not supported")
	})

	t.Run("count total", func(t *testing.T) {
		resp, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Pagination: &query.PageRequest{Limit: 3, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.Locks, 3)
		require.Equal(t, uint64(10), resp.Pagination.Total)
	})

	t.Run("deterministic ordering", func(t *testing.T) {
		resp1, err := qs.Locks(ctx, &types.QueryLocksRequest{})
		require.NoError(t, err)

		resp2, err := qs.Locks(ctx, &types.QueryLocksRequest{})
		require.NoError(t, err)

		require.Equal(t, len(resp1.Locks), len(resp2.Locks))
		for i := range resp1.Locks {
			require.Equal(t, resp1.Locks[i].LockId, resp2.Locks[i].LockId)
		}
	})
}

// ---------------------------------------------------------------------------
// Locks (list) query tests — router filter
// ---------------------------------------------------------------------------

func TestQueryServer_Locks_RouterFilter(t *testing.T) {
	skipCloneLockGap(t)
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	// Create locks with different routers
	routers := []string{"routerA", "routerA", "routerB", "routerA", "routerB"}
	for i, router := range routers {
		lock := &types.Lock{
			LockId:    fmt.Sprintf("lock-%02d", i+1),
			Router:    router,
			SessionId: fmt.Sprintf("session-%d", i),
			Amount:    protoCoin("lac", "1000"),
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		}
		require.NoError(t, keeper.SaveLock(ctx, lock))
	}

	t.Run("filter by router", func(t *testing.T) {
		resp, err := qs.Locks(ctx, &types.QueryLocksRequest{Router: "routerA"})
		require.NoError(t, err)
		require.Len(t, resp.Locks, 3) // Only routerA locks
		for _, lock := range resp.Locks {
			require.Equal(t, "routerA", lock.Router)
		}
	})

	t.Run("filter with pagination and count", func(t *testing.T) {
		resp, err := qs.Locks(ctx, &types.QueryLocksRequest{
			Router:     "routerA",
			Pagination: &query.PageRequest{Limit: 2, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.Locks, 2)
		require.Equal(t, uint64(3), resp.Pagination.Total) // Total matching filter
	})

	t.Run("filter no matches", func(t *testing.T) {
		resp, err := qs.Locks(ctx, &types.QueryLocksRequest{Router: "nonexistent-router"})
		require.NoError(t, err)
		require.Empty(t, resp.Locks)
	})

	t.Run("filter routerB only", func(t *testing.T) {
		resp, err := qs.Locks(ctx, &types.QueryLocksRequest{Router: "routerB"})
		require.NoError(t, err)
		require.Len(t, resp.Locks, 2)
		for _, lock := range resp.Locks {
			require.Equal(t, "routerB", lock.Router)
		}
	})
}

func TestQueryServer_Locks_CloneSafety(t *testing.T) {
	skipCloneLockGap(t)
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	for i := 1; i <= 2; i++ {
		lock := &types.Lock{
			LockId:    fmt.Sprintf("lock-list-clone-%02d", i),
			Router:    "lumera1routerlist",
			SessionId: fmt.Sprintf("session-list-%d", i),
			Amount:    protoCoin("lac", fmt.Sprintf("%d00", i)),
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		}
		require.NoError(t, keeper.SaveLock(ctx, lock))
	}

	resp, err := qs.Locks(ctx, &types.QueryLocksRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp.Locks, 2)

	resp.Locks[0].Router = "lumera1mutated"
	resp.Locks[0].Status = types.LockStatus_LOCK_STATUS_RELEASED
	resp.Locks[0].Amount.Amount = sdkmath.NewInt(999)

	fresh, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: "lock-list-clone-01"})
	require.NoError(t, err)
	require.Equal(t, "lumera1routerlist", fresh.Lock.Router)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, fresh.Lock.Status)
	require.Equal(t, "100", fresh.Lock.Amount.Amount.String())
}

func TestQueryServer_Hold_ByHoldID(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	lock := &types.Lock{
		LockId:    "hold-joined-01",
		Router:    "router-joined",
		SessionId: "session-joined",
		ToolId:    "tool-joined",
		QuoteId:   "quote-joined",
		Amount:    protoCoin(types.DefaultCreditDenom, "1000000"),
		Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
	}
	lock.CreatedAt = ctx.BlockTime()
	lock.ExpiresAt = ctx.BlockTime().Add(10 * time.Minute)
	require.NoError(t, keeper.SaveLock(ctx, lock))

	receiptID := "receipt-joined-01"
	require.NoError(t, keeper.SaveSettlement(ctx, &types.SettlementRecord{
		Id:         receiptID,
		Status:     types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Stage:      "partial",
		FillCount:  2,
		CacheHit:   true,
		ActionId:   "action-joined",
		LockId:     lock.LockId,
		TotalCost:  sdk.Coins{types.CoinToProto(sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000))},
		BurnAmount: sdk.Coins{types.CoinToProto(sdk.NewInt64Coin(types.DefaultCreditDenom, 12_000))},
		NetAmount:  sdk.Coins{types.CoinToProto(sdk.NewInt64Coin(types.DefaultCreditDenom, 388_000))},
		Timestamp:  ctx.BlockTime(),
	}))
	require.NoError(t, keeper.state.LockReceipts.Set(ctx, lock.LockId, receiptID))

	resp, err := qs.Hold(ctx, &types.QueryHoldRequest{HoldId: lock.LockId})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Hold)
	require.Equal(t, lock.LockId, resp.Hold.HoldId)
	require.Equal(t, "session-joined", resp.Hold.SessionId)
	require.Equal(t, types.HoldStatus_HOLD_STATUS_HELD, resp.Hold.Status)
	require.Equal(t, "1", resp.Hold.Economics.ReservedAmount.Amount)
	require.Equal(t, "0.4", resp.Hold.Economics.ChargedAmount.Amount)
	require.Equal(t, "0.6", resp.Hold.Economics.RemainingReservedAmount.Amount)
	require.Equal(t, "0.012", resp.Hold.Economics.BurnedAmount.Amount)
	require.Equal(t, "0.388", resp.Hold.Economics.NetAmount.Amount)
	require.NotNil(t, resp.Hold.Settlement)
	require.Equal(t, receiptID, resp.Hold.Settlement.ReceiptId)
	require.Equal(t, types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_PENDING, resp.Hold.Settlement.Status)
	require.Equal(t, uint64(2), resp.Hold.Settlement.FillCount)
}

func TestQueryServer_Hold_ZeroCostSettlementShowsExplicitEconomics(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0).UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-free",
		lockAmount,
		"tool-free",
		"quote-free",
		"policy@1",
		"intent-free",
	)
	require.NoError(t, err)

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(5 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, sdk.NewInt64Coin(types.DefaultCreditDenom, 0), SettlementRequest{
		ReceiptID:     "receipt-free",
		ToolID:        "tool-free",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	})
	require.NoError(t, err)
	require.True(t, bank.Balance(moduleAddr).IsZero())

	resp, err := qs.Hold(ctx, &types.QueryHoldRequest{HoldId: lockID})
	require.NoError(t, err)
	require.NotNil(t, resp.Hold)
	require.Equal(t, types.HoldStatus_HOLD_STATUS_CAPTURED, resp.Hold.Status)
	require.NotNil(t, resp.Hold.Settlement)
	require.Equal(t, types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_COMPLETED, resp.Hold.Settlement.Status)

	econ := resp.Hold.Economics
	require.NotNil(t, econ)
	require.Equal(t, "1", econ.ReservedAmount.Amount)
	require.NotNil(t, econ.ChargedAmount)
	require.Equal(t, "0", econ.ChargedAmount.Amount)
	require.NotNil(t, econ.BurnedAmount)
	require.Equal(t, "0", econ.BurnedAmount.Amount)
	require.NotNil(t, econ.NetAmount)
	require.Equal(t, "0", econ.NetAmount.Amount)
	require.Equal(t, "0", econ.RemainingReservedAmount.Amount)
	require.NotNil(t, econ.RefundAmount)
	require.Equal(t, "1", econ.RefundAmount.Amount)
}

func TestQueryServer_Holds_BySessionID(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	locks := []*types.Lock{
		{
			LockId:    "hold-session-01",
			Router:    "router-a",
			SessionId: "session-a",
			Amount:    protoCoin(types.DefaultCreditDenom, "1000000"),
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		},
		{
			LockId:    "hold-session-02",
			Router:    "router-a",
			SessionId: "session-a",
			Amount:    protoCoin(types.DefaultCreditDenom, "500000"),
			Status:    types.LockStatus_LOCK_STATUS_RELEASED,
		},
		{
			LockId:    "hold-session-03",
			Router:    "router-b",
			SessionId: "session-b",
			Amount:    protoCoin(types.DefaultCreditDenom, "750000"),
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		},
	}
	for _, lock := range locks {
		require.NoError(t, keeper.SaveLock(ctx, lock))
	}

	resp, err := qs.Holds(ctx, &types.QueryHoldsRequest{SessionId: "session-a"})
	require.NoError(t, err)
	require.Len(t, resp.Holds, 2)
	require.Equal(t, "hold-session-01", resp.Holds[0].HoldId)
	require.Equal(t, types.HoldStatus_HOLD_STATUS_HELD, resp.Holds[0].Status)
	require.Equal(t, "hold-session-02", resp.Holds[1].HoldId)
	require.Equal(t, types.HoldStatus_HOLD_STATUS_RELEASED, resp.Holds[1].Status)
}

func TestQueryServer_Holds_ActiveOnlyPagination(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	statuses := []types.LockStatus{
		types.LockStatus_LOCK_STATUS_ACTIVE,
		types.LockStatus_LOCK_STATUS_RELEASED,
		types.LockStatus_LOCK_STATUS_ACTIVE,
		types.LockStatus_LOCK_STATUS_ACTIVE,
		types.LockStatus_LOCK_STATUS_ACTIVE,
	}
	for i, status := range statuses {
		lock := &types.Lock{
			LockId:    fmt.Sprintf("hold-page-%02d", i+1),
			Router:    "router-page",
			SessionId: "session-page",
			Amount:    protoCoin(types.DefaultCreditDenom, "1000000"),
			Status:    status,
		}
		require.NoError(t, keeper.SaveLock(ctx, lock))
	}

	resp1, err := qs.Holds(ctx, &types.QueryHoldsRequest{
		ActiveOnly: true,
		Pagination: &query.PageRequest{Limit: 2, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, resp1.Holds, 2)
	require.Equal(t, "hold-page-01", resp1.Holds[0].HoldId)
	require.Equal(t, "hold-page-03", resp1.Holds[1].HoldId)
	require.NotNil(t, resp1.Pagination)
	require.Equal(t, uint64(4), resp1.Pagination.Total)
	require.Equal(t, "hold-page-03", string(resp1.Pagination.NextKey))

	resp2, err := qs.Holds(ctx, &types.QueryHoldsRequest{
		ActiveOnly: true,
		Pagination: &query.PageRequest{Key: resp1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp2.Holds, 2)
	require.Equal(t, "hold-page-04", resp2.Holds[0].HoldId)
	require.Equal(t, "hold-page-05", resp2.Holds[1].HoldId)
	require.Empty(t, resp2.Pagination.NextKey)

	_, err = qs.Holds(ctx, &types.QueryHoldsRequest{
		ActiveOnly: true,
		Pagination: &query.PageRequest{Key: resp1.Pagination.NextKey, Offset: 1, Limit: 2},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "key and offset")

	_, err = qs.Holds(ctx, &types.QueryHoldsRequest{
		ActiveOnly: true,
		Pagination: &query.PageRequest{Reverse: true, Limit: 2},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reverse pagination not supported")
}
