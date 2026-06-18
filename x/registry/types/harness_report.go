
package types

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// HarnessReportManifestVersion identifies the canonical signed harness manifest shape.
	HarnessReportManifestVersion = "lumera.harness.report_manifest.v1"
	// HarnessReportSignatureVersion identifies the detached signature envelope shape.
	HarnessReportSignatureVersion = "lumera.harness.report_signature.v1"
	// HarnessReportVerificationContractVersion identifies the verification contract shape.
	HarnessReportVerificationContractVersion = "lumera.harness.report_verification.v1"
	// HarnessReportSignatureAlgorithmEd448 is the only allowed harness report signature algorithm.
	HarnessReportSignatureAlgorithmEd448 = "ed448"
	// HarnessReportSignedScopeManifest means the detached signature covers the canonical manifest bytes.
	HarnessReportSignedScopeManifest = "report_manifest"
	// HarnessReportCanonicalizerRFC8785JCS is the canonical JSON transform applied before signing.
	HarnessReportCanonicalizerRFC8785JCS = "RFC8785-JCS"
	// HarnessReportArtifactHashAlgorithmSHA256 is the required artifact digest algorithm.
	HarnessReportArtifactHashAlgorithmSHA256 = "sha256"
	// HarnessReportArtifactMerkleAlgorithmSHA256 is the canonical artifact Merkle construction.
	HarnessReportArtifactMerkleAlgorithmSHA256 = "sha256-name-payload-v1"
	// HarnessReportScenarioClassGolden is the canonical golden-task scenario class.
	HarnessReportScenarioClassGolden = "golden"
	// HarnessReportScenarioClassEconomic is the canonical economic simulation scenario class.
	HarnessReportScenarioClassEconomic = "economic"
	// HarnessReportScenarioClassSecurity is the canonical security probe scenario class.
	HarnessReportScenarioClassSecurity = "security"

	harnessReportSHA256DigestPrefix = "sha256:"
	harnessReportSHA256DigestLength = len(harnessReportSHA256DigestPrefix) + 64
	harnessReportEd448Prefix        = "ed448:"
	harnessReportEd448PublicKeyHex  = 114
)

// HarnessReportArtifactStatus records how an artifact participates in report verification.
type HarnessReportArtifactStatus string

const (
	HarnessReportArtifactStatusPresent         HarnessReportArtifactStatus = "present"
	HarnessReportArtifactStatusMissingOptional HarnessReportArtifactStatus = "missing_optional"
	HarnessReportArtifactStatusMissingRequired HarnessReportArtifactStatus = "missing_required"
	HarnessReportArtifactStatusPartial         HarnessReportArtifactStatus = "partial"
)

// HarnessReportVerificationCode identifies a specific contract-level verification outcome.
type HarnessReportVerificationCode string

const (
	HarnessReportVerificationCodeMissingManifest         HarnessReportVerificationCode = "missing_manifest"
	HarnessReportVerificationCodeInvalidManifest         HarnessReportVerificationCode = "invalid_manifest"
	HarnessReportVerificationCodeManifestDigestMismatch  HarnessReportVerificationCode = "manifest_digest_mismatch"
	HarnessReportVerificationCodeMissingSignature        HarnessReportVerificationCode = "missing_signature"
	HarnessReportVerificationCodeInvalidSignature        HarnessReportVerificationCode = "invalid_signature"
	HarnessReportVerificationCodeInvalidSignatureMeta    HarnessReportVerificationCode = "invalid_signature_metadata"
	HarnessReportVerificationCodeUnresolvedSignerKey     HarnessReportVerificationCode = "unresolved_signer_key"
	HarnessReportVerificationCodeMerkleRootMismatch      HarnessReportVerificationCode = "merkle_root_mismatch"
	HarnessReportVerificationCodeMissingRequiredArtifact HarnessReportVerificationCode = "missing_required_artifact"
	HarnessReportVerificationCodePartialArtifact         HarnessReportVerificationCode = "partial_artifact"
	HarnessReportVerificationCodeMissingOptionalArtifact HarnessReportVerificationCode = "missing_optional_artifact"
	HarnessReportVerificationCodeArtifactInventoryDrift  HarnessReportVerificationCode = "artifact_inventory_drift"
)

