
package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// This file closes DIRECT-test coverage for the module-identity
// strings and store-layout prefix bytes defined in keys.go.
// Had ZERO direct tests prior despite being the canonical
// specification of the credits module's on-chain state layout
// — any silent change to a prefix byte invalidates every
// stored record of that type at upgrade time.
//
// Scan-angle #6 (security-critical invariants tested only at
// happy path) applies: these bytes are THE state layout. A
// refactor that accidentally renumbered any prefix would
// produce a chain-fork event on upgrade — pre-existing state
// written under the old byte would be invisible (or
// misinterpreted as a different record type) under the new
// layout.
//
// Scan-angle #5 (sibling-pattern pinning with structural
// invariants) applies:
//   - Each numeric prefix constant has a matching []byte prefix
//     var. A refactor changing ONE without the OTHER would
//     produce internal inconsistency.
//   - All 15 prefix bytes must be PAIRWISE DISTINCT — a
//     duplicate byte would collide two record families in
//     store iteration.

// TestCreditsModuleIdentity_StableStrings pins the four string
// constants that feed routing/storage tables. A rename is a
// chain-fork event.
func TestCreditsModuleIdentity_StableStrings(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "credits", ModuleName,
		"ModuleName rename → chain fork: every existing module "+
			"account, every store reference, every routing entry "+
			"keys off this string.")
	assert.Equal(t, "credits", StoreKey,
		"StoreKey = ModuleName (pinned identity)")
	assert.Equal(t, "credits", RouterKey,
		"RouterKey = ModuleName (pinned identity)")
	assert.Equal(t, "credits", QuerierRoute,
		"QuerierRoute = ModuleName (pinned identity)")
	assert.Equal(t, "credits", ModuleAccountName,
		"ModuleAccountName = ModuleName (pins the module-account "+
			"bech32 derivation — changing this migrates all "+
			"module-held coins to a NEW account.)")
}

// TestCreditsPrefixBytes_PinnedValues pins every byte-valued
// store prefix. Each prefix is THE first byte of every key for
// that record family; a silent change corrupts every pre-
// existing record at upgrade.
func TestCreditsPrefixBytes_PinnedValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"ParamsPrefixByte", ParamsPrefixByte, 0x01},
		{"LocksPrefixByte", LocksPrefixByte, 0x02},
		{"LockSeqKeyPrefixByte", LockSeqKeyPrefixByte, 0x03},
		{"SettlementsPrefixByte", SettlementsPrefixByte, 0x04},
		{"DisputesPrefixByte", DisputesPrefixByte, 0x05},
		{"MetricsPrefixByte", MetricsPrefixByte, 0x06},
		{"CACRoyaltyPrefixByte", CACRoyaltyPrefixByte, 0x07},
		{"CACStatsPrefixByte", CACStatsPrefixByte, 0x08},
		{"LockExpiryPrefixByte", LockExpiryPrefixByte, 0x09},
		{"PendingSettlementsPrefixByte", PendingSettlementsPrefixByte, 0x0A},
		{"SettlementsByTimePrefixByte", SettlementsByTimePrefixByte, 0x0B},
		{"FinalizedLocksPrefixByte", FinalizedLocksPrefixByte, 0x0C},
		{"LockReceiptsPrefixByte", LockReceiptsPrefixByte, 0x0D},
		{"LocksByQuotePrefixByte", LocksByQuotePrefixByte, 0x0E},
		{"CACSeqPrefixByte", CACSeqPrefixByte, 0x0F},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, c.got,
				"%s = %#x is the chain-fork-event-defining byte for "+
					"its record family. A silent change invalidates "+
					"every pre-existing record at upgrade.", c.name, c.want)
		})
	}
}

// TestCreditsPrefixBytes_PairwiseDistinct is the CRITICAL scan-
// angle #5 structural invariant: no two record families share a
// prefix byte. A duplicate would collide in store iteration and
// cause one record type to masquerade as another.
func TestCreditsPrefixBytes_PairwiseDistinct(t *testing.T) {
	t.Parallel()
	allPrefixes := []struct {
		name string
		b    uint8
	}{
		{"ParamsPrefixByte", ParamsPrefixByte},
		{"LocksPrefixByte", LocksPrefixByte},
		{"LockSeqKeyPrefixByte", LockSeqKeyPrefixByte},
		{"SettlementsPrefixByte", SettlementsPrefixByte},
		{"DisputesPrefixByte", DisputesPrefixByte},
		{"MetricsPrefixByte", MetricsPrefixByte},
		{"CACRoyaltyPrefixByte", CACRoyaltyPrefixByte},
		{"CACStatsPrefixByte", CACStatsPrefixByte},
		{"LockExpiryPrefixByte", LockExpiryPrefixByte},
		{"PendingSettlementsPrefixByte", PendingSettlementsPrefixByte},
		{"SettlementsByTimePrefixByte", SettlementsByTimePrefixByte},
		{"FinalizedLocksPrefixByte", FinalizedLocksPrefixByte},
		{"LockReceiptsPrefixByte", LockReceiptsPrefixByte},
		{"LocksByQuotePrefixByte", LocksByQuotePrefixByte},
		{"CACSeqPrefixByte", CACSeqPrefixByte},
	}
	seen := make(map[uint8]string, len(allPrefixes))
	for _, p := range allPrefixes {
		if prev, exists := seen[p.b]; exists {
			t.Errorf("prefix byte %#x collision: %s AND %s share the "+
				"same byte. Pins the pairwise-distinctness invariant — "+
				"a refactor that accidentally duplicated a byte would "+
				"cause store-iteration for one family to return "+
				"records from the other.", p.b, prev, p.name)
		}
		seen[p.b] = p.name
	}
	assert.Equal(t, 15, len(seen),
		"expected 15 distinct prefix bytes")
}

