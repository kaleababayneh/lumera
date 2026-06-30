package types

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
)

func TestStaticWorkflowCard_ValidDAGPasses(t *testing.T) {
	card := validStaticWorkflowCard()
	if err := StaticCheckWorkflowCard(card); err != nil {
		t.Fatalf("valid workflow card should pass static validation: %v", err)
	}
	if findings := AnalyzeWorkflowCard(card); len(findings) != 0 {
		t.Fatalf("valid workflow card should not produce findings: %+v", findings)
	}
}

func TestStaticWorkflowCard_RejectsNonCanonicalWorkflowVersion(t *testing.T) {
	cases := []struct {
		name    string
		version string
	}{
		{
			name:    "padded version",
			version: " 1.0.0 ",
		},
		{
			name:    "v prefixed version",
			version: "v1.0.0",
		},
		{
			name:    "leading zero version",
			version: "01.0.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			card.Version = tc.version

			assertStaticValidationReason(t, card, WorkflowStaticReasonVersionInvalid)
		})
	}
}

func TestStaticWorkflowCard_RejectsCycleUnknownDependencyAndDuplicate(t *testing.T) {
	cases := []struct {
		name string
		card *WorkflowCard
		want string
	}{
		{
			name: "cycle",
			card: workflowCardWithSteps(
				staticStep("step-a", "step-b"),
				staticStep("step-b", "step-a"),
			),
			want: WorkflowStaticReasonDAGCycle,
		},
		{
			name: "unknown dependency",
			card: workflowCardWithSteps(
				staticStep("step-a", "missing-step"),
			),
			want: WorkflowStaticReasonStepDependencyUnknown,
		},
		{
			name: "duplicate step id",
			card: workflowCardWithSteps(
				staticStep("step-a"),
				staticStep("step-a"),
			),
			want: WorkflowStaticReasonStepDuplicate,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStaticValidationReason(t, tc.card, tc.want)
		})
	}
}

func TestStaticWorkflowCard_RejectsFallbackAndVersionErrors(t *testing.T) {
	fallbackSelf := staticStep("step-a")
	fallbackSelf.FailureAction = FailureAction_FAILURE_ACTION_FALLBACK_STEP
	fallbackSelf.FallbackStepId = "step-a"

	rangeVersion := staticStep("step-a")
	rangeVersion.ToolVersionConstraint = ">=1.0.0 <2.0.0"

	paddedExactVersion := staticStep("step-a")
	paddedExactVersion.ToolVersionConstraint = " 1.0.0 "

	doubleEqualsVersion := staticStep("step-a")
	doubleEqualsVersion.ToolVersionConstraint = strings.Repeat("=", 2) + "1.0.0"

	cases := []struct {
		name string
		card *WorkflowCard
		want string
	}{
		{
			name: "fallback to self",
			card: workflowCardWithSteps(fallbackSelf),
			want: WorkflowStaticReasonStepFallbackSelf,
		},
		{
			name: "version range",
			card: workflowCardWithSteps(rangeVersion),
			want: WorkflowStaticReasonStepVersionInvalid,
		},
		{
			name: "padded exact version",
			card: workflowCardWithSteps(paddedExactVersion),
			want: WorkflowStaticReasonStepVersionInvalid,
		},
		{
			name: "double equals exact version",
			card: workflowCardWithSteps(doubleEqualsVersion),
			want: WorkflowStaticReasonStepVersionInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStaticValidationReason(t, tc.card, tc.want)
		})
	}
}

func TestStaticWorkflowCard_RedactsToolVersionConstraintDiagnostics(t *testing.T) {
	marker := strings.Join([]string{"workflow", "static", "secret"}, "-")
	apiKeyField := strings.Join([]string{"api", "key"}, "_")
	clientSecretField := strings.Join([]string{"client", "secret"}, "_")
	card := validStaticWorkflowCard()
	card.Dag[0].ToolVersionConstraint = " 1.0.0?" + apiKeyField + "=" + marker + "&" + clientSecretField + "=" + marker + " "

	err := StaticCheckWorkflowCard(card)
	if err == nil {
		t.Fatal("expected invalid tool_version_constraint error")
	}
	got := err.Error()
	if strings.Contains(got, marker) {
		t.Fatalf("static validation error leaked raw constraint marker: %q", got)
	}
	if !strings.Contains(got, apiKeyField+"=[REDACTED]") || !strings.Contains(got, clientSecretField+"=[REDACTED]") {
		t.Fatalf("static validation error missing redaction markers: %q", got)
	}

	findings := fmt.Sprint(AnalyzeWorkflowCard(card))
	if strings.Contains(findings, marker) {
		t.Fatalf("static findings leaked raw constraint marker: %q", findings)
	}
}

func TestStaticWorkflowCard_AcceptsSingleEqualsExactVersion(t *testing.T) {
	card := validStaticWorkflowCard()
	card.Dag[0].ToolVersionConstraint = "=1.0.0"

	if err := StaticCheckWorkflowCard(card); err != nil {
		t.Fatalf("single-equals exact version should pass static validation: %v", err)
	}
}

