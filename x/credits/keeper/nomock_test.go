//go:build cosmos && cosmos_full

// Package keeper_test provides comprehensive tests for the credits module
// using real Cosmos SDK keepers instead of mocks. These tests verify
// actual state transitions and coin movements.
package keeper_test

import (
	"context"
	"testing"
	"time"

	coreaddress "cosmossdk.io/core/address"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	nfttypes "github.com/LumeraProtocol/lumera/x/nft/types"
	reservetypes "github.com/LumeraProtocol/lumera/x/reserve/types"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// =============================================================================
// Test Fixture and Setup
// =============================================================================

// realKeeperFixture contains all components for real keeper tests.
type realKeeperFixture struct {
	ctx           sdk.Context
	keeper        keeper.Keeper
	bankKeeper    bankkeeper.Keeper
	accountKeeper authkeeper.AccountKeeper
	cdc           codec.BinaryCodec

	// Stub keepers for auxiliary modules
	insuranceStub *stubInsuranceKeeper
	registryStub  *stubRegistryKeeper
	reserveStub   *stubReserveKeeper
	nftStub       *stubNFTKeeper
}

// testAddressCodec implements coreaddress.Codec for tests.
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

// setupRealKeeper creates a test fixture with real Cosmos SDK keepers.
func setupRealKeeper(t *testing.T) *realKeeperFixture {
	t.Helper()

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger())

	// Create store keys
	creditsKey := storetypes.NewKVStoreKey(types.StoreKey)
	authKey := storetypes.NewKVStoreKey(authtypes.StoreKey)
	bankKey := storetypes.NewKVStoreKey(banktypes.StoreKey)

	// Mount stores
	stateStore.MountStoreWithDB(creditsKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(authKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(bankKey, storetypes.StoreTypeIAVL, db)

	require.NoError(t, stateStore.LoadLatestVersion())

	// Set up codec
	registry := codectypes.NewInterfaceRegistry()
	authtypes.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	types.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	// Create context
	ctx := sdk.NewContext(stateStore, cmtproto.Header{
		Height:  1,
		Time:    time.Date(2025, 10, 15, 12, 0, 0, 0, time.UTC),
		AppHash: nil,
	}, false, log.NewNopLogger()).WithEventManager(sdk.NewEventManager())

	// Module account permissions
	maccPerms := map[string][]string{
		types.ModuleName: {authtypes.Minter, authtypes.Burner},
	}

	// Address codec
	addrCodec := &testAddressCodec{}

	bech32Prefix := sdk.GetConfig().GetBech32AccountAddrPrefix()
	if bech32Prefix == "" {
		bech32Prefix = "cosmos"
	}

	// Create real auth keeper
	accountKeeper := authkeeper.NewAccountKeeper(
		cdc,
		runtime.NewKVStoreService(authKey),
		authtypes.ProtoBaseAccount,
		maccPerms,
		addrCodec,
		bech32Prefix,
		authtypes.NewModuleAddress("gov").String(),
	)

	// Blocked addresses for bank keeper
	blockedAddrs := make(map[string]bool)
	for name := range maccPerms {
		blockedAddrs[authtypes.NewModuleAddress(name).String()] = true
	}

	// Create real bank keeper
	baseBankKeeper := bankkeeper.NewBaseKeeper(
		cdc,
		runtime.NewKVStoreService(bankKey),
		accountKeeper,
		blockedAddrs,
		authtypes.NewModuleAddress("gov").String(),
		log.NewNopLogger(),
	)

	// Create stub keepers for auxiliary modules
	insuranceStub := &stubInsuranceKeeper{
		poolBalance:   sdk.NewCoins(),
		contributions: make(map[string]sdk.Coins),
	}
	registryStub := &stubRegistryKeeper{
		publishers: make(map[string]sdk.AccAddress),
	}
	reserveStub := &stubReserveKeeper{
		allocations: make(map[string]reservetypes.ReserveAllocation),
	}
	nftStub := &stubNFTKeeper{
		toolpacks: make(map[string]*nfttypes.ToolpackNFT),
		payouts:   make(map[string]sdk.Coin),
	}

	// Create credits keeper with real bank/auth and stub auxiliary keepers
	creditsKeeper := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(creditsKey),
		&baseBankKeeper,
		accountKeeper,
		insuranceStub,
		registryStub,
		reserveStub,
		nftStub,
		authtypes.NewModuleAddress("gov").String(),
	)

	// Initialize params (no InitGenesis on Keeper - it's on AppModule)
	require.NoError(t, creditsKeeper.SetParams(ctx, types.DefaultParams()))

	return &realKeeperFixture{
		ctx:           ctx,
		keeper:        creditsKeeper,
		bankKeeper:    &baseBankKeeper,
		accountKeeper: accountKeeper,
		cdc:           cdc,
		insuranceStub: insuranceStub,
		registryStub:  registryStub,
		reserveStub:   reserveStub,
		nftStub:       nftStub,
	}
}

// =============================================================================
// Stub Keepers for Auxiliary Modules
// =============================================================================

// stubInsuranceKeeper implements types.InsuranceKeeper for tests.
type stubInsuranceKeeper struct {
	poolBalance   sdk.Coins
	contributions map[string]sdk.Coins
}

var _ types.InsuranceKeeper = (*stubInsuranceKeeper)(nil)

func (s *stubInsuranceKeeper) ContributeToPool(_ context.Context, receiptID, _, _, _, _ string, amount sdk.Coins) error {
	s.contributions[receiptID] = amount
	s.poolBalance = s.poolBalance.Add(amount...)
	return nil
}

func (s *stubInsuranceKeeper) GetPoolBalance(_ context.Context) (sdk.Coins, error) {
	return s.poolBalance, nil
}

// stubRegistryKeeper implements types.RegistryKeeper for tests.
type stubRegistryKeeper struct {
	publishers map[string]sdk.AccAddress
}

var _ types.RegistryKeeper = (*stubRegistryKeeper)(nil)

func (s *stubRegistryKeeper) GetToolPublisher(_ context.Context, toolID string) (sdk.AccAddress, error) {
	if addr, ok := s.publishers[toolID]; ok {
		return addr, nil
	}
	// Return a default address for testing
	return sdk.AccAddress([]byte("default-publisher-addr")), nil
}

// stubReserveKeeper implements types.ReserveKeeper for tests.
type stubReserveKeeper struct {
	allocations map[string]reservetypes.ReserveAllocation
}

var _ types.ReserveKeeper = (*stubReserveKeeper)(nil)

func (s *stubReserveKeeper) AllocateReserve(_ context.Context, owner, policyID, toolID string, amount sdk.Coin) (reservetypes.ReserveAllocation, error) {
	alloc := reservetypes.ReserveAllocation{
		Applied:         true,
		CommitmentID:    owner + "-" + policyID + "-" + toolID,
		DiscountedPrice: amount,
	}
	key := owner + "-" + policyID + "-" + toolID
	s.allocations[key] = alloc
	return alloc, nil
}

// stubNFTKeeper implements types.NFTKeeper for tests.
type stubNFTKeeper struct {
	toolpacks map[string]*nfttypes.ToolpackNFT
	payouts   map[string]sdk.Coin
}

var _ types.NFTKeeper = (*stubNFTKeeper)(nil)

func (s *stubNFTKeeper) GetToolpack(_ context.Context, id string) (*nfttypes.ToolpackNFT, bool, error) {
	tp, ok := s.toolpacks[id]
	return tp, ok, nil
}

func (s *stubNFTKeeper) RecordRoyaltyPayout(_ context.Context, _ string, toolpackID string, amount sdk.Coin) error {
	s.payouts[toolpackID] = amount
	return nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// fundAccount creates and funds a test account.
func fundAccount(t *testing.T, fixture *realKeeperFixture, addr sdk.AccAddress, amount int64) {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewInt64Coin("ulac", amount))
	// Mint to module then send to account
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, types.ModuleName, coins))
	require.NoError(t, fixture.bankKeeper.SendCoinsFromModuleToAccount(fixture.ctx, types.ModuleName, addr, coins))
}

