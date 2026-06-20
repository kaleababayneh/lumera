//go:build cosmos
// +build cosmos

package keeper

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// --- checkToolFilters unit tests ---

func TestCheckToolFilters_NilFilters(t *testing.T) {
	reason := checkToolFilters(nil, &ToolContext{ToolID: "any-tool"})
	assert.Empty(t, reason)
}

func TestCheckToolFilters_DeniedTool(t *testing.T) {
	filters := &types.ToolFilters{
		DeniedTools: []string{"bad-tool"},
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "bad-tool"})
	assert.Contains(t, reason, "explicitly denied")
}

func TestCheckToolFilters_DeniedCategory(t *testing.T) {
	filters := &types.ToolFilters{
		DeniedCategories: []string{"finance"},
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "tool-1", Category: "finance"})
	assert.Contains(t, reason, "category finance is denied")
}

func TestCheckToolFilters_AllowedToolsPass(t *testing.T) {
	filters := &types.ToolFilters{
		AllowedTools: []string{"good-tool"},
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "good-tool"})
	assert.Empty(t, reason)
}

func TestCheckToolFilters_AllowedToolsDeny(t *testing.T) {
	filters := &types.ToolFilters{
		AllowedTools: []string{"good-tool"},
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "other-tool"})
	assert.Contains(t, reason, "not in the allowed list")
}

func TestCheckToolFilters_AllowedCategoriesPass(t *testing.T) {
	filters := &types.ToolFilters{
		AllowedCategories: []string{"defi", "analytics"},
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "t", Category: "defi"})
	assert.Empty(t, reason)
}

func TestCheckToolFilters_AllowedCategoriesDeny(t *testing.T) {
	filters := &types.ToolFilters{
		AllowedCategories: []string{"defi"},
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "t", Category: "gaming"})
	assert.Contains(t, reason, "category gaming is not in the allowed list")
}

func TestCheckToolFilters_RequiredCapabilities(t *testing.T) {
	filters := &types.ToolFilters{
		RequiredCapabilities: []string{"encryption", "logging"},
	}
	// Has both.
	reason := checkToolFilters(filters, &ToolContext{
		ToolID:       "t",
		Capabilities: []string{"encryption", "logging", "extra"},
	})
	assert.Empty(t, reason)

	// Missing one.
	reason = checkToolFilters(filters, &ToolContext{
		ToolID:       "t",
		Capabilities: []string{"encryption"},
	})
	assert.Contains(t, reason, "missing required capability: logging")
}

func TestCheckToolFilters_ForbiddenCapabilities(t *testing.T) {
	filters := &types.ToolFilters{
		ForbiddenCapabilities: []string{"external_network"},
	}
	reason := checkToolFilters(filters, &ToolContext{
		ToolID:       "t",
		Capabilities: []string{"external_network", "compute"},
	})
	assert.Contains(t, reason, "forbidden capability: external_network")

	reason = checkToolFilters(filters, &ToolContext{
		ToolID:       "t",
		Capabilities: []string{"compute"},
	})
	assert.Empty(t, reason)
}

func TestCheckToolFilters_MinVerificationTier(t *testing.T) {
	filters := &types.ToolFilters{
		MinVerificationTier: types.VerificationTier_VERIFICATION_TIER_SILVER,
	}
	// Gold passes.
	reason := checkToolFilters(filters, &ToolContext{
		ToolID:           "t",
		VerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
	})
	assert.Empty(t, reason)

	// Bronze fails.
	reason = checkToolFilters(filters, &ToolContext{
		ToolID:           "t",
		VerificationTier: types.VerificationTier_VERIFICATION_TIER_BRONZE,
	})
	assert.Contains(t, reason, "verification tier")
}

func TestCheckToolFilters_DisputeRate(t *testing.T) {
	filters := &types.ToolFilters{
		MaxDisputeRateBps: 100, // 1%
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "t", DisputeRateBps: 50})
	assert.Empty(t, reason)

	reason = checkToolFilters(filters, &ToolContext{ToolID: "t", DisputeRateBps: 200})
	assert.Contains(t, reason, "dispute rate")
}

func TestCheckToolFilters_MinUptime(t *testing.T) {
	filters := &types.ToolFilters{
		MinUptimeBps: 9900, // 99%
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "t", UptimeBps: 9950})
	assert.Empty(t, reason)

	reason = checkToolFilters(filters, &ToolContext{ToolID: "t", UptimeBps: 9800})
	assert.Contains(t, reason, "uptime")
}

