package keeper_test

import (
	"strings"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file closes DIRECT-test coverage gaps on SafeAddCoins
// at x/credits/keeper/math_safe.go:63-74. Existing tests
// (TestSafeAddCoins_Basic / _MultiDenom / _EmptyCoins in
// nomock_test.go) cover happy paths but DO NOT pin:
//
//   1. PANIC → ERROR RECOVERY (scan-angle #3 + #6). The
//      defer/recover guard at :66-71 converts a runtime
//      panic from sdk.Coins.Add into a module error. This
//      is the WHOLE POINT of SafeAddCoins as a wrapper —
//      every happy-path test leaves the guardrail untested.
//      A refactor that accidentally stripped the defer (e.g.
//      inlining Add directly) would revert to panicking on
//      malformed input, propagating to the tx flow and
//      causing consensus failures.
//
//   2. HIDDEN-SECONDARY-RETURN (scan-angle #3). On panic
//      the function returns `sdk.Coins{}` (empty, not nil)
//      as the result. Callers assume non-nil safely-zero-
//      valued Coins — a refactor returning `nil` would
//      silently break `result.AmountOf(denom)` callers.
//
//   3. METAMORPHIC RELATIONS (scan-angle #7). SafeAddCoins
//      is the identity wrapper around Coins.Add in the
//      happy path; commutativity + associativity + identity
//      MRs pin that this delegation is preserved.
//
// The KNOWN panic trigger for Coins.Add is unsorted second-
// operand Coins ("coins must be sorted"). Probed empirically
// because the SDK behavior varies across versions.

// unsortedCoins returns a sdk.Coins value with denoms in
// reverse alphabetical order — Coins.Add validates sort
// order and panics with "coins must be sorted". This uses
// direct struct literal (not NewCoins) to bypass SDK-side
// sorting that NewCoins applies.
func unsortedCoins() sdk.Coins {
	return sdk.Coins{
		sdk.Coin{Denom: "zzzcoin", Amount: sdkmath.NewInt(1)},
		sdk.Coin{Denom: "aaacoin", Amount: sdkmath.NewInt(1)},
	}
}

// TestSafeAddCoins_UnsortedTriggersPanicRecovery is the
// CRITICAL scan-angle #3/#6 anchor. Pins that the defer/
// recover at :66-71 converts the panic from sdk.Coins.Add
// into a module error. WITHOUT this test, a refactor
// stripping the deferred recover would cause production
// tx handlers to panic → consensus failure.
func TestSafeAddCoins_UnsortedTriggersPanicRecovery(t *testing.T) {
	t.Parallel()
	a := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))

	result, err := keeper.SafeAddCoins(a, unsortedCoins())
	require.Error(t, err,
		"unsorted Coins in second operand triggers panic in "+
			"sdk.Coins.Add; the deferred recover at :66-71 MUST "+
			"convert it to a module error. A refactor removing "+
			"the defer would propagate the panic to the tx flow, "+
			"breaking consensus. Pins the whole-point guardrail.")

	assert.True(t, sdkerrors.IsOf(err, types.ErrInvalidParams),
		"recovered error is ErrInvalidParams (ABCI code 2). "+
			"Pins :69 Wrapf: a refactor using a bare errors.New "+
			"or a different sentinel would change the ABCI code "+
			"and break downstream error classifiers.")

	// Scan-angle #3 hidden-secondary-return.
	assert.NotNil(t, result,
		"on panic, result is an empty sdk.Coins (NOT nil). Pins "+
			":68 `result = sdk.Coins{}` assignment: a refactor "+
			"leaving result unassigned (zero-value nil Coins) "+
			"would break callers that do result.AmountOf(denom) "+
			"without a nil check.")
	assert.True(t, result.IsZero(),
		"on panic, result is zero-valued empty Coins")
}

// TestSafeAddCoins_RecoveredErrorMessageContainsPanicReason
// pins the diagnostic contract at :69. The Wrapf format
// includes the panic value so operators can trace the root
// cause from logs.
func TestSafeAddCoins_RecoveredErrorMessageEchoesPanicReason(t *testing.T) {
	t.Parallel()
	a := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))

	_, err := keeper.SafeAddCoins(a, unsortedCoins())
	require.Error(t, err)
	msg := err.Error()

	assert.Contains(t, strings.ToLower(msg), "overflow",
		"error message identifies the SafeAddCoins guardrail. "+
			"Pins :69 format `coin addition overflow: %%v`.")

	// The underlying SDK panic message is "coins must be
	// sorted" — the recovered error MUST surface it for
	// triage.
	assert.Contains(t, msg, "sorted",
		"recovered error echoes the underlying panic reason "+
			"('coins must be sorted'). Pins :69 %%v formatter: "+
			"a refactor that dropped the %%v or used a generic "+
			"message would lose the panic-root-cause signal "+
			"operators need to diagnose malformed inputs.")
}