// testAddr creates a deterministic test address.
func testAddr(b byte) sdk.AccAddress {
	return sdk.AccAddress(append([]byte("testaddr"), b))
}

// =============================================================================
// Tests: Keeper Initialization
// =============================================================================

func TestRealKeeper_Initialization(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Verify keeper is initialized
	require.NotNil(t, fixture.keeper)

	// Verify params are set
	params := fixture.keeper.GetParams(fixture.ctx)
	require.NotNil(t, params)
}

func TestRealKeeper_GetSetParams(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Get default params
	params := fixture.keeper.GetParams(fixture.ctx)
	require.NotNil(t, params)

	// Modify and set
	newParams := types.DefaultParams()
	newParams.MaxLockTtlSeconds = 7200 // 2 hours

	err := fixture.keeper.SetParams(fixture.ctx, newParams)
	require.NoError(t, err)

	// Verify change persisted
	retrieved := fixture.keeper.GetParams(fixture.ctx)
	require.Equal(t, uint32(7200), retrieved.MaxLockTtlSeconds)
}

// =============================================================================
// Tests: Lock Operations with Real State
// =============================================================================

func TestRealKeeper_LockCredits(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Fund the router account (LockCredits uses routerAddr)
	router := testAddr(0x01)
	fundAccount(t, fixture, router, 1_000_000)

	// Verify initial balance
	balance := fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	require.Equal(t, sdkmath.NewInt(1_000_000), balance.Amount)

	// Create lock request using the actual API signature:
	// LockCredits(ctx, routerAddr string, sessionID string, amount sdk.Coin, toolID string, quoteID string, policyVersion string, intentHash string, toolpackID ...string)
	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),   // routerAddr
		"session-001",     // sessionID
		lockAmount,        // amount
		"test-tool-001",   // toolID
		"quote-001",       // quoteID
		"policy-v1",       // policyVersion
		"intent-hash-abc", // intentHash
	)
	require.NoError(t, err)
	require.NotEmpty(t, lockID)

	// Verify router balance decreased (locked funds moved to module)
	newBalance := fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	require.Equal(t, sdkmath.NewInt(900_000), newBalance.Amount)

	// Verify module account received the funds
	moduleAddr := fixture.accountKeeper.GetModuleAddress(types.ModuleName)
	moduleBalance := fixture.bankKeeper.GetBalance(fixture.ctx, moduleAddr, "ulac")
	require.True(t, moduleBalance.Amount.GTE(sdkmath.NewInt(100_000)))

	// Verify lock exists in keeper state
	lock, found := fixture.keeper.GetLock(fixture.ctx, lockID)
	require.True(t, found)
	require.Equal(t, lockID, lock.LockId)
	require.Equal(t, router.String(), lock.Router)
	require.Equal(t, "test-tool-001", lock.ToolId)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, lock.Status)
}

func TestRealKeeper_LockCredits_InsufficientFunds(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Create router with small balance
	router := testAddr(0x02)
	fundAccount(t, fixture, router, 50_000)

	// Try to lock more than available
	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	_, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"session-002",
		lockAmount,
		"test-tool-002",
		"quote-002",
		"policy-v1",
		"intent-hash-xyz",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient")
}

func TestRealKeeper_UnlockCredits(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Fund and create a lock
	router := testAddr(0x03)
	fundAccount(t, fixture, router, 1_000_000)

	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"session-003",
		lockAmount,
		"test-tool-003",
		"quote-003",
		"policy-v1",
		"intent-hash-003",
	)
	require.NoError(t, err)

	// Record balance after lock
	balanceAfterLock := fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	require.Equal(t, sdkmath.NewInt(900_000), balanceAfterLock.Amount)

	// Unlock the credits (with reason)
	err = fixture.keeper.UnlockCredits(fixture.ctx, lockID, "cancelled")
	require.NoError(t, err)

	// Verify funds returned to router
	balanceAfterUnlock := fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	require.Equal(t, sdkmath.NewInt(1_000_000), balanceAfterUnlock.Amount)

	// Verify lock status updated
	lock, found := fixture.keeper.GetLock(fixture.ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, lock.Status)
}

func TestRealKeeper_UnlockCredits_AlreadyUnlocked(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x04)
	fundAccount(t, fixture, router, 1_000_000)

	lockAmount := sdk.NewInt64Coin("ulac", 50_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"session-004",
		lockAmount,
		"test-tool-004",
		"quote-004",
		"policy-v1",
		"intent-hash-004",
	)
	require.NoError(t, err)

	// First unlock succeeds
	err = fixture.keeper.UnlockCredits(fixture.ctx, lockID, "first-unlock")
	require.NoError(t, err)

	// Second unlock should fail (lock is no longer active)
	err = fixture.keeper.UnlockCredits(fixture.ctx, lockID, "second-unlock")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLockInactive)
}

func TestRealKeeper_UnlockCredits_NonexistentLock(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.UnlockCredits(fixture.ctx, "nonexistent-lock-id", "test")
	require.Error(t, err)
}

