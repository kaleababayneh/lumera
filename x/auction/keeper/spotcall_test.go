package keeper

import (
	"testing"
	"time"

	"sync"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/auction/types"
	prioritykeeper "github.com/LumeraProtocol/lumera/x/priority/keeper"
	prioritytypes "github.com/LumeraProtocol/lumera/x/priority/types"
	reservekeeper "github.com/LumeraProtocol/lumera/x/reserve/keeper"
	reservetypes "github.com/LumeraProtocol/lumera/x/reserve/types"
)

func TestSpotAuctionFlow(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	req := types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-1",
		ToolID:       "tool-alpha",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	}

	auction, err := k.CreateSpotAuction(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, auction)
	require.Equal(t, types.AuctionStatusActive, auction.Status)

	active, err := k.state.ActiveAuctions.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), active)

	a1 := newAccountAddr(t)
	a2 := newAccountAddr(t)
	a3 := newAccountAddr(t)

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    a1,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		LatencyMs: 1_500,
	})
	require.NoError(t, err)

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    a2,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000),
		LatencyMs: 1_900,
	})
	require.NoError(t, err)

	bid3, err := k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    a3,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000),
		LatencyMs: 1_600,
	})
	require.NoError(t, err)

	auctionRecord, err := k.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, bid3.ID, auctionRecord.BestBidID)
	active, err = k.state.ActiveAuctions.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), active)

	finalAuction, winningBid, err := k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusSettled, finalAuction.Status)
	require.NotNil(t, winningBid)
	require.Equal(t, bid3.ID, winningBid.ID)

	count, err := k.state.ActiveAuctions.Get(ctx)
	if err == nil {
		require.Equal(t, uint64(0), count)
	}
}

func TestSpotAuctionValidation(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	req := types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-duplicate",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 1_000,
	}
	_, err := k.CreateSpotAuction(ctx, req)
	require.NoError(t, err)

	_, err = k.CreateSpotAuction(ctx, req)
	require.ErrorIs(t, err, types.ErrAuctionExists)

	bidder := newAccountAddr(t)
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: "nonexistent",
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100),
		LatencyMs: 100,
	})
	require.ErrorIs(t, err, types.ErrAuctionNotFound)

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: "auc-1",
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin("unknown", 100),
		LatencyMs: 100,
	})
	require.Error(t, err)
}

func TestFinalizeWithoutBidsExpires(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	req := types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-empty",
		ToolID:       "tool-empty",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 250_000),
		MaxLatencyMs: 800,
	}
	auction, err := k.CreateSpotAuction(ctx, req)
	if err != nil {
		t.Fatalf("create error: %v", err)
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	finalAuction, winningBid, err := k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.Nil(t, winningBid)
	require.Equal(t, types.AuctionStatusExpired, finalAuction.Status)
}

func TestReserveAppliedWhenNoBids(t *testing.T) {
	ctx, auctionKeeper, reserveKeeper := setupAuctionKeeperWithReserve(t)
	owner := newAccountAddr(t)

	_, err := reserveKeeper.CreateCommitment(ctx, reservetypes.ReserveRequest{
		Owner:    owner,
		PolicyID: "policy-reserve",
		ToolID:   "tool-reserve",
		Tier:     "silver",
		Amount:   sdk.NewInt64Coin(reservetypes.DefaultCreditDenom, 700_000),
		Duration: time.Hour,
	})
	require.NoError(t, err)

	req := types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-reserve",
		ToolID:       "tool-reserve",
		PolicyID:     "policy-reserve",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 300_000),
		MaxLatencyMs: 2_000,
	}
	auction, err := auctionKeeper.CreateSpotAuction(ctx, req)
	require.NoError(t, err)

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	finalAuction, winningBid, err := auctionKeeper.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.Nil(t, winningBid)
	require.True(t, finalAuction.ReserveApplied)
	require.NotEmpty(t, finalAuction.ReserveCommitmentID)
	require.True(t, finalAuction.BestBidPrice.Amount.LT(req.MaxPrice.Amount))
}

