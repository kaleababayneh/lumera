
package types

import (
	"crypto/sha256"
	"fmt"
	"strings"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Transaction type identifiers emitted by registry messages.
const (
	TypeMsgRegisterTool      = "register_tool"
	TypeMsgUpdateTool        = "update_tool"
	TypeMsgDelistTool        = "delist_tool"
	TypeMsgSubmitReceipt     = "submit_receipt"
	TypeMsgAnchorBundle      = "anchor_bundle"
	TypeMsgChallengeReceipt  = "challenge_receipt"
	TypeMsgSettleReceipt     = "settle_receipt"
	TypeMsgUpdateParams      = "update_params"
	TypeMsgCreateBond        = "create_bond"
	TypeMsgWithdrawBond      = "withdraw_bond"
	TypeMsgRegisterWatcher   = "register_watcher"
	TypeMsgUnregisterWatcher = "unregister_watcher"
	TypeMsgSubmitSLOProbe    = "submit_slo_probe_receipt"
	TypeMsgSetToolCapsule    = "set_tool_capsule"
)

func sanitizeAddress(addr string) (sdk.AccAddress, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, fmt.Errorf("address cannot be empty")
	}
	return sdk.AccAddressFromBech32(addr)
}

func safeAddress(addr string) sdk.AccAddress {
	a, err := sanitizeAddress(addr)
	if err != nil {
		return sdk.AccAddress{}
	}
	return a
}

func validateCanonicalToolID(field, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != id {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	if err := validateToolID(id); err != nil {
		return fmt.Errorf("%s is invalid: %w", field, err)
	}
	return nil
}

func validateRequiredIdentifier(field, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != id {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func coinFromProto(c *v1beta1.Coin, field string) (sdk.Coin, error) {
	if c == nil {
		return sdk.Coin{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s coin required", field)
	}
	denom := strings.TrimSpace(c.Denom)
	if denom == "" {
		return sdk.Coin{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s denom cannot be empty", field)
	}
	// ValidateBasic invokes this helper on user-supplied coin fields
	// (register-tool bond, update-tool payloads); sdk.NewCoin panics
	// on denom that's non-empty but fails sdk.ValidateDenom's format
	// rules, so check format here before the constructor.
	if err := sdk.ValidateDenom(denom); err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s denom is invalid: %s", field, err)
	}
	amountStr := strings.TrimSpace(c.Amount)
	if amountStr == "" {
		return sdk.Coin{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s amount cannot be empty", field)
	}
	amount, ok := sdkmath.NewIntFromString(amountStr)
	if !ok || amount.IsNegative() {
		return sdk.Coin{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s amount must be non-negative", field)
	}
	return sdk.NewCoin(denom, amount), nil
}

func coinsFromProto(coins []*v1beta1.Coin, field string) (sdk.Coins, error) {
	if len(coins) == 0 {
		return sdk.Coins{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "%s must contain at least one coin", field)
	}
	sdkCoins := sdk.NewCoins()
	for idx, coin := range coins {
		sdkCoin, err := coinFromProto(coin, fmt.Sprintf("%s[%d]", field, idx))
		if err != nil {
			return sdk.Coins{}, err
		}
		sdkCoins = sdkCoins.Add(sdkCoin)
	}
	return sdkCoins, nil
}

// Route implements sdk.Msg for MsgRegisterTool.
func (m *MsgRegisterTool) Route() string { return RouterKey }

// Type returns the message type identifier for MsgRegisterTool.
func (m *MsgRegisterTool) Type() string { return TypeMsgRegisterTool }

// GetSigners identifies the required signer for MsgRegisterTool.
func (m *MsgRegisterTool) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetOwner())}
}

// ValidateBasic performs stateless validation on MsgRegisterTool.
func (m *MsgRegisterTool) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetOwner()); err != nil {
		return err
	}
	if m.ToolCard == nil {
		return fmt.Errorf("tool_card.tool_id is required")
	}
	if err := validateCanonicalToolID("tool_card.tool_id", m.ToolCard.ToolId); err != nil {
		return err
	}
	if len(m.GetBond()) > 0 {
		if _, err := coinsFromProto(m.GetBond(), "bond"); err != nil {
			return err
		}
	}
	return nil
}

// Route implements sdk.Msg for MsgUpdateTool.
func (m *MsgUpdateTool) Route() string { return RouterKey }

// Type returns the message type identifier for MsgUpdateTool.
func (m *MsgUpdateTool) Type() string { return TypeMsgUpdateTool }

// GetSigners identifies the authorized signer for MsgUpdateTool.
func (m *MsgUpdateTool) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetOwner())}
}

