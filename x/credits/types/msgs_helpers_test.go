//go:build cosmos

package types

import (
	"strings"
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes DIRECT-test coverage for THREE package-
// private helpers in x/credits/types/msgs.go that had ZERO
// direct test coverage prior (the published sites only reach
// them indirectly via Msg.ValidateBasic round-trips):
//
//   - parseAccAddress  (:31-36) — Bech32 validator with
//                                 trim-empty guard
//   - mustAddr         (:38-44) — panic-on-error wrapper
//   - coinFromProto    (:46-70) — FIELD-NAME-SCOPED coin
//                                 parser with zero-rejection
//                                 semantic
//
// Scan-angle #5 (sibling-pattern pinning with INTENTIONAL
// asymmetry) applies in TWO places:
//
// (A) parseAccAddress vs mustAddr: the error-path variant
//     (parseAccAddress) is for user-supplied input; mustAddr
//     is the panic-on-invariant-violation wrapper. Parallels
//     the CoinFromProtoSafe / CoinFromProto asymmetry in
//     proto_helpers.go — msg_server MUST use parseAccAddress
//     for wire input.
//
// (B) coinFromProto vs CoinFromProtoSafe: BOTH return
//     (coin, err) for bad input, but they DIVERGE on:
//
//        coinFromProto:      rejects ZERO amount (requires
//                            IsPositive) — msg-field invariant
//        CoinFromProtoSafe:  ACCEPTS zero amount (sdk.NewCoin
//                            contract) — generic safe parser
//
//     A refactor unifying the two would either (a) make
//     msg validation accept zero-amount coins (breaking the
//     lock/settle invariants where a zero lock is meaningless)
//     or (b) reject zero in generic coin parsing (breaking
//     CACRoyaltyRecord fields that MAY be zero-coin).
//
//     Also: coinFromProto's error messages include the
//     FIELD NAME for operator triage — a scan-angle #3
//     hidden-secondary-return contract that ValidateBasic
//     error composition relies on.
//
// Scan-angle #6 (security-critical invariants tested only at
// happy path) applies to coinFromProto: in-code comment at
// :54-57 explicitly documents the guard rationale — "sdk.
// NewCoin panics on denom that's non-empty but fails
// sdk.ValidateDenom's format rules... so check format
// explicitly." A refactor skipping the ValidateDenom call
// would let wire-supplied denoms like "1bad" panic the
// msg-validation pipeline.

// ---- parseAccAddress ----

// TestParseAccAddress_EmptyReturnsError pins :32-34.
func TestParseAccAddress_EmptyReturnsError(t *testing.T) {
	t.Parallel()
	for _, empty := range []string{"", "   ", "\t\n"} {
		_, err := parseAccAddress(empty)
		require.Error(t, err,
			"empty/whitespace %q → error. Pins :32-34 TrimSpace "+
				"+ empty-check: a refactor dropping this would "+
				"let sdk.AccAddressFromBech32 run on empty input, "+
				"producing a cryptic 'empty address' error from "+
				"deep inside bech32 decoding instead of the "+
				"operator-friendly 'address cannot be empty'.", empty)
		assert.Contains(t, err.Error(), "cannot be empty")
	}
}

// TestParseAccAddress_InvalidBech32ReturnsError pins :35 —
// malformed bech32 returns the sdk error.
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

// TestParseAccAddress_ValidReturnsAddress pins the happy path.
func TestParseAccAddress_ValidReturnsAddress(t *testing.T) {
	t.Parallel()
	valid := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	addr, err := parseAccAddress(valid)
	require.NoError(t, err)
	assert.NotNil(t, addr)
	assert.Equal(t, valid, addr.String(),
		"round-trip through AccAddress preserves the bech32 string")
}

// TestParseAccAddress_NoWhitespaceStrippingInsideAddress pins
// that TrimSpace only handles LEADING/TRAILING whitespace —
// embedded spaces produce a bech32 decode error, NOT a silent
// strip.
func TestParseAccAddress_EmbeddedSpaceReturnsError(t *testing.T) {
	t.Parallel()
	bad := "cosmos1qypqxpq 9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	_, err := parseAccAddress(bad)
	assert.Error(t, err,
		"embedded space in address → error (TrimSpace only trims "+
			"edges, not internal whitespace)")
}

// ---- mustAddr ----

// TestMustAddr_ValidDoesNotPanic pins the happy path.
func TestMustAddr_ValidDoesNotPanic(t *testing.T) {
	t.Parallel()
	valid := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	assert.NotPanics(t, func() {
		addr := mustAddr(valid)
		assert.Equal(t, valid, addr.String())
	})
}

// TestMustAddr_EmptyPanics is the scan-angle #5 CROSS-SIBLING
// anchor: the must* variant PANICS on the exact input that
// parseAccAddress errors on.
func TestMustAddr_EmptyPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { _ = mustAddr("") },
		"mustAddr panics on empty address. Pins the scan-angle "+
			"#5 sibling contract: msg_server code paths MUST use "+
			"parseAccAddress (error-returning) for user input; "+
			"mustAddr is reserved for invariants that MUST hold by "+
			"construction. A refactor making msg_server use "+
			"mustAddr directly would crash the handler through "+
			"baseapp recover on every malformed bech32 input.")
}

