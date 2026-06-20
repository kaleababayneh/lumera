
// Package oracle wires the oracle module into the Cosmos-SDK application runtime.
package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	oraclecli "github.com/LumeraProtocol/lumera/x/oracle/client/cli"
	"github.com/LumeraProtocol/lumera/x/oracle/keeper"
	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic defines the basic application module used by the oracle module.
type AppModuleBasic struct{}

// Name returns the oracle module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the oracle module's types on the LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers interface types.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns default genesis state as raw bytes for the oracle module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(&types.GenesisState{Params: types.DefaultParams()})
}

// ValidateGenesis validates the oracle module's genesis state.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	genesis := &types.GenesisState{Params: types.DefaultParams()}
	if len(bz) != 0 {
		if err := cdc.UnmarshalJSON(bz, genesis); err != nil {
			return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
		}
	}
	if genesis.Params == nil {
		genesis.Params = types.DefaultParams()
	}
	return validateGenesisState(genesis)
}

// RegisterGRPCGatewayRoutes registers the gRPC gateway routes for the module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *legacyruntime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(fmt.Errorf("failed to register oracle grpc-gateway query client: %w", err))
	}
}

// GetTxCmd returns no root tx command for the oracle module (votes are injected via ABCI).
func (AppModuleBasic) GetTxCmd() *cobra.Command { return nil }

// GetQueryCmd returns the root query command for the oracle module.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return oraclecli.GetQueryCmd() }

// AppModule implements an application module for the oracle module.
type AppModule struct {
	AppModuleBasic
	keeper *keeper.Keeper
}

// IsAppModule satisfies the depinject tag interface.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType marks the module as one-per-module in depinject graphs.
func (AppModule) IsOnePerModuleType() {}

// NewAppModule creates a new AppModule object.
func NewAppModule(k *keeper.Keeper) AppModule {
	return AppModule{AppModuleBasic: AppModuleBasic{}, keeper: k}
}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(*am.keeper))
}

// InitGenesis performs genesis initialization for the oracle module.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	genesis := &types.GenesisState{Params: types.DefaultParams()}
	if len(data) != 0 {
		cdc.MustUnmarshalJSON(data, genesis)
	}
	if err := validateGenesisState(genesis); err != nil {
		panic(fmt.Errorf("invalid oracle genesis: %w", err))
	}

	params := genesis.Params
	if params == nil {
		params = types.DefaultParams()
	}
	if err := am.keeper.SetParams(ctx, params); err != nil {
		panic(fmt.Errorf("failed to set oracle params: %w", err))
	}

	for _, feed := range genesis.PriceFeeds {
		if feed == nil {
			continue
		}
		if err := am.keeper.SetPriceFeed(ctx, feed); err != nil {
			panic(fmt.Errorf("failed to set oracle price feed: %w", err))
		}
	}

	for _, aggregated := range genesis.AggregatedPrices {
		if aggregated == nil {
			continue
		}
		if err := am.keeper.SetAggregatedPrice(ctx, aggregated); err != nil {
			panic(fmt.Errorf("failed to set oracle aggregated price: %w", err))
		}
	}
}

func validateGenesisState(genesis *types.GenesisState) error {
	if genesis == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	params := genesis.Params
	if params == nil {
		params = types.DefaultParams()
	}
	if err := params.Validate(); err != nil {
		return err
	}
	if err := validateGenesisPriceFeeds(genesis.PriceFeeds); err != nil {
		return err
	}
	return validateGenesisAggregatedPrices(genesis.AggregatedPrices)
}

func validateGenesisPriceFeeds(feeds []*types.PriceFeed) error {
	seen := make(map[string]struct{}, len(feeds))
	for i, feed := range feeds {
		if feed == nil {
			return types.ErrInvalidPrice.Wrapf("price_feed[%d] cannot be nil", i)
		}
		assetPair, err := validateGenesisAssetPair(feed.AssetPair, fmt.Sprintf("price_feed[%d]", i))
		if err != nil {
			return err
		}
		if _, ok := seen[assetPair]; ok {
			return types.ErrInvalidAssetPair.Wrapf("duplicate price_feed asset pair %q", assetPair)
		}
		seen[assetPair] = struct{}{}
		if err := validatePositiveGenesisDec(feed.Price, fmt.Sprintf("price_feed[%d] price", i)); err != nil {
			return err
		}
		if err := validateOptionalNonNegativeGenesisDec(feed.Volume_24H, fmt.Sprintf("price_feed[%d] volume_24h", i)); err != nil {
			return err
		}
		if err := validateOptionalConfidenceGenesisDec(feed.ConfidenceScore, fmt.Sprintf("price_feed[%d] confidence_score", i)); err != nil {
			return err
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("price_feed[%d] timestamp", i), feed.GetTimestamp()); err != nil {
			return err
		}
	}
	return nil
}

