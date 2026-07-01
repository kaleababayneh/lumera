// Package payment_rails wires the programmable-settlement payment-rails module
// into the app.
package payment_rails

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

	paymentrailscli "github.com/LumeraProtocol/lumera/x/payment_rails/client/cli"
	"github.com/LumeraProtocol/lumera/x/payment_rails/keeper"
	"github.com/LumeraProtocol/lumera/x/payment_rails/types"
)

// ConsensusVersion defines the current module consensus version.
const ConsensusVersion = 1

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
	_ appmodule.AppModule   = AppModule{}
)

// AppModuleBasic implements the basic payment_rails module.
type AppModuleBasic struct{}

// Name returns the payment_rails module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the payment_rails amino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the payment_rails interface types.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns the payment_rails default genesis as raw bytes.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis validates the payment_rails genesis state.
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

// RegisterGRPCGatewayRoutes is a no-op (payment_rails REST is reachable over gRPC/CLI).
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *legacyruntime.ServeMux) {}

// GetTxCmd returns the payment_rails tx root command.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return paymentrailscli.GetTxCmd() }

// GetQueryCmd returns the payment_rails query root command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return paymentrailscli.GetQueryCmd() }

// AppModule implements the payment_rails application module.
type AppModule struct {
	AppModuleBasic
	keeper *keeper.Keeper
}

// NewAppModule constructs the payment_rails AppModule.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

// IsAppModule implements appmodule.AppModule.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType marks the module one-per-module in depinject graphs.
func (AppModule) IsOnePerModuleType() {}

// RegisterInvariants is a no-op.
func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices registers the payment_rails msg + query servers.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

// InitGenesis initializes the payment_rails module state.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesis()
	if len(data) != 0 {
		cdc.MustUnmarshalJSON(data, genesis)
	}
	if err := am.keeper.InitGenesis(ctx, genesis); err != nil {
		panic(fmt.Errorf("failed to init payment_rails genesis: %w", err))
	}
}

// ExportGenesis exports the payment_rails module state.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	genesis, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export payment_rails genesis: %w", err))
	}
	return cdc.MustMarshalJSON(genesis)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }
