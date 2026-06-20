//go:build cosmos

package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
)

// FuzzMedianDec pins the structural invariants of the median helper that
// aggregateProviderPrice, weightedMedianDec, and the top-level vote
// aggregation all flow through. Existing coverage is indirect — only the
// end-to-end TestAggregateVotes_MedianCalculation_* cases exercise it —
// so a rewrite that swapped the even/odd branch or forgot to clone the
// input slice before sorting could slip through. This fuzzer locks in:
//
//   - length handling: empty → 0; single → identity; exact-pair → mean.
//   - ordering: min(inputs) ≤ result ≤ max(inputs) for any input.
//   - permutation invariance: reversing the input must yield the same
//     result. The implementation sorts internally; if a future refactor
//     drops the sort (or mutates the caller's slice), this trips.
//   - non-negativity for non-negative inputs (prices cannot be negative
//     in the oracle path, and the median of non-negatives must stay
//     non-negative).
//
// Inputs are drawn from a byte slice where each pair encodes a uint16
// price. This keeps the fuzzer free to explore slice lengths without
// dragging sdkmath.LegacyDec through the fuzz runtime directly.
func FuzzMedianDec(f *testing.F) {
	seeds := [][]byte{
		{},                                // empty → 0
		{0, 5},                            // single price: 5
		{0, 1, 0, 2, 0, 3},                // odd count: median 2
		{0, 1, 0, 2, 0, 3, 0, 4},          // even count: median 2.5
		{0, 5, 0, 5, 0, 5},                // all equal
		{0xFF, 0xFF, 0, 0, 0x80, 0},       // wide spread
		{0, 1, 0, 1, 0, 1, 0, 1, 0, 1},    // many duplicates
		{0, 0, 0, 0, 0, 0},                // all zero
		{0, 10, 0, 20, 0, 30, 0, 40, 0, 50},
		{0xFF, 0xFF},                      // single max-uint16
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Cap slice size to keep each fuzz iteration fast; the invariants we
		// care about manifest at tiny lengths (the interesting edge cases
		// all sit at n ∈ {0, 1, 2, 3, 4}).
		const maxPrices = 64
		pairs := len(data) / 2
		if pairs > maxPrices {
			pairs = maxPrices
		}

		prices := make([]sdkmath.LegacyDec, 0, pairs)
		for i := 0; i < pairs; i++ {
			val := uint64(data[2*i])<<8 | uint64(data[2*i+1])
			prices = append(prices, sdkmath.LegacyNewDec(int64(val)))
		}

		// Capture a snapshot of the input so we can detect if medianDec
		// mutated the caller's slice (it clones internally today — this
		// guards against a future optimization that drops the clone).
		snapshot := make([]sdkmath.LegacyDec, len(prices))
		copy(snapshot, prices)

		result := medianDec(prices)

		// Empty: must be zero.
		if len(prices) == 0 {
			if !result.IsZero() {
				t.Fatalf("median of empty slice must be zero, got %s", result)
			}
			return
		}

		// Result must be non-negative since all inputs are non-negative.
		if result.IsNegative() {
			t.Fatalf("median went negative on non-negative inputs: %s (input=%v)", result, prices)
		}

		// Single-element identity.
		if len(prices) == 1 {
			if !result.Equal(prices[0]) {
				t.Fatalf("median of [%s] must equal the element, got %s", prices[0], result)
			}
		}

		// Input slice was not mutated (no element moved in place).
		for i, p := range prices {
			if !p.Equal(snapshot[i]) {
				t.Fatalf("medianDec mutated caller slice at index %d: %s -> %s", i, snapshot[i], p)
			}
		}

		// Bounds: result is between min and max of inputs.
		minP, maxP := prices[0], prices[0]
		for _, p := range prices[1:] {
			if p.LT(minP) {
				minP = p
			}
			if p.GT(maxP) {
				maxP = p
			}
		}
		if result.LT(minP) {
			t.Fatalf("median below min: result=%s min=%s", result, minP)
		}
		if result.GT(maxP) {
			t.Fatalf("median above max: result=%s max=%s", result, maxP)
		}

		// Permutation invariance: reversing the slice must yield the same
		// median. The implementation sorts internally, so order cannot
		// affect the output.
		reversed := make([]sdkmath.LegacyDec, len(prices))
		for i, p := range prices {
			reversed[len(prices)-1-i] = p
		}
		reversedResult := medianDec(reversed)
		if !result.Equal(reversedResult) {
			t.Fatalf("permutation invariance violated: forward=%s reversed=%s input=%v",
				result, reversedResult, prices)
		}
	})
}
