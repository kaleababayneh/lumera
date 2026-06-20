
package keeper

import (
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gometrics "github.com/hashicorp/go-metrics"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// Telemetry labels used for insurance module metrics
const (
	labelInsurancePoolBalance        = "insurance_pool_balance"
	labelInsuranceUtilization        = "insurance_utilization"
	labelInsuranceClaimRate          = "insurance_claim_rate"
	labelInsurancePremiumBPS         = "insurance_premium_bps"
	labelInsurancePendingClaims      = "insurance_pending_claims"
	labelInsuranceCoverageRatio      = "insurance_coverage_ratio"
	labelInsuranceClaimsPaid         = "insurance_claims_paid"
	labelInsuranceClaimsAutoApproved = "insurance_claims_auto_approved"
)

// EmitPoolMetrics emits Prometheus-compatible metrics for the insurance pool.
// This should be called periodically (e.g., in EndBlocker) to update gauges.
func (k Keeper) EmitPoolMetrics(ctx sdk.Context) {
	poolState, err := k.state.PoolBalance.Get(ctx)
	if err != nil {
		return // No pool state yet
	}

	poolMetrics, metricsErr := k.state.PoolMetrics.Get(ctx)
	if metricsErr != nil {
		poolMetrics = types.DefaultPoolMetrics()
	}

	params := k.GetParams(ctx)

	// Parse pool balance and emit gauge
	if totalFunds, ok := telemetryDecimalFloat32(poolState.TotalFunds); ok {
		telemetry.SetGaugeWithLabels(
			[]string{types.ModuleName, labelInsurancePoolBalance},
			totalFunds,
			[]gometrics.Label{
				telemetry.NewLabel("denom", "ulac"),
			},
		)
	}

	// Parse and emit utilization
	if currentUtil, ok := telemetryDecimalFloat32(poolState.CurrentUtilization); ok {
		telemetry.SetGaugeWithLabels(
			[]string{types.ModuleName, labelInsuranceUtilization},
			currentUtil,
			[]gometrics.Label{
				telemetry.NewLabel("target", poolState.TargetUtilization),
			},
		)
	}

	// Emit claim approval rate
	if approvalRate, ok := telemetryDecimalFloat32(poolMetrics.ClaimApprovalRate); ok {
		telemetry.SetGauge(
			approvalRate,
			types.ModuleName, labelInsuranceClaimRate,
		)
	}

	// Emit premium BPS from params
	if params != nil {
		telemetry.SetGauge(
			float32(params.InsurancePoolBps),
			types.ModuleName, labelInsurancePremiumBPS,
		)
	}

	// Emit pending claims count
	telemetry.SetGauge(
		float32(poolMetrics.PendingClaims),
		types.ModuleName, labelInsurancePendingClaims,
	)

	// Emit coverage ratio
	if coverageRatio, ok := telemetryDecimalFloat32(poolMetrics.CoverageRatio); ok {
		telemetry.SetGauge(
			coverageRatio,
			types.ModuleName, labelInsuranceCoverageRatio,
		)
	}
}

func telemetryDecimalFloat32(raw string) (float32, bool) {
	value, err := decimal.NewFromString(raw)
	if err != nil || !moneyguard.IsSafeExponent(value) {
		return 0, false
	}
	return float32(value.InexactFloat64()), true
}

// EmitClaimProcessed emits a counter metric when a claim is processed
func (k Keeper) EmitClaimProcessed(_ sdk.Context, _ string, status string, amount sdk.Coin) {
	telemetry.IncrCounterWithLabels(
		[]string{types.ModuleName, "claims_processed"},
		1,
		[]gometrics.Label{
			telemetry.NewLabel("status", status),
		},
	)

	if status == "paid" {
		telemetry.IncrCounter(
			1,
			types.ModuleName, labelInsuranceClaimsPaid,
		)

		// Emit payout amount (use BigInt to avoid int64 overflow for large amounts)
		amountDec := decimal.NewFromBigInt(amount.Amount.BigInt(), 0)
		amountFloat := float32(amountDec.InexactFloat64())
		telemetry.IncrCounterWithLabels(
			[]string{types.ModuleName, "claims_payout_total"},
			amountFloat,
			[]gometrics.Label{
				telemetry.NewLabel("denom", amount.Denom),
			},
		)
	}
}

// EmitClaimAutoApproved emits a counter when a claim is auto-approved by EndBlocker
func (k Keeper) EmitClaimAutoApproved(_ sdk.Context) {
	telemetry.IncrCounter(
		1,
		types.ModuleName, labelInsuranceClaimsAutoApproved,
	)
}

// EmitContribution emits a counter when a contribution is made to the pool
func (k Keeper) EmitContribution(_ sdk.Context, amount sdk.Coin) {
	// Use BigInt to avoid int64 overflow for large amounts
	amountDec := decimal.NewFromBigInt(amount.Amount.BigInt(), 0)
	amountFloat := float32(amountDec.InexactFloat64())
	telemetry.IncrCounterWithLabels(
		[]string{types.ModuleName, "contributions_total"},
		amountFloat,
		[]gometrics.Label{
			telemetry.NewLabel("denom", amount.Denom),
		},
	)
}

// EmitRecidivistPenalty emits a counter when a recidivist publisher penalty is applied
func (k Keeper) EmitRecidivistPenalty(_ sdk.Context, publisherID string, _ uint32, _ float64) {
	telemetry.IncrCounterWithLabels(
		[]string{types.ModuleName, "recidivist_penalties"},
		1,
		[]gometrics.Label{
			telemetry.NewLabel("publisher_id", publisherID),
		},
	)
}

// EmitPayoutCapped emits a counter when a payout is capped due to MaxClaimPercent
func (k Keeper) EmitPayoutCapped(_ sdk.Context, _ string) {
	telemetry.IncrCounter(
		1,
		types.ModuleName, "payouts_capped",
	)
}
