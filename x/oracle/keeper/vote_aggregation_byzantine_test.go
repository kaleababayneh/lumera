//go:build cosmos

package keeper

import (
	"fmt"
	"math/rand"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// Byzantine metamorphic tests for x/oracle vote aggregation.
//
// BFT context
// -----------
// Lumera consensus tolerates up to f byzantine validators in a set of
// n = 3f+1 total validators. The oracle module must preserve price
// integrity under this fault model:
//   - |honest| = 2f+1 (supermajority)
//   - |byzantine| ≤ f
//
// A byzantine validator can vote any price, submit duplicate feeds,
// stay silent, or collude with other byzantines. The aggregated median
// must still reflect the honest supermajority — otherwise the chain
// can be manipulated by f colluding validators, breaking the BFT
// security guarantee.
//
// These metamorphic relations (MRs) encode input transformations that
// probe byzantine resistance:
//
//   MR1 RangeBounded: 2f+1 honest votes in [L,H] + f byzantine votes
//       anywhere => median in [L,H] (strict bound when outlier filter
//       rejects extremes; soft bound otherwise but honest supermajority
//       still dominates the median position).
//
//   MR2 HonestSupermajorityDominates: 2f+1 honest votes at price P +
//       f byzantine votes at arbitrary prices => median = P.
//       Catches comparators where the byzantine minority can shift the
//       sorted-median position.
//
//   MR3 SymmetricByzantineCancels: f byzantine split evenly high/low
//       around the honest median => median unchanged. A bias in the
//       median calculation (off-by-one, integer rounding) would break
//       this symmetry.
//
//   MR4 ByzantineDuplicateIgnored: A byzantine validator submitting
//       duplicate price feeds for the same asset pair must have ALL
//       its feeds dropped — its vote count must not inflate to help
//       the byzantine side cross a threshold.
//
//   MR5 NonResponsiveTolerance: Silencing up to f honest validators
//       (leaving ≥ 2f+1 votes) preserves the aggregation output when
//       all remaining validators agree.
//
//   MR6 ByzantineExtremeInjectionFilteredWithDeviation: With
//       MaxPriceDeviation set, byzantine votes at extreme prices get
//       filtered and the median equals the honest consensus.
//
//   MR7 ByzantineOrderingInvariance: Permuting the order in which
//       byzantine votes are submitted produces the same aggregated
//       median (vote submission order must not leak into state).
//
//   MR8 F1ScalingInvariance: The MRs hold across byzantine-tolerance
//       scales f=1 (n=4), f=2 (n=7), f=3 (n=10), and f=4 (n=13).

// byzantineTestTime is deterministic for reproducible vote aging.
var byzantineTestTime = testTime

// submitVote stores a validator vote with a single price feed. Helper
// to keep per-MR test bodies focused on the scenario rather than the
// ValidatorVote construction boilerplate.
func submitVote(t *testing.T, k *Keeper, ctx sdk.Context, validator, assetPair, price string) {
	t.Helper()
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: validator,
		PriceFeeds:       []*types.PriceFeed{{AssetPair: assetPair, Price: price}},
		BlockHeight:      ctx.BlockHeight(),
		Timestamp:        byzantineTestTime,
	}))
}

// byzantineAssetPair is the single pair used across all byzantine MR
// tests. Keeping it constant means any drift in the aggregation result
// comes from byzantine-set variation, not from pair-specific logic.
const byzantineAssetPair = "BTC/USD"

// setupByzantineKeeper configures an oracle keeper with a deterministic
// set of params tuned for byzantine metamorphic testing.
func setupByzantineKeeper(t *testing.T, maxDeviation string) (*Keeper, sdk.Context) {
	t.Helper()
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: maxDeviation,
		AssetPairs:        []string{byzantineAssetPair},
		MaxVoteAge:        300,
	}))
	return k, ctx
}

// runAggregation triggers AggregateVotes and returns the aggregated
// price for the byzantine test pair.
func runAggregation(t *testing.T, k *Keeper, ctx sdk.Context) *types.AggregatedPrice {
	t.Helper()
	require.NoError(t, k.AggregateVotes(ctx))
	agg, err := k.GetAggregatedPrice(ctx, byzantineAssetPair)
	require.NoError(t, err)
	require.NotNil(t, agg)
	return agg
}

