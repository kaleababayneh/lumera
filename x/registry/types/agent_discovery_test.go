//go:build cosmos

package types

import (
	"testing"
	"time"
)

func TestActiveAgentListRequestNormalizeAndValidate(t *testing.T) {
	req := ActiveAgentListRequest{
		Cursor: "  ed448:abc  ",
		Status: " active ",
		ToolFilter: ActiveAgentToolFilter{
			Capability:  "  vision ",
			Category:    "  defi ",
			Protocol:    " mcp+https ",
			LicenseLane: " byo_key ",
			Region:      " us-east-1 ",
		},
	}

	normalized := req.Normalize()
	if normalized.Cursor != "ed448:abc" {
		t.Fatalf("cursor = %q, want %q", normalized.Cursor, "ed448:abc")
	}
	if normalized.Status != "active" {
		t.Fatalf("status = %q, want %q", normalized.Status, "active")
	}
	if normalized.Limit != DefaultActiveAgentListLimit {
		t.Fatalf("limit = %d, want %d", normalized.Limit, DefaultActiveAgentListLimit)
	}
	if normalized.ToolFilter.Capability != "vision" {
		t.Fatalf("capability = %q, want %q", normalized.ToolFilter.Capability, "vision")
	}
	if normalized.ToolFilter.Category != "defi" {
		t.Fatalf("category = %q, want %q", normalized.ToolFilter.Category, "defi")
	}
	if normalized.ToolFilter.Protocol != "mcp+https" {
		t.Fatalf("protocol = %q, want %q", normalized.ToolFilter.Protocol, "mcp+https")
	}
	if normalized.ToolFilter.LicenseLane != "byo_key" {
		t.Fatalf("license_lane = %q, want %q", normalized.ToolFilter.LicenseLane, "byo_key")
	}
	if normalized.ToolFilter.Region != "us-east-1" {
		t.Fatalf("region = %q, want %q", normalized.ToolFilter.Region, "us-east-1")
	}
	if err := normalized.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestActiveAgentListRequestValidateRejectsOversizedLimit(t *testing.T) {
	req := ActiveAgentListRequest{Limit: MaxActiveAgentListLimit + 1}
	if err := req.Validate(); err == nil {
		t.Fatal("expected limit validation error")
	}
}

func TestAgentHeartbeatRecordFreshAt(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	record := AgentHeartbeatRecord{
		AgentPubkey: "ed448:abc",
		ObservedAt:  now.Add(-30 * time.Second),
		ExpiresAt:   now.Add(30 * time.Second),
		Source:      "router_session",
	}
	if !record.FreshAt(now) {
		t.Fatal("expected heartbeat to be fresh")
	}
	if record.FreshAt(now.Add(31 * time.Second)) {
		t.Fatal("expected heartbeat to be stale after expiry")
	}
}

func TestActiveAgentPruneRequestCutoffAndNormalize(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	req := ActiveAgentPruneRequest{
		Now: now,
	}

	normalized := req.Normalize()
	if normalized.Retention != DefaultActiveAgentHeartbeatRetention {
		t.Fatalf("retention = %s, want %s", normalized.Retention, DefaultActiveAgentHeartbeatRetention)
	}
	if normalized.Limit != DefaultActiveAgentListLimit {
		t.Fatalf("limit = %d, want %d", normalized.Limit, DefaultActiveAgentListLimit)
	}
	if err := normalized.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	wantCutoff := now.Add(-DefaultActiveAgentHeartbeatRetention)
	if got := normalized.Cutoff(); !got.Equal(wantCutoff) {
		t.Fatalf("cutoff = %s, want %s", got, wantCutoff)
	}
}
