package moneyguard

import (
	"testing"

	"github.com/shopspring/decimal"
)

// FuzzIsSafeExponent pins the contract of the shopspring-decimal
// DoS guard. IsSafeExponent is called at every boundary where an
// external decimal string is first parsed (publisher responses,
// signed receipts, webhooks, gRPC requests) — output gates any
// downstream arithmetic on the value. Drift here silently re-
// opens the DoS vector documented in the package docstring.
//
// Four invariants across arbitrary exponent values:
//
//  1. No panic on any decimal.Decimal input (constructed from a
//     fuzz-generated string that may or may not parse).
//  2. Deterministic: same input produces same output on
//     consecutive calls.
//  3. Boundary values |Exponent()| == MaxAbsExponent are ACCEPTED
//     (inclusive upper bound).
//  4. Values with |Exponent()| > MaxAbsExponent are REJECTED.
//     A refactor that flipped the comparison to strict-less-than
//     would let exponents at the boundary through — which is a
//     permissive direction but still a contract change.
func FuzzIsSafeExponent(f *testing.F) {
	seeds := []string{
		"0",
		"1",
		"-1",
		"1e-100",
		"1e100",
		"1e-101",
		"1e101",
		"1.234567890123456789e-50",
		"9999999999999999999999999999999e99",
		"1e1000000", // extreme
		"0.0000000000000000000000001",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		d, err := decimal.NewFromString(raw)
		if err != nil {
			// Skip unparseable inputs — IsSafeExponent only
			// applies to valid decimal values.
			return
		}

		// Invariant 1: no panic.
		got := IsSafeExponent(d)

		// Invariant 2: deterministic.
		if again := IsSafeExponent(d); again != got {
			t.Fatalf("non-deterministic: first=%v second=%v (raw=%q)",
				got, again, raw)
		}

		// Invariant 3/4: boundary contract.
		e := d.Exponent()
		expected := e >= -MaxAbsExponent && e <= MaxAbsExponent
		if got != expected {
			t.Fatalf("boundary mismatch: raw=%q Exponent=%d got=%v expected=%v (|e| vs MaxAbsExponent=%d)",
				raw, e, got, expected, MaxAbsExponent)
		}
	})
}
