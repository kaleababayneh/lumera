package types

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This file closes DIRECT-test coverage for the CAC-related
// genesis validation paths in x/credits/types/genesis.go that
// had ZERO direct tests prior:
//
//   - validateCACRoyalties (:161-176): 3 error arms (nil
//     record, empty record_id, duplicate record_id)
//   - validateCACStats     (:179-194): 3 error arms (nil
//     stats, empty tool_id, duplicate tool_id)
//   - NewGenesisState      (:11-34):   nil-params fallback +
//     slice-to-pointer conversion
//
// Existing TestGenesis_Validate_* tests cover Locks /
// Settlements / Disputes (same validation pattern) but NOT
// the CAC-royalty or CAC-stats paths. CAC royalties are the
// credits module's cross-publisher distribution ledger (tick
// 206 pinned the CACRoyalty* proto helpers); genesis import
// without validation would let an attacker inject duplicate
// record IDs to shadow legitimate ones.
//
// Scan-angle #5 (sibling-pattern pinning with shared semantic)
// applies: all FIVE genesis validators (Locks, Settlements,
// Disputes, CACRoyalties, CACStats) share the same 3-arm
// pattern: (a) nil entry rejected, (b) empty primary-key
// rejected, (c) duplicate primary-key rejected. A refactor
// that relaxed ONE would diverge the consistency invariant.
//
// Scan-angle #3 (hidden-secondary-return pinning) on
// NewGenesisState's slice-to-pointer conversion at :15-26:
// the function takes []T slices and converts them to []*T
// arrays. A refactor that shared slice backing (e.g., via
// append + slice-of-pointer) would create aliasing where
// caller mutations to the input slice leaked into the
// genesis state.

// ---- validateCACRoyalties ----

// TestGenesis_Validate_NilCACRoyalty pins the :163-165 nil-
// entry guard.
func TestGenesis_Validate_NilCACRoyalty(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacRoyalties = []*CACRoyaltyRecord{nil}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAC royalty record cannot be nil")
}

// TestGenesis_Validate_EmptyCACRoyaltyID pins :166-168 empty-
// ID guard.
func TestGenesis_Validate_EmptyCACRoyaltyID(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacRoyalties = []*CACRoyaltyRecord{{RecordId: ""}}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAC royalty record id cannot be empty")
}

// TestGenesis_Validate_DuplicateCACRoyaltyID pins :169-171
// duplicate-ID guard. CRITICAL — genesis duplicates would
// let an attacker shadow legitimate records on state import.
func TestGenesis_Validate_DuplicateCACRoyaltyID(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacRoyalties = []*CACRoyaltyRecord{
		{RecordId: "cac-1"},
		{RecordId: "cac-1"},
	}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate CAC royalty record id cac-1",
		"CRITICAL — duplicate detection pins the primary-key-"+
			"uniqueness invariant at genesis import. A refactor that "+
			"dropped the `seen` set would let an attacker inject "+
			"duplicate record_ids that shadow legitimate royalties "+
			"(last-write-wins in downstream collections.Map.)")
}

// TestGenesis_Validate_ValidCACRoyaltySet pins the happy path.
func TestGenesis_Validate_ValidCACRoyaltySet(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacRoyalties = []*CACRoyaltyRecord{
		{RecordId: "cac-1", OriginToolId: "t1", ServingToolId: "t2"},
		{RecordId: "cac-2", OriginToolId: "t3", ServingToolId: "t4"},
	}
	require.NoError(t, gs.Validate())
}

// ---- validateCACStats ----

// TestGenesis_Validate_NilCACStats pins :181-183 nil guard.
func TestGenesis_Validate_NilCACStats(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacStats = []*CACRoyaltyStats{nil}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAC stats entry cannot be nil")
}

// TestGenesis_Validate_EmptyCACStatsToolID pins :184-186
// empty-tool-id guard. Unlike CACRoyalties (primary key =
// record_id), CACStats are keyed by TOOL_ID — the per-tool
// aggregate.
func TestGenesis_Validate_EmptyCACStatsToolID(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacStats = []*CACRoyaltyStats{{ToolId: ""}}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAC stats tool_id cannot be empty",
		"pins that CACStats primary key is tool_id, NOT record_id "+
			"(asymmetric with CACRoyalty). A refactor that keyed "+
			"stats by record_id would break per-tool aggregation "+
			"rollups.")
}

