//go:build cosmos

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// ---------------------------------------------------------------------------
// Command structure — Tx
// ---------------------------------------------------------------------------

func TestGetTxCmd_HasSubcommands(t *testing.T) {
	t.Parallel()
	cmd := GetTxCmd()
	if cmd.Use != types.ModuleName {
		t.Errorf("Use = %q, want %q", cmd.Use, types.ModuleName)
	}
	subs := cmd.Commands()
	if len(subs) != 6 {
		t.Fatalf("expected 6 tx subcommands, got %d", len(subs))
	}
	names := map[string]bool{}
	for _, sub := range subs {
		names[sub.Name()] = true
	}
	for _, want := range []string{
		"create-policy", "update-policy", "activate-policy",
		"deprecate-policy", "archive-policy", "update-params",
	} {
		if !names[want] {
			t.Errorf("missing tx subcommand %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Command structure — Query
// ---------------------------------------------------------------------------

func TestGetQueryCmd_HasSubcommands(t *testing.T) {
	t.Parallel()
	cmd := GetQueryCmd()
	if cmd.Use != types.ModuleName {
		t.Errorf("Use = %q, want %q", cmd.Use, types.ModuleName)
	}
	subs := cmd.Commands()
	if len(subs) != 3 {
		t.Fatalf("expected 3 query subcommands, got %d", len(subs))
	}
	names := map[string]bool{}
	for _, sub := range subs {
		names[sub.Name()] = true
	}
	for _, want := range []string{"policy", "policies", "params"} {
		if !names[want] {
			t.Errorf("missing query subcommand %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// CreatePolicy flags
// ---------------------------------------------------------------------------

func TestCreatePolicyCmd_HasFlags(t *testing.T) {
	t.Parallel()
	cmd := NewCreatePolicyCmd()
	for _, name := range []string{
		flagPolicyFile, flagPolicyID, flagVersion, flagName,
		flagDescription, flagSchemaVersion,
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

func TestReadPolicyFile_AllowsMaxPolicyFileBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	payload := bytes.Repeat([]byte(" "), int(maxPolicyFileBytes))
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	got, err := readPolicyFile(path)
	if err != nil {
		t.Fatalf("readPolicyFile returned error: %v", err)
	}
	if len(got) != int(maxPolicyFileBytes) {
		t.Fatalf("readPolicyFile length = %d, want %d", len(got), maxPolicyFileBytes)
	}
}

func TestReadPolicyFile_RejectsOversizedPolicyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	payload := bytes.Repeat([]byte(" "), int(maxPolicyFileBytes)+1)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	_, err := readPolicyFile(path)
	if err == nil {
		t.Fatal("expected oversized policy file to be rejected")
	}
	if !strings.Contains(err.Error(), "policy file exceeds") {
		t.Fatalf("error = %q, want policy file size error", err)
	}
}

func TestReadPolicyProfileFile_ParsesPolicyJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	payload := []byte(`{"policy_id":"tenant-a","version":"1.0.0","schema_version":"1.0","metadata":{"name":"Tenant A"}}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	policy, err := readPolicyProfileFile(path)
	if err != nil {
		t.Fatalf("readPolicyProfileFile returned error: %v", err)
	}
	if policy.PolicyId != "tenant-a" {
		t.Fatalf("policy_id = %q, want tenant-a", policy.PolicyId)
	}
	if policy.GetMetadata().GetName() != "Tenant A" {
		t.Fatalf("metadata.name = %q, want Tenant A", policy.GetMetadata().GetName())
	}
}

// ---------------------------------------------------------------------------
// UpdatePolicy flags & required marks
// ---------------------------------------------------------------------------

func TestUpdatePolicyCmd_HasRequiredFlags(t *testing.T) {
	t.Parallel()
	cmd := NewUpdatePolicyCmd()
	for _, name := range []string{flagPolicyID, flagPolicyFile, flagUpdateReason} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

// ---------------------------------------------------------------------------
// ActivatePolicy args
// ---------------------------------------------------------------------------

func TestActivatePolicyCmd_ExactArgs(t *testing.T) {
	t.Parallel()
	cmd := NewActivatePolicyCmd()
	if cmd.Args == nil {
		t.Error("expected Args validator to be set")
	}
}

// ---------------------------------------------------------------------------
// DeprecatePolicy args
// ---------------------------------------------------------------------------

func TestDeprecatePolicyCmd_ExactArgs(t *testing.T) {
	t.Parallel()
	cmd := NewDeprecatePolicyCmd()
	if cmd.Args == nil {
		t.Error("expected Args validator to be set")
	}
}

// ---------------------------------------------------------------------------
// ArchivePolicy args
// ---------------------------------------------------------------------------

func TestArchivePolicyCmd_ExactArgs(t *testing.T) {
	t.Parallel()
	cmd := NewArchivePolicyCmd()
	if cmd.Args == nil {
		t.Error("expected Args validator to be set")
	}
}

// ---------------------------------------------------------------------------
// UpdateParams flags
// ---------------------------------------------------------------------------

func TestUpdateParamsCmd_HasFlags(t *testing.T) {
	t.Parallel()
	cmd := NewUpdateParamsCmd()
	for _, name := range []string{
		flagMinPolicyDeposit, flagMaxPolicyVersionHistory,
		flagDefaultMigrationWindowSecs,
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Query policies filter flags
// ---------------------------------------------------------------------------

func TestPoliciesCmd_HasFilterFlags(t *testing.T) {
	t.Parallel()
	cmd := NewQueryPoliciesCmd()
	for _, name := range []string{flagOwner, flagState} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing filter flag --%s", name)
		}
	}
}
