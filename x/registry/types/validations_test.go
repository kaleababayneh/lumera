
package types

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
)

// ---------------------------------------------------------------------------
// Test address helpers
// ---------------------------------------------------------------------------

func testBech32Addr(t *testing.T) string {
	t.Helper()
	addr := sdk.AccAddress(bytes.Repeat([]byte{0xAA}, 20))
	return addr.String()
}

func testBech32Addr2(t *testing.T) string {
	t.Helper()
	addr := sdk.AccAddress(bytes.Repeat([]byte{0xBB}, 20))
	return addr.String()
}

func testBech32Addr3(t *testing.T) string {
	t.Helper()
	addr := sdk.AccAddress(bytes.Repeat([]byte{0xCC}, 20))
	return addr.String()
}

func testTimestamp(offset time.Duration) *timestamppb.Timestamp {
	return timestamppb.New(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).Add(offset))
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestValidateToolID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid simple", "my-tool", false},
		{"valid with dots", "defi.token_price", false},
		{"valid with underscores", "tool_v2", false},
		{"valid min length", "a", false},
		{"empty", "", true},
		{"spaces only", "   ", true},
		{"too long", strings.Repeat("a", MaxToolIDLength+1), true},
		{"max length ok", strings.Repeat("a", MaxToolIDLength), false},
		{"uppercase", "MyTool", true},
		{"special chars", "tool@v1", true},
		{"slash", "org/tool", true},
		{"space inside", "my tool", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateToolID(tt.id)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateHTTPURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		raw       string
		field     string
		allowHTTP bool
		wantErr   bool
	}{
		{"valid https", "https://api.example.com", "url", false, false},
		{"valid http when allowed", "http://api.example.com", "url", true, false},
		{"http not allowed", "http://api.example.com", "url", false, true},
		{"https userinfo", "https://operator@example.com/v1/tools", "url", false, true},
		{"http userinfo when allowed", "http://operator@example.com/v1/tools", "url", true, true},
		{"ftp not allowed", "ftp://files.example.com", "url", true, true},
		{"empty", "", "url", false, true},
		{"no host", "https://", "url", false, true},
		{"hostless with port", "https://:443/v1/tools", "url", false, true},
		{"spaces only", "   ", "url", false, true},
		{"valid with path", "https://api.example.com/v1/tools", "url", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHTTPURL(tt.raw, tt.field, tt.allowHTTP)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateHTTPURLRejectsMalformedUserinfoWithoutLeak(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://operator:secret@api.example.com/%zz",
		"//operator:secret@api.example.com/%zz",
	} {
		err := validateHTTPURL(raw, "url", false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "userinfo")
		require.NotContains(t, err.Error(), "operator:secret")
	}
}

func TestValidateHTTPURLRejectsMalformedSecretQueryWithoutLeak(t *testing.T) {
	t.Parallel()

	raw := "https://api.example.com/%zz?access_token=secret-token&safe=1"
	err := validateHTTPURL(raw, "url", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid url")
	require.Contains(t, err.Error(), "%zz")
	require.NotContains(t, err.Error(), raw)
	require.NotContains(t, err.Error(), "access_token=")
	require.NotContains(t, err.Error(), "secret-token")
	require.NotContains(t, err.Error(), "safe=1")
}

func TestValidateEvidenceURI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"valid https", "https://evidence.example.com/proof", false},
		{"valid http", "http://evidence.example.com/proof", false},
		{"valid ipfs", "ipfs://QmHash/proof", false},
		{"valid ipns", "ipns://QmHash/proof", false},
		{"https userinfo", "https://operator@example.com/proof", true},
		{"ipfs userinfo", "ipfs://operator@QmHash/proof", true},
		{"empty", "", true},
		{"no scheme", "example.com/proof", true},
		{"ftp rejected", "ftp://files.example.com", true},
		{"no host", "https://", true},
		{"hostless with port", "ipfs://:5001/proof", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEvidenceURI(tt.raw, "evidence")
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateEvidenceURIRejectsMalformedUserinfoWithoutLeak(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://operator:secret@evidence.example.com/%zz",
		"//operator:secret@evidence.example.com/%zz",
	} {
		err := validateEvidenceURI(raw, "evidence")
		require.Error(t, err)
		require.Contains(t, err.Error(), "userinfo")
		require.NotContains(t, err.Error(), "operator:secret")
	}
}

func TestValidateEvidenceURIRejectsMalformedSecretQueryWithoutLeak(t *testing.T) {
	t.Parallel()

	raw := "https://evidence.example.com/%zz?api_key=secret-key&safe=1"
	err := validateEvidenceURI(raw, "evidence")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid evidence")
	require.Contains(t, err.Error(), "%zz")
	require.NotContains(t, err.Error(), raw)
	require.NotContains(t, err.Error(), "api_key=")
	require.NotContains(t, err.Error(), "secret-key")
	require.NotContains(t, err.Error(), "safe=1")
}

func TestParseMetadataTimestamp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid unix seconds", "1700000000", false},
		{"valid RFC3339", "2025-12-01T00:00:00Z", false},
		{"valid RFC3339Nano", "2025-12-01T00:00:00.123456789Z", false},
		{"empty", "", true},
		{"zero seconds", "0", true},
		{"negative seconds", "-1", true},
		{"invalid string", "not-a-time", true},
		{"spaces only", "   ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMetadataTimestamp(tt.value, "field")
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsDigits(t *testing.T) {
	t.Parallel()
	require.True(t, isDigits("12345"))
	require.True(t, isDigits("0"))
	require.False(t, isDigits("12.5"))
	require.False(t, isDigits("abc"))
	require.False(t, isDigits("-1"))
	require.True(t, isDigits(""))
}

func TestValidateEnum(t *testing.T) {
	t.Parallel()
	allowed := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	require.NoError(t, validateEnum("a", allowed, "field"))
	require.NoError(t, validateEnum("b", allowed, "field"))
	require.Error(t, validateEnum("d", allowed, "field"))
	require.Error(t, validateEnum("", allowed, "field"))
	require.Error(t, validateEnum("  ", allowed, "field"))
}

func TestValidateOriginIDSchema(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"empty is valid", "", false},
		{"valid format", "injective:iagent", false},
		{"valid with numbers", "chain1:surface2", false},
		{"valid with dashes", "my-chain:my-surface", false},
		{"too long overall", strings.Repeat("a", 33) + ":" + strings.Repeat("b", 33), true},
		{"no colon", "noseparator", true},
		{"too many colons", "a:b:c", true},
		{"empty namespace", ":surface", true},
		{"empty surface", "namespace:", true},
		{"namespace starts with dash", "-bad:surface", true},
		{"surface starts with underscore", "ns:_bad", true},
		{"uppercase chars normalized", "NS:surface", false}, // lowercased before validation
		{"invalid char in namespace", "ns!:surface", true},
		{"namespace too long", strings.Repeat("a", 33) + ":b", true},
		{"surface too long", "a:" + strings.Repeat("b", 33), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOriginIDSchema(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRequireTimestamp(t *testing.T) {
	t.Parallel()
	ts := timestamppb.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	got, err := requireTimestamp(ts, "test")
	require.NoError(t, err)
	require.Equal(t, 2026, got.Year())

	_, err = requireTimestamp(nil, "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

// ---------------------------------------------------------------------------
// UsageReceipt.Validate()
// ---------------------------------------------------------------------------

func validUsageReceipt(t *testing.T) *UsageReceipt {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return &UsageReceipt{
		ReceiptId:     "rcpt-001",
		ToolId:        "defi.token-price",
		RequestId:     "req-001",
		RequestHash:   bytes.Repeat([]byte{0x01}, 32),
		UnitsUsed:     "1.0",
		Unit:          "req",
		PricePerUnit:  "0.05",
		QuotedAmount:  "0.05",
		ActualAmount:  "0.05",
		RouterAddress: testBech32Addr(t),
		UserAddress:   testBech32Addr2(t),
		Timestamp:     timestamppb.New(now),
		ExpiresAt:     timestamppb.New(now.Add(48 * time.Hour)),
		Status:        ReceiptStatusPending,
	}
}

func TestUsageReceiptValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validUsageReceipt(t).Validate())
}

func TestUsageReceiptValidate_Nil(t *testing.T) {
	t.Parallel()
	var r *UsageReceipt
	require.Error(t, r.Validate())
}

func TestUsageReceiptValidate_EmptyFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*UsageReceipt)
	}{
		{"empty receipt_id", func(r *UsageReceipt) { r.ReceiptId = "" }},
		{"empty tool_id", func(r *UsageReceipt) { r.ToolId = "" }},
		{"empty request_id", func(r *UsageReceipt) { r.RequestId = "" }},
		{"empty request_hash", func(r *UsageReceipt) { r.RequestHash = nil }},
		{"empty unit", func(r *UsageReceipt) { r.Unit = "" }},
		{"empty router_address", func(r *UsageReceipt) { r.RouterAddress = "" }},
		{"empty user_address", func(r *UsageReceipt) { r.UserAddress = "" }},
		{"nil timestamp", func(r *UsageReceipt) { r.Timestamp = nil }},
		{"nil expires_at", func(r *UsageReceipt) { r.ExpiresAt = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validUsageReceipt(t)
			tt.mutate(r)
			require.Error(t, r.Validate())
		})
	}
}

func TestUsageReceiptValidate_InvalidUnits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		unit string
	}{
		{name: "unknown_unit", unit: "call"},
		{name: "padded_unit", unit: " req"},
		{name: "uppercase_unit", unit: "REQ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validUsageReceipt(t)
			r.Unit = tt.unit
			require.Error(t, r.Validate())
		})
	}
}

