
package types

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	params := DefaultParams()
	return &GenesisState{
		Params:               params,
		ToolCards:            []*ToolCard{},
		BondRecords:          []*BondRecord{},
		Receipts:             []*UsageReceipt{},
		Challenges:           []*Challenge{},
		Settlements:          []*SettlementRecord{},
		RoyaltyTotals:        []*RoyaltyTotals{},
		EconomicsMetrics:     &EconomicsMetrics{},
		BundleAnchors:        []*BundleAnchor{},
		LaneRegistry:         []*LaneRegistryEntry{},
		Watchers:             []*WatcherRecord{},
		SloProbeReceipts:     []*SLOProbeReceipt{},
		SloProbeAggregates:   []*SLOProbeAggregate{},
		OriginRoutingConfigs: []*OriginRoutingConfig{},
		ToolMetrics:          []*ToolMetrics{},
		ActivationSet:        []string{},
		ActivationHistory:    []*ActivationRecord{},
		SlaTemplates:         map[string]string{},
		DisputeTerms:         map[string]string{},
		QueuedReceipts:       []*QueuedReceipt{},
	}
}

// Validate performs basic genesis state validation. It intentionally uses a
// deterministic epoch reference for time-dependent tool-card checks because
// AppModuleBasic.ValidateGenesis does not receive the chain genesis time.
func (gs *GenesisState) Validate() error {
	return gs.ValidateAtTime(time.Unix(0, 0).UTC())
}

