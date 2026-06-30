package keeper

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"cosmossdk.io/math"
	"github.com/cloudflare/circl/sign/ed448"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

const (
	workflowTelemetryName                 = "github.com/LumeraProtocol/lumera/x/workflows/keeper"
	workflowStepSpanName                  = "workflow.step.invoke"
	metricWorkflowsInvokedTotalName       = "workflows_invoked_total"
	metricWorkflowOutcomeTotalName        = "workflow_outcome_total"
	metricWorkflowBundleDurationName      = "workflow_bundle_duration_seconds"
	metricWorkflowStepDurationSecondsName = "workflow_step_duration_seconds"
	metricWorkflowBundleCostName          = "workflow_bundle_cost"
	metricWorkflowWastedWorkFeeFailures   = "workflow_wasted_work_fee_charge_failures_total"
	workflowFallbackNotTriggeredReason    = "fallback_not_triggered"
	workflowConditionNotMetReason         = "condition_not_met"
)

var workflowMetricCache struct {
	sync.Mutex
	provider metric.MeterProvider
	metrics  *workflowMetrics
}

type workflowMetrics struct {
	invoked        metric.Int64Counter
	outcome        metric.Int64Counter
	bundleDuration metric.Float64Histogram
	stepDuration   metric.Float64Histogram
	bundleCost     metric.Float64Histogram
	feeFailures    metric.Int64Counter
}

// WorkflowCreditsLedger is the lock/settle/revert boundary used by invoke_workflow.
type WorkflowCreditsLedger interface {
	LockWorkflowBundle(ctx context.Context, quote *types.BundleQuote) (string, error)
	SettleWorkflowBundle(ctx context.Context, lockID string, receipt *types.WorkflowInvocationReceipt) error
	RevertWorkflowBundle(ctx context.Context, lockID string, receipt *types.WorkflowInvocationReceipt) error
}

// WorkflowWastedWorkFeeSink applies the reverted-bundle fee after credits are unlocked.
type WorkflowWastedWorkFeeSink interface {
	ChargeWorkflowWastedWorkFee(ctx context.Context, quote *types.BundleQuote, receipt *types.WorkflowInvocationReceipt, fee types.QuoteCoin) error
}

// WorkflowReceiptAnchorer anchors a finalized workflow receipt merkle root.
type WorkflowReceiptAnchorer interface {
	AnchorWorkflowReceipt(ctx context.Context, receipt *types.WorkflowInvocationReceipt) (*types.WorkflowReceiptAnchor, error)
}

// WorkflowStepExecutor executes one resolved workflow DAG step.
type WorkflowStepExecutor interface {
	ExecuteWorkflowStep(ctx context.Context, step WorkflowStepExecution) (WorkflowStepResult, error)
}

// WorkflowStepExecution is the deterministic input to a step executor.
type WorkflowStepExecution struct {
	BundleID   string
	WorkflowID string
	Version    string
	Step       *types.Step
	Quote      types.BundleStepQuote
	Attempt    uint32
	TraceID    string
	InputsHash string
}

// WorkflowStepResult is the executor output consumed by the atomic engine.
type WorkflowStepResult struct {
	Outcome      string
	Cost         types.QuoteCoin
	Duration     time.Duration
	ErrorCode    string
	ErrorMessage string
	OutputClaims []types.WorkflowOutputClaim
}

// InvokeWorkflowRequest contains the live dependencies for bundle invocation.
type InvokeWorkflowRequest struct {
	Quote                *types.BundleQuote
	ExpectedRouterPubkey string
	Now                  time.Time
	TraceID              string
	Ledger               WorkflowCreditsLedger
	Executor             WorkflowStepExecutor
	ReceiptSignerKey     ed448.PrivateKey
	ReceiptAnchorer      WorkflowReceiptAnchorer
	WastedWorkFeeSink    WorkflowWastedWorkFeeSink
}

