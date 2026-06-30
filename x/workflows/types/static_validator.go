package types

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/internal/logging"
	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
)

const (
	WorkflowStaticSeverityError   = "error"
	WorkflowStaticSeverityWarning = "warning"
)

const (
	WorkflowStaticReasonCardRequired                = "workflow_static_card_required"
	WorkflowStaticReasonDAGEmpty                    = "workflow_dag_empty"
	WorkflowStaticReasonStepNil                     = "workflow_step_nil"
	WorkflowStaticReasonStepIDEmpty                 = "workflow_step_id_empty"
	WorkflowStaticReasonStepIDInvalid               = "workflow_step_id_invalid"
	WorkflowStaticReasonStepDuplicate               = "workflow_step_duplicate"
	WorkflowStaticReasonStepToolIDEmpty             = "workflow_step_tool_id_empty"
	WorkflowStaticReasonStepToolIDInvalid           = "workflow_step_tool_id_invalid"
	WorkflowStaticReasonStepVersionInvalid          = "workflow_step_version_constraint_invalid"
	WorkflowStaticReasonStepInputBindingEmpty       = "workflow_step_input_binding_empty"
	WorkflowStaticReasonStepDependencyInvalid       = "workflow_step_dependency_invalid"
	WorkflowStaticReasonStepDependencyUnknown       = "workflow_step_dependency_unknown"
	WorkflowStaticReasonStepDependencySelf          = "workflow_step_dependency_self"
	WorkflowStaticReasonStepFallbackRequired        = "workflow_step_fallback_required"
	WorkflowStaticReasonStepFallbackInvalid         = "workflow_step_fallback_invalid"
	WorkflowStaticReasonStepFallbackUnknown         = "workflow_step_fallback_unknown"
	WorkflowStaticReasonStepFallbackSelf            = "workflow_step_fallback_self"
	WorkflowStaticReasonStepFailureActionInvalid    = "workflow_step_failure_action_invalid"
	WorkflowStaticReasonStepSideEffectInvalid       = "workflow_step_side_effect_invalid"
	WorkflowStaticReasonStepSideEffectNonTerminal   = "workflow_step_side_effect_non_terminal"
	WorkflowStaticReasonStepMaxSubCostInvalid       = "workflow_step_max_sub_cost_invalid"
	WorkflowStaticReasonStepSubSLOInvalid           = "workflow_step_sub_slo_invalid"
	WorkflowStaticReasonStepRetryPolicyInvalid      = "workflow_step_retry_policy_invalid"
	WorkflowStaticReasonDAGCycle                    = "workflow_dag_cycle"
	WorkflowStaticReasonConditionDeadBranch         = "workflow_step_condition_dead_branch"
	WorkflowStaticReasonConditionSelfReference      = "workflow_step_condition_self_reference"
	WorkflowStaticReasonConditionUnsupported        = "workflow_step_condition_unsupported"
	WorkflowStaticReasonConditionUnknownStep        = "workflow_step_condition_unknown_step"
	WorkflowStaticReasonWorkflowInputUnused         = "workflow_input_unused"
	WorkflowStaticReasonVersionInvalid              = "workflow_version_invalid"
	WorkflowStaticReasonSafetyInvariantMalformed    = "workflow_safety_invariant_malformed"
	WorkflowStaticReasonToolResolutionFailed        = "workflow_tool_resolution_failed"
	WorkflowStaticReasonStepToolNotFound            = "workflow_step_tool_not_found"
	WorkflowStaticReasonStepToolVersionMismatch     = "workflow_step_tool_version_mismatch"
	WorkflowStaticReasonStepToolInputMalformed      = "workflow_step_tool_input_schema_malformed"
	WorkflowStaticReasonStepToolOutputMalformed     = "workflow_step_tool_output_schema_malformed"
	WorkflowStaticReasonStepInputUnknownInput       = "workflow_step_input_binding_unknown_input"
	WorkflowStaticReasonStepInputUnknownStep        = "workflow_step_input_binding_unknown_step"
	WorkflowStaticReasonStepInputMissingDep         = "workflow_step_input_binding_missing_dependency"
	WorkflowStaticReasonStepInputSchemaMismatch     = "workflow_step_input_schema_mismatch"
	WorkflowStaticReasonWorkflowInputSchemaInvalid  = "workflow_input_schema_invalid"
	WorkflowStaticReasonWorkflowOutputSchemaInvalid = "workflow_output_schema_invalid"
	WorkflowStaticReasonPricingRequired             = "workflow_pricing_required"
	WorkflowStaticReasonPricingModelInvalid         = "workflow_pricing_model_invalid"
	WorkflowStaticReasonPricingMinBondInvalid       = "workflow_pricing_min_bond_invalid"
	WorkflowStaticReasonPricingBPSInvalid           = "workflow_pricing_bps_invalid"
	WorkflowStaticReasonPassportRequired            = "workflow_passport_requirements_required"
	WorkflowStaticReasonPassportTierInvalid         = "workflow_passport_tier_invalid"
	WorkflowStaticReasonPassportReputationInvalid   = "workflow_passport_reputation_invalid"
	WorkflowStaticReasonPassportBadgeInvalid        = "workflow_passport_badge_invalid"
	WorkflowStaticReasonGovernanceRequired          = "workflow_governance_required"
	WorkflowStaticReasonGovernanceAuthorInvalid     = "workflow_governance_author_invalid"
	WorkflowStaticReasonGovernancePolicyInvalid     = "workflow_governance_policy_invalid"
	WorkflowStaticReasonMetadataInvalid             = "workflow_metadata_invalid"
	WorkflowStaticReasonCategoriesInvalid           = "workflow_categories_invalid"
	WorkflowStaticReasonLicenseLaneInvalid          = "workflow_license_lane_invalid"
)

var (
	workflowInputSchemaNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	workflowInputRefPattern        = regexp.MustCompile(`\$\.(?:inputs)\.([A-Za-z_][A-Za-z0-9_-]*)`)
	workflowStepOutputRefPattern   = regexp.MustCompile(`\$\.(?:steps)\.([A-Za-z0-9_-]+)\.output(?:\.([A-Za-z_][A-Za-z0-9_-]*))?`)
	workflowStepIDPattern          = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
	workflowVersionPattern         = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	workflowAuthorPubkeyPattern    = regexp.MustCompile(`^(ed448:)?[0-9a-fA-F]{114}$`)
	workflowPricingModels          = map[string]struct{}{
		"fixed_bundle":          {},
		"sum_steps_plus_margin": {},
		"tiered_bundle":         {},
	}
	workflowLicenseLanes = map[string]struct{}{
		"byo_key":         {},
		"licensed_resale": {},
		"community":       {},
	}
)

