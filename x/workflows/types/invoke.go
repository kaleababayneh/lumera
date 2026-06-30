package types

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/LumeraProtocol/lumera/internal/logging"
)

const (
	// WorkflowOutcomeFinalized marks a fully successful atomic bundle.
	WorkflowOutcomeFinalized = "FINALIZED"
	// WorkflowOutcomeReverted marks a bundle whose lock was reverted without settlement.
	WorkflowOutcomeReverted = "REVERTED"
	// WorkflowOutcomePartialSkip marks a bundle settled after skip_downstream policy.
	WorkflowOutcomePartialSkip = "PARTIAL_SKIP"
)

// WorkflowStepInvocation records the deterministic result of one workflow step.
type WorkflowStepInvocation struct {
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
	ReceiptHash   []byte                `json:"receipt_hash,omitempty"`
}

// WorkflowInvocationReceipt is the persisted result returned by invoke_workflow.
type WorkflowInvocationReceipt struct {
	BundleID               string                           `json:"bundle_id"`
	WorkflowID             string                           `json:"workflow_id"`
	Version                string                           `json:"version"`
	Outcome                string                           `json:"outcome"`
	StepReceipts           []WorkflowStepInvocation         `json:"step_receipts"`
	StepReceiptHashes      [][]byte                         `json:"step_receipt_hashes,omitempty"`
	MerkleRoot             []byte                           `json:"merkle_root,omitempty"`
	CanonicalStepOrder     []string                         `json:"canonical_step_order,omitempty"`
	TotalCost              QuoteCoin                        `json:"total_cost"`
	LockID                 string                           `json:"lock_id"`
	FailureCode            string                           `json:"failure_code,omitempty"`
	FailureReason          string                           `json:"failure_reason,omitempty"`
	TraceID                string                           `json:"trace_id,omitempty"`
	CompletedAt            string                           `json:"completed_at"`
	NonDeterministicInputs []*WorkflowNonDeterministicInput `json:"non_deterministic_inputs,omitempty"`
	Anchors                []*WorkflowReceiptAnchor         `json:"anchors,omitempty"`
	ExecutorPubkey         string                           `json:"executor_pubkey,omitempty"`
	ExecutorSig            []byte                           `json:"executor_sig,omitempty"`
	FailureAttributions    []*WorkflowFailureAttribution    `json:"failure_attributions,omitempty"`
	InvariantLogs          []InvariantEvaluationLog         `json:"invariant_logs,omitempty"`
}

// WorkflowInvocationRecord stores idempotent invoke_workflow replay state.
type WorkflowInvocationRecord struct {
	BundleID      string                     `json:"bundle_id"`
	Receipt       *WorkflowInvocationReceipt `json:"receipt,omitempty"`
	UpdatedHeight int64                      `json:"updated_height"`
}

