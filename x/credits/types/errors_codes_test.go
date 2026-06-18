//go:build cosmos

package types

import (
	"testing"

	"cosmossdk.io/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes DIRECT-test coverage for the SPECIFIC ABCI
// CODES of x/credits/types/errors.go sentinels. The existing
// TestSentinelErrors in types_test.go only pins 'not nil +
// has message' — it does NOT pin the numeric codes that
// travel over the wire in ABCI tx responses.
//
// Scan-angle #6 (security-critical invariants tested only at
// happy path). The error codes are a WIRE-FACING CONTRACT:
// downstream clients (SDK libraries, explorers, CEX order
// engines) match on `(codespace, code)` to distinguish error
// classes. A silent renumber breaks every such client.
//
// Scan-angle #5 (sibling-pattern pinning with structural
// invariants):
//   - Codes are contiguous 2..9 (no gaps to preserve for
//     external enumeration)
//   - All 8 codes are pairwise distinct
//   - All 8 carry the credits codespace
//   - Code 1 reserved for internal/unspecified
//
// Scan-angle #4 (historical-fix regression guard) applies
// indirectly: a cosmos-sdk errors upgrade that changed the
// Error.ABCICode() contract would break this test explicitly,
// forcing the port author to verify that downstream consumers
// still see the same codes.

// abciCode extracts the ABCI code from a sentinel error via
// the errors.ABCIInfo helper (which handles both registered
// *Error and stack-wrapped forms).
func abciCodeOf(t *testing.T, err error) (string, uint32) {
	t.Helper()
	codespace, code, _ := errors.ABCIInfo(err, false)
	return codespace, code
}

// TestCreditsErrors_ABCICodesPinned is the CRITICAL scan-
// angle #6 anchor. Each sentinel's ABCI code MUST stay at
// its registered value. A refactor that renumbered (even
// accidentally via reordering `errors.Register` calls)
// would break every downstream client.
func TestCreditsErrors_ABCICodesPinned(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		code uint32
	}{
		{"ErrInvalidParams", ErrInvalidParams, 2},
		{"ErrInsufficientFunds", ErrInsufficientFunds, 3},
		{"ErrLockNotFound", ErrLockNotFound, 4},
		{"ErrLockExpired", ErrLockExpired, 5},
		{"ErrLockInactive", ErrLockInactive, 6},
		{"ErrSettlementFailed", ErrSettlementFailed, 7},
		{"ErrDisputeFailed", ErrDisputeFailed, 8},
		{"ErrReleaseFailed", ErrReleaseFailed, 9},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, code := abciCodeOf(t, c.err)
			assert.Equal(t, c.code, code,
				"%s ABCI code = %d must stay pinned. Pins the "+
					"wire-facing contract: downstream clients match "+
					"on (codespace, code). A refactor that reordered "+
					"or inserted an `errors.Register` call earlier in "+
					"the list would shift every subsequent code "+
					"silently, breaking every client with numeric "+
					"code-matching logic.", c.name, c.code)
		})
	}
}

// TestCreditsErrors_CodespacePinned pins the codespace ON the
// wire. All 8 sentinels share codespace = ModuleName =
// "credits". A refactor that registered errors in a different
// module (e.g., via copy-paste) would break ABCI filtering
// downstream.
func TestCreditsErrors_CodespacePinned(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		ErrInvalidParams, ErrInsufficientFunds, ErrLockNotFound,
		ErrLockExpired, ErrLockInactive, ErrSettlementFailed,
		ErrDisputeFailed, ErrReleaseFailed,
	} {
		codespace, _ := abciCodeOf(t, err)
		assert.Equal(t, ModuleName, codespace,
			"codespace == ModuleName for every credits sentinel. "+
				"A refactor passing a different codespace would "+
				"break `errors.IsOf(err, ErrLockNotFound)`-style "+
				"matching used across msg_server and tests.")
	}
}

// TestCreditsErrors_PairwiseDistinctCodes pins that no two
// sentinels share a code. Duplicate codes would make two
// error classes indistinguishable to downstream consumers.
func TestCreditsErrors_PairwiseDistinctCodes(t *testing.T) {
	t.Parallel()
	sentinels := map[string]error{
		"ErrInvalidParams":     ErrInvalidParams,
		"ErrInsufficientFunds": ErrInsufficientFunds,
		"ErrLockNotFound":      ErrLockNotFound,
		"ErrLockExpired":       ErrLockExpired,
		"ErrLockInactive":      ErrLockInactive,
		"ErrSettlementFailed":  ErrSettlementFailed,
		"ErrDisputeFailed":     ErrDisputeFailed,
		"ErrReleaseFailed":     ErrReleaseFailed,
	}
	seen := make(map[uint32]string, len(sentinels))
	for name, err := range sentinels {
		_, code := abciCodeOf(t, err)
		if prev, collision := seen[code]; collision {
			t.Errorf("code %d shared by %s AND %s — pins pairwise "+
				"distinctness: duplicate codes make error classes "+
				"indistinguishable to clients.", code, prev, name)
		}
		seen[code] = name
	}
	assert.Len(t, seen, 8,
		"expected 8 distinct codes")
}