// WorkflowToolDescriptor is the ToolCard surface needed for static workflow checks.
type WorkflowToolDescriptor struct {
	ToolID       string
	Version      string
	InputSchema  string
	OutputSchema string
}

// WorkflowToolResolver resolves exact ToolCard versions for workflow steps.
type WorkflowToolResolver interface {
	ResolveWorkflowTool(toolID, exactVersion string) (WorkflowToolDescriptor, bool, error)
}

// WorkflowToolVersionLister optionally exposes available ToolCard versions for diagnostics.
type WorkflowToolVersionLister interface {
	WorkflowToolVersions(toolID string) []string
}

// WorkflowToolResolverFunc adapts a function to WorkflowToolResolver.
type WorkflowToolResolverFunc func(toolID, exactVersion string) (WorkflowToolDescriptor, bool, error)

func (fn WorkflowToolResolverFunc) ResolveWorkflowTool(toolID, exactVersion string) (WorkflowToolDescriptor, bool, error) {
	return fn(toolID, exactVersion)
}

type registryWorkflowToolResolver struct {
	byKey    map[string]WorkflowToolDescriptor
	versions map[string][]string
}

// WorkflowCardStaticFinding is one deterministic publish-time validator result.
type WorkflowCardStaticFinding struct {
	Severity   string
	ReasonCode string
	StepID     string
	Field      string
	Message    string
}

// WorkflowCardStaticValidationError reports hard static validation failures.
type WorkflowCardStaticValidationError struct {
	Findings []WorkflowCardStaticFinding
}

func (e *WorkflowCardStaticValidationError) Error() string {
	if e == nil || len(e.Findings) == 0 {
		return "workflow static validation failed"
	}
	first := e.Findings[0]
	if len(e.Findings) == 1 {
		return fmt.Sprintf("workflow static validation failed: %s: %s", first.ReasonCode, first.Message)
	}
	return fmt.Sprintf("workflow static validation failed: %s: %s (+%d more)", first.ReasonCode, first.Message, len(e.Findings)-1)
}

func (e *WorkflowCardStaticValidationError) Unwrap() error {
	return ErrInvalidWorkflow
}

// StaticCheckWorkflowCard rejects malformed publish-time WorkflowCard DAGs.
func StaticCheckWorkflowCard(card *WorkflowCard) error {
	findings := AnalyzeWorkflowCard(card)
	return workflowStaticValidationError(findings)
}

// StaticCheckWorkflowCardWithToolResolver rejects malformed WorkflowCards using ToolCard context.
func StaticCheckWorkflowCardWithToolResolver(card *WorkflowCard, resolver WorkflowToolResolver) error {
	findings := AnalyzeWorkflowCardWithToolResolver(card, resolver)
	return workflowStaticValidationError(findings)
}

func workflowStaticValidationError(findings []WorkflowCardStaticFinding) error {
	errorsOnly := make([]WorkflowCardStaticFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Severity == WorkflowStaticSeverityError {
			errorsOnly = append(errorsOnly, finding)
		}
	}
	if len(errorsOnly) == 0 {
		return nil
	}
	return &WorkflowCardStaticValidationError{Findings: errorsOnly}
}

// NewRegistryWorkflowToolResolver builds a resolver from registry ToolCards.
func NewRegistryWorkflowToolResolver(tools ...*registrytypes.ToolCard) WorkflowToolResolver {
	resolver := registryWorkflowToolResolver{
		byKey:    make(map[string]WorkflowToolDescriptor, len(tools)),
		versions: make(map[string][]string),
	}
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		toolID := strings.TrimSpace(tool.GetToolId())
		version := strings.TrimSpace(tool.GetVersion())
		if toolID == "" || version == "" {
			continue
		}
		descriptor := WorkflowToolDescriptor{
			ToolID:       toolID,
			Version:      version,
			InputSchema:  tool.GetInputSchema(),
			OutputSchema: tool.GetOutputSchema(),
		}
		resolver.byKey[workflowToolKey(toolID, version)] = descriptor
		resolver.versions[toolID] = append(resolver.versions[toolID], version)
	}
	for toolID := range resolver.versions {
		sort.Strings(resolver.versions[toolID])
	}
	return resolver
}

func (r registryWorkflowToolResolver) ResolveWorkflowTool(toolID, exactVersion string) (WorkflowToolDescriptor, bool, error) {
	tool, ok := r.byKey[workflowToolKey(toolID, exactVersion)]
	return tool, ok, nil
}

func (r registryWorkflowToolResolver) WorkflowToolVersions(toolID string) []string {
	versions := r.versions[strings.TrimSpace(toolID)]
	return append([]string(nil), versions...)
}

