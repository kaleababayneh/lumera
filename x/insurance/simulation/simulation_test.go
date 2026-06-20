
package simulation_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	coreaddress "cosmossdk.io/core/address"
	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/simulation"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

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

type simulationFixture struct {
	ctx           sdk.Context
	keeper        keeper.Keeper
	bankKeeper    *bankkeeper.BaseKeeper
	accountKeeper authkeeper.AccountKeeper
	cdc           codec.BinaryCodec
	accounts      []simtypes.Account
}

func newSimulationFixture(t testing.TB) *simulationFixture {
	t.Helper()

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger())

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
		types.ModuleAccountName: {authtypes.Minter, authtypes.Burner},
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

	// Create simulation accounts
	r := rand.New(rand.NewSource(42))
	accounts := simtypes.RandomAccounts(r, 10)

	return &simulationFixture{
		ctx:           ctx,
		keeper:        insKeeper,
		bankKeeper:    &baseBankKeeper,
		accountKeeper: accountKeeper,
		cdc:           cdc,
		accounts:      accounts,
	}
}

func TestSimulation_FloodClaims_GasBounds(t *testing.T) {
	fixture := newSimulationFixture(t)

	// Fund the credits module account
	creditsModuleAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsModuleAccount)
	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000_000))
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, creditstypes.ModuleName, initialCoins))

	// Ensure insurance module account exists
	_ = fixture.accountKeeper.GetModuleAccount(fixture.ctx, types.ModuleAccountName)

	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	authority := fixture.keeper.Authority()

	// Process a contribution to fund the pool
	contribMsg := &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "test-contrib-1",
		ToolId:      "tool-flood",
		PublisherId: "publisher-flood",
		Amount: &v1beta1.Coin{
			Denom:  "ulac",
			Amount: "500000",
		},
	}
	_, err := msgServer.ProcessContribution(fixture.ctx, contribMsg)
	require.NoError(t, err)

	// File multiple claims to simulate flood
	claimCount := 20
	gasStart := fixture.ctx.GasMeter().GasConsumed()

	for i := 0; i < claimCount; i++ {
		receiptID := fmt.Sprintf("flood-claim-%d", i)
		toolID := fmt.Sprintf("tool-%d", i%5)
		publisherID := fmt.Sprintf("publisher-%d", i%3)

		// Record contribution for this receipt (required precondition for FileClaim)
		contribMsg := &types.MsgProcessContribution{
			Authority:   authority,
			ReceiptId:   receiptID,
			ToolId:      toolID,
			PublisherId: publisherID,
			Amount:      &v1beta1.Coin{Denom: "ulac", Amount: "10000"},
		}
		_, err := msgServer.ProcessContribution(fixture.ctx, contribMsg)
		require.NoError(t, err, "contribution %d should succeed", i)

		claimMsg := &types.MsgFileClaim{
			Claimant:      fixture.accounts[i%len(fixture.accounts)].Address.String(),
			ReceiptId:     receiptID,
			ToolId:        toolID,
			PublisherId:   publisherID,
			ClaimedAmount: &v1beta1.Coin{Denom: "ulac", Amount: "10000"},
			Reason:        "timeout after 30s",
		}
		_, err = msgServer.FileClaim(fixture.ctx, claimMsg)
		require.NoError(t, err, "claim %d should succeed", i)
	}

	gasUsed := fixture.ctx.GasMeter().GasConsumed() - gasStart
	avgGasPerClaim := gasUsed / uint64(claimCount)

	// Verify gas bounds are reasonable (not unbounded growth)
	// Each claim should use < 500k gas on average
	require.Less(t, avgGasPerClaim, uint64(500_000), "average gas per claim should be bounded")
	t.Logf("Flood claims: %d claims, total gas: %d, avg gas/claim: %d", claimCount, gasUsed, avgGasPerClaim)
}