// InvokeWorkflow executes a quoted workflow atomically: lock, step, verify, settle-or-revert.
func (k *Keeper) InvokeWorkflow(ctx context.Context, req InvokeWorkflowRequest) (*types.WorkflowInvocationReceipt, error) {
	invocationStarted := time.Now()
	if req.Quote == nil {
		return nil, types.ErrInvalidWorkflow.Wrap("bundle quote is required")
	}
	if req.Ledger == nil {
		return nil, types.ErrInvalidWorkflow.Wrap("workflow credits ledger is required")
	}
	if req.Executor == nil {
		return nil, types.ErrInvalidWorkflow.Wrap("workflow step executor is required")
	}
	if err := validateWorkflowReceiptSignerKey(req.ReceiptSignerKey); err != nil {
		return nil, err
	}
	if err := validateWorkflowInvokeTraceID(req.TraceID); err != nil {
		return nil, err
	}
	if err := req.Quote.ValidateBasic(); err != nil {
		return nil, err
	}

	record, found, err := k.GetBundleQuote(ctx, req.Quote.BundleID)
	if err != nil {
		return nil, err
	}
	if found && record.Consumed && record.Invocation != nil && record.Invocation.Receipt != nil {
		if err := types.VerifyBundleQuoteSignature(req.Quote, req.ExpectedRouterPubkey); err != nil {
			return nil, err
		}
		if err := storedQuoteMatches(record.Quote, req.Quote); err != nil {
			return nil, err
		}
		return cloneWorkflowInvocationReceipt(record.Invocation.Receipt), nil
	}
	if err := k.ValidateBundleQuote(ctx, req.Quote, req.ExpectedRouterPubkey, req.Now); err != nil {
		return nil, err
	}
	if !found {
		record, found, err = k.GetBundleQuote(ctx, req.Quote.BundleID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, types.ErrInvalidWorkflow.Wrapf("bundle quote not found: %s", req.Quote.BundleID)
		}
	}

	workflow, found, err := k.GetWorkflow(ctx, req.Quote.WorkflowID, req.Quote.Version)
	if err != nil {
		return nil, err
	}
	if !found || workflow == nil || workflow.Card == nil {
		return nil, types.ErrInvalidWorkflow.Wrapf("workflow version not found: %s/%s", req.Quote.WorkflowID, req.Quote.Version)
	}
	if workflow.Status != types.WorkflowStatusActive {
		return nil, types.ErrInvalidWorkflow.Wrapf("workflow version is not active: %s/%s", req.Quote.WorkflowID, req.Quote.Version)
	}
	card := workflow.Card

	order, _, stepMap, err := workflowExecutionPlan(card)
	if err != nil {
		return nil, err
	}
	if err := enforceNonReversibleLeaves(stepMap); err != nil {
		return nil, err
	}
	quoteByStep, err := quoteStepsByID(req.Quote.StepQuotes, stepMap)
	if err != nil {
		return nil, err
	}

	invariantLogs, err := evaluateWorkflowInvariants(card.GetSafetyInvariants(), types.InvariantPhase_INVARIANT_PHASE_LOCK, types.InvariantEvaluationInput{
		TotalCost:    req.Quote.TotalMaxCost.Amount,
		MaxCost:      req.Quote.TotalMaxCost.Amount,
		InputsDigest: req.Quote.InputsHash,
	})
	if err != nil {
		return nil, err
	}

	lockID, err := req.Ledger.LockWorkflowBundle(ctx, req.Quote)
	if err != nil {
		return nil, err
	}
	lockID = strings.TrimSpace(lockID)
	if lockID == "" {
		return nil, types.ErrInvalidWorkflow.Wrap("workflow credits ledger returned empty lock_id")
	}

	receipt := &types.WorkflowInvocationReceipt{
		BundleID:      req.Quote.BundleID,
		WorkflowID:    req.Quote.WorkflowID,
		Version:       req.Quote.Version,
		Outcome:       types.WorkflowOutcomeFinalized,
		TotalCost:     zeroQuoteCoin(req.Quote.TotalMaxCost.Denom),
		LockID:        lockID,
		TraceID:       req.TraceID,
		CompletedAt:   invokeNow(ctx, req.Now).Format(time.RFC3339Nano),
		InvariantLogs: invariantLogs,
	}

	executed := make(map[string]types.WorkflowStepInvocation, len(stepMap))
	blockedByFailure := make(map[string]struct{})
	fallbackSources := workflowFallbackSources(stepMap)
	totalCost := math.ZeroInt()
	for _, stepID := range order {
		step := stepMap[stepID]
		quote := quoteByStep[stepID]
		if workflowFallbackUntriggered(stepID, fallbackSources, executed) {
			skipped := skippedStepInvocation(step, quote, workflowFallbackNotTriggeredReason)
			receipt.StepReceipts = append(receipt.StepReceipts, skipped)
			executed[stepID] = skipped
			continue
		}
		if workflowDependencyBlocksStep(step, executed, blockedByFailure, stepMap) {
			skipped := skippedStepInvocation(step, quote, "dependency_skipped")
			receipt.StepReceipts = append(receipt.StepReceipts, skipped)
			executed[stepID] = skipped
			blockedByFailure[stepID] = struct{}{}
			if receipt.Outcome == types.WorkflowOutcomeFinalized {
				receipt.Outcome = types.WorkflowOutcomePartialSkip
				recordWorkflowPartialSkip(receipt, skipped.ErrorCode, skipped.ErrorMessage)
			}
			continue
		}
		conditionMet, err := workflowStepConditionMet(step, executed)
		if err != nil {
			return nil, err
		}
		if !conditionMet {
			skipped := skippedStepInvocation(step, quote, workflowConditionNotMetReason)
			receipt.StepReceipts = append(receipt.StepReceipts, skipped)
			executed[stepID] = skipped
			continue
		}

		invocation, stepCost, err := k.invokeStepWithRetries(ctx, req, step, quote)
		receipt.StepReceipts = append(receipt.StepReceipts, invocation)
		executed[stepID] = invocation
		hardFailure := err != nil && isHardStepFailure(invocation.ErrorCode)
		if invocation.Outcome != types.WorkflowStepOutcomeSuccess {
			action := effectiveFailureAction(step.step.GetFailureAction())
			if !hardFailure && action == types.FailureAction_FAILURE_ACTION_SKIP_DOWNSTREAM {
				receipt.Outcome = types.WorkflowOutcomePartialSkip
				recordWorkflowPartialSkip(receipt, stepFailureCode(invocation), stepFailureReason(invocation, err))
				blockedByFailure[stepID] = struct{}{}
				continue
			}
			if !hardFailure && workflowFallbackStepID(step) != "" {
				continue
			}
			receipt.Outcome = types.WorkflowOutcomeReverted
			receipt.FailureCode = stepFailureCode(invocation)
			receipt.FailureReason = stepFailureReason(invocation, err)
			break
		}
		if err != nil {
			receipt.Outcome = types.WorkflowOutcomeReverted
			receipt.FailureCode = stepFailureCode(invocation)
			receipt.FailureReason = stepFailureReason(invocation, err)
			break
		}
		totalCost = totalCost.Add(stepCost)
		if totalCost.GT(quoteCoinAmount(req.Quote.TotalMaxCost)) {
			receipt.Outcome = types.WorkflowOutcomeReverted
			receipt.FailureCode = "bundle_budget_exceeded"
			receipt.FailureReason = "workflow actual cost exceeds bundle quote"
			break
		}
	}

	if receipt.Outcome != types.WorkflowOutcomeReverted {
		actualP95, err := actualCriticalPathP95(stepMap, executed)
		if err != nil {
			return nil, err
		}
		if actualP95 > req.Quote.TotalSloP95Ms {
			receipt.Outcome = types.WorkflowOutcomeReverted
			receipt.FailureCode = "bundle_slo_exceeded"
			receipt.FailureReason = fmt.Sprintf("workflow p95 %dms exceeds quote %dms", actualP95, req.Quote.TotalSloP95Ms)
		}
	}

	totalCoin, err := types.NewQuoteCoin(req.Quote.TotalMaxCost.Denom, totalCost.String())
	if err != nil {
		return nil, err
	}
	receipt.TotalCost = totalCoin
	verifyLogs, verifyErr := evaluateWorkflowInvariants(card.GetSafetyInvariants(), types.InvariantPhase_INVARIANT_PHASE_VERIFY, invariantInputForReceipt(req.Quote, receipt))
	receipt.InvariantLogs = append(receipt.InvariantLogs, verifyLogs...)
	if verifyErr != nil && receipt.Outcome != types.WorkflowOutcomeReverted {
		receipt.Outcome = types.WorkflowOutcomeReverted
		receipt.FailureCode = "workflow_invariant_violation"
		receipt.FailureReason = verifyErr.Error()
	}

	if err := types.PopulateWorkflowFailureAttributions(receipt); err != nil {
		return nil, err
	}
	if err := finalizeWorkflowReceipt(ctx, req, receipt); err != nil {
		return nil, err
	}
	if receipt.Outcome != types.WorkflowOutcomeReverted && req.ReceiptAnchorer != nil {
		anchor, err := req.ReceiptAnchorer.AnchorWorkflowReceipt(ctx, receipt)
		if err != nil {
			receipt.Outcome = types.WorkflowOutcomeReverted
			receipt.FailureCode = "workflow_anchor_failed"
			receipt.FailureReason = err.Error()
			if attrErr := types.PopulateWorkflowFailureAttributions(receipt); attrErr != nil {
				return nil, attrErr
			}
			if finalizeErr := finalizeWorkflowReceipt(ctx, req, receipt); finalizeErr != nil {
				return nil, finalizeErr
			}
		} else if anchor != nil {
			if anchorErr := validateWorkflowReceiptAnchorCandidate(receipt, anchor); anchorErr != nil {
				receipt.Outcome = types.WorkflowOutcomeReverted
				receipt.FailureCode = "workflow_anchor_invalid"
				receipt.FailureReason = anchorErr.Error()
				if attrErr := types.PopulateWorkflowFailureAttributions(receipt); attrErr != nil {
					return nil, attrErr
				}
				if finalizeErr := finalizeWorkflowReceipt(ctx, req, receipt); finalizeErr != nil {
					return nil, finalizeErr
				}
			} else {
				receipt.Anchors = append(receipt.Anchors, anchor)
			}
		}
	}

	var ledgerErr error
	if receipt.Outcome == types.WorkflowOutcomeReverted {
		if revertErr := req.Ledger.RevertWorkflowBundle(ctx, lockID, receipt); revertErr != nil {
			ledgerErr = revertErr
			k.Logger().Error("failed to revert workflow bundle", "error", revertErr)
		} else if feeErr := k.chargeWorkflowWastedWorkFee(ctx, req, receipt); feeErr != nil {
			k.Logger().Error("failed to charge workflow wasted work fee", "error", feeErr)
			recordWorkflowWastedWorkFeeChargeFailure(ctx, receipt)
		}
	} else if settleErr := req.Ledger.SettleWorkflowBundle(ctx, lockID, receipt); settleErr != nil {
		receipt.Outcome = types.WorkflowOutcomeReverted
		receipt.FailureCode = "workflow_settlement_failed"
		receipt.FailureReason = settleErr.Error()
		if attrErr := types.PopulateWorkflowFailureAttributions(receipt); attrErr != nil {
			k.Logger().Error("failed to populate failure attributions", "error", attrErr)
		}
		if finalizeErr := finalizeWorkflowReceipt(ctx, req, receipt); finalizeErr != nil {
			k.Logger().Error("failed to finalize receipt", "error", finalizeErr)
		}
		if revertErr := req.Ledger.RevertWorkflowBundle(ctx, lockID, receipt); revertErr != nil {
			ledgerErr = revertErr
			k.Logger().Error("failed to revert workflow bundle after settlement failure", "error", revertErr)
		} else if feeErr := k.chargeWorkflowWastedWorkFee(ctx, req, receipt); feeErr != nil {
			k.Logger().Error("failed to charge workflow wasted work fee", "error", feeErr)
			recordWorkflowWastedWorkFeeChargeFailure(ctx, receipt)
		}
	}

	if err := persistWorkflowInvocation(ctx, k, record, receipt); err != nil {
		return nil, err
	}
	if ledgerErr != nil {
		return nil, ledgerErr
	}
	recordWorkflowInvocationMetrics(ctx, receipt, time.Since(invocationStarted))
	k.emitWorkflowInvocation(ctx, receipt)
	return cloneWorkflowInvocationReceipt(receipt), nil
}

