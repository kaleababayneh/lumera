// Package cac wires the content-addressable cache module into the app.
package cac

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

	caccli "github.com/LumeraProtocol/lumera/x/cac/client/cli"
	"github.com/LumeraProtocol/lumera/x/cac/keeper"
	"github.com/LumeraProtocol/lumera/x/cac/types"
)

// ConsensusVersion defines the current module consensus version.
const ConsensusVersion = 1

var (
	_ module.AppModule        = AppModule{}
	_ module.AppModuleBasic   = AppModuleBasic{}
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// AppModuleBasic implements the basic cac module.
type AppModuleBasic struct{}

// Name returns the cac module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the cac amino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the cac interface types.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns the cac default genesis as raw bytes. The cac
// GenesisState is a hand-written JSON struct (not a proto message), so it is
// marshaled with encoding/json rather than the proto JSON codec.
func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	bz, err := json.Marshal(types.DefaultGenesisState())
	if err != nil {
		panic(fmt.Errorf("failed to marshal cac default genesis: %w", err))
	}
	return bz
}

// ValidateGenesis validates the cac genesis state.
func (AppModuleBasic) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	if len(bz) == 0 {
		return nil
	}
	var data types.GenesisState
	if err := json.Unmarshal(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return data.Validate()
}

// RegisterGRPCGatewayRoutes is a no-op (cac REST is reachable over gRPC/CLI).
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *legacyruntime.ServeMux) {}

// GetTxCmd returns the cac tx root command.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return caccli.GetTxCmd() }

// GetQueryCmd returns the cac query root command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return caccli.GetQueryCmd() }

// AppModule implements the cac application module.
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

// NewAppModule constructs the cac AppModule.
func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType marks the module one-per-module in depinject graphs.
func (AppModule) IsOnePerModuleType() {}

// RegisterInvariants is a no-op.
func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices registers the cac msg + query servers.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(&am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(&am.keeper))
}

// InitGenesis initializes the cac module state.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesisState()
	if len(data) != 0 {
		if err := json.Unmarshal(data, genesis); err != nil {
			panic(fmt.Errorf("failed to unmarshal cac genesis: %w", err))
		}
	}
	if err := am.keeper.InitGenesis(ctx, genesis); err != nil {
		panic(fmt.Errorf("failed to init cac genesis: %w", err))
	}
}

// ExportGenesis exports the cac module state.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	genesis, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export cac genesis: %w", err))
	}
	bz, err := json.Marshal(genesis)
	if err != nil {
		panic(fmt.Errorf("failed to marshal cac genesis: %w", err))
	}
	return bz
}

// EndBlock auto-expires cache entries (TTL eviction). Errors are logged, not
// fatal — a transient eviction failure must never halt the chain.
func (am AppModule) EndBlock(ctx context.Context) error {
	if _, err := am.keeper.TickDecay(ctx, 100); err != nil {
		am.keeper.Logger(sdk.UnwrapSDKContext(ctx)).Error("failed to process expired cache entries", "error", err)
	}
	return nil
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
