package reservemodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	reserve "github.com/LumeraProtocol/lumera/x/reserve"
	reservekeeper "github.com/LumeraProtocol/lumera/x/reserve/keeper"
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
	Config       *Module
	Logger       log.Logger
}

type ModuleOutputs struct {
	depinject.Out

	ReserveKeeper *reservekeeper.Keeper
	Module        appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := reservekeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		authority,
		in.Logger,
	)

	m := reserve.NewAppModule(k)

	return ModuleOutputs{ReserveKeeper: k, Module: m}
}
