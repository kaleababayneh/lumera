package types

import (
	"math"
	"testing"
)

func TestClamp01_NaN(t *testing.T) {
	t.Parallel()
	// NaN must be clamped to 0 to prevent consensus divergence from
	// non-deterministic NaN bit patterns across validator platforms.
	if got := Clamp01(math.NaN()); got != 0 {
		t.Fatalf("Clamp01(NaN) = %v, want 0", got)
	}
	// Also verify Inf is clamped.
	if got := Clamp01(math.Inf(1)); got != 1 {
		t.Fatalf("Clamp01(+Inf) = %v, want 1", got)
	}
	if got := Clamp01(math.Inf(-1)); got != 0 {
		t.Fatalf("Clamp01(-Inf) = %v, want 0", got)
	}
}

func TestExpNeg_BasicValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		x    float64
		want float64
	}{
		{0, 1.0},
		{1, math.Exp(-1)},
		{2, math.Exp(-2)},
		{0.5, math.Exp(-0.5)},
		{10, math.Exp(-10)},
		{15, math.Exp(-15)},
	}
	for _, tc := range cases {
		got := ExpNeg(tc.x)
		if math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("ExpNeg(%v) = %v, want %v (diff %e)", tc.x, got, tc.want, got-tc.want)
		}
	}
}

func TestExpNeg_NegativeInput(t *testing.T) {
	t.Parallel()
	if got := ExpNeg(-1); got != 1.0 {
		t.Errorf("ExpNeg(-1) = %v, want 1.0", got)
	}
	if got := ExpNeg(0); got != 1.0 {
		t.Errorf("ExpNeg(0) = %v, want 1.0", got)
	}
}

func TestExpNeg_LargeInput(t *testing.T) {
	t.Parallel()
	if got := ExpNeg(20); got != 0 {
		t.Errorf("ExpNeg(20) = %v, want 0", got)
	}
	if got := ExpNeg(100); got != 0 {
		t.Errorf("ExpNeg(100) = %v, want 0", got)
	}
}

func TestExpNeg_NonFiniteInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		x    float64
		want float64
	}{
		{"NaN", math.NaN(), 0},
		{"+Inf", math.Inf(1), 0},
		{"-Inf", math.Inf(-1), 1},
	}

	for _, tc := range cases {
		got := ExpNeg(tc.x)
		if got != tc.want || math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("ExpNeg(%s) = %v, want finite %v", tc.name, got, tc.want)
		}
	}
}

func TestExpNeg_ScoringRange(t *testing.T) {
	t.Parallel()
	// In scoring: math.Exp(-ageDays/d) where ageDays/d ranges from ~0 to ~12
	// and math.Exp(-k/tau) where k/tau ranges from ~0.1 to ~5
	// Verify millis-level accuracy across the full scoring range
	for i := 0; i <= 120; i++ {
		x := float64(i) / 10.0 // 0.0 to 12.0
		got := ExpNeg(x)
		want := math.Exp(-x)
		// After millis quantization (round to nearest 0.001), must match
		gotMillis := math.Round(got * 1000)
		wantMillis := math.Round(want * 1000)
		if gotMillis != wantMillis {
			t.Errorf("ExpNeg(%v): millis mismatch: got %v, want %v (raw: %v vs %v)",
				x, gotMillis, wantMillis, got, want)
		}
	}
}

func TestLog1p_BasicValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		x    float64
		want float64
	}{
		{0, 0},
		{1, math.Log1p(1)},
		{10, math.Log1p(10)},
		{100, math.Log1p(100)},
		{365, math.Log1p(365)},
	}
	for _, tc := range cases {
		got := Log1p(tc.x)
		if math.Abs(got-tc.want) > 1e-8 {
			t.Errorf("Log1p(%v) = %v, want %v (diff %e)", tc.x, got, tc.want, got-tc.want)
		}
	}
}

func TestLog1p_NegativeInput(t *testing.T) {
	t.Parallel()
	if got := Log1p(-1); got != 0 {
		t.Errorf("Log1p(-1) = %v, want 0", got)
	}
	if got := Log1p(0); got != 0 {
		t.Errorf("Log1p(0) = %v, want 0", got)
	}
}

func TestLog1p_NonFiniteInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		x    float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}

	for _, tc := range cases {
		if got := Log1p(tc.x); got != 0 || math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("Log1p(%s) = %v, want finite 0", tc.name, got)
		}
	}
}

