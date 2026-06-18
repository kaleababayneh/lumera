//go:build cosmos

package types

import (
	"strings"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes DIRECT-test coverage for CoinFromProtoSafe
// at proto_helpers.go:41-61 — the user-input-safe variant of
// the proto→sdk.Coin conversion. Had ZERO direct tests prior
// despite being the gated entry point for every msg_server
// handler that accepts a protobuf Coin from the wire.
//
// Scan-angle #6 (security-critical invariants tested only at
// happy path) applies heavily. The in-code comment at :49-53
// explicitly documents the DoS/crash vector: "sdk.NewCoin
// panics on empty/invalid denom or negative amount; convert
// those to errors here so the function matches its 'safe for
// user input' contract. Every caller in msg_server relies on
// the returned error — a panic would crash the handler
// through baseapp's recover instead of surfacing as a clean
// validation error to the user."
//
// Scan-angle #5 (sibling-pattern pinning with intentional
// asymmetry) applies to the pair:
//
//   CoinFromProtoSafe: (coin, err) — error path for bad input
//   CoinFromProto:     (coin)      — PANICS on bad input
//
// A refactor that unified the two would either (a) introduce
// panics into wire-handling paths (fatal for baseapp) or (b)
// silently swallow errors in internal code that relies on
// the panic-on-invariant-violation behavior.

// TestCoinFromProtoSafe_NilReturnsZeroCoinNoError pins :42-44.
// A nil proto message returns a zero-valued coin with an
// initialized (not-nil) Amount. Pinned against a regression
// where nil input would produce sdk.Coin{} (Amount uninit
// math.Int) — that zero-value leaks nil math.Int internals
// into arithmetic, causing cryptic panics downstream.
func TestCoinFromProtoSafe_NilReturnsZeroCoinNoError(t *testing.T) {
	t.Parallel()
	c, err := CoinFromProtoSafe(nil)
	require.NoError(t, err,
		"nil proto → no error. Pins :42-44 early return so user-"+
			"submitted messages with optional coin fields don't "+
			"reject when the field is omitted entirely.")
	assert.Equal(t, "", c.Denom,
		"zero Denom for nil input")
	assert.False(t, c.Amount.IsNil(),
		"Amount is INITIALIZED (math.ZeroInt), not nil. Pins "+
			":43 explicit math.ZeroInt() initialization — a "+
			"refactor that returned sdk.Coin{} directly would "+
			"leave Amount as a nil math.Int, causing panics in "+
			"downstream arithmetic like IsPositive() or Add().")
	assert.True(t, c.Amount.IsZero())
}

// TestCoinFromProtoSafe_ValidInputPassesThrough pins the happy
// path at :45,:60.
func TestCoinFromProtoSafe_ValidInputPassesThrough(t *testing.T) {
	t.Parallel()
	p := &basev1beta1.Coin{Denom: "ulac", Amount: "1234"}
	c, err := CoinFromProtoSafe(p)
	require.NoError(t, err)
	assert.Equal(t, "ulac", c.Denom)
	assert.True(t, c.Amount.Equal(math.NewInt(1234)))
}

// TestCoinFromProtoSafe_ZeroAmountAccepted pins that an
// EXPLICIT zero amount ("0") is VALID (sdk.NewCoin accepts
// zero). This is a non-obvious boundary because sdk.NewCoin
// has SOMETIMES rejected zero in the past; the current
// version allows it. Pinned so a future sdk upgrade that
// changes this behavior surfaces here.
func TestCoinFromProtoSafe_ZeroAmountAccepted(t *testing.T) {
	t.Parallel()
	p := &basev1beta1.Coin{Denom: "ulac", Amount: "0"}
	c, err := CoinFromProtoSafe(p)
	require.NoError(t, err,
		"explicit zero amount accepted. Pins the current sdk.NewCoin "+
			"contract — a refactor upstream that made zero rejected "+
			"would surface here rather than in msg_server handlers.")
	assert.True(t, c.Amount.IsZero())
}

// TestCoinFromProtoSafe_LargeAmountAccepted pins that very
// large decimal-string amounts parse correctly. Pinned because
// cosmos-sdk occasionally tightens max-int bounds; a regression
// would surface here before reaching user-facing handlers.
func TestCoinFromProtoSafe_LargeAmountAccepted(t *testing.T) {
	t.Parallel()
	// 2^64 - 1 (would overflow uint64 but fits in math.Int)
	p := &basev1beta1.Coin{Denom: "ulac", Amount: "18446744073709551615"}
	c, err := CoinFromProtoSafe(p)
	require.NoError(t, err)
	assert.Equal(t, "ulac", c.Denom)
	expected, _ := math.NewIntFromString("18446744073709551615")
	assert.True(t, c.Amount.Equal(expected))
}

// TestCoinFromProtoSafe_InvalidAmountReturnsError is the FIRST
// scan-angle #6 anchor: a non-numeric Amount returns an error
// (NOT a panic).
func TestCoinFromProtoSafe_InvalidAmountStringReturnsError(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"not-a-number",
		"1.5",  // decimals not allowed (math.Int is integer)
		"1e10", // scientific notation not accepted by NewIntFromString
		"",     // empty string
	} {
		p := &basev1beta1.Coin{Denom: "ulac", Amount: bad}
		_, err := CoinFromProtoSafe(p)
		require.Error(t, err,
			"invalid Amount %q must return error (not panic). Pins "+
				":45-48 math.NewIntFromString guard — the CRITICAL "+
				"scan-angle #6 contract documented at :49-53: a "+
				"refactor that let this reach sdk.NewCoin would "+
				"panic, crashing the msg_server handler through "+
				"baseapp recover.", bad)
		assert.Contains(t, err.Error(), "invalid coin amount",
			"error message identifies the failed validation class "+
				"for operator triage")
		assert.Contains(t, err.Error(), bad,
			"error message echoes the offending raw input %q", bad)
	}
}

