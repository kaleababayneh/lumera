
package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validHarnessReportArtifactSHA256 = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const validHarnessReportMerkleRoot = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
const validHarnessReportScoreDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

// validHarnessManifest returns a manifest that passes Validate.
// Tests mutate one field at a time to exercise individual branches.
func validHarnessManifest() HarnessReportManifest {
	return HarnessReportManifest{
		Version:     HarnessReportManifestVersion,
		GeneratedAt: time.Now().UTC(),
		Run: HarnessReportRunRef{
			RunID:  "run-abc",
			ToolID: "tool-1",
		},
		ArtifactHashAlgorithm:   HarnessReportArtifactHashAlgorithmSHA256,
		ArtifactMerkleAlgorithm: HarnessReportArtifactMerkleAlgorithmSHA256,
	}
}

// TestHarnessReportManifestValidate_HappyPath pins the full
// validator at harness_report.go:286-308. Prior coverage for
// this file only exercised the ArtifactRecord child validator
// (TestHarnessReportArtifactRecordValidateRejectsMissingReason)
// and not the parent Manifest's own branches.
func TestHarnessReportManifestValidate_HappyPath(t *testing.T) {
	t.Parallel()
	require.NoError(t, validHarnessManifest().Validate())
}

// TestHarnessReportManifestValidate_RejectsVersionMismatch pins
// the version guard at :287-289. The manifest contract is
// versioned for forward-compatibility; any future v2 manifest
// being passed through a v1 signer MUST be rejected so the
// signer never endorses a shape the verifier can't parse.
func TestHarnessReportManifestValidate_RejectsVersionMismatch(t *testing.T) {
	t.Parallel()
	m := validHarnessManifest()
	m.Version = "lumera.harness.report_manifest.v2"
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), HarnessReportManifestVersion)
}

// TestHarnessReportManifestValidate_RejectsEmptyVersion pins the
// same version guard for the empty case — a manifest produced
// before the version field was wired up must also be rejected.
func TestHarnessReportManifestValidate_RejectsEmptyVersion(t *testing.T) {
	t.Parallel()
	m := validHarnessManifest()
	m.Version = ""
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest version")
}

// TestHarnessReportManifestValidate_RejectsEmptyRunID pins the
// run_id guard at :290-292. A manifest without a RunID cannot be
// correlated to the actual harness run that produced it — the
// signature would bind to an anonymous report, breaking the audit
// trail that downstream tooling relies on.
func TestHarnessReportManifestValidate_RejectsEmptyRunID(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "   ", "\t"} {
		m := validHarnessManifest()
		m.Run.RunID = bad
		err := m.Validate()
		require.Errorf(t, err, "RunID=%q must be rejected", bad)
		assert.Contains(t, err.Error(), "run_id is required")
	}
}

func TestHarnessReportManifestValidate_RejectsPaddedRunID(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{" run-abc", "run-abc ", "\trun-abc"} {
		m := validHarnessManifest()
		m.Run.RunID = bad
		err := m.Validate()
		require.Errorf(t, err, "RunID=%q must be rejected", bad)
		assert.Contains(t, err.Error(), "run_id")
	}
}

// TestHarnessReportManifestValidate_RejectsEmptyToolID pins the
// tool_id guard at :293-295. Same rationale as RunID: without the
// tool binding, the signature endorses a report that can't be
// traced back to its subject.
func TestHarnessReportManifestValidate_RejectsEmptyToolID(t *testing.T) {
	t.Parallel()
	m := validHarnessManifest()
	m.Run.ToolID = ""
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool_id is required")
}

func TestHarnessReportManifestValidate_RejectsPaddedToolID(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{" tool-1", "tool-1 ", "\ttool-1"} {
		m := validHarnessManifest()
		m.Run.ToolID = bad
		err := m.Validate()
		require.Errorf(t, err, "ToolID=%q must be rejected", bad)
		assert.Contains(t, err.Error(), "tool_id")
	}
}

