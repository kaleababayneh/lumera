// Package types defines protobuf-backed structures for priority assignments.
package types

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PriorityAssignment binds a policy to a priority tier.
type PriorityAssignment struct {
	PolicyID   string    `json:"policy_id"`
	Tier       string    `json:"tier"`
	AssignedAt time.Time `json:"assigned_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ValidateBasic ensures assignment sanity.
func (a PriorityAssignment) ValidateBasic() error {
	policyID := strings.TrimSpace(a.PolicyID)
	if policyID == "" {
		return fmt.Errorf("policy id required")
	}
	if policyID != a.PolicyID {
		return fmt.Errorf("policy id cannot contain leading or trailing whitespace")
	}
	tier := strings.TrimSpace(a.Tier)
	if tier == "" {
		return fmt.Errorf("tier required")
	}
	if tier != a.Tier {
		return fmt.Errorf("tier cannot contain leading or trailing whitespace")
	}
	if a.AssignedAt.IsZero() {
		return fmt.Errorf("assignedAt must be set")
	}
	if !a.ExpiresAt.IsZero() && a.ExpiresAt.Before(a.AssignedAt) {
		return fmt.Errorf("expiresAt must be after assignedAt")
	}
	return nil
}

// PriorityAdjustments describes tuning applied to auctions and queue ordering.
type PriorityAdjustments struct {
	Applied         bool
	TierName        string
	MaxLatencyMs    uint32
	AuctionTTL      time.Duration
	SpotDiscountBps uint32

	// QueueWeight is the multiplier for queue advancement (100 = 1x, 200 = 2x, etc.).
	// Used by the router to calculate weighted queue position.
	QueueWeight uint32

	// PricingMultiplierBps is the cost multiplier in basis points (100 = 1x, 150 = 1.5x).
	// Applied to base tool pricing for this tier.
	PricingMultiplierBps uint32

	// ReservedCapacityBps is the percentage of total capacity reserved for this tier.
	// Used by the router to check if tier-specific capacity is available.
	ReservedCapacityBps uint32
}

// PriorityKeeper interface exposed to other modules.
type PriorityKeeper interface {
	ResolveAdjustments(ctx context.Context, policyID string, defaultLatency uint32, defaultTTL time.Duration) (PriorityAdjustments, error)
}
