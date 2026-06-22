
package keeper_test

import (
	"testing"
	"time"

	coreaddress "cosmossdk.io/core/address"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx           sdk.Context
	keeper        keeper.Keeper
	bankKeeper    bankkeeper.Keeper
	accountKeeper authkeeper.AccountKeeper
	cdc           codec.BinaryCodec
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) SetupTest() {
	fixture := newKeeperFixture(suite.T())
	suite.ctx = fixture.ctx
	suite.keeper = fixture.keeper
	suite.bankKeeper = fixture.bankKeeper
	suite.accountKeeper = fixture.accountKeeper
	suite.cdc = fixture.cdc
}

type keeperFixture struct {
	ctx           sdk.Context
	keeper        keeper.Keeper
	bankKeeper    bankkeeper.Keeper
	accountKeeper authkeeper.AccountKeeper
	cdc           codec.BinaryCodec
}

type testAddressCodec struct{}

var _ coreaddress.Codec = (*testAddressCodec)(nil)

func (testAddressCodec) StringToBytes(text string) ([]byte, error) {
	addr, err := sdk.AccAddressFromBech32(text)
	if err != nil {
		return nil, err
	}
	return addr.Bytes(), nil
}

func (testAddressCodec) BytesToString(bz []byte) (string, error) {
	return sdk.AccAddress(bz).String(), nil
}

func newKeeperFixture(t testing.TB) *keeperFixture {
	t.Helper()

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())

	insuranceKey := storetypes.NewKVStoreKey(types.StoreKey)
	authKey := storetypes.NewKVStoreKey(authtypes.StoreKey)
	bankKey := storetypes.NewKVStoreKey(banktypes.StoreKey)

	stateStore.MountStoreWithDB(insuranceKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(authKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(bankKey, storetypes.StoreTypeIAVL, db)

	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	authtypes.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	types.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	ctx := sdk.NewContext(stateStore, cmtproto.Header{
		Height:  1,
		Time:    time.Date(2025, 9, 30, 12, 0, 0, 0, time.UTC),
		AppHash: nil,
	}, false, log.NewNopLogger()).WithEventManager(sdk.NewEventManager())

	maccPerms := map[string][]string{
		types.ModuleAccountName: nil,
		creditstypes.ModuleName: {authtypes.Minter, authtypes.Burner},
	}

	bech32Prefix := sdk.GetConfig().GetBech32AccountAddrPrefix()
	if bech32Prefix == "" {
		bech32Prefix = "cosmos"
	}

	addrCodec := &testAddressCodec{}

	accountKeeper := authkeeper.NewAccountKeeper(
		cdc,
		runtime.NewKVStoreService(authKey),
		authtypes.ProtoBaseAccount,
		maccPerms,
		addrCodec,
		bech32Prefix,
		authtypes.NewModuleAddress("gov").String(),
	)

	blockedAddrs := make(map[string]bool)
	for name := range maccPerms {
		blockedAddrs[authtypes.NewModuleAddress(name).String()] = true
	}

	baseBankKeeper := bankkeeper.NewBaseKeeper(
		cdc,
		runtime.NewKVStoreService(bankKey),
		accountKeeper,
		blockedAddrs,
		authtypes.NewModuleAddress("gov").String(),
		log.NewNopLogger(),
	)
	insKeeper := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(insuranceKey),
		&baseBankKeeper,
		accountKeeper,
		authtypes.NewModuleAddress("gov").String(),
	)
	insKeeper.InitGenesis(ctx, types.DefaultGenesis())

	return &keeperFixture{
		ctx:           ctx,
		keeper:        insKeeper,
		bankKeeper:    &baseBankKeeper,
		accountKeeper: accountKeeper,
		cdc:           cdc,
	}
}

func setupKeeperTest(t *testing.T) *keeperFixture {
	return newKeeperFixture(t)
}

func (suite *KeeperTestSuite) TestNewKeeper() {
	require.NotNil(suite.T(), suite.keeper)
	require.Equal(suite.T(), authtypes.NewModuleAddress("gov").String(), suite.keeper.Authority())
}

func (suite *KeeperTestSuite) TestInitGenesis() {
	genesis := types.DefaultGenesis()
	suite.keeper.InitGenesis(suite.ctx, genesis)

	params := suite.keeper.GetParams(suite.ctx)
	require.NotNil(suite.T(), params)
}

