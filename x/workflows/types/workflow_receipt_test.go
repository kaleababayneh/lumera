package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/ed448"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

// cloneWorkflowReceiptProto deep-copies a WorkflowReceipt via a marshal/unmarshal
// round-trip. gogoproto's proto.Clone panics on messages that embed a Coin /
// math.Int (TotalCost, step Cost), so the round-trip is used instead.
func cloneWorkflowReceiptProto(t require.TestingT, src *WorkflowReceipt) *WorkflowReceipt {
	raw, err := proto.Marshal(src)
	require.NoError(t, err)
	dst := &WorkflowReceipt{}
	require.NoError(t, proto.Unmarshal(raw, dst))
	return dst
}

func TestReceipt_ProtoRoundtrip(t *testing.T) {
	receipt, pubkey, _ := workflowReceiptFixture(t)

	asProto, err := receipt.ToProto()
	require.NoError(t, err)
	wire, err := proto.Marshal(asProto)
	require.NoError(t, err)

	var decoded WorkflowReceipt
	require.NoError(t, proto.Unmarshal(wire, &decoded))
	roundTripped, err := WorkflowInvocationReceiptFromProto(&decoded)
	require.NoError(t, err)

	require.Equal(t, receipt.MerkleRoot, roundTripped.MerkleRoot)
	require.Equal(t, receipt.StepReceiptHashes, roundTripped.StepReceiptHashes)
	require.Equal(t, receipt.ExecutorSig, roundTripped.ExecutorSig)
	require.NoError(t, VerifyWorkflowReceipt(roundTripped, pubkey))
}

func TestVerifyWorkflowReceiptRejectsNilReceipt(t *testing.T) {
	require.NotPanics(t, func() {
		err := VerifyWorkflowReceipt(nil, "")
		require.ErrorContains(t, err, "workflow invocation receipt cannot be nil")
	})
}

func TestVerifyWorkflowReceiptMerkleRootRejectsShortCanonicalOrder(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)
	receipt.CanonicalStepOrder = receipt.CanonicalStepOrder[:1]

	require.NotPanics(t, func() {
		err := VerifyWorkflowReceiptMerkleRoot(receipt)
		require.ErrorContains(t, err, "workflow receipt canonical order count mismatch")
	})
}

func TestReceipt_FromProtoRejectsNonCanonicalFields(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)
	asProto, err := receipt.ToProto()
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(*WorkflowReceipt)
		want   string
	}{
		{
			name: "workflow id",
			mutate: func(receipt *WorkflowReceipt) {
				receipt.WorkflowId = " " + receipt.GetWorkflowId() + " "
			},
			want: "workflow invocation receipt workflow_id must be canonical",
		},
		{
			name: "trace id",
			mutate: func(receipt *WorkflowReceipt) {
				receipt.TraceId = " " + receipt.GetTraceId() + " "
			},
			want: "workflow invocation receipt trace_id must be canonical",
		},
		{
			name: "step tool id",
			mutate: func(receipt *WorkflowReceipt) {
				receipt.StepReceipts[0].ToolId = " " + receipt.StepReceipts[0].GetToolId() + " "
			},
			want: "workflow invocation step step-a tool_id must be canonical",
		},
		{
			name: "step error code",
			mutate: func(receipt *WorkflowReceipt) {
				receipt.StepReceipts[0].ErrorCode = " publisher_down "
			},
			want: "workflow invocation step step-a error_code must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutated := cloneWorkflowReceiptProto(t, asProto)
			tc.mutate(mutated)

			_, err := WorkflowInvocationReceiptFromProto(mutated)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// NOTE: TestReceipt_FromProtoRejectsMalformedCompletedAtTimestamp was removed
// after the gogoproto migration: WorkflowReceipt.completed_at is now a value
// time.Time (stdtime) which has no out-of-range/invalid state to reject.

func TestReceipt_FailureAttribution_ProtoRoundtripAndSignature(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	receipt.Outcome = WorkflowOutcomeReverted
	receipt.FailureCode = "publisher_down"
	receipt.FailureReason = "publisher unavailable"
	receipt.TotalCost = QuoteCoin{Denom: "ulac", Amount: "3"}
	receipt.StepReceipts[1].Outcome = WorkflowStepOutcomeFailed
	receipt.StepReceipts[1].Cost = QuoteCoin{Denom: "ulac", Amount: "0"}
	receipt.StepReceipts[1].ErrorCode = "publisher_down"
	receipt.StepReceipts[1].ErrorMessage = "publisher unavailable"

	require.NoError(t, PopulateWorkflowFailureAttributions(receipt))
	require.Len(t, receipt.FailureAttributions, 1)
	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}))
	require.NoError(t, VerifyWorkflowReceipt(receipt, pubkey))

	asProto, err := receipt.ToProto()
	require.NoError(t, err)
	wire, err := proto.Marshal(asProto)
	require.NoError(t, err)

	var decoded WorkflowReceipt
	require.NoError(t, proto.Unmarshal(wire, &decoded))
	roundTripped, err := WorkflowInvocationReceiptFromProto(&decoded)
	require.NoError(t, err)

	require.Len(t, roundTripped.FailureAttributions, 1)
	attr := roundTripped.FailureAttributions[0]
	require.Equal(t, "step-b", attr.GetStepId())
	require.Equal(t, "publisher_down", attr.GetReasonCode())
	var snapshot workflowFailureStateSnapshot
	require.NoError(t, json.Unmarshal(attr.GetStateSnapshot(), &snapshot))
	require.Equal(t, receipt.BundleID, snapshot.BundleID)
	require.Equal(t, receipt.WorkflowID, snapshot.WorkflowID)
	require.Equal(t, WorkflowOutcomeReverted, snapshot.Outcome)
	require.Equal(t, "publisher_down", snapshot.FailureCode)
	require.NotNil(t, snapshot.Step)
	require.Equal(t, "step-b", snapshot.Step.StepID)
	require.Equal(t, WorkflowStepOutcomeFailed, snapshot.Step.Outcome)
	require.Equal(t, "publisher_down", snapshot.Step.ErrorCode)
	require.NoError(t, VerifyWorkflowReceipt(roundTripped, pubkey))
}

