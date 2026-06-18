package types

import (
	"testing"

	"cosmossdk.io/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes DIRECT-test coverage for the SPECIFIC ABCI
// CODES of x/reserve/types/errors.go sentinels. The existing
// TestSentinelErrors in types_test.go pins nil/non-empty +
// pairwise distinctness but DOES NOT pin the numeric codes
// (1200..1205) that travel over the wire in ABCI tx responses.
//
// Scan-angle #6 (security-critical invariants tested only at
// happy path). Error codes are a WIRE-FACING CONTRACT:
// downstream clients (SDK bindings, explorers, auction
// engines that consume Reserve allocation failures) match on
// (codespace, code). A silent renumber breaks every such
// client.
//
// Scan-angle #5 (sibling-pattern pinning with structural
// invariants):
//   - 6 sentinels contiguous 1200..1205 (in a distinct high
//     range vs credits module's 2..9)
//   - All pairwise distinct
//   - All share codespace = 'reserve'
//
// Scan-angle #4 (historical-fix regression guard) — the
// credits module's errors use the low range (2..9) matching
// the cosmos-sdk convention; the reserve module deliberately
// uses the 1200+ range to carve out a distinct namespace.
// Pinned so a refactor that renumbered into the low range
// would collide with the credits codes on a future unified-
// codespace scheme.

func abciCodeOf(t *testing.T, err error) (string, uint32) {
	t.Helper()
	codespace, code, _ := errors.ABCIInfo(err, false)
	return codespace, code
}

// TestReserveErrors_ABCICodesPinned pins the exact registered
// code numbers. A refactor reordering errors.Register calls
// would shift codes silently and break every downstream
// client matching on numeric values.
func TestReserveErrors_ABCICodesPinned(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		code uint32
	}{
		{"ErrTierNotFound", ErrTierNotFound, 1200},
		{"ErrInvalidCommitment", ErrInvalidCommitment, 1201},
		{"ErrCommitmentNotFound", ErrCommitmentNotFound, 1202},
		{"ErrCommitmentExpired", ErrCommitmentExpired, 1203},
		{"ErrInsufficientCapacity", ErrInsufficientCapacity, 1204},
		{"ErrCreditDenomMismatch", ErrCreditDenomMismatch, 1205},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, code := abciCodeOf(t, c.err)
			assert.Equal(t, c.code, code,
				"%s ABCI code = %d. Pins the wire-facing "+
					"contract: downstream clients match on "+
					"(codespace, code). A refactor that reordered "+
					"or inserted an errors.Register call earlier "+
					"would shift every subsequent code.", c.name, c.code)
		})
	}
}

// TestReserveErrors_CodespacePinned pins codespace on every
// sentinel. All 6 share codespace = ModuleName = 'reserve'.
func TestReserveErrors_CodespacePinned(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		ErrTierNotFound, ErrInvalidCommitment, ErrCommitmentNotFound,
		ErrCommitmentExpired, ErrInsufficientCapacity, ErrCreditDenomMismatch,
	} {
		codespace, _ := abciCodeOf(t, err)
		assert.Equal(t, ModuleName, codespace,
			"codespace == ModuleName for every reserve sentinel. "+
				"Pins the (codespace, code) pair consumers use to "+
				"filter per-module error classes.")
	}
}

// TestReserveErrors_ContiguousRange1200To1205 pins the
// contiguous block 1200..1205. Unlike the credits module
// which uses the low range (2..9), reserve carves out 1200+
// to avoid collision on any future unified-codespace scheme.
func TestReserveErrors_ContiguousRange1200Through1205(t *testing.T) {
	t.Parallel()
	codes := make(map[uint32]struct{}, 6)
	for _, err := range []error{
		ErrTierNotFound, ErrInvalidCommitment, ErrCommitmentNotFound,
		ErrCommitmentExpired, ErrInsufficientCapacity, ErrCreditDenomMismatch,
	} {
		_, code := abciCodeOf(t, err)
		codes[code] = struct{}{}
	}

	// All 6 must be present in 1200..1205.
	for want := uint32(1200); want <= 1205; want++ {
		_, present := codes[want]
		assert.True(t, present,
			"code %d registered (part of contiguous 1200..1205 "+
				"allocation). New reserve sentinels should extend "+
				"to 1206+, not backfill gaps.", want)
	}

	// No codes outside 1200..1205.
	for c := range codes {
		assert.True(t, c >= 1200 && c <= 1205,
			"code %d is outside the pinned 1200..1205 range. Pins "+
				"against accidental collision with low-range "+
				"modules (e.g., credits 2..9).", c)
	}
}

// TestReserveErrors_DistinctFromCreditsLowRange pins the
// RANGE-SEPARATION invariant between the two cc_3 domain
// modules. Reserve codes (1200+) never collide with credits
// codes (2..9) in the event of a future codespace-
// consolidation refactor.
func TestReserveErrors_DistinctFromCreditsLowRange(t *testing.T) {
	t.Parallel()
	reserveLow := uint32(1200)
	for _, err := range []error{
		ErrTierNotFound, ErrInvalidCommitment, ErrCommitmentNotFound,
		ErrCommitmentExpired, ErrInsufficientCapacity, ErrCreditDenomMismatch,
	} {
		_, code := abciCodeOf(t, err)
		assert.GreaterOrEqual(t, code, reserveLow,
			"reserve code %d must be in 1200+ range. Pins the "+
				"namespace-separation invariant with credits module "+
				"(which uses 2..9). A refactor renumbering into the "+
				"low range would collide on any future unified-"+
				"codespace scheme.", code)
	}
}

// TestReserveErrors_WrappedErrorsRoundTripThroughIsOf pins
// the errors.IsOf-compatibility contract for reserve
// sentinels. Keeper code uses `errors.IsOf(err,
// ErrCommitmentNotFound)` to branch on error class.
func TestReserveErrors_WrappedErrorsRoundTripThroughIsOf(t *testing.T) {
	t.Parallel()
	wrapped := errors.Wrapf(ErrCommitmentNotFound, "id=%s", "commit-1")
	require.Error(t, wrapped)
	assert.True(t, errors.IsOf(wrapped, ErrCommitmentNotFound),
		"wrapped sentinel round-trips through errors.IsOf. Pins "+
			"the matching contract caller code uses for error-"+
			"class branching.")

	_, origCode := abciCodeOf(t, ErrCommitmentNotFound)
	_, wrapCode := abciCodeOf(t, wrapped)
	assert.Equal(t, origCode, wrapCode,
		"ABCI code preserved through Wrapf")
}
