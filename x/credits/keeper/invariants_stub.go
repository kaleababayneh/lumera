//go:build cosmos && !cosmos_full

package keeper

import sdk "github.com/cosmos/cosmos-sdk/types"

// RegisterInvariants is a no-op in the stub build.
func RegisterInvariants(sdk.InvariantRegistry, Keeper) {}

// AllInvariants returns a dummy invariant in the stub build.
func AllInvariants(Keeper) sdk.Invariant {
	return func(sdk.Context) (string, bool) { return "credits invariants stub", false }
}

// ActiveLockBalanceInvariant returns a dummy invariant in the stub build.
func ActiveLockBalanceInvariant(Keeper) sdk.Invariant {
	return func(sdk.Context) (string, bool) { return "credits invariants stub", false }
}

// LockStateInvariant returns a dummy invariant in the stub build.
func LockStateInvariant(Keeper) sdk.Invariant {
	return func(sdk.Context) (string, bool) { return "credits invariants stub", false }
}

// TotalSupplyInvariant returns a dummy invariant in the stub build.
func TotalSupplyInvariant(Keeper) sdk.Invariant {
	return func(sdk.Context) (string, bool) { return "credits invariants stub", false }
}
