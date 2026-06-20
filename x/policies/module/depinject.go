package policiesmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	policies "github.com/LumeraProtocol/lumera/x/policies"
	policieskeeper "github.com/LumeraProtocol/lumera/x/policies/keeper"
)

// ----------------------------------------------------------------------------
// App Wiring Setup (mirrors x/oracle/module/depinject.go)
// ----------------------------------------------------------------------------

func init() {
	appmodule.Register(
		&Module{},
		appmodule.Provide(ProvideModule),
	)
}

type ModuleInputs struct {
	depinject.In

	StoreService store.KVStoreService
	Cdc          codec.Codec
	Config       *Module
}

type ModuleOutputs struct {
	depinject.Out

	PoliciesKeeper *policieskeeper.Keeper
	Module         appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	// Default to the governance module account if no authority is configured.
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := policieskeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authority,
	)

	m := policies.NewAppModule(k)

	return ModuleOutputs{PoliciesKeeper: k, Module: m}
}
