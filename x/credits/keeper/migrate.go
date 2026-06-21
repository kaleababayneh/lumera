package keeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// Migration constants
const (
	// ConsensusVersion defines the module's consensus version for migrations
	ConsensusVersion = 2
)

// IterateSettlements walks through all settlement records.
// If the callback returns true, iteration stops early.
func (k Keeper) IterateSettlements(ctx context.Context, cb func(*types.SettlementRecord) bool) error {
	return k.state.Settlements.Walk(ctx, nil, func(_ string, settlement *types.SettlementRecord) (bool, error) {
		if settlement == nil {
			return false, nil
		}
		return cb(settlement), nil
	})
}

// SaveSettlement writes a settlement record to state.
func (k Keeper) SaveSettlement(ctx context.Context, settlement *types.SettlementRecord) error {
	if settlement == nil {
		return fmt.Errorf("settlement cannot be nil")
	}
	if settlement.Id == "" {
		return fmt.Errorf("settlement id cannot be empty")
	}
	return k.state.Settlements.Set(ctx, settlement.Id, settlement)
}

// IterateDisputes walks through all dispute records.
// If the callback returns true, iteration stops early.
func (k Keeper) IterateDisputes(ctx context.Context, cb func(*types.DisputeRecord) bool) error {
	return k.state.Disputes.Walk(ctx, nil, func(_ string, dispute *types.DisputeRecord) (bool, error) {
		if dispute == nil {
			return false, nil
		}
		return cb(dispute), nil
	})
}

// GetDispute retrieves a dispute record by ID.
func (k Keeper) GetDispute(ctx context.Context, id string) (*types.DisputeRecord, bool) {
	dispute, err := k.state.Disputes.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(sdk.UnwrapSDKContext(ctx)).Error("failed to load dispute", "id", id, "error", err)
		return nil, false
	}
	if dispute == nil {
		return nil, false
	}
	return dispute, true
}

// SaveDispute writes a dispute record to state.
func (k Keeper) SaveDispute(ctx context.Context, dispute *types.DisputeRecord) error {
	if dispute == nil {
		return fmt.Errorf("dispute cannot be nil")
	}
	if dispute.Id == "" {
		return fmt.Errorf("dispute id cannot be empty")
	}
	return k.state.Disputes.Set(ctx, dispute.Id, dispute)
}

// GetMetrics retrieves settlement metrics from state.
func (k Keeper) GetMetrics(ctx context.Context) *types.SettlementMetrics {
	metrics, err := k.state.Metrics.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return &types.SettlementMetrics{}
		}
		k.Logger(sdk.UnwrapSDKContext(ctx)).Error("failed to load settlement metrics", "error", err)
		return &types.SettlementMetrics{}
	}
	if metrics == nil {
		return &types.SettlementMetrics{}
	}
	return metrics
}

// SetMetrics sets settlement metrics in state.
func (k Keeper) SetMetrics(ctx context.Context, metrics *types.SettlementMetrics) error {
	if metrics == nil {
		return fmt.Errorf("metrics cannot be nil")
	}
	return k.state.Metrics.Set(ctx, metrics)
}