// TestCoinFromProtoSafe_InvalidDenomReturnsError is the SECOND
// scan-angle #6 anchor at :54-56. Empty or malformed denoms
// would panic in sdk.NewCoin; CoinFromProtoSafe converts them
// to errors.
func TestCoinFromProtoSafe_InvalidDenomReturnsError(t *testing.T) {
	t.Parallel()
	// Empty denom.
	p := &basev1beta1.Coin{Denom: "", Amount: "100"}
	_, err := CoinFromProtoSafe(p)
	require.Error(t, err,
		"empty denom returns error, not panic. Pins :54-56 "+
			"sdk.ValidateDenom guard: a refactor skipping this "+
			"would reach sdk.NewCoin which panics on empty denom.")
	assert.Contains(t, err.Error(), "invalid coin denom")

	// Denom with whitespace or special chars (rejected by
	// cosmos-sdk denom regex).
	for _, badDenom := range []string{
		"has space",
		"!@#$",
		"a", // too short (< 3 chars in default cosmos denom regex)
	} {
		p2 := &basev1beta1.Coin{Denom: badDenom, Amount: "100"}
		_, err := CoinFromProtoSafe(p2)
		require.Error(t, err,
			"malformed denom %q returns error", badDenom)
	}
}

// TestCoinFromProtoSafe_NegativeAmountReturnsError is the
// THIRD scan-angle #6 anchor at :57-59. Negative amounts
// would panic in sdk.NewCoin; this converts them to errors.
// Critical because a "refund-shaped" negative coin from a
// malicious peer would otherwise reach sdk.NewCoin.
func TestCoinFromProtoSafe_NegativeAmountReturnsError(t *testing.T) {
	t.Parallel()
	for _, neg := range []string{
		"-1",
		"-100",
		"-1000000",
	} {
		p := &basev1beta1.Coin{Denom: "ulac", Amount: neg}
		_, err := CoinFromProtoSafe(p)
		require.Error(t, err,
			"negative amount %q returns error, not panic. Pins "+
				":57-59 negative guard — without it, a wire-supplied "+
				"negative Coin would panic sdk.NewCoin and crash the "+
				"msg_server handler.", neg)
		assert.Contains(t, err.Error(), "negative coin amount")
		// Error surfaces the offending value for triage.
		assert.Contains(t, err.Error(), strings.TrimPrefix(neg, "-"))
	}
}

