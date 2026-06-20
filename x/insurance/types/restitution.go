package types

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Restitution routing per specs/governance/slashing-rules.md §"Restitution
// Routing". Slashed bond amounts are split four ways. The burn share is
// hard-coded (not a governance parameter) so governance cannot tune it away
// to profit from cascading slashes; the other three shares are governance-
// adjustable within a max-delta-per-epoch guard that lives in module params.
const (
	RestitutionUsersBps     uint32 = 6000 // 60% — instant insurance credit to impacted users
	RestitutionInsuranceBps uint32 = 2500 // 25% — replenish insurance reserve
	RestitutionTreasuryBps  uint32 = 1000 // 10% — governance treasury (audits, bounties)
	RestitutionBurnBps      uint32 = 500  // 5%  — burned, immutable

	RestitutionTotalBps uint32 = 10_000
)

// RestitutionSplit is the per-destination breakdown of a single slashed
// amount. The sum of all four fields equals the input exactly; rounding
// residuals from integer arithmetic are assigned to the Users share so the
// user-protection leg absorbs any dust rather than the burn destination.
type RestitutionSplit struct {
	Users     decimal.Decimal
	Insurance decimal.Decimal
	Treasury  decimal.Decimal
	Burn      decimal.Decimal
}

// Total returns the sum of all four shares. Callers use this as a round-trip
// check against the original slashed amount.
func (s RestitutionSplit) Total() decimal.Decimal {
	return s.Users.Add(s.Insurance).Add(s.Treasury).Add(s.Burn)
}

// ComputeRestitutionSplit divides a slashed amount into the four restitution
// destinations per the spec. Returns an error if the amount is negative.
//
// The split is computed as floor(amount * share_bps / 10_000) for
// Insurance/Treasury/Burn, with the residual (amount - sum-of-floors)
// assigned to Users. This guarantees:
//   - No share is ever rounded up above its nominal percentage (so burn
//     stays at ≤5% even with rounding), preserving the spec's immutability
//     guarantee for the burn leg.
//   - The total always equals the input exactly — no value is silently lost
//     to truncation.
//   - Users receive any dust, matching the spec's user-protection intent.
//
// The amount is expected to be an integer-valued Decimal (whole coin units).
// Non-integer inputs are preserved bit-for-bit in the output; the function
// makes no rounding assumption beyond the integer-bps math.
func ComputeRestitutionSplit(amount decimal.Decimal) (RestitutionSplit, error) {
	if amount.IsNegative() {
		return RestitutionSplit{}, fmt.Errorf("restitution amount cannot be negative: %s", amount.String())
	}
	if amount.IsZero() {
		return RestitutionSplit{
			Users:     decimal.Zero,
			Insurance: decimal.Zero,
			Treasury:  decimal.Zero,
			Burn:      decimal.Zero,
		}, nil
	}

	// Sanity check that the compile-time constants still sum to 100%. A
	// future edit that adjusts one share without rebalancing the others
	// would otherwise produce a split that silently drops or double-counts
	// value.
	if RestitutionUsersBps+RestitutionInsuranceBps+RestitutionTreasuryBps+RestitutionBurnBps != RestitutionTotalBps {
		return RestitutionSplit{}, fmt.Errorf(
			"restitution bps constants do not sum to %d: users=%d insurance=%d treasury=%d burn=%d",
			RestitutionTotalBps,
			RestitutionUsersBps, RestitutionInsuranceBps, RestitutionTreasuryBps, RestitutionBurnBps,
		)
	}

	denom := decimal.NewFromInt(int64(RestitutionTotalBps))

	insurance := amount.Mul(decimal.NewFromInt(int64(RestitutionInsuranceBps))).Div(denom).Floor()
	treasury := amount.Mul(decimal.NewFromInt(int64(RestitutionTreasuryBps))).Div(denom).Floor()
	burn := amount.Mul(decimal.NewFromInt(int64(RestitutionBurnBps))).Div(denom).Floor()

	// Users absorbs the residual so the total is exact.
	users := amount.Sub(insurance).Sub(treasury).Sub(burn)

	return RestitutionSplit{
		Users:     users,
		Insurance: insurance,
		Treasury:  treasury,
		Burn:      burn,
	}, nil
}
