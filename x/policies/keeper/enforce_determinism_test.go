//go:build cosmos

package keeper

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// Policy enforcement determinism conformance tests.
// These tests prove that policy evaluation produces identical decisions
// regardless of field ordering, repeated evaluations, or evaluation sequence.
// Consensus-critical: non-deterministic enforcement would cause nodes to
// disagree on whether invocations are allowed.

// TestCheckToolFilters_DeterministicAcrossRepeatedCalls proves that
// evaluating the same filters+context multiple times produces identical results.
func TestCheckToolFilters_DeterministicAcrossRepeatedCalls(t *testing.T) {
	t.Parallel()

	filters := &types.ToolFilters{
		AllowedTools:         []string{"tool-a", "tool-b", "tool-c"},
		AllowedCategories:    []string{"defi", "analytics"},
		RequiredCapabilities: []string{"encryption", "logging"},
		MinVerificationTier:  types.VerificationTier_VERIFICATION_TIER_SILVER,
		MaxDisputeRateBps:    500,
		MinUptimeBps:         9500,
	}

	tool := &ToolContext{
		ToolID:           "tool-b",
		Category:         "defi",
		Capabilities:     []string{"encryption", "logging", "compute"},
		VerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
		DisputeRateBps:   200,
		UptimeBps:        9800,
	}

	var firstResult string
	for i := 0; i < 20; i++ {
		result := checkToolFilters(filters, tool)
		if i == 0 {
			firstResult = result
			continue
		}
		require.Equal(t, firstResult, result,
			"iteration %d: filter evaluation not deterministic", i)
	}
}

// TestCheckToolFilters_ListOrderIndependent proves that the order of items
// in allowlists/denylists doesn't affect the decision.
func TestCheckToolFilters_ListOrderIndependent(t *testing.T) {
	t.Parallel()

	baseFilters := func() *types.ToolFilters {
		return &types.ToolFilters{
			AllowedTools:         []string{"tool-a", "tool-b", "tool-c"},
			AllowedCategories:    []string{"defi", "analytics", "trading"},
			RequiredCapabilities: []string{"cap1", "cap2", "cap3"},
			DeniedTools:          []string{"bad1", "bad2"},
			DeniedCategories:     []string{"gambling", "malware"},
		}
	}

	tool := &ToolContext{
		ToolID:       "tool-b",
		Category:     "defi",
		Capabilities: []string{"cap1", "cap2", "cap3", "extra"},
	}

	// Get canonical result
	canonical := checkToolFilters(baseFilters(), tool)

	rng := rand.New(rand.NewSource(42))

	// Shuffle lists 20 times
	for i := 0; i < 20; i++ {
		filters := baseFilters()

		// Shuffle each list
		rng.Shuffle(len(filters.AllowedTools), func(a, b int) {
			filters.AllowedTools[a], filters.AllowedTools[b] = filters.AllowedTools[b], filters.AllowedTools[a]
		})
		rng.Shuffle(len(filters.AllowedCategories), func(a, b int) {
			filters.AllowedCategories[a], filters.AllowedCategories[b] = filters.AllowedCategories[b], filters.AllowedCategories[a]
		})
		rng.Shuffle(len(filters.RequiredCapabilities), func(a, b int) {
			filters.RequiredCapabilities[a], filters.RequiredCapabilities[b] = filters.RequiredCapabilities[b], filters.RequiredCapabilities[a]
		})
		rng.Shuffle(len(filters.DeniedTools), func(a, b int) {
			filters.DeniedTools[a], filters.DeniedTools[b] = filters.DeniedTools[b], filters.DeniedTools[a]
		})
		rng.Shuffle(len(filters.DeniedCategories), func(a, b int) {
			filters.DeniedCategories[a], filters.DeniedCategories[b] = filters.DeniedCategories[b], filters.DeniedCategories[a]
		})

		result := checkToolFilters(filters, tool)
		require.Equal(t, canonical, result,
			"shuffle %d: list order affected filter decision", i)
	}
}

