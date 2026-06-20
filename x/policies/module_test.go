//go:build cosmos

package policies

import (
	"testing"
	"time"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/protobuf/proto"

	"github.com/LumeraProtocol/lumera/x/policies/keeper"
	"github.com/LumeraProtocol/lumera/x/policies/types"
)

func TestAppModuleInitGenesisRejectsOmittedParams(t *testing.T) {
	ctx, cdc, module := setupAppModuleTest(t)
	bz := []byte(`{"policies":[]}`)

	require.Error(t, AppModuleBasic{}.ValidateGenesis(cdc, nil, bz))
	require.Panics(t, func() {
		module.InitGenesis(ctx, cdc, bz)
	})
}

func TestAppModuleInitGenesisEmptyBytesUseDefaults(t *testing.T) {
	ctx, cdc, module := setupAppModuleTest(t)

	require.NotPanics(t, func() {
		module.InitGenesis(ctx, cdc, nil)
	})

	params, err := module.keeper.GetParams(ctx)
	require.NoError(t, err)
	require.True(t, proto.Equal(types.DefaultParams(), params))
}

func setupAppModuleTest(t *testing.T) (sdk.Context, codec.JSONCodec, AppModule) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, logger)
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)

	policiesKeeper := keeper.NewKeeper(cdc, runtime.NewKVStoreService(storeKey), authtypes.NewModuleAddress("gov").String())
	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)

	return ctx, cdc, NewAppModule(policiesKeeper)
}
