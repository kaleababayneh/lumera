//go:build cosmos

package keeper

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// Attack-vector metamorphic tests for x/oracle vote aggregation.
//
// Complementary to vote_aggregation_byzantine_test.go (commit a26a7d8e8),
// which pins the THRESHOLD angle of BFT (|byzantine| ≤ f -> median
// preserved). This file pins the ATTACK-VECTOR angle: specific
// byzantine behaviors that could bypass the aggregation logic —
// replay, drop, and duplicate strategies.
//
// Defense surfaces covered
// ------------------------
//   1. Height-monotone replay defense (SetValidatorVote line 304):
//      vote.BlockHeight must be strictly greater than lastVoteHeights
//      for that validator. Attempts to resubmit at <= previous height
//      are rejected with ErrInvalidVoteExtension.
//
//   2. Timestamp freshness (ValidateVote):
//      - future timestamps rejected
//      - stale timestamps (> MaxVoteAge seconds old) rejected
//
//   3. Stale-vote filter at aggregation (filterStaleVotes):
//      catches stale votes that slipped past ValidateVote (e.g. via
//      direct SetValidatorVote call from module-internal code paths
//      that bypass msg_server.ValidateVote).
//
//   4. Per-vote duplicate-asset-pair rejection (ValidateVote):
//      the pre-aggregation path rejects votes with duplicate pairs.
//      The aggregation path (groupVotesByAssetWithDrops) applies the
//      same rule defensively for any vote that reaches aggregation.
//
//   5. Fresh-each-period state (ClearValidatorVotes):
//      stored votes cleared after AggregateVotes. A replay across
//      periods must fail because there is no carryover state to
//      attack.
//
// Each MR below encodes an attack variant and asserts the defense
// holds. Failures mean a byzantine validator has a new DoS or
// manipulation surface — consensus-critical to catch before ship.

const replayAssetPair = "BTC/USD"

// configureReplayKeeper sets deterministic parameters for replay tests.
// MaxVoteAge = 300s so we can construct votes on both sides of the
// freshness boundary without fighting default params.
func configureReplayKeeper(t *testing.T) (*Keeper, sdk.Context) {
	t.Helper()
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{replayAssetPair},
		MaxVoteAge:        300,
	}))
	return k, ctx
}

// TestReplayMR_HeightMonotoneReplayRejected (Vector 1) proves that
// SetValidatorVote rejects any attempt to resubmit at an equal or
// lower block height than the validator's previous accepted vote.
// Without this defense, a byzantine validator could replay its vote
// multiple times within a period to shift the apparent vote count.
func TestReplayMR_HeightMonotoneReplayRejected(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)

	// First vote at height 10 succeeds.
	v1 := &types.ValidatorVote{
		ValidatorAddress: "val-attacker",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(byzantineTestTime),
	}
	require.NoError(t, k.SetValidatorVote(ctx, v1))

	// Replay at same height -> rejected.
	err := k.SetValidatorVote(ctx, v1)
	require.Error(t, err,
		"same-height replay accepted — byzantine validator could stuff its own vote")
	require.Contains(t, err.Error(), "greater than previous",
		"expected height-monotone error, got %q", err.Error())

	// Replay at lower height -> rejected.
	v2 := proto.Clone(v1).(*types.ValidatorVote)
	v2.BlockHeight = 5
	err = k.SetValidatorVote(ctx, v2)
	require.Error(t, err,
		"lower-height replay accepted — byzantine validator could inject stale vote")
	require.Contains(t, err.Error(), "greater than previous")

	// Replay at strictly higher height -> accepted (legitimate new vote).
	v3 := proto.Clone(v1).(*types.ValidatorVote)
	v3.BlockHeight = 11
	require.NoError(t, k.SetValidatorVote(ctx, v3),
		"strictly-higher height should be accepted as a fresh vote")
}

// TestReplayMR_HeightMonotoneAcrossValidators (Vector 1 scope-check)
// proves that height-monotone is PER-VALIDATOR, not global. Validator
// A's vote at height 10 must not block validator B from voting at
// height 10 — the replay-defense key is (validator, height), not
// just height.
func TestReplayMR_HeightMonotoneAcrossValidators(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)

	v1 := &types.ValidatorVote{
		ValidatorAddress: "val-A",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(byzantineTestTime),
	}
	require.NoError(t, k.SetValidatorVote(ctx, v1))

	// Validator B votes at the same height — must be accepted.
	v2 := &types.ValidatorVote{
		ValidatorAddress: "val-B",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(byzantineTestTime),
	}
	require.NoError(t, k.SetValidatorVote(ctx, v2),
		"cross-validator same-height vote rejected — replay defense "+
			"is global instead of per-validator")
}