func TestUsageReceiptValidate_InvalidDecimals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*UsageReceipt)
	}{
		{"invalid units_used", func(r *UsageReceipt) { r.UnitsUsed = "not-a-number" }},
		{"negative units_used", func(r *UsageReceipt) { r.UnitsUsed = "-1" }},
		{"invalid price_per_unit", func(r *UsageReceipt) { r.PricePerUnit = "abc" }},
		{"negative price_per_unit", func(r *UsageReceipt) { r.PricePerUnit = "-0.01" }},
		{"invalid quoted_amount", func(r *UsageReceipt) { r.QuotedAmount = "xyz" }},
		{"negative quoted_amount", func(r *UsageReceipt) { r.QuotedAmount = "-1" }},
		{"invalid actual_amount", func(r *UsageReceipt) { r.ActualAmount = "bad" }},
		{"negative actual_amount", func(r *UsageReceipt) { r.ActualAmount = "-1" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validUsageReceipt(t)
			tt.mutate(r)
			require.Error(t, r.Validate())
		})
	}
}

func TestUsageReceiptValidate_InvalidAddresses(t *testing.T) {
	t.Parallel()
	r := validUsageReceipt(t)
	r.RouterAddress = "not-bech32"
	require.Error(t, r.Validate())

	r2 := validUsageReceipt(t)
	r2.UserAddress = "not-bech32"
	require.Error(t, r2.Validate())
}

func TestUsageReceiptValidate_ExpiresBeforeTimestamp(t *testing.T) {
	t.Parallel()
	r := validUsageReceipt(t)
	r.ExpiresAt = timestamppb.New(r.Timestamp.AsTime().Add(-time.Hour))
	require.Error(t, r.Validate())
}

func TestUsageReceiptValidate_InvalidStatus(t *testing.T) {
	t.Parallel()
	r := validUsageReceipt(t)
	r.Status = "invalid_status"
	require.Error(t, r.Validate())
}

func TestUsageReceiptValidate_ValidStatuses(t *testing.T) {
	t.Parallel()
	for _, status := range []string{ReceiptStatusPending, ReceiptStatusSettled, ReceiptStatusDisputed, ReceiptStatusExpired, ""} {
		r := validUsageReceipt(t)
		r.Status = status
		require.NoError(t, r.Validate(), "status %q should be valid", status)
	}
}

func TestUsageReceiptValidate_InvalidOriginID(t *testing.T) {
	t.Parallel()
	r := validUsageReceipt(t)
	r.OriginId = "no-separator"
	require.Error(t, r.Validate())
}

func TestUsageReceiptValidate_ValidOriginID(t *testing.T) {
	t.Parallel()
	r := validUsageReceipt(t)
	r.OriginId = "injective:iagent"
	require.NoError(t, r.Validate())
}

// ---------------------------------------------------------------------------
// Challenge.Validate()
// ---------------------------------------------------------------------------

func validChallenge(t *testing.T) *Challenge {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return &Challenge{
		ChallengeId:       "chal-001",
		ReceiptId:         "rcpt-001",
		ChallengerAddress: testBech32Addr(t),
		ChallengerStake:   []*v1beta1.Coin{{Denom: "ulume", Amount: "1000"}},
		Reason:            "SLA violation",
		ChallengedAt:      timestamppb.New(now),
		DeadlineAt:        timestamppb.New(now.Add(48 * time.Hour)),
	}
}

func TestChallengeValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validChallenge(t).Validate())
}

func TestChallengeValidate_Nil(t *testing.T) {
	t.Parallel()
	var c *Challenge
	require.Error(t, c.Validate())
}

func TestChallengeValidate_EmptyFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Challenge)
	}{
		{"empty challenge_id", func(c *Challenge) { c.ChallengeId = "" }},
		{"empty receipt_id", func(c *Challenge) { c.ReceiptId = "" }},
		{"empty challenger_address", func(c *Challenge) { c.ChallengerAddress = "" }},
		{"empty challenger_stake", func(c *Challenge) { c.ChallengerStake = nil }},
		{"empty reason", func(c *Challenge) { c.Reason = "" }},
		{"nil challenged_at", func(c *Challenge) { c.ChallengedAt = nil }},
		{"nil deadline_at", func(c *Challenge) { c.DeadlineAt = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validChallenge(t)
			tt.mutate(c)
			require.Error(t, c.Validate())
		})
	}
}

func TestChallengeValidate_InvalidAddress(t *testing.T) {
	t.Parallel()
	c := validChallenge(t)
	c.ChallengerAddress = "not-bech32"
	require.Error(t, c.Validate())
}

func TestChallengeValidate_DeadlineBeforeChallenge(t *testing.T) {
	t.Parallel()
	c := validChallenge(t)
	c.DeadlineAt = timestamppb.New(c.ChallengedAt.AsTime().Add(-time.Hour))
	require.Error(t, c.Validate())
}

// ---------------------------------------------------------------------------
// BondRecord.Validate()
// ---------------------------------------------------------------------------

func validBondRecord(t *testing.T) *BondRecord {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return &BondRecord{
		ToolId:        "defi.token-price",
		Owner:         testBech32Addr(t),
		BondedAmount:  []*v1beta1.Coin{{Denom: "ulume", Amount: "5000"}},
		Status:        BondStatusActive,
		BondedAt:      timestamppb.New(now),
		LastUpdatedAt: timestamppb.New(now),
	}
}

func TestBondRecordValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validBondRecord(t).Validate())
}

func TestBondRecordValidate_Nil(t *testing.T) {
	t.Parallel()
	var b *BondRecord
	require.Error(t, b.Validate())
}

func TestBondRecordValidate_EmptyToolID(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.ToolId = ""
	require.Error(t, b.Validate())
}

func TestBondRecordValidate_EmptyOwner(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.Owner = ""
	require.Error(t, b.Validate())
}

func TestBondRecordValidate_InvalidOwnerAddress(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.Owner = "invalid-address"
	require.Error(t, b.Validate())
}

func TestBondRecordValidate_EmptyBondedAmountActiveStatus(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.BondedAmount = nil
	require.Error(t, b.Validate())
}

func TestBondRecordValidate_EmptyBondedAmountAllowedForWithdrawn(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.BondedAmount = nil
	b.Status = BondStatusWithdrawn
	require.NoError(t, b.Validate())
}

func TestBondRecordValidate_EmptyBondedAmountAllowedForSlashed(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.BondedAmount = nil
	b.Status = BondStatusSlashed
	require.NoError(t, b.Validate())
}

func TestBondRecordValidate_InvalidInsurancePremiumMultiplier(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.InsurancePremiumMultiplier = "not-a-number"
	require.Error(t, b.Validate())
}

func TestBondRecordValidate_NegativeInsurancePremiumMultiplier(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.InsurancePremiumMultiplier = "-1.5"
	require.Error(t, b.Validate())
}

func TestBondRecordValidate_ValidInsurancePremiumMultiplier(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.InsurancePremiumMultiplier = "1.5"
	require.NoError(t, b.Validate())
}

func TestBondRecordValidate_InvalidStatus(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.Status = "unknown_status"
	require.Error(t, b.Validate())
}

func TestBondRecordValidate_ValidStatuses(t *testing.T) {
	t.Parallel()
	for _, status := range []string{BondStatusActive, BondStatusWithdrawing, BondStatusWithdrawn, BondStatusSlashed, ""} {
		b := validBondRecord(t)
		b.Status = status
		if status == BondStatusWithdrawn || status == BondStatusSlashed {
			b.BondedAmount = nil
		}
		require.NoError(t, b.Validate(), "status %q should be valid", status)
	}
}

