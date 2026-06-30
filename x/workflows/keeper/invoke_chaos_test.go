package keeper

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

type chaosFaultClass string

const (
	chaosKillMidStep             chaosFaultClass = "kill_mid_step"
	chaosNetworkPartition        chaosFaultClass = "network_partition"
	chaosStepTimeout             chaosFaultClass = "step_timeout"
	chaosDoubleInvoke            chaosFaultClass = "double_invoke"
	chaosOutOfOrderCompletion    chaosFaultClass = "out_of_order_step_completion"
	chaosPartialStepReceipt      chaosFaultClass = "partial_step_receipt"
	chaosCreditsLedgerDesync     chaosFaultClass = "credits_ledger_desync"
	chaosRouterRestartMidVerify  chaosFaultClass = "router_restart_mid_verify"
	chaosScenarioSeedCount       int             = 5
	chaosRandomizedSoakScenarios int             = 10_000
)

var chaosFaultClasses = []chaosFaultClass{
	chaosKillMidStep,
	chaosNetworkPartition,
	chaosStepTimeout,
	chaosDoubleInvoke,
	chaosOutOfOrderCompletion,
	chaosPartialStepReceipt,
	chaosCreditsLedgerDesync,
	chaosRouterRestartMidVerify,
}

func TestChaos_FaultMatrix_BundleInvariant(t *testing.T) {
	for _, class := range chaosFaultClasses {
		class := class
		for seed := int64(1); seed <= int64(chaosScenarioSeedCount); seed++ {
			seed := seed
			t.Run(fmt.Sprintf("%s_seed_%02d", class, seed), func(t *testing.T) {
				result := runChaosInvokeScenario(t, class, seed)
				assertChaosBundleInvariant(t, result)
			})
		}
	}
}

func TestChaos_RandomizedFaultSoak_Conservation(t *testing.T) {
	rng := rand.New(rand.NewSource(0x8c0ffee))
	for i := 0; i < chaosRandomizedSoakScenarios; i++ {
		class := chaosFaultClasses[rng.Intn(len(chaosFaultClasses))]
		result := runChaosInvokeScenario(t, class, int64(i+1))
		assertChaosBundleInvariant(t, result)
	}
}

func FuzzChaos_AdversarialExecutor(f *testing.F) {
	f.Add(uint8(0), uint8(1), uint8(0), uint8(0))
	f.Add(uint8(2), uint8(3), uint8(1), uint8(1))
	f.Add(uint8(5), uint8(7), uint8(2), uint8(2))

	f.Fuzz(func(t *testing.T, rawFault uint8, rawSeed uint8, rawSteps uint8, rawMode uint8) {
		class := chaosFaultClasses[int(rawFault)%len(chaosFaultClasses)]
		result := runChaosInvokeScenarioWithStepCount(t, class, int64(rawSeed)+1, int(rawSteps%4)+1, rawMode)
		assertChaosBundleInvariant(t, result)
	})
}

type chaosScenarioResult struct {
	class         chaosFaultClass
	seed          int64
	receipt       *types.WorkflowInvocationReceipt
	err           error
	ledger        *chaosWorkflowLedger
	executorCalls []string
	expectedCode  string
}

type chaosWorkflowLedger struct {
	lockCalls      int
	settleCalls    int
	revertCalls    int
	activeLocks    map[string]struct{}
	failSettleOnce bool
}

func newChaosWorkflowLedger() *chaosWorkflowLedger {
	return &chaosWorkflowLedger{activeLocks: make(map[string]struct{})}
}

func (l *chaosWorkflowLedger) LockWorkflowBundle(_ context.Context, quote *types.BundleQuote) (string, error) {
	l.lockCalls++
	lockID := fmt.Sprintf("lock-%s-%d", quote.BundleID, l.lockCalls)
	l.activeLocks[lockID] = struct{}{}
	return lockID, nil
}

func (l *chaosWorkflowLedger) SettleWorkflowBundle(_ context.Context, lockID string, _ *types.WorkflowInvocationReceipt) error {
	l.settleCalls++
	if l.failSettleOnce {
		l.failSettleOnce = false
		return errors.New("simulated credits keeper write failure after lock")
	}
	delete(l.activeLocks, lockID)
	return nil
}

func (l *chaosWorkflowLedger) RevertWorkflowBundle(_ context.Context, lockID string, _ *types.WorkflowInvocationReceipt) error {
	l.revertCalls++
	delete(l.activeLocks, lockID)
	return nil
}

func runChaosInvokeScenario(t *testing.T, class chaosFaultClass, seed int64) chaosScenarioResult {
	t.Helper()
	return runChaosInvokeScenarioWithStepCount(t, class, seed, 3, 0)
}

