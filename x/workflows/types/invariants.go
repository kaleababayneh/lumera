package types

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"lukechampine.com/blake3"
)

const (
	// InvariantResultPass records a successful runtime evaluation.
	InvariantResultPass = "pass"
	// InvariantResultFail records an invariant breach.
	InvariantResultFail = "fail"
	// InvariantResultSkip records an invariant whose phase is not active.
	InvariantResultSkip = "skip"
)

const (
	// Workflow invariant reason codes are emitted in per-evaluation logs.
	InvariantReasonOK                   = ""
	InvariantReasonSkipped              = "workflow_invariant_phase_skipped"
	InvariantReasonInvalid              = "workflow_invariant_invalid"
	InvariantReasonUnsupported          = "workflow_invariant_unsupported"
	InvariantReasonInputsMissing        = "workflow_invariant_inputs_missing"
	InvariantReasonCostExceeded         = "workflow_cost_exceeded"
	InvariantReasonJurisdictionDenied   = "workflow_jurisdiction_denied"
	InvariantReasonStepErrorRevert      = "workflow_step_error_revert"
	InvariantReasonStepMissing          = "workflow_step_missing"
	InvariantReasonStepOutcomeMismatch  = "workflow_step_outcome_mismatch"
	InvariantReasonNoSteps              = "workflow_no_steps"
	InvariantReasonSuccessCountMismatch = "workflow_success_count_mismatch"
)

const (
	// Step outcomes used by the workflow invariant runtime context.
	WorkflowStepOutcomeSuccess = "success"
	WorkflowStepOutcomeFailed  = "failed"
	WorkflowStepOutcomeSkipped = "skipped"
	WorkflowStepOutcomeError   = "error"
)

type invariantExpressionKind uint8

const (
	invariantKindTotalCostCap invariantExpressionKind = iota + 1
	invariantKindTotalCostLiteralCap
	invariantKindJurisdictionAllowList
	invariantKindJurisdictionLiteralSet
	invariantKindAnyStepErrorRevert
	invariantKindStepOutcomeEquals
	invariantKindAllStepsSuccessful
	invariantKindSuccessfulCountEquals
)

var (
	errEmptyInvariantExpression = errors.New("workflow invariant expression is empty")
	errUnsupportedInvariant     = errors.New("unsupported workflow invariant expression")

	invariantIDPattern         = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
	invariantAmountPattern     = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	totalCostCapPattern        = regexp.MustCompile(`^total_cost\s*<=\s*max_cost$`)
	totalCostLiteralCapPattern = regexp.MustCompile(`^total_cost\s*<=\s*((?:0|[1-9][0-9]*)(?:\.[0-9]+)?)$`)
	jurisdictionMembership     = regexp.MustCompile(`^jurisdiction\s+in\s+(.+)$`)
	anyStepErrorRevertPattern  = regexp.MustCompile(`^any\s+step(?:\.outcome|)\s*(?:==|returns)\s*error\s*->\s*(revert_bundle|revert)$`)
	stepOutcomePattern         = regexp.MustCompile(`^steps\.([a-z][a-z0-9_-]{1,63})\.outcome\s*==\s*['"](success|failed|skipped|error)['"]$`)
	allStepsSuccessfulPattern  = regexp.MustCompile(`^all\(step\.outcome\s*==\s*['"]success['"]\s+for\s+step\s+in\s+steps\)$`)
	successfulCountPattern     = regexp.MustCompile(`^count\(successful\(\[(.*)\]\)\)\s*==\s*([0-9]+)$`)
	invariantStepIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
	vacuousTrueFragments       = []string{"|| true", "or true"}
	vacuousFalseFragments      = []string{"&& false", "and false"}
)

// InvariantExpression is the parsed representation of the V1 safety invariant DSL.
type InvariantExpression struct {
	raw  string
	kind invariantExpressionKind
	args []string
}

