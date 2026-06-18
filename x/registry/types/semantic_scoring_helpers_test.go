package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// SemanticScoringDimension.Validate tests
// -----------------------------------------------------------------------------

func TestSemanticScoringDimension_Validate_Valid(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:         "test_dimension",
		Weight:       0.5,
		MinimumScore: 0.8,
		Description:  "A test dimension",
	}
	require.NoError(t, dim.Validate())
}

func TestSemanticScoringDimension_Validate_EmptyName(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:        "",
		Weight:      0.5,
		Description: "Missing name",
	}
	err := dim.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestSemanticScoringDimension_Validate_WhitespaceName(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:        "   ",
		Weight:      0.5,
		Description: "Whitespace name",
	}
	err := dim.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestSemanticScoringDimension_Validate_ZeroWeight(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:        "zero_weight",
		Weight:      0,
		Description: "Zero weight",
	}
	err := dim.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "weight must be within (0,1]")
}

func TestSemanticScoringDimension_Validate_NegativeWeight(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:        "negative_weight",
		Weight:      -0.5,
		Description: "Negative weight",
	}
	err := dim.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "weight must be within (0,1]")
}

func TestSemanticScoringDimension_Validate_WeightAboveOne(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:        "high_weight",
		Weight:      1.5,
		Description: "Weight above 1",
	}
	err := dim.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "weight must be within (0,1]")
}

func TestSemanticScoringDimension_Validate_WeightExactlyOne(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:        "max_weight",
		Weight:      1.0,
		Description: "Exactly one weight",
	}
	require.NoError(t, dim.Validate())
}

func TestSemanticScoringDimension_Validate_NegativeMinimumScore(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:         "negative_min",
		Weight:       0.5,
		MinimumScore: -0.1,
		Description:  "Negative minimum score",
	}
	err := dim.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minimum_score must be within [0,1]")
}

func TestSemanticScoringDimension_Validate_MinimumScoreAboveOne(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:         "high_min",
		Weight:       0.5,
		MinimumScore: 1.1,
		Description:  "Minimum score above 1",
	}
	err := dim.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minimum_score must be within [0,1]")
}

func TestSemanticScoringDimension_Validate_EmptyDescription(t *testing.T) {
	dim := SemanticScoringDimension{
		Name:   "no_desc",
		Weight: 0.5,
	}
	err := dim.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "description is required")
}

// -----------------------------------------------------------------------------
// SemanticDimensionResult.Validate tests
// -----------------------------------------------------------------------------

func TestSemanticDimensionResult_Validate_Valid(t *testing.T) {
	result := SemanticDimensionResult{
		Name:   "test_result",
		Weight: 0.5,
		Score:  0.9,
		Passed: true,
		Reason: "All good",
	}
	require.NoError(t, result.Validate())
}

func TestSemanticDimensionResult_Validate_EmptyName(t *testing.T) {
	result := SemanticDimensionResult{
		Name:   "",
		Weight: 0.5,
		Score:  0.9,
	}
	err := result.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestSemanticDimensionResult_Validate_NegativeWeight(t *testing.T) {
	result := SemanticDimensionResult{
		Name:   "negative_weight",
		Weight: -0.1,
		Score:  0.9,
	}
	err := result.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "weight must be within [0,1]")
}

func TestSemanticDimensionResult_Validate_WeightAboveOne(t *testing.T) {
	result := SemanticDimensionResult{
		Name:   "high_weight",
		Weight: 1.5,
		Score:  0.9,
	}
	err := result.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "weight must be within [0,1]")
}

func TestSemanticDimensionResult_Validate_NegativeScore(t *testing.T) {
	result := SemanticDimensionResult{
		Name:   "negative_score",
		Weight: 0.5,
		Score:  -0.1,
	}
	err := result.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "score must be within [0,1]")
}

func TestSemanticDimensionResult_Validate_ScoreAboveOne(t *testing.T) {
	result := SemanticDimensionResult{
		Name:   "high_score",
		Weight: 0.5,
		Score:  1.5,
	}
	err := result.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "score must be within [0,1]")
}