// TestCheckToolFilters_CapabilityOrderIndependent proves that the order of
// tool capabilities doesn't affect required capability matching.
func TestCheckToolFilters_CapabilityOrderIndependent(t *testing.T) {
	t.Parallel()

	filters := &types.ToolFilters{
		RequiredCapabilities: []string{"cap-a", "cap-b", "cap-c"},
	}

	baseTool := func() *ToolContext {
		return &ToolContext{
			ToolID:       "test-tool",
			Capabilities: []string{"cap-a", "cap-b", "cap-c", "cap-d"},
		}
	}

	canonical := checkToolFilters(filters, baseTool())

	rng := rand.New(rand.NewSource(123))
	for i := 0; i < 15; i++ {
		tool := baseTool()
		rng.Shuffle(len(tool.Capabilities), func(a, b int) {
			tool.Capabilities[a], tool.Capabilities[b] = tool.Capabilities[b], tool.Capabilities[a]
		})

		result := checkToolFilters(filters, tool)
		require.Equal(t, canonical, result,
			"shuffle %d: capability order affected matching", i)
	}
}

// TestCheckBudgetLimits_DeterministicDecision proves budget evaluation
// is deterministic across repeated calls.
func TestCheckBudgetLimits_DeterministicDecision(t *testing.T) {
	t.Parallel()

	budgets := &types.BudgetControls{
		PerCall:    &types.BudgetLimit{HardLimit: "1000000", SoftLimit: "800000"},
		PerSession: &types.BudgetLimit{HardLimit: "5000000", SoftLimit: "4000000"},
		PerHour:    &types.BudgetLimit{HardLimit: "10000000"},
		PerDay:     &types.BudgetLimit{HardLimit: "50000000"},
	}

	req := &InvocationRequest{
		CostMicroLAC:        500000,
		SessionCostMicroLAC: 2000000,
		HourCostMicroLAC:    5000000,
		DayCostMicroLAC:     20000000,
		WeekCostMicroLAC:    50000000,
		MonthCostMicroLAC:   100000000,
	}

	var firstReason string
	var firstWarnings []string
	for i := 0; i < 20; i++ {
		reason, warnings := checkBudgetLimits(budgets, req)
		if i == 0 {
			firstReason = reason
			firstWarnings = warnings
			continue
		}
		require.Equal(t, firstReason, reason,
			"iteration %d: budget denial reason changed", i)
		require.Equal(t, firstWarnings, warnings,
			"iteration %d: budget warnings changed", i)
	}
}

// TestCheckBudgetLimits_WarningsOrderStable proves warnings accumulate
// in a deterministic order.
func TestCheckBudgetLimits_WarningsOrderStable(t *testing.T) {
	t.Parallel()

	// Create budgets that will trigger multiple soft limit warnings
	budgets := &types.BudgetControls{
		PerCall:    &types.BudgetLimit{HardLimit: "10000000", SoftLimit: "100"},
		PerSession: &types.BudgetLimit{HardLimit: "50000000", SoftLimit: "200"},
		PerHour:    &types.BudgetLimit{HardLimit: "100000000", SoftLimit: "300"},
	}

	req := &InvocationRequest{
		CostMicroLAC:        500,
		SessionCostMicroLAC: 100,
		HourCostMicroLAC:    100,
		DayCostMicroLAC:     100,
		WeekCostMicroLAC:    100,
		MonthCostMicroLAC:   100,
	}

	var firstWarnings []string
	for i := 0; i < 10; i++ {
		_, warnings := checkBudgetLimits(budgets, req)
		if i == 0 {
			firstWarnings = warnings
			continue
		}
		require.Equal(t, firstWarnings, warnings,
			"iteration %d: warning order not stable", i)
	}
}

