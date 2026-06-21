package keeper_test

import (
	stdmath "math"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
)

// FuzzSafeMulDiv tests SafeMulDiv with arbitrary amounts and rates to ensure
// no panics occur and overflow protection works correctly.
func FuzzSafeMulDiv(f *testing.F) {
	// Seed corpus with representative cases
	f.Add(int64(1000), int64(250), int64(10000))                // Normal percentage
	f.Add(int64(0), int64(5000), int64(10000))                  // Zero amount
	f.Add(int64(1000), int64(0), int64(10000))                  // Zero rate
	f.Add(int64(1000), int64(10000), int64(10000))              // Full rate (100%)
	f.Add(int64(stdmath.MaxInt64/2), int64(5000), int64(10000)) // Large amount
	f.Add(int64(1), int64(1), int64(10000))                     // Minimal values
	f.Add(int64(stdmath.MaxInt64), int64(1), int64(10000))      // Max int64
	f.Add(int64(100), int64(-100), int64(10000))                // Negative rate (should error)
	f.Add(int64(100), int64(100), int64(0))                     // Zero scale (should error)
	f.Add(int64(100), int64(10001), int64(10000))               // Rate exceeds scale (should error)

	f.Fuzz(func(t *testing.T, amountInt, rate, scale int64) {
		// Skip obviously invalid scale
		if scale <= 0 {
			return
		}

		amount := math.NewInt(amountInt)
		result, err := keeper.SafeMulDiv(amount, rate, scale)

		// Verify invariants
		if rate < 0 {
			require.Error(t, err, "negative rate should error")
			return
		}
		if rate > scale {
			require.Error(t, err, "rate > scale should error")
			return
		}

		// If no error, verify result is reasonable
		if err == nil {
			// Result should be non-negative when inputs are non-negative
			if amountInt >= 0 {
				require.False(t, result.IsNegative(), "result should be non-negative")
			}

			// Result should be <= amount when rate <= scale
			if amountInt >= 0 && rate <= scale {
				require.True(t, result.LTE(amount) || result.Equal(amount),
					"result should not exceed amount when rate <= scale")
			}
		}
	})
}

// FuzzSafePercentage tests basis point calculations with arbitrary amounts.
func FuzzSafePercentage(f *testing.F) {
	// Seed corpus
	f.Add(int64(1000), uint32(1000))             // 10% of 1000
	f.Add(int64(1000000), uint32(1))             // 0.01% of 1M
	f.Add(int64(500), uint32(10000))             // 100% of 500
	f.Add(int64(500), uint32(0))                 // 0% of 500
	f.Add(int64(100), uint32(10001))             // Exceeds 100% (should error)
	f.Add(int64(stdmath.MaxInt64), uint32(5000)) // Large amount
	f.Add(int64(1), uint32(1))                   // Small amount with small bps
	f.Add(int64(10000), uint32(5000))            // Round number

	f.Fuzz(func(t *testing.T, amountInt int64, basisPoints uint32) {
		amount := math.NewInt(amountInt)
		result, err := keeper.SafePercentage(amount, basisPoints)

		// Verify invariants
		if basisPoints > 10000 {
			require.Error(t, err, "bps > 10000 should error")
			return
		}

		// If no error, verify result
		if err == nil {
			// Result should be non-negative for non-negative input
			if amountInt >= 0 {
				require.False(t, result.IsNegative(), "result should be non-negative")
			}

			// Result should be <= amount (since bps <= 10000)
			if amountInt >= 0 {
				require.True(t, result.LTE(amount),
					"result should not exceed amount for bps <= 10000")
			}

			// Zero bps should yield zero result
			if basisPoints == 0 {
				require.True(t, result.IsZero(), "0 bps should yield 0")
			}

			// 10000 bps (100%) should yield original amount
			if basisPoints == 10000 {
				require.Equal(t, amount.String(), result.String(), "10000 bps should yield original")
			}
		}
	})
}

// FuzzSafeSubtract tests subtraction with underflow detection.
func FuzzSafeSubtract(f *testing.F) {
	// Seed corpus
	f.Add(int64(1000), int64(300))                            // Normal subtraction
	f.Add(int64(500), int64(0))                               // Subtract zero
	f.Add(int64(100), int64(100))                             // Subtract equal
	f.Add(int64(100), int64(101))                             // Would underflow
	f.Add(int64(stdmath.MaxInt64), int64(stdmath.MaxInt64/2)) // Large numbers
	f.Add(int64(0), int64(0))                                 // Both zero
	f.Add(int64(0), int64(1))                                 // Zero minus positive

	f.Fuzz(func(t *testing.T, minuendInt, subtrahendInt int64) {
		minuend := math.NewInt(minuendInt)
		subtrahend := math.NewInt(subtrahendInt)

		result, err := keeper.SafeSubtract(minuend, subtrahend)

		// Verify invariants
		if minuend.LT(subtrahend) {
			require.Error(t, err, "underflow should error")
			return
		}

		// If no error, verify result
		if err == nil {
			require.False(t, result.IsNegative(), "result should be non-negative")

			// Verify arithmetic: minuend - subtrahend = result
			expectedResult := minuend.Sub(subtrahend)
			require.Equal(t, expectedResult.String(), result.String())
		}
	})
}