// TestGenesis_Validate_DuplicateCACStatsToolID pins :187-189
// duplicate-tool-id guard.
func TestGenesis_Validate_DuplicateCACStatsToolID(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacStats = []*CACRoyaltyStats{
		{ToolId: "tool-1"},
		{ToolId: "tool-1"},
	}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate CAC stats for tool tool-1")
}

// TestGenesis_Validate_ValidCACStatsSet pins the happy path.
func TestGenesis_Validate_ValidCACStatsSet(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacStats = []*CACRoyaltyStats{
		{ToolId: "tool-1"},
		{ToolId: "tool-2"},
	}
	require.NoError(t, gs.Validate())
}

// ---- validateLocks state invariants ----

func validGenesisLock() *Lock {
	now := time.Unix(1_700_000_000, 0).UTC()
	return &Lock{
		LockId:    "lock-valid",
		Router:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		SessionId: "session-1",
		ToolId:    "tool-1",
		Amount:    &basev1beta1.Coin{Denom: DefaultCreditDenom, Amount: "1000"},
		CreatedAt: timestamppb.New(now),
		ExpiresAt: timestamppb.New(now.Add(time.Hour)),
		Status:    LockStatus_LOCK_STATUS_ACTIVE,
	}
}

func invalidGenesisTimestamp() *timestamppb.Timestamp {
	return &timestamppb.Timestamp{Seconds: 253402300800}
}

// TestGenesis_Validate_ActiveLockRequiresExpiresAt pins parity with
// the keeper lock-state invariant: an active lock without ExpiresAt
// is never rebuilt into the expiry index, so import must fail closed.
func TestGenesis_Validate_ActiveLockRequiresExpiresAt(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	lock := validGenesisLock()
	lock.ExpiresAt = nil
	gs.Locks = []*Lock{lock}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "active lock lock-valid missing expires_at")
}

// TestGenesis_Validate_UnspecifiedLockStatusRejected pins that the
// proto enum zero value cannot be imported as persisted lock state.
func TestGenesis_Validate_UnspecifiedLockStatusRejected(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	lock := validGenesisLock()
	lock.Status = LockStatus_LOCK_STATUS_UNSPECIFIED
	gs.Locks = []*Lock{lock}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock lock-valid has unspecified status")
}

// TestGenesis_Validate_UnknownLockStatusRejected pins validation for
// out-of-range enum values decoded from hand-edited genesis JSON.
func TestGenesis_Validate_UnknownLockStatusRejected(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	lock := validGenesisLock()
	lock.Status = LockStatus(99)
	gs.Locks = []*Lock{lock}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock lock-valid has invalid status 99")
}

// TestGenesis_Validate_LockAmountMustBePositive pins the same
// non-positive lock rejection enforced by runtime invariants.
func TestGenesis_Validate_LockAmountMustBePositive(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	lock := validGenesisLock()
	lock.Amount = &basev1beta1.Coin{Denom: DefaultCreditDenom, Amount: "0"}
	gs.Locks = []*Lock{lock}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock lock-valid amount must be positive")
}

// TestGenesis_Validate_LockTimestampsMustBeValid rejects malformed
// protobuf timestamps before InitGenesis stores them.
func TestGenesis_Validate_LockTimestampsMustBeValid(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	lock := validGenesisLock()
	lock.ExpiresAt = invalidGenesisTimestamp()
	gs.Locks = []*Lock{lock}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock lock-valid has invalid expires_at")
}

// TestGenesis_Validate_LockExpiresAfterCreatedAt rejects imported
// active locks that could not be produced by the keeper lock lifecycle.
func TestGenesis_Validate_LockExpiresAfterCreatedAt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		expiresAt time.Time
	}{
		{
			name:      "equal_created_at",
			expiresAt: time.Unix(1_700_000_000, 0).UTC(),
		},
		{
			name:      "before_created_at",
			expiresAt: time.Unix(1_699_999_999, 0).UTC(),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gs := DefaultGenesis()
			lock := validGenesisLock()
			lock.ExpiresAt = timestamppb.New(tc.expiresAt)
			gs.Locks = []*Lock{lock}

			err := gs.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "lock lock-valid expires_at must be after created_at")
		})
	}
}

