package cacmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	cac "github.com/LumeraProtocol/lumera/x/cac"
	cackeeper "github.com/LumeraProtocol/lumera/x/cac/keeper"
	cactypes "github.com/LumeraProtocol/lumera/x/cac/types"
	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	registrykeeper "github.com/LumeraProtocol/lumera/x/registry/keeper"
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

	AccountKeeper  cactypes.AccountKeeper
	BankKeeper     cactypes.BankKeeper
	CreditsKeeper  *creditskeeper.Keeper
	RegistryKeeper registrykeeper.Keeper
}

type ModuleOutputs struct {
	depinject.Out

	CacKeeper cackeeper.Keeper
	Module    appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := cackeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.BankKeeper,
		in.AccountKeeper,
		in.CreditsKeeper,
		in.RegistryKeeper,
		authority,
	)

	m := cac.NewAppModule(k)

	return ModuleOutputs{CacKeeper: k, Module: m}
}
