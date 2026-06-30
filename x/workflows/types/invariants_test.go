package types

import (
	"errors"
	"strings"
	"testing"
)

func TestInvariants_Grammar_Accept(t *testing.T) {
	valid := []string{
		"total_cost <= max_cost",
		"total_cost<=max_cost",
		"total_cost <= 0",
		"total_cost <= 1",
		"total_cost <= 100000",
		"total_cost <= 123.45",
		"jurisdiction in allow_list",
		`jurisdiction in ["US"]`,
		`jurisdiction in ["US","EU"]`,
		`jurisdiction in ["US", "GB"]`,
		`jurisdiction in ["SG", "JP", "AU"]`,
		"any step.outcome == error -> revert_bundle",
		"any step.outcome == error \u2192 revert_bundle",
		"any step returns error -> revert",
		"any step returns error -> revert_bundle",
		"steps.fetch_price.outcome == 'success'",
		"steps.fetch_price.outcome == 'failed'",
		"steps.redact_result.outcome == \"success\"",
		"all(step.outcome == 'success' for step in steps)",
		"count(successful([primary_search, archive_search])) == 1",
	}

	for _, expr := range valid {
		if _, err := ParseInvariantExpression(expr); err != nil {
			t.Fatalf("%q should parse: %v", expr, err)
		}
	}
}

func TestInvariants_Grammar_Reject(t *testing.T) {
	invalid := []string{
		"",
		"true",
		"false",
		"1 == 1",
		"1 == 0",
		"total_cost <= total_cost",
		"max_cost <= max_cost",
		"total_cost <= max_cost || true",
		"total_cost < max_cost",
		"total_cost <= -1",
		"jurisdiction allow_list",
		"jurisdiction in []",
		`jurisdiction in ["*"]`,
		`jurisdiction in [US]`,
		"any error -> revert",
		"any step.outcome == error -> skip_downstream",
		"steps..outcome == 'success'",
		"steps.fetch_price.status == 'success'",
		"steps.fetch_price.outcome == 'ok'",
		"count(successful([])) == 0",
		"drop table workflows",
	}

	for _, expr := range invalid {
		if _, err := ParseInvariantExpression(expr); err == nil {
			t.Fatalf("%q should be rejected", expr)
		}
	}
}

func TestInvariants_StaticCheck_VacuouslyTrue_Reject(t *testing.T) {
	for _, expr := range []string{"true", "1 == 1", "total_cost <= total_cost", `jurisdiction in ["*"]`} {
		invariant := validInvariant("no_op_guard", expr, InvariantPhase_INVARIANT_PHASE_LOCK)
		if err := StaticCheckSafetyInvariant(invariant); err == nil {
			t.Fatalf("%q should be rejected as vacuous", expr)
		}
	}
}

func TestInvariants_StaticCheck_RejectsNonCanonicalSeverity(t *testing.T) {
	for _, severity := range []string{"ERROR", " error ", "Warn"} {
		invariant := validInvariant("severity_guard", "total_cost <= max_cost", InvariantPhase_INVARIANT_PHASE_LOCK)
		invariant.Severity = severity
		if err := StaticCheckSafetyInvariant(invariant); err == nil {
			t.Fatalf("severity %q should be rejected as non-canonical", severity)
		}
	}
}

func TestInvariants_StaticCheck_RejectsNonCanonicalInvariantID(t *testing.T) {
	invariant := validInvariant(" severity_guard ", "total_cost <= max_cost", InvariantPhase_INVARIANT_PHASE_LOCK)
	if err := StaticCheckSafetyInvariant(invariant); err == nil {
		t.Fatal("padded invariant_id should be rejected as non-canonical")
	}
}

func TestInvariants_RuntimeCheck_LockPhase_TotalCostCap(t *testing.T) {
	invariant := validInvariant("total_cost_bound", "total_cost <= max_cost", InvariantPhase_INVARIANT_PHASE_LOCK)

	log, err := EvaluateSafetyInvariant(invariant, InvariantPhase_INVARIANT_PHASE_LOCK, InvariantEvaluationInput{
		TotalCost: "90",
		MaxCost:   "100",
	})
	if err != nil {
		t.Fatalf("evaluate passing cap: %v", err)
	}
	if !sameInvariantText(log.Result, InvariantResultPass) || !hasInvariantDigest(log.InputsDigest) || !sameInvariantText(log.Phase, "lock") {
		t.Fatalf("expected passing cap with digest, got %+v", log)
	}

	log, err = EvaluateSafetyInvariant(invariant, InvariantPhase_INVARIANT_PHASE_LOCK, InvariantEvaluationInput{
		TotalCost: "100.01",
		MaxCost:   "100",
	})
	if err == nil {
		t.Fatalf("expected cost cap violation")
	}
	var violation *InvariantViolation
	if !errors.As(err, &violation) {
		t.Fatalf("expected InvariantViolation, got %T: %v", err, err)
	}
	if !sameInvariantText(log.Result, InvariantResultFail) || !sameInvariantText(log.ReasonCode, InvariantReasonCostExceeded) {
		t.Fatalf("expected cost cap failure log, got %+v", log)
	}
}

