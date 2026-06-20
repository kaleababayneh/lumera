//go:build cosmos && cosmos_full

package keeper

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

func TestInjectOracleVotes_SuccessAggregates(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(testTime)

	msgServer := NewMsgServerImpl(k)
	voteHeight := ctx.BlockHeight() - 1
	rewardAddr := sdk.AccAddress([]byte("oracle_reward_addr1")).String()

	vote1 := &types.ValidatorVote{
		ValidatorAddress: rewardAddr,
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      voteHeight,
		Timestamp:        timestamppb.New(testTime),
	}
	vote2 := &types.ValidatorVote{
		ValidatorAddress: rewardAddr,
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.60"}},
		BlockHeight:      voteHeight,
		Timestamp:        timestamppb.New(testTime),
	}

	bz1, err := types.MarshalVoteExtension(vote1)
	require.NoError(t, err)
	bz2, err := types.MarshalVoteExtension(vote2)
	require.NoError(t, err)

	msg := &types.MsgInjectOracleVotes{
		Authority: k.GetAuthority(),
		Height:    voteHeight,
		Votes: []*types.InjectedVoteExtension{
			{ValidatorAddress: []byte{0x01}, VoteExtension: bz1},
			{ValidatorAddress: []byte{0x02}, VoteExtension: bz2},
		},
	}

	_, err = msgServer.InjectOracleVotes(ctx, msg)
	require.NoError(t, err)

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	require.Equal(t, int32(2), agg.NumValidators)
	require.Equal(t, "1.550000000000000000", agg.MedianPrice)

	votes, err := k.GetAllValidatorVotes(ctx)
	require.NoError(t, err)
	require.Empty(t, votes)
}

func TestInjectOracleVotes_UnsortedAddresses(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(testTime)

	msgServer := NewMsgServerImpl(k)
	voteHeight := ctx.BlockHeight() - 1
	rewardAddr := sdk.AccAddress([]byte("oracle_reward_addr1")).String()

	vote := &types.ValidatorVote{
		ValidatorAddress: rewardAddr,
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      voteHeight,
		Timestamp:        timestamppb.New(testTime),
	}
	bz, err := types.MarshalVoteExtension(vote)
	require.NoError(t, err)

	msg := &types.MsgInjectOracleVotes{
		Authority: k.GetAuthority(),
		Height:    voteHeight,
		Votes: []*types.InjectedVoteExtension{
			{ValidatorAddress: []byte{0x02}, VoteExtension: bz},
			{ValidatorAddress: []byte{0x01}, VoteExtension: bz},
		},
	}

	_, err = msgServer.InjectOracleVotes(ctx, msg)
	require.Error(t, err)
}

func TestInjectOracleVotes_InvalidHeight(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(testTime)

	msgServer := NewMsgServerImpl(k)
	voteHeight := ctx.BlockHeight() - 1
	rewardAddr := sdk.AccAddress([]byte("oracle_reward_addr1")).String()

	vote := &types.ValidatorVote{
		ValidatorAddress: rewardAddr,
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      voteHeight,
		Timestamp:        timestamppb.New(testTime),
	}
	bz, err := types.MarshalVoteExtension(vote)
	require.NoError(t, err)

	msg := &types.MsgInjectOracleVotes{
		Authority: k.GetAuthority(),
		Height:    voteHeight - 1,
		Votes: []*types.InjectedVoteExtension{
			{ValidatorAddress: []byte{0x01}, VoteExtension: bz},
		},
	}

	_, err = msgServer.InjectOracleVotes(ctx, msg)
	require.Error(t, err)
}

func TestInjectOracleVotes_InvalidAuthority(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(testTime)

	msgServer := NewMsgServerImpl(k)
	wrongAuthority := sdk.AccAddress([]byte("wrong_oracle_auth___")).String()

	msg := &types.MsgInjectOracleVotes{
		Authority: wrongAuthority,
		Height:    ctx.BlockHeight() - 1,
	}

	_, err := msgServer.InjectOracleVotes(ctx, msg)
	require.Error(t, err)
}

func TestInjectOracleVotes_RejectsPaddedAuthority(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(testTime)

	msgServer := NewMsgServerImpl(k)
	msg := &types.MsgInjectOracleVotes{
		Authority: " " + k.GetAuthority() + " ",
		Height:    ctx.BlockHeight() - 1,
	}

	_, err := msgServer.InjectOracleVotes(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority address")
}

func TestInjectOracleVotes_RejectsOversizedVotesSlice(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(testTime)

	votes := make([]*types.InjectedVoteExtension, types.MaxInjectedVotesPerMsg+1)
	msgServer := NewMsgServerImpl(k)
	msg := &types.MsgInjectOracleVotes{
		Authority: k.GetAuthority(),
		Height:    ctx.BlockHeight() - 1,
		Votes:     votes,
	}

	_, err := msgServer.InjectOracleVotes(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "votes exceeds")
}

func TestInjectOracleVotes_ValidatesBeforeKeeperAccess(t *testing.T) {
	_, k := setupOracleKeeper(t)

	invalidMsg := &types.MsgInjectOracleVotes{
		Authority: "",
		Height:    1,
	}
	_, err := (&msgServer{}).InjectOracleVotes(context.Background(), invalidMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "authority is required")

	var nilServer *msgServer
	_, err = nilServer.InjectOracleVotes(context.Background(), invalidMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "authority is required")

	validMsg := &types.MsgInjectOracleVotes{
		Authority: k.GetAuthority(),
		Height:    1,
	}
	_, err = (&msgServer{}).InjectOracleVotes(context.Background(), validMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "oracle keeper not initialized")
}

func TestInjectOracleVotes_StaleVoteRejected(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(testTime)

	params := types.DefaultParams()
	params.MaxVoteAge = 10
	require.NoError(t, k.SetParams(ctx, params))

	msgServer := NewMsgServerImpl(k)
	voteHeight := ctx.BlockHeight() - 1
	rewardAddr := sdk.AccAddress([]byte("oracle_reward_addr1")).String()

	staleTime := testTime.Add(-30 * time.Second)
	vote := &types.ValidatorVote{
		ValidatorAddress: rewardAddr,
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      voteHeight,
		Timestamp:        timestamppb.New(staleTime),
	}
	bz, err := types.MarshalVoteExtension(vote)
	require.NoError(t, err)

	msg := &types.MsgInjectOracleVotes{
		Authority: k.GetAuthority(),
		Height:    voteHeight,
		Votes: []*types.InjectedVoteExtension{
			{ValidatorAddress: []byte{0x01}, VoteExtension: bz},
		},
	}

	_, err = msgServer.InjectOracleVotes(ctx, msg)
	require.Error(t, err)
}

func TestInjectOracleVotes_InvalidVoteExtension(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ctx = ctx.WithBlockHeight(10).WithBlockTime(testTime)

	msgServer := NewMsgServerImpl(k)
	voteHeight := ctx.BlockHeight() - 1

	msg := &types.MsgInjectOracleVotes{
		Authority: k.GetAuthority(),
		Height:    voteHeight,
		Votes: []*types.InjectedVoteExtension{
			{ValidatorAddress: []byte{0x01}, VoteExtension: []byte{0xFF}},
		},
	}

	_, err := msgServer.InjectOracleVotes(ctx, msg)
	require.Error(t, err)
}