func TestStaticWorkflowCard_RejectsMissingOrUnknownSideEffect(t *testing.T) {
	missingSideEffect := staticStep("step-a")
	missingSideEffect.SideEffect = SideEffect_SIDE_EFFECT_UNSPECIFIED

	unknownSideEffect := staticStep("step-a")
	unknownSideEffect.SideEffect = SideEffect(99)

	cases := []struct {
		name string
		card *WorkflowCard
	}{
		{
			name: "missing side effect declaration",
			card: workflowCardWithSteps(missingSideEffect),
		},
		{
			name: "unknown side effect enum",
			card: workflowCardWithSteps(unknownSideEffect),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStaticValidationReason(t, tc.card, WorkflowStaticReasonStepSideEffectInvalid)
		})
	}
}

func TestStaticWorkflowCard_RejectsNonTerminalNonReversibleSteps(t *testing.T) {
	explicit := validStaticWorkflowCard()
	explicit.Dag[0].SideEffect = SideEffect_SIDE_EFFECT_NON_REVERSIBLE

	fallback := workflowCardWithSteps(staticStep("step-a"), staticStep("step-b"))
	fallback.Dag[0].SideEffect = SideEffect_SIDE_EFFECT_NON_REVERSIBLE
	fallback.Dag[0].FailureAction = FailureAction_FAILURE_ACTION_FALLBACK_STEP
	fallback.Dag[0].FallbackStepId = "step-b"

	condition := workflowCardWithSteps(staticStep("step-a"), staticStep("step-b"))
	condition.Dag[0].SideEffect = SideEffect_SIDE_EFFECT_NON_REVERSIBLE
	condition.Dag[1].Condition = "steps.step-a.output.policy_allowed == true"

	cases := []struct {
		name string
		card *WorkflowCard
	}{
		{
			name: "explicit dependent",
			card: explicit,
		},
		{
			name: "fallback dependent",
			card: fallback,
		},
		{
			name: "condition dependent",
			card: condition,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStaticValidationReason(t, tc.card, WorkflowStaticReasonStepSideEffectNonTerminal)
		})
	}
}

func TestStaticWorkflowCard_AcceptsTerminalNonReversibleStep(t *testing.T) {
	card := validStaticWorkflowCard()
	card.Dag[1].SideEffect = SideEffect_SIDE_EFFECT_NON_REVERSIBLE

	if err := StaticCheckWorkflowCard(card); err != nil {
		t.Fatalf("terminal non_reversible step should pass static validation: %v", err)
	}
}

func TestStaticWorkflowCard_RejectsMissingOrUnknownFailureAction(t *testing.T) {
	missingFailureAction := staticStep("step-a")
	missingFailureAction.FailureAction = FailureAction_FAILURE_ACTION_UNSPECIFIED

	unknownFailureAction := staticStep("step-a")
	unknownFailureAction.FailureAction = FailureAction(99)

	cases := []struct {
		name string
		card *WorkflowCard
	}{
		{
			name: "missing failure action declaration",
			card: workflowCardWithSteps(missingFailureAction),
		},
		{
			name: "unknown failure action enum",
			card: workflowCardWithSteps(unknownFailureAction),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := StaticCheckWorkflowCard(tc.card)
			if err == nil {
				t.Fatal("expected workflow card with invalid failure_action to fail static validation")
			}
			if !strings.Contains(err.Error(), "failure_action") {
				t.Fatalf("expected failure_action validation error, got %v", err)
			}
		})
	}
}

