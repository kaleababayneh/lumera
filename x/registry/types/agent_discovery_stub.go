//go:build !cosmos

package types

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	ActiveAgentDiscoverySchemaV1       = "lumera.agent_discovery.v1"
	ActiveAgentDiscoveryOrderPubkeyAsc = "agent_pubkey_asc"

	DefaultActiveAgentListLimit uint64 = 50
	MaxActiveAgentListLimit     uint64 = 200

	DefaultActiveAgentHeartbeatTTL       = 5 * time.Minute
	DefaultActiveAgentHeartbeatRetention = 24 * time.Hour
)

type ActiveAgentToolFilter struct {
	Capability      string `json:"capability,omitempty"`
	Category        string `json:"category,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	LicenseLane     string `json:"license_lane,omitempty"`
	Region          string `json:"region,omitempty"`
	RequiresEnclave *bool  `json:"requires_enclave,omitempty"`
	VerifiedOnly    bool   `json:"verified_only,omitempty"`
}

func (f ActiveAgentToolFilter) Normalize() ActiveAgentToolFilter {
	f.Capability = strings.TrimSpace(f.Capability)
	f.Category = strings.TrimSpace(f.Category)
	f.Protocol = strings.TrimSpace(f.Protocol)
	f.LicenseLane = strings.TrimSpace(f.LicenseLane)
	f.Region = strings.TrimSpace(f.Region)
	return f
}

type ActiveAgentListRequest struct {
	Cursor       string                `json:"cursor,omitempty"`
	Limit        uint64                `json:"limit,omitempty"`
	IncludeTools bool                  `json:"include_tools,omitempty"`
	Status       string                `json:"status,omitempty"`
	ToolFilter   ActiveAgentToolFilter `json:"tool_filter,omitempty"`
}

func (r ActiveAgentListRequest) Normalize() ActiveAgentListRequest {
	r.Cursor = strings.TrimSpace(r.Cursor)
	r.Status = strings.TrimSpace(r.Status)
	if r.Limit == 0 {
		r.Limit = DefaultActiveAgentListLimit
	}
	r.ToolFilter = r.ToolFilter.Normalize()
	return r
}

func (r ActiveAgentListRequest) Validate() error {
	if r.Limit == 0 {
		return fmt.Errorf("limit must be > 0")
	}
	if r.Limit > MaxActiveAgentListLimit {
		return fmt.Errorf("limit must be <= %d", MaxActiveAgentListLimit)
	}
	return nil
}

type ActiveAgentSkipCounts struct {
	HeartbeatStale        uint64 `json:"heartbeat_stale"`
	PassportMissing       uint64 `json:"passport_missing"`
	PassportInactive      uint64 `json:"passport_inactive"`
	NoRegisteredTools     uint64 `json:"no_registered_tools"`
	NoActiveTools         uint64 `json:"no_active_tools"`
	OrphanActivationEntry uint64 `json:"orphan_activation_entry"`
}

type ActiveAgentListDiagnostics struct {
	HeartbeatsScanned uint64                `json:"heartbeats_scanned"`
	AgentsReturned    uint64                `json:"agents_returned"`
	Skipped           ActiveAgentSkipCounts `json:"skipped"`
}

type AgentHeartbeatRecord struct {
	AgentPubkey               string            `json:"agent_pubkey"`
	ObservedAt                time.Time         `json:"observed_at"`
	ExpiresAt                 time.Time         `json:"expires_at"`
	Source                    string            `json:"source"`
	AdvertisedIntervalSeconds uint32            `json:"advertised_interval_seconds,omitempty"`
	Metadata                  map[string]string `json:"metadata,omitempty"`
}

func (h AgentHeartbeatRecord) FreshAt(now time.Time) bool {
	if now.IsZero() || h.ExpiresAt.IsZero() {
		return false
	}
	return h.ExpiresAt.After(now)
}

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

type ActiveAgentRecord struct {
	AgentPubkey    string                       `json:"agent_pubkey"`
	PassportID     string                       `json:"passport_id,omitempty"`
	OwnerAddress   string                       `json:"owner_address,omitempty"`
	PassportStatus int32                        `json:"passport_status,omitempty"`
	Heartbeat      *AgentHeartbeatRecord        `json:"heartbeat,omitempty"`
	Capabilities   ActiveAgentCapabilitySummary `json:"capabilities"`
	Tools          []ActiveAgentToolSummary     `json:"tools,omitempty"`
}

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

type ActiveAgentPruneRequest struct {
	Now       time.Time     `json:"now"`
	Retention time.Duration `json:"retention,omitempty"`
	Limit     uint64        `json:"limit,omitempty"`
}

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

func (r ActiveAgentPruneRequest) Validate() error {
	if r.Limit == 0 {
		return fmt.Errorf("limit must be > 0")
	}
	return nil
}

func (r ActiveAgentPruneRequest) Cutoff() time.Time {
	normalized := r.Normalize()
	return normalized.Now.Add(-normalized.Retention)
}

type ActiveAgentPruneResponse struct {
	Cutoff                 time.Time `json:"cutoff"`
	RetentionSeconds       uint32    `json:"retention_seconds"`
	PrunedHeartbeatRecords uint64    `json:"pruned_heartbeat_records"`
}

type ActiveAgentHeartbeatStore interface {
	ListHeartbeats(ctx context.Context, cursor string, limit uint64) ([]*AgentHeartbeatRecord, string, error)
	PruneExpiredHeartbeats(ctx context.Context, olderThan time.Time, limit uint64) (uint64, error)
}

type ActiveAgentPassportReader interface {
	GetPassportByAgent(ctx context.Context, agentPubkey string) (any, bool, error)
}

type ActiveAgentRegistryReader interface {
	ListToolsByOwnerPubkey(ctx context.Context, ownerPubkey string) ([]*ToolCard, error)
	IsToolActive(ctx context.Context, toolID string) (bool, error)
}

type ActiveAgentDiscovery interface {
	ListActiveAgents(ctx context.Context, req *ActiveAgentListRequest) (*ActiveAgentListResponse, error)
	PruneStaleHeartbeats(ctx context.Context, req *ActiveAgentPruneRequest) (*ActiveAgentPruneResponse, error)
}
