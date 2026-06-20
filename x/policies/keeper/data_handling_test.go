//go:build cosmos
// +build cosmos

package keeper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// Unit tests for four under-covered enforcement helpers:
//
//   checkDataClassification
//   checkEncryptionRequirements
//   checkEnclaveRequirements
//   hasCertification / hasCapability
//
// These run on every EvaluatePolicy call but until now only had
// indirect coverage through the higher-level checkPrivacyControls /
// checkSecurityControls tests. A refactor that swaps a handling
// policy enum value, inverts an isolation-level threshold, or
// forgets the "tool has category" guard could silently regress.
//
// Invariants pinned:
//
//   hasCertification / hasCapability
//     • empty slice → false
//     • exact-string match → true
//     • case-sensitive — "foo" ≠ "FOO"
//     • whitespace counts — " foo " ≠ "foo"
//
//   checkDataClassification
//     • empty (UNSPECIFIED everywhere) → pass
//     • DENY + tool has "<class>_handler" cert → deny with scope-label
//     • DENY + tool lacks cert → pass (policy is gated on handler cert)
//     • ENCRYPT + handler cert + isolation < CONTAINER → deny
//     • ENCRYPT + handler cert + isolation >= CONTAINER → pass
//     • REDACT + handler cert + no redaction capability → deny
//     • REDACT + handler cert + redaction capability → pass
//     • class label lowercasing: "PII" drives "pii_handler" cert lookup
//       (regression guard — a refactor that used the upper-case label
//       directly would silently mis-match real handler certs)
//
//   checkEncryptionRequirements
//     • QUANTUM_RESISTANT + isolation < ENCLAVE → deny
//     • QUANTUM_RESISTANT + isolation == ENCLAVE → pass
//     • AES512 + isolation < VM → deny
//     • AES512 + isolation >= VM → pass
//     • AES256 / NONE / UNSPECIFIED → pass regardless of isolation
//     • KeyManagement "hsm" + isolation < ENCLAVE → deny
//     • KeyManagement "enclave" + isolation < ENCLAVE → deny
//     • KeyManagement "managed"/"byok" → pass
//
//   checkEnclaveRequirements
//     • tool.Category == "" → pass (no category to enforce)
//     • category NOT in required_for → pass
//     • category in required_for + isolation < ENCLAVE → deny
//     • category in required_for + isolation == ENCLAVE + no allowed providers → pass
//     • category in required_for + isolation == ENCLAVE + tool.SandboxProfile in allowed → pass
//     • category in required_for + isolation == ENCLAVE + tool.SandboxProfile NOT in allowed → deny
//     • category in required_for + isolation == ENCLAVE + empty SandboxProfile + allowed providers → deny
//       (provider identity is required when the policy declares an allowlist)

// ---------------------------------------------------------------------------
// hasCertification / hasCapability
// ---------------------------------------------------------------------------

func TestHasCertification_EmptySlice(t *testing.T) {
	if hasCertification(&ToolContext{}, "pii_handler") {
		t.Error("empty certifications should not match")
	}
}

func TestHasCertification_ExactMatch(t *testing.T) {
	tool := &ToolContext{Certifications: []string{"hipaa", "pii_handler", "pci_dss"}}
	if !hasCertification(tool, "pii_handler") {
		t.Error("expected match for pii_handler")
	}
}

func TestHasCertification_CaseSensitive(t *testing.T) {
	// The helper uses == (not EqualFold). A certification string with
	// different case must NOT match. Regression guard: any refactor
	// that adds strings.EqualFold would change policy semantics.
	tool := &ToolContext{Certifications: []string{"PII_HANDLER"}}
	if hasCertification(tool, "pii_handler") {
		t.Error("case-insensitive match leaked through; expected strict ==")
	}
}

func TestHasCertification_WhitespaceCounts(t *testing.T) {
	// Certifications are compared literally — trailing/leading
	// whitespace is significant.
	tool := &ToolContext{Certifications: []string{" pii_handler "}}
	if hasCertification(tool, "pii_handler") {
		t.Error("whitespace-padded cert matched; helper must use literal ==")
	}
}

func TestHasCapability_EmptySlice(t *testing.T) {
	if hasCapability(&ToolContext{}, "redaction") {
		t.Error("empty capabilities should not match")
	}
}