func TestStaticWorkflowCard_RejectsMalformedStepExecutionPolicy(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WorkflowCard)
		want string
	}{
		{
			name: "missing max sub cost",
			edit: func(card *WorkflowCard) {
				card.Dag[0].MaxSubCost = sdk.Coin{}
			},
			want: WorkflowStaticReasonStepMaxSubCostInvalid,
		},
		{
			name: "nil amount max sub cost",
			edit: func(card *WorkflowCard) {
				card.Dag[0].MaxSubCost = sdk.Coin{Denom: "ulac"}
			},
			want: WorkflowStaticReasonStepMaxSubCostInvalid,
		},
		{
			name: "negative max sub cost",
			edit: func(card *WorkflowCard) {
				card.Dag[0].MaxSubCost.Amount = sdkmath.NewInt(-1)
			},
			want: WorkflowStaticReasonStepMaxSubCostInvalid,
		},
		{
			name: "zero max sub cost",
			edit: func(card *WorkflowCard) {
				card.Dag[0].MaxSubCost.Amount = sdkmath.ZeroInt()
			},
			want: WorkflowStaticReasonStepMaxSubCostInvalid,
		},
		{
			name: "missing sub slo",
			edit: func(card *WorkflowCard) {
				card.Dag[0].SubSloP95Ms = 0
			},
			want: WorkflowStaticReasonStepSubSLOInvalid,
		},
		{
			name: "missing retry policy",
			edit: func(card *WorkflowCard) {
				card.Dag[0].RetryPolicy = nil
			},
			want: WorkflowStaticReasonStepRetryPolicyInvalid,
		},
		{
			name: "zero max attempts",
			edit: func(card *WorkflowCard) {
				card.Dag[0].RetryPolicy.MaxAttempts = 0
			},
			want: WorkflowStaticReasonStepRetryPolicyInvalid,
		},
		{
			name: "backoff multiplier below schema minimum",
			edit: func(card *WorkflowCard) {
				card.Dag[0].RetryPolicy.BackoffMultiplier = 0.5
			},
			want: WorkflowStaticReasonStepRetryPolicyInvalid,
		},
		{
			name: "backoff multiplier negative",
			edit: func(card *WorkflowCard) {
				card.Dag[0].RetryPolicy.BackoffMultiplier = -1
			},
			want: WorkflowStaticReasonStepRetryPolicyInvalid,
		},
		{
			name: "backoff multiplier nan",
			edit: func(card *WorkflowCard) {
				card.Dag[0].RetryPolicy.BackoffMultiplier = math.NaN()
			},
			want: WorkflowStaticReasonStepRetryPolicyInvalid,
		},
		{
			name: "backoff multiplier infinity",
			edit: func(card *WorkflowCard) {
				card.Dag[0].RetryPolicy.BackoffMultiplier = math.Inf(1)
			},
			want: WorkflowStaticReasonStepRetryPolicyInvalid,
		},
		{
			name: "empty retryable code",
			edit: func(card *WorkflowCard) {
				card.Dag[0].RetryPolicy.RetryableErrorCodes = []string{""}
			},
			want: WorkflowStaticReasonStepRetryPolicyInvalid,
		},
		{
			name: "padded retryable code",
			edit: func(card *WorkflowCard) {
				card.Dag[0].RetryPolicy.RetryableErrorCodes = []string{" temporary "}
			},
			want: WorkflowStaticReasonStepRetryPolicyInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReason(t, card, tc.want)
		})
	}
}

func TestStaticWorkflowCard_RejectsNonCanonicalStepIdentifiers(t *testing.T) {
	paddedStepID := workflowCardWithSteps(staticStep(" step-a"))
	uppercaseStepID := workflowCardWithSteps(staticStep("StepA"))

	emptyToolID := validStaticWorkflowCard()
	emptyToolID.Dag[0].ToolId = " "

	paddedToolID := validStaticWorkflowCard()
	paddedToolID.Dag[0].ToolId = " tool.step-a "

	paddedInputBinding := validStaticWorkflowCard()
	paddedInputBinding.Dag[0].InputBinding = " $.inputs.asset "

	paddedDependency := workflowCardWithSteps(staticStep("step-a"), staticStep("step-b", " step-a"))

	paddedFallback := workflowCardWithSteps(staticStep("step-a"), staticStep("step-b"))
	paddedFallback.Dag[0].FailureAction = FailureAction_FAILURE_ACTION_FALLBACK_STEP
	paddedFallback.Dag[0].FallbackStepId = " step-b"

	cases := []struct {
		name string
		card *WorkflowCard
		want string
	}{
		{
			name: "padded step id",
			card: paddedStepID,
			want: WorkflowStaticReasonStepIDInvalid,
		},
		{
			name: "pattern-invalid step id",
			card: uppercaseStepID,
			want: WorkflowStaticReasonStepIDInvalid,
		},
		{
			name: "empty tool id",
			card: emptyToolID,
			want: WorkflowStaticReasonStepToolIDEmpty,
		},
		{
			name: "padded tool id",
			card: paddedToolID,
			want: WorkflowStaticReasonStepToolIDInvalid,
		},
		{
			name: "padded input binding",
			card: paddedInputBinding,
			want: WorkflowStaticReasonStepInputBindingEmpty,
		},
		{
			name: "padded dependency",
			card: paddedDependency,
			want: WorkflowStaticReasonStepDependencyInvalid,
		},
		{
			name: "padded fallback",
			card: paddedFallback,
			want: WorkflowStaticReasonStepFallbackInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStaticValidationReason(t, tc.card, tc.want)
		})
	}
}

func TestStaticWorkflowCard_WarnsOnDeadBranchAndUnusedInput(t *testing.T) {
	card := validStaticWorkflowCard()
	card.InputSchema = `{"type":"object","properties":{"asset":{"type":"string"},"unused_budget":{"type":"string"}}}`
	card.Dag[0].Condition = "false"

	findings := AnalyzeWorkflowCard(card)
	assertFinding(t, findings, WorkflowStaticSeverityWarning, WorkflowStaticReasonConditionDeadBranch)
	assertFinding(t, findings, WorkflowStaticSeverityWarning, WorkflowStaticReasonWorkflowInputUnused)
	if err := StaticCheckWorkflowCard(card); err != nil {
		t.Fatalf("warnings should not reject workflow card: %v", err)
	}
}