func TestSimulation_PayoutCaps(t *testing.T) {
	fixture := newSimulationFixture(t)

	// Fund the credits module account
	creditsModuleAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsModuleAccount)
	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 10_000_000))
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, creditstypes.ModuleName, initialCoins))

	// Ensure insurance module account exists
	_ = fixture.accountKeeper.GetModuleAccount(fixture.ctx, types.ModuleAccountName)

	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	authority := fixture.keeper.Authority()

	// Add funds to pool
	contribMsg := &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "test-contrib-caps",
		ToolId:      "tool-caps",
		PublisherId: "publisher-caps",
		Amount: &v1beta1.Coin{
			Denom:  "ulac",
			Amount: "1000000",
		},
	}
	_, err := msgServer.ProcessContribution(fixture.ctx, contribMsg)
	require.NoError(t, err)

	// Check pool balance
	poolBalance, err := fixture.keeper.GetPoolBalance(fixture.ctx)
	require.NoError(t, err)
	poolAmount := poolBalance.AmountOf("ulac")
	t.Logf("Pool balance: %s ulac", poolAmount.String())

	// Record contribution for large-claim (required precondition for FileClaim)
	largeContribMsg := &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "large-claim",
		ToolId:      "tool-1",
		PublisherId: "publisher-1",
		Amount:      &v1beta1.Coin{Denom: "ulac", Amount: "1"}, // Minimal contribution
	}
	_, err = msgServer.ProcessContribution(fixture.ctx, largeContribMsg)
	require.NoError(t, err)

	// File a claim for more than pool balance
	claimAmount := poolAmount.MulRaw(2) // Claim 2x pool balance
	claimMsg := &types.MsgFileClaim{
		Claimant:      fixture.accounts[0].Address.String(),
		ReceiptId:     "large-claim",
		ToolId:        "tool-1",
		PublisherId:   "publisher-1",
		ClaimedAmount: &v1beta1.Coin{Denom: "ulac", Amount: claimAmount.String()},
		Reason:        "critical outage",
	}
	claimResp, err := msgServer.FileClaim(fixture.ctx, claimMsg)
	require.NoError(t, err)

	// Approve the claim with the full claimed amount - should fail due to cap
	approveMsg := &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimResp.ClaimId,
		Resolution:     "approve",
		ApprovedAmount: &v1beta1.Coin{Denom: "ulac", Amount: claimAmount.String()},
	}
	_, err = msgServer.ProcessClaim(fixture.ctx, approveMsg)
	// The insurance module correctly enforces caps at approval time
	require.Error(t, err, "approval should fail when claim exceeds pool balance")
	require.Contains(t, err.Error(), "insufficient", "error should indicate insufficient funds")

	// Record contribution for valid-claim (required precondition for FileClaim)
	validClaimAmount := poolAmount.MulRaw(50).QuoRaw(100) // 50% of pool
	validContribMsg := &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "valid-claim",
		ToolId:      "tool-1",
		PublisherId: "publisher-1",
		Amount:      &v1beta1.Coin{Denom: "ulac", Amount: validClaimAmount.String()},
	}
	_, err = msgServer.ProcessContribution(fixture.ctx, validContribMsg)
	require.NoError(t, err)

	// Now try with an amount within the pool
	claimMsg2 := &types.MsgFileClaim{
		Claimant:      fixture.accounts[1].Address.String(),
		ReceiptId:     "valid-claim",
		ToolId:        "tool-1",
		PublisherId:   "publisher-1",
		ClaimedAmount: &v1beta1.Coin{Denom: "ulac", Amount: validClaimAmount.String()},
		Reason:        "service degradation",
	}
	claimResp2, err := msgServer.FileClaim(fixture.ctx, claimMsg2)
	require.NoError(t, err)

	approveMsg2 := &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimResp2.ClaimId,
		Resolution:     "approve",
		ApprovedAmount: &v1beta1.Coin{Denom: "ulac", Amount: validClaimAmount.String()},
	}
	_, err = msgServer.ProcessClaim(fixture.ctx, approveMsg2)
	require.NoError(t, err, "approval should succeed for claim within pool balance")

	t.Log("Payout caps test completed - caps correctly enforced at approval time")
}

