
package types

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file adds METAMORPHIC tests for EvaluatePoolHealth at
// insurance_types.go:192-232. Existing tests cover the 4
// happy-path classifications (Healthy, Underfunded, Critical,
// Overfunded) plus zero-target edges and exponent-bomb degrade.
// What's NOT pinned:
//
//   - TIGHT BOUNDARIES at 0.5, 0.8, 1.2: the code uses
//     `LessThan` (strict) — exactly 0.5 falls OUT of OVERFUNDED
//     and INTO HEALTHY (not at the edge below). Scan-angle #1
//     (watchdog comparisons + tiebreak arms).
//
//   - SCALE INVARIANCE: doubling BOTH current and target keeps
//     the ratio constant → same PoolStatus. Pins the Div is
//     operating on the RATIO, not individual magnitudes.
//
//   - MONOTONIC PROGRESSION: as currentUtilization grows with
//     target fixed, status traverses OVERFUNDED → HEALTHY →
//     UNDERFUNDED → CRITICAL and never regresses.
//
//   - ZERO CURRENT BOUNDARY: current=0, target>0 → ratio=0 →
//     OVERFUNDED. Pins that a drained pool is classified
//     correctly (not HEALTHY as the zero-target branch might
//     suggest).
//
//   - DETERMINISM: same PoolState always yields same PoolStatus.
//
// Apply testing-metamorphic skill. Catches refactors that:
//   - flip strict vs non-strict at any band boundary
//   - introduce scale-dependent bias (absolute not ratio)
//   - reorder branches so ranges overlap
//   - add non-determinism (hidden random, map iteration order)

// Severity ordering for MONOTONIC PROGRESSION MR. The PoolStatus
// enum numeric values are NOT severity-ordered (HEALTHY=1,
// UNDERFUNDED=2, CRITICAL=3, OVERFUNDED=4 — OVERFUNDED is the
// highest enum value but LEAST severe). So we define severity
// explicitly. Refactors must not diverge this ordering from the
// band semantics: lower ratio = less severe.
func poolStatusSeverity(s PoolStatus) int {
	switch s {
	case PoolStatus_POOL_STATUS_OVERFUNDED:
		return 0 // least severe
	case PoolStatus_POOL_STATUS_HEALTHY:
		return 1
	case PoolStatus_POOL_STATUS_UNDERFUNDED:
		return 2
	case PoolStatus_POOL_STATUS_CRITICAL:
		return 3 // most severe
	default:
		return -1
	}
}

// pool builds a minimal PoolState with just the utilization
// fields set (everything else zero-valued). EvaluatePoolHealth
// only reads target/current, so this is sufficient.
func pool(current, target string) *PoolState {
	return &PoolState{
		CurrentUtilization: current,
		TargetUtilization:  target,
	}
}

// --------------------------------------------------------------
// MR — BOUNDARY: tight comparison semantics at each band edge
// --------------------------------------------------------------

// TestEvaluatePoolHealth_MR_BoundaryAt0_5IsHealthyNotOverfunded
// pins the STRICT-less-than comparison at :223. At ratio=0.5
// exactly, the `< 0.5` test FAILS, so the status is HEALTHY
// (the next band). A refactor to `<=` would flip this to
// OVERFUNDED — a silent change in how "just barely half of
// target" pools are classified by operator dashboards.
func TestEvaluatePoolHealth_MR_BoundaryAt05IsHealthyNotOverfunded(t *testing.T) {
	t.Parallel()
	// current=500, target=1000 → ratio=0.5 EXACTLY.
	status := EvaluatePoolHealth(pool("500", "1000"))
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, status,
		"MR boundary 0.5: strict LessThan(0.5) means ratio=0.5 "+
			"falls through to HEALTHY. Pins :223 strict comparison "+
			"— a refactor to `LessThanOrEqual` would classify this "+
			"as OVERFUNDED, silently shifting dashboard semantics "+
			"for every pool exactly half-filled.")
}

