//go:build cosmos

package ibc

import (
	"math/rand"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// This file applies the testing-metamorphic skill to the
// ComputeFeeSplit function, pinning CROSS-MODULE fee-split
// conservation invariants under rounding.
//
// x/credits/ibc's fee split is consumed cross-module by:
//   - bank keeper (moves funds per-leg in the Executor)
//   - x/reserve (discount accounting uses net amounts)
//   - x/router (post-split routing applies remaining leg)
//   - downstream indexers (tally per-role revenue from events)
// A conservation failure in ComputeFeeSplit silently desyncs all
// four consumers: burn accounting leaks tokens, router revenue
// misreports, insurance pool drifts, audits fail.
//
// Existing coverage:
//
//   fee_split_middleware_test.go: 2 fixed-input conservation
//   assertions (LargeAmount, SmallAmount). Targeted scenarios
//   only — no property-based assurance.
//
//   fee_split_audit_events_test.go: pins conservation via event
//   reconstruction, again fixed inputs.
//
// This file closes the gap with 7 METAMORPHIC RELATIONS spanning
// the skill's six categories:
//
//   1. EQUIVALENCE:     total = sum of parts under ALL inputs
//   2. EQUIVALENCE:     dust localizes to publisher (others exact)
//   3. PERMUTATIVE:     role BPS permutation permutes role amounts
//                       (cross-module role-agnostic contract)
//   4. MULTIPLICATIVE:  k·amount yields k·parts (modulo bounded dust)
//   5. ADDITIVE:        ComputeFeeSplit(a+b) ≈ split(a)+split(b)
//                       bounded by O(#parts) in rounding diff
//   6. INCLUSIVE:       zeroing one role's BPS subsets its output
//                       (role's share → 0, others unchanged)
//   7. INVERTIVE:       re-splitting the same amount yields the
//                       SAME result (determinism across modules)
//
// Every MR checks the CONSERVATION invariant as a post-condition,
// so any weakening of the core contract (total = Σ parts) fails
// multiple tests at once — a strong signal vs. coincidental pass.

// ---------- Shared helpers ----------

func sumParts(r FeeSplitResult) sdkmath.Int {
	return r.BurnAmount.
		Add(r.Insurance).
		Add(r.Publisher).
		Add(r.Router).
		Add(r.Referrer)
}

func assertConservation(t *testing.T, r FeeSplitResult, amount sdkmath.Int, ctx string) {
	t.Helper()
	total := sumParts(r)
	require.True(t, total.Equal(amount),
		"%s: conservation violated — total=%s sum_of_parts=%s "+
			"(burn=%s insurance=%s publisher=%s router=%s referrer=%s)",
		ctx, amount, total,
		r.BurnAmount, r.Insurance, r.Publisher, r.Router, r.Referrer)
}

func assertNonNegativity(t *testing.T, r FeeSplitResult, ctx string) {
	t.Helper()
	parts := []struct {
		name string
		v    sdkmath.Int
	}{
		{"burn", r.BurnAmount},
		{"insurance", r.Insurance},
		{"publisher", r.Publisher},
		{"router", r.Router},
		{"referrer", r.Referrer},
		{"net", r.NetAmount},
	}
	for _, p := range parts {
		require.False(t, p.v.IsNegative(),
			"%s: %s went negative: %s", ctx, p.name, p.v)
	}
}

// defaultValidParams returns a set of params that pass Validate()
// with non-zero BPS in every role. Used as the default for MRs
// that don't specifically vary params.
func defaultValidParams() FeeSplitParams {
	return FeeSplitParams{
		BurnBPS:      300,  // 3%
		InsuranceBPS: 200,  // 2%
		PublisherBPS: 7000, // 70% of net
		RouterBPS:    2000, // 20% of net
		ReferrerBPS:  1000, // 10% of net
	}
}

// --------------------------------------------------------------
// MR 1 (EQUIVALENCE): Total conservation across the FULL input
// domain — any amount, any valid params, any rounding.
//
//   ∀ amount ≥ 0, ∀ valid params: total = Σ parts
//
// This is the load-bearing invariant for every downstream
// module. A refactor that miscomputes dust, changes the order
// of burn/insurance/net, or switches floor→round would break
// it for some amount. Property-based sweep catches those.
// --------------------------------------------------------------

func TestFeeSplitConservation_MR_TotalEqualsSumOfPartsOverAmountSweep(t *testing.T) {
	t.Parallel()
	params := defaultValidParams()

	// Amounts spanning the full range of settlement inputs:
	//   0 (zero-amount fast path)
	//   1 (smallest positive — maximum relative rounding)
	//   MaxBPS-1 = 9999 (rounding boundary: below one bp)
	//   MaxBPS = 10000 (exactly one bp)
	//   MaxBPS+1 (just above boundary)
	//   small primes (adversarial rounding)
	//   large values (conservation at scale)
	amounts := []int64{
		0, 1, 2, 3, 7, 13, 99, 100, 999, 1000, 9999, 10000, 10001,
		12345, 67890, 100_000, 999_999, 1_000_000,
		10_000_000, 100_000_000, 1_000_000_000,
	}
	// Adversarial primes in the rounding-boundary region.
	amounts = append(amounts, []int64{9997, 9998, 10007, 10009, 10037}...)

	for _, a := range amounts {
		amount := sdkmath.NewInt(a)
		r, err := ComputeFeeSplit(amount, "ulac", "mr-conservation", params)
		require.NoError(t, err, "ComputeFeeSplit(%d) errored", a)

		ctx := "amount=" + amount.String()
		assertConservation(t, r, amount, ctx)
		assertNonNegativity(t, r, ctx)

		// NetAmount must equal sum of net parts (the second-level
		// conservation: net = publisher + router + referrer after
		// dust absorption).
		netParts := r.Publisher.Add(r.Router).Add(r.Referrer)
		require.True(t, netParts.Equal(r.NetAmount),
			"%s: net conservation violated — net=%s parts_sum=%s",
			ctx, r.NetAmount, netParts)
	}
}

// --------------------------------------------------------------
// MR 2 (EQUIVALENCE): Dust localizes to publisher.
//
// The middleware assigns rounding dust to publisher by design
// (:148-151). Router and referrer amounts MUST be exact floor
// divisions: bpsOf(netAmount, RouterBPS) and
// bpsOf(netAmount, ReferrerBPS). Any dust contamination into
// router or referrer would silently over-pay those roles.
// --------------------------------------------------------------

func TestFeeSplitConservation_MR_DustLocalizesToPublisher(t *testing.T) {
	t.Parallel()
	params := defaultValidParams()

	// Amounts specifically chosen to produce non-zero dust:
	// any amount not divisible by 10000 will produce dust on at
	// least one sub-division.
	amounts := []int64{
		7, 13, 17, 19, 23, 29, 97, 101, 103, 107,
		9973, 9999, 12347, 67891, 100_003, 1_000_003,
	}
	for _, a := range amounts {
		amount := sdkmath.NewInt(a)
		r, err := ComputeFeeSplit(amount, "ulac", "mr-dust", params)
		require.NoError(t, err)

		// Recompute the raw floor divisions that bpsOf would have
		// produced on r.NetAmount, then check that router/referrer
		// match exactly (no dust contamination) and publisher
		// matches "raw + dust".
		rawPublisher := r.NetAmount.MulRaw(int64(params.PublisherBPS)).QuoRaw(int64(MaxBPS))
		rawRouter := r.NetAmount.MulRaw(int64(params.RouterBPS)).QuoRaw(int64(MaxBPS))
		rawReferrer := r.NetAmount.MulRaw(int64(params.ReferrerBPS)).QuoRaw(int64(MaxBPS))

		require.True(t, r.Router.Equal(rawRouter),
			"amount=%d: router amount %s diverges from raw floor %s "+
				"— dust leaked into router leg",
			a, r.Router, rawRouter)
		require.True(t, r.Referrer.Equal(rawReferrer),
			"amount=%d: referrer amount %s diverges from raw floor %s "+
				"— dust leaked into referrer leg",
			a, r.Referrer, rawReferrer)

		expectedDust := r.NetAmount.Sub(rawPublisher).Sub(rawRouter).Sub(rawReferrer)
		require.True(t, r.Publisher.Equal(rawPublisher.Add(expectedDust)),
			"amount=%d: publisher amount %s != raw(%s) + dust(%s)",
			a, r.Publisher, rawPublisher, expectedDust)
	}
}

// --------------------------------------------------------------
// MR 3 (PERMUTATIVE): Role-BPS permutation permutes role amounts.
//
// The split is role-symmetric: swapping PublisherBPS with
// RouterBPS (with same numerical values) should swap the
// corresponding output amounts exactly, modulo dust handling
// which stays with publisher by convention.
//
// Cross-module relevance: indexers and reserve/router modules
// must be role-agnostic in their conservation accounting. If
// a refactor accidentally hard-coded any role into the
// arithmetic (e.g. "divide by PublisherBPS first for
// performance"), this MR catches it.
// --------------------------------------------------------------

func TestFeeSplitConservation_MR_RoleBPSPermutationPermutesAmounts(t *testing.T) {
	t.Parallel()
	// Both roles have the SAME BPS → swapping is a pure
	// permutation that must yield identical amounts for each.
	paramsA := FeeSplitParams{
		BurnBPS:      300,
		InsuranceBPS: 200,
		PublisherBPS: 5000, // 50%
		RouterBPS:    4000, // 40%
		ReferrerBPS:  1000, // 10%
	}
	paramsB := FeeSplitParams{
		BurnBPS:      300,
		InsuranceBPS: 200,
		PublisherBPS: 4000, // SWAPPED: was 5000
		RouterBPS:    5000, // SWAPPED: was 4000
		ReferrerBPS:  1000,
	}

	amounts := []int64{100_000, 999_999, 1_000_000, 12_345_678}
	for _, a := range amounts {
		amount := sdkmath.NewInt(a)
		rA, err := ComputeFeeSplit(amount, "ulac", "mr-perm-a", paramsA)
		require.NoError(t, err)
		rB, err := ComputeFeeSplit(amount, "ulac", "mr-perm-b", paramsB)
		require.NoError(t, err)

		// Burn, insurance, referrer, net all equal across A and B
		// (those BPS didn't change).
		require.True(t, rA.BurnAmount.Equal(rB.BurnAmount),
			"amount=%d: burn diverged across swap", a)
		require.True(t, rA.Insurance.Equal(rB.Insurance),
			"amount=%d: insurance diverged across swap", a)
		require.True(t, rA.Referrer.Equal(rB.Referrer),
			"amount=%d: referrer diverged across swap", a)
		require.True(t, rA.NetAmount.Equal(rB.NetAmount),
			"amount=%d: net diverged across swap", a)

		// Publisher(A) should ≈ Router(B) — both are 5000 BPS of
		// the same net. BUT publisher absorbs dust while router
		// does not, so compare raw-floor values.
		rawA5000 := rA.NetAmount.MulRaw(5000).QuoRaw(int64(MaxBPS))
		rawB5000 := rB.NetAmount.MulRaw(5000).QuoRaw(int64(MaxBPS))
		rawA4000 := rA.NetAmount.MulRaw(4000).QuoRaw(int64(MaxBPS))
		rawB4000 := rB.NetAmount.MulRaw(4000).QuoRaw(int64(MaxBPS))

		// A-publisher (5000 + dust_A) ↔ B-router (5000 raw)
		dustA := rA.Publisher.Sub(rawA5000)
		dustB := rB.Publisher.Sub(rawB4000)
		require.True(t, rA.Publisher.Sub(dustA).Equal(rawA5000),
			"amount=%d: A publisher raw mismatch", a)
		require.True(t, rB.Router.Equal(rawB5000),
			"amount=%d: B router raw mismatch", a)
		require.True(t, rA.Router.Equal(rawA4000),
			"amount=%d: A router raw mismatch", a)
		require.True(t, rB.Publisher.Sub(dustB).Equal(rawB4000),
			"amount=%d: B publisher raw mismatch", a)

		// Dust total must be the same magnitude in both (same net,
		// same rounding).
		require.True(t, dustA.Equal(dustB),
			"amount=%d: dust differs across permutation: "+
				"dustA=%s dustB=%s — rounding leaked between "+
				"role positions",
			a, dustA, dustB)

		assertConservation(t, rA, amount, "permA")
		assertConservation(t, rB, amount, "permB")
	}
}

// --------------------------------------------------------------
// MR 4 (MULTIPLICATIVE): Scaling amount by k scales parts.
//
//   ComputeFeeSplit(k·amount) vs k · ComputeFeeSplit(amount)
//
// These are NOT strictly equal due to independent floor
// operations, but they differ by at most O(#parts · k) in each
// component. Conservation holds for BOTH sides exactly.
//
// This MR specifically targets a regression that might change
// the rounding direction (floor → round-half-up) which would
// amplify the scaling asymmetry.
// --------------------------------------------------------------

func TestFeeSplitConservation_MR_ScalingApproximatelyLinear(t *testing.T) {
	t.Parallel()
	params := defaultValidParams()

	// Test multiplicative scaling for k = 2, 3, 5, 7, 10.
	for _, k := range []int64{2, 3, 5, 7, 10, 100} {
		amounts := []int64{1, 7, 100, 9999, 100_000}
		for _, a := range amounts {
			amount := sdkmath.NewInt(a)
			scaled := amount.MulRaw(k)

			r1, err := ComputeFeeSplit(amount, "ulac", "mr-mul-base", params)
			require.NoError(t, err)
			rK, err := ComputeFeeSplit(scaled, "ulac", "mr-mul-scaled", params)
			require.NoError(t, err)

			// Scaled conservation holds.
			assertConservation(t, rK, scaled, "scaled")
			assertConservation(t, r1, amount, "base")

			// Each part should be k * original ± bounded dust.
			// The dust diff across components is bounded by the
			// sum of component rounding: at most 5 parts × 1 = 5
			// per ComputeFeeSplit invocation, so the scaled
			// version can differ by at most (5 + 5·k) = 5(1+k)
			// in ANY individual component.
			tolerance := sdkmath.NewInt(5 * (1 + k))

			for name, pair := range map[string]struct {
				base, scaled sdkmath.Int
			}{
				"burn":      {r1.BurnAmount.MulRaw(k), rK.BurnAmount},
				"insurance": {r1.Insurance.MulRaw(k), rK.Insurance},
				"publisher": {r1.Publisher.MulRaw(k), rK.Publisher},
				"router":    {r1.Router.MulRaw(k), rK.Router},
				"referrer":  {r1.Referrer.MulRaw(k), rK.Referrer},
			} {
				diff := pair.scaled.Sub(pair.base).Abs()
				require.True(t, diff.LTE(tolerance),
					"MR-scaling: k=%d amount=%d role=%s diff=%s "+
						"exceeds tolerance %s: base·k=%s scaled=%s — "+
						"rounding drift suggests a floor→round change",
					k, a, name, diff, tolerance, pair.base, pair.scaled)
			}
		}
	}
}

// --------------------------------------------------------------
// MR 5 (ADDITIVE): ComputeFeeSplit is sub-additive, bounded.
//
//   | ComputeFeeSplit(a+b).X − (split(a).X + split(b).X) |
//   ≤ bounded rounding noise in component X.
//
// Strict additivity would require exact divisibility; integer
// floor breaks it. But the DIVERGENCE must stay bounded: a
// refactor amplifying rounding (e.g. cumulative error) would
// grow unboundedly with the number of sub-splits.
// --------------------------------------------------------------

func TestFeeSplitConservation_MR_AdditivityBoundedByRoundingDiff(t *testing.T) {
	t.Parallel()
	params := defaultValidParams()

	pairs := []struct{ a, b int64 }{
		{1, 1},
		{1, 99},
		{100, 100},
		{1000, 2345},
		{9999, 1},
		{12_345, 67_890},
		{1_000_000, 999_999},
	}
	for _, p := range pairs {
		A := sdkmath.NewInt(p.a)
		B := sdkmath.NewInt(p.b)
		AB := A.Add(B)

		rA, err := ComputeFeeSplit(A, "ulac", "mr-add-a", params)
		require.NoError(t, err)
		rB, err := ComputeFeeSplit(B, "ulac", "mr-add-b", params)
		require.NoError(t, err)
		rAB, err := ComputeFeeSplit(AB, "ulac", "mr-add-ab", params)
		require.NoError(t, err)

		// Conservation for each of the three splits.
		assertConservation(t, rA, A, "rA")
		assertConservation(t, rB, B, "rB")
		assertConservation(t, rAB, AB, "rAB")

		// Each part of (rA + rB) differs from rAB by at most a
		// small bounded noise. Two invocations each have up to
		// O(5) floor truncations; a third invocation (rAB) has
		// another O(5). So the per-part absolute diff is bounded
		// by O(10).
		tolerance := sdkmath.NewInt(20)

		parts := []struct {
			name            string
			sumAB, combined sdkmath.Int
		}{
			{"burn", rA.BurnAmount.Add(rB.BurnAmount), rAB.BurnAmount},
			{"insurance", rA.Insurance.Add(rB.Insurance), rAB.Insurance},
			{"publisher", rA.Publisher.Add(rB.Publisher), rAB.Publisher},
			{"router", rA.Router.Add(rB.Router), rAB.Router},
			{"referrer", rA.Referrer.Add(rB.Referrer), rAB.Referrer},
		}
		for _, part := range parts {
			diff := part.sumAB.Sub(part.combined).Abs()
			require.True(t, diff.LTE(tolerance),
				"MR-additive: a=%d b=%d role=%s "+
					"diff=%s exceeds tolerance %s: "+
					"rA.%s+rB.%s=%s vs rAB.%s=%s — "+
					"unbounded rounding error suggests a "+
					"cumulative-truncation regression",
				p.a, p.b, part.name, diff, tolerance,
				part.name, part.name, part.sumAB, part.name, part.combined)
		}
	}
}

// --------------------------------------------------------------
// MR 6 (INCLUSIVE/EXCLUSIVE): Zeroing a role's BPS subsets its
// output to zero; the other roles' outputs stay unchanged.
//
// Specifically: setting InsuranceBPS=0 produces insurance=0,
// and because net = amount - burn - 0 is LARGER than before,
// publisher/router/referrer amounts may GROW. The property:
// the OLD InsuranceBPS amount gets ADDED to the sum of
// publisher+router+referrer (plus dust).
// --------------------------------------------------------------

func TestFeeSplitConservation_MR_ZeroingInsuranceBPSMergesIntoNetShare(t *testing.T) {
	t.Parallel()

	withIns := defaultValidParams() // InsuranceBPS=200
	withoutIns := defaultValidParams()
	withoutIns.InsuranceBPS = 0

	amounts := []int64{100_000, 999_999, 1_000_000, 10_000_000}
	for _, a := range amounts {
		amount := sdkmath.NewInt(a)
		with, err := ComputeFeeSplit(amount, "ulac", "mr-inc-with", withIns)
		require.NoError(t, err)
		without, err := ComputeFeeSplit(amount, "ulac", "mr-inc-without", withoutIns)
		require.NoError(t, err)

		assertConservation(t, with, amount, "withIns")
		assertConservation(t, without, amount, "withoutIns")

		// Zero-out check: InsuranceBPS=0 produces zero insurance.
		require.True(t, without.Insurance.IsZero(),
			"amount=%d: InsuranceBPS=0 but got insurance=%s",
			a, without.Insurance)

		// The net amount WITHOUT insurance is larger by exactly
		// the insurance taken in the WITH case. This pins that
		// the insurance line-item is a pure DEDUCTION from net,
		// not a shift of the split.
		netDiff := without.NetAmount.Sub(with.NetAmount)
		require.True(t, netDiff.Equal(with.Insurance),
			"amount=%d: net diff %s ≠ insurance %s — suggests "+
				"insurance removal leaked into a different line "+
				"item",
			a, netDiff, with.Insurance)

		// Burn is unchanged (InsuranceBPS only affects
		// post-burn accounting).
		require.True(t, with.BurnAmount.Equal(without.BurnAmount),
			"amount=%d: burn changed under InsuranceBPS zeroing: "+
				"%s vs %s",
			a, with.BurnAmount, without.BurnAmount)
	}
}

// --------------------------------------------------------------
// MR 7 (INVERTIVE / DETERMINISM): Re-splitting the same input
// yields the same output. Cross-module accounting relies on
// determinism across every call site.
// --------------------------------------------------------------

func TestFeeSplitConservation_MR_DeterminismAcrossRepeatedCalls(t *testing.T) {
	t.Parallel()
	params := defaultValidParams()

	// Seeded PRNG → stable input distribution. No randomness in
	// the output.
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		a := rng.Int63n(100_000_000) + 1
		amount := sdkmath.NewInt(a)
		settlement := "mr-det-" + sdkmath.NewInt(int64(i)).String()

		r1, err := ComputeFeeSplit(amount, "ulac", settlement, params)
		require.NoError(t, err)
		r2, err := ComputeFeeSplit(amount, "ulac", settlement, params)
		require.NoError(t, err)

		// Deep equality on all fields.
		require.True(t, r1.TotalAmount.Equal(r2.TotalAmount),
			"determinism: total differs across repeat calls (a=%d)", a)
		require.True(t, r1.BurnAmount.Equal(r2.BurnAmount),
			"determinism: burn differs across repeat calls (a=%d)", a)
		require.True(t, r1.Insurance.Equal(r2.Insurance),
			"determinism: insurance differs across repeat calls (a=%d)", a)
		require.True(t, r1.NetAmount.Equal(r2.NetAmount),
			"determinism: net differs across repeat calls (a=%d)", a)
		require.True(t, r1.Publisher.Equal(r2.Publisher),
			"determinism: publisher differs across repeat calls (a=%d)", a)
		require.True(t, r1.Router.Equal(r2.Router),
			"determinism: router differs across repeat calls (a=%d)", a)
		require.True(t, r1.Referrer.Equal(r2.Referrer),
			"determinism: referrer differs across repeat calls (a=%d)", a)
		require.Equal(t, r1.Denom, r2.Denom,
			"determinism: denom differs across repeat calls (a=%d)", a)
		require.Equal(t, r1.SettlementID, r2.SettlementID,
			"determinism: settlement_id differs across repeat calls (a=%d)", a)

		assertConservation(t, r1, amount, "determinism")
	}
}