// ValidateBasic validates an invocation receipt shape.
func (r *WorkflowInvocationReceipt) ValidateBasic() error {
	if r == nil {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt cannot be nil")
	}
	for _, field := range []struct{ name, value string }{
		{"bundle_id", r.BundleID},
		{"workflow_id", r.WorkflowID},
		{"version", r.Version},
		{"outcome", r.Outcome},
		{"lock_id", r.LockID},
		{"completed_at", r.CompletedAt},
	} {
		if strings.TrimSpace(field.value) == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invocation receipt %s is required", field.name)
		}
	}
	for _, field := range []struct{ name, value string }{
		{"bundle_id", r.BundleID},
		{"workflow_id", r.WorkflowID},
		{"version", r.Version},
		{"lock_id", r.LockID},
		{"completed_at", r.CompletedAt},
	} {
		if strings.TrimSpace(field.value) != field.value {
			return ErrInvalidWorkflow.Wrapf("workflow invocation receipt %s must be canonical: %q", field.name, field.value)
		}
	}
	for _, field := range []struct{ name, value string }{
		{"failure_code", r.FailureCode},
		{"failure_reason", r.FailureReason},
		{"trace_id", r.TraceID},
		{"executor_pubkey", r.ExecutorPubkey},
	} {
		if field.value != "" && strings.TrimSpace(field.value) != field.value {
			return ErrInvalidWorkflow.Wrapf("workflow invocation receipt %s must be canonical: %q", field.name, field.value)
		}
	}
	if err := validateWorkflowReceiptOutcome(r.Outcome); err != nil {
		return err
	}
	if err := validateWorkflowReceiptCompletedAt(r.CompletedAt); err != nil {
		return err
	}
	if _, err := NewQuoteCoin(r.TotalCost.Denom, r.TotalCost.Amount); err != nil {
		return err
	}
	if len(r.StepReceipts) == 0 {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt requires step receipts")
	}
	if len(r.StepReceiptHashes) != len(r.StepReceipts) {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt step hash count mismatch")
	}
	if len(r.CanonicalStepOrder) != len(r.StepReceipts) {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt canonical step order mismatch")
	}
	if len(r.MerkleRoot) != workflowReceiptHashSize {
		return ErrInvalidWorkflow.Wrapf("workflow invocation receipt merkle_root must be %d bytes", workflowReceiptHashSize)
	}
	seenStepIDs := make(map[string]struct{}, len(r.StepReceipts))
	for i, step := range r.StepReceipts {
		stepID := strings.TrimSpace(step.StepID)
		if stepID == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %d missing step_id", i)
		}
		if stepID != step.StepID {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step_id must be canonical: %q", step.StepID)
		}
		if _, ok := seenStepIDs[stepID]; ok {
			return ErrInvalidWorkflow.Wrapf("duplicate workflow receipt step_id: %s", stepID)
		}
		seenStepIDs[stepID] = struct{}{}
		toolID := strings.TrimSpace(step.ToolID)
		if toolID == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %s missing tool_id", step.StepID)
		}
		if toolID != step.ToolID {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %s tool_id must be canonical: %q", stepID, step.ToolID)
		}
		toolVersion := strings.TrimSpace(step.ToolVersion)
		if toolVersion == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %s missing tool_version", step.StepID)
		}
		if toolVersion != step.ToolVersion {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %s tool_version must be canonical: %q", stepID, step.ToolVersion)
		}
		if err := validateWorkflowStepReceiptOutcome(stepID, step.Outcome); err != nil {
			return err
		}
		if step.ErrorCode != "" && strings.TrimSpace(step.ErrorCode) != step.ErrorCode {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %s error_code must be canonical: %q", stepID, step.ErrorCode)
		}
		if step.ErrorMessage != "" && strings.TrimSpace(step.ErrorMessage) != step.ErrorMessage {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %s error_message must be canonical: %q", stepID, step.ErrorMessage)
		}
		normalizedClaims, err := NormalizeWorkflowOutputClaimsForStep(stepID, step.OutputClaims)
		if err != nil {
			return err
		}
		if !workflowOutputClaimsEqual(normalizedClaims, step.OutputClaims) {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %s output_claims must be canonical", stepID)
		}
		if _, err := NewQuoteCoin(step.Cost.Denom, step.Cost.Amount); err != nil {
			return err
		}
		if len(step.ReceiptHash) != workflowReceiptHashSize {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step %s receipt_hash must be %d bytes", step.StepID, workflowReceiptHashSize)
		}
		if len(r.StepReceiptHashes[i]) != workflowReceiptHashSize {
			return ErrInvalidWorkflow.Wrapf("workflow invocation step_receipt_hashes[%d] must be %d bytes", i, workflowReceiptHashSize)
		}
		orderStepID := strings.TrimSpace(r.CanonicalStepOrder[i])
		if orderStepID != r.CanonicalStepOrder[i] {
			return ErrInvalidWorkflow.Wrapf("workflow invocation canonical step order entry must be canonical: %q", r.CanonicalStepOrder[i])
		}
		if orderStepID != stepID {
			return ErrInvalidWorkflow.Wrapf("workflow invocation canonical step order mismatch at %d", i)
		}
	}
	if err := validateWorkflowReceiptOutcomeConsistency(r); err != nil {
		return err
	}
	if err := validateWorkflowReceiptNonDeterministicInputs(r.NonDeterministicInputs); err != nil {
		return err
	}
	if err := validateWorkflowReceiptAnchors(r.Anchors); err != nil {
		return err
	}
	if err := validateWorkflowReceiptInvariantLogs(r.InvariantLogs); err != nil {
		return err
	}
	for i, attr := range r.FailureAttributions {
		if attr == nil {
			return ErrInvalidWorkflow.Wrapf("workflow failure attribution %d cannot be nil", i)
		}
		attrStepID := strings.TrimSpace(attr.GetStepId())
		if attrStepID == "" {
			return ErrInvalidWorkflow.Wrapf("workflow failure attribution %d missing step_id", i)
		}
		if attrStepID != attr.GetStepId() {
			return ErrInvalidWorkflow.Wrapf("workflow failure attribution step_id must be canonical: %q", attr.GetStepId())
		}
		reasonCode := strings.TrimSpace(attr.GetReasonCode())
		if reasonCode == "" {
			return ErrInvalidWorkflow.Wrapf("workflow failure attribution %s missing reason_code", attr.GetStepId())
		}
		if reasonCode != attr.GetReasonCode() {
			return ErrInvalidWorkflow.Wrapf("workflow failure attribution reason_code must be canonical: %q", attr.GetReasonCode())
		}
		if len(attr.GetStateSnapshot()) == 0 {
			return ErrInvalidWorkflow.Wrapf("workflow failure attribution %s missing state_snapshot", attr.GetStepId())
		}
	}
	return nil
}

