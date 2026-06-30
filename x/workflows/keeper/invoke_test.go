package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/cloudflare/circl/sign/ed448"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

func TestInvoke_HappyPath_Settle(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "5"), Duration: 4 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		TraceID:              "trace-happy",
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, "8", receipt.TotalCost.Amount)
	require.Len(t, receipt.MerkleRoot, 32)
	require.Equal(t, []string{"step-a", "step-b"}, receipt.CanonicalStepOrder)
	require.NoError(t, types.VerifyWorkflowReceiptMerkleRoot(receipt))
	require.Equal(t, 1, ledger.lockCalls)
	require.Equal(t, 1, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
	require.Equal(t, []string{"step-a", "step-b"}, executor.stepIDs())

	record, found, err := k.GetBundleQuote(ctx, quote.BundleID)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, record.Consumed)
	require.NotNil(t, record.Invocation)
	require.Equal(t, receipt.BundleID, record.Invocation.Receipt.BundleID)
}

func TestInvoke_WorkflowReceipt_SignedAndAnchored(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
	})
	_, priv := deterministicQuoteKey()
	ledger := &fakeWorkflowLedger{}
	anchorer := &fakeWorkflowReceiptAnchorer{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "5"), Duration: 4 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		ReceiptSignerKey:     priv,
		ReceiptAnchorer:      anchorer,
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, 1, anchorer.calls)
	require.Len(t, receipt.Anchors, 1)
	require.Equal(t, "lumera", receipt.Anchors[0].GetChainId())
	require.Equal(t, int64(1), receipt.Anchors[0].GetAnchoredHeight())
	require.NoError(t, types.VerifyWorkflowReceipt(receipt, pubkey))
	proof, err := types.BuildWorkflowReceiptProof(receipt, "step-b")
	require.NoError(t, err)
	require.NoError(t, types.VerifyWorkflowStepReveal(receipt.MerkleRoot, receipt.StepReceipts[1], proof))
	anchorLog, err := json.Marshal(receipt.AnchorLogFields(receipt.Anchors[0]))
	require.NoError(t, err)
	t.Logf("workflow_receipt_anchor=%s", anchorLog)
}

func TestInvoke_InvalidReceiptAnchor_RevertsBeforeSettle(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	anchorer := &invalidWorkflowReceiptAnchorer{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		ReceiptAnchorer:      anchorer,
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "workflow_anchor_invalid", receipt.FailureCode)
	require.Contains(t, receipt.FailureReason, "workflow receipt anchor chain_id must be canonical")
	require.Equal(t, 1, anchorer.calls)
	require.Equal(t, 1, ledger.lockCalls)
	require.Equal(t, 0, ledger.settleCalls)
	require.Equal(t, 1, ledger.revertCalls)
	require.Empty(t, receipt.Anchors)
	require.Len(t, receipt.FailureAttributions, 1)
	require.NoError(t, receipt.ValidateBasic())
}

func TestInvoke_InvalidReceiptSignerRejectsBeforeLock(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
	}}

	_, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		ReceiptSignerKey:     ed448.PrivateKey{0x01},
	})

	require.ErrorContains(t, err, "private key must")
	require.Equal(t, 0, ledger.lockCalls)
	require.Equal(t, 0, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
	require.Empty(t, executor.stepIDs())
}

func TestInvokeRejectsNonCanonicalTraceIDBeforeLock(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{}

	_, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		TraceID:              " trace-invoke ",
	})

	require.ErrorContains(t, err, "workflow invoke trace_id must be canonical")
	require.Equal(t, 0, ledger.lockCalls)
	require.Equal(t, 0, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
	require.Empty(t, executor.stepIDs())
}

func TestInvoke_SingleStepFail_Revert(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "publisher_down", ErrorMessage: "publisher unavailable"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "publisher_down", receipt.FailureCode)
	require.Equal(t, 1, ledger.lockCalls)
	require.Equal(t, 0, ledger.settleCalls)
	require.Equal(t, 1, ledger.revertCalls)
	require.Len(t, receipt.FailureAttributions, 1)
	assertWorkflowFailureAttribution(t, receipt.FailureAttributions[0], "step-a", "publisher_down", map[string]any{
		"bundle_id":       receipt.BundleID,
		"workflow_id":     receipt.WorkflowID,
		"outcome":         types.WorkflowOutcomeReverted,
		"failure_code":    "publisher_down",
		"step.step_id":    "step-a",
		"step.outcome":    types.WorkflowStepOutcomeFailed,
		"step.error_code": "publisher_down",
	})
	require.NoError(t, types.VerifyWorkflowReceiptMerkleRoot(receipt))
}

func TestInvoke_InvalidStepOutcomeRevertsLockedBundle(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: "unexpected", Cost: quoteCoin("ulac", "1"), Duration: time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "workflow_step_outcome_invalid", receipt.FailureCode)
	require.Contains(t, receipt.FailureReason, `invalid workflow step outcome: "unexpected"`)
	require.Equal(t, types.WorkflowStepOutcomeFailed, receipt.StepReceipts[0].Outcome)
	require.Equal(t, "workflow_step_outcome_invalid", receipt.StepReceipts[0].ErrorCode)
	require.Equal(t, 1, ledger.lockCalls)
	require.Equal(t, 0, ledger.settleCalls)
	require.Equal(t, 1, ledger.revertCalls)
	require.NoError(t, receipt.ValidateBasic())
	require.NoError(t, types.VerifyWorkflowReceiptMerkleRoot(receipt))
}

func TestInvokeStepRejectsNonCanonicalErrorFields(t *testing.T) {
	tests := []struct {
		name   string
		result WorkflowStepResult
	}{
		{
			name: "padded error code",
			result: WorkflowStepResult{
				Outcome:      types.WorkflowStepOutcomeFailed,
				ErrorCode:    " publisher_down ",
				ErrorMessage: "publisher unavailable",
			},
		},
		{
			name: "padded error message",
			result: WorkflowStepResult{
				Outcome:      types.WorkflowStepOutcomeFailed,
				ErrorCode:    "publisher_down",
				ErrorMessage: " publisher unavailable ",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
				invokeStep("step-a", 10, nil),
			})
			ledger := &fakeWorkflowLedger{}
			executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
				"step-a": {tc.result},
			}}

			receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

			require.NoError(t, err)
			require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
			require.Equal(t, "step_result_noncanonical", receipt.FailureCode)
			require.Len(t, receipt.StepReceipts, 1)
			require.Equal(t, "step_result_noncanonical", receipt.StepReceipts[0].ErrorCode)
			require.Contains(t, receipt.StepReceipts[0].ErrorMessage, "must be canonical")
			require.Equal(t, 1, ledger.lockCalls)
			require.Equal(t, 0, ledger.settleCalls)
			require.Equal(t, 1, ledger.revertCalls)
		})
	}
}

func TestInvoke_RevertedBundleChargesWastedWorkFeeFromExecutedCost(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
	})
	params := types.DefaultParams()
	params.WastedWorkBPS = 2500
	require.NoError(t, k.SetParams(ctx, params))

	ledger := &fakeWorkflowLedger{}
	feeSink := &fakeWastedWorkFeeSink{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "11"), Duration: 3 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "publisher_down", ErrorMessage: "publisher unavailable"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		WastedWorkFeeSink:    feeSink,
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "11", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.revertCalls)
	require.Equal(t, 1, feeSink.calls)
	require.Equal(t, quote.BundleID, feeSink.quotes[0].BundleID)
	require.Equal(t, receipt.BundleID, feeSink.receipts[0].BundleID)
	require.Equal(t, types.QuoteCoin{Denom: "ulac", Amount: "3"}, feeSink.fees[0])
}

func TestInvoke_RevertedBundleWastedWorkFeeFailureRecordsMetric(t *testing.T) {
	reader := withWorkflowMetricReader(t)
	ctx, k, quote, pubkey := invokeFixtureWithStoredCard(t, invokeWorkflowCard([]*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
	}))
	params := types.DefaultParams()
	params.WastedWorkBPS = 2500
	require.NoError(t, k.SetParams(ctx, params))

	ledger := &fakeWorkflowLedger{}
	feeSink := &fakeWastedWorkFeeSink{err: errors.New("fee sink unavailable")}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "11"), Duration: 3 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "publisher_down", ErrorMessage: "publisher unavailable"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		WastedWorkFeeSink:    feeSink,
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, 1, ledger.revertCalls)
	require.Equal(t, 1, feeSink.calls)

	metrics := collectWorkflowMetrics(t, reader)
	labels := map[string]string{"workflow_id": quote.WorkflowID, "version": quote.Version}
	feeFailures := findWorkflowInt64Point(t, metrics, metricWorkflowWastedWorkFeeFailures, labels)
	require.Equal(t, int64(1), feeFailures.Value)
	require.Equal(t, labels, workflowMetricLabelMap(feeFailures.Attributes))
}

