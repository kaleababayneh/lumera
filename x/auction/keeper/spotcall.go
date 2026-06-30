package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/auction/types"
)

// CreateSpotAuction opens a new SpotCall auction for a router request.
func (k *Keeper) CreateSpotAuction(ctx context.Context, req types.CreateAuctionRequest) (*types.SpotAuction, error) {
	if err := validateCreateRequestShape(req); err != nil {
		return nil, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, fmt.Errorf("load params: %w", err)
	}
	if err := validateCreateRequest(*params, req); err != nil {
		return nil, err
	}

	priorityTier := ""
	priorityDiscount := uint32(0)
	auctionTTL := params.AuctionTTL()
	maxLatency := req.MaxLatencyMs
	if k.priorityKeeper != nil {
		if adj, err := k.priorityKeeper.ResolveAdjustments(ctx, req.PolicyID, req.MaxLatencyMs, auctionTTL); err == nil && adj.Applied {
			priorityTier = adj.TierName
			if adj.MaxLatencyMs > 0 && (maxLatency == 0 || adj.MaxLatencyMs < maxLatency) {
				maxLatency = adj.MaxLatencyMs
			}
			if adj.AuctionTTL > 0 {
				auctionTTL = adj.AuctionTTL
			}
			priorityDiscount = adj.SpotDiscountBps
		}
	}

	if exists, err := k.state.AuctionByRequest.Has(ctx, req.RequestID); err != nil {
		return nil, fmt.Errorf("check existing auction: %w", err)
	} else if exists {
		return nil, types.ErrAuctionExists
	}

	activeCount, err := k.getActiveCount(ctx)
	if err != nil {
		return nil, err
	}
	if activeCount >= uint64(params.MaxActiveAuctions) {
		return nil, fmt.Errorf("active auction limit reached: %d", params.MaxActiveAuctions)
	}

	seq, err := k.state.AuctionSeq.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("allocate auction id: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return nil, fmt.Errorf("auction: block time must be set")
	}

	if auctionTTL <= 0 {
		auctionTTL = 30 * time.Second
	}
	expires := now.Add(auctionTTL)

	auction := &types.SpotAuction{
		ID:                  fmt.Sprintf("auc-%d", seq),
		Owner:               req.Owner,
		RequestID:           req.RequestID,
		ToolID:              req.ToolID,
		PolicyID:            req.PolicyID,
		MaxPrice:            req.MaxPrice,
		MaxLatencyMs:        maxLatency,
		CreatedAt:           now,
		ExpiresAt:           expires,
		Status:              types.AuctionStatusActive,
		PriorityTier:        priorityTier,
		PriorityDiscountBps: priorityDiscount,
		BestBidID:           "",
		BestBidPrice:        sdk.NewCoin(params.CreditDenom, sdkmath.ZeroInt()),
		BestBidLatencyMs:    0,
		ReserveCommitmentID: "",
		ReserveApplied:      false,
	}

	if err := auction.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("invalid auction %s: %w (created=%s expires=%s ttl=%s)",
			auction.ID, err, auction.CreatedAt.UTC().Format(time.RFC3339Nano), auction.ExpiresAt.UTC().Format(time.RFC3339Nano), auctionTTL)
	}

	if err := k.state.Auctions.Set(ctx, auction.ID, *auction); err != nil {
		return nil, fmt.Errorf("store auction: %w", err)
	}
	if err := k.state.AuctionByRequest.Set(ctx, req.RequestID, auction.ID); err != nil {
		return nil, fmt.Errorf("index auction request: %w", err)
	}
	if err := k.state.AuctionsByExpiry.Set(ctx, collections.Join(auction.ExpiresAt, auction.ID)); err != nil {
		return nil, fmt.Errorf("index auction expiry: %w", err)
	}
	if err := k.setActiveCount(ctx, activeCount+1); err != nil {
		return nil, fmt.Errorf("update active counter: %w", err)
	}

	return auction, nil
}