// ValidateAtTime performs genesis validation using the supplied reference time
// for checks that depend on chain time. Keeper.InitGenesis passes ctx.BlockTime
// so imported state cannot bypass active tool-card invariants.
func (gs *GenesisState) ValidateAtTime(now time.Time) error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}

	if gs.Params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// Check for duplicate tool IDs
	toolIDs := make(map[string]bool)
	for i, tool := range gs.ToolCards {
		if tool == nil {
			return fmt.Errorf("tool entry cannot be nil")
		}
		toolID, err := validateGenesisToolID(i, tool.ToolId)
		if err != nil {
			return err
		}
		if err := validateToolCardGenesisTimestamps(tool); err != nil {
			return err
		}
		if toolIDs[toolID] {
			return fmt.Errorf("duplicate tool ID: %s", toolID)
		}
		toolIDs[toolID] = true
	}
	for i, tool := range gs.ToolCards {
		if err := tool.ValidateAtTime(now); err != nil {
			return fmt.Errorf("invalid tool card %s at index %d: %w", tool.ToolId, i, err)
		}
	}

	// Check that bond records reference existing tools
	for _, bond := range gs.BondRecords {
		if bond == nil {
			return fmt.Errorf("bond entry cannot be nil")
		}
		if !toolIDs[bond.ToolId] {
			return fmt.Errorf("bond references non-existent tool: %s", bond.ToolId)
		}
		if err := bond.Validate(); err != nil {
			return fmt.Errorf("invalid bond record: %w", err)
		}
		if err := validateBondGenesisTimestamps(bond); err != nil {
			return err
		}
	}

	// Check that receipts reference existing tools
	receiptIDs := make(map[string]bool)
	for _, receipt := range gs.Receipts {
		if receipt == nil {
			return fmt.Errorf("receipt entry cannot be nil")
		}
		if !toolIDs[receipt.ToolId] {
			return fmt.Errorf("receipt references non-existent tool: %s", receipt.ToolId)
		}
		if err := receipt.Validate(); err != nil {
			return fmt.Errorf("invalid receipt: %w", err)
		}
		if receiptIDs[receipt.ReceiptId] {
			return fmt.Errorf("duplicate receipt ID: %s", receipt.ReceiptId)
		}
		receiptIDs[receipt.ReceiptId] = true
	}

	// Check that challenges reference existing receipts and no duplicates
	challengeIDs := make(map[string]bool)
	for _, challenge := range gs.Challenges {
		if challenge == nil {
			return fmt.Errorf("challenge entry cannot be nil")
		}
		if !receiptIDs[challenge.ReceiptId] {
			return fmt.Errorf("challenge references non-existent receipt: %s", challenge.ReceiptId)
		}
		if err := challenge.Validate(); err != nil {
			return fmt.Errorf("invalid challenge: %w", err)
		}
		if err := validateRegistryGenesisTimestamp(fmt.Sprintf("challenge %s resolved_at", challenge.ChallengeId), challenge.ResolvedAt); err != nil {
			return err
		}
		if challenge.ResolvedAt != nil && challenge.ResolvedAt.AsTime().Before(challenge.ChallengedAt.AsTime()) {
			return fmt.Errorf("challenge %s resolved_at must be at or after challenged_at", challenge.ChallengeId)
		}
		if challengeIDs[challenge.ChallengeId] {
			return fmt.Errorf("duplicate challenge ID: %s", challenge.ChallengeId)
		}
		challengeIDs[challenge.ChallengeId] = true
	}

	// Check that settlements reference existing receipts and no duplicates
	settlementIDs := make(map[string]bool)
	for _, settlement := range gs.Settlements {
		if settlement == nil {
			return fmt.Errorf("settlement entry cannot be nil")
		}
		if !receiptIDs[settlement.ReceiptId] {
			return fmt.Errorf("settlement references non-existent receipt: %s", settlement.ReceiptId)
		}
		if err := settlement.Validate(); err != nil {
			return fmt.Errorf("invalid settlement: %w", err)
		}
		if settlementIDs[settlement.ReceiptId] {
			return fmt.Errorf("duplicate settlement for receipt: %s", settlement.ReceiptId)
		}
		settlementIDs[settlement.ReceiptId] = true
	}

	toolIDSet := make(map[string]bool)
	for _, tool := range gs.ToolCards {
		toolIDSet[tool.ToolId] = true
	}
	for _, royalty := range gs.RoyaltyTotals {
		if royalty == nil {
			return fmt.Errorf("royalty entry cannot be nil")
		}
		if !toolIDSet[royalty.ToolId] {
			return fmt.Errorf("royalty totals reference unknown tool: %s", royalty.ToolId)
		}
	}

	anchorKeys := make(map[string]bool)
	for _, anchor := range gs.BundleAnchors {
		if anchor == nil {
			return fmt.Errorf("bundle anchor entry cannot be nil")
		}
		chainID := strings.ToLower(strings.TrimSpace(anchor.ChainId))
		if chainID == "" {
			return fmt.Errorf("bundle anchor chain_id cannot be empty")
		}
		if len(anchor.MerkleRoot) != 32 {
			return fmt.Errorf("bundle anchor merkle_root must be 32 bytes")
		}
		if err := validateRegistryGenesisTimestamp(fmt.Sprintf("bundle anchor %s anchored_at", chainID), anchor.AnchoredAt); err != nil {
			return err
		}
		key := chainID + ":" + hex.EncodeToString(anchor.MerkleRoot)
		if anchorKeys[key] {
			return fmt.Errorf("duplicate bundle anchor: %s", key)
		}
		anchorKeys[key] = true
	}

	laneIDs := make(map[string]bool)
	for _, lane := range gs.LaneRegistry {
		if lane == nil {
			return fmt.Errorf("lane registry entry cannot be nil")
		}
		if laneIDs[lane.LaneId] {
			return fmt.Errorf("duplicate lane ID: %s", lane.LaneId)
		}
		laneIDs[lane.LaneId] = true
		if err := lane.Validate(); err != nil {
			return fmt.Errorf("invalid lane registry entry %s: %w", lane.LaneId, err)
		}
	}

	capsuleIDs := make(map[string]bool)
	for _, capsule := range gs.ToolCapsules {
		if capsule == nil {
			return fmt.Errorf("tool capsule entry cannot be nil")
		}
		if capsuleIDs[capsule.ToolId] {
			return fmt.Errorf("duplicate tool capsule for tool: %s", capsule.ToolId)
		}
		capsuleIDs[capsule.ToolId] = true
		if !toolIDSet[capsule.ToolId] {
			return fmt.Errorf("tool capsule references unknown tool: %s", capsule.ToolId)
		}
		if err := capsule.Validate(); err != nil {
			return fmt.Errorf("invalid tool capsule %s: %w", capsule.ToolId, err)
		}
	}

	watcherIDs := make(map[string]bool)
	for _, watcher := range gs.Watchers {
		if watcher == nil {
			return fmt.Errorf("watcher entry cannot be nil")
		}
		if watcherIDs[watcher.WatcherAddress] {
			return fmt.Errorf("duplicate watcher: %s", watcher.WatcherAddress)
		}
		watcherIDs[watcher.WatcherAddress] = true
		if err := watcher.Validate(); err != nil {
			return fmt.Errorf("invalid watcher %s: %w", watcher.WatcherAddress, err)
		}
	}

	probeIDs := make(map[string]bool)
	for _, probe := range gs.SloProbeReceipts {
		if probe == nil {
			return fmt.Errorf("slo probe receipt cannot be nil")
		}
		if probeIDs[probe.ReceiptId] {
			return fmt.Errorf("duplicate slo probe receipt: %s", probe.ReceiptId)
		}
		probeIDs[probe.ReceiptId] = true
		if !toolIDSet[probe.ToolId] {
			return fmt.Errorf("slo probe references unknown tool: %s", probe.ToolId)
		}
		if err := probe.Validate(); err != nil {
			return fmt.Errorf("invalid slo probe receipt %s: %w", probe.ReceiptId, err)
		}
	}

	aggregateKeys := make(map[string]bool)
	for _, aggregate := range gs.SloProbeAggregates {
		if aggregate == nil {
			return fmt.Errorf("slo probe aggregate cannot be nil")
		}
		if !toolIDSet[aggregate.ToolId] {
			return fmt.Errorf("slo probe aggregate references unknown tool: %s", aggregate.ToolId)
		}
		if err := aggregate.Validate(); err != nil {
			return fmt.Errorf("invalid slo probe aggregate %s: %w", aggregate.ToolId, err)
		}
		windowEnd := ""
		if aggregate.WindowEnd != nil {
			windowEnd = aggregate.WindowEnd.AsTime().UTC().Format(time.RFC3339Nano)
		}
		key := fmt.Sprintf("%s:%s:v%d", aggregate.ToolId, windowEnd, aggregate.Version)
		if aggregateKeys[key] {
			return fmt.Errorf("duplicate slo probe aggregate: %s", key)
		}
		aggregateKeys[key] = true
	}

	originIDs := make(map[string]bool)
	for _, orc := range gs.OriginRoutingConfigs {
		if orc == nil {
			return fmt.Errorf("origin routing config cannot be nil")
		}
		if orc.OriginId == "" {
			return fmt.Errorf("origin routing config missing origin_id")
		}
		if originIDs[orc.OriginId] {
			return fmt.Errorf("duplicate origin routing config: %s", orc.OriginId)
		}
		originIDs[orc.OriginId] = true
		if orc.Splits == nil {
			return fmt.Errorf("origin routing config %s: splits cannot be nil", orc.OriginId)
		}
		if err := validateOriginRoutingGenesisSplits(orc); err != nil {
			return err
		}
		if err := validateOriginRoutingGenesisTimestamps(orc); err != nil {
			return err
		}
	}

	if err := validateEconomicsMetricsGenesisTimestamps(gs.EconomicsMetrics); err != nil {
		return err
	}

	metricsIDs := make(map[string]bool)
	for _, m := range gs.ToolMetrics {
		if m == nil {
			return fmt.Errorf("tool metrics entry cannot be nil")
		}
		if m.ToolId == "" {
			return fmt.Errorf("tool metrics entry missing tool_id")
		}
		if metricsIDs[m.ToolId] {
			return fmt.Errorf("duplicate tool metrics for: %s", m.ToolId)
		}
		metricsIDs[m.ToolId] = true
		if err := validateToolMetricsGenesisTimestamps(m); err != nil {
			return err
		}
	}

	activationIDs := make(map[string]bool)
	for _, id := range gs.ActivationSet {
		if id == "" {
			return fmt.Errorf("activation set entry cannot be empty")
		}
		if activationIDs[id] {
			return fmt.Errorf("duplicate activation set entry: %s", id)
		}
		activationIDs[id] = true
	}

	for _, record := range gs.ActivationHistory {
		if record == nil {
			return fmt.Errorf("activation history entry cannot be nil")
		}
		if record.ToolId == "" {
			return fmt.Errorf("activation history entry missing tool_id")
		}
		if err := validateRegistryGenesisTimestamp(fmt.Sprintf("activation history %s timestamp", record.ToolId), record.Timestamp); err != nil {
			return err
		}
	}

	for _, royalty := range gs.RoyaltyTotals {
		if royalty == nil {
			continue
		}
		if err := validateRegistryGenesisTimestamp(fmt.Sprintf("royalty totals %s updated_at", royalty.ToolId), royalty.UpdatedAt); err != nil {
			return err
		}
	}

	queuedReceiptIDs := make(map[string]bool)
	for _, qr := range gs.QueuedReceipts {
		if qr == nil {
			return fmt.Errorf("queued receipt cannot be nil")
		}
		if qr.Receipt == nil {
			return fmt.Errorf("queued receipt missing receipt")
		}
		receipt := qr.Receipt
		if err := receipt.Validate(); err != nil {
			return fmt.Errorf("invalid queued receipt %s: %w", receipt.ReceiptId, err)
		}
		if !toolIDs[receipt.ToolId] {
			return fmt.Errorf("queued receipt references non-existent tool: %s", receipt.ToolId)
		}
		if err := validateQueuedReceiptStatus(qr.Status); err != nil {
			return fmt.Errorf("invalid queued receipt %s: %w", receipt.ReceiptId, err)
		}
		if queuedReceiptNeedsReadyAt(qr.Status) && qr.ReadyAt == nil {
			return fmt.Errorf("queued receipt %s missing ready_at", receipt.ReceiptId)
		}
		if qr.QueuedAt != nil {
			if err := qr.QueuedAt.CheckValid(); err != nil {
				return fmt.Errorf("queued receipt %s invalid queued_at: %w", receipt.ReceiptId, err)
			}
		}
		if qr.ReadyAt != nil {
			if err := qr.ReadyAt.CheckValid(); err != nil {
				return fmt.Errorf("queued receipt %s invalid ready_at: %w", receipt.ReceiptId, err)
			}
		}
		if qr.ProcessedAt != nil {
			if err := qr.ProcessedAt.CheckValid(); err != nil {
				return fmt.Errorf("queued receipt %s invalid processed_at: %w", receipt.ReceiptId, err)
			}
		}
		if queuedReceiptIDs[receipt.ReceiptId] {
			return fmt.Errorf("duplicate queued receipt ID: %s", receipt.ReceiptId)
		}
		queuedReceiptIDs[receipt.ReceiptId] = true
	}

	return nil
}