func TestStaticWorkflowCard_AcceptsOutputClaimConditionAndAddsImplicitEdges(t *testing.T) {
	card := workflowCardWithSteps(staticStep("step-a"), staticStep("step-b"))
	card.Dag[1].Condition = "steps.step-a.output.policy_allowed == true"

	if err := StaticCheckWorkflowCard(card); err != nil {
		t.Fatalf("output-claim condition should pass static validation: %v", err)
	}

	cyclic := workflowCardWithSteps(staticStep("step-a", "step-b"), staticStep("step-b"))
	cyclic.Dag[1].Condition = "steps.step-a.output.policy_allowed == true"

	assertStaticValidationReason(t, cyclic, WorkflowStaticReasonDAGCycle)
}

func TestStaticWorkflowCard_RejectsMalformedConditionDSL(t *testing.T) {
	cases := []string{
		"steps.step-a.output.ok == true && steps.step-b.output.ok == true",
		"steps.step-a.output.PolicyAllowed == true",
		"steps.step-a.output.amount == -0",
		"steps.step-a.output.label == 'hold'",
	}

	for _, condition := range cases {
		t.Run(condition, func(t *testing.T) {
			card := workflowCardWithSteps(staticStep("step-a"), staticStep("step-b"))
			card.Dag[1].Condition = condition

			assertStaticValidationReason(t, card, WorkflowStaticReasonConditionUnsupported)
		})
	}
}

func TestStaticWorkflowCard_RejectsOutputConditionUnknownAndSelfReferences(t *testing.T) {
	unknown := workflowCardWithSteps(staticStep("step-a"), staticStep("step-b"))
	unknown.Dag[1].Condition = "steps.missing-step.output.policy_allowed == true"
	assertStaticValidationReason(t, unknown, WorkflowStaticReasonConditionUnknownStep)

	self := workflowCardWithSteps(staticStep("step-a"), staticStep("step-b"))
	self.Dag[1].Condition = "steps.step-b.output.policy_allowed == true"
	assertStaticValidationReason(t, self, WorkflowStaticReasonConditionSelfReference)
}

func TestStaticWorkflowCard_RejectsMalformedWorkflowSchemas(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WorkflowCard)
		want string
	}{
		{
			name: "missing input schema",
			edit: func(card *WorkflowCard) {
				card.InputSchema = ""
			},
			want: WorkflowStaticReasonWorkflowInputSchemaInvalid,
		},
		{
			name: "malformed input schema json",
			edit: func(card *WorkflowCard) {
				card.InputSchema = "{"
			},
			want: WorkflowStaticReasonWorkflowInputSchemaInvalid,
		},
		{
			name: "non object input schema",
			edit: func(card *WorkflowCard) {
				card.InputSchema = `{"type":"array"}`
			},
			want: WorkflowStaticReasonWorkflowInputSchemaInvalid,
		},
		{
			name: "missing output schema",
			edit: func(card *WorkflowCard) {
				card.OutputSchema = ""
			},
			want: WorkflowStaticReasonWorkflowOutputSchemaInvalid,
		},
		{
			name: "malformed output schema json",
			edit: func(card *WorkflowCard) {
				card.OutputSchema = "{"
			},
			want: WorkflowStaticReasonWorkflowOutputSchemaInvalid,
		},
		{
			name: "non object output schema",
			edit: func(card *WorkflowCard) {
				card.OutputSchema = `{"type":["null","string"]}`
			},
			want: WorkflowStaticReasonWorkflowOutputSchemaInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReason(t, card, tc.want)
		})
	}
}

