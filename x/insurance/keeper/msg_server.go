
package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// msgServer implements the x/insurance Msg service by delegating state changes
// to the keeper.
type msgServer struct {
	types.UnimplementedMsgServer
	keeper Keeper
}

// NewMsgServerImpl creates a new Msg server backed by the keeper.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func (m msgServer) requireKeeper() (*Keeper, error) {
	if m.keeper.storeService == nil || m.keeper.bankKeeper == nil || m.keeper.accountKeeper == nil || m.keeper.state.ClaimsByReceipt == nil {
		return nil, status.Error(codes.Internal, "insurance keeper not initialized")
	}
	return &m.keeper, nil
}

func recoverInsurance(action string, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		default:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		}
	}
}

func (m msgServer) ProcessContribution(ctx context.Context, msg *types.MsgProcessContribution) (resp *types.MsgProcessContributionResponse, err error) {
	defer recoverInsurance("insurance/ProcessContribution", &err)
	if msg == nil {
		return nil, types.ErrInvalidContribution.Wrap("message cannot be nil")
	}
	if msg.ReceiptId == "" {
		return nil, types.ErrInvalidContribution.Wrap("receipt_id is required")
	}
	if msg.Amount.Amount.IsNil() {
		return nil, types.ErrInvalidContribution.Wrap("amount is required")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, types.ErrInvalidContribution.Wrap(err.Error())
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if err := keeper.ValidateAuthority(msg.Authority); err != nil {
		return nil, err
	}
	coin, err := protoCoinToSDK(msg.Amount)
	if err != nil {
		return nil, types.ErrInvalidContribution.Wrapf("invalid amount: %s", err)
	}

	rateLimiter := NewRateLimiter(keeper)
	if err := rateLimiter.CheckContributionRate(ctx, msg.ReceiptId); err != nil {
		return nil, err
	}

	sdkCtx := unwrapSDKContext(ctx)

	// Peek at the contribution sequence so we can report the generated ID in events.
	// ContributeToPool now handles recording internally.
	nextSeq, err := keeper.state.ContribCounter.Peek(sdkCtx)
	if err != nil {
		return nil, err
	}

	if err := keeper.ContributeToPool(ctx, msg.ReceiptId, msg.ToolId, msg.PublisherId, msg.PolicyVersion, "", sdk.NewCoins(coin)); err != nil {
		return nil, err
	}

	contributionID := fmt.Sprintf("contrib-%d", nextSeq)

	poolBalance, err := keeper.GetPoolBalance(ctx)
	if err != nil {
		poolBalance = sdk.NewCoins()
	}

	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyContributionID, contributionID),
		sdk.NewAttribute(types.AttributeKeyReceiptID, msg.ReceiptId),
		sdk.NewAttribute(types.AttributeKeyAmount, coin.String()),
	}
	if msg.ToolId != "" {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyToolID, msg.ToolId))
	}
	if msg.PublisherId != "" {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyPublisher, msg.PublisherId))
	}
	if len(poolBalance) > 0 {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyPoolBalance, poolBalance.String()))
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeContribution, attrs...))

	return &types.MsgProcessContributionResponse{}, nil
}