func TestInvoke_RevertedBundleKeeperSinkDebitsAuthorBond(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
	})
	params := types.DefaultParams()
	params.WastedWorkBPS = 2500
	require.NoError(t, k.SetParams(ctx, params))
	bond, found, err := k.GetAuthorBond(ctx, workflowTestAuthorAddress())
	require.NoError(t, err)
	require.True(t, found)
	bond.Bond.Amount = sdkmath.NewInt(1000010)
	require.NoError(t, k.PutAuthorBond(ctx, bond))

	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "11"), Duration: 3 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "publisher_down", ErrorMessage: "publisher unavailable"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		WastedWorkFeeSink:    k,
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "11", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.revertCalls)

	bond, found, err = k.GetAuthorBond(ctx, workflowTestAuthorAddress())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "1000007", bond.Bond.Amount.String())
	require.Equal(t, "3", bond.Slashed.Amount.String())
	require.Equal(t, []string{"wf-invoke/1.0.0"}, bond.LockedFor)
}

func TestInvoke_RevertedBundleKeeperSinkCapsAtAuthorBondReserve(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
	})
	params := types.DefaultParams()
	params.WastedWorkBPS = 2500
	require.NoError(t, k.SetParams(ctx, params))
	bond, found, err := k.GetAuthorBond(ctx, workflowTestAuthorAddress())
	require.NoError(t, err)
	require.True(t, found)
	bond.Bond.Amount = sdkmath.NewInt(1000002)
	require.NoError(t, k.PutAuthorBond(ctx, bond))

	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "11"), Duration: 3 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "publisher_down", ErrorMessage: "publisher unavailable"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		WastedWorkFeeSink:    k,
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "11", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.revertCalls)

	bond, found, err = k.GetAuthorBond(ctx, workflowTestAuthorAddress())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "1000000", bond.Bond.Amount.String())
	require.Equal(t, "2", bond.Slashed.Amount.String())
	require.Equal(t, []string{"wf-invoke/1.0.0"}, bond.LockedFor)
}

func TestInvoke_RevertedBundleSkipsWastedWorkFeeWhenNoWorkExecuted(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	feeSink := &fakeWastedWorkFeeSink{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "publisher_down", ErrorMessage: "publisher unavailable"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		WastedWorkFeeSink:    feeSink,
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "0", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.revertCalls)
	require.Equal(t, 0, feeSink.calls)
}

func TestInvoke_FinalizedBundleDoesNotChargeWastedWorkFee(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	feeSink := &fakeWastedWorkFeeSink{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "11"), Duration: 3 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		WastedWorkFeeSink:    feeSink,
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, 1, ledger.settleCalls)
	require.Equal(t, 0, feeSink.calls)
}

func TestInvoke_CascadeFailure_Revert(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 10, []string{"step-a"}),
		invokeStep("step-c", 10, []string{"step-b"}),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "bad_step", ErrorMessage: "bad step"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, []string{"step-a", "step-b"}, executor.stepIDs())
	require.Equal(t, 1, ledger.revertCalls)
}

func TestInvoke_SubSLO_Timeout_Revert(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: 11 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "step_slo_exceeded", receipt.FailureCode)
	require.Equal(t, 1, ledger.revertCalls)
}

func TestInvoke_BundleSLO_Timeout_Revert(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 10, []string{"step-a"}),
	})
	quote = forceQuoteTotalSLO(t, ctx, k, quote, 10)
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: 8 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: 7 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "bundle_slo_exceeded", receipt.FailureCode)
	require.Equal(t, "2", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.revertCalls)
}

func TestInvoke_StaticInvariantBreach_RejectAtLock(t *testing.T) {
	step := invokeStep("step-a", 10, nil)
	card := invokeWorkflowCard([]*types.Step{step})
	card.SafetyInvariants = []*types.SafetyInvariant{
		validInvokeInvariant("cost_cap", "total_cost <= 10", types.InvariantPhase_INVARIANT_PHASE_LOCK),
	}
	ctx, k, quote, pubkey := invokeFixtureWithCard(t, card)
	ledger := &fakeWorkflowLedger{}

	_, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: &scriptedWorkflowExecutor{}})

	require.ErrorContains(t, err, "workflow invariant")
	require.Equal(t, 0, ledger.lockCalls)
}

func TestInvoke_RuntimeInvariantBreach_Revert(t *testing.T) {
	stepA := invokeStep("step-a", 10, nil)
	stepA.FailureAction = types.FailureAction_FAILURE_ACTION_SKIP_DOWNSTREAM
	card := invokeWorkflowCard([]*types.Step{stepA})
	card.SafetyInvariants = []*types.SafetyInvariant{
		validInvokeInvariant("all_success", "all(step.outcome == 'success' for step in steps)", types.InvariantPhase_INVARIANT_PHASE_VERIFY),
	}
	ctx, k, quote, pubkey := invokeFixtureWithCard(t, card)
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "soft_fail", ErrorMessage: "soft fail"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "workflow_invariant_violation", receipt.FailureCode)
	require.Equal(t, 1, ledger.revertCalls)
}

func TestInvoke_IdempotentDoubleInvoke(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: time.Millisecond}},
	}}

	first, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})
	require.NoError(t, err)
	second, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})
	require.NoError(t, err)

	require.Equal(t, first.BundleID, second.BundleID)
	require.Equal(t, first.Outcome, second.Outcome)
	require.Equal(t, 1, ledger.lockCalls)
	require.Equal(t, 1, ledger.settleCalls)
}

func TestInvoke_IdempotentReplayRejectsNonCanonicalQuote(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: time.Millisecond}},
	}}

	_, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})
	require.NoError(t, err)

	replayQuote := *quote
	replayQuote.BundleID = " " + replayQuote.BundleID + " "
	_, err = k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: &replayQuote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})
	require.ErrorContains(t, err, "bundle quote bundle_id must be canonical")
	require.Equal(t, 1, ledger.lockCalls)
	require.Equal(t, 1, ledger.settleCalls)
}

func TestInvoke_PartialSkip_ProratedSettle(t *testing.T) {
	stepA := invokeStep("step-a", 10, nil)
	stepA.FailureAction = types.FailureAction_FAILURE_ACTION_SKIP_DOWNSTREAM
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		stepA,
		invokeStep("step-b", 10, []string{"step-a"}),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "skip_branch", ErrorMessage: "skip branch"}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomePartialSkip, receipt.Outcome)
	require.Equal(t, "skip_branch", receipt.FailureCode)
	require.Equal(t, "skip branch", receipt.FailureReason)
	require.Equal(t, "0", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
	require.Len(t, receipt.StepReceipts, 2)
	require.Equal(t, types.WorkflowStepOutcomeSkipped, receipt.StepReceipts[1].Outcome)
	require.Len(t, receipt.FailureAttributions, 2)
	assertWorkflowFailureAttribution(t, receipt.FailureAttributions[0], "step-a", "skip_branch", map[string]any{
		"bundle_id":       receipt.BundleID,
		"workflow_id":     receipt.WorkflowID,
		"outcome":         types.WorkflowOutcomePartialSkip,
		"step.step_id":    "step-a",
		"step.outcome":    types.WorkflowStepOutcomeFailed,
		"step.error_code": "skip_branch",
	})
	assertWorkflowFailureAttribution(t, receipt.FailureAttributions[1], "step-b", "dependency_skipped", map[string]any{
		"bundle_id":       receipt.BundleID,
		"workflow_id":     receipt.WorkflowID,
		"outcome":         types.WorkflowOutcomePartialSkip,
		"step.step_id":    "step-b",
		"step.outcome":    types.WorkflowStepOutcomeSkipped,
		"step.error_code": "dependency_skipped",
	})
}

