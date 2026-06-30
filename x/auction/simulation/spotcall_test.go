package simulation

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	keeperpkg "github.com/LumeraProtocol/lumera/x/auction/keeper"
	"github.com/LumeraProtocol/lumera/x/auction/types"
)

func TestSpotAuctionRandomizedBids(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	ctx, keeper := setupAuctionKeeper(t)

	req := types.CreateAuctionRequest{
		Owner:        randomAddress(t),
		RequestID:    "sim-req",
		ToolID:       "sim-tool",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_500_000),
		MaxLatencyMs: 2_500,
	}
	a, err := keeper.CreateSpotAuction(ctx, req)
	require.NoError(t, err)

	bestPrice := sdkmath.NewInt(1_500_000)
	bestLatency := uint32(2_500)
	var bestBid string
	accepted := 0

	for i := 0; i < 200; i++ {
		bidder := randomAddress(t)
		price := sdkmath.NewInt(900_000 + r.Int63n(500_000))
		latency := uint32(500 + r.Intn(2_000))

		bid, err := keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
			AuctionID: a.ID,
			Bidder:    bidder,
			Price:     sdk.NewCoin(types.DefaultCreditDenom, price),
			LatencyMs: latency,
		})
		if err != nil {
			// Bids that don't improve enough on best bid are rejected per MinBidDecrementBps.
			continue
		}
		accepted++

		if price.LT(bestPrice) || (price.Equal(bestPrice) && latency < bestLatency) {
			bestPrice = price
			bestLatency = latency
			bestBid = bid.ID
		}
	}

	require.Greater(t, accepted, 0, "at least one bid should be accepted")

	finalAuction, winningBid, err := keeper.FinalizeSpotAuction(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusSettled, finalAuction.Status)
	require.NotNil(t, winningBid)
	require.Equal(t, bestBid, winningBid.ID)
	priceCoin := sdk.NewCoin(types.DefaultCreditDenom, bestPrice)
	require.True(t, winningBid.Price.Equal(priceCoin))
}

func setupAuctionKeeper(t *testing.T) (sdk.Context, *keeperpkg.Keeper) {
	ensureBech32Config()

	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)

	keeper := keeperpkg.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		authtypes.NewModuleAddress("gov").String(),
		logger,
	)

	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	sdkCtx := sdk.NewContext(cms, header, false, logger)
	params := types.DefaultParams()
	require.NoError(t, keeper.SetParams(sdkCtx, &params))
	return sdkCtx, keeper
}

func randomAddress(t *testing.T) string {
	t.Helper()
	priv := secp256k1.GenPrivKey()
	return sdk.AccAddress(priv.PubKey().Address()).String()
}

var bech32Once sync.Once

func ensureBech32Config() {
	bech32Once.Do(func() {
		cfg := sdk.GetConfig()
		cfg.SetBech32PrefixForAccount("lumera", "lumerapub")
		cfg.SetBech32PrefixForValidator("lumeravaloper", "lumeravaloperpub")
		cfg.SetBech32PrefixForConsensusNode("lumeravalcons", "lumeravalconspub")
		cfg.Seal()
	})
}