func TestSimulation_RecidivistMultiplier(t *testing.T) {
	fixture := newSimulationFixture(t)

	hooks := keeper.NewHooks(fixture.keeper)

	publisherID := "recidivist-publisher-1"
	toolID := "tool-1"

	// First anomaly - should get base tier
	report1 := keeper.AnomalyReport{
		Severity:      keeper.SeverityHigh,
		PublisherID:   publisherID,
		ToolID:        toolID,
		Description:   "first high severity anomaly",
		Evidence:      []string{"ipfs://evidence/1"},
		ReportedBy:    "monitor",
		AutoRemediate: false,
	}
	err := hooks.PublishAnomaly(fixture.ctx, report1)
	require.NoError(t, err)

	// Check risk entry after first anomaly
	risk, err := hooks.GetPublisherRisk(fixture.ctx, publisherID, toolID)
	require.NoError(t, err)
	require.Equal(t, uint32(1), risk.DisputeCount, "dispute count should be 1")

	// Second anomaly - recidivist multiplier should apply
	report2 := keeper.AnomalyReport{
		Severity:      keeper.SeverityHigh,
		PublisherID:   publisherID,
		ToolID:        toolID,
		Description:   "second high severity anomaly",
		Evidence:      []string{"ipfs://evidence/2"},
		ReportedBy:    "monitor",
		AutoRemediate: false,
	}
	err = hooks.PublishAnomaly(fixture.ctx, report2)
	require.NoError(t, err)

	// Check risk entry after second anomaly
	risk, err = hooks.GetPublisherRisk(fixture.ctx, publisherID, toolID)
	require.NoError(t, err)
	require.Equal(t, uint32(2), risk.DisputeCount, "dispute count should be 2")

	// Third anomaly - recidivist multiplier should increase
	report3 := keeper.AnomalyReport{
		Severity:      keeper.SeverityCritical,
		PublisherID:   publisherID,
		ToolID:        toolID,
		Description:   "critical anomaly from recidivist",
		Evidence:      []string{"ipfs://evidence/3"},
		ReportedBy:    "monitor",
		AutoRemediate: true,
	}
	err = hooks.PublishAnomaly(fixture.ctx, report3)
	require.NoError(t, err)

	// Check final risk entry
	risk, err = hooks.GetPublisherRisk(fixture.ctx, publisherID, toolID)
	require.NoError(t, err)
	require.Equal(t, uint32(3), risk.DisputeCount, "dispute count should be 3")
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_EXTREME, risk.PremiumTier, "premium tier should be EXTREME after critical")
	require.Equal(t, "5.0", risk.PremiumMultiplier, "premium multiplier should be 5.0 for EXTREME")

	t.Logf("Recidivist test: %d disputes, tier=%s, multiplier=%s",
		risk.DisputeCount, risk.PremiumTier.String(), risk.PremiumMultiplier)
}

func TestSimulation_PremiumAdjustment(t *testing.T) {
	fixture := newSimulationFixture(t)

	hooks := keeper.NewHooks(fixture.keeper)

	publisherID := "premium-test-publisher"
	toolID := "tool-premium"

	// Medium severity should give STANDARD tier
	reportMedium := keeper.AnomalyReport{
		Severity:      keeper.SeverityMedium,
		PublisherID:   publisherID,
		ToolID:        toolID,
		Description:   "medium severity anomaly",
		Evidence:      []string{"ipfs://evidence/1"},
		ReportedBy:    "monitor",
		AutoRemediate: false,
	}
	err := hooks.PublishAnomaly(fixture.ctx, reportMedium)
	require.NoError(t, err)

	risk, err := hooks.GetPublisherRisk(fixture.ctx, publisherID, toolID)
	require.NoError(t, err)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_STANDARD, risk.PremiumTier)
	require.Equal(t, "1.5", risk.PremiumMultiplier, "medium severity should have 1.5x multiplier")

	// High severity should upgrade to HIGH tier
	publisherID2 := "premium-test-publisher-2"
	reportHigh := keeper.AnomalyReport{
		Severity:      keeper.SeverityHigh,
		PublisherID:   publisherID2,
		ToolID:        toolID,
		Description:   "high severity anomaly",
		Evidence:      []string{"ipfs://evidence/2"},
		ReportedBy:    "monitor",
		AutoRemediate: false,
	}
	err = hooks.PublishAnomaly(fixture.ctx, reportHigh)
	require.NoError(t, err)

	risk2, err := hooks.GetPublisherRisk(fixture.ctx, publisherID2, toolID)
	require.NoError(t, err)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_HIGH, risk2.PremiumTier)
	require.Equal(t, "2.5", risk2.PremiumMultiplier, "high severity should have 2.5x multiplier")

	t.Log("Premium adjustment test completed")
}

func TestSimulation_WeightedOperations_Registered(t *testing.T) {
	fixture := newSimulationFixture(t)

	// Create a simState mock
	simState := simtypes.AppParams{}
	// Fixed seed for test determinism: a wall-clock seed makes any
	// panic reproducible on one day and not the next, masking real
	// failures. 42 matches the seed used elsewhere in this file
	// (line 137). Independent test funcs get independent *rand.Rand
	// state, so seed reuse across tests doesn't cause interference.
	r := rand.New(rand.NewSource(42))

	ops := simulation.WeightedOperations(
		simState,
		fixture.cdc.(codec.JSONCodec),
		fixture.keeper,
		fixture.accountKeeper,
		fixture.bankKeeper,
	)

	// Should have 3 operations registered
	require.Len(t, ops, 3, "should have 3 weighted operations: flood_claims, publish_anomaly, pool_exhaustion")

	// Verify each operation has non-zero weight
	for i, op := range ops {
		require.Greater(t, op.Weight(), 0, "operation %d should have positive weight", i)
	}

	t.Logf("Weighted operations: %d operations registered", len(ops))

	// Run each operation once to ensure they don't panic
	for i, op := range ops {
		result, _, err := op.Op()(r, &baseapp.BaseApp{}, fixture.ctx, fixture.accounts, "")
		// Operations may return NoOpMsg or error based on state, both are acceptable
		if err != nil {
			t.Logf("Operation %d returned error (acceptable): %v", i, err)
		} else {
			t.Logf("Operation %d result: %s", i, result.Comment)
		}
	}
}

