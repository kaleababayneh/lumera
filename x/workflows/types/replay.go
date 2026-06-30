package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cosmos/gogoproto/proto"
)

const (
	// WorkflowReplayOutcomePass means the offline replay matched the
	// anchored receipt and pinned replay inputs.
	WorkflowReplayOutcomePass = "PASS"
	// WorkflowReplayOutcomeFail means replay found at least one divergence.
	WorkflowReplayOutcomeFail = "FAIL"
)

// WorkflowReplaySnapshot is the state snapshot side of an offline replay.
type WorkflowReplaySnapshot struct {
	SnapshotID             string                           `json:"snapshot_id"`
	AnchoredHeight         int64                            `json:"anchored_height"`
	ToolVersions           map[string]string                `json:"tool_versions,omitempty"`
	StateFields            map[string]string                `json:"state_fields,omitempty"`
	NonDeterministicInputs []*WorkflowNonDeterministicInput `json:"non_deterministic_inputs,omitempty"`
}

// WorkflowReplayInputs are the deterministic execution inputs used to rebuild
// a WorkflowReceipt without contacting publishers or a live chain.
type WorkflowReplayInputs struct {
	BundleID               string                        `json:"bundle_id"`
	WorkflowID             string                        `json:"workflow_id"`
	Version                string                        `json:"version"`
	Outcome                string                        `json:"outcome"`
	TotalCost              QuoteCoin                     `json:"total_cost"`
	LockID                 string                        `json:"lock_id"`
	FailureCode            string                        `json:"failure_code,omitempty"`
	FailureReason          string                        `json:"failure_reason,omitempty"`
	TraceID                string                        `json:"trace_id,omitempty"`
	CompletedAt            string                        `json:"completed_at"`
	StepReceipts           []WorkflowStepInvocation      `json:"step_receipts"`
	ToolVersions           map[string]string             `json:"tool_versions,omitempty"`
	StateFields            map[string]string             `json:"state_fields,omitempty"`
	NonDeterministicInputs []WorkflowReplayCapturedInput `json:"non_deterministic_inputs,omitempty"`
}

// WorkflowReplayCapturedInput carries the raw deterministic value whose hash
// must match the value captured in WorkflowReceipt.non_deterministic_inputs.
type WorkflowReplayCapturedInput struct {
	InputID        string `json:"input_id"`
	Source         string `json:"source"`
	AnchoredHeight int64  `json:"anchored_height"`
	Value          string `json:"value"`
}

// WorkflowReplayOptions configures an offline receipt replay.
type WorkflowReplayOptions struct {
	ExpectedExecutorPubkey string
}

// WorkflowReplayReport is the structured JSON log emitted for every replay.
type WorkflowReplayReport struct {
	BundleID              string                `json:"bundle_id"`
	SnapshotID            string                `json:"snapshot_id,omitempty"`
	AnchoredHeight        int64                 `json:"anchored_height"`
	ReplayOutcome         string                `json:"replay_outcome"`
	ReceiptOutcome        string                `json:"receipt_outcome"`
	CanonicalReceiptMatch bool                  `json:"canonical_receipt_match"`
	ReceiptBytesMatch     bool                  `json:"receipt_bytes_match"`
	ReceiptSignatureValid bool                  `json:"receipt_signature_valid"`
	DivergenceCount       int                   `json:"divergence_count"`
	Diff                  WorkflowReplayDiffSet `json:"diff"`
}

// WorkflowReplayDiffSet groups divergences by the surface they invalidate.
type WorkflowReplayDiffSet struct {
	Receipt                []WorkflowReplayFieldDiff `json:"receipt,omitempty"`
	Steps                  []WorkflowReplayStepDiff  `json:"steps,omitempty"`
	State                  []WorkflowReplayFieldDiff `json:"state,omitempty"`
	ToolVersions           []WorkflowReplayStepDiff  `json:"tool_versions,omitempty"`
	NonDeterministicInputs []WorkflowReplayInputDiff `json:"non_deterministic_inputs,omitempty"`
}

// WorkflowReplayFieldDiff describes a scalar receipt or state divergence.
type WorkflowReplayFieldDiff struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Reason   string `json:"reason"`
}

// WorkflowReplayStepDiff describes a step-scoped divergence.
type WorkflowReplayStepDiff struct {
	StepID   string `json:"step_id"`
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Reason   string `json:"reason"`
}

