package keeper

import (
	"context"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	workflowtypes "github.com/LumeraProtocol/lumera/x/workflows/types"
)

// CreditsWorkflowKeeper is the x/credits keeper surface needed by invoke_workflow.
type CreditsWorkflowKeeper interface {
	LockCredits(ctx context.Context, routerAddr string, sessionID string, amount sdk.Coin, toolID string, quoteID string, policyVersion string, intentHash string, toolpackID ...string) (string, error)
	SettleLock(ctx context.Context, lockID string, actualCost sdk.Coin, receipt creditskeeper.SettlementRequest) (*creditskeeper.SettlementResult, error)
	UnlockCredits(ctx context.Context, lockID string, reason string) error
}

// CreditsWorkflowLedgerOptions carries router/session metadata used by the credits ledger.
type CreditsWorkflowLedgerOptions struct {
	RouterAddr    string
	SessionID     string
	PolicyVersion string
	IntentHash    string
	ToolpackID    string
	ToolID        string

	PublisherAddr sdk.AccAddress
	RouterAddrSDK sdk.AccAddress
	ReferrerAddr  sdk.AccAddress

	PublisherID string
	RouterID    string
	ReferrerID  string
	UserID      string

	CacheHit     bool
	OriginToolID string
	OriginID     string
}

// CreditsWorkflowLedger adapts workflow bundle invocations to x/credits lock settlement.
type CreditsWorkflowLedger struct {
	credits CreditsWorkflowKeeper
	options CreditsWorkflowLedgerOptions
}

var _ WorkflowCreditsLedger = (*CreditsWorkflowLedger)(nil)

// NewCreditsWorkflowLedger creates a real x/credits-backed workflow ledger.
func NewCreditsWorkflowLedger(credits CreditsWorkflowKeeper, options CreditsWorkflowLedgerOptions) (*CreditsWorkflowLedger, error) {
	if credits == nil {
		return nil, workflowtypes.ErrInvalidWorkflow.Wrap("credits keeper is required")
	}
	if strings.TrimSpace(options.RouterAddr) == "" {
		return nil, workflowtypes.ErrInvalidWorkflow.Wrap("router address is required")
	}
	return &CreditsWorkflowLedger{credits: credits, options: options}, nil
}

// LockWorkflowBundle locks the bundle ceiling against the quote bundle_id.
func (l *CreditsWorkflowLedger) LockWorkflowBundle(ctx context.Context, quote *workflowtypes.BundleQuote) (string, error) {
	if l == nil || l.credits == nil {
		return "", workflowtypes.ErrInvalidWorkflow.Wrap("credits workflow ledger is not initialized")
	}
	if quote == nil {
		return "", workflowtypes.ErrInvalidWorkflow.Wrap("bundle quote is required")
	}
	amount, err := quoteCoinToSDKCoin(quote.TotalMaxCost)
	if err != nil {
		return "", err
	}
	toolID := l.toolID(quote.WorkflowID)
	if toolID == "" {
		return "", workflowtypes.ErrInvalidWorkflow.Wrap("workflow tool_id is required")
	}
	toolpacks := make([]string, 0, 1)
	if toolpackID := strings.TrimSpace(l.options.ToolpackID); toolpackID != "" {
		toolpacks = append(toolpacks, toolpackID)
	}
	return l.credits.LockCredits(
		ctx,
		strings.TrimSpace(l.options.RouterAddr),
		strings.TrimSpace(l.options.SessionID),
		amount,
		toolID,
		strings.TrimSpace(quote.BundleID),
		strings.TrimSpace(l.options.PolicyVersion),
		strings.TrimSpace(l.options.IntentHash),
		toolpacks...,
	)
}

// SettleWorkflowBundle settles the actual receipt cost and lets x/credits refund the unused lock.
func (l *CreditsWorkflowLedger) SettleWorkflowBundle(ctx context.Context, lockID string, receipt *workflowtypes.WorkflowInvocationReceipt) error {
	if l == nil || l.credits == nil {
		return workflowtypes.ErrInvalidWorkflow.Wrap("credits workflow ledger is not initialized")
	}
	lockID = strings.TrimSpace(lockID)
	if lockID == "" {
		return workflowtypes.ErrInvalidWorkflow.Wrap("lock_id is required")
	}
	if receipt == nil {
		return workflowtypes.ErrInvalidWorkflow.Wrap("workflow invocation receipt is required")
	}
	actualCost, err := quoteCoinToSDKCoin(receipt.TotalCost)
	if err != nil {
		return err
	}
	_, err = l.credits.SettleLock(ctx, lockID, actualCost, l.settlementRequest(lockID, receipt))
	return err
}

