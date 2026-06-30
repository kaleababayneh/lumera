package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	WorkflowOutputClaimHashAlgorithm = "sha256:jcs:workflow-output-claim:v1"

	maxWorkflowOutputClaimsPerStep = 32
)

// WorkflowOutputClaim is deterministic receipt evidence for a scalar output
// claim exposed to workflow conditions. Redacted claims omit CanonicalValue and
// keep the same claim hash commitment.
type WorkflowOutputClaim struct {
	Path           []string `json:"path"`
	ScalarKind     string   `json:"scalar_kind"`
	CanonicalValue string   `json:"canonical_value,omitempty"`
	ClaimHash      []byte   `json:"claim_hash"`
	HashAlgorithm  string   `json:"hash_algorithm"`
	Redacted       bool     `json:"redacted,omitempty"`
}

type workflowOutputClaimHashPayload struct {
	Algorithm      string   `json:"algorithm"`
	StepID         string   `json:"step_id"`
	Path           []string `json:"path"`
	ScalarKind     string   `json:"scalar_kind"`
	CanonicalValue string   `json:"canonical_value"`
}

// NewWorkflowOutputClaimCommitment returns redacted condition evidence for a
// single scalar claim. The canonical scalar value is hashed but not persisted.
func NewWorkflowOutputClaimCommitment(stepID string, path []string, literal WorkflowConditionLiteral) (WorkflowOutputClaim, error) {
	return newWorkflowOutputClaim(stepID, path, literal, true)
}

// NewWorkflowOutputClaimReveal returns condition evidence that carries the
// canonical scalar value alongside its commitment.
func NewWorkflowOutputClaimReveal(stepID string, path []string, literal WorkflowConditionLiteral) (WorkflowOutputClaim, error) {
	return newWorkflowOutputClaim(stepID, path, literal, false)
}

// EvaluateWorkflowOutputClaimCondition verifies receipt evidence for an output
// claim condition and returns the deterministic comparison result.
func EvaluateWorkflowOutputClaimCondition(step WorkflowStepInvocation, condition WorkflowCondition) (bool, error) {
	if condition.Kind != WorkflowConditionKindOutputClaimComparison {
		return false, ErrInvalidWorkflow.Wrap("workflow condition is not an output claim comparison")
	}
	if strings.TrimSpace(step.StepID) != condition.StepID {
		return false, ErrInvalidWorkflow.Wrap("workflow output claim condition step_id mismatch")
	}
	claims, err := NormalizeWorkflowOutputClaimsForStep(step.StepID, step.OutputClaims)
	if err != nil {
		return false, err
	}
	claim, ok := findWorkflowOutputClaim(claims, condition.ClaimPath)
	if !ok {
		return false, ErrInvalidWorkflow.Wrapf("workflow output claim missing path: %s", strings.Join(condition.ClaimPath, "."))
	}
	kind, canonical, err := workflowOutputClaimCanonicalScalar(condition.Literal)
	if err != nil {
		return false, err
	}
	expectedHash, err := computeWorkflowOutputClaimHash(condition.StepID, condition.ClaimPath, kind, canonical)
	if err != nil {
		return false, err
	}
	matches := claim.ScalarKind == kind && bytes.Equal(claim.ClaimHash, expectedHash)
	if condition.Operator == WorkflowConditionOperatorNotEqual {
		return !matches, nil
	}
	return matches, nil
}