func validateWorkflowReceiptOutcomeConsistency(r *WorkflowInvocationReceipt) error {
	switch r.Outcome {
	case WorkflowOutcomeFinalized:
		if r.FailureCode != "" {
			return ErrInvalidWorkflow.Wrap("workflow invocation finalized receipt must not include failure_code")
		}
		if r.FailureReason != "" {
			return ErrInvalidWorkflow.Wrap("workflow invocation finalized receipt must not include failure_reason")
		}
		if len(r.FailureAttributions) != 0 {
			return ErrInvalidWorkflow.Wrap("workflow invocation finalized receipt must not include failure_attributions")
		}
	case WorkflowOutcomeReverted, WorkflowOutcomePartialSkip:
		if r.FailureCode == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invocation %s receipt failure_code is required", r.Outcome)
		}
		if r.FailureReason == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invocation %s receipt failure_reason is required", r.Outcome)
		}
	}
	for _, step := range r.StepReceipts {
		stepID := workflowReceiptDiagnostic(step.StepID)
		switch step.Outcome {
		case WorkflowStepOutcomeSuccess:
			if step.ErrorCode != "" {
				return ErrInvalidWorkflow.Wrapf("workflow invocation successful step %s must not include error_code", stepID)
			}
			if step.ErrorMessage != "" {
				return ErrInvalidWorkflow.Wrapf("workflow invocation successful step %s must not include error_message", stepID)
			}
		case WorkflowStepOutcomeFailed, WorkflowStepOutcomeError, WorkflowStepOutcomeSkipped:
			if step.ErrorCode == "" {
				return ErrInvalidWorkflow.Wrapf("workflow invocation step %s error_code is required for %s outcome", stepID, step.Outcome)
			}
		}
	}
	return nil
}

func validateWorkflowReceiptInvariantLogs(logs []InvariantEvaluationLog) error {
	for i, log := range logs {
		invariant := strings.TrimSpace(log.Invariant)
		if invariant == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %d missing invariant", i)
		}
		if invariant != log.Invariant {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log invariant must be canonical: %q", log.Invariant)
		}
		phase := strings.TrimSpace(log.Phase)
		if phase == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s missing phase", invariant)
		}
		if phase != log.Phase {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s phase must be canonical: %q", invariant, log.Phase)
		}
		switch phase {
		case "static", "lock", "verify":
		default:
			return ErrInvalidWorkflow.Wrapf("workflow invariant log phase is invalid: %s", phase)
		}
		inputsDigest := strings.TrimSpace(log.InputsDigest)
		if inputsDigest == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s missing inputs_digest", invariant)
		}
		if subtle.ConstantTimeCompare([]byte(inputsDigest), []byte(log.InputsDigest)) != 1 {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s inputs_digest must be canonical: %q", invariant, log.InputsDigest)
		}
		result := strings.TrimSpace(log.Result)
		if result == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s missing result", invariant)
		}
		if result != log.Result {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s result must be canonical: %q", invariant, log.Result)
		}
		switch result {
		case InvariantResultPass, InvariantResultFail, InvariantResultSkip:
		default:
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s result is invalid: %s", invariant, result)
		}
		reasonCode := strings.TrimSpace(log.ReasonCode)
		if reasonCode != log.ReasonCode {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s reason_code must be canonical: %q", invariant, log.ReasonCode)
		}
		if result == InvariantResultPass {
			if reasonCode != "" {
				return ErrInvalidWorkflow.Wrapf("workflow invariant log %s passing result must not include reason_code", invariant)
			}
			continue
		}
		if reasonCode == "" {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s reason_code is required", invariant)
		}
		if !isWorkflowInvariantReasonCode(reasonCode) {
			return ErrInvalidWorkflow.Wrapf("workflow invariant log %s reason_code is invalid: %s", invariant, reasonCode)
		}
	}
	return nil
}