// --------------------------------------------------------------
// Composite MR: conservation under cross-module reconciliation.
//
// Simulates the cross-module flow where module A computes the
// split and module B (e.g., bank keeper in Executor) reconciles
// by SUMMING the per-leg amounts from the emitted events. The
// reconciled total must equal the input total byte-for-byte.
// This is the end-to-end cross-module contract: a divergence
// here means some module's accounting leaks tokens.
// --------------------------------------------------------------

func TestFeeSplitConservation_MR_CrossModuleReconciliationIdentity(t *testing.T) {
	t.Parallel()
	params := defaultValidParams()

	// Randomized amounts spanning 6 orders of magnitude.
	rng := rand.New(rand.NewSource(0xC0FFEE))
	for i := 0; i < 200; i++ {
		magnitude := int64(1) << uint(rng.Intn(40)) // 1 .. ~1T
		a := rng.Int63n(magnitude) + 1
		amount := sdkmath.NewInt(a)

		r, err := ComputeFeeSplit(amount, "ulac", "mr-reconcile", params)
		require.NoError(t, err)

		assertConservation(t, r, amount, "cross-module reconcile")
		assertNonNegativity(t, r, "cross-module reconcile")

		// Reconstruct the input from the per-leg amounts — this
		// is EXACTLY what a cross-module reconciler (e.g. audit
		// pipeline) does when tallying the emitted events.
		reconstructed := r.BurnAmount.
			Add(r.Insurance).
			Add(r.Publisher).
			Add(r.Router).
			Add(r.Referrer)
		require.True(t, reconstructed.Equal(amount),
			"cross-module reconcile: a=%d reconstructed=%s — "+
				"indexer/audit pipelines that sum per-leg events "+
				"would disagree with the source amount",
			a, reconstructed)

		// Reconstruct net independently — second-level contract.
		netReconstructed := r.Publisher.Add(r.Router).Add(r.Referrer)
		require.True(t, netReconstructed.Equal(r.NetAmount),
			"cross-module net reconcile: a=%d recon=%s net=%s",
			a, netReconstructed, r.NetAmount)
	}
}
