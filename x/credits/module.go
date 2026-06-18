//go:build cosmos

package credits

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	v2runtime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/spf13/cobra"

	creditscli "github.com/LumeraProtocol/lumera/x/credits/client/cli"
	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/credits/simulation"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic defines the basic application module used by the credits module
// providing only codec registration and genesis handling.
type AppModuleBasic struct{}

// Name returns the credits module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the credits module's types on the LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers interface types.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns default genesis state as raw bytes for the credits module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the credits module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	if len(bz) == 0 {
		return nil
	}
	var data types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return data.Validate()
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the credits module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *legacyruntime.ServeMux) {
	bridge := v2runtime.NewServeMux()
	if err := types.RegisterQueryHandlerClient(context.Background(), bridge, types.NewQueryClient(clientCtx)); err != nil {
		panic(fmt.Errorf("failed to register credits grpc-gateway query client: %w", err))
	}

	delegate := func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		bridge.ServeHTTP(w, r)
	}

	routes := []legacyruntime.Pattern{
		legacyruntime.MustPattern(legacyruntime.NewPattern(1, []int{2, 0, 2, 1}, []string{"lumera.credits.v1.Query", "Lock"}, "")),
		legacyruntime.MustPattern(legacyruntime.NewPattern(1, []int{2, 0, 2, 1}, []string{"lumera.credits.v1.Query", "Locks"}, "")),
		legacyruntime.MustPattern(legacyruntime.NewPattern(1, []int{2, 0, 2, 1}, []string{"lumera.credits.v1.Query", "Hold"}, "")),
		legacyruntime.MustPattern(legacyruntime.NewPattern(1, []int{2, 0, 2, 1}, []string{"lumera.credits.v1.Query", "Holds"}, "")),
		legacyruntime.MustPattern(legacyruntime.NewPattern(1, []int{2, 0, 2, 1}, []string{"lumera.credits.v1.Query", "Params"}, "")),
	}
	for _, route := range routes {
		mux.Handle(http.MethodPost, route, delegate)
	}
}

// GetTxCmd returns the root tx command for the credits module.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return creditscli.GetTxCmd() }

// GetQueryCmd returns no root query command for the credits module.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return creditscli.GetQueryCmd() }

// AppModule implements an application module for the credits module.
type AppModule struct {
	AppModuleBasic
	keeper *keeper.Keeper
}

// IsAppModule implements the appmodule.AppModule interface.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType marks the module as one-per-module in depinject graphs.
func (AppModule) IsOnePerModuleType() {}

// NewAppModule creates a new AppModule object.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

// RegisterInvariants wires the credits invariants into the application registry.
//
//nolint:staticcheck // SA1019: legacy invariant registry remains until x/crisis removal lands upstream.
func (am AppModule) RegisterInvariants(ir sdk.InvariantRegistry) {
	if am.keeper == nil {
		return
	}
	keeper.RegisterInvariants(ir, *am.keeper)
}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServer(am.keeper))
}

// InitGenesis performs genesis initialization for the credits module.
// This handles the full state including locks, settlements, disputes, and metrics.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesis()
	if len(data) != 0 {
		genesis = &types.GenesisState{}
		cdc.MustUnmarshalJSON(data, genesis)
	}

	// Use the keeper's ImportState for full genesis import
	if err := am.keeper.ImportState(ctx, genesis); err != nil {
		panic(fmt.Errorf("failed to import credits genesis: %w", err))
	}
}

// ExportGenesis returns the exported genesis state as raw bytes for the credits module.
// This exports the full state including locks, settlements, disputes, and metrics.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	genesis, err := am.keeper.ExportState(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export credits genesis: %w", err))
	}
	return cdc.MustMarshalJSON(genesis)
}

// BeginBlock implements appmodule.BeginBlocker interface.
func (am AppModule) BeginBlock(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return BeginBlocker(sdkCtx, am.keeper)
}

// EndBlock implements appmodule.EndBlocker interface.
func (am AppModule) EndBlock(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return EndBlocker(sdkCtx, am.keeper)
}

// ConsensusVersion implements AppModule/Configurator.
// Version history:
//   - 1: Initial version with basic lock and params support
//   - 2: Full Collections v2 migration with settlements, disputes, and metrics
func (AppModule) ConsensusVersion() uint64 { return keeper.ConsensusVersion }

// RegisterMigrations registers module migrations.
// This is called by the Cosmos SDK during app initialization to wire up
// any necessary state migrations for chain upgrades.
func (am AppModule) RegisterMigrations(cfg module.Configurator) error {
	// Migration from v1 to v2: adds full settlements, disputes, metrics support
	// The actual migration is handled by MigrateV1ToV2 in the keeper
	if err := cfg.RegisterMigration(types.ModuleName, 1, func(ctx sdk.Context) error {
		return am.keeper.MigrateV1ToV2(ctx)
	}); err != nil {
		return fmt.Errorf("failed to register credits v1->v2 migration: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Simulation
// ---------------------------------------------------------------------------

// GenerateGenesisState creates a randomized GenesisState for simulation testing.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomizedGenState(simState)
}

// RegisterStoreDecoder registers a decoder for credits module's types.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = simulation.NewDecodeStore(am.keeper)
}

// WeightedOperations returns simulation operations with weights for the credits module.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return simulation.WeightedOperations(
		simState.AppParams,
		simState.Cdc,
		am.keeper,
		am.keeper.AccountKeeper(),
		am.keeper.BankKeeper(),
	)
}
