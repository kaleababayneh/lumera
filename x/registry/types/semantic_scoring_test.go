package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultSemanticScoringContract(t *testing.T) {
	contract := DefaultSemanticScoringContract()

	require.NoError(t, contract.Validate())
	require.Equal(t, SemanticScoringContractVersion, contract.Version)
	require.Equal(t, 0.85, contract.Threshold)
	require.Len(t, contract.Dimensions, 4)
	require.Contains(t, contract.FixturePlan, "semantic-fail-critical-number-drift")
	require.Contains(t, contract.ReplayInputs, "semantic.dimension_scores")
	require.Contains(t, contract.RequiredLogFields, "semantic_contract_version")
	require.NotEmpty(t, contract.SuccessSemantics)
	require.NotEmpty(t, contract.FailureSemantics)
}

func TestSemanticScoringContractValidateRejectsDimensionDrift(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Dimensions[0].Weight = 0.40

	err := contract.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "required_facts weight")
}

func TestSemanticIntermediateRepresentationValidateRejectsVersionDrift(t *testing.T) {
	representation := SemanticIntermediateRepresentation{
		Version:       "lumera.harness.semantic_ir.v0",
		Format:        "json",
		StructureKind: "object",
	}

	err := representation.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "version")
}

func TestSemanticExplanationValidate(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 0.92,
		Decision:       SemanticDecisionPass,
		ExpectedRepresentation: &SemanticIntermediateRepresentation{
			Version:       SemanticIntermediateRepresentationVersion,
			Format:        "json",
			StructureKind: "object",
			StructureKeys: []string{"count", "items", "status"},
			RequiredFacts: []string{"alpha", "pass"},
			CriticalFacts: []string{"num:3"},
		},
		ActualRepresentation: &SemanticIntermediateRepresentation{
			Version:       SemanticIntermediateRepresentationVersion,
			Format:        "json",
			StructureKind: "object",
			StructureKeys: []string{"count", "items", "status"},
			RequiredFacts: []string{"alpha", "pass"},
			CriticalFacts: []string{"num:3"},
		},
		Dimensions: []SemanticDimensionResult{
			{Name: SemanticDimensionRequiredFacts, Weight: 0.45, Score: 1.0, Passed: true},
			{Name: SemanticDimensionCriticalFacts, Weight: 0.25, Score: 1.0, Passed: true},
			{Name: SemanticDimensionStructureAlignment, Weight: 0.20, Score: 0.80, Passed: true},
			{Name: SemanticDimensionNormalizedOverlap, Weight: 0.10, Score: 0.88, Passed: true},
		},
		NormalizedExpected: "status pass count 3 alpha",
		NormalizedActual:   "status pass count 3 alpha",
	}

	require.NoError(t, explanation.Validate())
}