func validateWorkflowReceiptSignerKey(priv ed448.PrivateKey) error {
	if len(priv) == 0 {
		return nil
	}
	_, err := types.RouterPubkeyFromPrivateKey(priv)
	return err
}

func validateWorkflowInvokeTraceID(traceID string) error {
	if strings.TrimSpace(traceID) != traceID {
		return types.ErrInvalidWorkflow.Wrapf("workflow invoke trace_id must be canonical: %q", traceID)
	}
	return nil
}

func (k *Keeper) chargeWorkflowWastedWorkFee(ctx context.Context, req InvokeWorkflowRequest, receipt *types.WorkflowInvocationReceipt) error {
	if req.WastedWorkFeeSink == nil || receipt == nil || strings.TrimSpace(receipt.Outcome) != types.WorkflowOutcomeReverted {
		return nil
	}
	fee, err := k.computeWorkflowWastedWorkFee(ctx, req.Quote, receipt)
	if err != nil {
		return err
	}
	if quoteCoinAmount(fee).IsZero() {
		return nil
	}
	if err := req.WastedWorkFeeSink.ChargeWorkflowWastedWorkFee(ctx, req.Quote, receipt, fee); err != nil {
		return fmt.Errorf("charge workflow wasted-work fee: %w", err)
	}
	return nil
}

func (k *Keeper) computeWorkflowWastedWorkFee(ctx context.Context, quote *types.BundleQuote, receipt *types.WorkflowInvocationReceipt) (types.QuoteCoin, error) {
	denom := ""
	if quote != nil {
		denom = quote.TotalMaxCost.Denom
	}
	if receipt != nil && strings.TrimSpace(receipt.TotalCost.Denom) != "" {
		denom = receipt.TotalCost.Denom
	}
	zero := zeroQuoteCoin(denom)
	if receipt == nil || strings.TrimSpace(receipt.Outcome) != types.WorkflowOutcomeReverted {
		return zero, nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return zero, err
	}
	if params == nil || params.WastedWorkBPS == 0 {
		return zero, nil
	}
	executedCost := quoteCoinAmount(receipt.TotalCost)
	if executedCost.IsZero() {
		return zero, nil
	}
	fee := ceilBPS(executedCost, params.WastedWorkBPS)
	if quote != nil {
		maxFee := quoteCoinAmount(quote.TotalMaxCost)
		if !maxFee.IsZero() && fee.GT(maxFee) {
			fee = maxFee
		}
	}
	if fee.IsZero() {
		return zero, nil
	}
	return types.NewQuoteCoin(denom, fee.String())
}

// ChargeWorkflowWastedWorkFee implements WorkflowWastedWorkFeeSink by debiting
// the workflow author's bond through the existing slash path.
func (k *Keeper) ChargeWorkflowWastedWorkFee(ctx context.Context, quote *types.BundleQuote, receipt *types.WorkflowInvocationReceipt, fee types.QuoteCoin) error {
	if quote == nil {
		return types.ErrInvalidWorkflow.Wrap("bundle quote is required for wasted-work fee")
	}
	if receipt == nil {
		return types.ErrInvalidWorkflow.Wrap("workflow receipt is required for wasted-work fee")
	}
	if strings.TrimSpace(receipt.Outcome) != types.WorkflowOutcomeReverted {
		return nil
	}
	if quote.BundleID != "" && receipt.BundleID != "" && quote.BundleID != receipt.BundleID {
		return types.ErrInvalidWorkflow.Wrapf("wasted-work fee bundle mismatch: %s != %s", quote.BundleID, receipt.BundleID)
	}
	feeCoin, err := quoteCoinToSDK(fee)
	if err != nil {
		return err
	}
	coin, err := normalizeWorkflowCoin(&feeCoin, quote.TotalMaxCost.Denom)
	if err != nil {
		return err
	}
	amount, err := workflowCoinAmount(coin)
	if err != nil {
		return err
	}
	if amount.IsZero() {
		return nil
	}
	coin, err = k.capWastedWorkFeeToAuthorBondReserve(ctx, quote.WorkflowID, quote.Version, coin)
	if err != nil {
		return err
	}
	amount, err = workflowCoinAmount(coin)
	if err != nil {
		return err
	}
	if amount.IsZero() {
		return nil
	}
	return k.SlashWorkflowAuthorBond(ctx, quote.WorkflowID, quote.Version, coin, "wasted_work_fee")
}

func (k *Keeper) capWastedWorkFeeToAuthorBondReserve(ctx context.Context, workflowID, version string, fee *sdk.Coin) (*sdk.Coin, error) {
	workflow, found, err := k.GetWorkflow(ctx, workflowID, version)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, types.ErrInvalidWorkflow.Wrapf("workflow version not found: %s/%s", workflowID, version)
	}
	bond, found, err := k.GetAuthorBond(ctx, workflow.AuthorAddress)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, types.ErrInvalidWorkflow.Wrapf("author bond not found: %s", workflow.AuthorAddress)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	reserveAmount, ok := math.NewIntFromString(strings.TrimSpace(params.MinAuthorBondAmount))
	if !ok {
		return nil, types.ErrInvalidWorkflow.Wrapf("params min_author_bond_amount must be an integer: %q", params.MinAuthorBondAmount)
	}
	reserve, err := normalizeWorkflowCoin(&sdk.Coin{Denom: strings.TrimSpace(params.BondDenom), Amount: reserveAmount}, workflowBondDenom(bond.Bond))
	if err != nil {
		return nil, err
	}
	current, err := workflowCoinAmount(bond.Bond)
	if err != nil {
		return nil, err
	}
	normalizedReserve, err := workflowCoinAmount(reserve)
	if err != nil {
		return nil, err
	}
	if current.LTE(normalizedReserve) {
		return zeroWorkflowCoin(workflowBondDenom(bond.Bond)), nil
	}
	feeAmount, err := workflowCoinAmount(fee)
	if err != nil {
		return nil, err
	}
	maxSlash := current.Sub(normalizedReserve)
	if feeAmount.GT(maxSlash) {
		fee = cloneWorkflowCoin(fee)
		fee.Amount = maxSlash
	}
	return fee, nil
}

