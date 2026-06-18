//go:build cosmos && cosmos_full

package keeper

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestIteratePendingSettlementsDeterministic(t *testing.T) {
	ctx, k, _, _, _ := setupCreditsKeeper(t)

	now := ctx.BlockTime()
	createSettlement := func(id string) *types.SettlementRecord {
		record := &types.SettlementRecord{
			Id:        id,
			Status:    types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
			Timestamp: timestamppb.New(now.Add(-48 * time.Hour)),
		}
		record.SetTotalCostCoins(sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 100)))
		return record
	}

	ids := []string{"settlement-c", "settlement-a", "settlement-b", "settlement-d"}
	for _, id := range ids {
		record := createSettlement(id)
		require.NoError(t, k.CreateSettlement(ctx, record))
	}

	collect := func() []string {
		var seen []string
		require.NoError(t, k.IteratePendingSettlements(ctx, len(ids), func(record *types.SettlementRecord) (bool, bool, error) {
			seen = append(seen, record.Id)
			return false, false, nil
		}))
		return seen
	}

	first := collect()
	require.Equal(t, []string{"settlement-a", "settlement-b", "settlement-c", "settlement-d"}, first)
	second := collect()
	require.Equal(t, first, second)
}