func TestSemanticDimensionResult_Validate_ZeroWeightAndScore(t *testing.T) {
	result := SemanticDimensionResult{
		Name:   "zeros",
		Weight: 0,
		Score:  0,
	}
	require.NoError(t, result.Validate())
}

// -----------------------------------------------------------------------------
// SemanticIntermediateRepresentation.Validate tests
// -----------------------------------------------------------------------------

func TestSemanticIntermediateRepresentation_Validate_Valid(t *testing.T) {
	rep := SemanticIntermediateRepresentation{
		Version:       SemanticIntermediateRepresentationVersion,
		Format:        "json",
		StructureKind: "object",
		Normalized:    "normalized text",
	}
	require.NoError(t, rep.Validate())
}

func TestSemanticIntermediateRepresentation_Validate_WrongVersion(t *testing.T) {
	rep := SemanticIntermediateRepresentation{
		Version:       "wrong.version",
		Format:        "json",
		StructureKind: "object",
	}
	err := rep.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version must be")
}

func TestSemanticIntermediateRepresentation_Validate_EmptyFormat(t *testing.T) {
	rep := SemanticIntermediateRepresentation{
		Version:       SemanticIntermediateRepresentationVersion,
		Format:        "",
		StructureKind: "object",
	}
	err := rep.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "format is required")
}

func TestSemanticIntermediateRepresentation_Validate_EmptyStructureKind(t *testing.T) {
	rep := SemanticIntermediateRepresentation{
		Version:       SemanticIntermediateRepresentationVersion,
		Format:        "json",
		StructureKind: "",
	}
	err := rep.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "structure_kind is required")
}

func TestSemanticIntermediateRepresentation_Validate_WithOptionalFields(t *testing.T) {
	rep := SemanticIntermediateRepresentation{
		Version:        SemanticIntermediateRepresentationVersion,
		Format:         "json",
		StructureKind:  "array",
		Normalized:     "normalized content",
		StructureKeys:  []string{"key1", "key2"},
		RequiredFacts:  []string{"fact1"},
		CriticalFacts:  []string{"num:42"},
		SemanticTokens: []string{"token1", "token2"},
	}
	require.NoError(t, rep.Validate())
}

// -----------------------------------------------------------------------------
// SemanticExplanation.Validate tests
// -----------------------------------------------------------------------------

func TestSemanticExplanation_Validate_MinimalValid(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 0.9,
		Decision:       SemanticDecisionPass,
	}
	require.NoError(t, explanation.Validate())
}

func TestSemanticExplanation_Validate_WrongVersion(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        "wrong.version",
		Threshold:      0.85,
		CompositeScore: 0.9,
		Decision:       SemanticDecisionPass,
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version must be")
}

func TestSemanticExplanation_Validate_ZeroThreshold(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0,
		CompositeScore: 0.9,
		Decision:       SemanticDecisionPass,
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "threshold must be within (0,1]")
}

func TestSemanticExplanation_Validate_NegativeThreshold(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      -0.5,
		CompositeScore: 0.9,
		Decision:       SemanticDecisionPass,
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "threshold must be within (0,1]")
}

func TestSemanticExplanation_Validate_ThresholdAboveOne(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      1.5,
		CompositeScore: 0.9,
		Decision:       SemanticDecisionPass,
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "threshold must be within (0,1]")
}

func TestSemanticExplanation_Validate_NegativeCompositeScore(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: -0.1,
		Decision:       SemanticDecisionPass,
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "composite_score must be within [0,1]")
}

func TestSemanticExplanation_Validate_CompositeScoreAboveOne(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 1.5,
		Decision:       SemanticDecisionPass,
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "composite_score must be within [0,1]")
}

func TestSemanticExplanation_Validate_InvalidDecision(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 0.9,
		Decision:       "invalid",
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decision must be")
}

func TestSemanticExplanation_Validate_FailDecision(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 0.5,
		Decision:       SemanticDecisionFail,
	}
	require.NoError(t, explanation.Validate())
}

