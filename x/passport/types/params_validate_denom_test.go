//go:build cosmos

package types

import (
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
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
// unparseable or negative amounts must fail validation cleanly rather
// than reaching MinStakeCoin where the panic would surface through
// governance-tx recovery instead of as a clean error.
func TestParams_Validate_InvalidAmount(t *testing.T) {
	t.Parallel()
	cases := []string{"", "nota_number", "-1", "abc"}
	for _, amount := range cases {
		amount := amount
		t.Run(amount, func(t *testing.T) {
			t.Parallel()
			p := DefaultParams()
			p.MinStake.Amount = amount
			if err := p.Validate(); err == nil {
				t.Errorf("expected Validate error for malformed amount %q", amount)
			}
		})
	}
}

// TestCoinFromProto_NoPanicOnInvalidInputs documents the
// defense-in-depth layer: even if a malformed MinStake survives
// Params.Validate (old storage, forged state, etc.), CoinFromProto
// must not panic. It returns sdk.Coin{} (zero value) on any
// structural failure — callers are expected to check coin.IsValid()
// or Denom != "" before using the result.
func TestCoinFromProto_NoPanicOnInvalidInputs(t *testing.T) {
	t.Parallel()
	cases := []*v1beta1.Coin{
		nil,
		{Denom: "", Amount: "1"},
		{Denom: "UPPER", Amount: "1"},
		{Denom: "1bad", Amount: "1"},
		{Denom: "ulume", Amount: "-1"},
		{Denom: "ulume", Amount: "abc"},
	}
	for _, c := range cases {
		c := c
		name := "nil"
		if c != nil {
			name = c.Denom + "|" + c.Amount
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("CoinFromProto panicked on %+v: %v", c, r)
				}
			}()
			// Return value not asserted — the whole point is that the
			// call completes without panicking. Semantics of each
			// branch (zero vs. zero-amount coin) live in the docstring
			// and are already covered by the existing unit suite.
			_ = CoinFromProto(c)
		})
	}
}