// Raw returns the canonical trimmed expression text used in evaluation logs.
func (expr InvariantExpression) Raw() string { return expr.raw }

// InvariantStepResult is the runtime status of one workflow step.
type InvariantStepResult struct {
	StepID        string        `json:"step_id"`
	Outcome       string        `json:"outcome"`
	ErrorCode     string        `json:"error_code,omitempty"`
	FailureAction FailureAction `json:"failure_action,omitempty"`
}

// InvariantEvaluationInput contains the data available to lock/verify checks.
type InvariantEvaluationInput struct {
	TotalCost    string                `json:"total_cost,omitempty"`
	MaxCost      string                `json:"max_cost,omitempty"`
	Jurisdiction string                `json:"jurisdiction,omitempty"`
	AllowList    []string              `json:"allow_list,omitempty"`
	Steps        []InvariantStepResult `json:"steps,omitempty"`
	InputsDigest string                `json:"inputs_digest,omitempty"`
}

// InvariantEvaluationLog is the structured audit record emitted for each check.
type InvariantEvaluationLog struct {
	Invariant    string `json:"invariant"`
	Phase        string `json:"phase"`
	InputsDigest string `json:"inputs_digest"`
	Result       string `json:"result"`
	ReasonCode   string `json:"reason_code,omitempty"`
}

// InvariantViolation reports a hard safety invariant failure.
type InvariantViolation struct {
	InvariantID string
	ReasonCode  string
	Log         InvariantEvaluationLog
}

func (e *InvariantViolation) Error() string {
	if e == nil {
		return "workflow invariant violation"
	}
	if e.InvariantID == "" {
		return fmt.Sprintf("workflow invariant violation: %s", e.ReasonCode)
	}
	return fmt.Sprintf("workflow invariant %s violated: %s", e.InvariantID, e.ReasonCode)
}

// ParseInvariantExpression parses the narrow V1 workflow safety invariant DSL.
func ParseInvariantExpression(raw string) (InvariantExpression, error) {
	expr := normalizeInvariantExpression(raw)
	if expr == "" {
		return InvariantExpression{}, errEmptyInvariantExpression
	}
	if err := rejectVacuousInvariant(expr); err != nil {
		return InvariantExpression{}, err
	}

	switch {
	case totalCostCapPattern.MatchString(expr):
		return InvariantExpression{raw: expr, kind: invariantKindTotalCostCap}, nil
	case totalCostLiteralCapPattern.MatchString(expr):
		matches := totalCostLiteralCapPattern.FindStringSubmatch(expr)
		return InvariantExpression{raw: expr, kind: invariantKindTotalCostLiteralCap, args: []string{matches[1]}}, nil
	case expr == "jurisdiction in allow_list":
		return InvariantExpression{raw: expr, kind: invariantKindJurisdictionAllowList}, nil
	case jurisdictionMembership.MatchString(expr):
		matches := jurisdictionMembership.FindStringSubmatch(expr)
		items, err := parseInvariantQuotedList(matches[1])
		if err != nil {
			return InvariantExpression{}, err
		}
		return InvariantExpression{raw: expr, kind: invariantKindJurisdictionLiteralSet, args: items}, nil
	case anyStepErrorRevertPattern.MatchString(expr):
		return InvariantExpression{raw: expr, kind: invariantKindAnyStepErrorRevert}, nil
	case stepOutcomePattern.MatchString(expr):
		matches := stepOutcomePattern.FindStringSubmatch(expr)
		return InvariantExpression{raw: expr, kind: invariantKindStepOutcomeEquals, args: []string{matches[1], matches[2]}}, nil
	case allStepsSuccessfulPattern.MatchString(expr):
		return InvariantExpression{raw: expr, kind: invariantKindAllStepsSuccessful}, nil
	case successfulCountPattern.MatchString(expr):
		matches := successfulCountPattern.FindStringSubmatch(expr)
		steps, err := parseInvariantBareList(matches[1])
		if err != nil {
			return InvariantExpression{}, err
		}
		return InvariantExpression{raw: expr, kind: invariantKindSuccessfulCountEquals, args: append(steps, matches[2])}, nil
	default:
		return InvariantExpression{}, fmt.Errorf("%w: %s", errUnsupportedInvariant, expr)
	}
}