// TestEvaluatePoolHealth_MR_BoundaryJustBelow0_5IsOverfunded
// pins the mirror: just below the threshold IS overfunded.
func TestEvaluatePoolHealth_MR_BoundaryJustBelow05IsOverfunded(t *testing.T) {
	t.Parallel()
	// current=499, target=1000 → ratio=0.499 < 0.5.
	status := EvaluatePoolHealth(pool("499", "1000"))
	assert.Equal(t, PoolStatus_POOL_STATUS_OVERFUNDED, status,
		"ratio 0.499 < 0.5 → OVERFUNDED (the band LEFT of 0.5 "+
			"boundary)")
}

// TestEvaluatePoolHealth_MR_BoundaryAt0_8IsUnderfundedNotHealthy
// pins the strict comparison at :225. At ratio=0.8, `< 0.8`
// fails → status UNDERFUNDED (next band).
func TestEvaluatePoolHealth_MR_BoundaryAt08IsUnderfundedNotHealthy(t *testing.T) {
	t.Parallel()
	// current=800, target=1000 → ratio=0.8 EXACTLY.
	status := EvaluatePoolHealth(pool("800", "1000"))
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, status,
		"MR boundary 0.8: strict LessThan(0.8) means ratio=0.8 "+
			"falls through to UNDERFUNDED. Pins :225 — a refactor "+
			"to `<=` would classify '80%% full' pools as HEALTHY, "+
			"delaying operator rebalancing alerts.")
}

// TestEvaluatePoolHealth_MR_BoundaryJustBelow0_8IsHealthy mirror.
func TestEvaluatePoolHealth_MR_BoundaryJustBelow08IsHealthy(t *testing.T) {
	t.Parallel()
	status := EvaluatePoolHealth(pool("799", "1000"))
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, status)
}

// TestEvaluatePoolHealth_MR_BoundaryAt1_2IsCriticalNotUnderfunded
// pins the strict comparison at :227. At ratio=1.2, `< 1.2`
// fails → CRITICAL (default branch).
func TestEvaluatePoolHealth_MR_BoundaryAt12IsCriticalNotUnderfunded(t *testing.T) {
	t.Parallel()
	// current=1200, target=1000 → ratio=1.2 EXACTLY.
	status := EvaluatePoolHealth(pool("1200", "1000"))
	assert.Equal(t, PoolStatus_POOL_STATUS_CRITICAL, status,
		"MR boundary 1.2: strict LessThan(1.2) means ratio=1.2 "+
			"falls through to CRITICAL. Pins :227 — a refactor to "+
			"`<=` would classify '120%% full' pools as UNDERFUNDED, "+
			"masking critical overflow conditions from incident "+
			"response.")
}

// TestEvaluatePoolHealth_MR_BoundaryJustBelow1_2IsUnderfunded mirror.
func TestEvaluatePoolHealth_MR_BoundaryJustBelow12IsUnderfunded(t *testing.T) {
	t.Parallel()
	status := EvaluatePoolHealth(pool("1199", "1000"))
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, status)
}

// --------------------------------------------------------------
// MR — SCALE INVARIANCE
// --------------------------------------------------------------

// TestEvaluatePoolHealth_MR_ScaleInvariance pins that multiplying
// BOTH current and target by the same factor k > 0 keeps the
// ratio constant → same PoolStatus. A refactor introducing an
// absolute-magnitude term (e.g. "if target < threshold, auto-
// CRITICAL") would break this.
func TestEvaluatePoolHealth_MR_ScaleInvarianceAcrossMagnitudes(t *testing.T) {
	t.Parallel()
	// Three magnitudes, same ratio=0.6 (HEALTHY band).
	smallScale := EvaluatePoolHealth(pool("6", "10"))
	midScale := EvaluatePoolHealth(pool("6000", "10000"))
	largeScale := EvaluatePoolHealth(pool("600000000000", "1000000000000"))

	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, smallScale)
	assert.Equal(t, smallScale, midScale,
		"MR scale-invariance: same ratio at different magnitudes "+
			"yields same status. small=%v mid=%v", smallScale, midScale)
	assert.Equal(t, smallScale, largeScale,
		"MR scale-invariance: holds at realistic chain magnitudes "+
			"too. small=%v large=%v", smallScale, largeScale)
}