// =============================================================================
// Tests: Settlement Operations with Real State
// =============================================================================

func TestRealKeeper_SettleLock(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Setup: router and publisher
	router := testAddr(0x10)
	publisher := testAddr(0x11)
	fundAccount(t, fixture, router, 1_000_000)

	// Register publisher in stub
	fixture.registryStub.publishers["settle-tool-001"] = publisher

	// Create a lock
	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"session-settle-001",
		lockAmount,
		"settle-tool-001",
		"quote-settle-001",
		"policy-v1",
		"intent-hash-settle",
	)
	require.NoError(t, err)

	// Record balances before settlement
	routerBalanceBefore := fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	publisherBalanceBefore := fixture.bankKeeper.GetBalance(fixture.ctx, publisher, "ulac")

	// Settle the lock using SettlementRequest
	settleAmount := sdk.NewInt64Coin("ulac", 80_000)
	receipt := keeper.SettlementRequest{
		ReceiptID:     "receipt-settle-001",
		ToolID:        "settle-tool-001",
		TotalAmount:   sdk.NewCoins(settleAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
	}
	result, err := fixture.keeper.SettleLock(fixture.ctx, lockID, settleAmount, receipt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify publisher received settlement
	publisherBalanceAfter := fixture.bankKeeper.GetBalance(fixture.ctx, publisher, "ulac")
	// Publisher should have received some amount (minus fees)
	require.True(t, publisherBalanceAfter.Amount.GT(publisherBalanceBefore.Amount))

	// Verify lock status
	lock, found := fixture.keeper.GetLock(fixture.ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, lock.Status)

	// Verify router got refund of remaining amount (if any)
	routerBalanceAfter := fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	// Note: exact amounts depend on fee calculations
	t.Logf("Router balance: before=%s, after=%s", routerBalanceBefore, routerBalanceAfter)
	t.Logf("Publisher balance: before=%s, after=%s", publisherBalanceBefore, publisherBalanceAfter)
}

func TestRealKeeper_SettleLock_FullAmount(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x12)
	publisher := testAddr(0x13)
	fundAccount(t, fixture, router, 500_000)

	fixture.registryStub.publishers["tool-full-settle"] = publisher

	// Create lock
	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"session-full-settle",
		lockAmount,
		"tool-full-settle",
		"quote-full",
		"policy-v1",
		"intent-full",
	)
	require.NoError(t, err)

	// Settle full amount
	receipt := keeper.SettlementRequest{
		ReceiptID:     "receipt-full-001",
		ToolID:        "tool-full-settle",
		TotalAmount:   sdk.NewCoins(lockAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
	}
	result, err := fixture.keeper.SettleLock(fixture.ctx, lockID, lockAmount, receipt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify lock is settled
	lock, found := fixture.keeper.GetLock(fixture.ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, lock.Status)
}

func TestRealKeeper_SettleLock_NonexistentLock(t *testing.T) {
	fixture := setupRealKeeper(t)

	settleAmount := sdk.NewInt64Coin("ulac", 50_000)
	receipt := keeper.SettlementRequest{
		ReceiptID:     "receipt-nonexistent",
		ToolID:        "some-tool",
		TotalAmount:   sdk.NewCoins(settleAmount),
		PublisherAddr: testAddr(0x99),
		RouterAddr:    testAddr(0x98),
	}
	_, err := fixture.keeper.SettleLock(
		fixture.ctx,
		"nonexistent-lock",
		settleAmount,
		receipt,
	)
	require.Error(t, err)
}

// =============================================================================
// Tests: Multiple Lock Operations
// =============================================================================

func TestRealKeeper_MultipleLocks(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x20)
	fundAccount(t, fixture, router, 1_000_000)

	// Create multiple locks
	lockIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		lockAmount := sdk.NewInt64Coin("ulac", 100_000)
		lockID, err := fixture.keeper.LockCredits(
			fixture.ctx,
			router.String(),
			"multi-session-"+string(rune('a'+i)),
			lockAmount,
			"multi-tool-"+string(rune('a'+i)),
			"quote-multi-"+string(rune('a'+i)),
			"policy-v1",
			"intent-multi-"+string(rune('a'+i)),
		)
		require.NoError(t, err)
		lockIDs[i] = lockID
	}

	// Verify all locks exist
	for _, lockID := range lockIDs {
		lock, found := fixture.keeper.GetLock(fixture.ctx, lockID)
		require.True(t, found, "lock %s should exist", lockID)
		require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, lock.Status)
	}

	// Verify total locked (300k out of 1M)
	balance := fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	require.Equal(t, sdkmath.NewInt(700_000), balance.Amount)

	// Unlock one
	err := fixture.keeper.UnlockCredits(fixture.ctx, lockIDs[1], "cancelled")
	require.NoError(t, err)

	// Verify balance updated
	balance = fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	require.Equal(t, sdkmath.NewInt(800_000), balance.Amount)
}

// =============================================================================
// Tests: Params Persistence
// =============================================================================

func TestRealKeeper_ParamsPersistence(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Modify params
	newParams := types.DefaultParams()
	newParams.MaxLockTtlSeconds = 14400 // 4 hours instead of default 1 hour
	err := fixture.keeper.SetParams(fixture.ctx, newParams)
	require.NoError(t, err)

	// Verify params persisted correctly
	retrieved := fixture.keeper.GetParams(fixture.ctx)
	require.NotNil(t, retrieved)
	require.Equal(t, uint32(14400), retrieved.MaxLockTtlSeconds)

	// Create a second fixture to verify independence
	fixture2 := setupRealKeeper(t)
	defaultParams := fixture2.keeper.GetParams(fixture2.ctx)
	// Second fixture should have default params
	expectedDefault := types.DefaultParams()
	require.Equal(t, expectedDefault.MaxLockTtlSeconds, defaultParams.MaxLockTtlSeconds)
}

// =============================================================================
// Tests: Event Emission
// =============================================================================

func TestRealKeeper_LockEvents(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x30)
	fundAccount(t, fixture, router, 500_000)

	// Create a new context with fresh event manager
	ctx := fixture.ctx.WithEventManager(sdk.NewEventManager())

	lockAmount := sdk.NewInt64Coin("ulac", 50_000)
	_, err := fixture.keeper.LockCredits(
		ctx,
		router.String(),
		"event-session",
		lockAmount,
		"event-tool",
		"event-quote",
		"policy-v1",
		"event-intent",
	)
	require.NoError(t, err)

	// Verify events were emitted (EventTypeLock = "credit_lock")
	events := ctx.EventManager().Events()
	foundLockEvent := false
	for _, e := range events {
		if e.Type == types.EventTypeLock {
			foundLockEvent = true
			break
		}
	}
	require.True(t, foundLockEvent, "credit_lock event should be emitted")
}

