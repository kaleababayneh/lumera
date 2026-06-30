package auctionmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	auction "github.com/LumeraProtocol/lumera/x/auction"
	auctionkeeper "github.com/LumeraProtocol/lumera/x/auction/keeper"
)

func init() {
	appmodule.Register(&Module{}, appmodule.Provide(ProvideModule))
}

type ModuleInputs struct {
	depinject.In

	StoreService store.KVStoreService
	Cdc          codec.Codec
	Logger       log.Logger
	Config       *Module
}

type ModuleOutputs struct {
	depinject.Out

	AuctionKeeper *auctionkeeper.Keeper
	Module        appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}
	k := auctionkeeper.NewKeeper(in.Cdc, in.StoreService, authority, in.Logger)
	return ModuleOutputs{AuctionKeeper: k, Module: auction.NewAppModule(k)}
}