func TestHarnessReportManifestValidate_RejectsUnsupportedScenarioClasses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*HarnessReportManifest)
	}{
		{
			name: "unsupported run category",
			edit: func(m *HarnessReportManifest) {
				m.Run.Category = "loadtest"
			},
		},
		{
			name: "unsupported scenario job type",
			edit: func(m *HarnessReportManifest) {
				m.Scenario.JobType = "loadtest"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validHarnessManifest()
			tt.edit(&m)
			err := m.Validate()
			require.ErrorContains(t, err, "must be one of golden, economic, security")
		})
	}
}

// TestHarnessReportManifestValidate_RejectsNonSHA256HashAlgorithm
// pins the hash-algorithm pin at :296-298. The manifest's
// contract is SHA-256 specifically; accepting any other algorithm
// would break downstream signature-verification code that uses
// crypto/sha256 on the hex payload. A drift here would silently
// mismatch hashes across signer and verifier.
func TestHarnessReportManifestValidate_RejectsNonSHA256HashAlgorithm(t *testing.T) {
	t.Parallel()
	m := validHarnessManifest()
	m.ArtifactHashAlgorithm = "sha512" // drift from v1 contract
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), HarnessReportArtifactHashAlgorithmSHA256)
}

// TestHarnessReportManifestValidate_RejectsNonSHA256MerkleAlgorithm
// pins the Merkle-algorithm pin at :299-301. The v1 contract
// specifies "sha256-name-payload-v1" — any drift would break the
// merkle-root reconstruction at verification time.
func TestHarnessReportManifestValidate_RejectsNonSHA256MerkleAlgorithm(t *testing.T) {
	t.Parallel()
	m := validHarnessManifest()
	m.ArtifactMerkleAlgorithm = "keccak256-v1"
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), HarnessReportArtifactMerkleAlgorithmSHA256)
}

// TestHarnessReportManifestValidate_PropagatesArtifactError pins
// the for-loop delegation at :302-306. An inventory containing
// an invalid artifact (e.g., Partial status without missing_reason)
// must fail manifest validation — defended against a regression
// where only the MANIFEST's top-level fields got validated while
// the inventory was silently accepted.
func TestHarnessReportManifestValidate_PropagatesArtifactError(t *testing.T) {
	t.Parallel()
	m := validHarnessManifest()
	m.ArtifactInventory = []HarnessReportArtifactRecord{
		{Name: "valid.jsonl", Status: HarnessReportArtifactStatusPresent, SHA256: validHarnessReportArtifactSHA256},
		// This one is invalid: Partial status requires a MissingReason.
		{Name: "bad.jsonl", Status: HarnessReportArtifactStatusPartial},
	}
	err := m.Validate()
	require.Error(t, err,
		"manifest validation must propagate artifact-level errors; without this, "+
			"a manifest with an invalid inventory would be signed against, "+
			"committing the chain to an unverifiable report")
	assert.Contains(t, err.Error(), "missing_reason")
}

// TestHarnessReportManifestValidate_AcceptsValidArtifactInventory
// pins the happy path through the delegation: a manifest with
// multiple well-formed artifacts validates cleanly.
func TestHarnessReportManifestValidate_AcceptsValidArtifactInventory(t *testing.T) {
	t.Parallel()
	m := validHarnessManifest()
	m.ArtifactInventory = []HarnessReportArtifactRecord{
		{Name: "trace.jsonl", Status: HarnessReportArtifactStatusPresent, SHA256: validHarnessReportArtifactSHA256},
		{Name: "stats.json", Status: HarnessReportArtifactStatusMissingOptional, MissingReason: "opted out"},
		{Name: "logs.tgz", Status: HarnessReportArtifactStatusMissingRequired, MissingReason: "disk full"},
	}
	require.NoError(t, m.Validate())
}