type workflowExecutionStep struct {
	id        string
	step      *types.Step
	dependsOn []string
}

func workflowExecutionPlan(card *types.WorkflowCard) ([]string, bundleLatencyPlan, map[string]workflowExecutionStep, error) {
	normalized := make(map[string]normalizedBundleStep, len(card.GetDag()))
	steps := make(map[string]workflowExecutionStep, len(card.GetDag()))
	for _, step := range card.GetDag() {
		if step == nil {
			return nil, bundleLatencyPlan{}, nil, types.ErrInvalidWorkflow.Wrap("workflow step cannot be nil")
		}
		stepID := strings.TrimSpace(step.GetStepId())
		if stepID == "" {
			return nil, bundleLatencyPlan{}, nil, types.ErrInvalidWorkflow.Wrap("workflow step_id cannot be empty")
		}
		deps := normalizedStepDependencies(step.GetDependsOn())
		normalized[stepID] = normalizedBundleStep{id: stepID, subSLO: step.GetSubSloP95Ms(), dependsOn: deps}
		steps[stepID] = workflowExecutionStep{id: stepID, step: step, dependsOn: deps}
	}
	if err := applyFallbackStepDependencies(normalized, card.GetDag()); err != nil {
		return nil, bundleLatencyPlan{}, nil, err
	}
	if err := applyConditionStepDependencies(normalized, card.GetDag()); err != nil {
		return nil, bundleLatencyPlan{}, nil, err
	}
	for stepID, normalizedStep := range normalized {
		step := steps[stepID]
		step.dependsOn = normalizedStep.dependsOn
		steps[stepID] = step
	}
	order, latency, err := computeBundleLatencyPlan(normalized)
	if err != nil {
		return nil, bundleLatencyPlan{}, nil, err
	}
	return order, latency, steps, nil
}

func enforceNonReversibleLeaves(steps map[string]workflowExecutionStep) error {
	dependents := make(map[string]int, len(steps))
	for _, step := range steps {
		for _, dep := range step.dependsOn {
			dependents[dep]++
		}
	}
	for id, step := range steps {
		if step.step.GetSideEffect() == types.SideEffect_SIDE_EFFECT_NON_REVERSIBLE && dependents[id] > 0 {
			return types.ErrInvalidWorkflow.Wrapf("workflow step %s has non_reversible side effect but is not terminal", id)
		}
	}
	return nil
}

func quoteStepsByID(quotes []types.BundleStepQuote, steps map[string]workflowExecutionStep) (map[string]types.BundleStepQuote, error) {
	out := make(map[string]types.BundleStepQuote, len(quotes))
	for _, quote := range quotes {
		stepID := strings.TrimSpace(quote.StepID)
		if _, ok := steps[stepID]; !ok {
			return nil, types.ErrInvalidWorkflow.Wrapf("bundle quote step %s is not in workflow DAG", stepID)
		}
		if _, ok := out[stepID]; ok {
			return nil, types.ErrInvalidWorkflow.Wrapf("duplicate bundle quote step: %s", stepID)
		}
		out[stepID] = quote
	}
	if len(out) != len(steps) {
		return nil, types.ErrInvalidWorkflow.Wrap("bundle quote steps do not match workflow DAG")
	}
	return out, nil
}

func (k *Keeper) invokeStepWithRetries(ctx context.Context, req InvokeWorkflowRequest, step workflowExecutionStep, quote types.BundleStepQuote) (types.WorkflowStepInvocation, math.Int, error) {
	maxAttempts := uint32(1)
	if policy := step.step.GetRetryPolicy(); policy != nil && policy.GetMaxAttempts() > 0 {
		maxAttempts = policy.GetMaxAttempts()
	}
	var last types.WorkflowStepInvocation
	totalCost := math.ZeroInt()
	var totalDuration uint32
	for attempt := uint32(1); attempt <= maxAttempts; attempt++ {
		stepCtx, span := otel.Tracer(workflowTelemetryName).Start(
			ctx,
			workflowStepSpanName,
			trace.WithAttributes(traceWorkflowStepAttributes(req, step, quote, attempt)...),
		)
		result, execErr := req.Executor.ExecuteWorkflowStep(stepCtx, WorkflowStepExecution{
			BundleID:   req.Quote.BundleID,
			WorkflowID: req.Quote.WorkflowID,
			Version:    req.Quote.Version,
			Step:       step.step,
			Quote:      quote,
			Attempt:    attempt,
			TraceID:    req.TraceID,
			InputsHash: req.Quote.InputsHash,
		})
		invocation, cost, err := normalizeStepInvocation(step, quote, result, attempt, execErr)
		recordWorkflowStepSpan(span, invocation, cost, err)
		totalCost = totalCost.Add(cost)
		totalDuration = addWorkflowStepAttemptDuration(totalDuration, invocation.DurationMS)
		invocation, cumulativeErr := cumulativeWorkflowStepInvocation(invocation, quote, totalCost, totalDuration)
		last = invocation
		if cumulativeErr != nil {
			return invocation, totalCost, cumulativeErr
		}
		if err == nil && invocation.Outcome == types.WorkflowStepOutcomeSuccess {
			return invocation, totalCost, nil
		}
		if attempt == maxAttempts || !isRetryableStepError(step.step.GetRetryPolicy(), invocation.ErrorCode) {
			return invocation, totalCost, err
		}
	}
	return last, totalCost, types.ErrInvalidWorkflow.Wrap("workflow step retry exhausted")
}

func cumulativeWorkflowStepInvocation(invocation types.WorkflowStepInvocation, quote types.BundleStepQuote, totalCost math.Int, totalDuration uint32) (types.WorkflowStepInvocation, error) {
	coin, err := types.NewQuoteCoin(quote.SubMaxCost.Denom, totalCost.String())
	if err != nil {
		invocation.Outcome = types.WorkflowStepOutcomeFailed
		invocation.Cost = zeroQuoteCoin(quote.SubMaxCost.Denom)
		invocation.DurationMS = totalDuration
		invocation.ErrorCode = "step_cost_invalid"
		invocation.ErrorMessage = err.Error()
		return invocation, err
	}
	invocation.Cost = coin
	invocation.DurationMS = totalDuration
	if totalCost.GT(quoteCoinAmount(quote.SubMaxCost)) {
		invocation.Outcome = types.WorkflowStepOutcomeFailed
		invocation.ErrorCode = "step_cost_exceeded"
		invocation.ErrorMessage = "workflow step cumulative cost exceeds sub quote"
		return invocation, types.ErrInvalidWorkflow.Wrap(invocation.ErrorMessage)
	}
	if totalDuration > quote.SubSloP95Ms {
		invocation.Outcome = types.WorkflowStepOutcomeFailed
		invocation.ErrorCode = "step_slo_exceeded"
		invocation.ErrorMessage = "workflow step cumulative duration exceeded sub-SLO"
		return invocation, types.ErrInvalidWorkflow.Wrap(invocation.ErrorMessage)
	}
	return invocation, nil
}

