package workflowsmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	workflows "github.com/LumeraProtocol/lumera/x/workflows"
	workflowskeeper "github.com/LumeraProtocol/lumera/x/workflows/keeper"
)

func init() {
	appmodule.Register(&Module{}, appmodule.Provide(ProvideModule))
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

	WorkflowsKeeper *workflowskeeper.Keeper
	Module          appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}
	k := workflowskeeper.NewKeeper(in.Cdc, in.StoreService, authority, in.Logger)
	return ModuleOutputs{WorkflowsKeeper: k, Module: workflows.NewAppModule(k)}
}
