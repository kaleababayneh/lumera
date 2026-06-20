package insurancemodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	insurance "github.com/LumeraProtocol/lumera/x/insurance"
	insurancekeeper "github.com/LumeraProtocol/lumera/x/insurance/keeper"
	insurancetypes "github.com/LumeraProtocol/lumera/x/insurance/types"
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

	AccountKeeper insurancetypes.AccountKeeper
	BankKeeper    insurancetypes.BankKeeper
}

type ModuleOutputs struct {
	depinject.Out

	InsuranceKeeper insurancekeeper.Keeper
	Module          appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	// Default to the governance module account if no authority is configured.
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := insurancekeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.BankKeeper,
		in.AccountKeeper,
		authority,
	)

	m := insurance.NewAppModule(k)

	return ModuleOutputs{InsuranceKeeper: k, Module: m}
}
