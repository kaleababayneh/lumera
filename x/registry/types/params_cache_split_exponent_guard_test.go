//go:build cosmos

package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression guards for the exponent-bomb DoS on the x/registry
// governance parameter-validation path. Params.Validate runs inside
// MsgUpdateParams handling on EVERY validator for EVERY block that
// contains a parameter update — a symbolic exponent in any of these
// string decimal fields would expand shopspring's big.Int on the
// immediate next arithmetic op and halt consensus chain-wide.
//
// Unguarded parse+arithmetic chains closed by this sweep:
//
//   * params.go:~105 — Params.Validate → MinBondAmount
//       → downstream Mul/Sub/GreaterThan on bond/slashing paths
//   * params.go:validateCacheSplit → Origin/Serving/Router/Burn
//       → origin.Add(serving).Add(router).Add(burn)
//       → total.Sub(DecimalOne).Abs().GreaterThan(DecimalTolerance)
//       A single bomb in any of the four fields explodes the chain.
//   * params.go:validateMinBondAmount (ParamSetPair path) — same
//       shape as Params.Validate MinBondAmount; different call path.
//   * params.go:validateInsuranceTarget (ParamSetPair path) —
//       dec.GreaterThan(decimal.NewFromInt(1)) directly after parse.
//   * validations.go:~368 — BondRecord.Validate →
//       InsurancePremiumMultiplier (settlement-path Mul arithmetic)
//   * validations.go:~793-811 — SettlementRecord.Validate → LockedQuote,
//       ActualSettled, BurnAmount (refund/burn distribution Mul/Sub)
//
// shopspring.decimal.NewFromString("1e11100100") parses in O(1)
// (exponent stored symbolically) but every Add/Sub/Mul/Cmp forces
// big.Int alignment and expands to millions of digits — seconds to
// minutes per op, String hangs indefinitely. This is the SAME DoS
// class closed in sibling commits:
//
//   * 8438b6354 — x/router grpc_router MaxCost + deriveToolCost
//   * 25d34d734 — x/router cache.go OriginalCost read paths
//   * 5c237b056 — x/router/types central decimalFromString helper
//   * cbbaba3cb — x/router/keeper msg_server.go + quotes.go
//   * c1ec4b822 — strategy/execute BuildGroupReceipt LegState
//   * b21923578 — internet/manifest parseDecimal
//   * 35a96822d — router/publisher_http asDecimal
//
// Fix: insert moneyguard.IsSafeExponent check after each parse, before
// any arithmetic or comparison that would trigger big.Int expansion.

// TestValidateCacheSplit_ExponentBombRejected pins the
// validateCacheSplit guards. Each of the 4 fields (Origin, Serving,
// Router, Burn) is tested with a bomb value to confirm the guard
// rejects BEFORE the Add chain at params.go:244 explodes.
// Deadline-guarded so a regression surfaces as a visible timeout
// rather than a silent multi-minute hang.
func TestValidateCacheSplit_ExponentBombRejected(t *testing.T) {
	t.Parallel()

	validSplit := func() *CacheFeeSplit {
		return &CacheFeeSplit{Origin: "0.60", Serving: "0.35", Router: "0.04", Burn: "0.01"}
	}

	cases := []struct {
		name     string
		mutate   func(*CacheFeeSplit)
		wantSub  string
	}{
		{
			name:    "origin_bomb",
			mutate:  func(s *CacheFeeSplit) { s.Origin = "1e11100100" },
			wantSub: "origin magnitude out of range",
		},
		{
			name:    "serving_bomb",
			mutate:  func(s *CacheFeeSplit) { s.Serving = "1e11100100" },
			wantSub: "serving magnitude out of range",
		},
		{
			name:    "router_bomb",
			mutate:  func(s *CacheFeeSplit) { s.Router = "1e11100100" },
			wantSub: "router magnitude out of range",
		},
		{
			name:    "burn_bomb",
			mutate:  func(s *CacheFeeSplit) { s.Burn = "1e11100100" },
			wantSub: "burn magnitude out of range",
		},
		{
			name:    "just_past_bound_origin_101",
			mutate:  func(s *CacheFeeSplit) { s.Origin = "1e101" },
			wantSub: "origin magnitude out of range",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			split := validSplit()
			tc.mutate(split)

			done := make(chan error, 1)
			go func() { done <- validateCacheSplit(split) }()

			select {
			case err := <-done:
				require.Error(t, err,
					"validateCacheSplit must reject bomb in %q — pre-guard "+
						"the Add chain at params.go would expand big.Int "+
						"and halt block production on every validator.",
					tc.name)
				assert.Contains(t, err.Error(), tc.wantSub,
					"error must carry the field-specific sentinel; got: %v", err)
			case <-time.After(2 * time.Second):
				t.Fatalf("validateCacheSplit HUNG on bomb in %s — "+
					"moneyguard missing; this is a validator halt-vector "+
					"on the MsgUpdateParams governance path.", tc.name)
			}
		})
	}
}

