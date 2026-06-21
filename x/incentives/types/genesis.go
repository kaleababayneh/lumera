package types

import (
	"fmt"
	"time"
)

// DefaultGenesis returns the default genesis state for the incentives module.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:          DefaultParams(),
		TierConfigs:     DefaultTierConfigs(),
		Badges:          []*Badge{},
		MetricSnapshots: []*MetricSnapshot{},
		BadgeEvents:     []*BadgeEvent{},
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}

	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// Validate tier configs
	tiersSeen := make(map[BadgeTier]bool)
	for i, config := range gs.TierConfigs {
		if err := ValidateTierConfig(config); err != nil {
			return fmt.Errorf("invalid tier config at index %d: %w", i, err)
		}
		if tiersSeen[config.Tier] {
			return fmt.Errorf("duplicate tier config for tier %v", config.Tier)
		}
		tiersSeen[config.Tier] = true
	}

	// Validate badges
	badgesSeen := make(map[string]bool)
	for i, badge := range gs.Badges {
		if badge == nil {
			return fmt.Errorf("badge at index %d is nil", i)
		}
		if err := validateToolID(badge.ToolId); err != nil {
			return fmt.Errorf("badge at index %d has invalid tool_id: %w", i, err)
		}
		if badgesSeen[badge.ToolId] {
			return fmt.Errorf("duplicate badge for tool_id %s", badge.ToolId)
		}
		badgesSeen[badge.ToolId] = true

		if badge.Tier == BadgeTier_BADGE_TIER_UNSPECIFIED {
			return fmt.Errorf("badge at index %d has unspecified tier", i)
		}
		if badge.CompositeScore > MaxScore {
			return fmt.Errorf("badge at index %d has invalid composite score %d", i, badge.CompositeScore)
		}
		if err := validateBadgeGenesisTimestamps(i, badge); err != nil {
			return err
		}
	}

	// Validate metric snapshots using the same keying invariant as
	// Keeper.RecordMetrics: one non-empty tool_id per stored latest snapshot.
	metricsSeen := make(map[string]bool)
	for i, snapshot := range gs.MetricSnapshots {
		if snapshot == nil {
			return fmt.Errorf("metric snapshot at index %d is nil", i)
		}
		toolID := snapshot.ToolId
		if err := validateToolID(toolID); err != nil {
			return fmt.Errorf("metric snapshot at index %d has invalid tool_id: %w", i, err)
		}
		if metricsSeen[toolID] {
			return fmt.Errorf("duplicate metric snapshot for tool_id %s", toolID)
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("metric snapshot %s timestamp", toolID), snapshot.Timestamp); err != nil {
			return err
		}
		metricsSeen[toolID] = true
	}

	// Validate badge events using the same keying invariant as the keeper's
	// composite event key: tool_id + timestamp + event_type + tier transition.
	eventsSeen := make(map[string]bool)
	for i, event := range gs.BadgeEvents {
		if event == nil {
			return fmt.Errorf("badge event at index %d is nil", i)
		}
		if err := validateToolID(event.ToolId); err != nil {
			return fmt.Errorf("badge event at index %d has invalid tool_id: %w", i, err)
		}
		if event.EventType == "" {
			return fmt.Errorf("badge event at index %d has empty event_type", i)
		}
		if event.Timestamp.IsZero() {
			return fmt.Errorf("badge event at index %d has no timestamp", i)
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("badge event %s timestamp", event.ToolId), event.Timestamp); err != nil {
			return err
		}
		key := fmt.Sprintf("%s-%d-%s-%d-%d", event.ToolId, event.Timestamp.UnixNano(), event.EventType, int32(event.PreviousTier), int32(event.NewTier))
		if eventsSeen[key] {
			return fmt.Errorf("duplicate badge event for key %s", key)
		}
		eventsSeen[key] = true
	}

	return nil
}

func validateBadgeGenesisTimestamps(index int, badge *Badge) error {
	prefix := fmt.Sprintf("badge %s at index %d", badge.ToolId, index)
	if err := validateGenesisTimestamp(prefix+" awarded_at", badge.AwardedAt); err != nil {
		return err
	}
	if err := validateGenesisTimestamp(prefix+" expires_at", badge.ExpiresAt); err != nil {
		return err
	}
	if err := validateGenesisTimestamp(prefix+" last_evaluated_at", badge.LastEvaluatedAt); err != nil {
		return err
	}
	return validateGenesisTimestamp(prefix+" grace_period_ends_at", badge.GracePeriodEndsAt)
}

func validateGenesisTimestamp(field string, ts time.Time) error {
	// A value time.Time is always well-formed; the zero value is treated as
	// "unset", which is permitted in genesis.
	_ = field
	_ = ts
	return nil
}

// NewGenesisState creates a new genesis state with the provided parameters.
func NewGenesisState(
	params *Params,
	tierConfigs []*TierConfig,
	badges []*Badge,
	metricSnapshots []*MetricSnapshot,
	badgeEvents []*BadgeEvent,
) *GenesisState {
	return &GenesisState{
		Params:          params,
		TierConfigs:     tierConfigs,
		Badges:          badges,
		MetricSnapshots: metricSnapshots,
		BadgeEvents:     badgeEvents,
	}
}