// StaticCheckSafetyInvariant validates syntax, metadata, vacuity, and phase fit.
func StaticCheckSafetyInvariant(invariant *SafetyInvariant) error {
	if invariant == nil {
		return fmt.Errorf("%w: invariant cannot be nil", ErrInvalidWorkflow)
	}
	invariantID := invariant.GetInvariantId()
	if strings.TrimSpace(invariantID) != invariantID || !invariantIDPattern.MatchString(invariantID) {
		return fmt.Errorf("%w: invalid invariant_id %q", ErrInvalidWorkflow, invariant.GetInvariantId())
	}
	if err := validateInvariantPhase(invariant.GetPhase()); err != nil {
		return err
	}
	if err := validateInvariantSeverity(invariant.GetSeverity()); err != nil {
		return err
	}
	if strings.TrimSpace(invariant.GetErrorCode()) == "" {
		return fmt.Errorf("%w: invariant %s error_code is required", ErrInvalidWorkflow, invariant.GetInvariantId())
	}
	if strings.TrimSpace(invariant.GetHintMessage()) == "" {
		return fmt.Errorf("%w: invariant %s hint_message is required", ErrInvalidWorkflow, invariant.GetInvariantId())
	}
	expr, err := ParseInvariantExpression(invariant.GetExpression())
	if err != nil {
		return fmt.Errorf("%w: invariant %s: %v", ErrInvalidWorkflow, invariant.GetInvariantId(), err)
	}
	if err := validateInvariantExpressionPhase(expr, invariant.GetPhase()); err != nil {
		return fmt.Errorf("%w: invariant %s: %v", ErrInvalidWorkflow, invariant.GetInvariantId(), err)
	}
	return nil
}

// StaticCheckSafetyInvariants validates every invariant attached to a workflow card.
func StaticCheckSafetyInvariants(invariants []*SafetyInvariant) error {
	for _, invariant := range invariants {
		if err := StaticCheckSafetyInvariant(invariant); err != nil {
			return err
		}
	}
	return nil
}

// EvaluateSafetyInvariant evaluates one invariant at a lock or verify phase.
func EvaluateSafetyInvariant(invariant *SafetyInvariant, phase InvariantPhase, input InvariantEvaluationInput) (InvariantEvaluationLog, error) {
	if invariant == nil {
		return InvariantEvaluationLog{}, fmt.Errorf("%w: invariant cannot be nil", ErrInvalidWorkflow)
	}
	if err := validateInvariantPhase(phase); err != nil {
		return InvariantEvaluationLog{}, err
	}
	if err := StaticCheckSafetyInvariant(invariant); err != nil {
		return InvariantEvaluationLog{
			Invariant:    invariant.GetExpression(),
			Phase:        InvariantPhaseLabel(phase),
			InputsDigest: input.digest(),
			Result:       InvariantResultFail,
			ReasonCode:   InvariantReasonInvalid,
		}, err
	}
	expr, _ := ParseInvariantExpression(invariant.GetExpression())
	log := InvariantEvaluationLog{
		Invariant:    expr.Raw(),
		Phase:        InvariantPhaseLabel(phase),
		InputsDigest: input.digest(),
		Result:       InvariantResultPass,
	}
	if !invariantAppliesAtPhase(invariant.GetPhase(), phase) {
		log.Result = InvariantResultSkip
		log.ReasonCode = InvariantReasonSkipped
		return log, nil
	}

	ok, reason := expr.evaluate(input)
	if ok {
		return log, nil
	}
	if reason == "" {
		reason = InvariantReasonUnsupported
	}
	log.Result = InvariantResultFail
	log.ReasonCode = reason
	if strings.EqualFold(strings.TrimSpace(invariant.GetSeverity()), "warn") {
		return log, nil
	}
	return log, &InvariantViolation{
		InvariantID: invariant.GetInvariantId(),
		ReasonCode:  reason,
		Log:         log,
	}
}

