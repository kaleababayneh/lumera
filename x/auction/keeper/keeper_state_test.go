package keeper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/auction/types"
)

// TestKeeper_GetSetParams tests parameter retrieval and storage.
func TestKeeper_GetSetParams(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)

	t.Run("get default params", func(t *testing.T) {
		params, err := keeper.GetParams(ctx)
		require.NoError(t, err)
		require.NotNil(t, params)
		assert.Equal(t, types.DefaultCreditDenom, params.CreditDenom)
		assert.Equal(t, uint64(30), params.DefaultAuctionTTLSeconds)
		assert.Equal(t, uint32(1024), params.MaxActiveAuctions)
		assert.Equal(t, uint32(100), params.MinBidDecrementBps)
		assert.Equal(t, uint32(5_000), params.MaxBidLatencyMs)
	})

	t.Run("set custom params", func(t *testing.T) {
		customParams := &types.Params{
			CreditDenom:              "custom",
			DefaultAuctionTTLSeconds: 60,
			MaxActiveAuctions:        500,
			MinBidDecrementBps:       200,
			MaxBidLatencyMs:          10_000,
		}
		err := keeper.SetParams(ctx, customParams)
		require.NoError(t, err)

		retrieved, err := keeper.GetParams(ctx)
		require.NoError(t, err)
		assert.Equal(t, "custom", retrieved.CreditDenom)
		assert.Equal(t, uint64(60), retrieved.DefaultAuctionTTLSeconds)
		assert.Equal(t, uint32(500), retrieved.MaxActiveAuctions)
		assert.Equal(t, uint32(200), retrieved.MinBidDecrementBps)
		assert.Equal(t, uint32(10_000), retrieved.MaxBidLatencyMs)
	})

	t.Run("set invalid params fails", func(t *testing.T) {
		invalidParams := &types.Params{
			CreditDenom:              "", // Invalid: empty
			DefaultAuctionTTLSeconds: 30,
			MaxActiveAuctions:        100,
			MinBidDecrementBps:       100,
			MaxBidLatencyMs:          5_000,
		}
		err := keeper.SetParams(ctx, invalidParams)
		require.Error(t, err)
	})

	t.Run("zero TTL is invalid", func(t *testing.T) {
		invalidParams := &types.Params{
			CreditDenom:              types.DefaultCreditDenom,
			DefaultAuctionTTLSeconds: 0, // Invalid
			MaxActiveAuctions:        100,
			MinBidDecrementBps:       100,
			MaxBidLatencyMs:          5_000,
		}
		err := keeper.SetParams(ctx, invalidParams)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TTL")
	})

	t.Run("zero max active auctions is invalid", func(t *testing.T) {
		invalidParams := &types.Params{
			CreditDenom:              types.DefaultCreditDenom,
			DefaultAuctionTTLSeconds: 30,
			MaxActiveAuctions:        0, // Invalid
			MinBidDecrementBps:       100,
			MaxBidLatencyMs:          5_000,
		}
		err := keeper.SetParams(ctx, invalidParams)
		require.Error(t, err)
	})

	t.Run("min bid decrement over 10000 is invalid", func(t *testing.T) {
		invalidParams := &types.Params{
			CreditDenom:              types.DefaultCreditDenom,
			DefaultAuctionTTLSeconds: 30,
			MaxActiveAuctions:        100,
			MinBidDecrementBps:       10_001, // Invalid: > 100%
			MaxBidLatencyMs:          5_000,
		}
		err := keeper.SetParams(ctx, invalidParams)
		require.Error(t, err)
	})

	t.Run("zero max bid latency is invalid", func(t *testing.T) {
		invalidParams := &types.Params{
			CreditDenom:              types.DefaultCreditDenom,
			DefaultAuctionTTLSeconds: 30,
			MaxActiveAuctions:        100,
			MinBidDecrementBps:       100,
			MaxBidLatencyMs:          0, // Invalid
		}
		err := keeper.SetParams(ctx, invalidParams)
		require.Error(t, err)
	})
}

// TestKeeper_ActiveAuctionCount tests the active auction counter.
func TestKeeper_ActiveAuctionCount(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)

	t.Run("initial count is zero", func(t *testing.T) {
		count, err := keeper.getActiveCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), count)
	})

	t.Run("set and get count", func(t *testing.T) {
		require.NoError(t, keeper.setActiveCount(ctx, 5))

		count, err := keeper.getActiveCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(5), count)
	})

	t.Run("count persists across operations", func(t *testing.T) {
		require.NoError(t, keeper.setActiveCount(ctx, 10))

		// Do some other operations
		params := types.DefaultParams()
		require.NoError(t, keeper.SetParams(ctx, &params))

		// Count should still be there
		count, err := keeper.getActiveCount(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(10), count)
	})
}

// TestKeeper_Logger tests the logger getter.
func TestKeeper_Logger(t *testing.T) {
	_, keeper := setupAuctionKeeper(t)

	logger := keeper.Logger()
	require.NotNil(t, logger)
}

// TestKeeper_Schema tests schema access.
func TestKeeper_Schema(t *testing.T) {
	_, keeper := setupAuctionKeeper(t)

	schema := keeper.Schema()
	require.NotNil(t, schema)
}