// byzantineConfigs enumerates n = 3f+1 configurations for scale tests.
var byzantineConfigs = []struct {
	name string
	f    int // byzantine tolerance; n = 3f+1
}{
	{"f1_n4", 1},
	{"f2_n7", 2},
	{"f3_n10", 3},
	{"f4_n13", 4},
}

// TestByzantineMR_HonestSupermajorityDominates (MR2) is the core BFT
// property: 2f+1 honest votes at price P plus f byzantine votes at any
// price must produce aggregated median = P. Failure means the
// byzantine minority can shift the result — catastrophic BFT break.
func TestByzantineMR_HonestSupermajorityDominates(t *testing.T) {
	for _, cfg := range byzantineConfigs {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			honestCount := 2*cfg.f + 1
			byzantineCount := cfg.f

			// Attack variants: byzantine prices that try to pull the median.
			attacks := []string{
				"1",              // min
				"999999999",      // max extreme
				"0",              // zero
				"50",             // below honest
				"500",            // well above honest
				"99999999999999", // huge
			}

			for _, attackPrice := range attacks {
				attackPrice := attackPrice
				t.Run("attack_price_"+attackPrice, func(t *testing.T) {
					k, ctx := setupByzantineKeeper(t, "0") // no outlier filter

					// 2f+1 honest validators all vote 100.
					for i := 0; i < honestCount; i++ {
						submitVote(t, k, ctx,
							fmt.Sprintf("val-honest-%d", i),
							byzantineAssetPair, "100")
					}
					// f byzantine validators vote the attack price.
					for i := 0; i < byzantineCount; i++ {
						submitVote(t, k, ctx,
							fmt.Sprintf("val-byzantine-%d", i),
							byzantineAssetPair, attackPrice)
					}

					agg := runAggregation(t, k, ctx)
					require.Equal(t, "100.000000000000000000", agg.MedianPrice,
						"f=%d, attack=%s: byzantine minority shifted the median",
						cfg.f, attackPrice)
					// NumValidators is at least the honest count; byzantines
					// voting 0 get rejected at ingress (non-positive prices
					// are filtered by groupVotesByAssetWithDrops), so the
					// count may equal honestCount when attackPrice=="0".
					require.GreaterOrEqual(t, agg.NumValidators, int32(honestCount),
						"f=%d, attack=%s: honest votes dropped by aggregation",
						cfg.f, attackPrice)
				})
			}
		})
	}
}

// TestByzantineMR_RangeBounded (MR1) proves that with 2f+1 honest
// votes in [L,H] and up to f byzantine anywhere, the median stays
// in [L, H]. The honest supermajority owns the median position.
func TestByzantineMR_RangeBounded(t *testing.T) {
	for _, cfg := range byzantineConfigs {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			honestCount := 2*cfg.f + 1
			byzantineCount := cfg.f
			rng := rand.New(rand.NewSource(int64(cfg.f)))

			k, ctx := setupByzantineKeeper(t, "0")

			// 2f+1 honest votes in [100, 110], deterministic.
			honestMin, honestMax := int64(100), int64(110)
			for i := 0; i < honestCount; i++ {
				price := honestMin + rng.Int63n(honestMax-honestMin+1)
				submitVote(t, k, ctx,
					fmt.Sprintf("val-honest-%d", i),
					byzantineAssetPair, fmt.Sprintf("%d", price))
			}

			// f byzantine votes at extreme prices.
			byzantinePrices := []string{"1", "999999999", "500"}
			for i := 0; i < byzantineCount; i++ {
				submitVote(t, k, ctx,
					fmt.Sprintf("val-byzantine-%d", i),
					byzantineAssetPair, byzantinePrices[i%len(byzantinePrices)])
			}

			agg := runAggregation(t, k, ctx)

			// Parse median and verify bounds.
			requireMedianInRange(t, agg.MedianPrice, honestMin, honestMax,
				"f=%d: byzantine minority pulled median outside honest range [%d, %d]",
				cfg.f, honestMin, honestMax)
		})
	}
}