func TestStaticWorkflowCard_RejectsMalformedPricing(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WorkflowCard)
		want string
	}{
		{
			name: "missing pricing",
			edit: func(card *WorkflowCard) {
				card.Pricing = nil
			},
			want: WorkflowStaticReasonPricingRequired,
		},
		{
			name: "empty pricing model",
			edit: func(card *WorkflowCard) {
				card.Pricing.PricingModel = ""
			},
			want: WorkflowStaticReasonPricingModelInvalid,
		},
		{
			name: "padded pricing model",
			edit: func(card *WorkflowCard) {
				card.Pricing.PricingModel = " sum_steps_plus_margin "
			},
			want: WorkflowStaticReasonPricingModelInvalid,
		},
		{
			name: "unknown pricing model",
			edit: func(card *WorkflowCard) {
				card.Pricing.PricingModel = "per_call"
			},
			want: WorkflowStaticReasonPricingModelInvalid,
		},
		{
			name: "missing min bond",
			edit: func(card *WorkflowCard) {
				card.Pricing.MinBond = sdk.Coin{}
			},
			want: WorkflowStaticReasonPricingMinBondInvalid,
		},
		{
			name: "negative min bond",
			edit: func(card *WorkflowCard) {
				card.Pricing.MinBond.Amount = sdkmath.NewInt(-1)
			},
			want: WorkflowStaticReasonPricingMinBondInvalid,
		},
		{
			name: "zero min bond",
			edit: func(card *WorkflowCard) {
				card.Pricing.MinBond.Amount = sdkmath.ZeroInt()
			},
			want: WorkflowStaticReasonPricingMinBondInvalid,
		},
		{
			name: "negative minimum cost",
			edit: func(card *WorkflowCard) {
				card.Pricing.MinimumCost = sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(-1)}
			},
			want: WorkflowStaticReasonPricingMinBondInvalid,
		},
		{
			name: "padded maximum cost denom",
			edit: func(card *WorkflowCard) {
				card.Pricing.MaximumCost = sdk.Coin{Denom: " ulac ", Amount: sdkmath.NewInt(10)}
			},
			want: WorkflowStaticReasonPricingMinBondInvalid,
		},
		{
			name: "minimum cost above maximum cost",
			edit: func(card *WorkflowCard) {
				card.Pricing.MinimumCost = sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(150000)}
				card.Pricing.MaximumCost = sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(120000)}
			},
			want: WorkflowStaticReasonPricingMinBondInvalid,
		},
		{
			name: "author margin bps above cap",
			edit: func(card *WorkflowCard) {
				card.Pricing.AuthorMarginBps = BPSDenominator + 1
			},
			want: WorkflowStaticReasonPricingBPSInvalid,
		},
		{
			name: "insurance bps above cap",
			edit: func(card *WorkflowCard) {
				card.Pricing.InsuranceBps = BPSDenominator + 1
			},
			want: WorkflowStaticReasonPricingBPSInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReason(t, card, tc.want)
		})
	}
}

func TestStaticWorkflowCard_RejectsMalformedPassportRequirements(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WorkflowCard)
		want string
	}{
		{
			name: "missing passport requirements",
			edit: func(card *WorkflowCard) {
				card.PassportRequirements = nil
			},
			want: WorkflowStaticReasonPassportRequired,
		},
		{
			name: "unknown min tier",
			edit: func(card *WorkflowCard) {
				card.PassportRequirements = &PassportRequirements{
					MinTier: PassportTier(99),
				}
			},
			want: WorkflowStaticReasonPassportTierInvalid,
		},
		{
			name: "unspecified min tier",
			edit: func(card *WorkflowCard) {
				card.PassportRequirements = &PassportRequirements{}
			},
			want: WorkflowStaticReasonPassportTierInvalid,
		},
		{
			name: "reputation above schema cap",
			edit: func(card *WorkflowCard) {
				card.PassportRequirements = &PassportRequirements{
					MinTier:            PassportTier_PASSPORT_TIER_STANDARD,
					MinReputationScore: MaxPassportReputationScore + 1,
				}
			},
			want: WorkflowStaticReasonPassportReputationInvalid,
		},
		{
			name: "empty badge",
			edit: func(card *WorkflowCard) {
				card.PassportRequirements = &PassportRequirements{
					MinTier:        PassportTier_PASSPORT_TIER_STANDARD,
					RequiredBadges: []string{"verified-spend", " "},
				}
			},
			want: WorkflowStaticReasonPassportBadgeInvalid,
		},
		{
			name: "padded badge",
			edit: func(card *WorkflowCard) {
				card.PassportRequirements = &PassportRequirements{
					MinTier:        PassportTier_PASSPORT_TIER_STANDARD,
					RequiredBadges: []string{" verified-spend "},
				}
			},
			want: WorkflowStaticReasonPassportBadgeInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReason(t, card, tc.want)
		})
	}
}

func TestStaticWorkflowCard_RejectsMalformedGovernance(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WorkflowCard)
		want string
	}{
		{
			name: "missing governance",
			edit: func(card *WorkflowCard) {
				card.Governance = nil
			},
			want: WorkflowStaticReasonGovernanceRequired,
		},
		{
			name: "missing author addresses",
			edit: func(card *WorkflowCard) {
				card.Governance.AuthorAddresses = nil
			},
			want: WorkflowStaticReasonGovernanceAuthorInvalid,
		},
		{
			name: "empty author address",
			edit: func(card *WorkflowCard) {
				card.Governance.AuthorAddresses = []string{"lumera1workflowauthor", " "}
			},
			want: WorkflowStaticReasonGovernanceAuthorInvalid,
		},
		{
			name: "padded author address",
			edit: func(card *WorkflowCard) {
				card.Governance.AuthorAddresses = []string{" lumera1workflowauthor "}
			},
			want: WorkflowStaticReasonGovernanceAuthorInvalid,
		},
		{
			name: "unspecified upgrade policy",
			edit: func(card *WorkflowCard) {
				card.Governance.UpgradePolicy = UpgradePolicy_UPGRADE_POLICY_UNSPECIFIED
			},
			want: WorkflowStaticReasonGovernancePolicyInvalid,
		},
		{
			name: "unknown upgrade policy",
			edit: func(card *WorkflowCard) {
				card.Governance.UpgradePolicy = UpgradePolicy(99)
			},
			want: WorkflowStaticReasonGovernancePolicyInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReason(t, card, tc.want)
		})
	}
}

