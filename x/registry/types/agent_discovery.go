//go:build cosmos

package types

import (
	"context"
	"fmt"
	"strings"
	"time"

	passporttypes "github.com/LumeraProtocol/lumera/x/passport/types"
)

const (
	// ActiveAgentDiscoverySchemaV1 is the canonical schema identifier for v1 active-agent discovery.
	ActiveAgentDiscoverySchemaV1 = "lumera.agent_discovery.v1"
	// ActiveAgentDiscoveryOrderPubkeyAsc is the only supported v1 ordering.
	ActiveAgentDiscoveryOrderPubkeyAsc = "agent_pubkey_asc"

	// DefaultActiveAgentListLimit is applied when a caller omits the page size.
	DefaultActiveAgentListLimit uint64 = 50
	// MaxActiveAgentListLimit bounds the response size for operator-safe discovery queries.
	MaxActiveAgentListLimit uint64 = 200

	// DefaultActiveAgentHeartbeatTTL is the canonical freshness window when the heartbeat source does not advertise a stricter expiry.
	DefaultActiveAgentHeartbeatTTL = 5 * time.Minute
	// DefaultActiveAgentHeartbeatRetention is how long expired heartbeat records are retained before prune jobs may remove them.
	DefaultActiveAgentHeartbeatRetention = 24 * time.Hour

	// ActiveAgentSkipReasonHeartbeatStale indicates the agent heartbeat expired before the query cutoff.
	ActiveAgentSkipReasonHeartbeatStale = "heartbeat_stale"
	// ActiveAgentSkipReasonPassportMissing indicates there is no passport for the agent pubkey.
	ActiveAgentSkipReasonPassportMissing = "passport_missing"
	// ActiveAgentSkipReasonPassportInactive indicates the passport exists but is not in ACTIVE status.
	ActiveAgentSkipReasonPassportInactive = "passport_inactive"
	// ActiveAgentSkipReasonNoRegisteredTools indicates no registry tools are owned by the agent pubkey.
	ActiveAgentSkipReasonNoRegisteredTools = "no_registered_tools"
	// ActiveAgentSkipReasonNoActiveTools indicates registry tools exist but none are currently active.
	ActiveAgentSkipReasonNoActiveTools = "no_active_tools"
	// ActiveAgentSkipReasonOrphanActivationEntry indicates activation state referenced a missing toolcard.
	ActiveAgentSkipReasonOrphanActivationEntry = "orphan_activation_entry"

	// ActiveAgentDiscoveryEventList is emitted for successful list queries.
	ActiveAgentDiscoveryEventList = "agent_discovery_list"
	// ActiveAgentDiscoveryEventSkip is emitted when a candidate agent is skipped.
	ActiveAgentDiscoveryEventSkip = "agent_discovery_skip"
	// ActiveAgentDiscoveryEventPrune is emitted when stale heartbeats are pruned.
	ActiveAgentDiscoveryEventPrune = "agent_discovery_prune"
)

