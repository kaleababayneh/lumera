// Package router wires the routing-telemetry / metrics-aggregation module into
// the app.
package router

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	routercli "github.com/LumeraProtocol/lumera/x/router/client/cli"
	"github.com/LumeraProtocol/lumera/x/router/keeper"
	"github.com/LumeraProtocol/lumera/x/router/types"
)

// ConsensusVersion defines the current module consensus version.
const ConsensusVersion = 1

var (
	_ module.AppModule          = AppModule{}
	_ module.AppModuleBasic     = AppModuleBasic{}
	_ appmodule.AppModule       = AppModule{}
	_ appmodule.HasBeginBlocker = AppModule{}
	_ appmodule.HasEndBlocker   = AppModule{}
)

// AppModuleBasic implements the basic router module.
type AppModuleBasic struct{}

// Name returns the router module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the router amino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the router interface types.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns the router default genesis as raw bytes.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis validates the router genesis state.
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

// RegisterGRPCGatewayRoutes is a no-op (router REST is reachable over gRPC).
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *legacyruntime.ServeMux) {}

// GetTxCmd returns the router tx root command. Only the tool-owner-signable
// activation record is exposed; the authority-gated records are driven by the
// off-chain router over gRPC.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return routercli.GetTxCmd() }

// GetQueryCmd returns the router query root command (params, metrics, rankings).
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return routercli.GetQueryCmd() }

// AppModule implements the router application module.
type AppModule struct {
	AppModuleBasic
	keeper *keeper.Keeper
}

// NewAppModule constructs the router AppModule.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType marks the module one-per-module in depinject graphs.
func (AppModule) IsOnePerModuleType() {}

// RegisterInvariants is a no-op.
//
//nolint:staticcheck // Required to satisfy AppModule interface until crisis removal.
func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices registers the router msg + query servers.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServer(am.keeper))
}

// InitGenesis initializes the router module state.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesis()
	if len(data) != 0 {
		cdc.MustUnmarshalJSON(data, genesis)
	}
	am.keeper.InitGenesis(ctx, genesis)
}

// ExportGenesis exports the router module state.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(am.keeper.ExportGenesis(ctx))
}

// BeginBlock runs the router begin-block lifecycle.
func (am AppModule) BeginBlock(ctx context.Context) error {
	return am.keeper.BeginBlocker(sdk.UnwrapSDKContext(ctx))
}

// EndBlock runs the router end-block lifecycle (replay-cache cleanup, session
// expiry, metrics rollover).
func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.EndBlocker(sdk.UnwrapSDKContext(ctx))
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
