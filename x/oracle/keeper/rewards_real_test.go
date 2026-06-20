//go:build cosmos && cosmos_full

package keeper_test

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	oraclekeeper "github.com/LumeraProtocol/lumera/x/oracle/keeper"
	oracletypes "github.com/LumeraProtocol/lumera/x/oracle/types"
	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestOracleStaleVoteRejectedRealApp(t *testing.T) {
	ctx, app := setupOracleApp(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(time.Unix(1_700_000_900, 0).UTC())

	params := oracletypes.DefaultParams()
	params.MaxVoteAge = 10
	require.NoError(t, app.OracleKeeper.SetParams(ctx, params))

	msgServer := oraclekeeper.NewMsgServerImpl(app.OracleKeeper)
	voteHeight := ctx.BlockHeight() - 1

	staleTime := ctx.BlockTime().Add(-30 * time.Second)
	vote := &oracletypes.ValidatorVote{
		ValidatorAddress: "val-1",
		PriceFeeds:       []*oracletypes.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      voteHeight,
		Timestamp:        timestamppb.New(staleTime),
	}

	msg := &oracletypes.MsgInjectOracleVotes{
		Authority: app.OracleKeeper.GetAuthority(),
		Height:    voteHeight,
		Votes: []*oracletypes.InjectedVoteExtension{
			{ValidatorAddress: []byte{0x01}, VoteExtension: mustMarshalVote(t, vote)},
		},
	}

	_, err := msgServer.InjectOracleVotes(ctx, msg)
	require.Error(t, err)
	require.ErrorIs(t, err, oracletypes.ErrStaleVote)

	_, err = app.OracleKeeper.GetAggregatedPrice(ctx, "LAC/USD")
	require.Error(t, err)
}

func TestOracleRewardsDistributedRealApp(t *testing.T) {
	ctx, app := setupOracleApp(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(time.Unix(1_700_001_000, 0).UTC())

	setOracleParams(t, ctx, app.OracleKeeper)

	rewardCoin := sdk.NewCoin(oracletypes.DefaultRewardDenom, sdkmath.NewInt(oracletypes.DefaultRewardAmount))
	totalRewards := sdk.NewCoins(rewardCoin.Add(rewardCoin))

	require.NoError(t, app.BankKeeper.MintCoins(ctx, creditstypes.ModuleName, totalRewards))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, creditstypes.ModuleName, oracletypes.ModuleName, totalRewards))

	rewardAddr := sdk.AccAddress([]byte("oracle_reward_addr1"))
	require.True(t, rewardAddr.Empty() == false)

	msgServer := oraclekeeper.NewMsgServerImpl(app.OracleKeeper)
	voteHeight := ctx.BlockHeight() - 1

	vote1 := &oracletypes.ValidatorVote{
		ValidatorAddress: rewardAddr.String(),
		PriceFeeds:       []*oracletypes.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      voteHeight,
		Timestamp:        timestamppb.New(ctx.BlockTime()),
	}
	vote2 := &oracletypes.ValidatorVote{
		ValidatorAddress: rewardAddr.String(),
		PriceFeeds:       []*oracletypes.PriceFeed{{AssetPair: "LAC/USD", Price: "1.60"}},
		BlockHeight:      voteHeight,
		Timestamp:        timestamppb.New(ctx.BlockTime()),
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

	balance := app.BankKeeper.GetBalance(ctx, rewardAddr, oracletypes.DefaultRewardDenom)
	expected := rewardCoin.Amount.Mul(sdkmath.NewInt(2))
	require.True(t, balance.Amount.Equal(expected))
}