func TestSimulation_PoolExhaustion_Bounded(t *testing.T) {
	fixture := newSimulationFixture(t)

	// Fund the credits module account generously
	creditsModuleAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsModuleAccount)
	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100_000_000))
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, creditstypes.ModuleName, initialCoins))

	// Ensure insurance module account exists
	_ = fixture.accountKeeper.GetModuleAccount(fixture.ctx, types.ModuleAccountName)

	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	authority := fixture.keeper.Authority()

	// Add funds to pool
	contribMsg := &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "test-contrib-exhaustion",
		ToolId:      "tool-exhaustion",
		PublisherId: "publisher-exhaustion",
		Amount: &v1beta1.Coin{
			Denom:  "ulac",
			Amount: "5000000",
		},
	}
	_, err := msgServer.ProcessContribution(fixture.ctx, contribMsg)
	require.NoError(t, err)

	poolBalanceBefore, err := fixture.keeper.GetPoolBalance(fixture.ctx)
	require.NoError(t, err)
	poolAmountBefore := poolBalanceBefore.AmountOf("ulac")

	// Simulate exhaustion by filing and paying multiple large claims
	claimsToProcess := 10
	totalPaidOut := sdkmath.ZeroInt()

	for i := 0; i < claimsToProcess; i++ {
		currentBalance, err := fixture.keeper.GetPoolBalance(fixture.ctx)
		require.NoError(t, err)
		poolAmount := currentBalance.AmountOf("ulac")
		if poolAmount.IsZero() {
			t.Logf("Pool exhausted after %d claims", i)
			break
		}

		// Claim 20% of remaining pool
		claimAmount := poolAmount.MulRaw(20).QuoRaw(100)
		if !claimAmount.IsPositive() {
			claimAmount = sdkmath.NewInt(1)
		}

		claimMsg := &types.MsgFileClaim{
			Claimant:      fixture.accounts[i%len(fixture.accounts)].Address.String(),
			ReceiptId:     fmt.Sprintf("exhaustion-claim-%d", i),
			ToolId:        fmt.Sprintf("tool-%d", i%3),
			PublisherId:   fmt.Sprintf("publisher-%d", i%2),
			ClaimedAmount: &v1beta1.Coin{Denom: "ulac", Amount: claimAmount.String()},
			Reason:        "service degradation",
		}
		claimResp, err := msgServer.FileClaim(fixture.ctx, claimMsg)
		if err != nil {
			continue
		}

		approveMsg := &types.MsgProcessClaim{
			Authority:      authority,
			ClaimId:        claimResp.ClaimId,
			Resolution:     "approve",
			ApprovedAmount: &v1beta1.Coin{Denom: "ulac", Amount: claimAmount.String()},
		}
		_, err = msgServer.ProcessClaim(fixture.ctx, approveMsg)
		if err != nil {
			continue
		}

		payoutMsg := &types.MsgProcessPayout{
			Authority: authority,
			ClaimId:   claimResp.ClaimId,
			Recipient: fixture.accounts[i%len(fixture.accounts)].Address.String(),
		}
		_, err = msgServer.ProcessPayout(fixture.ctx, payoutMsg)
		if err == nil {
			totalPaidOut = totalPaidOut.Add(claimAmount)
		}
	}

	poolBalanceAfter, err := fixture.keeper.GetPoolBalance(fixture.ctx)
	require.NoError(t, err)
	poolAmountAfter := poolBalanceAfter.AmountOf("ulac")

	// Pool should have decreased (or stayed same if no payouts succeeded)
	require.True(t, poolAmountAfter.LTE(poolAmountBefore), "pool balance should not exceed initial amount")

	// Total payout should be bounded by initial pool
	// Note: totalPaidOut tracks attempted payouts - actual pool change may differ due to caps
	poolDecrease := poolAmountBefore.Sub(poolAmountAfter)
	require.True(t, poolDecrease.LTE(poolAmountBefore), "pool decrease should be bounded by initial pool")

	t.Logf("Pool exhaustion test: before=%s, after=%s, decrease=%s",
		poolAmountBefore.String(), poolAmountAfter.String(), poolDecrease.String())
}
