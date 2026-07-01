package workflows

import (
	"encoding/json"
	"testing"
	"time"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/LumeraProtocol/lumera/x/workflows/keeper"
	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

func TestAppModuleInitGenesisRejectsOmittedParams(t *testing.T) {
	bz, err := json.Marshal(types.GenesisState{Workflows: []*types.WorkflowRecord{}})
	require.NoError(t, err)

	require.Error(t, AppModuleBasic{}.ValidateGenesis(nil, nil, bz))

	ctx, module := setupAppModuleTest(t)
	require.Panics(t, func() {
		module.InitGenesis(ctx, nil, bz)
	})
}

func TestAppModuleInitGenesisEmptyBytesUseDefaults(t *testing.T) {
	ctx, module := setupAppModuleTest(t)

	require.NotPanics(t, func() {
		module.InitGenesis(ctx, nil, nil)
	})

	params, err := module.keeper.GetParams(ctx)
	require.NoError(t, err)
	require.NotNil(t, params)
	require.Equal(t, types.DefaultParams(), params)
}

func setupAppModuleTest(t *testing.T) (sdk.Context, AppModule) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)
	workflowsKeeper := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		authtypes.NewModuleAddress("gov").String(),
		logger,
	)
	header := tmproto.Header{Height: 1, ChainID: "lumera-workflows-test", Time: time.Unix(1_700_000_000, 0).UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)

	return ctx, NewAppModule(workflowsKeeper)
}
