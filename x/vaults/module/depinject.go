package vaultsmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	reservekeeper "github.com/LumeraProtocol/lumera/x/reserve/keeper"
	vaults "github.com/LumeraProtocol/lumera/x/vaults"
	vaultskeeper "github.com/LumeraProtocol/lumera/x/vaults/keeper"
	vaultstypes "github.com/LumeraProtocol/lumera/x/vaults/types"
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

	BankKeeper    vaultstypes.BankKeeper
	ReserveKeeper *reservekeeper.Keeper
}

type ModuleOutputs struct {
	depinject.Out

	VaultsKeeper *vaultskeeper.Keeper
	Module       appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := vaultskeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.BankKeeper,
		in.ReserveKeeper,
		authority,
		in.Logger,
	)

	m := vaults.NewAppModule(k)

	return ModuleOutputs{VaultsKeeper: k, Module: m}
}
