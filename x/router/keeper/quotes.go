package keeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/router/types"
)

var (
	defaultQuoteEstCost    = decimal.RequireFromString("0.001")
	quoteMaxCostMultiplier = decimal.RequireFromString("1.2")
)

// CreateQuote generates a new quote for tool invocation
func (k Keeper) CreateQuote(ctx context.Context, toolID string, args map[string]string, sessionID string, maxCost string) (*types.QuoteRecord, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if _, found := k.registryKeeper.GetToolCard(sdkCtx, toolID); !found {
		return nil, fmt.Errorf("tool not found: %s", toolID)
	}

	estCost := defaultQuoteEstCost
	if maxCost != "" {
		maxCostDec, err := decimal.NewFromString(maxCost)
		if err != nil {
			return nil, fmt.Errorf("invalid max cost: %w", err)
		}
		if !moneyguard.IsSafeExponent(maxCostDec) {
			return nil, fmt.Errorf("max cost magnitude out of safe range")
		}
		if estCost.GreaterThan(maxCostDec) {
			estCost = maxCostDec
		}
	}

	quoteSeq, err := k.state.QuoteSequence.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate quote sequence: %w", err)
	}
	quoteID := fmt.Sprintf("quote-%s-%d-%d", toolID, sdkCtx.BlockTime().UnixNano(), quoteSeq)

	quote := types.NewQuoteRecord(quoteID, toolID, sessionID)
	quote.SetEstCostDecimal(estCost)
	quote.SetMaxCostDecimal(estCost.Mul(quoteMaxCostMultiplier))
	quote.P95Latency = 500
	quote.CacheEligible = k.isCacheEligible(sdkCtx, toolID, args)
	quote.Status = "active"
	quote.SetValidUntil(sdkCtx.BlockTime().Add(5 * time.Minute))
	quote.SetCreatedAt(sdkCtx.BlockTime())

	lacAmount, err := decimalToCreditCoin(estCost)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quote cost to credit coin: %w", err)
	}
	intentHash := fmt.Sprintf("intent_%s_%s", toolID, sessionID)
	policyVersion := "v1"

	lockID, err := k.creditsKeeper.LockCredits(
		ctx,
		k.authority,
		sessionID,
		lacAmount,
		toolID,
		quoteID,
		policyVersion,
		intentHash,
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to lock credits: %w", err)
	}

	quote.LockId = lockID
	quote.SetLockedCoin(lacAmount)
	quote.IntentHash = intentHash

	if err := k.SaveQuote(ctx, quote); err != nil {
		if unlockErr := k.creditsKeeper.UnlockCredits(ctx, lockID, "quote save failed"); unlockErr != nil {
			k.Logger(sdkCtx).Error("failed to unlock credits after quote save error", "lock_id", lockID, "error", unlockErr)
		}
		return nil, fmt.Errorf("failed to save quote: %w", err)
	}

	return quote, nil
}

// SaveQuote persists a quote to state storage
func (k Keeper) SaveQuote(ctx context.Context, quote *types.QuoteRecord) error {
	if err := k.state.Quotes.Set(ctx, quote.GetQuoteId(), quote); err != nil {
		return fmt.Errorf("failed to save quote: %w", err)
	}
	return nil
}

// GetQuote retrieves a quote by ID
func (k Keeper) GetQuote(ctx context.Context, quoteID string) (*types.QuoteRecord, error) {
	quote, err := k.state.Quotes.Get(ctx, quoteID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, fmt.Errorf("quote not found: %s", quoteID)
		}
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}
	return quote, nil
}

// UseQuote marks a quote as used and processes settlement
func (k Keeper) UseQuote(ctx context.Context, quoteID string, actualCost sdk.Coin, receipt interface{}) error {
	quote, err := k.GetQuote(ctx, quoteID)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if sdkCtx.BlockTime().After(quote.ValidUntilTime()) {
		return fmt.Errorf("quote expired")
	}
	if quote.Status != "active" {
		return fmt.Errorf("quote already used or cancelled")
	}

	settlementReq, ok := receipt.(creditskeeper.SettlementRequest)
	if !ok {
		return fmt.Errorf("invalid settlement payload type %T", receipt)
	}

	if actualCost.IsZero() {
		if len(settlementReq.TotalAmount) == 0 {
			return fmt.Errorf("settlement request missing total amount")
		}
		actualCost = settlementReq.TotalAmount[0]
	}

	if actualCost.Amount.IsZero() {
		return fmt.Errorf("actual cost cannot be zero")
	}

	if _, err := k.creditsKeeper.SettleLock(ctx, quote.GetLockId(), actualCost, settlementReq); err != nil {
		return fmt.Errorf("failed to settle lock: %w", err)
	}

	quote.Status = "used"
	return k.SaveQuote(ctx, quote)
}

// CancelQuote cancels a quote and releases locked credits
func (k Keeper) CancelQuote(ctx context.Context, quoteID string) error {
	quote, err := k.GetQuote(ctx, quoteID)
	if err != nil {
		return err
	}

	if quote.Status != "active" {
		return fmt.Errorf("quote not active")
	}

	if err := k.creditsKeeper.UnlockCredits(ctx, quote.GetLockId(), "quote cancelled"); err != nil {
		return fmt.Errorf("failed to unlock credits: %w", err)
	}

	quote.Status = "cancelled"
	return k.SaveQuote(ctx, quote)
}