func validateQueuedReceiptStatus(status string) error {
	if strings.TrimSpace(status) != status {
		return fmt.Errorf("invalid status: %s", status)
	}
	switch status {
	case "", "pending", "disputed", "ready", "failed", "settled":
		return nil
	default:
		return fmt.Errorf("invalid status: %s", status)
	}
}

func queuedReceiptNeedsReadyAt(status string) bool {
	switch status {
	case "", "pending", "ready":
		return true
	default:
		return false
	}
}

func validateGenesisToolID(index int, toolID string) (string, error) {
	if strings.TrimSpace(toolID) != toolID {
		return "", fmt.Errorf("tool entry at index %d has non-canonical tool_id %q", index, toolID)
	}
	if err := validateToolID(toolID); err != nil {
		return "", fmt.Errorf("tool entry at index %d has invalid tool_id: %w", index, err)
	}
	return toolID, nil
}

func validateToolCardGenesisTimestamps(tool *ToolCard) error {
	if err := validateRegistryGenesisTimestamp(fmt.Sprintf("tool card %s registered_at", tool.ToolId), tool.RegisteredAt); err != nil {
		return err
	}
	if err := validateRegistryGenesisTimestamp(fmt.Sprintf("tool card %s updated_at", tool.ToolId), tool.UpdatedAt); err != nil {
		return err
	}
	if tool.RegisteredAt != nil && tool.UpdatedAt != nil && tool.UpdatedAt.AsTime().Before(tool.RegisteredAt.AsTime()) {
		return fmt.Errorf("tool card %s updated_at must be at or after registered_at", tool.ToolId)
	}
	return nil
}