// FuzzCalculateBurnAmount tests burn calculations with arbitrary totals and rates.
func FuzzCalculateBurnAmount(f *testing.F) {
	// Seed corpus
	f.Add(int64(1000), uint32(1000))                // 10% burn
	f.Add(int64(1000), uint32(0))                   // 0% burn
	f.Add(int64(500), uint32(10000))                // 100% burn
	f.Add(int64(100), uint32(10001))                // Exceeds 100% (should error)
	f.Add(int64(stdmath.MaxInt64/10), uint32(5000)) // Large amount with 50% burn
	f.Add(int64(1), uint32(5000))                   // Minimal amount

	f.Fuzz(func(t *testing.T, totalInt int64, burnRateBPS uint32) {
		if totalInt < 0 {
			return // Skip negative totals
		}

		total := math.NewInt(totalInt)
		burn, remaining, err := keeper.CalculateBurnAmount(total, burnRateBPS)

		// Verify invariants
		if burnRateBPS > 10000 {
			require.Error(t, err, "burn rate > 10000 should error")
			return
		}

		// If no error, verify conservation
		if err == nil {
			// burn + remaining should equal total (conservation)
			sum := burn.Add(remaining)
			require.Equal(t, total.String(), sum.String(),
				"burn + remaining should equal total (conservation of value)")

			// Both should be non-negative
			require.False(t, burn.IsNegative(), "burn should be non-negative")
			require.False(t, remaining.IsNegative(), "remaining should be non-negative")

			// 0% burn means no burn
			if burnRateBPS == 0 {
				require.True(t, burn.IsZero(), "0% burn should yield 0")
				require.Equal(t, total.String(), remaining.String())
			}

			// 100% burn means nothing remains
			if burnRateBPS == 10000 {
				require.True(t, remaining.IsZero(), "100% burn should leave nothing")
				require.Equal(t, total.String(), burn.String())
			}
		}
	})
}

// FuzzCalculateSplit tests revenue split calculations with arbitrary amounts and BPS distributions.
func FuzzCalculateSplit(f *testing.F) {
	// Seed corpus - BPS must sum to 10000
	f.Add(int64(1000), uint32(6000), uint32(3000), uint32(0), uint32(0), uint32(1000))                    // Standard
	f.Add(int64(999), uint32(3333), uint32(3334), uint32(0), uint32(0), uint32(3333))                     // Rounding test
	f.Add(int64(500), uint32(10000), uint32(0), uint32(0), uint32(0), uint32(0))                          // All to publisher
	f.Add(int64(100), uint32(2000), uint32(2000), uint32(2000), uint32(2000), uint32(2000))               // Even 5-way split
	f.Add(int64(stdmath.MaxInt64/10), uint32(5000), uint32(3000), uint32(1000), uint32(500), uint32(500)) // Large amount
	f.Add(int64(1), uint32(5000), uint32(3000), uint32(1000), uint32(500), uint32(500))                   // Minimal amount
	f.Add(int64(7), uint32(3333), uint32(3333), uint32(1667), uint32(834), uint32(833))                   // Prime number, rounding

	f.Fuzz(func(t *testing.T, amountInt int64, pubBPS, routerBPS, originBPS, treasuryBPS, refBPS uint32) {
		if amountInt < 0 {
			return // Skip negative amounts
		}

		// Only test when BPS sum to 10000
		totalBPS := uint64(pubBPS) + uint64(routerBPS) + uint64(originBPS) + uint64(treasuryBPS) + uint64(refBPS)
		if totalBPS != 10000 {
			return
		}

		amount := math.NewInt(amountInt)
		pub, router, origin, treasury, ref, err := keeper.CalculateSplit(
			amount, pubBPS, routerBPS, originBPS, treasuryBPS, refBPS,
		)

		// If successful, verify invariants
		if err == nil {
			// Conservation: all splits should sum to original amount
			total := pub.Add(router).Add(origin).Add(treasury).Add(ref)
			require.Equal(t, amount.String(), total.String(),
				"splits must sum to original amount (conservation)")

			// All values should be non-negative
			require.False(t, pub.IsNegative(), "publisher should be non-negative")
			require.False(t, router.IsNegative(), "router should be non-negative")
			require.False(t, origin.IsNegative(), "origin_surface should be non-negative")
			require.False(t, treasury.IsNegative(), "treasury should be non-negative")
			require.False(t, ref.IsNegative(), "referrer should be non-negative")

			// Zero BPS should yield zero allocation
			if pubBPS == 0 {
				require.True(t, pub.IsZero(), "0 bps publisher should be 0")
			}
			if routerBPS == 0 {
				require.True(t, router.IsZero(), "0 bps router should be 0")
			}
			if originBPS == 0 {
				require.True(t, origin.IsZero(), "0 bps origin should be 0")
			}
			if treasuryBPS == 0 {
				require.True(t, treasury.IsZero(), "0 bps treasury should be 0")
			}
			if refBPS == 0 {
				require.True(t, ref.IsZero(), "0 bps referrer should be 0")
			}
		}
	})
}