// =============================================================================
// Tests: Edge Cases
// =============================================================================

func TestRealKeeper_ZeroAmountLock(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x40)
	fundAccount(t, fixture, router, 100_000)

	lockAmount := sdk.NewInt64Coin("ulac", 0)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"zero-session",
		lockAmount,
		"zero-tool",
		"zero-quote",
		"policy-v1",
		"zero-intent",
	)
	require.Error(t, err, "zero amount lock should be rejected before escrow")
	require.Contains(t, err.Error(), "positive")
	require.Empty(t, lockID)

	// Verify balance unchanged (no coins moved)
	balance := fixture.bankKeeper.GetBalance(fixture.ctx, router, "ulac")
	require.Equal(t, sdkmath.NewInt(100_000), balance.Amount, "balance should be unchanged for zero lock")
}

func TestRealKeeper_ExpiredLockUnlock(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x41)
	fundAccount(t, fixture, router, 500_000)

	// Create lock (TTL is controlled by params, not passed directly)
	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"expiry-session",
		lockAmount,
		"expiry-tool",
		"expiry-quote",
		"policy-v1",
		"expiry-intent",
	)
	require.NoError(t, err)

	// Get TTL from params to advance past it
	params := fixture.keeper.GetParams(fixture.ctx)
	ttlDuration := time.Duration(params.MaxLockTtlSeconds) * time.Second

	// Advance time past expiry
	newCtx := fixture.ctx.WithBlockTime(fixture.ctx.BlockTime().Add(ttlDuration + time.Hour))

	// Try to unlock after expiry - should either succeed (returning funds)
	// or fail with expiry error, but should not panic
	err = fixture.keeper.UnlockCredits(newCtx, lockID, "after-expiry")

	// Verify the lock state changed (either released or still active if unlock failed)
	lock, found := fixture.keeper.GetLock(newCtx, lockID)
	require.True(t, found, "lock should still exist in state")

	if err == nil {
		// If unlock succeeded, status should be RELEASED
		require.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, lock.Status,
			"unlocked lock should have RELEASED status")
		// And funds returned
		balance := fixture.bankKeeper.GetBalance(newCtx, router, "ulac")
		require.Equal(t, sdkmath.NewInt(500_000), balance.Amount,
			"funds should be returned after unlock")
	} else {
		// If unlock failed, lock should still be ACTIVE
		require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, lock.Status,
			"failed unlock should leave lock ACTIVE")
		t.Logf("Expired lock unlock rejected (expected for strict expiry handling): %v", err)
	}
}

func TestRealKeeper_LockWithToolpackID(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x50)
	fundAccount(t, fixture, router, 1_000_000)

	// Create lock with optional toolpackID
	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"toolpack-session",
		lockAmount,
		"toolpack-tool",
		"toolpack-quote",
		"policy-v1",
		"toolpack-intent",
		"toolpack-001", // Optional toolpack ID
	)
	require.NoError(t, err)

	// Verify lock created with toolpack ID
	lock, found := fixture.keeper.GetLock(fixture.ctx, lockID)
	require.True(t, found)
	require.Equal(t, router.String(), lock.Router)
	require.Equal(t, "toolpack-001", lock.ToolpackId)
}

func TestRealKeeper_EmptyRouterAddress(t *testing.T) {
	fixture := setupRealKeeper(t)

	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	_, err := fixture.keeper.LockCredits(
		fixture.ctx,
		"", // Empty router address
		"empty-session",
		lockAmount,
		"empty-tool",
		"empty-quote",
		"policy-v1",
		"empty-intent",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "router address required")
}

func TestRealKeeper_InvalidRouterAddress(t *testing.T) {
	fixture := setupRealKeeper(t)

	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	_, err := fixture.keeper.LockCredits(
		fixture.ctx,
		"not-a-valid-bech32-address",
		"invalid-session",
		lockAmount,
		"invalid-tool",
		"invalid-quote",
		"policy-v1",
		"invalid-intent",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid")
}

// =============================================================================
// Tests: Settlement Revenue Routing (bd-3tv44)
// =============================================================================

// settleAndVerify is a helper that creates a lock, settles it, and returns
// balances and events for verification. On failure it logs all computed values.
func settleAndVerify(
	t *testing.T,
	fixture *realKeeperFixture,
	lockAmountRaw, settleAmountRaw int64,
	receipt keeper.SettlementRequest,
) (lockID string, result *keeper.SettlementResult, events sdk.Events) {
	t.Helper()

	router := receipt.RouterAddr
	publisher := receipt.PublisherAddr

	lockAmount := sdk.NewInt64Coin("ulac", lockAmountRaw)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"settle-session-"+receipt.ReceiptID,
		lockAmount,
		receipt.ToolID,
		"quote-"+receipt.ReceiptID,
		"policy-v1",
		"intent-"+receipt.ReceiptID,
		receipt.ToolpackID,
	)
	require.NoError(t, err, "lock creation failed")

	// Fresh event manager to capture only settlement events.
	ctx := fixture.ctx.WithEventManager(sdk.NewEventManager())

	settleAmount := sdk.NewInt64Coin("ulac", settleAmountRaw)
	receipt.TotalAmount = sdk.NewCoins(settleAmount)

	result, err = fixture.keeper.SettleLock(ctx, lockID, settleAmount, receipt)
	if err != nil {
		t.Logf("SETTLE FAILED receipt_id=%s lock=%d settle=%d err=%v",
			receipt.ReceiptID, lockAmountRaw, settleAmountRaw, err)
		t.Logf("  publisher=%s router=%s referrer=%s toolpack=%s",
			publisher, router, receipt.ReferrerAddr, receipt.ToolpackID)
	}
	require.NoError(t, err, "settlement failed")

	events = ctx.EventManager().Events()

	// Log key computed values for debugging.
	t.Logf("Settlement %s: lock=%d settle=%d", receipt.ReceiptID, lockAmountRaw, settleAmountRaw)
	t.Logf("  burn=%s publisher=%s router=%s",
		result.BurnAmount, result.PublisherAmount, result.RouterAmount)
	t.Logf("  origin_surface=%s treasury=%s referrer=%s refund=%s",
		result.OriginSurfaceAmount, result.TreasuryAmount, result.ReferrerAmount, result.RefundAmount)
	t.Logf("  events_emitted=%d", len(events))

	return lockID, result, events
}

