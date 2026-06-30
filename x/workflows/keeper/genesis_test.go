package keeper

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

func workflowKeeperAddress(seed byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20)).String()
}

func workflowTestAuthorAddress() string {
	return workflowKeeperAddress(0x41)
}

func workflowTestOtherAuthorAddress() string {
	return workflowKeeperAddress(0x42)
}

func workflowTestPrefundAuthorAddress() string {
	return workflowKeeperAddress(0x43)
}

func workflowTestGovernanceAuthorAddress() string {
	return workflowKeeperAddress(0x44)
}

func TestWorkflowsModule_GenesisRoundtrip(t *testing.T) {
	ctx, k := setupKeeper(t)
	genesis := &types.GenesisState{
		Params: &types.Params{
			MinAuthorBondAmount:  "2500000",
			BondDenom:            "ulac",
			WastedWorkBPS:        750,
			MaxWorkflowVersions:  16,
			DisputeWindowSeconds: 3600,
		},
		Workflows: []*types.WorkflowRecord{
			{
				WorkflowID:    "11111111-1111-4111-8111-111111111111",
				Version:       "1.0.0",
				Status:        types.WorkflowStatusActive,
				AuthorAddress: workflowTestAuthorAddress(),
				Card: &types.WorkflowCard{
					WorkflowId:   "11111111-1111-4111-8111-111111111111",
					Version:      "1.0.0",
					DisplayName:  "Scaffold Workflow",
					AuthorId:     "author-1",
					AuthorPubkey: workflowAuthorPubkey(),
					Categories:   []string{"agent-contracts"},
					LicenseLane:  "byo_key",
					InputSchema:  `{"type":"object","properties":{"asset":{"type":"string"}}}`,
					OutputSchema: `{"type":"object"}`,
					Dag: []*types.Step{
						{
							StepId:                "step-a",
							ToolId:                "tool.step-a",
							ToolVersionConstraint: "1.0.0",
							InputBinding:          "$.inputs.asset",
							MaxSubCost:            sdk.NewCoin("ulac", sdkmath.NewInt(1)),
							SubSloP95Ms:           1000,
							RetryPolicy:           &types.RetryPolicy{MaxAttempts: 1},
							FailureAction:         types.FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
							SideEffect:            types.SideEffect_SIDE_EFFECT_REVERSIBLE,
						},
					},
					PassportRequirements: keeperGenesisPassportRequirements(),
					Pricing:              keeperGenesisWorkflowPricing(),
					Governance:           keeperGenesisWorkflowGovernance(),
					SafetyInvariants:     []*types.SafetyInvariant{workflowTestSafetyInvariant()},
				},
				CreatedHeight: 7,
				UpdatedHeight: 7,
			},
		},
		AuthorBonds: []*types.AuthorBondRecord{
			{
				AuthorAddress: workflowTestAuthorAddress(),
				Bond:          &sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(2500000)},
				LockedFor:     []string{"11111111-1111-4111-8111-111111111111/1.0.0"},
				UpdatedHeight: 7,
			},
		},
		BundleQuotes: []*types.BundleQuoteRecord{},
	}

	require.NoError(t, k.InitGenesis(ctx, genesis))
	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, genesis, exported)

	ctx2, k2 := setupKeeper(t)
	require.NoError(t, k2.InitGenesis(ctx2, exported))
	exportedAgain, err := k2.ExportGenesis(ctx2)
	require.NoError(t, err)
	require.Equal(t, exported, exportedAgain)
}

func TestWorkflowsModule_InitGenesisRejectsMalformedWorkflowCard(t *testing.T) {
	ctx, k := setupKeeper(t)
	genesis := types.DefaultGenesis()
	genesis.Workflows = []*types.WorkflowRecord{
		{
			WorkflowID:    "11111111-1111-4111-8111-111111111111",
			Version:       "1.0.0",
			Status:        types.WorkflowStatusActive,
			AuthorAddress: workflowTestAuthorAddress(),
			Card: &types.WorkflowCard{
				WorkflowId:   "11111111-1111-4111-8111-111111111111",
				Version:      "1.0.0",
				DisplayName:  "Malformed Workflow",
				AuthorId:     "author-1",
				AuthorPubkey: workflowAuthorPubkey(),
				Categories:   []string{"agent-contracts"},
			},
		},
	}

	err := k.InitGenesis(ctx, genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, types.WorkflowStaticReasonDAGEmpty)
}

// NOTE: TestWorkflowsModule_InitGenesisRejectsInvalidWorkflowCardTimestamp was
// removed after the gogoproto migration: WorkflowCard.created_at is now a value
// time.Time (stdtime) which has no out-of-range/invalid state to reject.

func TestWorkflowsModule_InitGenesisRejectsUncanonicalWorkflowStatus(t *testing.T) {
	ctx, k := setupKeeper(t)
	workflowID := "11111111-1111-4111-8111-111111111111"
	version := "1.0.0"
	genesis := types.DefaultGenesis()
	genesis.Workflows = []*types.WorkflowRecord{
		{
			WorkflowID:    workflowID,
			Version:       version,
			Status:        " active ",
			AuthorAddress: workflowTestAuthorAddress(),
			Card:          keeperGenesisWorkflowCard(workflowID, version),
		},
	}

	err := k.InitGenesis(ctx, genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "status must be canonical")

	_, found, err := k.GetWorkflow(ctx, workflowID, version)
	require.NoError(t, err)
	require.False(t, found)
}