func TestReceipt_FinalizeRejectsNonCanonicalFailureAttributions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowFailureAttribution)
		want   string
	}{
		{
			name: "step id",
			mutate: func(attr *WorkflowFailureAttribution) {
				attr.StepId = " " + attr.GetStepId() + " "
			},
			want: "workflow failure attribution step_id must be canonical",
		},
		{
			name: "reason code",
			mutate: func(attr *WorkflowFailureAttribution) {
				attr.ReasonCode = " " + attr.GetReasonCode() + " "
			},
			want: "workflow failure attribution reason_code must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, priv := workflowReceiptFixture(t)
			receipt.Outcome = WorkflowOutcomeReverted
			receipt.FailureCode = "publisher_down"
			receipt.FailureReason = "publisher unavailable"
			receipt.TotalCost = QuoteCoin{Denom: "ulac", Amount: "3"}
			receipt.StepReceipts[1].Outcome = WorkflowStepOutcomeFailed
			receipt.StepReceipts[1].Cost = QuoteCoin{Denom: "ulac", Amount: "0"}
			receipt.StepReceipts[1].ErrorCode = "publisher_down"
			receipt.StepReceipts[1].ErrorMessage = "publisher unavailable"
			require.NoError(t, PopulateWorkflowFailureAttributions(receipt))
			require.NotEmpty(t, receipt.FailureAttributions)
			tc.mutate(receipt.FailureAttributions[0])

			require.ErrorContains(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
				ExecutorPrivateKey:     priv,
				NonDeterministicInputs: receipt.NonDeterministicInputs,
			}), tc.want)
		})
	}
}

func TestReceipt_MerkleRoot_Deterministic(t *testing.T) {
	receipt, _, priv := workflowReceiptFixture(t)
	firstRoot := append([]byte(nil), receipt.MerkleRoot...)
	firstHashes := cloneWorkflowReceiptMatrix(receipt.StepReceiptHashes)

	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}))

	require.Equal(t, firstRoot, receipt.MerkleRoot)
	require.Equal(t, firstHashes, receipt.StepReceiptHashes)
}

func TestReceipt_FinalizeRejectsNonCanonicalFields(t *testing.T) {
	receipt, _, priv := workflowReceiptFixture(t)
	receipt.WorkflowID = " " + receipt.WorkflowID + " "

	require.ErrorContains(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}), "workflow invocation receipt workflow_id must be canonical")
}

func TestReceipt_ToProtoRejectsNonCanonicalFields(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)
	receipt.WorkflowID = " " + receipt.WorkflowID + " "

	_, err := receipt.ToProto()
	require.ErrorContains(t, err, "workflow invocation receipt workflow_id must be canonical")
}

func TestReceipt_Tamper_Detection(t *testing.T) {
	receipt, pubkey, _ := workflowReceiptFixture(t)
	tampered := cloneWorkflowReceiptForTest(t, receipt)
	tampered.StepReceipts[0].Cost.Amount = "999"

	err := VerifyWorkflowReceipt(tampered, pubkey)
	require.ErrorContains(t, err, "step hash mismatch")
}

func TestReceipt_OutputClaimEvidenceRoundtripAndConditionProof(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	condition, err := ParseWorkflowCondition("steps.step-a.output.policy_allowed == true")
	require.NoError(t, err)
	claim, err := NewWorkflowOutputClaimCommitment("step-a", condition.ClaimPath, condition.Literal)
	require.NoError(t, err)
	require.True(t, claim.Redacted)
	require.Empty(t, claim.CanonicalValue)
	receipt.StepReceipts[0].OutputClaims = []WorkflowOutputClaim{claim}

	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}))
	require.NoError(t, VerifyWorkflowReceipt(receipt, pubkey))

	asProto, err := receipt.ToProto()
	require.NoError(t, err)
	require.Len(t, asProto.GetStepReceipts()[0].GetOutputClaims(), 1)
	require.Empty(t, asProto.GetStepReceipts()[0].GetOutputClaims()[0].GetCanonicalValue())
	roundTripped, err := WorkflowInvocationReceiptFromProto(asProto)
	require.NoError(t, err)
	require.NoError(t, VerifyWorkflowReceipt(roundTripped, pubkey))

	matches, err := EvaluateWorkflowOutputClaimCondition(roundTripped.StepReceipts[0], condition)
	require.NoError(t, err)
	require.True(t, matches)
	proof, err := BuildWorkflowReceiptProof(roundTripped, "step-a")
	require.NoError(t, err)
	require.NoError(t, VerifyWorkflowStepReveal(roundTripped.MerkleRoot, roundTripped.StepReceipts[0], proof))
}