func TestBondRecordValidate_NilTimestamps(t *testing.T) {
	t.Parallel()
	b := validBondRecord(t)
	b.BondedAt = nil
	require.Error(t, b.Validate())

	b2 := validBondRecord(t)
	b2.LastUpdatedAt = nil
	require.Error(t, b2.Validate())
}

// ---------------------------------------------------------------------------
// Pricing.Validate()
// ---------------------------------------------------------------------------

func validPricing() *Pricing {
	return &Pricing{
		Model:        "per_call",
		Unit:         "req",
		PricePerUnit: "0.10",
	}
}

func TestPricingValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validPricing().Validate())
}

func TestPricingValidate_Nil(t *testing.T) {
	t.Parallel()
	var p *Pricing
	require.Error(t, p.Validate())
}

func TestPricingValidate_InvalidModel(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.Model = "invalid_model"
	require.Error(t, p.Validate())
}

func TestPricingValidate_ValidModels(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"per_call", "per_unit", "per_byte", "per_token"} {
		p := validPricing()
		p.Model = model
		require.NoError(t, p.Validate(), "model %q should be valid", model)
	}
}

func TestPricingValidate_InvalidUnit(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.Unit = "invalid_unit"
	require.Error(t, p.Validate())
}

func TestPricingValidate_ValidUnits(t *testing.T) {
	t.Parallel()
	for _, unit := range []string{"req", "page", "sec", "token", "byte"} {
		p := validPricing()
		p.Unit = unit
		require.NoError(t, p.Validate(), "unit %q should be valid", unit)
	}
}

func TestPricingValidate_InvalidPricePerUnit(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.PricePerUnit = "not-a-number"
	require.Error(t, p.Validate())
}

func TestPricingValidate_NegativePricePerUnit(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.PricePerUnit = "-0.01"
	require.Error(t, p.Validate())
}

func TestPricingValidate_ZeroPricePerUnit(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.PricePerUnit = "0"
	require.NoError(t, p.Validate())
}

func TestPricingValidate_CostBounds(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.MinimumCost = "0.05"
	p.MaximumCost = "1.00"
	require.NoError(t, p.Validate())

	// Optional bounds may be supplied independently.
	pMinOnly := validPricing()
	pMinOnly.MinimumCost = "0.05"
	require.NoError(t, pMinOnly.Validate())

	pMaxOnly := validPricing()
	pMaxOnly.MaximumCost = "1.00"
	require.NoError(t, pMaxOnly.Validate())

	// max < min
	p2 := validPricing()
	p2.MinimumCost = "1.00"
	p2.MaximumCost = "0.05"
	require.Error(t, p2.Validate())
}

func TestPricingValidate_NegativeCosts(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.MinimumCost = "-0.01"
	require.Error(t, p.Validate())

	p2 := validPricing()
	p2.MaximumCost = "-0.01"
	require.Error(t, p2.Validate())
}

func TestPricingValidate_InvalidCostStrings(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.MinimumCost = "abc"
	require.Error(t, p.Validate())

	p2 := validPricing()
	p2.MaximumCost = "xyz"
	require.Error(t, p2.Validate())
}

// TestPricingValidate_RejectsAbsurdExponent is the consensus-critical
// regression for the shopspring-decimal DoS vector on MsgRegisterTool /
// MsgUpdateTool. Pricing.Validate() is called by the registry keeper's
// ValidateToolCard helper on the consensus path; a malicious tool-owner
// submitting pricing fields like "1e11100100" would otherwise hang every
// validator's Validate() call when maxCost.LessThan(minCost) forces
// shopspring to expand the symbolic big.Int to match exponents. Block
// production halts = chain halt. The moneyguard.IsSafeExponent gates at
// the three parse sites reject before the vulnerable arithmetic.
func TestPricingValidate_RejectsAbsurdExponent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		mutate   func(*Pricing)
		contains string
	}{
		{
			name:     "PricePerUnit",
			mutate:   func(p *Pricing) { p.PricePerUnit = "1e11100100" },
			contains: "price per unit magnitude",
		},
		{
			name: "MinimumCost",
			mutate: func(p *Pricing) {
				p.MinimumCost = "1e11100100"
				p.MaximumCost = "1.00"
			},
			contains: "minimum cost magnitude",
		},
		{
			name: "MaximumCost",
			mutate: func(p *Pricing) {
				p.MinimumCost = "0.05"
				p.MaximumCost = "1e11100100"
			},
			contains: "maximum cost magnitude",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPricing()
			tc.mutate(p)
			err := p.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.contains)
		})
	}
	// Sanity: legitimate scientific-notation (well within moneyguard bound)
	// still validates.
	t.Run("LegitimateScientificNotationAccepted", func(t *testing.T) {
		p := validPricing()
		p.PricePerUnit = "1e-6"
		require.NoError(t, p.Validate())
	})
}

// TestParamsValidate_RejectsAbsurdInsuranceTargetUtil is the regression
// for the same consensus-halt class on x/registry Params: the parameter
// set is applied via MsgUpdateParams (governance); validation runs on
// every validator and does `insTarget.GreaterThan(decimal.NewFromInt(1))`,
// which would expand a symbolic big.Int and halt block production.
func TestParamsValidate_RejectsAbsurdInsuranceTargetUtil(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.InsuranceTargetUtil = "1e11100100"
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "insurance target utilization magnitude")

	// Legitimate small-exponent value still validates.
	pOK := DefaultParams()
	pOK.InsuranceTargetUtil = "0.3"
	require.NoError(t, pOK.Validate())
}

func TestParamsValidate_RejectsOverflowingDurationSeconds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		mutate   func(*Params)
		contains string
	}{
		{
			name: "SettlementPeriodBlocks",
			mutate: func(p *Params) {
				p.SettlementPeriodBlocks = maxSafeSettlementPeriodBlocks + 1
			},
			contains: "settlement period exceeds maximum safe duration blocks",
		},
		{
			name: "SlashingGracePeriod",
			mutate: func(p *Params) {
				p.SlashingGracePeriod = maxSafeDurationSeconds + 1
			},
			contains: "slashing grace period exceeds maximum safe duration seconds",
		},
		{
			name: "DisputeWindowSeconds",
			mutate: func(p *Params) {
				p.DisputeWindowSeconds = maxSafeDurationSeconds + 1
			},
			contains: "dispute window exceeds maximum safe duration seconds",
		},
		{
			name: "DisputeWindowSecondsRegistryParamsWidth",
			mutate: func(p *Params) {
				p.DisputeWindowSeconds = maxRegistryParamsDurationSeconds + 1
			},
			contains: "dispute window exceeds maximum registry params seconds",
		},
		{
			name: "ChallengeResolutionDeadlineSeconds",
			mutate: func(p *Params) {
				p.ChallengeResolutionDeadlineSeconds = maxSafeDurationSeconds + 1
			},
			contains: "challenge resolution deadline exceeds maximum safe duration seconds",
		},
		{
			name: "ChallengeResolutionDeadlineSecondsRegistryParamsWidth",
			mutate: func(p *Params) {
				p.ChallengeResolutionDeadlineSeconds = maxRegistryParamsDurationSeconds + 1
			},
			contains: "challenge resolution deadline exceeds maximum registry params seconds",
		},
		{
			name: "SettledReceiptRetentionSeconds",
			mutate: func(p *Params) {
				p.SettledReceiptRetentionSeconds = maxSafeDurationSeconds + 1
			},
			contains: "settled receipt retention exceeds maximum safe duration seconds",
		},
		{
			name: "SettledReceiptRetentionSecondsRegistryParamsWidth",
			mutate: func(p *Params) {
				p.SettledReceiptRetentionSeconds = maxRegistryParamsDurationSeconds + 1
			},
			contains: "settled receipt retention exceeds maximum registry params seconds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := DefaultParams()
			tc.mutate(p)

			err := p.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.contains)
		})
	}

	p := DefaultParams()
	p.SettlementPeriodBlocks = maxSafeSettlementPeriodBlocks
	p.SlashingGracePeriod = maxSafeDurationSeconds
	p.DisputeWindowSeconds = maxRegistryParamsDurationSeconds
	p.ChallengeResolutionDeadlineSeconds = maxRegistryParamsDurationSeconds
	p.SettledReceiptRetentionSeconds = maxRegistryParamsDurationSeconds
	require.NoError(t, p.Validate())
}

