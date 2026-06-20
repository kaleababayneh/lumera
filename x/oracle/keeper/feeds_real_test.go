//go:build cosmos && cosmos_full

package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	oracletypes "github.com/LumeraProtocol/lumera/x/oracle/types"
)

func TestOraclePriceFeedsRealApp(t *testing.T) {
	ctx, app := setupOracleApp(t)
	ctx = ctx.WithBlockHeight(5).WithBlockTime(time.Unix(1_700_000_500, 0).UTC())

	setOracleParams(t, ctx, app.OracleKeeper)

	feed := &oracletypes.PriceFeed{
		AssetPair: "LAC/USD",
		Price:     "1.50",
		Timestamp: timestamppb.New(ctx.BlockTime()),
	}
	require.NoError(t, app.OracleKeeper.SetPriceFeed(ctx, feed))

	got, err := app.OracleKeeper.GetPriceFeed(ctx, "LAC/USD")
	require.NoError(t, err)
	require.Equal(t, "LAC/USD", got.AssetPair)
	require.Equal(t, "1.500000000000000000", got.Price)

	require.NoError(t, app.OracleKeeper.SetPriceFeed(ctx, &oracletypes.PriceFeed{
		AssetPair: "ETH/USD",
		Price:     "3500.25",
		Timestamp: timestamppb.New(ctx.BlockTime()),
	}))
	feeds, err := app.OracleKeeper.GetAllPriceFeeds(ctx)
	require.NoError(t, err)
	require.Len(t, feeds, 2)
}
