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

// This file applies the testing-metamorphic skill to the
// COOPERATIVE consistency between x/policies enforce and
// x/credits Lock. Where tick 2b75bab27 (cooperative-consistency
// conformance) pinned 7 specific MUST clauses, this file pins
// METAMORPHIC RELATIONS — invariants that hold across MANY
// input variations of the same cooperative flow.
//
// Cooperative pattern recap:
//
//   For each invocation:
//     1. EvaluatePolicy(policyID, req with CostMicroLAC)
//     2. if decision.Allowed: caller invokes credits.LockCredits(amount)
//     3. policies budget counters increment by CostMicroLAC
//     4. credits escrow accumulates the locked amount
//
// THE NO-DRIFT INVARIANT: Σ policies-budget consumed for a
// (user, policy) tuple == Σ credits-Lock amounts for the same
// tuple, at every moment.
//
// Six MRs across the cooperative flow:
//
//   MR-1 (LINEARITY): scaling all CostMicroLAC by k yields
//     k× both sides (no fixed offsets, no nonlinearity)
//   MR-2 (COMMUTATIVITY): requests in any order produce
//     IDENTICAL final state (no order-dependent accounting)
//   MR-3 (DISTRIBUTION): one big-cost request equals N
//     small requests summing to the same cost (no per-request
//     overhead diverging the totals)
//   MR-4 (ZERO-COST-NEUTRALITY): a request with CostMicroLAC=0
//     increments NEITHER side (no spurious 1-unit drift)
//   MR-5 (DENIAL-NULL-EFFECT): a denied request leaves both
//     sides EXACTLY unchanged (deny path doesn't half-commit)
//   MR-6 (REPLAY-DETERMINISM): re-running the same script on a
//     fresh keeper yields byte-equal final state per user

// --------------------------------------------------------------
// Cooperative ledger — mirrors x/credits Lock from caller's view.
// --------------------------------------------------------------

type driftLedger struct {
	locks map[string]uint64 // key="userID|policyID" → cumulative lock
}

func newDriftLedger() *driftLedger {
	return &driftLedger{locks: map[string]uint64{}}
}

func (l *driftLedger) lock(userID, policyID string, amount uint64) {
	l.locks[userID+"|"+policyID] = safeAddUint64(l.locks[userID+"|"+policyID], amount)
}

func (l *driftLedger) totalForUser(userID, policyID string) uint64 {
	return l.locks[userID+"|"+policyID]
}

func (l *driftLedger) cumulativeTotal() uint64 {
	var t uint64
	for _, v := range l.locks {
		t = safeAddUint64(t, v)
	}
	return t
}

// --------------------------------------------------------------
// Test fixture — generous budget policy so MR tests aren't
// gated on limit-rejection (which is tested elsewhere).
// --------------------------------------------------------------

func setupDriftPolicy(t *testing.T) (sdk.Context, *Keeper, string) {
	t.Helper()
	ctx, k := setupPoliciesKeeper(t)
	policy := makeDriftPolicy()
	require.NoError(t, k.CreatePolicy(ctx, policy))
	require.NoError(t, k.SetPolicyState(ctx, policy.PolicyId, policy.Version,
		types.PolicyState_POLICY_STATE_ACTIVE))
	return ctx, k, policy.PolicyId
}

func makeDriftPolicy() *types.PolicyProfile {
	return &types.PolicyProfile{
		PolicyId:      "drift-policy-1",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Drift MR Policy",
			Owner: "lumera1driftowner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: "lumera1driftowner",
		},
		ToolFilters: &types.ToolFilters{
			AllowedTools:        []string{"drift-tool-allow"},
			DeniedTools:         []string{"drift-tool-deny"},
			MinVerificationTier: types.VerificationTier_VERIFICATION_TIER_BRONZE,
		},
		Budgets: &types.BudgetControls{
			PerCall: &types.BudgetLimit{SoftLimit: "100000000", HardLimit: "100000000"},
			PerHour: &types.BudgetLimit{SoftLimit: "1000000000", HardLimit: "1000000000"},
			PerDay:  &types.BudgetLimit{SoftLimit: "10000000000", HardLimit: "10000000000"},
		},
	}
}

