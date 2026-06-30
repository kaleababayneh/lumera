package keeper

import (
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/priority/types"
)

func TestGetParamsDefault(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	params, err := keeper.GetParams(sdkCtx)
	require.NoError(t, err)
	require.NotNil(t, params)
	require.Equal(t, "standard", params.DefaultTier)
	require.Len(t, params.Tiers, 4)
}

func TestSetParamsValid(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	customParams := &types.Params{
		DefaultTier: "basic",
		Tiers: []types.Tier{
			{
				Name:                 "basic",
				MaxLatencyMs:         3000,
				AuctionTTLMs:         60000,
				SpotDiscountBps:      50,
				QueueWeight:          100,
				PricingMultiplierBps: 100,
				ReservedCapacityBps:  10000,
			},
		},
	}

	err := keeper.SetParams(sdkCtx, customParams)
	require.NoError(t, err)

	retrieved, err := keeper.GetParams(sdkCtx)
	require.NoError(t, err)
	require.Equal(t, "basic", retrieved.DefaultTier)
	require.Len(t, retrieved.Tiers, 1)
	require.Equal(t, uint32(3000), retrieved.Tiers[0].MaxLatencyMs)
}

func TestSetParamsInvalid(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	tests := []struct {
		name   string
		params *types.Params
	}{
		{
			name:   "nil params",
			params: nil,
		},
		{
			name: "empty default tier",
			params: &types.Params{
				DefaultTier: "",
				Tiers:       types.DefaultParams().Tiers,
			},
		},
		{
			name: "no tiers",
			params: &types.Params{
				DefaultTier: "standard",
				Tiers:       nil,
			},
		},
		{
			name: "default tier not found",
			params: &types.Params{
				DefaultTier: "nonexistent",
				Tiers:       types.DefaultParams().Tiers,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := keeper.SetParams(sdkCtx, tc.params)
			require.Error(t, err)
		})
	}
}

func TestGetParams_InvalidStoredBytes(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	require.NoError(t, keeper.state.Params.Set(sdkCtx, []byte("not-json")))

	_, err := keeper.GetParams(sdkCtx)
	require.Error(t, err)
}

func TestAssignPolicyTierEmptyPolicyID(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	err := keeper.AssignPolicyTier(sdkCtx, "", "standard", time.Hour)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAssignment)
}

func TestAssignPolicyTierRejectsPaddedIdentifiers(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	tests := []struct {
		name     string
		policyID string
		tierName string
	}{
		{
			name:     "blank policy id",
			policyID: " ",
			tierName: "standard",
		},
		{
			name:     "padded policy id",
			policyID: " policy-123 ",
			tierName: "standard",
		},
		{
			name:     "blank tier name",
			policyID: "policy-123",
			tierName: " ",
		},
		{
			name:     "padded tier name",
			policyID: "policy-123",
			tierName: " standard ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := keeper.AssignPolicyTier(sdkCtx, tc.policyID, tc.tierName, time.Hour)
			require.ErrorIs(t, err, types.ErrInvalidAssignment)

			assignment, found, getErr := keeper.GetAssignment(sdkCtx, tc.policyID)
			require.NoError(t, getErr)
			require.False(t, found)
			require.Nil(t, assignment)
		})
	}
}

func TestAssignPolicyTierUnknownTier(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	err := keeper.AssignPolicyTier(sdkCtx, "policy-123", "nonexistent-tier", time.Hour)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPriorityTierNotFound)
}

func TestAssignPolicyTierNoDuration(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	// Zero duration means no expiration
	err := keeper.AssignPolicyTier(sdkCtx, "policy-permanent", "priority", 0)
	require.NoError(t, err)

	// Verify assignment exists
	assignment, found, err := keeper.GetAssignment(sdkCtx, "policy-permanent")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "priority", assignment.Tier)
	require.True(t, assignment.ExpiresAt.IsZero())
}

func TestAssignPolicyTierRejectsNegativeDuration(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	err := keeper.AssignPolicyTier(sdkCtx, "policy-negative", "priority", -time.Second)
	require.ErrorIs(t, err, types.ErrInvalidAssignment)

	assignment, found, err := keeper.GetAssignment(sdkCtx, "policy-negative")
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, assignment)
}

