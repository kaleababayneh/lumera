
package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pins module identity + KVStore prefix bytes + MaxRoyaltyBPS
// in x/nft/types/keys.go. Previously unpinned.

func TestNFTModuleIdentity_StableStrings(t *testing.T) {
	assert.Equal(t, "nft", ModuleName,
		"ModuleName rename is a chain-fork event")
	assert.Equal(t, "nft", StoreKey)
	assert.Equal(t, "nft", RouterKey)
}

// TestMaxRoyaltyBPS_PinnedValue pins the 10% royalty cap
// (1000 bps). The comment at keys.go:13 cites "10% of net
// settlement"; a silent bump (to 2000 bps = 20%) would let
// curators take a bigger cut than the documented ceiling,
// breaking economic assumptions in downstream contracts.
func TestMaxRoyaltyBPS_PinnedValue(t *testing.T) {
	assert.Equal(t, 1000, MaxRoyaltyBPS,
		"MaxRoyaltyBPS = 1000 (10%%) — any change alters the published royalty ceiling")
}

func TestNFTPrefixBytes_ExactValues(t *testing.T) {
	cases := []struct {
		name  string
		slice []byte
		want  byte
	}{
		// Primary toolpack storage at 0x01/0x02, indexes at
		// 0x11/0x21, accumulator at 0x31 — deliberate 0x10
		// spacing between families for future expansion.
		{"ToolpackKeyPrefix", ToolpackKeyPrefix, 0x01},
		{"ToolpackHistoryPrefix", ToolpackHistoryPrefix, 0x02},
		{"ToolpackCuratorIndex", ToolpackCuratorIndex, 0x11},
		{"ToolpackHistoryIndex", ToolpackHistoryIndex, 0x21},
		{"RoyaltyAccumulatorPrefix", RoyaltyAccumulatorPrefix, 0x31},
	}
	for _, c := range cases {
		require.Lenf(t, c.slice, 1, "%s must be 1 byte", c.name)
		assert.Equalf(t, c.want, c.slice[0],
			"%s = 0x%02x; want 0x%02x", c.name, c.slice[0], c.want)
	}
}

func TestNFTPrefixBytes_Unique(t *testing.T) {
	prefixes := map[string][]byte{
		"ToolpackKeyPrefix":        ToolpackKeyPrefix,
		"ToolpackHistoryPrefix":    ToolpackHistoryPrefix,
		"ToolpackCuratorIndex":     ToolpackCuratorIndex,
		"ToolpackHistoryIndex":     ToolpackHistoryIndex,
		"RoyaltyAccumulatorPrefix": RoyaltyAccumulatorPrefix,
	}
	byByte := make(map[byte][]string, len(prefixes))
	for name, p := range prefixes {
		byByte[p[0]] = append(byByte[p[0]], name)
	}
	for b, names := range byByte {
		require.Lenf(t, names, 1, "prefix 0x%02x shared by %v", b, names)
	}
}
