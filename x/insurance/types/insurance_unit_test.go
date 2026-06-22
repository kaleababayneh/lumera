
package types

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --------------------------------------------------------------------------
// PoolStatus tests
// --------------------------------------------------------------------------

func TestPoolStatus_Constants(t *testing.T) {
	assert.Equal(t, PoolStatus(0), PoolStatus_POOL_STATUS_UNSPECIFIED)
	assert.Equal(t, PoolStatus(1), PoolStatus_POOL_STATUS_HEALTHY)
	assert.Equal(t, PoolStatus(2), PoolStatus_POOL_STATUS_UNDERFUNDED)
	assert.Equal(t, PoolStatus(3), PoolStatus_POOL_STATUS_CRITICAL)
	assert.Equal(t, PoolStatus(4), PoolStatus_POOL_STATUS_OVERFUNDED)
}

// --------------------------------------------------------------------------
// ClaimStatus tests
// --------------------------------------------------------------------------

func TestClaimStatus_Constants(t *testing.T) {
	assert.Equal(t, ClaimStatus(0), ClaimStatus_CLAIM_STATUS_UNSPECIFIED)
	assert.Equal(t, ClaimStatus(1), ClaimStatus_CLAIM_STATUS_PENDING)
	assert.Equal(t, ClaimStatus(2), ClaimStatus_CLAIM_STATUS_APPROVED)
	assert.Equal(t, ClaimStatus(3), ClaimStatus_CLAIM_STATUS_REJECTED)
	assert.Equal(t, ClaimStatus(4), ClaimStatus_CLAIM_STATUS_PAID)
	assert.Equal(t, ClaimStatus(5), ClaimStatus_CLAIM_STATUS_EXPIRED)
	assert.Equal(t, ClaimStatus(6), ClaimStatus_CLAIM_STATUS_DISPUTED)
}

func TestClaimStatus_String(t *testing.T) {
	tests := []struct {
		status ClaimStatus
		want   string
	}{
		{ClaimStatus_CLAIM_STATUS_UNSPECIFIED, "CLAIM_STATUS_UNSPECIFIED"},
		{ClaimStatus_CLAIM_STATUS_PENDING, "CLAIM_STATUS_PENDING"},
		{ClaimStatus_CLAIM_STATUS_APPROVED, "CLAIM_STATUS_APPROVED"},
		{ClaimStatus_CLAIM_STATUS_REJECTED, "CLAIM_STATUS_REJECTED"},
		{ClaimStatus_CLAIM_STATUS_PAID, "CLAIM_STATUS_PAID"},
		{ClaimStatus_CLAIM_STATUS_EXPIRED, "CLAIM_STATUS_EXPIRED"},
		{ClaimStatus_CLAIM_STATUS_DISPUTED, "CLAIM_STATUS_DISPUTED"},
		// gogoproto's generated String() renders an unknown enum value as its
		// decimal form (the protobuf-go runtime used to map it to the zero name).
		{ClaimStatus(99), "99"}, // unknown value
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.status.String())
		})
	}
}

// --------------------------------------------------------------------------
// PayoutStatus tests
// --------------------------------------------------------------------------

func TestPayoutStatus_Constants(t *testing.T) {
	assert.Equal(t, PayoutStatus(0), PayoutStatus_PAYOUT_STATUS_UNSPECIFIED)
	assert.Equal(t, PayoutStatus(1), PayoutStatus_PAYOUT_STATUS_PENDING)
	assert.Equal(t, PayoutStatus(2), PayoutStatus_PAYOUT_STATUS_COMPLETED)
	assert.Equal(t, PayoutStatus(3), PayoutStatus_PAYOUT_STATUS_FAILED)
}

func TestPayoutStatus_String(t *testing.T) {
	tests := []struct {
		status PayoutStatus
		want   string
	}{
		{PayoutStatus_PAYOUT_STATUS_UNSPECIFIED, "PAYOUT_STATUS_UNSPECIFIED"},
		{PayoutStatus_PAYOUT_STATUS_PENDING, "PAYOUT_STATUS_PENDING"},
		{PayoutStatus_PAYOUT_STATUS_COMPLETED, "PAYOUT_STATUS_COMPLETED"},
		{PayoutStatus_PAYOUT_STATUS_FAILED, "PAYOUT_STATUS_FAILED"},
		// gogoproto's generated String() renders an unknown enum value as its
		// decimal form (the protobuf-go runtime used to map it to the zero name).
		{PayoutStatus(99), "99"}, // unknown value
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.status.String())
		})
	}
}

