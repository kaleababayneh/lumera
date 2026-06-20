
package types

import (
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newClaimTestContext creates a minimal SDK context for testing.
func newClaimTestContext(t *testing.T) sdk.Context {
	t.Helper()
	return sdk.NewContext(nil, cmtproto.Header{
		Time:   time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
		Height: 100,
	}, false, log.NewNopLogger()).WithBlockGasMeter(storetypes.NewInfiniteGasMeter())
}

// ---------------------------------------------------------------------------
// NewClaim
// ---------------------------------------------------------------------------

func TestNewClaim_ValidRequest(t *testing.T) {
	ctx := newClaimTestContext(t)

	req := ClaimRequest{
		RequestID:      "req-001",
		ReceiptID:      "receipt-abc",
		ClaimantID:     "claimant-xyz",
		ClaimedAmount:  decimal.NewFromInt(1000),
		Reason:         "latency violation",
		Evidence:       []Evidence{{Type: "log", Description: "response time 5000ms"}},
		PolicySnapshot: "v1.0",
	}

	claim, err := NewClaim(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, claim)

	assert.NotEmpty(t, claim.Id)
	assert.Equal(t, "receipt-abc", claim.ReceiptId)
	assert.Equal(t, "claimant-xyz", claim.ClaimantId)
	assert.Equal(t, "latency violation", claim.Reason)
	assert.Equal(t, ClaimStatus_CLAIM_STATUS_PENDING, claim.Status)
	assert.Equal(t, "v1.0", claim.PolicySnapshot)
	assert.NotNil(t, claim.ClaimedAmount)
	assert.Equal(t, "ulac", claim.ClaimedAmount.Denom)
	assert.Equal(t, "1000", claim.ClaimedAmount.Amount)
	assert.Len(t, claim.Evidence, 1)
}

func TestNewClaim_EmptyReceiptID(t *testing.T) {
	ctx := newClaimTestContext(t)

	req := ClaimRequest{
		RequestID:     "req-001",
		ReceiptID:     "", // empty
		ClaimantID:    "claimant-xyz",
		ClaimedAmount: decimal.NewFromInt(1000),
	}

	claim, err := NewClaim(ctx, req)
	require.ErrorIs(t, err, ErrInvalidClaimRequest)
	require.Nil(t, claim)
}

func TestNewClaim_EmptyClaimantID(t *testing.T) {
	ctx := newClaimTestContext(t)

	req := ClaimRequest{
		RequestID:     "req-001",
		ReceiptID:     "receipt-abc",
		ClaimantID:    "", // empty
		ClaimedAmount: decimal.NewFromInt(1000),
	}

	claim, err := NewClaim(ctx, req)
	require.ErrorIs(t, err, ErrInvalidClaimRequest)
	require.Nil(t, claim)
}

func TestNewClaim_NegativeAmount(t *testing.T) {
	ctx := newClaimTestContext(t)

	req := ClaimRequest{
		RequestID:     "req-001",
		ReceiptID:     "receipt-abc",
		ClaimantID:    "claimant-xyz",
		ClaimedAmount: decimal.NewFromInt(-500), // negative
	}

	claim, err := NewClaim(ctx, req)
	require.ErrorIs(t, err, ErrInvalidAmount)
	require.Nil(t, claim)
}

func TestNewClaim_ZeroAmount(t *testing.T) {
	ctx := newClaimTestContext(t)

	req := ClaimRequest{
		RequestID:     "req-001",
		ReceiptID:     "receipt-abc",
		ClaimantID:    "claimant-xyz",
		ClaimedAmount: decimal.Zero, // zero
	}

	claim, err := NewClaim(ctx, req)
	require.ErrorIs(t, err, ErrInvalidAmount)
	require.Nil(t, claim)
}

func TestNewClaim_NoEvidence(t *testing.T) {
	ctx := newClaimTestContext(t)

	req := ClaimRequest{
		RequestID:     "req-001",
		ReceiptID:     "receipt-abc",
		ClaimantID:    "claimant-xyz",
		ClaimedAmount: decimal.NewFromInt(100),
		Evidence:      nil, // no evidence is allowed
	}

	claim, err := NewClaim(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, claim)
	assert.Len(t, claim.Evidence, 0)
}

func TestNewClaim_LargeAmount(t *testing.T) {
	ctx := newClaimTestContext(t)

	req := ClaimRequest{
		RequestID:     "req-001",
		ReceiptID:     "receipt-abc",
		ClaimantID:    "claimant-xyz",
		ClaimedAmount: decimal.RequireFromString("999999999999999999"),
		Reason:        "large claim",
	}

	claim, err := NewClaim(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, claim)
	assert.Equal(t, "999999999999999999", claim.ClaimedAmount.Amount)
}

func TestNewClaim_MultipleEvidence(t *testing.T) {
	ctx := newClaimTestContext(t)

	req := ClaimRequest{
		RequestID:     "req-001",
		ReceiptID:     "receipt-abc",
		ClaimantID:    "claimant-xyz",
		ClaimedAmount: decimal.NewFromInt(1000),
		Evidence: []Evidence{
			{Type: "log", Description: "error log"},
			{Type: "metric", Description: "latency spike"},
			{Type: "screenshot", Description: "UI error"},
		},
	}

	claim, err := NewClaim(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, claim)
	assert.Len(t, claim.Evidence, 3)
}

// ---------------------------------------------------------------------------
// GenerateClaimID
// ---------------------------------------------------------------------------

func TestGenerateClaimID_Format(t *testing.T) {
	ctx := newClaimTestContext(t)
	id := GenerateClaimID(ctx)

	assert.NotEmpty(t, id)
	assert.Contains(t, id, "claim-")
}

func TestGenerateClaimID_IncludesTimestamp(t *testing.T) {
	ctx := newClaimTestContext(t)
	id := GenerateClaimID(ctx)

	// The ID should contain a numeric timestamp component
	assert.Regexp(t, `^claim-\d+$`, id)
}

// ---------------------------------------------------------------------------
// CalculatePremium - additional edge cases not covered in types_test.go
// ---------------------------------------------------------------------------

func TestCalculatePremium_HighRiskMultiplier(t *testing.T) {
	risk := &PublisherRisk{
		PremiumMultiplier: "2.5",
	}
	params := &Parameters{
		InsurancePoolBPS: 500, // 5%
	}
	baseAmount := decimal.NewFromInt(10000)

	premium := CalculatePremium(risk, baseAmount, params)

	// 10000 * 500/10000 * 2.5 = 1250
	expected := decimal.NewFromInt(1250)
	assert.True(t, premium.Equal(expected), "expected %s, got %s", expected, premium)
}

func TestCalculatePremium_LowRiskMultiplier(t *testing.T) {
	risk := &PublisherRisk{
		PremiumMultiplier: "0.5",
	}
	params := &Parameters{
		InsurancePoolBPS: 300, // 3%
	}
	baseAmount := decimal.NewFromInt(10000)

	premium := CalculatePremium(risk, baseAmount, params)

	// 10000 * 300/10000 * 0.5 = 150
	expected := decimal.NewFromInt(150)
	assert.True(t, premium.Equal(expected), "expected %s, got %s", expected, premium)
}

func TestCalculatePremium_EmptyMultiplierDefaultsToOne(t *testing.T) {
	risk := &PublisherRisk{
		PremiumMultiplier: "", // empty
	}
	params := &Parameters{
		InsurancePoolBPS: 300, // 3%
	}
	baseAmount := decimal.NewFromInt(10000)

	premium := CalculatePremium(risk, baseAmount, params)

	// Should default to multiplier of 1: 10000 * 300/10000 * 1 = 300
	expected := decimal.NewFromInt(300)
	assert.True(t, premium.Equal(expected), "expected %s, got %s", expected, premium)
}

func TestCalculatePremium_InvalidMultiplierDefaultsToOne(t *testing.T) {
	risk := &PublisherRisk{
		PremiumMultiplier: "not_a_number",
	}
	params := &Parameters{
		InsurancePoolBPS: 300,
	}
	baseAmount := decimal.NewFromInt(10000)

	premium := CalculatePremium(risk, baseAmount, params)

	// Should default to multiplier of 1
	expected := decimal.NewFromInt(300)
	assert.True(t, premium.Equal(expected), "expected %s, got %s", expected, premium)
}

func TestCalculatePremium_ZeroBaseAmount(t *testing.T) {
	risk := &PublisherRisk{
		PremiumMultiplier: "1.5",
	}
	params := &Parameters{
		InsurancePoolBPS: 500,
	}
	baseAmount := decimal.Zero

	premium := CalculatePremium(risk, baseAmount, params)

	assert.True(t, premium.IsZero(), "zero base amount should yield zero premium")
}

func TestCalculatePremium_FractionalMultiplier(t *testing.T) {
	risk := &PublisherRisk{
		PremiumMultiplier: "1.25",
	}
	params := &Parameters{
		InsurancePoolBPS: 400, // 4%
	}
	baseAmount := decimal.NewFromInt(10000)

	premium := CalculatePremium(risk, baseAmount, params)

	// 10000 * 400/10000 * 1.25 = 500
	expected := decimal.NewFromInt(500)
	assert.True(t, premium.Equal(expected), "expected %s, got %s", expected, premium)
}

// ---------------------------------------------------------------------------
// EvaluatePoolHealth - additional edge cases not covered in types_test.go
// ---------------------------------------------------------------------------

func TestEvaluatePoolHealth_InvalidTargetUtilization(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "not_a_number",
		CurrentUtilization: "0.5",
	}

	status := EvaluatePoolHealth(pool)
	// Invalid target parses to zero, non-zero current -> underfunded
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, status)
}