func TestHasCapability_ExactMatch(t *testing.T) {
	tool := &ToolContext{Capabilities: []string{"streaming", "redaction", "batch"}}
	if !hasCapability(tool, "redaction") {
		t.Error("expected match for redaction")
	}
}

func TestHasCapability_CaseSensitive(t *testing.T) {
	tool := &ToolContext{Capabilities: []string{"REDACTION"}}
	if hasCapability(tool, "redaction") {
		t.Error("case-insensitive match leaked through")
	}
}

// ---------------------------------------------------------------------------
// checkDataClassification
// ---------------------------------------------------------------------------

func TestCheckDataClassification_Empty(t *testing.T) {
	// All-UNSPECIFIED classification → no enforcement fires.
	reason := checkDataClassification(&types.DataClassification{}, &ToolContext{})
	if reason != "" {
		t.Errorf("UNSPECIFIED classification denied: %q", reason)
	}
}

func TestCheckDataClassification_DenyWithHandlerCert_Blocks(t *testing.T) {
	dc := &types.DataClassification{
		PiiHandling: types.DataHandlingPolicy_DATA_HANDLING_POLICY_DENY,
	}
	tool := &ToolContext{Certifications: []string{"pii_handler"}}
	reason := checkDataClassification(dc, tool)
	if reason == "" {
		t.Fatal("expected denial when PII DENY and tool has pii_handler cert")
	}
	if !strings.Contains(reason, "PII") {
		t.Errorf("denial reason %q missing PII scope label", reason)
	}
}

func TestCheckDataClassification_DenyWithoutHandlerCert_Passes(t *testing.T) {
	// The policy is gated on the tool declaring the handler cert.
	// If the tool doesn't process this class at all (no cert), DENY
	// is a no-op.
	dc := &types.DataClassification{
		PiiHandling: types.DataHandlingPolicy_DATA_HANDLING_POLICY_DENY,
	}
	tool := &ToolContext{Certifications: []string{"some_other_cert"}}
	if reason := checkDataClassification(dc, tool); reason != "" {
		t.Errorf("DENY without handler cert denied: %q", reason)
	}
}

func TestCheckDataClassification_EncryptBelowContainerDenied(t *testing.T) {
	dc := &types.DataClassification{
		PhiHandling: types.DataHandlingPolicy_DATA_HANDLING_POLICY_ENCRYPT,
	}
	tool := &ToolContext{
		Certifications: []string{"phi_handler"},
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_PROCESS,
	}
	reason := checkDataClassification(dc, tool)
	if reason == "" {
		t.Fatal("expected denial for PHI ENCRYPT with process-level isolation")
	}
	if !strings.Contains(reason, "PHI") || !strings.Contains(reason, "encryption") {
		t.Errorf("denial reason %q missing PHI/encryption context", reason)
	}
}

func TestCheckDataClassification_EncryptAtContainerPasses(t *testing.T) {
	dc := &types.DataClassification{
		PhiHandling: types.DataHandlingPolicy_DATA_HANDLING_POLICY_ENCRYPT,
	}
	tool := &ToolContext{
		Certifications: []string{"phi_handler"},
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_CONTAINER,
	}
	if reason := checkDataClassification(dc, tool); reason != "" {
		t.Errorf("CONTAINER isolation rejected for ENCRYPT: %q", reason)
	}
}

func TestCheckDataClassification_RedactWithoutCapabilityDenied(t *testing.T) {
	dc := &types.DataClassification{
		PciHandling: types.DataHandlingPolicy_DATA_HANDLING_POLICY_REDACT,
	}
	tool := &ToolContext{
		Certifications: []string{"pci_handler"},
		// No "redaction" capability.
	}
	reason := checkDataClassification(dc, tool)
	if reason == "" {
		t.Fatal("expected denial for PCI REDACT without redaction capability")
	}
	if !strings.Contains(reason, "redaction") {
		t.Errorf("denial reason %q missing redaction context", reason)
	}
}

func TestCheckDataClassification_RedactWithCapabilityPasses(t *testing.T) {
	dc := &types.DataClassification{
		PciHandling: types.DataHandlingPolicy_DATA_HANDLING_POLICY_REDACT,
	}
	tool := &ToolContext{
		Certifications: []string{"pci_handler"},
		Capabilities:   []string{"redaction"},
	}
	if reason := checkDataClassification(dc, tool); reason != "" {
		t.Errorf("REDACT with capability denied: %q", reason)
	}
}

