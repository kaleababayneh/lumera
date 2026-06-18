//go:build cosmos && !cosmos_full

package credits

import (
	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker is a no-op stub for light builds.
func BeginBlocker(_ sdk.Context, _ *keeper.Keeper) error { return nil }

// EndBlocker is a no-op stub for light builds.
func EndBlocker(_ sdk.Context, _ *keeper.Keeper) error { return nil }