func TestParamsValidate_RecursiveRoyaltyGovernanceBounds(t *testing.T) {
	t.Parallel()

	defaults := DefaultParams()
	require.Equal(t, uint32(DefaultRecursiveRoyaltyMaxDepth), defaults.RecursiveRoyaltyMaxDepth)
	require.Equal(t, uint32(DefaultRecursiveRoyaltyMaxAggregateBps), defaults.RecursiveRoyaltyMaxAggregateBps)
	require.NoError(t, defaults.Validate())

	cases := []struct {
		name     string
		mutate   func(*Params)
		contains string
	}{
		{
			name: "ZeroDepth",
			mutate: func(p *Params) {
				p.RecursiveRoyaltyMaxDepth = 0
			},
			contains: "recursive royalty max depth must be > 0",
		},
		{
			name: "DepthTooLarge",
			mutate: func(p *Params) {
				p.RecursiveRoyaltyMaxDepth = maxRecursiveRoyaltyDepth + 1
			},
			contains: "recursive royalty max depth must be <=",
		},
		{
			name: "ZeroAggregateBps",
			mutate: func(p *Params) {
				p.RecursiveRoyaltyMaxAggregateBps = 0
			},
			contains: "recursive royalty max aggregate bps must be > 0",
		},
		{
			name: "AggregateBpsTooLarge",
			mutate: func(p *Params) {
				p.RecursiveRoyaltyMaxAggregateBps = BPSDenominator + 1
			},
			contains: "recursive royalty max aggregate bps must be <=",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := DefaultParams()
			tc.mutate(p)

			err := p.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.contains)
		})
	}
}

func TestValidatePositiveSettlementPeriodBlocksRejectsOverflow(t *testing.T) {
	t.Parallel()

	require.NoError(t, validatePositiveSettlementPeriodBlocksInt64(maxSafeSettlementPeriodBlocks))

	err := validatePositiveSettlementPeriodBlocksInt64(maxSafeSettlementPeriodBlocks + 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "settlement period blocks exceeds maximum safe value")
}

func TestValidatePositiveDurationSecondsInt64RejectsOverflow(t *testing.T) {
	t.Parallel()

	require.NoError(t, validatePositiveDurationSecondsInt64(maxSafeDurationSeconds))

	err := validatePositiveDurationSecondsInt64(maxSafeDurationSeconds + 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duration seconds exceeds maximum safe value")

	err = validatePositiveDurationSecondsInt64(int64(0))
	require.Error(t, err)
	require.Contains(t, err.Error(), "value must be positive")

	err = validatePositiveDurationSecondsInt64("600")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid parameter type")
}

func TestValidateRegistryParamsDurationSecondsInt64RejectsUint32Overflow(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateRegistryParamsDurationSecondsInt64(maxRegistryParamsDurationSeconds))

	err := validateRegistryParamsDurationSecondsInt64(maxRegistryParamsDurationSeconds + 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duration seconds exceeds maximum registry params value")

	err = validateRegistryParamsDurationSecondsInt64(maxSafeDurationSeconds + 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duration seconds exceeds maximum safe value")
}

// TestSLOValidate_RejectsAbsurdExponent is the parallel regression for
// SLO.Validate: Availability is compared against 0 and 1 with
// LessThanOrEqual / GreaterThan, which expand shopspring's big.Int for a
// symbolic exponent. Same consensus-halt DoS surface as Pricing.
func TestSLOValidate_RejectsAbsurdExponent(t *testing.T) {
	t.Parallel()
	s := &SLO{
		P95LatencyMs: 100,
		Availability: "1e11100100",
		TimeoutMs:    1000,
	}
	err := s.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "availability magnitude")

	// Legitimate small-exponent value still validates.
	sOK := &SLO{
		P95LatencyMs: 100,
		Availability: "0.99",
		TimeoutMs:    1000,
	}
	require.NoError(t, sOK.Validate())
}

func TestPricingValidate_QuoteEndpoint(t *testing.T) {
	t.Parallel()
	p := validPricing()
	p.QuoteEndpoint = "https://api.example.com/quote"
	require.NoError(t, p.Validate())

	p2 := validPricing()
	p2.QuoteEndpoint = "http://not-https.com/quote"
	require.Error(t, p2.Validate())
}

func TestPricingValidate_QuoteEndpointRejectsWhitespaceAndControl(t *testing.T) {
	t.Parallel()
	tests := []string{
		" https://api.example.com/quote",
		"https://api.example.com/quote ",
		"https://api.example.com/quo te",
		"https://api.example.com/quote\nnext",
		"https://api.example.com/quote\x7f",
	}

	for _, raw := range tests {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			t.Parallel()
			p := validPricing()
			p.QuoteEndpoint = raw
			err := p.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "whitespace or control")
		})
	}
}

// ---------------------------------------------------------------------------
// SLO.Validate()
// ---------------------------------------------------------------------------

func validSLO() *SLO {
	return &SLO{
		P95LatencyMs: 2500,
		Availability: "0.99",
		ErrorRateBps: 100,
		TimeoutMs:    5000,
	}
}

func TestSLOValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validSLO().Validate())
}

func TestSLOValidate_Nil(t *testing.T) {
	t.Parallel()
	var s *SLO
	require.Error(t, s.Validate())
}

func TestSLOValidate_ZeroP95(t *testing.T) {
	t.Parallel()
	s := validSLO()
	s.P95LatencyMs = 0
	require.Error(t, s.Validate())
}

func TestSLOValidate_InvalidAvailability(t *testing.T) {
	t.Parallel()
	s := validSLO()
	s.Availability = "not-a-number"
	require.Error(t, s.Validate())
}

func TestSLOValidate_AvailabilityOutOfRange(t *testing.T) {
	t.Parallel()
	// zero
	s := validSLO()
	s.Availability = "0"
	require.Error(t, s.Validate())

	// over 1
	s2 := validSLO()
	s2.Availability = "1.01"
	require.Error(t, s2.Validate())

	// exactly 1 is ok
	s3 := validSLO()
	s3.Availability = "1"
	require.NoError(t, s3.Validate())
}

func TestSLOValidate_ErrorRateTooHigh(t *testing.T) {
	t.Parallel()
	s := validSLO()
	s.ErrorRateBps = 10001
	require.Error(t, s.Validate())
}

func TestSLOValidate_ZeroTimeout(t *testing.T) {
	t.Parallel()
	s := validSLO()
	s.TimeoutMs = 0
	require.Error(t, s.Validate())
}

// ---------------------------------------------------------------------------
// SandboxProfile.Validate()
// ---------------------------------------------------------------------------

func validSandbox() *SandboxProfile {
	return &SandboxProfile{
		Profile:         "defi-analytics",
		MaxExecutionSec: 30,
	}
}

func TestSandboxProfileValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validSandbox().Validate())
}

func TestSandboxProfileValidate_Nil(t *testing.T) {
	t.Parallel()
	var s *SandboxProfile
	require.Error(t, s.Validate())
}

func TestSandboxProfileValidate_InvalidProfile(t *testing.T) {
	t.Parallel()
	s := validSandbox()
	s.Profile = "unknown-profile"
	require.Error(t, s.Validate())
}

func TestSandboxProfileValidate_ValidProfiles(t *testing.T) {
	t.Parallel()
	profiles := []string{"scrape", "docs-search", "defi-analytics", "onchain", "community", "enclave", "enclave-sovereign"}
	for _, p := range profiles {
		s := validSandbox()
		s.Profile = p
		require.NoError(t, s.Validate(), "profile %q should be valid", p)
	}
}

func TestSandboxProfileValidate_ZeroExecution(t *testing.T) {
	t.Parallel()
	s := validSandbox()
	s.MaxExecutionSec = 0
	require.Error(t, s.Validate())
}

func TestSandboxProfileValidate_EgressAllowlist(t *testing.T) {
	t.Parallel()
	s := validSandbox()
	s.EgressAllowlist = []string{"https://api.example.com"}
	require.NoError(t, s.Validate())

	s2 := validSandbox()
	s2.EgressAllowlist = []string{"ftp://bad.com"}
	require.Error(t, s2.Validate())
}

func TestSandboxProfileValidate_PiiHandling(t *testing.T) {
	t.Parallel()
	for _, pii := range []string{"none", "hashed", "redacted", ""} {
		s := validSandbox()
		s.PiiHandling = pii
		require.NoError(t, s.Validate(), "pii %q should be valid", pii)
	}

	s := validSandbox()
	s.PiiHandling = "invalid-pii"
	require.Error(t, s.Validate())
}

// ---------------------------------------------------------------------------
// CachePolicy.Validate()
// ---------------------------------------------------------------------------

func TestCachePolicyValidate_NilOK(t *testing.T) {
	t.Parallel()
	var c *CachePolicy
	require.NoError(t, c.Validate())
}

