
package types

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression guards for the exponent-bomb DoS at the x/registry/types
// central READ helpers. SafeDecimalFromString, SafeDecimalFromStringWithError,
// and ValidateDecimalString are consumed 30+ times across the keeper
// (settlement.go, invariants.go, receipt.go, receipt_queue.go, bond.go,
// quality_rebate.go, params_conversion.go, verified_badge.go) to parse
// persisted decimal strings that then flow directly into arithmetic:
//
//   * settlement.go:832 — TotalRevenue.Add(SafeDecimalFromString(ActualSettled))
//   * settlement.go:846+856 — delta.Add on per-tool Revenue
//   * settlement.go:543-546 — revenue-split fractions (Origin/Serving/
//     Router/Burn) used in downstream Mul
//   * settlement.go:695+702 — target util + utilization ewma (Cmp/Div)
//   * invariants.go:133-150, 247-253 — settlement-record invariant Add/Sub
//     chain across {actual, burn, insurance, publisherServing,
//     publisherOrigin, routerGross, referrerShare, refund, lockedQuote}
//   * receipt.go:198-201 — Origin/Serving/Router/Burn totals
//   * bond.go:795 — per-stat revenue sum
//   * quality_rebate.go:296+332 — QualityScoreEwma read for EWMA Mul
//   * params_conversion.go:68 — InsuranceTargetUtil governance conversion
//   * verified_badge.go:315 — verified-badge param value
//
// shopspring.decimal.NewFromString parses "1e11100100" in O(1) (exponent
// stored symbolically), but every Add/Sub/Mul/Div/Cmp/String on the result
// expands big.Int by exponent-alignment to multi-million digits —
// measured 1.3s+ per op pre-guard, String hangs indefinitely.
//
// Reachability: this is the central settlement-path read helper. Writes
// are already moneyguard-gated at UsageReceipt.Validate /
// SettlementRecord.Validate / Pricing.Validate / Params.Validate /
// BondRecord.Validate (prior commits 25d34d734, 09cffa7b4, and existing
// guards). But defense-in-depth at the READ helper closes the
// write→poison→read→explode path for any future writer bug, migration
// error, or adversarial state injection that bypasses the write-time
// guard. Same pattern as 5c237b056 (x/router/types decimalFromString
// central helper).
//
// Fix: insert moneyguard.IsSafeExponent check after each NewFromString.
// SafeDecimalFromString degrades to zero (same contract as its existing
// err branch). SafeDecimalFromStringWithError + ValidateDecimalString
// surface field-level errors so callers know to reject.
//
// Sibling guards: 8438b6354 (grpc_router), 25d34d734 (cache),
// 5c237b056 (router types), cbbaba3cb (msg_server/quotes),
// 09cffa7b4 (registry params/validations), bf5be3a18 (insurance).

func TestSafeDecimalFromString_ExponentBombDegradesToZero(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"positive_exponent_bomb", "1e11100100"},
		{"negative_exponent_bomb", "1e-11100100"},
		{"just_past_bound_101", "1e101"},
		{"negative_exponent_101", "1e-101"},
		{"float_large_exponent", "1.5e500"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			done := make(chan decimal.Decimal, 1)
			go func() { done <- SafeDecimalFromString(tc.input) }()
			select {
			case got := <-done:
				assert.True(t, got.IsZero(),
					"SafeDecimalFromString(%q) must degrade exponent-bomb "+
						"to zero, matching the existing err-branch "+
						"behavior. Otherwise every caller (settlement.go, "+
						"invariants.go, receipt.go, bond.go, ...) feeds "+
						"the bomb into its next Add/Mul/Cmp and hangs "+
						"block production. Got: %s", tc.input, got.String())
			case <-time.After(2 * time.Second):
				t.Fatalf("SafeDecimalFromString(%q) HUNG — this is the "+
					"central settlement-path read helper and the hang is "+
					"a validator halt-vector on every block that "+
					"processes a poisoned field", tc.input)
			}
		})
	}
}

