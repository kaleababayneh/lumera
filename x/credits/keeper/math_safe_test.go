//go:build cosmos && cosmos_full

package keeper_test

import (
	stdmath "math"
	mathrand "math/rand"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
)

func TestSafeMulDiv(t *testing.T) {
	tests := []struct {
		name      string
		amount    math.Int
		rate      int64
		scale     int64
		expected  math.Int
		expectErr bool
		errMsg    string
	}{
		{
			name:     "normal percentage calculation",
			amount:   math.NewInt(1000),
			rate:     250, // 2.5%
			scale:    10000,
			expected: math.NewInt(25),
		},
		{
			name:     "zero rate",
			amount:   math.NewInt(1000),
			rate:     0,
			scale:    10000,
			expected: math.NewInt(0),
		},
		{
			name:     "full rate",
			amount:   math.NewInt(1000),
			rate:     10000,
			scale:    10000,
			expected: math.NewInt(1000),
		},
		{
			name:      "negative rate",
			amount:    math.NewInt(1000),
			rate:      -100,
			scale:     10000,
			expectErr: true,
			errMsg:    "rate must be non-negative",
		},
		{
			name:      "zero scale",
			amount:    math.NewInt(1000),
			rate:      100,
			scale:     0,
			expectErr: true,
			errMsg:    "scale must be positive",
		},
		{
			name:      "rate exceeds scale",
			amount:    math.NewInt(1000),
			rate:      10001,
			scale:     10000,
			expectErr: true,
			errMsg:    "rate cannot exceed scale",
		},
		{
			name:     "large amount calculation",
			amount:   math.NewIntFromUint64(stdmath.MaxUint64 / 2),
			rate:     5000, // 50%
			scale:    10000,
			expected: math.NewIntFromUint64(stdmath.MaxUint64 / 4),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := keeper.SafeMulDiv(tc.amount, tc.rate, tc.scale)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected.String(), result.String())
			}
		})
	}
}

func TestSafePercentage(t *testing.T) {
	tests := []struct {
		name        string
		amount      math.Int
		basisPoints uint32
		expected    math.Int
		expectErr   bool
		errMsg      string
	}{
		{
			name:        "10% of 1000",
			amount:      math.NewInt(1000),
			basisPoints: 1000, // 10%
			expected:    math.NewInt(100),
		},
		{
			name:        "0.01% of 1000000",
			amount:      math.NewInt(1000000),
			basisPoints: 1, // 0.01%
			expected:    math.NewInt(100),
		},
		{
			name:        "100% of amount",
			amount:      math.NewInt(500),
			basisPoints: 10000,
			expected:    math.NewInt(500),
		},
		{
			name:        "0% of amount",
			amount:      math.NewInt(500),
			basisPoints: 0,
			expected:    math.NewInt(0),
		},
		{
			name:        "exceeds 100%",
			amount:      math.NewInt(100),
			basisPoints: 10001,
			expectErr:   true,
			errMsg:      "exceeds maximum 10000",
		},
		{
			name:        "2.5% of 1000",
			amount:      math.NewInt(1000),
			basisPoints: 250, // 2.5%
			expected:    math.NewInt(25),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := keeper.SafePercentage(tc.amount, tc.basisPoints)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected.String(), result.String())
			}
		})
	}
}

func TestSafeSubtract(t *testing.T) {
	tests := []struct {
		name       string
		minuend    math.Int
		subtrahend math.Int
		expected   math.Int
		expectErr  bool
		errMsg     string
	}{
		{
			name:       "normal subtraction",
			minuend:    math.NewInt(1000),
			subtrahend: math.NewInt(300),
			expected:   math.NewInt(700),
		},
		{
			name:       "subtract zero",
			minuend:    math.NewInt(500),
			subtrahend: math.NewInt(0),
			expected:   math.NewInt(500),
		},
		{
			name:       "subtract equal amount",
			minuend:    math.NewInt(100),
			subtrahend: math.NewInt(100),
			expected:   math.NewInt(0),
		},
		{
			name:       "would result in negative",
			minuend:    math.NewInt(100),
			subtrahend: math.NewInt(101),
			expectErr:  true,
			errMsg:     "negative value",
		},
		{
			name:       "large numbers",
			minuend:    math.NewIntFromUint64(stdmath.MaxUint64),
			subtrahend: math.NewIntFromUint64(stdmath.MaxUint64 / 2),
			expected:   math.NewIntFromUint64(stdmath.MaxUint64 - stdmath.MaxUint64/2),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := keeper.SafeSubtract(tc.minuend, tc.subtrahend)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected.String(), result.String())
			}
		})
	}
}