// TestEvaluatePoolHealth_MR_ScaleInvariantWithDecimalFractions
// pins the same invariant for non-integer ratios — catches
// refactors that handle big-int and fractional values differently.
func TestEvaluatePoolHealth_MR_ScaleInvariantWithFractionalValues(t *testing.T) {
	t.Parallel()
	// Ratio 0.75 (HEALTHY) at different precisions.
	a := EvaluatePoolHealth(pool("0.75", "1.0"))
	b := EvaluatePoolHealth(pool("7.5", "10"))
	c := EvaluatePoolHealth(pool("0.750000", "1.000000"))

	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, a)
	assert.Equal(t, a, b)
	assert.Equal(t, a, c)
}

// --------------------------------------------------------------
// MR — MONOTONIC PROGRESSION (permutative)
// --------------------------------------------------------------

// TestEvaluatePoolHealth_MR_MonotonicProgressionUnderRisingLoad
// pins that as currentUtilization grows (target fixed), the
// PoolStatus severity never regresses. Traversal:
// OVERFUNDED → HEALTHY → UNDERFUNDED → CRITICAL.
//
// A refactor introducing a non-monotonic transform (e.g. an
// "adaptive band" that narrows as target grows) would dip or
// jump severity out of order and break operator intuition.
func TestEvaluatePoolHealth_MR_MonotonicProgressionInRisingUtilization(t *testing.T) {
	t.Parallel()

	// Target = 1000 fixed.
	// Current sweeps 0 → 1500, passing through all 4 bands:
	//   0.000..499 → OVERFUNDED
	//   500..799   → HEALTHY
	//   800..1199  → UNDERFUNDED
	//   1200..∞    → CRITICAL
	const target = "1000"
	values := []string{
		"0", "100", "300", "499", // OVERFUNDED (ratio < 0.5)
		"500", "650", "799", // HEALTHY (0.5 <= ratio < 0.8)
		"800", "1000", "1199", // UNDERFUNDED (0.8 <= ratio < 1.2)
		"1200", "1500", "5000", // CRITICAL (ratio >= 1.2)
	}

	prevSeverity := -1
	for i, v := range values {
		status := EvaluatePoolHealth(pool(v, target))
		sev := poolStatusSeverity(status)
		require.GreaterOrEqual(t, sev, 0,
			"known severity for status %v at current=%s", status, v)
		if i > 0 {
			assert.GreaterOrEqual(t, sev, prevSeverity,
				"MR monotonic: rising utilization (%s → %s) MUST NOT "+
					"decrease severity. Got severity=%d (status=%v); "+
					"prev severity=%d. Pins the band ordering — a "+
					"refactor swapping two band thresholds would "+
					"surface as a non-monotonic dip here.",
				values[i-1], v, sev, status, prevSeverity)
		}
		prevSeverity = sev
	}
}

// TestEvaluatePoolHealth_MR_AllFourBandsReachable pins that the
// progression visits ALL FOUR distinct PoolStatus values as
// utilization rises from 0 to very-high. A refactor that
// collapsed two bands (made 0.5=0.8 say) would only produce 3
// distinct statuses.
func TestEvaluatePoolHealth_MR_AllFourBandsReachable(t *testing.T) {
	t.Parallel()
	seen := map[PoolStatus]bool{}
	for _, current := range []string{"100", "600", "900", "1500"} {
		status := EvaluatePoolHealth(pool(current, "1000"))
		seen[status] = true
	}
	assert.True(t, seen[PoolStatus_POOL_STATUS_OVERFUNDED],
		"current=100 → OVERFUNDED reachable")
	assert.True(t, seen[PoolStatus_POOL_STATUS_HEALTHY],
		"current=600 → HEALTHY reachable")
	assert.True(t, seen[PoolStatus_POOL_STATUS_UNDERFUNDED],
		"current=900 → UNDERFUNDED reachable")
	assert.True(t, seen[PoolStatus_POOL_STATUS_CRITICAL],
		"current=1500 → CRITICAL reachable")
	assert.Equal(t, 4, len(seen),
		"MR completeness: ALL FOUR bands are reachable. Pins that "+
			"no two band thresholds have collapsed into one — a "+
			"refactor making 0.5==0.8 would only produce 3 statuses.")
}

