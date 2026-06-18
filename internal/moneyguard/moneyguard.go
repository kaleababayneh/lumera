// Package moneyguard centralizes bounds checks for shopspring decimal
// inputs that arrive from external sources (publisher responses,
// signed receipts, webhooks, gRPC requests, etc.).
//
// shopspring's decimal.NewFromString accepts arbitrary scientific-
// notation exponents like "1e11100100" and stores them symbolically
// — but any downstream arithmetic (Add, Sub, Mul, Div, Cmp) or
// rendering (String, InexactFloat64) must expand the underlying
// big.Int to the full decimal representation. For absurd exponents
// that means millions of decimal digits, consuming minutes of CPU
// and gigabytes of memory per operation. That turns any caller
// that accepts an external decimal string into a DoS vector.
//
// The guard is uniformly applied with |Exponent()| ≤ MaxAbsExponent
// (default 100) at the boundary where external input is first
// parsed. Realistic market / billing / fee amounts fit comfortably
// within ±30, so ±100 leaves ample headroom for fixed-point
// currencies while still rejecting clearly-adversarial inputs.
//
// Call this at the first point the external decimal is parsed,
// before any arithmetic is performed, so the big.Int never gets
// created in an absurd form in the first place.
package moneyguard

import "github.com/shopspring/decimal"

// MaxAbsExponent is the hard cap on shopspring decimal exponents
// accepted from external sources. Amounts whose |Exponent()| exceeds
// this bound are treated as malformed input.
const MaxAbsExponent = 100

// IsSafeExponent reports whether d's exponent falls within
// [-MaxAbsExponent, +MaxAbsExponent]. Use this as the acceptance
// gate immediately after decimal.NewFromString on any caller-
// controlled decimal input.
func IsSafeExponent(d decimal.Decimal) bool {
	e := d.Exponent()
	return e >= -MaxAbsExponent && e <= MaxAbsExponent
}
