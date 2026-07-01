package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keys.go defines the module-identity strings and KVStore byte
// prefixes used by the passport keeper. Every byte here is
// consensus-critical — a silent drift would corrupt state
// migrations across every deployed node (reads by a new-version
// validator would return different bytes than writes from an
// old-version validator, producing consensus failures).
// Previously unpinned.

// TestModuleIdentity_StableStrings pins the module-identity
// strings at "passport". These are the routing keys the cosmos
// runtime uses to dispatch transactions and queries; a rename
// would reroute every existing passport tx to nowhere.
func TestModuleIdentity_StableStrings(t *testing.T) {
	assert.Equal(t, "passport", ModuleName,
		"ModuleName renames are a chain-fork event — any change must be deliberate")
	assert.Equal(t, "passport", StoreKey,
		"StoreKey must match ModuleName")
	assert.Equal(t, "passport", RouterKey,
		"RouterKey must match ModuleName")
	assert.Equal(t, "passport", QuerierRoute,
		"QuerierRoute must match ModuleName")
	assert.Equal(t, "passport", ModuleAccountName,
		"ModuleAccountName must match ModuleName")
}

// TestPrefixBytes_ExactValues pins the exact byte values of each
// KVStore prefix. These appear as the first byte of every state
// key; a silent byte flip (say, 0x02 → 0x12) would make a new
// binary read garbage for every pre-existing key, producing
// confusing "not found" errors across the entire passport
// keeper surface.
func TestPrefixBytes_ExactValues(t *testing.T) {
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"ParamsPrefixByte", ParamsPrefixByte, 0x01},
		{"PassportsPrefixByte", PassportsPrefixByte, 0x02},
		{"PassportSeqKeyPrefixByte", PassportSeqKeyPrefixByte, 0x03},
		{"AgentIndexPrefixByte", AgentIndexPrefixByte, 0x04},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.got,
			"%s = 0x%02x; want 0x%02x — any change requires a state migration",
			c.name, c.got, c.want)
	}
}

// TestPrefixBytes_Unique pins the no-collision invariant: no two
// prefix constants share a byte value. A collision would make
// one prefix's keys overlap another's — catastrophic silent
// state corruption. Using a map guarantees uniqueness across
// the full set; adding a new prefix constant requires adding
// it to this list AND confirming it doesn't collide.
func TestPrefixBytes_Unique(t *testing.T) {
	prefixes := map[string]uint8{
		"ParamsPrefixByte":         ParamsPrefixByte,
		"PassportsPrefixByte":      PassportsPrefixByte,
		"PassportSeqKeyPrefixByte": PassportSeqKeyPrefixByte,
		"AgentIndexPrefixByte":     AgentIndexPrefixByte,
	}

	// Reverse-map: byte → list of names using it.
	byByte := make(map[uint8][]string, len(prefixes))
	for name, b := range prefixes {
		byByte[b] = append(byByte[b], name)
	}

	for b, names := range byByte {
		require.Lenf(t, names, 1,
			"prefix byte 0x%02x is shared by %v — collisions cause state corruption",
			b, names)
	}
}

// TestPrefixSlices_MatchBytes pins that each []byte prefix is
// a 1-element slice containing exactly the corresponding
// *PrefixByte constant. The keeper uses the []byte slices for
// collections.NewPrefix; a mismatch between the byte constant
// and the slice would let two ways of referring to the same
// prefix drift apart silently.
func TestPrefixSlices_MatchBytes(t *testing.T) {
	cases := []struct {
		name  string
		slice []byte
		byt   uint8
	}{
		{"ParamsPrefix", ParamsPrefix, ParamsPrefixByte},
		{"PassportsPrefix", PassportsPrefix, PassportsPrefixByte},
		{"PassportSeqKeyPrefix", PassportSeqKeyPrefix, PassportSeqKeyPrefixByte},
		{"AgentIndexPrefix", AgentIndexPrefix, AgentIndexPrefixByte},
	}
	for _, c := range cases {
		require.Lenf(t, c.slice, 1,
			"%s must be exactly 1 byte long", c.name)
		assert.Equalf(t, c.byt, c.slice[0],
			"%s[0] = 0x%02x; want 0x%02x (matches %sByte)",
			c.name, c.slice[0], c.byt, c.name)
	}
}