func validateBondGenesisTimestamps(bond *BondRecord) error {
	if err := validateRegistryGenesisTimestamp(fmt.Sprintf("bond %s withdraw_initiated_at", bond.ToolId), bond.WithdrawInitiatedAt); err != nil {
		return err
	}
	for i, slash := range bond.PendingSlashes {
		if slash == nil {
			return fmt.Errorf("bond %s pending_slashes[%d] cannot be nil", bond.ToolId, i)
		}
		prefix := fmt.Sprintf("bond %s pending_slashes[%d]", bond.ToolId, i)
		if err := validateRegistryGenesisTimestamp(prefix+" proposed_at", slash.ProposedAt); err != nil {
			return err
		}
		if err := validateRegistryGenesisTimestamp(prefix+" execute_at", slash.ExecuteAt); err != nil {
			return err
		}
	}
	return nil
}

func validateEconomicsMetricsGenesisTimestamps(metrics *EconomicsMetrics) error {
	if metrics == nil {
		return nil
	}
	return validateRegistryGenesisTimestamp("economics_metrics last_updated", metrics.LastUpdated)
}

func validateOriginRoutingGenesisTimestamps(config *OriginRoutingConfig) error {
	if err := validateRegistryGenesisTimestamp(fmt.Sprintf("origin routing config %s created_at", config.OriginId), config.CreatedAt); err != nil {
		return err
	}
	return validateRegistryGenesisTimestamp(fmt.Sprintf("origin routing config %s updated_at", config.OriginId), config.UpdatedAt)
}