func TestInvoke_FallbackStep_RecoveryFinalizes(t *testing.T) {
	primary := invokeStep("primary_search", 10, nil)
	primary.FailureAction = types.FailureAction_FAILURE_ACTION_FALLBACK_STEP
	primary.FallbackStepId = "archive_search"
	archive := invokeStep("archive_search", 20, nil)
	redact := invokeStep("redact_result", 20, []string{"primary_search", "archive_search"})
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		archive,
		redact,
		primary,
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"primary_search": {{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "primary_timeout", ErrorMessage: "primary timed out"}},
		"archive_search": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "4"), Duration: 4 * time.Millisecond}},
		"redact_result":  {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, "7", receipt.TotalCost.Amount)
	require.Equal(t, []string{"primary_search", "archive_search", "redact_result"}, executor.stepIDs())
	require.Equal(t, 1, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
	require.Empty(t, receipt.FailureAttributions)
	require.Len(t, receipt.StepReceipts, 3)
	require.Equal(t, types.WorkflowStepOutcomeFailed, receipt.StepReceipts[0].Outcome)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[1].Outcome)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[2].Outcome)
	require.NoError(t, types.VerifyWorkflowReceiptMerkleRoot(receipt))
}

func TestInvoke_FallbackStep_SkipsWhenPrimarySucceeds(t *testing.T) {
	primary := invokeStep("primary_search", 10, nil)
	primary.FailureAction = types.FailureAction_FAILURE_ACTION_FALLBACK_STEP
	primary.FallbackStepId = "archive_search"
	archive := invokeStep("archive_search", 20, nil)
	redact := invokeStep("redact_result", 20, []string{"primary_search", "archive_search"})
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		archive,
		redact,
		primary,
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"primary_search": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "2"), Duration: 2 * time.Millisecond}},
		"redact_result":  {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, "5", receipt.TotalCost.Amount)
	require.Equal(t, []string{"primary_search", "redact_result"}, executor.stepIDs())
	require.Equal(t, 1, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
	require.Len(t, receipt.StepReceipts, 3)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[0].Outcome)
	require.Equal(t, types.WorkflowStepOutcomeSkipped, receipt.StepReceipts[1].Outcome)
	require.Equal(t, workflowFallbackNotTriggeredReason, receipt.StepReceipts[1].ErrorCode)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[2].Outcome)
}

func TestInvoke_ConditionOutcomeSkipsUnmatchedBranch(t *testing.T) {
	primary := invokeStep("primary_search", 10, nil)
	archive := invokeStep("archive_search", 20, nil)
	archive.Condition = "steps.primary_search.outcome == 'failed'"
	redact := invokeStep("redact_result", 20, []string{"primary_search", "archive_search"})
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		archive,
		redact,
		primary,
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"primary_search": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "2"), Duration: 2 * time.Millisecond}},
		"redact_result":  {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, "5", receipt.TotalCost.Amount)
	require.Equal(t, []string{"primary_search", "redact_result"}, executor.stepIDs())
	require.Equal(t, 1, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
	require.Len(t, receipt.StepReceipts, 3)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[0].Outcome)
	require.Equal(t, types.WorkflowStepOutcomeSkipped, receipt.StepReceipts[1].Outcome)
	require.Equal(t, workflowConditionNotMetReason, receipt.StepReceipts[1].ErrorCode)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[2].Outcome)
}

func TestInvoke_OutputClaimConditionSkipsWithoutReceiptEvidence(t *testing.T) {
	stepA := invokeStep("step-a", 10, nil)
	stepB := invokeStep("step-b", 10, nil)
	stepB.Condition = "steps.step-a.output.policy_allowed == true"
	ctx, k, quote, pubkey := invokeFixtureWithStoredCard(t, invokeWorkflowCard([]*types.Step{stepA, stepB}))
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "2"), Duration: 2 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, []string{"step-a"}, executor.stepIDs())
	require.Equal(t, "2", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
	require.Len(t, receipt.StepReceipts, 2)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[0].Outcome)
	require.Equal(t, types.WorkflowStepOutcomeSkipped, receipt.StepReceipts[1].Outcome)
	require.Equal(t, workflowConditionNotMetReason, receipt.StepReceipts[1].ErrorCode)
}

func TestInvoke_OutputClaimConditionUsesReceiptEvidence(t *testing.T) {
	stepA := invokeStep("step-a", 10, nil)
	stepB := invokeStep("step-b", 10, nil)
	stepB.Condition = "steps.step-a.output.policy_allowed == true"
	condition, err := types.ParseWorkflowCondition(stepB.Condition)
	require.NoError(t, err)
	claim, err := types.NewWorkflowOutputClaimCommitment("step-a", condition.ClaimPath, condition.Literal)
	require.NoError(t, err)
	ctx, k, quote, pubkey := invokeFixtureWithStoredCard(t, invokeWorkflowCard([]*types.Step{stepA, stepB}))
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{
			Outcome:      types.WorkflowStepOutcomeSuccess,
			Cost:         quoteCoin("ulac", "2"),
			Duration:     2 * time.Millisecond,
			OutputClaims: []types.WorkflowOutputClaim{claim},
		}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, []string{"step-a", "step-b"}, executor.stepIDs())
	require.Equal(t, "5", receipt.TotalCost.Amount)
	require.Len(t, receipt.StepReceipts, 2)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[0].Outcome)
	require.Equal(t, types.WorkflowStepOutcomeSuccess, receipt.StepReceipts[1].Outcome)
	require.Len(t, receipt.StepReceipts[0].OutputClaims, 1)
	require.True(t, receipt.StepReceipts[0].OutputClaims[0].Redacted)
	matches, err := types.EvaluateWorkflowOutputClaimCondition(receipt.StepReceipts[0], condition)
	require.NoError(t, err)
	require.True(t, matches)
}

func TestInvoke_MalformedConditionEvaluatorFailsClosed(t *testing.T) {
	step := workflowExecutionStep{
		id:   "step-b",
		step: invokeStep("step-b", 10, nil),
	}
	step.step.Condition = "steps.step-a.output.policy_allowed == null"
	executed := map[string]types.WorkflowStepInvocation{
		"step-a": {Outcome: types.WorkflowStepOutcomeSuccess},
	}

	met, err := workflowStepConditionMet(step, executed)

	require.NoError(t, err)
	require.False(t, met)
}

func TestInvoke_StaticFalseConditionSkipsStep(t *testing.T) {
	step := invokeStep("step-a", 10, nil)
	step.Condition = "never"
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{step})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Empty(t, executor.stepIDs())
	require.Equal(t, "0", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.settleCalls)
	require.Len(t, receipt.StepReceipts, 1)
	require.Equal(t, types.WorkflowStepOutcomeSkipped, receipt.StepReceipts[0].Outcome)
	require.Equal(t, workflowConditionNotMetReason, receipt.StepReceipts[0].ErrorCode)
}

func TestInvoke_EmitsWorkflowStepSpans(t *testing.T) {
	recorder := withWorkflowStepTracer(t)
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "5"), Duration: 4 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		TraceID:              "trace-workflow-otel",
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	spans := workflowStepSpans(recorder.Ended())
	require.Len(t, spans, 2)

	stepA := findWorkflowStepSpan(t, spans, "step-a")
	require.Equal(t, "trace-workflow-otel", spanStringAttr(t, stepA, "trace_id"))
	require.Equal(t, quote.BundleID, spanStringAttr(t, stepA, "bundle_id"))
	require.Equal(t, quote.WorkflowID, spanStringAttr(t, stepA, "workflow_id"))
	require.Equal(t, quote.Version, spanStringAttr(t, stepA, "version"))
	require.Equal(t, "tool.step-a", spanStringAttr(t, stepA, "tool_id"))
	require.Equal(t, "success", spanStringAttr(t, stepA, "outcome"))
	require.Equal(t, "ulac", spanStringAttr(t, stepA, "cost_denom"))
	require.Equal(t, "3", spanStringAttr(t, stepA, "cost_amount"))
	require.Equal(t, int64(1), spanIntAttr(t, stepA, "attempt"))
	require.Equal(t, int64(3), spanIntAttr(t, stepA, "duration_ms"))
}

func TestInvoke_StepSpanRecordsFailureAttribution(t *testing.T) {
	recorder := withWorkflowStepTracer(t)
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{
			Outcome:      types.WorkflowStepOutcomeFailed,
			ErrorCode:    "publisher_down",
			ErrorMessage: "publisher unavailable",
		}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		TraceID:              "trace-workflow-fail",
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "publisher_down", receipt.FailureCode)
	spans := workflowStepSpans(recorder.Ended())
	require.Len(t, spans, 1)
	stepA := findWorkflowStepSpan(t, spans, "step-a")
	require.Equal(t, "trace-workflow-fail", spanStringAttr(t, stepA, "trace_id"))
	require.Equal(t, "failed", spanStringAttr(t, stepA, "outcome"))
	require.Equal(t, "publisher_down", spanStringAttr(t, stepA, "error_code"))
	require.Equal(t, codes.Error, stepA.Status().Code)
}

