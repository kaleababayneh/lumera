package types

import (
	"context"
	"fmt"
	"math"
	"strings"
)

const (
	// SemanticScoringContractVersion identifies the deterministic semantic comparison contract.
	SemanticScoringContractVersion = "lumera.harness.semantic_rubric.v1"
	// SemanticIntermediateRepresentationVersion identifies the stable intermediate semantic representation contract.
	SemanticIntermediateRepresentationVersion = "lumera.harness.semantic_ir.v1"
	// SemanticDecisionPass means the semantic comparison cleared every deterministic guardrail.
	SemanticDecisionPass = "pass"
	// SemanticDecisionFail means the semantic comparison failed one or more deterministic guardrails.
	SemanticDecisionFail = "fail"
	// SemanticDimensionRequiredFacts scores coverage of expected required facts.
	SemanticDimensionRequiredFacts = "required_facts"
	// SemanticDimensionCriticalFacts scores preservation of numeric, boolean, and negation facts.
	SemanticDimensionCriticalFacts = "critical_facts"
	// SemanticDimensionStructureAlignment scores structural agreement after normalization.
	SemanticDimensionStructureAlignment = "structure_alignment"
	// SemanticDimensionNormalizedOverlap scores overall normalized token overlap.
	SemanticDimensionNormalizedOverlap = "normalized_overlap"
)

// SemanticScoringDimension defines one deterministic semantic-scoring dimension.
type SemanticScoringDimension struct {
	Name         string  `json:"name"`
	Weight       float64 `json:"weight"`
	Description  string  `json:"description"`
	MinimumScore float64 `json:"minimum_score,omitempty"`
}

// Validate ensures the dimension stays within the canonical score range.
func (d SemanticScoringDimension) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("semantic dimension name is required")
	}
	if d.Weight <= 0 || d.Weight > 1 {
		return fmt.Errorf("semantic dimension %s weight must be within (0,1]", d.Name)
	}
	if d.MinimumScore < 0 || d.MinimumScore > 1 {
		return fmt.Errorf("semantic dimension %s minimum_score must be within [0,1]", d.Name)
	}
	if strings.TrimSpace(d.Description) == "" {
		return fmt.Errorf("semantic dimension %s description is required", d.Name)
	}
	return nil
}

// SemanticScoringContract defines the rubric, normalization steps, and replay/log contract for semantic comparisons.
type SemanticScoringContract struct {
	Version              string                     `json:"version"`
	Threshold            float64                    `json:"threshold"`
	NormalizationSteps   []string                   `json:"normalization_steps,omitempty"`
	Dimensions           []SemanticScoringDimension `json:"dimensions,omitempty"`
	AllowedDisagreements []string                   `json:"allowed_disagreements,omitempty"`
	FixturePlan          []string                   `json:"fixture_plan,omitempty"`
	ReplayInputs         []string                   `json:"replay_inputs,omitempty"`
	RequiredLogFields    []string                   `json:"required_log_fields,omitempty"`
	SuccessSemantics     string                     `json:"success_semantics,omitempty"`
	FailureSemantics     []string                   `json:"failure_semantics,omitempty"`
}

// DefaultSemanticScoringContract returns the canonical deterministic semantic comparison rubric.
func DefaultSemanticScoringContract() SemanticScoringContract {
	return SemanticScoringContract{
		Version:   SemanticScoringContractVersion,
		Threshold: 0.85,
		NormalizationSteps: []string{
			"Trim leading/trailing whitespace, lowercase text, and collapse repeated internal whitespace.",
			"Parse JSON when possible, sort object keys deterministically, and treat semantic-mode arrays as unordered bags of normalized element facts.",
			"Extract required facts from informative text tokens after stopword removal and light stemming.",
			"Extract critical facts from numbers, boolean markers, and negation markers; dropping any critical fact forces failure.",
			"Score deterministic dimensions and emit a structured explanation with missing required and critical facts.",
		},
		Dimensions: []SemanticScoringDimension{
			{
				Name:         SemanticDimensionRequiredFacts,
				Weight:       0.45,
				Description:  "Coverage of expected required facts after normalization.",
				MinimumScore: 0.85,
			},
			{
				Name:         SemanticDimensionCriticalFacts,
				Weight:       0.25,
				Description:  "Preservation of numeric, boolean, and negation facts.",
				MinimumScore: 1.0,
			},
			{
				Name:         SemanticDimensionStructureAlignment,
				Weight:       0.20,
				Description:  "Agreement on structural intent, including JSON keys or cross-format representation rules.",
				MinimumScore: 0.50,
			},
			{
				Name:         SemanticDimensionNormalizedOverlap,
				Weight:       0.10,
				Description:  "Overall overlap after normalization and canonicalization.",
				MinimumScore: 0.0,
			},
		},
		AllowedDisagreements: []string{
			"Semantic mode may pass despite exact byte mismatch when differences are limited to whitespace, punctuation, casing, JSON object key order, array ordering, or additional explanatory text.",
			"Semantic mode never overrides explicit exact/json/numeric/schema comparison modes; it only applies when compare_mode is semantic.",
			"Semantic mode must fail when required-fact coverage drops below the declared minimum or any critical numeric, boolean, or negation fact is lost.",
		},
		FixturePlan: []string{
			"semantic-pass-formatting-noise",
			"semantic-pass-json-key-order",
			"semantic-fail-critical-number-drift",
			"semantic-fail-negation-drift",
		},
		ReplayInputs: []string{
			"task.expected.compare_mode",
			"manifest.config.semantic_threshold",
			"semantic.normalized_expected",
			"semantic.normalized_actual",
			"semantic.expected_representation",
			"semantic.actual_representation",
			"semantic.dimension_scores",
			"semantic.missing_required_facts",
			"semantic.missing_critical_facts",
		},
		RequiredLogFields: []string{
			"task_id",
			"compare_mode",
			"semantic_contract_version",
			"semantic_threshold",
			"semantic_score",
			"semantic_decision",
			"semantic_expected_representation_version",
			"semantic_actual_representation_version",
			"semantic_expected_structure_kind",
			"semantic_actual_structure_kind",
			"semantic_missing_required",
			"semantic_missing_critical",
		},
		SuccessSemantics: "Semantic comparison passes only when the weighted deterministic rubric meets threshold and every minimum-score guarantee holds.",
		FailureSemantics: []string{
			"Loss of any critical numeric, boolean, or negation fact fails the comparison.",
			"Insufficient required-fact coverage fails the comparison even if generic token overlap is high.",
			"Structural drift below the declared minimum fails the comparison unless cross-format representation still preserves all required and critical facts.",
		},
	}
}