func isWorkflowInvariantReasonCode(reasonCode string) bool {
	switch reasonCode {
	case InvariantReasonSkipped,
		InvariantReasonInvalid,
		InvariantReasonUnsupported,
		InvariantReasonInputsMissing,
		InvariantReasonCostExceeded,
		InvariantReasonJurisdictionDenied,
		InvariantReasonStepErrorRevert,
		InvariantReasonStepMissing,
		InvariantReasonStepOutcomeMismatch,
		InvariantReasonNoSteps,
		InvariantReasonSuccessCountMismatch:
		return true
	default:
		return false
	}
}

func validateWorkflowReceiptAnchors(anchors []*WorkflowReceiptAnchor) error {
	for i, anchor := range anchors {
		if anchor == nil {
			return ErrInvalidWorkflow.Wrapf("workflow receipt anchor %d cannot be nil", i)
		}
		chainID := strings.TrimSpace(anchor.GetChainId())
		if chainID == "" {
			return ErrInvalidWorkflow.Wrapf("workflow receipt anchor %d missing chain_id", i)
		}
		if chainID != anchor.GetChainId() {
			return ErrInvalidWorkflow.Wrapf("workflow receipt anchor chain_id must be canonical: %q", anchor.GetChainId())
		}
		if len(anchor.GetTxHash()) == 0 {
			return ErrInvalidWorkflow.Wrapf("workflow receipt anchor %s tx_hash is required", chainID)
		}
		if anchor.GetAnchoredHeight() <= 0 {
			return ErrInvalidWorkflow.Wrapf("workflow receipt anchor %s anchored_height must be positive", chainID)
		}
		if anchor.GetAnchoredAt().IsZero() {
			return ErrInvalidWorkflow.Wrapf("workflow receipt anchor %s anchored_at is required", chainID)
		}
		status := strings.TrimSpace(anchor.GetStatus())
		if status == "" {
			return ErrInvalidWorkflow.Wrapf("workflow receipt anchor %s missing status", chainID)
		}
		if status != anchor.GetStatus() {
			return ErrInvalidWorkflow.Wrapf("workflow receipt anchor %s status must be canonical: %q", chainID, anchor.GetStatus())
		}
	}
	return nil
}

func validateWorkflowReceiptNonDeterministicInputs(inputs []*WorkflowNonDeterministicInput) error {
	seenInputIDs := make(map[string]struct{}, len(inputs))
	for i, input := range inputs {
		if input == nil {
			return ErrInvalidWorkflow.Wrapf("workflow nondeterministic input %d cannot be nil", i)
		}
		inputID := strings.TrimSpace(input.GetInputId())
		if inputID == "" {
			return ErrInvalidWorkflow.Wrapf("workflow nondeterministic input %d missing input_id", i)
		}
		if inputID != input.GetInputId() {
			return ErrInvalidWorkflow.Wrapf("workflow nondeterministic input input_id must be canonical: %q", input.GetInputId())
		}
		if _, ok := seenInputIDs[inputID]; ok {
			return ErrInvalidWorkflow.Wrapf("duplicate workflow nondeterministic input_id: %s", inputID)
		}
		seenInputIDs[inputID] = struct{}{}

		source := strings.TrimSpace(input.GetSource())
		if source == "" {
			return ErrInvalidWorkflow.Wrapf("workflow nondeterministic input %s missing source", inputID)
		}
		if source != input.GetSource() {
			return ErrInvalidWorkflow.Wrapf("workflow nondeterministic input %s source must be canonical: %q", inputID, input.GetSource())
		}
		if input.GetAnchoredHeight() <= 0 {
			return ErrInvalidWorkflow.Wrapf("workflow nondeterministic input %s anchored_height must be positive", inputID)
		}
		if len(input.GetInputHash()) != workflowReceiptHashSize {
			return ErrInvalidWorkflow.Wrapf("workflow nondeterministic input %s input_hash must be %d bytes", inputID, workflowReceiptHashSize)
		}
	}
	return nil
}