// TestCreditsErrors_ContiguousRange_2To9 pins that the codes
// form a contiguous range 2..9 (no gaps). Cosmos SDK reserves
// 1 for generic errors and 0 for success; the credits module
// allocates 2..9. A refactor that skipped a number would
// leave a gap that a future insertion could fill in a non-
// chronological way, confusing operators reading the error
// table.
func TestCreditsErrors_ContiguousRange2Through9(t *testing.T) {
	t.Parallel()
	// Collect codes.
	codes := make(map[uint32]struct{}, 8)
	for _, err := range []error{
		ErrInvalidParams, ErrInsufficientFunds, ErrLockNotFound,
		ErrLockExpired, ErrLockInactive, ErrSettlementFailed,
		ErrDisputeFailed, ErrReleaseFailed,
	} {
		_, code := abciCodeOf(t, err)
		codes[code] = struct{}{}
	}

	// Expect exactly 2..9.
	for want := uint32(2); want <= 9; want++ {
		_, present := codes[want]
		assert.True(t, present,
			"code %d must be registered (part of contiguous 2..9 "+
				"allocation). Pins against a refactor that skipped a "+
				"number: future additions should append (10, 11, ...) "+
				"not backfill gaps.", want)
	}

	// No codes outside 2..9 (pins that nothing got registered
	// below 2 or above 9 accidentally).
	for c := range codes {
		assert.True(t, c >= 2 && c <= 9,
			"code %d is outside the pinned 2..9 range. New sentinels "+
				"MUST extend the range (register code=10 etc.), not "+
				"insert at arbitrary positions.", c)
	}
}

// TestCreditsErrors_MessagesNonEmpty pins that every sentinel
// has a non-empty human-readable message. An empty message
// would surface as just "error: " in ABCI logs, killing
// operator triage.
func TestCreditsErrors_MessagesNonEmpty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"ErrInvalidParams", ErrInvalidParams},
		{"ErrInsufficientFunds", ErrInsufficientFunds},
		{"ErrLockNotFound", ErrLockNotFound},
		{"ErrLockExpired", ErrLockExpired},
		{"ErrLockInactive", ErrLockInactive},
		{"ErrSettlementFailed", ErrSettlementFailed},
		{"ErrDisputeFailed", ErrDisputeFailed},
		{"ErrReleaseFailed", ErrReleaseFailed},
	} {
		require.NotNil(t, tc.err)
		msg := tc.err.Error()
		assert.NotEmpty(t, msg, "%s must have non-empty message", tc.name)
	}
}

// TestCreditsErrors_CanBeMatchedViaIsOf pins the
// errors.IsOf-compatibility contract. Downstream code uses
// `errors.IsOf(err, ErrLockNotFound)` to branch on error
// class — this only works if the underlying error preserves
// the registered error's identity.
func TestCreditsErrors_WrappedErrorsRoundTripThroughIsOf(t *testing.T) {
	t.Parallel()
	// Wrap a sentinel like msg_server code does:
	// errorsmod.Wrapf(ErrInsufficientFunds, "need at least %d",
	//                  100)
	wrapped := errors.Wrapf(ErrInsufficientFunds, "need at least %d", 100)
	require.Error(t, wrapped)

	// The wrapped error MUST preserve the original's identity
	// for IsOf matching.
	assert.True(t, errors.IsOf(wrapped, ErrInsufficientFunds),
		"wrapped sentinel round-trips through errors.IsOf. Pins "+
			"the matching contract every caller uses for error-"+
			"class branching: a refactor that lost the identity "+
			"chain would break every `errors.IsOf` check in "+
			"msg_server.")

	// And the ABCI code of the wrapped error is the SAME as
	// the original (cosmossdk.io/errors preserves it).
	_, origCode := abciCodeOf(t, ErrInsufficientFunds)
	_, wrapCode := abciCodeOf(t, wrapped)
	assert.Equal(t, origCode, wrapCode,
		"ABCI code preserved through Wrapf")
}