// WorkflowReplayInputDiff describes a captured nondeterministic input mismatch.
type WorkflowReplayInputDiff struct {
	InputID  string `json:"input_id"`
	Source   string `json:"source,omitempty"`
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Reason   string `json:"reason"`
}

// DecodeWorkflowReplaySnapshotJSON parses a snapshot with strict JSON field
// names. It is used by the CLI and fuzzed as the replay snapshot boundary.
func DecodeWorkflowReplaySnapshotJSON(data []byte) (*WorkflowReplaySnapshot, error) {
	var snapshot WorkflowReplaySnapshot
	if err := decodeStrictWorkflowReplayJSON(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// DecodeWorkflowReplayInputsJSON parses deterministic replay inputs.
func DecodeWorkflowReplayInputsJSON(data []byte) (*WorkflowReplayInputs, error) {
	var inputs WorkflowReplayInputs
	if err := decodeStrictWorkflowReplayJSON(data, &inputs); err != nil {
		return nil, err
	}
	return &inputs, nil
}

func decodeStrictWorkflowReplayJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return ErrInvalidWorkflow.Wrap("workflow replay JSON contains trailing data")
	}
	return nil
}

// ReplayWorkflowReceipt rebuilds a WorkflowReceipt from offline replay inputs
// and compares it against an anchored receipt and state snapshot.
func ReplayWorkflowReceipt(
	receipt *WorkflowInvocationReceipt,
	snapshot *WorkflowReplaySnapshot,
	inputs *WorkflowReplayInputs,
	opts WorkflowReplayOptions,
) (*WorkflowReplayReport, error) {
	if receipt == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow replay receipt cannot be nil")
	}
	if snapshot == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow replay snapshot cannot be nil")
	}
	if inputs == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow replay inputs cannot be nil")
	}

	report := &WorkflowReplayReport{
		BundleID:       strings.TrimSpace(receipt.BundleID),
		SnapshotID:     strings.TrimSpace(snapshot.SnapshotID),
		AnchoredHeight: workflowReplayAnchoredHeight(receipt, snapshot),
		ReplayOutcome:  WorkflowReplayOutcomePass,
		ReceiptOutcome: strings.TrimSpace(receipt.Outcome),
	}

	if err := VerifyWorkflowReceipt(receipt, opts.ExpectedExecutorPubkey); err != nil {
		report.addReceiptDiff("workflow_receipt", "valid", err.Error(), "receipt_verification_failed")
	} else {
		report.ReceiptSignatureValid = len(receipt.ExecutorSig) == 0 || strings.TrimSpace(receipt.ExecutorPubkey) != ""
	}

	replayed, err := inputs.workflowInvocationReceipt()
	if err != nil {
		report.addReceiptDiff("replay_inputs", "valid", err.Error(), "replay_input_invalid")
		report.finish()
		return report, nil
	}
	if err := FinalizeWorkflowReceipt(replayed, WorkflowReceiptBuildOptions{
		NonDeterministicInputs: inputs.workflowNonDeterministicInputs(),
	}); err != nil {
		report.addReceiptDiff("replayed_receipt", "finalizable", err.Error(), "replay_finalize_failed")
		report.finish()
		return report, nil
	}
	replayed.ExecutorPubkey = strings.TrimSpace(receipt.ExecutorPubkey)

	compareWorkflowReplayReceiptScalars(report, receipt, replayed)
	compareWorkflowReplaySteps(report, receipt.StepReceipts, replayed.StepReceipts)
	compareWorkflowReplayToolPins(report, snapshot, inputs, replayed.StepReceipts)
	compareWorkflowReplayState(report, snapshot.StateFields, inputs.StateFields)
	compareWorkflowReplayInputs(report, receipt.NonDeterministicInputs, snapshot.NonDeterministicInputs, replayed.NonDeterministicInputs)
	compareWorkflowReplayBytes(report, receipt, replayed)

	report.finish()
	return report, nil
}

func (i *WorkflowReplayInputs) workflowInvocationReceipt() (*WorkflowInvocationReceipt, error) {
	if i == nil {
		return nil, ErrInvalidWorkflow.Wrap("workflow replay inputs cannot be nil")
	}
	receipt := &WorkflowInvocationReceipt{
		BundleID:               i.BundleID,
		WorkflowID:             i.WorkflowID,
		Version:                i.Version,
		Outcome:                i.Outcome,
		StepReceipts:           cloneWorkflowStepInvocations(i.StepReceipts),
		TotalCost:              i.TotalCost,
		LockID:                 i.LockID,
		FailureCode:            i.FailureCode,
		FailureReason:          i.FailureReason,
		TraceID:                i.TraceID,
		CompletedAt:            i.CompletedAt,
		NonDeterministicInputs: i.workflowNonDeterministicInputs(),
	}
	for stepIndex := range receipt.StepReceipts {
		receipt.StepReceipts[stepIndex].ReceiptHash = nil
	}
	return receipt, nil
}