func TestInvoke_OTLPCollectorReconstructsWorkflowDAG(t *testing.T) {
	collector, flush := withWorkflowOTLPCollector(t)
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 20, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
		invokeStep("step-c", 20, []string{"step-a", "step-b"}),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "2"), Duration: 2 * time.Millisecond}},
		"step-c": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		TraceID:              "trace-workflow-otlp-collector",
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	flush()

	spans := collector.workflowStepSpans()
	require.Len(t, spans, 3)
	require.Equal(t, []string{"step-a", "step-b", "step-c"}, otlpStepIDs(spans))

	stepA := findOTLPWorkflowStepSpan(t, spans, "step-a")
	require.Equal(t, "trace-workflow-otlp-collector", otlpSpanStringAttr(t, stepA, "trace_id"))
	require.Equal(t, quote.BundleID, otlpSpanStringAttr(t, stepA, "bundle_id"))
	require.Equal(t, quote.WorkflowID, otlpSpanStringAttr(t, stepA, "workflow_id"))
	require.Equal(t, quote.Version, otlpSpanStringAttr(t, stepA, "version"))
	require.Empty(t, otlpSpanStringSliceAttr(t, stepA, "depends_on"))

	stepB := findOTLPWorkflowStepSpan(t, spans, "step-b")
	require.Equal(t, []string{"step-a"}, otlpSpanStringSliceAttr(t, stepB, "depends_on"))

	stepC := findOTLPWorkflowStepSpan(t, spans, "step-c")
	require.Equal(t, []string{"step-a", "step-b"}, otlpSpanStringSliceAttr(t, stepC, "depends_on"))
	require.Equal(t, "success", otlpSpanStringAttr(t, stepC, "outcome"))
}

func TestInvoke_RecordsWorkflowMetrics(t *testing.T) {
	reader := withWorkflowMetricReader(t)
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 20, []string{"step-a"}),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "5"), Duration: 4 * time.Millisecond}},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		TraceID:              "trace-workflow-metrics",
	})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	metrics := collectWorkflowMetrics(t, reader)
	baseLabels := map[string]string{"workflow_id": quote.WorkflowID, "version": quote.Version}
	outcomeLabels := map[string]string{"workflow_id": quote.WorkflowID, "version": quote.Version, "outcome": types.WorkflowOutcomeFinalized}
	stepOutcomeLabels := map[string]string{"workflow_id": quote.WorkflowID, "version": quote.Version, "outcome": types.WorkflowStepOutcomeSuccess}

	invoked := findWorkflowInt64Point(t, metrics, metricWorkflowsInvokedTotalName, baseLabels)
	require.Equal(t, int64(1), invoked.Value)
	require.Equal(t, baseLabels, workflowMetricLabelMap(invoked.Attributes))

	outcome := findWorkflowInt64Point(t, metrics, metricWorkflowOutcomeTotalName, outcomeLabels)
	require.Equal(t, int64(1), outcome.Value)
	require.Equal(t, outcomeLabels, workflowMetricLabelMap(outcome.Attributes))

	bundleDuration := findWorkflowHistogramPoint(t, metrics, metricWorkflowBundleDurationName, outcomeLabels)
	require.Equal(t, uint64(1), bundleDuration.Count)
	require.GreaterOrEqual(t, bundleDuration.Sum, 0.0)
	require.Equal(t, outcomeLabels, workflowMetricLabelMap(bundleDuration.Attributes))

	stepDuration := findWorkflowHistogramPoint(t, metrics, metricWorkflowStepDurationSecondsName, stepOutcomeLabels)
	require.Equal(t, uint64(2), stepDuration.Count)
	require.InEpsilon(t, 0.007, stepDuration.Sum, 0.000001)
	require.Equal(t, stepOutcomeLabels, workflowMetricLabelMap(stepDuration.Attributes))

	bundleCost := findWorkflowHistogramPoint(t, metrics, metricWorkflowBundleCostName, outcomeLabels)
	require.Equal(t, uint64(1), bundleCost.Count)
	require.Equal(t, 8.0, bundleCost.Sum)
	require.Equal(t, outcomeLabels, workflowMetricLabelMap(bundleCost.Attributes))
}

func TestInvoke_ObservabilityE2E_MixedOutcomes(t *testing.T) {
	recorder := withWorkflowStepTracer(t)
	reader := withWorkflowMetricReader(t)

	const bundleCount = 100
	type observedBundle struct {
		receipt           *types.WorkflowInvocationReceipt
		traceID           string
		executedStepIDs   []string
		stepOutcomeCounts map[string]uint64
	}

	observed := make([]observedBundle, 0, bundleCount)
	expectedSpanCount := 0
	failureAttributionCount := 0
	outcomeCounts := map[string]int{
		types.WorkflowOutcomeFinalized:   0,
		types.WorkflowOutcomeReverted:    0,
		types.WorkflowOutcomePartialSkip: 0,
	}

	for i := 0; i < bundleCount; i++ {
		workflowID := fmt.Sprintf("wf-observability-%03d", i)
		version := fmt.Sprintf("1.%d.0", i%5)
		traceID := fmt.Sprintf("trace-observability-%03d", i)
		card, executor, expectedOutcome := observabilityE2ECase(t, i, workflowID, version)
		ctx, k, quote, pubkey := invokeFixtureWithCard(t, card)
		ledger := &fakeWorkflowLedger{}

		receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{
			Quote:                quote,
			ExpectedRouterPubkey: pubkey,
			Ledger:               ledger,
			Executor:             executor,
			TraceID:              traceID,
		})

		require.NoError(t, err)
		require.Equal(t, expectedOutcome, receipt.Outcome)
		require.Equal(t, workflowID, receipt.WorkflowID)
		require.Equal(t, version, receipt.Version)
		require.Equal(t, traceID, receipt.TraceID)
		require.Equal(t, 1, ledger.lockCalls)
		if expectedOutcome == types.WorkflowOutcomeReverted {
			require.Equal(t, 1, ledger.revertCalls)
			require.Equal(t, 0, ledger.settleCalls)
		} else {
			require.Equal(t, 1, ledger.settleCalls)
			require.Equal(t, 0, ledger.revertCalls)
		}
		if expectedOutcome == types.WorkflowOutcomeFinalized {
			require.Empty(t, receipt.FailureAttributions)
		} else {
			require.NotEmpty(t, receipt.FailureAttributions)
			failureAttributionCount += len(receipt.FailureAttributions)
		}

		executedStepIDs := executor.stepIDs()
		expectedSpanCount += len(executedStepIDs)
		outcomeCounts[receipt.Outcome]++
		observed = append(observed, observedBundle{
			receipt:           receipt,
			traceID:           traceID,
			executedStepIDs:   executedStepIDs,
			stepOutcomeCounts: workflowStepOutcomeCounts(receipt.StepReceipts),
		})
	}

	require.Equal(t, map[string]int{
		types.WorkflowOutcomeFinalized:   34,
		types.WorkflowOutcomeReverted:    33,
		types.WorkflowOutcomePartialSkip: 33,
	}, outcomeCounts)
	require.GreaterOrEqual(t, failureAttributionCount, 66)

	spans := workflowStepSpans(recorder.Ended())
	require.Len(t, spans, expectedSpanCount)
	for _, bundle := range observed {
		for _, stepID := range bundle.executedStepIDs {
			span := findWorkflowStepSpan(t, spans, stepID)
			require.Equal(t, bundle.traceID, spanStringAttr(t, span, "trace_id"))
			require.Equal(t, bundle.receipt.BundleID, spanStringAttr(t, span, "bundle_id"))
			require.Equal(t, bundle.receipt.WorkflowID, spanStringAttr(t, span, "workflow_id"))
			require.Equal(t, bundle.receipt.Version, spanStringAttr(t, span, "version"))
		}
	}

	metrics := collectWorkflowMetrics(t, reader)
	for _, bundle := range observed {
		receipt := bundle.receipt
		baseLabels := map[string]string{"workflow_id": receipt.WorkflowID, "version": receipt.Version}
		outcomeLabels := map[string]string{"workflow_id": receipt.WorkflowID, "version": receipt.Version, "outcome": receipt.Outcome}

		require.Equal(t, int64(1), findWorkflowInt64Point(t, metrics, metricWorkflowsInvokedTotalName, baseLabels).Value)
		require.Equal(t, int64(1), findWorkflowInt64Point(t, metrics, metricWorkflowOutcomeTotalName, outcomeLabels).Value)
		require.Equal(t, uint64(1), findWorkflowHistogramPoint(t, metrics, metricWorkflowBundleDurationName, outcomeLabels).Count)
		require.Equal(t, uint64(1), findWorkflowHistogramPoint(t, metrics, metricWorkflowBundleCostName, outcomeLabels).Count)
		for stepOutcome, count := range bundle.stepOutcomeCounts {
			stepLabels := map[string]string{"workflow_id": receipt.WorkflowID, "version": receipt.Version, "outcome": stepOutcome}
			require.Equal(t, count, findWorkflowHistogramPoint(t, metrics, metricWorkflowStepDurationSecondsName, stepLabels).Count)
		}
	}
}