func (suite *KeeperTestSuite) TestInitGenesisRejectsUnbalancedPoolState() {
	genesis := types.DefaultGenesis()
	genesis.Pool.TotalFunds = "100"
	genesis.Pool.AvailableFunds = "80"
	genesis.Pool.ReservedFunds = "30"

	require.PanicsWithError(
		suite.T(),
		"failed to validate insurance genesis: pool total funds must equal available funds plus reserved funds",
		func() {
			suite.keeper.InitGenesis(suite.ctx, genesis)
		},
	)

	exported := suite.keeper.ExportGenesis(suite.ctx)
	require.NotNil(suite.T(), exported.Pool)
	require.Equal(suite.T(), "0", exported.Pool.TotalFunds)
	require.Equal(suite.T(), "0", exported.Pool.AvailableFunds)
	require.Equal(suite.T(), "0", exported.Pool.ReservedFunds)
}

func (suite *KeeperTestSuite) TestExportGenesis() {
	genesis := suite.keeper.ExportGenesis(suite.ctx)
	require.NotNil(suite.T(), genesis)
	require.NotNil(suite.T(), genesis.Params)
}

func (suite *KeeperTestSuite) TestContributeToPool() {
	// Ensure module accounts exist and fund credits account
	creditsModuleAccount := suite.accountKeeper.GetModuleAccount(suite.ctx, creditstypes.ModuleName)
	require.NotNil(suite.T(), creditsModuleAccount)
	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1000000))
	require.NoError(suite.T(), suite.bankKeeper.MintCoins(suite.ctx, creditstypes.ModuleName, initialCoins))

	insuranceModuleAccount := suite.accountKeeper.GetModuleAccount(suite.ctx, types.ModuleAccountName)
	require.NotNil(suite.T(), insuranceModuleAccount)

	// Test contribution
	contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	err := suite.keeper.ContributeToPool(suite.ctx, "test-receipt-1", "tool-1", "publisher-1", "v1", "", contribution)
	require.NoError(suite.T(), err)

	// Verify pool balance increased
	poolBalance, err := suite.keeper.GetPoolBalance(suite.ctx)
	require.NoError(suite.T(), err)
	require.True(suite.T(), poolBalance.AmountOf("ulac").Equal(sdkmath.NewInt(100)))
}

func (suite *KeeperTestSuite) TestContributeToPoolRejectsNonCanonicalIdentifiers() {
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, creditstypes.ModuleName))
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, types.ModuleAccountName))
	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1000))
	require.NoError(suite.T(), suite.bankKeeper.MintCoins(suite.ctx, creditstypes.ModuleName, initialCoins))

	tests := []struct {
		name        string
		receiptID   string
		toolID      string
		publisherID string
		want        string
	}{
		{
			name:        "padded receipt id",
			receiptID:   " receipt-direct ",
			toolID:      "tool-1",
			publisherID: "publisher-1",
			want:        "receipt_id must be canonical",
		},
		{
			name:        "padded tool id",
			receiptID:   "receipt-direct-tool",
			toolID:      "\ttool-1",
			publisherID: "publisher-1",
			want:        "tool_id must be canonical",
		},
		{
			name:        "padded publisher id",
			receiptID:   "receipt-direct-publisher",
			toolID:      "tool-1",
			publisherID: "publisher-1 ",
			want:        "publisher_id must be canonical",
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			err := suite.keeper.ContributeToPool(
				suite.ctx,
				tt.receiptID,
				tt.toolID,
				tt.publisherID,
				"policy@1",
				"",
				sdk.NewCoins(sdk.NewInt64Coin("ulac", 1)),
			)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func (suite *KeeperTestSuite) TestCreditWorkflowWastedFeeToPool() {
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, types.ModuleAccountName))
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, creditstypes.ModuleName))
	amount := sdk.NewCoins(sdk.NewInt64Coin("ulac", 3))

	err := suite.keeper.CreditWorkflowWastedFeeToPool(
		suite.ctx,
		"bundle-wasted-fee",
		"wf-failure",
		"lumera1author",
		"policy@1",
		amount,
	)
	require.NoError(suite.T(), err)

	genesis := suite.keeper.ExportGenesis(suite.ctx)
	require.NotNil(suite.T(), genesis.Pool)
	require.Equal(suite.T(), "3", genesis.Pool.TotalFunds)
	require.Equal(suite.T(), "3", genesis.Pool.AvailableFunds)
	require.Equal(suite.T(), "3", genesis.Pool.TotalContributions)
	require.Len(suite.T(), genesis.Contributions, 1)
	require.Equal(suite.T(), "bundle-wasted-fee", genesis.Contributions[0].ReceiptId)
	require.Equal(suite.T(), "wf-failure", genesis.Contributions[0].ToolId)
	require.Equal(suite.T(), "lumera1author", genesis.Contributions[0].PublisherId)
	require.Equal(suite.T(), "policy@1", genesis.Contributions[0].PolicyVersion)
	require.Equal(suite.T(), "3", genesis.Contributions[0].Amount.Amount.String())

	balance, err := suite.keeper.GetPoolBalance(suite.ctx)
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), sdkmath.NewInt(3), balance.AmountOf("ulac"))
}

