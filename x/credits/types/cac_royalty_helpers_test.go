package types

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PORTED NOTE (gogoproto migration):
// In lumera_ai the CACRoyalty* messages carried proto Coin lists plus
// hand-written helper methods (TotalAmountCoins/OriginShareCoins/
// ServingShareCoins/TotalRoyaltiesEarnedCoins/TotalRoyaltiesPaidCoins/
// TimestampTime/LastUpdatedTime) that projected the wire types into
// sdk.Coins / time.Time. After the gogoproto migration the fields ARE
// native sdk.Coins and time.Time (see credits.pb.go:1136 & 1253), so the
// projection helpers were intentionally NOT ported — the field IS the
// projected value. These tests are rewritten to pin the same semantics
// directly on the native fields, preserving the original intent
// (field-isolation, empty-vs-populated, zero-time handling).

// ---- CACRoyaltyRecord Coin projections ----

func TestCACRoyaltyRecord_TotalAmountCoins_Empty(t *testing.T) {
	t.Parallel()
	m := &CACRoyaltyRecord{}
	assert.Empty(t, m.TotalAmount,
		"nil TotalAmount → empty sdk.Coins")
}

func TestCACRoyaltyRecord_TotalAmountCoins_Populated(t *testing.T) {
	t.Parallel()
	m := &CACRoyaltyRecord{
		TotalAmount: sdk.NewCoins(
			sdk.NewInt64Coin("ulac", 100),
			sdk.NewInt64Coin("ulume", 200),
		),
	}
	got := m.GetTotalAmount()
	require.Len(t, got, 2)
	ulac := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100), sdk.NewInt64Coin("ulume", 200))
	assert.True(t, got.Equal(ulac),
		"TotalAmount → equivalent sdk.Coins (both denoms)")
}

func TestCACRoyaltyRecord_OriginShareCoins(t *testing.T) {
	t.Parallel()
	empty := &CACRoyaltyRecord{}
	assert.Empty(t, empty.OriginShare)

	m := &CACRoyaltyRecord{
		OriginShare: sdk.NewCoins(sdk.NewInt64Coin("ulac", 50)),
	}
	got := m.GetOriginShare()
	require.Len(t, got, 1)
	assert.Equal(t, "ulac", got[0].Denom)
	assert.True(t, got[0].Amount.Equal(sdk.NewInt64Coin("ulac", 50).Amount))
}

func TestCACRoyaltyRecord_ServingShareCoins(t *testing.T) {
	t.Parallel()
	empty := &CACRoyaltyRecord{}
	assert.Empty(t, empty.ServingShare)

	m := &CACRoyaltyRecord{
		ServingShare: sdk.NewCoins(sdk.NewInt64Coin("ulac", 50)),
	}
	got := m.GetServingShare()
	require.Len(t, got, 1)
	assert.Equal(t, "ulac", got[0].Denom)
}

// TestCACRoyaltyRecord_ThreeShareHelpersReadDistinctFields pins field
// isolation: the three Coin fields read distinct storage, so a refactor
// aliasing two would silently double-count royalties.
func TestCACRoyaltyRecord_ThreeShareHelpersReadDistinctFields(t *testing.T) {
	t.Parallel()
	m := &CACRoyaltyRecord{
		TotalAmount:  sdk.NewCoins(sdk.NewInt64Coin("ulac", 300)),
		OriginShare:  sdk.NewCoins(sdk.NewInt64Coin("ulac", 200)),
		ServingShare: sdk.NewCoins(sdk.NewInt64Coin("ulac", 100)),
	}
	total := m.GetTotalAmount()
	origin := m.GetOriginShare()
	serving := m.GetServingShare()

	require.Len(t, total, 1)
	require.Len(t, origin, 1)
	require.Len(t, serving, 1)

	assert.Equal(t, "300", total[0].Amount.String(),
		"TotalAmount reads the TotalAmount field")
	assert.Equal(t, "200", origin[0].Amount.String(),
		"OriginShare reads the OriginShare field")
	assert.Equal(t, "100", serving[0].Amount.String(),
		"ServingShare reads the ServingShare field")
}

// TestCACRoyaltyRecord_TimestampTime_Nil pins the zero-time getter guard.
func TestCACRoyaltyRecord_TimestampTime_Nil(t *testing.T) {
	t.Parallel()
	// Nil receiver: GetTimestamp returns zero time without panicking.
	var m *CACRoyaltyRecord
	assert.True(t, m.GetTimestamp().IsZero(),
		"nil receiver → zero time")

	// Zero Timestamp field.
	m2 := &CACRoyaltyRecord{}
	assert.True(t, m2.GetTimestamp().IsZero(),
		"zero Timestamp field → zero time")
}