func TestStaticWorkflowCard_RejectsMalformedLicenseLane(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WorkflowCard)
	}{
		{
			name: "missing license lane",
			edit: func(card *WorkflowCard) {
				card.LicenseLane = ""
			},
		},
		{
			name: "padded license lane",
			edit: func(card *WorkflowCard) {
				card.LicenseLane = " community "
			},
		},
		{
			name: "unknown license lane",
			edit: func(card *WorkflowCard) {
				card.LicenseLane = "free"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReason(t, card, WorkflowStaticReasonLicenseLaneInvalid)
		})
	}
}

func TestStaticWorkflowCard_RejectsMalformedCategories(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WorkflowCard)
	}{
		{
			name: "missing categories",
			edit: func(card *WorkflowCard) {
				card.Categories = nil
			},
		},
		{
			name: "empty category",
			edit: func(card *WorkflowCard) {
				card.Categories = []string{"automation", ""}
			},
		},
		{
			name: "whitespace category",
			edit: func(card *WorkflowCard) {
				card.Categories = []string{"automation", " "}
			},
		},
		{
			name: "padded category",
			edit: func(card *WorkflowCard) {
				card.Categories = []string{" automation "}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReason(t, card, WorkflowStaticReasonCategoriesInvalid)
		})
	}
}

func TestStaticWorkflowCard_RejectsMalformedMetadata(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WorkflowCard)
	}{
		{
			name: "missing display name",
			edit: func(card *WorkflowCard) {
				card.DisplayName = ""
			},
		},
		{
			name: "padded display name",
			edit: func(card *WorkflowCard) {
				card.DisplayName = " Static validation fixture "
			},
		},
		{
			name: "missing author id",
			edit: func(card *WorkflowCard) {
				card.AuthorId = ""
			},
		},
		{
			name: "padded author id",
			edit: func(card *WorkflowCard) {
				card.AuthorId = " author-1 "
			},
		},
		{
			name: "missing author pubkey",
			edit: func(card *WorkflowCard) {
				card.AuthorPubkey = ""
			},
		},
		{
			name: "padded author pubkey",
			edit: func(card *WorkflowCard) {
				card.AuthorPubkey = " " + validStaticWorkflowAuthorPubkey()
			},
		},
		{
			name: "malformed author pubkey",
			edit: func(card *WorkflowCard) {
				card.AuthorPubkey = "ed448:xyz"
			},
		},
		{
			name: "empty tag",
			edit: func(card *WorkflowCard) {
				card.Tags = []string{"golden", " "}
			},
		},
		{
			name: "padded tag",
			edit: func(card *WorkflowCard) {
				card.Tags = []string{" golden "}
			},
		},
		{
			name: "empty jurisdiction",
			edit: func(card *WorkflowCard) {
				card.Jurisdictions = []string{"US", " "}
			},
		},
		{
			name: "padded jurisdiction",
			edit: func(card *WorkflowCard) {
				card.Jurisdictions = []string{" US "}
			},
		},
		// NOTE: After the gogoproto migration created_at/updated_at are value
		// time.Time (stdtime) and are always valid, so the former
		// out-of-range-timestamp metadata cases no longer represent reachable
		// states and have been removed.
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReason(t, card, WorkflowStaticReasonMetadataInvalid)
		})
	}
}

func TestStaticWorkflowCard_RejectsMissingSafetyInvariants(t *testing.T) {
	card := validStaticWorkflowCard()
	card.SafetyInvariants = nil

	assertStaticValidationReason(t, card, WorkflowStaticReasonSafetyInvariantMalformed)
}

func TestStaticWorkflowCardWithToolResolver_AcceptsResolvedTools(t *testing.T) {
	card := validStaticWorkflowCard()
	resolver := NewRegistryWorkflowToolResolver(
		registryTool("tool.step-a", "1.0.0", "asset", "string"),
		registryTool("tool.step-b", "1.0.0", "asset", "string"),
	)

	if err := StaticCheckWorkflowCardWithToolResolver(card, resolver); err != nil {
		t.Fatalf("resolved workflow card should pass static validation: %v", err)
	}
	if findings := AnalyzeWorkflowCardWithToolResolver(card, resolver); len(findings) != 0 {
		t.Fatalf("resolved workflow card should not produce findings: %+v", findings)
	}

	card = validStaticWorkflowCard()
	card.Dag[1].InputBinding = "$.steps.step-a.output.result"
	if err := StaticCheckWorkflowCardWithToolResolver(card, resolver); err != nil {
		t.Fatalf("step output field binding should pass static validation: %v", err)
	}
}

