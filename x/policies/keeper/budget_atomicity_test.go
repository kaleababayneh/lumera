//go:build cosmos
// +build cosmos

package keeper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// Regression guards for the keeper-side atomic-commit contract on
// lumera_ai-9oh42 (Budget accumulator enforcement not atomic).
//
// The original vulnerability: EvaluatePolicy trusted caller-supplied
// accumulators (InvocationRequest.{Session,Hour,Day,Week,Month}CostMicroLAC)
// as the source of truth for budget checks, allowing understatement
// attacks and silent double-spend under concurrent evaluation.
//
// The fix: accumulators are now persisted in keeper state (x/policies
// types.BudgetUsagePrefixByte = 0x0A), checked on every EvaluatePolicy,
// and committed ONLY when the entire decision is Allowed=true. Denial
// at ANY gate (tool filter, ANY scope's budget, security, privacy)
// short-circuits the commit — nothing is written.
//
// This file pins the internal atomicity contract across budget scopes,
// which is a stricter invariant than the file-level
// "TestEvaluatePolicy_DeniedRequestDoesNotCommitBudgetUsage" test in
// enforce_test.go (which only pins the security-denial-blocks-commit
// case). Here we exercise the case where an EARLIER scope in the
// check loop (per-session / per-hour) passes, then a LATER scope
// (per-day / per-month) denies, and verify that the earlier scope
// also does NOT commit.
//
// The code path is checkBudgetLimits() → on first denial it returns
// (reason, warnings, nil, nil) with explicitly-nil updates; the
// caller gates commitBudgetUsage on decision.Allowed. Both guards
// must hold for atomicity. Regressions in either — e.g., emitting a
// partial updates slice on early denial, or committing when
// decision.Allowed is false — would silently let a denied request
// inflate one scope's counter without inflating the scope that
// actually caused the denial. Over many requests the two counters
// would drift and the "tighter" scope would stop enforcing.

// TestBudgetAtomicity_DayDenialPreservesHourCounter pins the
// cross-scope all-or-nothing commit. Policy has two active scopes
// (PerHour hard=2000, PerDay hard=500). First call costs 600 micro-LAC
// — passes PerHour (600 <= 2000) but fails PerDay (600 > 500). The
// denial must leave BOTH counters at 0: nothing was authorized,
// nothing should be debited.
//
// A follow-up call costing 400 micro-LAC must then succeed (400 <=
// 500) — proving that the hour counter was NOT leaked to 600 during
// the rejected call. Without the atomic-commit guard, the hour
// counter would sit at 600, the day counter would sit at 0, and this
// second call would pass per-day (0+400 <= 500) AND per-hour
// (600+400 <= 2000) but NEVER via the contract the test asserts.
func TestBudgetAtomicity_DayDenialPreservesHourCounter(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	const policyID = "budget-atomicity"
	const userID = "user-atomic"
	policy := &types.PolicyProfile{
		PolicyId: policyID,
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Atomicity",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			// Hour is GENEROUS so the first scope passes — we need
			// the denial to fire at a LATER scope to exercise the
			// "earlier passed, later denied, nothing commits" path.
			PerHour: &types.BudgetLimit{
				SoftLimit: "1000",
				HardLimit: "2000",
			},
			// Day is TIGHT so a single 600-micro-LAC call blows it.
			PerDay: &types.BudgetLimit{
				SoftLimit: "400",
				HardLimit: "500",
			},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// First call: 600 cost. Per-hour should PASS (600 <= 2000), then
	// per-day MUST DENY (600 > 500). Neither scope should commit.
	denied, err := k.EvaluatePolicy(ctx, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 600,
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed, "first call must be denied by per-day scope")
	require.Contains(t, denied.DenialReason, "per-day",
		"denial must cite per-day — the scope that actually tripped the limit")

	// Inspect the persisted counters directly. Both hour AND day keys
	// must be absent (treated as 0 by getBudgetUsage). A regression
	// that emits partial updates on early denial would leave the hour
	// key set to 600 here.
	periods := budgetUsagePeriods(ctx)
	hourKey := budgetUsageKey(policyID, userID, "per-hour", periods.hour)
	dayKey := budgetUsageKey(policyID, userID, "per-day", periods.day)

	hourUsage, err := k.getBudgetUsage(ctx, hourKey)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), hourUsage,
		"per-hour counter leaked %d even though the call was denied — "+
			"atomicity violated (earlier-scope partial commit bug)", hourUsage)

	dayUsage, err := k.getBudgetUsage(ctx, dayKey)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), dayUsage,
		"per-day counter leaked %d even though the call was denied", dayUsage)

	// Second call: 400 cost. Must pass because both counters were
	// correctly reset to 0 by the atomic rollback above. If the hour
	// counter had leaked to 600 from the prior denial, this call would
	// STILL pass per-hour (1000 <= 2000) and per-day (0+400 <= 500),
	// so the passing verdict alone is not enough. The real proof is
	// inspecting the per-hour counter afterwards: it must be exactly
	// 400, not 1000, to prove the first call truly committed nothing.
	allowed, err := k.EvaluatePolicy(ctx, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 400,
	})
	require.NoError(t, err)
	require.True(t, allowed.Allowed, "second call must be allowed: %s", allowed.DenialReason)

	hourAfter, err := k.getBudgetUsage(ctx, hourKey)
	require.NoError(t, err)
	assert.Equal(t, uint64(400), hourAfter,
		"per-hour counter is %d after a single 400-cost allowed call; "+
			"expected 400. Value > 400 means the first (denied) call "+
			"silently committed its 600 cost; value < 400 means the "+
			"allowed call failed to commit.", hourAfter)

	dayAfter, err := k.getBudgetUsage(ctx, dayKey)
	require.NoError(t, err)
	assert.Equal(t, uint64(400), dayAfter,
		"per-day counter is %d after a single 400-cost allowed call; "+
			"expected 400", dayAfter)
}

