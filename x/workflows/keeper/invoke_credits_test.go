package keeper

import (
	"bytes"
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	workflowtypes "github.com/LumeraProtocol/lumera/x/workflows/types"
)

func TestCreditsWorkflowLedger_LockSettleAndRevertMapping(t *testing.T) {
	ctx := context.Background()
	credits := &fakeCreditsWorkflowKeeper{lockID: "lock-123"}
	publisher := testWorkflowCreditsAddr(1)
	router := testWorkflowCreditsAddr(2)
	referrer := testWorkflowCreditsAddr(3)
	ledger, err := NewCreditsWorkflowLedger(credits, CreditsWorkflowLedgerOptions{
		RouterAddr:    router.String(),
		SessionID:     "session-1",
		PolicyVersion: "policy@1",
		IntentHash:    "intent-hash",
		ToolpackID:    "pack-1",
		ToolID:        "workflow:wf-hedge@1.0.0",
		PublisherAddr: publisher,
		RouterAddrSDK: router,
		ReferrerAddr:  referrer,
		CacheHit:      true,
		OriginToolID:  "tool.origin",
		OriginID:      "registry:origin",
	})
	require.NoError(t, err)

	quote := &workflowtypes.BundleQuote{
		BundleID:     "bundle-123",
		WorkflowID:   "wf-hedge",
		Version:      "1.0.0",
		TotalMaxCost: mustWorkflowQuoteCoin(t, "ulac", "125000"),
	}
	lockID, err := ledger.LockWorkflowBundle(ctx, quote)
	require.NoError(t, err)
	require.Equal(t, "lock-123", lockID)
	require.Equal(t, router.String(), credits.lockRouter)
	require.Equal(t, "session-1", credits.lockSession)
	require.Equal(t, sdk.NewInt64Coin("ulac", 125000), credits.lockAmount)
	require.Equal(t, "workflow:wf-hedge@1.0.0", credits.lockToolID)
	require.Equal(t, "bundle-123", credits.lockQuoteID)
	require.Equal(t, "policy@1", credits.lockPolicy)
	require.Equal(t, "intent-hash", credits.lockIntent)
	require.Equal(t, []string{"pack-1"}, credits.lockToolpacks)

	receipt := &workflowtypes.WorkflowInvocationReceipt{
		BundleID:   "bundle-123",
		WorkflowID: "wf-hedge",
		Version:    "1.0.0",
		Outcome:    workflowtypes.WorkflowOutcomePartialSkip,
		TotalCost:  mustWorkflowQuoteCoin(t, "ulac", "118000"),
	}
	require.NoError(t, ledger.SettleWorkflowBundle(ctx, lockID, receipt))
	require.Equal(t, "lock-123", credits.settleLockID)
	require.Equal(t, sdk.NewInt64Coin("ulac", 118000), credits.settleActualCost)
	require.Equal(t, "bundle-123", credits.settleReceipt.ReceiptID)
	require.Equal(t, "workflow:wf-hedge@1.0.0", credits.settleReceipt.ToolID)
	require.Equal(t, publisher, credits.settleReceipt.PublisherAddr)
	require.Equal(t, router, credits.settleReceipt.RouterAddr)
	require.Equal(t, referrer, credits.settleReceipt.ReferrerAddr)
	require.Equal(t, publisher.String(), credits.settleReceipt.PublisherID)
	require.Equal(t, router.String(), credits.settleReceipt.RouterID)
	require.Equal(t, router.String(), credits.settleReceipt.UserID)
	require.Equal(t, referrer.String(), credits.settleReceipt.ReferrerID)
	require.Equal(t, "policy@1", credits.settleReceipt.PolicySnapshot)
	require.Equal(t, "pack-1", credits.settleReceipt.ToolpackID)
	require.Equal(t, "bundle-123", credits.settleReceipt.ActionID)
	require.Equal(t, "finalized", credits.settleReceipt.Stage)
	require.Equal(t, "session-1", credits.settleReceipt.SessionID)
	require.Equal(t, "bundle-123", credits.settleReceipt.QuoteID)
	require.Equal(t, "lock-123", credits.settleReceipt.LockID)
	require.Equal(t, "intent-hash", credits.settleReceipt.IntentHash)
	require.True(t, credits.settleReceipt.CacheHit)
	require.Equal(t, "tool.origin", credits.settleReceipt.OriginToolID)
	require.Equal(t, "registry:origin", credits.settleReceipt.OriginID)

	reverted := &workflowtypes.WorkflowInvocationReceipt{FailureCode: "publisher_down"}
	require.NoError(t, ledger.RevertWorkflowBundle(ctx, lockID, reverted))
	require.Equal(t, "lock-123", credits.unlockLockID)
	require.Equal(t, "workflow_reverted:publisher_down", credits.unlockReason)
}