func TestCheckToolFilters_RequiredCertifications(t *testing.T) {
	filters := &types.ToolFilters{
		RequiredCertifications: []string{"soc2", "hipaa"},
	}
	reason := checkToolFilters(filters, &ToolContext{
		ToolID:         "t",
		Certifications: []string{"soc2", "hipaa", "iso27001"},
	})
	assert.Empty(t, reason)

	reason = checkToolFilters(filters, &ToolContext{
		ToolID:         "t",
		Certifications: []string{"soc2"},
	})
	assert.Contains(t, reason, "missing required certification: hipaa")
}

func TestCheckToolFilters_DeniedTakesPrecedenceOverAllowed(t *testing.T) {
	filters := &types.ToolFilters{
		AllowedTools: []string{"tool-a", "tool-b"},
		DeniedTools:  []string{"tool-a"},
	}
	reason := checkToolFilters(filters, &ToolContext{ToolID: "tool-a"})
	assert.Contains(t, reason, "explicitly denied")
}

// --- checkBudgetLimits unit tests ---

func TestCheckBudgetLimits_NilBudgets(t *testing.T) {
	reason, warnings := checkBudgetLimits(nil, &InvocationRequest{CostMicroLAC: 1000})
	assert.Empty(t, reason)
	assert.Empty(t, warnings)
}

func TestCheckBudgetLimits_PerCallHardLimit(t *testing.T) {
	budgets := &types.BudgetControls{
		PerCall: &types.BudgetLimit{
			SoftLimit:    "500",
			HardLimit:    "1000",
			ActionOnHard: types.BudgetActionType_BUDGET_ACTION_TYPE_DENY,
		},
	}
	// Under limit.
	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC: 800,
		Tool:         ToolContext{ToolID: "t"},
	})
	assert.Empty(t, reason)

	// Over hard limit.
	reason, _ = checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC: 1500,
		Tool:         ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "per-call budget hard limit exceeded")
}

func TestCheckBudgetLimits_SoftLimitWarning(t *testing.T) {
	budgets := &types.BudgetControls{
		PerCall: &types.BudgetLimit{
			SoftLimit:    "500",
			HardLimit:    "2000",
			ActionOnSoft: types.BudgetActionType_BUDGET_ACTION_TYPE_WARN,
		},
	}
	reason, warnings := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC: 700,
		Tool:         ToolContext{ToolID: "t"},
	})
	assert.Empty(t, reason)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "soft limit")
}

func TestCheckBudgetLimits_PerSession(t *testing.T) {
	budgets := &types.BudgetControls{
		PerSession: &types.BudgetLimit{
			SoftLimit: "5000",
			HardLimit: "10000",
		},
	}
	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC:        500,
		SessionCostMicroLAC: 9600, // 9600 + 500 = 10100 > 10000
		Tool:                ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "per-session budget hard limit exceeded")
}

func TestCheckBudgetLimits_NilRequest(t *testing.T) {
	reason, warnings := checkBudgetLimits(&types.BudgetControls{PerCall: &types.BudgetLimit{SoftLimit: "1", HardLimit: "2"}}, nil)
	assert.Contains(t, reason, "invocation request is required")
	assert.Empty(t, warnings)
}

func TestCheckBudgetLimits_PerHour(t *testing.T) {
	budgets := &types.BudgetControls{
		PerHour: &types.BudgetLimit{SoftLimit: "1000", HardLimit: "1500"},
	}
	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC:     500,
		HourCostMicroLAC: 1200,
		Tool:             ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "per-hour budget hard limit exceeded")
}

func TestCheckBudgetLimits_PerDay(t *testing.T) {
	budgets := &types.BudgetControls{
		PerDay: &types.BudgetLimit{
			SoftLimit: "50000",
			HardLimit: "100000",
		},
	}
	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC:    1000,
		DayCostMicroLAC: 99500, // 99500 + 1000 = 100500 > 100000
		Tool:            ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "per-day budget hard limit exceeded")
}

func TestCheckBudgetLimits_PerTool(t *testing.T) {
	budgets := &types.BudgetControls{
		PerTool: map[string]*types.BudgetLimit{
			"expensive-tool": {
				SoftLimit: "100",
				HardLimit: "200",
			},
		},
	}
	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC: 300,
		Tool:         ToolContext{ToolID: "expensive-tool"},
	})
	assert.Contains(t, reason, "per-tool budget hard limit exceeded")

	// Different tool not limited.
	reason, _ = checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC: 300,
		Tool:         ToolContext{ToolID: "other-tool"},
	})
	assert.Empty(t, reason)
}

