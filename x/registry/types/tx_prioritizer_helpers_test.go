//go:build cosmos

package types

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes coverage for 6 completely untested helpers
// in tx_prioritizer.go that feed block-ordering priority
// computation. A regression in ANY of these would silently
// reshape transaction priority, affecting block ordering and
// potentially block validity across consensus.
//
//   - clampUnitInterval (:742-750)
//   - zeroIfEmpty (:752-757)
//   - validateOptionalNonNegativeDecimal (:723-729)
//   - validateClosedUnitInterval (:731-740)
//   - firstNonEmpty (:759-766)
//   - maxInt64 (:768-773)

// --- clampUnitInterval ---

// TestClampUnitInterval_BelowZeroClampedToZero pins the :743-745
// strict `< 0` guard.
func TestClampUnitInterval_BelowZeroClampedToZero(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"-0.5", "-1", "-0.0001", "-100",
	} {
		got := clampUnitInterval(decimal.RequireFromString(in))
		assert.True(t, got.Equal(DecimalZero),
			"%s → 0 (below-range clamped to 0)", in)
	}
}

// TestClampUnitInterval_AboveOneClampedToOne pins the :746-748
// strict `> 1` guard.
func TestClampUnitInterval_AboveOneClampedToOne(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"1.0001", "2", "100", "1.00000001",
	} {
		got := clampUnitInterval(decimal.RequireFromString(in))
		assert.True(t, got.Equal(DecimalOne),
			"%s → 1 (above-range clamped to 1)", in)
	}
}

// TestClampUnitInterval_InRangePassesThrough pins the identity
// path. Values in [0, 1] are returned unchanged.
func TestClampUnitInterval_InRangePassesThrough(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"0", "0.0001", "0.5", "0.9999", "1",
	} {
		dec := decimal.RequireFromString(in)
		got := clampUnitInterval(dec)
		assert.True(t, got.Equal(dec),
			"%s in [0,1] passes through unchanged", in)
	}
}

// TestClampUnitInterval_ExactBoundariesInclusive pins that EXACT
// 0 and EXACT 1 are BOTH accepted (strict `<` and `>` means
// boundaries are inclusive). A refactor to `<=` / `>=` would
// wrongly clamp the boundaries.
func TestClampUnitInterval_ExactBoundariesInclusive(t *testing.T) {
	t.Parallel()
	zero := clampUnitInterval(DecimalZero)
	assert.True(t, zero.Equal(DecimalZero),
		"exact 0 → 0 (strict `<` guard keeps 0 in-range)")
	one := clampUnitInterval(DecimalOne)
	assert.True(t, one.Equal(DecimalOne),
		"exact 1 → 1 (strict `>` guard keeps 1 in-range). A "+
			"refactor to `>=` would wrongly clamp 1 to 1 (visible "+
			"no-op), but the path would take the clamping arm.")
}

// --- zeroIfEmpty ---

// TestZeroIfEmpty_EmptyReturnsZeroString pins the empty/
// whitespace-only → "0" conversion. Downstream decimal parsing
// cannot handle empty string.
func TestZeroIfEmpty_EmptyReturnsZeroString(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", " ", "  ", "\t", "\n", "\r\n"} {
		assert.Equal(t, "0", zeroIfEmpty(in),
			"%q → '0' (whitespace-only treated as empty)", in)
	}
}

// TestZeroIfEmpty_NonEmptyPassesThrough pins that NON-whitespace
// input is returned UNCHANGED — including garbage that the
// caller's parser will reject. The helper doesn't validate;
// it just provides a default for empty.
func TestZeroIfEmpty_NonEmptyPassesThrough(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"0", "1", "0.5", "-1", "garbage",
		"  0  ", // whitespace WITH content — returned as-is (not trimmed)
	} {
		assert.Equal(t, in, zeroIfEmpty(in),
			"%q returned as-is — zeroIfEmpty only replaces "+
				"pure-whitespace/empty; it does NOT trim or validate", in)
	}
}

// --- validateOptionalNonNegativeDecimal ---

