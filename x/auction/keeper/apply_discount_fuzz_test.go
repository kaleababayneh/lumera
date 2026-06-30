package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FuzzApplyDiscount pins the contract of the discount helper that the auction
// finalize path uses to compute BestBidPrice and SubmitSpotBid uses to gate
// the min-decrement check. Existing table tests (TestApplyDiscount_*) cover
// 0/250/5000/over-max bps on a single amount; this fuzzer adds:
//
//   - denom preservation (the output coin always carries the input denom)
//   - zero-bps identity (discountBps == 0 ⇒ amount unchanged)
//   - full-discount zeroing (discountBps == 10_000 ⇒ amount zero)
//   - clamp at 10_000 (discountBps > 10_000 behaves identically to 10_000)
//   - non-negativity (result amount >= 0 for non-negative input)
//   - result bound (result amount <= input amount)
//   - monotonicity (a larger discount never yields a larger result)
//
// These are the invariants SubmitSpotBid's `limitPrice` comparison and
// FinalizeSpotAuction's `BestBidPrice` assignment implicitly depend on: a
// discount that could silently grow the amount, flip the denom, or go
// negative would corrupt the reserve-escrow accounting downstream.
func FuzzApplyDiscount(f *testing.F) {
	seeds := []struct {
		amount int64
		bps    uint32
	}{
		{0, 0},
		{0, 5000},
		{1, 0},
		{1, 10000},
		{10_000, 0},
		{10_000, 1},
		{10_000, 250},
		{10_000, 5000},
		{10_000, 9999},
		{10_000, 10_000},
		{10_000, 10_001},
		{10_000, 15_000},
		{1_000_000, 7777},
		{9_223_372_036, 1234}, // near max int32 scale
	}
	for _, s := range seeds {
		f.Add(s.amount, s.bps)
	}

	const denom = "ulac"

	f.Fuzz(func(t *testing.T, amountInt int64, discountBps uint32) {
		// Skip negative amounts: sdk.NewCoin would panic and the production
		// callers never construct negative amounts.
		if amountInt < 0 {
			return
		}
		amount := sdk.NewCoin(denom, sdkmath.NewInt(amountInt))

		result := applyDiscount(amount, discountBps)

		// Denom preservation.
		if result.Denom != amount.Denom {
			t.Fatalf("denom changed: %q -> %q", amount.Denom, result.Denom)
		}

		// Non-negativity.
		if result.Amount.IsNegative() {
			t.Fatalf("negative result: amount=%s bps=%d result=%s", amount.Amount, discountBps, result.Amount)
		}

		// Result <= input.
		if result.Amount.GT(amount.Amount) {
			t.Fatalf("result exceeds input: amount=%s bps=%d result=%s", amount.Amount, discountBps, result.Amount)
		}

		// Zero-bps identity.
		if discountBps == 0 && !result.Amount.Equal(amount.Amount) {
			t.Fatalf("zero-bps must be identity: amount=%s result=%s", amount.Amount, result.Amount)
		}

		// Full-discount zeroing (10_000 bps).
		if discountBps == 10_000 && !result.Amount.IsZero() {
			t.Fatalf("10000 bps must zero the amount: amount=%s result=%s", amount.Amount, result.Amount)
		}

		// Clamp at 10_000: any bps above the ceiling behaves identically to
		// exactly-10_000.
		if discountBps > 10_000 {
			clamped := applyDiscount(amount, 10_000)
			if !result.Amount.Equal(clamped.Amount) {
				t.Fatalf("over-max discount did not clamp: bps=%d result=%s clamped=%s",
					discountBps, result.Amount, clamped.Amount)
			}
		}

		// Monotonicity: a larger discount never produces a larger result. We
		// compare against discountBps-1 (when available) to pin the gradient.
		if discountBps > 0 && discountBps <= 10_000 {
			smaller := applyDiscount(amount, discountBps-1)
			if result.Amount.GT(smaller.Amount) {
				t.Fatalf("monotonicity violated: bps=%d (%s) < bps=%d (%s)",
					discountBps-1, smaller.Amount, discountBps, result.Amount)
			}
		}
	})
}