// AnalyzeWorkflowCard returns deterministic errors and warnings for a WorkflowCard.
func AnalyzeWorkflowCard(card *WorkflowCard) []WorkflowCardStaticFinding {
	var findings []WorkflowCardStaticFinding
	if card == nil {
		return []WorkflowCardStaticFinding{{
			Severity:   WorkflowStaticSeverityError,
			ReasonCode: WorkflowStaticReasonCardRequired,
			Field:      "workflow_card",
			Message:    "workflow_card is required",
		}}
	}

	if err := validateWorkflowCardVersion(card.GetVersion()); err != nil {
		findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonVersionInvalid, "", "version", err.Error()))
	}

	steps := card.GetDag()
	if len(steps) == 0 {
		findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonDAGEmpty, "", "dag", "workflow DAG cannot be empty"))
	}

	stepsByID := make(map[string]*Step, len(steps))
	stepIDs := make([]string, 0, len(steps))
	for idx, step := range steps {
		if step == nil {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepNil, "", "dag", fmt.Sprintf("workflow step at index %d cannot be nil", idx)))
			continue
		}
		rawStepID := step.GetStepId()
		stepID := strings.TrimSpace(rawStepID)
		if stepID == "" {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepIDEmpty, "", "step_id", "workflow step_id cannot be empty"))
			continue
		}
		if !isCanonicalWorkflowStepID(rawStepID) {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepIDInvalid, stepID, "step_id", fmt.Sprintf("workflow step_id must match %s: %q", workflowStepIDPattern.String(), rawStepID)))
			continue
		}
		if _, exists := stepsByID[stepID]; exists {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepDuplicate, stepID, "step_id", fmt.Sprintf("duplicate workflow step_id %q", stepID)))
			continue
		}
		stepsByID[stepID] = step
		stepIDs = append(stepIDs, stepID)
	}
	sort.Strings(stepIDs)

	graph := make(map[string][]string, len(stepIDs))
	indegree := make(map[string]int, len(stepIDs))
	inputBindings := make([]string, 0, len(stepIDs))
	for _, stepID := range stepIDs {
		step := stepsByID[stepID]
		indegree[stepID] = 0
		toolID := strings.TrimSpace(step.GetToolId())
		if toolID == "" {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepToolIDEmpty, stepID, "tool_id", "workflow step tool_id cannot be empty"))
		} else if toolID != step.GetToolId() {
			findings = append(findings, workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonStepToolIDInvalid,
				stepID,
				"tool_id",
				fmt.Sprintf("workflow step tool_id must be canonical: %q", step.GetToolId()),
			))
		}
		if err := validateExactToolVersionConstraint(step.GetToolVersionConstraint()); err != nil {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepVersionInvalid, stepID, "tool_version_constraint", err.Error()))
		}
		binding := strings.TrimSpace(step.GetInputBinding())
		switch {
		case binding == "":
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepInputBindingEmpty, stepID, "input_binding", "workflow step input_binding cannot be empty"))
		case binding != step.GetInputBinding():
			findings = append(findings, workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonStepInputBindingEmpty,
				stepID,
				"input_binding",
				fmt.Sprintf("workflow step input_binding must be canonical: %q", step.GetInputBinding()),
			))
		default:
			inputBindings = append(inputBindings, binding)
		}
		if err := validateWorkflowStepSideEffect(step.GetSideEffect()); err != nil {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepSideEffectInvalid, stepID, "side_effect", err.Error()))
		}
		if err := validateWorkflowStepFailureAction(step.GetFailureAction()); err != nil {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepFailureActionInvalid, stepID, "failure_action", err.Error()))
		}
		findings = append(findings, analyzeWorkflowStepExecutionPolicy(step)...)
		findings = append(findings, analyzeFallbackStep(step, stepsByID)...)
		findings = append(findings, analyzeStaticCondition(step)...)
		findings = append(findings, analyzeConditionStepReferences(step, stepsByID)...)

		deps, depFindings := canonicalWorkflowStepDependencies(step.GetDependsOn(), stepID)
		findings = append(findings, depFindings...)
		for _, dep := range deps {
			if dep == stepID {
				findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepDependencySelf, stepID, "depends_on", fmt.Sprintf("workflow step %s depends on itself", stepID)))
				continue
			}
			if _, ok := stepsByID[dep]; !ok {
				findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepDependencyUnknown, stepID, "depends_on", fmt.Sprintf("workflow step %s depends on unknown step %s", stepID, dep)))
				continue
			}
			graph[dep] = append(graph[dep], stepID)
			indegree[stepID]++
		}
	}
	addImplicitFallbackStepEdges(card.GetDag(), stepsByID, graph, indegree)
	addImplicitConditionStepEdges(card.GetDag(), stepsByID, graph, indegree)
	findings = append(findings, analyzeWorkflowNonReversibleLeaves(stepIDs, stepsByID, graph)...)
	findings = append(findings, analyzeWorkflowDAGCycles(stepIDs, graph, indegree)...)
	findings = append(findings, analyzeWorkflowSchemas(card)...)
	findings = append(findings, analyzeUnusedWorkflowInputs(card.GetInputSchema(), inputBindings)...)
	findings = append(findings, analyzeWorkflowMetadata(card)...)
	findings = append(findings, analyzeWorkflowCategories(card.GetCategories())...)
	findings = append(findings, analyzeWorkflowLicenseLane(card.GetLicenseLane())...)
	findings = append(findings, analyzeWorkflowPricing(card.GetPricing())...)
	findings = append(findings, analyzePassportRequirements(card.GetPassportRequirements())...)
	findings = append(findings, analyzeWorkflowGovernance(card.GetGovernance())...)

	if len(card.GetSafetyInvariants()) == 0 {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonSafetyInvariantMalformed,
			"",
			"safety_invariants",
			"workflow safety_invariants must contain at least one invariant",
		))
	} else if err := StaticCheckSafetyInvariants(card.GetSafetyInvariants()); err != nil {
		findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonSafetyInvariantMalformed, "", "safety_invariants", err.Error()))
	}

	sortWorkflowStaticFindings(findings)
	return findings
}

// AnalyzeWorkflowCardWithToolResolver adds ToolCard resolution and schema checks.
func AnalyzeWorkflowCardWithToolResolver(card *WorkflowCard, resolver WorkflowToolResolver) []WorkflowCardStaticFinding {
	findings := AnalyzeWorkflowCard(card)
	if card == nil || resolver == nil {
		return findings
	}
	findings = append(findings, analyzeWorkflowToolResolution(card, resolver)...)
	sortWorkflowStaticFindings(findings)
	return findings
}

func workflowStaticFinding(severity, reason, stepID, field, message string) WorkflowCardStaticFinding {
	return WorkflowCardStaticFinding{
		Severity:   severity,
		ReasonCode: reason,
		StepID:     stepID,
		Field:      field,
		Message:    message,
	}
}

func validateWorkflowCardVersion(raw string) error {
	if !workflowVersionPattern.MatchString(raw) {
		return fmt.Errorf("workflow version must match %s: %q", workflowVersionPattern.String(), raw)
	}
	return nil
}

func validateExactToolVersionConstraint(raw string) error {
	_, err := canonicalExactToolVersion(raw)
	return err
}

func validateWorkflowStepSideEffect(effect SideEffect) error {
	switch effect {
	case SideEffect_SIDE_EFFECT_REVERSIBLE, SideEffect_SIDE_EFFECT_NON_REVERSIBLE:
		return nil
	case SideEffect_SIDE_EFFECT_UNSPECIFIED:
		return fmt.Errorf("workflow step side_effect must be reversible or non_reversible")
	default:
		return fmt.Errorf("workflow step side_effect has unknown enum value %d", effect)
	}
}

func validateWorkflowStepFailureAction(action FailureAction) error {
	switch action {
	case FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
		FailureAction_FAILURE_ACTION_SKIP_DOWNSTREAM,
		FailureAction_FAILURE_ACTION_FALLBACK_STEP:
		return nil
	case FailureAction_FAILURE_ACTION_UNSPECIFIED:
		return fmt.Errorf("workflow step failure_action must be revert_bundle, skip_downstream, or fallback_step")
	default:
		return fmt.Errorf("workflow step failure_action has unknown enum value %d", action)
	}
}

func analyzeWorkflowSchemas(card *WorkflowCard) []WorkflowCardStaticFinding {
	findings := make([]WorkflowCardStaticFinding, 0)
	if _, err := parseWorkflowJSONObjectSchema(card.GetInputSchema()); err != nil {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonWorkflowInputSchemaInvalid,
			"",
			"input_schema",
			fmt.Sprintf("workflow input_schema must be a non-empty JSON object schema: %v", err),
		))
	}
	if _, err := parseWorkflowJSONObjectSchema(card.GetOutputSchema()); err != nil {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonWorkflowOutputSchemaInvalid,
			"",
			"output_schema",
			fmt.Sprintf("workflow output_schema must be a non-empty JSON object schema: %v", err),
		))
	}
	return findings
}

