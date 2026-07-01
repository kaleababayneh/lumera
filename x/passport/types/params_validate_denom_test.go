package types

import (
	"testing"

	sdkmath "cosmossdk.io/math"
)

// TestParams_Validate_InvalidDenomFormat pins the hardening landed in
// Params.Validate: a denom that passes the non-empty check but fails
// sdk.ValidateDenom's format rules (uppercase, leading digit, etc.)
// must be rejected here, not allowed to reach MinStakeCoin where the
// underlying sdk.NewCoin would panic. Closes lumera_ai-oyz0v.
func TestParams_Validate_InvalidDenomFormat(t *testing.T) {
	t.Parallel()
	// sdk.ValidateDenom rejects leading digits, special characters,
	// and denoms shorter than the minimum length. Uppercase letters
	// are accepted by the current SDK regex (^[a-zA-Z]...), so they
	// are NOT in this list.
	cases := []string{"1bad", "!bad", " spaced", "ab", "a/b?c"}
	for _, denom := range cases {
		denom := denom
		t.Run(denom, func(t *testing.T) {
			t.Parallel()
			p := DefaultParams()
			p.MinStake.Denom = denom
			if err := p.Validate(); err == nil {
				t.Errorf("expected Validate error for malformed denom %q", denom)
			}
		})
	}
}

// TestParams_Validate_InvalidAmount pins the companion amount check:
// nil (unset) or negative amounts must fail validation cleanly rather
// than reaching MinStakeCoin where the panic would surface through
// governance-tx recovery instead of as a clean error.
//
// MinStake.Amount is now a math.Int value, so the historical
// unparseable-string cases ("nota_number", "abc", "") can no longer be
// constructed — a math.Int is either nil (unset) or a valid integer.
// Those invalid-string sub-cases are therefore obsolete; the
// representable invalid amounts (nil and negative) are pinned here.
func TestParams_Validate_InvalidAmount(t *testing.T) {
	t.Parallel()
	cases := map[string]sdkmath.Int{
		"nil":      {},                 // unset amount
		"negative": sdkmath.NewInt(-1), // negative amount
	}
	for name, amount := range cases {
		name, amount := name, amount
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := DefaultParams()
			p.MinStake.Amount = amount
			if err := p.Validate(); err == nil {
				t.Errorf("expected Validate error for invalid amount %q", name)
			}
		})
	}
}