// HarnessReportFailureSemantic defines one user-visible failure outcome in the verification contract.
type HarnessReportFailureSemantic struct {
	Code           HarnessReportVerificationCode `json:"code"`
	UserMessage    string                        `json:"user_message"`
	OperatorAction string                        `json:"operator_action"`
}

// HarnessReportVerificationContract defines what is signed, how it is verified, and how failures surface.
type HarnessReportVerificationContract struct {
	Version                   string                         `json:"version"`
	ManifestVersion           string                         `json:"manifest_version"`
	SignatureVersion          string                         `json:"signature_version"`
	SignatureAlgorithm        string                         `json:"signature_algorithm"`
	SignatureCanonicalizer    string                         `json:"signature_canonicalizer"`
	SignedScope               string                         `json:"signed_scope"`
	ArtifactHashAlgorithm     string                         `json:"artifact_hash_algorithm"`
	ArtifactMerkleAlgorithm   string                         `json:"artifact_merkle_algorithm"`
	OptionalArtifactSemantics string                         `json:"optional_artifact_semantics"`
	PartialArtifactSemantics  string                         `json:"partial_artifact_semantics"`
	FailureReportingSemantics string                         `json:"failure_reporting_semantics"`
	FixturePlan               []string                       `json:"fixture_plan,omitempty"`
	ReplayInputs              []string                       `json:"replay_inputs,omitempty"`
	RequiredLogFields         []string                       `json:"required_log_fields,omitempty"`
	SuccessSemantics          string                         `json:"success_semantics"`
	FailureSemantics          []HarnessReportFailureSemantic `json:"failure_semantics,omitempty"`
}

// DefaultHarnessReportVerificationContract returns the canonical v1 verification contract.
func DefaultHarnessReportVerificationContract() HarnessReportVerificationContract {
	return HarnessReportVerificationContract{
		Version:                   HarnessReportVerificationContractVersion,
		ManifestVersion:           HarnessReportManifestVersion,
		SignatureVersion:          HarnessReportSignatureVersion,
		SignatureAlgorithm:        HarnessReportSignatureAlgorithmEd448,
		SignatureCanonicalizer:    HarnessReportCanonicalizerRFC8785JCS,
		SignedScope:               HarnessReportSignedScopeManifest,
		ArtifactHashAlgorithm:     HarnessReportArtifactHashAlgorithmSHA256,
		ArtifactMerkleAlgorithm:   HarnessReportArtifactMerkleAlgorithmSHA256,
		OptionalArtifactSemantics: "missing_optional artifacts stay listed in the signed inventory but are excluded from the Merkle root and only surface as informational findings",
		PartialArtifactSemantics:  "partial artifacts are never trusted, never included in the Merkle root, and always fail verification until the full payload is restored",
		FailureReportingSemantics: "verification must return structured issue codes, fail closed for every issue except missing_optional_artifact, and preserve enough detail for replay and audit tooling",
		FixturePlan: []string{
			"Unit fixtures must include a fully signed report, a report with a missing optional artifact, and negative-path vectors for bad signature, bad Merkle root, missing required artifact, and partial artifact inventory.",
			"Implementation beads must keep deterministic Ed448 fixtures and canonical manifest vectors so future tooling can verify the exact bytes that were signed.",
		},
		ReplayInputs: []string{
			"manifest.run.run_id",
			"manifest.run.tool_id",
			"manifest.run.policy_version",
			"manifest.scenario.job_type",
			"manifest.scenario.manifest_path",
			"manifest.artifact_inventory",
			"manifest.score_digest",
		},
		RequiredLogFields: []string{
			"run_id",
			"job_id",
			"tool_id",
			"manifest_path",
			"trace_id",
			"artifact_name",
			"artifact_status",
			"verification_code",
		},
		SuccessSemantics: "A harness report verifies only when the canonical manifest bytes, score digest, present artifact inventory, Merkle root, and detached Ed448 signature all agree. Optional missing artifacts may be disclosed without failing the report.",
		FailureSemantics: []HarnessReportFailureSemantic{
			{Code: HarnessReportVerificationCodeInvalidSignature, UserMessage: "The signed harness report could not be authenticated.", OperatorAction: "Rebuild the canonical manifest bytes, verify the configured Ed448 signer, and re-issue the report."},
			{Code: HarnessReportVerificationCodeMerkleRootMismatch, UserMessage: "The report artifact bundle no longer matches its signed Merkle root.", OperatorAction: "Recompute artifact hashes from the canonical inventory and recover or regenerate the missing or modified artifacts."},
			{Code: HarnessReportVerificationCodeMissingRequiredArtifact, UserMessage: "A required harness artifact is missing from the signed inventory.", OperatorAction: "Restore the required artifact from canonical storage or rerun the harness so the signed inventory is complete."},
			{Code: HarnessReportVerificationCodePartialArtifact, UserMessage: "Only a partial artifact payload is available, so the report fails closed.", OperatorAction: "Fetch the full artifact payload before trusting the report or rerun the scenario."},
		},
	}
}