// TestCheckSecurityControls_Deterministic proves security control
// evaluation is deterministic.
func TestCheckSecurityControls_Deterministic(t *testing.T) {
	t.Parallel()

	security := &types.SecurityControls{
		IsolationLevel:           types.IsolationLevel_ISOLATION_LEVEL_CONTAINER,
		RequiredSandboxProfiles:  []string{"gvisor", "firecracker"},
		ForbiddenSandboxProfiles: []string{"none"},
		GeoBlocking:              []string{"CN", "RU"},
	}

	tool := &ToolContext{
		ToolID:         "test-tool",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_VM,
		SandboxProfile: "gvisor",
		Region:         "US",
	}

	var firstResult string
	for i := 0; i < 15; i++ {
		result := checkSecurityControls(security, tool)
		if i == 0 {
			firstResult = result
			continue
		}
		require.Equal(t, firstResult, result,
			"iteration %d: security control result changed", i)
	}
}

// TestCheckSecurityControls_ListOrderIndependent proves that the order of
// sandbox profiles and geo-blocking lists doesn't affect decisions.
func TestCheckSecurityControls_ListOrderIndependent(t *testing.T) {
	t.Parallel()

	baseSecurity := func() *types.SecurityControls {
		return &types.SecurityControls{
			RequiredSandboxProfiles:  []string{"profile-a", "profile-b", "profile-c"},
			ForbiddenSandboxProfiles: []string{"forbidden-x", "forbidden-y"},
			GeoBlocking:              []string{"CN", "RU", "IR", "KP"},
		}
	}

	tool := &ToolContext{
		ToolID:         "test-tool",
		SandboxProfile: "profile-b",
		Region:         "US",
	}

	canonical := checkSecurityControls(baseSecurity(), tool)

	rng := rand.New(rand.NewSource(456))
	for i := 0; i < 15; i++ {
		security := baseSecurity()
		rng.Shuffle(len(security.RequiredSandboxProfiles), func(a, b int) {
			security.RequiredSandboxProfiles[a], security.RequiredSandboxProfiles[b] =
				security.RequiredSandboxProfiles[b], security.RequiredSandboxProfiles[a]
		})
		rng.Shuffle(len(security.ForbiddenSandboxProfiles), func(a, b int) {
			security.ForbiddenSandboxProfiles[a], security.ForbiddenSandboxProfiles[b] =
				security.ForbiddenSandboxProfiles[b], security.ForbiddenSandboxProfiles[a]
		})
		rng.Shuffle(len(security.GeoBlocking), func(a, b int) {
			security.GeoBlocking[a], security.GeoBlocking[b] =
				security.GeoBlocking[b], security.GeoBlocking[a]
		})

		result := checkSecurityControls(security, tool)
		require.Equal(t, canonical, result,
			"shuffle %d: list order affected security decision", i)
	}
}

// TestCheckPrivacyControls_Deterministic proves privacy control
// evaluation is deterministic.
func TestCheckPrivacyControls_Deterministic(t *testing.T) {
	t.Parallel()

	privacy := &types.PrivacyControls{
		DataResidency: &types.DataResidency{
			AllowedRegions: []string{"US", "EU", "JP"},
			DeniedRegions:  []string{"CN", "RU"},
		},
		EncryptionRequirements: &types.EncryptionRequirements{
			AtRest:        types.EncryptionLevel_ENCRYPTION_LEVEL_AES256,
			KeyManagement: "kms",
		},
	}

	tool := &ToolContext{
		ToolID:         "test-tool",
		Region:         "US",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_VM,
	}

	var firstResult string
	for i := 0; i < 15; i++ {
		result := checkPrivacyControls(privacy, tool)
		if i == 0 {
			firstResult = result
			continue
		}
		require.Equal(t, firstResult, result,
			"iteration %d: privacy control result changed", i)
	}
}