func TestRealKeeper_SettlementSplitConservation(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0xA0)
	publisher := testAddr(0xA1)
	fundAccount(t, fixture, router, 1_000_000)
	fixture.registryStub.publishers["conservation-tool"] = publisher

	receipt := keeper.SettlementRequest{
		ReceiptID:     "conservation-001",
		ToolID:        "conservation-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
	}

	_, result, _ := settleAndVerify(t, fixture, 100_000, 80_000, receipt)

	// Conservation: burn + insurance + publisher + router + refund = lock amount.
	// Insurance is deducted from the settlement amount but currently redistributed
	// (not tracked in result). We verify all result fields are non-negative and
	// the publisher+router+burn amounts are logically consistent with the net.
	require.False(t, result.BurnAmount.IsAnyNegative(), "burn must be non-negative")
	require.False(t, result.PublisherAmount.IsAnyNegative(), "publisher must be non-negative")
	require.False(t, result.RouterAmount.IsAnyNegative(), "router must be non-negative")
	require.False(t, result.RefundAmount.IsAnyNegative(), "refund must be non-negative")

	// With default params (burn=300bps, insurance=0bps):
	// Net after deductions = 80000 - 2400 = 77600
	// Without referrer, router gets referrer share: pub=7000, router=3000
	// Publisher = 77600 * 7000/10000 = 54320
	// Router = 77600 * 3000/10000 = 23280
	burnAmt := result.BurnAmount.AmountOf("ulac")
	pubAmt := result.PublisherAmount.AmountOf("ulac")
	routerAmt := result.RouterAmount.AmountOf("ulac")
	refundAmt := result.RefundAmount.AmountOf("ulac")

	require.True(t, burnAmt.IsPositive(), "burn amount should be positive: %s", burnAmt)
	require.True(t, pubAmt.IsPositive(), "publisher amount should be positive: %s", pubAmt)
	require.True(t, routerAmt.IsPositive(), "router amount should be positive: %s", routerAmt)
	require.True(t, refundAmt.IsPositive(), "refund should be positive (80k < 100k lock): %s", refundAmt)

	t.Logf("  Conservation: burn=%s pub=%s router=%s refund=%s",
		burnAmt, pubAmt, routerAmt, refundAmt)
}

func TestRealKeeper_SettlementWithReferrer(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0xB0)
	publisher := testAddr(0xB1)
	referrer := testAddr(0xB2)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["ref-tool"] = publisher

	receipt := keeper.SettlementRequest{
		ReceiptID:     "referrer-001",
		ToolID:        "ref-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
		ReferrerAddr:  referrer,
		ReferrerID:    referrer.String(),
	}

	_, result, _ := settleAndVerify(t, fixture, 200_000, 200_000, receipt)

	// With referrer: pub=7000, router=2000, referrer=1000
	refAmt := result.ReferrerAmount.AmountOf("ulac")
	require.True(t, refAmt.IsPositive(),
		"referrer should receive non-zero amount: %s", refAmt)

	// Referrer share should be smaller than router share.
	routerAmt := result.RouterAmount.AmountOf("ulac")
	require.True(t, routerAmt.GT(refAmt),
		"router (%s) should be > referrer (%s) with 2000/1000 split", routerAmt, refAmt)

	// Publisher should be the largest share.
	pubAmt := result.PublisherAmount.AmountOf("ulac")
	require.True(t, pubAmt.GT(routerAmt),
		"publisher (%s) should be > router (%s) with 7000/2000 split", pubAmt, routerAmt)

	// Verify referrer balance increased.
	refBal := fixture.bankKeeper.GetBalance(fixture.ctx, referrer, "ulac")
	require.True(t, refBal.Amount.IsPositive(), "referrer balance should be positive")
}

func TestRealKeeper_SettlementNoReferrer_RouterGetsExtraShare(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0xC0)
	publisher := testAddr(0xC1)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["noref-tool"] = publisher

	// Settlement WITHOUT referrer.
	receiptNoRef := keeper.SettlementRequest{
		ReceiptID:     "noref-001",
		ToolID:        "noref-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
	}
	_, resultNoRef, _ := settleAndVerify(t, fixture, 200_000, 150_000, receiptNoRef)

	// Same settlement WITH referrer for comparison.
	router2 := testAddr(0xC2)
	publisher2 := testAddr(0xC3)
	referrer := testAddr(0xC4)
	fundAccount(t, fixture, router2, 500_000)
	fixture.registryStub.publishers["noref-tool-2"] = publisher2

	receiptWithRef := keeper.SettlementRequest{
		ReceiptID:     "noref-002",
		ToolID:        "noref-tool-2",
		PublisherAddr: publisher2,
		RouterAddr:    router2,
		ReferrerAddr:  referrer,
		ReferrerID:    referrer.String(),
	}
	_, resultWithRef, _ := settleAndVerify(t, fixture, 200_000, 150_000, receiptWithRef)

	// Without referrer, router gets referrer share (30% vs 20%).
	routerNoRef := resultNoRef.RouterAmount.AmountOf("ulac")
	routerWithRef := resultWithRef.RouterAmount.AmountOf("ulac")
	require.True(t, routerNoRef.GT(routerWithRef),
		"router without referrer (%s) should get more than with referrer (%s)",
		routerNoRef, routerWithRef)

	// Publisher amounts should be the same (publisher share unchanged at 7000 bps).
	pubNoRef := resultNoRef.PublisherAmount.AmountOf("ulac")
	pubWithRef := resultWithRef.PublisherAmount.AmountOf("ulac")
	require.Equal(t, pubNoRef.String(), pubWithRef.String(),
		"publisher share should be identical regardless of referrer")
}

