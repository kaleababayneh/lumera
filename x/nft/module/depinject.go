package nftmodule

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	nft "github.com/LumeraProtocol/lumera/x/nft"
	nftkeeper "github.com/LumeraProtocol/lumera/x/nft/keeper"
	nfttypes "github.com/LumeraProtocol/lumera/x/nft/types"
)

func init() {
	appmodule.Register(
		&Module{},
		appmodule.Provide(ProvideModule),
	)
}

type ModuleInputs struct {
	depinject.In

	StoreService store.KVStoreService
	Cdc          codec.Codec
	Config       *Module

	BankKeeper nfttypes.BankKeeper
}

type ModuleOutputs struct {
	depinject.Out

	NFTKeeper nftkeeper.Keeper
	Module    appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority).String()
	}

	k := nftkeeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.BankKeeper,
		authority,
	)

	m := nft.NewAppModule(k)

	return ModuleOutputs{NFTKeeper: k, Module: m}
}
