
package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const validHarnessReportPayloadDigest = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

var validHarnessReportPublicKey = "ed448:" + strings.Repeat("ab", 57)

func TestDefaultHarnessReportVerificationContract(t *testing.T) {
	contract := DefaultHarnessReportVerificationContract()
	require.NoError(t, contract.Validate())
	require.Equal(t, HarnessReportVerificationContractVersion, contract.Version)
	require.Equal(t, HarnessReportManifestVersion, contract.ManifestVersion)
	require.Equal(t, HarnessReportSignatureVersion, contract.SignatureVersion)
	require.Equal(t, HarnessReportSignatureAlgorithmEd448, contract.SignatureAlgorithm)
	require.Equal(t, HarnessReportCanonicalizerRFC8785JCS, contract.SignatureCanonicalizer)
	require.Equal(t, HarnessReportSignedScopeManifest, contract.SignedScope)
	require.NotEmpty(t, contract.FixturePlan)
	require.NotEmpty(t, contract.ReplayInputs)
	require.NotEmpty(t, contract.RequiredLogFields)
	require.NotEmpty(t, contract.FailureSemantics)
}

func TestHarnessReportVerificationContractValidateRejectsBlankEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*HarnessReportVerificationContract)
	}{
		{
			name: "blank fixture plan entry",
			edit: func(c *HarnessReportVerificationContract) {
				c.FixturePlan = []string{"signed happy-path report", " "}
			},
		},
		{
			name: "blank replay input entry",
			edit: func(c *HarnessReportVerificationContract) {
				c.ReplayInputs = []string{"manifest.run.run_id", "\t"}
			},
		},
		{
			name: "blank required log field entry",
			edit: func(c *HarnessReportVerificationContract) {
				c.RequiredLogFields = []string{"run_id", ""}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := DefaultHarnessReportVerificationContract()
			tt.edit(&contract)
			err := contract.Validate()
			require.ErrorContains(t, err, "is required")
		})
	}
}

func TestHarnessReportVerificationContractValidateRejectsMalformedFailureSemantics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		replace HarnessReportFailureSemantic
	}{
		{
			name: "blank code",
			replace: HarnessReportFailureSemantic{
				UserMessage:    "The report could not be verified.",
				OperatorAction: "Regenerate the report.",
			},
		},
		{
			name: "blank user message",
			replace: HarnessReportFailureSemantic{
				Code:           HarnessReportVerificationCodeInvalidSignature,
				UserMessage:    " ",
				OperatorAction: "Regenerate the report.",
			},
		},
		{
			name: "blank operator action",
			replace: HarnessReportFailureSemantic{
				Code:           HarnessReportVerificationCodeInvalidSignature,
				UserMessage:    "The report could not be verified.",
				OperatorAction: "\t",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := DefaultHarnessReportVerificationContract()
			contract.FailureSemantics = []HarnessReportFailureSemantic{tt.replace}
			err := contract.Validate()
			require.ErrorContains(t, err, "failure_semantics[0]")
		})
	}
}

func TestHarnessReportVerificationContractValidateRejectsUnknownFailureCode(t *testing.T) {
	t.Parallel()

	contract := DefaultHarnessReportVerificationContract()
	contract.FailureSemantics = []HarnessReportFailureSemantic{
		{
			Code:           HarnessReportVerificationCode("unexpected_failure_code"),
			UserMessage:    "The report could not be verified.",
			OperatorAction: "Regenerate the report with a supported failure code.",
		},
	}

	err := contract.Validate()
	require.ErrorContains(t, err, "failure_semantics[0] code")
}

func TestHarnessReportRunRefMarshalJSON_OmitsZeroTimestamps(t *testing.T) {
	run := HarnessReportRunRef{
		RunID:          "run-zero-times",
		JobID:          "job-zero-times",
		ToolID:         "tool.demo",
		DurationMillis: 42,
	}

	raw, err := json.Marshal(run)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "started_at")
	require.NotContains(t, string(raw), "completed_at")
}

func TestHarnessReportEnvelopeValidate(t *testing.T) {
	now := time.Unix(1_770_000_000, 0).UTC()
	envelope := HarnessReportEnvelope{
		Manifest: HarnessReportManifest{
			Version:                 HarnessReportManifestVersion,
			GeneratedAt:             now,
			Run:                     HarnessReportRunRef{RunID: "run-001", JobID: "job-001", ToolID: "tool.demo", Category: "golden", PolicyVersion: "policy.demo@1", StartedAt: now, CompletedAt: now.Add(30 * time.Second), DurationMillis: 30_000},
			Scenario:                HarnessReportScenarioRef{JobType: "golden", ManifestPath: "tools/harness/manifests/demo.yaml", LogEntryCount: 12},
			ArtifactInventory:       []HarnessReportArtifactRecord{{Name: "summary.json", SHA256: validHarnessReportArtifactSHA256, SizeBytes: 128, Status: HarnessReportArtifactStatusPresent}},
			ArtifactHashAlgorithm:   HarnessReportArtifactHashAlgorithmSHA256,
			ArtifactMerkleAlgorithm: HarnessReportArtifactMerkleAlgorithmSHA256,
			ArtifactMerkleRoot:      validHarnessReportMerkleRoot,
			ScoreDigest:             validHarnessReportScoreDigest,
		},
		VerificationContract: DefaultHarnessReportVerificationContract(),
		Signature: &HarnessReportSignatureEnvelope{
			Version:        HarnessReportSignatureVersion,
			Algorithm:      HarnessReportSignatureAlgorithmEd448,
			Canonicalizer:  HarnessReportCanonicalizerRFC8785JCS,
			Scope:          HarnessReportSignedScopeManifest,
			SignerIdentity: "lumera.harness.attestor.test",
			KeyID:          "harness-ed448-1",
			PublicKey:      validHarnessReportPublicKey,
			PayloadDigest:  validHarnessReportPayloadDigest,
			Signature:      "ed448:deadbeef",
			SignedAt:       now,
		},
	}
	require.NoError(t, envelope.Validate())
}

