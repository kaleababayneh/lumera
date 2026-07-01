// Package vaults wires the vault module into the runtime.
package vaults

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	vaultcli "github.com/LumeraProtocol/lumera/x/vaults/client/cli"
	"github.com/LumeraProtocol/lumera/x/vaults/keeper"
	"github.com/LumeraProtocol/lumera/x/vaults/types"
)

var (
	_ module.AppModule          = AppModule{}
	_ module.AppModuleBasic     = AppModuleBasic{}
	_ appmodule.HasBeginBlocker = AppModule{}
	_ appmodule.HasEndBlocker   = AppModule{}
)

// AppModuleBasic implements basic module methods.
type AppModuleBasic struct{}

// Name returns module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers amino codecs (no-op).
func (AppModuleBasic) RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

// RegisterInterfaces registers interface types.
func (AppModuleBasic) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns default genesis state.
func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	bz, err := json.Marshal(types.DefaultGenesis())
	if err != nil {
		panic(fmt.Errorf("failed to marshal default vaults genesis: %w", err))
	}
	return bz
}

// ValidateGenesis validates genesis data.
func (AppModuleBasic) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	if len(bz) == 0 {
		return types.DefaultGenesis().Validate()
	}
	var state types.GenesisState
	if err := json.Unmarshal(bz, &state); err != nil {
		return err
	}
	return state.Validate()
}

// RegisterGRPCGatewayRoutes is a no-op (vaults exposes no REST gateway routes;
// queries are reachable over gRPC and the CLI).
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *legacyruntime.ServeMux) {}

// GetTxCmd returns the root tx command.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return vaultcli.GetTxCmd() }

// GetQueryCmd returns the root query command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return vaultcli.GetQueryCmd() }

// AppModule implements the Cosmos SDK AppModule interface.
type AppModule struct {
	AppModuleBasic
	keeper *keeper.Keeper
}

// NewAppModule creates a new AppModule.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{keeper: k}
}

// IsAppModule satisfies depinject expectations.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType enforces a single module instance in depinject graphs.
func (AppModule) IsOnePerModuleType() {}

// RegisterServices registers gRPC services.
func (am AppModule) RegisterServices(registrar module.Configurator) {
	types.RegisterMsgServer(registrar.MsgServer(), keeper.NewMsgServer(am.keeper))
	types.RegisterQueryServer(registrar.QueryServer(), keeper.NewQueryServer(am.keeper))
}

// RegisterInvariants registers module invariants.
func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// InitGenesis initializes module state from genesis data.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesis()
	if len(data) != 0 {
		if err := json.Unmarshal(data, genesis); err != nil {
			panic(fmt.Errorf("failed to unmarshal vaults genesis: %w", err))
		}
	}
	if err := am.keeper.InitGenesis(ctx, genesis); err != nil {
		panic(fmt.Errorf("failed to init vaults genesis: %w", err))
	}
}

// ExportGenesis exports module state to genesis format.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	state, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export vaults genesis: %w", err))
	}
	bz, err := json.Marshal(state)
	if err != nil {
		panic(fmt.Errorf("failed to marshal vaults genesis: %w", err))
	}
	return bz
}

// ConsensusVersion returns module consensus version.
func (AppModule) ConsensusVersion() uint64 { return 1 }

// BeginBlock executes begin blocker logic.
// Intentionally a no-op: the vaults module has no per-block housekeeping
// (no TTL pruning, no period rollover, no auto-unlock timers). Adding
// block-scoped work here would widen the module's state surface — if
// that becomes necessary, wire a keeper method rather than pushing
// logic directly into the hook.
func (AppModule) BeginBlock(context.Context) error { return nil }

// EndBlock executes end blocker logic.
func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.RefundExpiredVaults(ctx)
}