func TestCalculateBurnAmount(t *testing.T) {
	tests := []struct {
		name         string
		total        math.Int
		burnRateBPS  uint32
		expectedBurn math.Int
		expectedRem  math.Int
		expectErr    bool
	}{
		{
			name:         "10% burn",
			total:        math.NewInt(1000),
			burnRateBPS:  1000, // 10%
			expectedBurn: math.NewInt(100),
			expectedRem:  math.NewInt(900),
		},
		{
			name:         "0% burn",
			total:        math.NewInt(1000),
			burnRateBPS:  0,
			expectedBurn: math.NewInt(0),
			expectedRem:  math.NewInt(1000),
		},
		{
			name:         "100% burn",
			total:        math.NewInt(500),
			burnRateBPS:  10000,
			expectedBurn: math.NewInt(500),
			expectedRem:  math.NewInt(0),
		},
		{
			name:        "exceeds 100%",
			total:       math.NewInt(100),
			burnRateBPS: 10001,
			expectErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			burn, remaining, err := keeper.CalculateBurnAmount(tc.total, tc.burnRateBPS)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedBurn.String(), burn.String())
				require.Equal(t, tc.expectedRem.String(), remaining.String())
			}
		})
	}
}

func TestCalculateSplit(t *testing.T) {
	tests := []struct {
		name         string
		amount       math.Int
		publisherBPS uint32
		routerBPS    uint32
		referrerBPS  uint32
		expectErr    bool
		errMsg       string
	}{
		{
			name:         "standard split 60-30-10",
			amount:       math.NewInt(1000),
			publisherBPS: 6000,
			routerBPS:    3000,
			referrerBPS:  1000,
			expectErr:    false,
		},
		{
			name:         "equal split",
			amount:       math.NewInt(999), // Test rounding
			publisherBPS: 3333,
			routerBPS:    3334,
			referrerBPS:  3333,
			expectErr:    false,
		},
		{
			name:         "all to publisher",
			amount:       math.NewInt(500),
			publisherBPS: 10000,
			routerBPS:    0,
			referrerBPS:  0,
			expectErr:    false,
		},
		{
			name:         "invalid sum",
			amount:       math.NewInt(100),
			publisherBPS: 5000,
			routerBPS:    4000,
			referrerBPS:  500, // Sum = 9500, not 10000
			expectErr:    true,
			errMsg:       "splits must sum to 10000",
		},
		{
			name:         "exceeds 100%",
			amount:       math.NewInt(100),
			publisherBPS: 5000,
			routerBPS:    4000,
			referrerBPS:  2000, // Sum = 11000
			expectErr:    true,
			errMsg:       "splits must sum to 10000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			publisher, router, originSurface, treasury, referrer, err := keeper.CalculateSplit(
				tc.amount, tc.publisherBPS, tc.routerBPS, 0, 0, tc.referrerBPS,
			)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
				// Verify split amounts sum to original
				total := publisher.Add(router).Add(originSurface).Add(treasury).Add(referrer)
				require.Equal(t, tc.amount.String(), total.String())

				// Verify approximate percentages
				if !tc.amount.IsZero() {
					pubPercent := publisher.MulRaw(10000).Quo(tc.amount)
					routerPercent := router.MulRaw(10000).Quo(tc.amount)
					originPercent := originSurface.MulRaw(10000).Quo(tc.amount)
					treasuryPercent := treasury.MulRaw(10000).Quo(tc.amount)
					refPercent := referrer.MulRaw(10000).Quo(tc.amount)

					amountInt := tc.amount.Int64()
					if amountInt <= 0 {
						amountInt = 1
					}
					tolerance := stdmath.Ceil(10000/float64(amountInt)) + 10
					require.InDelta(t, float64(tc.publisherBPS), float64(pubPercent.Int64()), tolerance)
					require.InDelta(t, float64(tc.routerBPS), float64(routerPercent.Int64()), tolerance)
					require.InDelta(t, float64(0), float64(originPercent.Int64()), tolerance)
					require.InDelta(t, float64(0), float64(treasuryPercent.Int64()), tolerance)
					require.InDelta(t, float64(tc.referrerBPS), float64(refPercent.Int64()), tolerance)
				}
			}
		})
	}
}

