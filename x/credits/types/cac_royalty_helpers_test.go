package types

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This file closes DIRECT-test coverage for SIX CACRoyalty*
// helpers in proto_helpers.go (:290-331) that had ZERO direct
// tests prior:
//
//   CACRoyaltyRecord:
//     - TotalAmountCoins   (:291-293)
//     - OriginShareCoins   (:296-298)
//     - ServingShareCoins  (:301-303)
//     - TimestampTime      (:306-311)   [already tested via
//                                        CACRoyaltyStats sibling,
//                                        but this one is Record's]
//
//   CACRoyaltyStats:
//     - TotalRoyaltiesEarnedCoins (:316-318)
//     - TotalRoyaltiesPaidCoins   (:321-323)
//     - LastUpdatedTime           (:326-331)
//
// Scan-angle #5 (sibling-pattern pinning with shared semantic)
// applies heavily: all three Coin-list projections on
// CACRoyaltyRecord AND both on CACRoyaltyStats delegate to
// CoinsFromProto. A refactor adding defensive-copy OR per-
// helper error handling on ONE of them would diverge the
// family; pinned here so the parity is verified mechanically.
//
// Scan-angle #2 (race-safe missing-key/nil guards) applies to
// the TimestampTime and LastUpdatedTime helpers: nil-receiver
// AND nil-field both return time.Time{} without panicking. A
// refactor dropping either guard would panic on partially-
// populated records surfaced from on-chain state queries.
//
// Scan-angle #3 (hidden-secondary-return pinning) applies to
// CoinsFromProto's ZERO-COIN return shape: empty input returns
// a NON-NIL empty sdk.Coins slice (pins "nil vs empty" convention
// callers rely on for iteration).

// ---- CACRoyaltyRecord Coin projections ----

// TestCACRoyaltyRecord_TotalAmountCoins_Empty pins the empty-
// field path.
func TestCACRoyaltyRecord_TotalAmountCoins_Empty(t *testing.T) {
	t.Parallel()
	m := &CACRoyaltyRecord{}
	got := m.TotalAmountCoins()
	assert.Empty(t, got,
		"nil TotalAmount → empty sdk.Coins (via CoinsFromProto). "+
			"Pins the 'empty, not nil' convention so callers can "+
			"iterate without nil-check.")
}

// TestCACRoyaltyRecord_TotalAmountCoins_Populated pins the
// happy path.
func TestCACRoyaltyRecord_TotalAmountCoins_Populated(t *testing.T) {
	t.Parallel()
	m := &CACRoyaltyRecord{
		TotalAmount: []*basev1beta1.Coin{
			{Denom: "ulac", Amount: "100"},
			{Denom: "ulume", Amount: "200"},
		},
	}
	got := m.TotalAmountCoins()
	// sdk.Coins is sorted by denom — both elements present.
	require.Len(t, got, 2)
	// Verify both denoms present with correct amounts.
	ulac := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100), sdk.NewInt64Coin("ulume", 200))
	assert.True(t, got.Equal(ulac),
		"TotalAmount proto → equivalent sdk.Coins (both denoms)")
}

// TestCACRoyaltyRecord_OriginShareCoins pins the parallel
// helper for the OriginShare field.
func TestCACRoyaltyRecord_OriginShareCoins(t *testing.T) {
	t.Parallel()
	// Empty.
	empty := &CACRoyaltyRecord{}
	assert.Empty(t, empty.OriginShareCoins())

	// Populated.
	m := &CACRoyaltyRecord{
		OriginShare: []*basev1beta1.Coin{{Denom: "ulac", Amount: "50"}},
	}
	got := m.OriginShareCoins()
	require.Len(t, got, 1)
	assert.Equal(t, "ulac", got[0].Denom)
	assert.True(t, got[0].Amount.Equal(sdk.NewInt64Coin("ulac", 50).Amount))
}

// TestCACRoyaltyRecord_ServingShareCoins pins the third sibling.
func TestCACRoyaltyRecord_ServingShareCoins(t *testing.T) {
	t.Parallel()
	empty := &CACRoyaltyRecord{}
	assert.Empty(t, empty.ServingShareCoins())

	m := &CACRoyaltyRecord{
		ServingShare: []*basev1beta1.Coin{{Denom: "ulac", Amount: "50"}},
	}
	got := m.ServingShareCoins()
	require.Len(t, got, 1)
	assert.Equal(t, "ulac", got[0].Denom)
}

