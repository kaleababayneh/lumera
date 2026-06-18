//go:build cosmos

// Package simulation implements simulation operations for the credits module.
package simulation

import (
	"encoding/json"
	"fmt"
	"math/rand"

	storekv "github.com/cosmos/cosmos-sdk/types/kv"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// RandomizedGenState generates a randomized GenesisState for the credits module.
func RandomizedGenState(simState *module.SimulationState) {
	genesis := types.DefaultGenesis()
	bz, err := json.Marshal(genesis)
	if err != nil {
		panic(fmt.Errorf("credits: failed to marshal default simulation genesis: %w", err))
	}
	simState.GenState[types.ModuleName] = bz
}

// ProposalContents returns no governance proposals for the credits module simulation.
func ProposalContents(_ module.SimulationState) []simtypes.WeightedProposalMsg {
	return nil
}

// ParamChanges returns no randomized param changes for the credits simulation.
func ParamChanges(_ *rand.Rand) []simtypes.LegacyParamChange { return nil }

// NewDecodeStore provides a KVStore decoder for the credits module.
func NewDecodeStore(_ any) func(kvA, kvB storekv.Pair) string {
	return func(kvA, kvB storekv.Pair) string {
		_ = kvA
		_ = kvB
		return ""
	}
}
