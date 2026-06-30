package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/ed448"
	"github.com/stretchr/testify/require"
)

func TestReplay_IdenticalOutcome(t *testing.T) {
	for i := 0; i < 10; i++ {
		receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, i)

		report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
			ExpectedExecutorPubkey: pubkey,
		})
		require.NoError(t, err)
		require.Equal(t, WorkflowReplayOutcomePass, report.ReplayOutcome)
		require.Equal(t, 0, report.DivergenceCount)
		require.True(t, report.CanonicalReceiptMatch)
		require.True(t, report.ReceiptBytesMatch)
		require.True(t, report.ReceiptSignatureValid)

		if i == 0 {
			encoded, err := json.Marshal(report)
			require.NoError(t, err)
			t.Logf("workflow_replay_report=%s", encoded)
		}
	}
}

func TestReplay_NonDeterministicInput_Detected(t *testing.T) {
	receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, 0)
	inputs.NonDeterministicInputs[1].Value = "nonce-mutated"

	report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
		ExpectedExecutorPubkey: pubkey,
	})

	require.NoError(t, err)
	require.Equal(t, WorkflowReplayOutcomeFail, report.ReplayOutcome)
	require.Positive(t, report.DivergenceCount)
	require.False(t, report.CanonicalReceiptMatch)
	require.Contains(t, workflowReplayInputDiffReasons(report), "non_deterministic_input_hash_mismatch")
}

func TestReplay_NonDeterministicInputRejectsNonCanonicalSnapshotFields(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mutate     func(*WorkflowReplaySnapshot)
		wantReason string
	}{
		{
			name: "snapshot input id padded",
			mutate: func(snapshot *WorkflowReplaySnapshot) {
				snapshot.NonDeterministicInputs[0].InputId = " " + snapshot.NonDeterministicInputs[0].InputId + " "
			},
			wantReason: "non_deterministic_input_presence_mismatch",
		},
		{
			name: "snapshot source padded",
			mutate: func(snapshot *WorkflowReplaySnapshot) {
				snapshot.NonDeterministicInputs[0].Source = " " + snapshot.NonDeterministicInputs[0].Source + " "
			},
			wantReason: "non_deterministic_input_source_mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, 0)
			tc.mutate(snapshot)

			report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
				ExpectedExecutorPubkey: pubkey,
			})

			require.NoError(t, err)
			require.Equal(t, WorkflowReplayOutcomeFail, report.ReplayOutcome)
			require.Positive(t, report.DivergenceCount)
			require.Contains(t, workflowReplayInputDiffReasons(report), tc.wantReason)
		})
	}
}

func TestReplay_ReceiptScalarsRejectNonCanonicalReplayInputs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mutate     func(*WorkflowReplayInputs)
		wantReason string
	}{
		{
			name: "workflow id padded",
			mutate: func(inputs *WorkflowReplayInputs) {
				inputs.WorkflowID = " " + inputs.WorkflowID + " "
			},
			wantReason: "receipt_identity_mismatch",
		},
		{
			name: "trace id padded",
			mutate: func(inputs *WorkflowReplayInputs) {
				inputs.TraceID = " " + inputs.TraceID + " "
			},
			wantReason: "receipt_trace_mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, 0)
			tc.mutate(inputs)

			report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
				ExpectedExecutorPubkey: pubkey,
			})

			require.NoError(t, err)
			require.Equal(t, WorkflowReplayOutcomeFail, report.ReplayOutcome)
			require.Positive(t, report.DivergenceCount)
			require.Contains(t, workflowReplayReceiptDiffReasons(report), tc.wantReason)
		})
	}
}

func TestReplay_StepDiffRejectsNonCanonicalReplayInputs(t *testing.T) {
	receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, 0)
	inputs.StepReceipts[0].ToolID = " " + inputs.StepReceipts[0].ToolID + " "

	report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
		ExpectedExecutorPubkey: pubkey,
	})

	require.NoError(t, err)
	require.Equal(t, WorkflowReplayOutcomeFail, report.ReplayOutcome)
	require.Positive(t, report.DivergenceCount)
	require.Contains(t, workflowReplayReceiptDiffReasons(report), "replay_finalize_failed")
}