func TestPriorityAdjustmentsApplied(t *testing.T) {
	ctx, auctionKeeper, priorityKeeper := setupAuctionKeeperWithPriority(t)

	// Register the "mission-critical" tier in priority params before assignment.
	params, err := priorityKeeper.GetParams(ctx)
	require.NoError(t, err)
	params.Tiers = append(params.Tiers, prioritytypes.Tier{
		Name:                 "mission-critical",
		MaxLatencyMs:         1_000,
		AuctionTTLMs:         uint64((10 * time.Second) / time.Millisecond),
		SpotDiscountBps:      250,
		QueueWeight:          800,
		PricingMultiplierBps: 300,
		ReservedCapacityBps:  0,
		Description:          "Mission-critical tier for test",
	})
	require.NoError(t, priorityKeeper.SetParams(ctx, params))

	require.NoError(t, priorityKeeper.AssignPolicyTier(ctx, "policy-priority", "mission-critical", time.Hour))

	owner := newAccountAddr(t)
	req := types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-priority",
		ToolID:       "tool-priority",
		PolicyID:     "policy-priority",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000),
		MaxLatencyMs: 2_500,
	}
	auction, err := auctionKeeper.CreateSpotAuction(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "mission-critical", auction.PriorityTier)
	require.Equal(t, uint32(1_000), auction.MaxLatencyMs)
	require.Equal(t, uint32(250), auction.PriorityDiscountBps)
}

func setupAuctionKeeper(t *testing.T) (sdk.Context, *Keeper) {
	ctx, keeper, _, _ := setupAuctionKeeperBase(t, false, false)
	return ctx, keeper
}

func setupAuctionKeeperWithReserve(t *testing.T) (sdk.Context, *Keeper, *reservekeeper.Keeper) {
	ctx, keeper, reserveKeeper, _ := setupAuctionKeeperBase(t, true, false)
	return ctx, keeper, reserveKeeper
}

func setupAuctionKeeperWithPriority(t *testing.T) (sdk.Context, *Keeper, *prioritykeeper.Keeper) {
	ctx, keeper, _, priorityKeeper := setupAuctionKeeperBase(t, false, true)
	return ctx, keeper, priorityKeeper
}

func setupAuctionKeeperBase(t *testing.T, withReserve, withPriority bool) (sdk.Context, *Keeper, *reservekeeper.Keeper, *prioritykeeper.Keeper) {
	ensureBech32Config()

	t.Helper()

	auctionKey := storetypes.NewKVStoreKey(types.StoreKey)
	memKey := storetypes.NewMemoryStoreKey("auction-mem")

	var reserveKey *storetypes.KVStoreKey
	if withReserve {
		reserveKey = storetypes.NewKVStoreKey(reservetypes.StoreKey)
	}
	var priorityKey *storetypes.KVStoreKey
	if withPriority {
		priorityKey = storetypes.NewKVStoreKey(prioritytypes.StoreKey)
	}

	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(auctionKey, storetypes.StoreTypeIAVL, db)
	cms.MountStoreWithDB(memKey, storetypes.StoreTypeMemory, nil)
	if withReserve {
		cms.MountStoreWithDB(reserveKey, storetypes.StoreTypeIAVL, db)
	}
	if withPriority {
		cms.MountStoreWithDB(priorityKey, storetypes.StoreTypeIAVL, db)
	}
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)

	auctionKeeper := NewKeeper(
		cdc,
		runtime.NewKVStoreService(auctionKey),
		authtypes.NewModuleAddress("gov").String(),
		logger,
	)

	var reserveKeeper *reservekeeper.Keeper
	if withReserve {
		reserveKeeper = reservekeeper.NewKeeper(cdc, runtime.NewKVStoreService(reserveKey), authtypes.NewModuleAddress("gov").String(), logger)
	}

	var priorityKeeper *prioritykeeper.Keeper
	if withPriority {
		priorityKeeper = prioritykeeper.NewKeeper(cdc, runtime.NewKVStoreService(priorityKey), authtypes.NewModuleAddress("gov").String(), logger)
	}

	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	sdkCtx := sdk.NewContext(cms, header, false, logger)

	auctionParams := types.DefaultParams()
	require.NoError(t, auctionKeeper.SetParams(sdkCtx, &auctionParams))

	if reserveKeeper != nil {
		reserveParams := reservetypes.DefaultParams()
		require.NoError(t, reserveKeeper.SetParams(sdkCtx, reserveParams))
		auctionKeeper.SetReserveKeeper(reserveKeeper)
	}

	if priorityKeeper != nil {
		priorityParams := prioritytypes.DefaultParams()
		require.NoError(t, priorityKeeper.SetParams(sdkCtx, priorityParams))
		auctionKeeper.SetPriorityKeeper(priorityKeeper)
	}

	return sdkCtx, auctionKeeper, reserveKeeper, priorityKeeper
}

func newAccountAddr(t *testing.T) string {
	ensureBech32Config()

	t.Helper()
	privKey := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(privKey.PubKey().Address())
	return addr.String()
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