// EvaluateSafetyInvariants evaluates active invariants and returns all logs.
func EvaluateSafetyInvariants(invariants []*SafetyInvariant, phase InvariantPhase, input InvariantEvaluationInput) ([]InvariantEvaluationLog, error) {
	logs := make([]InvariantEvaluationLog, 0, len(invariants))
	var firstErr error
	for _, invariant := range invariants {
		log, err := EvaluateSafetyInvariant(invariant, phase, input)
		if strings.Compare(log.Result, InvariantResultSkip) != 0 {
			logs = append(logs, log)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return logs, firstErr
}

// InvariantPhaseLabel returns the schema-facing label for a generated phase enum.
func InvariantPhaseLabel(phase InvariantPhase) string {
	switch phase {
	case InvariantPhase_INVARIANT_PHASE_STATIC:
		return "static"
	case InvariantPhase_INVARIANT_PHASE_LOCK:
		return "lock"
	case InvariantPhase_INVARIANT_PHASE_VERIFY:
		return "verify"
	default:
		return "unspecified"
	}
}

func (expr InvariantExpression) evaluate(input InvariantEvaluationInput) (bool, string) {
	switch expr.kind {
	case invariantKindTotalCostCap:
		return evaluateAmountLTE("total_cost", "max_cost", input)
	case invariantKindTotalCostLiteralCap:
		return evaluateAmountLTE("total_cost", expr.args[0], input)
	case invariantKindJurisdictionAllowList:
		if strings.TrimSpace(input.Jurisdiction) == "" || len(input.AllowList) == 0 {
			return false, InvariantReasonInputsMissing
		}
		if containsInvariantString(input.AllowList, input.Jurisdiction) {
			return true, InvariantReasonOK
		}
		return false, InvariantReasonJurisdictionDenied
	case invariantKindJurisdictionLiteralSet:
		if strings.TrimSpace(input.Jurisdiction) == "" {
			return false, InvariantReasonInputsMissing
		}
		if containsInvariantString(expr.args, input.Jurisdiction) {
			return true, InvariantReasonOK
		}
		return false, InvariantReasonJurisdictionDenied
	case invariantKindAnyStepErrorRevert:
		if len(input.Steps) == 0 {
			return false, InvariantReasonNoSteps
		}
		for _, step := range input.Steps {
			if stepHasError(step) {
				return false, InvariantReasonStepErrorRevert
			}
		}
		return true, InvariantReasonOK
	case invariantKindStepOutcomeEquals:
		step, ok := findInvariantStep(input.Steps, expr.args[0])
		if !ok {
			return false, InvariantReasonStepMissing
		}
		if normalizedStepOutcome(step) == expr.args[1] {
			return true, InvariantReasonOK
		}
		return false, InvariantReasonStepOutcomeMismatch
	case invariantKindAllStepsSuccessful:
		if len(input.Steps) == 0 {
			return false, InvariantReasonNoSteps
		}
		for _, step := range input.Steps {
			if normalizedStepOutcome(step) != WorkflowStepOutcomeSuccess {
				return false, InvariantReasonStepOutcomeMismatch
			}
		}
		return true, InvariantReasonOK
	case invariantKindSuccessfulCountEquals:
		want, ok := parseInvariantNonNegativeInt(expr.args[len(expr.args)-1])
		if !ok {
			return false, InvariantReasonInvalid
		}
		count := 0
		for _, stepID := range expr.args[:len(expr.args)-1] {
			step, ok := findInvariantStep(input.Steps, stepID)
			if ok && normalizedStepOutcome(step) == WorkflowStepOutcomeSuccess {
				count++
			}
		}
		if count == want {
			return true, InvariantReasonOK
		}
		return false, InvariantReasonSuccessCountMismatch
	default:
		return false, InvariantReasonUnsupported
	}
}

func evaluateAmountLTE(left, right string, input InvariantEvaluationInput) (bool, string) {
	leftAmount, ok := resolveInvariantAmount(left, input)
	if !ok {
		return false, InvariantReasonInputsMissing
	}
	rightAmount, ok := resolveInvariantAmount(right, input)
	if !ok {
		return false, InvariantReasonInputsMissing
	}
	if leftAmount.Cmp(rightAmount) <= 0 {
		return true, InvariantReasonOK
	}
	return false, InvariantReasonCostExceeded
}

func resolveInvariantAmount(value string, input InvariantEvaluationInput) (*big.Rat, bool) {
	switch value {
	case "total_cost":
		return parseInvariantAmount(input.TotalCost)
	case "max_cost":
		return parseInvariantAmount(input.MaxCost)
	default:
		return parseInvariantAmount(value)
	}
}

func parseInvariantAmount(raw string) (*big.Rat, bool) {
	raw = strings.TrimSpace(raw)
	if !invariantAmountPattern.MatchString(raw) {
		return nil, false
	}
	amount, ok := new(big.Rat).SetString(raw)
	return amount, ok
}

func normalizeInvariantExpression(raw string) string {
	expr := strings.TrimSpace(raw)
	expr = strings.ReplaceAll(expr, "\u2192", "->")
	return expr
}

func rejectVacuousInvariant(expr string) error {
	normalized := strings.ToLower(strings.Join(strings.Fields(expr), " "))
	switch normalized {
	case "true", "1 == 1", "total_cost <= total_cost", "max_cost <= max_cost":
		return fmt.Errorf("vacuously true invariant rejected: %s", expr)
	case "false", "1 == 0", "total_cost < total_cost", "max_cost < max_cost":
		return fmt.Errorf("vacuously false invariant rejected: %s", expr)
	}
	for _, fragment := range vacuousTrueFragments {
		if strings.Contains(normalized, fragment) {
			return fmt.Errorf("vacuously true invariant rejected: %s", expr)
		}
	}
	for _, fragment := range vacuousFalseFragments {
		if strings.Contains(normalized, fragment) {
			return fmt.Errorf("vacuously false invariant rejected: %s", expr)
		}
	}
	return nil
}

func parseInvariantQuotedList(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: empty literal set", errUnsupportedInvariant)
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("%w: literal set must be a JSON string array", errUnsupportedInvariant)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: empty literal set", errUnsupportedInvariant)
	}
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%w: literal set contains empty value", errUnsupportedInvariant)
		}
		if value == "*" {
			return nil, fmt.Errorf("vacuously true invariant rejected: jurisdiction wildcard")
		}
		values[i] = value
		seen[value] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("%w: empty literal set", errUnsupportedInvariant)
	}
	return values, nil
}