// TestCheckPrivacyControls_RegionOrderIndependent proves that the order
// of allowed/denied regions doesn't affect decisions.
func TestCheckPrivacyControls_RegionOrderIndependent(t *testing.T) {
	t.Parallel()

	basePrivacy := func() *types.PrivacyControls {
		return &types.PrivacyControls{
			DataResidency: &types.DataResidency{
				AllowedRegions: []string{"US", "EU", "JP", "SG", "AU"},
				DeniedRegions:  []string{"CN", "RU", "IR", "KP"},
			},
		}
	}

	tool := &ToolContext{
		ToolID: "test-tool",
		Region: "EU",
	}

	canonical := checkPrivacyControls(basePrivacy(), tool)

	rng := rand.New(rand.NewSource(789))
	for i := 0; i < 15; i++ {
		privacy := basePrivacy()
		rng.Shuffle(len(privacy.DataResidency.AllowedRegions), func(a, b int) {
			privacy.DataResidency.AllowedRegions[a], privacy.DataResidency.AllowedRegions[b] =
				privacy.DataResidency.AllowedRegions[b], privacy.DataResidency.AllowedRegions[a]
		})
		rng.Shuffle(len(privacy.DataResidency.DeniedRegions), func(a, b int) {
			privacy.DataResidency.DeniedRegions[a], privacy.DataResidency.DeniedRegions[b] =
				privacy.DataResidency.DeniedRegions[b], privacy.DataResidency.DeniedRegions[a]
		})

		result := checkPrivacyControls(privacy, tool)
		require.Equal(t, canonical, result,
			"shuffle %d: region order affected privacy decision", i)
	}
}

// TestValidateBudgetInvariants_Deterministic proves budget invariant
// validation is deterministic.
func TestValidateBudgetInvariants_Deterministic(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		req  *InvocationRequest
	}{
		{
			name: "valid_monotonic",
			req: &InvocationRequest{
				HourCostMicroLAC:  100,
				DayCostMicroLAC:   500,
				WeekCostMicroLAC:  2000,
				MonthCostMicroLAC: 8000,
			},
		},
		{
			name: "invalid_hour_gt_day",
			req: &InvocationRequest{
				HourCostMicroLAC:  1000,
				DayCostMicroLAC:   500,
				WeekCostMicroLAC:  2000,
				MonthCostMicroLAC: 8000,
			},
		},
		{
			name: "invalid_day_gt_week",
			req: &InvocationRequest{
				HourCostMicroLAC:  100,
				DayCostMicroLAC:   5000,
				WeekCostMicroLAC:  2000,
				MonthCostMicroLAC: 8000,
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var firstResult string
			for i := 0; i < 10; i++ {
				result := validateBudgetInvariants(tc.req)
				if i == 0 {
					firstResult = result
					continue
				}
				require.Equal(t, firstResult, result,
					"iteration %d: invariant check not deterministic", i)
			}
		})
	}
}

// TestParseMicroLAC_Deterministic proves amount parsing is deterministic.
func TestParseMicroLAC_Deterministic(t *testing.T) {
	t.Parallel()

	testInputs := []string{
		"",
		"0",
		"1",
		"1000000",
		"18446744073709551615", // max uint64
		"  123  ",              // whitespace
		"invalid",
		"-100",
	}

	for _, input := range testInputs {
		input := input
		t.Run("input_"+input, func(t *testing.T) {
			t.Parallel()

			var firstVal uint64
			var firstOK bool
			for i := 0; i < 10; i++ {
				val, ok := parseMicroLAC(input)
				if i == 0 {
					firstVal = val
					firstOK = ok
					continue
				}
				require.Equal(t, firstOK, ok,
					"iteration %d: parse success changed", i)
				require.Equal(t, firstVal, val,
					"iteration %d: parsed value changed", i)
			}
		})
	}
}

// TestSafeAddUint64_Deterministic proves overflow-safe addition is deterministic.
func TestSafeAddUint64_Deterministic(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		a, b uint64
	}{
		{0, 0},
		{1, 1},
		{1000, 2000},
		{^uint64(0), 1},           // overflow
		{^uint64(0) - 100, 200},   // overflow
		{^uint64(0) / 2, ^uint64(0) / 2 + 10}, // overflow
	}

	for _, tc := range testCases {
		tc := tc
		t.Run("", func(t *testing.T) {
			t.Parallel()

			var firstResult uint64
			for i := 0; i < 10; i++ {
				result := safeAddUint64(tc.a, tc.b)
				if i == 0 {
					firstResult = result
					continue
				}
				require.Equal(t, firstResult, result,
					"iteration %d: safeAdd not deterministic", i)
			}
		})
	}
}

