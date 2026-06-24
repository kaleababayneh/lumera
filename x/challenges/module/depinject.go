package challengesmodule

import (
	"context"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	challenges "github.com/LumeraProtocol/lumera/x/challenges"
	challengeskeeper "github.com/LumeraProtocol/lumera/x/challenges/keeper"
	challengestypes "github.com/LumeraProtocol/lumera/x/challenges/types"
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
	Logger       log.Logger
	Config       *Module

	AccountKeeper  challengestypes.AccountKeeper
	BankKeeper     challengestypes.BankKeeper
	RegistryKeeper registrykeeper.Keeper
}

type ModuleOutputs struct {
	depinject.Out

	ChallengesKeeper *challengeskeeper.Keeper
	Module           appmodule.AppModule
}

// challengesRegistryAdapter adapts the registry keeper to the challenges
// RegistryKeeper interface. Tool-category eligibility is served by the real
// registry; the SLO-probe lifecycle hooks are not yet backed by registry SLO
// state, so they return an explicit unsupported error. Those hooks gate only
// the SLO_PROBE challenge type — the core tournament flow (performance,
// quality, conformance, composite) never touches them.
type challengesRegistryAdapter struct {
	rk registrykeeper.Keeper
}

func (a challengesRegistryAdapter) GetToolCategories(ctx context.Context, toolID string) ([]string, bool) {
	return a.rk.GetToolCategories(ctx, toolID)
}

func (a challengesRegistryAdapter) RecordSLOProbeChallengeIssued(context.Context, challengestypes.SLOProbeChallengeReference) error {
	return fmt.Errorf("challenges: SLO-probe challenge lifecycle is not supported on this chain")
}

func (a challengesRegistryAdapter) RecordSLOProbeChallengeOutcome(context.Context, challengestypes.SLOProbeChallengeReference) error {
	return fmt.Errorf("challenges: SLO-probe challenge lifecycle is not supported on this chain")
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := challengeskeeper.NewKeeper(in.Cdc, in.StoreService, in.Logger)
	k.SetAuthority(authority)
	k.SetBankKeeper(in.BankKeeper)
	k.SetAccountKeeper(in.AccountKeeper)
	k.SetRegistryKeeper(challengesRegistryAdapter{rk: in.RegistryKeeper})
	// LumeraIDKeeper is intentionally left unset: the IDENTITY_ATTESTATION
	// challenge type (LumeraID nonce/signature proof) is deferred. The keeper
	// nil-guards that path, so it degrades cleanly without it.

	m := challenges.NewAppModule(k)

	return ModuleOutputs{ChallengesKeeper: k, Module: m}
}