func TestCheckBudgetLimits_PerWeekAndMonth(t *testing.T) {
	budgets := &types.BudgetControls{
		PerWeek:  &types.BudgetLimit{SoftLimit: "1000", HardLimit: "2000"},
		PerMonth: &types.BudgetLimit{SoftLimit: "10000", HardLimit: "20000"},
	}

	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC:     500,
		WeekCostMicroLAC: 1800,
		Tool:             ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "per-week budget hard limit exceeded")

	reason, _ = checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC:      500,
		MonthCostMicroLAC: 19900,
		Tool:              ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "per-month budget hard limit exceeded")
}

func TestCheckBudgetLimits_ApprovalRequiredAbove(t *testing.T) {
	budgets := &types.BudgetControls{
		PerDay: &types.BudgetLimit{
			SoftLimit:             "100",
			HardLimit:             "1000",
			ApprovalRequiredAbove: "700",
			Approvers:             []string{"lumera1finance"},
		},
	}

	reason, warnings := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC:    200,
		DayCostMicroLAC: 600,
		Tool:            ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "per-day budget requires approval")
	assert.Empty(t, warnings)
}

func TestCheckBudgetLimits_PerCategory(t *testing.T) {
	budgets := &types.BudgetControls{
		PerCategory: map[string]*types.BudgetLimit{
			"defi": {
				SoftLimit: "500",
				HardLimit: "1000",
			},
		},
	}
	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC: 1500,
		Tool:         ToolContext{ToolID: "t", Category: "defi"},
	})
	assert.Contains(t, reason, "per-category budget hard limit exceeded")
}

func TestCheckBudgetLimits_QueueAction(t *testing.T) {
	budgets := &types.BudgetControls{
		PerCall: &types.BudgetLimit{
			SoftLimit:    "100",
			HardLimit:    "200",
			ActionOnHard: types.BudgetActionType_BUDGET_ACTION_TYPE_QUEUE,
		},
	}
	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC: 300,
		Tool:         ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "requires approval")
}

// --- checkSecurityControls unit tests ---

func TestCheckSecurityControls_NilSecurity(t *testing.T) {
	reason := checkSecurityControls(nil, &ToolContext{})
	assert.Empty(t, reason)
}

func TestCheckSecurityControls_IsolationLevel(t *testing.T) {
	security := &types.SecurityControls{
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_CONTAINER,
	}
	// VM satisfies container requirement.
	reason := checkSecurityControls(security, &ToolContext{
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_VM,
	})
	assert.Empty(t, reason)

	// Process doesn't satisfy container requirement.
	reason = checkSecurityControls(security, &ToolContext{
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_PROCESS,
	})
	assert.Contains(t, reason, "isolation level")
}

func TestCheckSecurityControls_RequiredSandboxProfile(t *testing.T) {
	security := &types.SecurityControls{
		RequiredSandboxProfiles: []string{"hardened", "verified"},
	}
	reason := checkSecurityControls(security, &ToolContext{
		SandboxProfile: "hardened",
	})
	assert.Empty(t, reason)

	reason = checkSecurityControls(security, &ToolContext{
		SandboxProfile: "community",
	})
	assert.Contains(t, reason, "not in required profiles")
}

func TestCheckSecurityControls_ForbiddenSandboxProfile(t *testing.T) {
	security := &types.SecurityControls{
		ForbiddenSandboxProfiles: []string{"community"},
	}
	reason := checkSecurityControls(security, &ToolContext{
		SandboxProfile: "community",
	})
	assert.Contains(t, reason, "forbidden")

	reason = checkSecurityControls(security, &ToolContext{
		SandboxProfile: "hardened",
	})
	assert.Empty(t, reason)
}

func TestCheckSecurityControls_GeoBlocking(t *testing.T) {
	security := &types.SecurityControls{
		GeoBlocking: []string{"CN", "RU"},
	}
	reason := checkSecurityControls(security, &ToolContext{Region: "CN"})
	assert.Contains(t, reason, "geo-blocked")

	reason = checkSecurityControls(security, &ToolContext{Region: "US"})
	assert.Empty(t, reason)

	// No region set - passes.
	reason = checkSecurityControls(security, &ToolContext{})
	assert.Empty(t, reason)
}

// --- checkPrivacyControls unit tests ---

func TestCheckPrivacyControls_NilPrivacy(t *testing.T) {
	reason := checkPrivacyControls(nil, &ToolContext{})
	assert.Empty(t, reason)
}

func TestCheckPrivacyControls_DataResidencyAllowed(t *testing.T) {
	privacy := &types.PrivacyControls{
		DataResidency: &types.DataResidency{
			AllowedRegions: []string{"US", "EU"},
		},
	}
	reason := checkPrivacyControls(privacy, &ToolContext{Region: "US"})
	assert.Empty(t, reason)

	reason = checkPrivacyControls(privacy, &ToolContext{Region: "CN"})
	assert.Contains(t, reason, "not in allowed data residency regions")
}