// --------------------------------------------------------------------------
// ReceiptSettlementStatus tests
// --------------------------------------------------------------------------

func TestReceiptSettlementStatus_Constants(t *testing.T) {
	assert.Equal(t, ReceiptSettlementStatus("pending"), SettlementStatusPending)
	assert.Equal(t, ReceiptSettlementStatus("settled"), SettlementStatusSettled)
	assert.Equal(t, ReceiptSettlementStatus("challenged"), SettlementStatusChallenged)
}

// --------------------------------------------------------------------------
// EvaluatePoolHealth tests
// --------------------------------------------------------------------------

func TestEvaluatePoolHealth_ZeroTargetZeroCurrent_Unit(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "0",
		CurrentUtilization: "0",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_ZeroTargetNonzeroCurrent_Unit(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "0",
		CurrentUtilization: "0.5",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_Overfunded_Unit(t *testing.T) {
	// ratio < 0.5 => overfunded
	pool := &PoolState{
		TargetUtilization:  "0.8",
		CurrentUtilization: "0.3",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_OVERFUNDED, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_Healthy_Unit(t *testing.T) {
	// 0.5 <= ratio < 0.8 => healthy
	pool := &PoolState{
		TargetUtilization:  "0.5",
		CurrentUtilization: "0.3",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_Underfunded_Unit(t *testing.T) {
	// 0.8 <= ratio < 1.2 => underfunded
	pool := &PoolState{
		TargetUtilization:  "0.5",
		CurrentUtilization: "0.5",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_Critical_Unit(t *testing.T) {
	// ratio >= 1.2 => critical
	pool := &PoolState{
		TargetUtilization:  "0.5",
		CurrentUtilization: "0.8",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_CRITICAL, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_InvalidTargetUtilization_Unit(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "not_a_number",
		CurrentUtilization: "0.5",
	}
	// Invalid target defaults to zero, then non-zero current => underfunded
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_InvalidCurrentUtilization_Unit(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "0.5",
		CurrentUtilization: "not_a_number",
	}
	// Invalid current defaults to zero, ratio = 0 => overfunded
	assert.Equal(t, PoolStatus_POOL_STATUS_OVERFUNDED, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_BothInvalid(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "invalid",
		CurrentUtilization: "also_invalid",
	}
	// Both default to zero => healthy
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_EdgeRatios(t *testing.T) {
	tests := []struct {
		name   string
		target string
		current string
		want   PoolStatus
	}{
		{"ratio_exactly_0.5", "1.0", "0.5", PoolStatus_POOL_STATUS_HEALTHY},
		{"ratio_just_below_0.5", "1.0", "0.49", PoolStatus_POOL_STATUS_OVERFUNDED},
		{"ratio_exactly_0.8", "1.0", "0.8", PoolStatus_POOL_STATUS_UNDERFUNDED},
		{"ratio_just_below_0.8", "1.0", "0.79", PoolStatus_POOL_STATUS_HEALTHY},
		{"ratio_exactly_1.2", "1.0", "1.2", PoolStatus_POOL_STATUS_CRITICAL},
		{"ratio_just_below_1.2", "1.0", "1.19", PoolStatus_POOL_STATUS_UNDERFUNDED},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool := &PoolState{
				TargetUtilization:  tc.target,
				CurrentUtilization: tc.current,
			}
			assert.Equal(t, tc.want, EvaluatePoolHealth(pool))
		})
	}
}

// --------------------------------------------------------------------------
// CalculatePremium tests
// --------------------------------------------------------------------------

func TestCalculatePremium_BasicCalculation(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "1.5"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 300} // 3%

	premium := CalculatePremium(risk, baseAmount, params)
	// base * BPS / 10000 = 1000 * 300 / 10000 = 30
	// 30 * 1.5 = 45
	assert.True(t, premium.Equal(decimal.NewFromInt(45)))
}

func TestCalculatePremium_ZeroMultiplier_Unit(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "0"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 300}

	premium := CalculatePremium(risk, baseAmount, params)
	assert.True(t, premium.IsZero())
}

func TestCalculatePremium_ZeroBPS_Unit(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "2.0"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 0}

	premium := CalculatePremium(risk, baseAmount, params)
	assert.True(t, premium.IsZero())
}

func TestCalculatePremium_EmptyMultiplier(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: ""}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 300}

	premium := CalculatePremium(risk, baseAmount, params)
	// Empty multiplier defaults to 1
	// 1000 * 300 / 10000 = 30
	assert.True(t, premium.Equal(decimal.NewFromInt(30)))
}

func TestCalculatePremium_InvalidMultiplier(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "not_a_number"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 300}

	premium := CalculatePremium(risk, baseAmount, params)
	// Invalid multiplier defaults to 1
	assert.True(t, premium.Equal(decimal.NewFromInt(30)))
}

func TestCalculatePremium_LargeValues(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "3.0"}
	baseAmount := decimal.NewFromInt(1_000_000_000) // 1 billion
	params := &Parameters{InsurancePoolBPS: 500}    // 5%

	premium := CalculatePremium(risk, baseAmount, params)
	// 1_000_000_000 * 500 / 10000 = 50_000_000
	// 50_000_000 * 3.0 = 150_000_000
	assert.True(t, premium.Equal(decimal.NewFromInt(150_000_000)))
}

func TestCalculatePremium_DecimalMultiplier(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "1.25"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 400} // 4%

	premium := CalculatePremium(risk, baseAmount, params)
	// 1000 * 400 / 10000 = 40
	// 40 * 1.25 = 50
	assert.True(t, premium.Equal(decimal.NewFromInt(50)))
}

// --------------------------------------------------------------------------
// Evidence JSON serialization tests
// --------------------------------------------------------------------------

func TestEvidence_JSONMarshal(t *testing.T) {
	evidence := &Evidence{
		Type:        "screenshot",
		Hash:        "sha256:abc123",
		Uri:         "https://example.com/evidence.png",
		Description: "Error message screenshot",
	}

	data, err := json.Marshal(evidence)
	require.NoError(t, err)

	var decoded Evidence
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, evidence.Type, decoded.Type)
	assert.Equal(t, evidence.Hash, decoded.Hash)
	assert.Equal(t, evidence.Uri, decoded.Uri)
	assert.Equal(t, evidence.Description, decoded.Description)
}

func TestEvidence_JSONMarshal_EmptyUrl(t *testing.T) {
	evidence := &Evidence{
		Type:        "log",
		Hash:        "sha256:def456",
		Uri:         "",
		Description: "Service logs",
	}

	data, err := json.Marshal(evidence)
	require.NoError(t, err)

	// URI should be omitted when empty (omitempty)
	assert.NotContains(t, string(data), `"uri":""`)
}

// --------------------------------------------------------------------------
// Claim struct tests
// --------------------------------------------------------------------------

func TestClaim_JSONMarshal(t *testing.T) {
	claim := &Claim{
		Id:          "claim-123",
		ReceiptId:   "receipt-456",
		ClaimantId:  "user-789",
		ToolId:      "tool-abc",
		PublisherId: "pub-def",
		Status:      ClaimStatus_CLAIM_STATUS_PENDING,
		Reason:      "Service failure",
		Evidence: []*Evidence{
			{Type: "log", Hash: "sha256:123", Description: "Error logs"},
		},
	}

	data, err := json.Marshal(claim)
	require.NoError(t, err)

	var decoded Claim
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, claim.Id, decoded.Id)
	assert.Equal(t, claim.ReceiptId, decoded.ReceiptId)
	assert.Equal(t, claim.ClaimantId, decoded.ClaimantId)
	assert.Equal(t, claim.Status, decoded.Status)
	assert.Len(t, decoded.Evidence, 1)
}

// --------------------------------------------------------------------------
// Contribution struct tests
// --------------------------------------------------------------------------

func TestContribution_JSONMarshal(t *testing.T) {
	contribution := &Contribution{
		ReceiptId:   "receipt-001",
		PublisherId: "pub-001",
		ToolId:      "tool-001",
	}

	data, err := json.Marshal(contribution)
	require.NoError(t, err)

	var decoded Contribution
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, contribution.ReceiptId, decoded.ReceiptId)
	assert.Equal(t, contribution.PublisherId, decoded.PublisherId)
	assert.Equal(t, contribution.ToolId, decoded.ToolId)
}

