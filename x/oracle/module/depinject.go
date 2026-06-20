package oraclemodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	oracle "github.com/LumeraProtocol/lumera/x/oracle"
	oraclekeeper "github.com/LumeraProtocol/lumera/x/oracle/keeper"
)

// ----------------------------------------------------------------------------
// App Wiring Setup (mirrors x/credits/module/depinject.go)
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

	OracleKeeper *oraclekeeper.Keeper
	Module       appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	// Default to the governance module account if no authority is configured.
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := oraclekeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authority,
	)

	m := oracle.NewAppModule(k)

	return ModuleOutputs{OracleKeeper: k, Module: m}
}