func TestReceipt_OutputClaimEvidenceRejectsTamperingAndInvalidClaims(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	condition, err := ParseWorkflowCondition("steps.step-a.output.policy_allowed == true")
	require.NoError(t, err)
	claim, err := NewWorkflowOutputClaimReveal("step-a", condition.ClaimPath, condition.Literal)
	require.NoError(t, err)
	receipt.StepReceipts[0].OutputClaims = []WorkflowOutputClaim{claim}
	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}))
	require.NoError(t, VerifyWorkflowReceipt(receipt, pubkey))

	tampered := cloneWorkflowReceiptForTest(t, receipt)
	tampered.StepReceipts[0].OutputClaims[0].CanonicalValue = "false"
	require.ErrorContains(t, VerifyWorkflowReceipt(tampered, pubkey), "claim_hash does not match canonical_value")

	missingPath, err := ParseWorkflowCondition("steps.step-a.output.missing == true")
	require.NoError(t, err)
	_, err = EvaluateWorkflowOutputClaimCondition(receipt.StepReceipts[0], missingPath)
	require.ErrorContains(t, err, "workflow output claim missing path")

	_, err = NewWorkflowOutputClaimReveal("step-a", []string{"status"}, WorkflowConditionLiteral{Kind: WorkflowConditionLiteralOutcome, String: "success"})
	require.ErrorContains(t, err, "scalar kind unsupported")

	_, err = NewWorkflowOutputClaimReveal("step-a", []string{"long_value"}, WorkflowConditionLiteral{Kind: WorkflowConditionLiteralString, String: strings.Repeat("x", 257)})
	require.ErrorContains(t, err, "string exceeds 256 bytes")

	redactedWithValue := claim
	redactedWithValue.Redacted = true
	_, err = NormalizeWorkflowOutputClaimsForStep("step-a", []WorkflowOutputClaim{redactedWithValue})
	require.ErrorContains(t, err, "redacted output claim cannot carry canonical_value")
}

func TestReceipt_VerifyWorkflowReceiptCondition(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	condition, err := ParseWorkflowCondition("steps.step-a.output.policy_allowed == true")
	require.NoError(t, err)
	claim, err := NewWorkflowOutputClaimCommitment("step-a", condition.ClaimPath, condition.Literal)
	require.NoError(t, err)
	receipt.StepReceipts[0].OutputClaims = []WorkflowOutputClaim{claim}
	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}))
	require.NoError(t, VerifyWorkflowReceipt(receipt, pubkey))

	matches, err := VerifyWorkflowReceiptCondition(receipt, pubkey, "steps.step-a.output.policy_allowed == true")
	require.NoError(t, err)
	require.True(t, matches)

	matches, err = VerifyWorkflowReceiptCondition(receipt, pubkey, "steps.step-a.output.policy_allowed == false")
	require.NoError(t, err)
	require.False(t, matches)

	matches, err = VerifyWorkflowReceiptCondition(receipt, pubkey, "steps.step-a.outcome == 'success'")
	require.NoError(t, err)
	require.True(t, matches)

	matches, err = VerifyWorkflowReceiptCondition(receipt, pubkey, "steps.step-a.outcome != 'success'")
	require.NoError(t, err)
	require.False(t, matches)

	matches, err = VerifyWorkflowReceiptCondition(receipt, pubkey, "false")
	require.NoError(t, err)
	require.False(t, matches)
}

func TestReceipt_VerifyWorkflowReceiptConditionFailsClosed(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	condition, err := ParseWorkflowCondition("steps.step-a.output.policy_allowed == true")
	require.NoError(t, err)
	claim, err := NewWorkflowOutputClaimReveal("step-a", condition.ClaimPath, condition.Literal)
	require.NoError(t, err)
	receipt.StepReceipts[0].OutputClaims = []WorkflowOutputClaim{claim}
	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}))

	_, err = VerifyWorkflowReceiptCondition(receipt, pubkey, "steps.step-missing.output.policy_allowed == true")
	require.ErrorContains(t, err, "workflow condition references missing receipt step")

	_, err = VerifyWorkflowReceiptCondition(receipt, pubkey, "steps.step-a.output.missing == true")
	require.ErrorContains(t, err, "workflow output claim missing path")

	matches, err := VerifyWorkflowReceiptCondition(receipt, pubkey, "steps.step-a.output.policy_allowed == \"true\"")
	require.NoError(t, err)
	require.False(t, matches)

	tampered := cloneWorkflowReceiptForTest(t, receipt)
	tampered.StepReceipts[0].OutputClaims[0].CanonicalValue = "false"
	_, err = VerifyWorkflowReceiptCondition(tampered, pubkey, "steps.step-a.output.policy_allowed == true")
	require.ErrorContains(t, err, "claim_hash does not match canonical_value")

	tampered = cloneWorkflowReceiptForTest(t, receipt)
	tampered.StepReceipts[0].OutputClaims[0].ClaimHash[0] ^= 0x01
	_, err = VerifyWorkflowReceiptCondition(tampered, pubkey, "steps.step-a.output.policy_allowed == true")
	require.ErrorContains(t, err, "claim_hash does not match canonical_value")
}

func TestReceipt_VerifyRejectsUnknownStepOutcome(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	receipt.StepReceipts[0].Outcome = "completed"
	forceFinalizeWorkflowReceiptForTest(t, receipt, priv)

	require.ErrorContains(t, VerifyWorkflowReceipt(receipt, pubkey), "invalid workflow invocation step step-a outcome")
}

func TestReceipt_VerifyRedactsStepOutcomeDiagnostics(t *testing.T) {
	t.Run("step id", func(t *testing.T) {
		receipt, pubkey, priv := workflowReceiptFixture(t)
		rawValue := "workflow-step-secret-" + strings.Repeat("x", 20)
		receipt.StepReceipts[0].StepID = "step?Authorization=Bearer " + rawValue
		receipt.StepReceipts[0].Outcome = "completed"
		forceFinalizeWorkflowReceiptForTest(t, receipt, priv)

		err := VerifyWorkflowReceipt(receipt, pubkey)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid workflow invocation step")
		require.Contains(t, err.Error(), "[REDACTED]")
		require.NotContains(t, err.Error(), rawValue)
	})

	t.Run("outcome", func(t *testing.T) {
		receipt, pubkey, priv := workflowReceiptFixture(t)
		rawValue := "workflow-outcome-secret-" + strings.Repeat("x", 20)
		receipt.StepReceipts[0].Outcome = "Authorization=Bearer " + rawValue
		forceFinalizeWorkflowReceiptForTest(t, receipt, priv)

		err := VerifyWorkflowReceipt(receipt, pubkey)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid workflow invocation step step-a outcome")
		require.Contains(t, err.Error(), "[REDACTED]")
		require.NotContains(t, err.Error(), rawValue)
	})

	t.Run("top level outcome", func(t *testing.T) {
		receipt, pubkey, priv := workflowReceiptFixture(t)
		rawValue := "workflow-receipt-outcome-secret-" + strings.Repeat("x", 20)
		receipt.Outcome = "Authorization=Bearer " + rawValue
		forceFinalizeWorkflowReceiptForTest(t, receipt, priv)

		err := VerifyWorkflowReceipt(receipt, pubkey)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid workflow invocation outcome")
		require.Contains(t, err.Error(), "[REDACTED]")
		require.NotContains(t, err.Error(), rawValue)
	})
}