func TestEvaluatePoolHealth_InvalidCurrentUtilization(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "0.5",
		CurrentUtilization: "invalid",
	}

	status := EvaluatePoolHealth(pool)
	// Current parses to zero, 0/0.5 = 0% < 50% -> overfunded
	assert.Equal(t, PoolStatus_POOL_STATUS_OVERFUNDED, status)
}

func TestEvaluatePoolHealth_ExactlyAtHealthyUpperBound(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "1.0",
		CurrentUtilization: "0.8", // exactly 80%
	}

	status := EvaluatePoolHealth(pool)
	// 0.8/1.0 = 0.8, which is not < 0.8, so it's underfunded
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, status)
}

func TestEvaluatePoolHealth_JustBelowHealthyUpperBound(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "1.0",
		CurrentUtilization: "0.79", // just below 80%
	}

	status := EvaluatePoolHealth(pool)
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, status)
}

func TestEvaluatePoolHealth_ExactlyAtOverfundedBound(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "1.0",
		CurrentUtilization: "0.5", // exactly 50%
	}

	status := EvaluatePoolHealth(pool)
	// 0.5/1.0 = 0.5, which is not < 0.5, so it's healthy
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, status)
}

func TestEvaluatePoolHealth_JustBelowOverfundedBound(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "1.0",
		CurrentUtilization: "0.49", // just below 50%
	}

	status := EvaluatePoolHealth(pool)
	// 0.49 < 0.5 -> overfunded
	assert.Equal(t, PoolStatus_POOL_STATUS_OVERFUNDED, status)
}

func TestEvaluatePoolHealth_ExactlyAtCriticalBound(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "1.0",
		CurrentUtilization: "1.2", // exactly 120%
	}

	status := EvaluatePoolHealth(pool)
	// 1.2 is not < 1.2, so it's critical
	assert.Equal(t, PoolStatus_POOL_STATUS_CRITICAL, status)
}

func TestEvaluatePoolHealth_JustBelowCriticalBound(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "1.0",
		CurrentUtilization: "1.19", // just below 120%
	}

	status := EvaluatePoolHealth(pool)
	// 1.19 < 1.2 -> underfunded
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, status)
}