func (suite *KeeperTestSuite) TestCreditWorkflowWastedFeeToPoolRejectsNonCanonicalIdentifiers() {
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, types.ModuleAccountName))
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, creditstypes.ModuleName))
	amount := sdk.NewCoins(sdk.NewInt64Coin("ulac", 3))

	tests := []struct {
		name       string
		receiptID  string
		workflowID string
		authorID   string
		want       string
	}{
		{
			name:       "padded receipt id",
			receiptID:  " bundle-wasted-fee ",
			workflowID: "wf-failure",
			authorID:   "lumera1author",
			want:       "receipt_id must be canonical",
		},
		{
			name:       "padded workflow id",
			receiptID:  "bundle-wasted-fee-workflow",
			workflowID: "wf-failure ",
			authorID:   "lumera1author",
			want:       "workflow_id must be canonical",
		},
		{
			name:       "padded author id",
			receiptID:  "bundle-wasted-fee-author",
			workflowID: "wf-failure",
			authorID:   "\tlumera1author",
			want:       "author_id must be canonical",
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			err := suite.keeper.CreditWorkflowWastedFeeToPool(
				suite.ctx,
				tt.receiptID,
				tt.workflowID,
				tt.authorID,
				"policy@1",
				amount,
			)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func (suite *KeeperTestSuite) TestCreditWorkflowWastedFeeToPoolRejectsDuplicateReceipt() {
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, types.ModuleAccountName))
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, creditstypes.ModuleName))
	amount := sdk.NewCoins(sdk.NewInt64Coin("ulac", 3))
	require.NoError(suite.T(), suite.keeper.CreditWorkflowWastedFeeToPool(
		suite.ctx,
		"bundle-duplicate-wasted-fee",
		"wf-failure",
		"lumera1author",
		"policy@1",
		amount,
	))

	err := suite.keeper.CreditWorkflowWastedFeeToPool(
		suite.ctx,
		"bundle-duplicate-wasted-fee",
		"wf-failure",
		"lumera1author",
		"policy@1",
		amount,
	)
	require.Error(suite.T(), err)
	require.Contains(suite.T(), err.Error(), "contribution already recorded")
}