func (i *WorkflowReplayInputs) workflowNonDeterministicInputs() []*WorkflowNonDeterministicInput {
	if i == nil || len(i.NonDeterministicInputs) == 0 {
		return nil
	}
	out := make([]*WorkflowNonDeterministicInput, 0, len(i.NonDeterministicInputs))
	for _, input := range i.NonDeterministicInputs {
		out = append(out, NewWorkflowNonDeterministicInput(
			input.InputID,
			input.Source,
			input.AnchoredHeight,
			input.Value,
		))
	}
	return out
}

func workflowReplayAnchoredHeight(receipt *WorkflowInvocationReceipt, snapshot *WorkflowReplaySnapshot) int64 {
	if snapshot != nil && snapshot.AnchoredHeight > 0 {
		return snapshot.AnchoredHeight
	}
	if receipt != nil {
		for _, anchor := range receipt.Anchors {
			if anchor != nil && anchor.GetAnchoredHeight() > 0 {
				return anchor.GetAnchoredHeight()
			}
		}
	}
	return 0
}

func compareWorkflowReplayReceiptScalars(report *WorkflowReplayReport, expected *WorkflowInvocationReceipt, actual *WorkflowInvocationReceipt) {
	for _, diff := range []WorkflowReplayFieldDiff{
		workflowReplayFieldDiff("bundle_id", expected.BundleID, actual.BundleID, "receipt_identity_mismatch"),
		workflowReplayFieldDiff("workflow_id", expected.WorkflowID, actual.WorkflowID, "receipt_identity_mismatch"),
		workflowReplayFieldDiff("version", expected.Version, actual.Version, "receipt_identity_mismatch"),
		workflowReplayFieldDiff("outcome", expected.Outcome, actual.Outcome, "receipt_outcome_mismatch"),
		workflowReplayFieldDiff("total_cost.denom", expected.TotalCost.Denom, actual.TotalCost.Denom, "cost_distribution_mismatch"),
		workflowReplayFieldDiff("total_cost.amount", expected.TotalCost.Amount, actual.TotalCost.Amount, "cost_distribution_mismatch"),
		workflowReplayFieldDiff("lock_id", expected.LockID, actual.LockID, "receipt_lock_mismatch"),
		workflowReplayFieldDiff("failure_code", expected.FailureCode, actual.FailureCode, "receipt_failure_mismatch"),
		workflowReplayFieldDiff("failure_reason", expected.FailureReason, actual.FailureReason, "receipt_failure_mismatch"),
		workflowReplayFieldDiff("trace_id", expected.TraceID, actual.TraceID, "receipt_trace_mismatch"),
		workflowReplayFieldDiff("completed_at", expected.CompletedAt, actual.CompletedAt, "captured_clock_mismatch"),
		workflowReplayBytesFieldDiff("merkle_root", expected.MerkleRoot, actual.MerkleRoot, "receipt_merkle_root_mismatch"),
	} {
		if diff.Expected != diff.Actual {
			report.Diff.Receipt = append(report.Diff.Receipt, diff)
		}
	}
}

