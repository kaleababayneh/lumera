package types

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type WorkflowConditionKind string

const (
	WorkflowConditionKindEmpty                 WorkflowConditionKind = "empty"
	WorkflowConditionKindAlways                WorkflowConditionKind = "always"
	WorkflowConditionKindNever                 WorkflowConditionKind = "never"
	WorkflowConditionKindOutcomeComparison     WorkflowConditionKind = "outcome_comparison"
	WorkflowConditionKindOutputClaimComparison WorkflowConditionKind = "output_claim_comparison"
)

type WorkflowConditionLiteralKind string

const (
	WorkflowConditionLiteralBool    WorkflowConditionLiteralKind = "bool"
	WorkflowConditionLiteralString  WorkflowConditionLiteralKind = "string"
	WorkflowConditionLiteralNumber  WorkflowConditionLiteralKind = "number"
	WorkflowConditionLiteralOutcome WorkflowConditionLiteralKind = "outcome"
)

type WorkflowConditionOperator string

const (
	WorkflowConditionOperatorEqual    WorkflowConditionOperator = "=="
	WorkflowConditionOperatorNotEqual WorkflowConditionOperator = "!="
)

type WorkflowConditionLiteral struct {
	Kind   WorkflowConditionLiteralKind
	Bool   bool
	String string
	Number string
}

type WorkflowCondition struct {
	Raw       string
	Canonical string
	Kind      WorkflowConditionKind
	StepID    string
	ClaimPath []string
	Operator  WorkflowConditionOperator
	Literal   WorkflowConditionLiteral
}

var (
	workflowConditionStepIDPattern       = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
	workflowConditionClaimSegmentPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	workflowConditionDecimalPattern      = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`)
)

func ParseWorkflowCondition(raw string) (WorkflowCondition, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return WorkflowCondition{Raw: raw, Kind: WorkflowConditionKindEmpty}, nil
	}

	normalized := strings.ToLower(strings.Join(strings.Fields(trimmed), " "))
	switch normalized {
	case "true", "1 == 1":
		return WorkflowCondition{Raw: raw, Canonical: "true", Kind: WorkflowConditionKindAlways}, nil
	case "false", "0 == 1", "1 == 0", "never":
		return WorkflowCondition{Raw: raw, Canonical: "false", Kind: WorkflowConditionKindNever}, nil
	}

	left, op, right, err := splitWorkflowConditionComparison(trimmed)
	if err != nil {
		return WorkflowCondition{}, err
	}
	parts := strings.Split(left, ".")
	if len(parts) == 3 && parts[0] == "steps" && parts[2] == "outcome" {
		return parseWorkflowOutcomeCondition(raw, parts[1], op, right)
	}
	if len(parts) >= 4 && parts[0] == "steps" && parts[2] == "output" {
		return parseWorkflowOutputClaimCondition(raw, parts[1], parts[3:], op, right)
	}
	return WorkflowCondition{}, fmt.Errorf("workflow condition left side must be steps.<step_id>.outcome or steps.<step_id>.output.<claim_path>")
}

func WorkflowConditionStepReferences(raw string) ([]string, error) {
	condition, err := ParseWorkflowCondition(raw)
	if err != nil {
		return nil, err
	}
	if condition.StepID == "" {
		return nil, nil
	}
	return []string{condition.StepID}, nil
}

func splitWorkflowConditionComparison(raw string) (string, WorkflowConditionOperator, string, error) {
	idx, op := workflowConditionOperatorIndex(raw)
	if idx < 0 {
		return "", "", "", fmt.Errorf("workflow condition must contain == or !=")
	}
	left := strings.TrimSpace(raw[:idx])
	right := strings.TrimSpace(raw[idx+len(op):])
	if left == "" || right == "" {
		return "", "", "", fmt.Errorf("workflow condition comparison requires both sides")
	}
	if hasEmbeddedWorkflowOperator(right) {
		return "", "", "", fmt.Errorf("workflow condition contains more than one comparison operator")
	}
	return left, WorkflowConditionOperator(op), right, nil
}

func workflowConditionOperatorIndex(raw string) (int, string) {
	inString := false
	escaped := false
	for i := 0; i < len(raw)-1; i++ {
		c := raw[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		pair := raw[i : i+2]
		if pair == "==" || pair == "!=" {
			return i, pair
		}
	}
	return -1, ""
}

func hasEmbeddedWorkflowOperator(raw string) bool {
	idx, _ := workflowConditionOperatorIndex(raw)
	return idx >= 0
}

func parseWorkflowOutcomeCondition(raw, stepID string, op WorkflowConditionOperator, right string) (WorkflowCondition, error) {
	if !workflowConditionStepIDPattern.MatchString(stepID) {
		return WorkflowCondition{}, fmt.Errorf("workflow condition step_id must match %s: %q", workflowConditionStepIDPattern.String(), stepID)
	}
	outcome, err := parseWorkflowOutcomeLiteral(right)
	if err != nil {
		return WorkflowCondition{}, err
	}
	return WorkflowCondition{
		Raw:       raw,
		Canonical: fmt.Sprintf("steps.%s.outcome %s %s", stepID, op, outcome),
		Kind:      WorkflowConditionKindOutcomeComparison,
		StepID:    stepID,
		Operator:  op,
		Literal:   WorkflowConditionLiteral{Kind: WorkflowConditionLiteralOutcome, String: outcome},
	}, nil
}

func parseWorkflowOutputClaimCondition(raw, stepID string, claimPath []string, op WorkflowConditionOperator, right string) (WorkflowCondition, error) {
	if !workflowConditionStepIDPattern.MatchString(stepID) {
		return WorkflowCondition{}, fmt.Errorf("workflow condition step_id must match %s: %q", workflowConditionStepIDPattern.String(), stepID)
	}
	if len(claimPath) == 0 || len(claimPath) > 4 {
		return WorkflowCondition{}, fmt.Errorf("workflow condition claim path must contain one to four segments")
	}
	for _, segment := range claimPath {
		if !workflowConditionClaimSegmentPattern.MatchString(segment) {
			return WorkflowCondition{}, fmt.Errorf("workflow condition claim path segment must match %s: %q", workflowConditionClaimSegmentPattern.String(), segment)
		}
	}
	literal, canonicalLiteral, err := parseWorkflowConditionScalar(right)
	if err != nil {
		return WorkflowCondition{}, err
	}
	canonicalPath := strings.Join(claimPath, ".")
	return WorkflowCondition{
		Raw:       raw,
		Canonical: fmt.Sprintf("steps.%s.output.%s %s %s", stepID, canonicalPath, op, canonicalLiteral),
		Kind:      WorkflowConditionKindOutputClaimComparison,
		StepID:    stepID,
		ClaimPath: append([]string(nil), claimPath...),
		Operator:  op,
		Literal:   literal,
	}, nil
}

func parseWorkflowOutcomeLiteral(raw string) (string, error) {
	literal := strings.TrimSpace(raw)
	if literal == "" {
		return "", fmt.Errorf("workflow condition outcome literal cannot be empty")
	}
	if strings.HasPrefix(literal, `"`) {
		decoded, err := parseWorkflowJSONStringLiteral(literal)
		if err != nil {
			return "", fmt.Errorf("workflow condition outcome string must be quoted JSON")
		}
		literal = decoded
	} else if strings.HasPrefix(literal, "'") {
		if len(literal) < 2 || !strings.HasSuffix(literal, "'") {
			return "", fmt.Errorf("workflow condition outcome string quote is unterminated")
		}
		literal = literal[1 : len(literal)-1]
	}
	switch strings.ToLower(literal) {
	case "success":
		return "success", nil
	case "failed":
		return "failed", nil
	case "skipped":
		return "skipped", nil
	default:
		return "", fmt.Errorf("workflow condition outcome must be success, failed, or skipped")
	}
}