// TestByzantineMR_SymmetricByzantineCancels (MR3) proves that f
// byzantine votes split symmetrically around the honest median leave
// the median unchanged. Catches asymmetric bias (off-by-one, rounding
// skew) that would appear only under balanced byzantine pressure.
func TestByzantineMR_SymmetricByzantineCancels(t *testing.T) {
	for _, cfg := range byzantineConfigs {
		if cfg.f < 2 {
			continue // need at least 2 byzantines to split symmetrically
		}
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			honestCount := 2*cfg.f + 1
			byzantineCount := cfg.f
			halfLow := byzantineCount / 2
			halfHigh := byzantineCount - halfLow

			// Reference run: only honest votes.
			reference := func() string {
				k, ctx := setupByzantineKeeper(t, "0")
				for i := 0; i < honestCount; i++ {
					submitVote(t, k, ctx,
						fmt.Sprintf("val-honest-%d", i),
						byzantineAssetPair, "100")
				}
				// Pad with extras to match total count of the symmetric case
				// (honestCount + byzantineCount). Using honest votes keeps
				// the honest median at 100.
				for i := 0; i < byzantineCount; i++ {
					submitVote(t, k, ctx,
						fmt.Sprintf("val-extra-%d", i),
						byzantineAssetPair, "100")
				}
				return runAggregation(t, k, ctx).MedianPrice
			}()

			// Attack run: same honest set + f byzantine split symmetrically.
			attack := func() string {
				k, ctx := setupByzantineKeeper(t, "0")
				for i := 0; i < honestCount; i++ {
					submitVote(t, k, ctx,
						fmt.Sprintf("val-honest-%d", i),
						byzantineAssetPair, "100")
				}
				for i := 0; i < halfLow; i++ {
					submitVote(t, k, ctx,
						fmt.Sprintf("val-byzantine-low-%d", i),
						byzantineAssetPair, "50")
				}
				for i := 0; i < halfHigh; i++ {
					submitVote(t, k, ctx,
						fmt.Sprintf("val-byzantine-high-%d", i),
						byzantineAssetPair, "150")
				}
				return runAggregation(t, k, ctx).MedianPrice
			}()

			require.Equal(t, reference, attack,
				"f=%d: symmetric byzantine attack (%d low + %d high) shifted median from %s to %s",
				cfg.f, halfLow, halfHigh, reference, attack)
		})
	}
}

// TestByzantineMR_DuplicateFeedIgnored (MR4) proves that a byzantine
// validator submitting duplicate feeds for the same asset pair has ALL
// its feeds dropped. If duplicates were silently counted, a byzantine
// validator could inflate its apparent vote weight past the 2f+1
// threshold.
func TestByzantineMR_DuplicateFeedIgnored(t *testing.T) {
	for _, cfg := range byzantineConfigs {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			honestCount := 2*cfg.f + 1
			byzantineCount := cfg.f

			k, ctx := setupByzantineKeeper(t, "0")

			// Honest validators vote the consensus price.
			for i := 0; i < honestCount; i++ {
				submitVote(t, k, ctx,
					fmt.Sprintf("val-honest-%d", i),
					byzantineAssetPair, "100")
			}

			// Byzantine validators submit 3 duplicate feeds each at attack price.
			for i := 0; i < byzantineCount; i++ {
				require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
					ValidatorAddress: fmt.Sprintf("val-byzantine-%d", i),
					PriceFeeds: []*types.PriceFeed{
						{AssetPair: byzantineAssetPair, Price: "1000"},
						{AssetPair: byzantineAssetPair, Price: "1000"},
						{AssetPair: byzantineAssetPair, Price: "1000"},
					},
					BlockHeight: ctx.BlockHeight(),
					Timestamp:   byzantineTestTime,
				}))
			}

			agg := runAggregation(t, k, ctx)
			require.Equal(t, "100.000000000000000000", agg.MedianPrice,
				"f=%d: duplicate-feed attack shifted median", cfg.f)
			// Only honest validators counted — byzantine duplicates all dropped.
			require.Equal(t, int32(honestCount), agg.NumValidators,
				"f=%d: byzantine duplicate feeds inflated validator count "+
					"to %d (expected %d honest only)",
				cfg.f, agg.NumValidators, honestCount)
		})
	}
}

