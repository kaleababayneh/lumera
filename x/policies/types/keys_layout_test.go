//go:build cosmos

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKeyPrefixLayout_Stable pins the prefix-byte layout for every
// collection in the policies module. Commit 0018566a9 introduced
// BudgetUsagePrefixByte = 0x0A and claimed "prefix-byte layout
// assertions" in its test coverage — but the keeper tests only
// exercised end-to-end budget persistence behaviour, never the
// raw byte value. This test pins the exact byte value for every
// prefix so that a future reordering that collides BudgetUsage
// with ParamsPrefixByte (or any sibling) is caught at test time
// BEFORE the collision corrupts on-chain state on the next upgrade.
//
// Rule: once a prefix byte ships into state, its value is a
// permanent part of the storage contract. Any re-use (including
// "unused" re-assignment) is a consensus break. Pin here.
func TestKeyPrefixLayout_Stable(t *testing.T) {
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"ParamsPrefixByte", ParamsPrefixByte, 0x01},
		{"PolicyPrefixByte", PolicyPrefixByte, 0x02},
		{"PolicyVersionPrefixByte", PolicyVersionPrefixByte, 0x03},
		{"PolicyUpdatePrefixByte", PolicyUpdatePrefixByte, 0x04},
		{"PolicyUpdateCounterPrefixByte", PolicyUpdateCounterPrefixByte, 0x05},
		{"PolicyAuditPrefixByte", PolicyAuditPrefixByte, 0x06},
		{"PolicyByOwnerPrefixByte", PolicyByOwnerPrefixByte, 0x07},
		{"PolicyByStatePrefixByte", PolicyByStatePrefixByte, 0x08},
		{"PolicyAuditCounterPrefixByte", PolicyAuditCounterPrefixByte, 0x09},
		{"BudgetUsagePrefixByte", BudgetUsagePrefixByte, 0x0A},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.got,
				"prefix-byte layout is a durable storage contract; "+
					"changing %s from 0x%02X to 0x%02X would corrupt "+
					"every chain state iterating over this prefix on upgrade",
				tc.name, tc.want, tc.got)
		})
	}
}

// TestKeyPrefixLayout_Unique pins that every prefix byte is distinct.
// A future addition that accidentally reuses an already-claimed byte
// would silently merge two independent collection iterators under a
// shared prefix — a consensus-affecting corruption. This test exists
// to catch that at type-check time rather than in production.
func TestKeyPrefixLayout_Unique(t *testing.T) {
	all := []uint8{
		ParamsPrefixByte,
		PolicyPrefixByte,
		PolicyVersionPrefixByte,
		PolicyUpdatePrefixByte,
		PolicyUpdateCounterPrefixByte,
		PolicyAuditPrefixByte,
		PolicyByOwnerPrefixByte,
		PolicyByStatePrefixByte,
		PolicyAuditCounterPrefixByte,
		BudgetUsagePrefixByte,
	}
	seen := make(map[uint8]int, len(all))
	for i, b := range all {
		if first, exists := seen[b]; exists {
			t.Fatalf("prefix-byte collision: index %d and %d share value 0x%02X — "+
				"one of the two collections is shadowed", first, i, b)
		}
		seen[b] = i
	}
}
