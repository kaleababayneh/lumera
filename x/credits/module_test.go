package credits

import (
	"context"
	"testing"
	"time"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/protobuf/proto"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestAppModuleInitGenesisRejectsOmittedParams(t *testing.T) {
	ctx, cdc, module := setupAppModuleTest(t)
	bz := []byte(`{"locks":[]}`)

	err := AppModuleBasic{}.ValidateGenesis(cdc, nil, bz)
	require.Error(t, err)
	require.Contains(t, err.Error(), "params must be provided")
	require.Panics(t, func() {
		module.InitGenesis(ctx, cdc, bz)
	})
}

func TestAppModuleInitGenesisEmptyBytesUseDefaults(t *testing.T) {
	ctx, cdc, module := setupAppModuleTest(t)

	require.NotPanics(t, func() {
		module.InitGenesis(ctx, cdc, nil)
	})

	params := module.keeper.GetParams(ctx)
	require.NotNil(t, params)
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

	creditsKeeper := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		noopBankKeeper{},
		moduleAccountKeeper{moduleAddr: authtypes.NewModuleAddress(types.ModuleAccountName)},
		nil,
		nil,
		nil,
		nil,
		authtypes.NewModuleAddress("gov").String(),
	)
	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)

	return ctx, cdc, NewAppModule(&creditsKeeper)
}

type noopBankKeeper struct{}

func (noopBankKeeper) SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}

func (noopBankKeeper) SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error {
	return nil
}

func (noopBankKeeper) BurnCoins(context.Context, string, sdk.Coins) error { return nil }

func (noopBankKeeper) MintCoins(context.Context, string, sdk.Coins) error { return nil }

func (noopBankKeeper) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	if denom == "" {
		denom = types.DefaultCreditDenom
	}
	return sdk.NewInt64Coin(denom, 0)
}

func (noopBankKeeper) GetSupply(_ context.Context, denom string) sdk.Coin {
	if denom == "" {
		denom = types.DefaultCreditDenom
	}
	return sdk.NewInt64Coin(denom, 0)
}

type moduleAccountKeeper struct {
	moduleAddr sdk.AccAddress
}

func (m moduleAccountKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	if moduleName == types.ModuleAccountName {
		return m.moduleAddr
	}
	return nil
}

func (moduleAccountKeeper) IterateAccounts(context.Context, func(sdk.AccountI) bool) {}
