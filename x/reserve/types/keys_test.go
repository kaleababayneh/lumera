package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pins the module identity + KVStore prefix bytes in
// x/reserve/types/keys.go. Previously unpinned.

func TestReserveModuleIdentity_StableStrings(t *testing.T) {
	assert.Equal(t, "reserve", ModuleName,
		"ModuleName rename is a chain-fork event")
	assert.Equal(t, "reserve", StoreKey)
	assert.Equal(t, "mem_reserve", MemStoreKey,
		"MemStoreKey is 'mem_' + ModuleName convention")
}

func TestReservePrefixBytes_ExactValues(t *testing.T) {
	cases := []struct {
		name  string
		slice []byte
		want  byte
	}{
		{"ParamsKeyPrefix", ParamsKeyPrefix, 0x01},
		// Commitment family starts at 0x10, leaving 0x02..0x0F for
		// future params-adjacent expansion.
		{"CommitmentKeyPrefix", CommitmentKeyPrefix, 0x10},
		{"CommitmentByPolicyKeyPrefix", CommitmentByPolicyKeyPrefix, 0x11},
		{"CommitmentSeqKeyPrefix", CommitmentSeqKeyPrefix, 0x12},
		{"CommitmentExpiryKeyPrefix", CommitmentExpiryKeyPrefix, 0x13},
		{"CommitmentByOwnerKeyPrefix", CommitmentByOwnerKeyPrefix, 0x14},
	}
	for _, c := range cases {
		require.Lenf(t, c.slice, 1, "%s must be 1 byte", c.name)
		assert.Equalf(t, c.want, c.slice[0],
			"%s = 0x%02x; want 0x%02x", c.name, c.slice[0], c.want)
	}
}

func TestReservePrefixBytes_Unique(t *testing.T) {
	prefixes := map[string][]byte{
		"ParamsKeyPrefix":             ParamsKeyPrefix,
		"CommitmentKeyPrefix":         CommitmentKeyPrefix,
		"CommitmentByPolicyKeyPrefix": CommitmentByPolicyKeyPrefix,
		"CommitmentSeqKeyPrefix":      CommitmentSeqKeyPrefix,
		"CommitmentExpiryKeyPrefix":   CommitmentExpiryKeyPrefix,
		"CommitmentByOwnerKeyPrefix":  CommitmentByOwnerKeyPrefix,
	}
	byByte := make(map[byte][]string, len(prefixes))
	for name, p := range prefixes {
		byByte[p[0]] = append(byByte[p[0]], name)
	}
	for b, names := range byByte {
		require.Lenf(t, names, 1, "prefix 0x%02x shared by %v", b, names)
	}
}