// TestReplayMR_PostAggregationReplayRejected (Vector 1 + 5) proves
// that after AggregateVotes (which calls ClearValidatorVotes), a
// byzantine replay of the SAME vote at the SAME height is still
// rejected. The lastVoteHeights entry for that validator survives
// the vote wipe, so a period-rollover attack that reuses a previous
// period's vote fails.
func TestReplayMR_PostAggregationReplayRejected(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)

	// Round 1: vote at height 10, aggregate.
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-attacker",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(byzantineTestTime),
	}))
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-honest",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(byzantineTestTime),
	}))
	require.NoError(t, k.AggregateVotes(ctx))

	// Round 2: attacker replays the EXACT same vote (same height).
	// Must be rejected by the persisted lastVoteHeights check.
	err := k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-attacker",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(byzantineTestTime),
	})
	require.Error(t, err,
		"cross-period replay of same-height vote accepted — ClearValidatorVotes "+
			"must not wipe the lastVoteHeights tracking")
	require.Contains(t, err.Error(), "greater than previous")

	// A strictly newer height still works.
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-attacker",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      11,
		Timestamp:        timestamppb.New(byzantineTestTime),
	}),
		"legitimate higher-height vote rejected after period rollover")
}

// TestReplayMR_StaleTimestampFilteredAtAggregation (Vector 3) proves
// that votes with timestamps older than MaxVoteAge get filtered at
// aggregation time even if SetValidatorVote accepted them. This is
// the defense-in-depth check: any code path that stores a vote
// without going through msg_server.ValidateVote must still see stale
// votes drop at aggregation.
func TestReplayMR_StaleTimestampFilteredAtAggregation(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)
	sdkCtx := ctx
	now := sdkCtx.BlockTime()

	// Fresh vote from honest validator — inside MaxVoteAge window.
	require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
		ValidatorAddress: "val-fresh",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(now.Add(-30 * time.Second)),
	}))

	// Byzantine vote with timestamp ancient — past MaxVoteAge (300s).
	require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
		ValidatorAddress: "val-replay-old",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "999999999"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(now.Add(-1000 * time.Second)),
	}))

	// Byzantine vote with future timestamp — would pass MaxVoteAge but
	// gets dropped because ageDur < 0 in filterStaleVotes.
	require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
		ValidatorAddress: "val-replay-future",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "999999999"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(now.Add(1000 * time.Second)),
	}))

	require.NoError(t, k.AggregateVotes(sdkCtx))
	agg, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
	require.NoError(t, err)

	// Only the fresh honest vote should have counted.
	require.Equal(t, int32(1), agg.NumValidators,
		"stale/future vote not filtered at aggregation: got %d validators, expected 1",
		agg.NumValidators)
	require.Equal(t, "100.000000000000000000", agg.MedianPrice,
		"byzantine replay leaked into median despite being stale/future")
}

// TestReplayMR_BoundaryAgeVotesHandledCleanly (Vector 3 edge case)
// probes the exact MaxVoteAge boundary. Votes strictly within the
// window must be accepted; votes past it must be dropped. Off-by-one
// bugs here would let byzantine validators replay one-tick-old votes
// forever.
func TestReplayMR_BoundaryAgeVotesHandledCleanly(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)
	sdkCtx := ctx
	now := sdkCtx.BlockTime()

	// Vote exactly at the boundary (300s ago) — inclusive boundary,
	// should be accepted (filterStaleVotes uses <= maxAge).
	require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
		ValidatorAddress: "val-boundary",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(now.Add(-300 * time.Second)),
	}))

	// Vote past the boundary — rejected by filterStaleVotes.
	require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
		ValidatorAddress: "val-past-boundary",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "999"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(now.Add(-301 * time.Second)),
	}))

	require.NoError(t, k.AggregateVotes(sdkCtx))
	agg, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
	require.NoError(t, err)

	require.Equal(t, int32(1), agg.NumValidators,
		"boundary-age handling off: expected 1 validator (the boundary-inclusive vote)")
	require.Equal(t, "100.000000000000000000", agg.MedianPrice,
		"past-boundary byzantine vote leaked into median")
}