func TestLog1p_LongevityRange(t *testing.T) {
	t.Parallel()
	// In scoring: log1p(daysActive) / log1p(maxDays)
	// daysActive ranges 0-365+, maxDays is 365
	maxDays := 365.0
	log1pMax := Log1p(maxDays)
	for days := 0; days <= 400; days++ {
		d := float64(days)
		got := Log1p(d)
		want := math.Log1p(d)
		if math.Abs(got-want) > 1e-8 {
			t.Errorf("Log1p(%v) = %v, want %v (diff %e)", d, got, want, got-want)
		}
		// Verify the ratio (the actual scoring formula) matches at millis level
		gotScore := Clamp01(got / log1pMax)
		wantScore := Clamp01(want / math.Log1p(maxDays))
		gotMillis := math.Round(gotScore * 1000)
		wantMillis := math.Round(wantScore * 1000)
		if gotMillis != wantMillis {
			t.Errorf("longevity(%v days): millis mismatch: got %v, want %v",
				days, gotMillis, wantMillis)
		}
	}
}

func TestExpNeg_Determinism(t *testing.T) {
	t.Parallel()
	// Same inputs must produce identical outputs (bit-for-bit)
	inputs := []float64{0.1, 0.5, 1.0, 2.5, 3.0, 5.0, 7.7, 10.0, 12.5}
	for _, x := range inputs {
		r1 := ExpNeg(x)
		r2 := ExpNeg(x)
		if r1 != r2 {
			t.Errorf("ExpNeg(%v) not deterministic: %v vs %v", x, r1, r2)
		}
	}
}

func TestLog1p_Determinism(t *testing.T) {
	t.Parallel()
	inputs := []float64{0.1, 1, 10, 50, 100, 200, 365}
	for _, x := range inputs {
		r1 := Log1p(x)
		r2 := Log1p(x)
		if r1 != r2 {
			t.Errorf("Log1p(%v) not deterministic: %v vs %v", x, r1, r2)
		}
	}
}

// TestExpNeg_MonotonicallyDecreasingMetamorphic asserts that ExpNeg
// is a monotonically non-increasing function of x for x >= 0. This is
// the fundamental shape property of exponential decay. A Pade-approximant
// refactor that accidentally flipped the numerator/denominator sign on
// one coefficient would surface as a score that rose with age — which
// is semantically the opposite of what the scoring formula intends.
func TestExpNeg_MonotonicallyDecreasingMetamorphic(t *testing.T) {
	t.Parallel()
	prev := 1.0 // ExpNeg(0) == 1
	// Sweep x across the full [0, 20] scoring range with fine steps.
	for i := 0; i <= 2000; i++ {
		x := float64(i) / 100.0 // 0.00, 0.01, ..., 20.00
		got := ExpNeg(x)
		if got > prev {
			t.Fatalf("ExpNeg non-monotone at x=%v: prev=%v got=%v", x, prev, got)
		}
		if got < 0 || got > 1 {
			t.Fatalf("ExpNeg(%v) = %v outside [0,1]", x, got)
		}
		prev = got
	}
}

// TestLog1p_MonotonicallyIncreasingMetamorphic asserts that Log1p is
// a monotonically non-decreasing function of x for x >= 0. Used by
// the longevity scoring to scale days-active against a max-days
// denominator; any monotonicity regression would let a tool with
// fewer days active score higher than one with more.
func TestLog1p_MonotonicallyIncreasingMetamorphic(t *testing.T) {
	t.Parallel()
	prev := 0.0 // Log1p(0) == 0
	// Sweep x across a wide non-negative range.
	for i := 0; i <= 2000; i++ {
		x := float64(i) / 2.0 // 0, 0.5, ..., 1000
		got := Log1p(x)
		if got < prev {
			t.Fatalf("Log1p non-monotone at x=%v: prev=%v got=%v", x, prev, got)
		}
		if got < 0 {
			t.Fatalf("Log1p(%v) = %v is negative (Log1p of non-negative x is always >= 0)", x, got)
		}
		prev = got
	}
}

// TestExpNeg_Log1p_NegativeInputsClampMetamorphic asserts the
// defensive-clamp contract for negative inputs: both functions treat
// x < 0 as if x == 0. ExpNeg(-k) == 1 (not e^k) and Log1p(-k) == 0
// (not log(1-k), which would be negative or NaN). The scoring pipeline
// feeds these functions with ratios that can briefly go negative due
// to timing drift; the clamp prevents consensus-breaking NaN
// propagation.
func TestExpNeg_Log1p_NegativeInputsClampMetamorphic(t *testing.T) {
	t.Parallel()
	negatives := []float64{-0.0001, -1, -100, -1e9}
	for _, x := range negatives {
		if got := ExpNeg(x); got != 1.0 {
			t.Errorf("ExpNeg(%v) = %v, want 1.0 (negative input must clamp)", x, got)
		}
		if got := Log1p(x); got != 0.0 {
			t.Errorf("Log1p(%v) = %v, want 0.0 (negative input must clamp)", x, got)
		}
	}
}