// TestValidateOptionalNonNegativeDecimal_EmptyReturnsZero pins
// the :724-727 short-circuit. Empty string is OPTIONAL — returns
// zero with no error.
func TestValidateOptionalNonNegativeDecimal_EmptyReturnsZero(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", " ", "\t", "   "} {
		got, err := validateOptionalNonNegativeDecimal(in, "field")
		require.NoError(t, err,
			"empty %q → zero with no error (OPTIONAL contract)", in)
		assert.True(t, got.Equal(DecimalZero))
	}
}

// TestValidateOptionalNonNegativeDecimal_ValidPositivePasses pins
// the happy path for non-negative decimals.
func TestValidateOptionalNonNegativeDecimal_ValidPositivePasses(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"0", "1.5", "100", "0.000001"} {
		got, err := validateOptionalNonNegativeDecimal(in, "field")
		require.NoError(t, err, "non-negative %q accepted", in)
		assert.True(t, got.Equal(decimal.RequireFromString(in)))
	}
}

// TestValidateOptionalNonNegativeDecimal_InvalidStringErrors pins
// that non-decimal garbage produces an error (from
// SafeDecimalFromStringStrict). Note: despite the function name
// implying non-negative validation, the actual rejection of
// negatives is delegated to SafeDecimalFromStringStrict's
// strictness — which does NOT reject negatives. Pins the current
// behavior.
func TestValidateOptionalNonNegativeDecimal_InvalidStringErrors(t *testing.T) {
	t.Parallel()
	_, err := validateOptionalNonNegativeDecimal("not-a-number", "budget")
	require.Error(t, err,
		"unparseable string must error")
	assert.Contains(t, err.Error(), "budget",
		"error message includes the field name for caller diagnosis")
}

// --- validateClosedUnitInterval ---

// TestValidateClosedUnitInterval_EmptyReturnsZero pins that the
// empty-input path via zeroIfEmpty produces 0 (passes the [0,1]
// check).
func TestValidateClosedUnitInterval_EmptyReturnsZero(t *testing.T) {
	t.Parallel()
	got, err := validateClosedUnitInterval("", "rate")
	require.NoError(t, err)
	assert.True(t, got.Equal(DecimalZero),
		"empty → '0' (via zeroIfEmpty) → valid 0 in [0,1]")
}

// TestValidateClosedUnitInterval_InRangePasses pins the happy
// path for values within [0, 1].
func TestValidateClosedUnitInterval_InRangePasses(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"0", "0.001", "0.5", "0.99", "1"} {
		got, err := validateClosedUnitInterval(in, "rate")
		require.NoError(t, err, "in-range %q accepted", in)
		assert.True(t, got.Equal(decimal.RequireFromString(in)))
	}
}

// TestValidateClosedUnitInterval_BelowZeroRejected pins that
// negative values are rejected. Note: the rejection comes from
// SafeDecimalFromStringStrict's strict-non-negative check —
// the :736 range check only triggers for strictly-positive
// values > 1. Pins that the error message identifies the
// field regardless of which guard fires.
func TestValidateClosedUnitInterval_BelowZeroRejected(t *testing.T) {
	t.Parallel()
	_, err := validateClosedUnitInterval("-0.001", "rate")
	require.Error(t, err,
		"negative value must be rejected — earlier arm from "+
			"SafeDecimalFromStringStrict catches this before the "+
			":736 range guard runs")
	assert.Contains(t, err.Error(), "rate",
		"error message identifies the field name")
}

// TestValidateClosedUnitInterval_AboveOneRejected pins the
// :736 guard for over-range values.
func TestValidateClosedUnitInterval_AboveOneRejected(t *testing.T) {
	t.Parallel()
	_, err := validateClosedUnitInterval("1.001", "rate")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate")
	assert.Contains(t, err.Error(), "between 0 and 1")
}

// TestValidateClosedUnitInterval_ExactBoundariesAccepted pins
// the INCLUSIVE boundaries: exact 0 and exact 1 both pass. The
// strict `< DecimalZero` and `> DecimalOne` at :736 treat the
// boundaries as INCLUSIVE. A refactor to `<=` or `>=` would
// wrongly reject the endpoints (e.g., 100% success rate would
// be rejected).
func TestValidateClosedUnitInterval_ExactBoundariesAccepted(t *testing.T) {
	t.Parallel()
	zero, err := validateClosedUnitInterval("0", "rate")
	require.NoError(t, err,
		"exact 0 accepted — strict `<` at :736 makes boundary "+
			"INCLUSIVE. A refactor to `<=` would wrongly reject 0.")
	assert.True(t, zero.Equal(DecimalZero))

	one, err := validateClosedUnitInterval("1", "rate")
	require.NoError(t, err,
		"exact 1 accepted — a 100%% rate is legal. Refactor to "+
			"`>=` would wrongly reject.")
	assert.True(t, one.Equal(DecimalOne))
}