// --------------------------------------------------------------
// MR — ZERO-CURRENT WITH NONZERO-TARGET (boundary)
// --------------------------------------------------------------

// TestEvaluatePoolHealth_MR_ZeroCurrentNonzeroTargetIsOverfunded
// pins the specific boundary: drained pool (current=0) with a
// non-zero target produces OVERFUNDED (ratio=0 < 0.5). This is
// SEMANTICALLY unusual — a drained pool is "overfunded" vs
// target — but it's what the math says. Pin the current
// behavior so a refactor adding a "drained = CRITICAL" special
// case surfaces as a requirement.
func TestEvaluatePoolHealth_MR_ZeroCurrentNonzeroTargetOverfunded(t *testing.T) {
	t.Parallel()
	status := EvaluatePoolHealth(pool("0", "1000"))
	assert.Equal(t, PoolStatus_POOL_STATUS_OVERFUNDED, status,
		"MR drained-pool: current=0, target=1000 → ratio=0 → "+
			"OVERFUNDED (band < 0.5). Semantically unusual but it's "+
			"what the math says. Pin this so a refactor adding a "+
			"'drained = CRITICAL' case surfaces as an intentional "+
			"change, not a silent semantic shift.")
}

// --------------------------------------------------------------
// MR — ZERO-TARGET branch
// --------------------------------------------------------------

// TestEvaluatePoolHealth_MR_ZeroTargetBranchDichotomy pins the
// two zero-target sub-cases at :212-218:
//   target=0, current=0  → HEALTHY (no target, no activity)
//   target=0, current>0  → UNDERFUNDED (activity without budget)
//
// The branch is a degenerate case; pin it so a refactor
// collapsing it into the general formula would divide-by-zero.
func TestEvaluatePoolHealth_MR_ZeroTargetDichotomy(t *testing.T) {
	t.Parallel()

	// target=0, current=0 → HEALTHY
	s1 := EvaluatePoolHealth(pool("0", "0"))
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, s1,
		"MR zero-target-zero-current: HEALTHY (no activity, no target)")

	// target=0, current>0 → UNDERFUNDED
	s2 := EvaluatePoolHealth(pool("1", "0"))
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, s2,
		"MR zero-target-nonzero-current: UNDERFUNDED (activity, no "+
			"budget). Pins :217 — a refactor collapsing this special "+
			"branch into the ratio computation would trigger the "+
			"shopspring divide-by-zero panic.")

	// target=0, current arbitrarily large → still UNDERFUNDED (no
	// CRITICAL promotion).
	s3 := EvaluatePoolHealth(pool("999999999", "0"))
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, s3,
		"zero-target branch does NOT promote to CRITICAL no matter "+
			"how large current is — pins that the branch is a flat "+
			"UNDERFUNDED return, not a further-split classifier.")
}

// --------------------------------------------------------------
// MR — DETERMINISM
// --------------------------------------------------------------

// TestEvaluatePoolHealth_MR_DeterministicOnRepeatedCalls pins
// that the same PoolState always yields the same PoolStatus.
// A refactor introducing hidden state (cache, counter) would
// break this.
func TestEvaluatePoolHealth_MR_DeterministicAcrossCalls(t *testing.T) {
	t.Parallel()
	cases := []struct{ current, target string }{
		{"0", "0"}, {"500", "1000"}, {"800", "1000"},
		{"1200", "1000"}, {"1e-5", "1"}, {"1", "0"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.current+"/"+c.target, func(t *testing.T) {
			first := EvaluatePoolHealth(pool(c.current, c.target))
			for i := 0; i < 10; i++ {
				again := EvaluatePoolHealth(pool(c.current, c.target))
				require.Equal(t, first, again,
					"MR determinism: repeated calls with same input "+
						"yield same status. call=%d first=%v again=%v",
					i, first, again)
			}
		})
	}
}

// --------------------------------------------------------------
// MR — RATIO DIRECTLY CONSTRUCTED (pins Div is the only math)
// --------------------------------------------------------------

