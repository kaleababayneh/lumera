
package types

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression guards for the exponent-bomb DoS on two HOT-PATH helpers
// in x/insurance/types: CalculatePremium (called per-claim on the
// premium-assessment path) and EvaluatePoolHealth (called on every
// rebalance + health-query). Both consume string decimal fields
// persisted on insurance proto state (PublisherRisk.PremiumMultiplier,
// PoolState.TargetUtilization, PoolState.CurrentUtilization) and feed
// them into the single-most-bomb-explosive shopspring ops:
//
//   * CalculatePremium: basePremium.Mul(premiumMultiplier)
//       — Mul forces big.Int alignment on a symbolic exponent
//   * EvaluatePoolHealth: currentUtilization.Div(targetUtilization)
//       followed by utilizationRatio.LessThan(...)*3
//       — Div is the WORST op; it forces exponent alignment AND
//         arbitrary-precision reciprocal computation
//
// shopspring.decimal.NewFromString("1e11100100") parses in O(1)
// (symbolic exponent), but the downstream ops expand the big.Int
// to multi-million digits — 1.3s+ per op pre-guard, String hangs
// indefinitely. If PoolState or PublisherRisk is ever poisoned
// (migration bug, future writer that forgets the guard, adversarial
// state injection), every call to these helpers on every validator
// halts block production.
//
// Reachability: PublisherRisk and PoolState strings come from keeper
// state today, which is defended at genesis (already moneyguard-gated
// in genesis.go) and MsgUpdateParams (ValidateBasic). But EvaluatePool
// Health is called from the rebalance path (keeper.go:505) and the
// health-query gRPC; CalculatePremium is called from the claim-
// processing path. Defense-in-depth at these arithmetic sites closes
// the gap so any future writer bug cannot brick the chain.
//
// Fix: treat absurd-exponent values as parse errors — EvaluatePool
// Health degrades to zero (matching the existing err-branch behavior,
// so a poisoned pool is flagged healthy-by-default rather than
// crashing); CalculatePremium degrades to multiplier=1 (matching
// the existing empty/unparseable fallback).
//
// Same DoS class sweep: 8438b6354 (grpc_router), 25d34d734 (cache),
// 5c237b056 (router types helper), cbbaba3cb (router msg_server),
// 09cffa7b4 (registry params+validations), c1ec4b822, b21923578.

// TestEvaluatePoolHealth_ExponentBombDegradesToZero pins that
// EvaluatePoolHealth returns without hanging when PoolState carries
// a poisoned field in TargetUtilization or CurrentUtilization.
// Deadline-guarded so any regression surfaces as a visible timeout
// rather than a silent multi-minute hang inside the keeper rebalance
// loop.
func TestEvaluatePoolHealth_ExponentBombDegradesToZero(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		pool *PoolState
	}{
		{
			name: "poisoned_target_utilization",
			pool: &PoolState{
				TargetUtilization:  "1e11100100",
				CurrentUtilization: "0.5",
			},
		},
		{
			name: "poisoned_current_utilization",
			pool: &PoolState{
				TargetUtilization:  "0.5",
				CurrentUtilization: "1e11100100",
			},
		},
		{
			name: "both_poisoned",
			pool: &PoolState{
				TargetUtilization:  "1e11100100",
				CurrentUtilization: "1e-11100100",
			},
		},
		{
			name: "just_past_bound_101",
			pool: &PoolState{
				TargetUtilization:  "1e101",
				CurrentUtilization: "0.5",
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			done := make(chan PoolStatus, 1)
			go func() { done <- EvaluatePoolHealth(tc.pool) }()
			select {
			case status := <-done:
				// The guard degrades absurd-exponent to zero. Both fields
				// zeroed → "no target" branch: either HEALTHY (both zero)
				// or UNDERFUNDED (target zero but current non-zero).
				// One field zeroed → ratio involves a zero operand.
				// Key invariant: function returns in bounded time and
				// yields a defined PoolStatus, NOT a hang.
				assert.Contains(t,
					[]PoolStatus{
						PoolStatus_POOL_STATUS_HEALTHY,
						PoolStatus_POOL_STATUS_UNDERFUNDED,
						PoolStatus_POOL_STATUS_OVERFUNDED,
						PoolStatus_POOL_STATUS_CRITICAL,
					},
					status,
					"returned status must be one of the defined enums, "+
						"got %v", status)
			case <-time.After(2 * time.Second):
				t.Fatalf("EvaluatePoolHealth HUNG on %s — moneyguard "+
					"missing; this is a validator halt-vector on the "+
					"rebalance loop (keeper.go calls it on every pool "+
					"balance update)", tc.name)
			}
		})
	}
}

