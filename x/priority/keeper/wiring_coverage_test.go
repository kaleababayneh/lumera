package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/priority/types"
)

// TestKeeper_Logger_ReturnsNonNil pins the defensive invariant: the
// module logger must never be nil. ResolveAdjustments emits a Warn
// entry on stale-assignment cleanup paths; a nil logger there would
// crash block production rather than emit a diagnostic.
func TestKeeper_Logger_ReturnsNonNil(t *testing.T) {
	_, keeper := setupKeeper(t)
	require.NotNil(t, keeper.Logger(),
		"Logger() must never return nil — stale-assignment cleanup paths dereference directly")
}

// TestKeeper_Logger_IsIdempotent pins that Logger returns the same
// reference across calls. NewKeeper wraps the injected log.Logger with
// a "module" tag exactly once at construction; re-wrapping per call
// would double-tag entries under sustained tier-assignment load.
func TestKeeper_Logger_IsIdempotent(t *testing.T) {
	_, keeper := setupKeeper(t)
	require.Equal(t, keeper.Logger(), keeper.Logger(),
		"Logger() must be a stable reference — re-wrapping would double-tag log entries")
}

// TestKeeper_Logger_TagsByModuleName pins the source-of-truth for the
// logger tag. NewKeeper does fmt.Sprintf("x/%s", types.ModuleName);
// making the module-name constant an assertion surface ensures a
// cross-cutting rename is deliberate.
func TestKeeper_Logger_TagsByModuleName(t *testing.T) {
	require.Equal(t, "priority", types.ModuleName,
		"ModuleName is the source-of-truth for the x/priority logger tag")
}

// TestGetAssignment_SurfacesUnmarshalError covers the previously-uncovered
// branch in GetAssignment where the stored bytes cannot be decoded as a
// PriorityAssignment. A silent mask-as-not-found here would let bogus
// priority state leak into subsequent tier lookups and produce
// unpredictable queue-weight adjustments.
//
// Pattern mirrors TestGetParams_InvalidStoredBytes — direct state
// injection via keeper.state.Assignments is the only way to reach this
// branch deterministically.
func TestGetAssignment_SurfacesUnmarshalError(t *testing.T) {
	sdkCtx, keeper := setupKeeper(t)

	// Directly inject a non-JSON blob under a policy id. Tests are
	// in-package so state.Assignments is reachable.
	require.NoError(t,
		keeper.state.Assignments.Set(sdkCtx, "corrupt-policy", []byte("not-json-at-all")),
		"pre-condition: injected corrupt assignment bytes")

	got, found, err := keeper.GetAssignment(sdkCtx, "corrupt-policy")
	require.Error(t, err,
		"GetAssignment must return the unmarshal error verbatim, not mask as not-found")
	require.Nil(t, got)
	require.False(t, found,
		"found must be false when the stored bytes cannot be decoded — "+
			"a half-shaped assignment is not an assignment")
}