// TestEvaluatePoolHealth_MR_RatioMatchesShopspringComputation
// pins that the classification uses the SAME ratio value as
// `currentUtilization.Div(targetUtilization)` would compute
// directly. Catches any refactor that replaced Div with an
// alternate (approximated) ratio.
func TestEvaluatePoolHealth_MR_ClassificationMatchesDirectRatio(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name            string
		current, target string
		wantRatio       string
		wantStatus      PoolStatus
	}{
		{"below half", "250", "1000", "0.25", PoolStatus_POOL_STATUS_OVERFUNDED},
		{"at half", "500", "1000", "0.5", PoolStatus_POOL_STATUS_HEALTHY},
		{"mid health", "650", "1000", "0.65", PoolStatus_POOL_STATUS_HEALTHY},
		{"at underfund", "800", "1000", "0.8", PoolStatus_POOL_STATUS_UNDERFUNDED},
		{"at crit", "1200", "1000", "1.2", PoolStatus_POOL_STATUS_CRITICAL},
		{"far crit", "5000", "1000", "5", PoolStatus_POOL_STATUS_CRITICAL},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// Compute ratio DIRECTLY via shopspring — this is what
			// the classification internally does.
			cur, _ := decimal.NewFromString(c.current)
			tgt, _ := decimal.NewFromString(c.target)
			gotRatio := cur.Div(tgt)
			wantRatio, _ := decimal.NewFromString(c.wantRatio)
			require.True(t, gotRatio.Equal(wantRatio),
				"precondition: ratio matches declared wantRatio")

			status := EvaluatePoolHealth(pool(c.current, c.target))
			assert.Equal(t, c.wantStatus, status,
				"ratio=%s classified as %v (%s)",
				gotRatio.String(), status, c.name)
		})
	}
}

// --------------------------------------------------------------
// MR — SUBSET relation on band membership
// --------------------------------------------------------------

// TestEvaluatePoolHealth_MR_BandsPartitionTheRatioSpace pins that
// the 4 bands form a DISJOINT COVER of [0, ∞): every ratio
// classifies into EXACTLY ONE band, and union covers the space.
// A refactor producing overlap (e.g. if an `if/else` was
// broken into `if/if`) would let two bands claim the same
// ratio.
func TestEvaluatePoolHealth_MR_BandsArePartitionOfRatioSpace(t *testing.T) {
	t.Parallel()
	// Probe 0.0, 0.49, 0.5, 0.79, 0.8, 1.19, 1.2, 2.0 — the
	// boundaries + just-inside-each-band representatives.
	probes := []struct {
		current, target string
		wantStatus      PoolStatus
	}{
		{"0", "1000", PoolStatus_POOL_STATUS_OVERFUNDED},
		{"490", "1000", PoolStatus_POOL_STATUS_OVERFUNDED},
		{"499", "1000", PoolStatus_POOL_STATUS_OVERFUNDED},
		{"500", "1000", PoolStatus_POOL_STATUS_HEALTHY},
		{"790", "1000", PoolStatus_POOL_STATUS_HEALTHY},
		{"799", "1000", PoolStatus_POOL_STATUS_HEALTHY},
		{"800", "1000", PoolStatus_POOL_STATUS_UNDERFUNDED},
		{"1190", "1000", PoolStatus_POOL_STATUS_UNDERFUNDED},
		{"1199", "1000", PoolStatus_POOL_STATUS_UNDERFUNDED},
		{"1200", "1000", PoolStatus_POOL_STATUS_CRITICAL},
		{"2000", "1000", PoolStatus_POOL_STATUS_CRITICAL},
		{"10000", "1000", PoolStatus_POOL_STATUS_CRITICAL},
	}
	for _, p := range probes {
		status := EvaluatePoolHealth(pool(p.current, p.target))
		assert.Equal(t, p.wantStatus, status,
			"MR partition: current=%s target=%s → exactly one band "+
				"(%v). A refactor producing overlapping bands would "+
				"make this nondeterministic.",
			p.current, p.target, status)
	}
}
