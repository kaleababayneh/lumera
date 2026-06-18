//go:build !cosmos

package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ActiveAgentToolFilter.Normalize
// ---------------------------------------------------------------------------

func TestActiveAgentToolFilter_Normalize_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	filter := ActiveAgentToolFilter{
		Capability:  "  vision  ",
		Category:    "\tdefi\t",
		Protocol:    " mcp+https ",
		LicenseLane: "  byo_key  ",
		Region:      " us-east-1 ",
	}

	normalized := filter.Normalize()

	assert.Equal(t, "vision", normalized.Capability)
	assert.Equal(t, "defi", normalized.Category)
	assert.Equal(t, "mcp+https", normalized.Protocol)
	assert.Equal(t, "byo_key", normalized.LicenseLane)
	assert.Equal(t, "us-east-1", normalized.Region)
}

func TestActiveAgentToolFilter_Normalize_PreservesBoolFields(t *testing.T) {
	t.Parallel()

	requiresEnclave := true
	filter := ActiveAgentToolFilter{
		RequiresEnclave: &requiresEnclave,
		VerifiedOnly:    true,
	}

	normalized := filter.Normalize()

	require.NotNil(t, normalized.RequiresEnclave)
	assert.True(t, *normalized.RequiresEnclave)
	assert.True(t, normalized.VerifiedOnly)
}

func TestActiveAgentToolFilter_Normalize_EmptyFields(t *testing.T) {
	t.Parallel()

	filter := ActiveAgentToolFilter{}
	normalized := filter.Normalize()

	assert.Empty(t, normalized.Capability)
	assert.Empty(t, normalized.Category)
	assert.Empty(t, normalized.Protocol)
	assert.Empty(t, normalized.LicenseLane)
	assert.Empty(t, normalized.Region)
	assert.Nil(t, normalized.RequiresEnclave)
	assert.False(t, normalized.VerifiedOnly)
}

// ---------------------------------------------------------------------------
// ActiveAgentListRequest.Normalize
// ---------------------------------------------------------------------------

func TestActiveAgentListRequest_Normalize_TrimsAndDefaultsLimit(t *testing.T) {
	t.Parallel()

	req := ActiveAgentListRequest{
		Cursor: "  ed448:abc  ",
		Status: " active ",
		ToolFilter: ActiveAgentToolFilter{
			Capability: "  vision ",
		},
	}

	normalized := req.Normalize()

	assert.Equal(t, "ed448:abc", normalized.Cursor)
	assert.Equal(t, "active", normalized.Status)
	assert.Equal(t, DefaultActiveAgentListLimit, normalized.Limit)
	assert.Equal(t, "vision", normalized.ToolFilter.Capability)
}

func TestActiveAgentListRequest_Normalize_PreservesExistingLimit(t *testing.T) {
	t.Parallel()

	req := ActiveAgentListRequest{
		Limit: 100,
	}

	normalized := req.Normalize()

	assert.Equal(t, uint64(100), normalized.Limit)
}

func TestActiveAgentListRequest_Normalize_PreservesIncludeTools(t *testing.T) {
	t.Parallel()

	req := ActiveAgentListRequest{
		IncludeTools: true,
	}

	normalized := req.Normalize()

	assert.True(t, normalized.IncludeTools)
}

// ---------------------------------------------------------------------------
// ActiveAgentListRequest.Validate
// ---------------------------------------------------------------------------

func TestActiveAgentListRequest_Validate_ZeroLimitError(t *testing.T) {
	t.Parallel()

	req := ActiveAgentListRequest{Limit: 0}
	err := req.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit must be > 0")
}

func TestActiveAgentListRequest_Validate_ExceedsMaxLimit(t *testing.T) {
	t.Parallel()

	req := ActiveAgentListRequest{Limit: MaxActiveAgentListLimit + 1}
	err := req.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit must be <=")
}

func TestActiveAgentListRequest_Validate_MaxLimitOk(t *testing.T) {
	t.Parallel()

	req := ActiveAgentListRequest{Limit: MaxActiveAgentListLimit}
	err := req.Validate()

	require.NoError(t, err)
}

