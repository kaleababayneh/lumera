// Package nft wires the nft (Toolpack NFT) module into the Cosmos-SDK application.
package nft

import (
	"encoding/json"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	nftcli "github.com/LumeraProtocol/lumera/x/nft/client/cli"
	"github.com/LumeraProtocol/lumera/x/nft/keeper"
	"github.com/LumeraProtocol/lumera/x/nft/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic implements the basic nft module.
type AppModuleBasic struct{}

func (AppModuleBasic) Name() string { return types.ModuleName }

func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis — the nft GenesisState is a plain Go/JSON struct.
func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	bz, err := json.Marshal(types.DefaultGenesis())
	if err != nil {
		panic(err)
	}
	return bz
}

func (AppModuleBasic) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	if len(bz) == 0 {
		return nil
	}
	var gs types.GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis: %w", types.ModuleName, err)
	}
	return gs.Validate()
}

// RegisterGRPCGatewayRoutes is a no-op for this slice (nft REST not exposed yet).
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *legacyruntime.ServeMux) {}

func (AppModuleBasic) GetTxCmd() *cobra.Command    { return nftcli.GetTxCmd() }
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return nftcli.GetQueryCmd() }

// AppModule implements the nft application module.
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

func (AppModule) IsAppModule()           {}
func (AppModule) IsOnePerModuleType()    {}
func (AppModule) ConsensusVersion() uint64 { return 1 }

//nolint:staticcheck // Required to satisfy AppModule interface until crisis removal.
func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, data json.RawMessage) {
	genesis := types.DefaultGenesis()
	if len(data) != 0 {
		if err := json.Unmarshal(data, genesis); err != nil {
			panic(fmt.Errorf("failed to unmarshal nft genesis: %w", err))
		}
	}
	am.keeper.InitGenesis(ctx, genesis)
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	bz, err := json.Marshal(am.keeper.ExportGenesis(ctx))
	if err != nil {
		panic(err)
	}
	return bz
}
