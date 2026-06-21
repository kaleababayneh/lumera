// Package incentives wires the incentives (reputation/badge) module into the
// Cosmos-SDK application.
package incentives

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	incentivescli "github.com/LumeraProtocol/lumera/x/incentives/client/cli"
	"github.com/LumeraProtocol/lumera/x/incentives/keeper"
	"github.com/LumeraProtocol/lumera/x/incentives/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic implements the basic incentives module.
type AppModuleBasic struct{}

// Name returns the incentives module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the incentives amino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the incentives interface types.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns the incentives default genesis as raw bytes.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis validates the incentives genesis state.
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

// RegisterGRPCGatewayRoutes is a no-op (REST is not exposed for this module).
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *legacyruntime.ServeMux) {}

// GetTxCmd returns the incentives tx root command.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return incentivescli.GetTxCmd() }

// GetQueryCmd returns the incentives query root command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return incentivescli.GetQueryCmd() }

// AppModule implements the incentives application module.
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

// NewAppModule constructs the incentives AppModule.
func NewAppModule(k keeper.Keeper) AppModule {
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

// RegisterServices registers the incentives msg + query servers.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServer(&am.keeper))
}

// InitGenesis initializes the incentives module state.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesis()
	if len(data) != 0 {
		cdc.MustUnmarshalJSON(data, genesis)
	}
	am.keeper.InitGenesis(ctx, genesis)
}

// ExportGenesis exports the incentives module state.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(am.keeper.ExportGenesis(ctx))
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 1 }

// EndBlock runs the periodic badge-expiry sweep (re-evaluate or revoke expired
// badges), gated to the configured evaluation interval.
func (am AppModule) EndBlock(goCtx context.Context) error {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := am.keeper.GetParams(ctx)
	interval := uint64(0)
	if params != nil {
		interval = uint64(params.EvaluationIntervalBlocks)
	}
	if interval == 0 {
		return nil
	}
	if ctx.BlockHeight() > 0 && uint64(ctx.BlockHeight())%interval == 0 {
		am.keeper.ProcessExpiredBadges(ctx)
	}
	return nil
}