// --------------------------------------------------------------------------
// Payout struct tests
// --------------------------------------------------------------------------

func TestPayout_JSONMarshal(t *testing.T) {
	payout := &Payout{
		ClaimId:     "claim-001",
		RecipientId: "user-001",
		Status:      PayoutStatus_PAYOUT_STATUS_COMPLETED,
	}

	data, err := json.Marshal(payout)
	require.NoError(t, err)

	var decoded Payout
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, payout.ClaimId, decoded.ClaimId)
	assert.Equal(t, payout.RecipientId, decoded.RecipientId)
	assert.Equal(t, payout.Status, decoded.Status)
}

// --------------------------------------------------------------------------
// PoolState struct tests
// --------------------------------------------------------------------------

func TestPoolState_JSONMarshal(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "0.7",
		CurrentUtilization: "0.5",
	}

	data, err := json.Marshal(pool)
	require.NoError(t, err)

	var decoded PoolState
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, pool.TargetUtilization, decoded.TargetUtilization)
	assert.Equal(t, pool.CurrentUtilization, decoded.CurrentUtilization)
}

// --------------------------------------------------------------------------
// PublisherRisk struct tests
// --------------------------------------------------------------------------

func TestPublisherRisk_JSONMarshal(t *testing.T) {
	risk := &PublisherRisk{
		PremiumMultiplier: "1.5",
	}

	data, err := json.Marshal(risk)
	require.NoError(t, err)

	var decoded PublisherRisk
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, risk.PremiumMultiplier, decoded.PremiumMultiplier)
}

// --------------------------------------------------------------------------
// RateLimitInfo struct tests
// --------------------------------------------------------------------------

func TestRateLimitInfo_Fields(t *testing.T) {
	info := RateLimitInfo{
		Address:       "cosmos1abc",
		CurrentCount:  5,
		MaxAllowed:    10,
		RateLimitType: "claim",
	}

	assert.Equal(t, "cosmos1abc", info.Address)
	assert.Equal(t, uint32(5), info.CurrentCount)
	assert.Equal(t, uint32(10), info.MaxAllowed)
	assert.Equal(t, "claim", info.RateLimitType)
}

// --------------------------------------------------------------------------
// Parameters struct tests
// --------------------------------------------------------------------------

func TestParameters_Fields(t *testing.T) {
	params := Parameters{
		InsurancePoolBPS:     300,
		TargetUtilization:    decimal.NewFromFloat(0.7),
		MinPoolBalance:       decimal.NewFromInt(1000),
		MaxClaimPercent:      decimal.NewFromFloat(0.1),
		ClaimWindowSeconds:   86400,
		DisputeStakeLAC:      decimal.NewFromInt(100),
		PremiumAdjustmentBPS: 25,
		AutoApproveThreshold: decimal.NewFromInt(10),
		SlashDecayDays:       30,
	}

	assert.Equal(t, 300, params.InsurancePoolBPS)
	assert.True(t, params.TargetUtilization.Equal(decimal.NewFromFloat(0.7)))
	assert.Equal(t, 86400, params.ClaimWindowSeconds)
	assert.Equal(t, 30, params.SlashDecayDays)
}

// --------------------------------------------------------------------------
// Metrics struct tests
// --------------------------------------------------------------------------

func TestMetrics_Fields(t *testing.T) {
	metrics := Metrics{
		PendingClaims:     5,
		AverageClaimAmount: decimal.NewFromInt(100),
		ClaimApprovalRate:  decimal.NewFromFloat(0.8),
		PoolHealthScore:    decimal.NewFromInt(95),
		Samples:           1000,
	}

	assert.Equal(t, 5, metrics.PendingClaims)
	assert.True(t, metrics.AverageClaimAmount.Equal(decimal.NewFromInt(100)))
	assert.True(t, metrics.ClaimApprovalRate.Equal(decimal.NewFromFloat(0.8)))
	assert.Equal(t, uint64(1000), metrics.Samples)
}

// --------------------------------------------------------------------------
// ContributionRequest struct tests
// --------------------------------------------------------------------------

