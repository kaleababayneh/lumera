
package keeper

import (
	"fmt"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// RegisterInvariants wires the credits module invariants into the global registry.
//
//nolint:staticcheck // SA1019: legacy invariant registry remains until the SDK offers a replacement.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "active-lock-balance", ActiveLockBalanceInvariant(k))
	ir.RegisterRoute(types.ModuleName, "lock-state", LockStateInvariant(k))
	ir.RegisterRoute(types.ModuleName, "total-supply", TotalSupplyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "settlement-conservation", SettlementConservationInvariant(k))
	ir.RegisterRoute(types.ModuleName, "metrics-consistency", MetricsConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "params-rates", ParamsRatesInvariant(k))
}

// AllInvariants combines all credits module invariants.
//
//nolint:staticcheck // SA1019: uses legacy invariant types until upstream migration is complete.
func AllInvariants(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		res, stop := ActiveLockBalanceInvariant(k)(ctx)
		if stop {
			return res, stop
		}
		res, stop = LockStateInvariant(k)(ctx)
		if stop {
			return res, stop
		}
		res, stop = TotalSupplyInvariant(k)(ctx)
		if stop {
			return res, stop
		}
		res, stop = SettlementConservationInvariant(k)(ctx)
		if stop {
			return res, stop
		}
		res, stop = MetricsConsistencyInvariant(k)(ctx)
		if stop {
			return res, stop
		}
		return ParamsRatesInvariant(k)(ctx)
	}
}

// ActiveLockBalanceInvariant ensures module balances match active lock amounts.
//
//nolint:staticcheck // SA1019: deprecated invariant hooks remain necessary until upstream removal is complete.
func ActiveLockBalanceInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		params := k.GetParams(ctx)
		denom := params.CreditDenom
		if denom == "" {
			denom = types.DefaultCreditDenom
		}

		moduleAddr := k.ModuleAddress()
		if moduleAddr == nil {
			return "credits: module account not configured", true
		}

		expected := sdkmath.ZeroInt()
		var anomalies []string

		err := k.IterateLocks(ctx, func(lock *types.Lock) bool {
			if lock == nil {
				return false
			}
			if lock.Status != types.LockStatus_LOCK_STATUS_ACTIVE {
				return false
			}
			coin := types.CoinFromProto(lock.Amount)
			if coin.Denom != denom {
				anomalies = append(anomalies, fmt.Sprintf("%s(denom=%s)", lock.LockId, coin.Denom))
				return false
			}
			if !coin.IsPositive() {
				anomalies = append(anomalies, fmt.Sprintf("%s(non-positive=%s)", lock.LockId, coin.String()))
				return false
			}
			expected = expected.Add(coin.Amount)
			return false
		})

		if err != nil {
			return fmt.Sprintf("credits: active lock balance iteration failed: %v", err), true
		}

		if len(anomalies) > 0 {
			return fmt.Sprintf("credits: active lock anomalies detected: %s", strings.Join(anomalies, ", ")), true
		}

		balance := k.BankKeeper().GetBalance(ctx, moduleAddr, denom)
		if balance.Amount.LT(expected) {
			return fmt.Sprintf("credits: module balance %s < active lock total %s", balance.String(), sdk.NewCoin(denom, expected)), true
		}

		return "credits: active lock balance invariant ok", false
	}
}