// TestCheckToolFilters_DenialPrecedence proves denial reasons follow
// a deterministic precedence order.
func TestCheckToolFilters_DenialPrecedence(t *testing.T) {
	t.Parallel()

	// Tool that violates multiple filter conditions
	tool := &ToolContext{
		ToolID:           "bad-tool",
		Category:         "gambling",
		Capabilities:     []string{"forbidden-cap"},
		VerificationTier: types.VerificationTier_VERIFICATION_TIER_UNSPECIFIED,
		DisputeRateBps:   9000,
		UptimeBps:        1000,
	}

	filters := &types.ToolFilters{
		DeniedTools:          []string{"bad-tool"},
		DeniedCategories:     []string{"gambling"},
		ForbiddenCapabilities: []string{"forbidden-cap"},
		MinVerificationTier:  types.VerificationTier_VERIFICATION_TIER_PLATINUM,
		MaxDisputeRateBps:    100,
		MinUptimeBps:         9000,
	}

	// Multiple violations exist; verify denial reason is consistent
	var firstReason string
	for i := 0; i < 20; i++ {
		reason := checkToolFilters(filters, tool)
		require.NotEmpty(t, reason, "should be denied")
		if i == 0 {
			firstReason = reason
			continue
		}
		require.Equal(t, firstReason, reason,
			"iteration %d: denial precedence not deterministic", i)
	}

	// Verify denied tools take precedence (checked first in code)
	require.Contains(t, firstReason, "explicitly denied",
		"denied tools should have highest precedence")
}

// TestCertificationOrderIndependent proves that certification list
// order doesn't affect matching.
func TestCertificationOrderIndependent(t *testing.T) {
	t.Parallel()

	filters := &types.ToolFilters{
		RequiredCertifications: []string{"soc2", "iso27001", "hipaa"},
	}

	baseTool := func() *ToolContext {
		return &ToolContext{
			ToolID:         "certified-tool",
			Certifications: []string{"soc2", "iso27001", "hipaa", "gdpr"},
		}
	}

	canonical := checkToolFilters(filters, baseTool())

	rng := rand.New(rand.NewSource(999))
	for i := 0; i < 15; i++ {
		tool := baseTool()
		rng.Shuffle(len(tool.Certifications), func(a, b int) {
			tool.Certifications[a], tool.Certifications[b] = tool.Certifications[b], tool.Certifications[a]
		})

		// Also shuffle required certifications
		filtersCopy := &types.ToolFilters{
			RequiredCertifications: make([]string, len(filters.RequiredCertifications)),
		}
		copy(filtersCopy.RequiredCertifications, filters.RequiredCertifications)
		rng.Shuffle(len(filtersCopy.RequiredCertifications), func(a, b int) {
			filtersCopy.RequiredCertifications[a], filtersCopy.RequiredCertifications[b] =
				filtersCopy.RequiredCertifications[b], filtersCopy.RequiredCertifications[a]
		})

		result := checkToolFilters(filtersCopy, tool)
		require.Equal(t, canonical, result,
			"shuffle %d: certification order affected matching", i)
	}
}

// TestEnclaveRequirements_ProviderOrderIndependent proves that allowed
// provider list order doesn't affect enclave requirement checking.
func TestEnclaveRequirements_ProviderOrderIndependent(t *testing.T) {
	t.Parallel()

	baseReq := func() *types.EnclaveRequirements {
		return &types.EnclaveRequirements{
			RequiredFor:      []string{"financial", "medical"},
			AllowedProviders: []string{"sgx", "sev", "nitro", "trustzone"},
		}
	}

	tool := &ToolContext{
		ToolID:         "enclave-tool",
		Category:       "financial",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE,
		SandboxProfile: "nitro",
	}

	canonical := checkEnclaveRequirements(baseReq(), tool)

	rng := rand.New(rand.NewSource(111))
	for i := 0; i < 15; i++ {
		req := baseReq()
		rng.Shuffle(len(req.RequiredFor), func(a, b int) {
			req.RequiredFor[a], req.RequiredFor[b] = req.RequiredFor[b], req.RequiredFor[a]
		})
		rng.Shuffle(len(req.AllowedProviders), func(a, b int) {
			req.AllowedProviders[a], req.AllowedProviders[b] = req.AllowedProviders[b], req.AllowedProviders[a]
		})

		result := checkEnclaveRequirements(req, tool)
		require.Equal(t, canonical, result,
			"shuffle %d: provider order affected enclave check", i)
	}
}