func TestAssignPolicyTierSetsExpiry(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	start := sdkCtx.BlockTime()
	duration := 2 * time.Hour
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-expiry", "express", duration))

	assignment, found, err := keeper.GetAssignment(sdkCtx, "policy-expiry")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, start.Add(duration), assignment.ExpiresAt)
}

func TestClearAssignment(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	// First assign
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-clear", "enterprise", time.Hour))

	// Verify exists
	_, found, err := keeper.GetAssignment(sdkCtx, "policy-clear")
	require.NoError(t, err)
	require.True(t, found)

	// Clear it
	err = keeper.ClearAssignment(sdkCtx, "policy-clear")
	require.NoError(t, err)

	// Verify gone
	_, found, err = keeper.GetAssignment(sdkCtx, "policy-clear")
	require.NoError(t, err)
	require.False(t, found)
}

func TestClearAssignmentNonexistent(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	// Clearing a nonexistent assignment should not error
	err := keeper.ClearAssignment(sdkCtx, "policy-nonexistent")
	require.NoError(t, err)
}

func TestGetAssignment(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	// Not found case
	assignment, found, err := keeper.GetAssignment(sdkCtx, "policy-missing")
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, assignment)

	// Create assignment
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-get", "express", 2*time.Hour))

	// Found case
	assignment, found, err = keeper.GetAssignment(sdkCtx, "policy-get")
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, assignment)
	require.Equal(t, "policy-get", assignment.PolicyID)
	require.Equal(t, "express", assignment.Tier)
	require.False(t, assignment.AssignedAt.IsZero())
	require.False(t, assignment.ExpiresAt.IsZero())
}

func TestResolveAdjustmentsUnassignedPolicy(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	reqLatency := uint32(5000)
	defaultTTL := 60 * time.Second

	// Policy without assignment should use default tier
	adjustments, err := keeper.ResolveAdjustments(sdkCtx, "unassigned-policy", reqLatency, defaultTTL)
	require.NoError(t, err)
	require.True(t, adjustments.Applied)
	require.Equal(t, "standard", adjustments.TierName)
	// Default tier caps latency to 2500ms
	require.Equal(t, uint32(2500), adjustments.MaxLatencyMs)
}

func TestResolveAdjustments_InvalidAssignmentBytes(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	policyID := "policy-invalid-bytes"
	require.NoError(t, keeper.state.Assignments.Set(sdkCtx, policyID, []byte("nope")))

	_, err := keeper.ResolveAdjustments(sdkCtx, policyID, 2_000, 30*time.Second)
	require.Error(t, err)
}

func TestResolveAdjustments_InvalidAssignmentState(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	now := sdkCtx.BlockTime()

	valid := types.PriorityAssignment{
		PolicyID:   "policy-invalid-state",
		Tier:       "enterprise",
		AssignedAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}

	tests := []struct {
		name   string
		mutate func(*types.PriorityAssignment)
	}{
		{
			name: "zero assigned time",
			mutate: func(a *types.PriorityAssignment) {
				a.AssignedAt = time.Time{}
				a.ExpiresAt = time.Time{}
			},
		},
		{
			name: "padded tier",
			mutate: func(a *types.PriorityAssignment) {
				a.Tier = " enterprise "
			},
		},
		{
			name: "policy id mismatches state key",
			mutate: func(a *types.PriorityAssignment) {
				a.PolicyID = "other-policy"
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policyID := valid.PolicyID + "-" + tc.name
			assignment := valid
			assignment.PolicyID = policyID
			tc.mutate(&assignment)

			bz, err := marshalAssignment(assignment)
			require.NoError(t, err)
			require.NoError(t, keeper.state.Assignments.Set(sdkCtx, policyID, bz))

			_, err = keeper.ResolveAdjustments(sdkCtx, policyID, 2_000, 30*time.Second)
			require.ErrorIs(t, err, types.ErrInvalidAssignment)
		})
	}
}

func TestGetAssignment_InvalidAssignmentState(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	policyID := "policy-invalid-get"
	assignment := types.PriorityAssignment{
		PolicyID:   "other-policy",
		Tier:       "enterprise",
		AssignedAt: sdkCtx.BlockTime(),
		ExpiresAt:  sdkCtx.BlockTime().Add(time.Hour),
	}
	bz, err := marshalAssignment(assignment)
	require.NoError(t, err)
	require.NoError(t, keeper.state.Assignments.Set(sdkCtx, policyID, bz))

	got, found, err := keeper.GetAssignment(sdkCtx, policyID)
	require.ErrorIs(t, err, types.ErrInvalidAssignment)
	require.Nil(t, got)
	require.False(t, found)
}

