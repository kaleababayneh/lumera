//go:build cosmos
// +build cosmos

package keeper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression guards for validateBudgetInvariants (lumera_ai-t7cld,
// scoped sub-fix for lumera_ai-9oh42). These tests pin the monotonic-
// nesting invariant for caller-supplied period accumulators:
//
//	HourCostMicroLAC <= DayCostMicroLAC <= WeekCostMicroLAC <= MonthCostMicroLAC
//
// Session accumulator is intentionally excluded — sessions can span
// multiple hours/days, so SessionCostMicroLAC has no ordering relation
// with time-bucket accumulators.
//
// The invariant must hold across bucket rollovers: immediately after
// an hour flip, HourCost resets to 0 but the previous hour's cost is
// still counted in Day/Week/Month. So the monotonic chain is
// preserved as 0 <= X <= X <= X. Same for day/week/month rollovers.
//
// Failures of this invariant are provably wrong inputs — either a
// caller bug or an adversarial caller trying to bypass budget limits
// by understating a large-bucket accumulator. Rejecting them at the
// validation gate prevents silent evaluation against falsified data.

// TestValidateBudgetInvariants_RejectsHourExceedsDay pins the
// innermost violation. An attacker supplying hour_cost=100,
// day_cost=50 would — pre-fix — pass the per-day check (50+new <=
// limit) even though actual per-hour spend already exceeds per-day.
func TestValidateBudgetInvariants_RejectsHourExceedsDay(t *testing.T) {
	t.Parallel()
	req := &InvocationRequest{
		CostMicroLAC:      10,
		HourCostMicroLAC:  100,
		DayCostMicroLAC:   50, // violation: hour > day
		WeekCostMicroLAC:  100,
		MonthCostMicroLAC: 100,
	}
	reason := validateBudgetInvariants(req)
	require.NotEmpty(t, reason,
		"hour_cost > day_cost is a provably impossible temporal "+
			"nesting — must reject. Pre-guard this would silently "+
			"evaluate budget checks against the lie.")
	assert.Contains(t, reason, "hour_cost=100")
	assert.Contains(t, reason, "day_cost=50")
	assert.Contains(t, reason, "temporal nesting")
}

// TestValidateBudgetInvariants_RejectsDayExceedsWeek pins the
// middle violation. day_cost must be a subset of week_cost.
func TestValidateBudgetInvariants_RejectsDayExceedsWeek(t *testing.T) {
	t.Parallel()
	req := &InvocationRequest{
		HourCostMicroLAC:  10,
		DayCostMicroLAC:   500,
		WeekCostMicroLAC:  200, // violation
		MonthCostMicroLAC: 1000,
	}
	reason := validateBudgetInvariants(req)
	require.NotEmpty(t, reason)
	assert.Contains(t, reason, "day_cost=500")
	assert.Contains(t, reason, "week_cost=200")
}

// TestValidateBudgetInvariants_RejectsWeekExceedsMonth pins the
// outermost violation. week_cost must be a subset of month_cost.
func TestValidateBudgetInvariants_RejectsWeekExceedsMonth(t *testing.T) {
	t.Parallel()
	req := &InvocationRequest{
		HourCostMicroLAC:  10,
		DayCostMicroLAC:   100,
		WeekCostMicroLAC:  800,
		MonthCostMicroLAC: 500, // violation
	}
	reason := validateBudgetInvariants(req)
	require.NotEmpty(t, reason)
	assert.Contains(t, reason, "week_cost=800")
	assert.Contains(t, reason, "month_cost=500")
}

// TestValidateBudgetInvariants_AcceptsMonotonicChain is the negative-
// regression guard: legitimate monotonic chains must pass.
func TestValidateBudgetInvariants_AcceptsMonotonicChain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		req  *InvocationRequest
	}{
		{
			name: "strictly_increasing",
			req: &InvocationRequest{
				HourCostMicroLAC:  100,
				DayCostMicroLAC:   500,
				WeekCostMicroLAC:  2000,
				MonthCostMicroLAC: 10000,
			},
		},
		{
			name: "all_zero_fresh_session",
			req: &InvocationRequest{
				HourCostMicroLAC:  0,
				DayCostMicroLAC:   0,
				WeekCostMicroLAC:  0,
				MonthCostMicroLAC: 0,
			},
		},
		{
			name: "post_hour_rollover_hour_zero",
			req: &InvocationRequest{
				HourCostMicroLAC:  0,   // just rolled over
				DayCostMicroLAC:   500, // previous hour's spend
				WeekCostMicroLAC:  500,
				MonthCostMicroLAC: 500,
			},
		},
		{
			name: "all_equal_first_invocation_of_week",
			req: &InvocationRequest{
				HourCostMicroLAC:  50,
				DayCostMicroLAC:   50,
				WeekCostMicroLAC:  50,
				MonthCostMicroLAC: 50,
			},
		},
		{
			name: "session_unrelated_to_monotonic_chain",
			req: &InvocationRequest{
				SessionCostMicroLAC: 10000, // session >> other buckets, OK
				HourCostMicroLAC:    10,
				DayCostMicroLAC:     100,
				WeekCostMicroLAC:    500,
				MonthCostMicroLAC:   2000,
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reason := validateBudgetInvariants(tc.req)
			require.Empty(t, reason,
				"legitimate monotonic chain %s must pass — guard must "+
					"not over-reject. Reason: %s", tc.name, reason)
		})
	}
}

// TestValidateBudgetInvariants_SessionOrderingIgnored explicitly
// confirms that session cost is NOT part of the monotonic chain.
// A session spanning many hours can legitimately exceed the current
// hour's/day's/week's bucket, and that must not trip the guard.
func TestValidateBudgetInvariants_SessionOrderingIgnored(t *testing.T) {
	t.Parallel()
	// Session has accumulated a lot from previous hours; current hour
	// is fresh; current day just rolled over. All valid.
	req := &InvocationRequest{
		SessionCostMicroLAC: 100000,
		HourCostMicroLAC:    5,
		DayCostMicroLAC:     5,
		WeekCostMicroLAC:    100000,
		MonthCostMicroLAC:   100000,
	}
	reason := validateBudgetInvariants(req)
	assert.Empty(t, reason,
		"session cost is intentionally orthogonal to the time-bucket "+
			"chain — high session cost with fresh hour/day buckets is "+
			"legitimate and must not trigger the invariant guard")
}