func TestReplay_ToolVersionPinning(t *testing.T) {
	receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, 1)
	snapshot.ToolVersions[receipt.StepReceipts[0].ToolID] = "9.9.9"

	report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
		ExpectedExecutorPubkey: pubkey,
	})

	require.NoError(t, err)
	require.Equal(t, WorkflowReplayOutcomeFail, report.ReplayOutcome)
	require.Positive(t, report.DivergenceCount)
	require.Contains(t, workflowReplayToolDiffReasons(report), "tool_version_pin_mismatch")
}

func TestReplay_ToolVersionPinningRejectsNonCanonicalPins(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mutate     func(*WorkflowInvocationReceipt, *WorkflowReplaySnapshot, *WorkflowReplayInputs)
		wantReason string
	}{
		{
			name: "snapshot pin padded",
			mutate: func(receipt *WorkflowInvocationReceipt, snapshot *WorkflowReplaySnapshot, _ *WorkflowReplayInputs) {
				step := receipt.StepReceipts[0]
				snapshot.ToolVersions[step.ToolID] = " " + step.ToolVersion + " "
			},
			wantReason: "tool_version_pin_mismatch",
		},
		{
			name: "inputs pin padded",
			mutate: func(receipt *WorkflowInvocationReceipt, _ *WorkflowReplaySnapshot, inputs *WorkflowReplayInputs) {
				step := receipt.StepReceipts[0]
				inputs.ToolVersions[step.ToolID] = " " + step.ToolVersion + " "
			},
			wantReason: "tool_version_pin_mismatch",
		},
		{
			name: "snapshot pin key padded",
			mutate: func(receipt *WorkflowInvocationReceipt, snapshot *WorkflowReplaySnapshot, _ *WorkflowReplayInputs) {
				step := receipt.StepReceipts[0]
				snapshot.ToolVersions[" "+step.ToolID+" "] = step.ToolVersion
			},
			wantReason: "tool_version_pin_key_mismatch",
		},
		{
			name: "inputs pin key padded",
			mutate: func(receipt *WorkflowInvocationReceipt, _ *WorkflowReplaySnapshot, inputs *WorkflowReplayInputs) {
				step := receipt.StepReceipts[0]
				inputs.ToolVersions[" "+step.StepID+" "] = step.ToolVersion
			},
			wantReason: "tool_version_pin_key_mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, 1)
			tc.mutate(receipt, snapshot, inputs)

			report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
				ExpectedExecutorPubkey: pubkey,
			})

			require.NoError(t, err)
			require.Equal(t, WorkflowReplayOutcomeFail, report.ReplayOutcome)
			require.Positive(t, report.DivergenceCount)
			require.Contains(t, workflowReplayToolDiffReasons(report), tc.wantReason)
		})
	}
}

func TestReplay_StateFieldDiff(t *testing.T) {
	receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, 2)
	inputs.StateFields["module:x/workflows/sequence"] = "mutated"

	report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
		ExpectedExecutorPubkey: pubkey,
	})

	require.NoError(t, err)
	require.Equal(t, WorkflowReplayOutcomeFail, report.ReplayOutcome)
	require.Contains(t, workflowReplayStateDiffReasons(report), "state_field_mismatch")
}

func TestReplay_StateFieldDiffRejectsNonCanonicalValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*WorkflowReplaySnapshot, *WorkflowReplayInputs)
	}{
		{
			name: "snapshot state padded",
			mutate: func(snapshot *WorkflowReplaySnapshot, _ *WorkflowReplayInputs) {
				snapshot.StateFields["module:x/workflows/sequence"] = " " + snapshot.StateFields["module:x/workflows/sequence"] + " "
			},
		},
		{
			name: "inputs state padded",
			mutate: func(_ *WorkflowReplaySnapshot, inputs *WorkflowReplayInputs) {
				inputs.StateFields["module:x/workflows/sequence"] = " " + inputs.StateFields["module:x/workflows/sequence"] + " "
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, 2)
			tc.mutate(snapshot, inputs)

			report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
				ExpectedExecutorPubkey: pubkey,
			})

			require.NoError(t, err)
			require.Equal(t, WorkflowReplayOutcomeFail, report.ReplayOutcome)
			require.Positive(t, report.DivergenceCount)
			require.Contains(t, workflowReplayStateDiffReasons(report), "state_field_mismatch")
		})
	}
}

