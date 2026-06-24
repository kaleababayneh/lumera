package paymentrailsmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	oraclekeeper "github.com/LumeraProtocol/lumera/x/oracle/keeper"
	paymentrails "github.com/LumeraProtocol/lumera/x/payment_rails"
	paymentrailskeeper "github.com/LumeraProtocol/lumera/x/payment_rails/keeper"
	paymentrailstypes "github.com/LumeraProtocol/lumera/x/payment_rails/types"
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

	BankKeeper    paymentrailstypes.BankKeeper
	CreditsKeeper *creditskeeper.Keeper
	OracleKeeper  *oraclekeeper.Keeper
}

type ModuleOutputs struct {
	depinject.Out

	PaymentRailsKeeper *paymentrailskeeper.Keeper
	Module             appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := paymentrailskeeper.NewKeeper(in.Cdc, in.StoreService, authority, in.Logger)
	k.SetBankKeeper(in.BankKeeper)
	k.SetCreditsKeeper(in.CreditsKeeper)
	k.SetOracleKeeper(in.OracleKeeper)

	m := paymentrails.NewAppModule(k)

	return ModuleOutputs{PaymentRailsKeeper: k, Module: m}
}