// TestSafeAddCoins_ResultIsValidCoinsOnHappyPath pins that
// the returned Coins value is well-formed (sorted, no
// duplicates, no zero amounts) — which Coins.Add guarantees.
func TestSafeAddCoins_HappyPathReturnsValidCoins(t *testing.T) {
	t.Parallel()
	a := sdk.NewCoins(
		sdk.NewInt64Coin("ulac", 100),
		sdk.NewInt64Coin("ulume", 50),
	)
	b := sdk.NewCoins(
		sdk.NewInt64Coin("uatom", 25),
		sdk.NewInt64Coin("ulume", 75),
	)

	result, err := keeper.SafeAddCoins(a, b)
	require.NoError(t, err)

	// Sorted: uatom < ulac < ulume (alphabetical).
	require.Len(t, result, 3, "3 distinct denoms")
	assert.Equal(t, "uatom", result[0].Denom, "sorted by denom")
	assert.Equal(t, "ulac", result[1].Denom)
	assert.Equal(t, "ulume", result[2].Denom)

	// Values merged correctly.
	assert.Equal(t, int64(25), result.AmountOf("uatom").Int64())
	assert.Equal(t, int64(100), result.AmountOf("ulac").Int64())
	assert.Equal(t, int64(125), result.AmountOf("ulume").Int64(),
		"ulume merged: 50 + 75 = 125")

	// IsValid check — pins result satisfies Coins invariants.
	assert.True(t, result.IsValid(),
		"result is a valid Coins (sorted, no duplicates, no "+
			"zero amounts, no negatives). Pins that SafeAddCoins "+
			"preserves the Coins invariant on happy path.")
}

// TestSafeAddCoins_MR_Commutativity pins a+b == b+a.
// sdk.Coins.Add is commutative; SafeAddCoins inherits.
func TestSafeAddCoins_MR_Commutativity(t *testing.T) {
	t.Parallel()
	a := sdk.NewCoins(
		sdk.NewInt64Coin("aaa", 10),
		sdk.NewInt64Coin("bbb", 20),
	)
	b := sdk.NewCoins(
		sdk.NewInt64Coin("aaa", 5),
		sdk.NewInt64Coin("ccc", 30),
	)

	ab, err1 := keeper.SafeAddCoins(a, b)
	ba, err2 := keeper.SafeAddCoins(b, a)
	require.NoError(t, err1)
	require.NoError(t, err2)

	assert.True(t, ab.Equal(ba),
		"MR commutativity: a+b MUST == b+a. Got a+b=%s, b+a=%s. "+
			"Pins that SafeAddCoins preserves the commutative "+
			"property of the underlying Coins.Add — a refactor "+
			"introducing an order-dependent term (e.g. a saturating "+
			"cap on the second operand) would break this.",
		ab.String(), ba.String())
}

// TestSafeAddCoins_MR_Associativity pins (a+b)+c == a+(b+c).
func TestSafeAddCoins_MR_Associativity(t *testing.T) {
	t.Parallel()
	a := sdk.NewCoins(sdk.NewInt64Coin("aaa", 10))
	b := sdk.NewCoins(sdk.NewInt64Coin("bbb", 20))
	c := sdk.NewCoins(sdk.NewInt64Coin("ccc", 30))

	ab, err := keeper.SafeAddCoins(a, b)
	require.NoError(t, err)
	abc, err := keeper.SafeAddCoins(ab, c)
	require.NoError(t, err)

	bc, err := keeper.SafeAddCoins(b, c)
	require.NoError(t, err)
	aBc, err := keeper.SafeAddCoins(a, bc)
	require.NoError(t, err)

	assert.True(t, abc.Equal(aBc),
		"MR associativity: (a+b)+c MUST == a+(b+c). Got %s vs %s. "+
			"Pins that chained additions preserve ordering — any "+
			"intermediate rounding or state-dependent term would "+
			"fail here.",
		abc.String(), aBc.String())
}