// NormalizeWorkflowOutputClaimsForStep validates, fills hash metadata when the
// canonical value is present, and returns claims in canonical path order.
func NormalizeWorkflowOutputClaimsForStep(stepID string, claims []WorkflowOutputClaim) ([]WorkflowOutputClaim, error) {
	if len(claims) == 0 {
		return nil, nil
	}
	stepID = strings.TrimSpace(stepID)
	if !workflowConditionStepIDPattern.MatchString(stepID) {
		return nil, ErrInvalidWorkflow.Wrapf("workflow output claim step_id must match %s: %q", workflowConditionStepIDPattern.String(), stepID)
	}
	if len(claims) > maxWorkflowOutputClaimsPerStep {
		return nil, ErrInvalidWorkflow.Wrapf("workflow step %s output claims exceed limit %d", stepID, maxWorkflowOutputClaimsPerStep)
	}
	out := make([]WorkflowOutputClaim, 0, len(claims))
	seen := make(map[string]struct{}, len(claims))
	for i, claim := range claims {
		normalized, key, err := normalizeWorkflowOutputClaim(stepID, claim)
		if err != nil {
			return nil, ErrInvalidWorkflow.Wrapf("workflow step %s output claim %d: %v", stepID, i, err)
		}
		if _, ok := seen[key]; ok {
			return nil, ErrInvalidWorkflow.Wrapf("workflow step %s duplicate output claim path: %s", stepID, key)
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	sort.Slice(out, func(i, j int) bool {
		return workflowOutputClaimKey(out[i].Path) < workflowOutputClaimKey(out[j].Path)
	})
	return out, nil
}

func newWorkflowOutputClaim(stepID string, path []string, literal WorkflowConditionLiteral, redacted bool) (WorkflowOutputClaim, error) {
	kind, canonical, err := workflowOutputClaimCanonicalScalar(literal)
	if err != nil {
		return WorkflowOutputClaim{}, err
	}
	claimHash, err := computeWorkflowOutputClaimHash(stepID, path, kind, canonical)
	if err != nil {
		return WorkflowOutputClaim{}, err
	}
	claim := WorkflowOutputClaim{
		Path:          append([]string(nil), path...),
		ScalarKind:    kind,
		ClaimHash:     claimHash,
		HashAlgorithm: WorkflowOutputClaimHashAlgorithm,
		Redacted:      redacted,
	}
	if !redacted {
		claim.CanonicalValue = canonical
	}
	normalized, _, err := normalizeWorkflowOutputClaim(stepID, claim)
	if err != nil {
		return WorkflowOutputClaim{}, err
	}
	return normalized, nil
}

func normalizeWorkflowOutputClaim(stepID string, claim WorkflowOutputClaim) (WorkflowOutputClaim, string, error) {
	path, err := validateWorkflowOutputClaimPath(claim.Path)
	if err != nil {
		return WorkflowOutputClaim{}, "", err
	}
	kind := strings.TrimSpace(claim.ScalarKind)
	if kind != claim.ScalarKind || !workflowOutputClaimKindSupported(kind) {
		return WorkflowOutputClaim{}, "", fmt.Errorf("scalar_kind must be bool, string, or number: %q", claim.ScalarKind)
	}
	algorithm := strings.TrimSpace(claim.HashAlgorithm)
	if algorithm == "" {
		algorithm = WorkflowOutputClaimHashAlgorithm
	}
	if algorithm != WorkflowOutputClaimHashAlgorithm {
		return WorkflowOutputClaim{}, "", fmt.Errorf("hash_algorithm must be %q", WorkflowOutputClaimHashAlgorithm)
	}
	if claim.Redacted && claim.CanonicalValue != "" {
		return WorkflowOutputClaim{}, "", fmt.Errorf("redacted output claim cannot carry canonical_value")
	}
	if !claim.Redacted {
		if strings.TrimSpace(claim.CanonicalValue) != claim.CanonicalValue {
			return WorkflowOutputClaim{}, "", fmt.Errorf("canonical_value must be canonical")
		}
		if err := validateWorkflowOutputClaimCanonicalValue(kind, claim.CanonicalValue); err != nil {
			return WorkflowOutputClaim{}, "", err
		}
		expected, err := computeWorkflowOutputClaimHash(stepID, path, kind, claim.CanonicalValue)
		if err != nil {
			return WorkflowOutputClaim{}, "", err
		}
		if len(claim.ClaimHash) == 0 {
			claim.ClaimHash = expected
		} else if !bytes.Equal(claim.ClaimHash, expected) {
			return WorkflowOutputClaim{}, "", fmt.Errorf("claim_hash does not match canonical_value")
		}
	}
	if len(claim.ClaimHash) != workflowReceiptHashSize {
		return WorkflowOutputClaim{}, "", fmt.Errorf("claim_hash must be %d bytes", workflowReceiptHashSize)
	}
	return WorkflowOutputClaim{
		Path:           path,
		ScalarKind:     kind,
		CanonicalValue: claim.CanonicalValue,
		ClaimHash:      cloneWorkflowReceiptBytes(claim.ClaimHash),
		HashAlgorithm:  algorithm,
		Redacted:       claim.Redacted,
	}, workflowOutputClaimKey(path), nil
}

func workflowOutputClaimCanonicalScalar(literal WorkflowConditionLiteral) (string, string, error) {
	switch literal.Kind {
	case WorkflowConditionLiteralBool:
		if literal.Bool {
			return string(WorkflowConditionLiteralBool), "true", nil
		}
		return string(WorkflowConditionLiteralBool), "false", nil
	case WorkflowConditionLiteralString:
		if len(literal.String) > 256 {
			return "", "", fmt.Errorf("workflow output claim string exceeds 256 bytes")
		}
		for _, r := range literal.String {
			if r < 0x20 {
				return "", "", fmt.Errorf("workflow output claim string contains a control character")
			}
		}
		canonical, err := json.Marshal(literal.String)
		if err != nil {
			return "", "", fmt.Errorf("workflow output claim string cannot be canonicalized")
		}
		return string(WorkflowConditionLiteralString), string(canonical), nil
	case WorkflowConditionLiteralNumber:
		number := strings.TrimSpace(literal.Number)
		if number != literal.Number || !workflowConditionDecimalPattern.MatchString(number) {
			return "", "", fmt.Errorf("workflow output claim number must be a canonical decimal")
		}
		if isNegativeZeroWorkflowDecimal(number) {
			return "", "", fmt.Errorf("workflow output claim decimal cannot be negative zero")
		}
		return string(WorkflowConditionLiteralNumber), number, nil
	default:
		return "", "", fmt.Errorf("workflow output claim scalar kind unsupported: %q", literal.Kind)
	}
}

func validateWorkflowOutputClaimPath(path []string) ([]string, error) {
	if len(path) == 0 || len(path) > 4 {
		return nil, fmt.Errorf("path must contain one to four segments")
	}
	out := make([]string, len(path))
	for i, segment := range path {
		if !workflowConditionClaimSegmentPattern.MatchString(segment) {
			return nil, fmt.Errorf("path segment must match %s: %q", workflowConditionClaimSegmentPattern.String(), segment)
		}
		out[i] = segment
	}
	return out, nil
}

func validateWorkflowOutputClaimCanonicalValue(kind, canonical string) error {
	literal, canonicalAgain, err := parseWorkflowConditionScalar(canonical)
	if err != nil {
		return err
	}
	parsedKind, _, err := workflowOutputClaimCanonicalScalar(literal)
	if err != nil {
		return err
	}
	if parsedKind != kind || canonicalAgain != canonical {
		return fmt.Errorf("canonical_value does not match scalar_kind")
	}
	return nil
}

func computeWorkflowOutputClaimHash(stepID string, path []string, kind, canonical string) ([]byte, error) {
	stepID = strings.TrimSpace(stepID)
	if !workflowConditionStepIDPattern.MatchString(stepID) {
		return nil, ErrInvalidWorkflow.Wrapf("workflow output claim step_id must match %s: %q", workflowConditionStepIDPattern.String(), stepID)
	}
	canonicalPath, err := validateWorkflowOutputClaimPath(path)
	if err != nil {
		return nil, err
	}
	payload := workflowOutputClaimHashPayload{
		Algorithm:      WorkflowOutputClaimHashAlgorithm,
		StepID:         stepID,
		Path:           canonicalPath,
		ScalarKind:     kind,
		CanonicalValue: canonical,
	}
	canonicalBytes, err := canonicalJSON(payload)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonicalBytes)
	return append([]byte(nil), sum[:]...), nil
}

func workflowOutputClaimKindSupported(kind string) bool {
	switch kind {
	case string(WorkflowConditionLiteralBool), string(WorkflowConditionLiteralString), string(WorkflowConditionLiteralNumber):
		return true
	default:
		return false
	}
}

func findWorkflowOutputClaim(claims []WorkflowOutputClaim, path []string) (WorkflowOutputClaim, bool) {
	want := workflowOutputClaimKey(path)
	for _, claim := range claims {
		if workflowOutputClaimKey(claim.Path) == want {
			return claim, true
		}
	}
	return WorkflowOutputClaim{}, false
}

func workflowOutputClaimKey(path []string) string {
	return strings.Join(path, ".")
}

func workflowOutputClaimsEqual(left, right []WorkflowOutputClaim) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if workflowOutputClaimKey(left[i].Path) != workflowOutputClaimKey(right[i].Path) ||
			left[i].ScalarKind != right[i].ScalarKind ||
			left[i].CanonicalValue != right[i].CanonicalValue ||
			left[i].HashAlgorithm != right[i].HashAlgorithm ||
			left[i].Redacted != right[i].Redacted ||
			!bytes.Equal(left[i].ClaimHash, right[i].ClaimHash) {
			return false
		}
	}
	return true
}

