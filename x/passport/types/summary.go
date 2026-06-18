//go:build cosmos

package types

import (
	"fmt"
	"math"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Validate checks basic invariants for passport summary fields.
func (s *PassportSummary) Validate() error {
	if s == nil {
		return nil
	}
	if err := validateSummaryCoin("total_spend", s.TotalSpend); err != nil {
		return err
	}
	if err := validateSummaryCoin("settled_spend", s.SettledSpend); err != nil {
		return err
	}
	if err := validateSummaryCoin("refunded_spend", s.RefundedSpend); err != nil {
		return err
	}
	if err := validateSummaryShare("tool_diversity_index", s.ToolDiversityIndex); err != nil {
		return err
	}
	if err := validateSummaryShare("verified_spend_share", s.VerifiedSpendShare); err != nil {
		return err
	}
	if err := validateSummaryShare("collusion_risk_score", s.CollusionRiskScore); err != nil {
		return err
	}
	if err := validateSummaryReceiptTimestamps(s.FirstReceiptTs, s.LastReceiptTs); err != nil {
		return err
	}
	return nil
}

func validateSummaryCoin(field string, coin *v1beta1.Coin) error {
	if coin == nil {
		return nil
	}
	if coin.Denom == "" {
		return fmt.Errorf("%s denom cannot be empty", field)
	}
	if err := sdk.ValidateDenom(coin.Denom); err != nil {
		return fmt.Errorf("%s denom is invalid: %w", field, err)
	}
	amount, ok := sdkmath.NewIntFromString(coin.Amount)
	if !ok {
		return fmt.Errorf("%s amount must be a valid integer", field)
	}
	if amount.IsNegative() {
		return fmt.Errorf("%s amount cannot be negative", field)
	}
	return nil
}

func validateSummaryReceiptTimestamps(first, last int64) error {
	if first > 0 && last > 0 && first > last {
		return fmt.Errorf("first_receipt_ts must be less than or equal to last_receipt_ts")
	}
	return nil
}

func validateSummaryShare(field string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s must be a finite number", field)
	}
	if value < 0 || value > 1 {
		return fmt.Errorf("%s must be between 0 and 1", field)
	}
	return nil
}