// LockStateInvariant verifies each lock maintains sane state transitions.
//
//nolint:staticcheck // SA1019: deprecated invariant hooks remain necessary until upstream removal is complete.
func LockStateInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		blockTime := ctx.BlockTime()
		var issues []string

		err := k.IterateLocks(ctx, func(lock *types.Lock) bool {
			if lock == nil {
				return false
			}
			coin := types.CoinFromProto(lock.Amount)
			if !coin.IsPositive() {
				issues = append(issues, fmt.Sprintf("lock %s has non-positive amount %s", lock.LockId, coin.String()))
			}

			if lock.Status == types.LockStatus_LOCK_STATUS_ACTIVE {
				if lock.ExpiresAt == nil {
					issues = append(issues, fmt.Sprintf("active lock %s missing expiry", lock.LockId))
				} else if lock.ExpiresAt.AsTime().Before(blockTime) {
					issues = append(issues, fmt.Sprintf("active lock %s expired at %s before block %s", lock.LockId, lock.ExpiresAt.AsTime().Format(time.RFC3339), blockTime.Format(time.RFC3339)))
				}
			} else if lock.Status == types.LockStatus_LOCK_STATUS_UNSPECIFIED {
				issues = append(issues, fmt.Sprintf("lock %s has unspecified status", lock.LockId))
			}

			return false
		})

		if err != nil {
			return fmt.Sprintf("credits: lock state iteration failed: %v", err), true
		}

		if len(issues) > 0 {
			return fmt.Sprintf("credits: lock state invariant violations: %s", strings.Join(issues, "; ")), true
		}

		return "credits: lock state invariant ok", false
	}
}

// TotalSupplyInvariant checks that the total supply of the credit token is consistent.
//
//nolint:staticcheck // SA1019: deprecated invariant hooks remain necessary until upstream removal is complete.
func TotalSupplyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		params := k.GetParams(ctx)
		denom := params.CreditDenom
		if denom == "" {
			denom = types.DefaultCreditDenom
		}

		totalSupply := k.BankKeeper().GetSupply(ctx, denom)

		calculatedSupply := sdkmath.ZeroInt()

		// Sum of all account balances
		k.AccountKeeper().IterateAccounts(ctx, func(acc sdk.AccountI) bool {
			calculatedSupply = calculatedSupply.Add(k.BankKeeper().GetBalance(ctx, acc.GetAddress(), denom).Amount)
			return false
		})

		if !totalSupply.Amount.Equal(calculatedSupply) {
			return fmt.Sprintf("credits: bank supply %s != calculated supply %s", totalSupply.Amount, calculatedSupply), true
		}

		return "credits: total supply invariant ok", false
	}
}

// SettlementConservationInvariant verifies that completed settlements have internally
// consistent amounts: total_cost >= burn_amount + net_amount (accounting for insurance).
// This is a critical economic invariant ensuring no value is created or destroyed
// outside defined flows.
//
//nolint:staticcheck // SA1019: deprecated invariant hooks remain necessary until upstream removal is complete.
func SettlementConservationInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		params := k.GetParams(ctx)
		denom := params.CreditDenom
		if denom == "" {
			denom = types.DefaultCreditDenom
		}

		var violations []string

		err := k.IterateSettlements(ctx, func(settlement *types.SettlementRecord) bool {
			if settlement == nil {
				return false
			}

			// Only check completed settlements (pending settlements may have partial data)
			if settlement.Status != types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
				return false
			}

			// Calculate total cost
			totalCost := sdkmath.ZeroInt()
			for _, coin := range settlement.TotalCost {
				if coin.Denom == denom {
					amt, ok := sdkmath.NewIntFromString(coin.Amount)
					if ok && amt.IsPositive() {
						totalCost = totalCost.Add(amt)
					}
				}
			}

			// Calculate burn amount
			burnAmount := sdkmath.ZeroInt()
			for _, coin := range settlement.BurnAmount {
				if coin.Denom == denom {
					amt, ok := sdkmath.NewIntFromString(coin.Amount)
					if ok && amt.IsPositive() {
						burnAmount = burnAmount.Add(amt)
					}
				}
			}

			// Calculate net amount (distributed after burn and insurance)
			netAmount := sdkmath.ZeroInt()
			for _, coin := range settlement.NetAmount {
				if coin.Denom == denom {
					amt, ok := sdkmath.NewIntFromString(coin.Amount)
					if ok && amt.IsPositive() {
						netAmount = netAmount.Add(amt)
					}
				}
			}

			// Invariant 1: burn + net should not exceed total (allows for insurance deduction)
			outflow := burnAmount.Add(netAmount)
			if outflow.GT(totalCost) {
				violations = append(violations, fmt.Sprintf(
					"settlement %s: outflow %s exceeds total_cost %s",
					settlement.Id, outflow.String(), totalCost.String(),
				))
			}

			// Invariant 2: non-negative amounts
			if burnAmount.IsNegative() {
				violations = append(violations, fmt.Sprintf(
					"settlement %s: negative burn_amount %s",
					settlement.Id, burnAmount.String(),
				))
			}
			if netAmount.IsNegative() {
				violations = append(violations, fmt.Sprintf(
					"settlement %s: negative net_amount %s",
					settlement.Id, netAmount.String(),
				))
			}

			// Invariant 3: total cost must be positive for completed settlements
			if !totalCost.IsPositive() {
				violations = append(violations, fmt.Sprintf(
					"settlement %s: completed settlement has non-positive total_cost %s",
					settlement.Id, totalCost.String(),
				))
			}

			return false
		})

		if err != nil {
			return fmt.Sprintf("credits: settlement conservation iteration failed: %v", err), true
		}

		if len(violations) > 0 {
			return fmt.Sprintf("credits: settlement conservation violations: %s", strings.Join(violations, "; ")), true
		}

		return "credits: settlement conservation invariant ok", false
	}
}

