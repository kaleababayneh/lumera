
package types

import (
	"math"
	"testing"
	"time"
)

// Deadline-guarded regression tests for b453672ee
// (fix(passport): clamp non-finite deterministic math inputs).
//
// The fix added:
//
//   ExpNeg:  `if x != x { return 0 }` — NaN guard
//   Log1p:   `if x != x { return 0 }` — NaN guard
//            `if x > maxFiniteFloat64 { return 0 }` — +Inf guard
//
// Of these, the +Inf case on Log1p is the MOST CRITICAL: pre-fix,
// `Log1p(+Inf)` entered an infinite loop at the argument-reduction
// step:
//
//   y := 1.0 + x           // +Inf
//   k := 0
//   m := y                 // +Inf
//   for m >= 2 {           // +Inf >= 2 is true forever
//       m /= 2             // +Inf / 2 is still +Inf
//       k++
//   }
//
// The commit's companion test TestLog1p_NonFiniteInput (detmath_test.go:136-152)
// asserts the correct post-fix values for NaN/+Inf/-Inf — BUT it
// has no deadline guard. With the pre-fix code, the test would
// enter the infinite loop and HANG indefinitely, only surfacing as
// a global `go test -timeout` failure with no indication of WHICH
// input hung. Consensus-critical: if a future refactor dropped the
// `x > maxFiniteFloat64` branch, tests would stop failing cleanly
// and CI would start timing out opaquely.
//
// These deadline-guarded tests are the negative-case regression
// that WOULD HAVE CAUGHT b453672ee's bug with a clear signal:
// "Log1p(+Inf) HUNG — infinite loop in argument reduction".
// Each case runs in a goroutine with a 2-second select deadline;
// a hang surfaces as a `t.Fatal` with the specific input named,
// not as an opaque global-timeout failure.

// TestLog1p_PlusInfDoesNotHang pins the specific bug fixed in
// b453672ee: Log1p(+Inf) must return in bounded time. Pre-fix this
// would enter an infinite loop (`for m >= 2 { m /= 2 }` with m=+Inf).
func TestLog1p_PlusInfDoesNotHang(t *testing.T) {
	t.Parallel()

	done := make(chan float64, 1)
	go func() {
		done <- Log1p(math.Inf(1))
	}()

	select {
	case got := <-done:
		// Post-fix: the maxFiniteFloat64 guard at detmath.go:85-87
		// returns 0 immediately. If a regression removed that guard,
		// we'd be in the infinite loop path and never hit this case.
		if got != 0 {
			t.Fatalf("Log1p(+Inf) = %v, want 0 (non-finite clamp)", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Log1p(+Inf) HUNG — infinite loop in argument reduction " +
			"(for m >= 2; m /= 2 with m=+Inf). Regression to pre-b453672ee " +
			"behavior: the `if x > maxFiniteFloat64 { return 0 }` guard at " +
			"detmath.go:85-87 has been removed or reordered after the " +
			"argument-reduction loop. This is a validator-halt vector on " +
			"the passport scoring path — any upstream non-finite produces " +
			"a hang inside consensus BeginBlock/EndBlock work.")
	}
}

// TestLog1p_NaNDoesNotHang pins the NaN path. While NaN doesn't
// produce an infinite loop (all NaN comparisons are false, so
// the `for m >= 2` exits immediately), the downstream series
// computation propagates NaN. Pre-fix, Log1p(NaN) returned NaN
// — a non-deterministic hazard (downstream callers comparing
// against NaN get false results that silently fork the chain
// if one validator's optimizer handles NaN differently).
func TestLog1p_NaNDoesNotHang(t *testing.T) {
	t.Parallel()

	done := make(chan float64, 1)
	go func() {
		done <- Log1p(math.NaN())
	}()

	select {
	case got := <-done:
		if got != 0 || math.IsNaN(got) {
			t.Fatalf("Log1p(NaN) = %v, want exactly 0 — non-finite must clamp "+
				"to 0 so downstream scoring is deterministic. NaN leaking here "+
				"breaks consensus because NaN comparisons are always false "+
				"and platform optimizers may handle NaN operations differently",
				got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Log1p(NaN) HUNG — the NaN guard at detmath.go:79-81 is missing")
	}
}

// TestExpNeg_NaNDoesNotHang pins the NaN guard in ExpNeg. The +Inf
// case in ExpNeg is covered by the existing `x >= 20` short-circuit
// (20 < +Inf), so +Inf was always fine; only NaN was an issue since
// `NaN >= 20` is false, allowing NaN to flow into the Padé
// approximant and return NaN.
func TestExpNeg_NaNDoesNotHang(t *testing.T) {
	t.Parallel()

	done := make(chan float64, 1)
	go func() {
		done <- ExpNeg(math.NaN())
	}()

	select {
	case got := <-done:
		if got != 0 || math.IsNaN(got) {
			t.Fatalf("ExpNeg(NaN) = %v, want exactly 0 — non-finite must clamp "+
				"to 0. NaN leaking from ExpNeg would propagate into "+
				"scoring composites and break consensus determinism", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ExpNeg(NaN) HUNG — the NaN guard at detmath.go:27-29 is missing")
	}
}

// TestExpNeg_PlusInfDoesNotHang pins the +Inf short-circuit. The value is
// already covered in detmath_test.go; this deadline guard makes regressions
// fail with a specific signal instead of an opaque package timeout.
func TestExpNeg_PlusInfDoesNotHang(t *testing.T) {
	t.Parallel()

	done := make(chan float64, 1)
	go func() {
		done <- ExpNeg(math.Inf(1))
	}()

	select {
	case got := <-done:
		if got != 0 || math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("ExpNeg(+Inf) = %v, want exactly 0", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ExpNeg(+Inf) HUNG — expected immediate x >= 20 clamp")
	}
}

// TestLog1p_NearMaxFloatDoesNotHang pins the boundary case — a
// finite-but-near-max input must still terminate. The argument
// reduction loop `for m >= 2 { m /= 2 }` halves 1+x repeatedly
// down to [1, 2). For x near math.MaxFloat64, this takes ~1024
// iterations (since log2(MaxFloat64) ≈ 1023.99). Bounded, not
// infinite — but we want an explicit deadline assertion so a
// future refactor that, e.g., inlines the loop with a smaller
// clamp bound doesn't regress the finite-range performance.
func TestLog1p_NearMaxFloatDoesNotHang(t *testing.T) {
	t.Parallel()

	done := make(chan float64, 1)
	go func() {
		// math.MaxFloat64 is at the boundary of the guard at
		// detmath.go:85 (`x > maxFiniteFloat64`). MaxFloat64 == max,
		// so the `>` check is false and we proceed to the loop.
		done <- Log1p(math.MaxFloat64)
	}()

	select {
	case got := <-done:
		// log(1 + MaxFloat64) ≈ log(MaxFloat64) ≈ 709.78 (the largest
		// natural log of any finite float64). Assert finite + positive
		// and within 10% of the stdlib reference.
		if math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("Log1p(MaxFloat64) = %v; must be finite positive", got)
		}
		want := math.Log1p(math.MaxFloat64)
		if math.Abs(got-want)/want > 0.1 {
			t.Fatalf("Log1p(MaxFloat64) = %v; stdlib got %v (relative error > 10%%)", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Log1p(MaxFloat64) exceeded 2s — argument-reduction loop " +
			"appears unbounded or extremely slow. Expected ~1024 iterations")
	}
}