// ValidateBasic performs stateless validation on MsgUpdateTool.
func (m *MsgUpdateTool) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetOwner()); err != nil {
		return err
	}
	if err := validateCanonicalToolID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	return nil
}

// Route implements sdk.Msg for MsgDelistTool.
func (m *MsgDelistTool) Route() string { return RouterKey }

// Type returns the message type identifier for MsgDelistTool.
func (m *MsgDelistTool) Type() string { return TypeMsgDelistTool }

// GetSigners identifies the required signer for MsgDelistTool.
func (m *MsgDelistTool) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetOwner())}
}

// ValidateBasic performs stateless validation on MsgDelistTool.
func (m *MsgDelistTool) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetOwner()); err != nil {
		return err
	}
	if err := validateCanonicalToolID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	return nil
}

// Route implements sdk.Msg for MsgSubmitReceipt.
func (m *MsgSubmitReceipt) Route() string { return RouterKey }

// Type returns the message type identifier for MsgSubmitReceipt.
func (m *MsgSubmitReceipt) Type() string { return TypeMsgSubmitReceipt }

// GetSigners identifies the required signer for MsgSubmitReceipt.
func (m *MsgSubmitReceipt) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetRouter())}
}

// ValidateBasic performs stateless validation on MsgSubmitReceipt.
func (m *MsgSubmitReceipt) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetRouter()); err != nil {
		return err
	}
	if m.Receipt == nil {
		return fmt.Errorf("receipt.receipt_id is required")
	}
	if err := validateRequiredIdentifier("receipt.receipt_id", m.Receipt.GetReceiptId()); err != nil {
		return err
	}
	return nil
}

// Route implements sdk.Msg for MsgAnchorBundle.
func (m *MsgAnchorBundle) Route() string { return RouterKey }

// Type returns the message type identifier for MsgAnchorBundle.
func (m *MsgAnchorBundle) Type() string { return TypeMsgAnchorBundle }

// GetSigners identifies the required signer for MsgAnchorBundle.
func (m *MsgAnchorBundle) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetCreator())}
}

// ValidateBasic performs stateless validation on MsgAnchorBundle.
func (m *MsgAnchorBundle) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetCreator()); err != nil {
		return err
	}
	if strings.TrimSpace(m.GetChainId()) == "" {
		return fmt.Errorf("chain_id is required")
	}
	if len(m.GetMerkleRoot()) != sha256.Size {
		return fmt.Errorf("merkle_root must be %d bytes", sha256.Size)
	}
	if m.GetReceiptCount() == 0 {
		return fmt.Errorf("receipt_count must be > 0")
	}
	start := m.GetWindowStartTs()
	end := m.GetWindowEndTs()
	if start != 0 && end != 0 && end < start {
		return fmt.Errorf("window_end_ts must be >= window_start_ts")
	}
	return nil
}

// Route implements sdk.Msg for MsgChallengeReceipt.
func (m *MsgChallengeReceipt) Route() string { return RouterKey }

// Type returns the message type string for MsgChallengeReceipt.
func (m *MsgChallengeReceipt) Type() string { return TypeMsgChallengeReceipt }

// GetSigners identifies the signer for MsgChallengeReceipt.
func (m *MsgChallengeReceipt) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetChallenger())}
}

// ValidateBasic performs stateless validation for MsgChallengeReceipt.
func (m *MsgChallengeReceipt) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetChallenger()); err != nil {
		return err
	}
	if m.Challenge == nil {
		return fmt.Errorf("challenge.receipt_id is required")
	}
	if err := validateRequiredIdentifier("challenge.receipt_id", m.Challenge.GetReceiptId()); err != nil {
		return err
	}
	return nil
}

// Route implements sdk.Msg for MsgSettleReceipt.
func (m *MsgSettleReceipt) Route() string { return RouterKey }

// Type returns the message type identifier for MsgSettleReceipt.
func (m *MsgSettleReceipt) Type() string { return TypeMsgSettleReceipt }

// GetSigners specifies the expected signer for MsgSettleReceipt.
func (m *MsgSettleReceipt) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetSettler())}
}

// ValidateBasic performs stateless validation on MsgSettleReceipt.
func (m *MsgSettleReceipt) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetSettler()); err != nil {
		return err
	}
	if err := validateRequiredIdentifier("receipt_id", m.GetReceiptId()); err != nil {
		return err
	}
	return nil
}