func TestReceipt_VerifyRejectsPaddedWorkflowOutcome(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	receipt.Outcome = " " + WorkflowOutcomeFinalized + " "
	forceFinalizeWorkflowReceiptForTest(t, receipt, priv)

	require.ErrorContains(t, VerifyWorkflowReceipt(receipt, pubkey), "workflow invocation outcome must be canonical")
}

func TestReceipt_VerifyRejectsPaddedStepID(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	receipt.StepReceipts[0].StepID = " " + receipt.StepReceipts[0].StepID + " "
	forceFinalizeWorkflowReceiptForTest(t, receipt, priv)

	require.ErrorContains(t, VerifyWorkflowReceipt(receipt, pubkey), "workflow invocation step_id must be canonical")
}

func TestReceipt_FinalizeRejectsPaddedStepToolIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowInvocationReceipt)
		want   string
	}{
		{
			name: "tool id",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.StepReceipts[0].ToolID = " " + receipt.StepReceipts[0].ToolID + " "
			},
			want: "workflow invocation step step-a tool_id must be canonical",
		},
		{
			name: "tool version",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.StepReceipts[0].ToolVersion = " " + receipt.StepReceipts[0].ToolVersion + " "
			},
			want: "workflow invocation step step-a tool_version must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, priv := workflowReceiptFixture(t)
			tc.mutate(receipt)

			require.ErrorContains(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
				ExecutorPrivateKey:     priv,
				NonDeterministicInputs: receipt.NonDeterministicInputs,
			}), tc.want)
		})
	}
}

func TestReceipt_VerifyRejectsMalformedCompletedAt(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	receipt.CompletedAt = "not-rfc3339"
	forceFinalizeWorkflowReceiptForTest(t, receipt, priv)

	require.ErrorContains(t, VerifyWorkflowReceipt(receipt, pubkey), "invalid workflow receipt completed_at")
}

func TestReceipt_ValidateRejectsPaddedCompletedAt(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)
	receipt.CompletedAt = " " + receipt.CompletedAt + " "

	require.ErrorContains(t, receipt.ValidateBasic(), "workflow invocation receipt completed_at must be canonical")
}

func TestReceipt_ValidateRejectsNonCanonicalOptionalFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowInvocationReceipt)
		want   string
	}{
		{
			name: "failure code",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.FailureCode = " " + receipt.FailureCode + "publisher_down "
			},
			want: "workflow invocation receipt failure_code must be canonical",
		},
		{
			name: "failure reason",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.FailureReason = " publisher unavailable "
			},
			want: "workflow invocation receipt failure_reason must be canonical",
		},
		{
			name: "trace id",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.TraceID = " " + receipt.TraceID + " "
			},
			want: "workflow invocation receipt trace_id must be canonical",
		},
		{
			name: "executor pubkey",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.ExecutorPubkey = " " + receipt.ExecutorPubkey + " "
			},
			want: "workflow invocation receipt executor_pubkey must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, _ := workflowReceiptFixture(t)
			tc.mutate(receipt)

			require.ErrorContains(t, receipt.ValidateBasic(), tc.want)
		})
	}
}

func TestReceipt_ValidateRejectsNonCanonicalStepErrorFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowInvocationReceipt)
		want   string
	}{
		{
			name: "error code",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.StepReceipts[0].Outcome = WorkflowStepOutcomeFailed
				receipt.StepReceipts[0].ErrorCode = " publisher_down "
				receipt.StepReceipts[0].ErrorMessage = "publisher unavailable"
			},
			want: "workflow invocation step step-a error_code must be canonical",
		},
		{
			name: "error message",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.StepReceipts[0].Outcome = WorkflowStepOutcomeFailed
				receipt.StepReceipts[0].ErrorCode = "publisher_down"
				receipt.StepReceipts[0].ErrorMessage = " publisher unavailable "
			},
			want: "workflow invocation step step-a error_message must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, _ := workflowReceiptFixture(t)
			tc.mutate(receipt)

			require.ErrorContains(t, receipt.ValidateBasic(), tc.want)
		})
	}
}

func TestReceipt_ValidateRejectsOutcomeFailureInconsistency(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowInvocationReceipt)
		want   string
	}{
		{
			name: "finalized receipt failure code",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.FailureCode = "publisher_down"
			},
			want: "finalized receipt must not include failure_code",
		},
		{
			name: "finalized receipt failure reason",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.FailureReason = "publisher unavailable"
			},
			want: "finalized receipt must not include failure_reason",
		},
		{
			name: "finalized receipt failure attributions",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.FailureAttributions = []*WorkflowFailureAttribution{{
					StepId:        "workflow",
					ReasonCode:    "publisher_down",
					StateSnapshot: []byte("{}"),
				}}
			},
			want: "finalized receipt must not include failure_attributions",
		},
		{
			name: "reverted receipt missing failure code",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Outcome = WorkflowOutcomeReverted
				receipt.FailureReason = "publisher unavailable"
			},
			want: "REVERTED receipt failure_code is required",
		},
		{
			name: "partial skip receipt missing failure reason",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Outcome = WorkflowOutcomePartialSkip
				receipt.FailureCode = "dependency_skipped"
			},
			want: "PARTIAL_SKIP receipt failure_reason is required",
		},
		{
			name: "successful step error code",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.StepReceipts[0].ErrorCode = "publisher_down"
			},
			want: "successful step step-a must not include error_code",
		},
		{
			name: "successful step error message",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.StepReceipts[0].ErrorMessage = "publisher unavailable"
			},
			want: "successful step step-a must not include error_message",
		},
		{
			name: "failed step missing error code",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Outcome = WorkflowOutcomeReverted
				receipt.FailureCode = "publisher_down"
				receipt.FailureReason = "publisher unavailable"
				receipt.StepReceipts[0].Outcome = WorkflowStepOutcomeFailed
			},
			want: "step step-a error_code is required for failed outcome",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, _ := workflowReceiptFixture(t)
			tc.mutate(receipt)

			require.ErrorContains(t, receipt.ValidateBasic(), tc.want)
		})
	}
}

