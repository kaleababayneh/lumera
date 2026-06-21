package keeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// TestFinalizeSettlementWithLock_RejectsNilRecord pins the first
// guard at keeper.go:2288-2290. FinalizeSettlementWithLock is
// called from the BeginBlocker pending-settlement loop at
// abci.go:70 — a nil record entering that loop must fast-fail
// rather than NPE-cascade through the subsequent SettleLock call.
// Prior to this test, neither FinalizeSettlementWithLock nor its
// BeginBlocker wrapper had direct coverage of this guard.
func TestFinalizeSettlementWithLock_RejectsNilRecord(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	err := keeper.FinalizeSettlementWithLock(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "record cannot be nil")
}

// TestFinalizeSettlementWithLock_RejectsEmptyLockID pins the
// lock_id guard at keeper.go:2291-2293. A settlement record whose
// LockId is empty cannot resolve to a lock to finalize against; the
// early-return here prevents the downstream SettleLock call from
// producing a misleading "lock not found" error on what is really a
// malformed settlement state-entry upstream.
func TestFinalizeSettlementWithLock_RejectsEmptyLockID(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	record := &types.SettlementRecord{
		Id:     "settlement-empty-lock",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		// LockId intentionally omitted.
	}
	err := keeper.FinalizeSettlementWithLock(ctx, record)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lock id")
}

// TestFinalizeSettlementWithLock_RejectsInvalidPublisherID pins the
// publisher bech32 parse at keeper.go:2296-2299. A malformed
// publisher_id must fail fast with a wrapped parse error — NOT
// cascade into SettleLock where it would surface as an obscure
// "invalid account address" from deep inside the revenue-split
// math. Guard ordering matters: this check runs BEFORE any state
// mutation, so even if SettleLock partially landed under some
// future refactor, this test would still surface the regression.
func TestFinalizeSettlementWithLock_RejectsInvalidPublisherID(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	record := &types.SettlementRecord{
		Id:          "settlement-bad-pub",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		LockId:      "lock-xxx",
		PublisherId: "not-a-valid-bech32",
		RouterId:    "cosmos1router" + "00000000000000000000000000000000000",
	}
	err := keeper.FinalizeSettlementWithLock(ctx, record)
	require.Error(t, err)
	require.Contains(t, err.Error(), "publisher")
}

// TestFinalizeSettlementWithLock_RejectsInvalidRouterID pins the
// router bech32 parse at keeper.go:2300-2303. Same guard-ordering
// rationale as the publisher check: error surfaces at the address
// layer, never at the settlement-math layer.
func TestFinalizeSettlementWithLock_RejectsInvalidRouterID(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	pub := newAccAddress()
	record := &types.SettlementRecord{
		Id:          "settlement-bad-router",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		LockId:      "lock-xxx",
		PublisherId: pub.String(),
		RouterId:    "not-a-valid-bech32-either",
	}
	err := keeper.FinalizeSettlementWithLock(ctx, record)
	require.Error(t, err)
	require.Contains(t, err.Error(), "router")
}

// TestFinalizeSettlementWithLock_RejectsInvalidReferrerIDButAllowsEmpty
// pins BOTH branches of the conditional referrer parse at
// keeper.go:2304-2310. The referrer is optional, so:
//
//   - ReferrerId == ""     → no parse attempted, function proceeds.
//   - ReferrerId != ""     → parse must succeed or the function
//     errors out before touching SettleLock.
//
// A regression that inverted the branch (parse on empty, skip on
// non-empty) would silently ignore malformed referrer addresses
// and push them into SettleLock where the error would look like it
// came from elsewhere.
func TestFinalizeSettlementWithLock_RejectsInvalidReferrerIDButAllowsEmpty(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	pub := newAccAddress()
	router := newAccAddress()

	// Non-empty + malformed ReferrerId → must error at referrer layer.
	recordBadReferrer := &types.SettlementRecord{
		Id:          "settlement-bad-ref",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		LockId:      "lock-xxx",
		PublisherId: pub.String(),
		RouterId:    router.String(),
		ReferrerId:  "not-a-valid-bech32",
	}
	err := keeper.FinalizeSettlementWithLock(ctx, recordBadReferrer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "referrer")

	// Empty ReferrerId → must NOT fail at the referrer layer. The
	// function will instead fail downstream because no lock exists
	// for lock-xxx; what we care about here is that it gets PAST
	// the referrer guard (not that the overall call succeeds).
	recordEmptyReferrer := &types.SettlementRecord{
		Id:          "settlement-no-ref",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		LockId:      "lock-xxx",
		PublisherId: pub.String(),
		RouterId:    router.String(),
		ReferrerId:  "", // intentionally empty
	}
	err = keeper.FinalizeSettlementWithLock(ctx, recordEmptyReferrer)
	require.Error(t, err, "should still error — lock-xxx does not exist")
	require.NotContains(t, err.Error(), "referrer",
		"empty ReferrerId must skip the referrer parse entirely, "+
			"not emit a 'invalid referrer' error")
}

// TestAdaptiveBurnWindowDuration_Constant pins the 30-day trailing
// retention window used by the BeginBlocker settlement-pruning
// comparison at abci.go:126. A regression that silently changed
// adaptiveBurnWindowDays (say, to 3 or 300) would shift the
// retention horizon without any direct test catching the drift —
// prod effects would only surface as chain-state-size changes or
// burn-rate oscillation that take days to observe.
func TestAdaptiveBurnWindowDuration_Constant(t *testing.T) {
	got := AdaptiveBurnWindowDuration()
	require.Equal(t, 30*24*time.Hour, got,
		"AdaptiveBurnWindowDuration pins the 30-day trailing window for "+
			"adaptive burn; change this constant only as part of a deliberate "+
			"governance-visible parameter migration")
	require.Positive(t, got, "duration must be positive — the comparison at "+
		"abci.go:126 uses `adaptiveWindow > retentionWindow` as a floor")
}
