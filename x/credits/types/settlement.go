//go:build ignore

// NOTE: Legacy manual types kept for reference only. Proto-generated definitions
// in credits.pb.go supersede these declarations, so this file is excluded from
// builds to avoid duplicate symbol errors.

package types

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Settlement status enum
type SettlementStatus int32

const (
	SettlementStatus_PENDING   SettlementStatus = 0
	SettlementStatus_COMPLETED SettlementStatus = 1
	SettlementStatus_FAILED    SettlementStatus = 2
	SettlementStatus_DISPUTED  SettlementStatus = 3
	SettlementStatus_REFUNDED  SettlementStatus = 4
)

// SettlementRecord tracks a pending or completed settlement
type SettlementRecord struct {
	Id            string           `json:"id"`
	ToolId        string           `json:"tool_id"`
	PublisherId   string           `json:"publisher_id"`
	UserId        string           `json:"user_id"`
	RouterId      string           `json:"router_id,omitempty"`
	ReferrerId    string           `json:"referrer_id,omitempty"`
	TotalCost     sdk.Coins        `json:"total_cost"`
	BurnAmount    sdk.Coins        `json:"burn_amount"`
	NetAmount     sdk.Coins        `json:"net_amount"`
	CacheHit      bool             `json:"cache_hit"`
	OriginToolId  string           `json:"origin_tool_id,omitempty"`
	Status        SettlementStatus `json:"status"`
	Timestamp     time.Time        `json:"timestamp"`
	CompletedAt   *time.Time       `json:"completed_at,omitempty"`
	FailureReason string           `json:"failure_reason,omitempty"`
	DisputeId     string           `json:"dispute_id,omitempty"`
	ReceiptHash   string           `json:"receipt_hash"`
}

func (*SettlementRecord) ProtoMessage() {}
func (m *SettlementRecord) Reset()      { *m = SettlementRecord{} }
func (m SettlementRecord) String() string {
	return fmt.Sprintf("SettlementRecord{id: %s, tool_id: %s, status: %d}",
		m.Id, m.ToolId, m.Status)
}

// Validate performs validation on a settlement record
func (s *SettlementRecord) Validate() error {
	if s.Id == "" {
		return fmt.Errorf("settlement ID cannot be empty")
	}
	if s.ToolId == "" {
		return fmt.Errorf("tool ID cannot be empty")
	}
	if s.PublisherId == "" {
		return fmt.Errorf("publisher ID cannot be empty")
	}
	if s.UserId == "" {
		return fmt.Errorf("user ID cannot be empty")
	}

	// Validate amounts
	if s.TotalCost.IsZero() || !s.TotalCost.IsAllPositive() {
		return fmt.Errorf("total cost must be positive")
	}

	// Validate timestamp
	if s.Timestamp.IsZero() {
		return fmt.Errorf("timestamp cannot be zero")
	}

	// Validate cache fields
	if s.CacheHit && s.OriginToolId == "" {
		return fmt.Errorf("origin tool ID required for cache hit")
	}

	return nil
}

// CreditLock represents locked credits pending settlement
type CreditLock struct {
	Id         string    `json:"id"`
	UserId     string    `json:"user_id"`
	Amount     sdk.Coins `json:"amount"`
	Purpose    string    `json:"purpose"`
	ReceiptId  string    `json:"receipt_id,omitempty"`
	LockedAt   time.Time `json:"locked_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	ReleasedAt time.Time `json:"released_at,omitempty"`
	Status     string    `json:"status"` // "locked", "released", "burned"
}

// IsExpired checks if the lock has expired
func (l *CreditLock) IsExpired(currentTime time.Time) bool {
	return currentTime.After(l.ExpiresAt) && l.Status == "locked"
}

// DisputeRecord tracks settlement disputes
type DisputeRecord struct {
	Id           string    `json:"id"`
	SettlementId string    `json:"settlement_id"`
	DisputedBy   string    `json:"disputed_by"`
	Reason       string    `json:"reason"`
	Evidence     []string  `json:"evidence"`
	CreatedAt    time.Time `json:"created_at"`
	ResolvedAt   time.Time `json:"resolved_at,omitempty"`
	Resolution   string    `json:"resolution,omitempty"`
	Status       string    `json:"status"` // "pending", "approved", "rejected"
}

func (*DisputeRecord) ProtoMessage() {}
func (m *DisputeRecord) Reset()      { *m = DisputeRecord{} }
func (m DisputeRecord) String() string {
	return fmt.Sprintf("DisputeRecord{id: %s, settlement_id: %s, status: %s}", m.Id, m.SettlementId, m.Status)
}

// Store key prefixes are defined in keys.go to avoid conflicts

// Event types
const (
	EventTypeSettlement = "settlement"
	EventTypeBurn       = "lac_burn"
	EventTypeDistribute = "revenue_distribute"
	EventTypeLock       = "credit_lock"
	EventTypeUnlock     = "credit_unlock"
	EventTypeDispute    = "settlement_dispute"
	EventTypeSwap       = "lume_lac_swap"
)

// Attribute keys
const (
	AttributeKeySettlementID = "settlement_id"
	AttributeKeyToolID       = "tool_id"
	AttributeKeyPublisher    = "publisher"
	AttributeKeyUser         = "user"
	AttributeKeyAmount       = "amount"
	AttributeKeyBurnAmount   = "burn_amount"
	AttributeKeyStatus       = "status"
	AttributeKeyLockID       = "lock_id"
	AttributeKeyDisputeID    = "dispute_id"
	AttributeKeySwapRate     = "swap_rate"
	AttributeKeyRouter       = "router"
	AttributeKeySessionID    = "session_id"
	AttributeKeyReason       = "reason"
	AttributeKeyExpiresAt    = "expires_at"
)

// SettlementMetrics tracks settlement processing statistics
type SettlementMetrics struct {
	TotalProcessed   uint64 `json:"total_processed"`
	TotalErrors      uint64 `json:"total_errors"`
	LastProcessedAt  string `json:"last_processed_at"`
	TotalBurned      string `json:"total_burned"`
	TotalDistributed string `json:"total_distributed"`
}

func (*SettlementMetrics) ProtoMessage() {}
func (m *SettlementMetrics) Reset()      { *m = SettlementMetrics{} }
func (m SettlementMetrics) String() string {
	return fmt.Sprintf("SettlementMetrics{processed: %d, errors: %d}", m.TotalProcessed, m.TotalErrors)
}