func makeDriftAllowedReq(userID string, cost uint64) *InvocationRequest {
	return &InvocationRequest{
		Tool: ToolContext{
			ToolID:           "drift-tool-allow",
			VerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
		},
		UserID:       userID,
		SessionID:    "drift-sess-" + userID,
		CostMicroLAC: cost,
	}
}

func makeDriftDeniedReq(userID string, cost uint64) *InvocationRequest {
	return &InvocationRequest{
		Tool: ToolContext{
			ToolID:           "drift-tool-deny",
			VerificationTier: types.VerificationTier_VERIFICATION_TIER_GOLD,
		},
		UserID:       userID,
		SessionID:    "drift-sess-" + userID,
		CostMicroLAC: cost,
	}
}

// runStep is the cooperative caller: EvaluatePolicy → if Allowed,
// lock in the cooperative ledger.
func runStep(t *testing.T, ctx sdk.Context, k *Keeper, ledger *driftLedger, policyID string, req *InvocationRequest) *PolicyDecision {
	t.Helper()
	decision, err := k.EvaluatePolicy(ctx, policyID, req)
	require.NoError(t, err)
	if decision.Allowed {
		ledger.lock(req.UserID, policyID, req.CostMicroLAC)
	}
	return decision
}

func readPerHourBudget(t *testing.T, ctx sdk.Context, k *Keeper, policyID, userID string) uint64 {
	t.Helper()
	_, err := k.GetPolicy(ctx, policyID, "")
	require.NoError(t, err)
	periods := budgetUsagePeriods(ctx)
	key := budgetUsageKey(policyID, userID, "per-hour", periods.hour)
	usage, err := k.getBudgetUsage(ctx, key)
	require.NoError(t, err)
	return usage
}

// --------------------------------------------------------------
// MR 1 (LINEARITY): scale all costs by k → k× both sides
// --------------------------------------------------------------

func TestPoliciesCreditsDrift_MR_LinearityInCostScale(t *testing.T) {
	t.Parallel()

	costs := []uint64{500, 1500, 750, 3000}
	const userID = "user-linear"

	runWithScale := func(scale uint64) (budget, ledgerSum uint64) {
		ctx, k, policyID := setupDriftPolicy(t)
		ledger := newDriftLedger()
		for _, c := range costs {
			runStep(t, ctx, k, ledger, policyID,
				makeDriftAllowedReq(userID, c*scale))
		}
		budget = readPerHourBudget(t, ctx, k, policyID, userID)
		ledgerSum = ledger.totalForUser(userID, policyID)
		return budget, ledgerSum
	}

	b1, l1 := runWithScale(1)
	b2, l2 := runWithScale(2)
	b3, l3 := runWithScale(3)

	require.Equal(t, b1, l1, "MR-1 baseline: budget=ledger at scale=1")
	require.Equal(t, b2, l2, "MR-1 scale=2: budget=ledger")
	require.Equal(t, b3, l3, "MR-1 scale=3: budget=ledger")

	require.Equal(t, b1*2, b2,
		"MR-1: scale=2 budget(%d) ≠ 2× scale=1 budget(%d×2=%d)",
		b2, b1, b1*2)
	require.Equal(t, b1*3, b3,
		"MR-1: scale=3 budget(%d) ≠ 3× scale=1 budget(%d×3=%d)",
		b3, b1, b1*3)
	require.Equal(t, l1*2, l2,
		"MR-1: scale=2 ledger(%d) ≠ 2× scale=1 ledger(%d×2=%d)",
		l2, l1, l1*2)
	require.Equal(t, l1*3, l3,
		"MR-1: scale=3 ledger(%d) ≠ 3× scale=1 ledger(%d×3=%d)",
		l3, l1, l1*3)
}