func parseWorkflowConditionScalar(raw string) (WorkflowConditionLiteral, string, error) {
	literal := strings.TrimSpace(raw)
	switch strings.ToLower(literal) {
	case "true":
		return WorkflowConditionLiteral{Kind: WorkflowConditionLiteralBool, Bool: true}, "true", nil
	case "false":
		return WorkflowConditionLiteral{Kind: WorkflowConditionLiteralBool, Bool: false}, "false", nil
	case "null":
		return WorkflowConditionLiteral{}, "", fmt.Errorf("workflow condition output claim literal cannot be null")
	}
	if strings.HasPrefix(literal, `"`) {
		decoded, err := parseWorkflowJSONStringLiteral(literal)
		if err != nil {
			return WorkflowConditionLiteral{}, "", fmt.Errorf("workflow condition output claim string must be quoted JSON")
		}
		if len(decoded) > 256 {
			return WorkflowConditionLiteral{}, "", fmt.Errorf("workflow condition output claim string exceeds 256 bytes")
		}
		for _, r := range decoded {
			if r < 0x20 {
				return WorkflowConditionLiteral{}, "", fmt.Errorf("workflow condition output claim string contains a control character")
			}
		}
		canonical, err := json.Marshal(decoded)
		if err != nil {
			return WorkflowConditionLiteral{}, "", fmt.Errorf("workflow condition output claim string cannot be canonicalized")
		}
		return WorkflowConditionLiteral{Kind: WorkflowConditionLiteralString, String: decoded}, string(canonical), nil
	}
	if strings.HasPrefix(literal, "'") {
		return WorkflowConditionLiteral{}, "", fmt.Errorf("workflow condition output claim strings must use JSON double quotes")
	}
	if !workflowConditionDecimalPattern.MatchString(literal) {
		return WorkflowConditionLiteral{}, "", fmt.Errorf("workflow condition output claim literal must be boolean, JSON string, or canonical decimal")
	}
	if isNegativeZeroWorkflowDecimal(literal) {
		return WorkflowConditionLiteral{}, "", fmt.Errorf("workflow condition output claim decimal cannot be negative zero")
	}
	return WorkflowConditionLiteral{Kind: WorkflowConditionLiteralNumber, Number: literal}, literal, nil
}

func parseWorkflowJSONStringLiteral(raw string) (string, error) {
	var decoded string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return "", err
	}
	return decoded, nil
}

func isNegativeZeroWorkflowDecimal(raw string) bool {
	if !strings.HasPrefix(raw, "-0") {
		return false
	}
	for _, r := range raw[1:] {
		if r != '0' && r != '.' {
			return false
		}
	}
	return true
}

func sortedWorkflowConditionStepReferences(raw string) []string {
	refs, err := WorkflowConditionStepReferences(raw)
	if err != nil || len(refs) == 0 {
		return nil
	}
	out := append([]string(nil), refs...)
	sort.Strings(out)
	return out
}