func compareWorkflowReplaySteps(report *WorkflowReplayReport, expected []WorkflowStepInvocation, actual []WorkflowStepInvocation) {
	if len(expected) != len(actual) {
		report.Diff.Receipt = append(report.Diff.Receipt, WorkflowReplayFieldDiff{
			Field:    "step_receipts.length",
			Expected: fmt.Sprint(len(expected)),
			Actual:   fmt.Sprint(len(actual)),
			Reason:   "step_count_mismatch",
		})
	}
	limit := len(expected)
	if len(actual) < limit {
		limit = len(actual)
	}
	for idx := 0; idx < limit; idx++ {
		want := expected[idx]
		got := actual[idx]
		stepID := strings.TrimSpace(want.StepID)
		if stepID == "" {
			stepID = strings.TrimSpace(got.StepID)
		}
		for _, diff := range []WorkflowReplayStepDiff{
			workflowReplayStepDiff(stepID, "step_id", want.StepID, got.StepID, "step_output_mismatch"),
			workflowReplayStepDiff(stepID, "tool_id", want.ToolID, got.ToolID, "step_output_mismatch"),
			workflowReplayStepDiff(stepID, "tool_version", want.ToolVersion, got.ToolVersion, "tool_version_mismatch"),
			workflowReplayStepDiff(stepID, "outcome", want.Outcome, got.Outcome, "step_output_mismatch"),
			workflowReplayStepDiff(stepID, "cost.denom", want.Cost.Denom, got.Cost.Denom, "cost_distribution_mismatch"),
			workflowReplayStepDiff(stepID, "cost.amount", want.Cost.Amount, got.Cost.Amount, "cost_distribution_mismatch"),
			workflowReplayStepDiff(stepID, "duration_ms", fmt.Sprint(want.DurationMS), fmt.Sprint(got.DurationMS), "step_output_mismatch"),
			workflowReplayStepDiff(stepID, "attempt_count", fmt.Sprint(want.AttemptCount), fmt.Sprint(got.AttemptCount), "step_output_mismatch"),
			workflowReplayStepDiff(stepID, "error_code", want.ErrorCode, got.ErrorCode, "step_output_mismatch"),
			workflowReplayStepDiff(stepID, "error_message", want.ErrorMessage, got.ErrorMessage, "step_output_mismatch"),
			workflowReplayStepDiff(stepID, "failure_action", want.FailureAction.String(), got.FailureAction.String(), "step_output_mismatch"),
			workflowReplayStepBytesDiff(stepID, "receipt_hash", want.ReceiptHash, got.ReceiptHash, "step_receipt_hash_mismatch"),
		} {
			if diff.Expected != diff.Actual {
				report.Diff.Steps = append(report.Diff.Steps, diff)
			}
		}
	}
}

func compareWorkflowReplayToolPins(
	report *WorkflowReplayReport,
	snapshot *WorkflowReplaySnapshot,
	inputs *WorkflowReplayInputs,
	steps []WorkflowStepInvocation,
) {
	pinSources := []struct {
		name string
		pins map[string]string
	}{
		{name: "snapshot", pins: snapshot.ToolVersions},
		{name: "inputs", pins: inputs.ToolVersions},
	}
	for _, source := range pinSources {
		compareWorkflowReplayToolPinKeys(report, source.name, source.pins)
	}
	for _, step := range steps {
		for _, source := range pinSources {
			for key, pin := range workflowReplayVersionPins(source.pins, step) {
				if pin != step.ToolVersion {
					report.Diff.ToolVersions = append(report.Diff.ToolVersions, WorkflowReplayStepDiff{
						StepID:   strings.TrimSpace(step.StepID),
						Field:    source.name + ".tool_versions." + key,
						Expected: pin,
						Actual:   step.ToolVersion,
						Reason:   "tool_version_pin_mismatch",
					})
				}
			}
		}
	}
}

func compareWorkflowReplayToolPinKeys(report *WorkflowReplayReport, source string, pins map[string]string) {
	for _, key := range workflowReplaySortedMapKeys(pins) {
		canonicalKey := strings.TrimSpace(key)
		if canonicalKey == "" || canonicalKey != key {
			report.Diff.ToolVersions = append(report.Diff.ToolVersions, WorkflowReplayStepDiff{
				StepID:   canonicalKey,
				Field:    source + ".tool_versions.key",
				Expected: canonicalKey,
				Actual:   key,
				Reason:   "tool_version_pin_key_mismatch",
			})
		}
	}
}

func compareWorkflowReplayState(report *WorkflowReplayReport, expected map[string]string, actual map[string]string) {
	for _, key := range workflowReplaySortedKeys(expected, actual) {
		want := expected[key]
		got := actual[key]
		if want != got {
			report.Diff.State = append(report.Diff.State, WorkflowReplayFieldDiff{
				Field:    key,
				Expected: want,
				Actual:   got,
				Reason:   "state_field_mismatch",
			})
		}
	}
}

func compareWorkflowReplayInputs(
	report *WorkflowReplayReport,
	receiptInputs []*WorkflowNonDeterministicInput,
	snapshotInputs []*WorkflowNonDeterministicInput,
	replayedInputs []*WorkflowNonDeterministicInput,
) {
	compareWorkflowReplayInputSet(report, "replay_inputs", receiptInputs, replayedInputs)
	if len(snapshotInputs) > 0 {
		compareWorkflowReplayInputSet(report, "snapshot", receiptInputs, snapshotInputs)
	}
}

