package types

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DIRECT-test coverage for THREE package-private helpers in
// x/credits/types/msgs.go: parseAccAddress, mustAddr, coinFromProto.
//
// PORTED NOTE (gogoproto migration): in lumera_ai coinFromProto took a
// *basev1beta1.Coin with a string Amount, so it could fail on empty/non-
// numeric/decimal/scientific amount strings and on a nil pointer. After the
// migration coinFromProto takes a native sdk.Coin (value, math.Int amount):
// the string-parse failure modes and the nil-pointer "required" arm no longer
// exist (the "required"/absent semantics moved to nonNegativeCoinFromProto).
// Those impossible-to-construct cases are dropped; every remaining invariant
// (empty/invalid denom, field-name scoping, zero/negative rejection,
// trimming, strict-positive vs Safe-accepts-zero asymmetry) is preserved
// against the native API.

// ---- parseAccAddress ----

func TestParseAccAddress_EmptyReturnsError(t *testing.T) {
	t.Parallel()
	for _, empty := range []string{"", "   ", "\t\n"} {
		_, err := parseAccAddress(empty)
		require.Error(t, err,
			"empty/whitespace %q → error", empty)
		assert.Contains(t, err.Error(), "cannot be empty")
	}
}

func TestParseAccAddress_InvalidBech32ReturnsError(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"not-a-bech32-address",
		"cosmos",               // missing data
		"cosmos1invalid!chars", // invalid bech32 chars
		"bc1abc",               // wrong HRP but plausible bech32
	} {
		_, err := parseAccAddress(bad)
		assert.Error(t, err,
			"invalid bech32 %q → error from sdk.AccAddressFromBech32", bad)
	}
}

func TestParseAccAddress_ValidReturnsAddress(t *testing.T) {
	t.Parallel()
	valid := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	addr, err := parseAccAddress(valid)
	require.NoError(t, err)
	assert.NotNil(t, addr)
	assert.Equal(t, valid, addr.String(),
		"round-trip through AccAddress preserves the bech32 string")
}

func TestParseAccAddress_EmbeddedSpaceReturnsError(t *testing.T) {
	t.Parallel()
	bad := "cosmos1qypqxpq 9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	_, err := parseAccAddress(bad)
	assert.Error(t, err,
		"embedded space in address → error (TrimSpace only trims edges)")
}

// ---- mustAddr ----

func TestMustAddr_ValidDoesNotPanic(t *testing.T) {
	t.Parallel()
	valid := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	assert.NotPanics(t, func() {
		addr := mustAddr(valid)
		assert.Equal(t, valid, addr.String())
	})
}

// TestMustAddr_EmptyPanics pins the must* sibling: it panics on the exact
// input that parseAccAddress errors on.
func TestMustAddr_EmptyPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { _ = mustAddr("") },
		"mustAddr panics on empty address — msg_server MUST use "+
			"parseAccAddress for user input")
}

func TestMustAddr_InvalidBech32Panics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { _ = mustAddr("not-a-bech32") })
}

// ---- coinFromProto ----

// TestCoinFromProto_NilReturnsFieldScopedError pins that a zero-value coin
// (empty denom) produces a field-name-scoped error.
//
// PORTED: the nil-pointer "required" arm no longer exists; a zero-value
// sdk.Coin trips the empty-denom guard, which still carries the field name.
func TestCoinFromProto_NilReturnsFieldScopedError(t *testing.T) {
	t.Parallel()
	_, err := coinFromProto(sdk.Coin{}, "amount")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount",
		"error message echoes field name 'amount' for per-field triage")
	assert.Contains(t, err.Error(), "denom cannot be empty")
}

func TestCoinFromProto_EmptyDenomReturnsFieldScopedError(t *testing.T) {
	t.Parallel()
	for _, denom := range []string{"", "   ", "\t"} {
		c := sdk.Coin{Denom: denom, Amount: math.NewInt(100)}
		_, err := coinFromProto(c, "locked_amount")
		require.Error(t, err,
			"empty/whitespace denom %q → error", denom)
		assert.Contains(t, err.Error(), "locked_amount",
			"error message scoped by field name for operator triage")
		assert.Contains(t, err.Error(), "denom cannot be empty")
	}
}

// TestCoinFromProto_InvalidDenomFormatReturnsError pins the ValidateDenom
// pre-check (msgs.go:89) — without it, sdk.NewCoin would panic the
// msg-validation pipeline on a wire-supplied denom like "1bad".
func TestCoinFromProto_InvalidDenomFormatReturnsError(t *testing.T) {
	t.Parallel()
	for _, badDenom := range []string{
		"1bad",      // starts with digit — rejected by cosmos denom regex
		"has space", // space rejected
		"!@#$",
		"a", // too short
	} {
		c := sdk.Coin{Denom: badDenom, Amount: math.NewInt(100)}
		_, err := coinFromProto(c, "amount")
		require.Error(t, err,
			"invalid denom %q → error (NOT panic)", badDenom)
		assert.Contains(t, err.Error(), "denom is invalid")
	}
}