// TestReplayMR_AllVotesStaleNoAggregation (Vector 3 extreme) proves
// that when every vote fails the staleness check, AggregateVotes
// completes without error but produces no aggregated price for the
// period. Without this defense, a byzantine network partition could
// replay a period of stale votes to keep a stale price on-chain.
func TestReplayMR_AllVotesStaleNoAggregation(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)
	sdkCtx := ctx
	now := sdkCtx.BlockTime()

	// All votes have ancient timestamps.
	for i := 0; i < 5; i++ {
		require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
			ValidatorAddress: fmt.Sprintf("val-%d", i),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
			BlockHeight:      int64(10 + i),
			Timestamp:        timestamppb.New(now.Add(-1000 * time.Second)),
		}))
	}

	// Aggregate should NOT fail — just silently produce no price.
	require.NoError(t, k.AggregateVotes(sdkCtx),
		"AggregateVotes errored on all-stale input — byzantine partition "+
			"would halt aggregation pipeline")

	// No aggregated price should exist for this pair.
	_, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
	require.Error(t, err,
		"all-stale period produced an aggregated price — byzantine "+
			"replay succeeded in recording stale median")
}

// TestReplayMR_DuplicateAssetPairValidateVoteRejects (Vector 4 —
// pre-aggregation path) proves ValidateVote rejects a vote containing
// duplicate asset pairs. Without this, a byzantine validator could
// stuff multiple price feeds for the same pair in a single vote and
// inflate its weight in aggregation.
func TestReplayMR_DuplicateAssetPairValidateVoteRejects(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)
	sdkCtx := ctx

	vote := &types.ValidatorVote{
		ValidatorAddress: "val-attacker",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: replayAssetPair, Price: "100"},
			{AssetPair: replayAssetPair, Price: "200"}, // byzantine: same pair
		},
		BlockHeight: 10,
		Timestamp:   timestamppb.New(sdkCtx.BlockTime()),
	}

	err := k.ValidateVote(sdkCtx, vote)
	require.Error(t, err,
		"ValidateVote accepted byzantine vote with duplicate asset pair")
	require.Contains(t, err.Error(), "duplicate asset pair")
}

// TestReplayMR_DuplicateAssetPairAggregationDropsBoth (Vector 4 —
// defense-in-depth at aggregation) proves that if a vote with
// duplicate asset pairs slips past ValidateVote (e.g. via a direct
// SetValidatorVote call), aggregation still drops ALL feeds for the
// duplicated pair. Complements the byzantine MR4 with an explicit
// per-attack-vector framing.
func TestReplayMR_DuplicateAssetPairAggregationDropsBoth(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)
	sdkCtx := ctx

	// Byzantine validator bypasses msg_server and writes a duplicate-
	// pair vote directly. Aggregation must reject.
	require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
		ValidatorAddress: "val-attacker",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: replayAssetPair, Price: "1"},
			{AssetPair: replayAssetPair, Price: "999999999"},
		},
		BlockHeight: 10,
		Timestamp:   timestamppb.New(sdkCtx.BlockTime()),
	}))
	// Honest validators vote normally.
	require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
		ValidatorAddress: "val-honest-1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(sdkCtx.BlockTime()),
	}))
	require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
		ValidatorAddress: "val-honest-2",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "102"}},
		BlockHeight:      10,
		Timestamp:        timestamppb.New(sdkCtx.BlockTime()),
	}))

	require.NoError(t, k.AggregateVotes(sdkCtx))
	agg, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
	require.NoError(t, err)

	// The attacker's duplicate-pair feeds must both drop.
	require.Equal(t, int32(2), agg.NumValidators,
		"byzantine duplicate-pair feeds not dropped at aggregation: "+
			"got %d validators, expected 2 honest", agg.NumValidators)
	require.Equal(t, "101.000000000000000000", agg.MedianPrice,
		"byzantine duplicate-pair vote polluted median")
}