// TestPerToolBudget_KeyOrderIndependent proves that map iteration order
// for per-tool budgets doesn't affect evaluation.
func TestPerToolBudget_KeyOrderIndependent(t *testing.T) {
	t.Parallel()

	tools := []string{"tool-a", "tool-b", "tool-c", "tool-d", "tool-e"}

	// Create budgets with per-tool limits
	baseBudgets := func() *types.BudgetControls {
		perTool := make(map[string]*types.BudgetLimit)
		for _, toolID := range tools {
			perTool[toolID] = &types.BudgetLimit{
				HardLimit: "1000000",
				SoftLimit: "800000",
			}
		}
		return &types.BudgetControls{
			PerTool: perTool,
		}
	}

	req := &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-c"},
		CostMicroLAC: 500000,
	}

	// Maps in Go have random iteration order, but results should be deterministic
	var firstReason string
	var firstWarnings []string
	for i := 0; i < 20; i++ {
		reason, warnings := checkBudgetLimits(baseBudgets(), req)
		if i == 0 {
			firstReason = reason
			firstWarnings = warnings
			continue
		}
		require.Equal(t, firstReason, reason,
			"iteration %d: per-tool budget result changed", i)
		require.Equal(t, firstWarnings, warnings,
			"iteration %d: per-tool budget warnings changed", i)
	}
}

// TestPerCategoryBudget_KeyOrderIndependent proves that map iteration order
// for per-category budgets doesn't affect evaluation.
func TestPerCategoryBudget_KeyOrderIndependent(t *testing.T) {
	t.Parallel()

	categories := []string{"defi", "analytics", "trading", "governance", "oracles"}

	baseBudgets := func() *types.BudgetControls {
		perCategory := make(map[string]*types.BudgetLimit)
		for _, cat := range categories {
			perCategory[cat] = &types.BudgetLimit{
				HardLimit: "5000000",
				SoftLimit: "4000000",
			}
		}
		return &types.BudgetControls{
			PerCategory: perCategory,
		}
	}

	req := &InvocationRequest{
		Tool:         ToolContext{ToolID: "any-tool", Category: "trading"},
		CostMicroLAC: 3000000,
	}

	var firstReason string
	var firstWarnings []string
	for i := 0; i < 20; i++ {
		reason, warnings := checkBudgetLimits(baseBudgets(), req)
		if i == 0 {
			firstReason = reason
			firstWarnings = warnings
			continue
		}
		require.Equal(t, firstReason, reason,
			"iteration %d: per-category budget result changed", i)
		require.Equal(t, firstWarnings, warnings,
			"iteration %d: per-category budget warnings changed", i)
	}
}

// TestEvaluateBudgetLimit_ActionTypeDeterminism proves that different
// action types produce consistent denial reasons.
func TestEvaluateBudgetLimit_ActionTypeDeterminism(t *testing.T) {
	t.Parallel()

	actionTypes := []types.BudgetActionType{
		types.BudgetActionType_BUDGET_ACTION_TYPE_UNSPECIFIED,
		types.BudgetActionType_BUDGET_ACTION_TYPE_DENY,
		types.BudgetActionType_BUDGET_ACTION_TYPE_WARN,
		types.BudgetActionType_BUDGET_ACTION_TYPE_QUEUE,
		types.BudgetActionType_BUDGET_ACTION_TYPE_ESCALATE,
	}

	for _, actionType := range actionTypes {
		actionType := actionType
		t.Run(actionType.String(), func(t *testing.T) {
			t.Parallel()

			limit := &types.BudgetLimit{
				HardLimit:    "1000",
				ActionOnHard: actionType,
			}

			var firstReason, firstWarn string
			for i := 0; i < 10; i++ {
				reason, warn := evaluateBudgetLimit(limit, 2000, "test-scope")
				if i == 0 {
					firstReason = reason
					firstWarn = warn
					continue
				}
				require.Equal(t, firstReason, reason,
					"iteration %d: action type %s denial changed", i, actionType)
				require.Equal(t, firstWarn, warn,
					"iteration %d: action type %s warning changed", i, actionType)
			}
		})
	}
}