func runChaosInvokeScenarioWithStepCount(t *testing.T, class chaosFaultClass, seed int64, stepCount int, mode uint8) chaosScenarioResult {
	t.Helper()
	if stepCount < 1 {
		stepCount = 1
	}
	steps := chaosSteps(stepCount)
	ctx, keeper, quote, pubkey := invokeFixture(t, steps)
	ledger := newChaosWorkflowLedger()
	executor := &scriptedWorkflowExecutor{results: chaosStepResults(class, seed, quote, steps, mode)}
	expectedCode := ""

	switch class {
	case chaosCreditsLedgerDesync:
		ledger.failSettleOnce = true
		expectedCode = "workflow_settlement_failed"
	case chaosStepTimeout:
		expectedCode = "step_slo_exceeded"
	case chaosPartialStepReceipt:
		expectedCode = "step_cost_invalid"
	case chaosKillMidStep:
		expectedCode = "router_killed_mid_step"
	case chaosNetworkPartition:
		expectedCode = "network_partition"
	}

	receipt, err := keeper.InvokeWorkflow(ctx, InvokeWorkflowRequest{
		Quote:                quote,
		ExpectedRouterPubkey: pubkey,
		Ledger:               ledger,
		Executor:             executor,
		TraceID:              fmt.Sprintf("trace-chaos-%s-%d", class, seed),
	})
	if class == chaosDoubleInvoke || class == chaosRouterRestartMidVerify {
		replayed, replayErr := keeper.InvokeWorkflow(ctx, InvokeWorkflowRequest{
			Quote:                quote,
			ExpectedRouterPubkey: pubkey,
			Ledger:               ledger,
			Executor:             executor,
			TraceID:              fmt.Sprintf("trace-chaos-replay-%s-%d", class, seed),
		})
		require.NoError(t, replayErr)
		require.NotNil(t, replayed)
		require.Equal(t, receipt.Outcome, replayed.Outcome)
		require.Equal(t, receipt.LockID, replayed.LockID)
	}

	return chaosScenarioResult{
		class:         class,
		seed:          seed,
		receipt:       receipt,
		err:           err,
		ledger:        ledger,
		executorCalls: executor.stepIDs(),
		expectedCode:  expectedCode,
	}
}

func chaosSteps(count int) []*types.Step {
	steps := make([]*types.Step, 0, count)
	for i := 0; i < count; i++ {
		stepID := fmt.Sprintf("step-%02d", i)
		var deps []string
		if i > 0 {
			deps = []string{fmt.Sprintf("step-%02d", i-1)}
		}
		step := invokeStep(stepID, 10, deps)
		steps = append(steps, step)
	}
	return steps
}

func chaosStepResults(class chaosFaultClass, seed int64, quote *types.BundleQuote, steps []*types.Step, mode uint8) map[string][]WorkflowStepResult {
	results := make(map[string][]WorkflowStepResult, len(steps))
	for i, step := range steps {
		stepID := step.GetStepId()
		result := WorkflowStepResult{
			Outcome:  types.WorkflowStepOutcomeSuccess,
			Cost:     quoteCoin("ulac", fmt.Sprintf("%d", (seed%3)+1)),
			Duration: time.Millisecond,
		}
		if i == 0 {
			switch class {
			case chaosKillMidStep:
				result.Outcome = types.WorkflowStepOutcomeFailed
				result.ErrorCode = "router_killed_mid_step"
				result.ErrorMessage = "return-error"
			case chaosNetworkPartition:
				result.Outcome = types.WorkflowStepOutcomeFailed
				result.ErrorCode = "network_partition"
				result.ErrorMessage = "return-error"
			case chaosStepTimeout:
				result.Duration = 11 * time.Millisecond
			case chaosPartialStepReceipt:
				result.Cost = types.QuoteCoin{Denom: "", Amount: "not-an-int"}
			}
		}
		if class == chaosOutOfOrderCompletion && mode%2 == 1 && i == len(steps)-1 {
			result.Duration = 2 * time.Millisecond
		}
		results[stepID] = []WorkflowStepResult{result}
	}
	if class == chaosOutOfOrderCompletion {
		// The executor API cannot supply an alternate step_id, so an adversarial
		// completion can only vary payload timing; the engine keeps DAG order.
		for _, quoteStep := range quote.StepQuotes {
			if _, ok := results[quoteStep.StepID]; !ok {
				results[quoteStep.StepID] = []WorkflowStepResult{{
					Outcome:  types.WorkflowStepOutcomeSuccess,
					Cost:     quoteCoin("ulac", "1"),
					Duration: time.Millisecond,
				}}
			}
		}
	}
	return results
}

func assertChaosBundleInvariant(t *testing.T, result chaosScenarioResult) {
	t.Helper()
	require.NoError(t, result.err, "scenario=%s seed=%d", result.class, result.seed)
	require.NotNil(t, result.receipt, "scenario=%s seed=%d", result.class, result.seed)
	require.NotEmpty(t, result.receipt.Outcome)
	require.Equal(t, 1, result.ledger.lockCalls, "scenario=%s seed=%d", result.class, result.seed)
	require.LessOrEqual(t, result.ledger.settleCalls, 1, "double-settle detected")
	require.Empty(t, result.ledger.activeLocks, "orphan locks detected")

	if result.expectedCode != "" {
		require.Equal(t, types.WorkflowOutcomeReverted, result.receipt.Outcome)
		require.Equal(t, result.expectedCode, result.receipt.FailureCode)
		require.Equal(t, 1, result.ledger.revertCalls)
	}
	if result.class == chaosOutOfOrderCompletion {
		expectedCalls := make([]string, 0, len(result.executorCalls))
		for i := range result.executorCalls {
			expectedCalls = append(expectedCalls, fmt.Sprintf("step-%02d", i))
		}
		require.Equal(t, expectedCalls, result.executorCalls)
	}
	if result.class == chaosDoubleInvoke || result.class == chaosRouterRestartMidVerify {
		require.Equal(t, 1, result.ledger.lockCalls)
		require.Equal(t, 1, result.ledger.settleCalls+result.ledger.revertCalls)
	}
}