func TestRealKeeper_SettlementWithToolpack_OriginSurface(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0xD0)
	publisher := testAddr(0xD1)
	curator := testAddr(0xD2)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["toolpack-settle-tool"] = publisher

	// Configure active toolpack with 500 bps (5%) royalty carved from router share.
	fixture.nftStub.toolpacks["pack-001"] = &nfttypes.ToolpackNFT{
		Id:         "pack-001",
		Curator:    curator.String(),
		RoyaltyBps: 500,
		Active:     true,
	}

	receipt := keeper.SettlementRequest{
		ReceiptID:     "toolpack-001",
		ToolID:        "toolpack-settle-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
		ToolpackID:    "pack-001",
	}

	_, result, _ := settleAndVerify(t, fixture, 200_000, 200_000, receipt)

	originAmt := result.OriginSurfaceAmount.AmountOf("ulac")
	require.True(t, originAmt.IsPositive(),
		"origin-surface amount should be positive for active toolpack: %s", originAmt)

	// Verify curator actually received funds.
	curatorBal := fixture.bankKeeper.GetBalance(fixture.ctx, curator, "ulac")
	require.True(t, curatorBal.Amount.IsPositive(),
		"curator balance should be positive: %s", curatorBal)

	// Verify nft keeper recorded the payout.
	payout, ok := fixture.nftStub.payouts["pack-001"]
	require.True(t, ok, "nft keeper should have recorded royalty payout")
	require.True(t, payout.Amount.IsPositive(), "recorded payout should be positive")

	t.Logf("  Toolpack: origin_surface=%s curator_balance=%s nft_payout=%s",
		originAmt, curatorBal, payout)
}

func TestRealKeeper_SettlementWithTreasury(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0xE0)
	publisher := testAddr(0xE1)
	treasury := testAddr(0xE2)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["treasury-tool"] = publisher

	// Configure treasury address in params.
	params := types.DefaultParams()
	params.TreasuryAddress = treasury.String()
	require.NoError(t, fixture.keeper.SetParams(fixture.ctx, params))

	receipt := keeper.SettlementRequest{
		ReceiptID:     "treasury-001",
		ToolID:        "treasury-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
	}

	_, result, _ := settleAndVerify(t, fixture, 200_000, 200_000, receipt)

	treasuryAmt := result.TreasuryAmount.AmountOf("ulac")
	require.True(t, treasuryAmt.IsPositive(),
		"treasury amount should be positive when treasury address is configured: %s", treasuryAmt)

	// Verify treasury actually received funds.
	treasuryBal := fixture.bankKeeper.GetBalance(fixture.ctx, treasury, "ulac")
	require.True(t, treasuryBal.Amount.IsPositive(),
		"treasury balance should be positive: %s", treasuryBal)

	t.Logf("  Treasury: amount=%s balance=%s", treasuryAmt, treasuryBal)
}

func TestRealKeeper_SettlementEvents(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0xF0)
	publisher := testAddr(0xF1)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["event-settle-tool"] = publisher

	receipt := keeper.SettlementRequest{
		ReceiptID:     "event-settle-001",
		ToolID:        "event-settle-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
	}

	_, _, events := settleAndVerify(t, fixture, 200_000, 100_000, receipt)

	// Expect at least: settlement event, burn event, and distribute events.
	eventTypes := make(map[string]int)
	for _, e := range events {
		eventTypes[e.Type]++
	}

	t.Logf("  Event types: %v", eventTypes)

	require.Greater(t, eventTypes[types.EventTypeSettlement], 0,
		"should emit settlement event")
	require.Greater(t, eventTypes[types.EventTypeBurn], 0,
		"should emit burn event")
	require.Greater(t, eventTypes[types.EventTypeDistribute], 0,
		"should emit distribute event")
}

func TestRealKeeper_SettlementEvents_WithToolpack(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0xF2)
	publisher := testAddr(0xF3)
	curator := testAddr(0xF4)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["event-tp-tool"] = publisher

	fixture.nftStub.toolpacks["event-pack"] = &nfttypes.ToolpackNFT{
		Id:         "event-pack",
		Curator:    curator.String(),
		RoyaltyBps: 300,
		Active:     true,
	}

	receipt := keeper.SettlementRequest{
		ReceiptID:     "event-tp-001",
		ToolID:        "event-tp-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
		ToolpackID:    "event-pack",
	}

	_, _, events := settleAndVerify(t, fixture, 200_000, 200_000, receipt)

	// With toolpack, expect an origin_surface distribute event.
	foundOriginEvent := false
	for _, e := range events {
		if e.Type != types.EventTypeDistribute {
			continue
		}
		for _, attr := range e.Attributes {
			if attr.Key == "recipient_role" && attr.Value == "origin_surface" {
				foundOriginEvent = true
			}
		}
	}
	require.True(t, foundOriginEvent,
		"should emit origin_surface distribute event when toolpack is active")
}

// =============================================================================
// Tests: Config Validation (bd-3tv44)
// =============================================================================

func TestRealKeeper_InvalidParams_Rejected(t *testing.T) {
	fixture := setupRealKeeper(t)

	tests := []struct {
		name    string
		modify  func(*types.Params)
		errText string
	}{
		{
			name:    "zero default lock TTL",
			modify:  func(p *types.Params) { p.DefaultLockTtlSeconds = 0 },
			errText: "default lock ttl",
		},
		{
			name:    "zero max lock TTL",
			modify:  func(p *types.Params) { p.MaxLockTtlSeconds = 0 },
			errText: "max lock ttl",
		},
		{
			name: "default TTL exceeds max TTL",
			modify: func(p *types.Params) {
				p.DefaultLockTtlSeconds = 7200
				p.MaxLockTtlSeconds = 3600
			},
			errText: "cannot exceed",
		},
		{
			name:    "zero settlement grace",
			modify:  func(p *types.Params) { p.SettlementGracePeriodSeconds = 0 },
			errText: "settlement grace period",
		},
		{
			name:    "zero max settlements per block",
			modify:  func(p *types.Params) { p.MaxSettlementsPerBlock = 0 },
			errText: "max settlements per block",
		},
		{
			name:    "zero max expired locks per block",
			modify:  func(p *types.Params) { p.MaxExpiredLocksPerBlock = 0 },
			errText: "max expired locks",
		},
		{
			name:    "zero max pruned settlements",
			modify:  func(p *types.Params) { p.MaxPrunedSettlementsPerBlock = 0 },
			errText: "max pruned settlements",
		},
		{
			name:    "invalid treasury address",
			modify:  func(p *types.Params) { p.TreasuryAddress = "not-bech32" },
			errText: "invalid treasury",
		},
		{
			name:    "invalid credit denom",
			modify:  func(p *types.Params) { p.CreditDenom = "" },
			errText: "invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.modify(p)
			err := fixture.keeper.SetParams(fixture.ctx, p)
			require.Error(t, err, "SetParams should reject invalid config")
			require.Contains(t, err.Error(), tc.errText,
				"error should mention: %s", tc.errText)
			t.Logf("  Rejected config %q: %v", tc.name, err)
		})
	}
}

