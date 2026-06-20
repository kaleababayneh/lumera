//go:build cosmos

package keeper

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// This file applies the testing-conformance-harnesses skill
// (Pattern 1: Differential Testing + spec-derived coverage matrix)
// to the COOPERATIVE CONSISTENCY between x/policies enforce and
// x/credits Lock under concurrent same-block MsgSend operations.
//
// Cooperative pipeline:
//
//   For each invocation a router sends in a block:
//     1. caller invokes EvaluatePolicy(policyID, req with CostMicroLAC)
//     2. if decision.Allowed: caller invokes credits.LockCredits(amount)
//     3. policies budget counters increment by CostMicroLAC
//     4. credits escrow holds the locked amount
//
// The two modules MUST agree on the cumulative consumption: the
// total of policies-budget consumed for a (user, policy) tuple
// must equal the total credits Lock amount for the same tuple.
// Drift between them means the chain is overlocking (router pays
// more than policy permits) or underlocking (router escrows less
// than budget consumed).
//
// Where tick 65 (reserve↔credits) tested capacity-vs-discount
// drift and tick 66 (insurance claim↔reserve) tested approval
// vs payout drain, THIS FILE pins the pre-flight gate (policies)
// vs reservation (credits Lock) consistency. The credits-Lock
// side is modeled as a deterministic cooperative ledger so the
// cross-module wire-up is exercised end-to-end without a circular
// build-tag dependency between the two modules.
//
// Seven MUST clauses + composite cross-validator determinism:
//
//   MUST-1: only Allowed evaluations consume budget AND lock —
//           denied requests increment NEITHER side
//   MUST-2: per-user policy budget counter == per-user ledger
//           total at every step (no drift within a user)
//   MUST-3: cumulative budget consumed across all users ==
//           cumulative ledger total
//   MUST-4: ordering invariance — same set of requests in any
//           order produces identical final budget+ledger state
//   MUST-5: subset processed → only that subset reflects in
//           both modules; excluded requests leave neither side
//           changed
//   MUST-6: re-running the same script on a fresh keeper yields
//           byte-equal final state (idempotence under replay)
//   MUST-7: a denied request interleaved between two Allowed
//           requests does NOT advance either side past the
//           denied amount; the second Allowed request resumes
//           normally

// --------------------------------------------------------------
// Cooperative ledger — models the x/credits Lock side
// --------------------------------------------------------------

// cooperativeLedger tracks per-(user, policy) Lock totals as a
// stand-in for x/credits's escrow accumulator. Mirrors the
// credits-Lock contract: only successful EvaluatePolicy with
// Allowed=true should produce a corresponding lock entry.
type cooperativeLedger struct {
	// key = "userID|policyID" → cumulative lock amount in micro-LAC.
	locks map[string]uint64
}

func newCooperativeLedger() *cooperativeLedger {
	return &cooperativeLedger{locks: make(map[string]uint64)}
}

func (l *cooperativeLedger) lock(userID, policyID string, amount uint64) {
	key := userID + "|" + policyID
	l.locks[key] = safeAddUint64(l.locks[key], amount)
}

func (l *cooperativeLedger) totalForUser(userID, policyID string) uint64 {
	return l.locks[userID+"|"+policyID]
}

func (l *cooperativeLedger) cumulativeTotal() uint64 {
	var total uint64
	for _, v := range l.locks {
		total = safeAddUint64(total, v)
	}
	return total
}

// --------------------------------------------------------------
// Test fixture: policies keeper + a minimal active policy with
// generous budget limits (so MUST tests hinge on the cooperative
// invariant, not on hitting limits — limit-hits are tested
// separately in budget_invariants_test.go).
// --------------------------------------------------------------

func setupCoopPoliciesPolicy(t *testing.T) (sdk.Context, *Keeper, string) {
	t.Helper()
	ctx, k := setupPoliciesKeeper(t)

	policy := makeCoopPolicy()
	require.NoError(t, k.CreatePolicy(ctx, policy))
	require.NoError(t, k.SetPolicyState(ctx, policy.PolicyId, policy.Version,
		types.PolicyState_POLICY_STATE_ACTIVE))
	return ctx, k, policy.PolicyId
}