// TestValidateCacheSplit_LegitimateInputsAccepted is the negative
// regression guard: the default cache split (0.60/0.35/0.04/0.01)
// and other realistic configurations must still validate.
func TestValidateCacheSplit_LegitimateInputsAccepted(t *testing.T) {
	t.Parallel()
	cases := []*CacheFeeSplit{
		{Origin: "0.60", Serving: "0.35", Router: "0.04", Burn: "0.01"},
		{Origin: "0.5", Serving: "0.5", Router: "0", Burn: "0"},
		{Origin: "0.25", Serving: "0.25", Router: "0.25", Burn: "0.25"},
		{Origin: "1.0", Serving: "0", Router: "0", Burn: "0"},
	}
	for i, split := range cases {
		split := split
		t.Run(splitDescribe(i, split), func(t *testing.T) {
			t.Parallel()
			require.NoError(t, validateCacheSplit(split),
				"realistic cache split must still validate — guard must "+
					"not over-reject in-bound values")
		})
	}
}

func splitDescribe(i int, s *CacheFeeSplit) string {
	return "split_" + s.Origin + "_" + s.Serving + "_" + s.Router + "_" + s.Burn
}

// TestValidateInsuranceTarget_ExponentBombRejected pins the
// ParamSetPair validator. The code does `dec.GreaterThan(...)`
// directly after parse, which is itself a big.Int bomb trigger.
func TestValidateInsuranceTarget_ExponentBombRejected(t *testing.T) {
	t.Parallel()
	done := make(chan error, 1)
	go func() { done <- validateInsuranceTarget("1e11100100") }()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insurance target utilization magnitude out of range")
	case <-time.After(2 * time.Second):
		t.Fatal("validateInsuranceTarget HUNG on bomb — the ParamSetPair " +
			"governance path is a halt-vector")
	}

	// Negative regression: legitimate values still pass.
	require.NoError(t, validateInsuranceTarget("0.3"))
	require.NoError(t, validateInsuranceTarget("1"))
	require.NoError(t, validateInsuranceTarget("0"))
}

// TestValidateMinBondAmount_ExponentBombRejected pins the
// ParamSetPair validator for MinBondAmount.
func TestValidateMinBondAmount_ExponentBombRejected(t *testing.T) {
	t.Parallel()
	done := make(chan error, 1)
	go func() { done <- validateMinBondAmount("1e11100100") }()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "min bond amount magnitude out of range")
	case <-time.After(2 * time.Second):
		t.Fatal("validateMinBondAmount HUNG on bomb")
	}

	// Negative regression.
	require.NoError(t, validateMinBondAmount("1000000"))
	require.NoError(t, validateMinBondAmount("0"))
}

// TestParamsValidate_MinBondAmountExponentBombRejected pins the
// top-level Params.Validate path for MinBondAmount (distinct from
// the ParamSetPair path). This is the path hit by the governance
// MsgUpdateParams handler.
func TestParamsValidate_MinBondAmountExponentBombRejected(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.MinBondAmount = "1e11100100"

	done := make(chan error, 1)
	go func() { done <- p.Validate() }()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "min bond amount magnitude out of range")
	case <-time.After(2 * time.Second):
		t.Fatal("Params.Validate HUNG on MinBondAmount bomb — " +
			"MsgUpdateParams halt-vector")
	}
}

// TestBondRecordValidate_InsurancePremiumMultiplierExponentBombRejected
// pins the BondRecord path. The multiplier flows into settlement-path
// Mul arithmetic; a bomb in this field would detonate during fee
// assessment on every validator.
func TestBondRecordValidate_InsurancePremiumMultiplierExponentBombRejected(t *testing.T) {
	t.Parallel()
	// Construct a minimally-valid BondRecord with a poisoned multiplier.
	b := validBondRecord(t)
	b.InsurancePremiumMultiplier = "1e11100100"

	done := make(chan error, 1)
	go func() { done <- b.Validate() }()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insurance premium multiplier magnitude out of range")
	case <-time.After(2 * time.Second):
		t.Fatal("BondRecord.Validate HUNG on InsurancePremiumMultiplier bomb")
	}
}

// TestSettlementRecordValidate_ExponentBombRejected pins each of
// the three SettlementRecord decimal fields (LockedQuote,
// ActualSettled, BurnAmount). The parsed values flow into refund /
// burn distribution arithmetic in the settlement finalization path.
func TestSettlementRecordValidate_ExponentBombRejected(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*SettlementRecord)
		wantSub string
	}{
		{
			name:    "locked_quote_bomb",
			mutate:  func(s *SettlementRecord) { s.LockedQuote = "1e11100100" },
			wantSub: "locked quote magnitude out of range",
		},
		{
			name:    "actual_settled_bomb",
			mutate:  func(s *SettlementRecord) { s.ActualSettled = "1e11100100" },
			wantSub: "actual settled magnitude out of range",
		},
		{
			name:    "burn_amount_bomb",
			mutate:  func(s *SettlementRecord) { s.BurnAmount = "1e11100100" },
			wantSub: "burn amount magnitude out of range",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := validSettlementRecord(t)
			tc.mutate(s)

			done := make(chan error, 1)
			go func() { done <- s.Validate() }()

			select {
			case err := <-done:
				require.Error(t, err,
					"SettlementRecord.Validate must reject %s — pre-guard "+
						"the parsed value flows into settlement Mul/Sub "+
						"arithmetic on every validator", tc.name)
				assert.Contains(t, err.Error(), tc.wantSub)
			case <-time.After(2 * time.Second):
				t.Fatalf("SettlementRecord.Validate HUNG on %s", tc.name)
			}
		})
	}
}