func TestActiveAgentListRequest_Validate_ValidRequest(t *testing.T) {
	t.Parallel()

	req := ActiveAgentListRequest{Limit: 50}
	err := req.Validate()

	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// AgentHeartbeatRecord.FreshAt
// ---------------------------------------------------------------------------

func TestAgentHeartbeatRecord_FreshAt_Fresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	record := AgentHeartbeatRecord{
		AgentPubkey: "ed448:abc",
		ObservedAt:  now.Add(-30 * time.Second),
		ExpiresAt:   now.Add(30 * time.Second),
		Source:      "router_session",
	}

	assert.True(t, record.FreshAt(now))
}

func TestAgentHeartbeatRecord_FreshAt_Stale(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	record := AgentHeartbeatRecord{
		AgentPubkey: "ed448:abc",
		ExpiresAt:   now.Add(-1 * time.Second),
	}

	assert.False(t, record.FreshAt(now))
}

func TestAgentHeartbeatRecord_FreshAt_ExactExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	record := AgentHeartbeatRecord{
		ExpiresAt: now, // Exactly at now - not fresh
	}

	assert.False(t, record.FreshAt(now))
}

func TestAgentHeartbeatRecord_FreshAt_ZeroNow(t *testing.T) {
	t.Parallel()

	record := AgentHeartbeatRecord{
		ExpiresAt: time.Now().Add(time.Hour),
	}

	assert.False(t, record.FreshAt(time.Time{}))
}

func TestAgentHeartbeatRecord_FreshAt_ZeroExpiresAt(t *testing.T) {
	t.Parallel()

	record := AgentHeartbeatRecord{}
	now := time.Now()

	assert.False(t, record.FreshAt(now))
}

func TestAgentHeartbeatRecord_FreshAt_BothZero(t *testing.T) {
	t.Parallel()

	record := AgentHeartbeatRecord{}

	assert.False(t, record.FreshAt(time.Time{}))
}

// ---------------------------------------------------------------------------
// ActiveAgentPruneRequest.Normalize
// ---------------------------------------------------------------------------

func TestActiveAgentPruneRequest_Normalize_DefaultsAll(t *testing.T) {
	t.Parallel()

	req := ActiveAgentPruneRequest{}
	normalized := req.Normalize()

	assert.False(t, normalized.Now.IsZero())
	assert.Equal(t, DefaultActiveAgentHeartbeatRetention, normalized.Retention)
	assert.Equal(t, DefaultActiveAgentListLimit, normalized.Limit)
}

func TestActiveAgentPruneRequest_Normalize_PreservesNow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	req := ActiveAgentPruneRequest{Now: now}
	normalized := req.Normalize()

	assert.Equal(t, now, normalized.Now)
}

func TestActiveAgentPruneRequest_Normalize_PreservesRetention(t *testing.T) {
	t.Parallel()

	req := ActiveAgentPruneRequest{Retention: 48 * time.Hour}
	normalized := req.Normalize()

	assert.Equal(t, 48*time.Hour, normalized.Retention)
}

func TestActiveAgentPruneRequest_Normalize_ZeroRetentionGetsDefault(t *testing.T) {
	t.Parallel()

	req := ActiveAgentPruneRequest{Retention: 0}
	normalized := req.Normalize()

	assert.Equal(t, DefaultActiveAgentHeartbeatRetention, normalized.Retention)
}

func TestActiveAgentPruneRequest_Normalize_NegativeRetentionGetsDefault(t *testing.T) {
	t.Parallel()

	req := ActiveAgentPruneRequest{Retention: -1 * time.Hour}
	normalized := req.Normalize()

	assert.Equal(t, DefaultActiveAgentHeartbeatRetention, normalized.Retention)
}

func TestActiveAgentPruneRequest_Normalize_PreservesLimit(t *testing.T) {
	t.Parallel()

	req := ActiveAgentPruneRequest{Limit: 100}
	normalized := req.Normalize()

	assert.Equal(t, uint64(100), normalized.Limit)
}

// ---------------------------------------------------------------------------
// ActiveAgentPruneRequest.Validate
// ---------------------------------------------------------------------------

func TestActiveAgentPruneRequest_Validate_ZeroLimitError(t *testing.T) {
	t.Parallel()

	req := ActiveAgentPruneRequest{Limit: 0}
	err := req.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit must be > 0")
}