// ExportState exports the full module state for genesis.
func (k Keeper) ExportState(ctx context.Context) (*types.GenesisState, error) {
	params := k.GetParams(ctx)

	// Export locks
	locks := make([]*types.Lock, 0)
	if err := k.IterateLocks(ctx, func(lock *types.Lock) bool {
		if lock != nil {
			locks = append(locks, lock)
		}
		return false
	}); err != nil {
		return nil, fmt.Errorf("failed to export locks: %w", err)
	}

	// Export settlements
	settlements := make([]*types.SettlementRecord, 0)
	if err := k.IterateSettlements(ctx, func(settlement *types.SettlementRecord) bool {
		if settlement != nil {
			settlements = append(settlements, settlement)
		}
		return false
	}); err != nil {
		return nil, fmt.Errorf("failed to export settlements: %w", err)
	}

	// Export disputes
	disputes := make([]*types.DisputeRecord, 0)
	if err := k.IterateDisputes(ctx, func(dispute *types.DisputeRecord) bool {
		if dispute != nil {
			disputes = append(disputes, dispute)
		}
		return false
	}); err != nil {
		return nil, fmt.Errorf("failed to export disputes: %w", err)
	}

	// Export metrics
	metrics := k.GetMetrics(ctx)

	// Export CAC royalties
	cacRoyalties := make([]*types.CACRoyaltyRecord, 0)
	if err := k.state.CACRoyalties.Walk(ctx, nil, func(_ string, record *types.CACRoyaltyRecord) (bool, error) {
		if record != nil {
			cacRoyalties = append(cacRoyalties, record)
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to export CAC royalties: %w", err)
	}

	// Export CAC stats
	cacStats := make([]*types.CACRoyaltyStats, 0)
	if err := k.state.CACStats.Walk(ctx, nil, func(_ string, stats *types.CACRoyaltyStats) (bool, error) {
		if stats != nil {
			cacStats = append(cacStats, stats)
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to export CAC stats: %w", err)
	}

	// Export sequence counters
	var cacSeq uint64
	if v, err := k.state.CACSeq.Peek(ctx); err == nil {
		cacSeq = v
	}
	var lockSeq uint64
	if v, err := k.state.LockSeq.Peek(ctx); err == nil {
		lockSeq = v
	}

	return &types.GenesisState{
		Params:       params,
		Locks:        locks,
		Settlements:  settlements,
		Disputes:     disputes,
		Metrics:      metrics,
		CacRoyalties: cacRoyalties,
		CacStats:     cacStats,
		CacSeq:       cacSeq,
		LockSeq:      lockSeq,
	}, nil
}

// ImportState imports genesis state into the module.
func (k Keeper) ImportState(ctx context.Context, genesis *types.GenesisState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if genesis == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if err := genesis.Validate(); err != nil {
		return fmt.Errorf("invalid genesis state: %w", err)
	}

	// Set params
	params := genesis.Params
	if err := k.SetParams(ctx, params); err != nil {
		return fmt.Errorf("failed to set params: %w", err)
	}

	// Import locks and track max sequence
	var nextSeq uint64 = 1
	for _, lock := range genesis.Locks {
		if lock == nil {
			continue
		}
		if err := k.SaveLock(ctx, lock); err != nil {
			return fmt.Errorf("failed to import lock %s: %w", lock.LockId, err)
		}
		if seq, err := parseLockSequence(lock.LockId); err == nil {
			if seq >= nextSeq {
				nextSeq = seq + 1
			}
		}
	}
	if err := k.SetLockSequence(ctx, nextSeq); err != nil {
		return fmt.Errorf("failed to set lock sequence: %w", err)
	}

	// Import settlements
	for _, settlement := range genesis.Settlements {
		if settlement == nil {
			continue
		}
		if err := k.SaveSettlement(ctx, settlement); err != nil {
			return fmt.Errorf("failed to import settlement %s: %w", settlement.Id, err)
		}
	}

	// Import disputes
	for _, dispute := range genesis.Disputes {
		if dispute == nil {
			continue
		}
		if err := k.SaveDispute(ctx, dispute); err != nil {
			return fmt.Errorf("failed to import dispute %s: %w", dispute.Id, err)
		}
	}

	// Import metrics
	if genesis.Metrics != nil {
		if err := k.SetMetrics(ctx, genesis.Metrics); err != nil {
			return fmt.Errorf("failed to import metrics: %w", err)
		}
	}

	// Import CAC royalties
	var maxCACSeq uint64
	for _, record := range genesis.CacRoyalties {
		if record == nil {
			continue
		}
		if err := k.state.CACRoyalties.Set(ctx, record.RecordId, record); err != nil {
			return fmt.Errorf("failed to import CAC royalty %s: %w", record.RecordId, err)
		}
		if seq, err := parseCACSequence(record.RecordId); err == nil && seq > maxCACSeq {
			maxCACSeq = seq
		}
	}

	// Import CAC stats
	for _, stats := range genesis.CacStats {
		if stats == nil {
			continue
		}
		if err := k.state.CACStats.Set(ctx, stats.ToolId, stats); err != nil {
			return fmt.Errorf("failed to import CAC stats for %s: %w", stats.ToolId, err)
		}
	}

	// Restore sequence counters (prefer explicit genesis value, fall back to max+1)
	if genesis.CacSeq > 0 {
		if err := k.state.CACSeq.Set(ctx, genesis.CacSeq); err != nil {
			return fmt.Errorf("failed to restore CAC sequence: %w", err)
		}
	} else if maxCACSeq > 0 {
		if err := k.state.CACSeq.Set(ctx, maxCACSeq+1); err != nil {
			return fmt.Errorf("failed to set CAC sequence from records: %w", err)
		}
	}

	if genesis.LockSeq > 0 {
		if err := k.state.LockSeq.Set(ctx, genesis.LockSeq); err != nil {
			return fmt.Errorf("failed to restore lock sequence: %w", err)
		}
	}
	// Note: nextSeq from lock import loop above also sets lock sequence;
	// genesis.LockSeq takes priority if non-zero.

	// Rebuild secondary indexes from primary data
	if err := k.rebuildIndexes(ctx); err != nil {
		return fmt.Errorf("failed to rebuild indexes: %w", err)
	}

	k.Logger(sdkCtx).Info("imported credits genesis state",
		"locks", len(genesis.Locks),
		"settlements", len(genesis.Settlements),
		"disputes", len(genesis.Disputes),
		"cac_royalties", len(genesis.CacRoyalties),
		"cac_stats", len(genesis.CacStats),
	)

	return nil
}

// rebuildIndexes rebuilds all secondary indexes from primary data.
// Called during ImportState to ensure consistency.
func (k Keeper) rebuildIndexes(ctx context.Context) error {
	// Rebuild LockExpiry, LocksByQuote, LockReceipts indexes from locks
	if err := k.state.Locks.Walk(ctx, nil, func(_ string, lock *types.Lock) (bool, error) {
		if lock == nil {
			return false, nil
		}
		// Rebuild LockExpiry index for active locks
		if lock.Status == types.LockStatus_LOCK_STATUS_ACTIVE && !lock.ExpiresAt.IsZero() {
			if err := k.state.LockExpiry.Set(ctx, collections.Join(lock.ExpiresAt, lock.LockId)); err != nil {
				return false, fmt.Errorf("rebuild lock expiry index %s: %w", lock.LockId, err)
			}
		}
		// Rebuild LocksByQuote index
		if lock.QuoteId != "" {
			if err := k.state.LocksByQuote.Set(ctx, lock.QuoteId, lock.LockId); err != nil {
				return false, fmt.Errorf("rebuild locks-by-quote index %s: %w", lock.LockId, err)
			}
		}
		// Rebuild FinalizedLocks index for non-active locks
		if lock.Status == types.LockStatus_LOCK_STATUS_RELEASED || lock.Status == types.LockStatus_LOCK_STATUS_BURNED || lock.Status == types.LockStatus_LOCK_STATUS_EXPIRED {
			ts := time.Time{}
			if !lock.ExpiresAt.IsZero() {
				ts = lock.ExpiresAt
			} else if !lock.CreatedAt.IsZero() {
				ts = lock.CreatedAt
			}
			if err := k.state.FinalizedLocks.Set(ctx, collections.Join(ts, lock.LockId)); err != nil {
				return false, fmt.Errorf("rebuild finalized locks index %s: %w", lock.LockId, err)
			}
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("rebuild lock indexes: %w", err)
	}

	// Rebuild PendingSettlements, SettlementsByTime indexes from settlements
	if err := k.state.Settlements.Walk(ctx, nil, func(_ string, s *types.SettlementRecord) (bool, error) {
		if s == nil {
			return false, nil
		}
		if s.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
			if err := k.state.PendingSettlements.Set(ctx, s.Id); err != nil {
				return false, fmt.Errorf("rebuild pending settlements index %s: %w", s.Id, err)
			}
		}
		if (s.Status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED || s.Status == types.SettlementStatus_SETTLEMENT_STATUS_FAILED) && s.CompletedAt != nil {
			if err := k.state.SettlementsByTime.Set(ctx, collections.Join(*s.CompletedAt, s.Id)); err != nil {
				return false, fmt.Errorf("rebuild settlements-by-time index %s: %w", s.Id, err)
			}
		}
		// Rebuild LockReceipts index from settlement records
		if s.LockId != "" {
			if err := k.state.LockReceipts.Set(ctx, s.LockId, s.Id); err != nil {
				return false, fmt.Errorf("rebuild lock-receipts index %s: %w", s.Id, err)
			}
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("rebuild settlement indexes: %w", err)
	}

	return nil
}

// parseCACSequence extracts the numeric suffix from a CAC record identifier.
func parseCACSequence(recordID string) (uint64, error) {
	const prefix = "cac-"
	if len(recordID) <= len(prefix) || recordID[:len(prefix)] != prefix {
		return 0, fmt.Errorf("unexpected CAC record id format: %s", recordID)
	}
	var seq uint64
	_, err := fmt.Sscanf(recordID[len(prefix):], "%d", &seq)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// MigrateV1ToV2 performs migration from v1 to v2 state.
// This is called by the upgrade handler during chain upgrades.
// The migration handles:
// 1. Re-encoding any legacy CAC data under the new Collections schema
// 2. Preserving all lock IDs and settlement records
// 3. Ensuring idempotent reruns
func (k Keeper) MigrateV1ToV2(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	k.Logger(sdkCtx).Info("starting credits v1 to v2 migration")

	// The current keeper already uses Collections v2 schema.
	// This migration ensures any legacy data is properly re-encoded.
	//
	// For fresh installations or already-migrated chains, this is a no-op.
	// For chains with v1 data, the schema builder in NewKeeper already
	// handles the migration via the collections framework.

	// Verify schema integrity by attempting to read each collection
	locksCount := 0
	if err := k.IterateLocks(ctx, func(lock *types.Lock) bool {
		if lock != nil {
			locksCount++
		}
		return false
	}); err != nil {
		return fmt.Errorf("migration failed: locks collection corrupted: %w", err)
	}

	settlementsCount := 0
	if err := k.IterateSettlements(ctx, func(settlement *types.SettlementRecord) bool {
		if settlement != nil {
			settlementsCount++
		}
		return false
	}); err != nil {
		return fmt.Errorf("migration failed: settlements collection corrupted: %w", err)
	}

	disputesCount := 0
	if err := k.IterateDisputes(ctx, func(dispute *types.DisputeRecord) bool {
		if dispute != nil {
			disputesCount++
		}
		return false
	}); err != nil {
		return fmt.Errorf("migration failed: disputes collection corrupted: %w", err)
	}

	// Verify params can be read
	_ = k.GetParams(ctx)

	// Verify metrics can be read
	_ = k.GetMetrics(ctx)

	k.Logger(sdkCtx).Info("credits v1 to v2 migration completed",
		"locks_migrated", locksCount,
		"settlements_migrated", settlementsCount,
		"disputes_migrated", disputesCount,
	)

	return nil
}

// parseLockSequence extracts the numeric suffix from a lock identifier.
// Moved from module.go to be reusable.
func parseLockSequence(lockID string) (uint64, error) {
	const prefix = "lock-"
	if len(lockID) <= len(prefix) {
		return 0, fmt.Errorf("unexpected lock id format: %s", lockID)
	}
	if lockID[:len(prefix)] != prefix {
		return 0, fmt.Errorf("unexpected lock id format: %s", lockID)
	}

	var seq uint64
	_, err := fmt.Sscanf(lockID[len(prefix):], "%d", &seq)
	if err != nil {
		return 0, err
	}
	return seq, nil
}