func TestSafeDecimalFromString_LegitimateInputsPreserved(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"empty_is_zero", ""},
		{"zero", "0"},
		{"one", "1"},
		{"typical_royalty_fraction", "0.05"},
		{"settlement_amount", "1000.123"},
		{"boundary_pos_100", "1e100"},
		{"boundary_neg_100", "1e-100"},
		{"inside_pos_99", "1e99"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SafeDecimalFromString(tc.input)
			if tc.input == "" {
				assert.True(t, got.IsZero(), "empty string must yield zero")
				return
			}
			expected, err := decimal.NewFromString(tc.input)
			require.NoError(t, err)
			assert.True(t, got.Equal(expected),
				"legitimate in-bound value %q must parse unchanged — "+
					"guard must not over-reject. got=%s want=%s",
				tc.input, got.String(), expected.String())
		})
	}
}

// TestSafeDecimalFromStringWithError_ExponentBombSurfacesError pins that
// the error-returning variant surfaces the exponent-range rejection so
// keeper code (params_conversion.go:68, verified_badge.go:315) can
// propagate the failure up through governance MsgUpdateParams handling.
func TestSafeDecimalFromStringWithError_ExponentBombSurfacesError(t *testing.T) {
	t.Parallel()
	done := make(chan struct {
		val decimal.Decimal
		err error
	}, 1)
	go func() {
		val, err := SafeDecimalFromStringWithError("1e11100100")
		done <- struct {
			val decimal.Decimal
			err error
		}{val, err}
	}()
	select {
	case r := <-done:
		require.Error(t, r.err,
			"SafeDecimalFromStringWithError must surface exponent-range "+
				"error so keeper callers on the governance path can "+
				"propagate the rejection up-stack")
		assert.Contains(t, r.err.Error(), "out of safe range")
		assert.True(t, r.val.IsZero(),
			"value must be zero when err is non-nil — matches existing "+
				"err-branch contract")
	case <-time.After(2 * time.Second):
		t.Fatal("SafeDecimalFromStringWithError HUNG on exponent-bomb — " +
			"the WithError variant bypasses the central guard")
	}

	// Negative regression: legitimate values still parse.
	ok, err := SafeDecimalFromStringWithError("1.5")
	require.NoError(t, err)
	require.True(t, ok.Equal(decimal.RequireFromString("1.5")))

	// Empty is still (zero, nil).
	emp, err := SafeDecimalFromStringWithError("")
	require.NoError(t, err)
	require.True(t, emp.IsZero())
}

// TestValidateDecimalString_ExponentBombSurfacesError pins that the
// validator returns a field-prefixed "magnitude out of safe range"
// error. Callers in keeper.go use the error message to build
// user-facing validation responses.
func TestValidateDecimalString_ExponentBombSurfacesError(t *testing.T) {
	t.Parallel()
	done := make(chan struct {
		val decimal.Decimal
		err error
	}, 1)
	go func() {
		val, err := ValidateDecimalString("1e11100100", "test_field")
		done <- struct {
			val decimal.Decimal
			err error
		}{val, err}
	}()
	select {
	case r := <-done:
		require.Error(t, r.err)
		assert.Contains(t, r.err.Error(), "invalid test_field",
			"error must carry the field-name prefix; got: %v", r.err)
		assert.Contains(t, r.err.Error(), "magnitude out of safe range",
			"error must carry the moneyguard sentinel so callers can "+
				"distinguish this failure from generic parse errors; "+
				"got: %v", r.err)
		assert.True(t, r.val.IsZero())
	case <-time.After(2 * time.Second):
		t.Fatal("ValidateDecimalString HUNG on exponent-bomb")
	}

	// Negative regression: legitimate values still validate.
	ok, err := ValidateDecimalString("0.5", "util")
	require.NoError(t, err)
	require.True(t, ok.Equal(decimal.RequireFromString("0.5")))

	// Empty still (zero, nil).
	emp, err := ValidateDecimalString("", "anything")
	require.NoError(t, err)
	require.True(t, emp.IsZero())
}
