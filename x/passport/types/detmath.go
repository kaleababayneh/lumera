
package types

// Deterministic math functions for consensus-safe scoring.
//
// Go's math.Exp and math.Log1p use platform-specific assembly on amd64
// but pure Go on ARM, producing potentially different results across
// CPU architectures. Since scoring results are stored on-chain, any
// bit-level divergence between validators causes consensus failure.
//
// These functions use only IEEE 754 basic operations (+, -, *, /, comparison,
// int conversion) which are REQUIRED to be correctly rounded, guaranteeing
// identical results on all compliant hardware.

// ExpNeg returns exp(-x) for x >= 0 using only IEEE 754 basic operations.
//
// Method: range reduction + Padé [3,3] approximant.
//   - Factor: exp(-x) = (1/e)^n * exp(-f) where n = floor(x), f = x - n, 0 <= f < 1
//   - (1/e)^n via repeated squaring (only *)
//   - exp(-f) via Padé [3,3]: (120 - 60f + 12f² - f³) / (120 + 60f + 12f² + f³)
//
// Accuracy: enough for the millis (0-1000) quantization used by ToProto.
// Fuzz tests pin the broader finite range to the implementation's empirical
// 1e-4 relative-error envelope.
func ExpNeg(x float64) float64 {
	if x != x {
		return 0
	}
	if x <= 0 {
		return 1.0
	}
	// exp(-20) ≈ 2e-9, below any scoring resolution
	if x >= 20 {
		return 0
	}

	// Range reduction: exp(-x) = invE^n * exp(-f)
	n := int(x)
	f := x - float64(n)

	// Compute invE^n via repeated squaring
	// invE = 1/e = 0.36787944117144232159... (nearest float64)
	const invE = 0.36787944117144232159
	pow := 1.0
	base := invE
	k := n
	for k > 0 {
		if k&1 == 1 {
			pow *= base
		}
		base *= base
		k >>= 1
	}

	// Padé [3,3] approximant for exp(-f), f in [0,1)
	// exp(-f) ≈ (120 - 60f + 12f² - f³) / (120 + 60f + 12f² + f³)
	f2 := f * f
	f3 := f2 * f
	num := 120 - 60*f + 12*f2 - f3
	den := 120 + 60*f + 12*f2 + f3

	return pow * (num / den)
}

// Log1p returns log(1+x) for x >= 0 using only IEEE 754 basic operations.
//
// Method: argument reduction + convergent series.
//   - Find k such that (1+x) = 2^k * m where 1 <= m < 2
//   - log(1+x) = k * ln2 + log(m)
//   - Substitute t = (m-1)/(m+1), so log(m) = 2 * sum(t^(2i+1)/(2i+1), i=0..N)
//   - Since m in [1,2), t in [0,1/3), the series converges very rapidly
//   - 10 terms keep scoring-range error below the millis quantization used by ToProto
//
// Only uses +, -, *, / operations on float64 values.
func Log1p(x float64) float64 {
	const maxFiniteFloat64 = 1.7976931348623157e308

	if x != x {
		return 0
	}
	if x <= 0 {
		return 0
	}
	if x > maxFiniteFloat64 {
		return 0
	}

	y := 1.0 + x

	// Argument reduction: find k, m such that y = 2^k * m, 1 <= m < 2
	k := 0
	m := y
	for m >= 2 {
		m /= 2
		k++
	}
	// m is now in [1, 2)

	// log(m) via the series: t = (m-1)/(m+1), log(m) = 2*(t + t³/3 + t⁵/5 + ...)
	t := (m - 1) / (m + 1)
	t2 := t * t

	// Accumulate: sum = t + t³/3 + t⁵/5 + ... + t¹⁹/19
	// For t < 1/3, 10 terms give error < 1e-11.
	sum := t
	tk := t
	tk *= t2
	sum += tk / 3
	tk *= t2
	sum += tk / 5
	tk *= t2
	sum += tk / 7
	tk *= t2
	sum += tk / 9
	tk *= t2
	sum += tk / 11
	tk *= t2
	sum += tk / 13
	tk *= t2
	sum += tk / 15
	tk *= t2
	sum += tk / 17
	tk *= t2
	sum += tk / 19

	// ln(2) as nearest float64
	const ln2 = 0.6931471805599453

	return float64(k)*ln2 + 2*sum
}