// --------------------------------------------------------------
// MR 2 (COMMUTATIVITY): order doesn't matter
// --------------------------------------------------------------

func TestPoliciesCreditsDrift_MR_OrderCommutative(t *testing.T) {
	t.Parallel()

	costs := []uint64{700, 200, 1300, 450, 900}
	users := []string{"u-a", "u-b", "u-c"}

	type op struct {
		user string
		cost uint64
	}
	// Build a script: each user gets each cost.
	script := make([]op, 0, len(users)*len(costs))
	for _, u := range users {
		for _, c := range costs {
			script = append(script, op{user: u, cost: c})
		}
	}

	runOrder := func(perm []int) (map[string]uint64, map[string]uint64) {
		ctx, k, policyID := setupDriftPolicy(t)
		ledger := newDriftLedger()
		for _, i := range perm {
			s := script[i]
			runStep(t, ctx, k, ledger, policyID,
				makeDriftAllowedReq(s.user, s.cost))
		}
		ledgerByUser := map[string]uint64{}
		budgetByUser := map[string]uint64{}
		for _, u := range users {
			ledgerByUser[u] = ledger.totalForUser(u, policyID)
			budgetByUser[u] = readPerHourBudget(t, ctx, k, policyID, u)
		}
		return ledgerByUser, budgetByUser
	}

	// Orderings: forward, reverse, deterministic shuffle.
	forward := make([]int, len(script))
	for i := range forward {
		forward[i] = i
	}
	reverse := make([]int, len(script))
	for i := range reverse {
		reverse[i] = len(script) - 1 - i
	}
	shuffled := make([]int, len(script))
	copy(shuffled, forward)
	rng := rand.New(rand.NewSource(0xCAFEDEAD))
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	ledgerF, budgetF := runOrder(forward)
	ledgerR, budgetR := runOrder(reverse)
	ledgerS, budgetS := runOrder(shuffled)

	for _, u := range users {
		require.Equal(t, ledgerF[u], ledgerR[u],
			"MR-2 user=%s: forward ledger(%d) ≠ reverse(%d)",
			u, ledgerF[u], ledgerR[u])
		require.Equal(t, ledgerF[u], ledgerS[u],
			"MR-2 user=%s: forward ledger(%d) ≠ shuffled(%d)",
			u, ledgerF[u], ledgerS[u])
		require.Equal(t, budgetF[u], budgetR[u],
			"MR-2 user=%s: forward budget(%d) ≠ reverse(%d)",
			u, budgetF[u], budgetR[u])
		require.Equal(t, budgetF[u], budgetS[u],
			"MR-2 user=%s: forward budget(%d) ≠ shuffled(%d)",
			u, budgetF[u], budgetS[u])
		require.Equal(t, ledgerF[u], budgetF[u],
			"MR-2 user=%s cross-module: ledger ≠ budget at any ordering",
			u)
	}
}

// --------------------------------------------------------------
// MR 3 (DISTRIBUTION): one big-cost == N small summing to same total
// --------------------------------------------------------------

func TestPoliciesCreditsDrift_MR_OneBigEqualsManySmallSummingToSame(t *testing.T) {
	t.Parallel()

	const userID = "user-distrib"
	const totalCost uint64 = 6000
	const numSmall = 12 // each = 500

	runOneBig := func() (budget, ledger uint64) {
		ctx, k, policyID := setupDriftPolicy(t)
		l := newDriftLedger()
		runStep(t, ctx, k, l, policyID,
			makeDriftAllowedReq(userID, totalCost))
		return readPerHourBudget(t, ctx, k, policyID, userID),
			l.totalForUser(userID, policyID)
	}

	runManySmall := func() (budget, ledger uint64) {
		ctx, k, policyID := setupDriftPolicy(t)
		l := newDriftLedger()
		each := totalCost / numSmall
		for i := 0; i < numSmall; i++ {
			runStep(t, ctx, k, l, policyID,
				makeDriftAllowedReq(userID, each))
		}
		return readPerHourBudget(t, ctx, k, policyID, userID),
			l.totalForUser(userID, policyID)
	}

	bigB, bigL := runOneBig()
	smallB, smallL := runManySmall()

	require.Equal(t, bigB, bigL,
		"MR-3 prereq: one-big budget=ledger")
	require.Equal(t, smallB, smallL,
		"MR-3 prereq: many-small budget=ledger")
	require.Equal(t, bigB, smallB,
		"MR-3: one-big budget(%d) ≠ many-small budget(%d) — "+
			"per-request overhead diverging the totals",
		bigB, smallB)
	require.Equal(t, bigL, smallL,
		"MR-3: one-big ledger(%d) ≠ many-small ledger(%d)",
		bigL, smallL)
}

