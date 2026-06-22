//go:build cosmos && cosmos_full

package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/app"
	"github.com/LumeraProtocol/lumera/internal/testutil"
	oraclekeeper "github.com/LumeraProtocol/lumera/x/oracle/keeper"
	oracletypes "github.com/LumeraProtocol/lumera/x/oracle/types"
)

func setupOracleApp(t *testing.T) (sdk.Context, *app.App) {
	t.Helper()
	ctx, lumeraApp := testutil.SetupTestApp(t)
	return ctx, lumeraApp
}

func setOracleParams(t *testing.T, ctx sdk.Context, keeper *oraclekeeper.Keeper) *oracletypes.Params {
	t.Helper()
	params := oracletypes.DefaultParams()
	require.NoError(t, keeper.SetParams(ctx, params))
	return params
}

func mustMarshalVote(t *testing.T, vote *oracletypes.ValidatorVote) []byte {
	t.Helper()
	bz, err := oracletypes.MarshalVoteExtension(vote)
	require.NoError(t, err)
	return bz
}

func requireEvent(t *testing.T, ctx sdk.Context, eventType string) {
	t.Helper()
	for _, event := range ctx.EventManager().Events() {
		if event.Type == eventType {
			return
		}
	}
	require.Failf(t, "missing event", "expected event %q", eventType)
}

func TestOracleAggregationRealApp(t *testing.T) {
	ctx, app := setupOracleApp(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(time.Unix(1_700_000_000, 0).UTC())

	setOracleParams(t, ctx, app.OracleKeeper)

	msgServer := oraclekeeper.NewMsgServerImpl(app.OracleKeeper)
	voteHeight := ctx.BlockHeight() - 1
	rewardAddr := sdk.AccAddress([]byte("oracle_reward_addr1")).String()

	vote1 := &oracletypes.ValidatorVote{
		ValidatorAddress: rewardAddr,
		PriceFeeds:       []*oracletypes.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      voteHeight,
		Timestamp:        ctx.BlockTime(),
	}
	vote2 := &oracletypes.ValidatorVote{
		ValidatorAddress: rewardAddr,
		PriceFeeds:       []*oracletypes.PriceFeed{{AssetPair: "LAC/USD", Price: "1.60"}},
		BlockHeight:      voteHeight,
		Timestamp:        ctx.BlockTime(),
	}

	msg := &oracletypes.MsgInjectOracleVotes{
		Authority: app.OracleKeeper.GetAuthority(),
		Height:    voteHeight,
		Votes: []*oracletypes.InjectedVoteExtension{
			{ValidatorAddress: []byte{0x01}, VoteExtension: mustMarshalVote(t, vote1)},
			{ValidatorAddress: []byte{0x02}, VoteExtension: mustMarshalVote(t, vote2)},
		},
	}

	_, err := msgServer.InjectOracleVotes(ctx, msg)
	require.NoError(t, err)

	agg, err := app.OracleKeeper.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	require.Equal(t, int32(2), agg.NumValidators)
	require.Equal(t, "1.550000000000000000", agg.MedianPrice)
	requireEvent(t, ctx, oracletypes.EventTypeAggregatedPrice)

	votes, err := app.OracleKeeper.GetAllValidatorVotes(ctx)
	require.NoError(t, err)
	require.Empty(t, votes)
}
