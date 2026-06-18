//go:build cosmos && cosmos_full

package simulation_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/LumeraProtocol/lumera/x/credits"
	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	credtypes "github.com/LumeraProtocol/lumera/x/credits/types"
	reservetypes "github.com/LumeraProtocol/lumera/x/reserve/types"
)

func TestBeginBlockerRespectsIterationLimits(t *testing.T) {
	ctx, k, bank, moduleAddr, accounts := setupCreditsSimulationKeeper(t)

	params := k.GetParams(ctx)
	params.MaxSettlementsPerBlock = 5
	params.MaxExpiredLocksPerBlock = 3
	params.MaxPrunedSettlementsPerBlock = 10
	require.NoError(t, k.SetParams(ctx, params))

	router := newAccAddress()
	publisher := newAccAddress()
	referrer := newAccAddress()
	accounts.register(router)
	accounts.register(publisher)
	accounts.register(referrer)

	bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(credtypes.DefaultCreditDenom, 100_000_000)))

	costCoin := sdk.NewInt64Coin(credtypes.DefaultCreditDenom, 10_000)
	lockAmount := sdk.NewInt64Coin(credtypes.DefaultCreditDenom, 1_000_000)
	total := 12
	lockIDs := make([]string, 0, total)
	settlementIDs := make([]string, 0, total)

	for i := 0; i < total; i++ {
		lockID, err := k.LockCredits(
			ctx,
			router.String(),
			fmt.Sprintf("session-%d", i),
			lockAmount,
			"tool-alpha",
			fmt.Sprintf("quote-%d", i),
			"policy@1",
			fmt.Sprintf("intent-%d", i),
		)
		require.NoError(t, err)
		lockIDs = append(lockIDs, lockID)

		settlementID := fmt.Sprintf("settlement-%d", i)
		settlement := &credtypes.SettlementRecord{
			Id:           settlementID,
			LockId:       lockID,
			ToolId:       "tool-alpha",
			PublisherId:  publisher.String(),
			UserId:       router.String(),
			RouterId:     router.String(),
			ReferrerId:   referrer.String(),
			CacheHit:     false,
			OriginToolId: "",
			Status:       credtypes.SettlementStatus_SETTLEMENT_STATUS_PENDING,
			Timestamp:    timestamppb.New(ctx.BlockTime().Add(-48 * time.Hour)),
		}
		settlement.SetTotalCostCoins(sdk.NewCoins(costCoin))
		require.NoError(t, k.CreateSettlement(ctx, settlement))
		settlementIDs = append(settlementIDs, settlementID)
	}

	// Advance block time so all locks are expired (expiry index is created on LockCredits).
	expireAfter := time.Duration(params.MaxLockTtlSeconds)*time.Second + time.Second
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(expireAfter))
	ctx = ctx.WithGasMeter(storetypes.NewGasMeter(5_000_000))

	require.NoError(t, credits.BeginBlocker(ctx, k))

	completed := 0
	for _, settlementID := range settlementIDs {
		settlement, found := k.GetSettlement(ctx, settlementID)
		require.True(t, found)
		if settlement.Status == credtypes.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
			completed++
		}
	}
	require.Equal(t, params.MaxSettlementsPerBlock, uint32(completed), "processed settlements exceed limit")

	released := 0
	for _, lockID := range lockIDs {
		lock, found := k.GetLock(ctx, lockID)
		require.True(t, found)
		if lock.Status == credtypes.LockStatus_LOCK_STATUS_RELEASED {
			released++
		}
	}
	require.Equal(t, int(params.MaxExpiredLocksPerBlock), released, "expired lock processing exceeded limit")

	moduleBalance := bank.Balance(moduleAddr)
	require.True(t, moduleBalance.AmountOf(credtypes.DefaultCreditDenom).IsPositive(), "module balance should retain unprocessed locks")
}

func TestBeginBlockerMissingTimestampFailsSettlement(t *testing.T) {
	ctx, k, _, _, accounts := setupCreditsSimulationKeeper(t)

	params := k.GetParams(ctx)
	params.MaxSettlementsPerBlock = 1
	params.MaxExpiredLocksPerBlock = 1
	params.MaxPrunedSettlementsPerBlock = 1
	require.NoError(t, k.SetParams(ctx, params))

	router := newAccAddress()
	publisher := newAccAddress()
	accounts.register(router)
	accounts.register(publisher)

	settlement := &credtypes.SettlementRecord{
		Id:          "settlement-missing-ts",
		ToolId:      "tool-alpha",
		PublisherId: publisher.String(),
		UserId:      router.String(),
		RouterId:    router.String(),
		Status:      credtypes.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}
	settlement.SetTotalCostCoins(sdk.NewCoins(sdk.NewInt64Coin(credtypes.DefaultCreditDenom, 1)))
	require.NoError(t, k.CreateSettlement(ctx, settlement))

	ctx = ctx.WithGasMeter(storetypes.NewGasMeter(5_000_000))
	require.NoError(t, credits.BeginBlocker(ctx, k))

	updated, found := k.GetSettlement(ctx, settlement.Id)
	require.True(t, found)
	require.Equal(t, credtypes.SettlementStatus_SETTLEMENT_STATUS_FAILED, updated.Status)
	require.Equal(t, "missing settlement timestamp", updated.FailureReason)
	require.NotNil(t, updated.CompletedAt)
}