func TestActiveAgentPruneRequest_Validate_ValidLimit(t *testing.T) {
	t.Parallel()

	req := ActiveAgentPruneRequest{Limit: 50}
	err := req.Validate()

	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ActiveAgentPruneRequest.Cutoff
// ---------------------------------------------------------------------------

func TestActiveAgentPruneRequest_Cutoff_ComputesCorrectly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	retention := 24 * time.Hour
	req := ActiveAgentPruneRequest{
		Now:       now,
		Retention: retention,
		Limit:     50,
	}

	cutoff := req.Cutoff()

	expected := now.Add(-retention)
	assert.Equal(t, expected, cutoff)
}

func TestActiveAgentPruneRequest_Cutoff_UsesDefaultsWhenZero(t *testing.T) {
	t.Parallel()

	req := ActiveAgentPruneRequest{}
	cutoff := req.Cutoff()

	// Should use default retention (24 hours) from a recent time
	assert.False(t, cutoff.IsZero())
	// The cutoff should be approximately 24 hours ago
	expectedApprox := time.Now().UTC().Add(-DefaultActiveAgentHeartbeatRetention)
	assert.WithinDuration(t, expectedApprox, cutoff, 2*time.Second)
}

// ---------------------------------------------------------------------------
// DefaultGenesis (stub)
// ---------------------------------------------------------------------------

func TestDefaultGenesis_Stub(t *testing.T) {
	t.Parallel()

	gs := DefaultGenesis()

	require.NotNil(t, gs)
	// Stub returns empty struct
	assert.Equal(t, GenesisState{}, *gs)
}

// ---------------------------------------------------------------------------
// DefaultParams (stub)
// ---------------------------------------------------------------------------

func TestDefaultParams_Stub(t *testing.T) {
	t.Parallel()

	params := DefaultParams()

	require.NotNil(t, params)
	// Stub returns empty struct
	assert.Equal(t, Params{}, *params)
}

// ---------------------------------------------------------------------------
// Params.Validate (stub)
// ---------------------------------------------------------------------------

func TestParams_Validate_Nil(t *testing.T) {
	t.Parallel()

	var params *Params
	err := params.Validate()

	require.NoError(t, err)
}

func TestParams_Validate_Empty(t *testing.T) {
	t.Parallel()

	params := &Params{}
	err := params.Validate()

	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Params.ParamSetPairs (stub)
// ---------------------------------------------------------------------------

func TestParams_ParamSetPairs_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	params := &Params{}
	pairs := params.ParamSetPairs()

	require.NotNil(t, pairs)
	assert.Empty(t, pairs)
}

// ---------------------------------------------------------------------------
// ParamKeyTable (stub)
// ---------------------------------------------------------------------------

func TestParamKeyTable_ReturnsKeyTable(t *testing.T) {
	t.Parallel()

	kt := ParamKeyTable()

	assert.Equal(t, KeyTable{}, kt)
}

// ---------------------------------------------------------------------------
// NewKeyTable (stub)
// ---------------------------------------------------------------------------

func TestNewKeyTable_ReturnsEmptyKeyTable(t *testing.T) {
	t.Parallel()

	kt := NewKeyTable()

	assert.Equal(t, KeyTable{}, kt)
}

// ---------------------------------------------------------------------------
// KeyTable.RegisterParamSet (stub)
// ---------------------------------------------------------------------------

func TestKeyTable_RegisterParamSet_ReturnsSelf(t *testing.T) {
	t.Parallel()

	kt := NewKeyTable()
	params := &Params{}

	result := kt.RegisterParamSet(params)

	assert.Equal(t, kt, result)
}

// ---------------------------------------------------------------------------
// Module constants
// ---------------------------------------------------------------------------

func TestModuleName_Stub(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "registry", ModuleName)
	assert.Equal(t, ModuleName, StoreKey)
}

// ---------------------------------------------------------------------------
// ToolVerifiedBadge constants
// ---------------------------------------------------------------------------

