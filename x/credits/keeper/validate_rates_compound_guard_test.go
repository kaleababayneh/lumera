//go:build cosmos && cosmos_full

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
)

// This file closes remaining DIRECT-test coverage gaps for
// ValidateRates at x/credits/keeper/math_safe.go:172-205.
// Existing TestValidateRates pins four scenarios (valid, burn
// rate > 10000, insurance rate > 10000, shares !=10000). What
// it does NOT pin:
//
//   1. The COMBINED burn+insurance > 10000 compound guard at
//      :188-194. The in-code comment explicitly documents this
//      as a "negative net amount" prevention invariant.
//   2. Boundary values (burn=10000 exactly accepted; burn+
//      insurance=10000 exactly accepted).
//   3. All-five-share invocation (existing test hardcodes
//      originSurface=0 and treasury=0).
//
// Scan-angle #1 (watchdog comparisons + tiebreak arms) on the
// three boundary guards — each uses strict `>`:
//     burnRate > 10000              (10000 exactly is valid)
//     insuranceRate > 10000         (10000 exactly is valid)
//     combinedDeductions > 10000    (10000 exactly is valid)
//   vs totalShares != 10000        (EQUALITY, not inequality)
// The asymmetry between `>` (burn/insurance bounds) and `!=`
// (shares sum) is intentional — a refactor unifying them
// would either reject the 100%-burn limit OR accept off-sum
// shares.
//
// Scan-angle #6 (security-critical invariants) on the
// combined-deductions guard. In-code comment documents the
// rationale: "This prevents negative net amounts after
// deductions" (:188). Without this, an attacker configuring
// burn=8000 + insurance=5000 (each individually valid) would
// produce a settlement where net = amount - 13000 bps = -3000
// bps of the original — causing every settlement to fail
// arithmetic downstream.

// TestValidateRates_BurnAtMaxAccepted pins the burnRate ==
// 10000 boundary (100% burn allowed as a single value).
func TestValidateRates_BurnAtMaxAccepted(t *testing.T) {
	t.Parallel()
	// Full burn, zero insurance, shares still sum to 10000.
	err := keeper.ValidateRates(
		10000, // burnRate at max
		0,
		6000, 3000, 0, 0, 1000,
	)
	require.NoError(t, err,
		"burnRate == 10000 must be accepted. Pins strict `> 10000` "+
			"guard at :177: a refactor to `>=` would reject the all-"+
			"burn edge case (legitimate policy: 100%% fee rebate into "+
			"burn when publisher opts out).")
}

// TestValidateRates_InsuranceAtMaxAccepted pins the mirror
// case: insuranceRate == 10000 accepted when burn = 0.
func TestValidateRates_InsuranceAtMaxAccepted(t *testing.T) {
	t.Parallel()
	err := keeper.ValidateRates(
		0,
		10000, // insuranceRate at max
		6000, 3000, 0, 0, 1000,
	)
	require.NoError(t, err,
		"insuranceRate == 10000 accepted. Sibling of burn-max pin.")
}

// TestValidateRates_CombinedAtMaxAccepted pins the compound
// boundary: burn + insurance = 10000 EXACTLY. The guard uses
// strict `>` so the exact-sum case is accepted.
func TestValidateRates_CombinedAtMaxAccepted(t *testing.T) {
	t.Parallel()
	// burn 5000 + insurance 5000 = 10000 exactly.
	err := keeper.ValidateRates(
		5000, 5000,
		6000, 3000, 0, 0, 1000,
	)
	require.NoError(t, err,
		"CRITICAL — burn+insurance exactly 10000 accepted. Pins "+
			":188 strict `>` comparison: a refactor to `>=` would "+
			"reject the valid 'all-fee-distributed-to-burn-and-"+
			"insurance-pool' configuration used for pool-only "+
			"settlement periods.")
}

// TestValidateRates_CombinedJustOverMaxRejected is the CRITICAL
// scan-angle #6 ANCHOR. In-code comment at :186-187 explains
// this guard prevents negative net amounts. Individual rates
// are both legal but their sum exceeds 100%.
func TestValidateRates_CombinedJustOverMaxRejected(t *testing.T) {
	t.Parallel()
	// Each valid individually (<= 10000), but sum > 10000.
	err := keeper.ValidateRates(
		5001, 5000, // 5001 + 5000 = 10001, just over
		6000, 3000, 0, 0, 1000,
	)
	require.Error(t, err,
		"CRITICAL — burn+insurance > 10000 rejected. Pins :188-194 "+
			"compound guard: without it, individually-valid rates "+
			"(both <= 10000) could combine to produce negative net "+
			"amounts after deductions, breaking every settlement's "+
			"downstream arithmetic.")
	assert.Contains(t, err.Error(), "combined burn and insurance",
		"error identifies the combined-deductions guard for "+
			"operator triage")
	assert.Contains(t, err.Error(), "10001",
		"error includes the offending sum for diagnostics")
}