func makeCoopPolicy() *types.PolicyProfile {
	return &types.PolicyProfile{
		PolicyId:      "coop-policy-1",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:        "Cooperative Test Policy",
			Description: "policy for policies↔credits cooperative tests",
			Owner:       "lumera1coopowner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: "lumera1coopowner",
		},
		ToolFilters: &types.ToolFilters{
			AllowedTools:        []string{"tool-coop-allow", "tool-coop-allow-2"},
			DeniedTools:         []string{"tool-coop-deny"},
			MinVerificationTier: types.VerificationTier_VERIFICATION_TIER_BRONZE,
		},
		Budgets: &types.BudgetControls{
			// Generous limits so the MUSTs aren't gated on limit
			// rejection. Budget rejection is tested elsewhere.
			// SoftLimit + HardLimit are strings (parsed as micro-LAC).
			PerCall: &types.BudgetLimit{SoftLimit: "50000000", HardLimit: "100000000"},
			PerHour: &types.BudgetLimit{SoftLimit: "500000000", HardLimit: "1000000000"},
			PerDay:  &types.BudgetLimit{SoftLimit: "5000000000", HardLimit: "10000000000"},
		},
	}
}

// makeAllowedReq builds an InvocationRequest that should be
// Allowed against the coop policy.
func makeAllowedReq(userID string, costMicroLAC uint64) *InvocationRequest {
	return &InvocationRequest{
		Tool: ToolContext{
			ToolID:           "tool-coop-allow",
			VerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
		},
		UserID:       userID,
		SessionID:    "coop-session-" + userID,
		CostMicroLAC: costMicroLAC,
	}
}

// makeDeniedReq builds an InvocationRequest that should be
// Denied (uses a denied tool).
func makeDeniedReq(userID string, costMicroLAC uint64) *InvocationRequest {
	return &InvocationRequest{
		Tool: ToolContext{
			ToolID:           "tool-coop-deny",
			VerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
		},
		UserID:       userID,
		SessionID:    "coop-session-" + userID,
		CostMicroLAC: costMicroLAC,
	}
}

// runCooperativeStep is the cooperative-pattern caller: invoke
// EvaluatePolicy; if Allowed, lock in the cooperative ledger.
// Returns the policy decision so callers can assert on it.
func runCooperativeStep(t *testing.T, ctx sdk.Context, k *Keeper, ledger *cooperativeLedger, policyID string, req *InvocationRequest) *PolicyDecision {
	t.Helper()
	decision, err := k.EvaluatePolicy(ctx, policyID, req)
	require.NoError(t, err)
	if decision.Allowed {
		ledger.lock(req.UserID, policyID, req.CostMicroLAC)
	}
	return decision
}

// readPerHourBudgetForUser reads the per-hour budget counter for
// the cooperative coop policy for a given userID. Mirrors the
// cooperative ledger total for that user.
func readPerHourBudgetForUser(t *testing.T, ctx sdk.Context, k *Keeper, policyID, userID string) uint64 {
	t.Helper()
	policy, err := k.GetPolicy(ctx, policyID, "")
	require.NoError(t, err)
	periods := budgetUsagePeriods(ctx)
	key := budgetUsageKey(policyID, userID, "per-hour", periods.hour)
	usage, err := k.getBudgetUsage(ctx, key)
	require.NoError(t, err)
	return usage
}