func TestCachePolicyValidate_Valid(t *testing.T) {
	t.Parallel()
	c := &CachePolicy{Enabled: true, TtlSeconds: 3600, RoyaltyShareBps: 500}
	require.NoError(t, c.Validate())
}

func TestCachePolicyValidate_EnabledZeroTTL(t *testing.T) {
	t.Parallel()
	c := &CachePolicy{Enabled: true, TtlSeconds: 0}
	require.Error(t, c.Validate())
}

func TestCachePolicyValidate_DisabledZeroTTL(t *testing.T) {
	t.Parallel()
	c := &CachePolicy{Enabled: false, TtlSeconds: 0}
	require.NoError(t, c.Validate())
}

func TestCachePolicyValidate_RoyaltyExceedsBPS(t *testing.T) {
	t.Parallel()
	c := &CachePolicy{Enabled: false, RoyaltyShareBps: BPSDenominator + 1}
	require.Error(t, c.Validate())
}

func TestCachePolicyValidate_RoyaltyMaxBPS(t *testing.T) {
	t.Parallel()
	c := &CachePolicy{Enabled: false, RoyaltyShareBps: BPSDenominator}
	require.NoError(t, c.Validate())
}

// ---------------------------------------------------------------------------
// Endpoint.Validate()
// ---------------------------------------------------------------------------

func TestEndpointValidate_Valid(t *testing.T) {
	t.Parallel()
	e := &Endpoint{Protocol: "https", Url: "https://api.example.com/invoke"}
	require.NoError(t, e.Validate())
}

func TestEndpointValidate_Nil(t *testing.T) {
	t.Parallel()
	var e *Endpoint
	require.Error(t, e.Validate())
}

func TestEndpointValidate_InvalidProtocol(t *testing.T) {
	t.Parallel()
	e := &Endpoint{Protocol: "http", Url: "https://api.example.com"}
	require.Error(t, e.Validate())
}

func TestEndpointValidate_InvalidURL(t *testing.T) {
	t.Parallel()
	e := &Endpoint{Protocol: "https", Url: "http://not-https.com"}
	require.Error(t, e.Validate())
}

func TestEndpointValidate_RejectsWhitespaceAndControlURL(t *testing.T) {
	t.Parallel()
	tests := []string{
		" https://api.example.com/invoke",
		"https://api.example.com/invoke ",
		"https://api.example.com/inv oke",
		"https://api.example.com/invoke\nnext",
		"https://api.example.com/invoke\x7f",
	}

	for _, raw := range tests {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			t.Parallel()
			e := &Endpoint{Protocol: "https", Url: raw}
			err := e.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "whitespace or control")
		})
	}
}

// ---------------------------------------------------------------------------
// PendingSlash.Validate()
// ---------------------------------------------------------------------------

func validPendingSlash() *PendingSlash {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return &PendingSlash{
		SlashId:    "slash-001",
		Amount:     []*v1beta1.Coin{{Denom: "ulume", Amount: "500"}},
		Reason:     "SLA violation",
		ProposedAt: timestamppb.New(now),
		ExecuteAt:  timestamppb.New(now.Add(24 * time.Hour)),
	}
}

func TestPendingSlashValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validPendingSlash().Validate())
}

func TestPendingSlashValidate_Nil(t *testing.T) {
	t.Parallel()
	var p *PendingSlash
	require.Error(t, p.Validate())
}

func TestPendingSlashValidate_EmptyFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*PendingSlash)
	}{
		{"empty slash_id", func(p *PendingSlash) { p.SlashId = "" }},
		{"empty amount", func(p *PendingSlash) { p.Amount = nil }},
		{"empty reason", func(p *PendingSlash) { p.Reason = "" }},
		{"nil proposed_at", func(p *PendingSlash) { p.ProposedAt = nil }},
		{"nil execute_at", func(p *PendingSlash) { p.ExecuteAt = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validPendingSlash()
			tt.mutate(p)
			require.Error(t, p.Validate())
		})
	}
}

func TestPendingSlashValidate_ExecuteBeforeProposed(t *testing.T) {
	t.Parallel()
	p := validPendingSlash()
	p.ExecuteAt = timestamppb.New(p.ProposedAt.AsTime().Add(-time.Hour))
	require.Error(t, p.Validate())
}

// ---------------------------------------------------------------------------
// SettlementRecord.Validate()
// ---------------------------------------------------------------------------

func validSettlementRecord(t *testing.T) *SettlementRecord {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return &SettlementRecord{
		ReceiptId:        "rcpt-001",
		ToolId:           "defi.token-price",
		LockedQuote:      "0.10",
		ActualSettled:    "0.08",
		BurnAmount:       "0.003",
		PublisherAddress: testBech32Addr(t),
		RouterAddress:    testBech32Addr2(t),
		UserAddress:      testBech32Addr3(t),
		SettledAt:        timestamppb.New(now),
	}
}

func TestSettlementRecordValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validSettlementRecord(t).Validate())
}

func TestSettlementRecordValidate_Nil(t *testing.T) {
	t.Parallel()
	var s *SettlementRecord
	require.Error(t, s.Validate())
}

func TestSettlementRecordValidate_EmptyFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*SettlementRecord)
	}{
		{"empty receipt_id", func(s *SettlementRecord) { s.ReceiptId = "" }},
		{"empty tool_id", func(s *SettlementRecord) { s.ToolId = "" }},
		{"empty publisher_address", func(s *SettlementRecord) { s.PublisherAddress = "" }},
		{"empty router_address", func(s *SettlementRecord) { s.RouterAddress = "" }},
		{"empty user_address", func(s *SettlementRecord) { s.UserAddress = "" }},
		{"nil settled_at", func(s *SettlementRecord) { s.SettledAt = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSettlementRecord(t)
			tt.mutate(s)
			require.Error(t, s.Validate())
		})
	}
}

func TestSettlementRecordValidate_InvalidDecimals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*SettlementRecord)
	}{
		{"invalid locked_quote", func(s *SettlementRecord) { s.LockedQuote = "abc" }},
		{"negative locked_quote", func(s *SettlementRecord) { s.LockedQuote = "-1" }},
		{"invalid actual_settled", func(s *SettlementRecord) { s.ActualSettled = "xyz" }},
		{"negative actual_settled", func(s *SettlementRecord) { s.ActualSettled = "-1" }},
		{"invalid burn_amount", func(s *SettlementRecord) { s.BurnAmount = "bad" }},
		{"negative burn_amount", func(s *SettlementRecord) { s.BurnAmount = "-1" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSettlementRecord(t)
			tt.mutate(s)
			require.Error(t, s.Validate())
		})
	}
}

func TestSettlementRecordValidate_InvalidAddresses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*SettlementRecord)
	}{
		{"invalid publisher_address", func(s *SettlementRecord) { s.PublisherAddress = "bad" }},
		{"invalid router_address", func(s *SettlementRecord) { s.RouterAddress = "bad" }},
		{"invalid user_address", func(s *SettlementRecord) { s.UserAddress = "bad" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSettlementRecord(t)
			tt.mutate(s)
			require.Error(t, s.Validate())
		})
	}
}

func TestSettlementRecordValidate_EmptyOptionalDecimals(t *testing.T) {
	t.Parallel()
	s := validSettlementRecord(t)
	s.LockedQuote = ""
	s.ActualSettled = ""
	s.BurnAmount = ""
	require.NoError(t, s.Validate())
}

// ---------------------------------------------------------------------------
// WatcherRecord.Validate()
// ---------------------------------------------------------------------------

func validWatcherRecord(t *testing.T) *WatcherRecord {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return &WatcherRecord{
		WatcherAddress: testBech32Addr(t),
		Status:         WatcherStatusActive,
		BondedAmount:   []*v1beta1.Coin{{Denom: "ulume", Amount: "5000"}},
		RegisteredAt:   timestamppb.New(now),
		LastUpdatedAt:  timestamppb.New(now),
	}
}

func TestWatcherRecordValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validWatcherRecord(t).Validate())
}

func TestWatcherRecordValidate_Nil(t *testing.T) {
	t.Parallel()
	var w *WatcherRecord
	require.Error(t, w.Validate())
}

func TestWatcherRecordValidate_EmptyAddress(t *testing.T) {
	t.Parallel()
	w := validWatcherRecord(t)
	w.WatcherAddress = ""
	require.Error(t, w.Validate())
}

func TestWatcherRecordValidate_InvalidAddress(t *testing.T) {
	t.Parallel()
	w := validWatcherRecord(t)
	w.WatcherAddress = "not-bech32"
	require.Error(t, w.Validate())
}

func TestWatcherRecordValidate_InvalidStatus(t *testing.T) {
	t.Parallel()
	w := validWatcherRecord(t)
	w.Status = "unknown_status"
	require.Error(t, w.Validate())
}