// Validate ensures the verification contract matches the canonical v1 semantics.
func (c HarnessReportVerificationContract) Validate() error {
	if strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("verification contract version is required")
	}
	switch c.Version {
	case HarnessReportVerificationContractVersion:
	default:
		return fmt.Errorf("verification contract version is not supported: %s", c.Version)
	}
	switch c.ManifestVersion {
	case HarnessReportManifestVersion:
	default:
		return fmt.Errorf("verification contract manifest_version must be %s", HarnessReportManifestVersion)
	}
	switch c.SignatureVersion {
	case HarnessReportSignatureVersion:
	default:
		return fmt.Errorf("verification contract signature_version must be %s", HarnessReportSignatureVersion)
	}
	switch c.SignatureAlgorithm {
	case HarnessReportSignatureAlgorithmEd448:
	default:
		return fmt.Errorf("verification contract signature_algorithm must be %s", HarnessReportSignatureAlgorithmEd448)
	}
	switch c.SignatureCanonicalizer {
	case HarnessReportCanonicalizerRFC8785JCS:
	default:
		return fmt.Errorf("verification contract signature_canonicalizer must be %s", HarnessReportCanonicalizerRFC8785JCS)
	}
	switch c.SignedScope {
	case HarnessReportSignedScopeManifest:
	default:
		return fmt.Errorf("verification contract signed_scope must be %s", HarnessReportSignedScopeManifest)
	}
	switch c.ArtifactHashAlgorithm {
	case HarnessReportArtifactHashAlgorithmSHA256:
	default:
		return fmt.Errorf("verification contract artifact_hash_algorithm must be %s", HarnessReportArtifactHashAlgorithmSHA256)
	}
	switch c.ArtifactMerkleAlgorithm {
	case HarnessReportArtifactMerkleAlgorithmSHA256:
	default:
		return fmt.Errorf("verification contract artifact_merkle_algorithm must be %s", HarnessReportArtifactMerkleAlgorithmSHA256)
	}
	if strings.TrimSpace(c.OptionalArtifactSemantics) == "" {
		return fmt.Errorf("verification contract optional_artifact_semantics is required")
	}
	if strings.TrimSpace(c.PartialArtifactSemantics) == "" {
		return fmt.Errorf("verification contract partial_artifact_semantics is required")
	}
	if strings.TrimSpace(c.FailureReportingSemantics) == "" {
		return fmt.Errorf("verification contract failure_reporting_semantics is required")
	}
	if len(c.FixturePlan) == 0 {
		return fmt.Errorf("verification contract fixture_plan is required")
	}
	if err := validateRequiredStringEntries("verification contract fixture_plan", c.FixturePlan); err != nil {
		return err
	}
	if len(c.ReplayInputs) == 0 {
		return fmt.Errorf("verification contract replay_inputs is required")
	}
	if err := validateRequiredStringEntries("verification contract replay_inputs", c.ReplayInputs); err != nil {
		return err
	}
	if len(c.RequiredLogFields) == 0 {
		return fmt.Errorf("verification contract required_log_fields is required")
	}
	if err := validateRequiredStringEntries("verification contract required_log_fields", c.RequiredLogFields); err != nil {
		return err
	}
	if strings.TrimSpace(c.SuccessSemantics) == "" {
		return fmt.Errorf("verification contract success_semantics is required")
	}
	if len(c.FailureSemantics) == 0 {
		return fmt.Errorf("verification contract failure_semantics is required")
	}
	for idx, semantic := range c.FailureSemantics {
		fieldPrefix := fmt.Sprintf("verification contract failure_semantics[%d]", idx)
		if strings.TrimSpace(string(semantic.Code)) == "" {
			return fmt.Errorf("%s code is required", fieldPrefix)
		}
		if !isSupportedHarnessReportVerificationCode(semantic.Code) {
			return fmt.Errorf("%s code is not supported: %s", fieldPrefix, semantic.Code)
		}
		if strings.TrimSpace(semantic.UserMessage) == "" {
			return fmt.Errorf("%s user_message is required", fieldPrefix)
		}
		if strings.TrimSpace(semantic.OperatorAction) == "" {
			return fmt.Errorf("%s operator_action is required", fieldPrefix)
		}
	}
	return nil
}