func TestCheckDataClassification_LabelLowercased(t *testing.T) {
	// Regression guard: helper lowercases the scope label ("PII"
	// → "pii") before building the handler-cert string. A refactor
	// that used the upper-case label directly would fail to detect
	// tools that declare the standard lowercase cert.
	//
	// Observed via proprietary → "proprietary_handler" lookup.
	dc := &types.DataClassification{
		ProprietaryHandling: types.DataHandlingPolicy_DATA_HANDLING_POLICY_DENY,
	}
	tool := &ToolContext{Certifications: []string{"proprietary_handler"}}
	if reason := checkDataClassification(dc, tool); reason == "" {
		t.Fatal("proprietary_handler DENY did not fire; label lowercasing may be broken")
	}
}

// ---------------------------------------------------------------------------
// checkEncryptionRequirements
// ---------------------------------------------------------------------------

func TestCheckEncryptionRequirements_QuantumRequiresEnclave(t *testing.T) {
	enc := &types.EncryptionRequirements{
		AtRest: types.EncryptionLevel_ENCRYPTION_LEVEL_QUANTUM_RESISTANT,
	}
	cases := []struct {
		name      string
		isolation types.IsolationLevel
		denied    bool
	}{
		{"none", types.IsolationLevel_ISOLATION_LEVEL_NONE, true},
		{"container", types.IsolationLevel_ISOLATION_LEVEL_CONTAINER, true},
		{"vm", types.IsolationLevel_ISOLATION_LEVEL_VM, true},
		{"enclave", types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := checkEncryptionRequirements(enc, &ToolContext{IsolationLevel: tc.isolation})
			if tc.denied {
				require.NotEmpty(t, reason, "quantum+isolation=%s should deny", tc.name)
			} else {
				require.Empty(t, reason, "quantum+isolation=%s should pass", tc.name)
			}
		})
	}
}

func TestCheckEncryptionRequirements_AES512RequiresVM(t *testing.T) {
	enc := &types.EncryptionRequirements{
		AtRest: types.EncryptionLevel_ENCRYPTION_LEVEL_AES512,
	}
	cases := []struct {
		name      string
		isolation types.IsolationLevel
		denied    bool
	}{
		{"none", types.IsolationLevel_ISOLATION_LEVEL_NONE, true},
		{"container", types.IsolationLevel_ISOLATION_LEVEL_CONTAINER, true},
		{"vm", types.IsolationLevel_ISOLATION_LEVEL_VM, false},
		{"enclave", types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := checkEncryptionRequirements(enc, &ToolContext{IsolationLevel: tc.isolation})
			if tc.denied {
				require.NotEmpty(t, reason, "AES512+isolation=%s should deny", tc.name)
			} else {
				require.Empty(t, reason, "AES512+isolation=%s should pass", tc.name)
			}
		})
	}
}

func TestCheckEncryptionRequirements_LowerLevelsNoIsolationCheck(t *testing.T) {
	// AES256, NONE, UNSPECIFIED levels have no isolation gate —
	// they pass regardless of tool isolation level (including NONE).
	cases := []types.EncryptionLevel{
		types.EncryptionLevel_ENCRYPTION_LEVEL_UNSPECIFIED,
		types.EncryptionLevel_ENCRYPTION_LEVEL_NONE,
		types.EncryptionLevel_ENCRYPTION_LEVEL_AES256,
	}
	for _, level := range cases {
		t.Run(level.String(), func(t *testing.T) {
			enc := &types.EncryptionRequirements{AtRest: level}
			reason := checkEncryptionRequirements(enc, &ToolContext{
				IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_NONE,
			})
			if reason != "" {
				t.Errorf("level=%s unexpectedly denied: %q", level, reason)
			}
		})
	}
}

func TestCheckEncryptionRequirements_HSMKeyManagementRequiresEnclave(t *testing.T) {
	enc := &types.EncryptionRequirements{KeyManagement: "hsm"}
	// Below enclave: deny.
	reason := checkEncryptionRequirements(enc, &ToolContext{
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_VM,
	})
	require.NotEmpty(t, reason, "HSM key management on VM-level isolation must deny")
	require.Contains(t, reason, "hsm")

	// At enclave: pass.
	reason = checkEncryptionRequirements(enc, &ToolContext{
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE,
	})
	require.Empty(t, reason)
}