// TestBudgetAtomicity_HourDenialPreservesSessionCounter pins the
// reverse ordering: per-session passes, per-hour denies. Same
// contract — the passed scope must not commit when a later scope
// denies. This exists in addition to the prior test because the
// internal check loop ordering in enforce.go could change and
// different scopes would become the "earlier" one; pinning both
// directions of the dependency prevents a refactor from accidentally
// scoping the atomicity guarantee to just the day/hour pair.
func TestBudgetAtomicity_HourDenialPreservesSessionCounter(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	const policyID = "budget-atomicity-session-hour"
	const userID = "user-atomic-sh"
	const sessionID = "session-atomic-sh"
	policy := &types.PolicyProfile{
		PolicyId: policyID,
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Atomicity Session+Hour",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerSession: &types.BudgetLimit{
				SoftLimit: "3000",
				HardLimit: "5000",
			},
			PerHour: &types.BudgetLimit{
				SoftLimit: "200",
				HardLimit: "300",
			},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Cost 400 blows per-hour (400 > 300) but easily fits per-session
	// (400 <= 5000).
	decision, err := k.EvaluatePolicy(ctx, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		SessionID:    sessionID,
		CostMicroLAC: 400,
	})
	require.NoError(t, err)
	require.False(t, decision.Allowed, "call must be denied by per-hour scope")
	require.Contains(t, decision.DenialReason, "per-hour")

	periods := budgetUsagePeriods(ctx)
	sessionKey := budgetUsageKey(policyID, userID, "per-session", budgetSessionPeriod(sessionID))
	hourKey := budgetUsageKey(policyID, userID, "per-hour", periods.hour)

	sessionUsage, err := k.getBudgetUsage(ctx, sessionKey)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), sessionUsage,
		"per-session counter leaked %d even though the call was denied by per-hour", sessionUsage)

	hourUsage, err := k.getBudgetUsage(ctx, hourKey)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), hourUsage,
		"per-hour counter itself must not commit when it is the denying scope")
}