func compareWorkflowReplayInputSet(report *WorkflowReplayReport, source string, expected []*WorkflowNonDeterministicInput, actual []*WorkflowNonDeterministicInput) {
	expectedByID := workflowReplayInputsByID(expected)
	actualByID := workflowReplayInputsByID(actual)
	for _, id := range workflowReplaySortedInputIDs(expectedByID, actualByID) {
		want := expectedByID[id]
		got := actualByID[id]
		if want == nil || got == nil {
			report.Diff.NonDeterministicInputs = append(report.Diff.NonDeterministicInputs, WorkflowReplayInputDiff{
				InputID:  id,
				Field:    source + ".presence",
				Expected: workflowReplayInputPresence(want),
				Actual:   workflowReplayInputPresence(got),
				Reason:   "non_deterministic_input_presence_mismatch",
			})
			continue
		}
		for _, diff := range []WorkflowReplayInputDiff{
			workflowReplayInputDiff(id, source+".source", want.GetSource(), got.GetSource(), "non_deterministic_input_source_mismatch"),
			workflowReplayInputDiff(id, source+".anchored_height", fmt.Sprint(want.GetAnchoredHeight()), fmt.Sprint(got.GetAnchoredHeight()), "non_deterministic_input_height_mismatch"),
			workflowReplayInputDiff(id, source+".input_hash", workflowReceiptHashHex(want.GetInputHash()), workflowReceiptHashHex(got.GetInputHash()), "non_deterministic_input_hash_mismatch"),
		} {
			if diff.Expected != diff.Actual {
				report.Diff.NonDeterministicInputs = append(report.Diff.NonDeterministicInputs, diff)
			}
		}
	}
}

func compareWorkflowReplayBytes(report *WorkflowReplayReport, expected *WorkflowInvocationReceipt, actual *WorkflowInvocationReceipt) {
	expectedCanonical, expectedCanonicalErr := CanonicalWorkflowReceiptBytes(expected)
	actualCanonical, actualCanonicalErr := CanonicalWorkflowReceiptBytes(actual)
	if expectedCanonicalErr == nil && actualCanonicalErr == nil && bytes.Equal(expectedCanonical, actualCanonical) {
		report.CanonicalReceiptMatch = true
	} else {
		report.Diff.Receipt = append(report.Diff.Receipt, WorkflowReplayFieldDiff{
			Field:    "canonical_receipt_bytes_sha256",
			Expected: workflowReplaySHA256Hex(expectedCanonical, expectedCanonicalErr),
			Actual:   workflowReplaySHA256Hex(actualCanonical, actualCanonicalErr),
			Reason:   "canonical_receipt_mismatch",
		})
	}

	expectedProto, expectedProtoErr := workflowReplayProtoBytes(expected)
	actualWithSignature := workflowReplayCopyForByteCompare(actual, expected)
	actualProto, actualProtoErr := workflowReplayProtoBytes(actualWithSignature)
	if expectedProtoErr == nil && actualProtoErr == nil && bytes.Equal(expectedProto, actualProto) {
		report.ReceiptBytesMatch = true
	} else {
		report.Diff.Receipt = append(report.Diff.Receipt, WorkflowReplayFieldDiff{
			Field:    "deterministic_proto_bytes_sha256",
			Expected: workflowReplaySHA256Hex(expectedProto, expectedProtoErr),
			Actual:   workflowReplaySHA256Hex(actualProto, actualProtoErr),
			Reason:   "receipt_bytes_mismatch",
		})
	}
}

func (r *WorkflowReplayReport) addReceiptDiff(field string, expected string, actual string, reason string) {
	r.Diff.Receipt = append(r.Diff.Receipt, WorkflowReplayFieldDiff{
		Field:    field,
		Expected: expected,
		Actual:   actual,
		Reason:   reason,
	})
}

func (r *WorkflowReplayReport) finish() {
	r.DivergenceCount = len(r.Diff.Receipt) +
		len(r.Diff.Steps) +
		len(r.Diff.State) +
		len(r.Diff.ToolVersions) +
		len(r.Diff.NonDeterministicInputs)
	if r.DivergenceCount > 0 {
		r.ReplayOutcome = WorkflowReplayOutcomeFail
	} else {
		r.ReplayOutcome = WorkflowReplayOutcomePass
	}
}