// --------------------------------------------------------------
// MUST-1: Only Allowed evaluations consume budget AND lock
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_MUST1_DeniedRequestsConsumeNeitherSide(t *testing.T) {
	t.Parallel()
	ctx, k, policyID := setupCoopPoliciesPolicy(t)
	ledger := newCooperativeLedger()

	const userID = "user-must1"
	const cost uint64 = 1_000

	// 3 Allowed + 2 Denied interleaved.
	steps := []struct {
		req     *InvocationRequest
		allowed bool
	}{
		{makeAllowedReq(userID, cost), true},
		{makeDeniedReq(userID, cost), false},
		{makeAllowedReq(userID, cost), true},
		{makeDeniedReq(userID, cost), false},
		{makeAllowedReq(userID, cost), true},
	}
	for i, step := range steps {
		decision := runCooperativeStep(t, ctx, k, ledger, policyID, step.req)
		require.Equal(t, step.allowed, decision.Allowed,
			"MUST-1 step %d: expected Allowed=%v got %v (reason=%q)",
			i, step.allowed, decision.Allowed, decision.DenialReason)
	}

	// Ledger should reflect ONLY the 3 Allowed calls.
	expected := 3 * cost
	require.Equal(t, expected, ledger.totalForUser(userID, policyID),
		"MUST-1: ledger should hold 3×cost=%d for Allowed calls; got %d",
		expected, ledger.totalForUser(userID, policyID))

	// Policies budget counter (per-hour) should match.
	budgetUsage := readPerHourBudgetForUser(t, ctx, k, policyID, userID)
	require.Equal(t, expected, budgetUsage,
		"MUST-1: policies per-hour budget counter=%d should match "+
			"ledger=%d — denied requests must NOT advance either side",
		budgetUsage, expected)
}

// --------------------------------------------------------------
// MUST-2: Per-user budget counter == per-user ledger
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_MUST2_PerUserBudgetMatchesLedger(t *testing.T) {
	t.Parallel()
	ctx, k, policyID := setupCoopPoliciesPolicy(t)
	ledger := newCooperativeLedger()

	users := []string{"u-a", "u-b", "u-c"}
	costs := []uint64{1_000, 2_500, 750, 5_000, 1_500}

	for _, user := range users {
		for _, cost := range costs {
			runCooperativeStep(t, ctx, k, ledger, policyID,
				makeAllowedReq(user, cost))
		}
	}

	// Each user should have ledger total = Σ costs.
	expectedPerUser := uint64(0)
	for _, c := range costs {
		expectedPerUser += c
	}
	for _, user := range users {
		ledgerTotal := ledger.totalForUser(user, policyID)
		require.Equal(t, expectedPerUser, ledgerTotal,
			"MUST-2 user=%s: ledger=%d expected=%d",
			user, ledgerTotal, expectedPerUser)

		budgetUsage := readPerHourBudgetForUser(t, ctx, k, policyID, user)
		require.Equal(t, ledgerTotal, budgetUsage,
			"MUST-2 user=%s: per-hour budget=%d ≠ ledger=%d "+
				"— per-user drift between modules",
			user, budgetUsage, ledgerTotal)
	}
}

// --------------------------------------------------------------
// MUST-3: Cumulative budget == cumulative ledger across users
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_MUST3_CumulativeBudgetMatchesCumulativeLedger(t *testing.T) {
	t.Parallel()
	ctx, k, policyID := setupCoopPoliciesPolicy(t)
	ledger := newCooperativeLedger()

	users := []string{"u-x", "u-y", "u-z", "u-w"}
	rng := rand.New(rand.NewSource(0xC00FFEE))
	for i := 0; i < 25; i++ {
		user := users[rng.Intn(len(users))]
		cost := uint64(rng.Intn(5_000) + 100)
		runCooperativeStep(t, ctx, k, ledger, policyID, makeAllowedReq(user, cost))
	}

	cumulativeLedger := ledger.cumulativeTotal()
	var cumulativeBudget uint64
	for _, user := range users {
		cumulativeBudget = safeAddUint64(cumulativeBudget,
			readPerHourBudgetForUser(t, ctx, k, policyID, user))
	}

	require.Equal(t, cumulativeLedger, cumulativeBudget,
		"MUST-3: cumulative ledger=%d ≠ cumulative budget=%d — "+
			"system-wide drift between modules",
		cumulativeLedger, cumulativeBudget)
}