func TestGenesis_Validate_SettlementTimestampsMustBeValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutator func(*SettlementRecord)
		wantMsg string
	}{
		{
			name:    "timestamp",
			mutator: func(record *SettlementRecord) { record.Timestamp = invalidGenesisTimestamp() },
			wantMsg: "settlement settlement-1 has invalid timestamp",
		},
		{
			name:    "completed_at",
			mutator: func(record *SettlementRecord) { record.CompletedAt = invalidGenesisTimestamp() },
			wantMsg: "settlement settlement-1 has invalid completed_at",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gs := DefaultGenesis()
			record := &SettlementRecord{Id: "settlement-1", Status: SettlementStatus_SETTLEMENT_STATUS_PENDING}
			c.mutator(record)
			gs.Settlements = []*SettlementRecord{record}
			err := gs.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

func TestGenesis_Validate_SettlementCompletedAtMustNotPrecedeTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()

	cases := []struct {
		name        string
		completedAt time.Time
		wantErr     bool
	}{
		{
			name:        "before timestamp rejected",
			completedAt: now.Add(-time.Second),
			wantErr:     true,
		},
		{
			name:        "equal timestamp allowed",
			completedAt: now,
			wantErr:     false,
		},
		{
			name:        "after timestamp allowed",
			completedAt: now.Add(time.Second),
			wantErr:     false,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			gs := DefaultGenesis()
			gs.Settlements = []*SettlementRecord{{
				Id:          "settlement-1",
				Status:      SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
				Timestamp:   timestamppb.New(now),
				CompletedAt: timestamppb.New(c.completedAt),
			}}

			err := gs.Validate()
			if c.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "completed_at must be at or after timestamp")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGenesis_Validate_SettlementStatusMustBeSpecified(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  SettlementStatus
		wantMsg string
	}{
		{
			name:    "unspecified",
			status:  SettlementStatus_SETTLEMENT_STATUS_UNSPECIFIED,
			wantMsg: "settlement settlement-1 has unspecified status",
		},
		{
			name:    "unknown",
			status:  SettlementStatus(99),
			wantMsg: "settlement settlement-1 has invalid status 99",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			gs := DefaultGenesis()
			gs.Settlements = []*SettlementRecord{{
				Id:     "settlement-1",
				Status: c.status,
			}}

			err := gs.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

func TestGenesis_Validate_SettlementStatusCompletedAtLifecycle(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()

	cases := []struct {
		name        string
		status      SettlementStatus
		completedAt *timestamppb.Timestamp
		wantMsg     string
	}{
		{
			name:    "completed missing completed_at",
			status:  SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
			wantMsg: "terminal settlement settlement-1 missing completed_at",
		},
		{
			name:    "failed missing completed_at",
			status:  SettlementStatus_SETTLEMENT_STATUS_FAILED,
			wantMsg: "terminal settlement settlement-1 missing completed_at",
		},
		{
			name:        "pending with completed_at",
			status:      SettlementStatus_SETTLEMENT_STATUS_PENDING,
			completedAt: timestamppb.New(now),
			wantMsg:     "pending settlement settlement-1 must not have completed_at",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			gs := DefaultGenesis()
			gs.Settlements = []*SettlementRecord{{
				Id:          "settlement-1",
				Status:      c.status,
				CompletedAt: c.completedAt,
			}}

			err := gs.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

func TestGenesis_Validate_DisputeTimestampsMustBeValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutator func(*DisputeRecord)
		wantMsg string
	}{
		{
			name:    "created_at",
			mutator: func(record *DisputeRecord) { record.CreatedAt = invalidGenesisTimestamp() },
			wantMsg: "dispute dispute-1 has invalid created_at",
		},
		{
			name:    "resolved_at",
			mutator: func(record *DisputeRecord) { record.ResolvedAt = invalidGenesisTimestamp() },
			wantMsg: "dispute dispute-1 has invalid resolved_at",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gs := DefaultGenesis()
			record := &DisputeRecord{Id: "dispute-1"}
			c.mutator(record)
			gs.Disputes = []*DisputeRecord{record}
			err := gs.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

func TestGenesis_Validate_DisputeResolvedAtMustNotPrecedeCreatedAt(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()

	cases := []struct {
		name       string
		resolvedAt time.Time
		wantErr    bool
	}{
		{
			name:       "before created rejected",
			resolvedAt: now.Add(-time.Second),
			wantErr:    true,
		},
		{
			name:       "equal created allowed",
			resolvedAt: now,
			wantErr:    false,
		},
		{
			name:       "after created allowed",
			resolvedAt: now.Add(time.Second),
			wantErr:    false,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			gs := DefaultGenesis()
			gs.Disputes = []*DisputeRecord{{
				Id:         "dispute-1",
				CreatedAt:  timestamppb.New(now),
				ResolvedAt: timestamppb.New(c.resolvedAt),
			}}

			err := gs.Validate()
			if c.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "resolved_at must be at or after created_at")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGenesis_Validate_CACTimestampsMustBeValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutator func(*GenesisState)
		wantMsg string
	}{
		{
			name: "royalty timestamp",
			mutator: func(gs *GenesisState) {
				gs.CacRoyalties = []*CACRoyaltyRecord{{
					RecordId:  "cac-1",
					Timestamp: invalidGenesisTimestamp(),
				}}
			},
			wantMsg: "CAC royalty record cac-1 has invalid timestamp",
		},
		{
			name: "stats last_updated",
			mutator: func(gs *GenesisState) {
				gs.CacStats = []*CACRoyaltyStats{{
					ToolId:      "tool-1",
					LastUpdated: invalidGenesisTimestamp(),
				}}
			},
			wantMsg: "CAC stats tool-1 has invalid last_updated",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gs := DefaultGenesis()
			c.mutator(gs)
			err := gs.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg)
		})
	}
}

func TestGenesis_Validate_NilOptionalTimestampsRemainAllowed(t *testing.T) {
	t.Parallel()

	gs := DefaultGenesis()
	gs.Settlements = []*SettlementRecord{{Id: "settlement-1", Status: SettlementStatus_SETTLEMENT_STATUS_PENDING}}
	gs.Disputes = []*DisputeRecord{{Id: "dispute-1"}}
	gs.CacRoyalties = []*CACRoyaltyRecord{{RecordId: "cac-1"}}
	gs.CacStats = []*CACRoyaltyStats{{ToolId: "tool-1"}}

	require.NoError(t, gs.Validate())
}

func TestGenesis_Validate_ValidActiveLock(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.Locks = []*Lock{validGenesisLock()}
	require.NoError(t, gs.Validate())
}

// ---- Cross-validator parity anchor ----

// TestGenesis_Validate_FiveValidatorsShareThreeArmPattern is
// the scan-angle #5 SIBLING ANCHOR. Five genesis validators
// (Locks, Settlements, Disputes, CACRoyalties, CACStats) share
// the same 3-arm pattern: nil-entry, empty-primary-key,
// duplicate-primary-key. Pin that each reports a DIFFERENT
// error message signaling WHICH collection failed — without
// the distinct messages, operator triage on genesis-import
// failure can't locate the bad record.
func TestGenesis_Validate_FiveCollectionsDistinctNilMessages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutator func(*GenesisState)
		wantMsg string
	}{
		{
			name:    "Locks",
			mutator: func(g *GenesisState) { g.Locks = []*Lock{nil} },
			wantMsg: "lock entry cannot be nil",
		},
		{
			name:    "Settlements",
			mutator: func(g *GenesisState) { g.Settlements = []*SettlementRecord{nil} },
			wantMsg: "settlement entry cannot be nil",
		},
		{
			name:    "Disputes",
			mutator: func(g *GenesisState) { g.Disputes = []*DisputeRecord{nil} },
			wantMsg: "dispute entry cannot be nil",
		},
		{
			name:    "CACRoyalties",
			mutator: func(g *GenesisState) { g.CacRoyalties = []*CACRoyaltyRecord{nil} },
			wantMsg: "CAC royalty record cannot be nil",
		},
		{
			name:    "CACStats",
			mutator: func(g *GenesisState) { g.CacStats = []*CACRoyaltyStats{nil} },
			wantMsg: "CAC stats entry cannot be nil",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gs := DefaultGenesis()
			c.mutator(gs)
			err := gs.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantMsg,
				"%s validator produces %q-specific message. Pins "+
					"that operators can locate which collection had "+
					"the bad record from the error alone; a refactor "+
					"using a generic 'nil entry' message would lose "+
					"the triage signal.",
				c.name, c.wantMsg)
		})
	}
}

// ---- NewGenesisState ----

// TestNewGenesisState_NilParamsFallsBackToDefault pins :12-14.
func TestNewGenesisState_NilParamsFallsBackToDefault(t *testing.T) {
	t.Parallel()
	gs := NewGenesisState(nil, nil, nil, nil, nil)
	require.NotNil(t, gs.Params,
		"nil Params → DefaultParams fallback. Pins :12-14 "+
			"bootstrap guarantee — callers passing nil for convenience "+
			"get a validated default set, not a nil-pointer panic at "+
			"Validate() time.")
	// Returned Params must be equivalent to DefaultParams.
	assert.Equal(t, DefaultParams().CreditDenom, gs.Params.CreditDenom)
}

// TestNewGenesisState_CustomParamsPreserved pins the happy
// path — non-nil params pass through.
func TestNewGenesisState_CustomParamsPreserved(t *testing.T) {
	t.Parallel()
	custom := &Params{CreditDenom: "ulac"}
	gs := NewGenesisState(custom, nil, nil, nil, nil)
	require.Same(t, custom, gs.Params,
		"caller-supplied Params stored verbatim (not copied)")
}

// TestNewGenesisState_SliceToPointerConversion pins the
// scan-angle #3 subtle invariant at :15-26. The function
// takes []Lock and returns []*Lock by taking the address of
// each slice element. A refactor that used the loop variable
// directly (like `lockPtrs[i] = &lock` inside `for i, lock :=
// range locks`) would alias every pointer to the same final
// slice element.
func TestNewGenesisState_SliceToPointerConversionNoAliasing(t *testing.T) {
	t.Parallel()
	locks := []Lock{
		{LockId: "lock-1", Amount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"}},
		{LockId: "lock-2", Amount: &basev1beta1.Coin{Denom: "ulac", Amount: "200"}},
		{LockId: "lock-3", Amount: &basev1beta1.Coin{Denom: "ulac", Amount: "300"}},
	}

	gs := NewGenesisState(nil, locks, nil, nil, nil)
	require.Len(t, gs.Locks, 3)

	// CRITICAL — each *Lock must point to a DISTINCT value.
	// A refactor that used `&lock` (the loop variable) would
	// alias all three pointers to the final element.
	assert.Equal(t, "lock-1", gs.Locks[0].LockId)
	assert.Equal(t, "lock-2", gs.Locks[1].LockId)
	assert.Equal(t, "lock-3", gs.Locks[2].LockId,
		"third Lock preserved. Pins against the loop-variable "+
			"aliasing bug: a refactor using `&lock` (loop var) "+
			"instead of `&locks[i]` would make all three pointers "+
			"reference the final iteration's value — silently "+
			"corrupting every imported genesis Lock set.")
}

// TestNewGenesisState_EmptyInputsProduceEmptySlices pins the
// make-with-len=0 invariant — nil input slices produce
// non-nil but empty-length pointer slices.
func TestNewGenesisState_EmptyInputsProduceNonNilSlices(t *testing.T) {
	t.Parallel()
	gs := NewGenesisState(nil, nil, nil, nil, nil)

	// All three pointer slices non-nil (make-with-len-0).
	assert.NotNil(t, gs.Locks)
	assert.Empty(t, gs.Locks)
	assert.NotNil(t, gs.Settlements)
	assert.Empty(t, gs.Settlements)
	assert.NotNil(t, gs.Disputes)
	assert.Empty(t, gs.Disputes)
}

// TestNewGenesisState_AllCollectionsPopulated pins the full-
// input happy path preserves all four collections.
func TestNewGenesisState_AllCollectionsPopulated(t *testing.T) {
	t.Parallel()
	locks := []Lock{{LockId: "l-1"}}
	settlements := []SettlementRecord{{Id: "s-1"}}
	disputes := []DisputeRecord{{Id: "d-1"}}
	metrics := &SettlementMetrics{TotalProcessed: 42}

	gs := NewGenesisState(nil, locks, settlements, disputes, metrics)
	require.Len(t, gs.Locks, 1)
	require.Len(t, gs.Settlements, 1)
	require.Len(t, gs.Disputes, 1)
	require.NotNil(t, gs.Metrics)
	assert.Equal(t, uint64(42), gs.Metrics.TotalProcessed)
}
