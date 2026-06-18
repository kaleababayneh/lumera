//go:build cosmos

package types

import (
	"fmt"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// TypeMsgCacheStore identifies the message to store content in cache.
	TypeMsgCacheStore = "cache_store"
	// TypeMsgCacheInvalidate identifies the message to invalidate cache entries.
	TypeMsgCacheInvalidate = "cache_invalidate"
	// TypeMsgRecordCacheHit identifies the message to record a cache hit.
	TypeMsgRecordCacheHit = "record_cache_hit"
	// TypeMsgTickDecay identifies the message to process CAC upkeep.
	TypeMsgTickDecay = "tick_decay"
	// TypeMsgPromoteTier identifies the message to promote an entry to a higher tier.
	TypeMsgPromoteTier = "promote_tier"
	// TypeMsgUpdateParams identifies the governance message for updating params.
	TypeMsgUpdateParams = "update_params"
)

// Per-field ValidateBasic caps. These are stateless defense-in-depth
// ceilings; keeper-side policy (via governance params, e.g.
// MaxEntrySizeBytes) enforces tighter runtime limits on top. Parity
// with caps shipped elsewhere this session (MaxInsuranceIDLen=256,
// MaxToolIDLen=256 in nft, MaxEvidenceFieldLen=4KiB in insurance).
const (
	// MaxCacheIDLen caps any identifier/address field in CAC Msgs
	// (tool_id, content_hash, request_hash, origin/serving tool_id,
	// target_value). 256 matches sibling modules' MaxIDLen.
	MaxCacheIDLen = 256
	// MaxCachedContentBytes caps MsgCacheStore.Content at 4 MiB — 4×
	// the DefaultMaxEntrySizeBytes (1 MiB) to leave headroom above
	// whatever governance sets without admitting max-tx-size payloads
	// straight through. A malicious publisher could otherwise spam
	// near-tx-size Msgs (~10 MiB protobuf ceiling) that consume Unmarshal
	// compute, state-write IO, and CacheEntries index writes per tx.
	MaxCachedContentBytes = 4 * 1024 * 1024
)

func parseAccAddress(addr string) (sdk.AccAddress, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, fmt.Errorf("address cannot be empty")
	}
	return sdk.AccAddressFromBech32(addr)
}

func mustAddr(addr string) sdk.AccAddress {
	a, err := parseAccAddress(addr)
	if err != nil {
		panic(err)
	}
	return a
}

func validateCacheID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > MaxCacheIDLen {
		return fmt.Errorf("%s length %d exceeds maximum %d", field, len(value), MaxCacheIDLen)
	}
	return nil
}

func validateOptionalTTLSeconds(field string, ttl uint64) error {
	if ttl > MaxTTLSeconds {
		return ErrInvalidTTL.Wrapf("%s exceeds maximum safe duration seconds (%d)", field, MaxTTLSeconds)
	}
	return nil
}

func isSupportedCacheInvalidationTargetType(target InvalidationTargetType) bool {
	switch target {
	case InvalidationTargetType_INVALIDATION_TARGET_TYPE_CONTENT_HASH,
		InvalidationTargetType_INVALIDATION_TARGET_TYPE_TOOL_ID,
		InvalidationTargetType_INVALIDATION_TARGET_TYPE_REQUEST_HASH:
		return true
	default:
		return false
	}
}

// Route implements sdk.Msg and returns the router key for MsgCacheStore.
func (m *MsgCacheStore) Route() string { return RouterKey }

// Type returns the message type for MsgCacheStore.
func (m *MsgCacheStore) Type() string { return TypeMsgCacheStore }

// ValidateBasic performs stateless validation on MsgCacheStore fields.
func (m *MsgCacheStore) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetPublisher()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid publisher address: %s", err)
	}
	if err := validateCacheID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if err := validateCacheID("request_hash", m.GetRequestHash()); err != nil {
		return err
	}
	if len(m.GetContent()) == 0 {
		return fmt.Errorf("content cannot be empty")
	}
	if len(m.GetContent()) > MaxCachedContentBytes {
		return fmt.Errorf("content size %d exceeds maximum %d", len(m.GetContent()), MaxCachedContentBytes)
	}
	if err := validateOptionalTTLSeconds("ttl_seconds", m.GetTtlSeconds()); err != nil {
		return err
	}
	// Royalties on a cache hit pay out for serving stored content as if it
	// were the tool's output. That only holds for deterministic tools, so a
	// non-deterministic entry must never be royalty-eligible — otherwise a
	// publisher could collect royalties for serving stale/divergent content.
	// The autopilot policy layer already enforces this
	// (internal/swarmledger/verify.go); this is the authoritative on-chain
	// counterpart on the settlement path.
	if m.GetRoyaltyEligible() && !m.GetIsDeterministic() {
		return fmt.Errorf("royalty_eligible requires is_deterministic")
	}
	return nil
}

