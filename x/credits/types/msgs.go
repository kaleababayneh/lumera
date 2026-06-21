package types

import (
	"fmt"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// TypeMsgSwapLUMEtoLAC identifies the swap request from LUME to LAC credits.
	TypeMsgSwapLUMEtoLAC = "swap_lume_to_lac"
	// TypeMsgSwapLACtoLUME identifies the swap request from LAC to LUME credits.
	TypeMsgSwapLACtoLUME = "swap_lac_to_lume"
	// TypeMsgLockCredits marks the message used to reserve credits prior to invocation.
	TypeMsgLockCredits = "lock_credits"
	// TypeMsgUnlockCredits marks the message used to release a previously reserved lock.
	TypeMsgUnlockCredits = "unlock_credits"
	// TypeMsgSettleCredits identifies the settlement request after invocation completes.
	TypeMsgSettleCredits = "settle_credits"
	// TypeMsgSettleOverdraft identifies a bonded overdraft settlement batch.
	TypeMsgSettleOverdraft = "settle_overdraft"
	// TypeMsgUpdateParams identifies the governance message for updating credit params.
	TypeMsgUpdateParams = "update_params"
)

func parseAccAddress(addr string) (sdk.AccAddress, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, fmt.Errorf("address cannot be empty")
	}
	return sdk.AccAddressFromBech32(addr)
}

func validateRequiredID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func validateOptionalID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

// ParseSwapMinOut validates an optional raw minimum-output amount.
func ParseSwapMinOut(field string, value string) (sdkmath.Int, bool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return sdkmath.Int{}, false, nil
	}
	amount, ok := sdkmath.NewIntFromString(trimmed)
	if !ok || !amount.IsPositive() {
		return sdkmath.Int{}, false, fmt.Errorf("%s must be a positive integer", field)
	}
	return amount, true, nil
}

func mustAddr(addr string) sdk.AccAddress {
	a, err := parseAccAddress(addr)
	if err != nil {
		panic(err)
	}
	return a
}

func coinFromProto(c sdk.Coin, field string) (sdk.Coin, error) {
	denom := strings.TrimSpace(c.Denom)
	if denom == "" {
		return sdk.Coin{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s denom cannot be empty", field)
	}
	// ValidateBasic runs this helper, so any panic here would propagate
	// through the message-validation pipeline. sdk.NewCoin panics on
	// denom that's non-empty but fails sdk.ValidateDenom's format
	// rules (e.g., "UPPER", "1bad"), so check format explicitly.
	if err := sdk.ValidateDenom(denom); err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s denom is invalid: %s", field, err)
	}
	if c.Amount.IsNil() || !c.Amount.IsPositive() {
		return sdk.Coin{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s amount must be positive", field)
	}
	return sdk.NewCoin(denom, c.Amount), nil
}

func nonNegativeCoinFromProto(c sdk.Coin, field string, required bool) (sdk.Coin, bool, error) {
	// A coin field left unset decodes to a zero-value sdk.Coin (empty denom,
	// nil amount); treat that as "absent".
	if c.Denom == "" && c.Amount.IsNil() {
		if required {
			return sdk.Coin{}, false, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s is required", field)
		}
		return sdk.Coin{}, false, nil
	}
	denom := strings.TrimSpace(c.Denom)
	if denom == "" {
		return sdk.Coin{}, false, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s denom cannot be empty", field)
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return sdk.Coin{}, false, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s denom is invalid: %s", field, err)
	}
	if c.Amount.IsNil() || c.Amount.IsNegative() {
		return sdk.Coin{}, false, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s amount must be non-negative", field)
	}
	return sdk.NewCoin(denom, c.Amount), true, nil
}

func validateCoinDenom(field string, coin sdk.Coin, denom string) error {
	if coin.Denom != denom {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s denom %s does not match credit_limit denom %s", field, coin.Denom, denom)
	}
	return nil
}

// Route implements sdk.Msg and returns the router key for MsgSwapLUMEtoLAC.
func (m *MsgSwapLUMEtoLAC) Route() string { return RouterKey }

// Type returns the message type for MsgSwapLUMEtoLAC.
func (m *MsgSwapLUMEtoLAC) Type() string { return TypeMsgSwapLUMEtoLAC }

// ValidateBasic performs stateless validation on MsgSwapLUMEtoLAC fields.
func (m *MsgSwapLUMEtoLAC) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetSender()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address: %s", err)
	}
	if _, err := coinFromProto(m.GetLumeAmount(), "lume_amount"); err != nil {
		return err
	}
	if _, _, err := ParseSwapMinOut("min_lac_out", m.GetMinLacOut()); err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	return nil
}

// GetSigners returns the bech32 sender for MsgSwapLUMEtoLAC.
func (m *MsgSwapLUMEtoLAC) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetSender())}
}

// Route implements sdk.Msg and returns the router key for MsgSwapLACtoLUME.
func (m *MsgSwapLACtoLUME) Route() string { return RouterKey }

// Type returns the message type for MsgSwapLACtoLUME.
func (m *MsgSwapLACtoLUME) Type() string { return TypeMsgSwapLACtoLUME }