func TestSemanticExplanation_Validate_RejectsPassBelowThreshold(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 0.84,
		Decision:       SemanticDecisionPass,
	}

	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pass decision requires composite_score")
}

func TestSemanticExplanation_Validate_InvalidExpectedRepresentation(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 0.9,
		Decision:       SemanticDecisionPass,
		ExpectedRepresentation: &SemanticIntermediateRepresentation{
			Version:       "wrong.version",
			Format:        "json",
			StructureKind: "object",
		},
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected_representation")
}

func TestSemanticExplanation_Validate_InvalidActualRepresentation(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 0.9,
		Decision:       SemanticDecisionPass,
		ActualRepresentation: &SemanticIntermediateRepresentation{
			Version:       SemanticIntermediateRepresentationVersion,
			Format:        "",
			StructureKind: "object",
		},
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "actual_representation")
}

func TestSemanticExplanation_Validate_InvalidDimension(t *testing.T) {
	explanation := SemanticExplanation{
		Version:        SemanticScoringContractVersion,
		Threshold:      0.85,
		CompositeScore: 0.9,
		Decision:       SemanticDecisionPass,
		Dimensions: []SemanticDimensionResult{
			{Name: "", Weight: 0.5, Score: 0.9},
		},
	}
	err := explanation.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

// -----------------------------------------------------------------------------
// SemanticScoringContract.Validate tests
// -----------------------------------------------------------------------------

func TestSemanticScoringContract_Validate_EmptyVersion(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Version = ""
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version is required")
}

func TestSemanticScoringContract_Validate_WrongVersion(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Version = "wrong.version"
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSemanticScoringContract_Validate_ZeroThreshold(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Threshold = 0
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "threshold must be within (0,1]")
}

func TestSemanticScoringContract_Validate_WrongThreshold(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Threshold = 0.90 // canonical is 0.85
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "threshold must be 0.85")
}

func TestSemanticScoringContract_Validate_WrongNormalizationSteps(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.NormalizationSteps = []string{"wrong step"}
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "normalization_steps")
}

func TestSemanticScoringContract_Validate_MissingDimensions(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Dimensions = []SemanticScoringDimension{}
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dimensions must contain")
}

func TestSemanticScoringContract_Validate_WrongDimensionName(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Dimensions[0].Name = "wrong_name"
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dimension[0] name must be")
}

func TestSemanticScoringContract_Validate_WrongDimensionMinimumScore(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Dimensions[0].MinimumScore = 0.90 // canonical is 0.85
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minimum_score must be")
}

func TestSemanticScoringContract_Validate_WrongDimensionDescription(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.Dimensions[0].Description = "wrong description"
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "description must match canonical")
}

func TestSemanticScoringContract_Validate_WrongAllowedDisagreements(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.AllowedDisagreements = []string{"wrong disagreement"}
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allowed_disagreements")
}

func TestSemanticScoringContract_Validate_WrongFixturePlan(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.FixturePlan = []string{"wrong-fixture"}
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fixture_plan")
}

func TestSemanticScoringContract_Validate_WrongReplayInputs(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.ReplayInputs = []string{"wrong.input"}
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replay_inputs")
}

func TestSemanticScoringContract_Validate_WrongRequiredLogFields(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.RequiredLogFields = []string{"wrong_field"}
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required_log_fields")
}

func TestSemanticScoringContract_Validate_WrongSuccessSemantics(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.SuccessSemantics = "wrong semantics"
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "success_semantics must match canonical")
}

func TestSemanticScoringContract_Validate_WrongFailureSemantics(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	contract.FailureSemantics = []string{"wrong failure"}
	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failure_semantics")
}

// -----------------------------------------------------------------------------
// floatAlmostEqual tests
// -----------------------------------------------------------------------------

func TestFloatAlmostEqual_Equal(t *testing.T) {
	assert.True(t, floatAlmostEqual(0.85, 0.85))
}