func addWorkflowStepAttemptDuration(total, next uint32) uint32 {
	if ^uint32(0)-total < next {
		return ^uint32(0)
	}
	return total + next
}

func traceWorkflowStepAttributes(req InvokeWorkflowRequest, step workflowExecutionStep, quote types.BundleStepQuote, attempt uint32) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("trace_id", strings.TrimSpace(req.TraceID)),
		attribute.String("bundle_id", strings.TrimSpace(req.Quote.BundleID)),
		attribute.String("workflow_id", strings.TrimSpace(req.Quote.WorkflowID)),
		attribute.String("version", strings.TrimSpace(req.Quote.Version)),
		attribute.String("step_id", strings.TrimSpace(step.id)),
		attribute.StringSlice("depends_on", step.dependsOn),
		attribute.String("tool_id", strings.TrimSpace(quote.ToolID)),
		attribute.String("tool_version", strings.TrimSpace(quote.ToolVersion)),
		attribute.Int64("attempt", int64(attempt)),
	}
}

func recordWorkflowStepSpan(span trace.Span, invocation types.WorkflowStepInvocation, cost math.Int, err error) {
	if span == nil {
		return
	}
	defer span.End()

	span.SetAttributes(
		attribute.String("outcome", strings.TrimSpace(invocation.Outcome)),
		attribute.String("error_code", strings.TrimSpace(invocation.ErrorCode)),
		attribute.String("cost_amount", cost.String()),
		attribute.String("cost_denom", strings.TrimSpace(invocation.Cost.Denom)),
		attribute.Int64("duration_ms", int64(invocation.DurationMS)),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, strings.TrimSpace(invocation.ErrorCode))
	}
}

func currentWorkflowMetrics() *workflowMetrics {
	provider := otel.GetMeterProvider()
	workflowMetricCache.Lock()
	defer workflowMetricCache.Unlock()
	if workflowMetricCache.metrics == nil || workflowMetricCache.provider != provider {
		workflowMetricCache.provider = provider
		workflowMetricCache.metrics = newWorkflowMetrics(provider.Meter(workflowTelemetryName))
	}
	return workflowMetricCache.metrics
}

func newWorkflowMetrics(meter metric.Meter) *workflowMetrics {
	invoked, _ := meter.Int64Counter(
		metricWorkflowsInvokedTotalName,
		metric.WithDescription("Total persisted workflow invocations (labels: workflow_id, version)."),
	)
	outcome, _ := meter.Int64Counter(
		metricWorkflowOutcomeTotalName,
		metric.WithDescription("Total persisted workflow invocation outcomes (labels: workflow_id, version, outcome)."),
	)
	bundleDuration, _ := meter.Float64Histogram(
		metricWorkflowBundleDurationName,
		metric.WithDescription("Workflow invocation wall-clock duration in seconds (labels: workflow_id, version, outcome)."),
		metric.WithUnit("s"),
	)
	stepDuration, _ := meter.Float64Histogram(
		metricWorkflowStepDurationSecondsName,
		metric.WithDescription("Workflow step receipt durations in seconds (labels: workflow_id, version, outcome)."),
		metric.WithUnit("s"),
	)
	bundleCost, _ := meter.Float64Histogram(
		metricWorkflowBundleCostName,
		metric.WithDescription("Workflow bundle cost amount recorded on persisted receipts (labels: workflow_id, version, outcome)."),
	)
	feeFailures, _ := meter.Int64Counter(
		metricWorkflowWastedWorkFeeFailures,
		metric.WithDescription("Total failed workflow wasted-work fee charges after a successful bundle revert (labels: workflow_id, version)."),
	)
	return &workflowMetrics{
		invoked:        invoked,
		outcome:        outcome,
		bundleDuration: bundleDuration,
		stepDuration:   stepDuration,
		bundleCost:     bundleCost,
		feeFailures:    feeFailures,
	}
}

func recordWorkflowInvocationMetrics(ctx context.Context, receipt *types.WorkflowInvocationReceipt, duration time.Duration) {
	if receipt == nil {
		return
	}
	metrics := currentWorkflowMetrics()
	if metrics == nil {
		return
	}
	baseAttrs := workflowMetricAttributes(receipt.WorkflowID, receipt.Version)
	outcomeAttrs := appendWorkflowMetricOutcome(baseAttrs, receipt.Outcome)
	if metrics.invoked != nil {
		metrics.invoked.Add(ctx, 1, metric.WithAttributes(baseAttrs...))
	}
	if metrics.outcome != nil {
		metrics.outcome.Add(ctx, 1, metric.WithAttributes(outcomeAttrs...))
	}
	if metrics.bundleDuration != nil {
		metrics.bundleDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(outcomeAttrs...))
	}
	if metrics.bundleCost != nil {
		metrics.bundleCost.Record(ctx, workflowMetricCoinAmount(receipt.TotalCost), metric.WithAttributes(outcomeAttrs...))
	}
	if metrics.stepDuration == nil {
		return
	}
	for _, step := range receipt.StepReceipts {
		stepAttrs := appendWorkflowMetricOutcome(baseAttrs, step.Outcome)
		metrics.stepDuration.Record(ctx, float64(step.DurationMS)/1000.0, metric.WithAttributes(stepAttrs...))
	}
}

func recordWorkflowWastedWorkFeeChargeFailure(ctx context.Context, receipt *types.WorkflowInvocationReceipt) {
	if receipt == nil {
		return
	}
	metrics := currentWorkflowMetrics()
	if metrics == nil || metrics.feeFailures == nil {
		return
	}
	metrics.feeFailures.Add(ctx, 1, metric.WithAttributes(workflowMetricAttributes(receipt.WorkflowID, receipt.Version)...))
}

func workflowMetricAttributes(workflowID, version string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("workflow_id", workflowMetricLabel(workflowID)),
		attribute.String("version", workflowMetricLabel(version)),
	}
}

func appendWorkflowMetricOutcome(attrs []attribute.KeyValue, outcome string) []attribute.KeyValue {
	out := append([]attribute.KeyValue(nil), attrs...)
	return append(out, attribute.String("outcome", workflowMetricLabel(outcome)))
}

func workflowMetricLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func workflowMetricCoinAmount(coin types.QuoteCoin) float64 {
	amount, ok := math.NewIntFromString(strings.TrimSpace(coin.Amount))
	if !ok {
		return 0
	}
	value, _ := new(big.Float).SetInt(amount.BigInt()).Float64()
	return value
}