func parseInvariantBareList(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%w: empty step list", errUnsupportedInvariant)
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if !invariantStepIDPattern.MatchString(value) {
			return nil, fmt.Errorf("%w: invalid step id %q", errUnsupportedInvariant, value)
		}
		values = append(values, value)
	}
	return values, nil
}

func validateInvariantPhase(phase InvariantPhase) error {
	switch phase {
	case InvariantPhase_INVARIANT_PHASE_STATIC,
		InvariantPhase_INVARIANT_PHASE_LOCK,
		InvariantPhase_INVARIANT_PHASE_VERIFY:
		return nil
	default:
		return fmt.Errorf("%w: invalid invariant phase %s", ErrInvalidWorkflow, InvariantPhaseLabel(phase))
	}
}

func validateInvariantSeverity(severity string) error {
	switch severity {
	case "error", "warn":
		return nil
	default:
		return fmt.Errorf("%w: invalid invariant severity %q", ErrInvalidWorkflow, severity)
	}
}

func validateInvariantExpressionPhase(expr InvariantExpression, phase InvariantPhase) error {
	if phase == InvariantPhase_INVARIANT_PHASE_VERIFY {
		return nil
	}
	switch expr.kind {
	case invariantKindAnyStepErrorRevert,
		invariantKindStepOutcomeEquals,
		invariantKindAllStepsSuccessful,
		invariantKindSuccessfulCountEquals:
		return fmt.Errorf("expression requires verify phase")
	default:
		return nil
	}
}