func analyzeWorkflowStepExecutionPolicy(step *Step) []WorkflowCardStaticFinding {
	stepID := strings.TrimSpace(step.GetStepId())
	findings := make([]WorkflowCardStaticFinding, 0)
	if err := validateCoin(fmt.Sprintf("workflow step %s max_sub_cost", stepID), step.GetMaxSubCost(), true); err != nil {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonStepMaxSubCostInvalid,
			stepID,
			"max_sub_cost",
			err.Error(),
		))
	}
	if step.GetSubSloP95Ms() == 0 {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonStepSubSLOInvalid,
			stepID,
			"sub_slo_p95_ms",
			fmt.Sprintf("workflow step %s sub_slo_p95_ms must be positive", stepID),
		))
	}
	findings = append(findings, analyzeWorkflowStepRetryPolicy(stepID, step.GetRetryPolicy())...)
	return findings
}

func analyzeWorkflowStepRetryPolicy(stepID string, policy *RetryPolicy) []WorkflowCardStaticFinding {
	if policy == nil {
		return []WorkflowCardStaticFinding{workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonStepRetryPolicyInvalid,
			stepID,
			"retry_policy",
			fmt.Sprintf("workflow step %s retry_policy is required", stepID),
		)}
	}
	findings := make([]WorkflowCardStaticFinding, 0)
	if policy.GetMaxAttempts() == 0 {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonStepRetryPolicyInvalid,
			stepID,
			"retry_policy.max_attempts",
			fmt.Sprintf("workflow step %s retry_policy.max_attempts must be positive", stepID),
		))
	}
	if multiplier := policy.GetBackoffMultiplier(); math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonStepRetryPolicyInvalid,
			stepID,
			"retry_policy.backoff_multiplier",
			fmt.Sprintf("workflow step %s retry_policy.backoff_multiplier must be a finite number", stepID),
		))
	} else if multiplier != 0 && multiplier < 1 {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonStepRetryPolicyInvalid,
			stepID,
			"retry_policy.backoff_multiplier",
			fmt.Sprintf("workflow step %s retry_policy.backoff_multiplier must be >= 1 when set", stepID),
		))
	}
	for _, code := range policy.GetRetryableErrorCodes() {
		canonical := strings.TrimSpace(code)
		if canonical == "" || canonical != code {
			findings = append(findings, workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonStepRetryPolicyInvalid,
				stepID,
				"retry_policy.retryable_error_codes",
				fmt.Sprintf("workflow step %s retryable error codes must be non-empty canonical strings: %q", stepID, code),
			))
			break
		}
	}
	return findings
}

func analyzeWorkflowPricing(pricing *WorkflowPricing) []WorkflowCardStaticFinding {
	if pricing == nil {
		return []WorkflowCardStaticFinding{workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPricingRequired,
			"",
			"pricing",
			"workflow pricing is required",
		)}
	}
	findings := make([]WorkflowCardStaticFinding, 0)
	model := strings.TrimSpace(pricing.GetPricingModel())
	if model == "" || model != pricing.GetPricingModel() {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPricingModelInvalid,
			"",
			"pricing.pricing_model",
			fmt.Sprintf("workflow pricing_model must be one of fixed_bundle, sum_steps_plus_margin, tiered_bundle: %q", pricing.GetPricingModel()),
		))
	} else if _, ok := workflowPricingModels[model]; !ok {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPricingModelInvalid,
			"",
			"pricing.pricing_model",
			fmt.Sprintf("workflow pricing_model must be one of fixed_bundle, sum_steps_plus_margin, tiered_bundle: %q", pricing.GetPricingModel()),
		))
	}
	if err := validateCoin("workflow pricing min_bond", pricing.GetMinBond(), true); err != nil {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPricingMinBondInvalid,
			"",
			"pricing.min_bond",
			err.Error(),
		))
	}
	if err := validateCoin("workflow pricing minimum_cost", pricing.GetMinimumCost(), false); err != nil {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPricingMinBondInvalid,
			"",
			"pricing.minimum_cost",
			err.Error(),
		))
	}
	if err := validateCoin("workflow pricing maximum_cost", pricing.GetMaximumCost(), false); err != nil {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPricingMinBondInvalid,
			"",
			"pricing.maximum_cost",
			err.Error(),
		))
	}
	minimumCost := pricing.GetMinimumCost()
	maximumCost := pricing.GetMaximumCost()
	if !workflowCoinUnset(minimumCost) && !workflowCoinUnset(maximumCost) && minimumCost.GetDenom() == maximumCost.GetDenom() {
		if !minimumCost.Amount.IsNil() && !maximumCost.Amount.IsNil() && minimumCost.Amount.GT(maximumCost.Amount) {
			findings = append(findings, workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonPricingMinBondInvalid,
				"",
				"pricing.minimum_cost",
				"workflow pricing minimum_cost must be <= maximum_cost",
			))
		}
	}
	if pricing.GetAuthorMarginBps() > BPSDenominator {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPricingBPSInvalid,
			"",
			"pricing.author_margin_bps",
			fmt.Sprintf("workflow author_margin_bps must be <= %d", BPSDenominator),
		))
	}
	if pricing.GetInsuranceBps() > BPSDenominator {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPricingBPSInvalid,
			"",
			"pricing.insurance_bps",
			fmt.Sprintf("workflow insurance_bps must be <= %d", BPSDenominator),
		))
	}
	return findings
}

func analyzePassportRequirements(requirements *PassportRequirements) []WorkflowCardStaticFinding {
	if requirements == nil {
		return []WorkflowCardStaticFinding{workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPassportRequired,
			"",
			"passport_requirements",
			"workflow passport_requirements is required",
		)}
	}
	findings := make([]WorkflowCardStaticFinding, 0)
	switch requirements.GetMinTier() {
	case PassportTier_PASSPORT_TIER_BASIC,
		PassportTier_PASSPORT_TIER_STANDARD,
		PassportTier_PASSPORT_TIER_TRUSTED,
		PassportTier_PASSPORT_TIER_INSTITUTIONAL:
	default:
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPassportTierInvalid,
			"",
			"passport_requirements.min_tier",
			fmt.Sprintf("workflow passport min_tier has unknown enum value %d", requirements.GetMinTier()),
		))
	}
	if score := requirements.GetMinReputationScore(); score > MaxPassportReputationScore {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonPassportReputationInvalid,
			"",
			"passport_requirements.min_reputation_score",
			fmt.Sprintf("workflow passport min_reputation_score must be <= %d", MaxPassportReputationScore),
		))
	}
	for _, badge := range requirements.GetRequiredBadges() {
		canonical := strings.TrimSpace(badge)
		if canonical == "" {
			findings = append(findings, workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonPassportBadgeInvalid,
				"",
				"passport_requirements.required_badges",
				"workflow passport required_badges entries cannot be empty",
			))
			break
		}
		if canonical != badge {
			findings = append(findings, workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonPassportBadgeInvalid,
				"",
				"passport_requirements.required_badges",
				fmt.Sprintf("workflow passport required_badges entries must be canonical: %q", badge),
			))
			break
		}
	}
	return findings
}