func normalizeStepInvocation(step workflowExecutionStep, quote types.BundleStepQuote, result WorkflowStepResult, attempt uint32, execErr error) (types.WorkflowStepInvocation, math.Int, error) {
	outcome := strings.ToLower(strings.TrimSpace(result.Outcome))
	if outcome == "" {
		outcome = types.WorkflowStepOutcomeSuccess
	}
	if execErr != nil {
		outcome = types.WorkflowStepOutcomeFailed
	}
	if !workflowStepOutcomeAllowed(outcome) {
		return invalidStepOutcomeInvocation(step, quote, result, attempt, outcome)
	}
	cost := result.Cost
	if strings.TrimSpace(cost.Denom) == "" && strings.TrimSpace(cost.Amount) == "" {
		cost = zeroQuoteCoin(quote.SubMaxCost.Denom)
	}
	costAmount := quoteCoinAmount(cost)
	maxCost := quoteCoinAmount(quote.SubMaxCost)
	if result.ErrorCode != "" && strings.TrimSpace(result.ErrorCode) != result.ErrorCode {
		return nonCanonicalStepResultInvocation(step, quote, result, attempt, "error_code", result.ErrorCode)
	}
	if result.ErrorMessage != "" && strings.TrimSpace(result.ErrorMessage) != result.ErrorMessage {
		return nonCanonicalStepResultInvocation(step, quote, result, attempt, "error_message", result.ErrorMessage)
	}
	outputClaims, err := types.NormalizeWorkflowOutputClaimsForStep(step.id, result.OutputClaims)
	if err != nil {
		return invalidOutputClaimStepResultInvocation(step, quote, result, attempt, err)
	}
	if outcome == types.WorkflowStepOutcomeSuccess {
		result.ErrorCode = ""
		result.ErrorMessage = ""
	}
	invocation := types.WorkflowStepInvocation{
		StepID:        step.id,
		ToolID:        quote.ToolID,
		ToolVersion:   quote.ToolVersion,
		Outcome:       outcome,
		Cost:          cost,
		DurationMS:    durationMillis(result.Duration),
		AttemptCount:  attempt,
		ErrorCode:     result.ErrorCode,
		ErrorMessage:  result.ErrorMessage,
		FailureAction: effectiveFailureAction(step.step.GetFailureAction()),
		OutputClaims:  outputClaims,
	}
	if execErr != nil && invocation.ErrorMessage == "" {
		invocation.ErrorMessage = execErr.Error()
	}
	if invocation.ErrorCode == "" && invocation.Outcome != types.WorkflowStepOutcomeSuccess {
		invocation.ErrorCode = "workflow_step_failed"
	}
	if _, err := types.NewQuoteCoin(cost.Denom, cost.Amount); err != nil {
		invocation.Outcome = types.WorkflowStepOutcomeFailed
		invocation.Cost = zeroQuoteCoin(quote.SubMaxCost.Denom)
		invocation.ErrorCode = "step_cost_invalid"
		invocation.ErrorMessage = err.Error()
		return invocation, math.ZeroInt(), err
	}
	if costAmount.GT(maxCost) {
		invocation.Outcome = types.WorkflowStepOutcomeFailed
		invocation.ErrorCode = "step_cost_exceeded"
		invocation.ErrorMessage = "workflow step cost exceeds sub quote"
		return invocation, costAmount, types.ErrInvalidWorkflow.Wrap(invocation.ErrorMessage)
	}
	if invocation.DurationMS > quote.SubSloP95Ms {
		invocation.Outcome = types.WorkflowStepOutcomeFailed
		invocation.ErrorCode = "step_slo_exceeded"
		invocation.ErrorMessage = "workflow step exceeded sub-SLO"
		return invocation, costAmount, types.ErrInvalidWorkflow.Wrap(invocation.ErrorMessage)
	}
	if invocation.Outcome == types.WorkflowStepOutcomeSuccess {
		return invocation, costAmount, nil
	}
	return invocation, costAmount, types.ErrInvalidWorkflow.Wrap(invocation.ErrorMessage)
}

func workflowStepOutcomeAllowed(outcome string) bool {
	switch outcome {
	case types.WorkflowStepOutcomeSuccess, types.WorkflowStepOutcomeFailed, types.WorkflowStepOutcomeSkipped, types.WorkflowStepOutcomeError:
		return true
	default:
		return false
	}
}

func invalidStepOutcomeInvocation(step workflowExecutionStep, quote types.BundleStepQuote, result WorkflowStepResult, attempt uint32, outcome string) (types.WorkflowStepInvocation, math.Int, error) {
	message := fmt.Sprintf("invalid workflow step outcome: %q", outcome)
	invocation := types.WorkflowStepInvocation{
		StepID:        step.id,
		ToolID:        quote.ToolID,
		ToolVersion:   quote.ToolVersion,
		Outcome:       types.WorkflowStepOutcomeFailed,
		Cost:          zeroQuoteCoin(quote.SubMaxCost.Denom),
		DurationMS:    durationMillis(result.Duration),
		AttemptCount:  attempt,
		ErrorCode:     "workflow_step_outcome_invalid",
		ErrorMessage:  message,
		FailureAction: effectiveFailureAction(step.step.GetFailureAction()),
	}
	return invocation, math.ZeroInt(), types.ErrInvalidWorkflow.Wrap(message)
}

func nonCanonicalStepResultInvocation(step workflowExecutionStep, quote types.BundleStepQuote, result WorkflowStepResult, attempt uint32, field string, value string) (types.WorkflowStepInvocation, math.Int, error) {
	message := fmt.Sprintf("workflow step result %s must be canonical: %q", field, value)
	invocation := types.WorkflowStepInvocation{
		StepID:        step.id,
		ToolID:        quote.ToolID,
		ToolVersion:   quote.ToolVersion,
		Outcome:       types.WorkflowStepOutcomeFailed,
		Cost:          zeroQuoteCoin(quote.SubMaxCost.Denom),
		DurationMS:    durationMillis(result.Duration),
		AttemptCount:  attempt,
		ErrorCode:     "step_result_noncanonical",
		ErrorMessage:  message,
		FailureAction: effectiveFailureAction(step.step.GetFailureAction()),
	}
	return invocation, math.ZeroInt(), types.ErrInvalidWorkflow.Wrap(message)
}

func invalidOutputClaimStepResultInvocation(step workflowExecutionStep, quote types.BundleStepQuote, result WorkflowStepResult, attempt uint32, err error) (types.WorkflowStepInvocation, math.Int, error) {
	message := fmt.Sprintf("workflow step result output_claims invalid: %v", err)
	invocation := types.WorkflowStepInvocation{
		StepID:        step.id,
		ToolID:        quote.ToolID,
		ToolVersion:   quote.ToolVersion,
		Outcome:       types.WorkflowStepOutcomeFailed,
		Cost:          zeroQuoteCoin(quote.SubMaxCost.Denom),
		DurationMS:    durationMillis(result.Duration),
		AttemptCount:  attempt,
		ErrorCode:     "step_output_claims_invalid",
		ErrorMessage:  message,
		FailureAction: effectiveFailureAction(step.step.GetFailureAction()),
	}
	return invocation, math.ZeroInt(), types.ErrInvalidWorkflow.Wrap(message)
}

func isRetryableStepError(policy *types.RetryPolicy, code string) bool {
	if policy == nil {
		return false
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	for _, retryable := range policy.GetRetryableErrorCodes() {
		if strings.TrimSpace(retryable) == code {
			return true
		}
	}
	return false
}

func effectiveFailureAction(action types.FailureAction) types.FailureAction {
	if action == types.FailureAction_FAILURE_ACTION_UNSPECIFIED {
		return types.FailureAction_FAILURE_ACTION_REVERT_BUNDLE
	}
	return action
}

func isHardStepFailure(code string) bool {
	switch strings.TrimSpace(code) {
	case "step_cost_invalid", "step_cost_exceeded", "step_slo_exceeded":
		return true
	default:
		return false
	}
}

func stepFailureCode(invocation types.WorkflowStepInvocation) string {
	if code := strings.TrimSpace(invocation.ErrorCode); code != "" {
		return code
	}
	return "workflow_step_failed"
}

func stepFailureReason(invocation types.WorkflowStepInvocation, err error) string {
	if msg := strings.TrimSpace(invocation.ErrorMessage); msg != "" {
		return msg
	}
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		return err.Error()
	}
	return stepFailureCode(invocation)
}

