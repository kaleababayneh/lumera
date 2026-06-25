package routermodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	registrykeeper "github.com/LumeraProtocol/lumera/x/registry/keeper"
	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
	router "github.com/LumeraProtocol/lumera/x/router"
	routerkeeper "github.com/LumeraProtocol/lumera/x/router/keeper"
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

	RegistryKeeper registrykeeper.Keeper
	CreditsKeeper  *creditskeeper.Keeper
}

type ModuleOutputs struct {
	depinject.Out

	RouterKeeper *routerkeeper.Keeper
	Module       appmodule.AppModule
}

// routerRegistryAdapter adapts the registry keeper to the router's
// RegistryKeeper interface. Tool discovery (GetToolCard/GetAllTools) delegates
// to the real registry. SubmitReceipt maps the router's (receipt, signature)
// shape onto the registry's (attestor, receipt) shape using the receipt's
// RouterAddress as the attestor — the registry then enforces its own
// supernode-attestation rules. GetToolMetrics has no registry-side snapshot
// yet, so it reports "no metrics" (the router's selection logic falls back
// gracefully).
type routerRegistryAdapter struct {
	rk registrykeeper.Keeper
}

func (a routerRegistryAdapter) GetToolCard(ctx sdk.Context, toolID string) (*registrytypes.ToolCard, bool) {
	return a.rk.GetToolCard(ctx, toolID)
}

func (a routerRegistryAdapter) GetAllTools(ctx sdk.Context) []*registrytypes.ToolCard {
	return a.rk.GetAllTools(ctx)
}

func (a routerRegistryAdapter) SubmitReceipt(ctx sdk.Context, receipt *registrytypes.UsageReceipt, _ []byte) error {
	attestor := ""
	if receipt != nil {
		attestor = receipt.RouterAddress
	}
	return a.rk.SubmitReceipt(ctx, attestor, receipt)
}

func (a routerRegistryAdapter) GetToolMetrics(_ sdk.Context, _ string) (*registrytypes.ToolMetrics, bool) {
	return nil, false
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := routerkeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.Logger,
		authority,
		routerRegistryAdapter{rk: in.RegistryKeeper},
		in.CreditsKeeper,
	)

	m := router.NewAppModule(k)

	return ModuleOutputs{RouterKeeper: k, Module: m}
}