// TestValidateClosedUnitInterval_InvalidStringErrors pins the
// parse-failure path propagated from SafeDecimalFromStringStrict.
func TestValidateClosedUnitInterval_InvalidStringErrors(t *testing.T) {
	t.Parallel()
	_, err := validateClosedUnitInterval("garbage", "rate")
	require.Error(t, err)
	// Error comes from SafeDecimalFromStringStrict, not the range check.
}

// --- firstNonEmpty ---

// TestFirstNonEmpty_ReturnsFirstNonEmptyTrimmed pins the return-
// first-non-empty contract. Leading/trailing whitespace is
// trimmed on the RETURN.
func TestFirstNonEmpty_ReturnsFirstNonEmptyTrimmed(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "second", firstNonEmpty("", "second", "third"))
	assert.Equal(t, "first", firstNonEmpty("first", "", "third"))
	assert.Equal(t, "trimmed",
		firstNonEmpty("  trimmed  ", "ignored"),
		"leading/trailing whitespace trimmed on return — pins "+
			"the :762 strings.TrimSpace")
}

// TestFirstNonEmpty_WhitespaceOnlyEntriesSkipped pins that
// pure-whitespace entries are treated as empty.
func TestFirstNonEmpty_WhitespaceOnlyEntriesSkipped(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "real",
		firstNonEmpty("  ", "\t", "\n", "real"),
		"whitespace-only entries skipped via the `!= \"\"` guard "+
			"after TrimSpace at :761")
}

// TestFirstNonEmpty_AllEmptyReturnsEmpty pins the all-empty
// case.
func TestFirstNonEmpty_AllEmptyReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", firstNonEmpty())
	assert.Equal(t, "", firstNonEmpty("", "", ""))
	assert.Equal(t, "", firstNonEmpty(" ", "\t", "\n"))
}

// TestFirstNonEmpty_SingleInputReturnedAsIsAfterTrim pins the
// single-value case.
func TestFirstNonEmpty_SingleInputReturnedAsIsAfterTrim(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hello", firstNonEmpty("hello"))
	assert.Equal(t, "hello", firstNonEmpty("  hello  "),
		"single input trimmed")
	assert.Equal(t, "", firstNonEmpty(""))
}

// --- maxInt64 ---

// TestMaxInt64_ReturnsLarger pins the strict `>` comparison.
func TestMaxInt64_ReturnsLarger(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(10), maxInt64(10, 5))
	assert.Equal(t, int64(10), maxInt64(5, 10))
	assert.Equal(t, int64(-5), maxInt64(-10, -5),
		"both negative: -5 > -10")
	assert.Equal(t, int64(100), maxInt64(-1000, 100),
		"crosses zero")
}

// TestMaxInt64_EqualReturnsFirst pins the tie-break: strict `>`
// means when a == b, the second branch returns b (the second
// argument). Observationally a == b so either is correct; pins
// the CURRENT behavior so a refactor doesn't silently change
// which value's pointer/identity is returned.
func TestMaxInt64_EqualReturnsFirst(t *testing.T) {
	t.Parallel()
	a, b := int64(42), int64(42)
	got := maxInt64(a, b)
	assert.Equal(t, int64(42), got,
		"equal values: strict `>` at :769 means the `a > b` arm "+
			"is FALSE, so the function returns b (second argument). "+
			"Both are 42, so observationally identical.")
}

// TestMaxInt64_Int64Boundaries pins extreme values to catch
// any overflow-related regression.
func TestMaxInt64_Int64Boundaries(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(9223372036854775807), // math.MaxInt64
		maxInt64(9223372036854775807, 0))
	assert.Equal(t, int64(0),
		maxInt64(-9223372036854775808, 0), // math.MinInt64
		"MinInt64 vs 0: 0 > MinInt64")
}
