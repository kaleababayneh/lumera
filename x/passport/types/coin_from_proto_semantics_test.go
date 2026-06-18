//go:build cosmos

package types

import (
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"
)

// Semantic pinning for the CoinFromProto / CoinsFromProto defensive
// conversion helpers. The existing types_test.go covers the happy
// path (nil, valid, basic invalid amount) and
// params_validate_denom_test.go covers no-panic. This file pins
// the *subtle* return-value semantics that downstream callers rely
// on:
//
//   CoinFromProto
//     • invalid denom → sdk.Coin{} (BOTH denom AND amount zero)
//     • valid denom + unparseable amount → zero-amount coin
//       WITH denom preserved (caller uses coin.Denom != "")
//     • valid denom + negative amount → sdk.Coin{} (denom NOT
//       preserved; fully-zero result)
//     • empty denom is treated as invalid by the ValidateDenom gate
//       (returns fully-zero)
//
//   CoinsFromProto
//     • filters nil entries
//     • filters invalid-denom entries (CoinFromProto returns
//       sdk.Coin{} which sdk.Coins.Add tolerates without panic
//       because the zero coin is "empty" from Add's perspective)
//     • aggregates duplicate denoms
//     • preserves canonical sdk.Coins ordering (alphabetical denom)
//
// The denom-preservation distinction (unparseable vs. negative)
// matters because downstream callers in x/passport/keeper check
// coin.Denom before coin.Amount — if a future refactor unified both
// branches to "fully-zero", the keeper would silently skip work on
// the unparseable-amount branch that it currently processes.

// ---------------------------------------------------------------------------
// CoinFromProto
// ---------------------------------------------------------------------------

func TestCoinFromProto_InvalidDenom_FullyZero(t *testing.T) {
	// Invalid denom → fully-zero sdk.Coin (no denom preserved).
	// Distinguishing from the unparseable-amount branch below.
	p := &v1beta1.Coin{Denom: "1bad", Amount: "100"}
	c := CoinFromProto(p)
	if c.Denom != "" {
		t.Errorf("invalid-denom result: denom=%q; want empty", c.Denom)
	}
}

func TestCoinFromProto_EmptyDenom_FullyZero(t *testing.T) {
	// Empty denom also fails ValidateDenom.
	p := &v1beta1.Coin{Denom: "", Amount: "100"}
	c := CoinFromProto(p)
	if c.Denom != "" {
		t.Errorf("empty-denom result: denom=%q; want empty", c.Denom)
	}
}

func TestCoinFromProto_UnparseableAmount_DenomPreserved(t *testing.T) {
	// Unparseable amount with a VALID denom → zero-amount coin
	// WITH denom preserved. Regression guard: a refactor that
	// unified this branch with invalid-denom (returning sdk.Coin{})
	// would change the semantic downstream callers depend on.
	p := &v1beta1.Coin{Denom: "ulume", Amount: "garbage"}
	c := CoinFromProto(p)
	if c.Denom != "ulume" {
		t.Errorf("unparseable-amount denom=%q; want 'ulume' (denom must be preserved)", c.Denom)
	}
	if !c.Amount.IsZero() {
		t.Errorf("unparseable-amount should zero Amount; got %s", c.Amount)
	}
}

func TestCoinFromProto_NegativeAmount_FullyZero(t *testing.T) {
	// Negative amount (but parseable) → fully-zero. The code
	// explicitly takes a different branch for negatives than for
	// unparseable strings; pin that asymmetry.
	p := &v1beta1.Coin{Denom: "ulume", Amount: "-1"}
	c := CoinFromProto(p)
	if c.Denom != "" {
		t.Errorf("negative-amount denom=%q; want empty (fully-zero)", c.Denom)
	}
}

func TestCoinFromProto_ZeroAmount_Passthrough(t *testing.T) {
	// Amount "0" is a valid parseable non-negative integer; the
	// helper should return a zero-amount coin WITH denom preserved,
	// not treat it as an error.
	p := &v1beta1.Coin{Denom: "ulume", Amount: "0"}
	c := CoinFromProto(p)
	if c.Denom != "ulume" {
		t.Errorf("zero-amount denom=%q; want 'ulume'", c.Denom)
	}
	if !c.Amount.Equal(math.ZeroInt()) {
		t.Errorf("zero-amount Amount=%s; want 0", c.Amount)
	}
}

// ---------------------------------------------------------------------------
// CoinsFromProto
// ---------------------------------------------------------------------------

func TestCoinsFromProto_FiltersInvalidDenomEntry(t *testing.T) {
	// Entry with invalid denom produces sdk.Coin{} from CoinFromProto;
	// sdk.Coins.Add folds that into an empty coin (no-op). The valid
	// entries survive.
	input := []*v1beta1.Coin{
		{Denom: "1bad", Amount: "100"},
		{Denom: "ulume", Amount: "50"},
		{Denom: "ulac", Amount: "25"},
	}
	c := CoinsFromProto(input)
	if len(c) != 2 {
		t.Fatalf("expected 2 coins after invalid-denom filter, got %d: %s", len(c), c)
	}
	for _, coin := range c {
		if coin.Denom == "1bad" {
			t.Errorf("invalid-denom entry leaked through: %s", coin)
		}
	}
}

func TestCoinsFromProto_FiltersNegativeAmountEntry(t *testing.T) {
	// Similarly for negative-amount entries.
	input := []*v1beta1.Coin{
		{Denom: "ulume", Amount: "-5"},
		{Denom: "ulume", Amount: "50"},
	}
	c := CoinsFromProto(input)
	if len(c) != 1 {
		t.Fatalf("expected 1 coin (negative filtered), got %d: %s", len(c), c)
	}
	if !c[0].Amount.Equal(math.NewInt(50)) {
		t.Errorf("amount=%s; want 50 (negative entry should not affect sum)", c[0].Amount)
	}
}

func TestCoinsFromProto_DuplicateDenomAggregates(t *testing.T) {
	// Two entries with the same denom should be summed via sdk.Coins.Add.
	input := []*v1beta1.Coin{
		{Denom: "ulume", Amount: "100"},
		{Denom: "ulume", Amount: "50"},
	}
	c := CoinsFromProto(input)
	if len(c) != 1 {
		t.Fatalf("expected 1 aggregated coin, got %d", len(c))
	}
	if !c[0].Amount.Equal(math.NewInt(150)) {
		t.Errorf("aggregate amount=%s; want 150", c[0].Amount)
	}
}

func TestCoinsFromProto_AlphabeticalOrder(t *testing.T) {
	// sdk.Coins canonicalizes to alphabetical denom order via Add.
	input := []*v1beta1.Coin{
		{Denom: "zzz", Amount: "1"},
		{Denom: "aaa", Amount: "2"},
		{Denom: "mmm", Amount: "3"},
	}
	c := CoinsFromProto(input)
	if len(c) != 3 {
		t.Fatalf("expected 3 coins, got %d", len(c))
	}
	want := []string{"aaa", "mmm", "zzz"}
	for i, denom := range want {
		if c[i].Denom != denom {
			t.Errorf("position %d: denom=%q; want %q", i, c[i].Denom, denom)
		}
	}
}