// MetricsConsistencyInvariant verifies that the aggregated settlement metrics
// (TotalBurned, TotalDistributed) are consistent with the sum of completed settlements.
//
//nolint:staticcheck // SA1019: deprecated invariant hooks remain necessary until upstream removal is complete.
func MetricsConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		params := k.GetParams(ctx)
		denom := params.CreditDenom
		if denom == "" {
			denom = types.DefaultCreditDenom
		}

		// Get stored metrics
		metrics := k.GetMetrics(ctx)
		if metrics == nil {
			return "credits: metrics consistency invariant ok (nil metrics)", false
		}

		// Parse stored totals
		storedBurned := sdkmath.ZeroInt()
		if metrics.TotalBurned != "" {
			parsed, ok := sdkmath.NewIntFromString(metrics.TotalBurned)
			if ok {
				storedBurned = parsed
			}
		}
		storedDistributed := sdkmath.ZeroInt()
		if metrics.TotalDistributed != "" {
			parsed, ok := sdkmath.NewIntFromString(metrics.TotalDistributed)
			if ok {
				storedDistributed = parsed
			}
		}

		// Calculate from settlements
		calculatedBurned := sdkmath.ZeroInt()
		calculatedDistributed := sdkmath.ZeroInt()
		settlementCount := uint64(0)

		err := k.IterateSettlements(ctx, func(settlement *types.SettlementRecord) bool {
			if settlement == nil {
				return false
			}
			if settlement.Status != types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
				return false
			}

			settlementCount++

			for _, coin := range settlement.BurnAmount {
				if coin.Denom == denom {
					amt, ok := sdkmath.NewIntFromString(coin.Amount)
					if ok && amt.IsPositive() {
						calculatedBurned = calculatedBurned.Add(amt)
					}
				}
			}

			for _, coin := range settlement.NetAmount {
				if coin.Denom == denom {
					amt, ok := sdkmath.NewIntFromString(coin.Amount)
					if ok && amt.IsPositive() {
						calculatedDistributed = calculatedDistributed.Add(amt)
					}
				}
			}

			return false
		})

		if err != nil {
			return fmt.Sprintf("credits: metrics consistency iteration failed: %v", err), true
		}

		var issues []string

		// Check burned totals match
		if !storedBurned.Equal(calculatedBurned) {
			issues = append(issues, fmt.Sprintf(
				"TotalBurned mismatch: stored=%s calculated=%s",
				storedBurned.String(), calculatedBurned.String(),
			))
		}

		// Check distributed totals match
		if !storedDistributed.Equal(calculatedDistributed) {
			issues = append(issues, fmt.Sprintf(
				"TotalDistributed mismatch: stored=%s calculated=%s",
				storedDistributed.String(), calculatedDistributed.String(),
			))
		}

		// Check processed count
		if metrics.TotalProcessed != settlementCount {
			issues = append(issues, fmt.Sprintf(
				"TotalProcessed mismatch: stored=%d counted=%d",
				metrics.TotalProcessed, settlementCount,
			))
		}

		if len(issues) > 0 {
			return fmt.Sprintf("credits: metrics consistency violations: %s", strings.Join(issues, "; ")), true
		}

		return "credits: metrics consistency invariant ok", false
	}
}