// TestCACRoyaltyRecord_ShareHelpersIndependent pins the scan-
// angle #5 FIELD-ISOLATION anchor. The three helpers
// (TotalAmount, OriginShare, ServingShare) MUST read from
// their OWN fields, not share or alias. A refactor that
// accidentally pointed two helpers to the same field would
// silently double-count royalties.
func TestCACRoyaltyRecord_ThreeShareHelpersReadDistinctFields(t *testing.T) {
	t.Parallel()
	m := &CACRoyaltyRecord{
		TotalAmount:  []*basev1beta1.Coin{{Denom: "ulac", Amount: "300"}},
		OriginShare:  []*basev1beta1.Coin{{Denom: "ulac", Amount: "200"}},
		ServingShare: []*basev1beta1.Coin{{Denom: "ulac", Amount: "100"}},
	}
	total := m.TotalAmountCoins()
	origin := m.OriginShareCoins()
	serving := m.ServingShareCoins()

	require.Len(t, total, 1)
	require.Len(t, origin, 1)
	require.Len(t, serving, 1)

	// All three return DIFFERENT amounts — pins that they read
	// DISTINCT fields.
	assert.Equal(t, "300", total[0].Amount.String(),
		"TotalAmount reads the TotalAmount field")
	assert.Equal(t, "200", origin[0].Amount.String(),
		"OriginShare reads the OriginShare field. A refactor that "+
			"aliased this helper to TotalAmount would silently "+
			"double-count origin royalties.")
	assert.Equal(t, "100", serving[0].Amount.String(),
		"ServingShare reads the ServingShare field. A refactor "+
			"that aliased to OriginShare would silently mirror the "+
			"origin amount on the serving side.")
}

// TestCACRoyaltyRecord_TimestampTime_Nil pins the nil-guard
// at :307-309.
func TestCACRoyaltyRecord_TimestampTime_Nil(t *testing.T) {
	t.Parallel()
	// Nil receiver.
	var m *CACRoyaltyRecord
	assert.True(t, m.TimestampTime().IsZero(),
		"nil receiver → zero time. Pins :307 nil-receiver guard: "+
			"a refactor dropping it would panic when called on a "+
			"zero-returned record from a missed store-read.")

	// Nil Timestamp field.
	m2 := &CACRoyaltyRecord{}
	assert.True(t, m2.TimestampTime().IsZero(),
		"nil Timestamp field → zero time")
}

// TestCACRoyaltyRecord_TimestampTime_Populated pins the happy
// path.
func TestCACRoyaltyRecord_TimestampTime_Populated(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)
	m := &CACRoyaltyRecord{
		Timestamp: timestamppb.New(now),
	}
	assert.Equal(t, now, m.TimestampTime())
}

// ---- CACRoyaltyStats Coin projections ----

// TestCACRoyaltyStats_TotalRoyaltiesEarnedCoins pins empty +
// populated paths.
func TestCACRoyaltyStats_TotalRoyaltiesEarnedCoins(t *testing.T) {
	t.Parallel()
	// Empty.
	empty := &CACRoyaltyStats{}
	assert.Empty(t, empty.TotalRoyaltiesEarnedCoins())

	// Populated.
	m := &CACRoyaltyStats{
		TotalRoyaltiesEarned: []*basev1beta1.Coin{
			{Denom: "ulac", Amount: "1000"},
		},
	}
	got := m.TotalRoyaltiesEarnedCoins()
	require.Len(t, got, 1)
	assert.Equal(t, "1000", got[0].Amount.String())
}

// TestCACRoyaltyStats_TotalRoyaltiesPaidCoins pins the
// sibling helper.
func TestCACRoyaltyStats_TotalRoyaltiesPaidCoins(t *testing.T) {
	t.Parallel()
	empty := &CACRoyaltyStats{}
	assert.Empty(t, empty.TotalRoyaltiesPaidCoins())

	m := &CACRoyaltyStats{
		TotalRoyaltiesPaid: []*basev1beta1.Coin{
			{Denom: "ulac", Amount: "800"},
		},
	}
	got := m.TotalRoyaltiesPaidCoins()
	require.Len(t, got, 1)
	assert.Equal(t, "800", got[0].Amount.String())
}