func TestWatcherRecordValidate_ValidStatuses(t *testing.T) {
	t.Parallel()
	for _, status := range []string{WatcherStatusActive, WatcherStatusJailed, WatcherStatusSlashed, ""} {
		w := validWatcherRecord(t)
		w.Status = status
		if status == WatcherStatusSlashed {
			w.BondedAmount = nil
		}
		require.NoError(t, w.Validate(), "status %q should be valid", status)
	}
}

func TestWatcherRecordValidate_EmptyBondedAmountActiveStatus(t *testing.T) {
	t.Parallel()
	w := validWatcherRecord(t)
	w.BondedAmount = nil
	require.Error(t, w.Validate())
}

func TestWatcherRecordValidate_EmptyBondedAmountSlashedOK(t *testing.T) {
	t.Parallel()
	w := validWatcherRecord(t)
	w.Status = WatcherStatusSlashed
	w.BondedAmount = nil
	require.NoError(t, w.Validate())
}

func TestWatcherRecordValidate_NilTimestamps(t *testing.T) {
	t.Parallel()
	w := validWatcherRecord(t)
	w.RegisteredAt = nil
	require.Error(t, w.Validate())

	w2 := validWatcherRecord(t)
	w2.LastUpdatedAt = nil
	require.Error(t, w2.Validate())
}

// ---------------------------------------------------------------------------
// SLOProbeReceipt.Validate()
// ---------------------------------------------------------------------------

func validSLOProbeReceipt(t *testing.T) *SLOProbeReceipt {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return &SLOProbeReceipt{
		ReceiptId:       "probe-001",
		ToolId:          "defi.token-price",
		WatcherAddress:  testBech32Addr(t),
		WindowStart:     timestamppb.New(now),
		WindowEnd:       timestamppb.New(now.Add(time.Hour)),
		ProbeCount:      100,
		SuccessCount:    95,
		FailureCount:    5,
		P95LatencyMs:    2000,
		ErrorRateBps:    500,
		AvailabilityBps: 9500,
		SubmittedAt:     timestamppb.New(now.Add(time.Hour + time.Minute)),
	}
}

func TestSLOProbeReceiptValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validSLOProbeReceipt(t).Validate())
}

func TestSLOProbeReceiptValidate_Nil(t *testing.T) {
	t.Parallel()
	var r *SLOProbeReceipt
	require.Error(t, r.Validate())
}

func TestSLOProbeReceiptValidate_EmptyFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*SLOProbeReceipt)
	}{
		{"empty receipt_id", func(r *SLOProbeReceipt) { r.ReceiptId = "" }},
		{"empty tool_id", func(r *SLOProbeReceipt) { r.ToolId = "" }},
		{"empty watcher_address", func(r *SLOProbeReceipt) { r.WatcherAddress = "" }},
		{"nil window_start", func(r *SLOProbeReceipt) { r.WindowStart = nil }},
		{"nil window_end", func(r *SLOProbeReceipt) { r.WindowEnd = nil }},
		{"nil submitted_at", func(r *SLOProbeReceipt) { r.SubmittedAt = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validSLOProbeReceipt(t)
			tt.mutate(r)
			require.Error(t, r.Validate())
		})
	}
}

func TestSLOProbeReceiptValidate_PaddedToolID(t *testing.T) {
	t.Parallel()
	r := validSLOProbeReceipt(t)
	r.ToolId = " defi.token-price "
	require.ErrorContains(t, r.Validate(), "tool_id cannot contain leading or trailing whitespace")
}

func TestSLOProbeReceiptValidate_InvalidAddress(t *testing.T) {
	t.Parallel()
	r := validSLOProbeReceipt(t)
	r.WatcherAddress = "bad"
	require.Error(t, r.Validate())
}

func TestSLOProbeReceiptValidate_WindowEndBeforeStart(t *testing.T) {
	t.Parallel()
	r := validSLOProbeReceipt(t)
	r.WindowEnd = timestamppb.New(r.WindowStart.AsTime().Add(-time.Hour))
	require.Error(t, r.Validate())
}

func TestSLOProbeReceiptValidate_ZeroProbeCount(t *testing.T) {
	t.Parallel()
	r := validSLOProbeReceipt(t)
	r.ProbeCount = 0
	require.Error(t, r.Validate())
}

func TestSLOProbeReceiptValidate_CountsExceedProbeCount(t *testing.T) {
	t.Parallel()
	r := validSLOProbeReceipt(t)
	r.SuccessCount = 60
	r.FailureCount = 50
	r.ProbeCount = 100
	require.Error(t, r.Validate())
}

func TestSLOProbeReceiptValidate_ZeroP95Latency(t *testing.T) {
	t.Parallel()
	r := validSLOProbeReceipt(t)
	r.P95LatencyMs = 0
	require.Error(t, r.Validate())
}

func TestSLOProbeReceiptValidate_BPSExceeds10000(t *testing.T) {
	t.Parallel()
	r := validSLOProbeReceipt(t)
	r.ErrorRateBps = 10001
	require.Error(t, r.Validate())

	r2 := validSLOProbeReceipt(t)
	r2.AvailabilityBps = 10001
	require.Error(t, r2.Validate())
}

// ---------------------------------------------------------------------------
// SLOProbeAggregate.Validate()
// ---------------------------------------------------------------------------

func validSLOProbeAggregate(t *testing.T) *SLOProbeAggregate {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return &SLOProbeAggregate{
		ToolId:                "defi.token-price",
		WindowStart:           timestamppb.New(now),
		WindowEnd:             timestamppb.New(now.Add(time.Hour)),
		WatcherCount:          5,
		MedianP95LatencyMs:    1800,
		MedianAvailabilityBps: 9500,
		MedianErrorRateBps:    200,
		AggregatedAt:          timestamppb.New(now.Add(time.Hour + time.Minute)),
		Version:               1,
		Status:                SLOProbeAggregateStatusFinal,
	}
}

func TestSLOProbeAggregateValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validSLOProbeAggregate(t).Validate())
}

func TestSLOProbeAggregateValidate_Nil(t *testing.T) {
	t.Parallel()
	var a *SLOProbeAggregate
	require.Error(t, a.Validate())
}

func TestSLOProbeAggregateValidate_EmptyToolID(t *testing.T) {
	t.Parallel()
	a := validSLOProbeAggregate(t)
	a.ToolId = ""
	require.Error(t, a.Validate())
}

func TestSLOProbeAggregateValidate_PaddedToolID(t *testing.T) {
	t.Parallel()
	a := validSLOProbeAggregate(t)
	a.ToolId = "\tdefi.token-price"
	require.ErrorContains(t, a.Validate(), "tool_id cannot contain leading or trailing whitespace")
}

func TestSLOProbeAggregateValidate_WindowEndBeforeStart(t *testing.T) {
	t.Parallel()
	a := validSLOProbeAggregate(t)
	a.WindowEnd = timestamppb.New(a.WindowStart.AsTime().Add(-time.Hour))
	require.Error(t, a.Validate())
}

func TestSLOProbeAggregateValidate_ZeroWatcherCount(t *testing.T) {
	t.Parallel()
	a := validSLOProbeAggregate(t)
	a.WatcherCount = 0
	require.Error(t, a.Validate())
}

func TestSLOProbeAggregateValidate_VersionAndStatus(t *testing.T) {
	t.Parallel()
	a := validSLOProbeAggregate(t)
	a.Version = 0
	require.ErrorContains(t, a.Validate(), "version must be > 0")

	a2 := validSLOProbeAggregate(t)
	a2.Status = "accepted"
	require.ErrorContains(t, a2.Validate(), "status must be provisional, final, challenged, or superseded")
}

func TestSLOProbeAggregateValidate_SupersededFields(t *testing.T) {
	t.Parallel()
	a := validSLOProbeAggregate(t)
	a.Status = SLOProbeAggregateStatusSuperseded
	a.SupersededByVersion = 2
	a.SupersededByChallengeId = "slo-challenge-1"
	a.SupersededAt = timestamppb.New(time.Date(2026, 1, 15, 13, 2, 0, 0, time.UTC))
	require.NoError(t, a.Validate())

	a2 := validSLOProbeAggregate(t)
	a2.Status = SLOProbeAggregateStatusSuperseded
	a2.SupersededByVersion = 1
	a2.SupersededByChallengeId = "slo-challenge-1"
	a2.SupersededAt = a.SupersededAt
	require.ErrorContains(t, a2.Validate(), "superseded_by_version must exceed version")

	a3 := validSLOProbeAggregate(t)
	a3.SupersededByVersion = 2
	require.ErrorContains(t, a3.Validate(), "superseded_by_version requires superseded status")
}