// TestCoinFromProto_ZeroAmountRejected pins the asymmetry vs CoinFromProtoSafe:
// coinFromProto requires STRICTLY POSITIVE, CoinFromProtoSafe accepts zero.
func TestCoinFromProto_ZeroAmountRejected(t *testing.T) {
	t.Parallel()
	c := sdk.Coin{Denom: "ulac", Amount: math.ZeroInt()}
	_, err := coinFromProto(c, "amount")
	require.Error(t, err,
		"CRITICAL — zero amount rejected (msg-invariant: can't lock/swap zero)")
	assert.Contains(t, err.Error(), "must be positive",
		"error mentions 'must be positive' (distinct from 'amount invalid')")
}

// TestCoinFromProto_NilAmountRejected pins that a nil math.Int (unset amount)
// is rejected by the IsNil() guard.
func TestCoinFromProto_NilAmountRejected(t *testing.T) {
	t.Parallel()
	c := sdk.Coin{Denom: "ulac"} // Amount is a nil math.Int
	_, err := coinFromProto(c, "amount")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestCoinFromProto_NegativeAmountRejected(t *testing.T) {
	t.Parallel()
	for _, neg := range []int64{-1, -100, -1000000} {
		c := sdk.Coin{Denom: "ulac", Amount: math.NewInt(neg)}
		_, err := coinFromProto(c, "amount")
		require.Error(t, err,
			"negative amount %d → error (IsPositive guard)", neg)
	}
}

func TestCoinFromProto_ValidPositive(t *testing.T) {
	t.Parallel()
	c := sdk.Coin{Denom: "ulac", Amount: math.NewInt(1000)}
	coin, err := coinFromProto(c, "amount")
	require.NoError(t, err)
	assert.Equal(t, "ulac", coin.Denom)
	assert.Equal(t, "1000", coin.Amount.String())
}

// TestCoinFromProto_DenomAndAmountTrimmed pins that leading/trailing
// whitespace in denom is stripped before ValidateDenom (msgs.go:81).
func TestCoinFromProto_DenomAndAmountTrimmed(t *testing.T) {
	t.Parallel()
	c := sdk.Coin{Denom: "  ulac  ", Amount: math.NewInt(100)}
	coin, err := coinFromProto(c, "amount")
	require.NoError(t, err)
	assert.Equal(t, "ulac", coin.Denom,
		"denom TrimSpace'd before ValidateDenom")
	assert.Equal(t, "100", coin.Amount.String())
}

// TestCoinFromProto_FieldNameInEveryErrorPath pins that the field name reaches
// every error arm so ValidateBasic error composition stays uniform.
func TestCoinFromProto_FieldNameInEveryErrorPath(t *testing.T) {
	t.Parallel()
	fieldName := "custom_field_name_for_triage"
	cases := []sdk.Coin{
		{},                                     // empty denom
		{Denom: "1bad", Amount: math.NewInt(100)},   // invalid denom format
		{Denom: "ulac", Amount: math.ZeroInt()},     // positivity
		{Denom: "ulac", Amount: math.NewInt(-1)},    // positivity
		{Denom: "ulac"},                             // nil amount
	}
	for _, c := range cases {
		_, err := coinFromProto(c, fieldName)
		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), fieldName),
			"error from input %+v contains field name %q", c, fieldName)
	}
}

// ---- Cross-sibling asymmetry anchor ----

// TestCoinFromProtoHelpers_ZeroAmountDivergesBetweenSiblings pins that the
// same zero-amount input produces different outcomes:
//
//	coinFromProto:     ERROR (positivity required)
//	CoinFromProtoSafe: SUCCESS (zero is a valid sdk.Coin)
func TestCoinFromProtoHelpers_ZeroAmountDivergesBetweenSiblings(t *testing.T) {
	t.Parallel()
	zeroCoin := sdk.Coin{Denom: "ulac", Amount: math.ZeroInt()}

	// Safe variant accepts zero.
	_, errSafe := CoinFromProtoSafe(zeroCoin)
	assert.NoError(t, errSafe,
		"CoinFromProtoSafe ACCEPTS zero amount (generic sdk.Coin contract)")

	// Package-private variant rejects zero.
	_, errStrict := coinFromProto(zeroCoin, "amount")
	assert.Error(t, errStrict,
		"coinFromProto REJECTS zero amount (msg-invariant)")
}
