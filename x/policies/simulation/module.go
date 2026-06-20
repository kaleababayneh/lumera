
// Package simulation provides Cosmos SDK simulation hooks for the policies module.
package simulation

import (
	"encoding/json"
	"math/rand"

	storekv "github.com/cosmos/cosmos-sdk/types/kv"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// RandomizedGenState generates a randomized GenesisState for the policies module.
func RandomizedGenState(simState *module.SimulationState) {
	genesis := types.DefaultGenesis()
	bz, err := json.Marshal(genesis)
	if err != nil {
		panic(err)
	}
	simState.GenState[types.ModuleName] = bz
}

// NewDecodeStore provides a KVStore decoder for the policies module.
func NewDecodeStore(_ any) func(kvA, kvB storekv.Pair) string {
	return func(kvA, kvB storekv.Pair) string {
		_ = kvA
		_ = kvB
		return ""
	}
}

// ProposalContents returns no governance proposals for policies simulation.
func ProposalContents(_ module.SimulationState) []simtypes.WeightedProposalMsg {
	return nil
}

// ParamChanges returns no randomized param changes for policies simulation.
func ParamChanges(_ *rand.Rand) []simtypes.LegacyParamChange { return nil }