// ValidateBasic performs stateless validation on MsgSwapLACtoLUME fields.
func (m *MsgSwapLACtoLUME) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetSender()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address: %s", err)
	}
	if _, err := coinFromProto(m.GetLacAmount(), "lac_amount"); err != nil {
		return err
	}
	if _, _, err := ParseSwapMinOut("min_lume_out", m.GetMinLumeOut()); err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	return nil
}

// GetSigners returns the sender address for MsgSwapLACtoLUME.
func (m *MsgSwapLACtoLUME) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetSender())}
}

// Route implements sdk.Msg and returns the router key for MsgLockCredits.
func (m *MsgLockCredits) Route() string { return RouterKey }

// Type returns the message type for MsgLockCredits.
func (m *MsgLockCredits) Type() string { return TypeMsgLockCredits }

// ValidateBasic performs stateless validation on MsgLockCredits fields.
func (m *MsgLockCredits) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetRouter()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid router address: %s", err)
	}
	if err := validateRequiredID("session_id", m.GetSessionId()); err != nil {
		return err
	}
	if err := validateRequiredID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if err := validateOptionalID("toolpack_id", m.GetToolpackId()); err != nil {
		return err
	}
	if _, err := coinFromProto(m.GetAmount(), "amount"); err != nil {
		return err
	}
	return nil
}

// GetSigners returns the router authority for MsgLockCredits.
func (m *MsgLockCredits) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetRouter())}
}

// Route implements sdk.Msg and returns the router key for MsgUnlockCredits.
func (m *MsgUnlockCredits) Route() string { return RouterKey }

// Type returns the message type for MsgUnlockCredits.
func (m *MsgUnlockCredits) Type() string { return TypeMsgUnlockCredits }

// ValidateBasic performs stateless validation on MsgUnlockCredits fields.
func (m *MsgUnlockCredits) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetRouter()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid router address: %s", err)
	}
	if err := validateRequiredID("lock_id", m.GetLockId()); err != nil {
		return err
	}
	return nil
}

// GetSigners returns the router authority for MsgUnlockCredits.
func (m *MsgUnlockCredits) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetRouter())}
}

// Route implements sdk.Msg and returns the router key for MsgSettleCredits.
func (m *MsgSettleCredits) Route() string { return RouterKey }

// Type returns the message type for MsgSettleCredits.
func (m *MsgSettleCredits) Type() string { return TypeMsgSettleCredits }

// ValidateBasic performs stateless validation on MsgSettleCredits fields.
func (m *MsgSettleCredits) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetRouter()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid router address: %s", err)
	}
	if err := validateRequiredID("lock_id", m.GetLockId()); err != nil {
		return err
	}
	if err := validateRequiredID("receipt_id", m.GetReceiptId()); err != nil {
		return err
	}
	if err := validateRequiredID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if err := validateOptionalID("toolpack_id", m.GetToolpackId()); err != nil {
		return err
	}
	if _, err := parseAccAddress(m.GetPublisher()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid publisher address: %s", err)
	}
	if strings.TrimSpace(m.GetReferrer()) != "" {
		if _, err := parseAccAddress(m.GetReferrer()); err != nil {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid referrer address: %s", err)
		}
	}

	if _, _, err := nonNegativeCoinFromProto(m.GetActualCost(), "actual_cost", true); err != nil {
		return err
	}

	if m.GetCacheHit() {
		if err := validateRequiredID("origin_tool_id", m.GetOriginToolId()); err != nil {
			return fmt.Errorf("%w when cache_hit is true", err)
		}
		if m.GetOriginToolId() == m.GetToolId() {
			return fmt.Errorf("origin_tool_id must differ from tool_id when cache_hit is true")
		}
	}
	return nil
}

// GetSigners returns the router authority for MsgSettleCredits.
func (m *MsgSettleCredits) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetRouter())}
}

// Route implements sdk.Msg and returns the router key for MsgSettleOverdraft.
func (m *MsgSettleOverdraft) Route() string { return RouterKey }

// Type returns the message type for MsgSettleOverdraft.
func (m *MsgSettleOverdraft) Type() string { return TypeMsgSettleOverdraft }