// Route implements sdk.Msg for MsgRegisterWatcher.
func (m *MsgRegisterWatcher) Route() string { return RouterKey }

// Type returns the message type identifier for MsgRegisterWatcher.
func (m *MsgRegisterWatcher) Type() string { return TypeMsgRegisterWatcher }

// GetSigners identifies the required signer for MsgRegisterWatcher.
func (m *MsgRegisterWatcher) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetWatcher())}
}

// ValidateBasic performs stateless validation on MsgRegisterWatcher.
func (m *MsgRegisterWatcher) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetWatcher()); err != nil {
		return err
	}
	if _, err := coinsFromProto(m.GetStake(), "stake"); err != nil {
		return err
	}
	return nil
}

// Route implements sdk.Msg for MsgUnregisterWatcher.
func (m *MsgUnregisterWatcher) Route() string { return RouterKey }

// Type returns the message type identifier for MsgUnregisterWatcher.
func (m *MsgUnregisterWatcher) Type() string { return TypeMsgUnregisterWatcher }

// GetSigners identifies the required signer for MsgUnregisterWatcher.
func (m *MsgUnregisterWatcher) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetWatcher())}
}

// ValidateBasic performs stateless validation on MsgUnregisterWatcher.
func (m *MsgUnregisterWatcher) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetWatcher()); err != nil {
		return err
	}
	return nil
}

// Route implements sdk.Msg for MsgSubmitSLOProbeReceipt.
func (m *MsgSubmitSLOProbeReceipt) Route() string { return RouterKey }

// Type returns the message type identifier for MsgSubmitSLOProbeReceipt.
func (m *MsgSubmitSLOProbeReceipt) Type() string { return TypeMsgSubmitSLOProbe }

// GetSigners identifies the required signer for MsgSubmitSLOProbeReceipt.
func (m *MsgSubmitSLOProbeReceipt) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetWatcher())}
}

// ValidateBasic performs stateless validation on MsgSubmitSLOProbeReceipt.
func (m *MsgSubmitSLOProbeReceipt) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetWatcher()); err != nil {
		return err
	}
	if m.Receipt == nil {
		return fmt.Errorf("receipt is required")
	}
	if err := validateRequiredIdentifier("receipt.receipt_id", m.Receipt.ReceiptId); err != nil {
		return err
	}
	// Determine the effective WatcherAddress to validate against. We
	// canNOT copy m.Receipt by value here — SLOProbeReceipt embeds a
	// protobuf MessageState which carries a sync.Mutex; copying the
	// struct triggers a real `go vet` warning and could in principle
	// produce a mutex-copy hazard if the copy is ever shared.
	//
	// Instead, validate on the pointer. If the receipt doesn't set
	// WatcherAddress, temporarily populate it from m.GetWatcher()
	// for the Validate call and restore the prior value afterward,
	// so the caller's receipt struct is unobservably changed. A
	// mismatched non-empty WatcherAddress still rejects with the
	// original semantics. The restore is deferred so a panic inside
	// Validate() (unlikely but possible) cannot leak the temporary
	// write back to the caller's struct.
	effectiveWatcher := m.GetWatcher()
	restore := m.Receipt.WatcherAddress
	defer func() { m.Receipt.WatcherAddress = restore }()
	if strings.TrimSpace(m.Receipt.WatcherAddress) == "" {
		m.Receipt.WatcherAddress = effectiveWatcher
	} else if m.Receipt.WatcherAddress != effectiveWatcher {
		return fmt.Errorf("receipt.watcher_address must match watcher")
	}
	if err := m.Receipt.Validate(); err != nil {
		return fmt.Errorf("invalid receipt: %w", err)
	}
	return nil
}

// Route implements sdk.Msg for MsgUpdateParams.
func (m *MsgUpdateParams) Route() string { return RouterKey }

// Type returns the message type identifier for MsgUpdateParams.
func (m *MsgUpdateParams) Type() string { return TypeMsgUpdateParams }

// GetSigners returns the governance authority signer for MsgUpdateParams.
func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetAuthority())}
}

// ValidateBasic performs stateless validation on MsgUpdateParams.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetAuthority()); err != nil {
		return err
	}
	if m.Params == nil {
		return fmt.Errorf("params is required")
	}
	return nil
}

// Route implements sdk.Msg for MsgCreateBond.
func (m *MsgCreateBond) Route() string { return RouterKey }

// Type returns the message type identifier for MsgCreateBond.
func (m *MsgCreateBond) Type() string { return TypeMsgCreateBond }

// GetSigners identifies the signer for MsgCreateBond.
func (m *MsgCreateBond) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetOwner())}
}

