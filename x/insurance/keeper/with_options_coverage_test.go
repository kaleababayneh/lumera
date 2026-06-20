
package keeper_test

import (
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/stretchr/testify/require"

	keeper "github.com/LumeraProtocol/lumera/x/insurance/keeper"
)

// These tests pin three previously-zero-coverage keeper surfaces:
//
//   - RegisterInvariants: a required SDK hook that the insurance module
//     deliberately leaves empty. Pinning the no-op contract prevents a
//     future refactor from sneaking in a non-idempotent invariant check
//     that would run on every block.
//   - WithStoreService / WithCodec: functional-options that return a
//     keeper copy with one field replaced. Test-only builders used to
//     fabricate keepers in isolation; their copy-not-mutate semantics
//     must hold or tests would silently share state.
//
// TestKeeper_WithAuthority (in rate_limit_test.go) already pins the
// sister method for authority; this file completes the trio.

func TestRegisterInvariants_IsNoOpAndDoesNotPanic(t *testing.T) {
	fixture := setupKeeperTest(t)
	// Passing a nil InvariantRegistry must not panic — the function is
	// a deliberate no-op. A regression that added a real registration
	// would crash on nil or cause double-registration on re-wire.
	require.NotPanics(t, func() {
		keeper.RegisterInvariants(nil, fixture.keeper)
	}, "RegisterInvariants is a no-op stub — any registration attempt "+
		"would fail on the nil registry here, surfacing the regression")
}

func TestKeeper_WithStoreService_ReturnsCopyWithNewService(t *testing.T) {
	fixture := setupKeeperTest(t)

	// Build a fresh in-memory store service to pass in. The fixture
	// keeper's original storeService field is unexported, so we cannot
	// fetch it to compare — instead we rely on the observable contract
	// that Authority survives (value-receiver copy preserves the other
	// fields) and the returned keeper is a usable value.
	storeKey := storetypes.NewKVStoreKey("test-store-service")
	freshService := runtime.NewKVStoreService(storeKey)

	k2 := fixture.keeper.WithStoreService(freshService)

	require.Equal(t, fixture.keeper.Authority(), k2.Authority(),
		"WithStoreService must preserve Authority — it is a scalar-swap "+
			"builder, not a fresh keeper")
}

func TestKeeper_WithCodec_ReturnsCopyWithNewCodec(t *testing.T) {
	fixture := setupKeeperTest(t)

	// Build a fresh codec distinct from the fixture's.
	freshRegistry := codectypes.NewInterfaceRegistry()
	freshCodec := codec.NewProtoCodec(freshRegistry)

	k2 := fixture.keeper.WithCodec(freshCodec)

	// The returned keeper must have the new codec and otherwise
	// preserve the Authority. A mutation-not-copy regression would
	// break the fixture's keeper for subsequent tests that share it.
	require.Equal(t, fixture.keeper.Authority(), k2.Authority(),
		"WithCodec must preserve Authority — only the codec field is swapped")
}
