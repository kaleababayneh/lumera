
package cli_test

import (
	"context"
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/internal/testutil"
	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestQueryParams(t *testing.T) {
	ctx, k := setupCreditsKeeper(t)
	var goCtx context.Context = ctx
	qs := creditskeeper.NewQueryServer(k)

	resp, err := qs.Params(goCtx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Params)
}

func TestQueryLock(t *testing.T) {
	ctx, k := setupCreditsKeeper(t)
	var goCtx context.Context = ctx
	qs := creditskeeper.NewQueryServer(k)

	// Create a lock
	lock := &types.Lock{
		LockId:    "test-lock-1",
		Router:    "lumera1router",
		SessionId: "session-1",
		Amount:    &v1beta1.Coin{Denom: "lac", Amount: "1000"},
		Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
	}
	require.NoError(t, k.SaveLock(ctx, lock))

	// Query the lock
	resp, err := qs.Lock(goCtx, &types.QueryLockRequest{LockId: "test-lock-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Lock)
	require.Equal(t, "test-lock-1", resp.Lock.LockId)
	require.Equal(t, "lumera1router", resp.Lock.Router)
}

func TestQueryLockNotFound(t *testing.T) {
	ctx, k := setupCreditsKeeper(t)
	var goCtx context.Context = ctx
	qs := creditskeeper.NewQueryServer(k)

	resp, err := qs.Lock(goCtx, &types.QueryLockRequest{LockId: "nonexistent-lock"})
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestQueryLockInvalidRequest(t *testing.T) {
	ctx, k := setupCreditsKeeper(t)
	var goCtx context.Context = ctx
	qs := creditskeeper.NewQueryServer(k)

	// Empty lock_id should fail
	resp, err := qs.Lock(goCtx, &types.QueryLockRequest{LockId: ""})
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestQueryLocksEmpty(t *testing.T) {
	ctx, k := setupCreditsKeeper(t)
	var goCtx context.Context = ctx
	qs := creditskeeper.NewQueryServer(k)

	resp, err := qs.Locks(goCtx, &types.QueryLocksRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Locks)
}

func TestQueryLocks(t *testing.T) {
	ctx, k := setupCreditsKeeper(t)
	var goCtx context.Context = ctx
	qs := creditskeeper.NewQueryServer(k)

	// Create multiple locks
	for i := 1; i <= 5; i++ {
		lock := &types.Lock{
			LockId:    "lock-" + string(rune('a'+i-1)),
			Router:    "lumera1router",
			SessionId: "session-" + string(rune('0'+i)),
			Amount:    &v1beta1.Coin{Denom: "lac", Amount: "1000"},
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		}
		require.NoError(t, k.SaveLock(ctx, lock))
	}

	resp, err := qs.Locks(goCtx, &types.QueryLocksRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Locks, 5)
}

func TestQueryLocksWithRouterFilter(t *testing.T) {
	ctx, k := setupCreditsKeeper(t)
	var goCtx context.Context = ctx
	qs := creditskeeper.NewQueryServer(k)

	// Create locks with different routers
	locks := []struct {
		id     string
		router string
	}{
		{"lock-1", "router-a"},
		{"lock-2", "router-a"},
		{"lock-3", "router-b"},
	}

	for _, l := range locks {
		lock := &types.Lock{
			LockId:    l.id,
			Router:    l.router,
			SessionId: "session",
			Amount:    &v1beta1.Coin{Denom: "lac", Amount: "1000"},
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		}
		require.NoError(t, k.SaveLock(ctx, lock))
	}

	// Filter by router-a
	resp, err := qs.Locks(goCtx, &types.QueryLocksRequest{Router: "router-a"})
	require.NoError(t, err)
	require.Len(t, resp.Locks, 2)
	for _, lock := range resp.Locks {
		require.Equal(t, "router-a", lock.Router)
	}

	// Filter by router-b
	resp, err = qs.Locks(goCtx, &types.QueryLocksRequest{Router: "router-b"})
	require.NoError(t, err)
	require.Len(t, resp.Locks, 1)
	require.Equal(t, "router-b", resp.Locks[0].Router)
}

func TestQueryLockWithDifferentStatuses(t *testing.T) {
	ctx, k := setupCreditsKeeper(t)
	var goCtx context.Context = ctx
	qs := creditskeeper.NewQueryServer(k)

	statuses := []types.LockStatus{
		types.LockStatus_LOCK_STATUS_ACTIVE,
		types.LockStatus_LOCK_STATUS_RELEASED,
		types.LockStatus_LOCK_STATUS_BURNED,
		types.LockStatus_LOCK_STATUS_EXPIRED,
	}

	for i, status := range statuses {
		lock := &types.Lock{
			LockId:    "lock-status-" + string(rune('0'+i)),
			Router:    "router",
			SessionId: "session",
			Amount:    &v1beta1.Coin{Denom: "lac", Amount: "1000"},
			Status:    status,
		}
		require.NoError(t, k.SaveLock(ctx, lock))
	}

	// Query each lock and verify status
	for i, expectedStatus := range statuses {
		lockID := "lock-status-" + string(rune('0'+i))
		resp, err := qs.Lock(goCtx, &types.QueryLockRequest{LockId: lockID})
		require.NoError(t, err)
		require.NotNil(t, resp.Lock)
		require.Equal(t, expectedStatus, resp.Lock.Status)
	}
}

func setupCreditsKeeper(t *testing.T) (sdk.Context, *creditskeeper.Keeper) {
	t.Helper()

	ctx, lumeraApp := testutil.SetupTestApp(t)

	if params := lumeraApp.CreditsKeeper.GetParams(ctx); params == nil || params.CreditDenom == "" {
		require.NoError(t, lumeraApp.CreditsKeeper.SetParams(ctx, types.DefaultParams()))
	}

	return ctx, lumeraApp.CreditsKeeper
}
