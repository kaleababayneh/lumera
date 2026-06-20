
package simulation

import (
	"math/rand"

	storekv "github.com/cosmos/cosmos-sdk/types/kv"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

// RandomizedGenState generates a random GenesisState for the insurance module
func RandomizedGenState(simState *module.SimulationState) {
	randomizedGenesisState(simState)
}

// ProposalContents returns governance proposals for the insurance module.
// Intentionally nil: the insurance module's parameter surface is
// governed via MsgUpdateParams (msg-server path), not via the legacy
// WeightedProposalMsg governance hook. This matches every sibling
// sim module (router, policies, challenges, credits, registry) —
// the LegacyParamChange-based proposal hook is a deprecated SDK
// pathway retained in the signature for interface compliance only.
func ProposalContents(module.SimulationState) []simtypes.WeightedProposalMsg {
	return nil
}

// ParamChanges defines randomized param changes for insurance module.
// Intentionally nil: see ProposalContents above — the legacy
// param-change simulation hook is not exercised by the insurance
// module because param updates flow through MsgUpdateParams, which
// is covered by the operations-weight simulation path instead.
func ParamChanges(*rand.Rand) []simtypes.LegacyParamChange {
	return nil
}

// NewDecodeStore provides a KVStore decoder for insurance module state
func NewDecodeStore(_ any) func(storekv.Pair, storekv.Pair) string {
	return func(_, _ storekv.Pair) string {
		// Basic decoder for debugging simulation state
		return "insurance module state comparison"
	}
}