func TestSLOProbeAggregateValidate_BPSExceeds10000(t *testing.T) {
	t.Parallel()
	a := validSLOProbeAggregate(t)
	a.MedianAvailabilityBps = 10001
	require.Error(t, a.Validate())

	a2 := validSLOProbeAggregate(t)
	a2.MedianErrorRateBps = 10001
	require.Error(t, a2.Validate())
}

func TestSLOProbeAggregateValidate_NilTimestamps(t *testing.T) {
	t.Parallel()
	a := validSLOProbeAggregate(t)
	a.WindowStart = nil
	require.Error(t, a.Validate())

	a2 := validSLOProbeAggregate(t)
	a2.AggregatedAt = nil
	require.Error(t, a2.Validate())
}

// ---------------------------------------------------------------------------
// ToolCard.ValidateAtTime() — the big one
// ---------------------------------------------------------------------------

func validToolCard(t *testing.T) *ToolCard {
	t.Helper()
	return &ToolCard{
		ToolId:          "defi.token-price",
		Owner:           testBech32Addr(t),
		Version:         "1.0.0",
		Categories:      []string{"defi"},
		LicenseLane:     LicenseLaneBYOKey,
		Jurisdictions:   []string{"US"},
		SchemaHash:      bytes.Repeat([]byte{0x01}, 32),
		SbomHash:        bytes.Repeat([]byte{0x02}, 32),
		AttestationRoot: bytes.Repeat([]byte{0x03}, 32),
		InputSchema:     `{"type":"object"}`,
		OutputSchema:    `{"type":"object"}`,
		Pricing:         validPricing(),
		Slo:             validSLO(),
		Sandbox:         validSandbox(),
		Endpoints:       []*Endpoint{{Protocol: "https", Url: "https://api.example.com/invoke"}},
		McpProtocols:    []string{"https"},
	}
}

func TestToolCardValidate_Valid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validToolCard(t).Validate())
}

func TestToolCardValidate_Nil(t *testing.T) {
	t.Parallel()
	var tc *ToolCard
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_RequiredFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*ToolCard)
	}{
		{"empty tool_id", func(tc *ToolCard) { tc.ToolId = "" }},
		{"empty owner", func(tc *ToolCard) { tc.Owner = "" }},
		{"invalid owner address", func(tc *ToolCard) { tc.Owner = "not-bech32" }},
		{"empty version", func(tc *ToolCard) { tc.Version = "" }},
		{"invalid semver", func(tc *ToolCard) { tc.Version = "not-semver" }},
		{"empty categories", func(tc *ToolCard) { tc.Categories = nil }},
		{"invalid license lane", func(tc *ToolCard) { tc.LicenseLane = "invalid" }},
		{"empty jurisdictions", func(tc *ToolCard) { tc.Jurisdictions = nil }},
		{"empty schema_hash", func(tc *ToolCard) { tc.SchemaHash = nil }},
		{"empty sbom_hash", func(tc *ToolCard) { tc.SbomHash = nil }},
		{"empty attestation_root", func(tc *ToolCard) { tc.AttestationRoot = nil }},
		{"empty input_schema", func(tc *ToolCard) { tc.InputSchema = "" }},
		{"empty output_schema", func(tc *ToolCard) { tc.OutputSchema = "" }},
		{"nil pricing", func(tc *ToolCard) { tc.Pricing = nil }},
		{"nil slo", func(tc *ToolCard) { tc.Slo = nil }},
		{"nil sandbox", func(tc *ToolCard) { tc.Sandbox = nil }},
		{"empty endpoints", func(tc *ToolCard) { tc.Endpoints = nil }},
		{"nil endpoint entry", func(tc *ToolCard) { tc.Endpoints = []*Endpoint{nil} }},
		{"empty mcp_protocols", func(tc *ToolCard) { tc.McpProtocols = nil }},
		{"invalid mcp_protocol", func(tc *ToolCard) { tc.McpProtocols = []string{"http"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := validToolCard(t)
			tt.mutate(tc)
			require.Error(t, tc.Validate())
		})
	}
}

func TestToolCardValidate_ValidLicenseLanes(t *testing.T) {
	t.Parallel()
	for _, lane := range []string{LicenseLaneBYOKey, LicenseLaneCommunity} {
		tc := validToolCard(t)
		tc.LicenseLane = lane
		require.NoError(t, tc.Validate(), "license lane %q should be valid", lane)
	}

	tc := validToolCard(t)
	tc.LicenseLane = LicenseLaneProxied
	tc.TrustClass = TrustClassRouterAttested
	require.NoError(t, tc.Validate(), "proxied tools must be valid with router_attested trust class")
}

func TestToolCardValidate_ProxiedTrustClassBoundary(t *testing.T) {
	t.Parallel()

	t.Run("proxied requires router attested", func(t *testing.T) {
		tc := validToolCard(t)
		tc.LicenseLane = LicenseLaneProxied
		require.ErrorContains(t, tc.Validate(), "trust class must be router_attested")
	})

	t.Run("router attested requires proxied lane", func(t *testing.T) {
		tc := validToolCard(t)
		tc.TrustClass = TrustClassRouterAttested
		require.ErrorContains(t, tc.Validate(), "only valid")
	})
}

func TestUsageReceiptValidate_TrustClassValues(t *testing.T) {
	t.Parallel()

	receipt := validUsageReceipt(t)
	receipt.TrustClass = TrustClassRouterAttested
	require.NoError(t, receipt.Validate())

	receipt.TrustClass = "unlabeled"
	require.ErrorContains(t, receipt.Validate(), "trust class")
}

