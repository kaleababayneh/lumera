//go:build cosmos

package types

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

// ---------------------------------------------------------------------------
// SafeDecimalFromString
// ---------------------------------------------------------------------------

func TestSafeDecimalFromString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected decimal.Decimal
	}{
		{"empty_string", "", DecimalZero},
		{"valid_zero", "0", DecimalZero},
		{"valid_integer", "42", decimal.NewFromInt(42)},
		{"valid_decimal", "3.14", decimal.RequireFromString("3.14")},
		{"valid_negative", "-10.5", decimal.RequireFromString("-10.5")},
		{"valid_large", "999999999999999999", decimal.RequireFromString("999999999999999999")},
		{"valid_small_decimal", "0.000000001", decimal.RequireFromString("0.000000001")},
		{"invalid_string", "not_a_number", DecimalZero},
		{"invalid_special", "NaN", DecimalZero},
		{"whitespace_only", "   ", DecimalZero},
		{"leading_zeros", "007", decimal.NewFromInt(7)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeDecimalFromString(tc.input)
			if !got.Equal(tc.expected) {
				t.Errorf("SafeDecimalFromString(%q) = %s, want %s", tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SafeDecimalFromStringWithError
// ---------------------------------------------------------------------------

func TestSafeDecimalFromStringWithError(t *testing.T) {
	t.Parallel()

	t.Run("empty_returns_zero_no_error", func(t *testing.T) {
		d, err := SafeDecimalFromStringWithError("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !d.Equal(DecimalZero) {
			t.Errorf("got %s, want 0", d)
		}
	})

	t.Run("valid_string", func(t *testing.T) {
		d, err := SafeDecimalFromStringWithError("123.456")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !d.Equal(decimal.RequireFromString("123.456")) {
			t.Errorf("got %s, want 123.456", d)
		}
	})

	t.Run("invalid_returns_error", func(t *testing.T) {
		_, err := SafeDecimalFromStringWithError("xyz")
		if err == nil {
			t.Error("expected error for invalid input, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// SafeDecimalFromStringStrict
// ---------------------------------------------------------------------------

func TestSafeDecimalFromStringStrict(t *testing.T) {
	t.Parallel()

	t.Run("empty_returns_error", func(t *testing.T) {
		_, err := SafeDecimalFromStringStrict("", "amount")
		if err == nil {
			t.Error("expected error for empty string")
		}
		if err != nil && !containsStr(err.Error(), "cannot be empty") {
			t.Errorf("error %q should mention 'cannot be empty'", err)
		}
	})

	t.Run("invalid_returns_error", func(t *testing.T) {
		_, err := SafeDecimalFromStringStrict("abc", "price")
		if err == nil {
			t.Error("expected error for invalid string")
		}
		if err != nil && !containsStr(err.Error(), "invalid") {
			t.Errorf("error %q should mention 'invalid'", err)
		}
	})

	t.Run("negative_returns_error", func(t *testing.T) {
		_, err := SafeDecimalFromStringStrict("-5", "amount")
		if err == nil {
			t.Error("expected error for negative value")
		}
		if err != nil && !containsStr(err.Error(), "negative") {
			t.Errorf("error %q should mention 'negative'", err)
		}
	})

	t.Run("zero_is_valid", func(t *testing.T) {
		d, err := SafeDecimalFromStringStrict("0", "amount")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !d.IsZero() {
			t.Errorf("got %s, want 0", d)
		}
	})

	t.Run("positive_is_valid", func(t *testing.T) {
		d, err := SafeDecimalFromStringStrict("42.5", "price")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !d.Equal(decimal.RequireFromString("42.5")) {
			t.Errorf("got %s, want 42.5", d)
		}
	})

	t.Run("field_name_in_error", func(t *testing.T) {
		_, err := SafeDecimalFromStringStrict("", "settlement_amount")
		if err == nil {
			t.Fatal("expected error")
		}
		if !containsStr(err.Error(), "settlement_amount") {
			t.Errorf("error should include field name, got: %s", err)
		}
	})

	// Regression: this helper is the central strict-parse used by every
	// consensus-critical x/registry path that handles user-supplied decimal
	// amounts (settlement, dispute, sla_dispute, verified_badge, IBC
	// reputation, tx_prioritizer). Without the moneyguard gate a single
	// MsgSubmitReceipt / MsgSettleReceipt / IBC reputation packet with a
	// symbolic exponent like "1e11100100" would stall every validator on
	// the downstream Mul/Div/Cmp/String — a chain-halt DoS. The gate
	// rejects synchronously so no downstream arithmetic is reached.
	t.Run("absurd_exponent_rejected", func(t *testing.T) {
		_, err := SafeDecimalFromStringStrict("1e11100100", "actual_amount")
		if err == nil {
			t.Fatal("expected error for absurd exponent")
		}
		if !containsStr(err.Error(), "actual_amount") {
			t.Errorf("error should include field name, got: %s", err)
		}
		if !containsStr(err.Error(), "magnitude out of range") {
			t.Errorf("error should mention 'magnitude out of range', got: %s", err)
		}
	})

	t.Run("legitimate_scientific_notation_accepted", func(t *testing.T) {
		// 1e-6 has exponent −6, well within moneyguard's ±100 bound.
		d, err := SafeDecimalFromStringStrict("1e-6", "price")
		if err != nil {
			t.Fatalf("legitimate small exponent should parse: %v", err)
		}
		if !d.Equal(decimal.RequireFromString("0.000001")) {
			t.Errorf("got %s, want 0.000001", d)
		}
	})
}

// ---------------------------------------------------------------------------
// DecimalToString
// ---------------------------------------------------------------------------

func TestDecimalToString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    decimal.Decimal
		expected string
	}{
		{"zero", DecimalZero, "0"},
		{"integer", decimal.NewFromInt(42), "42"},
		{"negative_integer", decimal.NewFromInt(-7), "-7"},
		{"simple_decimal", decimal.RequireFromString("3.14"), "3.14"},
		{"trailing_zeros_removed", decimal.RequireFromString("1.50000"), "1.5"},
		{"all_decimal_trailing_zeros", decimal.RequireFromString("2.000"), "2"},
		{"high_precision", decimal.RequireFromString("1.123456789012345678"), "1.123456789012345678"},
		{"very_small", decimal.RequireFromString("0.000000000000000001"), "0.000000000000000001"},
		{"large_number", decimal.RequireFromString("999999999999999999"), "999999999999999999"},
		{"one", DecimalOne, "1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecimalToString(tc.input)
			if got != tc.expected {
				t.Errorf("DecimalToString(%s) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestDecimalToString_Roundtrip(t *testing.T) {
	t.Parallel()
	values := []string{"0", "1", "42.5", "0.000000001", "999999.123456"}
	for _, v := range values {
		d := decimal.RequireFromString(v)
		s := DecimalToString(d)
		back := SafeDecimalFromString(s)
		if !back.Equal(d) {
			t.Errorf("roundtrip failed for %q: got %s after DecimalToString(%s) = %q",
				v, back, d, s)
		}
	}
}

// ---------------------------------------------------------------------------
// ValidateDecimalString
// ---------------------------------------------------------------------------

func TestValidateDecimalString(t *testing.T) {
	t.Parallel()

	t.Run("empty_is_zero", func(t *testing.T) {
		d, err := ValidateDecimalString("", "field")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !d.Equal(DecimalZero) {
			t.Errorf("got %s, want 0", d)
		}
	})

	t.Run("valid_decimal", func(t *testing.T) {
		d, err := ValidateDecimalString("100.5", "field")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !d.Equal(decimal.RequireFromString("100.5")) {
			t.Errorf("got %s, want 100.5", d)
		}
	})

	t.Run("invalid_returns_error", func(t *testing.T) {
		_, err := ValidateDecimalString("not_valid", "amount")
		if err == nil {
			t.Error("expected error for invalid string")
		}
		if err != nil && !containsStr(err.Error(), "amount") {
			t.Errorf("error should include field name, got: %s", err)
		}
	})

	t.Run("negative_is_valid", func(t *testing.T) {
		// ValidateDecimalString allows negatives (unlike Strict variant)
		d, err := ValidateDecimalString("-5", "field")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !d.Equal(decimal.NewFromInt(-5)) {
			t.Errorf("got %s, want -5", d)
		}
	})
}

// ---------------------------------------------------------------------------
// SafeBPSCalculation
// ---------------------------------------------------------------------------

func TestSafeBPSCalculation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		numerator   int64
		denominator int64
		expected    uint32
	}{
		// Edge cases
		{"zero_denominator", 100, 0, 0},
		{"negative_denominator", 100, -1, 0},
		{"zero_numerator", 0, 100, 0},
		{"negative_numerator", -5, 100, 0},

		// Standard percentages
		{"100_percent", 100, 100, 10000},
		{"50_percent", 50, 100, 5000},
		{"25_percent", 25, 100, 2500},
		{"10_percent", 10, 100, 1000},
		{"1_percent", 1, 100, 100},
		{"0.1_percent", 1, 1000, 10},
		{"0.01_percent", 1, 10000, 1},

		// Over 100% capped
		{"over_100_percent", 200, 100, 10000},
		{"way_over", 1000, 1, 10000},

		// Exact match
		{"equal", 500, 500, 10000},

		// Rounding: 1/3 * 10000 = 3333.33... → 3333
		{"one_third", 1, 3, 3333},
		// 2/3 * 10000 = 6666.66... → 6667
		{"two_thirds", 2, 3, 6667},

		// Large values (overflow protection)
		{"large_values", 1000000000, 2000000000, 5000},

		// Small fraction
		{"tiny_fraction", 1, 100000, 0}, // 0.001% → rounds to 0
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeBPSCalculation(tc.numerator, tc.denominator)
			if got != tc.expected {
				t.Errorf("SafeBPSCalculation(%d, %d) = %d, want %d",
					tc.numerator, tc.denominator, got, tc.expected)
			}
		})
	}
}

func TestSafeBPSCalculation_Monotonicity(t *testing.T) {
	t.Parallel()
	// Increasing numerator with fixed denominator should give non-decreasing BPS
	prev := uint32(0)
	for n := int64(0); n <= 100; n++ {
		got := SafeBPSCalculation(n, 100)
		if got < prev {
			t.Errorf("BPS decreased from %d to %d at numerator=%d", prev, got, n)
		}
		prev = got
	}
}

func TestSafeBPSCalculation_MaxInt64(t *testing.T) {
	t.Parallel()
	// Should not panic or overflow with very large values
	got := SafeBPSCalculation(math.MaxInt64/2, math.MaxInt64)
	if got != 5000 {
		t.Errorf("MaxInt64/2 / MaxInt64 = %d, want 5000", got)
	}
}

// ---------------------------------------------------------------------------
// Constants sanity checks
// ---------------------------------------------------------------------------

func TestBPSDenominator(t *testing.T) {
	t.Parallel()
	if BPSDenominator != 10000 {
		t.Errorf("BPSDenominator = %d, want 10000", BPSDenominator)
	}
}

func TestDecimalPrecision(t *testing.T) {
	t.Parallel()
	if DecimalPrecision != 18 {
		t.Errorf("DecimalPrecision = %d, want 18", DecimalPrecision)
	}
}

func TestDefaultDecimalVars(t *testing.T) {
	t.Parallel()
	if !DecimalZero.IsZero() {
		t.Error("DecimalZero should be zero")
	}
	if !DecimalOne.Equal(decimal.NewFromInt(1)) {
		t.Error("DecimalOne should equal 1")
	}
	if DecimalTolerance.IsNegative() || DecimalTolerance.IsZero() {
		t.Error("DecimalTolerance should be positive")
	}
}

// ---------------------------------------------------------------------------
// Slash restitution constants (lumera_ai-tvanr)
//
// These pin two invariants that cross-package callers depend on and that
// would be silently violated by a future parameter edit:
//
//   1. The four bps shares sum to exactly 10_000. Any future hand-edit that
//      drifts the total would silently leak (or overspend) slash proceeds.
//
//   2. The 5% burn share is the exact value mandated by
//      specs/governance/slashing-rules.md §"Restitution Routing", which
//      the spec itself flags as "hard-coded and immutable (not a governance
//      parameter) to prevent perverse incentives". Pinning it here means
//      an errant bump to 6% or 4% fails a unit test instead of merging.
//
//   3. The destination module names ("insurance" / "fee_collector") are
//      stable strings. Changing either would silently re-route slash
//      proceeds to a non-existent or wrong module and brick SlashBond.
// ---------------------------------------------------------------------------

func TestSlashRestitutionBpsSumToTenThousand(t *testing.T) {
	t.Parallel()
	sum := SlashRestitutionUserBps + SlashRestitutionInsuranceBps +
		SlashRestitutionTreasuryBps + SlashRestitutionBurnBps
	if sum != 10_000 {
		t.Fatalf("slash restitution bps must sum to 10000, got %d "+
			"(user=%d insurance=%d treasury=%d burn=%d)",
			sum, SlashRestitutionUserBps, SlashRestitutionInsuranceBps,
			SlashRestitutionTreasuryBps, SlashRestitutionBurnBps)
	}
}

func TestSlashRestitutionBurnBpsPinnedToSpec(t *testing.T) {
	t.Parallel()
	// Immutable per specs/governance/slashing-rules.md — do not "fix" this
	// value even if governance asks. If the spec itself changes, update the
	// spec first, then this test, then the constant.
	if SlashRestitutionBurnBps != 500 {
		t.Fatalf("slash burn bps pinned to 500 (5%%) per spec, got %d",
			SlashRestitutionBurnBps)
	}
}

func TestSlashRestitutionModuleNamesPinned(t *testing.T) {
	t.Parallel()
	if SlashInsuranceModule != "insurance" {
		t.Fatalf("slash insurance module pinned to %q, got %q",
			"insurance", SlashInsuranceModule)
	}
	// fee_collector is authtypes.FeeCollectorName — the canonical Cosmos
	// SDK treasury account. A drift here would route governance funds to
	// a non-existent module.
	if SlashTreasuryModule != "fee_collector" {
		t.Fatalf("slash treasury module pinned to %q, got %q",
			"fee_collector", SlashTreasuryModule)
	}
}

// helper to check substring without importing strings
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
