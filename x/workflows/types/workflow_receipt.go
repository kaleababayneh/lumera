package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/cloudflare/circl/sign/ed448"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
)

const workflowReceiptHashSize = sha256.Size

// WorkflowReceiptBuildOptions configures deterministic WorkflowReceipt finalization.
type WorkflowReceiptBuildOptions struct {
	ExecutorPrivateKey     ed448.PrivateKey
	ExecutorPubkey         string
	NonDeterministicInputs []*WorkflowNonDeterministicInput
	Anchors                []*WorkflowReceiptAnchor
}

type workflowStepReceiptPayload struct {
	StepID        string                `json:"step_id"`
	ToolID        string                `json:"tool_id"`
	ToolVersion   string                `json:"tool_version"`
	Outcome       string                `json:"outcome"`
	Cost          QuoteCoin             `json:"cost"`
	DurationMS    uint32                `json:"duration_ms"`
	AttemptCount  uint32                `json:"attempt_count"`
	ErrorCode     string                `json:"error_code,omitempty"`
	ErrorMessage  string                `json:"error_message,omitempty"`
	FailureAction FailureAction         `json:"failure_action,omitempty"`
	OutputClaims  []WorkflowOutputClaim `json:"output_claims,omitempty"`
}

type workflowReceiptSignaturePayload struct {
	BundleID               string                           `json:"bundle_id"`
	WorkflowID             string                           `json:"workflow_id"`
	Version                string                           `json:"version"`
	Outcome                string                           `json:"outcome"`
	StepReceipts           []WorkflowStepInvocation         `json:"step_receipts"`
	StepReceiptHashes      [][]byte                         `json:"step_receipt_hashes"`
	MerkleRoot             []byte                           `json:"merkle_root"`
	CanonicalStepOrder     []string                         `json:"canonical_step_order"`
	TotalCost              QuoteCoin                        `json:"total_cost"`
	LockID                 string                           `json:"lock_id"`
	FailureCode            string                           `json:"failure_code,omitempty"`
	FailureReason          string                           `json:"failure_reason,omitempty"`
	TraceID                string                           `json:"trace_id,omitempty"`
	CompletedAt            string                           `json:"completed_at"`
	NonDeterministicInputs []*WorkflowNonDeterministicInput `json:"non_deterministic_inputs,omitempty"`
	FailureAttributions    []*WorkflowFailureAttribution    `json:"failure_attributions,omitempty"`
	InvariantLogs          []InvariantEvaluationLog         `json:"invariant_logs,omitempty"`
	ExecutorPubkey         string                           `json:"executor_pubkey"`
}

type workflowFailureStateSnapshot struct {
	BundleID      string                       `json:"bundle_id"`
	WorkflowID    string                       `json:"workflow_id"`
	Version       string                       `json:"version"`
	Outcome       string                       `json:"outcome"`
	FailureCode   string                       `json:"failure_code,omitempty"`
	FailureReason string                       `json:"failure_reason,omitempty"`
	TraceID       string                       `json:"trace_id,omitempty"`
	TotalCost     QuoteCoin                    `json:"total_cost"`
	Step          *workflowFailureStepSnapshot `json:"step,omitempty"`
}

type workflowFailureStepSnapshot struct {
	StepID        string        `json:"step_id"`
	ToolID        string        `json:"tool_id"`
	ToolVersion   string        `json:"tool_version"`
	Outcome       string        `json:"outcome"`
	Cost          QuoteCoin     `json:"cost"`
	DurationMS    uint32        `json:"duration_ms"`
	AttemptCount  uint32        `json:"attempt_count"`
	ErrorCode     string        `json:"error_code,omitempty"`
	ErrorMessage  string        `json:"error_message,omitempty"`
	FailureAction FailureAction `json:"failure_action,omitempty"`
}