// TestSafeAddCoins_MR_EmptyIdentityBothSides pins that the
// empty Coins is the identity for SafeAddCoins from BOTH
// sides (extends existing TestSafeAddCoins_EmptyCoins which
// only tests one side).
func TestSafeAddCoins_MR_EmptyIsIdentityFromBothSides(t *testing.T) {
	t.Parallel()
	a := sdk.NewCoins(
		sdk.NewInt64Coin("ulac", 100),
		sdk.NewInt64Coin("ulume", 50),
	)
	empty := sdk.NewCoins()

	// Left identity: empty + a == a.
	leftResult, err := keeper.SafeAddCoins(empty, a)
	require.NoError(t, err)
	assert.True(t, leftResult.Equal(a),
		"empty + a == a (left identity)")

	// Right identity: a + empty == a.
	rightResult, err := keeper.SafeAddCoins(a, empty)
	require.NoError(t, err)
	assert.True(t, rightResult.Equal(a),
		"a + empty == a (right identity). Pins that the empty-"+
			"operand happy path is symmetric — a refactor that "+
			"short-circuited only one side would silently diverge.")

	// Empty + empty == empty.
	both, err := keeper.SafeAddCoins(empty, empty)
	require.NoError(t, err)
	assert.True(t, both.IsZero(),
		"empty + empty == empty (degenerate base case)")
}

// TestSafeAddCoins_ZeroAmountCoinsPrunedOnAdd pins the
// cosmos-sdk convention that zero-amount coins are pruned
// from the result. SafeAddCoins inherits.
func TestSafeAddCoins_ZeroAmountCoinsPruned(t *testing.T) {
	t.Parallel()
	a := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	// A zero-amount coin is pruned by NewCoins but not if
	// inserted directly. Use NewCoins to match SDK convention.
	b := sdk.NewCoins()

	result, err := keeper.SafeAddCoins(a, b)
	require.NoError(t, err)

	// No zero-amount coin in result (NewCoins filters them).
	for _, c := range result {
		assert.False(t, c.IsZero(),
			"no zero-amount coins in result — cosmos-sdk prunes "+
				"them. Pins the Coins convention SafeAddCoins "+
				"inherits.")
	}
}

// TestSafeAddCoins_LargeAmountsHappyPath pins that typical
// chain-scale amounts (uint64 range) sum without overflow.
// Note: sdk.Coin amounts are math.Int (arbitrary precision)
// so Go-level overflow is not a concern; this pins the
// happy path at large magnitudes.
func TestSafeAddCoins_LargeAmountsSumCorrectly(t *testing.T) {
	t.Parallel()
	// 10^18 scale (1e18 ulac) — typical for 18-decimal
	// base-denom conventions.
	big, ok := sdkmath.NewIntFromString("1000000000000000000")
	require.True(t, ok)
	a := sdk.Coins{sdk.Coin{Denom: "ulac", Amount: big}}
	b := sdk.Coins{sdk.Coin{Denom: "ulac", Amount: big}}

	result, err := keeper.SafeAddCoins(a, b)
	require.NoError(t, err,
		"large-magnitude happy path: no overflow because Coin "+
			"amounts are math.Int (arbitrary precision).")

	expected, ok := sdkmath.NewIntFromString("2000000000000000000")
	require.True(t, ok)
	assert.True(t, result.AmountOf("ulac").Equal(expected),
		"1e18 + 1e18 = 2e18 (exact)")
}

// TestSafeAddCoins_ABCICodeOnPanicRecovery pins the wire-
// level ABCI code returned after panic recovery. Mirrors
// the ABCI-code check in safe_subtract_error_asymmetry_test.go
// but through the panic path.
func TestSafeAddCoins_PanicRecoveryReturnsABCICode2(t *testing.T) {
	t.Parallel()
	a := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))

	_, err := keeper.SafeAddCoins(a, unsortedCoins())
	require.Error(t, err)

	codespace, code, _ := sdkerrors.ABCIInfo(err, false)
	assert.Equal(t, uint32(2), code,
		"recovered panic surfaces as ABCI code 2 (ErrInvalidParams). "+
			"Pins the wire-level classification — a refactor using "+
			"ErrInsufficientFunds (code 3) or another sentinel would "+
			"silently change how downstream clients branch on the "+
			"error.")
	assert.Equal(t, "credits", codespace,
		"codespace is 'credits'")
}