// SubmitSpotBid records a validator bid for an active auction.
func (k *Keeper) SubmitSpotBid(ctx context.Context, req types.SubmitBidRequest) (*types.SpotBid, error) {
	if err := validateCreateIdentifier("auction id", req.AuctionID); err != nil {
		return nil, err
	}
	if err := validateBidRequestShape(req); err != nil {
		return nil, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, fmt.Errorf("load params: %w", err)
	}

	auction, err := k.getAuction(ctx, req.AuctionID)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return nil, fmt.Errorf("auction: block time must be set")
	}

	if err := ensureAuctionActive(*params, auction, now); err != nil {
		return nil, err
	}

	if err := validateBid(*params, auction, req); err != nil {
		return nil, err
	}

	if auction.BestBidID != "" {
		currentPrice := auction.BestBidPrice
		currentLatency := auction.BestBidLatencyMs
		decrement := params.MinBidDecrementBps

		// Calculate max allowed price for a decrement (current * (1 - decrement))
		limitPrice := applyDiscount(currentPrice, decrement)

		isCheaperByDecrement := req.Price.Amount.LTE(limitPrice.Amount) && req.Price.Amount.LT(currentPrice.Amount)
		isAtLeastAsCheap := req.Price.Amount.LTE(currentPrice.Amount)
		isFaster := req.LatencyMs < currentLatency

		// Valid if:
		// 1. Significantly cheaper (<= limitPrice AND strictly < currentPrice)
		// 2. OR At least as cheap (<= currentPrice) AND Faster
		if !isCheaperByDecrement && !(isAtLeastAsCheap && isFaster) {
			return nil, types.ErrInvalidBid.Wrapf("bid does not improve enough on best bid")
		}
	}

	bidKey := collections.Join(req.AuctionID, req.Bidder)
	if exists, err := k.state.AuctionBidByBidder.Has(ctx, bidKey); err != nil {
		return nil, fmt.Errorf("check bidder duplicate: %w", err)
	} else if exists {
		return nil, types.ErrBidDuplicate
	}

	seq, err := k.state.BidSeq.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("allocate bid id: %w", err)
	}

	bid := &types.SpotBid{
		ID:          fmt.Sprintf("bid-%d", seq),
		AuctionID:   req.AuctionID,
		Bidder:      req.Bidder,
		Price:       req.Price,
		LatencyMs:   req.LatencyMs,
		SubmittedAt: now,
	}

	if err := bid.ValidateBasic(); err != nil {
		return nil, err
	}

	if err := k.state.Bids.Set(ctx, bid.ID, *bid); err != nil {
		return nil, fmt.Errorf("store bid: %w", err)
	}
	if err := k.state.AuctionBidByBidder.Set(ctx, bidKey, bid.ID); err != nil {
		return nil, fmt.Errorf("index bidder bid: %w", err)
	}

	if err := k.updateBestBid(ctx, auction, bid); err != nil {
		return nil, err
	}

	return bid, nil
}