func TestHarnessReportManifestValidate_RejectsMalformedDigestFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*HarnessReportManifest)
	}{
		{
			name: "short artifact merkle root",
			edit: func(m *HarnessReportManifest) {
				m.ArtifactMerkleRoot = "sha256:def456"
			},
		},
		{
			name: "uppercase artifact merkle root",
			edit: func(m *HarnessReportManifest) {
				m.ArtifactMerkleRoot = "sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
			},
		},
		{
			name: "wrong score digest prefix",
			edit: func(m *HarnessReportManifest) {
				m.ScoreDigest = "blake3:2222222222222222222222222222222222222222222222222222222222222222"
			},
		},
		{
			name: "non hex score digest",
			edit: func(m *HarnessReportManifest) {
				m.ScoreDigest = "sha256:222222222222222222222222222222222222222222222222222222222222222g"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validHarnessManifest()
			m.ArtifactMerkleRoot = validHarnessReportMerkleRoot
			m.ScoreDigest = validHarnessReportScoreDigest
			tt.edit(&m)
			err := m.Validate()
			require.ErrorContains(t, err, "must be sha256:<64 lowercase hex>")
		})
	}
}

// TestHarnessReportArtifactRecordValidate_RejectsEmptyName pins
// the name guard at :254-256. An empty name would produce an
// artifact entry that can't be addressed by downstream fetchers.
// Existing test file covers Partial-without-reason; this one
// complements by exercising the first guard.
func TestHarnessReportArtifactRecordValidate_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "   ", "\t"} {
		r := HarnessReportArtifactRecord{
			Name:   bad,
			Status: HarnessReportArtifactStatusPresent,
			SHA256: validHarnessReportArtifactSHA256,
		}
		err := r.Validate()
		require.Errorf(t, err, "Name=%q must be rejected", bad)
		assert.Contains(t, err.Error(), "name is required")
	}
}

func TestHarnessReportArtifactRecordValidate_RejectsPaddedInventoryFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		record HarnessReportArtifactRecord
	}{
		{
			name: "padded artifact name",
			record: HarnessReportArtifactRecord{
				Name:   " trace.jsonl",
				Status: HarnessReportArtifactStatusPresent,
				SHA256: validHarnessReportArtifactSHA256,
			},
		},
		{
			name: "padded present sha256",
			record: HarnessReportArtifactRecord{
				Name:   "trace.jsonl",
				Status: HarnessReportArtifactStatusPresent,
				SHA256: validHarnessReportArtifactSHA256 + " ",
			},
		},
		{
			name: "padded missing reason",
			record: HarnessReportArtifactRecord{
				Name:          "trace.jsonl",
				Status:        HarnessReportArtifactStatusMissingRequired,
				MissingReason: "\tdisk full",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.record.Validate()
			require.ErrorContains(t, err, "must not contain leading or trailing whitespace")
		})
	}
}

func TestHarnessReportArtifactRecordValidate_RejectsMalformedSHA256(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"abc123",
		"blake3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"sha256:abc123",
		"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdeg",
		"sha256:0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		r := HarnessReportArtifactRecord{
			Name:   "trace.jsonl",
			Status: HarnessReportArtifactStatusPresent,
			SHA256: bad,
		}
		err := r.Validate()
		require.ErrorContains(t, err, "must be sha256:<64 lowercase hex>")
	}
}

// TestHarnessReportArtifactRecordValidate_PresentRequiresSHA256
// pins the Present-status guard at :258-260. A present artifact
// without a SHA256 can't be verified against the Merkle root —
// reviewers see a "present" artifact in the inventory but can't
// prove the actual bytes match.
func TestHarnessReportArtifactRecordValidate_PresentRequiresSHA256(t *testing.T) {
	t.Parallel()
	r := HarnessReportArtifactRecord{
		Name:   "trace.jsonl",
		Status: HarnessReportArtifactStatusPresent,
		SHA256: "", // missing
	}
	err := r.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires sha256")
}

// TestHarnessReportArtifactRecordValidate_RejectsUnsupportedStatus
// pins the default-case at :266-267. An unrecognized status
// string must be rejected so the signer never endorses a report
// whose status enum the verifier can't interpret.
func TestHarnessReportArtifactRecordValidate_RejectsUnsupportedStatus(t *testing.T) {
	t.Parallel()
	r := HarnessReportArtifactRecord{
		Name:   "mystery.dat",
		Status: "corrupted", // not in the defined enum
	}
	err := r.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported status")
}