func isSupportedHarnessReportVerificationCode(code HarnessReportVerificationCode) bool {
	switch code {
	case HarnessReportVerificationCodeMissingManifest,
		HarnessReportVerificationCodeInvalidManifest,
		HarnessReportVerificationCodeManifestDigestMismatch,
		HarnessReportVerificationCodeMissingSignature,
		HarnessReportVerificationCodeInvalidSignature,
		HarnessReportVerificationCodeInvalidSignatureMeta,
		HarnessReportVerificationCodeUnresolvedSignerKey,
		HarnessReportVerificationCodeMerkleRootMismatch,
		HarnessReportVerificationCodeMissingRequiredArtifact,
		HarnessReportVerificationCodePartialArtifact,
		HarnessReportVerificationCodeMissingOptionalArtifact,
		HarnessReportVerificationCodeArtifactInventoryDrift:
		return true
	default:
		return false
	}
}

func validateHarnessReportSHA256Digest(field, value string) error {
	if len(value) != harnessReportSHA256DigestLength || !strings.HasPrefix(value, harnessReportSHA256DigestPrefix) {
		return fmt.Errorf("%s must be sha256:<64 lowercase hex>", field)
	}
	for _, ch := range value[len(harnessReportSHA256DigestPrefix):] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return fmt.Errorf("%s must be sha256:<64 lowercase hex>", field)
		}
	}
	return nil
}

func validateHarnessReportEd448Hex(field, value string, exactHexLen int) error {
	if !strings.HasPrefix(value, harnessReportEd448Prefix) {
		return fmt.Errorf("%s must be ed448:<lowercase hex>", field)
	}
	hexValue := value[len(harnessReportEd448Prefix):]
	if exactHexLen > 0 {
		if len(hexValue) != exactHexLen {
			return fmt.Errorf("%s must be ed448:<%d lowercase hex chars>", field, exactHexLen)
		}
	} else if hexValue == "" {
		return fmt.Errorf("%s must be ed448:<lowercase hex>", field)
	}
	for _, ch := range hexValue {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return fmt.Errorf("%s must be ed448:<lowercase hex>", field)
		}
	}
	return nil
}