func TestInvariants_RuntimeCheck_VerifyPhase_AnyErrorRevert(t *testing.T) {
	invariant := validInvariant("error_reverts_bundle", "any step.outcome == error -> revert_bundle", InvariantPhase_INVARIANT_PHASE_VERIFY)
	input := InvariantEvaluationInput{
		Steps: []InvariantStepResult{
			{StepID: "fetch_price", Outcome: WorkflowStepOutcomeSuccess},
			{StepID: "submit_tx", Outcome: WorkflowStepOutcomeFailed, ErrorCode: "publisher_unavailable"},
		},
	}

	log, err := EvaluateSafetyInvariant(invariant, InvariantPhase_INVARIANT_PHASE_VERIFY, input)
	if err == nil {
		t.Fatalf("expected verify-phase revert on step error")
	}
	if !sameInvariantText(log.Result, InvariantResultFail) || !sameInvariantText(log.ReasonCode, InvariantReasonStepErrorRevert) {
		t.Fatalf("expected step error revert, got %+v", log)
	}
}

func TestInvariants_JurisdictionSet_Enforcement(t *testing.T) {
	allowList := validInvariant("jurisdiction_policy", "jurisdiction in allow_list", InvariantPhase_INVARIANT_PHASE_LOCK)
	allowed, err := EvaluateSafetyInvariant(allowList, InvariantPhase_INVARIANT_PHASE_LOCK, InvariantEvaluationInput{
		Jurisdiction: "US",
		AllowList:    []string{"US", "GB"},
	})
	if err != nil {
		t.Fatalf("evaluate allow list: %v", err)
	}
	if !sameInvariantText(allowed.Result, InvariantResultPass) {
		t.Fatalf("expected jurisdiction allow, got %+v", allowed)
	}

	literalSet := validInvariant("literal_jurisdiction_policy", `jurisdiction in ["US", "GB"]`, InvariantPhase_INVARIANT_PHASE_LOCK)
	denied, err := EvaluateSafetyInvariant(literalSet, InvariantPhase_INVARIANT_PHASE_LOCK, InvariantEvaluationInput{Jurisdiction: "DE"})
	if err == nil {
		t.Fatalf("expected jurisdiction denial")
	}
	if !sameInvariantText(denied.Result, InvariantResultFail) || !sameInvariantText(denied.ReasonCode, InvariantReasonJurisdictionDenied) {
		t.Fatalf("expected jurisdiction denial, got %+v", denied)
	}
}

func TestInvariants_StaticCheck_RejectsVerifyOnlyPredicateAtLockPhase(t *testing.T) {
	invariant := validInvariant("step_error_policy", "any step.outcome == error -> revert_bundle", InvariantPhase_INVARIANT_PHASE_LOCK)
	if err := StaticCheckSafetyInvariant(invariant); err == nil {
		t.Fatalf("verify-only step outcome predicate must not be accepted at lock phase")
	}
}

func TestInvariants_VerifyPhase_EvaluatesLockAndVerifyInvariants(t *testing.T) {
	invariants := []*SafetyInvariant{
		validInvariant("total_cost_bound", "total_cost <= max_cost", InvariantPhase_INVARIANT_PHASE_LOCK),
		validInvariant("error_reverts_bundle", "any step.outcome == error -> revert_bundle", InvariantPhase_INVARIANT_PHASE_VERIFY),
	}
	logs, err := EvaluateSafetyInvariants(invariants, InvariantPhase_INVARIANT_PHASE_VERIFY, InvariantEvaluationInput{
		TotalCost: "10",
		MaxCost:   "20",
		Steps: []InvariantStepResult{
			{StepID: "fetch_price", Outcome: WorkflowStepOutcomeSuccess},
		},
	})
	if err != nil {
		t.Fatalf("verify evaluation should pass: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("verify phase should evaluate all invariants, got %d logs: %+v", len(logs), logs)
	}
	for _, log := range logs {
		if !sameInvariantText(log.Result, InvariantResultPass) || !hasInvariantDigest(log.InputsDigest) {
			t.Fatalf("expected passing structured log, got %+v", log)
		}
	}
}

func TestInvariants_MsgValidateBasic_StaticRejectsInvalidWorkflowCard(t *testing.T) {
	msg := &MsgPublishWorkflow{
		Author: validWorkflowAuthority,
		WorkflowCard: &WorkflowCard{
			WorkflowId: "wf-1",
			Version:    "1.0.0",
			SafetyInvariants: []*SafetyInvariant{
				validInvariant("no_op_guard", "true", InvariantPhase_INVARIANT_PHASE_LOCK),
			},
		},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Fatalf("publish validation should reject vacuous workflow invariant")
	}
}

func validInvariant(id, expression string, phase InvariantPhase) *SafetyInvariant {
	return &SafetyInvariant{
		InvariantId: id,
		Expression:  expression,
		Phase:       phase,
		Severity:    "error",
		ErrorCode:   "workflow_invariant_failed",
		HintMessage: "Adjust the workflow safety invariant.",
	}
}

func sameInvariantText(got, want string) bool {
	return strings.Compare(got, want) == 0
}

func hasInvariantDigest(digest string) bool {
	return len(digest) > 0
}
