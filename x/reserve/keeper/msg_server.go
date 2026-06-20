
package keeper

import (
	"context"
	"strconv"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/reserve/types"
)

const maxReserveMsgDurationSeconds = uint64((1<<63 - 1) / int64(time.Second))

type msgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

// NewMsgServerImpl returns the reserve Msg service implementation.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func (m *msgServer) requireKeeper() (*Keeper, error) {
	if m == nil || m.keeper == nil {
		return nil, status.Error(codes.Internal, "reserve keeper not initialized")
	}
	return m.keeper, nil
}

func requireReserveAuthority(k *Keeper, authority string) error {
	if strings.TrimSpace(authority) == "" {
		return status.Error(codes.InvalidArgument, "authority required")
	}
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		return status.Error(codes.InvalidArgument, "authority must be a valid bech32 address")
	}
	if authority != k.authority {
		return status.Error(codes.PermissionDenied, "invalid authority")
	}
	return nil
}

func reserveMsgDuration(seconds uint64) (time.Duration, error) {
	if seconds == 0 {
		return 0, nil
	}
	if seconds > maxReserveMsgDurationSeconds {
		return 0, status.Error(codes.InvalidArgument, "duration_seconds exceeds maximum safe duration")
	}
	return time.Duration(seconds) * time.Second, nil // #nosec G115 -- bounded by maxReserveMsgDurationSeconds above.
}

func reserveParamsFromMessage(msg *types.ReserveParams) (*types.Params, error) {
	if msg == nil {
		return nil, status.Error(codes.InvalidArgument, "params required")
	}
	params := &types.Params{
		CreditDenom: msg.CreditDenom,
		Tiers:       make([]types.TierConfig, 0, len(msg.Tiers)),
	}
	for i, tier := range msg.Tiers {
		if tier == nil {
			return nil, status.Errorf(codes.InvalidArgument, "tier %d required", i)
		}
		minAmount, ok := sdkmath.NewIntFromString(tier.MinCommitmentAmount)
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument, "tier %s has invalid min_commitment_amount", tier.Name)
		}
		params.Tiers = append(params.Tiers, types.TierConfig{
			Name:                tier.Name,
			MinCommitmentAmount: minAmount,
			DiscountBps:         tier.DiscountBps,
			DefaultDurationSec:  tier.DefaultDurationSeconds,
			MaxActivePerPolicy:  tier.MaxActivePerPolicy,
			RolloverAllowed:     tier.RolloverAllowed,
		})
	}
	if err := params.ValidateBasic(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return params, nil
}

// CreateCommitment provisions a prepaid reserve capacity block.
func (m *msgServer) CreateCommitment(ctx context.Context, msg *types.MsgCreateCommitment) (*types.MsgCreateCommitmentResponse, error) {
	if msg == nil {
		return nil, status.Error(codes.InvalidArgument, "msg required")
	}
	if strings.TrimSpace(msg.Owner) != msg.Owner {
		return nil, status.Error(codes.InvalidArgument, "owner must not contain leading or trailing whitespace")
	}
	if _, err := sdk.AccAddressFromBech32(msg.Owner); err != nil {
		return nil, status.Error(codes.InvalidArgument, "owner must be a valid bech32 address")
	}
	if err := validateReserveQueryIdentifier("policy_id", msg.PolicyId, true); err != nil {
		return nil, err
	}
	if err := validateReserveQueryIdentifier("tool_id", msg.ToolId, false); err != nil {
		return nil, err
	}
	if err := validateReserveQueryIdentifier("tier", msg.Tier, true); err != nil {
		return nil, err
	}
	// MsgCreateCommitment.Amount is now a native sdk.Coin (gogoproto migration).
	amount := msg.Amount
	if !amount.IsValid() || !amount.IsPositive() {
		return nil, status.Error(codes.InvalidArgument, "amount must be a positive valid coin")
	}
	duration, err := reserveMsgDuration(msg.DurationSeconds)
	if err != nil {
		return nil, err
	}

	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	commitment, err := keeper.CreateCommitment(ctx, types.ReserveRequest{
		Owner:    msg.Owner,
		PolicyID: msg.PolicyId,
		ToolID:   msg.ToolId,
		Tier:     msg.Tier,
		Amount:   amount,
		Duration: duration,
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	now := sdk.UnwrapSDKContext(ctx).BlockTime()
	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.MsgCreateCommitmentResponse{
		Commitment: reserveCommitmentSummary(*commitment, params, now),
	}, nil
}

// ReleaseExpired sweeps expired reserve commitments.
func (m *msgServer) ReleaseExpired(ctx context.Context, msg *types.MsgReleaseExpired) (*types.MsgReleaseExpiredResponse, error) {
	if msg == nil {
		return nil, status.Error(codes.InvalidArgument, "msg required")
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if err := requireReserveAuthority(keeper, msg.Authority); err != nil {
		return nil, err
	}
	if err := keeper.ReleaseExpired(ctx); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &types.MsgReleaseExpiredResponse{}, nil
}

// UpdateParams updates reserve module parameters through governance authority.
func (m *msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, status.Error(codes.InvalidArgument, "msg required")
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if err := requireReserveAuthority(keeper, msg.Authority); err != nil {
		return nil, err
	}
	beforeParams, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	params, err := reserveParamsFromMessage(&msg.Params)
	if err != nil {
		return nil, err
	}
	if err := keeper.SetParams(ctx, params); err != nil {
		return nil, status.Error(codes.InvalidArgument, "set params: "+err.Error())
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.TypeMsgUpdateParams,
			sdk.NewAttribute(types.AttributeKeyAuthority, msg.Authority),
			sdk.NewAttribute(types.AttributeKeyBeforeCreditDenom, beforeParams.CreditDenom),
			sdk.NewAttribute(types.AttributeKeyAfterCreditDenom, params.CreditDenom),
			sdk.NewAttribute(types.AttributeKeyBeforeTierCount, strconv.Itoa(len(beforeParams.Tiers))),
			sdk.NewAttribute(types.AttributeKeyAfterTierCount, strconv.Itoa(len(params.Tiers))),
		),
	)
	return &types.MsgUpdateParamsResponse{}, nil
}