func validateOptionalHarnessReportScenarioClass(field, value string) error {
	if value == "" {
		return nil
	}
	if err := validateRequiredIdentifier(field, value); err != nil {
		return err
	}
	switch value {
	case HarnessReportScenarioClassGolden,
		HarnessReportScenarioClassEconomic,
		HarnessReportScenarioClassSecurity:
		return nil
	default:
		return fmt.Errorf("%s must be one of golden, economic, security", field)
	}
}

func validateRequiredStringEntries(field string, values []string) error {
	for idx, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s[%d] is required", field, idx)
		}
	}
	return nil
}

// HarnessReportRunRef identifies the run whose artifacts and score are being signed.
type HarnessReportRunRef struct {
	RunID          string    `json:"run_id"`
	JobID          string    `json:"job_id,omitempty"`
	ToolID         string    `json:"tool_id"`
	Category       string    `json:"category,omitempty"`
	PolicyVersion  string    `json:"policy_version,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
	DurationMillis int64     `json:"duration_ms"`
}

type harnessReportRunRefJSON struct {
	RunID          string     `json:"run_id"`
	JobID          string     `json:"job_id,omitempty"`
	ToolID         string     `json:"tool_id"`
	Category       string     `json:"category,omitempty"`
	PolicyVersion  string     `json:"policy_version,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	DurationMillis int64      `json:"duration_ms"`
}

// MarshalJSON preserves the canonical report contract by omitting zero timestamps.
func (r HarnessReportRunRef) MarshalJSON() ([]byte, error) {
	payload := harnessReportRunRefJSON{
		RunID:          r.RunID,
		JobID:          r.JobID,
		ToolID:         r.ToolID,
		Category:       r.Category,
		PolicyVersion:  r.PolicyVersion,
		DurationMillis: r.DurationMillis,
	}
	if !r.StartedAt.IsZero() {
		startedAt := r.StartedAt
		payload.StartedAt = &startedAt
	}
	if !r.CompletedAt.IsZero() {
		completedAt := r.CompletedAt
		payload.CompletedAt = &completedAt
	}
	return json.Marshal(payload)
}