func sumPrincipalSplits(parts map[keeper.Principal]math.Int) math.Int {
	total := math.ZeroInt()
	for _, principal := range []keeper.Principal{
		keeper.PrincipalPublisher,
		keeper.PrincipalRouter,
		keeper.PrincipalOriginSurface,
		keeper.PrincipalTreasury,
		keeper.PrincipalReferrer,
		keeper.PrincipalWorkflowAuthor,
	} {
		total = total.Add(parts[principal])
	}
	return total
}

func requirePrincipalSplitConservation(t *testing.T, amount math.Int, parts map[keeper.Principal]math.Int) {
	t.Helper()

	require.Equal(t, amount.String(), sumPrincipalSplits(parts).String(),
		"principal splits must conserve the original amount")
	for _, principal := range []keeper.Principal{
		keeper.PrincipalPublisher,
		keeper.PrincipalRouter,
		keeper.PrincipalOriginSurface,
		keeper.PrincipalTreasury,
		keeper.PrincipalReferrer,
		keeper.PrincipalWorkflowAuthor,
	} {
		require.False(t, parts[principal].IsNegative(), "%s split must be non-negative", principal)
	}
}

func TestComputeSplits_WithWorkflowAuthor_Conservation(t *testing.T) {
	amount := math.NewInt(1_234_567)
	route := keeper.SplitRoute{
		PublisherBPS:      6500,
		RouterBPS:         1800,
		ReferrerBPS:       1000,
		WorkflowAuthorBPS: 700,
	}

	parts, err := keeper.ComputeSplits(amount, route)
	require.NoError(t, err)
	requirePrincipalSplitConservation(t, amount, parts)

	expectedAuthor := amount.MulRaw(int64(route.WorkflowAuthorBPS)).QuoRaw(10_000)
	require.Equal(t, expectedAuthor.String(), parts[keeper.PrincipalWorkflowAuthor].String(),
		"workflow_author share must be a first-class split-table principal")
	require.True(t, parts[keeper.PrincipalWorkflowAuthor].IsPositive(),
		"workflow_author share should be paid when its BPS is non-zero")
}

func TestComputeSplits_LegacyPath_UnaffectedByEnumAdd(t *testing.T) {
	route := keeper.SplitRoute{
		PublisherBPS: 7000,
		RouterBPS:    2000,
		ReferrerBPS:  1000,
	}

	for _, amountRaw := range []int64{1, 7, 333, 9_999, 10_000, 1_234_567} {
		amount := math.NewInt(amountRaw)
		legacyPublisher, legacyRouter, legacyOrigin, legacyTreasury, legacyReferrer, err := keeper.CalculateSplit(
			amount,
			route.PublisherBPS,
			route.RouterBPS,
			route.OriginSurfaceBPS,
			route.TreasuryBPS,
			route.ReferrerBPS,
		)
		require.NoError(t, err)

		parts, err := keeper.ComputeSplits(amount, route)
		require.NoError(t, err)
		requirePrincipalSplitConservation(t, amount, parts)

		require.Equal(t, legacyPublisher.String(), parts[keeper.PrincipalPublisher].String())
		require.Equal(t, legacyRouter.String(), parts[keeper.PrincipalRouter].String())
		require.Equal(t, legacyOrigin.String(), parts[keeper.PrincipalOriginSurface].String())
		require.Equal(t, legacyTreasury.String(), parts[keeper.PrincipalTreasury].String())
		require.Equal(t, legacyReferrer.String(), parts[keeper.PrincipalReferrer].String())
		require.True(t, parts[keeper.PrincipalWorkflowAuthor].IsZero(),
			"legacy tool-only settlements must keep workflow_author at zero")
	}
}