func analyzeWorkflowGovernance(governance *Governance) []WorkflowCardStaticFinding {
	if governance == nil {
		return []WorkflowCardStaticFinding{workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonGovernanceRequired,
			"",
			"governance",
			"workflow governance is required",
		)}
	}
	findings := make([]WorkflowCardStaticFinding, 0)
	if len(governance.GetAuthorAddresses()) == 0 {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonGovernanceAuthorInvalid,
			"",
			"governance.author_addresses",
			"workflow governance author_addresses must contain at least one address",
		))
	} else {
		for _, address := range governance.GetAuthorAddresses() {
			canonical := strings.TrimSpace(address)
			if canonical == "" {
				findings = append(findings, workflowStaticFinding(
					WorkflowStaticSeverityError,
					WorkflowStaticReasonGovernanceAuthorInvalid,
					"",
					"governance.author_addresses",
					"workflow governance author_addresses entries cannot be empty",
				))
				break
			}
			if canonical != address {
				findings = append(findings, workflowStaticFinding(
					WorkflowStaticSeverityError,
					WorkflowStaticReasonGovernanceAuthorInvalid,
					"",
					"governance.author_addresses",
					fmt.Sprintf("workflow governance author_addresses entries must be canonical: %q", address),
				))
				break
			}
		}
	}
	switch governance.GetUpgradePolicy() {
	case UpgradePolicy_UPGRADE_POLICY_IMMUTABLE,
		UpgradePolicy_UPGRADE_POLICY_SEMVER_COMPATIBLE,
		UpgradePolicy_UPGRADE_POLICY_GOVERNANCE_APPROVED:
	default:
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonGovernancePolicyInvalid,
			"",
			"governance.upgrade_policy",
			fmt.Sprintf("workflow governance upgrade_policy has unknown enum value %d", governance.GetUpgradePolicy()),
		))
	}
	return findings
}

func analyzeWorkflowLicenseLane(raw string) []WorkflowCardStaticFinding {
	licenseLane := strings.TrimSpace(raw)
	if licenseLane == "" || licenseLane != raw {
		return []WorkflowCardStaticFinding{workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonLicenseLaneInvalid,
			"",
			"license_lane",
			fmt.Sprintf("workflow license_lane must be one of byo_key, licensed_resale, community: %q", raw),
		)}
	}
	if _, ok := workflowLicenseLanes[licenseLane]; !ok {
		return []WorkflowCardStaticFinding{workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonLicenseLaneInvalid,
			"",
			"license_lane",
			fmt.Sprintf("workflow license_lane must be one of byo_key, licensed_resale, community: %q", raw),
		)}
	}
	return nil
}

func analyzeWorkflowCategories(categories []string) []WorkflowCardStaticFinding {
	if len(categories) == 0 {
		return []WorkflowCardStaticFinding{workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonCategoriesInvalid,
			"",
			"categories",
			"workflow categories must contain at least one category",
		)}
	}
	for _, category := range categories {
		canonical := strings.TrimSpace(category)
		if canonical == "" {
			return []WorkflowCardStaticFinding{workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonCategoriesInvalid,
				"",
				"categories",
				"workflow categories entries cannot be empty",
			)}
		}
		if canonical != category {
			return []WorkflowCardStaticFinding{workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonCategoriesInvalid,
				"",
				"categories",
				fmt.Sprintf("workflow categories entries must be canonical: %q", category),
			)}
		}
	}
	return nil
}

func analyzeWorkflowMetadata(card *WorkflowCard) []WorkflowCardStaticFinding {
	findings := make([]WorkflowCardStaticFinding, 0)
	if raw := card.GetDisplayName(); strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) != raw {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonMetadataInvalid,
			"",
			"display_name",
			fmt.Sprintf("workflow display_name cannot be empty or padded: %q", raw),
		))
	}
	if raw := card.GetAuthorId(); strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) != raw {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonMetadataInvalid,
			"",
			"author_id",
			fmt.Sprintf("workflow author_id cannot be empty or padded: %q", raw),
		))
	}
	if raw := card.GetAuthorPubkey(); strings.TrimSpace(raw) != raw || !workflowAuthorPubkeyPattern.MatchString(raw) {
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonMetadataInvalid,
			"",
			"author_pubkey",
			fmt.Sprintf("workflow author_pubkey must be an optional ed448: prefix followed by 114 hex characters: %q", raw),
		))
	}
	// created_at/updated_at are value time.Time (gogoproto stdtime) and always
	// valid; no per-field timestamp validity check is required after migration.
	findings = append(findings, analyzeWorkflowMetadataList("tags", "tag", card.GetTags(), 1)...)
	findings = append(findings, analyzeWorkflowMetadataList("jurisdictions", "jurisdiction", card.GetJurisdictions(), 2)...)
	return findings
}

func analyzeWorkflowMetadataList(field string, label string, values []string, minLen int) []WorkflowCardStaticFinding {
	for _, value := range values {
		canonical := strings.TrimSpace(value)
		if canonical == "" || len(canonical) < minLen {
			return []WorkflowCardStaticFinding{workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonMetadataInvalid,
				"",
				field,
				fmt.Sprintf("workflow %s entries must be at least %d canonical characters: %q", label, minLen, value),
			)}
		}
		if canonical != value {
			return []WorkflowCardStaticFinding{workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonMetadataInvalid,
				"",
				field,
				fmt.Sprintf("workflow %s entries must be canonical: %q", label, value),
			)}
		}
	}
	return nil
}

