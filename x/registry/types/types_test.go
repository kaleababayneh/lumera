//go:build cosmos

package types

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// ---------------------------------------------------------------------------
// Keys & constants
// ---------------------------------------------------------------------------

func TestModuleConstants(t *testing.T) {
	t.Parallel()
	if ModuleName != "registry" {
		t.Errorf("ModuleName = %q, want %q", ModuleName, "registry")
	}
	if StoreKey != ModuleName {
		t.Errorf("StoreKey = %q, want %q", StoreKey, ModuleName)
	}
	if RouterKey != ModuleName {
		t.Errorf("RouterKey = %q, want %q", RouterKey, ModuleName)
	}
	if QuerierRoute != ModuleName {
		t.Errorf("QuerierRoute = %q, want %q", QuerierRoute, ModuleName)
	}
	if MemStoreKey != "mem_registry" {
		t.Errorf("MemStoreKey = %q, want %q", MemStoreKey, "mem_registry")
	}
}

func TestKeyPrefixesUnique(t *testing.T) {
	t.Parallel()
	prefixes := [][]byte{
		ToolCardPrefix, ToolCapsulePrefix, BondRecordPrefix,
		ReceiptPrefix, SettlementPrefix, RoyaltyTotalsPrefix,
		MetricsPrefix, ChallengePrefix, ParamsKey,
	}
	seen := make(map[byte]struct{})
	for _, p := range prefixes {
		if len(p) != 1 {
			t.Fatalf("prefix %v should be length 1", p)
		}
		if _, ok := seen[p[0]]; ok {
			t.Errorf("duplicate prefix byte 0x%02x", p[0])
		}
		seen[p[0]] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	errs := []error{
		ErrInvalidAmount, ErrInvalidBPS, ErrInvalidSplits,
		ErrInvalidDuration, ErrToolNotFound, ErrToolExists,
		ErrInsufficientBond, ErrPendingReceipts, ErrActiveChallenges,
		ErrInvalidSignature, ErrQuoteExceeded, ErrDisputeExpired,
		ErrInsufficientStake, ErrReceiptNotFound, ErrDisputeActive,
		ErrChallengeActive, ErrBondNotFound, ErrUnauthorized,
		ErrReceiptExists, ErrDuplicateReceipt, ErrChallengeNotFound,
		ErrInvalidState, ErrSBOMMissing, ErrSBOMStale,
		ErrAttestationExpired, ErrAttestationMissing, ErrSchemaMissing,
		ErrAttestationRootMissing, ErrMaxToolsReached, ErrCategoryLimit,
		ErrWithdrawCooldown, ErrUnauthorizedFieldUpdate, ErrAttestationMismatch,
		ErrSchemaHashMismatch, ErrWatcherExists, ErrWatcherNotFound,
		ErrWatcherInactive,
	}
	for _, e := range errs {
		if e == nil {
			t.Error("sentinel error should not be nil")
		}
	}

	type coder interface{ ABCICode() uint32 }
	codes := make(map[uint32]string)
	for _, e := range errs {
		c, ok := e.(coder)
		if !ok {
			continue
		}
		code := c.ABCICode()
		if prev, dup := codes[code]; dup {
			t.Errorf("duplicate error code %d: %q and %q", code, prev, e.Error())
		}
		codes[code] = e.Error()
	}
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

func TestEventTypesNonEmpty(t *testing.T) {
	t.Parallel()
	events := []string{
		EventTypeToolRegistered, EventTypeToolUpdated, EventTypeToolDelisted,
		EventTypeBondCreated, EventTypeBondToppedUp, EventTypeBondWithdrawn,
		EventTypeBondLocked, EventTypeBondUnlocked, EventTypeReceiptSubmitted,
		EventTypeReceiptChallenged, EventTypeSettlement, EventTypeSlash,
		EventTypeChallengeResolved, EventTypeParamsUpdated,
		EventTypeReceiptQueued, EventTypeReceiptSettled, EventTypeReceiptFailed,
		EventTypeFinalizeBlockSettlementReceipt,
		EventTypeQualityScore, EventTypeQualityRebate,
		EventTypeGovernanceParamChange,
		EventTypeVerifiedStatusChanged,
		EventTypeWatcherRegistered, EventTypeWatcherUnregistered,
		EventTypeWatcherSlashed, EventTypeSLOProbeSubmitted,
		EventTypeSLOProbeAggregated,
	}
	seen := make(map[string]struct{})
	for _, et := range events {
		if et == "" {
			t.Error("event type should not be empty")
		}
		if _, ok := seen[et]; ok {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = struct{}{}
	}
}

func TestAttributeKeysNonEmpty(t *testing.T) {
	t.Parallel()
	attrs := []string{
		AttributeKeyToolID, AttributeKeyOwner, AttributeKeyVersion,
		AttributeKeyBond, AttributeKeySchemaHash, AttributeKeyReceiptID,
		AttributeKeyCost, AttributeKeyChallenger, AttributeKeyBurn,
		AttributeKeyInsurance, AttributeKeySeverity, AttributeKeyAmount,
		AttributeKeyResolution, AttributeKeyRefund, AttributeKeyStatus,
		AttributeKeyReadyAt, AttributeKeyError, AttributeKeyRetries,
		AttributeKeyProcessedAt, AttributeKeyReason, AttributeKeyEvidence,
		AttributeKeyBatchSize, AttributeKeyIdempotent, AttributeKeySource,
		AttributeKeyTimestamp, AttributeKeyBlockHeight, AttributeKeyNewTotal,
		AttributeKeyRemaining, AttributeKeyLockReason, AttributeKeyChallengeID,
		AttributeKeyStake, AttributeKeyOriginTool, AttributeKeyCacheHit,
		AttributeKeyCacheRoyalty, AttributeKeyPolicy,
		AttributeKeySplitPublisherOrigin, AttributeKeySplitPublisherServing,
		AttributeKeyRouterGross, AttributeKeyRouterNet, AttributeKeyReferrer,
		AttributeKeyRebate, AttributeKeyActual, AttributeKeyLockedQuote,
		AttributeKeyCategories, AttributeKeyUserAddress,
		AttributeKeyRouterAddress, AttributeKeyAction, AttributeKeyActor,
		AttributeKeyTarget, AttributeKeyDetails, AttributeKeyEntityType,
		AttributeKeyEntityID, AttributeKeyFromState, AttributeKeyToState,
		AttributeKeyQualityScore, AttributeKeyRebateTier,
		AttributeKeyRebateBps, AttributeKeyRebateAmount,
		AttributeKeyWatcher, AttributeKeyWindowStart, AttributeKeyWindowEnd,
		AttributeKeyMedianP95, AttributeKeyMedianAvailBps,
		AttributeKeyMedianErrBps,
		AttributeKeyGovernanceAuthority, AttributeKeyGovernanceModule,
		AttributeKeyParamChanges,
	}
	seen := make(map[string]struct{})
	for _, a := range attrs {
		if a == "" {
			t.Error("attribute key should not be empty")
		}
		if _, ok := seen[a]; ok {
			t.Errorf("duplicate attribute key: %s", a)
		}
		seen[a] = struct{}{}
	}
}

func TestDefaultParamsDisputeWindowDefaults(t *testing.T) {
	t.Parallel()
	params := DefaultParams()
	if params == nil {
		t.Fatal("DefaultParams() returned nil")
	}
	if params.DisputeWindowSeconds != DefaultDisputeWindowSeconds {
		t.Fatalf("DisputeWindowSeconds = %d, want %d", params.DisputeWindowSeconds, DefaultDisputeWindowSeconds)
	}
}

// ---------------------------------------------------------------------------
// DefaultGenesis
// ---------------------------------------------------------------------------

func TestDefaultGenesis(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	if gs == nil {
		t.Fatal("DefaultGenesis() returned nil")
	}
	if gs.Params == nil {
		t.Fatal("Params should not be nil")
	}
	if gs.ToolCards == nil {
		t.Fatal("ToolCards should not be nil")
	}
	if len(gs.ToolCards) != 0 {
		t.Errorf("len(ToolCards) = %d, want 0", len(gs.ToolCards))
	}
	if gs.BondRecords == nil {
		t.Fatal("BondRecords should not be nil")
	}
	if gs.Receipts == nil {
		t.Fatal("Receipts should not be nil")
	}
	if gs.Challenges == nil {
		t.Fatal("Challenges should not be nil")
	}
	if gs.Settlements == nil {
		t.Fatal("Settlements should not be nil")
	}
	if gs.RoyaltyTotals == nil {
		t.Fatal("RoyaltyTotals should not be nil")
	}
	if gs.EconomicsMetrics == nil {
		t.Fatal("EconomicsMetrics should not be nil")
	}
	if gs.BundleAnchors == nil {
		t.Fatal("BundleAnchors should not be nil")
	}
	if gs.LaneRegistry == nil {
		t.Fatal("LaneRegistry should not be nil")
	}
	if gs.Watchers == nil {
		t.Fatal("Watchers should not be nil")
	}
	if gs.SloProbeReceipts == nil {
		t.Fatal("SloProbeReceipts should not be nil")
	}
	if gs.SloProbeAggregates == nil {
		t.Fatal("SloProbeAggregates should not be nil")
	}
}

func TestDefaultGenesis_Validate(t *testing.T) {
	t.Parallel()
	if err := DefaultGenesis().Validate(); err != nil {
		t.Fatalf("default genesis should be valid: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GenesisState.Validate
// ---------------------------------------------------------------------------

func TestGenesisState_Validate_Nil(t *testing.T) {
	t.Parallel()
	var gs *GenesisState
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil genesis state")
	}
}

func TestGenesisState_Validate_NilParams(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.Params = nil
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil params")
	}
}

func validGenesisToolCard(t *testing.T, toolID string) *ToolCard {
	t.Helper()
	tool := validToolCard(t)
	tool.ToolId = toolID
	return tool
}

func TestGenesisState_Validate_NilToolEntry(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.ToolCards = []*ToolCard{nil}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil tool entry")
	}
}

func TestGenesisState_Validate_InvalidToolID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		toolID  string
		wantErr string
	}{
		{
			name:    "empty",
			toolID:  "",
			wantErr: "invalid tool_id",
		},
		{
			name:    "surrounding whitespace",
			toolID:  " tool-1",
			wantErr: "non-canonical tool_id",
		},
		{
			name:    "uppercase",
			toolID:  "Tool-1",
			wantErr: "invalid tool_id",
		},
		{
			name:    "slash",
			toolID:  "tool/1",
			wantErr: "invalid tool_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gs := DefaultGenesis()
			gs.ToolCards = []*ToolCard{{ToolId: tt.toolID}}
			err := gs.Validate()
			if err == nil {
				t.Fatal("expected error for invalid tool_id")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q should contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestGenesisState_Validate_RejectsMalformedToolCard(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	tool := validGenesisToolCard(t, "tool-1")
	tool.Owner = ""
	gs.ToolCards = []*ToolCard{tool}

	err := gs.Validate()
	if err == nil {
		t.Fatal("expected malformed tool card to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid tool card tool-1") {
		t.Fatalf("error = %q, want tool-card context", err.Error())
	}
	if !strings.Contains(err.Error(), "owner cannot be empty") {
		t.Fatalf("error = %q, want owner validation error", err.Error())
	}
}

func TestGenesisState_ValidateAtTime_RejectsExpiredLicensedResaleTool(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	tool := validGenesisToolCard(t, "tool-1")
	tool.LicenseLane = LicenseLaneLicensedResale
	tool.Metadata = map[string]string{
		"license_reference":           "https://license.example.com/tool-1",
		"license_expires_at":          now.Add(-time.Second).Format(time.RFC3339),
		"license_governance_status":   "approved",
		"license_governance_proposal": "https://governance.example.com/proposals/1",
	}
	gs := DefaultGenesis()
	gs.ToolCards = []*ToolCard{tool}

	err := gs.ValidateAtTime(now)
	if err == nil {
		t.Fatal("expected expired licensed resale tool to be rejected")
	}
	if !strings.Contains(err.Error(), "license has expired") {
		t.Fatalf("error = %q, want license expiry validation error", err.Error())
	}
}

func TestGenesisState_Validate_DuplicateToolID(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.ToolCards = []*ToolCard{
		{ToolId: "tool-1"},
		{ToolId: "tool-1"},
	}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for duplicate tool ID")
	}
}

func TestGenesisState_Validate_NilBondEntry(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.ToolCards = []*ToolCard{validGenesisToolCard(t, "tool-1")}
	gs.BondRecords = []*BondRecord{nil}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil bond entry")
	}
}

func TestGenesisState_Validate_BondReferencesNonExistentTool(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.BondRecords = []*BondRecord{{ToolId: "nonexistent"}}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for bond referencing non-existent tool")
	}
}

func TestGenesisState_Validate_NilRoyaltyEntry(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.RoyaltyTotals = []*RoyaltyTotals{nil}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil royalty entry")
	}
}

func TestGenesisState_Validate_NilLaneEntry(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.LaneRegistry = []*LaneRegistryEntry{nil}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil lane entry")
	}
}

func TestGenesisState_Validate_DuplicateLaneID(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.LaneRegistry = []*LaneRegistryEntry{
		{LaneId: "lane-1", LaneType: "public"},
		{LaneId: "lane-1", LaneType: "public"},
	}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for duplicate lane ID")
	}
}

