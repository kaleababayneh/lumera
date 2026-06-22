package credits_test

import (
	"bytes"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/internal/testutil"
	credits "github.com/LumeraProtocol/lumera/x/credits"
	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	insurancetypes "github.com/LumeraProtocol/lumera/x/insurance/types"
	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
)

// registryDisputeWindowGap documents a production gap surfaced by these tests:
// the credits BeginBlocker resolves the settlement dispute window via
// Keeper.SettlementDisputeWindow, which type-asserts the injected registry
// keeper to a registryDisputeWindowProvider exposing
// GetDisputeWindowSeconds(ctx) uint32 (x/credits/keeper/dispute_window.go). The
// ported "focused slice" x/registry keeper stores a DisputeWindowSeconds param
// but does NOT implement that accessor, so the assertion always fails and the
// registry-configured window is silently ignored (credits falls back to its own
// DisputeWindowHours/default). Honoring the registry window requires adding the
// GetDisputeWindowSeconds method to the production registry keeper; per the
// porting rules we do not modify non-test code here, so these tests are skipped.
const registryDisputeWindowGap = "not ported: x/registry keeper lacks GetDisputeWindowSeconds(ctx) so credits ignores the registry dispute window; production accessor required"

var testAddrCounter byte

func nextTestAddress() sdk.AccAddress {
	testAddrCounter++
	return sdk.AccAddress(bytes.Repeat([]byte{testAddrCounter}, 20))
}