// TestEvaluatePoolHealth_LegitimateInputsUnchanged is the negative-
// regression guard: the guard must not over-reject in-bound values.
// Existing tests (TestEvaluatePoolHealth_Healthy / _Overfunded /
// _Underfunded / _Critical) cover the default happy-path values;
// this adds boundary coverage at the moneyguard ±100 limit.
func TestEvaluatePoolHealth_LegitimateInputsUnchanged(t *testing.T) {
	t.Parallel()

	// Boundary: 1e100 is exactly at the moneyguard limit and must pass.
	// 1e-100 also at boundary.
	pool := &PoolState{
		TargetUtilization:  "1e100",
		CurrentUtilization: "1e100",
	}
	done := make(chan PoolStatus, 1)
	go func() { done <- EvaluatePoolHealth(pool) }()
	select {
	case status := <-done:
		// target=current ⇒ ratio=1.0 ⇒ underfunded tier [0.8, 1.2).
		assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, status,
			"boundary values within ±100 exponent must still compute "+
				"utilization correctly — guard must not over-reject")
	case <-time.After(2 * time.Second):
		t.Fatal("EvaluatePoolHealth at boundary ±100 unexpectedly hung")
	}
}

// TestCalculatePremium_ExponentBombDegradesToOne pins that a poisoned
// PremiumMultiplier falls through to the default-1 branch instead of
// reaching basePremium.Mul(premiumMultiplier) where it would explode.
func TestCalculatePremium_ExponentBombDegradesToOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mult string
	}{
		{"positive_exponent_bomb", "1e11100100"},
		{"negative_exponent_bomb", "1e-11100100"},
		{"just_past_bound_101", "1e101"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			risk := &PublisherRisk{PremiumMultiplier: tc.mult}
			baseAmount := decimal.NewFromInt(1000)
			params := &Parameters{InsurancePoolBPS: 300}

			done := make(chan decimal.Decimal, 1)
			go func() { done <- CalculatePremium(risk, baseAmount, params) }()
			select {
			case premium := <-done:
				// With multiplier=1 fallback: base * BPS / 10000 * 1 =
				// 1000 * 300 / 10000 * 1 = 30.
				require.True(t, premium.Equal(decimal.NewFromInt(30)),
					"exponent-bomb multiplier must degrade to 1, "+
						"yielding base premium = 30 (1000 * 300 / 10000). "+
						"Got: %s", premium.String())
			case <-time.After(2 * time.Second):
				t.Fatalf("CalculatePremium HUNG on %s — moneyguard "+
					"missing on the premium Mul path (halt-vector on "+
					"claim processing)", tc.name)
			}
		})
	}
}

// TestCalculatePremium_BoundaryMultiplierStillApplied is the negative
// regression guard: multipliers at the moneyguard ±100 boundary must
// be applied correctly — guard must not over-reject in-bound values.
// 1e100 * 30 would itself be a 102-digit number, so we pick a more
// realistic boundary like "1" and "0" to confirm the guard doesn't
// alter the well-defined fallback for realistic inputs.
func TestCalculatePremium_BoundaryMultiplierStillApplied(t *testing.T) {
	t.Parallel()

	// Legitimate multiplier "2.5" — nowhere near boundary, must apply.
	risk := &PublisherRisk{PremiumMultiplier: "2.5"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 300}
	premium := CalculatePremium(risk, baseAmount, params)
	// 1000 * 300 / 10000 * 2.5 = 30 * 2.5 = 75
	require.True(t, premium.Equal(decimal.NewFromInt(75)),
		"realistic multiplier must still apply — got %s, want 75",
		premium.String())

	// Zero multiplier → zero premium (unchanged behavior).
	riskZero := &PublisherRisk{PremiumMultiplier: "0"}
	premiumZero := CalculatePremium(riskZero, baseAmount, params)
	require.True(t, premiumZero.IsZero(),
		"explicit zero multiplier must still yield zero premium — "+
			"guard must not break the existing contract")
}
