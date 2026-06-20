//go:build cosmos

package keeper

import (
	"math"
	"testing"
)

// FuzzSafeAddUint64 pins the saturating-add contract so future rewrites
// (e.g. swapping in math/bits.Add64 or dropping overflow protection entirely)
// cannot silently change the behavior that the budget paths rely on:
//
//   - Identity:        a + 0 == a                         (and 0 + b == b)
//   - Commutativity:   safeAddUint64(a,b) == safeAddUint64(b,a)
//   - Saturation:      result <= MaxUint64
//   - Monotonicity:    result >= a  AND  result >= b
//   - Exactness:       when a + b does not overflow, result == a + b
//   - Saturation flag: when a + b overflows, result == MaxUint64
//
// These invariants are what the budget enforcement paths in enforce.go
// implicitly depend on when they accumulate cost windows: an overflow must
// reject the invocation (by appearing as a cost > hard limit) rather than
// wrapping to a small number that would slip past the limit check.
// TestCheckBudgetLimits_OverflowProtection already pins the end-to-end
// rejection; this test pins the arithmetic primitive itself.
func FuzzSafeAddUint64(f *testing.F) {
	seeds := [][2]uint64{
		{0, 0},
		{1, 0},
		{0, 1},
		{100, 200},
		{math.MaxUint64, 0},
		{0, math.MaxUint64},
		{math.MaxUint64, 1},
		{1, math.MaxUint64},
		{math.MaxUint64 - 10, 20},
		{math.MaxUint64 - 100, 50},
		{math.MaxUint64 / 2, math.MaxUint64 / 2},
		{math.MaxUint64, math.MaxUint64},
	}
	for _, seed := range seeds {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, a, b uint64) {
		result := safeAddUint64(a, b)

		// Commutativity: ordering must not change the answer.
		if reverse := safeAddUint64(b, a); reverse != result {
			t.Fatalf("commutativity violated: safeAddUint64(%d,%d)=%d but safeAddUint64(%d,%d)=%d",
				a, b, result, b, a, reverse)
		}

		// Monotonicity: result is at least the larger input. Both inputs are
		// uint64, so the result cannot be less than either.
		if result < a {
			t.Fatalf("monotonicity violated: safeAddUint64(%d,%d)=%d < %d", a, b, result, a)
		}
		if result < b {
			t.Fatalf("monotonicity violated: safeAddUint64(%d,%d)=%d < %d", a, b, result, b)
		}

		// Overflow branch: when a+b exceeds uint64 range, the function must
		// saturate to MaxUint64. Detect overflow via a > MaxUint64 - b (mirrors
		// the implementation but computed independently of the helper).
		if a > math.MaxUint64-b {
			if result != math.MaxUint64 {
				t.Fatalf("saturation violated: safeAddUint64(%d,%d)=%d, want MaxUint64", a, b, result)
			}
			return
		}

		// Non-overflow branch: the result must be exactly a+b.
		if got, want := result, a+b; got != want {
			t.Fatalf("exactness violated: safeAddUint64(%d,%d)=%d, want %d", a, b, got, want)
		}

		// Identity: adding zero on either side returns the other operand.
		if b == 0 && result != a {
			t.Fatalf("identity violated: safeAddUint64(%d,0)=%d, want %d", a, result, a)
		}
		if a == 0 && result != b {
			t.Fatalf("identity violated: safeAddUint64(0,%d)=%d, want %d", b, result, b)
		}
	})
}