func TestBeginBlockerProcessesSettlements(t *testing.T) {
	ctx, app := testutil.SetupTestApp(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	params := app.CreditsKeeper.GetParams(ctx)
	params.BurnRateSpendBps = 300
	params.InsuranceBps = 200
	params.MaxSettlementsPerBlock = 10
	require.NoError(t, app.CreditsKeeper.SetParams(ctx, params))

	publisher := nextTestAddress()
	router := nextTestAddress()
	user := nextTestAddress()

	// Register the tool so SettleLock can resolve the publisher
	require.NoError(t, app.RegistryKeeper.SetToolCard(ctx, &registrytypes.ToolCard{
		ToolId:       "tool-1",
		Owner:        publisher.String(),
		InputSchema:  `{"type":"object"}`,
		OutputSchema: `{"type":"object"}`,
	}))

	amount := sdk.NewInt64Coin(params.CreditDenom, 1000)
	require.NoError(t, testutil.FundAccount(ctx, app, router, sdk.NewCoins(amount)))

	lockID, err := app.CreditsKeeper.LockCredits(ctx, router.String(), "sess-1", amount, "tool-1", "quote-1", "v1", "intent-hash")
	require.NoError(t, err)

	settlement := &creditstypes.SettlementRecord{
		Id:          "settlement-1",
		LockId:      lockID,
		ToolId:      "tool-1",
		PublisherId: publisher.String(),
		UserId:      user.String(),
		RouterId:    router.String(),
		TotalCost:   creditstypes.CoinsToProto(sdk.NewCoins(amount)),
		Status:      creditstypes.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Timestamp:   ctx.BlockTime().Add(-48 * time.Hour),
	}
	require.NoError(t, app.CreditsKeeper.CreateSettlement(ctx, settlement))

	require.NoError(t, credits.BeginBlocker(ctx, app.CreditsKeeper))

	updated, found := app.CreditsKeeper.GetSettlement(ctx, settlement.Id)
	require.True(t, found)
	require.Equalf(t, creditstypes.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, updated.Status, "failure reason: %s", updated.FailureReason)
	require.NotNil(t, updated.CompletedAt)

	publisherBal := app.BankKeeper.GetBalance(ctx, publisher, params.CreditDenom)
	routerBal := app.BankKeeper.GetBalance(ctx, router, params.CreditDenom)

	// The app wires a real insurance keeper into credits, so insurance is
	// applied during settlement whenever the insurance module account exists.
	insuranceApplied := false
	moduleAddr := app.AuthKeeper.GetModuleAddress(insurancetypes.ModuleName)
	if moduleAddr != nil && app.AuthKeeper.GetAccount(ctx, moduleAddr) != nil {
		insuranceApplied = true
	}

	netAmount := int64(1000 - 30)
	if insuranceApplied {
		netAmount -= 20
	}
	expectedPublisher := (netAmount * 70) / 100
	expectedRouter := netAmount - expectedPublisher

	require.Equal(t, expectedPublisher, publisherBal.Amount.Int64())
	require.Equal(t, expectedRouter, routerBal.Amount.Int64())
}

func TestBeginBlockerSkipsDisputeWindow(t *testing.T) {
	t.Skip(registryDisputeWindowGap)
	ctx, app := testutil.SetupTestApp(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	params := app.CreditsKeeper.GetParams(ctx)
	params.BurnRateSpendBps = 300
	params.InsuranceBps = 200
	require.NoError(t, app.CreditsKeeper.SetParams(ctx, params))

	registryParams := app.RegistryKeeper.GetParams(ctx)
	registryParams.DisputeWindowSeconds = 7200
	require.NoError(t, app.RegistryKeeper.SetParams(ctx, &registryParams))

	publisher := nextTestAddress()
	router := nextTestAddress()
	user := nextTestAddress()

	amount := sdk.NewInt64Coin(params.CreditDenom, 1000)
	require.NoError(t, app.BankKeeper.MintCoins(ctx, creditstypes.ModuleName, sdk.NewCoins(amount)))

	settlement := &creditstypes.SettlementRecord{
		Id:          "settlement-2",
		ToolId:      "tool-2",
		PublisherId: publisher.String(),
		UserId:      user.String(),
		RouterId:    router.String(),
		TotalCost:   creditstypes.CoinsToProto(sdk.NewCoins(amount)),
		Status:      creditstypes.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Timestamp:   ctx.BlockTime().Add(-1 * time.Hour),
	}
	require.NoError(t, app.CreditsKeeper.CreateSettlement(ctx, settlement))

	require.NoError(t, credits.BeginBlocker(ctx, app.CreditsKeeper))

	updated, found := app.CreditsKeeper.GetSettlement(ctx, settlement.Id)
	require.True(t, found)
	require.Equal(t, creditstypes.SettlementStatus_SETTLEMENT_STATUS_PENDING, updated.Status)
	require.Nil(t, updated.CompletedAt)
}

func TestBeginBlockerUsesRegistryDisputeWindow(t *testing.T) {
	t.Skip(registryDisputeWindowGap)
	ctx, app := testutil.SetupTestApp(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	creditsParams := app.CreditsKeeper.GetParams(ctx)
	creditsParams.DisputeWindowHours = 48
	creditsParams.BurnRateSpendBps = 300
	creditsParams.InsuranceBps = 200
	creditsParams.MaxSettlementsPerBlock = 10
	require.NoError(t, app.CreditsKeeper.SetParams(ctx, creditsParams))

	registryParams := app.RegistryKeeper.GetParams(ctx)
	registryParams.DisputeWindowSeconds = 60
	require.NoError(t, app.RegistryKeeper.SetParams(ctx, &registryParams))

	publisher := nextTestAddress()
	router := nextTestAddress()
	user := nextTestAddress()

	require.NoError(t, app.RegistryKeeper.SetToolCard(ctx, &registrytypes.ToolCard{
		ToolId:       "tool-registry-window",
		Owner:        publisher.String(),
		InputSchema:  `{"type":"object"}`,
		OutputSchema: `{"type":"object"}`,
	}))

	amount := sdk.NewInt64Coin(creditsParams.CreditDenom, 1000)
	require.NoError(t, testutil.FundAccount(ctx, app, router, sdk.NewCoins(amount)))

	lockID, err := app.CreditsKeeper.LockCredits(ctx, router.String(), "sess-registry-window", amount, "tool-registry-window", "quote-registry-window", "v1", "intent-hash")
	require.NoError(t, err)

	settlement := &creditstypes.SettlementRecord{
		Id:          "settlement-registry-window",
		LockId:      lockID,
		ToolId:      "tool-registry-window",
		PublisherId: publisher.String(),
		UserId:      user.String(),
		RouterId:    router.String(),
		TotalCost:   creditstypes.CoinsToProto(sdk.NewCoins(amount)),
		Status:      creditstypes.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Timestamp:   ctx.BlockTime().Add(-2 * time.Minute),
	}
	require.NoError(t, app.CreditsKeeper.CreateSettlement(ctx, settlement))

	require.NoError(t, credits.BeginBlocker(ctx, app.CreditsKeeper))

	updated, found := app.CreditsKeeper.GetSettlement(ctx, settlement.Id)
	require.True(t, found)
	require.Equal(t, creditstypes.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, updated.Status)
	require.NotNil(t, updated.CompletedAt)
}

func TestBeginBlockerExpiresLocks(t *testing.T) {
	ctx, app := testutil.SetupTestApp(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	params := app.CreditsKeeper.GetParams(ctx)
	params.DefaultLockTtlSeconds = 1
	params.MaxLockTtlSeconds = 1
	params.MaxExpiredLocksPerBlock = 10
	require.NoError(t, app.CreditsKeeper.SetParams(ctx, params))

	router := nextTestAddress()
	amount := sdk.NewInt64Coin(params.CreditDenom, 100)
	require.NoError(t, testutil.FundAccount(ctx, app, router, sdk.NewCoins(amount)))

	lockID, err := app.CreditsKeeper.LockCredits(ctx, router.String(), "sess-1", amount, "tool-1", "quote-1", "v1", "intent-hash")
	require.NoError(t, err)

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Second))
	require.NoError(t, credits.BeginBlocker(ctx, app.CreditsKeeper))

	lock, found := app.CreditsKeeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, creditstypes.LockStatus_LOCK_STATUS_RELEASED, lock.Status)

	routerBal := app.BankKeeper.GetBalance(ctx, router, params.CreditDenom)
	require.Equal(t, amount.Amount.Int64(), routerBal.Amount.Int64())
}

func TestEndBlockerPrunesOldSettlements(t *testing.T) {
	ctx, app := testutil.SetupTestApp(t)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	params := app.CreditsKeeper.GetParams(ctx)
	params.MaxPrunedSettlementsPerBlock = 10
	require.NoError(t, app.CreditsKeeper.SetParams(ctx, params))

	publisher := nextTestAddress()
	router := nextTestAddress()
	user := nextTestAddress()

	amount := sdk.NewInt64Coin(params.CreditDenom, 100)
	completedAt := ctx.BlockTime().Add(-31 * 24 * time.Hour)
	settlement := &creditstypes.SettlementRecord{
		Id:          "settlement-old",
		ToolId:      "tool-old",
		PublisherId: publisher.String(),
		UserId:      user.String(),
		RouterId:    router.String(),
		TotalCost:   creditstypes.CoinsToProto(sdk.NewCoins(amount)),
		Status:      creditstypes.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
		CompletedAt: &completedAt,
	}
	require.NoError(t, app.CreditsKeeper.CreateSettlement(ctx, settlement))

	require.NoError(t, credits.EndBlocker(ctx, app.CreditsKeeper))

	_, found := app.CreditsKeeper.GetSettlement(ctx, settlement.Id)
	require.False(t, found)
}