// TestByzantineMR_NonResponsiveTolerance (MR5) proves that silencing
// up to f honest validators — while leaving at least 2f+1 votes —
// preserves the median. This models the "honest but offline" case,
// which counts the same as byzantine under BFT assumptions.
func TestByzantineMR_NonResponsiveTolerance(t *testing.T) {
	for _, cfg := range byzantineConfigs {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			// Full honest run: 3f+1 votes all at 100.
			fullMedian := func() string {
				k, ctx := setupByzantineKeeper(t, "0")
				for i := 0; i < 3*cfg.f+1; i++ {
					submitVote(t, k, ctx,
						fmt.Sprintf("val-%d", i),
						byzantineAssetPair, "100")
				}
				return runAggregation(t, k, ctx).MedianPrice
			}()

			// Partial run: f validators silent, 2f+1 remain.
			partialMedian := func() string {
				k, ctx := setupByzantineKeeper(t, "0")
				for i := 0; i < 2*cfg.f+1; i++ {
					submitVote(t, k, ctx,
						fmt.Sprintf("val-%d", i),
						byzantineAssetPair, "100")
				}
				return runAggregation(t, k, ctx).MedianPrice
			}()

			require.Equal(t, fullMedian, partialMedian,
				"f=%d: silencing f validators shifted median from %s to %s",
				cfg.f, fullMedian, partialMedian)
		})
	}
}

// TestByzantineMR_ExtremeInjectionFilteredWithDeviation (MR6) proves
// that with MaxPriceDeviation set, byzantine extreme votes are filtered
// as outliers — their prices never touch the aggregated median.
func TestByzantineMR_ExtremeInjectionFilteredWithDeviation(t *testing.T) {
	for _, cfg := range byzantineConfigs {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			honestCount := 2*cfg.f + 1
			byzantineCount := cfg.f

			// 5% max deviation — byzantine extreme prices will be filtered.
			k, ctx := setupByzantineKeeper(t, "0.05")

			// Honest votes in tight cluster [98..102].
			honestPrices := []string{"98", "99", "100", "101", "102",
				"100", "100", "100", "100", "100"}
			for i := 0; i < honestCount; i++ {
				submitVote(t, k, ctx,
					fmt.Sprintf("val-honest-%d", i),
					byzantineAssetPair, honestPrices[i%len(honestPrices)])
			}
			// Byzantine votes at extreme prices — should be filtered out.
			extremes := []string{"1", "999999999"}
			for i := 0; i < byzantineCount; i++ {
				submitVote(t, k, ctx,
					fmt.Sprintf("val-byzantine-%d", i),
					byzantineAssetPair, extremes[i%len(extremes)])
			}

			agg := runAggregation(t, k, ctx)

			// After outlier filter, only honest votes remain → median in
			// the honest cluster [98, 102].
			requireMedianInRange(t, agg.MedianPrice, 98, 102,
				"f=%d: outlier filter failed to reject byzantine extremes; "+
					"median=%s not in honest range [98, 102]",
				cfg.f, agg.MedianPrice)
			// Byzantine votes should be filtered out; count = honest count.
			require.Equal(t, int32(honestCount), agg.NumValidators,
				"f=%d: outlier filter did not remove byzantine extremes "+
					"(got %d validators, expected %d honest only)",
				cfg.f, agg.NumValidators, honestCount)
		})
	}
}