func TestReceipt_ValidateAcceptsKnownStepOutcomes(t *testing.T) {
	for _, outcome := range []string{
		WorkflowStepOutcomeSuccess,
		WorkflowStepOutcomeFailed,
		WorkflowStepOutcomeSkipped,
		WorkflowStepOutcomeError,
	} {
		require.NoError(t, validateWorkflowStepReceiptOutcome("step-a", outcome))
	}
}

func TestReceipt_ValidateRejectsNonCanonicalCosts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowInvocationReceipt)
		want   string
	}{
		{
			name: "total cost padded denom",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.TotalCost.Denom = " ulac "
			},
			want: "quote coin denom must be canonical",
		},
		{
			name: "total cost leading zero amount",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.TotalCost.Amount = "08"
			},
			want: "quote coin amount must be canonical",
		},
		{
			name: "step cost padded amount",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.StepReceipts[0].Cost.Amount = " 3 "
			},
			want: "quote coin amount must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, _ := workflowReceiptFixture(t)
			tc.mutate(receipt)

			err := receipt.ValidateBasic()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateBasic error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReceipt_PartialReveal_Verification(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)
	proof, err := BuildWorkflowReceiptProof(receipt, "step-b")
	require.NoError(t, err)

	require.NoError(t, VerifyWorkflowStepReveal(receipt.MerkleRoot, receipt.StepReceipts[1], proof))

	tampered := receipt.StepReceipts[1]
	tampered.ErrorMessage = "mutated"
	require.ErrorContains(t, VerifyWorkflowStepReveal(receipt.MerkleRoot, tampered, proof), "leaf hash mismatch")
}

func TestReceipt_BuildProofRejectsNonCanonicalStepID(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)

	_, err := BuildWorkflowReceiptProof(receipt, " step-b ")

	require.ErrorContains(t, err, "workflow receipt proof step_id must be canonical")
}

func TestReceipt_PartialRevealRejectsNonCanonicalFields(t *testing.T) {
	receipt, _, priv := workflowReceiptFixture(t)
	receipt.Outcome = WorkflowOutcomeReverted
	receipt.FailureCode = "publisher_down"
	receipt.FailureReason = "publisher unavailable"
	receipt.TotalCost = QuoteCoin{Denom: "ulac", Amount: "3"}
	receipt.StepReceipts[1].Outcome = WorkflowStepOutcomeFailed
	receipt.StepReceipts[1].Cost = QuoteCoin{Denom: "ulac", Amount: "0"}
	receipt.StepReceipts[1].ErrorCode = "publisher_down"
	receipt.StepReceipts[1].ErrorMessage = "publisher unavailable"
	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}))
	proof, err := BuildWorkflowReceiptProof(receipt, "step-b")
	require.NoError(t, err)

	tests := []struct {
		name        string
		mutateStep  func(*WorkflowStepInvocation)
		mutateProof func(*WorkflowMerkleProof)
		want        string
	}{
		{
			name: "proof step id",
			mutateProof: func(proof *WorkflowMerkleProof) {
				proof.StepId = " " + proof.GetStepId() + " "
			},
			want: "workflow receipt proof step_id must be canonical",
		},
		{
			name: "step id",
			mutateStep: func(step *WorkflowStepInvocation) {
				step.StepID = " " + step.StepID + " "
			},
			want: "workflow invocation step_id must be canonical",
		},
		{
			name: "tool id",
			mutateStep: func(step *WorkflowStepInvocation) {
				step.ToolID = " " + step.ToolID + " "
			},
			want: "workflow invocation step step-b tool_id must be canonical",
		},
		{
			name: "tool version",
			mutateStep: func(step *WorkflowStepInvocation) {
				step.ToolVersion = " " + step.ToolVersion + " "
			},
			want: "workflow invocation step step-b tool_version must be canonical",
		},
		{
			name: "outcome",
			mutateStep: func(step *WorkflowStepInvocation) {
				step.Outcome = " " + step.Outcome + " "
			},
			want: "workflow invocation step step-b outcome must be canonical",
		},
		{
			name: "error code",
			mutateStep: func(step *WorkflowStepInvocation) {
				step.ErrorCode = " " + step.ErrorCode + " "
			},
			want: "workflow invocation step step-b error_code must be canonical",
		},
		{
			name: "error message",
			mutateStep: func(step *WorkflowStepInvocation) {
				step.ErrorMessage = " " + step.ErrorMessage + " "
			},
			want: "workflow invocation step step-b error_message must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step := receipt.StepReceipts[1]
			step.ReceiptHash = cloneWorkflowReceiptBytes(step.ReceiptHash)
			proof := cloneWorkflowMerkleProofForTest(proof)
			if tc.mutateStep != nil {
				tc.mutateStep(&step)
			}
			if tc.mutateProof != nil {
				tc.mutateProof(proof)
			}

			require.ErrorContains(t, VerifyWorkflowStepReveal(receipt.MerkleRoot, step, proof), tc.want)
		})
	}
}

