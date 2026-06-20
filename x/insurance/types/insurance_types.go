package types

import (
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
)

// Pool status constants are now generated from protobuf
// See insurance.pb.go for PoolStatus enum

// ReceiptSettlementStatus tracks how a receipt progressed through the insurance workflow.
type ReceiptSettlementStatus string

// Settlement status string aliases used for legacy integrations.
const (
	SettlementStatusPending    ReceiptSettlementStatus = "pending"
	SettlementStatusSettled    ReceiptSettlementStatus = "settled"
	SettlementStatusChallenged ReceiptSettlementStatus = "challenged"
)

// Claim status constants are now generated from protobuf
// See insurance.pb.go for ClaimStatus enum

// Premium tier constants are now generated from protobuf
// See insurance.pb.go for PremiumTier enum

// Pool type is now replaced by PoolState in protobuf
// See insurance.pb.go for PoolState message

// Contribution type is now generated from protobuf
// See insurance.pb.go for Contribution message

// Claim type is now generated from protobuf
// See insurance.pb.go for Claim message

// Evidence type is now generated from protobuf
// See tx.pb.go for Evidence message

// PublisherRisk type is now generated from protobuf
// See insurance.pb.go for PublisherRisk message

// Payout type is now generated from protobuf
// See insurance.pb.go for Payout message

// RateLimitInfo provides current rate limit status for an address
type RateLimitInfo struct {
	Address       string
	CurrentCount  uint32
	MaxAllowed    uint32
	WindowStart   time.Time
	WindowEnd     time.Time
	RateLimitType string // "claim" or "contribution"
}

// Parameters represents configurable insurance parameters
type Parameters struct {
	InsurancePoolBPS     int             `json:"insurance_pool_bps"`     // Basis points for contribution
	TargetUtilization    decimal.Decimal `json:"target_utilization"`     // Target pool utilization (0.0-1.0)
	MinPoolBalance       decimal.Decimal `json:"min_pool_balance"`       // Minimum LAC in pool
	MaxClaimPercent      decimal.Decimal `json:"max_claim_percent"`      // Max claim as % of pool
	ClaimWindowSeconds   int             `json:"claim_window_seconds"`   // Time window to file claim
	DisputeStakeLAC      decimal.Decimal `json:"dispute_stake_lac"`      // Stake required to dispute
	PremiumAdjustmentBPS int             `json:"premium_adjustment_bps"` // Basis points for premium adjustments
	AutoApproveThreshold decimal.Decimal `json:"auto_approve_threshold"` // LAC amount for auto-approval
	SlashDecayDays       int             `json:"slash_decay_days"`       // Days for slash impact to decay
}

// Metrics represents insurance pool metrics for monitoring
type Metrics struct {
	TotalContributions24h decimal.Decimal `json:"total_contributions_24h"`
	TotalPayouts24h       decimal.Decimal `json:"total_payouts_24h"`
	PendingClaims         int             `json:"pending_claims"`
	AverageClaimAmount    decimal.Decimal `json:"average_claim_amount"`
	ClaimApprovalRate     decimal.Decimal `json:"claim_approval_rate"`
	PoolHealthScore       decimal.Decimal `json:"pool_health_score"` // 0-100
	RiskExposure          decimal.Decimal `json:"risk_exposure"`     // Total potential claims
	CoverageRatio         decimal.Decimal `json:"coverage_ratio"`    // Available funds / risk exposure
	UtilizationEWMA       decimal.Decimal `json:"utilization_ewma"`
	DisputeRateEWMA       decimal.Decimal `json:"dispute_rate_ewma"`
	Samples               uint64          `json:"samples"`
	LastAdjudicatedAt     *time.Time      `json:"last_adjudicated_at,omitempty"`
	LastPremiumAdjustAt   *time.Time      `json:"last_premium_adjust_at,omitempty"`
}

// ContributionRequest represents a request to contribute to the insurance pool
type ContributionRequest struct {
	RequestID     string          `json:"request_id"`
	ReceiptID     string          `json:"receipt_id"`
	ToolID        string          `json:"tool_id"`
	PublisherID   string          `json:"publisher_id"`
	InvokeAmount  decimal.Decimal `json:"invoke_amount"`
	PolicyVersion string          `json:"policy_version"`
}

// ClaimRequest represents a request to file an insurance claim
type ClaimRequest struct {
	RequestID      string          `json:"request_id"`
	ReceiptID      string          `json:"receipt_id"`
	ClaimantID     string          `json:"claimant_id"`
	ClaimedAmount  decimal.Decimal `json:"claimed_amount"`
	Reason         string          `json:"reason"`
	Evidence       []Evidence      `json:"evidence"`
	PolicySnapshot string          `json:"policy_snapshot"`
}

// PayoutRequest represents a request to process a payout
type PayoutRequest struct {
	ClaimID     string `json:"claim_id"`
	RecipientID string `json:"recipient_id"`
	Urgency     string `json:"urgency"` // "normal", "high", "critical"
}