func TestCreditsWorkflowLedger_DefaultToolIDAndValidation(t *testing.T) {
	credits := &fakeCreditsWorkflowKeeper{lockID: "lock-456"}
	ledger, err := NewCreditsWorkflowLedger(credits, CreditsWorkflowLedgerOptions{
		RouterAddr: "lumera1router",
	})
	require.NoError(t, err)

	_, err = ledger.LockWorkflowBundle(context.Background(), &workflowtypes.BundleQuote{
		BundleID:     "bundle-bad",
		WorkflowID:   "wf-default",
		TotalMaxCost: workflowtypes.QuoteCoin{Denom: "ulac", Amount: "not-an-int"},
	})
	require.ErrorContains(t, err, "quote coin amount")
	require.Equal(t, 0, credits.lockCalls)

	quote := &workflowtypes.BundleQuote{
		BundleID:     "bundle-default",
		WorkflowID:   "wf-default",
		TotalMaxCost: mustWorkflowQuoteCoin(t, "ulac", "10"),
	}
	lockID, err := ledger.LockWorkflowBundle(context.Background(), quote)
	require.NoError(t, err)
	require.Equal(t, "lock-456", lockID)
	require.Equal(t, "wf-default", credits.lockToolID)
}

func TestNewCreditsWorkflowLedgerRequiresCreditsAndRouter(t *testing.T) {
	_, err := NewCreditsWorkflowLedger(nil, CreditsWorkflowLedgerOptions{RouterAddr: "lumera1router"})
	require.ErrorContains(t, err, "credits keeper is required")

	_, err = NewCreditsWorkflowLedger(&fakeCreditsWorkflowKeeper{}, CreditsWorkflowLedgerOptions{})
	require.ErrorContains(t, err, "router address is required")
}

type fakeCreditsWorkflowKeeper struct {
	lockID string

	lockCalls     int
	lockRouter    string
	lockSession   string
	lockAmount    sdk.Coin
	lockToolID    string
	lockQuoteID   string
	lockPolicy    string
	lockIntent    string
	lockToolpacks []string

	settleLockID     string
	settleActualCost sdk.Coin
	settleReceipt    creditskeeper.SettlementRequest

	unlockLockID string
	unlockReason string
}

func (f *fakeCreditsWorkflowKeeper) LockCredits(_ context.Context, routerAddr string, sessionID string, amount sdk.Coin, toolID string, quoteID string, policyVersion string, intentHash string, toolpackID ...string) (string, error) {
	f.lockCalls++
	f.lockRouter = routerAddr
	f.lockSession = sessionID
	f.lockAmount = amount
	f.lockToolID = toolID
	f.lockQuoteID = quoteID
	f.lockPolicy = policyVersion
	f.lockIntent = intentHash
	f.lockToolpacks = append([]string(nil), toolpackID...)
	return f.lockID, nil
}

func (f *fakeCreditsWorkflowKeeper) SettleLock(_ context.Context, lockID string, actualCost sdk.Coin, receipt creditskeeper.SettlementRequest) (*creditskeeper.SettlementResult, error) {
	f.settleLockID = lockID
	f.settleActualCost = actualCost
	f.settleReceipt = receipt
	return &creditskeeper.SettlementResult{}, nil
}

func (f *fakeCreditsWorkflowKeeper) UnlockCredits(_ context.Context, lockID string, reason string) error {
	f.unlockLockID = lockID
	f.unlockReason = reason
	return nil
}

func testWorkflowCreditsAddr(seed byte) sdk.AccAddress {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20))
}

func mustWorkflowQuoteCoin(t *testing.T, denom string, amount string) workflowtypes.QuoteCoin {
	t.Helper()
	coin, err := workflowtypes.NewQuoteCoin(denom, amount)
	require.NoError(t, err)
	return coin
}