// ValidateBasic performs stateless validation on MsgCreateBond.
func (m *MsgCreateBond) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetOwner()); err != nil {
		return err
	}
	if err := validateCanonicalToolID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if _, err := coinsFromProto(m.GetAmount(), "amount"); err != nil {
		return err
	}
	return nil
}

// Route implements sdk.Msg for MsgWithdrawBond.
func (m *MsgWithdrawBond) Route() string { return RouterKey }

// Type returns the message type identifier for MsgWithdrawBond.
func (m *MsgWithdrawBond) Type() string { return TypeMsgWithdrawBond }

// GetSigners identifies the signer for MsgWithdrawBond.
func (m *MsgWithdrawBond) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetOwner())}
}

// ValidateBasic performs stateless validation on MsgWithdrawBond.
func (m *MsgWithdrawBond) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetOwner()); err != nil {
		return err
	}
	if err := validateCanonicalToolID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if _, err := coinsFromProto(m.GetAmount(), "amount"); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgSetSLATemplate.
func (m *MsgSetSLATemplate) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetAuthority()); err != nil {
		return err
	}
	if strings.TrimSpace(m.GetSlaId()) == "" {
		return fmt.Errorf("sla_id is required")
	}
	if len(m.GetSlaId()) > MaxSLAIdentifierLength {
		return fmt.Errorf("sla_id length %d exceeds maximum %d", len(m.GetSlaId()), MaxSLAIdentifierLength)
	}
	if strings.TrimSpace(m.GetPayload()) == "" {
		return fmt.Errorf("payload is required")
	}
	if len(m.GetPayload()) > MaxSLATemplatePayloadLength {
		return fmt.Errorf("payload length %d exceeds maximum %d", len(m.GetPayload()), MaxSLATemplatePayloadLength)
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgSetDisputeTerms.
func (m *MsgSetDisputeTerms) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetAuthority()); err != nil {
		return err
	}
	if strings.TrimSpace(m.GetDisputeTermsId()) == "" {
		return fmt.Errorf("dispute_terms_id is required")
	}
	if len(m.GetDisputeTermsId()) > MaxSLAIdentifierLength {
		return fmt.Errorf("dispute_terms_id length %d exceeds maximum %d", len(m.GetDisputeTermsId()), MaxSLAIdentifierLength)
	}
	if strings.TrimSpace(m.GetPayload()) == "" {
		return fmt.Errorf("payload is required")
	}
	if len(m.GetPayload()) > MaxSLATemplatePayloadLength {
		return fmt.Errorf("payload length %d exceeds maximum %d", len(m.GetPayload()), MaxSLATemplatePayloadLength)
	}
	return nil
}

// GetSigners identifies the signer for MsgSetLaneRegistryEntry.
func (m *MsgSetLaneRegistryEntry) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetAuthority())}
}

// ValidateBasic performs stateless validation on MsgSetLaneRegistryEntry.
func (m *MsgSetLaneRegistryEntry) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetAuthority()); err != nil {
		return err
	}
	if m.GetEntry() == nil {
		return fmt.Errorf("entry is required")
	}
	if strings.TrimSpace(m.GetEntry().GetLaneId()) == "" {
		return fmt.Errorf("lane_id is required")
	}
	return nil
}

// Route implements sdk.Msg for MsgSetToolCapsule.
func (m *MsgSetToolCapsule) Route() string { return RouterKey }

// Type returns the message type identifier for MsgSetToolCapsule.
func (m *MsgSetToolCapsule) Type() string { return TypeMsgSetToolCapsule }

// GetSigners identifies the signer for MsgSetToolCapsule.
func (m *MsgSetToolCapsule) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{safeAddress(m.GetOwner())}
}

// ValidateBasic performs stateless validation on MsgSetOriginRoutingConfig.
func (m *MsgSetOriginRoutingConfig) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetAuthority()); err != nil {
		return err
	}
	cfg := m.GetConfig()
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if strings.TrimSpace(cfg.GetOriginId()) == "" {
		return fmt.Errorf("config.origin_id is required")
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgSetToolCapsule.
func (m *MsgSetToolCapsule) ValidateBasic() error {
	if _, err := sanitizeAddress(m.GetOwner()); err != nil {
		return err
	}
	if m.GetCapsule() == nil {
		return fmt.Errorf("capsule is required")
	}
	if err := validateCanonicalToolID("capsule.tool_id", m.GetCapsule().GetToolId()); err != nil {
		return err
	}
	return nil
}