// ActiveAgentToolFilter narrows discovery to agents with at least one active tool matching every populated field.
type ActiveAgentToolFilter struct {
	Capability      string `json:"capability,omitempty"`
	Category        string `json:"category,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	LicenseLane     string `json:"license_lane,omitempty"`
	Region          string `json:"region,omitempty"`
	RequiresEnclave *bool  `json:"requires_enclave,omitempty"`
	VerifiedOnly    bool   `json:"verified_only,omitempty"`
}

// Normalize returns a trimmed copy of the filter.
func (f ActiveAgentToolFilter) Normalize() ActiveAgentToolFilter {
	f.Capability = strings.TrimSpace(f.Capability)
	f.Category = strings.TrimSpace(f.Category)
	f.Protocol = strings.TrimSpace(f.Protocol)
	f.LicenseLane = strings.TrimSpace(f.LicenseLane)
	f.Region = strings.TrimSpace(f.Region)
	return f
}

// ActiveAgentListRequest is the canonical v1 list request.
type ActiveAgentListRequest struct {
	Cursor       string                `json:"cursor,omitempty"`
	Limit        uint64                `json:"limit,omitempty"`
	IncludeTools bool                  `json:"include_tools,omitempty"`
	Status       string                `json:"status,omitempty"`
	ToolFilter   ActiveAgentToolFilter `json:"tool_filter,omitempty"`
}

// Normalize returns a trimmed request with canonical defaults applied.
func (r ActiveAgentListRequest) Normalize() ActiveAgentListRequest {
	r.Cursor = strings.TrimSpace(r.Cursor)
	r.Status = strings.TrimSpace(r.Status)
	if r.Limit == 0 {
		r.Limit = DefaultActiveAgentListLimit
	}
	r.ToolFilter = r.ToolFilter.Normalize()
	return r
}

// Validate ensures the request is safe and bounded.
func (r ActiveAgentListRequest) Validate() error {
	if r.Limit == 0 {
		return fmt.Errorf("limit must be > 0")
	}
	if r.Limit > MaxActiveAgentListLimit {
		return fmt.Errorf("limit must be <= %d", MaxActiveAgentListLimit)
	}
	return nil
}

// ActiveAgentSkipCounts captures bounded aggregate skip reasons for a query.
type ActiveAgentSkipCounts struct {
	HeartbeatStale        uint64 `json:"heartbeat_stale"`
	PassportMissing       uint64 `json:"passport_missing"`
	PassportInactive      uint64 `json:"passport_inactive"`
	NoRegisteredTools     uint64 `json:"no_registered_tools"`
	NoActiveTools         uint64 `json:"no_active_tools"`
	OrphanActivationEntry uint64 `json:"orphan_activation_entry"`
}

// ActiveAgentListDiagnostics records aggregate discovery diagnostics.
type ActiveAgentListDiagnostics struct {
	HeartbeatsScanned uint64                `json:"heartbeats_scanned"`
	AgentsReturned    uint64                `json:"agents_returned"`
	Skipped           ActiveAgentSkipCounts `json:"skipped"`
}

// AgentHeartbeatRecord is the canonical liveness record joined into active-agent discovery.
type AgentHeartbeatRecord struct {
	AgentPubkey               string            `json:"agent_pubkey"`
	ObservedAt                time.Time         `json:"observed_at"`
	ExpiresAt                 time.Time         `json:"expires_at"`
	Source                    string            `json:"source"`
	AdvertisedIntervalSeconds uint32            `json:"advertised_interval_seconds,omitempty"`
	Metadata                  map[string]string `json:"metadata,omitempty"`
}

// FreshAt reports whether the heartbeat is still fresh at the provided time.
func (h AgentHeartbeatRecord) FreshAt(now time.Time) bool {
	if now.IsZero() || h.ExpiresAt.IsZero() {
		return false
	}
	return h.ExpiresAt.After(now)
}

// ActiveAgentToolSummary is the bounded per-tool view returned by discovery.
type ActiveAgentToolSummary struct {
	ToolID             string            `json:"tool_id"`
	Version            string            `json:"version,omitempty"`
	VerifiedBadge      ToolVerifiedBadge `json:"verified_badge,omitempty"`
	Categories         []string          `json:"categories,omitempty"`
	Tags               []string          `json:"tags,omitempty"`
	MCPProtocols       []string          `json:"mcp_protocols,omitempty"`
	LicenseLane        string            `json:"license_lane,omitempty"`
	TrustClass         string            `json:"trust_class,omitempty"`
	Regions            []string          `json:"regions,omitempty"`
	RequiresEnclave    bool              `json:"requires_enclave,omitempty"`
	PIIHandling        string            `json:"pii_handling,omitempty"`
	DeterministicCache bool              `json:"deterministic_cache,omitempty"`
}

// ActiveAgentCapabilitySummary is the aggregate capability union for the returned active tool set.
type ActiveAgentCapabilitySummary struct {
	Categories                 []string `json:"categories,omitempty"`
	Tags                       []string `json:"tags,omitempty"`
	MCPProtocols               []string `json:"mcp_protocols,omitempty"`
	LicenseLanes               []string `json:"license_lanes,omitempty"`
	TrustClasses               []string `json:"trust_classes,omitempty"`
	Regions                    []string `json:"regions,omitempty"`
	PIIHandlingModes           []string `json:"pii_handling_modes,omitempty"`
	RequiresEnclave            bool     `json:"requires_enclave,omitempty"`
	SupportsDeterministicCache bool     `json:"supports_deterministic_cache,omitempty"`
	VerifiedToolsCount         uint32   `json:"verified_tools_count,omitempty"`
	RegisteredToolsCount       uint32   `json:"registered_tools_count,omitempty"`
	ActiveToolsCount           uint32   `json:"active_tools_count,omitempty"`
}

// ActiveAgentRecord is the canonical response item for a single active agent.
type ActiveAgentRecord struct {
	AgentPubkey    string                       `json:"agent_pubkey"`
	PassportID     string                       `json:"passport_id,omitempty"`
	OwnerAddress   string                       `json:"owner_address,omitempty"`
	PassportStatus passporttypes.PassportStatus `json:"passport_status,omitempty"`
	Heartbeat      *AgentHeartbeatRecord        `json:"heartbeat,omitempty"`
	Capabilities   ActiveAgentCapabilitySummary `json:"capabilities"`
	Tools          []ActiveAgentToolSummary     `json:"tools,omitempty"`
}

// ActiveAgentListResponse is the canonical bounded result for v1 discovery.
type ActiveAgentListResponse struct {
	SchemaVersion       string                     `json:"schema_version"`
	GeneratedAt         time.Time                  `json:"generated_at"`
	StateHeight         int64                      `json:"state_height"`
	OrderBy             string                     `json:"order_by"`
	HeartbeatTTLSeconds uint32                     `json:"heartbeat_ttl_seconds"`
	NextCursor          string                     `json:"next_cursor,omitempty"`
	Diagnostics         ActiveAgentListDiagnostics `json:"diagnostics"`
	Agents              []ActiveAgentRecord        `json:"agents"`
}

// ActiveAgentPruneRequest defines the canonical prune inputs for expired heartbeats.
type ActiveAgentPruneRequest struct {
	Now       time.Time     `json:"now"`
	Retention time.Duration `json:"retention,omitempty"`
	Limit     uint64        `json:"limit,omitempty"`
}

// Normalize applies canonical prune defaults.
func (r ActiveAgentPruneRequest) Normalize() ActiveAgentPruneRequest {
	if r.Now.IsZero() {
		r.Now = time.Now().UTC()
	}
	if r.Retention <= 0 {
		r.Retention = DefaultActiveAgentHeartbeatRetention
	}
	if r.Limit == 0 {
		r.Limit = DefaultActiveAgentListLimit
	}
	return r
}

// Validate ensures the prune request is bounded.
func (r ActiveAgentPruneRequest) Validate() error {
	if r.Limit == 0 {
		return fmt.Errorf("limit must be > 0")
	}
	return nil
}

// Cutoff returns the timestamp older than which expired heartbeats may be removed.
func (r ActiveAgentPruneRequest) Cutoff() time.Time {
	normalized := r.Normalize()
	return normalized.Now.Add(-normalized.Retention)
}

// ActiveAgentPruneResponse summarizes a prune pass.
type ActiveAgentPruneResponse struct {
	Cutoff                 time.Time `json:"cutoff"`
	RetentionSeconds       uint32    `json:"retention_seconds"`
	PrunedHeartbeatRecords uint64    `json:"pruned_heartbeat_records"`
}

// ActiveAgentHeartbeatStore provides the canonical heartbeat read/prune surface for discovery.
type ActiveAgentHeartbeatStore interface {
	ListHeartbeats(ctx context.Context, cursor string, limit uint64) ([]*AgentHeartbeatRecord, string, error)
	GetHeartbeat(ctx context.Context, agentPubkey string) (*AgentHeartbeatRecord, bool, error)
	PruneExpiredHeartbeats(ctx context.Context, olderThan time.Time, limit uint64) (uint64, error)
}

// ActiveAgentPassportReader provides ordered passport iteration and point lookups for discovery joins.
type ActiveAgentPassportReader interface {
	ListPassports(ctx context.Context, cursor string, limit uint64) ([]*passporttypes.AgentPassport, string, error)
	GetPassportByAgent(ctx context.Context, agentPubkey string) (*passporttypes.AgentPassport, bool, error)
}

// ActiveAgentRegistryReader provides the canonical registry join surface.
type ActiveAgentRegistryReader interface {
	ListToolsByOwnerPubkey(ctx context.Context, ownerPubkey string) ([]*ToolCard, error)
	IsToolActive(ctx context.Context, toolID string) (bool, error)
}

// ActiveAgentDiscovery defines the canonical list-and-prune discovery service.
type ActiveAgentDiscovery interface {
	ListActiveAgents(ctx context.Context, req *ActiveAgentListRequest) (*ActiveAgentListResponse, error)
	PruneStaleHeartbeats(ctx context.Context, req *ActiveAgentPruneRequest) (*ActiveAgentPruneResponse, error)
}
