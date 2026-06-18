//go:build cosmos

package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes coverage for txPrioritizerOptionalTimeUTC at
// tx_prioritizer.go:134-140 — a type-OrNil-family member with
// the NAMING VARIANT `OptionalTimeUTC` that was MISSED by the
// prior family-completion sweep (ticks 138-139 counted only
// the `*TimeOrNil` suffix, reaching 17/17). Case-sensitive grep
// `grep -rn "func.*TimeOrNil(ts time.Time) \*time.Time"` missed
// this entire naming-variant class.
//
// txPrioritizerOptionalTimeUTC serves TWO MarshalJSON consumer
// paths at :167 (TxPrioritizerReputationSnapshotV1.SourceTime)
// and :194 (TxPrioritizerStakeSnapshotV1.SourceTime). These are
// CONSENSUS-ADJACENT: reputation and stake snapshots feed tx
// priority calculation, and their persisted JSON shape must be
// deterministic. A zero timestamp MUST be omitted from the wire
// (not serialized as "0001-01-01T00:00:00Z") to keep consensus
// hashes stable across node implementations.
//
// Standard type-OrNil invariant matrix pinned:
//   - zero → nil
//   - non-zero → UTC-normalized fresh pointer
//   - fresh-per-call (no aliasing)
//   - near-epoch (Go-zero vs Unix-epoch boundary)
//   - in-situ MarshalJSON integration for BOTH consumer paths
//   - cross-package consistency with the 17 TimeOrNil siblings

// TestTxPrioritizerOptionalTimeUTC_ZeroReturnsNil pins the
// zero-branch. The zero Go timestamp must produce nil so the
// MarshalJSON consumers elide source_time from the wire.
func TestTxPrioritizerOptionalTimeUTC_ZeroReturnsNil(t *testing.T) {
	t.Parallel()
	got := txPrioritizerOptionalTimeUTC(time.Time{})
	assert.Nil(t, got,
		"zero time must return nil so omitempty elides source_time "+
			"in both TxPrioritizerReputationSnapshotV1 and "+
			"TxPrioritizerStakeSnapshotV1 JSON. Consensus stability "+
			"depends on deterministic omission of unset timestamps.")
}

// TestTxPrioritizerOptionalTimeUTC_NonZeroReturnsUTCPointer pins
// the non-zero branch: UTC-normalized fresh pointer.
func TestTxPrioritizerOptionalTimeUTC_NonZeroReturnsUTCPointer(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got := txPrioritizerOptionalTimeUTC(ts)
	require.NotNil(t, got)
	assert.True(t, got.Equal(ts))
	assert.Equal(t, time.UTC, got.Location(),
		"returned location must be UTC")
}

// TestTxPrioritizerOptionalTimeUTC_NormalizesNonUTCtoUTC pins
// the `.UTC()` call at :138. A non-UTC input produces a UTC-
// normalized output — consensus hashes depend on UTC-only
// serialization across all node operators regardless of their
// local timezone.
func TestTxPrioritizerOptionalTimeUTC_NormalizesNonUTCtoUTC(t *testing.T) {
	t.Parallel()
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	instant := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	tokyoTime := instant.In(tokyo)
	require.NotEqual(t, time.UTC, tokyoTime.Location(),
		"precondition: Tokyo is non-UTC")

	got := txPrioritizerOptionalTimeUTC(tokyoTime)
	require.NotNil(t, got)
	assert.Equal(t, time.UTC, got.Location(),
		"non-UTC input must be UTC-normalized. A regression dropping "+
			".UTC() would allow node operators in different TZs to "+
			"produce different snapshot JSON bytes for the same "+
			"instant — consensus hash divergence.")
	assert.True(t, got.Equal(instant))

	// Fixed-offset zone variant.
	bst := time.FixedZone("BST", 3600)
	bstTime := instant.In(bst)
	got2 := txPrioritizerOptionalTimeUTC(bstTime)
	require.NotNil(t, got2)
	assert.Equal(t, time.UTC, got2.Location())
	assert.True(t, got2.Equal(instant))
}

// TestTxPrioritizerOptionalTimeUTC_FreshPointerPerCall pins
// fresh-pointer semantics. Callers must be able to mutate the
// returned pointer without affecting subsequent calls.
func TestTxPrioritizerOptionalTimeUTC_FreshPointerPerCall(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got1 := txPrioritizerOptionalTimeUTC(ts)
	got2 := txPrioritizerOptionalTimeUTC(ts)
	require.NotNil(t, got1)
	require.NotNil(t, got2)
	assert.NotSame(t, got1, got2,
		"fresh pointer per call — independent allocations")
	assert.True(t, got1.Equal(*got2))
}

