
// Package policies implements the Policies module for on-chain policy management.
package policies

import (
	"encoding/json"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/policies/client/cli"
	"github.com/LumeraProtocol/lumera/x/policies/keeper"
	policiessim "github.com/LumeraProtocol/lumera/x/policies/simulation"
	"github.com/LumeraProtocol/lumera/x/policies/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic defines the basic application module used by the policies module
// providing only codec registration and genesis handling.
type AppModuleBasic struct{}

// Name returns the policies module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the policies module's types on the LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers interface types.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns default genesis state as raw bytes for the policies module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the policies module.
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

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the policies module.
//
// The policies Query service has no google.api.http annotations, so the
// gogoproto/grpc-gateway generator produces no REST handlers to register. The
// queries remain reachable over gRPC; this gateway hook is intentionally a
// no-op until HTTP annotations are added to the proto.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *legacyruntime.ServeMux) {}

// GetTxCmd returns the root tx command for the policies module.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return cli.GetTxCmd() }

// GetQueryCmd returns no root query command for the policies module.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return cli.GetQueryCmd() }

// AppModule implements an application module for the policies module.
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

// RegisterInvariants is a no-op for the policies module.
//
//nolint:staticcheck // SA1019: legacy invariant registry remains until x/crisis removal lands upstream.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServer(am.keeper))
}

// InitGenesis performs genesis initialization for the policies module.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesis()
	if len(data) != 0 {
		genesis = &types.GenesisState{}
		cdc.MustUnmarshalJSON(data, genesis)
	}

	// Use the keeper's ImportState for full genesis import
	if err := am.keeper.ImportState(ctx, genesis); err != nil {
		panic(fmt.Errorf("failed to import policies genesis: %w", err))
	}
}

// ExportGenesis returns the exported genesis state as raw bytes for the policies module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	genesis, err := am.keeper.ExportState(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export policies genesis: %w", err))
	}
	return cdc.MustMarshalJSON(genesis)
}

// ConsensusVersion implements AppModule/Configurator.
func (AppModule) ConsensusVersion() uint64 { return keeper.ConsensusVersion }

// ---------------------------------------------------------------------------
// Simulation
// ---------------------------------------------------------------------------

// GenerateGenesisState creates a randomized GenesisState for simulation testing.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	policiessim.RandomizedGenState(simState)
}

// RegisterStoreDecoder registers a decoder for policies module's types.
func (am AppModule) RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry) {
	sdr[types.StoreKey] = policiessim.NewDecodeStore(am.keeper)
}

// WeightedOperations returns simulation operations with weights for the policies module.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return policiessim.WeightedOperations(
		simState.AppParams,
		simState.Cdc,
		*am.keeper,
	)
}