func validateGenesisAggregatedPrices(prices []*types.AggregatedPrice) error {
	seen := make(map[string]struct{}, len(prices))
	for i, price := range prices {
		if price == nil {
			return types.ErrInvalidPrice.Wrapf("aggregated_price[%d] cannot be nil", i)
		}
		assetPair, err := validateGenesisAssetPair(price.AssetPair, fmt.Sprintf("aggregated_price[%d]", i))
		if err != nil {
			return err
		}
		if _, ok := seen[assetPair]; ok {
			return types.ErrInvalidAssetPair.Wrapf("duplicate aggregated_price asset pair %q", assetPair)
		}
		seen[assetPair] = struct{}{}
		if err := validatePositiveGenesisDec(price.MedianPrice, fmt.Sprintf("aggregated_price[%d] median price", i)); err != nil {
			return err
		}
		if err := validateOptionalPositiveGenesisDec(price.MeanPrice, fmt.Sprintf("aggregated_price[%d] mean price", i)); err != nil {
			return err
		}
		if err := validateOptionalNonNegativeGenesisDec(price.StandardDeviation, fmt.Sprintf("aggregated_price[%d] standard deviation", i)); err != nil {
			return err
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("aggregated_price[%d] timestamp", i), price.GetTimestamp()); err != nil {
			return err
		}
	}
	return nil
}

func validateGenesisAssetPair(raw, field string) (string, error) {
	assetPair := strings.TrimSpace(raw)
	if assetPair == "" {
		return "", types.ErrInvalidAssetPair.Wrapf("%s asset pair cannot be empty", field)
	}
	if assetPair != raw {
		return "", types.ErrInvalidAssetPair.Wrapf("%s asset pair must not have leading or trailing whitespace", field)
	}
	return assetPair, nil
}

func validateGenesisTimestamp(field string, ts time.Time) error {
	if ts.IsZero() {
		return nil
	}
	if ts.Year() < 1 || ts.Year() > 9999 {
		return types.ErrInvalidPrice.Wrapf("%s is invalid: year out of range", field)
	}
	return nil
}

func validatePositiveGenesisDec(value, field string) error {
	if err := validateGenesisDecExact(value, field); err != nil {
		return err
	}
	dec, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(value))
	if err != nil || !dec.IsPositive() {
		return types.ErrInvalidPrice.Wrapf("%s must be positive", field)
	}
	return nil
}

func validateOptionalPositiveGenesisDec(value, field string) error {
	if value == "" {
		return nil
	}
	if err := validateGenesisDecExact(value, field); err != nil {
		return err
	}
	dec, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(value))
	if err != nil {
		return types.ErrInvalidPrice.Wrapf("invalid %s: %v", field, err)
	}
	if !dec.IsPositive() {
		return types.ErrInvalidPrice.Wrapf("%s must be positive", field)
	}
	return nil
}

func validateOptionalNonNegativeGenesisDec(value, field string) error {
	if value == "" {
		return nil
	}
	if err := validateGenesisDecExact(value, field); err != nil {
		return err
	}
	dec, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(value))
	if err != nil {
		return types.ErrInvalidPrice.Wrapf("invalid %s: %v", field, err)
	}
	if dec.IsNegative() {
		return types.ErrInvalidPrice.Wrapf("%s cannot be negative", field)
	}
	return nil
}

func validateOptionalConfidenceGenesisDec(value, field string) error {
	if value == "" {
		return nil
	}
	if err := validateGenesisDecExact(value, field); err != nil {
		return err
	}
	dec, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(value))
	if err != nil {
		return types.ErrInvalidPrice.Wrapf("invalid %s: %v", field, err)
	}
	if dec.IsNegative() || dec.GT(sdkmath.LegacyOneDec()) {
		return types.ErrInvalidPrice.Wrapf("%s must be between 0 and 1", field)
	}
	return nil
}

func validateGenesisDecExact(value, field string) error {
	if value != strings.TrimSpace(value) {
		return types.ErrInvalidPrice.Wrapf("%s must not have leading or trailing whitespace", field)
	}
	return nil
}

// ExportGenesis returns the exported genesis state as raw bytes for the oracle module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	params, err := am.keeper.GetParams(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export oracle params: %w", err))
	}

	feeds, err := am.keeper.GetAllPriceFeeds(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export oracle price feeds: %w", err))
	}

	aggregated, err := am.keeper.GetAllAggregatedPrices(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export oracle aggregated prices: %w", err))
	}

	return cdc.MustMarshalJSON(&types.GenesisState{
		Params:           params,
		PriceFeeds:       feeds,
		AggregatedPrices: aggregated,
	})
}

// ConsensusVersion defines the module's consensus version for migrations.
func (AppModule) ConsensusVersion() uint64 { return 1 }