// TestCoinFromProtoSafe_ErrorOrderingAmountBeforeDenom pins
// the GUARD ORDERING: amount parse failure is reported BEFORE
// denom validation. If both are invalid, the amount error
// wins. Pinned so a refactor reversing the order would
// surface in operator-triage messaging (amount errors are
// caller-supplied format issues; denom errors are caller-
// supplied identity issues).
func TestCoinFromProtoSafe_AmountCheckBeforeDenom(t *testing.T) {
	t.Parallel()
	// BOTH invalid: the amount parse fails first.
	p := &basev1beta1.Coin{Denom: "", Amount: "not-a-number"}
	_, err := CoinFromProtoSafe(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid coin amount",
		"amount-error surfaces FIRST when both amount and denom are "+
			"invalid. Pins :45-48 runs before :54-56. A refactor "+
			"reordering the guards would change the error message "+
			"operators see for the first-encountered field, affecting "+
			"triage of wire-format issues.")
	assert.NotContains(t, err.Error(), "invalid coin denom",
		"denom error is NOT reported when amount already failed — "+
			"the function short-circuits on the first validation "+
			"failure")
}

// TestCoinFromProtoSafe_DenomCheckBeforeNegative pins that
// if Amount is a VALID non-negative integer but the denom is
// bad, denom error surfaces (not a negative-amount error).
func TestCoinFromProtoSafe_DenomCheckBeforeNegative(t *testing.T) {
	t.Parallel()
	// Amount is valid (parses to 100, not negative). Denom is
	// invalid. Only the denom error should fire.
	p := &basev1beta1.Coin{Denom: "", Amount: "100"}
	_, err := CoinFromProtoSafe(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid coin denom")
	assert.NotContains(t, err.Error(), "negative")
}

// TestCoinFromProto_PanicsOnInvalidInputMatchesContract is the
// scan-angle #5 CROSS-SIBLING anchor. CoinFromProto (non-Safe
// variant) PANICS on the exact same inputs that CoinFromProto
// Safe returns errors for. This is intentional per :64-65
// docstring: "It panics on malformed amounts — use
// CoinFromProtoSafe for user-supplied input."
func TestCoinFromProto_PanicsOnInvalidInputPerContract(t *testing.T) {
	t.Parallel()
	// Invalid amount → panic.
	assert.Panics(t, func() {
		_ = CoinFromProto(&basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "not-a-number",
		})
	}, "CoinFromProto panics on bad amount. Pins the scan-angle "+
		"#5 sibling contract: msg_server MUST use CoinFromProtoSafe "+
		"for user input. A refactor unifying the two would force "+
		"the wire-handling path into one or the other, breaking "+
		"internal-code callers that rely on panic-on-invariant.")

	// Invalid denom → also panic.
	assert.Panics(t, func() {
		_ = CoinFromProto(&basev1beta1.Coin{
			Denom:  "",
			Amount: "100",
		})
	})

	// Negative amount → panic.
	assert.Panics(t, func() {
		_ = CoinFromProto(&basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "-1",
		})
	})

	// But nil input is HANDLED (returns zero-coin, no panic) —
	// pins that the panic-vs-error distinction is ONLY on
	// malformed input, not on absent input.
	assert.NotPanics(t, func() {
		c := CoinFromProto(nil)
		assert.True(t, c.Amount.IsZero())
	}, "nil → zero-coin for BOTH helpers (no panic in either). "+
		"Pins the divergence at :67-71: only errors from "+
		"CoinFromProtoSafe translate to panics, not the nil fast "+
		"path.")
}

// TestCoinFromProtoSafe_RoundTripWithCoinToProto pins the
// lossless round-trip: CoinToProto → CoinFromProtoSafe
// reproduces the original coin. Regression guard against
// encoding/decoding drift.
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
		assert.True(t, orig.IsEqual(back),
			"round trip preserves coin %v", orig)
	}
}