// FinalizeSpotAuction selects the winning bid (if any) and marks auction as settled or expired.
func (k *Keeper) FinalizeSpotAuction(ctx context.Context, auctionID string) (*types.SpotAuction, *types.SpotBid, error) {
	if err := validateCreateIdentifier("auction id", auctionID); err != nil {
		return nil, nil, err
	}
	auction, err := k.getAuction(ctx, auctionID)
	if err != nil {
		return nil, nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return nil, nil, fmt.Errorf("auction: block time must be set")
	}

	if auction.Status != types.AuctionStatusActive && auction.Status != types.AuctionStatusPending {
		var winningPtr *types.SpotBid
		if auction.WinnerBidID != "" {
			storedBid, err := k.state.Bids.Get(ctx, auction.WinnerBidID)
			if err == nil {
				localBid := storedBid
				winningPtr = &localBid
			}
		}
		return auction, winningPtr, nil
	}

	activeCount, err := k.getActiveCount(ctx)
	if err != nil {
		return nil, nil, err
	}

	var (
		winningBid    types.SpotBid
		hasWinningBid bool
	)
	if auction.BestBidID != "" {
		storedBid, err := k.state.Bids.Get(ctx, auction.BestBidID)
		if err != nil {
			return nil, nil, fmt.Errorf("load best bid: %w", err)
		}
		winningBid = storedBid
		hasWinningBid = true
	}

	var winningPtr *types.SpotBid
	if hasWinningBid {
		auction.Status = types.AuctionStatusSettled
		auction.WinnerBidID = winningBid.ID
		auction.BestBidPrice = applyDiscount(winningBid.Price, auction.PriorityDiscountBps)
		auction.BestBidLatencyMs = winningBid.LatencyMs
		auction.BestBidSubmittedAt = winningBid.SubmittedAt
		localBid := winningBid
		winningPtr = &localBid
	} else {
		if k.reserveKeeper != nil {
			allocation, err := k.reserveKeeper.AllocateReserve(ctx, auction.Owner, auction.PolicyID, auction.ToolID, auction.MaxPrice)
			if err != nil {
				k.Logger().Error("failed to allocate reserve during auction fallback", "auction_id", auction.ID, "error", err)
				auction.Status = types.AuctionStatusExpired
			} else if allocation.Applied {
				auction.Status = types.AuctionStatusSettled
				auction.ReserveApplied = true
				auction.ReserveCommitmentID = allocation.CommitmentID
				auction.BestBidID = allocation.CommitmentID
				auction.WinnerBidID = allocation.CommitmentID
				auction.BestBidPrice = applyDiscount(allocation.DiscountedPrice, auction.PriorityDiscountBps)
				auction.BestBidLatencyMs = auction.MaxLatencyMs
				auction.BestBidSubmittedAt = now
			} else {
				auction.Status = types.AuctionStatusExpired
			}
		} else {
			auction.Status = types.AuctionStatusExpired
		}
	}

	// Remove from expiry index using the original expiration time
	if err := k.state.AuctionsByExpiry.Remove(ctx, collections.Join(auction.ExpiresAt, auction.ID)); err != nil {
		return nil, nil, fmt.Errorf("remove expiry index: %w", err)
	}

	auction.ExpiresAt = now

	if err := auction.ValidateBasic(); err != nil {
		return nil, nil, err
	}

	if err := k.state.Auctions.Set(ctx, auction.ID, *auction); err != nil {
		return nil, nil, fmt.Errorf("save auction: %w", err)
	}

	if activeCount > 0 {
		if err := k.setActiveCount(ctx, activeCount-1); err != nil {
			return nil, nil, err
		}
	}

	if err := k.state.AuctionByRequest.Remove(ctx, auction.RequestID); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, nil, err
	}

	// Index for pruning
	if err := k.state.AuctionsBySettledDate.Set(ctx, collections.Join(now, auction.ID)); err != nil {
		return nil, nil, fmt.Errorf("index settled auction: %w", err)
	}

	return auction, winningPtr, nil
}

func (k *Keeper) getAuction(ctx context.Context, auctionID string) (*types.SpotAuction, error) {
	auctionVal, err := k.state.Auctions.Get(ctx, auctionID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrAuctionNotFound
		}
		return nil, fmt.Errorf("load auction: %w", err)
	}
	auction := auctionVal
	return &auction, nil
}

func validateCreateRequest(params types.Params, req types.CreateAuctionRequest) error {
	if err := validateCreateRequestShape(req); err != nil {
		return err
	}
	if req.MaxPrice.Denom != params.CreditDenom {
		return fmt.Errorf("max price denom must be %s", params.CreditDenom)
	}
	if req.MaxLatencyMs > params.MaxBidLatencyMs {
		return fmt.Errorf("max latency exceeds global cap %d", params.MaxBidLatencyMs)
	}
	return nil
}