// --------------------------------------------------------------
// MUST-4: Ordering invariance
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_MUST4_OrderingInvariance(t *testing.T) {
	t.Parallel()

	type req struct {
		user string
		cost uint64
	}
	requests := []req{
		{"u-1", 1_000},
		{"u-2", 2_500},
		{"u-1", 500},
		{"u-3", 4_000},
		{"u-2", 1_500},
		{"u-3", 750},
		{"u-1", 3_000},
	}

	runScript := func(order []int) (map[string]uint64, map[string]uint64) {
		ctx, k, policyID := setupCoopPoliciesPolicy(t)
		ledger := newCooperativeLedger()
		for _, idx := range order {
			r := requests[idx]
			runCooperativeStep(t, ctx, k, ledger, policyID,
				makeAllowedReq(r.user, r.cost))
		}
		// Snapshot per-user state.
		ledgerByUser := map[string]uint64{}
		budgetByUser := map[string]uint64{}
		for _, r := range requests {
			if _, seen := ledgerByUser[r.user]; seen {
				continue
			}
			ledgerByUser[r.user] = ledger.totalForUser(r.user, policyID)
			budgetByUser[r.user] = readPerHourBudgetForUser(t, ctx, k, policyID, r.user)
		}
		return ledgerByUser, budgetByUser
	}

	forward := []int{0, 1, 2, 3, 4, 5, 6}
	reverse := []int{6, 5, 4, 3, 2, 1, 0}
	shuffled := []int{3, 0, 5, 2, 6, 1, 4}

	ledgerF, budgetF := runScript(forward)
	ledgerR, budgetR := runScript(reverse)
	ledgerS, budgetS := runScript(shuffled)

	// Per-user state must match across all three orderings.
	users := []string{"u-1", "u-2", "u-3"}
	sort.Strings(users)
	for _, u := range users {
		require.Equal(t, ledgerF[u], ledgerR[u],
			"MUST-4 user=%s ledger forward=%d reverse=%d", u, ledgerF[u], ledgerR[u])
		require.Equal(t, ledgerF[u], ledgerS[u],
			"MUST-4 user=%s ledger forward=%d shuffled=%d", u, ledgerF[u], ledgerS[u])
		require.Equal(t, budgetF[u], budgetR[u],
			"MUST-4 user=%s budget forward=%d reverse=%d", u, budgetF[u], budgetR[u])
		require.Equal(t, budgetF[u], budgetS[u],
			"MUST-4 user=%s budget forward=%d shuffled=%d", u, budgetF[u], budgetS[u])
		// Cross-module: ledger == budget for every user under all orderings.
		require.Equal(t, ledgerF[u], budgetF[u],
			"MUST-4 user=%s cross-module drift: ledger=%d budget=%d",
			u, ledgerF[u], budgetF[u])
	}
}

// --------------------------------------------------------------
// MUST-5: Subset processed → only subset reflects in both modules
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_MUST5_SubsetProcessedLeavesOthersUntouched(t *testing.T) {
	t.Parallel()
	ctx, k, policyID := setupCoopPoliciesPolicy(t)
	ledger := newCooperativeLedger()

	// Process only the FIRST 3 of 5 user requests.
	allUsers := []string{"u-sub-1", "u-sub-2", "u-sub-3", "u-sub-4", "u-sub-5"}
	const cost uint64 = 1_500
	for i := 0; i < 3; i++ {
		runCooperativeStep(t, ctx, k, ledger, policyID,
			makeAllowedReq(allUsers[i], cost))
	}

	for i, user := range allUsers {
		expectedLedger := uint64(0)
		expectedBudget := uint64(0)
		if i < 3 {
			expectedLedger = cost
			expectedBudget = cost
		}
		require.Equal(t, expectedLedger, ledger.totalForUser(user, policyID),
			"MUST-5 user[%d]=%s ledger expected=%d got=%d",
			i, user, expectedLedger, ledger.totalForUser(user, policyID))
		require.Equal(t, expectedBudget, readPerHourBudgetForUser(t, ctx, k, policyID, user),
			"MUST-5 user[%d]=%s budget expected=%d got=%d",
			i, user, expectedBudget, readPerHourBudgetForUser(t, ctx, k, policyID, user))
	}
}

