// Package types defines data structures for the reserve module.
package types

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxReserveIdentifierLen caps reserve identifiers that are persisted as collection keys
// or index components.
const MaxReserveIdentifierLen = 256

// ReserveCommitment tracks a prepaid capacity block for a policy.
type ReserveCommitment struct {
	ID              string    `json:"id"`
	Owner           string    `json:"owner"`
	PolicyID        string    `json:"policy_id"`
	ToolID          string    `json:"tool_id"`
	Tier            string    `json:"tier"`
	TotalAmount     sdk.Coin  `json:"total_amount"`
	RemainingAmount sdk.Coin  `json:"remaining_amount"`
	DiscountBps     uint32    `json:"discount_bps"`
	StartTime       time.Time `json:"start_time"`
	ExpireTime      time.Time `json:"expire_time"`
	RolloverAllowed bool      `json:"rollover_allowed"`
}

func isCommitmentIdentifierSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func validateCommitmentIdentifier(field, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s required", field)
		}
		return nil
	}
	if isCommitmentIdentifierSpace(value[0]) || isCommitmentIdentifierSpace(value[len(value)-1]) {
		for i := 0; i < len(value); i++ {
			if !isCommitmentIdentifierSpace(value[i]) {
				return fmt.Errorf("%s cannot contain leading or trailing whitespace", field)
			}
		}
		if required {
			return fmt.Errorf("%s required", field)
		}
		return fmt.Errorf("%s cannot contain only whitespace", field)
	}
	if len(value) > MaxReserveIdentifierLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", field, MaxReserveIdentifierLen, len(value))
	}
	return nil
}

// ValidateBasic ensures commitment consistency.
func (c ReserveCommitment) ValidateBasic() error {
	if err := validateCommitmentIdentifier("commitment id", c.ID, true); err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(c.Owner); err != nil {
		return fmt.Errorf("invalid owner address: %w", err)
	}
	if err := validateCommitmentIdentifier("policy id", c.PolicyID, true); err != nil {
		return err
	}
	// Empty tool ID is permitted; it denotes a wildcard commitment usable across tools.
	if err := validateCommitmentIdentifier("tool id", c.ToolID, false); err != nil {
		return err
	}
	if err := validateCommitmentIdentifier("tier", c.Tier, true); err != nil {
		return err
	}
	if !c.TotalAmount.IsValid() || !c.TotalAmount.Amount.IsPositive() {
		return fmt.Errorf("total amount invalid")
	}
	if c.RemainingAmount.Amount.IsNil() {
		return fmt.Errorf("remaining amount invalid")
	}
	if c.RemainingAmount.Denom != c.TotalAmount.Denom {
		return fmt.Errorf("remaining amount denom %s does not match total amount denom %s", c.RemainingAmount.Denom, c.TotalAmount.Denom)
	}
	if c.RemainingAmount.Amount.IsNegative() || c.RemainingAmount.Amount.GT(c.TotalAmount.Amount) {
		return fmt.Errorf("remaining amount out of bounds")
	}
	if c.DiscountBps > 10_000 {
		return fmt.Errorf("discount exceeds 100%%")
	}
	if c.StartTime.IsZero() {
		return fmt.Errorf("start time required")
	}
	if !c.ExpireTime.After(c.StartTime) {
		return fmt.Errorf("expiry must be after start")
	}
	return nil
}

// ReserveRequest describes input for creating a commitment.
type ReserveRequest struct {
	Owner    string
	PolicyID string
	ToolID   string
	Tier     string
	Amount   sdk.Coin
	Duration time.Duration
}

// ReserveAllocation captures the result of allocating reserve credit.
type ReserveAllocation struct {
	Applied         bool
	CommitmentID    string
	DiscountedPrice sdk.Coin
}

// ApplyDiscount returns amount after applying basis-point discount.
func ApplyDiscount(amount sdk.Coin, discountBps uint32) sdk.Coin {
	if discountBps == 0 {
		return amount
	}
	if discountBps > 10_000 {
		discountBps = 10_000
	}
	total := amount.Amount.MulRaw(int64(10_000 - discountBps)).QuoRaw(10_000)
	return sdk.NewCoin(amount.Denom, total)
}

// RemainingRatio returns Remaining/Total as BPS.
func (c ReserveCommitment) RemainingRatio() uint32 {
	if c.TotalAmount.Amount.IsZero() {
		return 0
	}
	bps := c.RemainingAmount.Amount.MulRaw(10_000).Quo(c.TotalAmount.Amount).Uint64()
	if bps > 10_000 {
		bps = 10_000
	}
	return uint32(bps) //#nosec G115 -- clamped to 10000
}