func TestResolveAdjustmentsUnknownTierFallsBack(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	policyID := "policy-unknown-tier"

	now := sdkCtx.BlockTime()
	assignment := types.PriorityAssignment{
		PolicyID:   policyID,
		Tier:       "unknown-tier",
		AssignedAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}
	bz, err := marshalAssignment(assignment)
	require.NoError(t, err)
	require.NoError(t, keeper.state.Assignments.Set(sdkCtx, policyID, bz))

	adj, err := keeper.ResolveAdjustments(sdkCtx, policyID, 2000, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, "standard", adj.TierName)

	stored, found, err := keeper.GetAssignment(sdkCtx, policyID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "unknown-tier", stored.Tier)
}

func TestResolveAdjustmentsLatencyCapping(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	// Enterprise tier has MaxLatencyMs=800
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-latency", "enterprise", time.Hour))

	tests := []struct {
		name            string
		requestLatency  uint32
		expectedLatency uint32
	}{
		{"higher than tier max", 2000, 800},
		{"equal to tier max", 800, 800},
		{"lower than tier max", 500, 500},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adj, err := keeper.ResolveAdjustments(sdkCtx, "policy-latency", tc.requestLatency, 30*time.Second)
			require.NoError(t, err)
			require.Equal(t, tc.expectedLatency, adj.MaxLatencyMs)
		})
	}
}

func TestResolveAdjustmentsTTLCapping(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	// Enterprise tier has AuctionTTL=15s
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-ttl", "enterprise", time.Hour))

	tests := []struct {
		name        string
		requestTTL  time.Duration
		expectedTTL time.Duration
	}{
		{"higher than tier max", 60 * time.Second, 15 * time.Second},
		{"equal to tier max", 15 * time.Second, 15 * time.Second},
		{"lower than tier max", 10 * time.Second, 10 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adj, err := keeper.ResolveAdjustments(sdkCtx, "policy-ttl", 2000, tc.requestTTL)
			require.NoError(t, err)
			require.Equal(t, tc.expectedTTL, adj.AuctionTTL)
		})
	}
}

func TestResolveAdjustmentsExpiredAssignmentCleanup(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	// Assign with very short duration
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-expire", "enterprise", time.Millisecond))

	// Verify assignment exists
	_, found, _ := keeper.GetAssignment(sdkCtx, "policy-expire")
	require.True(t, found)

	// Advance time past expiration
	sdkCtx = sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(time.Second))

	// Resolve should use default and remove expired assignment
	adj, err := keeper.ResolveAdjustments(sdkCtx, "policy-expire", 2000, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, "standard", adj.TierName)

	// Assignment should be cleaned up
	_, found, _ = keeper.GetAssignment(sdkCtx, "policy-expire")
	require.False(t, found)
}

func TestResolveAdjustmentsExpiresAtBoundaryCleanup(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-boundary", "enterprise", time.Second))

	sdkCtx = sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(time.Second))

	adj, err := keeper.ResolveAdjustments(sdkCtx, "policy-boundary", 2000, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, "standard", adj.TierName)

	_, found, err := keeper.GetAssignment(sdkCtx, "policy-boundary")
	require.NoError(t, err)
	require.False(t, found)
}

func TestAssignAndResolveAdjustments(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	reqLatency := uint32(2_500)
	defaultTTL := 45 * time.Second

	// Default tier
	adjustments, err := keeper.ResolveAdjustments(sdkCtx, "policy-x", reqLatency, defaultTTL)
	require.NoError(t, err)
	require.True(t, adjustments.Applied)
	require.Equal(t, "standard", adjustments.TierName)
	require.Equal(t, reqLatency, adjustments.MaxLatencyMs)
	require.Equal(t, defaultTTL, adjustments.AuctionTTL)
	require.Equal(t, uint32(0), adjustments.SpotDiscountBps)

	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-x", "enterprise", time.Hour))

	adjustments, err = keeper.ResolveAdjustments(sdkCtx, "policy-x", reqLatency, defaultTTL)
	require.NoError(t, err)
	require.Equal(t, "enterprise", adjustments.TierName)
	require.Equal(t, uint32(800), adjustments.MaxLatencyMs)    // enterprise has lower max latency
	require.Equal(t, 15*time.Second, adjustments.AuctionTTL)   // enterprise has shorter TTL
	require.Equal(t, uint32(300), adjustments.SpotDiscountBps) // enterprise has highest discount
}