// TestBalancedComposite_NilReceiverAndAllOnes asserts two spot
// contracts BalancedComposite relies on downstream: (1) nil
// receiver returns 0 (defensive) and (2) when all seven dimensions
// are 1.0, the composite is exactly 1.0 — this pins the weight-sum
// equality the docstring promises (0.25+0.30+0.10+0.10+0.15+0.10+
// 0.00 = 1.00). Any weight-table drift that let the sum drop below
// or rise above 1.0 would silently scale every reputation vector
// up or down and break the existing ToProto millis mapping.
func TestBalancedComposite_NilReceiverAndAllOnes(t *testing.T) {
	var nilR *ReputationResult
	if got := nilR.BalancedComposite(); got != 0 {
		t.Fatalf("nil receiver: got %v, want 0", got)
	}

	full := &ReputationResult{
		Reliability: 1, Safety: 1, Latency: 1,
		CostDiscipline: 1, Dispute: 1, Longevity: 1, Privacy: 1,
	}
	got := full.BalancedComposite()
	// The function sums floats — compare with a small tolerance
	// instead of exact equality to absorb ULP-level drift, but any
	// weight sum ≠ 1.0 in the design docs would fail outside this
	// window.
	if got < 0.9999999 || got > 1.0000001 {
		t.Fatalf("all-ones composite = %v, want ~1.0", got)
	}
}

// TestBalancedComposite_PrivacyWeightIsZero asserts the specific
// design choice documented on the function: the privacy dimension
// has weight 0.00 in the current balanced profile. Changing the
// Privacy field must not change the composite. This is load-bearing
// because the privacy axis is reported but intentionally excluded
// from ranking weight today; a refactor that silently promoted it
// to a non-zero weight would re-order the leaderboard without
// triggering any other test.
func TestBalancedComposite_PrivacyWeightIsZero(t *testing.T) {
	base := &ReputationResult{
		Reliability: 0.5, Safety: 0.5, Latency: 0.5,
		CostDiscipline: 0.5, Dispute: 0.5, Longevity: 0.5, Privacy: 0,
	}
	withPrivacy := *base
	withPrivacy.Privacy = 1.0

	baseScore := base.BalancedComposite()
	privScore := (&withPrivacy).BalancedComposite()
	if baseScore != privScore {
		t.Fatalf("privacy weight is not zero: base=%v withPrivacy=%v (delta=%v)",
			baseScore, privScore, privScore-baseScore)
	}
}

// TestBalancedComposite_MonotonicityMetamorphic asserts that for
// each of the six weighted dimensions (privacy excluded because
// its weight is zero), increasing that dimension from 0 to 1 while
// holding the others fixed monotonically increases the composite.
// Catches a weight-table refactor that accidentally made a
// dimension's weight negative — that would invert the ranking
// signal for tools excelling on that axis.
func TestBalancedComposite_MonotonicityMetamorphic(t *testing.T) {
	dims := []struct {
		name string
		set  func(r *ReputationResult, v float64)
	}{
		{"reliability", func(r *ReputationResult, v float64) { r.Reliability = v }},
		{"safety", func(r *ReputationResult, v float64) { r.Safety = v }},
		{"latency", func(r *ReputationResult, v float64) { r.Latency = v }},
		{"cost_discipline", func(r *ReputationResult, v float64) { r.CostDiscipline = v }},
		{"dispute", func(r *ReputationResult, v float64) { r.Dispute = v }},
		{"longevity", func(r *ReputationResult, v float64) { r.Longevity = v }},
	}
	for _, d := range dims {
		d := d
		t.Run(d.name, func(t *testing.T) {
			var prev float64
			for _, v := range []float64{0, 0.1, 0.25, 0.5, 0.75, 0.9, 1.0} {
				r := &ReputationResult{}
				d.set(r, v)
				got := r.BalancedComposite()
				if got < prev {
					t.Fatalf("%s=%v composite dropped: prev=%v got=%v", d.name, v, prev, got)
				}
				prev = got
			}
		})
	}
}
