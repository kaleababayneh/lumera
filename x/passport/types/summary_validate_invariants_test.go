package types

import (
	"math"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TestValidateSummaryCoin_RejectsNilAmount pins the amount-validity
// contract of validateSummaryCoin. With the gogoproto migration the
// coin amount is a math.Int value, so the only representable "invalid
// amount" is a nil (unset) Int paired with a non-empty denom — a
// present-but-empty coin (empty denom + nil amount) is intentionally
// treated as unset. Settlement dashboards read TotalSpend/SettledSpend
// verbatim; a regression that accepted a denom'd coin with a nil amount
// would let a corrupted state record through and display garbage.
//
// The historical string-parse cases ("1.5", "abc", "1e9", "1,000",
// leading/trailing space, "--100", …) are obsolete: a math.Int can no
// longer hold an unparseable string, so those inputs cannot be
// constructed. The surviving representable failure (nil amount) is
// pinned here.
func TestValidateSummaryCoin_RejectsNilAmount(t *testing.T) {
	t.Parallel()
	s := &PassportSummary{
		TotalSpend: sdk.Coin{Denom: "ulume"}, // denom set, amount nil
	}
	err := s.Validate()
	if err == nil {
		t.Fatalf("nil amount with denom should be rejected")
	}
	if !strings.Contains(err.Error(), "total_spend") {
		t.Fatalf("error %q does not mention total_spend", err)
	}
}

// TestValidateSummaryCoin_RejectsNegativeAmountMetamorphic asserts
// that negative coin amounts are rejected. Total spend is cumulative
// and cannot be negative; a negative record indicates corrupt state
// or a bug in settlement arithmetic elsewhere.
func TestValidateSummaryCoin_RejectsNegativeAmountMetamorphic(t *testing.T) {
	t.Parallel()
	negatives := []sdkmath.Int{
		sdkmath.NewInt(-1),
		sdkmath.NewInt(-100),
		sdkmath.NewInt(-999999999999999999),
	}
	for _, amount := range negatives {
		s := &PassportSummary{
			SettledSpend: sdk.Coin{Denom: "ulume", Amount: amount},
		}
		err := s.Validate()
		if err == nil {
			t.Fatalf("negative amount %s should be rejected", amount)
		}
		if !strings.Contains(err.Error(), "settled_spend") {
			t.Fatalf("negative amount %s: error %q does not mention settled_spend", amount, err)
		}
	}
}

// TestValidateSummaryCoin_RejectsMalformedDenomMetamorphic pins
// parity with Params.Validate: Passport summary spend fields are
// Cosmos coins, so non-empty but malformed denoms must fail before
// genesis can import corrupted spend records.
func TestValidateSummaryCoin_RejectsMalformedDenomMetamorphic(t *testing.T) {
	t.Parallel()
	invalidDenoms := []string{
		"bad denom",
		"ulume ",
		" ulume",
		"ulume!",
		"1ulume",
	}
	for _, denom := range invalidDenoms {
		cases := []struct {
			field   string
			summary PassportSummary
		}{
			{
				field: "total_spend",
				summary: PassportSummary{
					TotalSpend: sdk.Coin{Denom: denom, Amount: sdkmath.NewInt(1)},
				},
			},
			{
				field: "settled_spend",
				summary: PassportSummary{
					SettledSpend: sdk.Coin{Denom: denom, Amount: sdkmath.NewInt(1)},
				},
			},
			{
				field: "refunded_spend",
				summary: PassportSummary{
					RefundedSpend: sdk.Coin{Denom: denom, Amount: sdkmath.NewInt(1)},
				},
			},
		}
		for i := range cases {
			tc := &cases[i]
			err := tc.summary.Validate()
			if err == nil {
				t.Fatalf("%s denom %q should be rejected", tc.field, denom)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("denom=%q: error %q does not mention %s", denom, err, tc.field)
			}
		}
	}
}

// TestValidateSummaryCoin_AcceptsZeroAndPositiveMetamorphic asserts the
// accept path: zero and positive amounts with a non-empty denom pass.
// Zero is the initial state for a freshly-created passport, so any
// regression rejecting zero would break genesis paths.
func TestValidateSummaryCoin_AcceptsZeroAndPositiveMetamorphic(t *testing.T) {
	t.Parallel()
	amounts := []sdkmath.Int{
		sdkmath.NewInt(0),
		sdkmath.NewInt(1),
		sdkmath.NewInt(100),
		sdkmath.NewInt(1000000),
		sdkmath.NewInt(999999999999999999),
	}
	for _, amount := range amounts {
		s := &PassportSummary{
			TotalSpend:    sdk.Coin{Denom: "ulume", Amount: amount},
			SettledSpend:  sdk.Coin{Denom: "ulume", Amount: amount},
			RefundedSpend: sdk.Coin{Denom: "ulume", Amount: amount},
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("amount=%s should be accepted, got: %v", amount, err)
		}
	}
}

// TestValidateSummaryShare_BoundariesMetamorphic asserts the exact
// [0.0, 1.0] inclusive interval for share values. Callers set these
// from ratios that can legitimately be 0 (empty sample) or 1 (all
// traffic). A regression that used strict < or > would reject those
// boundaries and fail validate on fresh or unanimous-input summaries.
func TestValidateSummaryShare_BoundariesMetamorphic(t *testing.T) {
	t.Parallel()
	// Inclusive boundaries accepted.
	for _, v := range []float64{0.0, 1.0, 0.5, 0.999, 0.001} {
		s := &PassportSummary{
			ToolDiversityIndex: v,
			VerifiedSpendShare: v,
			CollusionRiskScore: v,
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("share=%v should be accepted, got: %v", v, err)
		}
	}
	// Just outside rejected.
	for _, v := range []float64{-0.0001, 1.0001, -1, 2, 100} {
		s := &PassportSummary{ToolDiversityIndex: v}
		if err := s.Validate(); err == nil {
			t.Fatalf("share=%v should be rejected", v)
		}
	}
}

// TestValidateSummaryShare_InfinityRejectionMetamorphic pins the
// "must be finite" rule: ±Inf and NaN all fail validation. The
// existing TestPassportSummaryValidate_InvalidShares covers NaN; this
// adds the infinity branches to guard against an accidental refactor
// that only checked NaN.
func TestValidateSummaryShare_InfinityRejectionMetamorphic(t *testing.T) {
	t.Parallel()
	nonfinite := []float64{
		math.NaN(),
		math.Inf(+1),
		math.Inf(-1),
	}
	for _, v := range nonfinite {
		// Try each field in turn to make sure EVERY share field rejects
		// non-finite values (not just the one covered by the existing
		// test).
		cases := []PassportSummary{
			{ToolDiversityIndex: v},
			{VerifiedSpendShare: v},
			{CollusionRiskScore: v},
		}
		for i := range cases {
			if err := cases[i].Validate(); err == nil {
				t.Fatalf("field[%d] value=%v should be rejected", i, v)
			}
		}
	}
}

// TestValidateSummaryShare_FieldAttributionMetamorphic asserts that
// the error message names the specific share field that failed.
// Debugging validation failures in production depends on seeing
// WHICH dimension tripped the check — a refactor that lost the field
// name and emitted a generic "share out of range" would waste
// operator time during an incident.
func TestValidateSummaryShare_FieldAttributionMetamorphic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		summary  PassportSummary
		wantWord string
	}{
		{
			name:     "tool_diversity_index",
			summary:  PassportSummary{ToolDiversityIndex: 1.5},
			wantWord: "tool_diversity_index",
		},
		{
			name:     "verified_spend_share",
			summary:  PassportSummary{VerifiedSpendShare: -1},
			wantWord: "verified_spend_share",
		},
		{
			name:     "collusion_risk_score",
			summary:  PassportSummary{CollusionRiskScore: math.NaN()},
			wantWord: "collusion_risk_score",
		},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.name, func(t *testing.T) {
			err := tc.summary.Validate()
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantWord) {
				t.Fatalf("error %q does not mention field %q", err, tc.wantWord)
			}
		})
	}
}

