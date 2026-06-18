//go:build cosmos && cosmos_full

package keeper

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"
)

func TestPruneOldSettlements_DanglingIndex(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	// Set block time
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	// 1. Create a dangling index entry
	// ID "dangling-1" does not exist in state.Settlements
	danglingID := "dangling-1"
	err := keeper.state.SettlementsByTime.Set(ctx, collections.Join(now, danglingID))
	require.NoError(t, err)

	// Verify it exists in index
	exists, err := keeper.state.SettlementsByTime.Has(ctx, collections.Join(now, danglingID))
	require.NoError(t, err)
	require.True(t, exists, "Index entry should exist")

	// 2. Run PruneOldSettlements
	// Prune everything older than now + 1 sec
	pruneThreshold := now.Add(1 * time.Second)
	err = keeper.PruneOldSettlements(ctx, pruneThreshold, 100)
	require.NoError(t, err)

	// 3. Verify index entry is GONE (index removal now happens before settlement lookup).
	exists, err = keeper.state.SettlementsByTime.Has(ctx, collections.Join(now, danglingID))
	require.NoError(t, err)
	require.False(t, exists, "Dangling index entry should have been pruned")
}