func validateWorkflowReceiptOutcome(outcome string) error {
	trimmed := strings.TrimSpace(outcome)
	if trimmed == "" {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt outcome is required")
	}
	if trimmed != outcome {
		return ErrInvalidWorkflow.Wrapf("workflow invocation outcome must be canonical: %q", workflowReceiptDiagnostic(outcome))
	}
	switch trimmed {
	case WorkflowOutcomeFinalized, WorkflowOutcomeReverted, WorkflowOutcomePartialSkip:
		return nil
	default:
		return ErrInvalidWorkflow.Wrapf("invalid workflow invocation outcome: %s", workflowReceiptDiagnostic(outcome))
	}
}

func validateWorkflowReceiptCompletedAt(completedAt string) error {
	trimmed := strings.TrimSpace(completedAt)
	if trimmed == "" {
		return ErrInvalidWorkflow.Wrap("workflow invocation receipt completed_at is required")
	}
	if trimmed != completedAt {
		return ErrInvalidWorkflow.Wrapf("workflow receipt completed_at must be canonical: %q", completedAt)
	}
	if _, err := time.Parse(time.RFC3339Nano, trimmed); err != nil {
		return ErrInvalidWorkflow.Wrapf("invalid workflow receipt completed_at: %v", err)
	}
	return nil
}

func validateWorkflowStepReceiptOutcome(stepID, outcome string) error {
	trimmed := strings.TrimSpace(outcome)
	safeStepID := workflowReceiptDiagnostic(stepID)
	if trimmed == "" {
		return ErrInvalidWorkflow.Wrapf("workflow invocation step %s missing outcome", safeStepID)
	}
	if trimmed != outcome {
		return ErrInvalidWorkflow.Wrapf("workflow invocation step %s outcome must be canonical: %q", safeStepID, workflowReceiptDiagnostic(outcome))
	}
	switch trimmed {
	case WorkflowStepOutcomeSuccess, WorkflowStepOutcomeFailed, WorkflowStepOutcomeSkipped, WorkflowStepOutcomeError:
		return nil
	default:
		return ErrInvalidWorkflow.Wrapf("invalid workflow invocation step %s outcome: %s", safeStepID, workflowReceiptDiagnostic(outcome))
	}
}

func workflowReceiptDiagnostic(value string) string {
	return logging.RedactPII(strings.TrimSpace(value))
}

// Validate validates an invocation replay record.
func (r *WorkflowInvocationRecord) Validate() error {
	if r == nil {
		return ErrInvalidWorkflow.Wrap("workflow invocation record cannot be nil")
	}
	if strings.TrimSpace(r.BundleID) == "" {
		return ErrInvalidWorkflow.Wrap("workflow invocation record missing bundle_id")
	}
	if strings.TrimSpace(r.BundleID) != r.BundleID {
		return ErrInvalidWorkflow.Wrapf("workflow invocation record bundle_id must be canonical: %q", r.BundleID)
	}
	if r.Receipt == nil {
		return ErrInvalidWorkflow.Wrap("workflow invocation record missing receipt")
	}
	if err := r.Receipt.ValidateBasic(); err != nil {
		return err
	}
	if r.BundleID != r.Receipt.BundleID {
		return fmt.Errorf("workflow invocation record id mismatch: %s != %s", r.BundleID, r.Receipt.BundleID)
	}
	return nil
}
