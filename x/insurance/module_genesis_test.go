
package insurance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type moduleTestBankKeeper struct{}

func (moduleTestBankKeeper) SendCoinsFromModuleToModule(
	context.Context,
	string,
	string,
	sdk.Coins,
) error {
	return nil
}

func (moduleTestBankKeeper) SendCoinsFromModuleToAccount(
	context.Context,
	string,
	sdk.AccAddress,
	sdk.Coins,
) error {
	return nil
}

func (moduleTestBankKeeper) BurnCoins(context.Context, string, sdk.Coins) error {
	return nil
}

func (moduleTestBankKeeper) GetBalance(context.Context, sdk.AccAddress, string) sdk.Coin {
	return sdk.Coin{}
}

func (moduleTestBankKeeper) GetAllBalances(context.Context, sdk.AccAddress) sdk.Coins {
	return nil
}

type moduleTestAccountKeeper struct{}

func (moduleTestAccountKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	return authtypes.NewModuleAddress(moduleName)
}

func (moduleTestAccountKeeper) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI {
	return nil
}

func setupInsuranceModuleTest(t *testing.T) (sdk.Context, AppModule, codec.Codec) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, logger)
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	types.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		moduleTestBankKeeper{},
		moduleTestAccountKeeper{},
		authtypes.NewModuleAddress("gov").String(),
	)

	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)

	return ctx, NewAppModule(k), cdc
}

func TestAppModuleInitGenesisRejectsOmittedParams(t *testing.T) {
	ctx, am, cdc := setupInsuranceModuleTest(t)
	badGenesis := json.RawMessage(`{}`)

	require.ErrorContains(t, AppModuleBasic{}.ValidateGenesis(cdc, nil, badGenesis), "params cannot be nil")
	require.PanicsWithError(t, "failed to validate insurance genesis: params cannot be nil", func() {
		am.InitGenesis(ctx, cdc, badGenesis)
	})
}

func TestAppModuleInitGenesisRejectsClaimTimestampOrder(t *testing.T) {
	ctx, am, cdc := setupInsuranceModuleTest(t)
	genesis := types.DefaultGenesis()
	genesis.Claims = []*types.Claim{
		{
			Id:        "claim-before-created",
			Status:    types.ClaimStatus_CLAIM_STATUS_PENDING,
			CreatedAt: timestamppb.New(time.Unix(1_700_000_100, 0).UTC()),
			UpdatedAt: timestamppb.New(time.Unix(1_700_000_000, 0).UTC()),
		},
	}

	badGenesis, err := json.Marshal(genesis)
	require.NoError(t, err)

	require.ErrorContains(
		t,
		AppModuleBasic{}.ValidateGenesis(cdc, nil, badGenesis),
		"claim claim-before-created updated_at cannot be before created_at",
	)
	require.PanicsWithError(
		t,
		"failed to validate insurance genesis: claim claim-before-created updated_at cannot be before created_at",
		func() {
			am.InitGenesis(ctx, cdc, badGenesis)
		},
	)
}

func TestAppModuleInitGenesisEmptyBytesUseDefaults(t *testing.T) {
	ctx, am, cdc := setupInsuranceModuleTest(t)

	require.NotPanics(t, func() {
		am.InitGenesis(ctx, cdc, nil)
	})

	params := am.keeper.GetParams(ctx)
	require.NotNil(t, params)
	require.Equal(t, types.DefaultParams().InsurancePoolBps, params.InsurancePoolBps)
}
