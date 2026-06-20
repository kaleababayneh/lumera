package types

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestComputeRestitutionSplit_SpecPercentages(t *testing.T) {
	// 10_000 units divides cleanly into 60/25/10/5 with no residual.
	amount := decimal.NewFromInt(10_000)
	split, err := ComputeRestitutionSplit(amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]decimal.Decimal{
		"users":     decimal.NewFromInt(6_000),
		"insurance": decimal.NewFromInt(2_500),
		"treasury":  decimal.NewFromInt(1_000),
		"burn":      decimal.NewFromInt(500),
	}
	if !split.Users.Equal(want["users"]) {
		t.Errorf("users: got %s, want %s", split.Users, want["users"])
	}
	if !split.Insurance.Equal(want["insurance"]) {
		t.Errorf("insurance: got %s, want %s", split.Insurance, want["insurance"])
	}
	if !split.Treasury.Equal(want["treasury"]) {
		t.Errorf("treasury: got %s, want %s", split.Treasury, want["treasury"])
	}
	if !split.Burn.Equal(want["burn"]) {
		t.Errorf("burn: got %s, want %s", split.Burn, want["burn"])
	}
	if !split.Total().Equal(amount) {
		t.Errorf("total: got %s, want %s", split.Total(), amount)
	}
}

func TestComputeRestitutionSplit_ResidualGoesToUsers(t *testing.T) {
	// 7 units: floor(7*0.25)=1, floor(7*0.10)=0, floor(7*0.05)=0, users=6.
	// All four floors together cover only 1 unit; users absorbs the other 6.
	// The key invariants are (a) no share is rounded up above its nominal
	// percentage — especially burn, which must stay ≤5% — and (b) the total
	// equals the input exactly.
	amount := decimal.NewFromInt(7)
	split, err := ComputeRestitutionSplit(amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !split.Insurance.Equal(decimal.NewFromInt(1)) {
		t.Errorf("insurance: got %s, want 1", split.Insurance)
	}
	if !split.Treasury.Equal(decimal.Zero) {
		t.Errorf("treasury: got %s, want 0 (floor(0.7))", split.Treasury)
	}
	if !split.Burn.Equal(decimal.Zero) {
		t.Errorf("burn: got %s, want 0 (floor(0.35))", split.Burn)
	}
	if !split.Users.Equal(decimal.NewFromInt(6)) {
		t.Errorf("users: got %s, want 6 (residual)", split.Users)
	}
	if !split.Total().Equal(amount) {
		t.Errorf("total: got %s, want %s (exactness broken)", split.Total(), amount)
	}
}

func TestComputeRestitutionSplit_BurnNeverExceedsFivePercent(t *testing.T) {
	// Stress: every amount from 1..1000 must keep burn ≤ 5%. Floor-based
	// computation is the only reason this holds; a rounding-up scheme on
	// burn would silently violate the spec's immutability guarantee.
	fivePct := decimal.NewFromFloat(0.05)
	for i := int64(1); i <= 1000; i++ {
		amount := decimal.NewFromInt(i)
		split, err := ComputeRestitutionSplit(amount)
		if err != nil {
			t.Fatalf("amount %d: unexpected error: %v", i, err)
		}
		if split.Burn.Div(amount).GreaterThan(fivePct) {
			t.Errorf("amount=%d burn=%s exceeds 5%% cap", i, split.Burn)
		}
		if !split.Total().Equal(amount) {
			t.Errorf("amount=%d total=%s not equal to input", i, split.Total())
		}
	}
}

func TestComputeRestitutionSplit_Zero(t *testing.T) {
	split, err := ComputeRestitutionSplit(decimal.Zero)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !split.Users.IsZero() || !split.Insurance.IsZero() || !split.Treasury.IsZero() || !split.Burn.IsZero() {
		t.Errorf("zero input must produce zero split, got %+v", split)
	}
}

func TestComputeRestitutionSplit_Negative(t *testing.T) {
	_, err := ComputeRestitutionSplit(decimal.NewFromInt(-1))
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}

// TestComputeRestitutionSplit_ScaleInvarianceMetamorphic asserts that
// when the input amount is a multiple of RestitutionTotalBps (10_000),
// the split is exact — every share equals amount * share_bps / 10_000
// with no residual assigned to Users. Any rounding error creeping into
// the floor() math would surface here because there is no slack left
// for Users to absorb.
func TestComputeRestitutionSplit_ScaleInvarianceMetamorphic(t *testing.T) {
	for _, k := range []int64{1, 2, 7, 100, 9_999, 1_000_000} {
		k := k
		amount := decimal.NewFromInt(k * int64(RestitutionTotalBps))
		split, err := ComputeRestitutionSplit(amount)
		if err != nil {
			t.Fatalf("k=%d: unexpected error: %v", k, err)
		}
		want := map[string]decimal.Decimal{
			"users":     decimal.NewFromInt(k * int64(RestitutionUsersBps)),
			"insurance": decimal.NewFromInt(k * int64(RestitutionInsuranceBps)),
			"treasury":  decimal.NewFromInt(k * int64(RestitutionTreasuryBps)),
			"burn":      decimal.NewFromInt(k * int64(RestitutionBurnBps)),
		}
		if !split.Users.Equal(want["users"]) {
			t.Errorf("k=%d users=%s want=%s", k, split.Users, want["users"])
		}
		if !split.Insurance.Equal(want["insurance"]) {
			t.Errorf("k=%d insurance=%s want=%s", k, split.Insurance, want["insurance"])
		}
		if !split.Treasury.Equal(want["treasury"]) {
			t.Errorf("k=%d treasury=%s want=%s", k, split.Treasury, want["treasury"])
		}
		if !split.Burn.Equal(want["burn"]) {
			t.Errorf("k=%d burn=%s want=%s", k, split.Burn, want["burn"])
		}
	}
}

// TestComputeRestitutionSplit_MonotonicityMetamorphic asserts that
// increasing the input amount never decreases the Insurance, Treasury,
// or Burn shares — each is floor(amount * bps / 10_000) over a fixed
// bps, which is a non-decreasing step function in amount. A buggy
// rounding scheme could produce a share that briefly dips as amount
// grows, which would be a correctness red flag for the underlying math.
//
// Users is intentionally NOT checked here: it is the residual
// (amount - sum-of-floors) and can legitimately decrease by up to 3
// when all three other shares step up simultaneously (e.g. going from
// amount=19 to amount=20 makes all three floors tick up by 1, shrinking
// Users by 2). Users-specific bounds are covered by the cap/floor
// invariants in the fuzz test below.
func TestComputeRestitutionSplit_MonotonicityMetamorphic(t *testing.T) {
	prev, err := ComputeRestitutionSplit(decimal.NewFromInt(0))
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	for i := int64(1); i <= 2_500; i++ {
		split, err := ComputeRestitutionSplit(decimal.NewFromInt(i))
		if err != nil {
			t.Fatalf("amount=%d: %v", i, err)
		}
		if split.Insurance.LessThan(prev.Insurance) {
			t.Fatalf("amount=%d: insurance dropped %s->%s", i, prev.Insurance, split.Insurance)
		}
		if split.Treasury.LessThan(prev.Treasury) {
			t.Fatalf("amount=%d: treasury dropped %s->%s", i, prev.Treasury, split.Treasury)
		}
		if split.Burn.LessThan(prev.Burn) {
			t.Fatalf("amount=%d: burn dropped %s->%s", i, prev.Burn, split.Burn)
		}
		prev = split
	}
}

// FuzzComputeRestitutionSplit_Invariants locks in the spec-level
// invariants across arbitrary non-negative amounts: no error, total
// equals input exactly, every share is non-negative, and each share
// stays at or below its nominal cap (with Users at or above its 60%
// floor since it absorbs the residual). A buggy rounding scheme that
// let burn exceed 5% on some exotic amount would fail here.
func FuzzComputeRestitutionSplit_Invariants(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(1))
	f.Add(int64(7))
	f.Add(int64(10_000))
	f.Add(int64(999_999_999))
	f.Add(int64(1 << 40))

	f.Fuzz(func(t *testing.T, amt int64) {
		if amt < 0 {
			// Negative is defined to error; covered by the unit test, skip here.
			t.Skip()
		}
		amount := decimal.NewFromInt(amt)
		split, err := ComputeRestitutionSplit(amount)
		if err != nil {
			t.Fatalf("amount=%d: unexpected error: %v", amt, err)
		}
		if split.Users.IsNegative() || split.Insurance.IsNegative() ||
			split.Treasury.IsNegative() || split.Burn.IsNegative() {
			t.Fatalf("amount=%d: negative share in split %+v", amt, split)
		}
		if !split.Total().Equal(amount) {
			t.Fatalf("amount=%d: total=%s not equal to input", amt, split.Total())
		}
		if amount.IsZero() {
			return
		}
		// Share-cap invariants. Users floors ≥ 60% (absorbs residual, so can
		// be strictly more); the other three are capped at their nominal bps.
		fraction := func(share decimal.Decimal) decimal.Decimal {
			return share.Mul(decimal.NewFromInt(int64(RestitutionTotalBps))).Div(amount)
		}
		if fraction(split.Burn).GreaterThan(decimal.NewFromInt(int64(RestitutionBurnBps))) {
			t.Fatalf("amount=%d burn=%s exceeds %d bps cap", amt, split.Burn, RestitutionBurnBps)
		}
		if fraction(split.Insurance).GreaterThan(decimal.NewFromInt(int64(RestitutionInsuranceBps))) {
			t.Fatalf("amount=%d insurance=%s exceeds %d bps cap", amt, split.Insurance, RestitutionInsuranceBps)
		}
		if fraction(split.Treasury).GreaterThan(decimal.NewFromInt(int64(RestitutionTreasuryBps))) {
			t.Fatalf("amount=%d treasury=%s exceeds %d bps cap", amt, split.Treasury, RestitutionTreasuryBps)
		}
		if fraction(split.Users).LessThan(decimal.NewFromInt(int64(RestitutionUsersBps))) {
			t.Fatalf("amount=%d users=%s below %d bps floor", amt, split.Users, RestitutionUsersBps)
		}
	})
}

func TestRestitutionBpsSumTo100Percent(t *testing.T) {
	// Guards against a future edit that adjusts one share without
	// rebalancing the others. ComputeRestitutionSplit itself fails closed
	// when this invariant breaks, but pin it at the constant level too so
	// the error surfaces at test time, not at first slashing event.
	got := RestitutionUsersBps + RestitutionInsuranceBps + RestitutionTreasuryBps + RestitutionBurnBps
	if got != RestitutionTotalBps {
		t.Fatalf("restitution bps sum to %d, want %d", got, RestitutionTotalBps)
	}
}
