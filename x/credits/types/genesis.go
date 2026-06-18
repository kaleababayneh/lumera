//go:build cosmos

// Package types holds shared types and helpers for the credits module.
//
//revive:disable:var-naming // The module uses the conventional Cosmos `types` namespace.
package types

import (
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// NewGenesisState builds a new genesis state instance with all state types.
func NewGenesisState(params *Params, locks []Lock, settlements []SettlementRecord, disputes []DisputeRecord, metrics *SettlementMetrics) *GenesisState {
	if params == nil {
		params = DefaultParams()
	}
	lockPtrs := make([]*Lock, len(locks))
	for i := range locks {
		lockPtrs[i] = &locks[i]
	}
	settlementPtrs := make([]*SettlementRecord, len(settlements))
	for i := range settlements {
		settlementPtrs[i] = &settlements[i]
	}
	disputePtrs := make([]*DisputeRecord, len(disputes))
	for i := range disputes {
		disputePtrs[i] = &disputes[i]
	}
	return &GenesisState{
		Params:      params,
		Locks:       lockPtrs,
		Settlements: settlementPtrs,
		Disputes:    disputePtrs,
		Metrics:     metrics,
	}
}

// DefaultGenesis returns the default genesis state for the credits module.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:       DefaultParams(),
		Locks:        []*Lock{},
		Settlements:  []*SettlementRecord{},
		Disputes:     []*DisputeRecord{},
		CacRoyalties: []*CACRoyaltyRecord{},
		CacStats:     []*CACRoyaltyStats{},
	}
}

// Validate ensures the genesis data is self-consistent.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if gs.Params == nil {
		return fmt.Errorf("params must be provided")
	}
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// Validate locks
	if err := gs.validateLocks(); err != nil {
		return err
	}

	// Validate settlements
	if err := gs.validateSettlements(); err != nil {
		return err
	}

	// Validate disputes
	if err := gs.validateDisputes(); err != nil {
		return err
	}

	// Validate metrics (optional, can be nil)
	if gs.Metrics != nil {
		if err := gs.validateMetrics(); err != nil {
			return err
		}
	}

	// Validate CAC royalties
	if err := gs.validateCACRoyalties(); err != nil {
		return err
	}

	// Validate CAC stats
	if err := gs.validateCACStats(); err != nil {
		return err
	}

	return nil
}

// validateLocks checks lock entries for consistency.
func (gs *GenesisState) validateLocks() error {
	seen := make(map[string]struct{}, len(gs.Locks))
	for _, lock := range gs.Locks {
		if lock == nil {
			return fmt.Errorf("lock entry cannot be nil")
		}
		if lock.LockId == "" {
			return fmt.Errorf("lock id cannot be empty")
		}
		if _, dup := seen[lock.LockId]; dup {
			return fmt.Errorf("duplicate lock id %s", lock.LockId)
		}
		seen[lock.LockId] = struct{}{}
		amount, err := CoinFromProtoSafe(lock.Amount)
		if err != nil {
			return fmt.Errorf("invalid amount for lock %s: %w", lock.LockId, err)
		}
		if amount.IsNil() || !amount.IsPositive() {
			return fmt.Errorf("lock %s amount must be positive", lock.LockId)
		}
		if err := validateGenesisLockTimestamps(lock); err != nil {
			return err
		}
		if err := validateGenesisLockStatus(lock); err != nil {
			return err
		}
	}
	return nil
}

func validateGenesisLockTimestamps(lock *Lock) error {
	if err := validateGenesisOptionalTimestamp("lock "+lock.LockId, "created_at", lock.CreatedAt); err != nil {
		return err
	}
	if err := validateGenesisOptionalTimestamp("lock "+lock.LockId, "expires_at", lock.ExpiresAt); err != nil {
		return err
	}
	if lock.CreatedAt != nil && lock.ExpiresAt != nil && !lock.ExpiresAt.AsTime().After(lock.CreatedAt.AsTime()) {
		return fmt.Errorf("lock %s expires_at must be after created_at", lock.LockId)
	}
	return nil
}

func validateGenesisOptionalTimestamp(owner, field string, ts *timestamppb.Timestamp) error {
	if ts == nil {
		return nil
	}
	if err := ts.CheckValid(); err != nil {
		return fmt.Errorf("%s has invalid %s: %w", owner, field, err)
	}
	return nil
}

func validateGenesisLockStatus(lock *Lock) error {
	if _, ok := LockStatus_name[int32(lock.Status)]; !ok {
		return fmt.Errorf("lock %s has invalid status %d", lock.LockId, lock.Status)
	}
	switch lock.Status {
	case LockStatus_LOCK_STATUS_UNSPECIFIED:
		return fmt.Errorf("lock %s has unspecified status", lock.LockId)
	case LockStatus_LOCK_STATUS_ACTIVE:
		if lock.ExpiresAt == nil {
			return fmt.Errorf("active lock %s missing expires_at", lock.LockId)
		}
	}
	return nil
}