func setupCreditsSimulationKeeper(t *testing.T) (sdk.Context, *keeper.Keeper, *mockBankKeeper, sdk.AccAddress, *mockAccountKeeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(credtypes.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, logger)
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	moduleAddr := authtypes.NewModuleAddress(credtypes.ModuleAccountName)
	bankKeeper := newMockBankKeeper(credtypes.ModuleAccountName, moduleAddr)
	accountKeeper := &mockAccountKeeper{
		moduleAddr: moduleAddr,
		accounts:   make(map[string]sdk.AccAddress),
	}

	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		bankKeeper,
		accountKeeper,
		nil,
		nil,
		stubReserveKeeper{},
		nil,
		authtypes.NewModuleAddress("gov").String(),
	)

	header := tmproto.Header{Time: time.Now().UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)
	require.NoError(t, k.SetParams(ctx, credtypes.DefaultParams()))

	return ctx, &k, bankKeeper, moduleAddr, accountKeeper
}

func newAccAddress() sdk.AccAddress {
	priv := secp256k1.GenPrivKey()
	return sdk.AccAddress(priv.PubKey().Address())
}

type mockAccountKeeper struct {
	moduleAddr sdk.AccAddress
	accounts   map[string]sdk.AccAddress
}

func (m *mockAccountKeeper) register(addr sdk.AccAddress) {
	m.accounts[addr.String()] = addr
}

func (m mockAccountKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	if moduleName == credtypes.ModuleAccountName {
		return m.moduleAddr
	}
	return nil
}

func (m mockAccountKeeper) IterateAccounts(_ context.Context, cb func(account sdk.AccountI) bool) {
	for _, addr := range m.accounts {
		acc := &authtypes.BaseAccount{Address: addr.String()}
		if cb(acc) {
			return
		}
	}
	if m.moduleAddr != nil {
		moduleAcc := &authtypes.BaseAccount{Address: m.moduleAddr.String()}
		cb(moduleAcc)
	}
}

type mockBankKeeper struct {
	moduleName string
	moduleAddr sdk.AccAddress
	balances   map[string]sdk.Coins
}

func newMockBankKeeper(moduleName string, moduleAddr sdk.AccAddress) *mockBankKeeper {
	return &mockBankKeeper{
		moduleName: moduleName,
		moduleAddr: moduleAddr,
		balances:   make(map[string]sdk.Coins),
	}
}

type stubReserveKeeper struct{}

func (stubReserveKeeper) AllocateReserve(_ context.Context, _ string, _ string, _ string, amount sdk.Coin) (reservetypes.ReserveAllocation, error) {
	return reservetypes.ReserveAllocation{Applied: false, DiscountedPrice: amount}, nil
}

func (stubReserveKeeper) ReleaseExpired(_ context.Context) error { return nil }

func (stubReserveKeeper) CreateCommitment(_ context.Context, _ reservetypes.ReserveRequest) (*reservetypes.ReserveCommitment, error) {
	return nil, errors.New("not implemented")
}

func (stubReserveKeeper) HasActiveCommitment(_ context.Context, _ string, _ string) (bool, error) {
	return false, nil
}

func (bk *mockBankKeeper) FundAccount(addr sdk.AccAddress, coins sdk.Coins) {
	bk.ensureAccount(addr)
	bk.balances[addr.String()] = bk.balances[addr.String()].Add(coins...)
}

func (bk *mockBankKeeper) Balance(addr sdk.AccAddress) sdk.Coins {
	if coins, ok := bk.balances[addr.String()]; ok {
		return coins
	}
	return sdk.NewCoins()
}

func (bk *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	if module != bk.moduleName {
		return fmt.Errorf("unknown module %s", module)
	}
	if err := bk.subtract(sender, amt); err != nil {
		return err
	}
	bk.ensureAccount(bk.moduleAddr)
	bk.balances[bk.moduleAddr.String()] = bk.balances[bk.moduleAddr.String()].Add(amt...)
	return nil
}

func (bk *mockBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error {
	if module != bk.moduleName {
		return fmt.Errorf("unknown module %s", module)
	}
	if err := bk.subtract(bk.moduleAddr, amt); err != nil {
		return err
	}
	bk.ensureAccount(recipient)
	bk.balances[recipient.String()] = bk.balances[recipient.String()].Add(amt...)
	return nil
}

func (bk *mockBankKeeper) BurnCoins(_ context.Context, module string, amt sdk.Coins) error {
	if module != bk.moduleName {
		return fmt.Errorf("unknown module %s", module)
	}
	return bk.subtract(bk.moduleAddr, amt)
}

func (bk *mockBankKeeper) MintCoins(_ context.Context, module string, amt sdk.Coins) error {
	if module != bk.moduleName {
		return fmt.Errorf("unknown module %s", module)
	}
	bk.ensureAccount(bk.moduleAddr)
	bk.balances[bk.moduleAddr.String()] = bk.balances[bk.moduleAddr.String()].Add(amt...)
	return nil
}

func (bk *mockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	coins := bk.Balance(addr)
	amount := coins.AmountOf(denom)
	return sdk.NewCoin(denom, amount)
}

func (bk *mockBankKeeper) GetSupply(_ context.Context, denom string) sdk.Coin {
	total := sdkmath.ZeroInt()
	for _, coins := range bk.balances {
		total = total.Add(coins.AmountOf(denom))
	}
	return sdk.NewCoin(denom, total)
}

func (bk *mockBankKeeper) ensureAccount(addr sdk.AccAddress) {
	if _, ok := bk.balances[addr.String()]; !ok {
		bk.balances[addr.String()] = sdk.NewCoins()
	}
}

func (bk *mockBankKeeper) subtract(addr sdk.AccAddress, amt sdk.Coins) error {
	if !bk.Balance(addr).IsAllGTE(amt) {
		return fmt.Errorf("insufficient funds for %s", addr.String())
	}
	bk.balances[addr.String()] = bk.balances[addr.String()].Sub(amt...)
	return nil
}