func TestInvoke_WorkflowMetricsExcludeHighCardinalityLabels(t *testing.T) {
	reader := withWorkflowMetricReader(t)
	receipt := &types.WorkflowInvocationReceipt{
		BundleID:   "bundle-do-not-label",
		WorkflowID: "workflow-cardinality-proof",
		Version:    "1.0.0",
		Outcome:    types.WorkflowOutcomeReverted,
		TotalCost:  quoteCoin("ulac", "17"),
		TraceID:    "trace-do-not-label",
		StepReceipts: []types.WorkflowStepInvocation{
			{
				StepID:     "step-do-not-label",
				Outcome:    types.WorkflowStepOutcomeFailed,
				Cost:       quoteCoin("ulac", "17"),
				DurationMS: 25,
				ErrorCode:  "publisher_down",
			},
		},
	}

	recordWorkflowInvocationMetrics(context.Background(), receipt, 250*time.Millisecond)
	recordWorkflowWastedWorkFeeChargeFailure(context.Background(), receipt)
	metrics := collectWorkflowMetrics(t, reader)
	baseLabels := map[string]string{"workflow_id": receipt.WorkflowID, "version": receipt.Version}
	outcomeLabels := map[string]string{"workflow_id": receipt.WorkflowID, "version": receipt.Version, "outcome": receipt.Outcome}
	stepOutcomeLabels := map[string]string{"workflow_id": receipt.WorkflowID, "version": receipt.Version, "outcome": types.WorkflowStepOutcomeFailed}

	requireWorkflowMetricLabelKeys(t, workflowMetricLabelMap(findWorkflowInt64Point(t, metrics, metricWorkflowsInvokedTotalName, baseLabels).Attributes), "workflow_id", "version")
	requireWorkflowMetricLabelKeys(t, workflowMetricLabelMap(findWorkflowInt64Point(t, metrics, metricWorkflowOutcomeTotalName, outcomeLabels).Attributes), "workflow_id", "version", "outcome")
	requireWorkflowMetricLabelKeys(t, workflowMetricLabelMap(findWorkflowHistogramPoint(t, metrics, metricWorkflowBundleDurationName, outcomeLabels).Attributes), "workflow_id", "version", "outcome")
	requireWorkflowMetricLabelKeys(t, workflowMetricLabelMap(findWorkflowHistogramPoint(t, metrics, metricWorkflowBundleCostName, outcomeLabels).Attributes), "workflow_id", "version", "outcome")
	requireWorkflowMetricLabelKeys(t, workflowMetricLabelMap(findWorkflowHistogramPoint(t, metrics, metricWorkflowStepDurationSecondsName, stepOutcomeLabels).Attributes), "workflow_id", "version", "outcome")
	requireWorkflowMetricLabelKeys(t, workflowMetricLabelMap(findWorkflowInt64Point(t, metrics, metricWorkflowWastedWorkFeeFailures, baseLabels).Attributes), "workflow_id", "version")
}

func FuzzWorkflow_MetricCardinality(f *testing.F) {
	for _, seed := range []struct {
		workflowID string
		version    string
		outcome    string
	}{
		{"workflow-a", "1.0.0", types.WorkflowOutcomeFinalized},
		{" workflow-with-spaces ", " 2.0.0 ", types.WorkflowOutcomeReverted},
		{"trace_id=trace-should-not-become-a-key", "bundle_id=bundle-should-not-become-a-key", "step_id=step-should-not-become-a-key"},
	} {
		f.Add(seed.workflowID, seed.version, seed.outcome)
	}

	f.Fuzz(func(t *testing.T, workflowID string, version string, outcome string) {
		baseAttrs := workflowMetricAttributes(workflowID, version)
		baseLabels := workflowMetricLabelMap(attribute.NewSet(baseAttrs...))
		requireWorkflowMetricLabelKeys(t, baseLabels, "workflow_id", "version")
		require.Equal(t, workflowMetricLabel(workflowID), baseLabels["workflow_id"])
		require.Equal(t, workflowMetricLabel(version), baseLabels["version"])

		outcomeAttrs := appendWorkflowMetricOutcome(baseAttrs, outcome)
		outcomeLabels := workflowMetricLabelMap(attribute.NewSet(outcomeAttrs...))
		requireWorkflowMetricLabelKeys(t, outcomeLabels, "workflow_id", "version", "outcome")
		require.Equal(t, workflowMetricLabel(outcome), outcomeLabels["outcome"])
	})
}

func TestInvoke_Metamorphic_IdenticalOutcomeOnTwoKeepers(t *testing.T) {
	steps := []*types.Step{
		invokeStep("step-a", 10, nil),
		invokeStep("step-b", 10, []string{"step-a"}),
	}
	ctxA, keeperA, quoteA, pubkeyA := invokeFixture(t, steps)
	ctxB, keeperB, quoteB, pubkeyB := invokeFixture(t, steps)
	results := map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "2"), Duration: 2 * time.Millisecond}},
		"step-b": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}},
	}

	receiptA, err := keeperA.InvokeWorkflow(ctxA, InvokeWorkflowRequest{
		Quote:                quoteA,
		ExpectedRouterPubkey: pubkeyA,
		Ledger:               &fakeWorkflowLedger{},
		Executor:             &scriptedWorkflowExecutor{results: cloneWorkflowStepResults(results)},
		TraceID:              "trace-metamorphic",
	})
	require.NoError(t, err)
	receiptB, err := keeperB.InvokeWorkflow(ctxB, InvokeWorkflowRequest{
		Quote:                quoteB,
		ExpectedRouterPubkey: pubkeyB,
		Ledger:               &fakeWorkflowLedger{},
		Executor:             &scriptedWorkflowExecutor{results: cloneWorkflowStepResults(results)},
		TraceID:              "trace-metamorphic",
	})
	require.NoError(t, err)

	require.Equal(t, receiptA.Outcome, receiptB.Outcome)
	require.Equal(t, receiptA.TotalCost, receiptB.TotalCost)
	require.Equal(t, receiptA.StepReceipts, receiptB.StepReceipts)
}

func observabilityE2ECase(t *testing.T, index int, workflowID string, version string) (*types.WorkflowCard, *scriptedWorkflowExecutor, string) {
	t.Helper()

	results := map[string][]WorkflowStepResult{}
	var steps []*types.Step
	var expectedOutcome string

	switch index % 3 {
	case 0:
		stepA := fmt.Sprintf("step-finalize-a-%03d", index)
		stepB := fmt.Sprintf("step-finalize-b-%03d", index)
		steps = []*types.Step{
			invokeStep(stepA, 20, nil),
			invokeStep(stepB, 20, []string{stepA}),
		}
		results[stepA] = []WorkflowStepResult{{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: 3 * time.Millisecond}}
		results[stepB] = []WorkflowStepResult{{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "5"), Duration: 4 * time.Millisecond}}
		expectedOutcome = types.WorkflowOutcomeFinalized
	case 1:
		stepID := fmt.Sprintf("step-revert-%03d", index)
		steps = []*types.Step{invokeStep(stepID, 20, nil)}
		results[stepID] = []WorkflowStepResult{{
			Outcome:      types.WorkflowStepOutcomeFailed,
			Cost:         quoteCoin("ulac", "0"),
			Duration:     5 * time.Millisecond,
			ErrorCode:    "publisher_down",
			ErrorMessage: "publisher unavailable",
		}}
		expectedOutcome = types.WorkflowOutcomeReverted
	default:
		stepA := fmt.Sprintf("step-partial-a-%03d", index)
		stepB := fmt.Sprintf("step-partial-b-%03d", index)
		first := invokeStep(stepA, 20, nil)
		first.FailureAction = types.FailureAction_FAILURE_ACTION_SKIP_DOWNSTREAM
		steps = []*types.Step{
			first,
			invokeStep(stepB, 20, []string{stepA}),
		}
		results[stepA] = []WorkflowStepResult{{
			Outcome:      types.WorkflowStepOutcomeFailed,
			Cost:         quoteCoin("ulac", "0"),
			Duration:     6 * time.Millisecond,
			ErrorCode:    "skip_branch",
			ErrorMessage: "skip branch",
		}}
		expectedOutcome = types.WorkflowOutcomePartialSkip
	}

	card := quoteWorkflowCard(workflowID, version)
	card.Dag = steps
	return card, &scriptedWorkflowExecutor{results: results}, expectedOutcome
}

