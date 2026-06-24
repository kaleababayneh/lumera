package passportmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	passport "github.com/LumeraProtocol/lumera/x/passport"
	passportkeeper "github.com/LumeraProtocol/lumera/x/passport/keeper"
	passporttypes "github.com/LumeraProtocol/lumera/x/passport/types"
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

	AccountKeeper passporttypes.AccountKeeper
	BankKeeper    passporttypes.BankKeeper
}

type ModuleOutputs struct {
	depinject.Out

	PassportKeeper passportkeeper.Keeper
	Module         appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := passportkeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.BankKeeper,
		in.AccountKeeper,
		authority,
	)

	m := passport.NewAppModule(k)

	return ModuleOutputs{PassportKeeper: k, Module: m}
}