func recordWorkflowPartialSkip(receipt *types.WorkflowInvocationReceipt, code, reason string) {
	if receipt == nil {
		return
	}
	if strings.TrimSpace(receipt.FailureCode) == "" {
		receipt.FailureCode = strings.TrimSpace(code)
	}
	if strings.TrimSpace(receipt.FailureReason) == "" {
		receipt.FailureReason = strings.TrimSpace(reason)
	}
	if strings.TrimSpace(receipt.FailureCode) == "" {
		receipt.FailureCode = "workflow_partial_skip"
	}
	if strings.TrimSpace(receipt.FailureReason) == "" {
		receipt.FailureReason = receipt.FailureCode
	}
}

func workflowFallbackSources(steps map[string]workflowExecutionStep) map[string][]string {
	out := make(map[string][]string)
	for stepID, step := range steps {
		fallbackID := workflowFallbackStepID(step)
		if fallbackID == "" {
			continue
		}
		out[fallbackID] = append(out[fallbackID], stepID)
	}
	for fallbackID := range out {
		sort.Strings(out[fallbackID])
	}
	return out
}

func workflowFallbackStepID(step workflowExecutionStep) string {
	if step.step == nil || effectiveFailureAction(step.step.GetFailureAction()) != types.FailureAction_FAILURE_ACTION_FALLBACK_STEP {
		return ""
	}
	return strings.TrimSpace(step.step.GetFallbackStepId())
}

func workflowStepConditionMet(step workflowExecutionStep, executed map[string]types.WorkflowStepInvocation) (bool, error) {
	if step.step == nil {
		return true, nil
	}
	condition, err := types.ParseWorkflowCondition(step.step.GetCondition())
	if err != nil {
		return false, nil
	}
	switch condition.Kind {
	case types.WorkflowConditionKindEmpty, types.WorkflowConditionKindAlways:
		return true, nil
	case types.WorkflowConditionKindNever:
		return false, nil
	}
	if condition.Kind != types.WorkflowConditionKindOutcomeComparison {
		if condition.Kind != types.WorkflowConditionKindOutputClaimComparison {
			return false, nil
		}
		got, ok := executed[condition.StepID]
		if !ok {
			return false, nil
		}
		matches, err := types.EvaluateWorkflowOutputClaimCondition(got, condition)
		if err != nil {
			return false, nil
		}
		return matches, nil
	}
	want := workflowConditionOutcome(condition.Literal.String)
	if want == "" {
		return false, nil
	}
	got, ok := executed[condition.StepID]
	if !ok {
		return false, nil
	}
	matches := got.Outcome == want
	if condition.Operator == types.WorkflowConditionOperatorNotEqual {
		return !matches, nil
	}
	return matches, nil
}

func workflowConditionOutcome(raw string) string {
	switch strings.TrimSpace(raw) {
	case "success":
		return types.WorkflowStepOutcomeSuccess
	case "failed":
		return types.WorkflowStepOutcomeFailed
	case "skipped":
		return types.WorkflowStepOutcomeSkipped
	default:
		return ""
	}
}

func workflowFallbackUntriggered(stepID string, fallbackSources map[string][]string, executed map[string]types.WorkflowStepInvocation) bool {
	sources := fallbackSources[stepID]
	if len(sources) == 0 {
		return false
	}
	allSourcesResolved := true
	for _, sourceID := range sources {
		invocation, ok := executed[sourceID]
		if !ok {
			allSourcesResolved = false
			continue
		}
		if invocation.Outcome == types.WorkflowStepOutcomeFailed {
			return false
		}
	}
	return allSourcesResolved
}

func workflowDependencyBlocksStep(step workflowExecutionStep, executed map[string]types.WorkflowStepInvocation, blocked map[string]struct{}, steps map[string]workflowExecutionStep) bool {
	for _, dep := range step.dependsOn {
		if workflowDependencyTriggersFallback(dep, step.id, executed, steps) {
			continue
		}
		if !workflowDependencySatisfied(dep, executed, blocked, steps) {
			return true
		}
	}
	return false
}

func workflowDependencyTriggersFallback(dep, stepID string, executed map[string]types.WorkflowStepInvocation, steps map[string]workflowExecutionStep) bool {
	invocation, ok := executed[dep]
	if !ok || invocation.Outcome != types.WorkflowStepOutcomeFailed {
		return false
	}
	source, ok := steps[dep]
	return ok && workflowFallbackStepID(source) == stepID
}

func workflowDependencySatisfied(dep string, executed map[string]types.WorkflowStepInvocation, blocked map[string]struct{}, steps map[string]workflowExecutionStep) bool {
	if _, ok := blocked[dep]; ok {
		return false
	}
	invocation, ok := executed[dep]
	if !ok {
		return false
	}
	switch invocation.Outcome {
	case types.WorkflowStepOutcomeSuccess:
		return true
	case types.WorkflowStepOutcomeSkipped:
		return invocation.ErrorCode == workflowFallbackNotTriggeredReason || invocation.ErrorCode == workflowConditionNotMetReason
	case types.WorkflowStepOutcomeFailed:
		source, ok := steps[dep]
		if !ok {
			return false
		}
		fallbackID := workflowFallbackStepID(source)
		if fallbackID == "" {
			return false
		}
		fallback, ok := executed[fallbackID]
		return ok && fallback.Outcome == types.WorkflowStepOutcomeSuccess
	default:
		return false
	}
}

func skippedStepInvocation(step workflowExecutionStep, quote types.BundleStepQuote, reason string) types.WorkflowStepInvocation {
	return types.WorkflowStepInvocation{
		StepID:        step.id,
		ToolID:        quote.ToolID,
		ToolVersion:   quote.ToolVersion,
		Outcome:       types.WorkflowStepOutcomeSkipped,
		Cost:          zeroQuoteCoin(quote.SubMaxCost.Denom),
		ErrorCode:     reason,
		ErrorMessage:  reason,
		FailureAction: effectiveFailureAction(step.step.GetFailureAction()),
	}
}

func actualCriticalPathP95(steps map[string]workflowExecutionStep, executed map[string]types.WorkflowStepInvocation) (uint32, error) {
	normalized := make(map[string]normalizedBundleStep, len(steps))
	for id, step := range steps {
		duration := uint32(0)
		if got, ok := executed[id]; ok && got.Outcome == types.WorkflowStepOutcomeSuccess {
			duration = got.DurationMS
		}
		normalized[id] = normalizedBundleStep{id: id, subSLO: duration, dependsOn: step.dependsOn}
	}
	_, latency, err := computeBundleLatencyPlan(normalized)
	return latency.computedP95MS, err
}

func evaluateWorkflowInvariants(invariants []*types.SafetyInvariant, phase types.InvariantPhase, input types.InvariantEvaluationInput) ([]types.InvariantEvaluationLog, error) {
	logs := make([]types.InvariantEvaluationLog, 0, len(invariants))
	for _, invariant := range invariants {
		log, err := types.EvaluateSafetyInvariant(invariant, phase, input)
		logs = append(logs, log)
		if err != nil {
			return logs, err
		}
	}
	return logs, nil
}