func workflowStepOutcomeCounts(steps []types.WorkflowStepInvocation) map[string]uint64 {
	out := make(map[string]uint64)
	for _, step := range steps {
		out[step.Outcome]++
	}
	return out
}

func withWorkflowStepTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return recorder
}

func withWorkflowOTLPCollector(t *testing.T) (*workflowOTLPCollector, func()) {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	collector := &workflowOTLPCollector{}
	collectorpb.RegisterTraceServiceServer(server, collector)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		require.NoError(t, listener.Close())
	})

	conn, err := grpc.NewClient("passthrough:///workflow-otlp-collector",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	exporter, err := otlptracegrpc.New(context.Background(), otlptracegrpc.WithGRPCConn(conn))
	require.NoError(t, err)
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	return collector, func() {
		require.NoError(t, provider.ForceFlush(context.Background()))
	}
}

func withWorkflowMetricReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	return reader
}

func collectWorkflowMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	return metrics
}

func findWorkflowInt64Point(t *testing.T, metrics metricdata.ResourceMetrics, name string, labels map[string]string) metricdata.DataPoint[int64] {
	t.Helper()

	sum, ok := findWorkflowMetric(t, metrics, name).Data.(metricdata.Sum[int64])
	require.True(t, ok)
	for _, point := range sum.DataPoints {
		if workflowMetricLabelsContain(point.Attributes, labels) {
			return point
		}
	}
	t.Fatalf("metric %s with labels %#v not found", name, labels)
	return metricdata.DataPoint[int64]{}
}

func findWorkflowHistogramPoint(t *testing.T, metrics metricdata.ResourceMetrics, name string, labels map[string]string) metricdata.HistogramDataPoint[float64] {
	t.Helper()

	histogram, ok := findWorkflowMetric(t, metrics, name).Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	for _, point := range histogram.DataPoints {
		if workflowMetricLabelsContain(point.Attributes, labels) {
			return point
		}
	}
	t.Fatalf("metric %s with labels %#v not found", name, labels)
	return metricdata.HistogramDataPoint[float64]{}
}

func findWorkflowMetric(t *testing.T, metrics metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()
	for _, scopeMetrics := range metrics.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name == name {
				return metric
			}
		}
	}
	t.Fatalf("metric %s not found", name)
	return metricdata.Metrics{}
}

func workflowMetricLabelsContain(attrs attribute.Set, want map[string]string) bool {
	got := workflowMetricLabelMap(attrs)
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}

func workflowMetricLabelMap(attrs attribute.Set) map[string]string {
	out := make(map[string]string, attrs.Len())
	for _, attr := range attrs.ToSlice() {
		out[string(attr.Key)] = attr.Value.AsString()
	}
	return out
}

func requireWorkflowMetricLabelKeys(t *testing.T, labels map[string]string, keys ...string) {
	t.Helper()
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
		require.Contains(t, labels, key)
	}
	require.Len(t, labels, len(keys))
	for key := range labels {
		_, ok := allowed[key]
		require.True(t, ok, "unexpected workflow metric label %q in %#v", key, labels)
	}
	require.NotContains(t, labels, "trace_id")
	require.NotContains(t, labels, "bundle_id")
	require.NotContains(t, labels, "step_id")
}

func workflowStepSpans(spans []sdktrace.ReadOnlySpan) []sdktrace.ReadOnlySpan {
	out := make([]sdktrace.ReadOnlySpan, 0, len(spans))
	for _, span := range spans {
		if span != nil && span.Name() == workflowStepSpanName {
			out = append(out, span)
		}
	}
	return out
}

type workflowOTLPCollector struct {
	collectorpb.UnimplementedTraceServiceServer

	mu    sync.Mutex
	spans []*tracepb.Span
}

func (c *workflowOTLPCollector) Export(_ context.Context, req *collectorpb.ExportTraceServiceRequest) (*collectorpb.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, resourceSpans := range req.GetResourceSpans() {
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			c.spans = append(c.spans, scopeSpans.GetSpans()...)
		}
	}
	return &collectorpb.ExportTraceServiceResponse{}, nil
}

func (c *workflowOTLPCollector) workflowStepSpans() []*tracepb.Span {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*tracepb.Span, 0, len(c.spans))
	for _, span := range c.spans {
		if span.GetName() == workflowStepSpanName {
			out = append(out, span)
		}
	}
	return out
}

func otlpStepIDs(spans []*tracepb.Span) []string {
	out := make([]string, 0, len(spans))
	for _, span := range spans {
		out = append(out, otlpSpanStringAttr(nil, span, "step_id"))
	}
	return out
}

func findWorkflowStepSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, stepID string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			if attr.Key == attribute.Key("step_id") && attr.Value.AsString() == stepID {
				return span
			}
		}
	}
	t.Fatalf("span for workflow step %q not found", stepID)
	return nil
}

func findOTLPWorkflowStepSpan(t *testing.T, spans []*tracepb.Span, stepID string) *tracepb.Span {
	t.Helper()
	for _, span := range spans {
		if otlpSpanStringAttr(t, span, "step_id") == stepID {
			return span
		}
	}
	t.Fatalf("OTLP span for workflow step %q not found", stepID)
	return nil
}

func otlpSpanStringAttr(t *testing.T, span *tracepb.Span, key string) string {
	if t != nil {
		t.Helper()
	}
	for _, attr := range span.GetAttributes() {
		if attr.GetKey() == key {
			return attr.GetValue().GetStringValue()
		}
	}
	if t != nil {
		t.Fatalf("OTLP span %q missing string attribute %q", span.GetName(), key)
	}
	return ""
}

func otlpSpanStringSliceAttr(t *testing.T, span *tracepb.Span, key string) []string {
	t.Helper()
	for _, attr := range span.GetAttributes() {
		if attr.GetKey() != key {
			continue
		}
		values := attr.GetValue().GetArrayValue().GetValues()
		out := make([]string, 0, len(values))
		for _, value := range values {
			out = append(out, value.GetStringValue())
		}
		return out
	}
	t.Fatalf("OTLP span %q missing string slice attribute %q", span.GetName(), key)
	return nil
}

func spanStringAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string) string {
	t.Helper()
	for _, attr := range span.Attributes() {
		if attr.Key == attribute.Key(key) {
			return attr.Value.AsString()
		}
	}
	t.Fatalf("span %q missing string attribute %q", span.Name(), key)
	return ""
}

func spanIntAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string) int64 {
	t.Helper()
	for _, attr := range span.Attributes() {
		if attr.Key == attribute.Key(key) {
			return attr.Value.AsInt64()
		}
	}
	t.Fatalf("span %q missing int attribute %q", span.Name(), key)
	return 0
}

func assertWorkflowFailureAttribution(t *testing.T, attr *types.WorkflowFailureAttribution, stepID string, reasonCode string, want map[string]any) {
	t.Helper()

	require.NotNil(t, attr)
	require.Equal(t, stepID, attr.GetStepId())
	require.Equal(t, reasonCode, attr.GetReasonCode())
	require.NotEmpty(t, attr.GetStateSnapshot())

	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(attr.GetStateSnapshot(), &snapshot))
	for key, wantValue := range want {
		gotValue, ok := snapshotValue(snapshot, key)
		require.True(t, ok, "snapshot missing %s", key)
		require.Equal(t, wantValue, gotValue, "snapshot field %s", key)
	}
}

func snapshotValue(snapshot map[string]any, key string) (any, bool) {
	parent, child, nested := strings.Cut(key, ".")
	if !nested {
		value, ok := snapshot[parent]
		return value, ok
	}
	parentValue, ok := snapshot[parent].(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := parentValue[child]
	return value, ok
}

func TestInvoke_Retry_WithinBudget_Settle(t *testing.T) {
	step := invokeStep("step-a", 10, nil)
	step.RetryPolicy = &types.RetryPolicy{MaxAttempts: 2, RetryableErrorCodes: []string{"temporary"}}
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{step})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {
			{Outcome: types.WorkflowStepOutcomeFailed, Cost: quoteCoin("ulac", "2"), Duration: 3 * time.Millisecond, ErrorCode: "temporary", ErrorMessage: "retry"},
			{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: 4 * time.Millisecond},
		},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeFinalized, receipt.Outcome)
	require.Equal(t, uint32(2), receipt.StepReceipts[0].AttemptCount)
	require.Equal(t, "3", receipt.StepReceipts[0].Cost.Amount)
	require.Equal(t, uint32(7), receipt.StepReceipts[0].DurationMS)
	require.Equal(t, "3", receipt.TotalCost.Amount)
	require.Equal(t, 1, ledger.settleCalls)
}