// TestFullPolicyEvaluation_InputShuffleStable creates a complex scenario
// and verifies the decision is stable regardless of how we construct
// the inputs (but with same values).
func TestFullPolicyEvaluation_InputShuffleStable(t *testing.T) {
	t.Parallel()

	makeFilters := func() *types.ToolFilters {
		return &types.ToolFilters{
			AllowedTools:         []string{"tool-1", "tool-2", "tool-3"},
			RequiredCapabilities: []string{"cap-a", "cap-b"},
			MinVerificationTier:  types.VerificationTier_VERIFICATION_TIER_SILVER,
		}
	}

	makeBudgets := func() *types.BudgetControls {
		return &types.BudgetControls{
			PerCall: &types.BudgetLimit{HardLimit: "10000000"},
			PerHour: &types.BudgetLimit{HardLimit: "50000000"},
		}
	}

	makeSecurity := func() *types.SecurityControls {
		return &types.SecurityControls{
			IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_CONTAINER,
		}
	}

	makeTool := func() *ToolContext {
		return &ToolContext{
			ToolID:           "tool-2",
			Capabilities:     []string{"cap-a", "cap-b", "cap-c"},
			VerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
			IsolationLevel:   types.IsolationLevel_ISOLATION_LEVEL_VM,
		}
	}

	makeReq := func() *InvocationRequest {
		return &InvocationRequest{
			Tool:              *makeTool(),
			CostMicroLAC:      1000000,
			HourCostMicroLAC:  10000000,
			DayCostMicroLAC:   20000000,
			WeekCostMicroLAC:  30000000,
			MonthCostMicroLAC: 40000000,
		}
	}

	// Evaluate each component
	var canonicalFilterResult string
	var canonicalBudgetReason string
	var canonicalBudgetWarnings []string
	var canonicalSecurityResult string

	for i := 0; i < 20; i++ {
		filterResult := checkToolFilters(makeFilters(), makeTool())
		budgetReason, budgetWarnings := checkBudgetLimits(makeBudgets(), makeReq())
		securityResult := checkSecurityControls(makeSecurity(), makeTool())

		if i == 0 {
			canonicalFilterResult = filterResult
			canonicalBudgetReason = budgetReason
			canonicalBudgetWarnings = budgetWarnings
			canonicalSecurityResult = securityResult
			continue
		}

		require.Equal(t, canonicalFilterResult, filterResult,
			"iteration %d: filter result changed", i)
		require.Equal(t, canonicalBudgetReason, budgetReason,
			"iteration %d: budget reason changed", i)
		require.Equal(t, canonicalBudgetWarnings, budgetWarnings,
			"iteration %d: budget warnings changed", i)
		require.Equal(t, canonicalSecurityResult, securityResult,
			"iteration %d: security result changed", i)
	}
}

// Verify sorting helper produces consistent results
func TestSortedKeys_Deterministic(t *testing.T) {
	t.Parallel()

	// Test that sorted iteration over maps is deterministic
	m := map[string]int{
		"zebra": 1,
		"alpha": 2,
		"beta":  3,
		"gamma": 4,
	}

	expectedOrder := []string{"alpha", "beta", "gamma", "zebra"}

	for i := 0; i < 20; i++ {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		require.Equal(t, expectedOrder, keys,
			"iteration %d: sorted keys not deterministic", i)
	}
}