// ValidateBasic performs stateless validation on MsgSettleOverdraft fields.
func (m *MsgSettleOverdraft) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetRouter()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid router address: %s", err)
	}
	if err := validateRequiredID("credit_line_id", m.GetCreditLineId()); err != nil {
		return err
	}
	if err := validateRequiredID("settlement_batch_id", m.GetSettlementBatchId()); err != nil {
		return err
	}
	if err := validateRequiredID("policy_version", m.GetPolicyVersion()); err != nil {
		return err
	}
	creditLimit, err := coinFromProto(m.GetCreditLimit(), "credit_limit")
	if err != nil {
		return err
	}
	if m.GetLiquidationThresholdBps() == 0 || m.GetLiquidationThresholdBps() > MaxBasisPoints {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "liquidation_threshold_bps must be between 1 and %d", MaxBasisPoints)
	}
	if len(m.GetEntries()) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "entries are required")
	}

	requestIDs := make(map[string]struct{}, len(m.GetEntries()))
	lockIDs := make(map[string]struct{}, len(m.GetEntries()))
	for i, entry := range m.GetEntries() {
		if entry == nil {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "entries[%d] is required", i)
		}
		if err := validateRequiredID(fmt.Sprintf("entries[%d].request_id", i), entry.GetRequestId()); err != nil {
			return err
		}
		if err := validateRequiredID(fmt.Sprintf("entries[%d].quote_id", i), entry.GetQuoteId()); err != nil {
			return err
		}
		if err := validateRequiredID(fmt.Sprintf("entries[%d].provisional_lock_id", i), entry.GetProvisionalLockId()); err != nil {
			return err
		}
		if err := validateRequiredID(fmt.Sprintf("entries[%d].receipt_id", i), entry.GetReceiptId()); err != nil {
			return err
		}
		if err := validateRequiredID(fmt.Sprintf("entries[%d].tool_id", i), entry.GetToolId()); err != nil {
			return err
		}
		if err := validateOptionalID(fmt.Sprintf("entries[%d].toolpack_id", i), entry.GetToolpackId()); err != nil {
			return err
		}
		if err := validateOptionalID(fmt.Sprintf("entries[%d].stage", i), entry.GetStage()); err != nil {
			return err
		}
		if _, exists := requestIDs[entry.GetRequestId()]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "duplicate request_id %s", entry.GetRequestId())
		}
		if _, exists := lockIDs[entry.GetProvisionalLockId()]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "duplicate provisional_lock_id %s", entry.GetProvisionalLockId())
		}
		requestIDs[entry.GetRequestId()] = struct{}{}
		lockIDs[entry.GetProvisionalLockId()] = struct{}{}

		quotedCost, err := coinFromProto(entry.GetQuotedCost(), fmt.Sprintf("entries[%d].quoted_cost", i))
		if err != nil {
			return err
		}
		if err := validateCoinDenom(fmt.Sprintf("entries[%d].quoted_cost", i), quotedCost, creditLimit.Denom); err != nil {
			return err
		}
		actualCost, err := coinFromProto(entry.GetActualCost(), fmt.Sprintf("entries[%d].actual_cost", i))
		if err != nil {
			return err
		}
		if err := validateCoinDenom(fmt.Sprintf("entries[%d].actual_cost", i), actualCost, creditLimit.Denom); err != nil {
			return err
		}
		for _, tc := range []struct {
			field string
			coin  sdk.Coin
		}{
			{field: "refund_amount", coin: entry.GetRefundAmount()},
			{field: "insurance_amount", coin: entry.GetInsuranceAmount()},
			{field: "burn_amount", coin: entry.GetBurnAmount()},
		} {
			coin, present, err := nonNegativeCoinFromProto(tc.coin, fmt.Sprintf("entries[%d].%s", i, tc.field), false)
			if err != nil {
				return err
			}
			if present {
				if err := validateCoinDenom(fmt.Sprintf("entries[%d].%s", i, tc.field), coin, creditLimit.Denom); err != nil {
					return err
				}
			}
		}
		for j, split := range entry.GetSplits() {
			if split == nil {
				return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "entries[%d].splits[%d] is required", i, j)
			}
			if err := validateRequiredID(fmt.Sprintf("entries[%d].splits[%d].role", i, j), split.GetRole()); err != nil {
				return err
			}
			if _, err := parseAccAddress(split.GetAddress()); err != nil {
				return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid entries[%d].splits[%d].address: %s", i, j, err)
			}
			amount, err := coinFromProto(split.GetAmount(), fmt.Sprintf("entries[%d].splits[%d].amount", i, j))
			if err != nil {
				return err
			}
			if err := validateCoinDenom(fmt.Sprintf("entries[%d].splits[%d].amount", i, j), amount, creditLimit.Denom); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetSigners returns the router authority for MsgSettleOverdraft.
func (m *MsgSettleOverdraft) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetRouter())}
}

// Route implements sdk.Msg and returns the router key for MsgUpdateParams.
func (m *MsgUpdateParams) Route() string { return RouterKey }

// Type returns the message type for MsgUpdateParams.
func (m *MsgUpdateParams) Type() string { return TypeMsgUpdateParams }

// ValidateBasic performs stateless validation on MsgUpdateParams fields.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetAuthority()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address: %s", err)
	}
	if m.GetDisableOverdraft() && (m.GetOverdraftMaxCreditLineToBondBps() > 0 || m.GetOverdraftLiquidationThresholdBps() > 0) {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "disable_overdraft cannot be combined with positive overdraft parameters")
	}
	if m.GetDisableBurnRateAdjustment() && m.GetBurnRateAdjustmentEpoch() > 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "disable_burn_rate_adjustment cannot be combined with a positive burn_rate_adjustment_epoch")
	}
	if m.GetResetDisputeWindow() && m.GetDisputeWindowHours() > 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "reset_dispute_window cannot be combined with positive dispute_window_hours")
	}
	return nil
}

// GetSigners returns the governance authority for MsgUpdateParams.
func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetAuthority())}
}