func TestReceipt_Signature_Verify(t *testing.T) {
	receipt, pubkey, _ := workflowReceiptFixture(t)
	require.NoError(t, VerifyWorkflowReceiptSignature(receipt, pubkey))

	tampered := cloneWorkflowReceiptForTest(t, receipt)
	tampered.TraceID = "trace-mutated"
	require.ErrorContains(t, VerifyWorkflowReceiptSignature(tampered, pubkey), "signature does not match")
}

func TestReceipt_SignatureCoversInvariantLogs(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	receipt.InvariantLogs = []InvariantEvaluationLog{workflowInvariantLogFixture()}
	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: receipt.NonDeterministicInputs,
	}))
	require.NoError(t, VerifyWorkflowReceiptSignature(receipt, pubkey))

	tampered := *receipt
	tampered.StepReceipts = cloneWorkflowStepInvocations(receipt.StepReceipts)
	tampered.StepReceiptHashes = cloneWorkflowReceiptMatrix(receipt.StepReceiptHashes)
	tampered.MerkleRoot = cloneWorkflowReceiptBytes(receipt.MerkleRoot)
	tampered.CanonicalStepOrder = append([]string(nil), receipt.CanonicalStepOrder...)
	tampered.NonDeterministicInputs = cloneWorkflowNonDeterministicInputs(receipt.NonDeterministicInputs)
	tampered.ExecutorSig = cloneWorkflowReceiptBytes(receipt.ExecutorSig)
	tampered.InvariantLogs = append([]InvariantEvaluationLog(nil), receipt.InvariantLogs...)
	tampered.InvariantLogs[0].Result = InvariantResultFail
	tampered.InvariantLogs[0].ReasonCode = InvariantReasonCostExceeded

	require.ErrorContains(t, VerifyWorkflowReceiptSignature(&tampered, pubkey), "signature does not match")
}

func TestReceipt_SignatureRejectsExpectedExecutorPubkeyMismatch(t *testing.T) {
	receipt, pubkey, priv := workflowReceiptFixture(t)
	otherPriv := ed448.NewKeyFromSeed(bytes.Repeat([]byte{0x43}, ed448.SeedSize))
	otherPubkey, err := RouterPubkeyFromPrivateKey(otherPriv)
	require.NoError(t, err)
	mismatched := cloneWorkflowReceiptForTest(t, receipt)
	mismatched.ExecutorPubkey = otherPubkey
	mismatched.ExecutorSig = nil
	canonical, err := CanonicalWorkflowReceiptBytes(mismatched)
	require.NoError(t, err)
	mismatched.ExecutorSig = ed448.Sign(priv, canonical, "")

	require.ErrorContains(t, VerifyWorkflowReceiptSignature(mismatched, pubkey), "executor pubkey does not match expected public key")
}

func TestReceipt_SignatureRejectsNonCanonicalFields(t *testing.T) {
	receipt, pubkey, _ := workflowReceiptFixture(t)
	receipt.WorkflowID = " " + receipt.WorkflowID + " "

	require.ErrorContains(t, VerifyWorkflowReceiptSignature(receipt, pubkey), "workflow invocation receipt workflow_id must be canonical")
}

func TestReceipt_NonDeterministicInputsCaptured(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)

	seen := make(map[string]*WorkflowNonDeterministicInput, len(receipt.NonDeterministicInputs))
	for _, item := range receipt.NonDeterministicInputs {
		seen[item.GetInputId()] = item
	}
	for _, id := range []string{"wall_clock.completed_at", "random_nonce.bundle_quote", "oracle_height.bundle_quote"} {
		got := seen[id]
		require.NotNil(t, got, "missing nondeterministic input %s", id)
		require.Greater(t, got.GetAnchoredHeight(), int64(0))
		require.Len(t, got.GetInputHash(), workflowReceiptHashSize)
	}
}

func TestReceipt_FinalizeRejectsMalformedNonDeterministicInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowInvocationReceipt)
		want   string
	}{
		{
			name: "nil input",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.NonDeterministicInputs = append(receipt.NonDeterministicInputs, nil)
			},
			want: "workflow nondeterministic input 3 cannot be nil",
		},
		{
			name: "padded input id",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.NonDeterministicInputs[0].InputId = " " + receipt.NonDeterministicInputs[0].GetInputId() + " "
			},
			want: "workflow nondeterministic input input_id must be canonical",
		},
		{
			name: "missing input id",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.NonDeterministicInputs[0].InputId = " "
			},
			want: "workflow nondeterministic input 0 missing input_id",
		},
		{
			name: "duplicate input id",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.NonDeterministicInputs[1].InputId = receipt.NonDeterministicInputs[0].GetInputId()
			},
			want: "duplicate workflow nondeterministic input_id",
		},
		{
			name: "padded source",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.NonDeterministicInputs[0].Source = " " + receipt.NonDeterministicInputs[0].GetSource() + " "
			},
			want: "workflow nondeterministic input wall_clock.completed_at source must be canonical",
		},
		{
			name: "missing source",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.NonDeterministicInputs[0].Source = " "
			},
			want: "workflow nondeterministic input wall_clock.completed_at missing source",
		},
		{
			name: "zero anchor height",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.NonDeterministicInputs[0].AnchoredHeight = 0
			},
			want: "workflow nondeterministic input wall_clock.completed_at anchored_height must be positive",
		},
		{
			name: "bad input hash length",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.NonDeterministicInputs[0].InputHash = []byte{0x01, 0x02}
			},
			want: "workflow nondeterministic input wall_clock.completed_at input_hash must be 32 bytes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, priv := workflowReceiptFixture(t)
			tc.mutate(receipt)

			require.ErrorContains(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
				ExecutorPrivateKey: priv,
			}), tc.want)
		})
	}
}