// NewClaim constructs a pending claim protobuf message from the request payload.
func NewClaim(ctx sdk.Context, req ClaimRequest) (*Claim, error) {
	if req.ReceiptID == "" || req.ClaimantID == "" {
		return nil, ErrInvalidClaimRequest
	}

	if req.ClaimedAmount.IsNegative() || req.ClaimedAmount.IsZero() {
		return nil, ErrInvalidAmount
	}

	// Convert Evidence slice to pointer slice
	evidence := make([]*Evidence, len(req.Evidence))
	for i := range req.Evidence {
		evidence[i] = &req.Evidence[i]
	}

	// Convert decimal amount to Coin
	amountInt := req.ClaimedAmount.BigInt()
	claimedCoin := sdk.NewCoin("ulac", sdkmath.NewIntFromBigInt(amountInt))

	now := ctx.BlockTime()

	return &Claim{
		Id:             GenerateClaimID(ctx),
		ReceiptId:      req.ReceiptID,
		ClaimantId:     req.ClaimantID,
		ToolId:         "", // Should be set from receipt verification
		PublisherId:    "", // Should be set from receipt verification
		ClaimedAmount:  claimedCoin,
		Reason:         req.Reason,
		Evidence:       evidence,
		Status:         ClaimStatus_CLAIM_STATUS_PENDING,
		CreatedAt:      &now,
		UpdatedAt:      &now,
		PolicySnapshot: req.PolicySnapshot,
	}, nil
}

// GenerateClaimID generates a claim ID from the block timestamp.
//
// NOT the production path: the real FileClaim handler in
// x/insurance/keeper/claim_operations.go uses the keeper's
// ClaimCounter sequence (see claim_operations.go:88-92) to
// produce monotonically-increasing, collision-free IDs. This
// helper only has test callers today and is retained as a
// pure-function escape hatch for code paths that don't have a
// keeper handle available (e.g., synthesis of test claim IDs at
// the types-package level).
//
// Callers who DO hold a keeper reference MUST use
// ClaimCounter.Next(sdkCtx) instead — two calls to this helper
// within the same block produce identical IDs because they share
// the same BlockTime().UnixNano(), which would silently collapse
// claim records on insert.
func GenerateClaimID(ctx sdk.Context) string {
	timestamp := ctx.BlockTime().UnixNano()
	return fmt.Sprintf("claim-%d", timestamp)
}

// CalculatePremium calculates the insurance premium for a publisher
func CalculatePremium(risk *PublisherRisk, baseAmount decimal.Decimal, params *Parameters) decimal.Decimal {
	// Base premium calculation
	basePremium := baseAmount.Mul(decimal.NewFromInt(int64(params.InsurancePoolBPS))).Div(decimal.NewFromInt(10000))

	// Parse and apply risk multiplier. An explicit "0" yields zero premium;
	// only default to 1 when the field is empty, unparseable, OR out of the
	// moneyguard exponent bound. Absurd exponents (e.g. "1e11100100")
	// would make basePremium.Mul(premiumMultiplier) expand shopspring's
	// big.Int to multi-million digits and hang every validator processing
	// the claim. Same DoS class as sibling guards: 8438b6354, 25d34d734,
	// 5c237b056, cbbaba3cb, 09cffa7b4.
	premiumMultiplier, err := decimal.NewFromString(risk.PremiumMultiplier)
	if err != nil || risk.PremiumMultiplier == "" || !moneyguard.IsSafeExponent(premiumMultiplier) {
		premiumMultiplier = decimal.NewFromInt(1)
	}
	adjustedPremium := basePremium.Mul(premiumMultiplier)

	return adjustedPremium
}

// EvaluatePoolHealth evaluates the current health of the insurance pool
func EvaluatePoolHealth(pool *PoolState) PoolStatus {
	// Parse utilization values. Moneyguard-gate the result: the
	// utilization computation below does currentUtilization.Div(
	// targetUtilization) followed by utilizationRatio.LessThan(...)
	// comparisons — Div is the most bomb-explosive shopspring op
	// (forces big.Int expansion to align exponents). A poisoned
	// PoolState persisted by a buggy migration or future writer
	// without moneyguard would freeze every rebalance / health
	// query on every validator. Degrade absurd-exponent fields to
	// zero, matching the existing err-branch behavior.
	targetUtilization, err1 := decimal.NewFromString(pool.TargetUtilization)
	if err1 != nil || !moneyguard.IsSafeExponent(targetUtilization) {
		targetUtilization = decimal.Zero
	}
	currentUtilization, err2 := decimal.NewFromString(pool.CurrentUtilization)
	if err2 != nil || !moneyguard.IsSafeExponent(currentUtilization) {
		currentUtilization = decimal.Zero
	}

	// Handle zero target utilization
	if targetUtilization.IsZero() {
		// If no target is set, evaluate based on absolute utilization
		if currentUtilization.IsZero() {
			return PoolStatus_POOL_STATUS_HEALTHY
		}
		return PoolStatus_POOL_STATUS_UNDERFUNDED
	}

	utilizationRatio := currentUtilization.Div(targetUtilization)

	switch {
	case utilizationRatio.LessThan(decimal.RequireFromString("0.5")):
		return PoolStatus_POOL_STATUS_OVERFUNDED
	case utilizationRatio.LessThan(decimal.RequireFromString("0.8")):
		return PoolStatus_POOL_STATUS_HEALTHY
	case utilizationRatio.LessThan(decimal.RequireFromString("1.2")):
		return PoolStatus_POOL_STATUS_UNDERFUNDED
	default:
		return PoolStatus_POOL_STATUS_CRITICAL
	}
}
