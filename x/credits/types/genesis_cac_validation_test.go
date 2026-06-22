package types

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DIRECT-test coverage for the CAC-related genesis validation paths in
// x/credits/types/genesis.go (validateCACRoyalties, validateCACStats,
// NewGenesisState) plus the lock/settlement/dispute state invariants.
//
// PORTED NOTE (gogoproto migration): in lumera_ai the Coin/timestamp fields
// were protobuf-go wire types (*basev1beta1.Coin, *timestamppb.Timestamp), and
// genesis validation rejected malformed protobuf timestamps (e.g. seconds out
// of range). After the migration these fields are native sdk.Coin and
// time.Time — a time.Time cannot be "malformed", so the "invalid timestamp"
// validation arms were intentionally NOT ported. Tests that exercised those
// arms are skipped with a reason; all other invariants (nil/empty/duplicate
// primary key, active-lock-requires-expires, status, ordering, slice→pointer
// no-aliasing) are preserved against the native API.

// ---- validateCACRoyalties ----

func TestGenesis_Validate_NilCACRoyalty(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacRoyalties = []*CACRoyaltyRecord{nil}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAC royalty record cannot be nil")
}

func TestGenesis_Validate_EmptyCACRoyaltyID(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacRoyalties = []*CACRoyaltyRecord{{RecordId: ""}}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAC royalty record id cannot be empty")
}

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
		"CRITICAL — duplicate detection pins the primary-key-uniqueness "+
			"invariant at genesis import.")
}

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

func TestGenesis_Validate_NilCACStats(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacStats = []*CACRoyaltyStats{nil}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAC stats entry cannot be nil")
}

func TestGenesis_Validate_EmptyCACStatsToolID(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.CacStats = []*CACRoyaltyStats{{ToolId: ""}}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAC stats tool_id cannot be empty",
		"pins that CACStats primary key is tool_id, NOT record_id")
}

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
		Amount:    sdk.NewInt64Coin(DefaultCreditDenom, 1000),
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
		Status:    LockStatus_LOCK_STATUS_ACTIVE,
	}
}

// TestGenesis_Validate_ActiveLockRequiresExpiresAt pins parity with
// the keeper lock-state invariant: an active lock without ExpiresAt
// is never rebuilt into the expiry index, so import must fail closed.
func TestGenesis_Validate_ActiveLockRequiresExpiresAt(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	lock := validGenesisLock()
	lock.ExpiresAt = time.Time{}
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
	lock.Amount = sdk.NewInt64Coin(DefaultCreditDenom, 0)
	gs.Locks = []*Lock{lock}
	err := gs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock lock-valid amount must be positive")
}

// TestGenesis_Validate_LockTimestampsMustBeValid: not ported — native
// time.Time fields cannot carry a malformed protobuf timestamp, so
// genesis.go has no "invalid expires_at" validation arm.
func TestGenesis_Validate_LockTimestampsMustBeValid(t *testing.T) {
	t.Skip("not ported: gogoproto stdtime fields are time.Time and cannot be malformed; no invalid-timestamp validation exists in genesis.go")
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
			lock.ExpiresAt = tc.expiresAt
			gs.Locks = []*Lock{lock}

			err := gs.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "lock lock-valid expires_at must be after created_at")
		})
	}
}

// TestGenesis_Validate_SettlementTimestampsMustBeValid: not ported (see note
// above) — no malformed-timestamp validation exists for settlements.
func TestGenesis_Validate_SettlementTimestampsMustBeValid(t *testing.T) {
	t.Skip("not ported: gogoproto stdtime fields cannot be malformed; genesis.go has no invalid-timestamp arm for settlements")
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

			completedAt := c.completedAt
			gs := DefaultGenesis()
			gs.Settlements = []*SettlementRecord{{
				Id:          "settlement-1",
				Status:      SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
				Timestamp:   now,
				CompletedAt: &completedAt,
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
		completedAt *time.Time
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
			completedAt: &now,
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

// TestGenesis_Validate_DisputeTimestampsMustBeValid: not ported (see note) —
// no malformed-timestamp validation exists for disputes.
func TestGenesis_Validate_DisputeTimestampsMustBeValid(t *testing.T) {
	t.Skip("not ported: gogoproto stdtime fields cannot be malformed; genesis.go has no invalid-timestamp arm for disputes")
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

			resolvedAt := c.resolvedAt
			gs := DefaultGenesis()
			gs.Disputes = []*DisputeRecord{{
				Id:         "dispute-1",
				CreatedAt:  now,
				ResolvedAt: &resolvedAt,
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

// TestGenesis_Validate_CACTimestampsMustBeValid: not ported (see note) — no
// malformed-timestamp validation exists for CAC royalties/stats.
func TestGenesis_Validate_CACTimestampsMustBeValid(t *testing.T) {
	t.Skip("not ported: gogoproto stdtime fields cannot be malformed; genesis.go has no invalid-timestamp arm for CAC royalties/stats")
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

// TestGenesis_Validate_FiveCollectionsDistinctNilMessages pins that each of
// the five genesis validators reports a distinct nil-entry message so
// operators can locate which collection had the bad record.
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
				"%s validator produces %q-specific message", c.name, c.wantMsg)
		})
	}
}

// ---- NewGenesisState ----

func TestNewGenesisState_NilParamsFallsBackToDefault(t *testing.T) {
	t.Parallel()
	gs := NewGenesisState(nil, nil, nil, nil, nil)
	require.NotNil(t, gs.Params,
		"nil Params → DefaultParams fallback")
	assert.Equal(t, DefaultParams().CreditDenom, gs.Params.CreditDenom)
}

func TestNewGenesisState_CustomParamsPreserved(t *testing.T) {
	t.Parallel()
	custom := &Params{CreditDenom: "ulac"}
	gs := NewGenesisState(custom, nil, nil, nil, nil)
	require.Same(t, custom, gs.Params,
		"caller-supplied Params stored verbatim (not copied)")
}

// TestNewGenesisState_SliceToPointerConversionNoAliasing pins that the
// []Lock → []*Lock conversion takes the address of each slice element
// (&locks[i]), not the loop variable — otherwise all pointers would alias the
// final element.
func TestNewGenesisState_SliceToPointerConversionNoAliasing(t *testing.T) {
	t.Parallel()
	locks := []Lock{
		{LockId: "lock-1", Amount: sdk.NewInt64Coin("ulac", 100)},
		{LockId: "lock-2", Amount: sdk.NewInt64Coin("ulac", 200)},
		{LockId: "lock-3", Amount: sdk.NewInt64Coin("ulac", 300)},
	}

	gs := NewGenesisState(nil, locks, nil, nil, nil)
	require.Len(t, gs.Locks, 3)

	assert.Equal(t, "lock-1", gs.Locks[0].LockId)
	assert.Equal(t, "lock-2", gs.Locks[1].LockId)
	assert.Equal(t, "lock-3", gs.Locks[2].LockId,
		"third Lock preserved — pins against the loop-variable aliasing bug")
}

func TestNewGenesisState_EmptyInputsProduceNonNilSlices(t *testing.T) {
	t.Parallel()
	gs := NewGenesisState(nil, nil, nil, nil, nil)

	assert.NotNil(t, gs.Locks)
	assert.Empty(t, gs.Locks)
	assert.NotNil(t, gs.Settlements)
	assert.Empty(t, gs.Settlements)
	assert.NotNil(t, gs.Disputes)
	assert.Empty(t, gs.Disputes)
}

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