func TestContributionRequest_Fields(t *testing.T) {
	req := ContributionRequest{
		RequestID:     "req-001",
		ReceiptID:     "receipt-001",
		ToolID:        "tool-001",
		PublisherID:   "pub-001",
		InvokeAmount:  decimal.NewFromInt(500),
		PolicyVersion: "v1.0",
	}

	assert.Equal(t, "req-001", req.RequestID)
	assert.Equal(t, "receipt-001", req.ReceiptID)
	assert.True(t, req.InvokeAmount.Equal(decimal.NewFromInt(500)))
	assert.Equal(t, "v1.0", req.PolicyVersion)
}

// --------------------------------------------------------------------------
// ClaimRequest struct tests
// --------------------------------------------------------------------------

func TestClaimRequest_Fields(t *testing.T) {
	req := ClaimRequest{
		RequestID:      "req-002",
		ReceiptID:      "receipt-002",
		ClaimantID:     "user-002",
		ClaimedAmount:  decimal.NewFromInt(100),
		Reason:         "Service outage",
		PolicySnapshot: "policy-v1",
		Evidence: []Evidence{
			{Type: "log", Hash: "sha256:abc", Description: "Error log"},
		},
	}

	assert.Equal(t, "req-002", req.RequestID)
	assert.Equal(t, "user-002", req.ClaimantID)
	assert.True(t, req.ClaimedAmount.Equal(decimal.NewFromInt(100)))
	assert.Len(t, req.Evidence, 1)
}

// --------------------------------------------------------------------------
// PayoutRequest struct tests
// --------------------------------------------------------------------------

func TestPayoutRequest_Fields(t *testing.T) {
	req := PayoutRequest{
		ClaimID:     "claim-001",
		RecipientID: "user-001",
		Urgency:     "high",
	}

	assert.Equal(t, "claim-001", req.ClaimID)
	assert.Equal(t, "user-001", req.RecipientID)
	assert.Equal(t, "high", req.Urgency)
}

// --------------------------------------------------------------------------
// Error sentinel tests
// --------------------------------------------------------------------------

func TestErrorSentinels(t *testing.T) {
	assert.NotNil(t, ErrInvalidClaimRequest)
	assert.NotNil(t, ErrInvalidAmount)
	assert.Contains(t, ErrInvalidClaimRequest.Error(), "invalid claim request")
	assert.Contains(t, ErrInvalidAmount.Error(), "invalid amount")
}

// --------------------------------------------------------------------------
// RestitutionSplit.Total tests
// --------------------------------------------------------------------------

func TestRestitutionSplit_Total(t *testing.T) {
	split := RestitutionSplit{
		Users:     decimal.NewFromInt(60),
		Insurance: decimal.NewFromInt(25),
		Treasury:  decimal.NewFromInt(10),
		Burn:      decimal.NewFromInt(5),
	}

	total := split.Total()
	assert.True(t, total.Equal(decimal.NewFromInt(100)))
}

func TestRestitutionSplit_TotalWithDecimals(t *testing.T) {
	split := RestitutionSplit{
		Users:     decimal.NewFromFloat(60.5),
		Insurance: decimal.NewFromFloat(25.25),
		Treasury:  decimal.NewFromFloat(10.15),
		Burn:      decimal.NewFromFloat(4.1),
	}

	total := split.Total()
	expected := decimal.NewFromFloat(100.0)
	assert.True(t, total.Equal(expected))
}

// --------------------------------------------------------------------------
// RestitutionBps constants tests
// --------------------------------------------------------------------------

func TestRestitutionBps_Constants(t *testing.T) {
	assert.Equal(t, uint32(6000), RestitutionUsersBps)
	assert.Equal(t, uint32(2500), RestitutionInsuranceBps)
	assert.Equal(t, uint32(1000), RestitutionTreasuryBps)
	assert.Equal(t, uint32(500), RestitutionBurnBps)
	assert.Equal(t, uint32(10000), RestitutionTotalBps)

	// Verify they sum to 100%
	sum := RestitutionUsersBps + RestitutionInsuranceBps + RestitutionTreasuryBps + RestitutionBurnBps
	assert.Equal(t, RestitutionTotalBps, sum)
}