func TestToolVerifiedBadge_Constants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ToolVerifiedBadge(0), ToolVerifiedBadge_TOOL_VERIFIED_BADGE_UNSPECIFIED)
	assert.Equal(t, ToolVerifiedBadge(1), ToolVerifiedBadge_TOOL_VERIFIED_BADGE_BRONZE)
	assert.Equal(t, ToolVerifiedBadge(2), ToolVerifiedBadge_TOOL_VERIFIED_BADGE_SILVER)
	assert.Equal(t, ToolVerifiedBadge(3), ToolVerifiedBadge_TOOL_VERIFIED_BADGE_GOLD)
}

// ---------------------------------------------------------------------------
// ActiveAgent discovery constants
// ---------------------------------------------------------------------------

func TestActiveAgentDiscovery_Constants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "lumera.agent_discovery.v1", ActiveAgentDiscoverySchemaV1)
	assert.Equal(t, "agent_pubkey_asc", ActiveAgentDiscoveryOrderPubkeyAsc)
	assert.Equal(t, uint64(50), DefaultActiveAgentListLimit)
	assert.Equal(t, uint64(200), MaxActiveAgentListLimit)
	assert.Equal(t, 5*time.Minute, DefaultActiveAgentHeartbeatTTL)
	assert.Equal(t, 24*time.Hour, DefaultActiveAgentHeartbeatRetention)
}

// ---------------------------------------------------------------------------
// ToolCard struct
// ---------------------------------------------------------------------------

func TestToolCard_Struct(t *testing.T) {
	t.Parallel()

	card := ToolCard{
		ToolId: "tool-123",
	}

	assert.Equal(t, "tool-123", card.ToolId)
}

// ---------------------------------------------------------------------------
// Type structs - basic initialization
// ---------------------------------------------------------------------------

func TestActiveAgentSkipCounts_Struct(t *testing.T) {
	t.Parallel()

	counts := ActiveAgentSkipCounts{
		HeartbeatStale:        10,
		PassportMissing:       5,
		PassportInactive:      3,
		NoRegisteredTools:     2,
		NoActiveTools:         1,
		OrphanActivationEntry: 0,
	}

	assert.Equal(t, uint64(10), counts.HeartbeatStale)
	assert.Equal(t, uint64(5), counts.PassportMissing)
	assert.Equal(t, uint64(3), counts.PassportInactive)
	assert.Equal(t, uint64(2), counts.NoRegisteredTools)
	assert.Equal(t, uint64(1), counts.NoActiveTools)
	assert.Equal(t, uint64(0), counts.OrphanActivationEntry)
}

func TestActiveAgentListDiagnostics_Struct(t *testing.T) {
	t.Parallel()

	diag := ActiveAgentListDiagnostics{
		HeartbeatsScanned: 100,
		AgentsReturned:    50,
		Skipped: ActiveAgentSkipCounts{
			HeartbeatStale: 10,
		},
	}

	assert.Equal(t, uint64(100), diag.HeartbeatsScanned)
	assert.Equal(t, uint64(50), diag.AgentsReturned)
	assert.Equal(t, uint64(10), diag.Skipped.HeartbeatStale)
}

func TestAgentHeartbeatRecord_Struct(t *testing.T) {
	t.Parallel()

	now := time.Now()
	record := AgentHeartbeatRecord{
		AgentPubkey:               "ed448:abc",
		ObservedAt:                now,
		ExpiresAt:                 now.Add(5 * time.Minute),
		Source:                    "router_session",
		AdvertisedIntervalSeconds: 30,
		Metadata:                  map[string]string{"key": "value"},
	}

	assert.Equal(t, "ed448:abc", record.AgentPubkey)
	assert.Equal(t, now, record.ObservedAt)
	assert.Equal(t, "router_session", record.Source)
	assert.Equal(t, uint32(30), record.AdvertisedIntervalSeconds)
	assert.Equal(t, "value", record.Metadata["key"])
}