// TestByzantineMR_SubmissionOrderInvariant (MR7) proves that permuting
// the order in which byzantine votes are submitted produces the same
// aggregated median. Submission-order sensitivity would cause chain
// forks under different validator proposer rotation.
func TestByzantineMR_SubmissionOrderInvariant(t *testing.T) {
	for _, cfg := range byzantineConfigs {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			n := 3*cfg.f + 1
			honestCount := 2*cfg.f + 1
			byzantineCount := cfg.f

			// Build vote sets: honest always vote 100, byzantine vote 500.
			voteSequence := make([]struct{ validator, price string }, 0, n)
			for i := 0; i < honestCount; i++ {
				voteSequence = append(voteSequence, struct{ validator, price string }{
					fmt.Sprintf("val-honest-%d", i), "100",
				})
			}
			for i := 0; i < byzantineCount; i++ {
				voteSequence = append(voteSequence, struct{ validator, price string }{
					fmt.Sprintf("val-byzantine-%d", i), "500",
				})
			}

			// Canonical order.
			canonicalMedian := func() string {
				k, ctx := setupByzantineKeeper(t, "0")
				for _, v := range voteSequence {
					submitVote(t, k, ctx, v.validator, byzantineAssetPair, v.price)
				}
				return runAggregation(t, k, ctx).MedianPrice
			}()

			// Shuffle 5 times; every permutation must yield the same median.
			for seed := int64(1); seed <= 5; seed++ {
				seed := seed
				t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
					shuffled := make([]struct{ validator, price string }, len(voteSequence))
					copy(shuffled, voteSequence)
					rng := rand.New(rand.NewSource(seed + int64(cfg.f)*100))
					rng.Shuffle(len(shuffled), func(i, j int) {
						shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
					})

					k, ctx := setupByzantineKeeper(t, "0")
					for _, v := range shuffled {
						submitVote(t, k, ctx, v.validator, byzantineAssetPair, v.price)
					}
					got := runAggregation(t, k, ctx).MedianPrice

					require.Equal(t, canonicalMedian, got,
						"f=%d seed=%d: submission order changed median from %s to %s",
						cfg.f, seed, canonicalMedian, got)
				})
			}
		})
	}
}

// TestByzantineMR_OverThresholdBreaksConsensus validates the NEGATIVE
// property: with f+1 byzantine (one more than tolerance), the BFT
// guarantee does NOT hold. This test asserts that the aggregation
// CAN be influenced when byzantine exceeds tolerance — confirming
// the bound is tight and the previous MRs aren't trivially always-true.
func TestByzantineMR_OverThresholdBreaksConsensus(t *testing.T) {
	// n = 4, f = 1, so the break threshold is 2 byzantine.
	// With 2 honest at 100 and 2 byzantine at 500, sorted is
	// [100, 100, 500, 500], median = (100+500)/2 = 300, NOT 100.
	// This test documents and locks in that behavior — it shows the
	// MR suite above isn't vacuously true and that the BFT boundary
	// is exactly at f.
	t.Parallel()

	k, ctx := setupByzantineKeeper(t, "0")
	submitVote(t, k, ctx, "val-honest-0", byzantineAssetPair, "100")
	submitVote(t, k, ctx, "val-honest-1", byzantineAssetPair, "100")
	submitVote(t, k, ctx, "val-byzantine-0", byzantineAssetPair, "500")
	submitVote(t, k, ctx, "val-byzantine-1", byzantineAssetPair, "500")

	agg := runAggregation(t, k, ctx)
	// 2 byzantine out of 4 (= f+1 for n=4,f=1) CAN shift the median.
	// If ever this starts equaling "100", the honest supermajority is
	// being held together by some hidden mechanism and the MR bounds
	// above may be miscalibrated.
	require.NotEqual(t, "100.000000000000000000", agg.MedianPrice,
		"over-threshold byzantine attack unexpectedly preserved honest consensus — "+
			"review whether the MR suite is detecting false positives")
	require.Equal(t, "300.000000000000000000", agg.MedianPrice,
		"expected structural median of [100,100,500,500] = 300")
}

// requireMedianInRange parses the aggregated median price string and
// asserts it lies in [lo, hi]. Used for soft-bound MRs (RangeBounded,
// ExtremeInjectionFilteredWithDeviation).
func requireMedianInRange(t *testing.T, medianStr string, lo, hi int64, msgFormat string, args ...interface{}) {
	t.Helper()
	// Strip trailing .000000000000000000 and convert to int64.
	// Median prices from oracle always have 18 fractional digits.
	const suffix = ".000000000000000000"
	intPart := medianStr
	if len(intPart) > len(suffix) && intPart[len(intPart)-len(suffix):] == suffix {
		intPart = intPart[:len(intPart)-len(suffix)]
	}
	var val int64
	_, err := fmt.Sscanf(intPart, "%d", &val)
	require.NoErrorf(t, err, "could not parse median %q as integer", medianStr)

	require.GreaterOrEqualf(t, val, lo,
		msgFormat+" (median=%d below lo=%d)",
		append(args, val, lo)...)
	require.LessOrEqualf(t, val, hi,
		msgFormat+" (median=%d above hi=%d)",
		append(args, val, hi)...)
}