// TestReplayMR_DropSingleHonestNoDOSWhenQuorum (Vector 5 — drop
// attack) proves that silencing one honest validator does not prevent
// aggregation when the remainder still has a vote for the asset pair.
// Network-level DoS against a single validator must not halt price
// updates.
func TestReplayMR_DropSingleHonestNoDOSWhenQuorum(t *testing.T) {
	t.Parallel()

	// Reference run: all 5 honest.
	refMedian := func() string {
		k, ctx := configureReplayKeeper(t)
		sdkCtx := ctx
		for i := 0; i < 5; i++ {
			require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
				ValidatorAddress: fmt.Sprintf("val-%d", i),
				PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
				BlockHeight:      int64(10 + i),
				Timestamp:        timestamppb.New(sdkCtx.BlockTime()),
			}))
		}
		require.NoError(t, k.AggregateVotes(sdkCtx))
		agg, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
		require.NoError(t, err)
		return agg.MedianPrice
	}()

	// Drop run: 4 of 5 vote.
	dropMedian := func() string {
		k, ctx := configureReplayKeeper(t)
		sdkCtx := ctx
		for i := 0; i < 4; i++ {
			require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
				ValidatorAddress: fmt.Sprintf("val-%d", i),
				PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
				BlockHeight:      int64(10 + i),
				Timestamp:        timestamppb.New(sdkCtx.BlockTime()),
			}))
		}
		require.NoError(t, k.AggregateVotes(sdkCtx))
		agg, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
		require.NoError(t, err,
			"single-validator drop caused aggregation failure — DoS on one "+
				"validator should not halt price updates")
		return agg.MedianPrice
	}()

	require.Equal(t, refMedian, dropMedian,
		"dropping a single unanimous validator changed the median: "+
			"ref=%s drop=%s", refMedian, dropMedian)
}

// TestReplayMR_DropAllThenRecover (Vector 5 — total-drop recovery)
// proves that a period of zero votes does not corrupt subsequent
// periods. AggregateVotes must handle the empty case cleanly, and
// the next period with fresh votes must aggregate normally.
func TestReplayMR_DropAllThenRecover(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)
	sdkCtx := ctx

	// Period 1: no votes.
	require.NoError(t, k.AggregateVotes(sdkCtx),
		"AggregateVotes errored on empty vote set — byzantine DoS that "+
			"suppresses all votes would halt the aggregation pipeline")
	_, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
	require.Error(t, err, "empty-period should not produce an aggregated price")

	// Period 2: normal votes.
	for i := 0; i < 3; i++ {
		require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
			ValidatorAddress: fmt.Sprintf("val-%d", i),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
			BlockHeight:      int64(10 + i),
			Timestamp:        timestamppb.New(sdkCtx.BlockTime()),
		}))
	}
	require.NoError(t, k.AggregateVotes(sdkCtx))

	agg, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
	require.NoError(t, err,
		"aggregation stuck after empty period — byzantine period-skip "+
			"attack leaves the pipeline in an unrecoverable state")
	require.Equal(t, int32(3), agg.NumValidators)
	require.Equal(t, "100.000000000000000000", agg.MedianPrice)
}

// TestReplayMR_IdenticalVotesDifferentValidatorsLegit (Vector 3 + 4
// negative case) proves that the same exact vote content submitted
// by DIFFERENT validators is NOT treated as replay. This is honest
// agreement, not an attack. A defense-mechanism bug that flags
// consensus-agreement as replay would break BFT liveness.
func TestReplayMR_IdenticalVotesDifferentValidatorsLegit(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)
	sdkCtx := ctx

	// 5 validators submit byte-identical vote content (except address).
	// All 5 must be accepted.
	for i := 0; i < 5; i++ {
		require.NoError(t, k.SetValidatorVote(sdkCtx, &types.ValidatorVote{
			ValidatorAddress: fmt.Sprintf("val-%d", i),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
			BlockHeight:      10,
			Timestamp:        timestamppb.New(sdkCtx.BlockTime()),
		}),
			"identical-content vote from validator %d rejected — consensus "+
				"agreement mis-flagged as replay", i)
	}

	require.NoError(t, k.AggregateVotes(sdkCtx))
	agg, err := k.GetAggregatedPrice(sdkCtx, replayAssetPair)
	require.NoError(t, err)
	require.Equal(t, int32(5), agg.NumValidators,
		"identical honest votes dropped — expected 5 validators counted")
}

// TestReplayMR_MissingTimestampRejected (Vector 2 edge case) probes
// the ValidateVote path for a nil timestamp. Byzantine validators
// omitting the timestamp field attempt to bypass the freshness check.
// ValidateVote must reject.
func TestReplayMR_MissingTimestampRejected(t *testing.T) {
	t.Parallel()

	k, ctx := configureReplayKeeper(t)
	sdkCtx := ctx

	vote := &types.ValidatorVote{
		ValidatorAddress: "val-attacker",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: replayAssetPair, Price: "100"}},
		BlockHeight:      10,
		Timestamp:        nil, // missing
	}

	err := k.ValidateVote(sdkCtx, vote)
	require.Error(t, err,
		"ValidateVote accepted byzantine vote with nil timestamp — "+
			"attacker could bypass the freshness check by omitting the field")
	require.Contains(t, err.Error(), "timestamp")
}