func (m msgServer) FileClaim(ctx context.Context, msg *types.MsgFileClaim) (resp *types.MsgFileClaimResponse, err error) {
	defer recoverInsurance("insurance/FileClaim", &err)
	// Validate basic message fields
	if msg == nil {
		return nil, types.ErrInvalidClaimRequest.Wrap("message cannot be nil")
	}
	if msg.ReceiptId == "" {
		return nil, types.ErrInvalidClaimRequest.Wrap("receipt_id is required")
	}
	if msg.Claimant == "" {
		return nil, types.ErrInvalidClaimRequest.Wrap("claimant address is required")
	}

	// Validate claimed amount
	if msg.ClaimedAmount.Amount.IsNil() {
		return nil, types.ErrInvalidAmount.Wrap("claimed_amount is required")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, types.ErrInvalidClaimRequest.Wrap(err.Error())
	}
	// Use centralized helper for consistent validation
	claimedCoin, err := protoCoinToSDK(msg.ClaimedAmount)
	if err != nil {
		return nil, err
	}
	if claimedCoin.IsZero() {
		return nil, types.ErrInvalidAmount.Wrap("claimed amount must be positive")
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Delegate to keeper for rate limiting, ID generation, storage, metrics, and events
	claimID, err := keeper.FileClaim(ctx, msg)
	if err != nil {
		return nil, err
	}

	return &types.MsgFileClaimResponse{
		ClaimId: claimID,
	}, nil
}

func (m msgServer) ProcessClaim(ctx context.Context, msg *types.MsgProcessClaim) (resp *types.MsgProcessClaimResponse, err error) {
	defer recoverInsurance("insurance/ProcessClaim", &err)
	if msg == nil {
		return nil, types.ErrInvalidClaimRequest.Wrap("message cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, types.ErrInvalidClaimRequest.Wrap(err.Error())
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	// Delegate to keeper for validation and processing
	if err := keeper.ProcessClaim(ctx, msg); err != nil {
		return nil, err
	}

	return &types.MsgProcessClaimResponse{}, nil
}

func (m msgServer) ProcessPayout(ctx context.Context, msg *types.MsgProcessPayout) (resp *types.MsgProcessPayoutResponse, err error) {
	defer recoverInsurance("insurance/ProcessPayout", &err)
	if msg == nil {
		return nil, types.ErrInvalidPayout.Wrap("message cannot be nil")
	}
	if strings.TrimSpace(msg.Authority) == "" {
		return nil, types.ErrInvalidPayout.Wrap("authority is required")
	}
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return nil, types.ErrInvalidPayout.Wrapf("invalid authority address: %s", err)
	}
	claimID := strings.TrimSpace(msg.ClaimId)
	if claimID == "" {
		return nil, types.ErrInvalidPayout.Wrap("claim_id is required")
	}
	if claimID != msg.ClaimId {
		return nil, types.ErrInvalidPayout.Wrap("claim_id must be canonical")
	}
	if len(msg.ClaimId) > types.MaxInsuranceIDLen {
		return nil, types.ErrInvalidPayout.Wrapf("claim_id exceeds %d-byte cap (got %d)", types.MaxInsuranceIDLen, len(msg.ClaimId))
	}
	if msg.Recipient == "" {
		return nil, types.ErrInvalidPayout.Wrap("recipient is required")
	}

	recipient, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, types.ErrInvalidPayout.Wrapf("invalid recipient: %s", err)
	}

	var payoutCoin *sdk.Coin
	if !msg.Amount.Amount.IsNil() {
		coin, convErr := protoCoinToSDK(msg.Amount)
		if convErr != nil {
			return nil, convErr
		}
		if !coin.IsPositive() {
			return nil, types.ErrInvalidAmount.Wrap("payout amount must be positive")
		}
		payoutCoin = &coin
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if err := keeper.ValidateAuthority(msg.Authority); err != nil {
		return nil, err
	}

	sdkCtx := unwrapSDKContext(ctx)
	if err := keeper.processPayout(sdkCtx, msg.ClaimId, recipient, payoutCoin, msg.TxHash); err != nil {
		return nil, err
	}

	return &types.MsgProcessPayoutResponse{}, nil
}

func (m msgServer) UpdatePublisherRisk(ctx context.Context, msg *types.MsgUpdatePublisherRisk) (resp *types.MsgUpdatePublisherRiskResponse, err error) {
	defer recoverInsurance("insurance/UpdatePublisherRisk", &err)
	if msg == nil {
		return nil, types.ErrInvalidPublisher.Wrap("message cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, types.ErrInvalidPublisher.Wrap(err.Error())
	}

	tier, err := premiumTierFromMessage(msg.PremiumTier, msg.RiskScoreBps)
	if err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if err := keeper.ValidateAuthority(msg.Authority); err != nil {
		return nil, err
	}

	sdkCtx := unwrapSDKContext(ctx)
	publisherRiskKey := fmt.Sprintf("%d:%s:%s", len(msg.PublisherId), msg.PublisherId, msg.ToolId)
	risk, err := keeper.state.PublisherRisks.Get(sdkCtx, publisherRiskKey)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, types.ErrInternalError.Wrapf("failed to load publisher risk: %s", err)
	}
	if err != nil || risk == nil {
		risk = &types.PublisherRisk{
			PublisherId:   msg.PublisherId,
			ToolId:        msg.ToolId,
			DisputeCount:  0,
			ClaimCount:    0,
			SuccessRate:   "1.0",
			LastEvaluated: timePtr(sdkCtx.BlockTime()),
		}
	}

	risk.PublisherId = msg.PublisherId
	risk.ToolId = msg.ToolId
	risk.PremiumTier = tier
	risk.PremiumMultiplier = premiumMultiplierForTier(tier)
	risk.RiskScore = formatRiskScoreBps(msg.RiskScoreBps)
	risk.LastEvaluated = timePtr(sdkCtx.BlockTime())

	if err := keeper.state.PublisherRisks.Set(sdkCtx, publisherRiskKey, risk); err != nil {
		return nil, types.ErrInternalError.Wrapf("failed to update publisher risk: %s", err)
	}

	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute("publisher_id", msg.PublisherId),
		sdk.NewAttribute(types.AttributeKeyToolID, msg.ToolId),
		sdk.NewAttribute(types.AttributeKeyRiskScore, risk.RiskScore),
		sdk.NewAttribute(types.AttributeKeyPremiumTier, tier.String()),
	}
	if strings.TrimSpace(msg.Notes) != "" {
		attrs = append(attrs, sdk.NewAttribute("notes", strings.TrimSpace(msg.Notes)))
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypePublisherRiskUpdated, attrs...))

	return &types.MsgUpdatePublisherRiskResponse{}, nil
}