// HarnessReportScenarioRef records the scenario inputs needed for replay and audit.
type HarnessReportScenarioRef struct {
	JobType       string            `json:"job_type,omitempty"`
	ManifestPath  string            `json:"manifest_path,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	LogEntryCount int               `json:"log_entry_count,omitempty"`
}

// HarnessReportArtifactRecord captures one artifact's integrity posture in the signed manifest.
type HarnessReportArtifactRecord struct {
	Name          string                      `json:"name"`
	SHA256        string                      `json:"sha256,omitempty"`
	SizeBytes     int                         `json:"size_bytes,omitempty"`
	Optional      bool                        `json:"optional,omitempty"`
	Status        HarnessReportArtifactStatus `json:"status"`
	MissingReason string                      `json:"missing_reason,omitempty"`
}

// Validate ensures the artifact record is semantically valid for the v1 contract.
func (r HarnessReportArtifactRecord) Validate() error {
	if err := validateRequiredIdentifier("artifact name", r.Name); err != nil {
		return err
	}
	switch r.Status {
	case HarnessReportArtifactStatusPresent:
		if strings.TrimSpace(r.SHA256) == "" {
			return fmt.Errorf("present artifact %s requires sha256", r.Name)
		}
		if err := validateRequiredIdentifier(fmt.Sprintf("present artifact %s sha256", r.Name), r.SHA256); err != nil {
			return err
		}
		if err := validateHarnessReportSHA256Digest(fmt.Sprintf("present artifact %s sha256", r.Name), r.SHA256); err != nil {
			return err
		}
	case HarnessReportArtifactStatusMissingOptional, HarnessReportArtifactStatusMissingRequired, HarnessReportArtifactStatusPartial:
		if strings.TrimSpace(r.MissingReason) == "" {
			return fmt.Errorf("artifact %s with status %s requires missing_reason", r.Name, r.Status)
		}
		if err := validateRequiredIdentifier(fmt.Sprintf("artifact %s missing_reason", r.Name), r.MissingReason); err != nil {
			return err
		}
	default:
		return fmt.Errorf("artifact %s has unsupported status %s", r.Name, r.Status)
	}
	return nil
}

// HarnessReportManifest is the canonical payload signed by the harness report signer.
type HarnessReportManifest struct {
	Version                 string                        `json:"version"`
	GeneratedAt             time.Time                     `json:"generated_at"`
	Run                     HarnessReportRunRef           `json:"run"`
	Scenario                HarnessReportScenarioRef      `json:"scenario"`
	ArtifactInventory       []HarnessReportArtifactRecord `json:"artifact_inventory,omitempty"`
	ArtifactHashAlgorithm   string                        `json:"artifact_hash_algorithm"`
	ArtifactMerkleAlgorithm string                        `json:"artifact_merkle_algorithm"`
	ArtifactMerkleRoot      string                        `json:"artifact_merkle_root,omitempty"`
	ScoreDigest             string                        `json:"score_digest,omitempty"`
}

// Validate ensures the manifest shape matches the canonical v1 contract.
func (m HarnessReportManifest) Validate() error {
	switch m.Version {
	case HarnessReportManifestVersion:
	default:
		return fmt.Errorf("manifest version must be %s", HarnessReportManifestVersion)
	}
	if err := validateRequiredIdentifier("manifest run_id", m.Run.RunID); err != nil {
		return err
	}
	if err := validateCanonicalToolID("manifest tool_id", m.Run.ToolID); err != nil {
		return err
	}
	if err := validateOptionalHarnessReportScenarioClass("manifest run category", m.Run.Category); err != nil {
		return err
	}
	if err := validateOptionalHarnessReportScenarioClass("manifest scenario job_type", m.Scenario.JobType); err != nil {
		return err
	}
	switch m.ArtifactHashAlgorithm {
	case HarnessReportArtifactHashAlgorithmSHA256:
	default:
		return fmt.Errorf("manifest artifact_hash_algorithm must be %s", HarnessReportArtifactHashAlgorithmSHA256)
	}
	switch m.ArtifactMerkleAlgorithm {
	case HarnessReportArtifactMerkleAlgorithmSHA256:
	default:
		return fmt.Errorf("manifest artifact_merkle_algorithm must be %s", HarnessReportArtifactMerkleAlgorithmSHA256)
	}
	if m.ArtifactMerkleRoot != "" {
		if err := validateRequiredIdentifier("manifest artifact_merkle_root", m.ArtifactMerkleRoot); err != nil {
			return err
		}
		if err := validateHarnessReportSHA256Digest("manifest artifact_merkle_root", m.ArtifactMerkleRoot); err != nil {
			return err
		}
	}
	if m.ScoreDigest != "" {
		if err := validateRequiredIdentifier("manifest score_digest", m.ScoreDigest); err != nil {
			return err
		}
		if err := validateHarnessReportSHA256Digest("manifest score_digest", m.ScoreDigest); err != nil {
			return err
		}
	}
	for _, artifact := range m.ArtifactInventory {
		if err := artifact.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// HarnessReportSignatureEnvelope defines the detached Ed448 signature over the canonical manifest bytes.
type HarnessReportSignatureEnvelope struct {
	Version        string    `json:"version"`
	Algorithm      string    `json:"algorithm"`
	Canonicalizer  string    `json:"canonicalizer"`
	Scope          string    `json:"scope"`
	SignerIdentity string    `json:"signer_identity"`
	KeyID          string    `json:"key_id,omitempty"`
	PublicKey      string    `json:"public_key,omitempty"`
	PayloadDigest  string    `json:"payload_digest"`
	Signature      string    `json:"signature"`
	SignedAt       time.Time `json:"signed_at"`
}

// Validate ensures the detached signature envelope matches the canonical v1 contract.
func (e *HarnessReportSignatureEnvelope) Validate() error {
	if e == nil {
		return fmt.Errorf("signature envelope is required")
	}
	switch e.Version {
	case HarnessReportSignatureVersion:
	default:
		return fmt.Errorf("signature version must be %s", HarnessReportSignatureVersion)
	}
	switch e.Algorithm {
	case HarnessReportSignatureAlgorithmEd448:
	default:
		return fmt.Errorf("signature algorithm must be %s", HarnessReportSignatureAlgorithmEd448)
	}
	switch e.Canonicalizer {
	case HarnessReportCanonicalizerRFC8785JCS:
	default:
		return fmt.Errorf("signature canonicalizer must be %s", HarnessReportCanonicalizerRFC8785JCS)
	}
	switch e.Scope {
	case HarnessReportSignedScopeManifest:
	default:
		return fmt.Errorf("signature scope must be %s", HarnessReportSignedScopeManifest)
	}
	if err := validateRequiredIdentifier("signature signer_identity", e.SignerIdentity); err != nil {
		return err
	}
	if e.PublicKey != "" {
		if err := validateRequiredIdentifier("signature public_key", e.PublicKey); err != nil {
			return err
		}
		if err := validateHarnessReportEd448Hex("signature public_key", e.PublicKey, harnessReportEd448PublicKeyHex); err != nil {
			return err
		}
	}
	if err := validateRequiredIdentifier("signature payload_digest", e.PayloadDigest); err != nil {
		return err
	}
	if err := validateHarnessReportSHA256Digest("signature payload_digest", e.PayloadDigest); err != nil {
		return err
	}
	if err := validateRequiredIdentifier("signature", e.Signature); err != nil {
		return err
	}
	if err := validateHarnessReportEd448Hex("signature", e.Signature, 0); err != nil {
		return err
	}
	return nil
}

// HarnessReportEnvelope is the registry-facing signed harness payload.
type HarnessReportEnvelope struct {
	Manifest             HarnessReportManifest             `json:"manifest"`
	VerificationContract HarnessReportVerificationContract `json:"verification_contract"`
	Signature            *HarnessReportSignatureEnvelope   `json:"signature,omitempty"`
}

// Validate ensures the envelope can be consumed by registry and explorer verifiers.
func (e HarnessReportEnvelope) Validate() error {
	if err := e.Manifest.Validate(); err != nil {
		return err
	}
	if err := e.VerificationContract.Validate(); err != nil {
		return err
	}
	if err := e.Signature.Validate(); err != nil {
		return err
	}
	return nil
}

// HarnessReportVerificationIssue captures one contract-level verification finding.
type HarnessReportVerificationIssue struct {
	Code    HarnessReportVerificationCode `json:"code"`
	Field   string                        `json:"field,omitempty"`
	Message string                        `json:"message"`
}

// HarnessReportVerificationResult summarizes the outcome of harness report verification.
type HarnessReportVerificationResult struct {
	Verified             bool                             `json:"verified"`
	ContractVersion      string                           `json:"contract_version"`
	SignerIdentity       string                           `json:"signer_identity,omitempty"`
	VerifiedAt           time.Time                        `json:"verified_at"`
	ArtifactCount        int                              `json:"artifact_count"`
	OptionalMissingCount int                              `json:"optional_missing_count,omitempty"`
	PartialArtifactCount int                              `json:"partial_artifact_count,omitempty"`
	Issues               []HarnessReportVerificationIssue `json:"issues,omitempty"`
}

// HarnessReportVerifier defines the registry/explorer verification surface for a signed report.
type HarnessReportVerifier interface {
	VerifyHarnessReport(ctx context.Context, report *HarnessReportEnvelope) (*HarnessReportVerificationResult, error)
}

// HarnessReportStore defines the durable storage surface for signed harness reports.
type HarnessReportStore interface {
	PutHarnessReport(ctx context.Context, report *HarnessReportEnvelope) error
	GetHarnessReport(ctx context.Context, runID string) (*HarnessReportEnvelope, bool, error)
}