func TestStaticWorkflowCardWithToolResolver_RejectsMissingAndVersionMismatch(t *testing.T) {
	cases := []struct {
		name     string
		resolver WorkflowToolResolver
		want     string
	}{
		{
			name:     "missing tool",
			resolver: NewRegistryWorkflowToolResolver(),
			want:     WorkflowStaticReasonStepToolNotFound,
		},
		{
			name: "missing exact version",
			resolver: NewRegistryWorkflowToolResolver(
				registryTool("tool.step-a", "2.0.0", "asset", "string"),
				registryTool("tool.step-b", "2.0.0", "asset", "string"),
			),
			want: WorkflowStaticReasonStepToolVersionMismatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStaticValidationReasonWithToolResolver(t, validStaticWorkflowCard(), tc.resolver, tc.want)
		})
	}
}

func TestStaticWorkflowCardWithToolResolver_RejectsInputBindingSourceErrors(t *testing.T) {
	resolver := NewRegistryWorkflowToolResolver(
		registryTool("tool.step-a", "1.0.0", "asset", "string"),
		registryTool("tool.step-b", "1.0.0", "asset", "string"),
	)

	cases := []struct {
		name string
		edit func(*WorkflowCard)
		want string
	}{
		{
			name: "unknown workflow input",
			edit: func(card *WorkflowCard) {
				card.Dag[0].InputBinding = "$.inputs.missing"
			},
			want: WorkflowStaticReasonStepInputUnknownInput,
		},
		{
			name: "unknown output step",
			edit: func(card *WorkflowCard) {
				card.Dag[1].InputBinding = "$.steps.missing.output"
			},
			want: WorkflowStaticReasonStepInputUnknownStep,
		},
		{
			name: "missing depends_on for output read",
			edit: func(card *WorkflowCard) {
				card.Dag[1].InputBinding = "$.steps.step-a.output"
				card.Dag[1].DependsOn = nil
			},
			want: WorkflowStaticReasonStepInputMissingDep,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := validStaticWorkflowCard()
			tc.edit(card)
			assertStaticValidationReasonWithToolResolver(t, card, resolver, tc.want)
		})
	}
}

func TestStaticWorkflowCardWithToolResolver_RejectsSchemaMismatchAndMalformedToolSchemas(t *testing.T) {
	cases := []struct {
		name     string
		resolver WorkflowToolResolver
		want     string
	}{
		{
			name: "input schema mismatch",
			resolver: NewRegistryWorkflowToolResolver(
				registryTool("tool.step-a", "1.0.0", "asset", "number"),
				registryTool("tool.step-b", "1.0.0", "asset", "string"),
			),
			want: WorkflowStaticReasonStepInputSchemaMismatch,
		},
		{
			name: "malformed tool input schema",
			resolver: NewRegistryWorkflowToolResolver(
				registryToolWithSchemas("tool.step-a", "1.0.0", "{", `{"type":"object"}`),
				registryTool("tool.step-b", "1.0.0", "asset", "string"),
			),
			want: WorkflowStaticReasonStepToolInputMalformed,
		},
		{
			name: "malformed tool output schema",
			resolver: NewRegistryWorkflowToolResolver(
				registryToolWithSchemas("tool.step-a", "1.0.0", workflowObjectSchema("asset", "string"), "{"),
				registryTool("tool.step-b", "1.0.0", "asset", "string"),
			),
			want: WorkflowStaticReasonStepToolOutputMalformed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStaticValidationReasonWithToolResolver(t, validStaticWorkflowCard(), tc.resolver, tc.want)
		})
	}
}

func TestStaticWorkflowCard_MsgValidateBasicRejectsDAGCycle(t *testing.T) {
	msg := &MsgPublishWorkflow{
		Author:       validWorkflowAuthority,
		WorkflowCard: workflowCardWithSteps(staticStep("step-a", "step-b"), staticStep("step-b", "step-a")),
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatalf("publish validation should reject cyclic workflow DAG")
	}
	var staticErr *WorkflowCardStaticValidationError
	if !errors.As(err, &staticErr) {
		t.Fatalf("expected static validation error, got %T: %v", err, err)
	}
	assertFinding(t, staticErr.Findings, WorkflowStaticSeverityError, WorkflowStaticReasonDAGCycle)
}

func FuzzWorkflowCardDAGStaticValidation(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{2, 1, 0, 1, 2})
	f.Add([]byte{1, 1, 1, 1, 1})

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) == 0 {
			return
		}
		stepCount := int(raw[0]%8) + 1
		card := workflowCardWithSteps()
		for i := 0; i < stepCount; i++ {
			deps := make([]string, 0, 2)
			if len(raw) > 1 {
				depIndex := int(raw[(i+1)%len(raw)]) % stepCount
				if depIndex != i {
					deps = append(deps, fmt.Sprintf("step-%02d", depIndex))
				}
			}
			step := staticStep(fmt.Sprintf("step-%02d", i), deps...)
			if raw[i%len(raw)]%5 == 0 {
				step.Condition = "false"
			}
			card.Dag = append(card.Dag, step)
		}
		_ = AnalyzeWorkflowCard(card)
		_ = StaticCheckWorkflowCard(card)
	})
}