func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverInsurance("insurance/UpdateParams", &err)
	if msg == nil {
		return nil, types.ErrInvalidParameters.Wrap("message cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, types.ErrInvalidParameters.Wrap(err.Error())
	}
	// MsgUpdateParams.Params is a value (gogoproto nullable=false); the state
	// layer and SetParams operate on *types.Params, so take its address.
	params := msg.Params
	if err := params.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if err := keeper.ValidateAuthority(msg.Authority); err != nil {
		return nil, err
	}
	if err := keeper.SetParams(ctx, &params); err != nil {
		return nil, err
	}

	sdkCtx := unwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeParamsUpdated,
			sdk.NewAttribute(types.AttributeKeyAuthority, msg.Authority),
		),
	)

	return &types.MsgUpdateParamsResponse{}, nil
}

// unwrapSDKContext wraps sdk.UnwrapSDKContext to keep msgServer methods focussed.
func unwrapSDKContext(ctx context.Context) sdk.Context {
	return sdk.UnwrapSDKContext(ctx)
}

func premiumTierFromMessage(raw string, riskScoreBps uint32) (types.PremiumTier, error) {
	normalized := strings.TrimSpace(strings.ToUpper(raw))
	if normalized == "" {
		return premiumTierForRiskScore(riskScoreBps), nil
	}
	normalized = strings.TrimPrefix(normalized, "PREMIUM_TIER_")
	switch normalized {
	case "LOW":
		return types.PremiumTier_PREMIUM_TIER_LOW, nil
	case "STANDARD":
		return types.PremiumTier_PREMIUM_TIER_STANDARD, nil
	case "HIGH":
		return types.PremiumTier_PREMIUM_TIER_HIGH, nil
	case "EXTREME":
		return types.PremiumTier_PREMIUM_TIER_EXTREME, nil
	default:
		return types.PremiumTier_PREMIUM_TIER_UNSPECIFIED,
			types.ErrInvalidPublisher.Wrapf("invalid premium_tier %q", raw)
	}
}

func premiumTierForRiskScore(riskScoreBps uint32) types.PremiumTier {
	switch {
	case riskScoreBps < 3_000:
		return types.PremiumTier_PREMIUM_TIER_LOW
	case riskScoreBps < 5_000:
		return types.PremiumTier_PREMIUM_TIER_STANDARD
	case riskScoreBps < 7_000:
		return types.PremiumTier_PREMIUM_TIER_HIGH
	default:
		return types.PremiumTier_PREMIUM_TIER_EXTREME
	}
}

func premiumMultiplierForTier(tier types.PremiumTier) string {
	switch tier {
	case types.PremiumTier_PREMIUM_TIER_LOW:
		return "0.8"
	case types.PremiumTier_PREMIUM_TIER_STANDARD:
		return "1.0"
	case types.PremiumTier_PREMIUM_TIER_HIGH:
		return "1.5"
	case types.PremiumTier_PREMIUM_TIER_EXTREME:
		return "2.0"
	default:
		return "1.0"
	}
}

func formatRiskScoreBps(riskScoreBps uint32) string {
	return fmt.Sprintf("%d.%02d", riskScoreBps/100, riskScoreBps%100)
}