func TestHarnessReportSignatureEnvelopeValidateRejectsPaddedRequiredFields(t *testing.T) {
	t.Parallel()
	base := HarnessReportSignatureEnvelope{
		Version:        HarnessReportSignatureVersion,
		Algorithm:      HarnessReportSignatureAlgorithmEd448,
		Canonicalizer:  HarnessReportCanonicalizerRFC8785JCS,
		Scope:          HarnessReportSignedScopeManifest,
		SignerIdentity: "lumera.harness.attestor.test",
		PayloadDigest:  validHarnessReportPayloadDigest,
		Signature:      "ed448:deadbeef",
		SignedAt:       time.Unix(1_770_000_000, 0).UTC(),
	}

	tests := []struct {
		name string
		edit func(*HarnessReportSignatureEnvelope)
	}{
		{
			name: "padded signer identity",
			edit: func(e *HarnessReportSignatureEnvelope) {
				e.SignerIdentity = " lumera.harness.attestor.test"
			},
		},
		{
			name: "padded payload digest",
			edit: func(e *HarnessReportSignatureEnvelope) {
				e.PayloadDigest = validHarnessReportPayloadDigest + " "
			},
		},
		{
			name: "padded signature",
			edit: func(e *HarnessReportSignatureEnvelope) {
				e.Signature = "\ted448:deadbeef"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envelope := base
			tt.edit(&envelope)
			err := envelope.Validate()
			require.ErrorContains(t, err, "must not contain leading or trailing whitespace")
		})
	}
}

func TestHarnessReportSignatureEnvelopeValidateRejectsMalformedPayloadDigest(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"abc123",
		"blake3:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		"sha256:abc123",
		"sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef012345678g",
		"sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	} {
		envelope := HarnessReportSignatureEnvelope{
			Version:        HarnessReportSignatureVersion,
			Algorithm:      HarnessReportSignatureAlgorithmEd448,
			Canonicalizer:  HarnessReportCanonicalizerRFC8785JCS,
			Scope:          HarnessReportSignedScopeManifest,
			SignerIdentity: "lumera.harness.attestor.test",
			PayloadDigest:  bad,
			Signature:      "ed448:deadbeef",
			SignedAt:       time.Unix(1_770_000_000, 0).UTC(),
		}
		err := envelope.Validate()
		require.ErrorContains(t, err, "payload_digest")
		require.ErrorContains(t, err, "must be sha256:<64 lowercase hex>")
	}
}

func TestHarnessReportSignatureEnvelopeValidateRejectsMalformedEd448Fields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*HarnessReportSignatureEnvelope)
	}{
		{
			name: "signature missing prefix",
			edit: func(e *HarnessReportSignatureEnvelope) {
				e.Signature = "deadbeef"
			},
		},
		{
			name: "signature uppercase hex",
			edit: func(e *HarnessReportSignatureEnvelope) {
				e.Signature = "ed448:DEADBEEF"
			},
		},
		{
			name: "signature non hex",
			edit: func(e *HarnessReportSignatureEnvelope) {
				e.Signature = "ed448:deadbeeg"
			},
		},
		{
			name: "public key too short",
			edit: func(e *HarnessReportSignatureEnvelope) {
				e.PublicKey = "ed448:001122"
			},
		},
		{
			name: "public key non hex",
			edit: func(e *HarnessReportSignatureEnvelope) {
				e.PublicKey = "ed448:" + strings.Repeat("ab", 56) + "ag"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envelope := HarnessReportSignatureEnvelope{
				Version:        HarnessReportSignatureVersion,
				Algorithm:      HarnessReportSignatureAlgorithmEd448,
				Canonicalizer:  HarnessReportCanonicalizerRFC8785JCS,
				Scope:          HarnessReportSignedScopeManifest,
				SignerIdentity: "lumera.harness.attestor.test",
				PublicKey:      validHarnessReportPublicKey,
				PayloadDigest:  validHarnessReportPayloadDigest,
				Signature:      "ed448:deadbeef",
				SignedAt:       time.Unix(1_770_000_000, 0).UTC(),
			}
			tt.edit(&envelope)
			err := envelope.Validate()
			require.ErrorContains(t, err, "ed448")
		})
	}
}

func TestHarnessReportVerificationContractValidateRejectsDrift(t *testing.T) {
	contract := DefaultHarnessReportVerificationContract()
	contract.SignedScope = "report_bundle"
	err := contract.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "signed_scope")
}

func TestHarnessReportArtifactRecordValidateRejectsMissingReason(t *testing.T) {
	record := HarnessReportArtifactRecord{Name: "trace.jsonl", Status: HarnessReportArtifactStatusPartial}
	err := record.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing_reason")
}