func TestReceipt_ValidateRejectsMalformedAnchors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowInvocationReceipt)
		want   string
	}{
		{
			name: "nil anchor",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Anchors = append(receipt.Anchors, nil)
			},
			want: "workflow receipt anchor 1 cannot be nil",
		},
		{
			name: "padded chain id",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Anchors[0].ChainId = " " + receipt.Anchors[0].GetChainId() + " "
			},
			want: "workflow receipt anchor chain_id must be canonical",
		},
		{
			name: "missing tx hash",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Anchors[0].TxHash = nil
			},
			want: "workflow receipt anchor lumera tx_hash is required",
		},
		{
			name: "zero height",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Anchors[0].AnchoredHeight = 0
			},
			want: "workflow receipt anchor lumera anchored_height must be positive",
		},
		{
			name: "missing timestamp",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Anchors[0].AnchoredAt = time.Time{}
			},
			want: "workflow receipt anchor lumera anchored_at is required",
		},
		// NOTE: the "invalid timestamp" case was removed: anchored_at is now a
		// value time.Time (gogoproto stdtime) which has no invalid state.
		{
			name: "padded status",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.Anchors[0].Status = " " + receipt.Anchors[0].GetStatus() + " "
			},
			want: "workflow receipt anchor lumera status must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, _ := workflowReceiptFixture(t)
			receipt.Anchors = []*WorkflowReceiptAnchor{workflowReceiptAnchorFixture(receipt)}
			tc.mutate(receipt)

			require.ErrorContains(t, receipt.ValidateBasic(), tc.want)
		})
	}
}

func TestReceipt_ValidateRejectsMalformedInvariantLogs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowInvocationReceipt)
		want   string
	}{
		{
			name: "missing invariant",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].Invariant = " "
			},
			want: "workflow invariant log 0 missing invariant",
		},
		{
			name: "padded invariant",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].Invariant = " " + receipt.InvariantLogs[0].Invariant + " "
			},
			want: "workflow invariant log invariant must be canonical",
		},
		{
			name: "invalid phase",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].Phase = "runtime"
			},
			want: "workflow invariant log phase is invalid",
		},
		{
			name: "missing inputs digest",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].InputsDigest = ""
			},
			want: "workflow invariant log total_cost <= max_cost missing inputs_digest",
		},
		{
			name: "invalid result",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].Result = "ok"
			},
			want: "workflow invariant log total_cost <= max_cost result is invalid",
		},
		{
			name: "pass reason",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].ReasonCode = InvariantReasonCostExceeded
			},
			want: "workflow invariant log total_cost <= max_cost passing result must not include reason_code",
		},
		{
			name: "fail missing reason",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].Result = InvariantResultFail
			},
			want: "workflow invariant log total_cost <= max_cost reason_code is required",
		},
		{
			name: "padded reason",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].Result = InvariantResultFail
				receipt.InvariantLogs[0].ReasonCode = " " + InvariantReasonCostExceeded + " "
			},
			want: "workflow invariant log total_cost <= max_cost reason_code must be canonical",
		},
		{
			name: "unknown reason",
			mutate: func(receipt *WorkflowInvocationReceipt) {
				receipt.InvariantLogs[0].Result = InvariantResultFail
				receipt.InvariantLogs[0].ReasonCode = "workflow_reason_unknown"
			},
			want: "workflow invariant log total_cost <= max_cost reason_code is invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receipt, _, _ := workflowReceiptFixture(t)
			receipt.InvariantLogs = []InvariantEvaluationLog{workflowInvariantLogFixture()}
			tc.mutate(receipt)

			require.ErrorContains(t, receipt.ValidateBasic(), tc.want)
		})
	}
}

func TestReceipt_RecordValidateRejectsDuplicateStepIDs(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)
	receipt.StepReceipts[1].StepID = receipt.StepReceipts[0].StepID
	receipt.CanonicalStepOrder[1] = receipt.CanonicalStepOrder[0]

	record := &WorkflowInvocationRecord{
		BundleID:      receipt.BundleID,
		Receipt:       receipt,
		UpdatedHeight: 1,
	}

	require.ErrorContains(t, record.Validate(), "duplicate workflow receipt step_id: step-a")
}

func TestReceipt_RecordValidateRejectsNonCanonicalBundleID(t *testing.T) {
	receipt, _, _ := workflowReceiptFixture(t)
	record := &WorkflowInvocationRecord{
		BundleID:      " " + receipt.BundleID + " ",
		Receipt:       receipt,
		UpdatedHeight: 1,
	}

	require.ErrorContains(t, record.Validate(), "workflow invocation record bundle_id must be canonical")
}

func TestReceipt_Metamorphic_SameBundleTwoChainsByteIdentical(t *testing.T) {
	receiptA, _, _ := workflowReceiptFixture(t)
	receiptB, _, _ := workflowReceiptFixture(t)

	protoA, err := receiptA.ToProto()
	require.NoError(t, err)
	protoB, err := receiptB.ToProto()
	require.NoError(t, err)
	bytesA, err := proto.Marshal(protoA)
	require.NoError(t, err)
	bytesB, err := proto.Marshal(protoB)
	require.NoError(t, err)
	require.True(t, bytes.Equal(bytesA, bytesB))
}

func FuzzReceipt_ProtoDecoding(f *testing.F) {
	receipt, _, _ := workflowReceiptFixture(f)
	asProto, err := receipt.ToProto()
	if err == nil {
		if wire, marshalErr := proto.Marshal(asProto); marshalErr == nil {
			f.Add(wire)
		}
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x03, 'b', 'a', 'd'})

	f.Fuzz(func(t *testing.T, data []byte) {
		var decoded WorkflowReceipt
		if err := proto.Unmarshal(data, &decoded); err != nil {
			return
		}
		receipt, err := WorkflowInvocationReceiptFromProto(&decoded)
		if err != nil {
			return
		}
		_ = VerifyWorkflowReceipt(receipt, "")
		if len(receipt.CanonicalStepOrder) > 0 {
			_, _ = BuildWorkflowReceiptProof(receipt, receipt.CanonicalStepOrder[0])
		}
	})
}