func canonicalExactToolVersion(raw string) (string, error) {
	if strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("tool_version_constraint must pin a canonical exact semver, got %q", workflowStaticDiagnostic(raw))
	}
	if raw == "" {
		return "", fmt.Errorf("tool_version_constraint must pin an exact semver")
	}
	constraint := strings.TrimPrefix(raw, "=")
	if constraint == "" {
		return "", fmt.Errorf("tool_version_constraint must pin an exact semver")
	}
	if strings.ContainsAny(constraint, "<>^~*=, ") {
		return "", fmt.Errorf("tool_version_constraint must pin an exact semver, got %q", workflowStaticDiagnostic(raw))
	}
	version, err := semver.NewVersion(constraint)
	if err != nil {
		return "", fmt.Errorf("tool_version_constraint must pin an exact semver: %s", workflowStaticDiagnostic(err.Error()))
	}
	canonical := version.String()
	if canonical != constraint {
		return "", fmt.Errorf("tool_version_constraint must pin a canonical exact semver, got %q", workflowStaticDiagnostic(raw))
	}
	return canonical, nil
}

func workflowStaticDiagnostic(value string) string {
	return logging.RedactPII(value)
}

func analyzeFallbackStep(step *Step, steps map[string]*Step) []WorkflowCardStaticFinding {
	if step.GetFailureAction() != FailureAction_FAILURE_ACTION_FALLBACK_STEP {
		return nil
	}
	stepID := strings.TrimSpace(step.GetStepId())
	rawFallbackID := step.GetFallbackStepId()
	fallbackID := strings.TrimSpace(rawFallbackID)
	if fallbackID == "" {
		return []WorkflowCardStaticFinding{workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepFallbackRequired, stepID, "fallback_step_id", fmt.Sprintf("workflow step %s requires fallback_step_id", stepID))}
	}
	if !isCanonicalWorkflowStepID(rawFallbackID) {
		return []WorkflowCardStaticFinding{workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepFallbackInvalid, stepID, "fallback_step_id", fmt.Sprintf("workflow step %s fallback_step_id must match %s: %q", stepID, workflowStepIDPattern.String(), rawFallbackID))}
	}
	if fallbackID == stepID {
		return []WorkflowCardStaticFinding{workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepFallbackSelf, stepID, "fallback_step_id", fmt.Sprintf("workflow step %s cannot fall back to itself", stepID))}
	}
	if _, ok := steps[fallbackID]; !ok {
		return []WorkflowCardStaticFinding{workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepFallbackUnknown, stepID, "fallback_step_id", fmt.Sprintf("workflow step %s falls back to unknown step %s", stepID, fallbackID))}
	}
	return nil
}

func addImplicitFallbackStepEdges(steps []*Step, stepsByID map[string]*Step, graph map[string][]string, indegree map[string]int) {
	for _, step := range steps {
		if step == nil || step.GetFailureAction() != FailureAction_FAILURE_ACTION_FALLBACK_STEP {
			continue
		}
		stepID := strings.TrimSpace(step.GetStepId())
		fallbackID := strings.TrimSpace(step.GetFallbackStepId())
		if stepID == "" || fallbackID == "" || stepID == fallbackID {
			continue
		}
		if _, ok := stepsByID[stepID]; !ok {
			continue
		}
		if _, ok := stepsByID[fallbackID]; !ok {
			continue
		}
		if workflowDAGEdgeExists(graph, stepID, fallbackID) {
			continue
		}
		graph[stepID] = append(graph[stepID], fallbackID)
		indegree[fallbackID]++
	}
}

func addImplicitConditionStepEdges(steps []*Step, stepsByID map[string]*Step, graph map[string][]string, indegree map[string]int) {
	for _, step := range steps {
		if step == nil || strings.TrimSpace(step.GetCondition()) == "" {
			continue
		}
		stepID := strings.TrimSpace(step.GetStepId())
		if stepID == "" {
			continue
		}
		if _, ok := stepsByID[stepID]; !ok {
			continue
		}
		for _, dep := range workflowConditionStepReferences(step.GetCondition()) {
			if dep == stepID {
				continue
			}
			if _, ok := stepsByID[dep]; !ok {
				continue
			}
			if workflowDAGEdgeExists(graph, dep, stepID) {
				continue
			}
			graph[dep] = append(graph[dep], stepID)
			indegree[stepID]++
		}
	}
}

func analyzeWorkflowNonReversibleLeaves(ids []string, stepsByID map[string]*Step, graph map[string][]string) []WorkflowCardStaticFinding {
	findings := make([]WorkflowCardStaticFinding, 0)
	for _, stepID := range ids {
		step := stepsByID[stepID]
		if step == nil || step.GetSideEffect() != SideEffect_SIDE_EFFECT_NON_REVERSIBLE {
			continue
		}
		dependents := workflowUniqueSortedStepIDs(graph[stepID])
		if len(dependents) == 0 {
			continue
		}
		findings = append(findings, workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonStepSideEffectNonTerminal,
			stepID,
			"side_effect",
			fmt.Sprintf("workflow step %s has non_reversible side_effect but is not terminal; downstream steps: %s", stepID, strings.Join(dependents, ", ")),
		))
	}
	return findings
}

func workflowUniqueSortedStepIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	sort.Strings(unique)
	return unique
}

func workflowConditionStepReferences(condition string) []string {
	return sortedWorkflowConditionStepReferences(condition)
}

func workflowDAGEdgeExists(graph map[string][]string, from, to string) bool {
	for _, child := range graph[from] {
		if child == to {
			return true
		}
	}
	return false
}

func isCanonicalWorkflowStepID(raw string) bool {
	return strings.TrimSpace(raw) == raw && workflowStepIDPattern.MatchString(raw)
}

func canonicalWorkflowStepDependencies(raw []string, stepID string) ([]string, []WorkflowCardStaticFinding) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	deps := make([]string, 0, len(raw))
	findings := make([]WorkflowCardStaticFinding, 0)
	for _, dep := range raw {
		trimmed := strings.TrimSpace(dep)
		if trimmed == "" {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepDependencyInvalid, stepID, "depends_on", fmt.Sprintf("workflow step %s depends_on entry cannot be empty", stepID)))
			continue
		}
		if !isCanonicalWorkflowStepID(dep) {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepDependencyInvalid, stepID, "depends_on", fmt.Sprintf("workflow step %s depends_on entry must match %s: %q", stepID, workflowStepIDPattern.String(), dep)))
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		deps = append(deps, trimmed)
	}
	sort.Strings(deps)
	return deps, findings
}

func analyzeStaticCondition(step *Step) []WorkflowCardStaticFinding {
	condition, err := ParseWorkflowCondition(step.GetCondition())
	if err != nil {
		return []WorkflowCardStaticFinding{workflowStaticFinding(
			WorkflowStaticSeverityError,
			WorkflowStaticReasonConditionUnsupported,
			strings.TrimSpace(step.GetStepId()),
			"condition",
			fmt.Sprintf("workflow step condition must follow Workflow Condition DSL V1: %v", err),
		)}
	}
	switch condition.Kind {
	case WorkflowConditionKindEmpty, WorkflowConditionKindAlways:
		return nil
	case WorkflowConditionKindNever:
		return []WorkflowCardStaticFinding{workflowStaticFinding(WorkflowStaticSeverityWarning, WorkflowStaticReasonConditionDeadBranch, strings.TrimSpace(step.GetStepId()), "condition", "workflow step condition is statically false")}
	default:
		return nil
	}
}