// FinalizeWorkflowReceipt computes ordered step hashes, the ordered Merkle root,
// and an optional Ed448 executor signature.
func FinalizeWorkflowReceipt(receipt *WorkflowInvocationReceipt, opts WorkflowReceiptBuildOptions) error {
	if receipt == nil {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	for i := range receipt.StepReceipts {
		claims, err := NormalizeWorkflowOutputClaimsForStep(receipt.StepReceipts[i].StepID, receipt.StepReceipts[i].OutputClaims)
		if err != nil {
			return err
		}
		receipt.StepReceipts[i].OutputClaims = claims
	}
	hashes, order, err := ComputeWorkflowReceiptStepHashes(receipt.StepReceipts)
	if err != nil {
		return err
	}
	root, err := ComputeWorkflowReceiptMerkleRoot(hashes)
	if err != nil {
		return err
	}
	for i := range receipt.StepReceipts {
		receipt.StepReceipts[i].ReceiptHash = cloneWorkflowReceiptBytes(hashes[i])
	}
	receipt.StepReceiptHashes = cloneWorkflowReceiptMatrix(hashes)
	receipt.MerkleRoot = cloneWorkflowReceiptBytes(root)
	receipt.CanonicalStepOrder = append([]string(nil), order...)
	if opts.NonDeterministicInputs != nil {
		receipt.NonDeterministicInputs = cloneWorkflowNonDeterministicInputs(opts.NonDeterministicInputs)
	}
	if opts.Anchors != nil {
		receipt.Anchors = cloneWorkflowReceiptAnchors(opts.Anchors)
	}
	if strings.TrimSpace(opts.ExecutorPubkey) != "" {
		receipt.ExecutorPubkey = strings.TrimSpace(opts.ExecutorPubkey)
	}
	if len(opts.ExecutorPrivateKey) > 0 {
		return SignWorkflowReceipt(receipt, opts.ExecutorPrivateKey)
	}
	return nil
}

// ComputeWorkflowReceiptStepHashes returns SHA-256 hashes over JCS-canonical
// per-step receipt payloads in the order supplied by the invoke engine.
func ComputeWorkflowReceiptStepHashes(steps []WorkflowStepInvocation) ([][]byte, []string, error) {
	if len(steps) == 0 {
		return nil, nil, ErrInvalidWorkflow.Wrap("workflow receipt requires at least one step receipt")
	}
	hashes := make([][]byte, 0, len(steps))
	order := make([]string, 0, len(steps))
	seen := make(map[string]struct{}, len(steps))
	for i, step := range steps {
		stepID := strings.TrimSpace(step.StepID)
		if stepID == "" {
			return nil, nil, ErrInvalidWorkflow.Wrapf("workflow receipt step %d missing step_id", i)
		}
		if _, ok := seen[stepID]; ok {
			return nil, nil, ErrInvalidWorkflow.Wrapf("duplicate workflow receipt step_id: %s", stepID)
		}
		seen[stepID] = struct{}{}
		hash, err := ComputeWorkflowStepReceiptHash(step)
		if err != nil {
			return nil, nil, err
		}
		hashes = append(hashes, hash)
		order = append(order, stepID)
	}
	return hashes, order, nil
}

func canonicalWorkflowStepReceiptPayload(step WorkflowStepInvocation) (workflowStepReceiptPayload, error) {
	stepID := strings.TrimSpace(step.StepID)
	if stepID == "" {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrap("workflow step receipt missing required field")
	}
	if stepID != step.StepID {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrapf("workflow invocation step_id must be canonical: %q", step.StepID)
	}
	toolID := strings.TrimSpace(step.ToolID)
	if toolID == "" {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrap("workflow step receipt missing required field")
	}
	if toolID != step.ToolID {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrapf("workflow invocation step %s tool_id must be canonical: %q", stepID, step.ToolID)
	}
	toolVersion := strings.TrimSpace(step.ToolVersion)
	if toolVersion == "" {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrap("workflow step receipt missing required field")
	}
	if toolVersion != step.ToolVersion {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrapf("workflow invocation step %s tool_version must be canonical: %q", stepID, step.ToolVersion)
	}
	outcome := strings.TrimSpace(step.Outcome)
	if outcome == "" {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrap("workflow step receipt missing required field")
	}
	if outcome != step.Outcome {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrapf("workflow invocation step %s outcome must be canonical: %q", stepID, step.Outcome)
	}
	if step.ErrorCode != "" && strings.TrimSpace(step.ErrorCode) != step.ErrorCode {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrapf("workflow invocation step %s error_code must be canonical: %q", stepID, step.ErrorCode)
	}
	if step.ErrorMessage != "" && strings.TrimSpace(step.ErrorMessage) != step.ErrorMessage {
		return workflowStepReceiptPayload{}, ErrInvalidWorkflow.Wrapf("workflow invocation step %s error_message must be canonical: %q", stepID, step.ErrorMessage)
	}
	outputClaims, err := NormalizeWorkflowOutputClaimsForStep(stepID, step.OutputClaims)
	if err != nil {
		return workflowStepReceiptPayload{}, err
	}
	return workflowStepReceiptPayload{
		StepID:        stepID,
		ToolID:        toolID,
		ToolVersion:   toolVersion,
		Outcome:       outcome,
		Cost:          step.Cost,
		DurationMS:    step.DurationMS,
		AttemptCount:  step.AttemptCount,
		ErrorCode:     step.ErrorCode,
		ErrorMessage:  step.ErrorMessage,
		FailureAction: step.FailureAction,
		OutputClaims:  outputClaims,
	}, nil
}

// ComputeWorkflowStepReceiptHash returns the SHA-256 hash of one canonical step receipt.
func ComputeWorkflowStepReceiptHash(step WorkflowStepInvocation) ([]byte, error) {
	payload, err := canonicalWorkflowStepReceiptPayload(step)
	if err != nil {
		return nil, err
	}
	if err := validateWorkflowStepReceiptOutcome(payload.StepID, payload.Outcome); err != nil {
		return nil, err
	}
	if _, err := NewQuoteCoin(payload.Cost.Denom, payload.Cost.Amount); err != nil {
		return nil, err
	}
	canonical, err := canonicalJSON(payload)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	return append([]byte(nil), sum[:]...), nil
}

// ComputeWorkflowReceiptMerkleRoot computes a SHA-256 Merkle root over leaves
// in canonical workflow DAG order. Leaves are not sorted; odd leaves are
// duplicated at each level.
func ComputeWorkflowReceiptMerkleRoot(hashes [][]byte) ([]byte, error) {
	level, err := normalizeWorkflowMerkleLeaves(hashes)
	if err != nil {
		return nil, err
	}
	for len(level) > 1 {
		level = workflowMerkleNextLevel(level)
	}
	return cloneWorkflowReceiptBytes(level[0]), nil
}

// BuildWorkflowReceiptProof returns the ordered Merkle inclusion proof for stepID.
func BuildWorkflowReceiptProof(receipt *WorkflowInvocationReceipt, stepID string) (*WorkflowMerkleProof, error) {
	if receipt == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	rawStepID := stepID
	stepID = strings.TrimSpace(rawStepID)
	if stepID == "" {
		return nil, ErrInvalidWorkflow.Wrap("workflow receipt proof step_id is required")
	}
	if stepID != rawStepID {
		return nil, ErrInvalidWorkflow.Wrapf("workflow receipt proof step_id must be canonical: %q", rawStepID)
	}
	if len(receipt.StepReceiptHashes) != len(receipt.CanonicalStepOrder) {
		return nil, ErrInvalidWorkflow.Wrap("workflow receipt proof hash/order count mismatch")
	}
	index := -1
	for i, got := range receipt.CanonicalStepOrder {
		orderStepID := strings.TrimSpace(got)
		if orderStepID == "" {
			return nil, ErrInvalidWorkflow.Wrapf("workflow receipt proof order step_id missing at %d", i)
		}
		if orderStepID != got {
			return nil, ErrInvalidWorkflow.Wrapf("workflow receipt proof order step_id must be canonical: %q", got)
		}
		if got == stepID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, ErrInvalidWorkflow.Wrapf("workflow receipt proof step not found: %s", stepID)
	}
	return buildWorkflowMerkleProof(receipt.StepReceiptHashes, stepID, index)
}

// VerifyWorkflowReceiptProof verifies an ordered Merkle proof against root.
func VerifyWorkflowReceiptProof(root []byte, proof *WorkflowMerkleProof) bool {
	if len(root) != workflowReceiptHashSize || proof == nil {
		return false
	}
	if len(proof.GetLeafHash()) != workflowReceiptHashSize {
		return false
	}
	if len(proof.GetSiblings()) != len(proof.GetSiblingOnRight()) {
		return false
	}
	hash := cloneWorkflowReceiptBytes(proof.GetLeafHash())
	for i, sibling := range proof.GetSiblings() {
		if len(sibling) != workflowReceiptHashSize {
			return false
		}
		if proof.GetSiblingOnRight()[i] {
			hash = workflowMerkleParent(hash, sibling)
		} else {
			hash = workflowMerkleParent(sibling, hash)
		}
	}
	return bytes.Equal(hash, root)
}

// VerifyWorkflowStepReveal verifies a partial step receipt reveal without
// requiring the rest of the workflow receipt bundle.
func VerifyWorkflowStepReveal(root []byte, step WorkflowStepInvocation, proof *WorkflowMerkleProof) error {
	if proof == nil {
		return ErrInvalidWorkflow.Wrap("workflow receipt proof is required")
	}
	hash, err := ComputeWorkflowStepReceiptHash(step)
	if err != nil {
		return err
	}
	proofStepID := proof.GetStepId()
	if strings.TrimSpace(proofStepID) == "" {
		return ErrInvalidWorkflow.Wrap("workflow receipt proof step_id is required")
	}
	if strings.TrimSpace(proofStepID) != proofStepID {
		return ErrInvalidWorkflow.Wrapf("workflow receipt proof step_id must be canonical: %q", proofStepID)
	}
	if proofStepID != step.StepID {
		return ErrInvalidWorkflow.Wrap("workflow receipt proof step_id mismatch")
	}
	if !bytes.Equal(hash, proof.GetLeafHash()) {
		return ErrInvalidWorkflow.Wrap("workflow receipt proof leaf hash mismatch")
	}
	if !VerifyWorkflowReceiptProof(root, proof) {
		return ErrInvalidWorkflow.Wrap("workflow receipt proof does not match merkle_root")
	}
	return nil
}

// VerifyWorkflowReceiptMerkleRoot recomputes all step hashes and the ordered root.
func VerifyWorkflowReceiptMerkleRoot(receipt *WorkflowInvocationReceipt) error {
	if receipt == nil {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	hashes, order, err := ComputeWorkflowReceiptStepHashes(receipt.StepReceipts)
	if err != nil {
		return err
	}
	if len(hashes) != len(receipt.StepReceiptHashes) {
		return ErrInvalidWorkflow.Wrap("workflow receipt step hash count mismatch")
	}
	if len(order) != len(receipt.CanonicalStepOrder) {
		return ErrInvalidWorkflow.Wrap("workflow receipt canonical order count mismatch")
	}
	for i := range hashes {
		if !bytes.Equal(hashes[i], receipt.StepReceiptHashes[i]) {
			return ErrInvalidWorkflow.Wrapf("workflow receipt step hash mismatch at %s", order[i])
		}
		if !bytes.Equal(hashes[i], receipt.StepReceipts[i].ReceiptHash) {
			return ErrInvalidWorkflow.Wrapf("workflow receipt embedded step hash mismatch at %s", order[i])
		}
		if strings.TrimSpace(receipt.CanonicalStepOrder[i]) != order[i] {
			return ErrInvalidWorkflow.Wrapf("workflow receipt canonical order mismatch at %d", i)
		}
	}
	root, err := ComputeWorkflowReceiptMerkleRoot(hashes)
	if err != nil {
		return err
	}
	if !bytes.Equal(root, receipt.MerkleRoot) {
		return ErrInvalidWorkflow.Wrap("workflow receipt merkle_root mismatch")
	}
	return nil
}

// CanonicalWorkflowReceiptBytes returns the JCS-canonical bytes signed by the executor.
func CanonicalWorkflowReceiptBytes(receipt *WorkflowInvocationReceipt) ([]byte, error) {
	if receipt == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	payload := workflowReceiptSignaturePayload{
		BundleID:               strings.TrimSpace(receipt.BundleID),
		WorkflowID:             strings.TrimSpace(receipt.WorkflowID),
		Version:                strings.TrimSpace(receipt.Version),
		Outcome:                strings.TrimSpace(receipt.Outcome),
		StepReceipts:           cloneWorkflowStepInvocations(receipt.StepReceipts),
		StepReceiptHashes:      cloneWorkflowReceiptMatrix(receipt.StepReceiptHashes),
		MerkleRoot:             cloneWorkflowReceiptBytes(receipt.MerkleRoot),
		CanonicalStepOrder:     append([]string(nil), receipt.CanonicalStepOrder...),
		TotalCost:              receipt.TotalCost,
		LockID:                 strings.TrimSpace(receipt.LockID),
		FailureCode:            strings.TrimSpace(receipt.FailureCode),
		FailureReason:          strings.TrimSpace(receipt.FailureReason),
		TraceID:                strings.TrimSpace(receipt.TraceID),
		CompletedAt:            strings.TrimSpace(receipt.CompletedAt),
		NonDeterministicInputs: cloneWorkflowNonDeterministicInputs(receipt.NonDeterministicInputs),
		FailureAttributions:    cloneWorkflowFailureAttributions(receipt.FailureAttributions),
		InvariantLogs:          append([]InvariantEvaluationLog(nil), receipt.InvariantLogs...),
		ExecutorPubkey:         strings.TrimSpace(receipt.ExecutorPubkey),
	}
	return canonicalJSON(payload)
}

// SignWorkflowReceipt signs the canonical workflow receipt bytes with an Ed448 executor key.
func SignWorkflowReceipt(receipt *WorkflowInvocationReceipt, priv ed448.PrivateKey) error {
	if receipt == nil {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	pubkey, err := RouterPubkeyFromPrivateKey(priv)
	if err != nil {
		return err
	}
	receipt.ExecutorPubkey = pubkey
	receipt.ExecutorSig = nil
	if err := receipt.ValidateBasic(); err != nil {
		return err
	}
	canonical, err := CanonicalWorkflowReceiptBytes(receipt)
	if err != nil {
		return err
	}
	receipt.ExecutorSig = ed448.Sign(priv, canonical, "")
	return nil
}

// VerifyWorkflowReceiptSignature verifies the executor Ed448 signature.
func VerifyWorkflowReceiptSignature(receipt *WorkflowInvocationReceipt, expectedPubkey string) error {
	if receipt == nil {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	if err := receipt.ValidateBasic(); err != nil {
		return err
	}
	if err := ensureEd448PublicKeyFieldMatches("workflow receipt executor pubkey", expectedPubkey, receipt.ExecutorPubkey); err != nil {
		return err
	}
	pubkeyText := strings.TrimSpace(expectedPubkey)
	if pubkeyText == "" {
		pubkeyText = strings.TrimSpace(receipt.ExecutorPubkey)
	}
	pubkey, err := decodeEd448PublicKey(pubkeyText)
	if err != nil {
		return ErrInvalidWorkflow.Wrapf("invalid workflow receipt executor pubkey: %v", err)
	}
	if len(receipt.ExecutorSig) != ed448.SignatureSize {
		return ErrInvalidWorkflow.Wrapf("workflow receipt executor signature must be %d bytes", ed448.SignatureSize)
	}
	canonical, err := CanonicalWorkflowReceiptBytes(receipt)
	if err != nil {
		return err
	}
	if !ed448.Verify(ed448.PublicKey(pubkey), canonical, receipt.ExecutorSig, "") {
		return ErrInvalidWorkflow.Wrap("workflow receipt executor signature does not match")
	}
	return nil
}

// VerifyWorkflowReceipt checks shape, ordered Merkle root, and optional signature.
func VerifyWorkflowReceipt(receipt *WorkflowInvocationReceipt, expectedPubkey string) error {
	if err := receipt.ValidateBasic(); err != nil {
		return err
	}
	if err := VerifyWorkflowReceiptMerkleRoot(receipt); err != nil {
		return err
	}
	if len(receipt.ExecutorSig) > 0 || strings.TrimSpace(expectedPubkey) != "" {
		if err := VerifyWorkflowReceiptSignature(receipt, expectedPubkey); err != nil {
			return err
		}
	}
	return nil
}

// VerifyWorkflowReceiptCondition verifies receipt integrity and evaluates a V1
// workflow condition from receipt-bound step evidence.
func VerifyWorkflowReceiptCondition(receipt *WorkflowInvocationReceipt, expectedPubkey, rawCondition string) (bool, error) {
	if err := VerifyWorkflowReceipt(receipt, expectedPubkey); err != nil {
		return false, err
	}
	return evaluateWorkflowReceiptCondition(receipt, rawCondition)
}

func evaluateWorkflowReceiptCondition(receipt *WorkflowInvocationReceipt, rawCondition string) (bool, error) {
	condition, err := ParseWorkflowCondition(rawCondition)
	if err != nil {
		return false, err
	}
	switch condition.Kind {
	case WorkflowConditionKindEmpty, WorkflowConditionKindAlways:
		return true, nil
	case WorkflowConditionKindNever:
		return false, nil
	}

	step, ok := workflowReceiptStepInvocationByID(receipt, condition.StepID)
	if !ok {
		return false, ErrInvalidWorkflow.Wrapf("workflow condition references missing receipt step: %s", condition.StepID)
	}
	switch condition.Kind {
	case WorkflowConditionKindOutputClaimComparison:
		return EvaluateWorkflowOutputClaimCondition(step, condition)
	case WorkflowConditionKindOutcomeComparison:
		want := workflowReceiptConditionOutcome(condition.Literal.String)
		if want == "" {
			return false, ErrInvalidWorkflow.Wrap("workflow condition outcome literal is unsupported")
		}
		matches := step.Outcome == want
		if condition.Operator == WorkflowConditionOperatorNotEqual {
			return !matches, nil
		}
		return matches, nil
	default:
		return false, ErrInvalidWorkflow.Wrap("workflow condition kind is unsupported")
	}
}

func workflowReceiptStepInvocationByID(receipt *WorkflowInvocationReceipt, stepID string) (WorkflowStepInvocation, bool) {
	if receipt == nil {
		return WorkflowStepInvocation{}, false
	}
	for _, step := range receipt.StepReceipts {
		if step.StepID == stepID {
			return step, true
		}
	}
	return WorkflowStepInvocation{}, false
}

func workflowReceiptConditionOutcome(raw string) string {
	switch strings.TrimSpace(raw) {
	case "success":
		return WorkflowStepOutcomeSuccess
	case "failed":
		return WorkflowStepOutcomeFailed
	case "skipped":
		return WorkflowStepOutcomeSkipped
	default:
		return ""
	}
}

// PopulateWorkflowFailureAttributions rebuilds deterministic attribution
// snapshots for reverted and partial-skip workflow receipts.
func PopulateWorkflowFailureAttributions(receipt *WorkflowInvocationReceipt) error {
	if receipt == nil {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	attributions, err := BuildWorkflowFailureAttributions(receipt)
	if err != nil {
		return err
	}
	receipt.FailureAttributions = attributions
	return nil
}

// BuildWorkflowFailureAttributions returns structured failure attribution for
// non-finalized receipts. Finalized receipts intentionally return no entries.
func BuildWorkflowFailureAttributions(receipt *WorkflowInvocationReceipt) ([]*WorkflowFailureAttribution, error) {
	if receipt == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	switch strings.TrimSpace(receipt.Outcome) {
	case WorkflowOutcomeReverted, WorkflowOutcomePartialSkip:
	default:
		return nil, nil
	}
	attributions := make([]*WorkflowFailureAttribution, 0, len(receipt.StepReceipts))
	for _, step := range receipt.StepReceipts {
		if strings.TrimSpace(step.Outcome) == WorkflowStepOutcomeSuccess {
			continue
		}
		attr, err := workflowFailureAttributionForStep(receipt, &step)
		if err != nil {
			return nil, err
		}
		attributions = append(attributions, attr)
	}
	if len(attributions) > 0 {
		return attributions, nil
	}
	attr, err := workflowFailureAttributionForStep(receipt, nil)
	if err != nil {
		return nil, err
	}
	return []*WorkflowFailureAttribution{attr}, nil
}

func workflowFailureAttributionForStep(receipt *WorkflowInvocationReceipt, step *WorkflowStepInvocation) (*WorkflowFailureAttribution, error) {
	stepID := "workflow"
	reasonCode := strings.TrimSpace(receipt.FailureCode)
	snapshot := workflowFailureStateSnapshot{
		BundleID:      strings.TrimSpace(receipt.BundleID),
		WorkflowID:    strings.TrimSpace(receipt.WorkflowID),
		Version:       strings.TrimSpace(receipt.Version),
		Outcome:       strings.TrimSpace(receipt.Outcome),
		FailureCode:   strings.TrimSpace(receipt.FailureCode),
		FailureReason: strings.TrimSpace(receipt.FailureReason),
		TraceID:       strings.TrimSpace(receipt.TraceID),
		TotalCost:     receipt.TotalCost,
	}
	if step != nil {
		stepID = strings.TrimSpace(step.StepID)
		if code := strings.TrimSpace(step.ErrorCode); code != "" {
			reasonCode = code
		}
		snapshot.Step = &workflowFailureStepSnapshot{
			StepID:        strings.TrimSpace(step.StepID),
			ToolID:        strings.TrimSpace(step.ToolID),
			ToolVersion:   strings.TrimSpace(step.ToolVersion),
			Outcome:       strings.TrimSpace(step.Outcome),
			Cost:          step.Cost,
			DurationMS:    step.DurationMS,
			AttemptCount:  step.AttemptCount,
			ErrorCode:     strings.TrimSpace(step.ErrorCode),
			ErrorMessage:  strings.TrimSpace(step.ErrorMessage),
			FailureAction: step.FailureAction,
		}
	}
	if stepID == "" {
		stepID = "workflow"
	}
	if reasonCode == "" {
		reasonCode = strings.TrimSpace(receipt.Outcome)
	}
	stateSnapshot, err := canonicalJSON(snapshot)
	if err != nil {
		return nil, err
	}
	return &WorkflowFailureAttribution{
		StepId:        stepID,
		ReasonCode:    reasonCode,
		StateSnapshot: stateSnapshot,
	}, nil
}

// ToProto converts the JSON receipt form to the WorkflowReceipt protobuf form.
func (r *WorkflowInvocationReceipt) ToProto() (*WorkflowReceipt, error) {
	if r == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	if err := r.ValidateBasic(); err != nil {
		return nil, err
	}
	completed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(r.CompletedAt))
	if err != nil {
		return nil, ErrInvalidWorkflow.Wrapf("invalid workflow receipt completed_at: %v", err)
	}
	out := &WorkflowReceipt{
		BundleId:               r.BundleID,
		WorkflowId:             r.WorkflowID,
		Version:                r.Version,
		Outcome:                r.Outcome,
		StepReceipts:           make([]*WorkflowStepReceipt, 0, len(r.StepReceipts)),
		StepReceiptHashes:      cloneWorkflowReceiptMatrix(r.StepReceiptHashes),
		MerkleRoot:             cloneWorkflowReceiptBytes(r.MerkleRoot),
		TotalCost:              quoteCoinToProto(r.TotalCost),
		LockId:                 r.LockID,
		FailureCode:            r.FailureCode,
		FailureReason:          r.FailureReason,
		TraceId:                r.TraceID,
		CompletedAt:            completed.UTC(),
		CanonicalStepOrder:     append([]string(nil), r.CanonicalStepOrder...),
		NonDeterministicInputs: cloneWorkflowNonDeterministicInputs(r.NonDeterministicInputs),
		Anchors:                cloneWorkflowReceiptAnchors(r.Anchors),
		ExecutorPubkey:         r.ExecutorPubkey,
		ExecutorSig:            cloneWorkflowReceiptBytes(r.ExecutorSig),
		FailureAttributions:    cloneWorkflowFailureAttributions(r.FailureAttributions),
	}
	for _, step := range r.StepReceipts {
		out.StepReceipts = append(out.StepReceipts, workflowStepInvocationToProto(step))
	}
	return out, nil
}

// WorkflowInvocationReceiptFromProto converts a WorkflowReceipt proto into the JSON receipt form.
func WorkflowInvocationReceiptFromProto(in *WorkflowReceipt) (*WorkflowInvocationReceipt, error) {
	if in == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow receipt proto cannot be nil")
	}
	totalCost, err := quoteCoinFromProto(in.GetTotalCost())
	if err != nil {
		return nil, err
	}
	completed := ""
	if !in.GetCompletedAt().IsZero() {
		completed = in.GetCompletedAt().UTC().Format(time.RFC3339Nano)
	}
	out := &WorkflowInvocationReceipt{
		BundleID:               in.GetBundleId(),
		WorkflowID:             in.GetWorkflowId(),
		Version:                in.GetVersion(),
		Outcome:                in.GetOutcome(),
		StepReceipts:           make([]WorkflowStepInvocation, 0, len(in.GetStepReceipts())),
		StepReceiptHashes:      cloneWorkflowReceiptMatrix(in.GetStepReceiptHashes()),
		MerkleRoot:             cloneWorkflowReceiptBytes(in.GetMerkleRoot()),
		CanonicalStepOrder:     append([]string(nil), in.GetCanonicalStepOrder()...),
		TotalCost:              totalCost,
		LockID:                 in.GetLockId(),
		FailureCode:            in.GetFailureCode(),
		FailureReason:          in.GetFailureReason(),
		TraceID:                in.GetTraceId(),
		CompletedAt:            completed,
		NonDeterministicInputs: cloneWorkflowNonDeterministicInputs(in.GetNonDeterministicInputs()),
		Anchors:                cloneWorkflowReceiptAnchors(in.GetAnchors()),
		ExecutorPubkey:         in.GetExecutorPubkey(),
		ExecutorSig:            cloneWorkflowReceiptBytes(in.GetExecutorSig()),
		FailureAttributions:    cloneWorkflowFailureAttributions(in.GetFailureAttributions()),
	}
	for _, step := range in.GetStepReceipts() {
		converted, err := workflowStepInvocationFromProto(step)
		if err != nil {
			return nil, err
		}
		out.StepReceipts = append(out.StepReceipts, converted)
	}
	if err := out.ValidateBasic(); err != nil {
		return nil, err
	}
	return out, nil
}

// NewWorkflowNonDeterministicInput hashes a captured external input value.
func NewWorkflowNonDeterministicInput(inputID string, source string, anchoredHeight int64, value string) *WorkflowNonDeterministicInput {
	sum := sha256.Sum256([]byte(value))
	return &WorkflowNonDeterministicInput{
		InputId:        strings.TrimSpace(inputID),
		Source:         strings.TrimSpace(source),
		AnchoredHeight: anchoredHeight,
		InputHash:      append([]byte(nil), sum[:]...),
	}
}

func buildWorkflowMerkleProof(hashes [][]byte, stepID string, index int) (*WorkflowMerkleProof, error) {
	level, err := normalizeWorkflowMerkleLeaves(hashes)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(level) {
		return nil, ErrInvalidWorkflow.Wrap("workflow receipt proof index out of range")
	}
	proof := &WorkflowMerkleProof{
		StepId:   stepID,
		LeafHash: cloneWorkflowReceiptBytes(level[index]),
	}
	for len(level) > 1 {
		if index%2 == 0 {
			siblingIndex := index + 1
			if siblingIndex >= len(level) {
				siblingIndex = index
			}
			proof.Siblings = append(proof.Siblings, cloneWorkflowReceiptBytes(level[siblingIndex]))
			proof.SiblingOnRight = append(proof.SiblingOnRight, true)
		} else {
			proof.Siblings = append(proof.Siblings, cloneWorkflowReceiptBytes(level[index-1]))
			proof.SiblingOnRight = append(proof.SiblingOnRight, false)
		}
		index /= 2
		level = workflowMerkleNextLevel(level)
	}
	return proof, nil
}

func normalizeWorkflowMerkleLeaves(hashes [][]byte) ([][]byte, error) {
	if len(hashes) == 0 {
		return nil, ErrInvalidWorkflow.Wrap("workflow receipt hashes missing")
	}
	out := make([][]byte, len(hashes))
	for i, h := range hashes {
		if len(h) != workflowReceiptHashSize {
			return nil, ErrInvalidWorkflow.Wrapf("workflow receipt hash %d must be %d bytes", i, workflowReceiptHashSize)
		}
		out[i] = cloneWorkflowReceiptBytes(h)
	}
	return out, nil
}

func workflowMerkleNextLevel(level [][]byte) [][]byte {
	next := make([][]byte, 0, (len(level)+1)/2)
	for i := 0; i < len(level); i += 2 {
		left := level[i]
		right := left
		if i+1 < len(level) {
			right = level[i+1]
		}
		next = append(next, workflowMerkleParent(left, right))
	}
	return next
}

func workflowMerkleParent(left []byte, right []byte) []byte {
	var buf [workflowReceiptHashSize * 2]byte
	copy(buf[:workflowReceiptHashSize], left)
	copy(buf[workflowReceiptHashSize:], right)
	sum := sha256.Sum256(buf[:])
	return append([]byte(nil), sum[:]...)
}

func workflowStepInvocationToProto(step WorkflowStepInvocation) *WorkflowStepReceipt {
	return &WorkflowStepReceipt{
		StepId:        step.StepID,
		ToolId:        step.ToolID,
		ToolVersion:   step.ToolVersion,
		Outcome:       step.Outcome,
		Cost:          quoteCoinToProto(step.Cost),
		DurationMs:    step.DurationMS,
		AttemptCount:  step.AttemptCount,
		ErrorCode:     step.ErrorCode,
		ErrorMessage:  step.ErrorMessage,
		FailureAction: step.FailureAction,
		ReceiptHash:   cloneWorkflowReceiptBytes(step.ReceiptHash),
		OutputClaims:  workflowOutputClaimsToProto(step.OutputClaims),
	}
}

func workflowStepInvocationFromProto(step *WorkflowStepReceipt) (WorkflowStepInvocation, error) {
	if step == nil {
		return WorkflowStepInvocation{}, ErrInvalidWorkflow.Wrap("workflow step receipt proto cannot be nil")
	}
	cost, err := quoteCoinFromProto(step.GetCost())
	if err != nil {
		return WorkflowStepInvocation{}, err
	}
	return WorkflowStepInvocation{
		StepID:        step.GetStepId(),
		ToolID:        step.GetToolId(),
		ToolVersion:   step.GetToolVersion(),
		Outcome:       step.GetOutcome(),
		Cost:          cost,
		DurationMS:    step.GetDurationMs(),
		AttemptCount:  step.GetAttemptCount(),
		ErrorCode:     step.GetErrorCode(),
		ErrorMessage:  step.GetErrorMessage(),
		FailureAction: step.GetFailureAction(),
		OutputClaims:  workflowOutputClaimsFromProto(step.GetOutputClaims()),
		ReceiptHash:   cloneWorkflowReceiptBytes(step.GetReceiptHash()),
	}, nil
}

func quoteCoinToProto(coin QuoteCoin) sdk.Coin {
	denom := strings.TrimSpace(coin.Denom)
	amount, ok := sdkmath.NewIntFromString(strings.TrimSpace(coin.Amount))
	if !ok {
		amount = sdkmath.ZeroInt()
	}
	return sdk.Coin{Denom: denom, Amount: amount}
}

func quoteCoinFromProto(coin sdk.Coin) (QuoteCoin, error) {
	if coin.Denom == "" && coin.Amount.IsNil() {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrap("workflow receipt coin is required")
	}
	amount := ""
	if !coin.Amount.IsNil() {
		amount = coin.Amount.String()
	}
	return NewQuoteCoin(coin.Denom, amount)
}

func cloneWorkflowStepInvocations(in []WorkflowStepInvocation) []WorkflowStepInvocation {
	out := append([]WorkflowStepInvocation(nil), in...)
	for i := range out {
		out[i].OutputClaims = cloneWorkflowOutputClaims(in[i].OutputClaims)
		out[i].ReceiptHash = cloneWorkflowReceiptBytes(in[i].ReceiptHash)
	}
	return out
}

func cloneWorkflowReceiptBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneWorkflowReceiptMatrix(in [][]byte) [][]byte {
	if in == nil {
		return nil
	}
	out := make([][]byte, len(in))
	for i := range in {
		out[i] = cloneWorkflowReceiptBytes(in[i])
	}
	return out
}

func cloneWorkflowNonDeterministicInputs(in []*WorkflowNonDeterministicInput) []*WorkflowNonDeterministicInput {
	if in == nil {
		return nil
	}
	out := make([]*WorkflowNonDeterministicInput, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, proto.Clone(item).(*WorkflowNonDeterministicInput))
	}
	return out
}

func cloneWorkflowReceiptAnchors(in []*WorkflowReceiptAnchor) []*WorkflowReceiptAnchor {
	if in == nil {
		return nil
	}
	out := make([]*WorkflowReceiptAnchor, 0, len(in))
	for _, anchor := range in {
		if anchor == nil {
			continue
		}
		out = append(out, proto.Clone(anchor).(*WorkflowReceiptAnchor))
	}
	return out
}

func cloneWorkflowFailureAttributions(in []*WorkflowFailureAttribution) []*WorkflowFailureAttribution {
	if in == nil {
		return nil
	}
	out := make([]*WorkflowFailureAttribution, 0, len(in))
	for _, attr := range in {
		if attr == nil {
			continue
		}
		out = append(out, proto.Clone(attr).(*WorkflowFailureAttribution))
	}
	return out
}

func workflowReceiptHashHex(hash []byte) string {
	if len(hash) == 0 {
		return ""
	}
	return hex.EncodeToString(hash)
}

func (r *WorkflowInvocationReceipt) MerkleRootHex() string {
	if r == nil {
		return ""
	}
	return workflowReceiptHashHex(r.MerkleRoot)
}

func (r *WorkflowInvocationReceipt) AnchorLogFields(anchor *WorkflowReceiptAnchor) map[string]any {
	fields := map[string]any{
		"bundle_id":   "",
		"merkle_root": "",
	}
	if r != nil {
		fields["bundle_id"] = strings.TrimSpace(r.BundleID)
		fields["merkle_root"] = r.MerkleRootHex()
	}
	if anchor != nil {
		fields["anchored_height"] = anchor.GetAnchoredHeight()
		fields["anchor_tx_hash"] = workflowReceiptHashHex(anchor.GetTxHash())
		if !anchor.GetAnchoredAt().IsZero() {
			fields["ts"] = anchor.GetAnchoredAt().UTC().Format(time.RFC3339Nano)
		}
	}
	return fields
}