func TestInvoke_Retry_CumulativeBudget_Revert(t *testing.T) {
	step := invokeStep("step-a", 10, nil)
	step.MaxSubCost.Amount = sdkmath.NewInt(5)
	step.RetryPolicy = &types.RetryPolicy{MaxAttempts: 2, RetryableErrorCodes: []string{"temporary"}}
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{step})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {
			{Outcome: types.WorkflowStepOutcomeFailed, Cost: quoteCoin("ulac", "4"), Duration: time.Millisecond, ErrorCode: "temporary", ErrorMessage: "retry"},
			{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "3"), Duration: time.Millisecond},
		},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "step_cost_exceeded", receipt.FailureCode)
	require.Equal(t, uint32(2), receipt.StepReceipts[0].AttemptCount)
	require.Equal(t, "7", receipt.StepReceipts[0].Cost.Amount)
	require.Equal(t, 1, ledger.revertCalls)
}

func TestInvoke_Retry_CumulativeSLO_Revert(t *testing.T) {
	step := invokeStep("step-a", 5, nil)
	step.RetryPolicy = &types.RetryPolicy{MaxAttempts: 2, RetryableErrorCodes: []string{"temporary"}}
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{step})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {
			{Outcome: types.WorkflowStepOutcomeFailed, Cost: quoteCoin("ulac", "1"), Duration: 3 * time.Millisecond, ErrorCode: "temporary", ErrorMessage: "retry"},
			{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: 3 * time.Millisecond},
		},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "step_slo_exceeded", receipt.FailureCode)
	require.Equal(t, uint32(2), receipt.StepReceipts[0].AttemptCount)
	require.Equal(t, "2", receipt.StepReceipts[0].Cost.Amount)
	require.Equal(t, uint32(6), receipt.StepReceipts[0].DurationMS)
	require.Equal(t, 1, ledger.revertCalls)
}

func TestInvoke_Retry_ExceedsBudget_Revert(t *testing.T) {
	step := invokeStep("step-a", 10, nil)
	step.RetryPolicy = &types.RetryPolicy{MaxAttempts: 2, RetryableErrorCodes: []string{"temporary"}}
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{step})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {
			{Outcome: types.WorkflowStepOutcomeFailed, ErrorCode: "temporary", ErrorMessage: "retry"},
			{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "999999"), Duration: time.Millisecond},
		},
	}}

	receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})

	require.NoError(t, err)
	require.Equal(t, types.WorkflowOutcomeReverted, receipt.Outcome)
	require.Equal(t, "step_cost_exceeded", receipt.FailureCode)
	require.Equal(t, 1, ledger.revertCalls)
}

func TestInvoke_SideEffectNonReversible_MustBeLast(t *testing.T) {
	stepA := invokeStep("step-a", 10, nil)
	stepA.SideEffect = types.SideEffect_SIDE_EFFECT_NON_REVERSIBLE
	card := invokeWorkflowCard([]*types.Step{
		stepA,
		invokeStep("step-b", 10, []string{"step-a"}),
	})
	ctx, k, quote, pubkey := invokeFixtureWithStoredCard(t, card)
	ledger := &fakeWorkflowLedger{}

	_, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: &scriptedWorkflowExecutor{}})

	require.ErrorContains(t, err, "non_reversible")
	require.Equal(t, 0, ledger.lockCalls)
}

func TestInvoke_RouterRestart_MidVerify_FinishesOrReverts_NeverPartial(t *testing.T) {
	ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{
		invokeStep("step-a", 10, nil),
	})
	ledger := &fakeWorkflowLedger{}
	executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
		"step-a": {{Outcome: types.WorkflowStepOutcomeSuccess, Cost: quoteCoin("ulac", "1"), Duration: time.Millisecond}},
	}}

	first, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})
	require.NoError(t, err)
	replayed, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})
	require.NoError(t, err)

	require.Equal(t, first.Outcome, replayed.Outcome)
	require.NotEqual(t, "", replayed.LockID)
	require.Equal(t, 1, ledger.lockCalls)
	require.Equal(t, 1, ledger.settleCalls)
	require.Equal(t, 0, ledger.revertCalls)
}

func TestPropInvoke_Conservation(t *testing.T) {
	for i := 0; i < 12; i++ {
		step := invokeStep("step-a", 10, nil)
		ctx, k, quote, pubkey := invokeFixture(t, []*types.Step{step})
		ledger := &fakeWorkflowLedger{}
		outcome := types.WorkflowStepOutcomeSuccess
		if i%3 == 0 {
			outcome = types.WorkflowStepOutcomeFailed
		}
		executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{
			"step-a": {{Outcome: outcome, Cost: quoteCoin("ulac", fmt.Sprintf("%d", i+1)), Duration: time.Millisecond, ErrorCode: "prop_fail", ErrorMessage: "prop fail"}},
		}}

		receipt, err := k.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})
		require.NoError(t, err)
		require.Equal(t, 1, ledger.lockCalls)
		require.Equal(t, 1, ledger.settleCalls+ledger.revertCalls)
		require.NotEmpty(t, receipt.Outcome)
		if receipt.Outcome == types.WorkflowOutcomeFinalized {
			require.Empty(t, receipt.StepReceipts[0].ErrorCode)
			require.Empty(t, receipt.StepReceipts[0].ErrorMessage)
		}
	}
}

func FuzzInvoke_AdversarialDAG(f *testing.F) {
	f.Add(uint8(1), uint8(0), uint8(1), uint8(0))
	f.Add(uint8(4), uint8(7), uint8(2), uint8(3))
	f.Add(uint8(6), uint8(31), uint8(5), uint8(9))

	f.Fuzz(func(t *testing.T, rawSteps uint8, failMask uint8, skipMask uint8, slowMask uint8) {
		stepCount := int(rawSteps%6) + 1
		steps := make([]*types.Step, 0, stepCount)
		for i := 0; i < stepCount; i++ {
			var deps []string
			if i > 0 && (failMask>>uint(i%8))&1 == 1 {
				deps = append(deps, fmt.Sprintf("step-%02d", i-1))
			}
			step := invokeStep(fmt.Sprintf("step-%02d", i), 10, deps)
			if (skipMask>>uint(i%8))&1 == 1 {
				step.FailureAction = types.FailureAction_FAILURE_ACTION_SKIP_DOWNSTREAM
			}
			steps = append(steps, step)
		}

		ctx, keeper, quote, pubkey := invokeFixture(t, steps)
		ledger := &fakeWorkflowLedger{}
		executor := &scriptedWorkflowExecutor{results: map[string][]WorkflowStepResult{}}
		for i := 0; i < stepCount; i++ {
			stepID := fmt.Sprintf("step-%02d", i)
			outcome := types.WorkflowStepOutcomeSuccess
			code := ""
			msg := ""
			duration := time.Millisecond
			if (failMask>>uint(i%8))&1 == 1 {
				outcome = types.WorkflowStepOutcomeFailed
				code = "fuzz_fault"
				msg = "fuzz fault"
			}
			if (slowMask>>uint(i%8))&1 == 1 {
				duration = 11 * time.Millisecond
			}
			executor.results[stepID] = []WorkflowStepResult{{
				Outcome:      outcome,
				Cost:         quoteCoin("ulac", "1"),
				Duration:     duration,
				ErrorCode:    code,
				ErrorMessage: msg,
			}}
		}

		receipt, err := keeper.InvokeWorkflow(ctx, InvokeWorkflowRequest{Quote: quote, ExpectedRouterPubkey: pubkey, Ledger: ledger, Executor: executor})
		require.NoError(t, err)
		require.NotEmpty(t, receipt.Outcome)
		require.Equal(t, 1, ledger.lockCalls)
		require.Equal(t, 1, ledger.settleCalls+ledger.revertCalls)
	})
}

type fakeWorkflowLedger struct {
	lockCalls   int
	settleCalls int
	revertCalls int
}

func (f *fakeWorkflowLedger) LockWorkflowBundle(_ context.Context, quote *types.BundleQuote) (string, error) {
	f.lockCalls++
	return "lock-" + quote.BundleID, nil
}