// TestTxPrioritizerOptionalTimeUTC_NearEpochNotTreatedAsZero
// pins the Go-zero-vs-Unix-epoch boundary. Unix epoch
// (time.Unix(0,0)) is NOT Go zero — only time.Time{} triggers
// the nil return. A refactor using `ts.Unix() == 0` would
// wrongly elide legitimate epoch timestamps.
func TestTxPrioritizerOptionalTimeUTC_NearEpochNotTreatedAsZero(t *testing.T) {
	t.Parallel()
	epoch := time.Unix(0, 0).UTC()
	require.False(t, epoch.IsZero(),
		"precondition: Unix epoch is NOT Go zero")
	require.NotNil(t, txPrioritizerOptionalTimeUTC(epoch),
		"Unix epoch passes through — only Go zero elides")

	justPast := time.Time{}.Add(time.Nanosecond)
	require.False(t, justPast.IsZero())
	require.NotNil(t, txPrioritizerOptionalTimeUTC(justPast))
}

// TestTxPrioritizerOptionalTimeUTC_ReputationSnapshotMarshalJSON
// pins the in-situ effect for the reputation snapshot consumer
// path at :167. Zero → omitted; non-zero non-UTC → UTC on wire.
func TestTxPrioritizerOptionalTimeUTC_ReputationSnapshotMarshalJSON(t *testing.T) {
	t.Parallel()

	// Zero source_time → omitted from wire.
	zeroSnap := TxPrioritizerReputationSnapshotV1{
		Score:        "0.85",
		ScoreVersion: "1",
		SampleSize:   100,
	}
	raw, err := json.Marshal(zeroSnap)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "source_time",
		"zero SourceTime elided from reputation snapshot JSON — "+
			"prevents phantom year-1 timestamp on wire")
	assert.NotContains(t, string(raw), "0001-01-01",
		"no year-1 phantom may escape")

	// Non-zero non-UTC source_time → normalized to UTC on wire.
	brt := time.FixedZone("BRT", -3*3600)
	populated := TxPrioritizerReputationSnapshotV1{
		Score:      "0.85",
		SourceTime: time.Date(2026, 5, 1, 9, 0, 0, 0, brt),
	}
	raw2, err := json.Marshal(populated)
	require.NoError(t, err)
	// 09:00 BRT (UTC-3) → 12:00 UTC.
	assert.Contains(t, string(raw2), "2026-05-01T12:00:00Z",
		"SourceTime UTC-normalized on wire (09:00 BRT → 12:00 UTC). "+
			"This is the consensus-stability guarantee: two validators "+
			"in different timezones produce byte-identical JSON.")
}

// TestTxPrioritizerOptionalTimeUTC_StakeSnapshotMarshalJSON pins
// the PARALLEL consumer path at :194 for
// TxPrioritizerStakeSnapshotV1. A regression treating the two
// consumer structs differently would surface here.
func TestTxPrioritizerOptionalTimeUTC_StakeSnapshotMarshalJSON(t *testing.T) {
	t.Parallel()

	zeroSnap := TxPrioritizerStakeSnapshotV1{
		BondDenom:    "ulumera",
		BondedAmount: "1000000",
	}
	raw, err := json.Marshal(zeroSnap)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "source_time",
		"zero SourceTime elided from stake snapshot JSON")

	brt := time.FixedZone("BRT", -3*3600)
	populated := TxPrioritizerStakeSnapshotV1{
		BondDenom:  "ulumera",
		SourceTime: time.Date(2026, 5, 1, 9, 0, 0, 0, brt),
	}
	raw2, err := json.Marshal(populated)
	require.NoError(t, err)
	assert.Contains(t, string(raw2), "2026-05-01T12:00:00Z",
		"stake snapshot SourceTime UTC-normalized — consensus "+
			"stability guarantee parallel to reputation snapshot")
}

// TestTxPrioritizerOptionalTimeUTC_SiblingPatternConsistency
// pins cross-package parity with the 17 TimeOrNil siblings + 4
// other optional* siblings (optionalReceiptTime,
// optionalFederatedTime, optionalAccountLinkTime, optionalTime).
// The contract: zero→nil, non-zero→fresh UTC-normalized pointer,
// Equal-to-input.
func TestTxPrioritizerOptionalTimeUTC_SiblingPatternConsistency(t *testing.T) {
	t.Parallel()
	cases := []time.Time{
		time.Time{},                         // zero → nil
		time.Unix(0, 0).UTC(),               // epoch → pointer
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2099, 12, 31, 23, 59, 59, 999_999_999, time.UTC),
	}
	for _, in := range cases {
		got := txPrioritizerOptionalTimeUTC(in)
		if in.IsZero() {
			assert.Nil(t, got,
				"zero input (%v) → nil — same contract as 17 "+
					"TimeOrNil siblings AND the 4 other optional* "+
					"siblings in internal/{receipts,storefront,auth}. "+
					"The naming variant OptionalTimeUTC was missed by "+
					"the prior family sweep (case-sensitive grep on "+
					"TimeOrNil suffix). Family is actually 22 "+
					"instances, not 17.", in)
			continue
		}
		require.NotNil(t, got)
		assert.Equal(t, time.UTC, got.Location(),
			"non-zero result UTC-normalized")
		assert.True(t, got.Equal(in))
	}
}