// GetSigners returns the publisher address for MsgCacheStore.
func (m *MsgCacheStore) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetPublisher())}
}

// Route implements sdk.Msg and returns the router key for MsgCacheInvalidate.
func (m *MsgCacheInvalidate) Route() string { return RouterKey }

// Type returns the message type for MsgCacheInvalidate.
func (m *MsgCacheInvalidate) Type() string { return TypeMsgCacheInvalidate }

// ValidateBasic performs stateless validation on MsgCacheInvalidate fields.
func (m *MsgCacheInvalidate) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetRequester()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid requester address: %s", err)
	}
	if m.GetTargetType() == InvalidationTargetType_INVALIDATION_TARGET_TYPE_UNSPECIFIED {
		return fmt.Errorf("target_type is required")
	}
	if !isSupportedCacheInvalidationTargetType(m.GetTargetType()) {
		return fmt.Errorf("unsupported target_type %s", m.GetTargetType().String())
	}
	if err := validateCacheID("target_value", m.GetTargetValue()); err != nil {
		return err
	}
	return nil
}

// GetSigners returns the requester address for MsgCacheInvalidate.
func (m *MsgCacheInvalidate) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetRequester())}
}

// Route implements sdk.Msg and returns the router key for MsgRecordCacheHit.
func (m *MsgRecordCacheHit) Route() string { return RouterKey }

// Type returns the message type for MsgRecordCacheHit.
func (m *MsgRecordCacheHit) Type() string { return TypeMsgRecordCacheHit }

// ValidateBasic performs stateless validation on MsgRecordCacheHit fields.
func (m *MsgRecordCacheHit) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetRouter()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid router address: %s", err)
	}
	if err := validateCacheID("content_hash", m.GetContentHash()); err != nil {
		return err
	}
	if err := validateCacheID("origin_tool_id", m.GetOriginToolId()); err != nil {
		return err
	}
	if err := validateCacheID("serving_tool_id", m.GetServingToolId()); err != nil {
		return err
	}
	if _, err := parseAccAddress(m.GetRequesterAddress()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid requester address: %s", err)
	}
	if m.GetTier() == CacheTier_CACHE_TIER_UNSPECIFIED {
		return fmt.Errorf("tier is required")
	}
	if _, ok := CacheTier_name[int32(m.GetTier())]; !ok {
		return fmt.Errorf("tier is invalid: %d", m.GetTier())
	}
	return nil
}

// GetSigners returns the router authority for MsgRecordCacheHit.
func (m *MsgRecordCacheHit) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetRouter())}
}

// Route implements sdk.Msg and returns the router key for MsgTickDecay.
func (m *MsgTickDecay) Route() string { return RouterKey }

// Type returns the message type for MsgTickDecay.
func (m *MsgTickDecay) Type() string { return TypeMsgTickDecay }

// ValidateBasic performs stateless validation on MsgTickDecay fields.
func (m *MsgTickDecay) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetAuthority()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address: %s", err)
	}
	return nil
}

// GetSigners returns the governance authority for MsgTickDecay.
func (m *MsgTickDecay) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetAuthority())}
}

// Route implements sdk.Msg and returns the router key for MsgPromoteTier.
func (m *MsgPromoteTier) Route() string { return RouterKey }

// Type returns the message type for MsgPromoteTier.
func (m *MsgPromoteTier) Type() string { return TypeMsgPromoteTier }

// ValidateBasic performs stateless validation on MsgPromoteTier fields.
func (m *MsgPromoteTier) ValidateBasic() error {
	if _, err := parseAccAddress(m.GetAuthority()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address: %s", err)
	}
	if err := validateCacheID("content_hash", m.GetContentHash()); err != nil {
		return err
	}
	if m.GetTargetTier() == CacheTier_CACHE_TIER_UNSPECIFIED {
		return fmt.Errorf("target_tier is required")
	}
	if _, ok := CacheTier_name[int32(m.GetTargetTier())]; !ok {
		return fmt.Errorf("target_tier is invalid: %d", m.GetTargetTier())
	}
	return nil
}

// GetSigners returns the authority address for MsgPromoteTier.
func (m *MsgPromoteTier) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetAuthority())}
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
	if m.GetParams() == nil {
		return fmt.Errorf("params is required")
	}
	return nil
}

// GetSigners returns the governance authority for MsgUpdateParams.
func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{mustAddr(m.GetAuthority())}
}
