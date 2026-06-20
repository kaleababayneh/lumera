package keeper

import (
	"context"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/nft/types"
)

// Keeper is the (focused) nft keeper. This slice covers the Toolpack NFT
// registry: mint, lookup (GetToolpack), and royalty recording
// (RecordRoyaltyPayout) — the two methods the credits settlement path consumes
// for toolpack-bundled invocations. Toolpack history, the curator secondary
// index, and cumulative royalty stats are ported as later slices.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	bankKeeper   types.BankKeeper
	authority    string

	Schema    collections.Schema
	toolpacks collections.Map[string, *types.ToolpackNFT]
}

// NewKeeper constructs the nft keeper on modern depinject wiring.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	bankKeeper types.BankKeeper,
	authority string,
) Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc:          cdc,
		storeService: storeService,
		bankKeeper:   bankKeeper,
		authority:    authority,
		toolpacks: collections.NewMap(
			sb,
			collections.NewPrefix(types.ToolpackKeyPrefix),
			"toolpacks",
			collections.StringKey,
			collPtrValue[types.ToolpackNFT](cdc),
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

// canonicalToolpackID trims and validates a toolpack identifier.
func canonicalToolpackID(id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", types.ErrInvalidToolpackID
	}
	return trimmed, nil
}

// ctxOf adapts an sdk.Context to the context.Context the collections API uses.
func ctxOf(ctx sdk.Context) context.Context { return ctx }