func validateOriginRoutingGenesisSplits(config *OriginRoutingConfig) error {
	splits := config.Splits
	sum := splits.DirectBps + splits.BuybackBps + splits.TreasuryBps + splits.InsuranceBps
	if sum != uint32(BPSDenominator) {
		return fmt.Errorf("origin routing config %s: splits must sum to %d, got %d", config.OriginId, BPSDenominator, sum)
	}
	if splits.DirectBps < OriginRoutingMinDirectBps {
		return fmt.Errorf(
			"origin routing config %s: direct_bps must be >= %d, got %d",
			config.OriginId,
			OriginRoutingMinDirectBps,
			splits.DirectBps,
		)
	}
	if splits.BuybackBps > OriginRoutingMaxBuybackBps {
		return fmt.Errorf(
			"origin routing config %s: buyback_bps must be <= %d, got %d",
			config.OriginId,
			OriginRoutingMaxBuybackBps,
			splits.BuybackBps,
		)
	}
	return nil
}

func validateToolMetricsGenesisTimestamps(metrics *ToolMetrics) error {
	if err := validateRegistryGenesisTimestamp(fmt.Sprintf("tool metrics %s last_updated", metrics.ToolId), metrics.LastUpdated); err != nil {
		return err
	}
	if err := validateRegistryGenesisTimestamp(fmt.Sprintf("tool metrics %s quality_score_last_updated", metrics.ToolId), metrics.QualityScoreLastUpdated); err != nil {
		return err
	}
	for i, stat := range metrics.DailyStats {
		if stat == nil {
			return fmt.Errorf("tool metrics %s daily_stats[%d] cannot be nil", metrics.ToolId, i)
		}
		if err := validateRegistryGenesisTimestamp(fmt.Sprintf("tool metrics %s daily_stats[%d].date", metrics.ToolId, i), stat.Date); err != nil {
			return err
		}
	}
	return nil
}

func validateRegistryGenesisTimestamp(field string, ts *timestamppb.Timestamp) error {
	if ts == nil {
		return nil
	}
	if err := ts.CheckValid(); err != nil {
		return fmt.Errorf("%s is invalid: %w", field, err)
	}
	return nil
}
