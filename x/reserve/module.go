
// Package reserve wires the reserve keeper into the Cosmos SDK module manager.
package reserve

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

	reservecli "github.com/LumeraProtocol/lumera/x/reserve/client/cli"
	"github.com/LumeraProtocol/lumera/x/reserve/keeper"
	"github.com/LumeraProtocol/lumera/x/reserve/types"
)

// ConsensusVersion defines the current reserve module consensus version.
const ConsensusVersion = 1

var (
	_ module.AppModule        = AppModule{}
	_ module.AppModuleBasic   = AppModuleBasic{}
	_ appmodule.HasEndBlocker = AppModule{}
)

// AppModuleBasic defines the basic application module used by x/reserve.
type AppModuleBasic struct{}

// Name returns the reserve module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the reserve module's amino types.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the reserve module's interface types.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns an empty genesis payload.
func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	return json.RawMessage("{}")
}

// ValidateGenesis validates the reserve module genesis payload.
func (AppModuleBasic) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	if len(bz) == 0 {
		return nil
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	if len(data) != 0 {
		return fmt.Errorf("%s genesis must be empty until reserve exports a full genesis state", types.ModuleName)
	}
	return nil
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for x/reserve.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *legacyruntime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(fmt.Errorf("failed to register reserve grpc-gateway query client: %w", err))
	}
}

// GetTxCmd returns the root tx command for x/reserve.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return reservecli.GetTxCmd() }

// GetQueryCmd returns the root query command for x/reserve.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return reservecli.GetQueryCmd() }

// AppModule implements an application module for x/reserve.
type AppModule struct {
	AppModuleBasic
	keeper *keeper.Keeper
}

// NewAppModule creates a reserve AppModule.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType marks x/reserve as a singleton module.
func (AppModule) IsOnePerModuleType() {}

// RegisterInvariants registers reserve invariants.
//
//nolint:staticcheck // sdk.InvariantRegistry remains until the SDK removes the legacy interface.
func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices registers reserve module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

// InitGenesis initializes reserve genesis.
func (am AppModule) InitGenesis(_ sdk.Context, _ codec.JSONCodec, data json.RawMessage) {
	if err := (AppModuleBasic{}).ValidateGenesis(nil, nil, data); err != nil {
		panic(err)
	}
}

// ExportGenesis exports reserve genesis.
func (AppModule) ExportGenesis(_ sdk.Context, _ codec.JSONCodec) json.RawMessage {
	return json.RawMessage("{}")
}

// EndBlock implements appmodule.HasEndBlocker. It sweeps a bounded batch of
// expired reserve commitments each block so abandoned commitments cannot
// accumulate unboundedly in the module indexes.
func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.EndBlocker(ctx)
}

// ConsensusVersion implements AppModule/Configurator.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