func cloneWorkflowOutputClaims(in []WorkflowOutputClaim) []WorkflowOutputClaim {
	if in == nil {
		return nil
	}
	out := make([]WorkflowOutputClaim, len(in))
	for i := range in {
		out[i] = WorkflowOutputClaim{
			Path:           append([]string(nil), in[i].Path...),
			ScalarKind:     in[i].ScalarKind,
			CanonicalValue: in[i].CanonicalValue,
			ClaimHash:      cloneWorkflowReceiptBytes(in[i].ClaimHash),
			HashAlgorithm:  in[i].HashAlgorithm,
			Redacted:       in[i].Redacted,
		}
	}
	return out
}

func workflowOutputClaimsToProto(claims []WorkflowOutputClaim) []*WorkflowOutputClaimEvidence {
	if len(claims) == 0 {
		return nil
	}
	out := make([]*WorkflowOutputClaimEvidence, 0, len(claims))
	for _, claim := range claims {
		out = append(out, &WorkflowOutputClaimEvidence{
			Path:           append([]string(nil), claim.Path...),
			ScalarKind:     claim.ScalarKind,
			CanonicalValue: claim.CanonicalValue,
			ClaimHash:      cloneWorkflowReceiptBytes(claim.ClaimHash),
			HashAlgorithm:  claim.HashAlgorithm,
			Redacted:       claim.Redacted,
		})
	}
	return out
}

func workflowOutputClaimsFromProto(claims []*WorkflowOutputClaimEvidence) []WorkflowOutputClaim {
	if len(claims) == 0 {
		return nil
	}
	out := make([]WorkflowOutputClaim, 0, len(claims))
	for _, claim := range claims {
		if claim == nil {
			continue
		}
		out = append(out, WorkflowOutputClaim{
			Path:           append([]string(nil), claim.GetPath()...),
			ScalarKind:     claim.GetScalarKind(),
			CanonicalValue: claim.GetCanonicalValue(),
			ClaimHash:      cloneWorkflowReceiptBytes(claim.GetClaimHash()),
			HashAlgorithm:  claim.GetHashAlgorithm(),
			Redacted:       claim.GetRedacted(),
		})
	}
	return out
}
