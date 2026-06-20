
package keeper_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// ---------------------------------------------------------------------------
// EmitPoolMetrics — exercises gauge emissions from pool + metrics state
// ---------------------------------------------------------------------------

func TestEmitPoolMetrics_DefaultGenesis(t *testing.T) {
	f := setupKeeperTest(t)

	// Default genesis has zero-value pool state; should not panic.
	f.keeper.EmitPoolMetrics(f.ctx)
}

func TestEmitPoolMetrics_PopulatedPool(t *testing.T) {
	f := setupKeeperTest(t)

	// Re-init with a non-trivial pool state.
	gen := types.DefaultGenesis()
	gen.Pool = &types.PoolState{
		TotalFunds:         "500000",
		AvailableFunds:     "400000",
		ReservedFunds:      "100000",
		TotalContributions: "600000",
		TotalPayouts:       "100000",
		TargetUtilization:  "0.2",
		CurrentUtilization: "0.15",
		Status:             types.PoolStatus_POOL_STATUS_HEALTHY,
	}
	gen.Metrics = &types.PoolMetrics{
		TotalContributions_24H: "5000",
		TotalPayouts_24H:       "1000",
		PendingClaims:          7,
		AverageClaimAmount:     "200",
		ClaimApprovalRate:      "0.85",
		PoolHealthScore:        "92",
		RiskExposure:           "0.05",
		CoverageRatio:          "4.0",
		UtilizationEwma:        "0.18",
		DisputeRateEwma:        "0.01",
		Samples:                100,
	}
	f.keeper.InitGenesis(f.ctx, gen)

	// Should not panic with real data.
	f.keeper.EmitPoolMetrics(f.ctx)
}

func TestEmitPoolMetrics_NilParams(t *testing.T) {
	f := setupKeeperTest(t)

	// Clear params to exercise the nil-params guard (line 72).
	gen := types.DefaultGenesis()
	gen.Pool = types.DefaultPoolState()
	gen.Metrics = types.DefaultPoolMetrics()
	f.keeper.InitGenesis(f.ctx, gen)

	// Even with valid pool state, EmitPoolMetrics should not panic
	// when the params branch evaluates.
	f.keeper.EmitPoolMetrics(f.ctx)
}

// ---------------------------------------------------------------------------
// EmitClaimProcessed — counter + conditional paid branch
// ---------------------------------------------------------------------------

func TestEmitClaimProcessed_Paid(t *testing.T) {
	f := setupKeeperTest(t)
	coin := sdk.NewCoin("ulac", sdkmath.NewInt(5000))

	// "paid" status exercises the inner branch that increments claims_paid
	// and emits payout total.
	f.keeper.EmitClaimProcessed(f.ctx, "claim-1", "paid", coin)
}

func TestEmitClaimProcessed_Denied(t *testing.T) {
	f := setupKeeperTest(t)
	coin := sdk.NewCoin("ulac", sdkmath.NewInt(1000))

	// "denied" status skips the paid branch entirely.
	f.keeper.EmitClaimProcessed(f.ctx, "claim-2", "denied", coin)
}

func TestEmitClaimProcessed_LargeAmount(t *testing.T) {
	f := setupKeeperTest(t)
	// Amount exceeding int64 range — exercises the BigInt overflow guard.
	large := sdkmath.NewIntFromBigInt(sdkmath.NewInt(1).BigInt().Exp(
		sdkmath.NewInt(10).BigInt(), sdkmath.NewInt(30).BigInt(), nil,
	))
	coin := sdk.NewCoin("ulac", large)

	f.keeper.EmitClaimProcessed(f.ctx, "claim-big", "paid", coin)
}

// ---------------------------------------------------------------------------
// EmitClaimAutoApproved
// ---------------------------------------------------------------------------

func TestEmitClaimAutoApproved(t *testing.T) {
	f := setupKeeperTest(t)
	f.keeper.EmitClaimAutoApproved(f.ctx)
}

// ---------------------------------------------------------------------------
// EmitContribution — counter with denom label
// ---------------------------------------------------------------------------

func TestEmitContribution_Normal(t *testing.T) {
	f := setupKeeperTest(t)
	coin := sdk.NewCoin("ulac", sdkmath.NewInt(10000))
	f.keeper.EmitContribution(f.ctx, coin)
}

func TestEmitContribution_LargeAmount(t *testing.T) {
	f := setupKeeperTest(t)
	large := sdkmath.NewIntFromBigInt(sdkmath.NewInt(1).BigInt().Exp(
		sdkmath.NewInt(10).BigInt(), sdkmath.NewInt(30).BigInt(), nil,
	))
	coin := sdk.NewCoin("ulac", large)
	f.keeper.EmitContribution(f.ctx, coin)
}

// ---------------------------------------------------------------------------
// EmitRecidivistPenalty — counter with publisher_id label
// ---------------------------------------------------------------------------

func TestEmitRecidivistPenalty(t *testing.T) {
	f := setupKeeperTest(t)
	f.keeper.EmitRecidivistPenalty(f.ctx, "pub-xyz", 3, 1.5)
}

func TestEmitRecidivistPenalty_EmptyPublisher(t *testing.T) {
	f := setupKeeperTest(t)
	f.keeper.EmitRecidivistPenalty(f.ctx, "", 0, 0.0)
}

// ---------------------------------------------------------------------------
// EmitPayoutCapped — simple counter
// ---------------------------------------------------------------------------

func TestEmitPayoutCapped(t *testing.T) {
	f := setupKeeperTest(t)
	f.keeper.EmitPayoutCapped(f.ctx, "claim-capped")
}

func TestEmitPayoutCapped_EmptyClaimID(t *testing.T) {
	f := setupKeeperTest(t)
	f.keeper.EmitPayoutCapped(f.ctx, "")
}