func analyzeConditionStepReferences(step *Step, steps map[string]*Step) []WorkflowCardStaticFinding {
	condition := strings.TrimSpace(step.GetCondition())
	if condition == "" {
		return nil
	}
	stepID := strings.TrimSpace(step.GetStepId())
	var findings []WorkflowCardStaticFinding
	for _, dep := range workflowConditionStepReferences(condition) {
		if dep == stepID {
			findings = append(findings, workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonConditionSelfReference,
				stepID,
				"condition",
				fmt.Sprintf("workflow step %s condition cannot reference itself", stepID),
			))
			continue
		}
		if _, ok := steps[dep]; !ok {
			findings = append(findings, workflowStaticFinding(
				WorkflowStaticSeverityError,
				WorkflowStaticReasonConditionUnknownStep,
				stepID,
				"condition",
				fmt.Sprintf("workflow step %s condition references unknown step %s", stepID, dep),
			))
		}
	}
	return findings
}

func normalizedWorkflowStepDependencies(raw []string) []string {
	deps, _ := canonicalWorkflowStepDependencies(raw, "")
	return deps
}

func analyzeWorkflowDAGCycles(ids []string, graph map[string][]string, indegree map[string]int) []WorkflowCardStaticFinding {
	if len(ids) == 0 {
		return nil
	}
	ready := make([]string, 0, len(ids))
	for _, id := range ids {
		if indegree[id] == 0 {
			ready = append(ready, id)
		}
		sort.Strings(graph[id])
	}
	visited := 0
	for len(ready) > 0 {
		sort.Strings(ready)
		current := ready[0]
		ready = ready[1:]
		visited++
		for _, child := range graph[current] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
			}
		}
	}
	if visited == len(ids) {
		return nil
	}
	return []WorkflowCardStaticFinding{workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonDAGCycle, "", "dag", "workflow DAG contains cycle")}
}

func analyzeUnusedWorkflowInputs(rawSchema string, bindings []string) []WorkflowCardStaticFinding {
	properties := workflowInputSchemaProperties(rawSchema)
	if len(properties) == 0 {
		return nil
	}
	joinedBindings := strings.Join(bindings, "\n")
	findings := make([]WorkflowCardStaticFinding, 0)
	for _, property := range properties {
		if workflowInputBindingUsesName(joinedBindings, property) {
			continue
		}
		findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityWarning, WorkflowStaticReasonWorkflowInputUnused, "", "input_schema", fmt.Sprintf("workflow input %q is not referenced by any step input_binding", property)))
	}
	return findings
}

func workflowInputSchemaProperties(rawSchema string) []string {
	properties := workflowInputSchemaPropertyMap(rawSchema)
	out := make([]string, 0, len(properties))
	for name := range properties {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func workflowInputSchemaPropertyMap(rawSchema string) map[string]json.RawMessage {
	if strings.TrimSpace(rawSchema) == "" {
		return nil
	}
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(rawSchema), &doc); err != nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(doc.Properties))
	for name := range doc.Properties {
		if workflowInputSchemaNamePattern.MatchString(name) {
			out[name] = doc.Properties[name]
		}
	}
	return out
}

func workflowInputBindingUsesName(bindings, name string) bool {
	if name == "" {
		return false
	}
	pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_-]|$)`)
	return pattern.FindStringIndex(bindings) != nil
}

func analyzeWorkflowToolResolution(card *WorkflowCard, resolver WorkflowToolResolver) []WorkflowCardStaticFinding {
	var findings []WorkflowCardStaticFinding
	stepsByID, stepIDs := validWorkflowSteps(card.GetDag())
	inputProperties := workflowInputSchemaPropertyMap(card.GetInputSchema())
	resolved := make(map[string]WorkflowToolDescriptor, len(stepIDs))

	for _, stepID := range stepIDs {
		step := stepsByID[stepID]
		version, err := canonicalExactToolVersion(step.GetToolVersionConstraint())
		if err != nil || strings.TrimSpace(step.GetToolId()) == "" {
			continue
		}
		tool, found, err := resolver.ResolveWorkflowTool(strings.TrimSpace(step.GetToolId()), version)
		if err != nil {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonToolResolutionFailed, stepID, "tool_id", err.Error()))
			continue
		}
		if !found {
			reason := WorkflowStaticReasonStepToolNotFound
			message := fmt.Sprintf("workflow step %s references missing tool %s@%s", stepID, strings.TrimSpace(step.GetToolId()), version)
			if lister, ok := resolver.(WorkflowToolVersionLister); ok && len(lister.WorkflowToolVersions(step.GetToolId())) > 0 {
				reason = WorkflowStaticReasonStepToolVersionMismatch
				message = fmt.Sprintf("workflow step %s references missing tool version %s@%s", stepID, strings.TrimSpace(step.GetToolId()), version)
			}
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, reason, stepID, "tool_version_constraint", message))
			continue
		}
		resolved[stepID] = tool
	}

	for _, stepID := range stepIDs {
		step := stepsByID[stepID]
		tool, ok := resolved[stepID]
		if !ok {
			continue
		}
		inputSchema, err := parseWorkflowJSONSchema(tool.InputSchema)
		if err != nil {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepToolInputMalformed, stepID, "input_schema", fmt.Sprintf("tool %s@%s input_schema is malformed: %v", tool.ToolID, tool.Version, err)))
			continue
		}
		if _, err := parseWorkflowJSONSchema(tool.OutputSchema); err != nil {
			findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepToolOutputMalformed, stepID, "output_schema", fmt.Sprintf("tool %s@%s output_schema is malformed: %v", tool.ToolID, tool.Version, err)))
			continue
		}
		findings = append(findings, analyzeStepInputBindingSources(step, stepsByID, inputProperties, resolved, inputSchema)...)
	}
	return findings
}

func validWorkflowSteps(steps []*Step) (map[string]*Step, []string) {
	stepsByID := make(map[string]*Step, len(steps))
	stepIDs := make([]string, 0, len(steps))
	for _, step := range steps {
		if step == nil {
			continue
		}
		rawStepID := step.GetStepId()
		stepID := strings.TrimSpace(rawStepID)
		if stepID == "" || !isCanonicalWorkflowStepID(rawStepID) {
			continue
		}
		if _, exists := stepsByID[stepID]; exists {
			continue
		}
		stepsByID[stepID] = step
		stepIDs = append(stepIDs, stepID)
	}
	sort.Strings(stepIDs)
	return stepsByID, stepIDs
}

type workflowJSONSchema struct {
	Type       any                        `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

