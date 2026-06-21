package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// --- SaveSettlement ---

func TestSaveSettlement_Success(t *testing.T) {
	fixture := setupRealKeeper(t)

	settlement := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
	}
	err := fixture.keeper.SaveSettlement(fixture.ctx, settlement)
	require.NoError(t, err)

	// Verify it was stored by iterating
	var found bool
	err = fixture.keeper.IterateSettlements(fixture.ctx, func(s *types.SettlementRecord) bool {
		if s.Id == "settlement-1" {
			found = true
			return true
		}
		return false
	})
	require.NoError(t, err)
	require.True(t, found)
}

func TestSaveSettlement_Nil(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.SaveSettlement(fixture.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestSaveSettlement_EmptyID(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.SaveSettlement(fixture.ctx, &types.SettlementRecord{Id: ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

func TestSaveSettlement_Overwrite(t *testing.T) {
	fixture := setupRealKeeper(t)

	s1 := &types.SettlementRecord{
		Id:     "settlement-ow",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}
	require.NoError(t, fixture.keeper.SaveSettlement(fixture.ctx, s1))

	s2 := &types.SettlementRecord{
		Id:     "settlement-ow",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
	}
	require.NoError(t, fixture.keeper.SaveSettlement(fixture.ctx, s2))

	// Verify latest version
	var count int
	err := fixture.keeper.IterateSettlements(fixture.ctx, func(s *types.SettlementRecord) bool {
		if s.Id == "settlement-ow" {
			count++
			require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, s.Status)
		}
		return false
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// --- IterateSettlements ---

func TestIterateSettlements_Empty(t *testing.T) {
	fixture := setupRealKeeper(t)

	var count int
	err := fixture.keeper.IterateSettlements(fixture.ctx, func(_ *types.SettlementRecord) bool {
		count++
		return false
	})
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestIterateSettlements_StopsOnTrue(t *testing.T) {
	fixture := setupRealKeeper(t)

	for i := 0; i < 5; i++ {
		require.NoError(t, fixture.keeper.SaveSettlement(fixture.ctx, &types.SettlementRecord{
			Id: "settlement-iter-" + string(rune('a'+i)),
		}))
	}

	var count int
	err := fixture.keeper.IterateSettlements(fixture.ctx, func(_ *types.SettlementRecord) bool {
		count++
		return true // stop after first
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// --- SaveDispute ---

func TestSaveDispute_Success(t *testing.T) {
	fixture := setupRealKeeper(t)

	dispute := &types.DisputeRecord{
		Id: "dispute-1",
	}
	err := fixture.keeper.SaveDispute(fixture.ctx, dispute)
	require.NoError(t, err)

	loaded, found := fixture.keeper.GetDispute(fixture.ctx, "dispute-1")
	require.True(t, found)
	require.Equal(t, "dispute-1", loaded.Id)
}

func TestSaveDispute_Nil(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.SaveDispute(fixture.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestSaveDispute_EmptyID(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.SaveDispute(fixture.ctx, &types.DisputeRecord{Id: ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

// --- GetDispute ---

func TestGetDispute_NotFound(t *testing.T) {
	fixture := setupRealKeeper(t)

	_, found := fixture.keeper.GetDispute(fixture.ctx, "nonexistent")
	require.False(t, found)
}

// --- IterateDisputes ---

func TestIterateDisputes_Empty(t *testing.T) {
	fixture := setupRealKeeper(t)

	var count int
	err := fixture.keeper.IterateDisputes(fixture.ctx, func(_ *types.DisputeRecord) bool {
		count++
		return false
	})
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestIterateDisputes_StopsOnTrue(t *testing.T) {
	fixture := setupRealKeeper(t)

	for i := 0; i < 3; i++ {
		require.NoError(t, fixture.keeper.SaveDispute(fixture.ctx, &types.DisputeRecord{
			Id: "dispute-iter-" + string(rune('a'+i)),
		}))
	}

	var count int
	err := fixture.keeper.IterateDisputes(fixture.ctx, func(_ *types.DisputeRecord) bool {
		count++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// --- SetMetrics / GetMetrics ---

func TestSetGetMetrics_RoundTrip(t *testing.T) {
	fixture := setupRealKeeper(t)

	metrics := &types.SettlementMetrics{
		TotalProcessed: 42,
		TotalBurned:    "1000000",
	}
	err := fixture.keeper.SetMetrics(fixture.ctx, metrics)
	require.NoError(t, err)

	loaded := fixture.keeper.GetMetrics(fixture.ctx)
	require.NotNil(t, loaded)
	require.Equal(t, uint64(42), loaded.TotalProcessed)
	require.Equal(t, "1000000", loaded.TotalBurned)
}

func TestSetMetrics_Nil(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.SetMetrics(fixture.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestGetMetrics_EmptyState(t *testing.T) {
	fixture := setupRealKeeper(t)

	metrics := fixture.keeper.GetMetrics(fixture.ctx)
	require.NotNil(t, metrics)
	// Should return empty/zero-value metrics
}

// --- ValidateSchemaVersion ---

func TestValidateSchemaVersion_CleanState(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.ValidateSchemaVersion(fixture.ctx)
	require.NoError(t, err)
}

// --- ExportState / ImportState ---

func TestExportState_EmptyState(t *testing.T) {
	fixture := setupRealKeeper(t)

	state, err := fixture.keeper.ExportState(fixture.ctx)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotNil(t, state.Params)
	require.Empty(t, state.Locks)
	require.Empty(t, state.Settlements)
	require.Empty(t, state.Disputes)
}

func TestImportState_Nil(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.ImportState(fixture.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestImportState_WithData(t *testing.T) {
	fixture := setupRealKeeper(t)

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Settlements: []*types.SettlementRecord{
			{Id: "settlement-import-1", Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING},
			{Id: "settlement-import-2", Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING},
		},
		Disputes: []*types.DisputeRecord{
			{Id: "dispute-import-1"},
		},
		Metrics: &types.SettlementMetrics{
			TotalProcessed: 10,
			TotalBurned:    "500000",
		},
	}

	err := fixture.keeper.ImportState(fixture.ctx, genesis)
	require.NoError(t, err)

	// Verify settlements were imported
	var settlementCount int
	err = fixture.keeper.IterateSettlements(fixture.ctx, func(_ *types.SettlementRecord) bool {
		settlementCount++
		return false
	})
	require.NoError(t, err)
	require.Equal(t, 2, settlementCount)

	// Verify disputes
	dispute, found := fixture.keeper.GetDispute(fixture.ctx, "dispute-import-1")
	require.True(t, found)
	require.Equal(t, "dispute-import-1", dispute.Id)

	// Verify metrics
	metrics := fixture.keeper.GetMetrics(fixture.ctx)
	require.Equal(t, uint64(10), metrics.TotalProcessed)
}

func TestImportState_RejectsInvalidGenesisBeforeMutation(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.ImportState(fixture.ctx, &types.GenesisState{
		Params: types.DefaultParams(),
		Locks:  []*types.Lock{nil},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid genesis state")
	require.Contains(t, err.Error(), "lock entry cannot be nil")

	state, exportErr := fixture.keeper.ExportState(fixture.ctx)
	require.NoError(t, exportErr)
	require.Empty(t, state.Locks)
}

func TestImportState_RejectsInvalidSettlementStatusBeforeMutation(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.ImportState(fixture.ctx, &types.GenesisState{
		Params: types.DefaultParams(),
		Settlements: []*types.SettlementRecord{{
			Id:     "settlement-invalid",
			Status: types.SettlementStatus_SETTLEMENT_STATUS_UNSPECIFIED,
		}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid genesis state")
	require.Contains(t, err.Error(), "settlement settlement-invalid has unspecified status")

	state, exportErr := fixture.keeper.ExportState(fixture.ctx)
	require.NoError(t, exportErr)
	require.Empty(t, state.Settlements)
}

func TestImportState_RebuildsFailedSettlementTimeIndex(t *testing.T) {
	fixture := setupRealKeeper(t)
	completedAt := time.Unix(1_700_000_000, 0).UTC()

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Settlements: []*types.SettlementRecord{{
			Id:          "settlement-failed-import",
			Status:      types.SettlementStatus_SETTLEMENT_STATUS_FAILED,
			CompletedAt: timestamppb.New(completedAt),
		}},
	}
	require.NoError(t, fixture.keeper.ImportState(fixture.ctx, genesis))

	require.NoError(t, fixture.keeper.PruneOldSettlements(fixture.ctx, completedAt.Add(time.Hour), 100))

	_, found := fixture.keeper.GetSettlement(fixture.ctx, "settlement-failed-import")
	require.False(t, found, "failed terminal settlements imported from genesis must be rebuilt into the time index")
}

func TestImportExportState_RoundTrip(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Import some data
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Settlements: []*types.SettlementRecord{
			{Id: "settlement-rt-1", Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING},
		},
		Disputes: []*types.DisputeRecord{
			{Id: "dispute-rt-1"},
		},
		Metrics: &types.SettlementMetrics{
			TotalProcessed: 5,
		},
	}
	require.NoError(t, fixture.keeper.ImportState(fixture.ctx, genesis))

	// Export and verify
	exported, err := fixture.keeper.ExportState(fixture.ctx)
	require.NoError(t, err)
	require.NotNil(t, exported.Params)
	require.Len(t, exported.Settlements, 1)
	require.Equal(t, "settlement-rt-1", exported.Settlements[0].Id)
	require.Len(t, exported.Disputes, 1)
	require.Equal(t, "dispute-rt-1", exported.Disputes[0].Id)
	require.Equal(t, uint64(5), exported.Metrics.TotalProcessed)
}

// --- MigrateV1ToV2 ---

func TestMigrateV1ToV2_EmptyState(t *testing.T) {
	fixture := setupRealKeeper(t)

	err := fixture.keeper.MigrateV1ToV2(fixture.ctx)
	require.NoError(t, err)
}

func TestMigrateV1ToV2_WithData(t *testing.T) {
	fixture := setupRealKeeper(t)

	// Seed some data
	require.NoError(t, fixture.keeper.SaveSettlement(fixture.ctx, &types.SettlementRecord{
		Id: "settlement-migrate-1",
	}))
	require.NoError(t, fixture.keeper.SaveDispute(fixture.ctx, &types.DisputeRecord{
		Id: "dispute-migrate-1",
	}))

	err := fixture.keeper.MigrateV1ToV2(fixture.ctx)
	require.NoError(t, err)

	// Data should still be readable after migration
	var count int
	err = fixture.keeper.IterateSettlements(fixture.ctx, func(_ *types.SettlementRecord) bool {
		count++
		return false
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