func validateCreateRequestShape(req types.CreateAuctionRequest) error {
	if req.Owner == "" {
		return fmt.Errorf("owner required")
	}
	if _, err := sdk.AccAddressFromBech32(req.Owner); err != nil {
		return fmt.Errorf("invalid owner address: %w", err)
	}
	if err := validateCreateIdentifier("request id", req.RequestID); err != nil {
		return err
	}
	if err := validateCreateIdentifier("tool id", req.ToolID); err != nil {
		return err
	}
	if err := sdk.ValidateDenom(req.MaxPrice.Denom); err != nil {
		return fmt.Errorf("invalid denom: %w", err)
	}
	if !req.MaxPrice.IsValid() || !req.MaxPrice.Amount.IsPositive() {
		return fmt.Errorf("max price must be positive")
	}
	if req.MaxLatencyMs == 0 {
		return fmt.Errorf("max latency must be > 0")
	}
	return nil
}

func validateCreateIdentifier(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s required", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > types.MaxSpotCallIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", field, types.MaxSpotCallIDLen, len(value))
	}
	return nil
}

func ensureAuctionActive(params types.Params, auction *types.SpotAuction, now time.Time) error {
	if auction.Status != types.AuctionStatusActive {
		return types.ErrAuctionClosed
	}
	if auction.IsExpired(now) {
		return types.ErrAuctionExpired
	}
	if auction.MaxLatencyMs > params.MaxBidLatencyMs {
		return fmt.Errorf("auction max latency exceeds global cap")
	}
	return nil
}

func validateBid(params types.Params, auction *types.SpotAuction, req types.SubmitBidRequest) error {
	if err := validateBidRequestShape(req); err != nil {
		return err
	}
	if req.Price.Denom != params.CreditDenom {
		return types.ErrBidInvalidDenom
	}
	if req.Price.Amount.GT(auction.MaxPrice.Amount) {
		return types.ErrBidTooExpensive
	}
	if req.LatencyMs > auction.MaxLatencyMs {
		return types.ErrBidLatencyExceeded
	}
	return nil
}

func validateBidRequestShape(req types.SubmitBidRequest) error {
	if _, err := sdk.AccAddressFromBech32(req.Bidder); err != nil {
		return fmt.Errorf("invalid bidder address: %w", err)
	}
	if err := sdk.ValidateDenom(req.Price.Denom); err != nil {
		return fmt.Errorf("invalid bid denom: %w", err)
	}
	if !req.Price.IsValid() || !req.Price.Amount.IsPositive() {
		return types.ErrInvalidBid
	}
	if req.LatencyMs == 0 {
		return types.ErrBidLatencyExceeded
	}
	return nil
}

func (k *Keeper) updateBestBid(ctx context.Context, auction *types.SpotAuction, bid *types.SpotBid) error {
	var (
		currentBest types.SpotBid
		hasBest     bool
	)
	if auction.BestBidID != "" {
		stored, err := k.state.Bids.Get(ctx, auction.BestBidID)
		if err != nil {
			return fmt.Errorf("load best bid: %w", err)
		}
		currentBest = stored
		hasBest = true
	}

	if !hasBest || bid.BetterThan(currentBest) {
		auction.BestBidID = bid.ID
		auction.BestBidPrice = bid.Price
		auction.BestBidLatencyMs = bid.LatencyMs
		auction.BestBidSubmittedAt = bid.SubmittedAt
		if err := auction.ValidateBasic(); err != nil {
			return err
		}
		if err := k.state.Auctions.Set(ctx, auction.ID, *auction); err != nil {
			return fmt.Errorf("update auction best bid: %w", err)
		}
	}

	return nil
}

func applyDiscount(amount sdk.Coin, discountBps uint32) sdk.Coin {
	if discountBps == 0 {
		return amount
	}
	if discountBps > 10_000 {
		discountBps = 10_000
	}
	adj := amount.Amount.MulRaw(int64(10_000 - discountBps)).QuoRaw(10_000)
	return sdk.NewCoin(amount.Denom, adj)
}