func TestGenesisState_Validate_NilWatcherEntry(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	gs.Watchers = []*WatcherRecord{nil}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil watcher entry")
	}
}

func TestGenesisState_Validate_QueuedReceipt(t *testing.T) {
	t.Parallel()
	gs := genesisWithQueuedReceipt(t)
	if err := gs.Validate(); err != nil {
		t.Fatalf("queued receipt genesis should be valid: %v", err)
	}
}

func TestGenesisState_Validate_InvalidQueuedReceipts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*GenesisState)
		wantErr string
	}{
		{
			name: "missing receipt",
			mutate: func(gs *GenesisState) {
				gs.QueuedReceipts[0].Receipt = nil
			},
			wantErr: "queued receipt missing receipt",
		},
		{
			name: "duplicate receipt id",
			mutate: func(gs *GenesisState) {
				gs.QueuedReceipts = append(gs.QueuedReceipts, gs.QueuedReceipts[0])
			},
			wantErr: "duplicate queued receipt ID",
		},
		{
			name: "unknown tool",
			mutate: func(gs *GenesisState) {
				gs.ToolCards = []*ToolCard{validGenesisToolCard(t, "other-tool")}
			},
			wantErr: "queued receipt references non-existent tool",
		},
		{
			name: "invalid receipt payload",
			mutate: func(gs *GenesisState) {
				gs.QueuedReceipts[0].Receipt.RequestId = ""
			},
			wantErr: "invalid queued receipt",
		},
		{
			name: "invalid queue status",
			mutate: func(gs *GenesisState) {
				gs.QueuedReceipts[0].Status = "processing"
			},
			wantErr: "invalid status",
		},
		{
			name: "pending without ready time",
			mutate: func(gs *GenesisState) {
				gs.QueuedReceipts[0].ReadyAt = nil
			},
			wantErr: "missing ready_at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gs := genesisWithQueuedReceipt(t)
			tt.mutate(gs)
			err := gs.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestGenesisState_Validate_RejectsOriginRoutingSplitBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		splits  *OriginRoutingSplits
		wantErr string
	}{
		{
			name: "direct below governance floor",
			splits: &OriginRoutingSplits{
				DirectBps:    OriginRoutingMinDirectBps - 1,
				BuybackBps:   OriginRoutingMaxBuybackBps,
				TreasuryBps:  1001,
				InsuranceBps: 1000,
			},
			wantErr: "direct_bps must be >=",
		},
		{
			name: "buyback above cap",
			splits: &OriginRoutingSplits{
				DirectBps:    OriginRoutingMinDirectBps,
				BuybackBps:   OriginRoutingMaxBuybackBps + 1,
				TreasuryBps:  999,
				InsuranceBps: 1000,
			},
			wantErr: "buyback_bps must be <=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gs := DefaultGenesis()
			gs.OriginRoutingConfigs = []*OriginRoutingConfig{{
				OriginId: "injective:iagent",
				Splits:   tt.splits,
			}}
			err := gs.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestGenesisState_Validate_RejectsInvalidOptionalTimestamps(t *testing.T) {
	t.Parallel()
	const toolID = "tool-1"

	tests := []struct {
		name    string
		mutate  func(*testing.T, *GenesisState)
		wantErr string
	}{
		{
			name: "tool card registered_at",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.ToolCards[0].RegisteredAt = invalidRegistryGenesisTimestamp()
			},
			wantErr: "registered_at is invalid",
		},
		{
			name: "tool card updated_at",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.ToolCards[0].UpdatedAt = invalidRegistryGenesisTimestamp()
			},
			wantErr: "updated_at is invalid",
		},
		{
			name: "bond withdraw_initiated_at",
			mutate: func(t *testing.T, gs *GenesisState) {
				bond := validBondRecord(t)
				bond.ToolId = toolID
				bond.WithdrawInitiatedAt = invalidRegistryGenesisTimestamp()
				gs.BondRecords = []*BondRecord{bond}
			},
			wantErr: "withdraw_initiated_at is invalid",
		},
		{
			name: "bond pending slash timestamp",
			mutate: func(t *testing.T, gs *GenesisState) {
				bond := validBondRecord(t)
				bond.ToolId = toolID
				bond.PendingSlashes = []*PendingSlash{{ProposedAt: invalidRegistryGenesisTimestamp()}}
				gs.BondRecords = []*BondRecord{bond}
			},
			wantErr: "pending_slashes[0] proposed_at is invalid",
		},
		{
			name: "bond pending slash nil",
			mutate: func(t *testing.T, gs *GenesisState) {
				bond := validBondRecord(t)
				bond.ToolId = toolID
				bond.PendingSlashes = []*PendingSlash{nil}
				gs.BondRecords = []*BondRecord{bond}
			},
			wantErr: "pending_slashes[0] cannot be nil",
		},
		{
			name: "challenge resolved_at",
			mutate: func(t *testing.T, gs *GenesisState) {
				receipt := validUsageReceipt(t)
				receipt.ToolId = toolID
				challenge := validChallenge(t)
				challenge.ResolvedAt = invalidRegistryGenesisTimestamp()
				gs.Receipts = []*UsageReceipt{receipt}
				gs.Challenges = []*Challenge{challenge}
			},
			wantErr: "resolved_at is invalid",
		},
		{
			name: "bundle anchor anchored_at",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.BundleAnchors = []*BundleAnchor{{
					ChainId:    "lumera",
					MerkleRoot: make([]byte, 32),
					AnchoredAt: invalidRegistryGenesisTimestamp(),
				}}
			},
			wantErr: "anchored_at is invalid",
		},
		{
			name: "origin routing created_at",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.OriginRoutingConfigs = []*OriginRoutingConfig{{
					OriginId:  "injective:iagent",
					Splits:    &OriginRoutingSplits{DirectBps: BPSDenominator},
					CreatedAt: invalidRegistryGenesisTimestamp(),
				}}
			},
			wantErr: "created_at is invalid",
		},
		{
			name: "origin routing updated_at",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.OriginRoutingConfigs = []*OriginRoutingConfig{{
					OriginId:  "injective:iagent",
					Splits:    &OriginRoutingSplits{DirectBps: BPSDenominator},
					UpdatedAt: invalidRegistryGenesisTimestamp(),
				}}
			},
			wantErr: "updated_at is invalid",
		},
		{
			name: "economics metrics last_updated",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.EconomicsMetrics.LastUpdated = invalidRegistryGenesisTimestamp()
			},
			wantErr: "economics_metrics last_updated is invalid",
		},
		{
			name: "tool metrics last_updated",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.ToolMetrics = []*ToolMetrics{{ToolId: toolID, LastUpdated: invalidRegistryGenesisTimestamp()}}
			},
			wantErr: "tool metrics tool-1 last_updated is invalid",
		},
		{
			name: "tool metrics quality_score_last_updated",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.ToolMetrics = []*ToolMetrics{{ToolId: toolID, QualityScoreLastUpdated: invalidRegistryGenesisTimestamp()}}
			},
			wantErr: "quality_score_last_updated is invalid",
		},
		{
			name: "tool metrics daily stat date",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.ToolMetrics = []*ToolMetrics{{ToolId: toolID, DailyStats: []*DailyStat{{Date: invalidRegistryGenesisTimestamp()}}}}
			},
			wantErr: "daily_stats[0].date is invalid",
		},
		{
			name: "tool metrics daily stat nil",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.ToolMetrics = []*ToolMetrics{{ToolId: toolID, DailyStats: []*DailyStat{nil}}}
			},
			wantErr: "daily_stats[0] cannot be nil",
		},
		{
			name: "activation history timestamp",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.ActivationHistory = []*ActivationRecord{{ToolId: toolID, Timestamp: invalidRegistryGenesisTimestamp()}}
			},
			wantErr: "activation history tool-1 timestamp is invalid",
		},
		{
			name: "royalty totals updated_at",
			mutate: func(_ *testing.T, gs *GenesisState) {
				gs.RoyaltyTotals = []*RoyaltyTotals{{ToolId: toolID, UpdatedAt: invalidRegistryGenesisTimestamp()}}
			},
			wantErr: "royalty totals tool-1 updated_at is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gs := DefaultGenesis()
			gs.ToolCards = []*ToolCard{validGenesisToolCard(t, toolID)}
			tt.mutate(t, gs)

			err := gs.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestGenesisState_Validate_ToolCardUpdatedAtMustNotPrecedeRegisteredAt(t *testing.T) {
	t.Parallel()

	registeredAt := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		updatedAt time.Time
		wantErr   string
	}{
		{
			name:      "before registered rejected",
			updatedAt: registeredAt.Add(-time.Second),
			wantErr:   "updated_at must be at or after registered_at",
		},
		{
			name:      "equal registered allowed",
			updatedAt: registeredAt,
		},
		{
			name:      "after registered allowed",
			updatedAt: registeredAt.Add(time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tool := validGenesisToolCard(t, "tool-1")
			tool.RegisteredAt = timestamppb.New(registeredAt)
			tool.UpdatedAt = timestamppb.New(tt.updatedAt)
			gs := DefaultGenesis()
			gs.ToolCards = []*ToolCard{tool}

			err := gs.Validate()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestGenesisState_Validate_ChallengeResolvedAtMustNotPrecedeChallengedAt(t *testing.T) {
	t.Parallel()

	challengedAt := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		resolvedAt time.Time
		wantErr    string
	}{
		{
			name:       "before challenged rejected",
			resolvedAt: challengedAt.Add(-time.Second),
			wantErr:    "resolved_at must be at or after challenged_at",
		},
		{
			name:       "equal challenged allowed",
			resolvedAt: challengedAt,
		},
		{
			name:       "after challenged allowed",
			resolvedAt: challengedAt.Add(time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			receipt := validUsageReceipt(t)
			challenge := validChallenge(t)
			challenge.ChallengedAt = timestamppb.New(challengedAt)
			challenge.DeadlineAt = timestamppb.New(challengedAt.Add(48 * time.Hour))
			challenge.ResolvedAt = timestamppb.New(tt.resolvedAt)

			gs := DefaultGenesis()
			gs.ToolCards = []*ToolCard{validGenesisToolCard(t, receipt.ToolId)}
			gs.Receipts = []*UsageReceipt{receipt}
			gs.Challenges = []*Challenge{challenge}

			err := gs.Validate()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func genesisWithQueuedReceipt(t *testing.T) *GenesisState {
	t.Helper()
	receipt := validUsageReceipt(t)
	gs := DefaultGenesis()
	gs.ToolCards = []*ToolCard{validGenesisToolCard(t, receipt.ToolId)}
	gs.QueuedReceipts = []*QueuedReceipt{
		{
			Receipt:  receipt,
			Status:   "pending",
			QueuedAt: receipt.Timestamp,
			ReadyAt:  receipt.ExpiresAt,
		},
	}
	return gs
}

func invalidRegistryGenesisTimestamp() *timestamppb.Timestamp {
	return &timestamppb.Timestamp{Nanos: 1_000_000_000}
}