// TestBudgetAtomicity_SecurityDenialAfterAllBudgetPassed pins the
// outermost atomicity guarantee: even when every budget scope passes,
// a LATER gate (security controls evaluated AFTER budgets) can still
// deny, and that denial must not commit any scope's counter. This is
// a subtly different path from TestEvaluatePolicy_DeniedRequestDoesNotCommitBudgetUsage
// in enforce_test.go, which never exercises a multi-scope budget pass
// because its policy has only PerDay; here we use three scopes to
// confirm that budgetUsageUpdate slices accumulated during the budget
// phase are STILL discarded when a downstream gate denies.
func TestBudgetAtomicity_SecurityDenialAfterAllBudgetPassed(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	const policyID = "budget-atomicity-sec-deny"
	const userID = "user-sec-deny"
	const sessionID = "session-sec-deny"
	policy := &types.PolicyProfile{
		PolicyId: policyID,
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Atomicity with Security Denial",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerSession: &types.BudgetLimit{SoftLimit: "1000", HardLimit: "2000"},
			PerHour:    &types.BudgetLimit{SoftLimit: "1000", HardLimit: "2000"},
			PerDay:     &types.BudgetLimit{SoftLimit: "5000", HardLimit: "10000"},
		},
		Security: &types.SecurityControls{
			// This will trip for any Region == "CN" request, AFTER
			// all three budget scopes have been evaluated and
			// accumulated updates.
			GeoBlocking: []string{"CN"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	decision, err := k.EvaluatePolicy(ctx, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1", Region: "CN"},
		UserID:       userID,
		SessionID:    sessionID,
		CostMicroLAC: 500, // comfortably under every scope's hard limit
	})
	require.NoError(t, err)
	require.False(t, decision.Allowed, "security must deny")
	require.Contains(t, decision.DenialReason, "geo-blocked")

	periods := budgetUsagePeriods(ctx)
	sessionKey := budgetUsageKey(policyID, userID, "per-session", budgetSessionPeriod(sessionID))
	hourKey := budgetUsageKey(policyID, userID, "per-hour", periods.hour)
	dayKey := budgetUsageKey(policyID, userID, "per-day", periods.day)

	for _, tc := range []struct {
		name string
		key  string
	}{
		{"per-session", sessionKey},
		{"per-hour", hourKey},
		{"per-day", dayKey},
	} {
		usage, err := k.getBudgetUsage(ctx, tc.key)
		require.NoError(t, err, "%s", tc.name)
		assert.Equal(t, uint64(0), usage,
			"%s counter leaked %d after security-gate denial — "+
				"budget updates must NOT commit unless the FINAL decision is Allowed",
			tc.name, usage)
	}
}

// TestBudgetAtomicity_InvariantsDenialCommitsNothing pins that a
// caller supplying a provably-inconsistent accumulator (e.g.,
// hour_cost > day_cost) is rejected at validateBudgetInvariants,
// BEFORE any budget scope is even evaluated. Since checkBudgetLimits
// never runs, there are no budgetUsageUpdates to discard — but the
// test still asserts the absence of any per-scope leakage as a
// belt-and-suspenders guard against a future refactor that moves the
// invariant check AFTER budget evaluation. Such a refactor would
// silently let a provably-wrong-but-budget-passing request commit.
func TestBudgetAtomicity_InvariantsDenialCommitsNothing(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	const policyID = "budget-atomicity-inv-deny"
	const userID = "user-inv-deny"
	policy := &types.PolicyProfile{
		PolicyId: policyID,
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Budget Atomicity with Invariant Denial",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerHour: &types.BudgetLimit{SoftLimit: "1000", HardLimit: "2000"},
			PerDay:  &types.BudgetLimit{SoftLimit: "5000", HardLimit: "10000"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// hour_cost = 200 > day_cost = 100 is provably impossible: the
	// hour bucket is a subset of the day bucket. validateBudgetInvariants
	// must reject this.
	decision, err := k.EvaluatePolicy(ctx, policyID, &InvocationRequest{
		Tool:             ToolContext{ToolID: "tool-1"},
		UserID:           userID,
		CostMicroLAC:     50,
		HourCostMicroLAC: 200,
		DayCostMicroLAC:  100,
	})
	require.NoError(t, err)
	require.False(t, decision.Allowed, "invariant violation must deny")

	// Keeper counters are untouched because checkBudgetLimits is
	// gated on decision.Allowed staying true after the invariants
	// check. If a future refactor moves validateBudgetInvariants
	// BELOW checkBudgetLimits, this assertion will catch the
	// regression: the keeper counters would have been incremented
	// during the budget phase and then never rolled back.
	periods := budgetUsagePeriods(ctx)
	hourKey := budgetUsageKey(policyID, userID, "per-hour", periods.hour)
	dayKey := budgetUsageKey(policyID, userID, "per-day", periods.day)

	hourUsage, err := k.getBudgetUsage(ctx, hourKey)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), hourUsage,
		"per-hour counter leaked %d after invariant-check denial", hourUsage)

	dayUsage, err := k.getBudgetUsage(ctx, dayKey)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), dayUsage,
		"per-day counter leaked %d after invariant-check denial", dayUsage)
}