func invariantInputForReceipt(quote *types.BundleQuote, receipt *types.WorkflowInvocationReceipt) types.InvariantEvaluationInput {
	steps := make([]types.InvariantStepResult, 0, len(receipt.StepReceipts))
	for _, step := range receipt.StepReceipts {
		steps = append(steps, types.InvariantStepResult{
			StepID:        step.StepID,
			Outcome:       step.Outcome,
			ErrorCode:     step.ErrorCode,
			FailureAction: step.FailureAction,
		})
	}
	return types.InvariantEvaluationInput{
		TotalCost:    receipt.TotalCost.Amount,
		MaxCost:      quote.TotalMaxCost.Amount,
		Steps:        steps,
		InputsDigest: quote.InputsHash,
	}
}

func persistWorkflowInvocation(ctx context.Context, k *Keeper, record *types.BundleQuoteRecord, receipt *types.WorkflowInvocationReceipt) error {
	if err := receipt.ValidateBasic(); err != nil {
		return err
	}
	record.Consumed = true
	record.Invocation = &types.WorkflowInvocationRecord{
		BundleID:      receipt.BundleID,
		Receipt:       cloneWorkflowInvocationReceipt(receipt),
		UpdatedHeight: sdk.UnwrapSDKContext(ctx).BlockHeight(),
	}
	record.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
	return k.PutBundleQuote(ctx, record)
}

func (k *Keeper) emitWorkflowInvocation(ctx context.Context, receipt *types.WorkflowInvocationReceipt) {
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeBundleInvoked,
			sdk.NewAttribute(types.AttributeKeyBundleID, receipt.BundleID),
			sdk.NewAttribute(types.AttributeKeyWorkflowID, receipt.WorkflowID),
			sdk.NewAttribute(types.AttributeKeyVersion, receipt.Version),
			sdk.NewAttribute(types.AttributeKeyOutcome, receipt.Outcome),
			sdk.NewAttribute(types.AttributeKeyLockID, receipt.LockID),
			sdk.NewAttribute(types.AttributeKeyTraceID, receipt.TraceID),
		),
	)
}

func finalizeWorkflowReceipt(ctx context.Context, req InvokeWorkflowRequest, receipt *types.WorkflowInvocationReceipt) error {
	inputs := workflowReceiptNonDeterministicInputs(ctx, req.Quote, receipt.CompletedAt)
	return types.FinalizeWorkflowReceipt(receipt, types.WorkflowReceiptBuildOptions{
		ExecutorPrivateKey:     req.ReceiptSignerKey,
		ExecutorPubkey:         req.Quote.RouterPubkey,
		NonDeterministicInputs: inputs,
	})
}

func validateWorkflowReceiptAnchorCandidate(receipt *types.WorkflowInvocationReceipt, anchor *types.WorkflowReceiptAnchor) error {
	if anchor == nil {
		return nil
	}
	candidate := *receipt
	candidate.Anchors = append(append([]*types.WorkflowReceiptAnchor(nil), receipt.Anchors...), anchor)
	return candidate.ValidateBasic()
}

func workflowReceiptNonDeterministicInputs(ctx context.Context, quote *types.BundleQuote, completedAt string) []*types.WorkflowNonDeterministicInput {
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	out := []*types.WorkflowNonDeterministicInput{
		types.NewWorkflowNonDeterministicInput("wall_clock.completed_at", "workflow.invoke.completed_at", height, completedAt),
	}
	if quote == nil {
		return out
	}
	quoteHeight := quote.AnchoredHeight
	if quoteHeight <= 0 {
		quoteHeight = height
	}
	out = append(out,
		types.NewWorkflowNonDeterministicInput("random_nonce.bundle_quote", "bundle_quote.nonce", quoteHeight, quote.Nonce),
		types.NewWorkflowNonDeterministicInput("oracle_height.bundle_quote", "bundle_quote.anchored_height", quoteHeight, fmt.Sprintf("%d", quote.AnchoredHeight)),
		types.NewWorkflowNonDeterministicInput("inputs_hash.bundle_quote", "bundle_quote.inputs_hash", quoteHeight, quote.InputsHash),
	)
	return out
}

func invokeNow(ctx context.Context, override time.Time) time.Time {
	if !override.IsZero() {
		return override.UTC()
	}
	return sdk.UnwrapSDKContext(ctx).BlockTime().UTC()
}

func durationMillis(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	ms := d / time.Millisecond
	if d%time.Millisecond != 0 {
		ms++
	}
	if ms > time.Duration(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(ms)
}

func zeroQuoteCoin(denom string) types.QuoteCoin {
	coin, err := types.NewQuoteCoin(denom, "0")
	if err != nil {
		return types.QuoteCoin{Denom: strings.TrimSpace(denom), Amount: "0"}
	}
	return coin
}

func quoteCoinAmount(coin types.QuoteCoin) math.Int {
	amount, ok := math.NewIntFromString(strings.TrimSpace(coin.Amount))
	if !ok {
		return math.ZeroInt()
	}
	return amount
}

func cloneWorkflowInvocationReceipt(in *types.WorkflowInvocationReceipt) *types.WorkflowInvocationReceipt {
	if in == nil {
		return nil
	}
	out := *in
	out.StepReceipts = append([]types.WorkflowStepInvocation(nil), in.StepReceipts...)
	for i := range out.StepReceipts {
		out.StepReceipts[i].ReceiptHash = append([]byte(nil), in.StepReceipts[i].ReceiptHash...)
	}
	out.StepReceiptHashes = cloneByteMatrix(in.StepReceiptHashes)
	out.MerkleRoot = append([]byte(nil), in.MerkleRoot...)
	out.CanonicalStepOrder = append([]string(nil), in.CanonicalStepOrder...)
	out.NonDeterministicInputs = append([]*types.WorkflowNonDeterministicInput(nil), in.NonDeterministicInputs...)
	for i, item := range out.NonDeterministicInputs {
		if item != nil {
			cloned := *item
			cloned.InputHash = append([]byte(nil), item.InputHash...)
			out.NonDeterministicInputs[i] = &cloned
		}
	}
	out.Anchors = append([]*types.WorkflowReceiptAnchor(nil), in.Anchors...)
	for i, anchor := range out.Anchors {
		if anchor != nil {
			cloned := *anchor
			cloned.TxHash = append([]byte(nil), anchor.TxHash...)
			out.Anchors[i] = &cloned
		}
	}
	out.FailureAttributions = append([]*types.WorkflowFailureAttribution(nil), in.FailureAttributions...)
	for i, attr := range out.FailureAttributions {
		if attr != nil {
			cloned := *attr
			cloned.StateSnapshot = append([]byte(nil), attr.StateSnapshot...)
			out.FailureAttributions[i] = &cloned
		}
	}
	out.ExecutorSig = append([]byte(nil), in.ExecutorSig...)
	out.InvariantLogs = append([]types.InvariantEvaluationLog(nil), in.InvariantLogs...)
	return &out
}

func cloneByteMatrix(in [][]byte) [][]byte {
	if in == nil {
		return nil
	}
	out := make([][]byte, len(in))
	for i := range in {
		out[i] = append([]byte(nil), in[i]...)
	}
	return out
}
