//go:build cosmos && cosmos_full && future_migration

package keeper

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestMigrateLegacyState_ReencodeCACData(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	store := keeper.storeService.OpenKVStore(ctx)

	origin := newAccAddress()
	serving := newAccAddress()

	legacyRecord := CACRoyaltyRecord{
		OriginToolID:     "origin-tool",
		ServingToolID:    "serving-tool",
		OriginPublisher:  origin,
		ServingPublisher: serving,
		TotalAmount:      sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)),
		OriginShare:      sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 300_000)),
		ServingShare:     sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)),
		SettlementID:     "settlement-1",
		Timestamp:        time.Unix(1_000, 0).UTC(),
	}
	bz, err := json.Marshal(&legacyRecord)
	require.NoError(t, err)

	recordKey := append([]byte{}, types.CACRoyaltyPrefix...)
	recordKey = append(recordKey, []byte("legacy-record")...)
	require.NoError(t, store.Set(recordKey, bz))

	legacyStats := CACRoyaltyStats{
		ToolID:               "origin-tool",
		TotalCacheHits:       5,
		TotalRoyaltiesEarned: sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)),
		TotalRoyaltiesPaid:   sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 300_000)),
		LastUpdated:          time.Unix(2_000, 0).UTC(),
	}
	statsBz, err := json.Marshal(&legacyStats)
	require.NoError(t, err)

	statsKey := append([]byte{}, types.CACStatsPrefix...)
	statsKey = append(statsKey, []byte("origin-tool")...)
	require.NoError(t, store.Set(statsKey, statsBz))

	require.NoError(t, keeper.MigrateLegacyState(ctx))

	storedRecord, err := keeper.state.CACRoyalties.Get(ctx, "legacy-record")
	require.NoError(t, err)
	require.Equal(t, "legacy-record", storedRecord.RecordID)
	require.Equal(t, legacyRecord.OriginToolID, storedRecord.OriginToolID)
	require.Equal(t, legacyRecord.ServingShare, storedRecord.ServingShare)

	storedStats, err := keeper.state.CACStats.Get(ctx, "origin-tool")
	require.NoError(t, err)
	require.Equal(t, legacyStats.TotalCacheHits, storedStats.TotalCacheHits)
	require.Equal(t, legacyStats.TotalRoyaltiesEarned, storedStats.TotalRoyaltiesEarned)
}