func workflowReplayProtoBytes(receipt *WorkflowInvocationReceipt) ([]byte, error) {
	asProto, err := receipt.ToProto()
	if err != nil {
		return nil, err
	}
	return proto.Marshal(asProto)
}

func workflowReplayCopyForByteCompare(actual *WorkflowInvocationReceipt, expected *WorkflowInvocationReceipt) *WorkflowInvocationReceipt {
	out := *actual
	out.StepReceipts = cloneWorkflowStepInvocations(actual.StepReceipts)
	out.StepReceiptHashes = cloneWorkflowReceiptMatrix(actual.StepReceiptHashes)
	out.MerkleRoot = cloneWorkflowReceiptBytes(actual.MerkleRoot)
	out.CanonicalStepOrder = append([]string(nil), actual.CanonicalStepOrder...)
	out.NonDeterministicInputs = cloneWorkflowNonDeterministicInputs(actual.NonDeterministicInputs)
	out.Anchors = cloneWorkflowReceiptAnchors(expected.Anchors)
	out.ExecutorPubkey = strings.TrimSpace(expected.ExecutorPubkey)
	out.ExecutorSig = cloneWorkflowReceiptBytes(expected.ExecutorSig)
	return &out
}

func workflowReplayVersionPins(pins map[string]string, step WorkflowStepInvocation) map[string]string {
	out := make(map[string]string, 3)
	if len(pins) == 0 {
		return out
	}
	for _, key := range []string{
		step.StepID,
		step.ToolID,
		step.StepID + "/" + step.ToolID,
	} {
		if key == "" {
			continue
		}
		if pin, ok := pins[key]; ok {
			out[key] = pin
		}
	}
	return out
}

func workflowReplaySortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func workflowReplayInputsByID(inputs []*WorkflowNonDeterministicInput) map[string]*WorkflowNonDeterministicInput {
	out := make(map[string]*WorkflowNonDeterministicInput, len(inputs))
	for _, input := range inputs {
		if input == nil {
			continue
		}
		id := input.GetInputId()
		if id != "" {
			out[id] = input
		}
	}
	return out
}

func workflowReplaySortedKeys(left map[string]string, right map[string]string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		seen[key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func workflowReplaySortedInputIDs(left map[string]*WorkflowNonDeterministicInput, right map[string]*WorkflowNonDeterministicInput) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		seen[key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func workflowReplayFieldDiff(field string, expected string, actual string, reason string) WorkflowReplayFieldDiff {
	return WorkflowReplayFieldDiff{
		Field:    field,
		Expected: expected,
		Actual:   actual,
		Reason:   reason,
	}
}

func workflowReplayBytesFieldDiff(field string, expected []byte, actual []byte, reason string) WorkflowReplayFieldDiff {
	return WorkflowReplayFieldDiff{
		Field:    field,
		Expected: workflowReceiptHashHex(expected),
		Actual:   workflowReceiptHashHex(actual),
		Reason:   reason,
	}
}

func workflowReplayStepDiff(stepID string, field string, expected string, actual string, reason string) WorkflowReplayStepDiff {
	return WorkflowReplayStepDiff{
		StepID:   strings.TrimSpace(stepID),
		Field:    field,
		Expected: expected,
		Actual:   actual,
		Reason:   reason,
	}
}

func workflowReplayStepBytesDiff(stepID string, field string, expected []byte, actual []byte, reason string) WorkflowReplayStepDiff {
	return WorkflowReplayStepDiff{
		StepID:   strings.TrimSpace(stepID),
		Field:    field,
		Expected: workflowReceiptHashHex(expected),
		Actual:   workflowReceiptHashHex(actual),
		Reason:   reason,
	}
}

func workflowReplayInputDiff(inputID string, field string, expected string, actual string, reason string) WorkflowReplayInputDiff {
	return WorkflowReplayInputDiff{
		InputID:  inputID,
		Field:    field,
		Expected: expected,
		Actual:   actual,
		Reason:   reason,
	}
}

func workflowReplayInputPresence(input *WorkflowNonDeterministicInput) string {
	if input == nil {
		return "missing"
	}
	return "present"
}

func workflowReplaySHA256Hex(data []byte, err error) string {
	if err != nil {
		return "error:" + err.Error()
	}
	sum := sha256.Sum256(data)
	return workflowReceiptHashHex(sum[:])
}
