package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/LumeraProtocol/lumera/x/payment_rails/types"
)

type msgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

// NewMsgServerImpl returns an implementation of the payment_rails Msg service.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func recoverPaymentRails(action string, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		default:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		}
	}
}

func (s *msgServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("payment_rails keeper not initialized")
	}
	return s.keeper, nil
}

func updateParamsHasNonPauseUpdate(msg *types.MsgUpdateParams) bool {
	return len(msg.AcceptedDenoms) > 0 ||
		msg.AcqFeeBps > 0 ||
		msg.MaxSlippageBps > 0 ||
		msg.MaxOracleDeviationBps > 0 ||
		msg.OracleStalenessSec > 0 ||
		msg.MinConfirmations > 0 ||
		msg.MaxTopupsPerHour > 0 ||
		msg.MaxLacPerDay != "" ||
		msg.MaxDepositLacPerAsset != "" ||
		msg.WithdrawDelaySec > 0
}

func lookupSettlementByReference(k *Keeper, goCtx context.Context, referenceID string) (*types.IBCSettlementRecord, error) {
	settlement, err := k.GetIBCSettlementByReference(goCtx, referenceID)
	if err != nil {
		if errors.Is(err, types.ErrSettlementNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return settlement, nil
}

// CreateDeposit processes an Injective asset deposit and mints LAC credits.
func (s *msgServer) CreateDeposit(goCtx context.Context, msg *types.MsgCreateDeposit) (resp *types.MsgCreateDepositResponse, err error) {
	defer recoverPaymentRails("payment_rails/CreateDeposit", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Convert proto coin to sdk.Coin
	amount, err := types.CoinFromProtoSafe(msg.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid deposit amount: %w", err)
	}
	k, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Build the deposit request
	req := types.DepositRequest{
		User:                msg.User,
		Amount:              amount,
		TxHash:              msg.TxHash,
		RequestID:           msg.RequestId,
		Confirmations:       msg.Confirmations,
		QuotedPrice:         msg.QuotedPrice,
		SettlementChannelID: msg.SettlementChannelId,
		SettlementPortID:    msg.SettlementPortId,
	}

	// Call the keeper
	deposit, err := k.CreateDeposit(goCtx, req)
	if err != nil {
		return nil, err
	}

	// Get the pricing and mint records for response
	pricing, _ := k.GetPricing(goCtx, deposit.DepositId)
	mint, _ := k.state.Mints.Get(goCtx, deposit.DepositId)

	resp = &types.MsgCreateDepositResponse{
		DepositId: deposit.DepositId,
	}

	if mint != nil {
		resp.LacMinted = mint.LacMinted
		resp.FeeLac = mint.FeeLac
	}

	if pricing != nil {
		resp.OraclePrice = pricing.OraclePrice
		resp.SlippageBps = pricing.SlippageBps
	}
	settlement, err := lookupSettlementByReference(k, goCtx, deposit.DepositId)
	if err != nil {
		return nil, err
	}
	if settlement != nil {
		resp.SettlementId = settlement.SettlementId
		resp.SettlementStatus = settlement.Status
	}

	return resp, nil
}

// RequestWithdraw burns LAC and records a withdrawal request.
func (s *msgServer) RequestWithdraw(goCtx context.Context, msg *types.MsgRequestWithdraw) (resp *types.MsgRequestWithdrawResponse, err error) {
	defer recoverPaymentRails("payment_rails/RequestWithdraw", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Convert proto coin to sdk.Coin
	lacAmount, err := types.CoinFromProtoSafe(msg.LacAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid withdraw amount: %w", err)
	}
	k, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Build the withdraw request
	req := types.WithdrawRequest{
		User:                msg.User,
		LacAmount:           lacAmount,
		Denom:               msg.Denom,
		RequestID:           msg.RequestId,
		QuotedPrice:         msg.QuotedPrice,
		SettlementChannelID: msg.SettlementChannelId,
		SettlementPortID:    msg.SettlementPortId,
	}

	// Call the keeper
	record, err := k.RequestWithdraw(goCtx, req)
	if err != nil {
		return nil, err
	}

	resp = &types.MsgRequestWithdrawResponse{
		WithdrawId:    record.WithdrawId,
		LacBurned:     record.LacBurned,
		AssetReleased: record.AssetReleased,
	}
	settlement, err := lookupSettlementByReference(k, goCtx, record.WithdrawId)
	if err != nil {
		return nil, err
	}
	if settlement != nil {
		resp.SettlementId = settlement.SettlementId
		resp.SettlementStatus = settlement.Status
	}

	return resp, nil
}

// FinalizeWithdraw marks a withdrawal as completed (governance only).
func (s *msgServer) FinalizeWithdraw(goCtx context.Context, msg *types.MsgFinalizeWithdraw) (resp *types.MsgFinalizeWithdrawResponse, err error) {
	defer recoverPaymentRails("payment_rails/FinalizeWithdraw", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	k, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Call the keeper (it validates authority)
	record, err := k.FinalizeWithdraw(goCtx, msg.Authority, msg.WithdrawId)
	if err != nil {
		return nil, err
	}

	resp = &types.MsgFinalizeWithdrawResponse{
		User:          record.User,
		AssetReleased: record.AssetReleased,
	}

	return resp, nil
}

// RefundDeposit refunds a deposit and burns minted LAC (governance only).
func (s *msgServer) RefundDeposit(goCtx context.Context, msg *types.MsgRefundDeposit) (resp *types.MsgRefundDepositResponse, err error) {
	defer recoverPaymentRails("payment_rails/RefundDeposit", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	k, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Get the mint record before refund for response
	mint, _ := k.state.Mints.Get(goCtx, msg.DepositId)

	// Call the keeper (it validates authority)
	deposit, err := k.RefundDeposit(goCtx, msg.Authority, msg.DepositId)
	if err != nil {
		return nil, err
	}

	resp = &types.MsgRefundDepositResponse{
		User:           deposit.User,
		OriginalAmount: deposit.Amount,
	}

	if mint != nil {
		resp.LacBurned = mint.LacMinted
	}

	return resp, nil
}

// UpdateParams updates module parameters (governance only).
func (s *msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverPaymentRails("payment_rails/UpdateParams", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Validate authority
	if strings.TrimSpace(msg.Authority) == "" {
		return nil, types.ErrUnauthorized
	}
	if msg.MaxLacPerDay != "" {
		_, ok := sdkmath.NewIntFromString(msg.MaxLacPerDay)
		if !ok {
			return nil, fmt.Errorf("invalid max_lac_per_day: %s", msg.MaxLacPerDay)
		}
	}
	if msg.MaxDepositLacPerAsset != "" {
		_, ok := sdkmath.NewIntFromString(msg.MaxDepositLacPerAsset)
		if !ok {
			return nil, fmt.Errorf("invalid max_deposit_lac_per_asset: %s", msg.MaxDepositLacPerAsset)
		}
	}
	k, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != k.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", k.Authority(), msg.Authority)
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(sdkCtx)
	hasNonPauseUpdate := updateParamsHasNonPauseUpdate(msg)

	// Apply updates (only non-zero/non-empty values)
	if len(msg.AcceptedDenoms) > 0 {
		params.AcceptedDenoms = msg.AcceptedDenoms
	}
	if msg.AcqFeeBps > 0 {
		params.AcqFeeBps = msg.AcqFeeBps
	}
	if msg.MaxSlippageBps > 0 {
		params.MaxSlippageBps = msg.MaxSlippageBps
	}
	if msg.MaxOracleDeviationBps > 0 {
		params.MaxOracleDeviationBps = msg.MaxOracleDeviationBps
	}
	if msg.OracleStalenessSec > 0 {
		params.OracleStalenessSec = msg.OracleStalenessSec
	}
	if msg.MinConfirmations > 0 {
		params.MinConfirmations = msg.MinConfirmations
	}
	if msg.MaxTopupsPerHour > 0 {
		params.MaxTopupsPerHour = msg.MaxTopupsPerHour
	}
	if msg.MaxLacPerDay != "" {
		params.MaxLacPerDay = msg.MaxLacPerDay
	}
	if msg.MaxDepositLacPerAsset != "" {
		params.MaxDepositLacPerAsset = msg.MaxDepositLacPerAsset
	}
	if msg.WithdrawDelaySec > 0 {
		params.WithdrawDelaySec = msg.WithdrawDelaySec
	}
	// Proto3 bools have no presence here: omitted pause_conversions arrives as
	// false. Preserve a paused circuit breaker during unrelated partial updates,
	// while still allowing true to pause immediately and a boolean-only false
	// message to unpause.
	if msg.PauseConversions || !hasNonPauseUpdate {
		params.PauseConversions = msg.PauseConversions
	}

	if err := k.SetParams(sdkCtx, params); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"update_params",
			sdk.NewAttribute("authority", msg.Authority),
		),
	)

	resp = &types.MsgUpdateParamsResponse{}
	return resp, nil
}