func parseWorkflowJSONSchema(raw string) (workflowJSONSchema, error) {
	if strings.TrimSpace(raw) == "" {
		return workflowJSONSchema{}, fmt.Errorf("schema is empty")
	}
	var schema workflowJSONSchema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return workflowJSONSchema{}, err
	}
	return schema, nil
}

func parseWorkflowJSONObjectSchema(raw string) (workflowJSONSchema, error) {
	schema, err := parseWorkflowJSONSchema(raw)
	if err != nil {
		return workflowJSONSchema{}, err
	}
	if !workflowSchemaAllowsObjectType(schema.Type) {
		return workflowJSONSchema{}, fmt.Errorf("schema type must be object")
	}
	return schema, nil
}

func workflowSchemaAllowsObjectType(raw any) bool {
	switch typed := raw.(type) {
	case string:
		return typed == "object"
	case []any:
		for _, item := range typed {
			if value, ok := item.(string); ok && value == "object" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

type workflowBindingSource struct {
	kind  string
	name  string
	field string
}

func analyzeStepInputBindingSources(
	step *Step,
	steps map[string]*Step,
	workflowInputs map[string]json.RawMessage,
	resolved map[string]WorkflowToolDescriptor,
	toolInput workflowJSONSchema,
) []WorkflowCardStaticFinding {
	stepID := strings.TrimSpace(step.GetStepId())
	sources := workflowInputBindingSources(step.GetInputBinding())
	findings := make([]WorkflowCardStaticFinding, 0)
	deps := stringSet(normalizedWorkflowStepDependencies(step.GetDependsOn()))

	var sourceSchema json.RawMessage
	singleSource := len(sources) == 1
	for _, source := range sources {
		switch source.kind {
		case "input":
			raw, ok := workflowInputs[source.name]
			if !ok {
				findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepInputUnknownInput, stepID, "input_binding", fmt.Sprintf("workflow step %s references unknown workflow input %s", stepID, source.name)))
				continue
			}
			if singleSource {
				sourceSchema = raw
			}
		case "step":
			if _, ok := steps[source.name]; !ok {
				findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepInputUnknownStep, stepID, "input_binding", fmt.Sprintf("workflow step %s references unknown output step %s", stepID, source.name)))
				continue
			}
			if _, ok := deps[source.name]; !ok {
				findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepInputMissingDep, stepID, "input_binding", fmt.Sprintf("workflow step %s reads %s output without declaring depends_on", stepID, source.name)))
				continue
			}
			if singleSource {
				tool := resolved[source.name]
				sourceSchema = workflowOutputBindingSchema(tool.OutputSchema, source.field)
			}
		}
	}
	if len(findings) > 0 || len(sourceSchema) == 0 {
		return findings
	}
	if requiredField, ok := singleRequiredSchemaField(toolInput); ok && !workflowSchemasCompatible(sourceSchema, toolInput.Properties[requiredField]) {
		findings = append(findings, workflowStaticFinding(WorkflowStaticSeverityError, WorkflowStaticReasonStepInputSchemaMismatch, stepID, "input_binding", fmt.Sprintf("workflow step %s input_binding is not schema-compatible with required tool input %s", stepID, requiredField)))
	}
	return findings
}

func workflowOutputBindingSchema(rawSchema, field string) json.RawMessage {
	if strings.TrimSpace(field) == "" {
		return json.RawMessage(rawSchema)
	}
	schema, err := parseWorkflowJSONSchema(rawSchema)
	if err != nil {
		return json.RawMessage{}
	}
	return schema.Properties[field]
}

func workflowInputBindingSources(binding string) []workflowBindingSource {
	seen := make(map[workflowBindingSource]struct{})
	out := make([]workflowBindingSource, 0)
	for _, match := range workflowInputRefPattern.FindAllStringSubmatch(binding, -1) {
		source := workflowBindingSource{kind: "input", name: match[1]}
		if _, ok := seen[source]; !ok {
			seen[source] = struct{}{}
			out = append(out, source)
		}
	}
	for _, match := range workflowStepOutputRefPattern.FindAllStringSubmatch(binding, -1) {
		source := workflowBindingSource{kind: "step", name: match[1]}
		if len(match) > 2 {
			source.field = match[2]
		}
		if _, ok := seen[source]; !ok {
			seen[source] = struct{}{}
			out = append(out, source)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].kind != out[j].kind {
			return out[i].kind < out[j].kind
		}
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return out[i].field < out[j].field
	})
	return out
}

func singleRequiredSchemaField(schema workflowJSONSchema) (string, bool) {
	if len(schema.Required) != 1 {
		return "", false
	}
	field := strings.TrimSpace(schema.Required[0])
	if field == "" || schema.Properties == nil || schema.Properties[field] == nil {
		return "", false
	}
	return field, true
}

func workflowSchemasCompatible(source, target json.RawMessage) bool {
	sourceTypes := workflowSchemaTypes(source)
	targetTypes := workflowSchemaTypes(target)
	if len(sourceTypes) == 0 || len(targetTypes) == 0 {
		return true
	}
	for sourceType := range sourceTypes {
		if _, ok := targetTypes[sourceType]; ok {
			return true
		}
	}
	return false
}

func workflowSchemaTypes(raw json.RawMessage) map[string]struct{} {
	var schema struct {
		Type any `json:"type"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil
	}
	switch typed := schema.Type.(type) {
	case string:
		return stringSet([]string{typed})
	case []any:
		types := make(map[string]struct{}, len(typed))
		for _, value := range typed {
			if item, ok := value.(string); ok {
				types[item] = struct{}{}
			}
		}
		return types
	default:
		return nil
	}
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func workflowToolKey(toolID, version string) string {
	return strings.TrimSpace(toolID) + "\x00" + strings.TrimSpace(version)
}

// workflowCoinUnset reports whether a gogoproto sdk.Coin field was not provided.
// An unset coin has no denom and a nil-or-zero amount (after a JSON round-trip
// an unset coin serializes as {"amount":"0"}, so a zero amount also counts).
func workflowCoinUnset(coin sdk.Coin) bool {
	return coin.Denom == "" && (coin.Amount.IsNil() || coin.Amount.IsZero())
}

func sortWorkflowStaticFindings(findings []WorkflowCardStaticFinding) {
	sort.SliceStable(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.ReasonCode != right.ReasonCode {
			return left.ReasonCode < right.ReasonCode
		}
		if left.StepID != right.StepID {
			return left.StepID < right.StepID
		}
		if left.Field != right.Field {
			return left.Field < right.Field
		}
		return left.Message < right.Message
	})
}