func TestFloatAlmostEqual_VeryClose(t *testing.T) {
	assert.True(t, floatAlmostEqual(0.85, 0.85+1e-10))
}

func TestFloatAlmostEqual_NotEqual(t *testing.T) {
	assert.False(t, floatAlmostEqual(0.85, 0.86))
}

func TestFloatAlmostEqual_ZeroValues(t *testing.T) {
	assert.True(t, floatAlmostEqual(0.0, 0.0))
}

func TestFloatAlmostEqual_NearZero(t *testing.T) {
	assert.True(t, floatAlmostEqual(0.0, 1e-10))
}

// -----------------------------------------------------------------------------
// Constants tests
// -----------------------------------------------------------------------------

func TestSemanticScoringConstants(t *testing.T) {
	assert.Equal(t, "lumera.harness.semantic_rubric.v1", SemanticScoringContractVersion)
	assert.Equal(t, "lumera.harness.semantic_ir.v1", SemanticIntermediateRepresentationVersion)
	assert.Equal(t, "pass", SemanticDecisionPass)
	assert.Equal(t, "fail", SemanticDecisionFail)
	assert.Equal(t, "required_facts", SemanticDimensionRequiredFacts)
	assert.Equal(t, "critical_facts", SemanticDimensionCriticalFacts)
	assert.Equal(t, "structure_alignment", SemanticDimensionStructureAlignment)
	assert.Equal(t, "normalized_overlap", SemanticDimensionNormalizedOverlap)
}

func TestDefaultSemanticScoringContract_DimensionWeightsSum(t *testing.T) {
	contract := DefaultSemanticScoringContract()
	var weightSum float64
	for _, dim := range contract.Dimensions {
		weightSum += dim.Weight
	}
	assert.InDelta(t, 1.0, weightSum, 1e-9)
}

// -----------------------------------------------------------------------------
// validateCanonicalDimensions edge cases
// -----------------------------------------------------------------------------

func TestValidateCanonicalDimensions_DimensionValidationFails(t *testing.T) {
	// Test when a dimension has an invalid weight (fails Validate())
	contract := DefaultSemanticScoringContract()
	// Set invalid weight (must be within (0,1])
	contract.Dimensions[0].Weight = -1.0

	err := contract.Validate()
	require.Error(t, err)
	// The error comes from SemanticScoringDimension.Validate()
	assert.Contains(t, err.Error(), "weight must be within (0,1]")
}

func TestValidateCanonicalDimensions_DimensionEmptyName(t *testing.T) {
	// Test when a dimension has empty name (fails Validate())
	contract := DefaultSemanticScoringContract()
	contract.Dimensions[0].Name = ""

	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestValidateCanonicalDimensions_DimensionEmptyDescription(t *testing.T) {
	// Test when a dimension has empty description (fails Validate())
	contract := DefaultSemanticScoringContract()
	contract.Dimensions[0].Description = ""

	err := contract.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "description is required")
}

// -----------------------------------------------------------------------------
// validateCanonicalStringSlice edge cases
// -----------------------------------------------------------------------------

func TestValidateCanonicalStringSlice_AllMatch(t *testing.T) {
	// Test when all values match (success case)
	contract := DefaultSemanticScoringContract()

	// Default contract should validate successfully
	err := contract.Validate()
	require.NoError(t, err)
}

func TestValidateCanonicalStringSlice_MiddleValueMismatch(t *testing.T) {
	// Test when a middle value doesn't match
	contract := DefaultSemanticScoringContract()
	if len(contract.FixturePlan) > 1 {
		// Change a middle value
		contract.FixturePlan[1] = "wrong-middle-value"
		err := contract.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fixture_plan[1] must match canonical v1 text")
	}
}

func TestValidateCanonicalStringSlice_LastValueMismatch(t *testing.T) {
	// Test when the last value doesn't match
	contract := DefaultSemanticScoringContract()
	lastIdx := len(contract.FixturePlan) - 1
	if lastIdx >= 0 {
		contract.FixturePlan[lastIdx] = "wrong-last-value"
		err := contract.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fixture_plan")
	}
}