func workflowReceiptFixture(t require.TestingT) (*WorkflowInvocationReceipt, string, ed448.PrivateKey) {
	seed := bytes.Repeat([]byte{0x24}, ed448.SeedSize)
	priv := ed448.NewKeyFromSeed(seed)
	pubkey, err := RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)

	receipt := &WorkflowInvocationReceipt{
		BundleID:    "bundle-workflow-receipt",
		WorkflowID:  "wf-receipt",
		Version:     "1.0.0",
		Outcome:     WorkflowOutcomeFinalized,
		TotalCost:   QuoteCoin{Denom: "ulac", Amount: "8"},
		LockID:      "lock-bundle-workflow-receipt",
		TraceID:     "trace-receipt",
		CompletedAt: "2026-05-08T12:00:00Z",
		StepReceipts: []WorkflowStepInvocation{
			{
				StepID:        "step-a",
				ToolID:        "tool.step-a",
				ToolVersion:   "1.0.0",
				Outcome:       WorkflowStepOutcomeSuccess,
				Cost:          QuoteCoin{Denom: "ulac", Amount: "3"},
				DurationMS:    3,
				AttemptCount:  1,
				FailureAction: FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
			},
			{
				StepID:        "step-b",
				ToolID:        "tool.step-b",
				ToolVersion:   "1.0.1",
				Outcome:       WorkflowStepOutcomeSuccess,
				Cost:          QuoteCoin{Denom: "ulac", Amount: "5"},
				DurationMS:    4,
				AttemptCount:  1,
				FailureAction: FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
			},
		},
	}
	inputs := []*WorkflowNonDeterministicInput{
		NewWorkflowNonDeterministicInput("wall_clock.completed_at", "workflow.invoke.completed_at", 42, receipt.CompletedAt),
		NewWorkflowNonDeterministicInput("random_nonce.bundle_quote", "bundle_quote.nonce", 41, "nonce-receipt"),
		NewWorkflowNonDeterministicInput("oracle_height.bundle_quote", "bundle_quote.anchored_height", 41, "41"),
	}
	require.NoError(t, FinalizeWorkflowReceipt(receipt, WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     priv,
		NonDeterministicInputs: inputs,
	}))
	return receipt, pubkey, priv
}

func workflowInvariantLogFixture() InvariantEvaluationLog {
	return InvariantEvaluationLog{
		Invariant:    "total_cost <= max_cost",
		Phase:        "lock",
		InputsDigest: "blake3:8b78f9c1d52251d835d3648fca77edaf3773a36927f08949801b5e970444f7e0",
		Result:       InvariantResultPass,
	}
}

func workflowReceiptAnchorFixture(receipt *WorkflowInvocationReceipt) *WorkflowReceiptAnchor {
	sum := sha256.Sum256(receipt.MerkleRoot)
	return &WorkflowReceiptAnchor{
		ChainId:        "lumera",
		TxHash:         append([]byte(nil), sum[:]...),
		AnchoredHeight: 42,
		AnchoredAt:     time.Date(2026, 5, 8, 12, 1, 0, 0, time.UTC),
		Status:         "anchored",
	}
}

func cloneWorkflowReceiptForTest(t *testing.T, receipt *WorkflowInvocationReceipt) *WorkflowInvocationReceipt {
	t.Helper()
	asProto, err := receipt.ToProto()
	require.NoError(t, err)
	clone, err := WorkflowInvocationReceiptFromProto(cloneWorkflowReceiptProto(t, asProto))
	require.NoError(t, err)
	return clone
}

func cloneWorkflowMerkleProofForTest(proof *WorkflowMerkleProof) *WorkflowMerkleProof {
	if proof == nil {
		return nil
	}
	return &WorkflowMerkleProof{
		StepId:         proof.GetStepId(),
		LeafHash:       cloneWorkflowReceiptBytes(proof.GetLeafHash()),
		Siblings:       cloneWorkflowReceiptMatrix(proof.GetSiblings()),
		SiblingOnRight: append([]bool(nil), proof.GetSiblingOnRight()...),
	}
}

func forceFinalizeWorkflowReceiptForTest(t *testing.T, receipt *WorkflowInvocationReceipt, priv ed448.PrivateKey) {
	t.Helper()
	hashes := make([][]byte, 0, len(receipt.StepReceipts))
	order := make([]string, 0, len(receipt.StepReceipts))
	for i, step := range receipt.StepReceipts {
		canonical, err := canonicalJSON(workflowStepReceiptPayload{
			StepID:        step.StepID,
			ToolID:        step.ToolID,
			ToolVersion:   step.ToolVersion,
			Outcome:       step.Outcome,
			Cost:          step.Cost,
			DurationMS:    step.DurationMS,
			AttemptCount:  step.AttemptCount,
			ErrorCode:     step.ErrorCode,
			ErrorMessage:  step.ErrorMessage,
			FailureAction: step.FailureAction,
			OutputClaims:  cloneWorkflowOutputClaims(step.OutputClaims),
		})
		require.NoError(t, err)
		sum := sha256.Sum256(canonical)
		hash := append([]byte(nil), sum[:]...)
		hashes = append(hashes, hash)
		receipt.StepReceipts[i].ReceiptHash = append([]byte(nil), hash...)
		order = append(order, step.StepID)
	}
	root, err := ComputeWorkflowReceiptMerkleRoot(hashes)
	require.NoError(t, err)
	receipt.StepReceiptHashes = cloneWorkflowReceiptMatrix(hashes)
	receipt.MerkleRoot = root
	receipt.CanonicalStepOrder = order
	receipt.ExecutorPubkey, err = RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)
	receipt.ExecutorSig = nil
	canonical, err := CanonicalWorkflowReceiptBytes(receipt)
	require.NoError(t, err)
	receipt.ExecutorSig = ed448.Sign(priv, canonical, "")
}