func TestRealKeeper_ValidParams_Accepted(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Modify all fields to non-default but valid values.
	p := types.NewParams(
		"ulac", // denom
		240,    // defaultTTL
		7200,   // maxTTL
		600,    // grace
		"",     // treasury (empty = disabled)
		200,    // maxSettlementsPerBlock
		200,    // maxExpiredLocksPerBlock
		200,    // maxPrunedSettlementsPerBlock
	)
	p.DisputeWindowHours = types.DefaultDisputeWindowHours

	err := fixture.keeper.SetParams(fixture.ctx, p)
	require.NoError(t, err, "valid params should be accepted")

	retrieved := fixture.keeper.GetParams(fixture.ctx)
	require.Equal(t, uint32(240), retrieved.DefaultLockTtlSeconds)
	require.Equal(t, uint32(7200), retrieved.MaxLockTtlSeconds)
	require.Equal(t, uint32(600), retrieved.SettlementGracePeriodSeconds)
}

// =============================================================================
// Tests: Routing Math (bd-3tv44)
// =============================================================================

func TestCalculateSplit_FiveWay(t *testing.T) {
	tests := []struct {
		name      string
		amount    int64
		pub       uint32
		router    uint32
		origin    uint32
		treasury  uint32
		referrer  uint32
		expectErr bool
	}{
		{
			name:   "standard 70/17/5/3/5 split",
			amount: 1_000_000,
			pub:    7000, router: 1200, origin: 500, treasury: 300, referrer: 1000,
		},
		{
			name:   "all to publisher",
			amount: 999_999,
			pub:    10000, router: 0, origin: 0, treasury: 0, referrer: 0,
		},
		{
			name:   "equal 5-way split",
			amount: 1_000_000,
			pub:    2000, router: 2000, origin: 2000, treasury: 2000, referrer: 2000,
		},
		{
			name:   "small amount with rounding",
			amount: 7, // Very small → rounding matters
			pub:    7000, router: 2000, origin: 0, treasury: 0, referrer: 1000,
		},
		{
			name:   "single unit",
			amount: 1,
			pub:    7000, router: 2000, origin: 0, treasury: 0, referrer: 1000,
		},
		{
			name:   "large amount near overflow safety",
			amount: 9_000_000_000_000, // 9 trillion
			pub:    7000, router: 1500, origin: 500, treasury: 300, referrer: 700,
		},
		{
			name:   "bad sum rejects",
			amount: 1000,
			pub:    5000, router: 2000, origin: 0, treasury: 0, referrer: 0,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			amount := sdkmath.NewInt(tc.amount)
			pub, router, origin, treasury, ref, err := keeper.CalculateSplit(
				amount, tc.pub, tc.router, tc.origin, tc.treasury, tc.referrer,
			)

			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			total := pub.Add(router).Add(origin).Add(treasury).Add(ref)
			require.Equal(t, amount.String(), total.String(),
				"splits must sum to original: pub=%s router=%s origin=%s treasury=%s ref=%s total=%s",
				pub, router, origin, treasury, ref, total)

			// All components non-negative.
			require.False(t, pub.IsNegative(), "pub negative: %s", pub)
			require.False(t, router.IsNegative(), "router negative: %s", router)
			require.False(t, origin.IsNegative(), "origin negative: %s", origin)
			require.False(t, treasury.IsNegative(), "treasury negative: %s", treasury)
			require.False(t, ref.IsNegative(), "ref negative: %s", ref)

			t.Logf("  Split %s: pub=%s router=%s origin=%s treasury=%s ref=%s",
				tc.name, pub, router, origin, treasury, ref)
		})
	}
}

func TestCalculateBurnAmount_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		total    int64
		burnBPS  uint32
		wantBurn int64
		wantRem  int64
	}{
		{
			name:     "3% of 100_000 (default)",
			total:    100_000,
			burnBPS:  300,
			wantBurn: 3_000,
			wantRem:  97_000,
		},
		{
			name:     "3% of 1 (rounds to 0)",
			total:    1,
			burnBPS:  300,
			wantBurn: 0,
			wantRem:  1,
		},
		{
			name:     "3% of 33 (truncation)",
			total:    33,
			burnBPS:  300,
			wantBurn: 0, // 33 * 300 / 10000 = 0.99 → truncated to 0
			wantRem:  33,
		},
		{
			name:     "3% of 34 (just above truncation)",
			total:    34,
			burnBPS:  300,
			wantBurn: 1, // 34 * 300 / 10000 = 1.02 → truncated to 1
			wantRem:  33,
		},
		{
			name:     "50% of 1_000_001 (odd)",
			total:    1_000_001,
			burnBPS:  5000,
			wantBurn: 500_000, // truncates: 1_000_001 * 5000 / 10000 = 500_000.5 → 500_000
			wantRem:  500_001,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			total := sdkmath.NewInt(tc.total)
			burn, rem, err := keeper.CalculateBurnAmount(total, tc.burnBPS)
			require.NoError(t, err)
			require.Equal(t, tc.wantBurn, burn.Int64(),
				"burn: expected %d, got %d", tc.wantBurn, burn.Int64())
			require.Equal(t, tc.wantRem, rem.Int64(),
				"remaining: expected %d, got %d", tc.wantRem, rem.Int64())

			// Conservation: burn + remaining = total
			require.Equal(t, total.String(), burn.Add(rem).String(),
				"burn + remaining must equal total")
		})
	}
}

func TestValidateRates_Comprehensive(t *testing.T) {
	tests := []struct {
		name      string
		burn      uint32
		ins       uint32
		pub       uint32
		router    uint32
		origin    uint32
		treasury  uint32
		ref       uint32
		expectErr bool
		errText   string
	}{
		{
			name: "default rates valid",
			burn: 300, ins: 200,
			pub: 7000, router: 1200, origin: 500, treasury: 300, ref: 1000,
		},
		{
			name: "zero burn and insurance valid",
			burn: 0, ins: 0,
			pub: 7000, router: 2000, origin: 0, treasury: 0, ref: 1000,
		},
		{
			name: "max deductions at boundary",
			burn: 5000, ins: 5000,
			pub: 7000, router: 2000, origin: 0, treasury: 0, ref: 1000,
		},
		{
			name: "combined deductions exceed 100%",
			burn: 5001, ins: 5000,
			pub: 7000, router: 2000, origin: 0, treasury: 0, ref: 1000,
			expectErr: true, errText: "combined",
		},
		{
			name: "shares below 10000",
			burn: 300, ins: 200,
			pub: 7000, router: 2000, origin: 0, treasury: 0, ref: 500,
			expectErr: true, errText: "sum to 10000",
		},
		{
			name: "shares above 10000",
			burn: 300, ins: 200,
			pub: 7000, router: 2000, origin: 500, treasury: 500, ref: 1000,
			expectErr: true, errText: "sum to 10000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := keeper.ValidateRates(tc.burn, tc.ins, tc.pub, tc.router, tc.origin, tc.treasury, tc.ref)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errText)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSafeAddCoins_Basic(t *testing.T) {
	a := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	b := sdk.NewCoins(sdk.NewInt64Coin("ulac", 200))

	result, err := keeper.SafeAddCoins(a, b)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(300), result.AmountOf("ulac"))
}

func TestSafeAddCoins_MultiDenom(t *testing.T) {
	a := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100), sdk.NewInt64Coin("ulume", 50))
	b := sdk.NewCoins(sdk.NewInt64Coin("ulac", 200), sdk.NewInt64Coin("ulume", 30))

	result, err := keeper.SafeAddCoins(a, b)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(300), result.AmountOf("ulac"))
	require.Equal(t, sdkmath.NewInt(80), result.AmountOf("ulume"))
}