// TestValidateSummary_NilCoinAcceptedMetamorphic pins the documented
// contract that unset coins are accepted (represents "not yet
// populated"). Freshly-created passports have zero-value coins until
// the first settlement; any regression that rejected the unset coin
// here would crash the validator on every new passport.
func TestValidateSummary_NilCoinAcceptedMetamorphic(t *testing.T) {
	t.Parallel()
	s := &PassportSummary{
		// All coins unset (zero value: empty denom + nil amount).
		TotalSpend:    sdk.Coin{},
		SettledSpend:  sdk.Coin{},
		RefundedSpend: sdk.Coin{},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("unset coins should be accepted (fresh passport): %v", err)
	}
	// Partial: some set, some unset — also accepted.
	s.TotalSpend = sdk.Coin{Denom: "ulume", Amount: sdkmath.NewInt(100)}
	if err := s.Validate(); err != nil {
		t.Fatalf("partial unset coins should be accepted: %v", err)
	}
}

func TestValidateSummary_ReceiptTimestampOrdering(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		first   int64
		last    int64
		wantErr bool
	}{
		{name: "both unset", first: 0, last: 0},
		{name: "first unset", first: 0, last: 2_000},
		{name: "last unset", first: 1_000, last: 0},
		{name: "first before last", first: 1_000, last: 2_000},
		{name: "equal timestamps", first: 1_000, last: 1_000},
		{name: "first after last", first: 2_000, last: 1_000, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &PassportSummary{
				FirstReceiptTs: tc.first,
				LastReceiptTs:  tc.last,
			}
			err := s.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected receipt timestamp ordering error")
				}
				if !strings.Contains(err.Error(), "first_receipt_ts") {
					t.Fatalf("error %q does not mention first_receipt_ts", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("receipt timestamp ordering should be accepted: %v", err)
			}
		})
	}
}
