// Package credits wires the ABCI lifecycle hooks for the credits module.
package credits

import (
	"fmt"
	"time"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/credits/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker processes settlements that have cleared their dispute window
func BeginBlocker(ctx sdk.Context, k *keeper.Keeper) error {
	logger := k.Logger(ctx).With("method", "BeginBlocker")

	currentTime := ctx.BlockTime()
	var processedCount, errorCount int

	params := k.GetParams(ctx)
	disputeWindow := k.SettlementDisputeWindow(ctx)

	maxToProcess := int(params.MaxSettlementsPerBlock)
	if maxToProcess <= 0 {
		maxToProcess = types.DefaultMaxSettlementsPerBlock
	}

	// Settlements MUST be finalized via SettleLock (driven by Registry or Router), which handles
	// both the accounting and the lock closure atomically.
	if err := k.IteratePendingSettlements(ctx, maxToProcess, func(settlement *types.SettlementRecord) (bool, bool, error) {
		if settlement.Timestamp.IsZero() {
			logger.Error("missing settlement timestamp", "settlement_id", settlement.Id)
			settlement.Status = types.SettlementStatus_SETTLEMENT_STATUS_FAILED
			settlement.FailureReason = "missing settlement timestamp"
			settlement.CompletedAt = &currentTime
			errorCount++
			if err := k.UpdateSettlement(ctx, settlement); err != nil {
				logger.Error("failed to mark settlement as failed", "settlement_id", settlement.Id, "error", err)
			}
			return true, false, nil
		}
		settlementTime := settlement.Timestamp

		// Skip until dispute window elapses without counting toward per-block limit.
		if currentTime.Sub(settlementTime) < disputeWindow {
			return false, false, nil
		}

		// If LockId is missing (legacy record), we cannot safely finalize it.
		// Mark as failed to prevent infinite retries.
		if settlement.LockId == "" {
			logger.Error("pending settlement missing lock_id", "settlement_id", settlement.Id)
			settlement.Status = types.SettlementStatus_SETTLEMENT_STATUS_FAILED
			settlement.FailureReason = "missing lock_id"
			settlement.CompletedAt = &currentTime
			errorCount++
			if err := k.UpdateSettlement(ctx, settlement); err != nil {
				logger.Error("failed to mark settlement as failed", "settlement_id", settlement.Id, "error", err)
			}
			return true, false, nil
		}

		ctx.GasMeter().ConsumeGas(keeper.GasPerSettlementProcess, "credits/settlement-processing")

		// Use cached context to ensure atomicity.
		cacheCtx, write := ctx.CacheContext()
		if err := k.FinalizeSettlementWithLock(cacheCtx, settlement); err != nil {
			logger.Error("failed to finalize settlement",
				"settlement_id", settlement.Id,
				"error", err,
			)

			settlement.Status = types.SettlementStatus_SETTLEMENT_STATUS_FAILED
			settlement.FailureReason = err.Error()
			settlement.CompletedAt = &currentTime
			errorCount++

			// Update the settlement failure status in the original context
			if err := k.UpdateSettlement(ctx, settlement); err != nil {
				logger.Error("failed to update failed settlement", "settlement_id", settlement.Id, "error", err)
			}
		} else {
			// Commit the successful processing
			write()
			processedCount++
		}

		return true, processedCount >= maxToProcess, nil
	}); err != nil {
		logger.Error("failed while scanning pending settlements", "error", err)
		return err
	}

	if processedCount > 0 || errorCount > 0 {
		logger.Info("settlement processing complete",
			"processed", processedCount,
			"errors", errorCount,
		)
	}

	// Process expired credit locks
	if err := k.ProcessExpiredLocks(ctx, int(params.MaxExpiredLocksPerBlock)); err != nil {
		logger.Error("failed to process expired locks", "error", err)
		return err
	}

	// Update metrics
	if err := k.UpdateSettlementMetrics(ctx, processedCount, errorCount); err != nil {
		return fmt.Errorf("update settlement metrics: %w", err)
	}

	return nil
}

// EndBlocker handles end-of-block processing
func EndBlocker(ctx sdk.Context, k *keeper.Keeper) error {
	if _, err := k.MaybeAdjustAdaptiveBurnRate(ctx); err != nil {
		k.Logger(ctx).Error("failed to evaluate adaptive burn rate", "error", err)
		return err
	}

	retentionWindow := 7 * 24 * time.Hour
	if adaptiveWindow := keeper.AdaptiveBurnWindowDuration(); adaptiveWindow > retentionWindow {
		retentionWindow = adaptiveWindow
	}
	pruneOlderThan := ctx.BlockTime().Add(-retentionWindow)
	params := k.GetParams(ctx)
	limit := int(params.MaxPrunedSettlementsPerBlock)
	if limit <= 0 {
		limit = int(types.DefaultMaxPrunedSettlementsPerBlock)
	}

	if err := k.PruneOldSettlements(ctx, pruneOlderThan, limit); err != nil {
		k.Logger(ctx).Error("failed to prune old settlements", "error", err)
	}

	if err := k.PruneFinalizedLocks(ctx, pruneOlderThan, limit); err != nil {
		k.Logger(ctx).Error("failed to prune finalized locks", "error", err)
	}

	return nil
}