func TestIntegrationCredits_Conservation_10kRandomized(t *testing.T) {
	rng := mathrand.New(mathrand.NewSource(0x80B9F04))

	for i := 0; i < 10_000; i++ {
		workflowAuthorBPS := uint32(rng.Intn(2501))
		remaining := uint32(10_000) - workflowAuthorBPS
		publisherBPS := uint32(rng.Intn(int(remaining) + 1))
		remaining -= publisherBPS
		routerBPS := uint32(rng.Intn(int(remaining) + 1))
		remaining -= routerBPS
		originSurfaceBPS := uint32(rng.Intn(int(remaining) + 1))
		remaining -= originSurfaceBPS
		treasuryBPS := uint32(rng.Intn(int(remaining) + 1))
		referrerBPS := remaining - treasuryBPS

		route := keeper.SplitRoute{
			PublisherBPS:      publisherBPS,
			RouterBPS:         routerBPS,
			OriginSurfaceBPS:  originSurfaceBPS,
			TreasuryBPS:       treasuryBPS,
			ReferrerBPS:       referrerBPS,
			WorkflowAuthorBPS: workflowAuthorBPS,
		}
		amount := math.NewInt(rng.Int63n(1_000_000_000_000) + 1)

		parts, err := keeper.ComputeSplits(amount, route)
		require.NoError(t, err, "iteration %d route=%+v amount=%s", i, route, amount)
		requirePrincipalSplitConservation(t, amount, parts)
	}
}

func TestComputeSplits_SameInvocationSequenceOnTwoReplicas(t *testing.T) {
	script := []struct {
		amount math.Int
		route  keeper.SplitRoute
	}{
		{
			amount: math.NewInt(1),
			route: keeper.SplitRoute{
				PublisherBPS:      6500,
				RouterBPS:         2000,
				ReferrerBPS:       1000,
				WorkflowAuthorBPS: 500,
			},
		},
		{
			amount: math.NewInt(999_999_937),
			route: keeper.SplitRoute{
				PublisherBPS:      5333,
				RouterBPS:         1777,
				OriginSurfaceBPS:  500,
				TreasuryBPS:       300,
				ReferrerBPS:       1400,
				WorkflowAuthorBPS: 690,
			},
		},
		{
			amount: math.NewInt(42_000_000),
			route: keeper.SplitRoute{
				PublisherBPS:      7000,
				RouterBPS:         1500,
				OriginSurfaceBPS:  250,
				TreasuryBPS:       250,
				ReferrerBPS:       0,
				WorkflowAuthorBPS: 1000,
			},
		},
	}

	run := func() []map[keeper.Principal]string {
		snapshots := make([]map[keeper.Principal]string, 0, len(script))
		for _, step := range script {
			parts, err := keeper.ComputeSplits(step.amount, step.route)
			require.NoError(t, err)
			requirePrincipalSplitConservation(t, step.amount, parts)

			snapshot := make(map[keeper.Principal]string, len(parts))
			for principal, amount := range parts {
				snapshot[principal] = amount.String()
			}
			snapshots = append(snapshots, snapshot)
		}
		return snapshots
	}

	require.Equal(t, run(), run(),
		"same invocation sequence must produce byte-identical principal splits on independent replicas")
}