func TestReplay_DecodeRejectsTrailingJSON(t *testing.T) {
	_, snapshot, inputs, _ := workflowReplayFixture(t, 0)

	snapshotJSON, err := json.Marshal(snapshot)
	require.NoError(t, err)
	_, err = DecodeWorkflowReplaySnapshotJSON(append(snapshotJSON, []byte(` {}`)...))
	require.ErrorContains(t, err, "workflow replay JSON contains trailing data")

	inputsJSON, err := json.Marshal(inputs)
	require.NoError(t, err)
	_, err = DecodeWorkflowReplayInputsJSON(append(inputsJSON, []byte("\n[]")...))
	require.ErrorContains(t, err, "workflow replay JSON contains trailing data")
}

func TestReplay_Integration_1kBundles(t *testing.T) {
	for i := 0; i < 1000; i++ {
		receipt, snapshot, inputs, pubkey := workflowReplayFixture(t, i)

		report, err := ReplayWorkflowReceipt(receipt, snapshot, inputs, WorkflowReplayOptions{
			ExpectedExecutorPubkey: pubkey,
		})

		require.NoError(t, err)
		require.Equal(t, WorkflowReplayOutcomePass, report.ReplayOutcome)
		require.Equal(t, 0, report.DivergenceCount)
	}
}

func FuzzReplay_SnapshotFormat(f *testing.F) {
	_, snapshot, _, _ := workflowReplayFixture(f, 0)
	seed, err := json.Marshal(snapshot)
	require.NoError(f, err)
	f.Add(seed)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"snapshot_id":"bad","anchored_height":"nope"}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		snapshot, err := DecodeWorkflowReplaySnapshotJSON(data)
		if err != nil {
			return
		}
		_, _ = json.Marshal(snapshot)
	})
}

func workflowReplayFixture(t require.TestingT, index int) (*WorkflowInvocationReceipt, *WorkflowReplaySnapshot, *WorkflowReplayInputs, string) {
	seedByte := byte(0x21 + (index % 64))
	seed := bytes.Repeat([]byte{seedByte}, ed448.SeedSize)
	priv := ed448.NewKeyFromSeed(seed)
	pubkey, err := RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)

	anchoredHeight := int64(1000 + index)
	completedAt := time.Date(2026, 5, 8, 12, index%60, index%60, 0, time.UTC).Format(time.RFC3339)
	stepCount := 2 + (index % 3)
	steps := make([]WorkflowStepInvocation, 0, stepCount)
	totalCost := 0
	for stepIndex := 0; stepIndex < stepCount; stepIndex++ {
		cost := index + stepIndex + 1
		totalCost += cost
		steps = append(steps, WorkflowStepInvocation{
			StepID:        fmt.Sprintf("step-%02d", stepIndex),
			ToolID:        fmt.Sprintf("tool.workflow.%02d", stepIndex),
			ToolVersion:   fmt.Sprintf("1.%d.%d", index%5, stepIndex),
			Outcome:       WorkflowStepOutcomeSuccess,
			Cost:          QuoteCoin{Denom: "ulac", Amount: fmt.Sprint(cost)},
			DurationMS:    uint32(10 + stepIndex),
			AttemptCount:  1,
			FailureAction: FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
		})
	}

	captured := []WorkflowReplayCapturedInput{
		{
			InputID:        "wall_clock.completed_at",
			Source:         "workflow.invoke.completed_at",
			AnchoredHeight: anchoredHeight,
			Value:          completedAt,
		},
		{
			InputID:        "random_nonce.bundle_quote",
			Source:         "bundle_quote.nonce",
			AnchoredHeight: anchoredHeight - 1,
			Value:          fmt.Sprintf("nonce-%04d", index),
		},
		{
			InputID:        "oracle_height.bundle_quote",
			Source:         "bundle_quote.anchored_height",
			AnchoredHeight: anchoredHeight - 1,
			Value:          fmt.Sprint(anchoredHeight - 1),
		},
	}

	receipt := &WorkflowInvocationReceipt{
		BundleID:     fmt.Sprintf("bundle-replay-%04d", index),
		WorkflowID:   "wf-replay",
		Version:      "1.0.0",
		Outcome:      WorkflowOutcomeFinalized,
		StepReceipts: cloneWorkflowStepInvocations(steps),
		TotalCost:    QuoteCoin{Denom: "ulac", Amount: fmt.Sprint(totalCost)},
		LockID:       fmt.Sprintf("lock-replay-%04d", index),
		TraceID:      fmt.Sprintf("trace-replay-%04d", index),
		CompletedAt:  completedAt,
	}
	anchors := []*WorkflowReceiptAnchor{
		{
			ChainId:        "lumera",
			TxHash:         bytes.Repeat([]byte{byte(index % 251)}, workflowReceiptHashSize),
			AnchoredHeight: anchoredHeight,
			AnchoredAt:     time.Date(2026, 5, 8, 12, index%60, index%60, 0, time.UTC),
			Status:         "committed",
		},
	}
	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: workflowReplayCapturedInputs(captured),
		Anchors:                anchors,
	}))

	toolVersions := make(map[string]string, len(steps)*2)
	inputSteps := cloneWorkflowStepInvocations(steps)
	for stepIndex := range inputSteps {
		inputSteps[stepIndex].ReceiptHash = nil
		toolVersions[inputSteps[stepIndex].StepID] = inputSteps[stepIndex].ToolVersion
		toolVersions[inputSteps[stepIndex].ToolID] = inputSteps[stepIndex].ToolVersion
	}
	stateFields := map[string]string{
		"module:x/workflows/sequence": fmt.Sprint(index),
		"receipt:bundle_id":           receipt.BundleID,
	}
	snapshot := &WorkflowReplaySnapshot{
		SnapshotID:             fmt.Sprintf("snapshot-replay-%04d", index),
		AnchoredHeight:         anchoredHeight,
		ToolVersions:           cloneWorkflowReplayMap(toolVersions),
		StateFields:            cloneWorkflowReplayMap(stateFields),
		NonDeterministicInputs: workflowReplayCapturedInputs(captured),
	}
	inputs := &WorkflowReplayInputs{
		BundleID:               receipt.BundleID,
		WorkflowID:             receipt.WorkflowID,
		Version:                receipt.Version,
		Outcome:                receipt.Outcome,
		TotalCost:              receipt.TotalCost,
		LockID:                 receipt.LockID,
		TraceID:                receipt.TraceID,
		CompletedAt:            receipt.CompletedAt,
		StepReceipts:           inputSteps,
		ToolVersions:           cloneWorkflowReplayMap(toolVersions),
		StateFields:            cloneWorkflowReplayMap(stateFields),
		NonDeterministicInputs: captured,
	}
	return receipt, snapshot, inputs, pubkey
}