func (f *fakeWorkflowLedger) SettleWorkflowBundle(_ context.Context, _ string, _ *types.WorkflowInvocationReceipt) error {
	f.settleCalls++
	return nil
}

func (f *fakeWorkflowLedger) RevertWorkflowBundle(_ context.Context, _ string, _ *types.WorkflowInvocationReceipt) error {
	f.revertCalls++
	return nil
}

type fakeWastedWorkFeeSink struct {
	calls    int
	quotes   []*types.BundleQuote
	receipts []*types.WorkflowInvocationReceipt
	fees     []types.QuoteCoin
	err      error
}

func (f *fakeWastedWorkFeeSink) ChargeWorkflowWastedWorkFee(_ context.Context, quote *types.BundleQuote, receipt *types.WorkflowInvocationReceipt, fee types.QuoteCoin) error {
	f.calls++
	f.quotes = append(f.quotes, quote)
	f.receipts = append(f.receipts, cloneWorkflowInvocationReceipt(receipt))
	f.fees = append(f.fees, fee)
	return f.err
}

type fakeWorkflowReceiptAnchorer struct {
	calls int
	roots [][]byte
}

func (f *fakeWorkflowReceiptAnchorer) AnchorWorkflowReceipt(ctx context.Context, receipt *types.WorkflowInvocationReceipt) (*types.WorkflowReceiptAnchor, error) {
	f.calls++
	f.roots = append(f.roots, append([]byte(nil), receipt.MerkleRoot...))
	sum := sha256.Sum256(receipt.MerkleRoot)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return &types.WorkflowReceiptAnchor{
		ChainId:        "lumera",
		TxHash:         append([]byte(nil), sum[:]...),
		AnchoredHeight: sdkCtx.BlockHeight(),
		AnchoredAt:     sdkCtx.BlockTime(),
		Status:         "anchored",
	}, nil
}

type invalidWorkflowReceiptAnchorer struct {
	calls int
}

func (f *invalidWorkflowReceiptAnchorer) AnchorWorkflowReceipt(ctx context.Context, receipt *types.WorkflowInvocationReceipt) (*types.WorkflowReceiptAnchor, error) {
	f.calls++
	sum := sha256.Sum256(receipt.MerkleRoot)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return &types.WorkflowReceiptAnchor{
		ChainId:        " lumera ",
		TxHash:         append([]byte(nil), sum[:]...),
		AnchoredHeight: sdkCtx.BlockHeight(),
		AnchoredAt:     sdkCtx.BlockTime(),
		Status:         "anchored",
	}, nil
}

type scriptedWorkflowExecutor struct {
	results map[string][]WorkflowStepResult
	calls   []WorkflowStepExecution
}

func (s *scriptedWorkflowExecutor) ExecuteWorkflowStep(_ context.Context, step WorkflowStepExecution) (WorkflowStepResult, error) {
	s.calls = append(s.calls, step)
	queue := s.results[step.Step.GetStepId()]
	if len(queue) == 0 {
		return WorkflowStepResult{
			Outcome:  types.WorkflowStepOutcomeSuccess,
			Cost:     quoteCoin(step.Quote.SubMaxCost.Denom, "0"),
			Duration: time.Millisecond,
		}, nil
	}
	next := queue[0]
	s.results[step.Step.GetStepId()] = queue[1:]
	if next.Outcome == types.WorkflowStepOutcomeFailed && next.ErrorMessage == "return-error" {
		return next, errors.New("return-error")
	}
	return next, nil
}

func (s *scriptedWorkflowExecutor) stepIDs() []string {
	out := make([]string, 0, len(s.calls))
	for _, call := range s.calls {
		out = append(out, call.Step.GetStepId())
	}
	return out
}

func invokeFixture(t *testing.T, steps []*types.Step) (sdkCtx context.Context, k *Keeper, quote *types.BundleQuote, pubkey string) {
	t.Helper()
	return invokeFixtureWithCard(t, invokeWorkflowCard(steps))
}

func invokeFixtureWithCard(t *testing.T, card *types.WorkflowCard) (sdkCtx context.Context, k *Keeper, quote *types.BundleQuote, pubkey string) {
	t.Helper()
	ctx, keeper := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	msg := quotePublishMsg(card.GetWorkflowId(), card.GetVersion())
	msg.WorkflowCard = card
	require.NoError(t, keeper.PublishWorkflow(ctx, msg))
	quote, err := keeper.QuoteWorkflow(ctx, &types.QuoteWorkflowRequest{
		WorkflowID:         card.GetWorkflowId(),
		Version:            card.GetVersion(),
		Inputs:             json.RawMessage(`{"asset":"ETH"}`),
		CallerPassportTier: "standard",
		Nonce:              "nonce-" + card.GetWorkflowId(),
		RouterPrivateKey:   priv,
	})
	require.NoError(t, err)
	pubkey, err = types.RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)
	return ctx, keeper, quote, pubkey
}

func invokeFixtureWithStoredCard(t *testing.T, card *types.WorkflowCard) (sdkCtx context.Context, k *Keeper, quote *types.BundleQuote, pubkey string) {
	t.Helper()
	ctx, keeper := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	require.NoError(t, keeper.PutWorkflow(ctx, &types.WorkflowRecord{
		WorkflowID:    card.GetWorkflowId(),
		Version:       card.GetVersion(),
		Status:        types.WorkflowStatusActive,
		AuthorAddress: workflowTestAuthorAddress(),
		Card:          card,
		CreatedHeight: height,
		UpdatedHeight: height,
	}))
	quote, err := keeper.QuoteWorkflow(ctx, &types.QuoteWorkflowRequest{
		WorkflowID:         card.GetWorkflowId(),
		Version:            card.GetVersion(),
		Inputs:             json.RawMessage(`{"asset":"ETH"}`),
		CallerPassportTier: "standard",
		Nonce:              "nonce-" + card.GetWorkflowId(),
		RouterPrivateKey:   priv,
	})
	require.NoError(t, err)
	pubkey, err = types.RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)
	return ctx, keeper, quote, pubkey
}

func invokeWorkflowCard(steps []*types.Step) *types.WorkflowCard {
	card := quoteWorkflowCard("wf-invoke", "1.0.0")
	card.Dag = steps
	return card
}

func invokeStep(stepID string, slo uint32, dependsOn []string) *types.Step {
	return &types.Step{
		StepId:                stepID,
		ToolId:                "tool." + stepID,
		ToolVersionConstraint: "1.0.0",
		InputBinding:          "$.inputs.asset",
		MaxSubCost:            sdk.NewCoin("ulac", sdkmath.NewInt(100)),
		SubSloP95Ms:           slo,
		RetryPolicy:           &types.RetryPolicy{MaxAttempts: 1},
		SideEffect:            types.SideEffect_SIDE_EFFECT_REVERSIBLE,
		FailureAction:         types.FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
		DependsOn:             dependsOn,
	}
}

func forceQuoteTotalSLO(t *testing.T, ctx context.Context, k *Keeper, quote *types.BundleQuote, totalSLO uint32) *types.BundleQuote {
	t.Helper()
	_, priv := deterministicQuoteKey()
	rewritten := *quote
	rewritten.TotalSloP95Ms = totalSLO
	var err error
	rewritten.BundleID, err = types.ComputeBundleQuoteID(&rewritten)
	require.NoError(t, err)
	require.NoError(t, types.SignBundleQuote(&rewritten, priv))
	require.NoError(t, k.PutBundleQuote(ctx, &types.BundleQuoteRecord{
		BundleID:      rewritten.BundleID,
		Quote:         &rewritten,
		ExpiresAt:     rewritten.ExpiresAt,
		UpdatedHeight: 1,
	}))
	return &rewritten
}

func cloneWorkflowStepResults(in map[string][]WorkflowStepResult) map[string][]WorkflowStepResult {
	out := make(map[string][]WorkflowStepResult, len(in))
	for stepID, results := range in {
		out[stepID] = append([]WorkflowStepResult(nil), results...)
	}
	return out
}

func quoteCoin(denom string, amount string) types.QuoteCoin {
	coin, err := types.NewQuoteCoin(denom, amount)
	if err != nil {
		panic(err)
	}
	return coin
}

func validInvokeInvariant(id string, expression string, phase types.InvariantPhase) *types.SafetyInvariant {
	return &types.SafetyInvariant{
		InvariantId: id,
		Expression:  expression,
		Phase:       phase,
		Severity:    "error",
		ErrorCode:   id + "_failed",
		HintMessage: "test invariant",
	}
}