// TestValidateRates_CombinedExtremeOverMaxRejected pins a
// more extreme case where both rates are individually high.
func TestValidateRates_CombinedExtremeOverMaxRejected(t *testing.T) {
	t.Parallel()
	err := keeper.ValidateRates(
		8000, 5000, // 8000 + 5000 = 13000 — way over
		6000, 3000, 0, 0, 1000,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "13000",
		"error reports the actual summed BPS value")
}

// TestValidateRates_AllFiveSharesNonZero pins the 5-way share
// distribution with ALL buckets non-zero — complements
// existing test which hardcodes origin+treasury to 0.
func TestValidateRates_AllFiveSharesNonZero(t *testing.T) {
	t.Parallel()
	// Valid rates + 5-way share sum = 10000:
	//   publisher=3000, router=2500, origin=2000, treasury=1500,
	//   referrer=1000 → sum 10000.
	err := keeper.ValidateRates(
		1000, 500, // 10% burn + 5% insurance = 15% deductions
		3000, 2500, 2000, 1500, 1000,
	)
	require.NoError(t, err,
		"5-bucket share split + non-zero burn/insurance accepted "+
			"when sum == 10000")
}

// TestValidateRates_SharesExceedingTenThousandRejected pins
// the `!= 10000` guard rejecting OVERSUM (not just undersum
// which existing test covers).
func TestValidateRates_SharesOverSumRejected(t *testing.T) {
	t.Parallel()
	// Shares sum to 11000 (over by 1000).
	err := keeper.ValidateRates(
		0, 0,
		5000, 3000, 1000, 1000, 1000, // sum = 11000
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shares must sum to 10000")
	assert.Contains(t, err.Error(), "11000",
		"error reports the oversum value for triage")
}

// TestValidateRates_AllZeroSharesRejected pins that the
// degenerate all-zero shares configuration is REJECTED
// (totalShares = 0 != 10000).
func TestValidateRates_AllZeroSharesRejected(t *testing.T) {
	t.Parallel()
	err := keeper.ValidateRates(1000, 500, 0, 0, 0, 0, 0)
	require.Error(t, err,
		"all-zero shares rejected (sum=0 != 10000). Pins against "+
			"a misconfigured deployment where all revenue would be "+
			"lost (no recipient).")
	assert.Contains(t, err.Error(), "shares must sum to 10000")
}

// TestValidateRates_ErrorMessagesExposeFieldNames pins that
// the shares-mismatch error echoes EACH share value for
// operator triage. A refactor that shortened the message
// (e.g., just "shares invalid") would lose diagnostic signal.
func TestValidateRates_ErrorMessagesExposeFieldNames(t *testing.T) {
	t.Parallel()
	err := keeper.ValidateRates(
		0, 0,
		1111, 2222, 3333, 2222, 111, // sum = 8999
	)
	require.Error(t, err)
	msg := err.Error()
	// Every per-field value must appear in the diagnostic.
	for _, expected := range []string{"1111", "2222", "3333", "111"} {
		assert.Contains(t, msg, expected,
			"error message echoes share value %s for operator "+
				"triage. Pins :200-203 format: a refactor shortening "+
				"to just 'shares invalid' would lose locality signal.",
			expected)
	}
	assert.Contains(t, msg, "pub=",
		"error message labels shares by role (pub=, router=, etc.)")
}

// ---- Scan-angle #5 cross-boundary parity ----

// TestValidateRates_IndividualAndCombinedGuardIndependent pins
// the SCAN-ANGLE #1 asymmetry: individual-rate guards use `>
// 10000` while the combined-rate guard uses `> 10000` too, but
// they can trigger independently. Individually-valid rates
// can combine to violate the compound guard and vice versa.
func TestValidateRates_IndividualAndCombinedGuardsTriggerIndependently(t *testing.T) {
	t.Parallel()
	// Case A: burn valid (10000), insurance valid (10000),
	// but combined invalid (20000).
	err := keeper.ValidateRates(
		10000, 10000,
		6000, 3000, 0, 0, 1000,
	)
	require.Error(t, err,
		"both individual rates at max BUT sum exceeds 10000 → "+
			"combined guard fires")
	assert.Contains(t, err.Error(), "combined")

	// Case B: single rate over max (no need to check combined).
	errB := keeper.ValidateRates(
		10001, 0,
		6000, 3000, 0, 0, 1000,
	)
	require.Error(t, errB)
	assert.Contains(t, errB.Error(), "burn rate",
		"individual-rate guard fires before combined guard when "+
			"the former is violated. Pins :176-180 ordering: "+
			"individual checks come FIRST so operators see the "+
			"primary failure before the derived compound message.")
}