// --------------------------------------------------------------
// MR 4 (ZERO-COST-NEUTRALITY): cost=0 increments neither side
// --------------------------------------------------------------

func TestPoliciesCreditsDrift_MR_ZeroCostNeutrality(t *testing.T) {
	t.Parallel()
	ctx, k, policyID := setupDriftPolicy(t)
	ledger := newDriftLedger()
	const userID = "user-zero"

	// Baseline: a 1000-cost request.
	runStep(t, ctx, k, ledger, policyID, makeDriftAllowedReq(userID, 1000))
	bAfter1, lAfter1 := readPerHourBudget(t, ctx, k, policyID, userID),
		ledger.totalForUser(userID, policyID)

	// Several 0-cost requests interleaved.
	for i := 0; i < 5; i++ {
		runStep(t, ctx, k, ledger, policyID, makeDriftAllowedReq(userID, 0))
	}
	bAfterZeros, lAfterZeros := readPerHourBudget(t, ctx, k, policyID, userID),
		ledger.totalForUser(userID, policyID)

	require.Equal(t, bAfter1, bAfterZeros,
		"MR-4: 0-cost requests changed budget %d → %d (must be no-op)",
		bAfter1, bAfterZeros)
	require.Equal(t, lAfter1, lAfterZeros,
		"MR-4: 0-cost requests changed ledger %d → %d (must be no-op)",
		lAfter1, lAfterZeros)

	// Final: another 1000-cost request increments by exactly 1000.
	runStep(t, ctx, k, ledger, policyID, makeDriftAllowedReq(userID, 1000))
	require.Equal(t, bAfter1+1000, readPerHourBudget(t, ctx, k, policyID, userID),
		"MR-4: post-zero non-zero request must increment by exactly its cost")
	require.Equal(t, lAfter1+1000, ledger.totalForUser(userID, policyID),
		"MR-4: ledger must mirror budget across the zero-bracket")
}

// --------------------------------------------------------------
// MR 5 (DENIAL-NULL-EFFECT): denied requests leave both sides untouched
// --------------------------------------------------------------

func TestPoliciesCreditsDrift_MR_DeniedRequestNullEffect(t *testing.T) {
	t.Parallel()
	ctx, k, policyID := setupDriftPolicy(t)
	ledger := newDriftLedger()
	const userID = "user-deny"

	// Establish baseline.
	runStep(t, ctx, k, ledger, policyID, makeDriftAllowedReq(userID, 2000))
	bBefore, lBefore := readPerHourBudget(t, ctx, k, policyID, userID),
		ledger.totalForUser(userID, policyID)

	// 3 denied requests with various costs — none should advance state.
	for _, cost := range []uint64{500, 1000, 5000} {
		decision := runStep(t, ctx, k, ledger, policyID,
			makeDriftDeniedReq(userID, cost))
		require.False(t, decision.Allowed,
			"MR-5 prereq: denied tool must produce decision.Allowed=false")
	}

	bAfter, lAfter := readPerHourBudget(t, ctx, k, policyID, userID),
		ledger.totalForUser(userID, policyID)
	require.Equal(t, bBefore, bAfter,
		"MR-5: budget changed from %d to %d through 3 denials",
		bBefore, bAfter)
	require.Equal(t, lBefore, lAfter,
		"MR-5: ledger changed from %d to %d through 3 denials",
		lBefore, lAfter)
}

