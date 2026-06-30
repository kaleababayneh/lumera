package prioritymodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	priority "github.com/LumeraProtocol/lumera/x/priority"
	prioritykeeper "github.com/LumeraProtocol/lumera/x/priority/keeper"
)

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
	Logger       log.Logger
	Config       *Module
}

type ModuleOutputs struct {
	depinject.Out

	PriorityKeeper *prioritykeeper.Keeper
	Module         appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := prioritykeeper.NewKeeper(in.Cdc, in.StoreService, authority, in.Logger)
	m := priority.NewAppModule(k)

	return ModuleOutputs{PriorityKeeper: k, Module: m}
}
