package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pins the module identity + KVStore prefix bytes in
// x/priority/types/keys.go. Previously unpinned.

func TestPriorityModuleIdentity_StableStrings(t *testing.T) {
	assert.Equal(t, "priority", ModuleName,
		"ModuleName rename is a chain-fork event")
	assert.Equal(t, "priority", StoreKey)
	assert.Equal(t, "mem_priority", MemStoreKey,
		"MemStoreKey is 'mem_' + ModuleName convention")
}

func TestPriorityPrefixBytes_ExactValues(t *testing.T) {
	cases := []struct {
		name  string
		slice []byte
		want  byte
	}{
		{"ParamsKeyPrefix", ParamsKeyPrefix, 0x01},
		// AssignmentKeyPrefix uses 0x10 — an explicit skip from
		// 0x01 so the namespace leaves room for future params-
		// adjacent prefixes (0x02..0x0F) without needing to
		// renumber the assignment prefix. Pin the current value.
		{"AssignmentKeyPrefix", AssignmentKeyPrefix, 0x10},
	}
	for _, c := range cases {
		require.Lenf(t, c.slice, 1, "%s must be 1 byte", c.name)
		assert.Equalf(t, c.want, c.slice[0],
			"%s = 0x%02x; want 0x%02x", c.name, c.slice[0], c.want)
	}
}

func TestPriorityPrefixBytes_Unique(t *testing.T) {
	prefixes := map[string][]byte{
		"ParamsKeyPrefix":     ParamsKeyPrefix,
		"AssignmentKeyPrefix": AssignmentKeyPrefix,
	}
	byByte := make(map[byte][]string, len(prefixes))
	for name, p := range prefixes {
		byByte[p[0]] = append(byByte[p[0]], name)
	}
	for b, names := range byByte {
		require.Lenf(t, names, 1, "prefix 0x%02x shared by %v", b, names)
	}
}