func TestResolveAdjustmentsNewFields(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	reqLatency := uint32(2_500)
	defaultTTL := 45 * time.Second

	// Standard tier - verify new fields
	adjustments, err := keeper.ResolveAdjustments(sdkCtx, "policy-std", reqLatency, defaultTTL)
	require.NoError(t, err)
	require.Equal(t, "standard", adjustments.TierName)
	require.Equal(t, uint32(100), adjustments.QueueWeight)          // 1x advancement
	require.Equal(t, uint32(100), adjustments.PricingMultiplierBps) // 1x pricing
	require.Equal(t, uint32(7000), adjustments.ReservedCapacityBps) // 70% capacity

	// Priority tier
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-pri", "priority", time.Hour))
	adjustments, err = keeper.ResolveAdjustments(sdkCtx, "policy-pri", reqLatency, defaultTTL)
	require.NoError(t, err)
	require.Equal(t, "priority", adjustments.TierName)
	require.Equal(t, uint32(200), adjustments.QueueWeight)          // 2x advancement
	require.Equal(t, uint32(150), adjustments.PricingMultiplierBps) // 1.5x pricing
	require.Equal(t, uint32(1000), adjustments.ReservedCapacityBps) // 10% capacity

	// Express tier
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-exp", "express", time.Hour))
	adjustments, err = keeper.ResolveAdjustments(sdkCtx, "policy-exp", reqLatency, defaultTTL)
	require.NoError(t, err)
	require.Equal(t, "express", adjustments.TierName)
	require.Equal(t, uint32(400), adjustments.QueueWeight)          // 4x advancement
	require.Equal(t, uint32(250), adjustments.PricingMultiplierBps) // 2.5x pricing
	require.Equal(t, uint32(2000), adjustments.ReservedCapacityBps) // 20% capacity

	// Enterprise tier
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-ent", "enterprise", time.Hour))
	adjustments, err = keeper.ResolveAdjustments(sdkCtx, "policy-ent", reqLatency, defaultTTL)
	require.NoError(t, err)
	require.Equal(t, "enterprise", adjustments.TierName)
	require.Equal(t, uint32(1000), adjustments.QueueWeight)       // 10x advancement
	require.Equal(t, uint32(0), adjustments.PricingMultiplierBps) // custom pricing
	require.Equal(t, uint32(0), adjustments.ReservedCapacityBps)  // dedicated pools
}

func TestAssignmentExpiry(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	require.NoError(t, keeper.AssignPolicyTier(sdkCtx, "policy-exp", "priority", time.Second))

	sdkCtx = sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(3 * time.Second))

	adj, err := keeper.ResolveAdjustments(sdkCtx, "policy-exp", 2_000, 40*time.Second)
	require.NoError(t, err)
	require.Equal(t, "standard", adj.TierName)
}

func TestAssignPolicyTierRequiresBlockTime(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	zeroTime := sdkCtx.WithBlockTime(time.Time{})

	err := keeper.AssignPolicyTier(zeroTime, "policy-x", "priority", time.Minute)
	require.Error(t, err)
}

func TestResolveAdjustmentsRequiresBlockTime(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)
	zeroTime := sdkCtx.WithBlockTime(time.Time{})

	_, err := keeper.ResolveAdjustments(zeroTime, "policy-x", 1_000, 20*time.Second)
	require.Error(t, err)
}

func setupKeeper(t *testing.T) (sdk.Context, *Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	memKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	cms.MountStoreWithDB(memKey, storetypes.StoreTypeMemory, nil)
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)

	keeper := NewKeeper(cdc, runtime.NewKVStoreService(storeKey), authtypes.NewModuleAddress("gov").String(), logger)
	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	sdkCtx := sdk.NewContext(cms, header, false, logger)
	return sdkCtx, keeper
}