func TestToolCardValidate_LicensedResaleRequiresMetadata(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.LicenseLane = LicenseLaneLicensedResale
	tc.Metadata = nil
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_LicensedResaleValid(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.LicenseLane = LicenseLaneLicensedResale
	future := time.Now().Add(365 * 24 * time.Hour)
	tc.Metadata = map[string]string{
		"license_reference":           "https://example.com/license",
		"license_expires_at":          future.Format(time.RFC3339),
		"license_governance_status":   "approved",
		"license_governance_proposal": "https://governance.example.com/prop-1",
	}
	require.NoError(t, tc.Validate())
}

func TestToolCardValidate_LicensedResaleMissingFields(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(365 * 24 * time.Hour)
	base := map[string]string{
		"license_reference":           "https://example.com/license",
		"license_expires_at":          future.Format(time.RFC3339),
		"license_governance_status":   "approved",
		"license_governance_proposal": "https://governance.example.com/prop-1",
	}

	requiredKeys := []string{
		"license_reference",
		"license_expires_at",
		"license_governance_status",
		"license_governance_proposal",
	}
	for _, key := range requiredKeys {
		t.Run("missing "+key, func(t *testing.T) {
			tc := validToolCard(t)
			tc.LicenseLane = LicenseLaneLicensedResale
			meta := make(map[string]string)
			for k, v := range base {
				meta[k] = v
			}
			delete(meta, key)
			tc.Metadata = meta
			require.Error(t, tc.Validate())
		})
	}
}

func TestToolCardValidate_LicensedResaleExpiredLicense(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.LicenseLane = LicenseLaneLicensedResale
	past := time.Now().Add(-24 * time.Hour)
	tc.Metadata = map[string]string{
		"license_reference":           "https://example.com/license",
		"license_expires_at":          past.Format(time.RFC3339),
		"license_governance_status":   "approved",
		"license_governance_proposal": "https://governance.example.com/prop-1",
	}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_LicensedResaleRevokedStatus(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.LicenseLane = LicenseLaneLicensedResale
	future := time.Now().Add(365 * 24 * time.Hour)
	tc.Metadata = map[string]string{
		"license_reference":           "https://example.com/license",
		"license_expires_at":          future.Format(time.RFC3339),
		"license_governance_status":   "revoked",
		"license_governance_proposal": "https://governance.example.com/prop-1",
	}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_LicensedResaleInvalidGovernanceStatus(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.LicenseLane = LicenseLaneLicensedResale
	future := time.Now().Add(365 * 24 * time.Hour)
	tc.Metadata = map[string]string{
		"license_reference":           "https://example.com/license",
		"license_expires_at":          future.Format(time.RFC3339),
		"license_governance_status":   "unknown",
		"license_governance_proposal": "https://governance.example.com/prop-1",
	}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_DescriptionTooLong(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Description = strings.Repeat("a", MaxDescriptionLength+1)
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_TooManyTags(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tags := make([]string, MaxTags+1)
	for i := range tags {
		tags[i] = "tag"
	}
	tc.Tags = tags
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_TagTooLong(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Tags = []string{strings.Repeat("x", MaxTagLength+1)}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_TooManyMetadataKeys(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	meta := make(map[string]string)
	for i := 0; i <= MaxMetadataKeys; i++ {
		meta[strings.Repeat("k", i+1)] = "v"
	}
	tc.Metadata = meta
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_MetadataKeyTooLong(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Metadata = map[string]string{
		strings.Repeat("k", MaxMetadataKeyLength+1): "v",
	}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_MetadataValueTooLong(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Metadata = map[string]string{
		"key": strings.Repeat("v", MaxMetadataValueLength+1),
	}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_MetadataDiagnosticsRedactCredentialShapedKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		metadata map[string]string
		rawLeak  string
		marker   string
	}{
		{
			name: "key too long",
			metadata: map[string]string{
				strings.Repeat("metadata.", 14) + "?api_key=registry-metadata-secret": "value",
			},
			rawLeak: "registry-metadata-secret",
			marker:  "api_key=[REDACTED]",
		},
		{
			name: "value too long names key",
			metadata: map[string]string{
				"license_reference?client_secret=registry-metadata-secret": strings.Repeat("v", MaxMetadataValueLength+1),
			},
			rawLeak: "registry-metadata-secret",
			marker:  "client_secret=[REDACTED]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			card := validToolCard(t)
			card.Metadata = tc.metadata
			err := card.Validate()
			require.Error(t, err)
			require.NotContains(t, err.Error(), tc.rawLeak)
			require.Contains(t, err.Error(), tc.marker)
		})
	}
}

func TestToolCardValidate_InvalidCachePolicy(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Cache = &CachePolicy{Enabled: true, TtlSeconds: 0}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_ValidCachePolicy(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Cache = &CachePolicy{Enabled: true, TtlSeconds: 3600, RoyaltyShareBps: 500}
	require.NoError(t, tc.Validate())
}

func TestToolCardValidate_InvalidEndpoint(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Endpoints = []*Endpoint{{Protocol: "https", Url: "http://not-https.com"}}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_InvalidPricing(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Pricing = &Pricing{Model: "invalid"}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_InvalidSLO(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Slo = &SLO{P95LatencyMs: 0}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_InvalidSandbox(t *testing.T) {
	t.Parallel()
	tc := validToolCard(t)
	tc.Sandbox = &SandboxProfile{Profile: "invalid"}
	require.Error(t, tc.Validate())
}

func TestToolCardValidate_ValidSemverVersions(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"1.0.0", "0.1.0", "2.5.3-beta.1", "1.0.0+build.123"} {
		tc := validToolCard(t)
		tc.Version = v
		require.NoError(t, tc.Validate(), "version %q should be valid", v)
	}
}

// ---------------------------------------------------------------------------
// LaneMeteringConfig.Validate()
// ---------------------------------------------------------------------------

func TestLaneMeteringConfigValidate_Valid(t *testing.T) {
	t.Parallel()
	m := &LaneMeteringConfig{MeterType: LaneMeterTypeTEESigned, PricingModelId: "pricing-1"}
	require.NoError(t, m.Validate())
}

func TestLaneMeteringConfigValidate_Nil(t *testing.T) {
	t.Parallel()
	var m *LaneMeteringConfig
	require.Error(t, m.Validate())
}

func TestLaneMeteringConfigValidate_InvalidMeterType(t *testing.T) {
	t.Parallel()
	m := &LaneMeteringConfig{MeterType: "invalid", PricingModelId: "pricing-1"}
	require.Error(t, m.Validate())
}

func TestLaneMeteringConfigValidate_EmptyPricingModelID(t *testing.T) {
	t.Parallel()
	m := &LaneMeteringConfig{MeterType: LaneMeterTypeTEESigned, PricingModelId: ""}
	require.Error(t, m.Validate())
}

// ---------------------------------------------------------------------------
// LaneComplianceConfig.Validate()
// ---------------------------------------------------------------------------

func TestLaneComplianceConfigValidate_Valid(t *testing.T) {
	t.Parallel()
	c := &LaneComplianceConfig{
		EgressAllowlist: []string{"https://api.example.com"},
		LogPolicy:       LaneLogPolicyHashOnly,
		PiiPolicy:       LanePIIPolicyDeny,
	}
	require.NoError(t, c.Validate())
}

func TestLaneComplianceConfigValidate_Nil(t *testing.T) {
	t.Parallel()
	var c *LaneComplianceConfig
	require.Error(t, c.Validate())
}

func TestLaneComplianceConfigValidate_EmptyEgress(t *testing.T) {
	t.Parallel()
	c := &LaneComplianceConfig{
		EgressAllowlist: nil,
		LogPolicy:       LaneLogPolicyHashOnly,
		PiiPolicy:       LanePIIPolicyDeny,
	}
	require.Error(t, c.Validate())
}

func TestLaneComplianceConfigValidate_InvalidLogPolicy(t *testing.T) {
	t.Parallel()
	c := &LaneComplianceConfig{
		EgressAllowlist: []string{"https://api.example.com"},
		LogPolicy:       "invalid",
		PiiPolicy:       LanePIIPolicyDeny,
	}
	require.Error(t, c.Validate())
}

func TestLaneComplianceConfigValidate_InvalidPiiPolicy(t *testing.T) {
	t.Parallel()
	c := &LaneComplianceConfig{
		EgressAllowlist: []string{"https://api.example.com"},
		LogPolicy:       LaneLogPolicyHashOnly,
		PiiPolicy:       "invalid",
	}
	require.Error(t, c.Validate())
}

// ---------------------------------------------------------------------------
// LaneAttestationPolicy.Validate()
// ---------------------------------------------------------------------------

func TestLaneAttestationPolicyValidate_Valid(t *testing.T) {
	t.Parallel()
	p := &LaneAttestationPolicy{
		TeeType:             LaneTEETypeNitro,
		PolicyHash:          bytes.Repeat([]byte{0x01}, 32),
		AllowedMeasurements: [][]byte{bytes.Repeat([]byte{0x02}, 32)},
		SignerKeys:          [][]byte{bytes.Repeat([]byte{0x03}, 32)},
	}
	require.NoError(t, p.Validate())
}

func TestLaneAttestationPolicyValidate_Nil(t *testing.T) {
	t.Parallel()
	var p *LaneAttestationPolicy
	require.Error(t, p.Validate())
}

func TestLaneAttestationPolicyValidate_InvalidTEEType(t *testing.T) {
	t.Parallel()
	p := &LaneAttestationPolicy{
		TeeType:    "invalid",
		PolicyHash: bytes.Repeat([]byte{0x01}, 32),
	}
	require.Error(t, p.Validate())
}

func TestLaneAttestationPolicyValidate_ValidTEETypes(t *testing.T) {
	t.Parallel()
	for _, tee := range []string{LaneTEETypeSGX, LaneTEETypeSEVSNP, LaneTEETypeNitro, LaneTEETypeSapphire, LaneTEETypeTrustZone} {
		p := &LaneAttestationPolicy{
			TeeType:    tee,
			PolicyHash: bytes.Repeat([]byte{0x01}, 32),
		}
		require.NoError(t, p.Validate(), "TEE type %q should be valid", tee)
	}
}

func TestLaneAttestationPolicyValidate_EmptyPolicyHash(t *testing.T) {
	t.Parallel()
	p := &LaneAttestationPolicy{
		TeeType:    LaneTEETypeNitro,
		PolicyHash: nil,
	}
	require.Error(t, p.Validate())
}

func TestLaneAttestationPolicyValidate_WrongPolicyHashSize(t *testing.T) {
	t.Parallel()
	p := &LaneAttestationPolicy{
		TeeType:    LaneTEETypeNitro,
		PolicyHash: []byte{0x01, 0x02}, // not 32 bytes
	}
	require.Error(t, p.Validate())
}

func TestLaneAttestationPolicyValidate_WrongMeasurementSize(t *testing.T) {
	t.Parallel()
	p := &LaneAttestationPolicy{
		TeeType:             LaneTEETypeNitro,
		PolicyHash:          bytes.Repeat([]byte{0x01}, 32),
		AllowedMeasurements: [][]byte{[]byte{0x01}}, // not 32 bytes
	}
	require.Error(t, p.Validate())
}

func TestLaneAttestationPolicyValidate_WrongSignerKeySize(t *testing.T) {
	t.Parallel()
	p := &LaneAttestationPolicy{
		TeeType:    LaneTEETypeNitro,
		PolicyHash: bytes.Repeat([]byte{0x01}, 32),
		SignerKeys: [][]byte{[]byte{0x01}}, // not 32 bytes
	}
	require.Error(t, p.Validate())
}