func TestWorkflowsModule_InitGenesisRejectsDanglingAuthorBondLock(t *testing.T) {
	ctx, k := setupKeeper(t)
	genesis := types.DefaultGenesis()
	genesis.AuthorBonds = []*types.AuthorBondRecord{
		{
			AuthorAddress: workflowTestAuthorAddress(),
			Bond:          &sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(2500000)},
			LockedFor:     []string{"ghost-workflow/1.0.0"},
		},
	}

	err := k.InitGenesis(ctx, genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "locked_for references unknown workflow")
}

func TestWorkflowsModule_InitGenesisRejectsMismatchedAuthorBondLock(t *testing.T) {
	ctx, k := setupKeeper(t)
	workflowID := "11111111-1111-4111-8111-111111111111"
	version := "1.0.0"
	genesis := types.DefaultGenesis()
	genesis.Workflows = []*types.WorkflowRecord{
		{
			WorkflowID:    workflowID,
			Version:       version,
			Status:        types.WorkflowStatusActive,
			AuthorAddress: workflowTestAuthorAddress(),
			Card:          keeperGenesisWorkflowCard(workflowID, version),
		},
	}
	genesis.AuthorBonds = []*types.AuthorBondRecord{
		{
			AuthorAddress: workflowTestOtherAuthorAddress(),
			Bond:          &sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(2500000)},
			LockedFor:     []string{workflowID + "/" + version},
		},
	}

	err := k.InitGenesis(ctx, genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "owned by "+workflowTestAuthorAddress())

	_, found, err := k.GetWorkflow(ctx, workflowID, version)
	require.NoError(t, err)
	require.False(t, found)
}

func TestWorkflowsModule_ParamHooks(t *testing.T) {
	ctx, k := setupKeeper(t)
	params := types.DefaultParams()
	params.MinAuthorBondAmount = "3333333"
	params.WastedWorkBPS = 2500

	require.NoError(t, k.SetParams(ctx, params))
	got, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, "3333333", got.MinAuthorBondAmount)
	require.Equal(t, uint32(2500), got.WastedWorkBPS)

	events := ctx.EventManager().Events()
	require.NotEmpty(t, events)
	require.Equal(t, types.EventTypeParamsUpdated, events[len(events)-1].Type)
}

func TestWorkflowsModule_LifecycleEvents(t *testing.T) {
	ctx, k := setupKeeper(t)
	k.EmitLifecycleEvent(ctx, "begin_block")
	k.EmitLifecycleEvent(ctx, "end_block")

	events := ctx.EventManager().Events()
	require.Len(t, events, 2)
	require.Equal(t, types.EventTypeLifecycle, events[0].Type)
	require.Equal(t, types.EventTypeLifecycle, events[1].Type)
}

func setupKeeper(t *testing.T) (sdk.Context, *Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)
	keeper := NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		authtypes.NewModuleAddress("gov").String(),
		logger,
	)
	header := tmproto.Header{Height: 1, ChainID: "lumera-workflows-test", Time: time.Unix(1_700_000_000, 0).UTC()}
	return sdk.NewContext(cms, header, false, logger), keeper
}

func keeperGenesisWorkflowCard(workflowID, version string) *types.WorkflowCard {
	return &types.WorkflowCard{
		WorkflowId:   workflowID,
		Version:      version,
		DisplayName:  "Scaffold Workflow",
		AuthorId:     "author-1",
		AuthorPubkey: workflowAuthorPubkey(),
		Categories:   []string{"agent-contracts"},
		LicenseLane:  "byo_key",
		InputSchema:  `{"type":"object","properties":{"asset":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		Dag: []*types.Step{
			{
				StepId:                "step-a",
				ToolId:                "tool.step-a",
				ToolVersionConstraint: "1.0.0",
				InputBinding:          "$.inputs.asset",
				MaxSubCost:            sdk.NewCoin("ulac", sdkmath.NewInt(1)),
				SubSloP95Ms:           1000,
				RetryPolicy:           &types.RetryPolicy{MaxAttempts: 1},
				FailureAction:         types.FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
				SideEffect:            types.SideEffect_SIDE_EFFECT_REVERSIBLE,
			},
		},
		PassportRequirements: keeperGenesisPassportRequirements(),
		Pricing:              keeperGenesisWorkflowPricing(),
		Governance:           keeperGenesisWorkflowGovernance(),
		SafetyInvariants:     []*types.SafetyInvariant{workflowTestSafetyInvariant()},
	}
}

func keeperGenesisPassportRequirements() *types.PassportRequirements {
	return &types.PassportRequirements{
		MinTier: types.PassportTier_PASSPORT_TIER_BASIC,
	}
}

func keeperGenesisWorkflowGovernance() *types.Governance {
	return &types.Governance{
		AuthorAddresses: []string{workflowTestGovernanceAuthorAddress()},
		UpgradePolicy:   types.UpgradePolicy_UPGRADE_POLICY_SEMVER_COMPATIBLE,
	}
}

func keeperGenesisWorkflowPricing() *types.WorkflowPricing {
	return &types.WorkflowPricing{
		PricingModel: "sum_steps_plus_margin",
		MinBond:      sdk.NewCoin("ulac", sdkmath.NewInt(1000000)),
		// The optional minimum/maximum cost coins are constructed in their
		// canonical post-JSON form (empty denom, zero amount) so the
		// InitGenesis -> ExportGenesis round-trip compares equal. A bare
		// sdk.Coin{} carries a nil math.Int, which the JSON value codec
		// normalizes to "0" on the way back out.
		MinimumCost: sdk.Coin{Amount: sdkmath.ZeroInt()},
		MaximumCost: sdk.Coin{Amount: sdkmath.ZeroInt()},
	}
}