func validStaticWorkflowCard() *WorkflowCard {
	return workflowCardWithSteps(staticStep("step-a"), staticStep("step-b", "step-a"))
}

func workflowCardWithSteps(steps ...*Step) *WorkflowCard {
	return &WorkflowCard{
		WorkflowId:   "wf-static",
		Version:      "1.0.0",
		DisplayName:  "Static validation fixture",
		AuthorId:     "author-1",
		AuthorPubkey: validStaticWorkflowAuthorPubkey(),
		Categories:   []string{"agent-contracts"},
		LicenseLane:  "byo_key",
		Dag:          steps,
		InputSchema:  `{"type":"object","properties":{"asset":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		Pricing:      validStaticWorkflowPricing(),
		PassportRequirements: &PassportRequirements{
			MinTier: PassportTier_PASSPORT_TIER_BASIC,
		},
		Governance:       validStaticWorkflowGovernance(),
		SafetyInvariants: []*SafetyInvariant{validStaticWorkflowSafetyInvariant()},
	}
}

func validStaticWorkflowAuthorPubkey() string {
	return "ed448:" + strings.Repeat("a", 114)
}

func validStaticWorkflowSafetyInvariant() *SafetyInvariant {
	return &SafetyInvariant{
		InvariantId: "total_cost_bound",
		Expression:  "total_cost <= max_cost",
		Phase:       InvariantPhase_INVARIANT_PHASE_LOCK,
		Severity:    "error",
		ErrorCode:   "workflow_cost_exceeded",
		HintMessage: "Keep the locked workflow cost within the signed quote budget.",
	}
}

func validStaticWorkflowPricing() *WorkflowPricing {
	return &WorkflowPricing{
		PricingModel: "sum_steps_plus_margin",
		MinBond:      sdk.NewCoin("ulac", sdkmath.NewInt(1000000)),
	}
}

func validStaticWorkflowGovernance() *Governance {
	return &Governance{
		AuthorAddresses: []string{"lumera1workflowauthor"},
		UpgradePolicy:   UpgradePolicy_UPGRADE_POLICY_SEMVER_COMPATIBLE,
	}
}

func staticStep(id string, deps ...string) *Step {
	return &Step{
		StepId:                id,
		ToolId:                "tool." + id,
		ToolVersionConstraint: "1.0.0",
		InputBinding:          "$.inputs.asset",
		MaxSubCost:            sdk.NewCoin("ulac", sdkmath.NewInt(1)),
		SubSloP95Ms:           1000,
		RetryPolicy:           &RetryPolicy{MaxAttempts: 1},
		FailureAction:         FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
		SideEffect:            SideEffect_SIDE_EFFECT_REVERSIBLE,
		DependsOn:             deps,
	}
}

func registryTool(toolID string, version string, requiredField string, requiredType string) *registrytypes.ToolCard {
	return registryToolWithSchemas(toolID, version, workflowObjectSchema(requiredField, requiredType), `{"type":"object","properties":{"result":{"type":"string"}}}`)
}

func registryToolWithSchemas(toolID string, version string, inputSchema string, outputSchema string) *registrytypes.ToolCard {
	return &registrytypes.ToolCard{
		ToolId:       toolID,
		Version:      version,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
	}
}

func workflowObjectSchema(requiredField string, requiredType string) string {
	return fmt.Sprintf(`{"type":"object","required":[%q],"properties":{%q:{"type":%q}}}`, requiredField, requiredField, requiredType)
}

func assertStaticValidationReason(t *testing.T, card *WorkflowCard, reason string) {
	t.Helper()
	err := StaticCheckWorkflowCard(card)
	if err == nil {
		t.Fatalf("expected static validation error %s", reason)
	}
	var staticErr *WorkflowCardStaticValidationError
	if !errors.As(err, &staticErr) {
		t.Fatalf("expected static validation error, got %T: %v", err, err)
	}
	assertFinding(t, staticErr.Findings, WorkflowStaticSeverityError, reason)
}

func assertStaticValidationReasonWithToolResolver(t *testing.T, card *WorkflowCard, resolver WorkflowToolResolver, reason string) {
	t.Helper()
	err := StaticCheckWorkflowCardWithToolResolver(card, resolver)
	if err == nil {
		t.Fatalf("expected static validation error %s", reason)
	}
	var staticErr *WorkflowCardStaticValidationError
	if !errors.As(err, &staticErr) {
		t.Fatalf("expected static validation error, got %T: %v", err, err)
	}
	assertFinding(t, staticErr.Findings, WorkflowStaticSeverityError, reason)
}

func assertFinding(t *testing.T, findings []WorkflowCardStaticFinding, severity string, reason string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Severity == severity && finding.ReasonCode == reason {
			return
		}
	}
	t.Fatalf("missing %s finding %s in %+v", severity, reason, findings)
}
