
package types

import (
	"math"
	"strings"
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
)

// TestValidateSummaryCoin_RejectsInvalidAmountMetamorphic pins the
// amount-parsing contract of validateSummaryCoin. sdkmath.NewIntFromString
// is strict: decimal points, alphabetic characters, and empty strings
// all fail to parse. Settlement dashboards read TotalSpend/SettledSpend
// verbatim; a regression that accepted "1.5" or "abc" as valid would
// let a corrupted state record through and display garbage.
func TestValidateSummaryCoin_RejectsInvalidAmountMetamorphic(t *testing.T) {
	t.Parallel()
	// Known-invalid amount forms. sdkmath.NewIntFromString wraps big.Int
	// with base 10 parsing and strict semantics, so these all fail. Hex
	// (0x10) and signed-positive (+100) are intentionally omitted — the
	// underlying big.Int parser may accept them in some SDK versions.
	invalid := []string{
		"",      // empty
		"abc",   // alpha
		"1.5",   // decimal point
		"1e9",   // scientific notation
		"1,000", // comma separator
		" 100",  // leading space
		"100 ",  // trailing space
		"- 100", // malformed negative
		"--100", // double negative
	}
	for _, amount := range invalid {
		s := &PassportSummary{
			TotalSpend: &v1beta1.Coin{Denom: "ulume", Amount: amount},
		}
		err := s.Validate()
		if err == nil {
			t.Fatalf("amount=%q should be rejected", amount)
		}
		if !strings.Contains(err.Error(), "total_spend") {
			t.Fatalf("amount=%q: error %q does not mention total_spend", amount, err)
		}
	}
}

// TestValidateSummaryCoin_RejectsNegativeAmountMetamorphic asserts
// that negative coin amounts are rejected. Total spend is cumulative
// and cannot be negative; a negative record indicates corrupt state
// or a bug in settlement arithmetic elsewhere.
func TestValidateSummaryCoin_RejectsNegativeAmountMetamorphic(t *testing.T) {
	t.Parallel()
	negatives := []string{"-1", "-100", "-999999999999999999"}
	for _, amount := range negatives {
		s := &PassportSummary{
			SettledSpend: &v1beta1.Coin{Denom: "ulume", Amount: amount},
		}
		err := s.Validate()
		if err == nil {
			t.Fatalf("negative amount %q should be rejected", amount)
		}
		if !strings.Contains(err.Error(), "settled_spend") {
			t.Fatalf("negative amount %q: error %q does not mention settled_spend", amount, err)
		}
	}
}

// TestValidateSummaryCoin_RejectsMalformedDenomMetamorphic pins
// parity with CoinFromProto and Params.Validate: Passport summary
// spend fields are Cosmos coins, so non-empty but malformed denoms
// must fail before genesis can import corrupted spend records.
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
					TotalSpend: &v1beta1.Coin{Denom: denom, Amount: "1"},
				},
			},
			{
				field: "settled_spend",
				summary: PassportSummary{
					SettledSpend: &v1beta1.Coin{Denom: denom, Amount: "1"},
				},
			},
			{
				field: "refunded_spend",
				summary: PassportSummary{
					RefundedSpend: &v1beta1.Coin{Denom: denom, Amount: "1"},
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
// regression rejecting "0" would break genesis paths.
func TestValidateSummaryCoin_AcceptsZeroAndPositiveMetamorphic(t *testing.T) {
	t.Parallel()
	amounts := []string{"0", "1", "100", "1000000", "999999999999999999"}
	for _, amount := range amounts {
		s := &PassportSummary{
			TotalSpend:    &v1beta1.Coin{Denom: "ulume", Amount: amount},
			SettledSpend:  &v1beta1.Coin{Denom: "ulume", Amount: amount},
			RefundedSpend: &v1beta1.Coin{Denom: "ulume", Amount: amount},
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("amount=%q should be accepted, got: %v", amount, err)
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
		// Index-based loop avoids copying PassportSummary, which embeds
		// protoimpl.MessageState (contains sync.Mutex). `go vet` flags
		// `for _, s := range cases` as "range var s copies lock"; using
		// &cases[i] takes the address of the original element so the
		// pointer-receiver Validate method operates on the authoritative
		// value.
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
	// Index-based loop to avoid copying the PassportSummary embedded in
	// each test case (contains protoimpl.MessageState → sync.Mutex).
	// `go vet` flags `for _, tc := range tests` as "range var tc copies
	// lock" even though the test is read-only; &tests[i] preserves the
	// single authoritative PassportSummary for the pointer-receiver
	// Validate call.
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
// contract that nil coin pointers are accepted (represents "not yet
// populated"). Freshly-created passports have nil coins until the
// first settlement; any regression that dereferenced nil here would
// crash the validator on every new passport.
func TestValidateSummary_NilCoinAcceptedMetamorphic(t *testing.T) {
	t.Parallel()
	s := &PassportSummary{
		// All coin pointers nil.
		TotalSpend:    nil,
		SettledSpend:  nil,
		RefundedSpend: nil,
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("nil coins should be accepted (fresh passport): %v", err)
	}
	// Partial nil: some set, some nil — also accepted.
	s.TotalSpend = &v1beta1.Coin{Denom: "ulume", Amount: "100"}
	if err := s.Validate(); err != nil {
		t.Fatalf("partial nil coins should be accepted: %v", err)
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