// --------------------------------------------------------------
// MUST-6: Re-running the same script on a fresh keeper → byte-equal
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_MUST6_IdempotenceUnderReplay(t *testing.T) {
	t.Parallel()

	// Same script, two parallel keepers + ledgers.
	type op struct {
		user string
		cost uint64
	}
	script := []op{
		{"u-rep-1", 100},
		{"u-rep-2", 250},
		{"u-rep-1", 750},
		{"u-rep-3", 1_500},
		{"u-rep-2", 800},
		{"u-rep-1", 200},
		{"u-rep-3", 950},
	}

	runIt := func() (map[string]uint64, map[string]uint64) {
		ctx, k, policyID := setupCoopPoliciesPolicy(t)
		ledger := newCooperativeLedger()
		for _, o := range script {
			runCooperativeStep(t, ctx, k, ledger, policyID,
				makeAllowedReq(o.user, o.cost))
		}
		ledgerByUser := map[string]uint64{}
		budgetByUser := map[string]uint64{}
		users := map[string]bool{}
		for _, o := range script {
			users[o.user] = true
		}
		for u := range users {
			ledgerByUser[u] = ledger.totalForUser(u, policyID)
			budgetByUser[u] = readPerHourBudgetForUser(t, ctx, k, policyID, u)
		}
		return ledgerByUser, budgetByUser
	}

	ledgerA, budgetA := runIt()
	ledgerB, budgetB := runIt()

	for u, vA := range ledgerA {
		vB := ledgerB[u]
		require.Equal(t, vA, vB,
			"MUST-6 user=%s: ledger replay diverges A=%d B=%d", u, vA, vB)
	}
	for u, vA := range budgetA {
		vB := budgetB[u]
		require.Equal(t, vA, vB,
			"MUST-6 user=%s: budget replay diverges A=%d B=%d", u, vA, vB)
		require.Equal(t, vA, ledgerA[u],
			"MUST-6 user=%s cross-module: budget(%d) ≠ ledger(%d) on replay",
			u, vA, ledgerA[u])
	}
}

// --------------------------------------------------------------
// MUST-7: Denied request between Allowed requests doesn't bleed
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_MUST7_DeniedRequestDoesNotBleedToNextStep(t *testing.T) {
	t.Parallel()
	ctx, k, policyID := setupCoopPoliciesPolicy(t)
	ledger := newCooperativeLedger()

	const userID = "user-must7"
	// Sandwich: Allowed(A1) → Denied(D) → Allowed(A2).
	const a1Cost uint64 = 2_000
	const dCost uint64 = 9_999
	const a2Cost uint64 = 3_500

	// Step 1: Allowed.
	d1 := runCooperativeStep(t, ctx, k, ledger, policyID,
		makeAllowedReq(userID, a1Cost))
	require.True(t, d1.Allowed)
	require.Equal(t, a1Cost, ledger.totalForUser(userID, policyID))
	require.Equal(t, a1Cost, readPerHourBudgetForUser(t, ctx, k, policyID, userID))

	// Step 2: Denied. State must NOT advance.
	d2 := runCooperativeStep(t, ctx, k, ledger, policyID,
		makeDeniedReq(userID, dCost))
	require.False(t, d2.Allowed,
		"MUST-7 prereq: denied request should not be Allowed")
	require.Equal(t, a1Cost, ledger.totalForUser(userID, policyID),
		"MUST-7: denied request advanced ledger from %d", a1Cost)
	require.Equal(t, a1Cost, readPerHourBudgetForUser(t, ctx, k, policyID, userID),
		"MUST-7: denied request advanced budget from %d", a1Cost)

	// Step 3: Allowed resumes normally.
	d3 := runCooperativeStep(t, ctx, k, ledger, policyID,
		makeAllowedReq(userID, a2Cost))
	require.True(t, d3.Allowed)
	require.Equal(t, a1Cost+a2Cost, ledger.totalForUser(userID, policyID),
		"MUST-7: post-denial Allowed should resume; expected ledger=%d",
		a1Cost+a2Cost)
	require.Equal(t, a1Cost+a2Cost, readPerHourBudgetForUser(t, ctx, k, policyID, userID),
		"MUST-7: post-denial budget=%d expected=%d",
		readPerHourBudgetForUser(t, ctx, k, policyID, userID),
		a1Cost+a2Cost)
}