// TestCACRoyaltyStats_EarnedAndPaidIndependent pins that the
// two coin helpers read DIFFERENT fields. Critical because
// "earned - paid" is the royalty ledger semantic: a refactor
// aliasing the two would make earned always equal paid,
// silently zeroing net royalties for every publisher.
func TestCACRoyaltyStats_EarnedAndPaidReadDistinctFields(t *testing.T) {
	t.Parallel()
	m := &CACRoyaltyStats{
		TotalRoyaltiesEarned: []*basev1beta1.Coin{{Denom: "ulac", Amount: "500"}},
		TotalRoyaltiesPaid:   []*basev1beta1.Coin{{Denom: "ulac", Amount: "300"}},
	}
	earned := m.TotalRoyaltiesEarnedCoins()
	paid := m.TotalRoyaltiesPaidCoins()

	assert.Equal(t, "500", earned[0].Amount.String())
	assert.Equal(t, "300", paid[0].Amount.String(),
		"earned=500, paid=300 — distinct fields. A refactor that "+
			"accidentally made both helpers read the same field "+
			"would zero the earned-minus-paid net royalty ledger for "+
			"every publisher.")
}

// TestCACRoyaltyStats_LastUpdatedTime_Nil pins the nil guards
// at :326-328.
func TestCACRoyaltyStats_LastUpdatedTime_Nil(t *testing.T) {
	t.Parallel()
	var m *CACRoyaltyStats
	assert.True(t, m.LastUpdatedTime().IsZero(),
		"nil receiver → zero time")

	m2 := &CACRoyaltyStats{}
	assert.True(t, m2.LastUpdatedTime().IsZero(),
		"nil LastUpdated field → zero time")
}

// TestCACRoyaltyStats_LastUpdatedTime_Populated pins the
// happy path.
func TestCACRoyaltyStats_LastUpdatedTime_Populated(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)
	m := &CACRoyaltyStats{
		LastUpdated: timestamppb.New(now),
	}
	assert.Equal(t, now, m.LastUpdatedTime())
}

// ---- Cross-sibling parity ----

// TestCACRoyalty_TimestampNilGuardsConsistent pins the scan-
// angle #2 CROSS-TYPE parity anchor. Both timestamp getters
// (CACRoyaltyRecord.TimestampTime + CACRoyaltyStats.
// LastUpdatedTime) handle nil receivers AND nil timestamp
// fields the same way — a refactor that dropped one's guard
// would create inconsistent panic behavior between sibling
// types with otherwise-identical timestamp contract.
func TestCACRoyalty_TimestampNilGuardsAreConsistent(t *testing.T) {
	t.Parallel()
	// All four nil-ish paths return zero time without panicking.
	var nilRecord *CACRoyaltyRecord
	var nilStats *CACRoyaltyStats
	emptyRecord := &CACRoyaltyRecord{}
	emptyStats := &CACRoyaltyStats{}

	// None panic; all return zero time.
	assert.True(t, nilRecord.TimestampTime().IsZero())
	assert.True(t, nilStats.LastUpdatedTime().IsZero())
	assert.True(t, emptyRecord.TimestampTime().IsZero())
	assert.True(t, emptyStats.LastUpdatedTime().IsZero())
}

// TestCACRoyalty_RoundTripViaCoinsToProto pins the lossless
// round-trip for Coin projections: sdk.Coins → CoinsToProto
// → stored in struct → method reads back → equivalent
// sdk.Coins.
func TestCACRoyalty_CoinRoundTripPreservesValues(t *testing.T) {
	t.Parallel()
	orig := sdk.NewCoins(
		sdk.NewInt64Coin("ulac", 1000),
		sdk.NewInt64Coin("ulume", 500),
	)
	m := &CACRoyaltyStats{
		TotalRoyaltiesEarned: CoinsToProto(orig),
	}
	back := m.TotalRoyaltiesEarnedCoins()
	assert.True(t, orig.Equal(back),
		"Coin roundtrip through CACRoyaltyStats preserves value. "+
			"Pins the composition of CoinsToProto + "+
			"TotalRoyaltiesEarnedCoins — a refactor in either "+
			"direction would break the settle-and-reload cycle.")
}