// FuzzValidateRates tests rate validation with arbitrary rate combinations.
func FuzzValidateRates(f *testing.F) {
	// Seed corpus
	f.Add(uint32(1000), uint32(500), uint32(6000), uint32(3000), uint32(0), uint32(0), uint32(1000))      // Valid
	f.Add(uint32(10001), uint32(500), uint32(6000), uint32(3000), uint32(0), uint32(0), uint32(1000))     // Burn too high
	f.Add(uint32(1000), uint32(10001), uint32(6000), uint32(3000), uint32(0), uint32(0), uint32(1000))    // Insurance too high
	f.Add(uint32(6000), uint32(5000), uint32(6000), uint32(3000), uint32(0), uint32(0), uint32(1000))     // Combined > 100%
	f.Add(uint32(0), uint32(0), uint32(10000), uint32(0), uint32(0), uint32(0), uint32(0))                // No deductions
	f.Add(uint32(5000), uint32(5000), uint32(5000), uint32(3000), uint32(1000), uint32(500), uint32(500)) // Max deductions

	f.Fuzz(func(t *testing.T, burnRate, insuranceRate, pubShare, routerShare, originShare, treasuryShare, refShare uint32) {
		err := keeper.ValidateRates(
			burnRate, insuranceRate,
			pubShare, routerShare, originShare, treasuryShare, refShare,
		)

		// Verify invariants
		if burnRate > 10000 {
			require.Error(t, err, "burn rate > 10000 should error")
			return
		}
		if insuranceRate > 10000 {
			require.Error(t, err, "insurance rate > 10000 should error")
			return
		}

		// Combined deductions cannot exceed 100%
		if uint64(burnRate)+uint64(insuranceRate) > 10000 {
			require.Error(t, err, "combined burn + insurance > 10000 should error")
			return
		}

		// Shares must sum to 10000
		totalShares := uint64(pubShare) + uint64(routerShare) + uint64(originShare) + uint64(treasuryShare) + uint64(refShare)
		if totalShares != 10000 {
			require.Error(t, err, "shares not summing to 10000 should error")
			return
		}

		// If we get here, all constraints are satisfied
		require.NoError(t, err, "valid rates should not error")
	})
}

// FuzzSafeIncrementCounter tests counter increment with overflow detection.
func FuzzSafeIncrementCounter(f *testing.F) {
	// Seed corpus
	f.Add(uint64(0))
	f.Add(uint64(100))
	f.Add(uint64(stdmath.MaxUint64 - 1))
	f.Add(uint64(stdmath.MaxUint64))
	f.Add(uint64(stdmath.MaxUint64 / 2))

	f.Fuzz(func(t *testing.T, counter uint64) {
		result, err := keeper.SafeIncrementCounter(counter)

		// Verify invariants
		if counter == stdmath.MaxUint64 {
			require.Error(t, err, "max uint64 should overflow")
			return
		}

		// If no error, verify result
		if err == nil {
			require.Equal(t, counter+1, result, "result should be counter + 1")
		}
	})
}

// FuzzLockSettleConservation tests that lock -> settle preserves value conservation.
func FuzzLockSettleConservation(f *testing.F) {
	// Test that splitting a locked amount and then summing parts equals original
	f.Add(int64(10000), uint32(1000), uint32(5000), uint32(3000), uint32(1000), uint32(500), uint32(500))

	f.Fuzz(func(t *testing.T, lockedAmountInt int64, burnBPS, pubBPS, routerBPS, originBPS, treasuryBPS, refBPS uint32) {
		if lockedAmountInt <= 0 {
			return
		}

		// Constrain burn rate
		if burnBPS > 10000 {
			return
		}

		// Ensure splits sum to 10000
		totalBPS := uint64(pubBPS) + uint64(routerBPS) + uint64(originBPS) + uint64(treasuryBPS) + uint64(refBPS)
		if totalBPS != 10000 {
			return
		}

		lockedAmount := math.NewInt(lockedAmountInt)

		// Step 1: Calculate burn
		burn, afterBurn, err := keeper.CalculateBurnAmount(lockedAmount, burnBPS)
		if err != nil {
			return
		}

		// Verify: locked = burn + afterBurn
		require.Equal(t, lockedAmount.String(), burn.Add(afterBurn).String(),
			"burn conservation: locked = burn + afterBurn")

		// Step 2: Calculate splits from afterBurn
		pub, router, origin, treasury, ref, err := keeper.CalculateSplit(
			afterBurn, pubBPS, routerBPS, originBPS, treasuryBPS, refBPS,
		)
		if err != nil {
			return
		}

		// Verify: afterBurn = sum of all splits
		splitSum := pub.Add(router).Add(origin).Add(treasury).Add(ref)
		require.Equal(t, afterBurn.String(), splitSum.String(),
			"split conservation: afterBurn = sum(splits)")

		// Final verification: locked = burn + sum(splits)
		totalDistributed := burn.Add(splitSum)
		require.Equal(t, lockedAmount.String(), totalDistributed.String(),
			"total conservation: locked = burn + sum(splits)")
	})
}
