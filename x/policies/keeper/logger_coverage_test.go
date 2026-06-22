//go:build cosmos
// +build cosmos

package keeper

import (
	"testing"
	"time"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// setupPoliciesKeeperNoParams mirrors setupPoliciesKeeper but
// deliberately skips the SetParams call so GetParams hits the
// NotFound fallback branch.
func setupPoliciesKeeperNoParams(t *testing.T) (sdk.Context, *Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	keeper := NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		authtypes.NewModuleAddress("gov").String(),
	)
	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	// INTENTIONALLY no SetParams here — exercise the GetParams fallback.
	return ctx, keeper
}

// Logger and the GetParams NotFound-fallback branch were both unreached
// by existing tests. The module logger is dereferenced by production
// code during enforcement failures (e.g., checkBudgetLimits warnings);
// a nil-return regression would crash policy evaluation on the first
// violation log. GetParams must fall back to DefaultParams at chain
// init before genesis runs — setupPoliciesKeeper always calls SetParams
// so the NotFound branch was never hit.

// TestKeeper_Logger_ReturnsNonNil pins the defensive invariant. Unlike
// other modules where Logger is a field-access, x/policies constructs
// the logger each call (ctx.Logger().With(...)) — a regression that
// made ctx.Logger() return nil (e.g. by unwrapping with an
// uninitialized sdkCtx) would panic.
func TestKeeper_Logger_ReturnsNonNil(t *testing.T) {
	ctx, keeper := setupPoliciesKeeper(t)
	require.NotNil(t, keeper.Logger(ctx),
		"Logger must never return nil — enforcement failure paths dereference directly")
}

// TestKeeper_Logger_TagsByModuleName pins the source-of-truth for the
// logger tag. The tag drives log-aggregator routing; a silent rename
// of ModuleName would strand all x/policies events in the default
// bucket without failing any other test.
func TestKeeper_Logger_TagsByModuleName(t *testing.T) {
	require.Equal(t, "policies", types.ModuleName,
		"ModuleName is the source-of-truth for the x/policies logger tag — "+
			"changing it is a cross-cutting rename that must be deliberate")
}

// TestKeeper_GetParams_FallsBackToDefaults pins the NotFound branch
// of GetParams. Without this test, a regression that returned the
// raw NotFound error to callers (instead of DefaultParams) would
// crash chain init before the first MsgUpdateParams ran.
//
// This test builds a fresh keeper without the SetParams call that
// setupPoliciesKeeper performs — the only way to reach the branch.
func TestKeeper_GetParams_FallsBackToDefaults(t *testing.T) {
	ctx, keeper := setupPoliciesKeeperNoParams(t)

	got, err := keeper.GetParams(ctx)
	require.NoError(t, err,
		"GetParams must not surface NotFound to callers — DefaultParams is the genesis-init contract")
	require.NotNil(t, got, "GetParams must never return nil")
	require.Equal(t, types.DefaultParams(), got,
		"unset params must fall back to DefaultParams verbatim (not zero-value)")
}
