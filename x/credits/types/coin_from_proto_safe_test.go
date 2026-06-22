package types

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DIRECT-test coverage for CoinFromProtoSafe (proto_helpers.go:29-40) — the
// user-input-safe variant of the Coin validation shim.
//
// PORTED NOTE (gogoproto migration): in lumera_ai the Coin fields were a
// separate protobuf-go wire type with string Amount, so CoinFromProtoSafe
// parsed the decimal string and could fail on non-numeric/decimal/scientific
// input. After the gogoproto migration the Coin field IS a native sdk.Coin
// (Amount already a math.Int), so the string-parse failure modes no longer
// exist. The function now validates: nil Amount → explicit zero; bad denom →
// error; negative amount → error. Tests that exercised string-parse errors
// are dropped (impossible to construct now); the remaining security-critical
// invariants (denom/negative guards, panic-vs-error sibling contract) are
// preserved against the native API.

// TestCoinFromProtoSafe_NilReturnsZeroCoinNoError pins the nil-Amount fast
// path: a zero-value coin (nil math.Int) returns a zero coin with an
// initialized (math.ZeroInt) Amount, not a leaked nil math.Int.
func TestCoinFromProtoSafe_NilReturnsZeroCoinNoError(t *testing.T) {
	t.Parallel()
	c, err := CoinFromProtoSafe(sdk.Coin{})
	require.NoError(t, err,
		"zero-value coin (nil Amount) → no error, treated as explicit zero")
	assert.Equal(t, "", c.Denom,
		"zero Denom for nil-Amount input")
	assert.False(t, c.Amount.IsNil(),
		"Amount is INITIALIZED (math.ZeroInt), not nil — a refactor "+
			"returning sdk.Coin{} directly would leave Amount as a nil "+
			"math.Int, panicking in downstream IsPositive()/Add().")
	assert.True(t, c.Amount.IsZero())
}

// TestCoinFromProtoSafe_ValidInputPassesThrough pins the happy path.
func TestCoinFromProtoSafe_ValidInputPassesThrough(t *testing.T) {
	t.Parallel()
	c, err := CoinFromProtoSafe(sdk.NewCoin("ulac", math.NewInt(1234)))
	require.NoError(t, err)
	assert.Equal(t, "ulac", c.Denom)
	assert.True(t, c.Amount.Equal(math.NewInt(1234)))
}

// TestCoinFromProtoSafe_ZeroAmountAccepted pins that an explicit zero amount
// is valid.
func TestCoinFromProtoSafe_ZeroAmountAccepted(t *testing.T) {
	t.Parallel()
	c, err := CoinFromProtoSafe(sdk.NewCoin("ulac", math.ZeroInt()))
	require.NoError(t, err,
		"explicit zero amount accepted")
	assert.True(t, c.Amount.IsZero())
}

// TestCoinFromProtoSafe_LargeAmountAccepted pins that very large amounts pass.
func TestCoinFromProtoSafe_LargeAmountAccepted(t *testing.T) {
	t.Parallel()
	big, _ := math.NewIntFromString("18446744073709551615")
	c, err := CoinFromProtoSafe(sdk.NewCoin("ulac", big))
	require.NoError(t, err)
	assert.Equal(t, "ulac", c.Denom)
	assert.True(t, c.Amount.Equal(big))
}

// TestCoinFromProtoSafe_InvalidDenomReturnsError pins the denom guard at
// proto_helpers.go:33-35: a malformed denom returns an error, not a panic.
func TestCoinFromProtoSafe_InvalidDenomReturnsError(t *testing.T) {
	t.Parallel()
	for _, badDenom := range []string{
		"has space",
		"!@#$",
		"a", // too short (< 3 chars in default cosmos denom regex)
	} {
		// Construct via struct literal to bypass sdk.NewCoin's own validation.
		c := sdk.Coin{Denom: badDenom, Amount: math.NewInt(100)}
		_, err := CoinFromProtoSafe(c)
		require.Error(t, err,
			"malformed denom %q returns error", badDenom)
		assert.Contains(t, err.Error(), "invalid coin denom")
	}
}

// TestCoinFromProtoSafe_NegativeAmountReturnsError pins the negative guard at
// proto_helpers.go:36-38: a negative amount returns an error, not a panic.
func TestCoinFromProtoSafe_NegativeAmountReturnsError(t *testing.T) {
	t.Parallel()
	for _, neg := range []int64{-1, -100, -1000000} {
		c := sdk.Coin{Denom: "ulac", Amount: math.NewInt(neg)}
		_, err := CoinFromProtoSafe(c)
		require.Error(t, err,
			"negative amount %d returns error, not panic", neg)
		assert.Contains(t, err.Error(), "negative coin amount")
	}
}

// TestCoinFromProtoSafe_DenomCheckBeforeNegative pins guard ordering: denom
// validation runs before the negative-amount check.
func TestCoinFromProtoSafe_DenomCheckBeforeNegative(t *testing.T) {
	t.Parallel()
	// Both invalid: bad denom AND negative amount. Denom error fires first.
	c := sdk.Coin{Denom: "", Amount: math.NewInt(-100)}
	_, err := CoinFromProtoSafe(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid coin denom")
	assert.NotContains(t, err.Error(), "negative",
		"denom guard short-circuits before the negative-amount guard")
}

// TestCoinFromProto_PanicsOnInvalidInputPerContract is the cross-sibling
// anchor: CoinFromProto panics on the same inputs CoinFromProtoSafe rejects.
func TestCoinFromProto_PanicsOnInvalidInputPerContract(t *testing.T) {
	t.Parallel()
	// Invalid denom → panic.
	assert.Panics(t, func() {
		_ = CoinFromProto(sdk.Coin{Denom: "", Amount: math.NewInt(100)})
	}, "CoinFromProto panics on bad denom — msg_server MUST use the Safe "+
		"variant for user input")

	// Negative amount → panic.
	assert.Panics(t, func() {
		_ = CoinFromProto(sdk.Coin{Denom: "ulac", Amount: math.NewInt(-1)})
	})

	// nil-Amount input is HANDLED (zero-coin, no panic) for both helpers.
	assert.NotPanics(t, func() {
		c := CoinFromProto(sdk.Coin{})
		assert.True(t, c.Amount.IsZero())
	}, "nil-Amount → zero-coin for BOTH helpers (no panic)")
}

// TestCoinFromProtoSafe_RoundTripViaCoinToProto pins the lossless round-trip:
// CoinToProto → CoinFromProtoSafe reproduces the original coin.
func TestCoinFromProtoSafe_RoundTripViaCoinToProto(t *testing.T) {
	t.Parallel()
	for _, orig := range []sdk.Coin{
		sdk.NewInt64Coin("ulac", 0),
		sdk.NewInt64Coin("ulac", 1),
		sdk.NewInt64Coin("ulac", 1_000_000),
		sdk.NewCoin("ulume", math.NewInt(42)),
	} {
		proto := CoinToProto(orig)
		back, err := CoinFromProtoSafe(proto)
		require.NoError(t, err)
		assert.True(t, orig.Equal(back),
			"round trip preserves coin %v", orig)
	}
}
