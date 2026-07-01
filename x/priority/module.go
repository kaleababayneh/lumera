// Package priority wires the priority-tier keeper (auction/routing support) into
// the app as a minimal state module: it holds the tier params + assignments
// on-chain. It has no tx/query service surface of its own — it is consumed by
// the routing layer — so it ships as a genesis-only module.
package priority

import (
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

	"github.com/LumeraProtocol/lumera/x/priority/keeper"
	"github.com/LumeraProtocol/lumera/x/priority/types"
)

// ConsensusVersion defines the current module consensus version.
const ConsensusVersion = 1

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
	_ appmodule.AppModule   = AppModule{}
)

// AppModuleBasic implements the basic priority module.
type AppModuleBasic struct{}

// Name returns the priority module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec is a no-op (no amino-registered messages).
func (AppModuleBasic) RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

// RegisterInterfaces is a no-op (no proto messages).
func (AppModuleBasic) RegisterInterfaces(_ codectypes.InterfaceRegistry) {}

// DefaultGenesis returns the default priority params as raw JSON.
func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	bz, err := json.Marshal(types.DefaultParams())
	if err != nil {
		panic(fmt.Errorf("failed to marshal priority default genesis: %w", err))
	}
	return bz
}

// ValidateGenesis validates the priority params.
func (AppModuleBasic) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	if len(bz) == 0 {
		return nil
	}
	var p types.Params
	if err := json.Unmarshal(bz, &p); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis: %w", types.ModuleName, err)
	}
	return p.ValidateBasic()
}

// RegisterGRPCGatewayRoutes is a no-op.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *legacyruntime.ServeMux) {}

// GetTxCmd returns no tx command.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return nil }

// GetQueryCmd returns no query command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return nil }

// AppModule implements the priority application module.
type AppModule struct {
	AppModuleBasic
	keeper *keeper.Keeper
}

// NewAppModule constructs the priority AppModule.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType marks the module one-per-module in depinject graphs.
func (AppModule) IsOnePerModuleType() {}

// RegisterInvariants is a no-op.
func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices is a no-op (no msg/query services).
func (AppModule) RegisterServices(_ module.Configurator) {}

// InitGenesis loads the priority params.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, data json.RawMessage) {
	params := types.DefaultParams()
	if len(data) != 0 {
		if err := json.Unmarshal(data, params); err != nil {
			panic(fmt.Errorf("failed to unmarshal priority genesis: %w", err))
		}
	}
	if err := am.keeper.SetParams(ctx, params); err != nil {
		panic(fmt.Errorf("failed to set priority params: %w", err))
	}
}

// ExportGenesis exports the priority params.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	params, err := am.keeper.GetParams(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to get priority params: %w", err))
	}
	bz, err := json.Marshal(params)
	if err != nil {
		panic(fmt.Errorf("failed to marshal priority genesis: %w", err))
	}
	return bz
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