// validateSettlements checks settlement records for consistency.
func (gs *GenesisState) validateSettlements() error {
	seen := make(map[string]struct{}, len(gs.Settlements))
	for _, settlement := range gs.Settlements {
		if settlement == nil {
			return fmt.Errorf("settlement entry cannot be nil")
		}
		if settlement.Id == "" {
			return fmt.Errorf("settlement id cannot be empty")
		}
		if _, dup := seen[settlement.Id]; dup {
			return fmt.Errorf("duplicate settlement id %s", settlement.Id)
		}
		seen[settlement.Id] = struct{}{}
		if err := validateGenesisOptionalTimestamp("settlement "+settlement.Id, "timestamp", settlement.Timestamp); err != nil {
			return err
		}
		if err := validateGenesisOptionalTimestamp("settlement "+settlement.Id, "completed_at", settlement.CompletedAt); err != nil {
			return err
		}
		if settlement.Timestamp != nil && settlement.CompletedAt != nil && settlement.CompletedAt.AsTime().Before(settlement.Timestamp.AsTime()) {
			return fmt.Errorf("settlement %s completed_at must be at or after timestamp", settlement.Id)
		}
		if err := validateGenesisSettlementStatus(settlement); err != nil {
			return err
		}
	}
	return nil
}

func validateGenesisSettlementStatus(settlement *SettlementRecord) error {
	if _, ok := SettlementStatus_name[int32(settlement.Status)]; !ok {
		return fmt.Errorf("settlement %s has invalid status %d", settlement.Id, settlement.Status)
	}
	switch settlement.Status {
	case SettlementStatus_SETTLEMENT_STATUS_UNSPECIFIED:
		return fmt.Errorf("settlement %s has unspecified status", settlement.Id)
	case SettlementStatus_SETTLEMENT_STATUS_PENDING:
		if settlement.CompletedAt != nil {
			return fmt.Errorf("pending settlement %s must not have completed_at", settlement.Id)
		}
	case SettlementStatus_SETTLEMENT_STATUS_COMPLETED, SettlementStatus_SETTLEMENT_STATUS_FAILED:
		if settlement.CompletedAt == nil {
			return fmt.Errorf("terminal settlement %s missing completed_at", settlement.Id)
		}
	}
	return nil
}

// validateDisputes checks dispute records for consistency.
func (gs *GenesisState) validateDisputes() error {
	seen := make(map[string]struct{}, len(gs.Disputes))
	for _, dispute := range gs.Disputes {
		if dispute == nil {
			return fmt.Errorf("dispute entry cannot be nil")
		}
		if dispute.Id == "" {
			return fmt.Errorf("dispute id cannot be empty")
		}
		if _, dup := seen[dispute.Id]; dup {
			return fmt.Errorf("duplicate dispute id %s", dispute.Id)
		}
		seen[dispute.Id] = struct{}{}
		if err := validateGenesisOptionalTimestamp("dispute "+dispute.Id, "created_at", dispute.CreatedAt); err != nil {
			return err
		}
		if err := validateGenesisOptionalTimestamp("dispute "+dispute.Id, "resolved_at", dispute.ResolvedAt); err != nil {
			return err
		}
		if dispute.CreatedAt != nil && dispute.ResolvedAt != nil && dispute.ResolvedAt.AsTime().Before(dispute.CreatedAt.AsTime()) {
			return fmt.Errorf("dispute %s resolved_at must be at or after created_at", dispute.Id)
		}
	}
	return nil
}

// validateMetrics checks metrics for consistency.
func (gs *GenesisState) validateMetrics() error {
	// Metrics validation is minimal - just ensure no negative counters
	// The protobuf types use uint64 which can't be negative anyway
	return nil
}

// validateCACRoyalties checks CAC royalty records for consistency.
func (gs *GenesisState) validateCACRoyalties() error {
	seen := make(map[string]struct{}, len(gs.CacRoyalties))
	for _, record := range gs.CacRoyalties {
		if record == nil {
			return fmt.Errorf("CAC royalty record cannot be nil")
		}
		if record.RecordId == "" {
			return fmt.Errorf("CAC royalty record id cannot be empty")
		}
		if _, dup := seen[record.RecordId]; dup {
			return fmt.Errorf("duplicate CAC royalty record id %s", record.RecordId)
		}
		seen[record.RecordId] = struct{}{}
		if err := validateGenesisOptionalTimestamp("CAC royalty record "+record.RecordId, "timestamp", record.Timestamp); err != nil {
			return err
		}
	}
	return nil
}

// validateCACStats checks CAC stats for consistency.
func (gs *GenesisState) validateCACStats() error {
	seen := make(map[string]struct{}, len(gs.CacStats))
	for _, stats := range gs.CacStats {
		if stats == nil {
			return fmt.Errorf("CAC stats entry cannot be nil")
		}
		if stats.ToolId == "" {
			return fmt.Errorf("CAC stats tool_id cannot be empty")
		}
		if _, dup := seen[stats.ToolId]; dup {
			return fmt.Errorf("duplicate CAC stats for tool %s", stats.ToolId)
		}
		seen[stats.ToolId] = struct{}{}
		if err := validateGenesisOptionalTimestamp("CAC stats "+stats.ToolId, "last_updated", stats.LastUpdated); err != nil {
			return err
		}
	}
	return nil
}
