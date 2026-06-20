package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// Keeper is the (incrementally ported) registry keeper. This first slice covers
// the ToolCard registry — registration, lookup, and GetToolPublisher (which the
// credits settlement path needs to pay publishers). Bonds, disputes, SLA/SLO,
// receipts, and the remaining surface are ported in later slices; the generated
// UnimplementedMsgServer/UnimplementedQueryServer no-op the not-yet-ported RPCs.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper

	Schema      collections.Schema
	params      collections.Item[*types.Params]
	toolCards   collections.Map[string, *types.ToolCard]
	bondRecords collections.Map[string, *types.BondRecord]
}

// NewKeeper constructs the registry keeper using modern depinject wiring
// (KVStoreService + collections), unlike the legacy lumera_ai keeper which used
// raw store keys and the deprecated param subspace.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	authority string,
) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc:           cdc,
		storeService:  storeService,
		authority:     authority,
		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,
		params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsKey),
			"params",
			collPtrValue[types.Params](cdc),
		),
		toolCards: collections.NewMap(
			sb,
			collections.NewPrefix(types.ToolCardPrefix),
			"tool_cards",
			collections.StringKey,
			collPtrValue[types.ToolCard](cdc),
		),
		bondRecords: collections.NewMap(
			sb,
			collections.NewPrefix(types.BondRecordPrefix),
			"bond_records",
			collections.StringKey,
			collPtrValue[types.BondRecord](cdc),
		),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	return k
}

// Logger returns a module-scoped logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// GetAuthority returns the module authority address.
func (k Keeper) GetAuthority() string { return k.authority }

// GetParams returns the registry module parameters, falling back to defaults
// when none are stored.
func (k Keeper) GetParams(ctx context.Context) types.Params {
	p, err := k.params.Get(ctx)
	if err != nil || p == nil {
		return *types.DefaultParams()
	}
	return *p
}

// SetParams stores the registry module parameters.
func (k Keeper) SetParams(ctx context.Context, p *types.Params) error {
	return k.params.Set(ctx, p)
}