func TestActiveAgentToolSummary_Struct(t *testing.T) {
	t.Parallel()

	summary := ActiveAgentToolSummary{
		ToolID:             "tool-123",
		Version:            "1.0.0",
		VerifiedBadge:      ToolVerifiedBadge_TOOL_VERIFIED_BADGE_GOLD,
		Categories:         []string{"defi", "trading"},
		Tags:               []string{"stable", "audited"},
		MCPProtocols:       []string{"mcp+https"},
		LicenseLane:        "byo_key",
		Regions:            []string{"us-east-1", "eu-west-1"},
		RequiresEnclave:    true,
		PIIHandling:        "encrypted",
		DeterministicCache: true,
	}

	assert.Equal(t, "tool-123", summary.ToolID)
	assert.Equal(t, ToolVerifiedBadge_TOOL_VERIFIED_BADGE_GOLD, summary.VerifiedBadge)
	assert.Contains(t, summary.Categories, "defi")
	assert.True(t, summary.RequiresEnclave)
}

func TestActiveAgentCapabilitySummary_Struct(t *testing.T) {
	t.Parallel()

	summary := ActiveAgentCapabilitySummary{
		Categories:                 []string{"defi"},
		Tags:                       []string{"stable"},
		MCPProtocols:               []string{"mcp+https"},
		LicenseLanes:               []string{"byo_key"},
		Regions:                    []string{"us-east-1"},
		PIIHandlingModes:           []string{"encrypted"},
		RequiresEnclave:            true,
		SupportsDeterministicCache: true,
		VerifiedToolsCount:         5,
		RegisteredToolsCount:       10,
		ActiveToolsCount:           8,
	}

	assert.Equal(t, uint32(5), summary.VerifiedToolsCount)
	assert.Equal(t, uint32(10), summary.RegisteredToolsCount)
	assert.Equal(t, uint32(8), summary.ActiveToolsCount)
}

func TestActiveAgentRecord_Struct(t *testing.T) {
	t.Parallel()

	now := time.Now()
	record := ActiveAgentRecord{
		AgentPubkey:    "ed448:abc",
		PassportID:     "passport-123",
		OwnerAddress:   "lumera1...",
		PassportStatus: 1,
		Heartbeat: &AgentHeartbeatRecord{
			AgentPubkey: "ed448:abc",
			ExpiresAt:   now.Add(5 * time.Minute),
		},
		Capabilities: ActiveAgentCapabilitySummary{
			ActiveToolsCount: 5,
		},
		Tools: []ActiveAgentToolSummary{
			{ToolID: "tool-1"},
		},
	}

	assert.Equal(t, "ed448:abc", record.AgentPubkey)
	assert.NotNil(t, record.Heartbeat)
	assert.Len(t, record.Tools, 1)
}

func TestActiveAgentListResponse_Struct(t *testing.T) {
	t.Parallel()

	now := time.Now()
	response := ActiveAgentListResponse{
		SchemaVersion:       ActiveAgentDiscoverySchemaV1,
		GeneratedAt:         now,
		StateHeight:         12345,
		OrderBy:             ActiveAgentDiscoveryOrderPubkeyAsc,
		HeartbeatTTLSeconds: 300,
		NextCursor:          "ed448:xyz",
		Diagnostics: ActiveAgentListDiagnostics{
			HeartbeatsScanned: 100,
		},
		Agents: []ActiveAgentRecord{},
	}

	assert.Equal(t, ActiveAgentDiscoverySchemaV1, response.SchemaVersion)
	assert.Equal(t, int64(12345), response.StateHeight)
	assert.Equal(t, "ed448:xyz", response.NextCursor)
}

func TestActiveAgentPruneRequest_Struct(t *testing.T) {
	t.Parallel()

	now := time.Now()
	req := ActiveAgentPruneRequest{
		Now:       now,
		Retention: 48 * time.Hour,
		Limit:     100,
	}

	assert.Equal(t, now, req.Now)
	assert.Equal(t, 48*time.Hour, req.Retention)
	assert.Equal(t, uint64(100), req.Limit)
}

func TestActiveAgentPruneResponse_Struct(t *testing.T) {
	t.Parallel()

	now := time.Now()
	response := ActiveAgentPruneResponse{
		Cutoff:                 now.Add(-24 * time.Hour),
		RetentionSeconds:       86400,
		PrunedHeartbeatRecords: 50,
	}

	assert.Equal(t, uint32(86400), response.RetentionSeconds)
	assert.Equal(t, uint64(50), response.PrunedHeartbeatRecords)
}