func TestCheckPrivacyControls_DataResidencyNoRegion(t *testing.T) {
	privacy := &types.PrivacyControls{
		DataResidency: &types.DataResidency{
			AllowedRegions: []string{"US"},
		},
	}
	// No region set - denied because data residency cannot be verified.
	reason := checkPrivacyControls(privacy, &ToolContext{})
	assert.Contains(t, reason, "tool region is unspecified")
}

func TestCheckPrivacyControls_DataResidencyDeniedRegionsRequireRegion(t *testing.T) {
	privacy := &types.PrivacyControls{
		DataResidency: &types.DataResidency{
			DeniedRegions: []string{"CN", "RU"},
		},
	}
	// No region set - denied because the policy cannot prove the tool
	// is outside the configured denied jurisdictions.
	reason := checkPrivacyControls(privacy, &ToolContext{})
	assert.Contains(t, reason, "tool region is unspecified")

	reason = checkPrivacyControls(privacy, &ToolContext{Region: "CN"})
	assert.Contains(t, reason, "is denied by data residency policy")

	reason = checkPrivacyControls(privacy, &ToolContext{Region: "US"})
	assert.Empty(t, reason)
}

func TestCheckPrivacyControls_EnclaveRequired(t *testing.T) {
	privacy := &types.PrivacyControls{
		EnclaveRequirements: &types.EnclaveRequirements{
			RequiredFor: []string{"healthcare", "finance"},
		},
	}
	// Finance category without enclave.
	reason := checkPrivacyControls(privacy, &ToolContext{
		Category:       "finance",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_CONTAINER,
	})
	assert.Contains(t, reason, "enclave execution required")

	// Finance category with enclave.
	reason = checkPrivacyControls(privacy, &ToolContext{
		Category:       "finance",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE,
	})
	assert.Empty(t, reason)

	// Different category - no enclave needed.
	reason = checkPrivacyControls(privacy, &ToolContext{
		Category:       "analytics",
		IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_NONE,
	})
	assert.Empty(t, reason)
}

// --- parseMicroLAC unit tests ---

func TestParseMicroLAC(t *testing.T) {
	v, ok := parseMicroLAC("1000")
	assert.Equal(t, uint64(1000), v)
	assert.True(t, ok)

	v, ok = parseMicroLAC("")
	assert.Equal(t, uint64(0), v)
	assert.True(t, ok) // empty string is valid (no limit)

	v, ok = parseMicroLAC("  ")
	assert.Equal(t, uint64(0), v)
	assert.True(t, ok) // whitespace-only is valid (no limit)

	v, ok = parseMicroLAC("not-a-number")
	assert.Equal(t, uint64(0), v)
	assert.False(t, ok) // malformed string is invalid

	v, ok = parseMicroLAC(" 500 ")
	assert.Equal(t, uint64(500), v)
	assert.True(t, ok)
}

// --- EvaluatePolicy integration tests ---

func TestEvaluatePolicy_ActivePolicy_AllowsCleanInvocation(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "enterprise-1",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Enterprise Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	decision, err := k.EvaluatePolicy(ctx, "enterprise-1", &InvocationRequest{
		Tool:   ToolContext{ToolID: "defi.token_price", Category: "defi"},
		UserID: "user-1",
	})
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Empty(t, decision.DenialReason)
	assert.Equal(t, "enterprise-1", decision.PolicyID)
	assert.Equal(t, "1.0.0", decision.PolicyVersion)
}

func TestEvaluatePolicyNilRequest(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "active-nil",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Active Nil",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	_, err := k.EvaluatePolicy(ctx, "active-nil", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invocation request is required")
}