// TestMustAddr_InvalidBech32Panics pins the sibling path for
// invalid (not just empty) addresses.
func TestMustAddr_InvalidBech32Panics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { _ = mustAddr("not-a-bech32") })
}

// ---- coinFromProto ----

// TestCoinFromProto_NilReturnsFieldScopedError pins :47-49.
// The CRITICAL scan-angle #3 anchor: the error message
// includes the caller-supplied field name for operator
// triage.
func TestCoinFromProto_NilReturnsFieldScopedError(t *testing.T) {
	t.Parallel()
	_, err := coinFromProto(nil, "amount")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount",
		"error message echoes field name 'amount'. Pins :48 "+
			"scan-angle #3 hidden-secondary-return: a refactor "+
			"that dropped the field param would produce identical "+
			"'is required' errors for every field, breaking "+
			"ValidateBasic's per-field error composition used in "+
			"operator-facing error UI.")
	assert.Contains(t, err.Error(), "required")
}

// TestCoinFromProto_EmptyDenomReturnsFieldScopedError pins
// :50-53.
func TestCoinFromProto_EmptyDenomReturnsFieldScopedError(t *testing.T) {
	t.Parallel()
	for _, denom := range []string{"", "   ", "\t"} {
		c := &v1beta1.Coin{Denom: denom, Amount: "100"}
		_, err := coinFromProto(c, "locked_amount")
		require.Error(t, err,
			"empty/whitespace denom %q → error", denom)
		assert.Contains(t, err.Error(), "locked_amount",
			"error message scoped by field name for operator triage")
		assert.Contains(t, err.Error(), "denom cannot be empty")
	}
}

// TestCoinFromProto_InvalidDenomFormatReturnsError is the
// scan-angle #6 anchor. In-code comment at :54-57 warns:
// "sdk.NewCoin panics on denom that's non-empty but fails
// sdk.ValidateDenom's format rules... so check format
// explicitly." A regression would panic the msg-validation
// pipeline on wire-supplied denoms like "1bad".
func TestCoinFromProto_InvalidDenomFormatReturnsError(t *testing.T) {
	t.Parallel()
	for _, badDenom := range []string{
		"1bad",      // starts with digit — rejected by cosmos denom regex
		"has space", // space rejected
		"!@#$",
		"a", // too short
	} {
		c := &v1beta1.Coin{Denom: badDenom, Amount: "100"}
		_, err := coinFromProto(c, "amount")
		require.Error(t, err,
			"invalid denom %q → error (NOT panic). Pins :58-60 "+
				"ValidateDenom pre-check. Critical per in-code "+
				"comment :54-57: without this check, the same "+
				"input would panic sdk.NewCoin and crash the "+
				"msg-validation pipeline through baseapp recover.",
			badDenom)
		assert.Contains(t, err.Error(), "denom is invalid")
	}
}

// TestCoinFromProto_EmptyAmountReturnsError pins :61-64.
func TestCoinFromProto_EmptyAmountReturnsError(t *testing.T) {
	t.Parallel()
	for _, amount := range []string{"", "   "} {
		c := &v1beta1.Coin{Denom: "ulac", Amount: amount}
		_, err := coinFromProto(c, "amount")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "amount cannot be empty")
	}
}

// TestCoinFromProto_InvalidAmountStringReturnsError pins
// :65-68 — the parse guard.
func TestCoinFromProto_InvalidAmountStringReturnsError(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"not-a-number",
		"1.5",  // decimals not allowed
		"1e10", // scientific notation not accepted
	} {
		c := &v1beta1.Coin{Denom: "ulac", Amount: bad}
		_, err := coinFromProto(c, "amount")
		require.Error(t, err,
			"invalid amount %q → error", bad)
		assert.Contains(t, err.Error(), "must be positive",
			"error mentions 'must be positive' (the combined parse/"+
				"positivity error)")
	}
}