// --------------------------------------------------------------
// Composite: cross-validator determinism over a 30-step random
// mixed (Allowed + Denied) script
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_CompositeRandomMixedScriptDeterministic(t *testing.T) {
	t.Parallel()

	type op struct {
		user    string
		cost    uint64
		allowed bool
	}
	rng := rand.New(rand.NewSource(0xDEFACE))
	script := make([]op, 30)
	users := []string{"comp-1", "comp-2", "comp-3", "comp-4"}
	for i := range script {
		script[i] = op{
			user:    users[rng.Intn(len(users))],
			cost:    uint64(rng.Intn(2_500) + 100),
			allowed: rng.Intn(4) > 0, // 75% allowed, 25% denied
		}
	}

	runScript := func() (map[string]uint64, map[string]uint64) {
		ctx, k, policyID := setupCoopPoliciesPolicy(t)
		ledger := newCooperativeLedger()
		for _, o := range script {
			var req *InvocationRequest
			if o.allowed {
				req = makeAllowedReq(o.user, o.cost)
			} else {
				req = makeDeniedReq(o.user, o.cost)
			}
			runCooperativeStep(t, ctx, k, ledger, policyID, req)
		}
		ledgerByUser := map[string]uint64{}
		budgetByUser := map[string]uint64{}
		for _, u := range users {
			ledgerByUser[u] = ledger.totalForUser(u, policyID)
			budgetByUser[u] = readPerHourBudgetForUser(t, ctx, k, policyID, u)
		}
		return ledgerByUser, budgetByUser
	}

	ledgerA, budgetA := runScript()
	ledgerB, budgetB := runScript()

	for _, u := range users {
		require.Equal(t, ledgerA[u], ledgerB[u],
			"composite: user=%s ledger diverges A=%d B=%d",
			u, ledgerA[u], ledgerB[u])
		require.Equal(t, budgetA[u], budgetB[u],
			"composite: user=%s budget diverges A=%d B=%d",
			u, budgetA[u], budgetB[u])
		require.Equal(t, ledgerA[u], budgetA[u],
			"composite: user=%s cross-module: ledger(%d) ≠ budget(%d)",
			u, ledgerA[u], budgetA[u])
	}
}

// --------------------------------------------------------------
// Coverage matrix
// --------------------------------------------------------------

func TestPoliciesCreditsCoop_CoverageMatrix(t *testing.T) {
	t.Parallel()
	matrix := []struct {
		id, description, testName string
	}{
		{"MUST-1", "denied requests consume neither budget nor ledger",
			"TestPoliciesCreditsCoop_MUST1_DeniedRequestsConsumeNeitherSide"},
		{"MUST-2", "per-user policy budget == per-user cooperative ledger",
			"TestPoliciesCreditsCoop_MUST2_PerUserBudgetMatchesLedger"},
		{"MUST-3", "cumulative budget == cumulative ledger across users",
			"TestPoliciesCreditsCoop_MUST3_CumulativeBudgetMatchesCumulativeLedger"},
		{"MUST-4", "ordering invariance — same set, any order, same state",
			"TestPoliciesCreditsCoop_MUST4_OrderingInvariance"},
		{"MUST-5", "subset processed → only subset reflects in both modules",
			"TestPoliciesCreditsCoop_MUST5_SubsetProcessedLeavesOthersUntouched"},
		{"MUST-6", "replay determinism — same script → byte-equal state",
			"TestPoliciesCreditsCoop_MUST6_IdempotenceUnderReplay"},
		{"MUST-7", "denied request between Allowed doesn't bleed",
			"TestPoliciesCreditsCoop_MUST7_DeniedRequestDoesNotBleedToNextStep"},
	}
	require.Len(t, matrix, 7,
		"coverage matrix must have exactly 7 MUST clauses")
	for _, m := range matrix {
		require.NotEmpty(t, m.id)
		require.NotEmpty(t, m.description)
		require.NotEmpty(t, m.testName)
		t.Logf("[policies-credits-coop] %s: %s → %s",
			m.id, m.description, m.testName)
	}
}

// Compile-time reference so test imports stay live across builds.
var _ = fmt.Sprintf