// Validate ensures the contract matches the canonical v1 semantic scoring semantics.
func (c SemanticScoringContract) Validate() error {
	if strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("semantic scoring contract version is required")
	}
	if c.Version != SemanticScoringContractVersion {
		return fmt.Errorf("semantic scoring contract version is not supported: %s", c.Version)
	}
	if c.Threshold <= 0 || c.Threshold > 1 {
		return fmt.Errorf("semantic scoring contract threshold must be within (0,1]")
	}

	canonical := DefaultSemanticScoringContract()
	if !floatAlmostEqual(c.Threshold, canonical.Threshold) {
		return fmt.Errorf("semantic scoring contract threshold must be %0.2f", canonical.Threshold)
	}
	if err := validateCanonicalStringSlice("semantic scoring contract normalization_steps", c.NormalizationSteps, canonical.NormalizationSteps); err != nil {
		return err
	}
	if err := validateCanonicalDimensions(c.Dimensions, canonical.Dimensions); err != nil {
		return err
	}
	if err := validateCanonicalStringSlice("semantic scoring contract allowed_disagreements", c.AllowedDisagreements, canonical.AllowedDisagreements); err != nil {
		return err
	}
	if err := validateCanonicalStringSlice("semantic scoring contract fixture_plan", c.FixturePlan, canonical.FixturePlan); err != nil {
		return err
	}
	if err := validateCanonicalStringSlice("semantic scoring contract replay_inputs", c.ReplayInputs, canonical.ReplayInputs); err != nil {
		return err
	}
	if err := validateCanonicalStringSlice("semantic scoring contract required_log_fields", c.RequiredLogFields, canonical.RequiredLogFields); err != nil {
		return err
	}
	if c.SuccessSemantics != canonical.SuccessSemantics {
		return fmt.Errorf("semantic scoring contract success_semantics must match canonical v1 text")
	}
	if err := validateCanonicalStringSlice("semantic scoring contract failure_semantics", c.FailureSemantics, canonical.FailureSemantics); err != nil {
		return err
	}
	return nil
}

// SemanticDimensionResult captures one scored semantic dimension for an explanation.
type SemanticDimensionResult struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	Score  float64 `json:"score"`
	Passed bool    `json:"passed"`
	Reason string  `json:"reason,omitempty"`
}

// Validate ensures the result stays within deterministic scoring bounds.
func (r SemanticDimensionResult) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("semantic dimension result name is required")
	}
	if r.Weight < 0 || r.Weight > 1 {
		return fmt.Errorf("semantic dimension result %s weight must be within [0,1]", r.Name)
	}
	if r.Score < 0 || r.Score > 1 {
		return fmt.Errorf("semantic dimension result %s score must be within [0,1]", r.Name)
	}
	return nil
}

// SemanticIntermediateRepresentation captures the stable canonical features used during semantic scoring.
type SemanticIntermediateRepresentation struct {
	Version        string   `json:"version"`
	Format         string   `json:"format"`
	StructureKind  string   `json:"structure_kind"`
	Normalized     string   `json:"normalized,omitempty"`
	StructureKeys  []string `json:"structure_keys,omitempty"`
	RequiredFacts  []string `json:"required_facts,omitempty"`
	CriticalFacts  []string `json:"critical_facts,omitempty"`
	SemanticTokens []string `json:"semantic_tokens,omitempty"`
}