func invariantAppliesAtPhase(invariantPhase, runtimePhase InvariantPhase) bool {
	switch runtimePhase {
	case InvariantPhase_INVARIANT_PHASE_LOCK:
		return invariantPhase == InvariantPhase_INVARIANT_PHASE_STATIC ||
			invariantPhase == InvariantPhase_INVARIANT_PHASE_LOCK
	case InvariantPhase_INVARIANT_PHASE_VERIFY:
		return invariantPhase == InvariantPhase_INVARIANT_PHASE_STATIC ||
			invariantPhase == InvariantPhase_INVARIANT_PHASE_LOCK ||
			invariantPhase == InvariantPhase_INVARIANT_PHASE_VERIFY
	case InvariantPhase_INVARIANT_PHASE_STATIC:
		return invariantPhase == InvariantPhase_INVARIANT_PHASE_STATIC
	default:
		return false
	}
}

func stepHasError(step InvariantStepResult) bool {
	outcome := normalizedStepOutcome(step)
	return outcome == WorkflowStepOutcomeFailed ||
		outcome == WorkflowStepOutcomeError ||
		strings.TrimSpace(step.ErrorCode) != ""
}

func normalizedStepOutcome(step InvariantStepResult) string {
	outcome := strings.ToLower(strings.TrimSpace(step.Outcome))
	if outcome == WorkflowStepOutcomeError {
		return WorkflowStepOutcomeError
	}
	if outcome == WorkflowStepOutcomeFailed || strings.TrimSpace(step.ErrorCode) != "" {
		return WorkflowStepOutcomeFailed
	}
	if outcome == WorkflowStepOutcomeSkipped {
		return WorkflowStepOutcomeSkipped
	}
	return outcome
}

func findInvariantStep(steps []InvariantStepResult, stepID string) (InvariantStepResult, bool) {
	for _, step := range steps {
		if step.StepID == stepID {
			return step, true
		}
	}
	return InvariantStepResult{}, false
}

func parseInvariantNonNegativeInt(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	value := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		value = value*10 + int(ch-'0')
	}
	return value, true
}

func containsInvariantString(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

func (input InvariantEvaluationInput) digest() string {
	if strings.TrimSpace(input.InputsDigest) != "" {
		return strings.TrimSpace(input.InputsDigest)
	}
	normalized := struct {
		TotalCost    string                `json:"total_cost,omitempty"`
		MaxCost      string                `json:"max_cost,omitempty"`
		Jurisdiction string                `json:"jurisdiction,omitempty"`
		AllowList    []string              `json:"allow_list,omitempty"`
		Steps        []InvariantStepResult `json:"steps,omitempty"`
	}{
		TotalCost:    strings.TrimSpace(input.TotalCost),
		MaxCost:      strings.TrimSpace(input.MaxCost),
		Jurisdiction: strings.TrimSpace(input.Jurisdiction),
		AllowList:    append([]string(nil), input.AllowList...),
		Steps:        append([]InvariantStepResult(nil), input.Steps...),
	}
	sort.Strings(normalized.AllowList)
	sort.Slice(normalized.Steps, func(i, j int) bool {
		return normalized.Steps[i].StepID < normalized.Steps[j].StepID
	})
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "blake3:invalid"
	}
	sum := blake3.Sum256(raw)
	return "blake3:" + hex.EncodeToString(sum[:])
}