// ParamsRatesInvariant verifies that module parameters contain economically valid rates.
// This ensures that burn rate + insurance rate don't exceed 100% (which would make
// settlements impossible) and that individual rates are within bounds.
//
//nolint:staticcheck // SA1019: deprecated invariant hooks remain necessary until upstream removal is complete.
func ParamsRatesInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		params := k.GetParams(ctx)

		var issues []string

		// Invariant 1: burn_rate_spend_bps must be <= 10000
		if params.BurnRateSpendBps > 10000 {
			issues = append(issues, fmt.Sprintf(
				"burn_rate_spend_bps %d exceeds maximum 10000",
				params.BurnRateSpendBps,
			))
		}

		// Invariant 2: insurance_bps must be <= 10000
		if params.InsuranceBps > 10000 {
			issues = append(issues, fmt.Sprintf(
				"insurance_bps %d exceeds maximum 10000",
				params.InsuranceBps,
			))
		}

		// Invariant 3: combined burn + insurance must be <= 10000
		// Otherwise net amount would be negative after deductions
		combinedDeductions := params.BurnRateSpendBps + params.InsuranceBps
		if combinedDeductions > 10000 {
			issues = append(issues, fmt.Sprintf(
				"combined burn_rate_spend_bps + insurance_bps = %d + %d = %d exceeds 10000",
				params.BurnRateSpendBps, params.InsuranceBps, combinedDeductions,
			))
		}

		// Invariant 4: burn_rate_acq_bps must be <= 10000
		if params.BurnRateAcqBps > 10000 {
			issues = append(issues, fmt.Sprintf(
				"burn_rate_acq_bps %d exceeds maximum 10000",
				params.BurnRateAcqBps,
			))
		}

		if params.MinBurnRateSpendBps > params.MaxBurnRateSpendBps {
			issues = append(issues, fmt.Sprintf(
				"min_burn_rate_spend_bps %d exceeds max_burn_rate_spend_bps %d",
				params.MinBurnRateSpendBps, params.MaxBurnRateSpendBps,
			))
		}

		if params.BurnRateSpendBps < params.MinBurnRateSpendBps || params.BurnRateSpendBps > params.MaxBurnRateSpendBps {
			issues = append(issues, fmt.Sprintf(
				"burn_rate_spend_bps %d outside adaptive bounds [%d,%d]",
				params.BurnRateSpendBps, params.MinBurnRateSpendBps, params.MaxBurnRateSpendBps,
			))
		}

		if params.TargetAnnualDeflationBps > 10000 {
			issues = append(issues, fmt.Sprintf(
				"target_annual_deflation_bps %d exceeds maximum 10000",
				params.TargetAnnualDeflationBps,
			))
		}

		if params.DeathSpiralSupplyContractionBps > 10000 {
			issues = append(issues, fmt.Sprintf(
				"death_spiral_supply_contraction_bps %d exceeds maximum 10000",
				params.DeathSpiralSupplyContractionBps,
			))
		}

		if params.DeathSpiralBurnRateCapBps < params.MinBurnRateSpendBps || params.DeathSpiralBurnRateCapBps > params.MaxBurnRateSpendBps {
			issues = append(issues, fmt.Sprintf(
				"death_spiral_burn_rate_cap_bps %d outside adaptive bounds [%d,%d]",
				params.DeathSpiralBurnRateCapBps, params.MinBurnRateSpendBps, params.MaxBurnRateSpendBps,
			))
		}

		if len(issues) > 0 {
			return fmt.Sprintf("credits: params rates violations: %s", strings.Join(issues, "; ")), true
		}

		return "credits: params rates invariant ok", false
	}
}
