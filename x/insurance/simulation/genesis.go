
package simulation

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// randomizedGenesisState applies the default insurance genesis configuration.
func randomizedGenesisState(simState *module.SimulationState) {
	genesis := types.DefaultGenesis()
	bz, err := json.Marshal(genesis)
	if err != nil {
		panic(err)
	}
	simState.GenState[types.ModuleName] = bz
}
