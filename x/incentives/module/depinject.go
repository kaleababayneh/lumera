package incentivesmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	incentives "github.com/LumeraProtocol/lumera/x/incentives"
	incentiveskeeper "github.com/LumeraProtocol/lumera/x/incentives/keeper"
	incentivestypes "github.com/LumeraProtocol/lumera/x/incentives/types"
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

	AccountKeeper  incentivestypes.AccountKeeper
	BankKeeper     incentivestypes.BankKeeper
	RegistryKeeper registrykeeper.Keeper
}

type ModuleOutputs struct {
	depinject.Out

	IncentivesKeeper incentiveskeeper.Keeper
	Module           appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	// routerKeeper is intentionally nil: the off-chain router is not a node
	// module; the badge engine never invokes it (metrics are fed by
	// authority / Proof-of-Service receipts). See the integration plan.
	k := incentiveskeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.AccountKeeper,
		in.BankKeeper,
		in.RegistryKeeper,
		nil,
		authority,
	)

	m := incentives.NewAppModule(k)

	return ModuleOutputs{IncentivesKeeper: k, Module: m}
}
