package creditsmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	credits "github.com/LumeraProtocol/lumera/x/credits"
	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	insurancekeeper "github.com/LumeraProtocol/lumera/x/insurance/keeper"
	registrykeeper "github.com/LumeraProtocol/lumera/x/registry/keeper"
)

// ----------------------------------------------------------------------------
// App Wiring Setup (mirrors x/lumeraid/module/depinject.go)
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

	AccountKeeper   creditstypes.AccountKeeper
	BankKeeper      creditstypes.BankKeeper
	InsuranceKeeper insurancekeeper.Keeper
	RegistryKeeper  registrykeeper.Keeper
}

type ModuleOutputs struct {
	depinject.Out

	CreditsKeeper *creditskeeper.Keeper
	Module        appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	// Default to the governance module account if no authority is configured.
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := creditskeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.BankKeeper,
		in.AccountKeeper,
		// Insurance + Registry are now REAL keepers (x/insurance, x/registry wired).
		in.InsuranceKeeper,
		in.RegistryKeeper,
		// TEMPORARY stubs — replace with the real module keepers before testnet.
		// Remaining: reserve -> nft (see CLAUDE.md).
		stubReserveKeeper{},
		stubNFTKeeper{},
		authority,
	)

	m := credits.NewAppModule(&k)

	return ModuleOutputs{CreditsKeeper: &k, Module: m}
}