func TestCheckEncryptionRequirements_EnclaveKeyManagementRequiresEnclave(t *testing.T) {
	enc := &types.EncryptionRequirements{KeyManagement: "enclave"}
	reason := checkEncryptionRequirements(enc, &ToolContext{
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_VM,
	})
	require.NotEmpty(t, reason)
}

func TestCheckEncryptionRequirements_SoftKeyManagementPasses(t *testing.T) {
	// "managed" and "byok" are software KMS modes and do not
	// require enclave isolation.
	for _, km := range []string{"managed", "byok", ""} {
		t.Run("km="+km, func(t *testing.T) {
			enc := &types.EncryptionRequirements{KeyManagement: km}
			reason := checkEncryptionRequirements(enc, &ToolContext{
				IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_NONE,
			})
			require.Empty(t, reason, "soft KM %q unexpectedly denied", km)
		})
	}
}

// ---------------------------------------------------------------------------
// checkEnclaveRequirements
// ---------------------------------------------------------------------------

func TestCheckEnclaveRequirements_EmptyCategoryPasses(t *testing.T) {
	// Tool with no category declared cannot match any required_for
	// entry — the function returns early with no denial.
	req := &types.EnclaveRequirements{RequiredFor: []string{"pii-processing"}}
	reason := checkEnclaveRequirements(req, &ToolContext{Category: ""})
	require.Empty(t, reason)
}

func TestCheckEnclaveRequirements_CategoryNotRequired(t *testing.T) {
	req := &types.EnclaveRequirements{RequiredFor: []string{"pii-processing"}}
	reason := checkEnclaveRequirements(req, &ToolContext{Category: "weather"})
	require.Empty(t, reason)
}

func TestCheckEnclaveRequirements_RequiredCategoryWithoutEnclaveDenied(t *testing.T) {
	req := &types.EnclaveRequirements{RequiredFor: []string{"pii-processing"}}
	tool := &ToolContext{
		Category:       "pii-processing",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_VM,
	}
	reason := checkEnclaveRequirements(req, tool)
	require.NotEmpty(t, reason)
	require.Contains(t, reason, "pii-processing")
}

func TestCheckEnclaveRequirements_EnclaveWithoutProviderListPasses(t *testing.T) {
	// No AllowedProviders constraint: enclave + category sufficient.
	req := &types.EnclaveRequirements{RequiredFor: []string{"pii-processing"}}
	tool := &ToolContext{
		Category:       "pii-processing",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE,
	}
	require.Empty(t, checkEnclaveRequirements(req, tool))
}

func TestCheckEnclaveRequirements_ProviderAllowlistEnforced(t *testing.T) {
	req := &types.EnclaveRequirements{
		RequiredFor:      []string{"pii-processing"},
		AllowedProviders: []string{"sgx-nitro", "sev-snp"},
	}
	// Tool with matching provider: pass.
	tool := &ToolContext{
		Category:       "pii-processing",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE,
		SandboxProfile: "sgx-nitro",
	}
	require.Empty(t, checkEnclaveRequirements(req, tool))

	// Tool with non-matching provider: deny.
	tool.SandboxProfile = "some-other-enclave"
	reason := checkEnclaveRequirements(req, tool)
	require.NotEmpty(t, reason)
	require.Contains(t, reason, "some-other-enclave")
}

func TestCheckEnclaveRequirements_AllowedProvidersRequireDeclaredProvider(t *testing.T) {
	// specs/privacy/tee-attestation.md requires provider authorization
	// checks. If a policy configures an allowlist, an enclave-isolated
	// tool must declare the provider identity being authorized.
	req := &types.EnclaveRequirements{
		RequiredFor:      []string{"pii-processing"},
		AllowedProviders: []string{"sgx-nitro"},
	}
	for _, sandboxProfile := range []string{"", " \t "} {
		t.Run("profile="+sandboxProfile, func(t *testing.T) {
			tool := &ToolContext{
				Category:       "pii-processing",
				IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE,
				SandboxProfile: sandboxProfile,
			}
			reason := checkEnclaveRequirements(req, tool)
			require.NotEmpty(t, reason)
			require.Contains(t, reason, "enclave provider is required")
			require.Contains(t, reason, "pii-processing")
		})
	}
}