// Validate ensures the intermediate representation exposes the canonical v1 shape.
func (r SemanticIntermediateRepresentation) Validate() error {
	if r.Version != SemanticIntermediateRepresentationVersion {
		return fmt.Errorf("semantic intermediate representation version must be %s", SemanticIntermediateRepresentationVersion)
	}
	if strings.TrimSpace(r.Format) == "" {
		return fmt.Errorf("semantic intermediate representation format is required")
	}
	if strings.TrimSpace(r.StructureKind) == "" {
		return fmt.Errorf("semantic intermediate representation structure_kind is required")
	}
	return nil
}

// SemanticExplanation records the deterministic semantic scoring decision for a comparison.
type SemanticExplanation struct {
	Version                string                              `json:"version"`
	Threshold              float64                             `json:"threshold"`
	CompositeScore         float64                             `json:"composite_score"`
	Decision               string                              `json:"decision"`
	ExpectedRepresentation *SemanticIntermediateRepresentation `json:"expected_representation,omitempty"`
	ActualRepresentation   *SemanticIntermediateRepresentation `json:"actual_representation,omitempty"`
	NormalizedExpected     string                              `json:"normalized_expected,omitempty"`
	NormalizedActual       string                              `json:"normalized_actual,omitempty"`
	Dimensions             []SemanticDimensionResult           `json:"dimensions,omitempty"`
	MissingRequiredFacts   []string                            `json:"missing_required_facts,omitempty"`
	MissingCriticalFacts   []string                            `json:"missing_critical_facts,omitempty"`
	Notes                  []string                            `json:"notes,omitempty"`
}

// Validate ensures the explanation stays within the canonical semantic scoring contract.
func (e SemanticExplanation) Validate() error {
	if e.Version != SemanticScoringContractVersion {
		return fmt.Errorf("semantic explanation version must be %s", SemanticScoringContractVersion)
	}
	if e.Threshold <= 0 || e.Threshold > 1 {
		return fmt.Errorf("semantic explanation threshold must be within (0,1]")
	}
	if e.CompositeScore < 0 || e.CompositeScore > 1 {
		return fmt.Errorf("semantic explanation composite_score must be within [0,1]")
	}
	switch e.Decision {
	case SemanticDecisionPass, SemanticDecisionFail:
	default:
		return fmt.Errorf("semantic explanation decision must be %s or %s", SemanticDecisionPass, SemanticDecisionFail)
	}
	if e.Decision == SemanticDecisionPass && e.CompositeScore < e.Threshold {
		return fmt.Errorf("semantic explanation pass decision requires composite_score >= threshold")
	}
	if e.ExpectedRepresentation != nil {
		if err := e.ExpectedRepresentation.Validate(); err != nil {
			return fmt.Errorf("semantic explanation expected_representation: %w", err)
		}
	}
	if e.ActualRepresentation != nil {
		if err := e.ActualRepresentation.Validate(); err != nil {
			return fmt.Errorf("semantic explanation actual_representation: %w", err)
		}
	}
	for _, dimension := range e.Dimensions {
		if err := dimension.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// SemanticScoringExplainer provides deterministic semantic explanations over expected and actual outputs.
type SemanticScoringExplainer interface {
	ExplainSemanticScore(ctx context.Context, expected []byte, actual []byte, contract SemanticScoringContract) (*SemanticExplanation, error)
}

// SemanticScoringContractStore resolves named semantic scoring contracts for verifiers and explorers.
type SemanticScoringContractStore interface {
	GetSemanticScoringContract(ctx context.Context, version string) (SemanticScoringContract, error)
}

func validateCanonicalDimensions(actual []SemanticScoringDimension, expected []SemanticScoringDimension) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("semantic scoring contract dimensions must contain %d canonical entries", len(expected))
	}
	for i := range expected {
		if err := actual[i].Validate(); err != nil {
			return err
		}
		if actual[i].Name != expected[i].Name {
			return fmt.Errorf("semantic scoring contract dimension[%d] name must be %s", i, expected[i].Name)
		}
		if !floatAlmostEqual(actual[i].Weight, expected[i].Weight) {
			return fmt.Errorf("semantic scoring contract dimension %s weight must be %0.2f", expected[i].Name, expected[i].Weight)
		}
		if !floatAlmostEqual(actual[i].MinimumScore, expected[i].MinimumScore) {
			return fmt.Errorf("semantic scoring contract dimension %s minimum_score must be %0.2f", expected[i].Name, expected[i].MinimumScore)
		}
		if actual[i].Description != expected[i].Description {
			return fmt.Errorf("semantic scoring contract dimension %s description must match canonical v1 text", expected[i].Name)
		}
	}
	return nil
}

func validateCanonicalStringSlice(field string, actual []string, expected []string) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("%s must contain %d canonical entries", field, len(expected))
	}
	for i := range expected {
		if actual[i] != expected[i] {
			return fmt.Errorf("%s[%d] must match canonical v1 text", field, i)
		}
	}
	return nil
}

func floatAlmostEqual(a float64, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}
