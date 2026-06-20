//go:build cosmos
// +build cosmos

package keeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// Characterization pins for the budget-period rollover semantics of
// x/policies/keeper/enforce.go:budgetUsagePeriods. Period IDs are
// derived from ctx.BlockTime().UTC() via Go time.Format strings:
//
//	hour  = "2006010215"       (yyyymmddHH)
//	day   = "20060102"         (yyyymmdd)
//	week  = "YYYY-WNN" ISO week
//	month = "200601"           (yyyymm)
//
// and each is embedded into budgetUsageKey alongside policy ID,
// version, user, and scope. Period boundaries therefore behave as
// "reset" events for the corresponding scope's keeper counter —
// crossing an hour boundary puts subsequent calls into a fresh key
// for per-hour while leaving per-day / per-week / per-month
// aggregating against the SAME keys.
//
// Before this file, no test in x/policies exercised a block-time
// advance across an hour or day boundary to pin that the scopes are
// independently time-keyed. A refactor that accidentally changed the
// period-ID format string (e.g., dropping the hour from per-hour, or
// using LOCAL timezone instead of UTC, or reading a relative
// block-height counter) would go unnoticed until production data
// drifted. These tests anchor the current contract.

// TestEvaluatePolicy_HourBoundaryResetsPerHourNotPerDay pins that
// advancing the block time across an hour boundary within the same
// UTC day:
//
//   - resets the per-hour counter (the user gets a fresh hour budget)
//   - does NOT reset the per-day counter (per-day continues to
//     accumulate across hours within the same day)
//
// Without this pin, a refactor that accidentally included the hour
// in the day-period format string (e.g., "2006010215" for both)
// would silently reset per-day every hour. Test would catch that
// regression in the per-day assertion after the hour flip.
func TestEvaluatePolicy_HourBoundaryResetsPerHourNotPerDay(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	const policyID = "budget-rollover-hour"
	const userID = "user-rollover-hour"

	policy := &types.PolicyProfile{
		PolicyId: policyID,
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Hour Rollover",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerHour: &types.BudgetLimit{SoftLimit: "800", HardLimit: "1000"},
			// PerDay is generous enough that neither hour alone
			// can trip it, but BOTH together SHOULD NOT exceed it
			// (we use 2000 here, so 900 + 900 = 1800 stays under).
			PerDay: &types.BudgetLimit{SoftLimit: "1800", HardLimit: "2000"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Anchor the first call to a stable UTC hour (14:30:00 UTC on
	// 2024-01-15). The date itself is deliberate — it is not a DST
	// transition for any common timezone, so a regression that
	// accidentally used local time would be caught regardless of
	// test-host TZ.
	hour1 := time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC)
	ctx1 := ctx.WithBlockTime(hour1)

	first, err := k.EvaluatePolicy(ctx1, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.True(t, first.Allowed, "first call must pass: 900 under both per-hour (1000) and per-day (2000)")

	// A second call in the SAME hour would blow the per-hour budget
	// (900 + 200 = 1100 > 1000 hard). Pin that the per-hour gate
	// fires before the hour flip resets it.
	denied, err := k.EvaluatePolicy(ctx1, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 200,
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed, "same-hour call must be denied — per-hour saturated")
	require.Contains(t, denied.DenialReason, "per-hour")

	// Advance to the NEXT hour on the SAME day (15:05:00 UTC). The
	// hour-period format "2006010215" produces "2024011515" vs the
	// previous "2024011514", so the per-hour key changes. The day-
	// period format "20060102" produces "20240115" for BOTH, so the
	// per-day key is unchanged and still holds the 900 from the
	// first call.
	hour2 := time.Date(2024, 1, 15, 15, 5, 0, 0, time.UTC)
	ctx2 := ctx.WithBlockTime(hour2)

	// A 900-cost call in hour 15 must PASS because:
	//   - per-hour: new bucket at 0, 0+900 <= 1000 → pass
	//   - per-day:  existing bucket at 900, 900+900 = 1800 <= 2000 → pass
	nextHour, err := k.EvaluatePolicy(ctx2, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.True(t, nextHour.Allowed,
		"hour-flipped call must pass: per-hour resets to 0 (%s) while per-day keeps 900 (%s)",
		nextHour.DenialReason, nextHour.DenialReason)

	// Verify the counters directly via getBudgetUsage to pin the
	// exact key-level behaviour that would regress if someone swapped
	// the format string. In the new hour:
	//   - per-hour bucket should hold exactly 900 (only THIS call)
	//   - per-day bucket should hold 900+900 = 1800 (both calls)
	periods2 := budgetUsagePeriods(ctx2)
	hour2Key := budgetUsageKey(policyID, userID, "per-hour", periods2.hour)
	day2Key := budgetUsageKey(policyID, userID, "per-day", periods2.day)

	hour2Usage, err := k.getBudgetUsage(ctx2, hour2Key)
	require.NoError(t, err)
	assert.Equal(t, uint64(900), hour2Usage,
		"new hour bucket holds only the post-flip call (900); got %d — "+
			"if this is 1800 the hour format string no longer distinguishes hours",
		hour2Usage)

	day2Usage, err := k.getBudgetUsage(ctx2, day2Key)
	require.NoError(t, err)
	assert.Equal(t, uint64(1800), day2Usage,
		"day bucket holds both pre- and post-flip calls (1800); got %d — "+
			"if this is 900 the day bucket was erroneously reset by the hour flip",
		day2Usage)

	// Pin that the OLD hour's bucket still holds 900 — it is not
	// cleared retroactively. Budget counters are append-only until
	// the TTL / cleanup path runs (which is not exercised here).
	periods1 := budgetUsagePeriods(ctx1)
	hour1Key := budgetUsageKey(policyID, userID, "per-hour", periods1.hour)
	hour1Usage, err := k.getBudgetUsage(ctx1, hour1Key)
	require.NoError(t, err)
	assert.Equal(t, uint64(900), hour1Usage,
		"historical hour bucket must retain its 900; got %d — "+
			"retroactive pruning is not part of the current contract",
		hour1Usage)
}

// TestEvaluatePolicy_DayBoundaryResetsPerHourAndPerDay pins that
// advancing the block time across a UTC day boundary resets both the
// per-hour AND the per-day counters (the user's rolling-window budget
// starts fresh). Complements the hour-only pin above: a refactor that
// left per-day aggregating across days (e.g., changed to
// "200601" which drops the day component) would pass the
// hour-boundary test but fail this one. Conversely, a refactor that
// keyed per-day to something monotonically non-resetting (e.g.,
// block height) would make the post-flip assertion fail here.
func TestEvaluatePolicy_DayBoundaryResetsPerHourAndPerDay(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	const policyID = "budget-rollover-day"
	const userID = "user-rollover-day"

	policy := &types.PolicyProfile{
		PolicyId: policyID,
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Day Rollover",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerHour: &types.BudgetLimit{SoftLimit: "800", HardLimit: "1000"},
			PerDay:  &types.BudgetLimit{SoftLimit: "800", HardLimit: "1000"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Day 1 at 23:30:00 UTC: spend right up to both hour AND day
	// hard limit. A further call this day is impossible.
	day1 := time.Date(2024, 1, 15, 23, 30, 0, 0, time.UTC)
	ctx1 := ctx.WithBlockTime(day1)

	first, err := k.EvaluatePolicy(ctx1, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.True(t, first.Allowed, "first call must pass: 900 under hard 1000")

	// Same day, different hour (00:30 hour 0 is still day 2024-01-15
	// if we picked 23:30 → 23:59 is fine but we want to stay in same
	// day first). Actually pick 23:45 — same day, same hour string
	// "2024011523". A further 200 call in the same hour/day:
	sameHourLaterMinute := time.Date(2024, 1, 15, 23, 45, 0, 0, time.UTC)
	ctxSameHour := ctx.WithBlockTime(sameHourLaterMinute)
	denied, err := k.EvaluatePolicy(ctxSameHour, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 200,
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed,
		"same-day call must be denied — per-hour (and per-day) saturated at 900 + 200 > 1000")

	// Advance to next UTC day at 00:30:00. Both hour and day period
	// IDs change (hour: 2024011600, day: 20240116), so both buckets
	// are fresh zero.
	day2 := time.Date(2024, 1, 16, 0, 30, 0, 0, time.UTC)
	ctx2 := ctx.WithBlockTime(day2)

	// A fresh 900-cost call the next day must PASS — both scopes
	// reset.
	nextDay, err := k.EvaluatePolicy(ctx2, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.True(t, nextDay.Allowed,
		"day-flipped call must pass: both per-hour and per-day reset after UTC midnight crossing")

	// Pin the new bucket values directly.
	periods2 := budgetUsagePeriods(ctx2)
	hour2Key := budgetUsageKey(policyID, userID, "per-hour", periods2.hour)
	day2Key := budgetUsageKey(policyID, userID, "per-day", periods2.day)

	hour2Usage, err := k.getBudgetUsage(ctx2, hour2Key)
	require.NoError(t, err)
	assert.Equal(t, uint64(900), hour2Usage,
		"new day's hour bucket holds exactly the post-flip 900; got %d", hour2Usage)

	day2Usage, err := k.getBudgetUsage(ctx2, day2Key)
	require.NoError(t, err)
	assert.Equal(t, uint64(900), day2Usage,
		"new day bucket holds exactly the post-flip 900; got %d — "+
			"if this is 1800, the day flip did NOT reset per-day, which "+
			"means the day format string lost its day component",
		day2Usage)
}

// TestEvaluatePolicy_PeriodKeysAreUTC pins that period IDs are
// derived from ctx.BlockTime().UTC(), not from the block time's
// original location. A regression that dropped the .UTC() call would
// make period boundaries host-timezone-dependent — validators in
// different timezones would disagree on which period a given block
// belongs to, producing consensus divergence. The check here uses
// a non-UTC block time and verifies the derived period IDs are what
// we get from the UTC interpretation of the same instant.
func TestEvaluatePolicy_PeriodKeysAreUTC(t *testing.T) {
	ctx, _ := setupPoliciesKeeper(t)

	// 03:30:00 UTC == 04:30:00 in Europe/Stockholm (CET/+01:00 in
	// January, no DST). If budgetUsagePeriods fails to normalize to
	// UTC, it would produce "04" in the hour string instead of "03".
	stockholm, err := time.LoadLocation("Europe/Stockholm")
	require.NoError(t, err)

	localTime := time.Date(2024, 1, 15, 4, 30, 0, 0, stockholm)
	ctxLocal := ctx.WithBlockTime(localTime)

	gotPeriods := budgetUsagePeriods(ctxLocal)
	assert.Equal(t, "2024011503", gotPeriods.hour,
		"hour period ID must reflect UTC (03), not local (04); got %s — "+
			"loss of .UTC() normalization would cause validator consensus divergence",
		gotPeriods.hour)
	assert.Equal(t, "20240115", gotPeriods.day,
		"day period ID must reflect UTC; got %s", gotPeriods.day)
	assert.Equal(t, "202401", gotPeriods.month,
		"month period ID must reflect UTC; got %s", gotPeriods.month)
}

// TestBudgetUsagePeriods_ISOWeekYearBoundary pins the week-period
// derivation at the canonical "ISO week spans year boundary" edge
// case. Go's time.ISOWeek() follows ISO 8601: weeks belong to the
// year in which the Thursday of the week falls. Consequence: a
// calendar date near year-end can belong to ISO week 01 of the NEXT
// year, and a date in very early January can belong to ISO week 52
// or 53 of the PREVIOUS year.
//
// Concrete fixture: 2024-12-30 is a Monday. The week
// 2024-12-30..2025-01-05 contains a Thursday (2025-01-02) that falls
// in 2025, so the week's ISO label is "2025-W01". If
// budgetUsagePeriods accidentally computed the week label from the
// calendar year (e.g., `fmt.Sprintf("%04d-W%02d", now.Year(), ...)`),
// the late-December dates would wrap to week 01 of the WRONG year,
// colliding with genuine week-01-of-next-year usage.
//
// This test pins the correct formatting by driving budgetUsagePeriods
// directly across a year boundary. A regression would surface as a
// different week string — the assertion names the expected
// "2025-W01" explicitly so the failure is self-describing.
func TestBudgetUsagePeriods_ISOWeekYearBoundary(t *testing.T) {
	ctx, _ := setupPoliciesKeeper(t)

	// Fixture 1: Monday 2024-12-30 12:00 UTC → ISO week is 2025-W01
	// because the Thursday of this week (2025-01-02) is in 2025.
	late2024 := time.Date(2024, 12, 30, 12, 0, 0, 0, time.UTC)
	ctx1 := ctx.WithBlockTime(late2024)
	periods1 := budgetUsagePeriods(ctx1)
	assert.Equal(t, "2025-W01", periods1.week,
		"2024-12-30 belongs to ISO week 2025-W01 (Thursday is 2025-01-02); "+
			"got %s — a regression using calendar-year formatting would "+
			"produce e.g. '2024-W01' here, colliding with genuine January-01 usage",
		periods1.week)
	// Day and month continue to reflect calendar-year values; pin
	// these to catch any collateral regression in the co-derivation.
	assert.Equal(t, "20241230", periods1.day,
		"day field must reflect calendar date regardless of ISO week year")
	assert.Equal(t, "202412", periods1.month,
		"month field must reflect calendar month regardless of ISO week year")

	// Fixture 2: 2025-01-02 12:00 UTC → ISO week is still 2025-W01
	// (same week as 2024-12-30). Same week string means the two
	// calendar-distinct dates share a budget bucket, which is the
	// INTENDED behaviour for ISO-week budgets.
	early2025 := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	ctx2 := ctx.WithBlockTime(early2025)
	periods2 := budgetUsagePeriods(ctx2)
	assert.Equal(t, periods1.week, periods2.week,
		"2024-12-30 and 2025-01-02 must produce identical week labels — "+
			"they are in the same ISO week (2025-W01) and should share a "+
			"per-week budget bucket; got %s vs %s",
		periods1.week, periods2.week)
	assert.Equal(t, "20250102", periods2.day,
		"day field differs across the boundary (this IS a day flip)")
	assert.Equal(t, "202501", periods2.month,
		"month field differs across the boundary (this IS a month flip)")

	// Fixture 3: 2021-01-03 (Sunday) belongs to ISO week 2020-W53
	// (the week-53 case — ISO week labels CAN exceed 52 in
	// long-year cases). Go's time.ISOWeek returns (2020, 53) for
	// this date. A format string that assumed max-52 week number
	// would silently truncate; pinning explicitly catches it.
	yearWith53 := time.Date(2021, 1, 3, 12, 0, 0, 0, time.UTC)
	ctx3 := ctx.WithBlockTime(yearWith53)
	periods3 := budgetUsagePeriods(ctx3)
	assert.Equal(t, "2020-W53", periods3.week,
		"2021-01-03 belongs to ISO week 2020-W53 (long-year case); got %s — "+
			"a regression assuming W01..W52 would mislabel long years",
		periods3.week)
}

// TestEvaluatePolicy_MonthBoundaryResetsPerMonth pins that advancing
// the block time across a UTC month boundary resets the per-month
// counter (the user gets a fresh monthly budget). Complements the
// hour/day boundary tests above: a regression that dropped the month
// component from the month format string would pass both hour and
// day rollover but fail this one.
func TestEvaluatePolicy_MonthBoundaryResetsPerMonth(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	const policyID = "budget-rollover-month"
	const userID = "user-rollover-month"

	policy := &types.PolicyProfile{
		PolicyId: policyID,
		Version:  "1.0.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Month Rollover",
			Owner: "org-owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State: types.PolicyState_POLICY_STATE_ACTIVE,
		},
		Budgets: &types.BudgetControls{
			PerMonth: &types.BudgetLimit{SoftLimit: "800", HardLimit: "1000"},
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Month 1: 2024-01-31 23:00 UTC — spend right to the month
	// hard limit.
	month1 := time.Date(2024, 1, 31, 23, 0, 0, 0, time.UTC)
	ctx1 := ctx.WithBlockTime(month1)
	first, err := k.EvaluatePolicy(ctx1, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.True(t, first.Allowed, "first call must pass: 900 under month hard 1000")

	// Still in January: another 200 call blows the month budget.
	ctxSameMonth := ctx.WithBlockTime(time.Date(2024, 1, 31, 23, 59, 0, 0, time.UTC))
	denied, err := k.EvaluatePolicy(ctxSameMonth, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 200,
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed, "same-month call must be denied by per-month")
	require.Contains(t, denied.DenialReason, "per-month")

	// Month 2: 2024-02-01 00:01 UTC — month period flips to
	// "202402", fresh bucket.
	month2 := time.Date(2024, 2, 1, 0, 1, 0, 0, time.UTC)
	ctx2 := ctx.WithBlockTime(month2)
	nextMonth, err := k.EvaluatePolicy(ctx2, policyID, &InvocationRequest{
		Tool:         ToolContext{ToolID: "tool-1"},
		UserID:       userID,
		CostMicroLAC: 900,
	})
	require.NoError(t, err)
	require.True(t, nextMonth.Allowed,
		"month-flipped call must pass: per-month resets on 2024-02-01 00:01")

	// Inspect the month-2 bucket directly.
	periods2 := budgetUsagePeriods(ctx2)
	month2Key := budgetUsageKey(policyID, userID, "per-month", periods2.month)
	month2Usage, err := k.getBudgetUsage(ctx2, month2Key)
	require.NoError(t, err)
	assert.Equal(t, uint64(900), month2Usage,
		"new month bucket holds only the post-flip 900; got %d — "+
			"if this is 1800, the month flip did not reset the bucket",
		month2Usage)

	// Verify both fixtures produced distinct month IDs (otherwise
	// the rollover test is vacuously passing).
	periods1 := budgetUsagePeriods(ctx1)
	assert.NotEqual(t, periods1.month, periods2.month,
		"fixtures must straddle a month boundary; got %s == %s (vacuous test)",
		periods1.month, periods2.month)
	assert.Equal(t, "202401", periods1.month, "month 1 label pinned")
	assert.Equal(t, "202402", periods2.month, "month 2 label pinned")
}