func (suite *KeeperTestSuite) TestCreditWorkflowWastedFeeToPoolFundsClaimsPayout() {
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, types.ModuleAccountName))
	require.NotNil(suite.T(), suite.accountKeeper.GetModuleAccount(suite.ctx, creditstypes.ModuleName))
	amount := sdk.NewCoins(sdk.NewInt64Coin("ulac", 3))
	receiptID := "bundle-wasted-fee-payout"
	require.NoError(suite.T(), suite.keeper.CreditWorkflowWastedFeeToPool(
		suite.ctx,
		receiptID,
		"wf-failure",
		"lumera1author",
		"policy@1",
		amount,
	))

	insuranceAddr := suite.accountKeeper.GetModuleAddress(types.ModuleAccountName)
	require.NotNil(suite.T(), insuranceAddr)
	require.Equal(suite.T(), sdkmath.NewInt(3), suite.bankKeeper.GetBalance(suite.ctx, insuranceAddr, "ulac").Amount)

	params := types.DefaultParams()
	params.MaxClaimPercent = "1.0"
	require.NoError(suite.T(), suite.keeper.SetParams(suite.ctx, params))

	claimant := authtypes.NewModuleAddress("workflow-claimant")
	claimID, err := suite.keeper.FileClaim(suite.ctx, &types.MsgFileClaim{
		Claimant:    claimant.String(),
		ReceiptId:   receiptID,
		ToolId:      "wf-failure",
		PublisherId: "lumera1author",
		ClaimedAmount: sdk.Coin{
			Denom:  "ulac",
			Amount: sdkmath.NewInt(2),
		},
		Reason: "workflow wasted-work fee refund",
	})
	require.NoError(suite.T(), err)
	require.NotEmpty(suite.T(), claimID)

	authority := authtypes.NewModuleAddress("gov").String()
	require.NoError(suite.T(), suite.keeper.ProcessClaim(suite.ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "approve",
		ApprovedAmount: sdk.Coin{
			Denom:  "ulac",
			Amount: sdkmath.NewInt(2),
		},
	}))

	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	_, err = msgServer.ProcessPayout(suite.ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID,
		Recipient: claimant.String(),
	})
	require.NoError(suite.T(), err)

	require.Equal(suite.T(), sdkmath.NewInt(2), suite.bankKeeper.GetBalance(suite.ctx, claimant, "ulac").Amount)
	require.Equal(suite.T(), sdkmath.NewInt(1), suite.bankKeeper.GetBalance(suite.ctx, insuranceAddr, "ulac").Amount)

	genesis := suite.keeper.ExportGenesis(suite.ctx)
	require.NotNil(suite.T(), genesis.Pool)
	require.Equal(suite.T(), "1", genesis.Pool.TotalFunds)
	require.Equal(suite.T(), "1", genesis.Pool.AvailableFunds)
	require.Equal(suite.T(), "0", genesis.Pool.ReservedFunds)
	require.Equal(suite.T(), "2", genesis.Pool.TotalPayouts)
}

func (suite *KeeperTestSuite) TestGetPoolBalance() {
	// Ensure insurance module account exists
	insuranceModuleAccount := suite.accountKeeper.GetModuleAccount(suite.ctx, types.ModuleAccountName)
	require.NotNil(suite.T(), insuranceModuleAccount)

	// Initially should be zero
	balance, err := suite.keeper.GetPoolBalance(suite.ctx)
	require.NoError(suite.T(), err)
	require.True(suite.T(), balance.IsZero())
}

func (suite *KeeperTestSuite) TestGetSetParams() {
	params := types.DefaultParams()
	err := suite.keeper.SetParams(suite.ctx, params)
	require.NoError(suite.T(), err)

	retrieved := suite.keeper.GetParams(suite.ctx)
	require.Equal(suite.T(), params.MinPoolBalance, retrieved.MinPoolBalance)
}

func (suite *KeeperTestSuite) TestValidateAuthority() {
	govAddr := authtypes.NewModuleAddress("gov").String()

	// Valid authority
	err := suite.keeper.ValidateAuthority(govAddr)
	require.NoError(suite.T(), err)

	// Invalid authority
	err = suite.keeper.ValidateAuthority("invalid-address")
	require.Error(suite.T(), err)
	require.Contains(suite.T(), err.Error(), "unauthorized")
}

func (suite *KeeperTestSuite) TestContributeToPool_ZeroAmount() {
	err := suite.keeper.ContributeToPool(suite.ctx, "test-receipt", "", "", "", "", sdk.NewCoins())
	require.NoError(suite.T(), err) // Should succeed but do nothing
}

func (suite *KeeperTestSuite) TestContributeToPool_ModuleAccountNotFound() {
	// Create and fund credits module account but intentionally omit insurance module account
	creditsModuleAccount := suite.accountKeeper.GetModuleAccount(suite.ctx, creditstypes.ModuleName)
	require.NotNil(suite.T(), creditsModuleAccount)
	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1000))
	require.NoError(suite.T(), suite.bankKeeper.MintCoins(suite.ctx, creditstypes.ModuleName, initialCoins))

	contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	err := suite.keeper.ContributeToPool(suite.ctx, "test-receipt", "tool-1", "publisher-1", "v1", "", contribution)
	require.Error(suite.T(), err)
	require.Contains(suite.T(), err.Error(), "module account not found")
}

func TestInvariantsRegistration(t *testing.T) {
	t.Helper()
	// This test verifies that RegisterInvariants doesn't panic
	keeper.RegisterInvariants(nil, keeper.Keeper{})
	// If we get here without panicking, test passes
}