func TestValidateRates(t *testing.T) {
	tests := []struct {
		name           string
		burnRate       uint32
		insuranceRate  uint32
		publisherShare uint32
		routerShare    uint32
		referrerShare  uint32
		expectErr      bool
		errMsg         string
	}{
		{
			name:           "valid rates",
			burnRate:       1000, // 10%
			insuranceRate:  500,  // 5%
			publisherShare: 6000, // 60%
			routerShare:    3000, // 30%
			referrerShare:  1000, // 10%
			expectErr:      false,
		},
		{
			name:           "burn rate too high",
			burnRate:       10001,
			insuranceRate:  500,
			publisherShare: 6000,
			routerShare:    3000,
			referrerShare:  1000,
			expectErr:      true,
			errMsg:         "burn rate",
		},
		{
			name:           "insurance rate too high",
			burnRate:       1000,
			insuranceRate:  10001,
			publisherShare: 6000,
			routerShare:    3000,
			referrerShare:  1000,
			expectErr:      true,
			errMsg:         "insurance rate",
		},
		{
			name:           "shares don't sum to 100%",
			burnRate:       1000,
			insuranceRate:  500,
			publisherShare: 5000,
			routerShare:    3000,
			referrerShare:  1000, // Sum = 9000
			expectErr:      true,
			errMsg:         "shares must sum to 10000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := keeper.ValidateRates(
				tc.burnRate, tc.insuranceRate,
				tc.publisherShare, tc.routerShare, 0, 0, tc.referrerShare,
			)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSafeIncrementCounter(t *testing.T) {
	tests := []struct {
		name      string
		counter   uint64
		expected  uint64
		expectErr bool
	}{
		{
			name:     "normal increment",
			counter:  100,
			expected: 101,
		},
		{
			name:     "increment from zero",
			counter:  0,
			expected: 1,
		},
		{
			name:     "near max value",
			counter:  stdmath.MaxUint64 - 1,
			expected: stdmath.MaxUint64,
		},
		{
			name:      "overflow at max",
			counter:   stdmath.MaxUint64,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := keeper.SafeIncrementCounter(tc.counter)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "overflow")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, result)
			}
		})
	}
}

// TestEdgeCases tests extreme edge cases for overflow protection
func TestEdgeCases(t *testing.T) {
	t.Run("max int operations", func(t *testing.T) {
		// Create a very large math.Int near max
		bigInt, ok := math.NewIntFromString("115792089237316195423570985008687907853269984665640564039457584007913129639935")
		require.True(t, ok)

		// Expect overflow protection rather than truncation
		_, err := keeper.SafePercentage(bigInt, 5000) // 50%
		require.Error(t, err)
		require.Contains(t, err.Error(), "overflow")
	})

	t.Run("rounding precision", func(t *testing.T) {
		// Test that small percentages work correctly
		amount := math.NewInt(1)
		result, err := keeper.SafePercentage(amount, 1) // 0.01%
		require.NoError(t, err)
		require.Equal(t, math.NewInt(0).String(), result.String()) // Rounds down

		amount = math.NewInt(10000)
		result, err = keeper.SafePercentage(amount, 1) // 0.01%
		require.NoError(t, err)
		require.Equal(t, math.NewInt(1).String(), result.String())
	})

	t.Run("split with rounding adjustment", func(t *testing.T) {
		// Test that rounding adjustments work correctly
		amount := math.NewInt(100)
		pub, router, originSurface, treasury, ref, err := keeper.CalculateSplit(amount, 3333, 3333, 0, 0, 3334)
		require.NoError(t, err)

		// Verify total equals original
		total := pub.Add(router).Add(originSurface).Add(treasury).Add(ref)
		require.Equal(t, amount.String(), total.String())

		require.Equal(t, math.NewInt(0).String(), originSurface.String())
		require.Equal(t, math.NewInt(0).String(), treasury.String())

		// Largest share absorbs the rounding adjustment.
		require.Equal(t, math.NewInt(33).String(), pub.String())
		require.Equal(t, math.NewInt(33).String(), router.String())
		require.Equal(t, math.NewInt(34).String(), ref.String())
	})
}
