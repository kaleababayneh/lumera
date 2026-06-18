//go:build cosmos

package types

const (
	// ModuleName defines the NFT module name used throughout the application.
	ModuleName = "nft"
	// StoreKey identifies the primary KV store for the module.
	StoreKey = ModuleName
	// RouterKey routes messages to the module's handlers.
	RouterKey = ModuleName

	// MaxRoyaltyBPS caps curator royalties at 10% of net settlement (1000 basis points).
	MaxRoyaltyBPS = 1000
)

var (
	// ToolpackKeyPrefix stores active toolpack metadata.
	ToolpackKeyPrefix = []byte{0x01}
	// ToolpackHistoryPrefix stores immutable toolpack history entries.
	ToolpackHistoryPrefix = []byte{0x02}
	// ToolpackCuratorIndex tracks toolpacks by curator address.
	ToolpackCuratorIndex = []byte{0x11}
	// ToolpackHistoryIndex indexes history records by curator.
	ToolpackHistoryIndex = []byte{0x21}
	// RoyaltyAccumulatorPrefix stores curator royalty totals per denomination.
	RoyaltyAccumulatorPrefix = []byte{0x31}
)
