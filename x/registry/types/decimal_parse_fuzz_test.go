//go:build cosmos

package types

import (
	"strings"
	"testing"
	"unicode"
)

// FuzzSafeDecimalFromString tests the SafeDecimalFromString helper with
// fuzz-generated inputs. This helper is used for non-critical parsing
// where errors are silently converted to zero.
//
// Metamorphic relations pinned:
//
//  1. Never panic: any input must return a value without panicking.
//
//  2. Empty string: must return DecimalZero.
//
//  3. Consistency: if SafeDecimalFromStringWithError returns (val, nil),
//     SafeDecimalFromString must return an equal value.
func FuzzSafeDecimalFromString(f *testing.F) {
	seeds := []string{
		// Valid numbers
		"0", "1", "-1", "0.5", "-0.5",
		"123456789", "0.123456789",
		"1e10", "1e-10", "1e50", "1e-50",

		// Boundary exponents
		"1e100", "1e-100", "1e99", "1e-99",

		// Exponent bombs
		"1e101", "1e-101", "1e11100100",

		// Empty and malformed
		"", " ", "NaN", "Inf", "abc",
		"1.2.3", "--1", "1ee10",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// --- Relation 1: Never panic ---
		val := SafeDecimalFromString(input)

		// --- Relation 2: Empty string ---
		if input == "" {
			if !val.Equal(DecimalZero) {
				t.Fatalf("empty string must return DecimalZero; got %s", val.String())
			}
		}

		// --- Relation 3: Consistency with WithError variant ---
		valWithErr, err := SafeDecimalFromStringWithError(input)
		if err == nil {
			// Both should return equal values when no error.
			// Note: SafeDecimalFromString doesn't have exponent guard,
			// so we can't guarantee equality for exponent bombs.
			// But for normal inputs they should match.
			if !containsLargeExponent(input) {
				if !val.Equal(valWithErr) {
					t.Fatalf("SafeDecimalFromString(%q)=%s != SafeDecimalFromStringWithError=%s",
						input, val.String(), valWithErr.String())
				}
			}
		}
	})
}

// FuzzSafeDecimalFromStringStrict tests the strict variant used for
// critical financial calculations. This is the primary guard against
// exponent-bomb DoS across settlement, dispute, SLA, and reputation paths.
//
// Metamorphic relations pinned:
//
//  1. Never panic: any input must return (value, error) without panicking.
//
//  2. Empty string rejection: must return error with fieldName.
//
//  3. Negative rejection: negative values must return error.
//
//  4. Exponent-bomb rejection: inputs with |exponent| > 100 must error.
//
//  5. Round-trip stability: if parse succeeds, re-parsing String()
//     must also succeed and yield an equal value.
func FuzzSafeDecimalFromStringStrict(f *testing.F) {
	seeds := []string{
		// Valid non-negative numbers
		"0", "1", "0.5", "123456789", "0.123456789",
		"1e10", "1e-10", "1e50", "1e-50",
		"1e100", "1e-100", "1e99", "1e-99",

		// Negative (must be rejected)
		"-1", "-0.5", "-100", "-1e10",

		// Exponent bombs (must be rejected)
		"1e101", "1e-101", "1e200", "1e-200",
		"1e11100100", "1.5e500", "9e999",

		// Empty and malformed
		"", " ", "NaN", "Inf", "abc",
		"1.2.3", "--1", "1ee10",

		// Unicode
		"١٢٣", "∞", "\x00",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		fieldName := "test_field"

		// --- Relation 1: Never panic ---
		val, err := SafeDecimalFromStringStrict(input, fieldName)

		// --- Relation 2: Empty string rejection ---
		if input == "" {
			if err == nil {
				t.Fatalf("empty string must error; got val=%s", val.String())
			}
			if !strings.Contains(err.Error(), fieldName) {
				t.Fatalf("error must contain fieldName; got %v", err)
			}
		}

		// --- Relation 3: Negative rejection ---
		if err == nil && val.IsNegative() {
			t.Fatalf("negative value must be rejected; got %s", val.String())
		}

		// --- Relation 4: Exponent-bomb rejection ---
		if err == nil {
			exp := val.Exponent()
			if exp > 100 || exp < -100 {
				t.Fatalf("exponent-bomb bypass: got exp=%d without error", exp)
			}
		}

		// --- Relation 5: Round-trip stability ---
		if err == nil && !val.IsZero() {
			repr := val.String()
			reparsed, err2 := SafeDecimalFromStringStrict(repr, fieldName)
			if err2 != nil {
				t.Fatalf("round-trip failed: String()=%q error=%v", repr, err2)
			}
			if !val.Equal(reparsed) {
				t.Fatalf("round-trip mismatch: %s vs %s", val.String(), reparsed.String())
			}
		}
	})
}

// FuzzValidateDecimalString tests the validation helper.
func FuzzValidateDecimalString(f *testing.F) {
	seeds := []string{
		"0", "42.5", "-100", "1e10",
		"", "NaN", "abc", "1e11100100",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		fieldName := "test_field"

		// Must never panic.
		val, err := ValidateDecimalString(input, fieldName)

		// Empty is valid and returns zero.
		if input == "" {
			if err != nil {
				t.Fatalf("empty string must not error; got %v", err)
			}
			if !val.Equal(DecimalZero) {
				t.Fatalf("empty string must return DecimalZero; got %s", val.String())
			}
		}

		// Note: ValidateDecimalString doesn't have an exponent guard, so
		// exponent bombs may pass. That's by design for non-critical paths.
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
