package types

import (
	"strings"
	"testing"
)

func TestParseWorkflowConditionAcceptedCases(t *testing.T) {
	cases := []struct {
		name              string
		condition         string
		wantCanonical     string
		wantKind          WorkflowConditionKind
		wantStepID        string
		wantClaimPath     string
		wantOperator      WorkflowConditionOperator
		wantLiteralKind   WorkflowConditionLiteralKind
		wantLiteralBool   bool
		wantLiteralString string
		wantLiteralNumber string
	}{
		{
			name:          "true literal",
			condition:     " TRUE ",
			wantCanonical: "true",
			wantKind:      WorkflowConditionKindAlways,
		},
		{
			name:          "false literal",
			condition:     "never",
			wantCanonical: "false",
			wantKind:      WorkflowConditionKindNever,
		},
		{
			name:              "outcome comparison",
			condition:         "steps.primary_search.outcome=='FAILED'",
			wantCanonical:     "steps.primary_search.outcome == failed",
			wantKind:          WorkflowConditionKindOutcomeComparison,
			wantStepID:        "primary_search",
			wantOperator:      WorkflowConditionOperatorEqual,
			wantLiteralKind:   WorkflowConditionLiteralOutcome,
			wantLiteralString: "failed",
		},
		{
			name:            "output boolean claim",
			condition:       "steps.calc_hedge_ratio.output.policy_allowed==true",
			wantCanonical:   "steps.calc_hedge_ratio.output.policy_allowed == true",
			wantKind:        WorkflowConditionKindOutputClaimComparison,
			wantStepID:      "calc_hedge_ratio",
			wantClaimPath:   "policy_allowed",
			wantOperator:    WorkflowConditionOperatorEqual,
			wantLiteralKind: WorkflowConditionLiteralBool,
			wantLiteralBool: true,
		},
		{
			name:              "output string claim",
			condition:         `steps.score.output.label != "hold"`,
			wantCanonical:     `steps.score.output.label != "hold"`,
			wantKind:          WorkflowConditionKindOutputClaimComparison,
			wantStepID:        "score",
			wantClaimPath:     "label",
			wantOperator:      WorkflowConditionOperatorNotEqual,
			wantLiteralKind:   WorkflowConditionLiteralString,
			wantLiteralString: "hold",
		},
		{
			name:              "output number claim",
			condition:         "steps.quote.output.max_slippage_bps == 50",
			wantCanonical:     "steps.quote.output.max_slippage_bps == 50",
			wantKind:          WorkflowConditionKindOutputClaimComparison,
			wantStepID:        "quote",
			wantClaimPath:     "max_slippage_bps",
			wantOperator:      WorkflowConditionOperatorEqual,
			wantLiteralKind:   WorkflowConditionLiteralNumber,
			wantLiteralNumber: "50",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseWorkflowCondition(tc.condition)
			if err != nil {
				t.Fatalf("ParseWorkflowCondition() error = %v", err)
			}
			if got.Canonical != tc.wantCanonical {
				t.Fatalf("canonical = %q, want %q", got.Canonical, tc.wantCanonical)
			}
			if got.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.StepID != tc.wantStepID {
				t.Fatalf("step_id = %q, want %q", got.StepID, tc.wantStepID)
			}
			if strings.Join(got.ClaimPath, ".") != tc.wantClaimPath {
				t.Fatalf("claim_path = %q, want %q", strings.Join(got.ClaimPath, "."), tc.wantClaimPath)
			}
			if got.Operator != tc.wantOperator {
				t.Fatalf("operator = %q, want %q", got.Operator, tc.wantOperator)
			}
			if got.Literal.Kind != tc.wantLiteralKind {
				t.Fatalf("literal kind = %q, want %q", got.Literal.Kind, tc.wantLiteralKind)
			}
			if got.Literal.Bool != tc.wantLiteralBool {
				t.Fatalf("literal bool = %v, want %v", got.Literal.Bool, tc.wantLiteralBool)
			}
			if got.Literal.String != tc.wantLiteralString {
				t.Fatalf("literal string = %q, want %q", got.Literal.String, tc.wantLiteralString)
			}
			if got.Literal.Number != tc.wantLiteralNumber {
				t.Fatalf("literal number = %q, want %q", got.Literal.Number, tc.wantLiteralNumber)
			}

			refs, err := WorkflowConditionStepReferences(tc.condition)
			if err != nil {
				t.Fatalf("WorkflowConditionStepReferences() error = %v", err)
			}
			if len(refs) == 0 && tc.wantStepID != "" {
				t.Fatalf("step references empty, want %q", tc.wantStepID)
			}
			if len(refs) > 0 && (len(refs) != 1 || refs[0] != tc.wantStepID) {
				t.Fatalf("step references = %v, want [%s]", refs, tc.wantStepID)
			}
		})
	}
}

func TestParseWorkflowConditionRejectedCases(t *testing.T) {
	cases := []string{
		"steps.a.output.policy_allowed",
		"steps.a.output.policy_allowed == null",
		`steps.a.output.items[0] == "x"`,
		"steps.a.output.price > 10",
		"steps.a.output.ok == true && steps.b.output.ok == true",
		"exists(steps.a.output.ok)",
		"$.steps.a.output.ok == true",
		"steps.a.output.PolicyAllowed == true",
		"steps.a.output.deep.path.more.than.four == true",
		`steps.score.output.label == 'hold'`,
		"steps.quote.output.amount == -0",
	}

	for _, condition := range cases {
		t.Run(condition, func(t *testing.T) {
			if got, err := ParseWorkflowCondition(condition); err == nil {
				t.Fatalf("ParseWorkflowCondition() = %+v, want error", got)
			}
			if refs, err := WorkflowConditionStepReferences(condition); err == nil {
				t.Fatalf("WorkflowConditionStepReferences() = %v, want error", refs)
			}
		})
	}
}
