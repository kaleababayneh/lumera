
package types

import (
	"strings"
	"testing"
	"unicode"

	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
)

// FuzzInsuranceDecimalParse tests the decimal parsing patterns used in
// insurance module functions (CalculatePremium, EvaluatePoolHealth, etc).
// These functions use inline decimal.NewFromString + moneyguard.IsSafeExponent
// rather than a central helper.
//
// Metamorphic relations pinned:
//
//  1. Never panic: any input must not crash decimal.NewFromString.
//
//  2. Exponent guard consistency: if IsSafeExponent returns false, the
//     consuming code must fall back to a safe default (zero or 1).
//
//  3. Round-trip stability: if parse + guard passes, re-parsing
//     String() must also pass and yield an equal value.
func FuzzInsuranceDecimalParse(f *testing.F) {
	seeds := []string{
		// Valid numbers
		"0", "1", "-1", "0.5", "-0.5",
		"123456789", "0.123456789",
		"1e10", "1e-10", "1e50", "1e-50",

		// Boundary exponents (±100 is the safe limit)
		"1e100", "1e-100", "1e99", "1e-99",

		// Exponent bombs (must be rejected by guard)
		"1e101", "1e-101", "1e200", "1e-200",
		"1e11100100", "1e-11100100",
		"1.5e500", "9e999",

		// Empty and malformed
		"", " ", "NaN", "Inf", "-Inf",
		"abc", "1.2.3", "--1", "1ee10",

		// Utilization-like values
		"0.75", "0.95", "1.0", "1.5", "2.0",

		// Premium multiplier-like values
		"0", "1", "1.5", "2", "0.5",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// --- Relation 1: Never panic ---
		// Simulate the inline pattern used in insurance functions.
		var val decimal.Decimal
		var parseErr error

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on input %q: %v", input, r)
				}
			}()
			val, parseErr = decimal.NewFromString(input)
		}()

		// --- Relation 2: Exponent guard consistency ---
		if parseErr == nil {
			safe := moneyguard.IsSafeExponent(val)

			// If input has a large exponent in string form, verify guard rejects.
			if containsLargeExponent(input) && safe {
				// Double-check: the actual exponent might be smaller if the
				// string representation is deceptive (e.g., trailing zeros).
				exp := val.Exponent()
				if exp > 100 || exp < -100 {
					t.Fatalf("exponent guard bypass: input=%q exp=%d passed IsSafeExponent",
						input, exp)
				}
			}

			// If guard rejects, verify exponent is actually out of range.
			if !safe {
				exp := val.Exponent()
				if exp >= -100 && exp <= 100 {
					t.Fatalf("false rejection: input=%q exp=%d rejected by IsSafeExponent",
						input, exp)
				}
			}
		}

		// --- Relation 3: Round-trip stability ---
		if parseErr == nil && moneyguard.IsSafeExponent(val) && !val.IsZero() {
			repr := val.String()
			reparsed, err2 := decimal.NewFromString(repr)
			if err2 != nil {
				t.Fatalf("round-trip parse failed: input=%q String()=%q err=%v",
					input, repr, err2)
			}
			if !moneyguard.IsSafeExponent(reparsed) {
				t.Fatalf("round-trip guard failed: input=%q String()=%q", input, repr)
			}
			if !val.Equal(reparsed) {
				t.Fatalf("round-trip value mismatch: input=%q original=%s reparsed=%s",
					input, val.String(), reparsed.String())
			}
		}
	})
}

// FuzzCalculatePremiumMultiplier tests the premium multiplier parsing
// pattern used in CalculatePremium. Empty/invalid/unsafe inputs must
// fall back to multiplier=1.
func FuzzCalculatePremiumMultiplier(f *testing.F) {
	seeds := []string{
		"", "0", "1", "1.5", "2",
		"-1", "abc", "1e101", "1e11100100",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Replicate the CalculatePremium pattern.
		var multiplier decimal.Decimal

		parsed, err := decimal.NewFromString(input)
		if err != nil || input == "" || !moneyguard.IsSafeExponent(parsed) {
			multiplier = decimal.NewFromInt(1)
		} else {
			multiplier = parsed
		}

		// Multiplier must always be safe for arithmetic.
		if !moneyguard.IsSafeExponent(multiplier) {
			t.Fatalf("unsafe multiplier: input=%q result=%s", input, multiplier.String())
		}

		// Non-empty valid safe inputs should use parsed value.
		if err == nil && input != "" && moneyguard.IsSafeExponent(parsed) {
			if !multiplier.Equal(parsed) {
				t.Fatalf("valid input not used: input=%q parsed=%s got=%s",
					input, parsed.String(), multiplier.String())
			}
		}

		// Invalid inputs should fall back to 1.
		if err != nil || input == "" || (err == nil && !moneyguard.IsSafeExponent(parsed)) {
			if !multiplier.Equal(decimal.NewFromInt(1)) {
				t.Fatalf("invalid input should default to 1: input=%q got=%s",
					input, multiplier.String())
			}
		}
	})
}

// FuzzUtilizationParse tests the utilization parsing pattern used in
// EvaluatePoolHealth. Invalid/unsafe inputs must fall back to zero.
func FuzzUtilizationParse(f *testing.F) {
	seeds := []string{
		"", "0", "0.5", "0.75", "0.95", "1.0", "1.5",
		"-0.5", "abc", "1e101", "1e11100100",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Replicate the EvaluatePoolHealth pattern.
		var utilization decimal.Decimal

		parsed, err := decimal.NewFromString(input)
		if err != nil || !moneyguard.IsSafeExponent(parsed) {
			utilization = decimal.Zero
		} else {
			utilization = parsed
		}

		// Utilization must always be safe.
		if !moneyguard.IsSafeExponent(utilization) {
			t.Fatalf("unsafe utilization: input=%q result=%s", input, utilization.String())
		}

		// Valid safe inputs should use parsed value.
		if err == nil && moneyguard.IsSafeExponent(parsed) {
			if !utilization.Equal(parsed) {
				t.Fatalf("valid input not used: input=%q parsed=%s got=%s",
					input, parsed.String(), utilization.String())
			}
		}

		// Invalid inputs should be zero.
		if err != nil || (err == nil && !moneyguard.IsSafeExponent(parsed)) {
			if !utilization.IsZero() {
				t.Fatalf("invalid input should be zero: input=%q got=%s",
					input, utilization.String())
			}
		}
	})
}

// containsLargeExponent detects potential exponent-bomb inputs.
func containsLargeExponent(s string) bool {
	s = strings.ToLower(s)
	idx := strings.IndexByte(s, 'e')
	if idx == -1 {
		return false
	}
	expPart := s[idx+1:]
	if len(expPart) == 0 {
		return false
	}
	if expPart[0] == '+' || expPart[0] == '-' {
		expPart = expPart[1:]
	}
	if len(expPart) == 0 {
		return false
	}
	digitCount := 0
	for _, r := range expPart {
		if unicode.IsDigit(r) {
			digitCount++
		} else {
			break
		}
	}
	return digitCount >= 3
}
