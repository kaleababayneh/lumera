// Package workflows implements the Agent Contracts workflow module scaffold.
package workflows

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
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	workflowscli "github.com/LumeraProtocol/lumera/x/workflows/client/cli"
	"github.com/LumeraProtocol/lumera/x/workflows/keeper"
	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

var (
	_ module.AppModule          = AppModule{}
	_ module.AppModuleBasic     = AppModuleBasic{}
	_ appmodule.HasBeginBlocker = AppModule{}
	_ appmodule.HasEndBlocker   = AppModule{}
)

// AppModuleBasic defines the basic application module used by x/workflows.
type AppModuleBasic struct{}

// Name returns the workflows module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers workflows module scaffold types.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers workflows interfaces.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns default genesis state as raw bytes for the workflows module.
func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	bz, err := json.Marshal(types.DefaultGenesis())
	if err != nil {
		panic(fmt.Errorf("failed to marshal default workflows genesis: %w", err))
	}
	return bz
}

// ValidateGenesis performs genesis state validation for the workflows module.
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

// RegisterGRPCGatewayRoutes is intentionally empty until query services land.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(client.Context, *legacyruntime.ServeMux) {}

// GetTxCmd returns no root tx command until tx services land.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return workflowscli.GetTxCmd() }

// GetQueryCmd returns no root query command until query services land.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return workflowscli.GetQueryCmd() }

// AppModule implements an application module for x/workflows.
type AppModule struct {
	AppModuleBasic
	keeper *keeper.Keeper
}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType marks the module as one-per-module in depinject graphs.
func (AppModule) IsOnePerModuleType() {}

// NewAppModule creates a new AppModule object.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

// RegisterInvariants wires workflows invariants into the application registry.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices registers the workflows msg + query servers (author-facing
// publish/upgrade/deactivate + bond management, and the read service).
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

// InitGenesis performs genesis initialization for the workflows module.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesis()
	if len(data) != 0 {
		genesis = &types.GenesisState{}
		if err := json.Unmarshal(data, genesis); err != nil {
			panic(fmt.Errorf("failed to unmarshal workflows genesis: %w", err))
		}
	}
	if err := am.keeper.InitGenesis(ctx, genesis); err != nil {
		panic(fmt.Errorf("failed to init workflows genesis: %w", err))
	}
}

// ExportGenesis returns the exported genesis state as raw bytes for the workflows module.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	genesis, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export workflows genesis: %w", err))
	}
	bz, err := json.Marshal(genesis)
	if err != nil {
		panic(fmt.Errorf("failed to marshal workflows genesis: %w", err))
	}
	return bz
}

// BeginBlock implements appmodule.BeginBlocker.
func (am AppModule) BeginBlock(ctx context.Context) error {
	am.keeper.EmitLifecycleEvent(ctx, "begin_block")
	return nil
}

// EndBlock implements appmodule.EndBlocker.
func (am AppModule) EndBlock(ctx context.Context) error {
	am.keeper.EmitLifecycleEvent(ctx, "end_block")
	return nil
}

// ConsensusVersion implements AppModule/Configurator.
func (AppModule) ConsensusVersion() uint64 { return keeper.ConsensusVersion }

// GenerateGenesisState creates default GenesisState for simulation scaffolding.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	if simState == nil {
		return
	}
	bz, err := json.Marshal(types.DefaultGenesis())
	if err != nil {
		panic(fmt.Errorf("failed to marshal workflows simulation genesis: %w", err))
	}
	simState.GenState[types.ModuleName] = bz
}

// RegisterStoreDecoder registers no custom decoder until workflow storage matures.
func (AppModule) RegisterStoreDecoder(simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns no randomized operations for the scaffold.
func (AppModule) WeightedOperations(module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