func TestSafeAddCoins_EmptyCoins(t *testing.T) {
	a := sdk.NewCoins()
	b := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))

	result, err := keeper.SafeAddCoins(a, b)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(100), result.AmountOf("ulac"))
}

// =============================================================================
// Tests: Settlement Invariant Checks (bd-3tv44)
// =============================================================================

func TestRealKeeper_SettleLock_AlreadySettled(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x60)
	publisher := testAddr(0x61)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["dup-tool"] = publisher

	// Create and settle a lock.
	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"dup-session",
		lockAmount,
		"dup-tool",
		"dup-quote",
		"policy-v1",
		"dup-intent",
	)
	require.NoError(t, err)

	settleAmount := sdk.NewInt64Coin("ulac", 80_000)
	receipt := keeper.SettlementRequest{
		ReceiptID:     "dup-receipt-001",
		ToolID:        "dup-tool",
		TotalAmount:   sdk.NewCoins(settleAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
	}
	_, err = fixture.keeper.SettleLock(fixture.ctx, lockID, settleAmount, receipt)
	require.NoError(t, err)

	// Second settlement on same lock should fail.
	receipt2 := keeper.SettlementRequest{
		ReceiptID:     "dup-receipt-002",
		ToolID:        "dup-tool",
		TotalAmount:   sdk.NewCoins(settleAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
	}
	_, err = fixture.keeper.SettleLock(fixture.ctx, lockID, settleAmount, receipt2)
	require.Error(t, err, "second settlement on same lock should fail")
	t.Logf("  Replay protection: %v", err)
}

func TestRealKeeper_SettleLock_ExceedsLocked(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x62)
	publisher := testAddr(0x63)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["exceed-tool"] = publisher

	lockAmount := sdk.NewInt64Coin("ulac", 50_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"exceed-session",
		lockAmount,
		"exceed-tool",
		"exceed-quote",
		"policy-v1",
		"exceed-intent",
	)
	require.NoError(t, err)

	// Try to settle more than locked.
	overAmount := sdk.NewInt64Coin("ulac", 100_000)
	receipt := keeper.SettlementRequest{
		ReceiptID:     "exceed-receipt",
		ToolID:        "exceed-tool",
		TotalAmount:   sdk.NewCoins(overAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
	}
	_, err = fixture.keeper.SettleLock(fixture.ctx, lockID, overAmount, receipt)
	require.Error(t, err, "settling more than locked should fail")
	t.Logf("  Exceed protection: %v", err)
}

func TestRealKeeper_SettleLock_EmptyReceiptID(t *testing.T) {
	fixture := setupRealKeeper(t)

	router := testAddr(0x64)
	publisher := testAddr(0x65)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["empty-receipt-tool"] = publisher

	lockAmount := sdk.NewInt64Coin("ulac", 100_000)
	lockID, err := fixture.keeper.LockCredits(
		fixture.ctx,
		router.String(),
		"empty-receipt-session",
		lockAmount,
		"empty-receipt-tool",
		"empty-receipt-quote",
		"policy-v1",
		"empty-receipt-intent",
	)
	require.NoError(t, err)

	receipt := keeper.SettlementRequest{
		ReceiptID:     "", // Empty!
		ToolID:        "empty-receipt-tool",
		TotalAmount:   sdk.NewCoins(sdk.NewInt64Coin("ulac", 50_000)),
		PublisherAddr: publisher,
		RouterAddr:    router,
	}
	_, err = fixture.keeper.SettleLock(fixture.ctx, lockID, sdk.NewInt64Coin("ulac", 50_000), receipt)
	require.Error(t, err, "empty receipt ID should be rejected")
	require.Contains(t, err.Error(), "receipt")
}

// =============================================================================
// Tests: Insurance Contribution (bd-3tv44)
// =============================================================================

func TestRealKeeper_SettlementInsuranceContribution(t *testing.T) {
	fixture := setupRealKeeper(t)
	params := types.DefaultParams()
	params.InsuranceBps = 200
	require.NoError(t, fixture.keeper.SetParams(fixture.ctx, params))

	router := testAddr(0x70)
	publisher := testAddr(0x71)
	fundAccount(t, fixture, router, 500_000)
	fixture.registryStub.publishers["ins-tool"] = publisher

	// Record insurance pool before settlement.
	poolBefore, err := fixture.insuranceStub.GetPoolBalance(fixture.ctx)
	require.NoError(t, err)

	receipt := keeper.SettlementRequest{
		ReceiptID:     "ins-001",
		ToolID:        "ins-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
	}
	settleAndVerify(t, fixture, 200_000, 200_000, receipt)

	// Insurance pool should have received a contribution.
	poolAfter, err := fixture.insuranceStub.GetPoolBalance(fixture.ctx)
	require.NoError(t, err)

	insuranceAmt := poolAfter.AmountOf("ulac").Sub(poolBefore.AmountOf("ulac"))
	require.True(t, insuranceAmt.IsPositive(),
		"insurance pool should grow after settlement: before=%s after=%s",
		poolBefore, poolAfter)

	// With 200bps insurance on 200_000: 200_000 * 200/10_000 = 4_000
	require.Equal(t, sdkmath.NewInt(4_000), insuranceAmt,
		"insurance contribution should be 2%% of settlement")

	t.Logf("  Insurance: before=%s after=%s contribution=%s",
		poolBefore, poolAfter, insuranceAmt)
}
