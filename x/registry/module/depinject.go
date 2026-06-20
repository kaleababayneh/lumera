package registrymodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	registry "github.com/LumeraProtocol/lumera/x/registry"
	registrykeeper "github.com/LumeraProtocol/lumera/x/registry/keeper"
	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
)

// ----------------------------------------------------------------------------
// App Wiring Setup (mirrors x/insurance/module/depinject.go)
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

	AccountKeeper registrytypes.AccountKeeper
	BankKeeper    registrytypes.BankKeeper
}

type ModuleOutputs struct {
	depinject.Out

	RegistryKeeper registrykeeper.Keeper
	Module         appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := registrykeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.AccountKeeper,
		in.BankKeeper,
		authority,
	)

	m := registry.NewAppModule(k)

	return ModuleOutputs{RegistryKeeper: k, Module: m}
}