// TestCoinFromProto_ZeroAmountRejected is the scan-angle #5
// ASYMMETRY anchor vs CoinFromProtoSafe: coinFromProto
// requires STRICTLY POSITIVE, while CoinFromProtoSafe
// accepts zero.
func TestCoinFromProto_ZeroAmountRejected(t *testing.T) {
	t.Parallel()
	c := &v1beta1.Coin{Denom: "ulac", Amount: "0"}
	_, err := coinFromProto(c, "amount")
	require.Error(t, err,
		"CRITICAL — zero amount rejected. Pins the scan-angle "+
			"#5 asymmetry with CoinFromProtoSafe (proto_helpers.go) "+
			"which ACCEPTS zero. This helper is used by "+
			"MsgLockCredits/MsgSettleCredits/MsgSwap ValidateBasic "+
			"where a zero amount is meaningless (can't lock or "+
			"swap zero). A refactor relaxing to >= 0 would let "+
			"nonsensical zero-coin messages reach the keeper.")
	assert.Contains(t, err.Error(), "must be positive",
		"error mentions 'must be positive' to signal the "+
			"positivity requirement (distinct from 'amount invalid')")
}

// TestCoinFromProto_NegativeAmountRejected pins the negative
// path (also via the IsPositive guard at :66).
func TestCoinFromProto_NegativeAmountRejected(t *testing.T) {
	t.Parallel()
	for _, neg := range []string{"-1", "-100", "-1000000"} {
		c := &v1beta1.Coin{Denom: "ulac", Amount: neg}
		_, err := coinFromProto(c, "amount")
		require.Error(t, err,
			"negative amount %q → error (IsPositive guard)", neg)
	}
}

// TestCoinFromProto_ValidPositive pins the happy path.
func TestCoinFromProto_ValidPositive(t *testing.T) {
	t.Parallel()
	c := &v1beta1.Coin{Denom: "ulac", Amount: "1000"}
	coin, err := coinFromProto(c, "amount")
	require.NoError(t, err)
	assert.Equal(t, "ulac", coin.Denom)
	assert.Equal(t, "1000", coin.Amount.String())
}

// TestCoinFromProto_DenomTrimmed pins that leading/trailing
// whitespace in denom is stripped (via :50 TrimSpace).
func TestCoinFromProto_DenomAndAmountTrimmed(t *testing.T) {
	t.Parallel()
	c := &v1beta1.Coin{Denom: "  ulac  ", Amount: "  100  "}
	coin, err := coinFromProto(c, "amount")
	require.NoError(t, err)
	assert.Equal(t, "ulac", coin.Denom,
		"denom TrimSpace'd before ValidateDenom")
	assert.Equal(t, "100", coin.Amount.String(),
		"amount TrimSpace'd before parse")
}

// TestCoinFromProto_FieldNamePropagatesEvenOnLateFailures pins
// that the field name reaches EVERY error path, not just the
// nil-check at :48. A refactor that hardcoded the field name
// in one arm would break uniform error composition.
func TestCoinFromProto_FieldNameInEveryErrorPath(t *testing.T) {
	t.Parallel()
	fieldName := "custom_field_name_for_triage"
	cases := []*v1beta1.Coin{
		nil,                                            // :47-49
		{Denom: "", Amount: "100"},                      // :51-53
		{Denom: "1bad", Amount: "100"},                  // :58-60
		{Denom: "ulac", Amount: ""},                     // :62-64
		{Denom: "ulac", Amount: "not-a-number"},         // :65-68
		{Denom: "ulac", Amount: "0"},                    // positivity
		{Denom: "ulac", Amount: "-1"},                   // positivity
	}
	for _, c := range cases {
		_, err := coinFromProto(c, fieldName)
		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), fieldName),
			"error from input %+v contains field name %q — pins "+
				"field-name propagation across all error arms.",
			c, fieldName)
	}
}

// ---- Cross-sibling asymmetry anchor ----

// TestCoinFromProtoHelpers_ZeroAmountDivergence is the top-
// level scan-angle #5 anchor. Same zero-amount input produces
// DIFFERENT outcomes across the two coin helpers:
//   coinFromProto:     ERROR (positivity required)
//   CoinFromProtoSafe: SUCCESS (zero is a valid sdk.Coin)
func TestCoinFromProtoHelpers_ZeroAmountDivergesBetweenSiblings(t *testing.T) {
	t.Parallel()
	zeroCoin := &v1beta1.Coin{Denom: "ulac", Amount: "0"}

	// Safe variant accepts zero.
	_, errSafe := CoinFromProtoSafe(zeroCoin)
	assert.NoError(t, errSafe,
		"CoinFromProtoSafe ACCEPTS zero amount (sdk.NewCoin "+
			"generic contract — zero is a legal coin)")

	// Package-private variant rejects zero.
	_, errStrict := coinFromProto(zeroCoin, "amount")
	assert.Error(t, errStrict,
		"coinFromProto REJECTS zero amount (msg-invariant: "+
			"no msg with a zero coin is meaningful). Pins the "+
			"scan-angle #5 intentional divergence: a refactor "+
			"unifying them would either relax msg validation or "+
			"break CACRoyalty fields that CAN be zero-coin.")
}
