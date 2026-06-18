//go:build cosmos && cosmos_full

package keeper_test

import (
	"strings"
	"testing"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file closes DIRECT-test coverage gaps on SafeSubtract
// at x/credits/keeper/math_safe.go:51-61. Existing
// TestSafeSubtract + FuzzSafeSubtract cover happy paths,
// zero subtrahend, equal amounts, underflow, and large
// numbers. What they DO NOT pin:
//
//   1. ERROR-TYPE ASYMMETRY (scan-angle #5, cross-helper
//      sibling pattern). SafeSubtract is the ONLY math_safe
//      helper that returns ErrInsufficientFunds (ABCI code 3)
//      rather than ErrInvalidParams (ABCI code 2). Every
//      other helper (SafeMulDiv, SafePercentage, SafeAddCoins,
//      SafeIncrementCounter, CalculateSplit, ValidateRates)
//      uses ErrInvalidParams. The asymmetry is intentional:
//      underflow on a balance subtraction IS an insufficient-
//      funds condition, not a programmer error. A refactor
//      that unified error types for "cleanup" would silently
//      downgrade the ABCI-code distinction that downstream
//      middlewares (IBC relayer, client SDKs) use to decide
//      whether to refund vs reject.
//
//   2. ERROR-MESSAGE OPERAND ECHO (scan-angle #3, hidden-
//      secondary-return pinning). The Wrapf at :55-58 echoes
//      BOTH operands for triage: "subtraction would result
//      in negative value: <minuend> - <subtrahend>". A
//      refactor shortening to "underflow" would lose the
//      diagnostic signal ops teams use to correlate a failed
//      settlement to the specific amount mismatch.
//
//   3. METAMORPHIC RELATIONS (scan-angle #7, testing-
//      metamorphic skill). SafeSubtract obeys inverse, self-
//      subtract, and zero-identity relations. Existing unit
//      tests pin pointwise values; MRs pin invariants across
//      ALL valid inputs, catching refactors that introduce
//      edge-case clamping or saturating arithmetic.

// TestSafeSubtract_UnderflowReturnsErrInsufficientFunds is
// the CRITICAL scan-angle #5 anchor. Pins the error TYPE
// (not just presence). A refactor switching to
// ErrInvalidParams would silently change the ABCI code
// from 3 → 2, breaking downstream classifiers.
func TestSafeSubtract_UnderflowReturnsErrInsufficientFunds(t *testing.T) {
	t.Parallel()
	_, err := keeper.SafeSubtract(math.NewInt(100), math.NewInt(101))
	require.Error(t, err)

	assert.True(t, errors.IsOf(err, types.ErrInsufficientFunds),
		"underflow MUST return ErrInsufficientFunds (ABCI code 3). "+
			"Pins the scan-angle #5 asymmetry: every OTHER math_safe "+
			"helper returns ErrInvalidParams (code 2). A refactor "+
			"that unified error types 'for consistency' would change "+
			"the ABCI code and break downstream middlewares (IBC "+
			"relayer refund logic, client SDKs' retry classification) "+
			"that branch on code=3 vs code=2.")

	assert.False(t, errors.IsOf(err, types.ErrInvalidParams),
		"MIRROR assertion: the returned error is NOT ErrInvalidParams. "+
			"Both .IsOf checks together pin the exact error identity.")
}

// TestSafeSubtract_ErrorMessageEchoesBothOperands pins the
// operand-echo diagnostic contract at :55-58. The Wrapf
// format must include BOTH minuend and subtrahend strings
// so operators can correlate a failed settlement to the
// specific amount mismatch.
func TestSafeSubtract_ErrorMessageEchoesBothOperands(t *testing.T) {
	t.Parallel()
	// Use distinctive-digit operands so both appear in the
	// message and neither is a substring of the other.
	_, err := keeper.SafeSubtract(math.NewInt(12345), math.NewInt(67890))
	require.Error(t, err)
	msg := err.Error()

	assert.Contains(t, msg, "12345",
		"minuend echoed in error message. Pins :57 format: a "+
			"refactor that shortened to just 'underflow' or dropped "+
			"either operand would lose diagnostic signal.")
	assert.Contains(t, msg, "67890",
		"subtrahend echoed in error message")
	assert.Contains(t, msg, "negative value",
		"human-readable reason present for operator triage")

	// Pin the order: minuend appears BEFORE subtrahend (the
	// message is written as 'minuend - subtrahend').
	minuendIdx := strings.Index(msg, "12345")
	subtrahendIdx := strings.Index(msg, "67890")
	assert.True(t, minuendIdx < subtrahendIdx,
		"minuend appears before subtrahend in error message. Pins "+
			"the 'minuend - subtrahend' ordering that matches the "+
			"mathematical convention — a refactor that reversed the "+
			"order would confuse operators reading the log.")
}

// TestSafeSubtract_BoundaryEqualOperandsAccepted pins the
// strict `.LT` comparison at :54. When minuend == subtrahend,
// subtraction succeeds (returns zero). A refactor to `.LTE`
// would reject the equal case, breaking every full-consume
// settlement that zeros out a balance.
func TestSafeSubtract_BoundaryEqualOperandsReturnsZero(t *testing.T) {
	t.Parallel()
	r, err := keeper.SafeSubtract(math.NewInt(500), math.NewInt(500))
	require.NoError(t, err,
		"equal minuend/subtrahend ACCEPTED. Pins :54 strict `.LT` "+
			"comparison: a refactor to `.LTE` would reject the "+
			"100%%-consume case used by full-settlement paths "+
			"(e.g. settling a lock for its exact balance).")
	assert.True(t, r.IsZero(),
		"equal operands → result is exactly zero")
}

// TestSafeSubtract_BoundaryJustUnderRejected pins the
// mirror boundary: minuend < subtrahend by the smallest
// positive amount (1 unit) is the tightest underflow case.
func TestSafeSubtract_BoundaryJustUnderRejected(t *testing.T) {
	t.Parallel()
	_, err := keeper.SafeSubtract(math.NewInt(500), math.NewInt(501))
	require.Error(t, err,
		"minuend one unit below subtrahend REJECTED. Pins the "+
			"tight off-by-one boundary at :54.")
	assert.True(t, errors.IsOf(err, types.ErrInsufficientFunds))
}

// TestSafeSubtract_MR_InverseRelation pins the fundamental
// metamorphic inverse: SafeSubtract(a+b, b) == a. This holds
// for any non-negative a, b (where a+b >= b).
func TestSafeSubtract_MR_InverseRelation(t *testing.T) {
	t.Parallel()
	for _, pair := range []struct {
		a, b int64
	}{
		{0, 0},
		{100, 0},
		{0, 100}, // a+b = 100, 100 - 100 = 0 == a
		{1, 1},
		{100, 100},
		{1_000_000, 42},
		{42, 1_000_000},
	} {
		a := math.NewInt(pair.a)
		b := math.NewInt(pair.b)
		sum := a.Add(b)

		result, err := keeper.SafeSubtract(sum, b)
		require.NoError(t, err, "a=%d b=%d", pair.a, pair.b)
		assert.True(t, result.Equal(a),
			"MR inverse: SafeSubtract(a+b, b) MUST == a. Got a=%d, "+
				"b=%d, result=%s. Pins that SafeSubtract is the exact "+
				"algebraic inverse of Add — a refactor introducing "+
				"saturating or rounding arithmetic would break this.",
			pair.a, pair.b, result.String())
	}
}

// TestSafeSubtract_MR_SelfSubtractProducesZero pins the
// identity SafeSubtract(a, a) == 0 for any non-negative a.
// This is the tightest boundary and a refactor that ever
// returned a != 0 sentinel for "full consume" would trip here.
func TestSafeSubtract_MR_SelfSubtractProducesZero(t *testing.T) {
	t.Parallel()
	for _, val := range []int64{0, 1, 100, 1_000_000, 1_000_000_000} {
		a := math.NewInt(val)
		r, err := keeper.SafeSubtract(a, a)
		require.NoError(t, err, "val=%d", val)
		assert.True(t, r.IsZero(),
			"MR: SafeSubtract(a, a) MUST == 0 for val=%d. Got %s. "+
				"Pins that the boundary case collapses to exactly "+
				"zero — a refactor returning a sentinel like -1 or "+
				"Int{} on full-consume would break settlement.",
			val, r.String())
	}
}

// TestSafeSubtract_MR_ZeroSubtrahendIsIdentity pins
// SafeSubtract(a, 0) == a for any a. The zero-subtrahend
// short-circuit isn't an explicit code path (the `.LT`
// comparison handles it implicitly), but the identity MUST
// hold.
func TestSafeSubtract_MR_ZeroSubtrahendIsIdentity(t *testing.T) {
	t.Parallel()
	zero := math.ZeroInt()
	for _, val := range []int64{0, 1, 100, 1_000_000, 1_000_000_000} {
		a := math.NewInt(val)
		r, err := keeper.SafeSubtract(a, zero)
		require.NoError(t, err, "val=%d", val)
		assert.True(t, r.Equal(a),
			"MR zero-identity: SafeSubtract(a, 0) MUST == a for "+
				"val=%d. Got %s. Pins that the subtrahend=0 case is "+
				"the identity (does NOT trigger the underflow guard).",
			val, r.String())
	}
}

// TestSafeSubtract_HighPrecision256Bit pins behavior at
// high-precision (256-bit) math.Int values. Existing tests
// go up to math.MaxUint64 (~18e18) — this pushes into the
// range used for uatom wei-equivalent computations. No
// overflow path exists on subtraction, so this MUST succeed.
func TestSafeSubtract_HighPrecision256Bit(t *testing.T) {
	t.Parallel()
	// ~2^200 — well past uint64 range, representative of
	// realistic cross-chain big-int balances.
	bigMinuend, ok := math.NewIntFromString("1606938044258990275541962092341162602522202993782792835301376")
	require.True(t, ok)
	bigSub, ok := math.NewIntFromString("803469022129495137770981046170581301261101496891396417650688")
	require.True(t, ok)

	result, err := keeper.SafeSubtract(bigMinuend, bigSub)
	require.NoError(t, err,
		"256-bit subtraction succeeds (no overflow path on "+
			"subtraction). Pins that arbitrary-precision inputs "+
			"don't trip any unintended guard.")

	// Reconstruct expected via Add to verify arithmetic correctness.
	expected := bigMinuend.Sub(bigSub)
	assert.True(t, result.Equal(expected),
		"high-precision result matches math.Int.Sub directly")
}

// TestSafeSubtract_ABCICodeIs3NotInvalidParamsCode2 pins
// the actual ABCI code value returned on underflow. Rather
// than relying on errors.IsOf (which checks wrapping chain),
// this asserts the unwrapped ABCI code directly. A refactor
// that switched to ErrInvalidParams would produce code=2,
// which would be silently tolerated by code that only
// checks errors.IsOf if the error types were aliased.
func TestSafeSubtract_ABCICodeMatchesErrInsufficientFunds(t *testing.T) {
	t.Parallel()
	_, err := keeper.SafeSubtract(math.NewInt(0), math.NewInt(1))
	require.Error(t, err)

	// Use errors.ABCIInfo — the canonical unwrap for both raw
	// *Error sentinels and Wrapf-wrapped forms. A direct type
	// assertion fails on the wrapper layer.
	codespace, code, _ := errors.ABCIInfo(err, false)

	assert.Equal(t, uint32(3), code,
		"ABCI code is 3 (ErrInsufficientFunds registration). Pins "+
			"the wire-level distinction from ErrInvalidParams "+
			"(code 2). Downstream clients that switch on (codespace, "+
			"code) tuples would break if these diverged.")
	assert.Equal(t, "credits", codespace,
		"codespace is 'credits' — pins module identity in error "+
			"envelope")
}

// TestSafeSubtract_NegativeMinuendPermitted pins that
// math.Int subtraction does NOT reject negative operands
// outright — only the RESULT sign matters. A negative
// minuend with a more-negative subtrahend yields a positive
// result. SafeSubtract uses `minuend.LT(subtrahend)` which
// correctly handles signed comparisons.
//
// This is a hidden secondary contract: existing tests only
// exercise non-negative operands. A refactor adding an
// `IsNegative()` guard on minuend would silently reject
// signed-balance code paths.
func TestSafeSubtract_NegativeOperandsHandledBySignedLT(t *testing.T) {
	t.Parallel()
	// -100 - (-200) = -100 + 200 = 100 (positive). The
	// signed LT: -100 < -200 is FALSE (since -100 > -200),
	// so the underflow guard does NOT fire.
	result, err := keeper.SafeSubtract(math.NewInt(-100), math.NewInt(-200))
	require.NoError(t, err,
		"signed comparison -100 < -200 is FALSE → no underflow. "+
			"Pins that math.Int.LT is signed-aware — a refactor "+
			"adding an unsigned-style guard (e.g. `if !minuend."+
			"IsPositive() || ...`) would reject this legitimate "+
			"signed-arithmetic case.")
	assert.Equal(t, int64(100), result.Int64())

	// Mirror: -200 - (-100) = -100 (negative) MUST fail
	// (since -200 < -100 signed, the guard fires).
	_, err = keeper.SafeSubtract(math.NewInt(-200), math.NewInt(-100))
	require.Error(t, err,
		"signed comparison -200 < -100 is TRUE → underflow fires. "+
			"Pins symmetric behavior in the opposite sign direction.")
	assert.True(t, errors.IsOf(err, types.ErrInsufficientFunds))
}