func workflowReplayCapturedInputs(inputs []WorkflowReplayCapturedInput) []*WorkflowNonDeterministicInput {
	out := make([]*WorkflowNonDeterministicInput, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, NewWorkflowNonDeterministicInput(input.InputID, input.Source, input.AnchoredHeight, input.Value))
	}
	return out
}

func workflowReplayInputDiffReasons(report *WorkflowReplayReport) []string {
	reasons := make([]string, 0, len(report.Diff.NonDeterministicInputs))
	for _, diff := range report.Diff.NonDeterministicInputs {
		reasons = append(reasons, diff.Reason)
	}
	return reasons
}

func workflowReplayReceiptDiffReasons(report *WorkflowReplayReport) []string {
	reasons := make([]string, 0, len(report.Diff.Receipt))
	for _, diff := range report.Diff.Receipt {
		reasons = append(reasons, diff.Reason)
	}
	return reasons
}

func workflowReplayStepDiffReasons(report *WorkflowReplayReport) []string {
	reasons := make([]string, 0, len(report.Diff.Steps))
	for _, diff := range report.Diff.Steps {
		reasons = append(reasons, diff.Reason)
	}
	return reasons
}

func workflowReplayToolDiffReasons(report *WorkflowReplayReport) []string {
	reasons := make([]string, 0, len(report.Diff.ToolVersions))
	for _, diff := range report.Diff.ToolVersions {
		reasons = append(reasons, diff.Reason)
	}
	return reasons
}

func workflowReplayStateDiffReasons(report *WorkflowReplayReport) []string {
	reasons := make([]string, 0, len(report.Diff.State))
	for _, diff := range report.Diff.State {
		reasons = append(reasons, diff.Reason)
	}
	return reasons
}

func cloneWorkflowReplayMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