func TestCACRoyaltyRecord_TimestampTime_Populated(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)
	m := &CACRoyaltyRecord{
		Timestamp: now,
	}
	assert.Equal(t, now, m.GetTimestamp())
}

// ---- CACRoyaltyStats Coin projections ----

func TestCACRoyaltyStats_TotalRoyaltiesEarnedCoins(t *testing.T) {
	t.Parallel()
	empty := &CACRoyaltyStats{}
	assert.Empty(t, empty.TotalRoyaltiesEarned)

	m := &CACRoyaltyStats{
		TotalRoyaltiesEarned: sdk.NewCoins(sdk.NewInt64Coin("ulac", 1000)),
	}
	got := m.GetTotalRoyaltiesEarned()
	require.Len(t, got, 1)
	assert.Equal(t, "1000", got[0].Amount.String())
}

func TestCACRoyaltyStats_TotalRoyaltiesPaidCoins(t *testing.T) {
	t.Parallel()
	empty := &CACRoyaltyStats{}
	assert.Empty(t, empty.TotalRoyaltiesPaid)

	m := &CACRoyaltyStats{
		TotalRoyaltiesPaid: sdk.NewCoins(sdk.NewInt64Coin("ulac", 800)),
	}
	got := m.GetTotalRoyaltiesPaid()
	require.Len(t, got, 1)
	assert.Equal(t, "800", got[0].Amount.String())
}

// TestCACRoyaltyStats_EarnedAndPaidReadDistinctFields pins that earned and
// paid read different fields — "earned - paid" is the royalty ledger
// semantic; aliasing would zero net royalties for every publisher.
func TestCACRoyaltyStats_EarnedAndPaidReadDistinctFields(t *testing.T) {
	t.Parallel()
	m := &CACRoyaltyStats{
		TotalRoyaltiesEarned: sdk.NewCoins(sdk.NewInt64Coin("ulac", 500)),
		TotalRoyaltiesPaid:   sdk.NewCoins(sdk.NewInt64Coin("ulac", 300)),
	}
	earned := m.GetTotalRoyaltiesEarned()
	paid := m.GetTotalRoyaltiesPaid()

	assert.Equal(t, "500", earned[0].Amount.String())
	assert.Equal(t, "300", paid[0].Amount.String(),
		"earned=500, paid=300 — distinct fields")
}

func TestCACRoyaltyStats_LastUpdatedTime_Nil(t *testing.T) {
	t.Parallel()
	var m *CACRoyaltyStats
	assert.True(t, m.GetLastUpdated().IsZero(),
		"nil receiver → zero time")

	m2 := &CACRoyaltyStats{}
	assert.True(t, m2.GetLastUpdated().IsZero(),
		"zero LastUpdated field → zero time")
}

func TestCACRoyaltyStats_LastUpdatedTime_Populated(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)
	m := &CACRoyaltyStats{
		LastUpdated: now,
	}
	assert.Equal(t, now, m.GetLastUpdated())
}

// ---- Cross-sibling parity ----

func TestCACRoyalty_TimestampNilGuardsAreConsistent(t *testing.T) {
	t.Parallel()
	var nilRecord *CACRoyaltyRecord
	var nilStats *CACRoyaltyStats
	emptyRecord := &CACRoyaltyRecord{}
	emptyStats := &CACRoyaltyStats{}

	assert.True(t, nilRecord.GetTimestamp().IsZero())
	assert.True(t, nilStats.GetLastUpdated().IsZero())
	assert.True(t, emptyRecord.GetTimestamp().IsZero())
	assert.True(t, emptyStats.GetLastUpdated().IsZero())
}

// TestCACRoyalty_CoinRoundTripPreservesValues pins the lossless round-trip
// for Coin projections through CoinsToProto (now an identity shim).
func TestCACRoyalty_CoinRoundTripPreservesValues(t *testing.T) {
	t.Parallel()
	orig := sdk.NewCoins(
		sdk.NewInt64Coin("ulac", 1000),
		sdk.NewInt64Coin("ulume", 500),
	)
	m := &CACRoyaltyStats{
		TotalRoyaltiesEarned: CoinsToProto(orig),
	}
	back := m.GetTotalRoyaltiesEarned()
	assert.True(t, orig.Equal(back),
		"Coin roundtrip through CACRoyaltyStats preserves value")
}