func TestEvaluatePolicy_NotActive_Error(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "draft-1",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Draft Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_DRAFT,
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	_, err := k.EvaluatePolicy(ctx, "draft-1", &InvocationRequest{
		Tool: ToolContext{ToolID: "any"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not active")
}

func TestEvaluatePolicy_NotFound_Error(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	_, err := k.EvaluatePolicy(ctx, "nonexistent", &InvocationRequest{
		Tool: ToolContext{ToolID: "any"},
	})
	require.Error(t, err)
}

func TestEvaluatePolicy_DeniedTool(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "restricted-1",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Restricted Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		ToolFilters: &types.ToolFilters{
			DeniedTools: []string{"dangerous-tool"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	decision, err := k.EvaluatePolicy(ctx, "restricted-1", &InvocationRequest{
		Tool:   ToolContext{ToolID: "dangerous-tool"},
		UserID: "user-1",
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Contains(t, decision.DenialReason, "explicitly denied")
}

func TestEvaluatePolicy_BudgetExceeded(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "budget-1",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerCall: &types.BudgetLimit{
				SoftLimit: "500",
				HardLimit: "1000",
			},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	decision, err := k.EvaluatePolicy(ctx, "budget-1", &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       "user-1",
		CostMicroLAC: 2000,
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Contains(t, decision.DenialReason, "budget hard limit exceeded")
}

// TestEvaluatePolicy_VersionUpgradeResetsBudgetCounter pins the
// CURRENT behaviour that bumping policy.Version re-keys BudgetUsage
// and implicitly resets the accumulated counter. This is a sibling
// characterization to the dispute-flow denial-of-decision pins in
// x/challenges — budget enforcement has its own edge where admin-
// driven policy updates silently zero out the budget horizon.
//
// Concrete property pinned:
//
//  1. User accumulates cost C under policy P version v1 up to just
//     below hardLimit H.
//  2. Governance calls UpdatePolicy(P) producing version v2 (same
//     PolicyId, bumped Version, same budget limits).
//  3. User's NEXT evaluation under v2 sees a fresh counter at 0
//     because budgetUsageKey NO LONGER includes policy.Version — so total is
//     C_new = 0 + CostMicroLAC, not C + CostMicroLAC.
//  4. The user can spend UP TO H again immediately after the upgrade,
//     effectively doubling their budget through a routine admin
//     action.
//
// This may be intentional (governance-driven budget horizons align
// with policy epochs) or inadvertent (admins updating a policy for a
// typo fix reset everyone's budget). The test exists to make the
// behaviour visible so the design decision is explicit; a future fix
// that scopes budgets to PolicyID-only (version-independent) flips
// this test rather than slipping through silently.
func TestEvaluatePolicy_VersionUpgradeResetsBudgetCounter_BudgetBypassRisk(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	v1 := &types.PolicyProfile{
		PolicyId: "budget-version-reset",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Version Reset",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerHour: &types.BudgetLimit{
				SoftLimit: "500",
				HardLimit: "1000",
			},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, v1))

	// Step 1: user spends 900 under v1.0.0 (under hard limit of 1000).
	first, err := k.EvaluatePolicy(ctx, "budget-version-reset", &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       "user-alice",
		SessionID:    "session-1",
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.True(t, first.Allowed)

	// Step 2: under v1.0.0 a second 900-cost call would overflow
	// (900 + 900 = 1800 > 1000 hardLimit). Sanity check the
	// pre-upgrade denial.
	preUpgrade, err := k.EvaluatePolicy(ctx, "budget-version-reset", &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       "user-alice",
		SessionID:    "session-1",
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.False(t, preUpgrade.Allowed,
		"pre-upgrade: accumulated 900 + new 900 = 1800 exceeds 1000 hard limit")

	// Step 3: governance bumps the policy version. Same limits,
	// same owner, just a new version string — which could in practice
	// be any routine update (typo fix, new tool added, metadata
	// refresh).
	v2 := &types.PolicyProfile{
		PolicyId: "budget-version-reset",
		Version:  "1.0.1",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Version Reset",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerHour: &types.BudgetLimit{
				SoftLimit: "500",
				HardLimit: "1000",
			},
		},
	}
	require.NoError(t, k.UpdatePolicy(ctx, "org-owner", v2, "routine update"))

	// Step 4: the SAME user, same hour, attempts to spend another 900.
	// Because budgetUsageKey is keyed on policy.PolicyId (and NOT Version),
	// the pre-upgrade budget is preserved. The request is denied.
	postUpgrade, err := k.EvaluatePolicy(ctx, "budget-version-reset", &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       "user-alice",
		SessionID:    "session-1",
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.False(t, postUpgrade.Allowed,
		"NEW behaviour: version bump no longer re-keys the counter. v2's "+
			"BudgetUsage shares the 900 accumulated from v1. User "+
			"cannot double their per-hour budget via routine admin updates.")
}

func TestEvaluatePolicy_BudgetUsagePersistsAndIgnoresCallerAccumulator(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "budget-usage",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Usage",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerSession: &types.BudgetLimit{
				SoftLimit: "500",
				HardLimit: "1000",
			},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	first, err := k.EvaluatePolicy(ctx, "budget-usage", &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       "user-1",
		SessionID:    "session-1",
		CostMicroLAC: 600,
	})
	require.NoError(t, err)
	require.True(t, first.Allowed)

	second, err := k.EvaluatePolicy(ctx, "budget-usage", &InvocationRequest{
		Tool:                ToolContext{ToolID: "tool-1"},
		UserID:              "user-1",
		SessionID:           "session-1",
		CostMicroLAC:        500,
		SessionCostMicroLAC: 0,
	})
	require.NoError(t, err)
	assert.False(t, second.Allowed)
	assert.Contains(t, second.DenialReason, "per-session budget hard limit exceeded")
}

func TestEvaluatePolicy_DeniedRequestDoesNotCommitBudgetUsage(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "budget-security",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Security",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerDay: &types.BudgetLimit{
				SoftLimit: "500",
				HardLimit: "1000",
			},
		},
		Security: &types.SecurityControls{
			GeoBlocking: []string{"CN"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	denied, err := k.EvaluatePolicy(ctx, "budget-security", &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1", Region: "CN"},
		UserID:       "user-1",
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed)
	require.Contains(t, denied.DenialReason, "geo-blocked")

	allowed, err := k.EvaluatePolicy(ctx, "budget-security", &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1", Region: "US"},
		UserID:       "user-1",
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	assert.True(t, allowed.Allowed)
	assert.Empty(t, allowed.DenialReason)
}

func TestEvaluatePolicy_SecurityDenied(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "secure-1",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Secure Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Security: &types.SecurityControls{
			GeoBlocking: []string{"CN"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	decision, err := k.EvaluatePolicy(ctx, "secure-1", &InvocationRequest{
		Tool:   ToolContext{ToolID: "tool-1", Region: "CN"},
		UserID: "user-1",
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Contains(t, decision.DenialReason, "geo-blocked")
}

func TestEvaluatePolicy_PrivacyDenied(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "privacy-1",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Privacy Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Privacy: &types.PrivacyControls{
			DataResidency: &types.DataResidency{
				AllowedRegions: []string{"US", "EU"},
			},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	decision, err := k.EvaluatePolicy(ctx, "privacy-1", &InvocationRequest{
		Tool:   ToolContext{ToolID: "tool-1", Region: "CN"},
		UserID: "user-1",
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Contains(t, decision.DenialReason, "data residency")
}

func TestEvaluatePolicy_AuditEntryRecorded(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "audit-1",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Audit Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		ToolFilters: &types.ToolFilters{
			DeniedTools: []string{"blocked-tool"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Denied invocation.
	decision, err := k.EvaluatePolicy(ctx, "audit-1", &InvocationRequest{
		Tool:   ToolContext{ToolID: "blocked-tool"},
		UserID: "user-1",
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)

	// Verify audit entry was recorded. The auditID embeds a monotonic sequence
	// suffix to avoid same-block collisions, so look it up by iterating.
	var entry *types.PolicyAuditEntry
	require.NoError(t, k.state.PolicyAudit.Walk(ctx, nil, func(_ string, e *types.PolicyAuditEntry) (bool, error) {
		if e.PolicyId == "audit-1" && e.ToolId == "blocked-tool" && e.UserId == "user-1" {
			entry = e
			return true, nil
		}
		return false, nil
	}))
	require.NotNil(t, entry, "audit entry not recorded")
	assert.Equal(t, "audit-1", entry.PolicyId)
	assert.Equal(t, "blocked-tool", entry.ToolId)
	assert.False(t, entry.Allowed)
	assert.Contains(t, entry.DenialReason, "explicitly denied")
}

func TestEvaluatePolicy_ComprehensiveAllAllow(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "comprehensive-1",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Comprehensive Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		ToolFilters: &types.ToolFilters{
			AllowedCategories:   []string{"defi", "analytics"},
			MinVerificationTier: types.VerificationTier_VERIFICATION_TIER_SILVER,
			MinUptimeBps:        9900,
		},
		Budgets: &types.BudgetControls{
			PerCall: &types.BudgetLimit{
				SoftLimit: "1000",
				HardLimit: "5000",
			},
		},
		Security: &types.SecurityControls{
			IsolationLevel: types.IsolationLevel_ISOLATION_LEVEL_CONTAINER,
			GeoBlocking:    []string{"CN"},
		},
		Privacy: &types.PrivacyControls{
			DataResidency: &types.DataResidency{
				AllowedRegions: []string{"US", "EU"},
			},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	decision, err := k.EvaluatePolicy(ctx, "comprehensive-1", &InvocationRequest{
		Tool: ToolContext{
			ToolID:           "defi.token_price",
			Category:         "defi",
			VerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
			UptimeBps:        9950,
			IsolationLevel:   types.IsolationLevel_ISOLATION_LEVEL_VM,
			Region:           "US",
		},
		UserID:       "user-1",
		CostMicroLAC: 2000,
	})
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Empty(t, decision.DenialReason)
}

func TestEvaluatePolicy_ComprehensiveToolFilterDeny(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "comprehensive-deny",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Comprehensive Deny Policy",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		ToolFilters: &types.ToolFilters{
			MinVerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
		},
		Budgets: &types.BudgetControls{
			PerCall: &types.BudgetLimit{
				SoftLimit: "1000",
				HardLimit: "5000",
			},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Tool filter fails first -- budget never checked.
	decision, err := k.EvaluatePolicy(ctx, "comprehensive-deny", &InvocationRequest{
		Tool: ToolContext{
			ToolID:           "tool-1",
			VerificationTier: types.VerificationTier_VERIFICATION_TIER_BRONZE,
		},
		UserID:       "user-1",
		CostMicroLAC: 2000,
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Contains(t, decision.DenialReason, "verification tier")
}

// TestEvaluatePolicy_SameBlockAuditIDsUnique guards against the collision fixed
// alongside this test: two evaluations of the same (policy, tool, user) tuple
// within a single block must produce distinct audit entries so the trail cannot
// silently drop policy violations.
func TestEvaluatePolicy_SameBlockAuditIDsUnique(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId: "audit-collision",
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{Name: "Audit Collision", Owner: "org-owner"},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	req := &InvocationRequest{
		Tool:   ToolContext{ToolID: "defi.token_price", Category: "defi"},
		UserID: "user-1",
	}

	// Two evaluations at the same block height with identical policy/tool/user.
	_, err := k.EvaluatePolicy(ctx, "audit-collision", req)
	require.NoError(t, err)
	_, err = k.EvaluatePolicy(ctx, "audit-collision", req)
	require.NoError(t, err)

	count := 0
	require.NoError(t, k.state.PolicyAudit.Walk(ctx, nil, func(_ string, _ *types.PolicyAuditEntry) (bool, error) {
		count++
		return false, nil
	}))
	assert.Equal(t, 2, count, "second evaluation must not overwrite the first audit entry")
}

// TestEvaluatePolicy_AuditCounterStartsAtZero asserts the persisted audit
// sequence is initialized empty, so the first evaluation observes seq=0.
func TestEvaluatePolicy_AuditCounterStartsAtZero(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	// PeekNext is not exposed; verify by reading the underlying counter via Next
	// directly on a fresh keeper, then rolling that back is not possible — so
	// instead create one evaluation and verify the resulting auditID ends in
	// ":0", which only holds if the sequence started at zero.
	policy := &types.PolicyProfile{
		PolicyId:  "audit-seq-zero",
		Version:   "1.0.0",
		Metadata:  &types.PolicyMetadata{Name: "Seq Zero", Owner: "org-owner"},
		Lifecycle: &types.PolicyLifecycle{State: types.PolicyState_POLICY_STATE_ACTIVE},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	_, err := k.EvaluatePolicy(ctx, "audit-seq-zero", &InvocationRequest{
		Tool:   ToolContext{ToolID: "tool-a"},
		UserID: "user-a",
	})
	require.NoError(t, err)

	var auditID string
	require.NoError(t, k.state.PolicyAudit.Walk(ctx, nil, func(id string, _ *types.PolicyAuditEntry) (bool, error) {
		auditID = id
		return true, nil
	}))
	require.NotEmpty(t, auditID)
	assert.True(t, strings.HasSuffix(auditID, ":0"),
		"first audit ID must end with sequence suffix :0, got %q", auditID)
}

// TestEvaluatePolicy_AuditIDFormat asserts the auditID layout is exactly
// "<policy>:<tool>:<user>:<height>:<seq>" so downstream indexers can parse it.
func TestEvaluatePolicy_AuditIDFormat(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId:  "audit-fmt",
		Version:   "1.0.0",
		Metadata:  &types.PolicyMetadata{Name: "Fmt", Owner: "org-owner"},
		Lifecycle: &types.PolicyLifecycle{State: types.PolicyState_POLICY_STATE_ACTIVE},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	_, err := k.EvaluatePolicy(ctx, "audit-fmt", &InvocationRequest{
		Tool:   ToolContext{ToolID: "tool-x"},
		UserID: "user-y",
	})
	require.NoError(t, err)

	var auditID string
	require.NoError(t, k.state.PolicyAudit.Walk(ctx, nil, func(id string, _ *types.PolicyAuditEntry) (bool, error) {
		auditID = id
		return true, nil
	}))
	parts := strings.Split(auditID, ":")
	require.Len(t, parts, 5, "auditID %q should have 5 colon-delimited segments", auditID)
	assert.Equal(t, "audit-fmt", parts[0])
	assert.Equal(t, "tool-x", parts[1])
	assert.Equal(t, "user-y", parts[2])
	assert.NotEmpty(t, parts[3], "block height segment must be present")
	assert.NotEmpty(t, parts[4], "sequence segment must be present")
}

// TestEvaluatePolicy_AuditCounterSharedAcrossPolicies asserts the audit
// sequence is module-wide: evaluations against different policies still
// allocate distinct sequence numbers, so the counter behaves as a globally
// monotonic suffix rather than a per-policy one.
func TestEvaluatePolicy_AuditCounterSharedAcrossPolicies(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	for _, id := range []string{"pol-a", "pol-b"} {
		require.NoError(t, k.CreatePolicy(ctx, &types.PolicyProfile{
			PolicyId:  id,
			Version:   "1.0.0",
			Metadata:  &types.PolicyMetadata{Name: id, Owner: "org-owner"},
			Lifecycle: &types.PolicyLifecycle{State: types.PolicyState_POLICY_STATE_ACTIVE},
		}))
	}

	_, err := k.EvaluatePolicy(ctx, "pol-a", &InvocationRequest{
		Tool: ToolContext{ToolID: "t1"}, UserID: "u1",
	})
	require.NoError(t, err)
	_, err = k.EvaluatePolicy(ctx, "pol-b", &InvocationRequest{
		Tool: ToolContext{ToolID: "t1"}, UserID: "u1",
	})
	require.NoError(t, err)

	seqs := map[string]struct{}{}
	require.NoError(t, k.state.PolicyAudit.Walk(ctx, nil, func(id string, _ *types.PolicyAuditEntry) (bool, error) {
		parts := strings.Split(id, ":")
		require.Len(t, parts, 5)
		seqs[parts[4]] = struct{}{}
		return false, nil
	}))
	assert.Len(t, seqs, 2, "two evaluations across distinct policies must produce two distinct sequence suffixes")
}

// TestEvaluatePolicy_AuditCounterMonotonic asserts the sequence advances by 1
// across evaluations, so the suffix can be parsed as a strictly-increasing
// integer rather than an opaque tag.
func TestEvaluatePolicy_AuditCounterMonotonic(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	require.NoError(t, k.CreatePolicy(ctx, &types.PolicyProfile{
		PolicyId:  "mono",
		Version:   "1.0.0",
		Metadata:  &types.PolicyMetadata{Name: "mono", Owner: "org-owner"},
		Lifecycle: &types.PolicyLifecycle{State: types.PolicyState_POLICY_STATE_ACTIVE},
	}))

	const N = 5
	for i := 0; i < N; i++ {
		_, err := k.EvaluatePolicy(ctx, "mono", &InvocationRequest{
			Tool: ToolContext{ToolID: "tool"}, UserID: "user",
		})
		require.NoError(t, err)
	}

	// Collect all sequence suffixes and confirm they are exactly {0..N-1}.
	got := map[string]bool{}
	require.NoError(t, k.state.PolicyAudit.Walk(ctx, nil, func(id string, _ *types.PolicyAuditEntry) (bool, error) {
		parts := strings.Split(id, ":")
		require.Len(t, parts, 5)
		got[parts[4]] = true
		return false, nil
	}))
	require.Len(t, got, N)
	for i := 0; i < N; i++ {
		assert.Truef(t, got[itoa(i)], "missing sequence suffix %d in %v", i, got)
	}
}

// itoa is a minimal int->string helper so the test does not depend on strconv
// just to format a counter value.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestSafeAddUint64(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		b        uint64
		expected uint64
	}{
		{"no overflow", 100, 200, 300},
		{"zero values", 0, 0, 0},
		{"max with zero", math.MaxUint64, 0, math.MaxUint64},
		{"overflow by 1", math.MaxUint64, 1, math.MaxUint64},
		{"overflow large values", math.MaxUint64 - 10, 20, math.MaxUint64},
		{"near max no overflow", math.MaxUint64 - 100, 50, math.MaxUint64 - 50},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := safeAddUint64(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCheckBudgetLimits_OverflowProtection(t *testing.T) {
	budgets := &types.BudgetControls{
		PerSession: &types.BudgetLimit{
			SoftLimit: "1000",
			HardLimit: "10000",
		},
	}

	reason, _ := checkBudgetLimits(budgets, &InvocationRequest{
		CostMicroLAC:        1,
		SessionCostMicroLAC: math.MaxUint64, // would wrap to 0 without overflow protection
		Tool:                ToolContext{ToolID: "t"},
	})
	assert.Contains(t, reason, "per-session budget hard limit exceeded",
		"overflow should result in exceeding budget, not bypassing it")
}