// TestCreditsPrefixSlices_MatchByteConstants pins that each
// []byte prefix slice contains EXACTLY the single byte from
// its numeric constant. A refactor that used a different
// representation (e.g., multi-byte prefix) would change
// collections.Map iteration scope.
func TestCreditsPrefixSlices_MatchByteConstants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		slice []byte
		b     uint8
	}{
		{"ParamsPrefix", ParamsPrefix, ParamsPrefixByte},
		{"LocksPrefix", LocksPrefix, LocksPrefixByte},
		{"LockExpiryPrefix", LockExpiryPrefix, LockExpiryPrefixByte},
		{"LockSeqKeyPrefix", LockSeqKeyPrefix, LockSeqKeyPrefixByte},
		{"SettlementPrefix", SettlementPrefix, SettlementsPrefixByte},
		{"PendingSettlementsPrefix", PendingSettlementsPrefix, PendingSettlementsPrefixByte},
		{"SettlementsByTimePrefix", SettlementsByTimePrefix, SettlementsByTimePrefixByte},
		{"FinalizedLocksPrefix", FinalizedLocksPrefix, FinalizedLocksPrefixByte},
		{"LockReceiptsPrefix", LockReceiptsPrefix, LockReceiptsPrefixByte},
		{"LocksByQuotePrefix", LocksByQuotePrefix, LocksByQuotePrefixByte},
		{"DisputePrefix", DisputePrefix, DisputesPrefixByte},
		{"MetricsPrefix", MetricsPrefix, MetricsPrefixByte},
		{"CACRoyaltyPrefix", CACRoyaltyPrefix, CACRoyaltyPrefixByte},
		{"CACStatsPrefix", CACStatsPrefix, CACStatsPrefixByte},
		{"CACSeqPrefix", CACSeqPrefix, CACSeqPrefixByte},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			require := assert.New(t)
			require.Len(c.slice, 1,
				"%s must be a single-byte slice. A refactor to a "+
					"multi-byte prefix would widen the iteration scope "+
					"and overlap with other record families.", c.name)
			require.Equal(c.b, c.slice[0],
				"%s[0] = %#x must equal %s constant",
				c.name, c.b, c.name+"Byte")
		})
	}
}

// TestCreditsPrefixBytes_PreservedBelow0x10 is a scan-angle #5
// FOR-FUTURE-GROWTH pin. All 15 current prefixes fit in the
// low nibble (0x01..0x0F). Future additions SHOULD start at
// 0x10 to preserve the clustering. Pinned as a structural
// observation — a refactor that renumbered inside the low
// nibble MUST explicitly update this test, but a genuine
// EXPANSION (e.g., 0x10) would land fine.
func TestCreditsPrefixBytes_LowNibbleClustering(t *testing.T) {
	t.Parallel()
	allBytes := []uint8{
		ParamsPrefixByte, LocksPrefixByte, LockSeqKeyPrefixByte,
		SettlementsPrefixByte, DisputesPrefixByte, MetricsPrefixByte,
		CACRoyaltyPrefixByte, CACStatsPrefixByte, LockExpiryPrefixByte,
		PendingSettlementsPrefixByte, SettlementsByTimePrefixByte,
		FinalizedLocksPrefixByte, LockReceiptsPrefixByte,
		LocksByQuotePrefixByte, CACSeqPrefixByte,
	}
	for _, b := range allBytes {
		assert.True(t, b >= 0x01 && b <= 0x0F,
			"prefix byte %#x currently fits in the low nibble. A "+
				"refactor adding NEW prefixes may legitimately use "+
				"0x10+; pinned to surface any renumbering inside "+
				"the already-used range (which would be a chain-"+
				"fork event).", b)
	}
}

// TestCreditsPrefixBytes_ZeroByteReserved pins that NO prefix
// uses 0x00. Cosmos SDK convention reserves 0x00 for internal
// framework keys, and a refactor accidentally using 0x00
// would collide with SDK-internal store entries.
func TestCreditsPrefixBytes_ZeroByteReserved(t *testing.T) {
	t.Parallel()
	allBytes := []uint8{
		ParamsPrefixByte, LocksPrefixByte, LockSeqKeyPrefixByte,
		SettlementsPrefixByte, DisputesPrefixByte, MetricsPrefixByte,
		CACRoyaltyPrefixByte, CACStatsPrefixByte, LockExpiryPrefixByte,
		PendingSettlementsPrefixByte, SettlementsByTimePrefixByte,
		FinalizedLocksPrefixByte, LockReceiptsPrefixByte,
		LocksByQuotePrefixByte, CACSeqPrefixByte,
	}
	for _, b := range allBytes {
		assert.NotEqual(t, uint8(0x00), b,
			"prefix byte 0x00 reserved for cosmos SDK framework use. "+
				"Pins the 0x01+ convention: a refactor using 0x00 "+
				"would collide with SDK-internal store entries.")
	}
}