// --------------------------------------------------------------
// MR 6 (REPLAY-DETERMINISM): same script → byte-equal state
// --------------------------------------------------------------

func TestPoliciesCreditsDrift_MR_ReplayDeterministic(t *testing.T) {
	t.Parallel()

	type op struct {
		user    string
		cost    uint64
		allowed bool
	}
	rng := rand.New(rand.NewSource(0xFEEDBEEF))
	users := []string{"rd-u1", "rd-u2", "rd-u3", "rd-u4"}
	script := make([]op, 30)
	for i := range script {
		script[i] = op{
			user:    users[rng.Intn(len(users))],
			cost:    uint64(rng.Intn(2_500) + 100),
			allowed: rng.Intn(4) > 0, // 75% allowed, 25% denied
		}
	}

	runScript := func() (map[string]uint64, map[string]uint64) {
		ctx, k, policyID := setupDriftPolicy(t)
		ledger := newDriftLedger()
		for _, op := range script {
			var req *InvocationRequest
			if op.allowed {
				req = makeDriftAllowedReq(op.user, op.cost)
			} else {
				req = makeDriftDeniedReq(op.user, op.cost)
			}
			runStep(t, ctx, k, ledger, policyID, req)
		}
		ledgerByUser := map[string]uint64{}
		budgetByUser := map[string]uint64{}
		for _, u := range users {
			ledgerByUser[u] = ledger.totalForUser(u, policyID)
			budgetByUser[u] = readPerHourBudget(t, ctx, k, policyID, u)
		}
		return ledgerByUser, budgetByUser
	}

	ledgerA, budgetA := runScript()
	ledgerB, budgetB := runScript()

	for _, u := range users {
		require.Equal(t, ledgerA[u], ledgerB[u],
			"MR-6 user=%s: ledger replay diverges A=%d B=%d",
			u, ledgerA[u], ledgerB[u])
		require.Equal(t, budgetA[u], budgetB[u],
			"MR-6 user=%s: budget replay diverges A=%d B=%d",
			u, budgetA[u], budgetB[u])
		require.Equal(t, ledgerA[u], budgetA[u],
			"MR-6 user=%s cross-module: ledger ≠ budget on replay",
			u)
	}
}

// --------------------------------------------------------------
// Composite: cumulative budget across users == cumulative ledger
// (cross-user no-drift over a 30-step random script)
// --------------------------------------------------------------

func TestPoliciesCreditsDrift_MR_CumulativeNoDriftAcrossUsers(t *testing.T) {
	t.Parallel()
	ctx, k, policyID := setupDriftPolicy(t)
	ledger := newDriftLedger()

	users := []string{"cum-u1", "cum-u2", "cum-u3", "cum-u4", "cum-u5"}
	rng := rand.New(rand.NewSource(0xBABECAFE))
	for i := 0; i < 30; i++ {
		user := users[rng.Intn(len(users))]
		cost := uint64(rng.Intn(5_000) + 100)
		runStep(t, ctx, k, ledger, policyID,
			makeDriftAllowedReq(user, cost))
	}

	cumulativeLedger := ledger.cumulativeTotal()
	var cumulativeBudget uint64
	usersSorted := append([]string{}, users...)
	sort.Strings(usersSorted)
	for _, u := range usersSorted {
		cumulativeBudget = safeAddUint64(cumulativeBudget,
			readPerHourBudget(t, ctx, k, policyID, u))
	}

	require.Equal(t, cumulativeLedger, cumulativeBudget,
		"composite no-drift: cumulative ledger=%d ≠ cumulative budget=%d",
		cumulativeLedger, cumulativeBudget)
	require.Greater(t, cumulativeLedger, uint64(0),
		"composite sanity: cumulative ledger should be positive")
}

// reference fmt to keep imports lean across builds.
var _ = fmt.Sprintf