// RevertWorkflowBundle unlocks the entire bundle lock without settlement.
func (l *CreditsWorkflowLedger) RevertWorkflowBundle(ctx context.Context, lockID string, receipt *workflowtypes.WorkflowInvocationReceipt) error {
	if l == nil || l.credits == nil {
		return workflowtypes.ErrInvalidWorkflow.Wrap("credits workflow ledger is not initialized")
	}
	lockID = strings.TrimSpace(lockID)
	if lockID == "" {
		return workflowtypes.ErrInvalidWorkflow.Wrap("lock_id is required")
	}
	return l.credits.UnlockCredits(ctx, lockID, workflowRevertReason(receipt))
}

func (l *CreditsWorkflowLedger) settlementRequest(lockID string, receipt *workflowtypes.WorkflowInvocationReceipt) creditskeeper.SettlementRequest {
	routerAddr := l.options.RouterAddrSDK
	routerID := strings.TrimSpace(l.options.RouterID)
	if routerID == "" {
		routerID = strings.TrimSpace(l.options.RouterAddr)
	}
	userID := strings.TrimSpace(l.options.UserID)
	if userID == "" {
		userID = routerID
	}
	publisherID := strings.TrimSpace(l.options.PublisherID)
	if publisherID == "" && l.options.PublisherAddr != nil {
		publisherID = l.options.PublisherAddr.String()
	}
	referrerID := strings.TrimSpace(l.options.ReferrerID)
	if referrerID == "" && l.options.ReferrerAddr != nil {
		referrerID = l.options.ReferrerAddr.String()
	}
	return creditskeeper.SettlementRequest{
		ReceiptID:      strings.TrimSpace(receipt.BundleID),
		ToolID:         l.toolID(receipt.WorkflowID),
		PublisherAddr:  l.options.PublisherAddr,
		RouterAddr:     routerAddr,
		ReferrerAddr:   l.options.ReferrerAddr,
		CacheHit:       l.options.CacheHit,
		OriginToolID:   strings.TrimSpace(l.options.OriginToolID),
		OriginID:       strings.TrimSpace(l.options.OriginID),
		PublisherID:    publisherID,
		UserID:         userID,
		RouterID:       routerID,
		ReferrerID:     referrerID,
		PolicySnapshot: strings.TrimSpace(l.options.PolicyVersion),
		ToolpackID:     strings.TrimSpace(l.options.ToolpackID),
		ActionID:       strings.TrimSpace(receipt.BundleID),
		Stage:          "finalized",
		SessionID:      strings.TrimSpace(l.options.SessionID),
		QuoteID:        strings.TrimSpace(receipt.BundleID),
		LockID:         lockID,
		IntentHash:     strings.TrimSpace(l.options.IntentHash),
	}
}

func (l *CreditsWorkflowLedger) toolID(fallback string) string {
	if toolID := strings.TrimSpace(l.options.ToolID); toolID != "" {
		return toolID
	}
	return strings.TrimSpace(fallback)
}

func quoteCoinToSDKCoin(coin workflowtypes.QuoteCoin) (sdk.Coin, error) {
	normalized, err := workflowtypes.NewQuoteCoin(coin.Denom, coin.Amount)
	if err != nil {
		return sdk.Coin{}, err
	}
	amount, ok := sdkmath.NewIntFromString(normalized.Amount)
	if !ok {
		return sdk.Coin{}, workflowtypes.ErrInvalidWorkflow.Wrap("quote coin amount must be an integer")
	}
	out := sdk.Coin{Denom: normalized.Denom, Amount: amount}
	if err := out.Validate(); err != nil {
		return sdk.Coin{}, workflowtypes.ErrInvalidWorkflow.Wrapf("invalid credits coin: %v", err)
	}
	return out, nil
}

func workflowRevertReason(receipt *workflowtypes.WorkflowInvocationReceipt) string {
	if receipt == nil {
		return "workflow_reverted"
	}
	code := strings.TrimSpace(receipt.FailureCode)
	if code == "" {
		return "workflow_reverted"
	}
	return "workflow_reverted:" + code
}
