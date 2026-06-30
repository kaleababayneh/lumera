package types

import "testing"

func FuzzInvariants_AdversarialExpressions(f *testing.F) {
	for _, seed := range []string{
		"total_cost <= max_cost",
		"total_cost <= 1000",
		`jurisdiction in ["US", "GB"]`,
		"any step.outcome == error -> revert_bundle",
		"steps.fetch_price.outcome == 'success'",
		"count(successful([primary_search, archive_search])) == 1",
		"true",
		"(((((((((",
		"jurisdiction in [",
		"any step.outcome == error \u2192 revert_bundle",
	} {
		f.Add(seed)
	}

	input := InvariantEvaluationInput{
		TotalCost:    "10",
		MaxCost:      "20",
		Jurisdiction: "US",
		AllowList:    []string{"US"},
		Steps: []InvariantStepResult{
			{StepID: "fetch_price", Outcome: WorkflowStepOutcomeSuccess},
			{StepID: "primary_search", Outcome: WorkflowStepOutcomeFailed},
			{StepID: "archive_search", Outcome: WorkflowStepOutcomeSuccess},
		},
	}

	f.Fuzz(func(t *testing.T, expression string) {
		invariant := validInvariant("fuzz_guard", expression, InvariantPhase_INVARIANT_PHASE_VERIFY)
		_, _ = ParseInvariantExpression(expression)
		_, _ = EvaluateSafetyInvariant(invariant, InvariantPhase_INVARIANT_PHASE_VERIFY, input)
	})
}
