
package keeper

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

type msgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

// NewMsgServerImpl returns an implementation of the credits Msg service.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func (s *msgServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("credits keeper not initialized")
	}
	return s.keeper, nil
}

func recoverCredits(action string, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		default:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		}
	}
}

func constantTimeStringEqual(a string, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

const exchangeRateScale = int64(10_000)

func formatRatioFixed4(numerator, denominator sdkmath.Int) string {
	if denominator.IsZero() || numerator.IsNegative() || denominator.IsNegative() {
		return "0.0000"
	}
	scaled := numerator.MulRaw(exchangeRateScale).Quo(denominator)
	whole := scaled.QuoRaw(exchangeRateScale)
	frac := scaled.ModRaw(exchangeRateScale)
	if !frac.IsUint64() {
		return fmt.Sprintf("%s.0000", whole.String())
	}
	return fmt.Sprintf("%s.%04d", whole.String(), frac.Uint64())
}

func validateCanonicalMsgID(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return "", fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return value, nil
}

func validateOptionalCanonicalMsgID(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if trimmed != value {
		return "", fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return value, nil
}

func (s *msgServer) LockCredits(goCtx context.Context, msg *types.MsgLockCredits) (resp *types.MsgLockCreditsResponse, err error) {
	defer recoverCredits("credits/LockCredits", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	if err := keeper.EnsureModuleAccount(sdkCtx); err != nil {
		return nil, err
	}

	params := keeper.GetParams(sdkCtx)

	if msg.Amount.Amount.IsNil() {
		return nil, fmt.Errorf("amount is required")
	}
	amount, err := types.CoinFromProtoSafe(msg.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid lock amount: %w", err)
	}
	if !amount.IsValid() {
		return nil, fmt.Errorf("invalid amount")
	}
	if !amount.IsPositive() {
		return nil, fmt.Errorf("amount must be positive")
	}
	if amount.Denom != params.CreditDenom {
		return nil, fmt.Errorf("expected denom %s got %s", params.CreditDenom, amount.Denom)
	}
	if _, err := validateCanonicalMsgID("session_id", msg.SessionId); err != nil {
		return nil, err
	}
	if _, err := validateCanonicalMsgID("tool_id", msg.ToolId); err != nil {
		return nil, err
	}

	ttlSeconds := msg.TtlSeconds
	if ttlSeconds > 0 {
		maxTTLSeconds := uint64(params.MaxLockTtlSeconds)
		if maxTTLSeconds == 0 {
			maxTTLSeconds = uint64(types.DefaultMaxLockTTLSeconds)
		}
		if ttlSeconds > maxTTLSeconds {
			ttlSeconds = maxTTLSeconds
		}
	}

	requestedTTL := time.Duration(ttlSeconds) * time.Second
	lockID, err := keeper.LockCreditsWithTTL(
		sdkCtx,
		msg.Router,
		msg.SessionId,
		amount,
		msg.ToolId,
		msg.QuoteId,
		msg.PolicyVersion,
		msg.IntentHash,
		requestedTTL,
		msg.ToolpackId,
	)
	if err != nil {
		return nil, err
	}

	lock, found := keeper.GetLock(sdkCtx, lockID)
	if !found || lock.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("lock %s missing expiry after lock creation", lockID)
	}

	resp = &types.MsgLockCreditsResponse{LockId: lockID, ExpiresAt: lock.ExpiresAt.Unix()}
	return resp, nil
}

func (s *msgServer) SettleCredits(goCtx context.Context, msg *types.MsgSettleCredits) (resp *types.MsgSettleCreditsResponse, err error) {
	defer recoverCredits("credits/SettleCredits", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	if err := keeper.EnsureModuleAccount(sdkCtx); err != nil {
		return nil, err
	}

	// Validate input
	if msg.ActualCost.Amount.IsNil() {
		return nil, fmt.Errorf("actual cost is required")
	}
	actualCost, err := types.CoinFromProtoSafe(msg.ActualCost)
	if err != nil {
		return nil, fmt.Errorf("invalid actual cost: %w", err)
	}
	if !actualCost.IsValid() || actualCost.Amount.IsNegative() {
		return nil, fmt.Errorf("invalid actual cost")
	}

	lockID, err := validateCanonicalMsgID("lock_id", msg.LockId)
	if err != nil {
		return nil, err
	}
	receiptID, err := validateCanonicalMsgID("receipt_id", msg.ReceiptId)
	if err != nil {
		return nil, err
	}
	toolID, err := validateCanonicalMsgID("tool_id", msg.ToolId)
	if err != nil {
		return nil, err
	}

	// Load lock and authorize router
	lock, found := keeper.GetLock(sdkCtx, lockID)
	if !found || lock == nil {
		return nil, fmt.Errorf("lock %s not found", lockID)
	}
	if strings.TrimSpace(lock.Router) == "" {
		return nil, fmt.Errorf("lock %s has empty router", lockID)
	}
	if !constantTimeStringEqual(msg.Router, lock.Router) {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("router %s cannot settle lock %s", msg.Router, lockID)
	}

	lockToolID := strings.TrimSpace(lock.ToolId)
	if lockToolID == "" {
		return nil, fmt.Errorf("lock %s has empty tool id", lockID)
	}
	if !constantTimeStringEqual(toolID, lockToolID) {
		return nil, fmt.Errorf("tool id mismatch: lock=%s msg=%s", lockToolID, toolID)
	}

	// Enforce denom uniqueness. Check receipt status to allow partial fills but reject completed ones.
	params := keeper.GetParams(sdkCtx)
	if strings.TrimSpace(params.CreditDenom) != "" && actualCost.Denom != params.CreditDenom {
		return nil, fmt.Errorf("expected denom %s got %s", params.CreditDenom, actualCost.Denom)
	}
	if existing, exists := keeper.GetSettlement(sdkCtx, receiptID); exists {
		if existing.Status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
			return nil, fmt.Errorf("receipt already settled: %s", receiptID)
		}
	}

	// Proof-of-Service gate (Step 4 — Verifiable Execution). Settlement pays only
	// against a verifiable, SuperNode-attested receipt anchored on-chain for this
	// tool: receipt_id must resolve to a registry UsageReceipt whose tool matches
	// the lock. This binds payment to proof of work actually performed.
	if keeper.registryKeeper == nil {
		return nil, fmt.Errorf("registry keeper unavailable: cannot verify proof-of-service")
	}
	if err := keeper.registryKeeper.ValidateReceipt(sdkCtx, receiptID, lockToolID); err != nil {
		return nil, fmt.Errorf("proof-of-service verification failed for receipt %s: %w", receiptID, err)
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"receipt_verified",
		sdk.NewAttribute("receipt_id", receiptID),
		sdk.NewAttribute("tool_id", lockToolID),
	))

	routerAddr, err := sdk.AccAddressFromBech32(lock.Router)
	if err != nil {
		return nil, fmt.Errorf("invalid router address in lock %s: %w", lockID, err)
	}

	var referrerAddr sdk.AccAddress
	referrerID, err := validateOptionalCanonicalMsgID("referrer", msg.Referrer)
	if err != nil {
		return nil, err
	}
	if referrerID != "" {
		referrerAddr, err = sdk.AccAddressFromBech32(referrerID)
		if err != nil {
			return nil, fmt.Errorf("invalid referrer address: %w", err)
		}
	}

	if msg.CacheHit {
		originToolID, err := validateCanonicalMsgID("origin_tool_id", msg.OriginToolId)
		if err != nil {
			if strings.TrimSpace(msg.OriginToolId) == "" {
				return nil, fmt.Errorf("origin tool id is required for cache hits")
			}
			return nil, err
		}
		if originToolID == "" {
			return nil, fmt.Errorf("origin tool id is required for cache hits")
		}
		if constantTimeStringEqual(originToolID, lockToolID) {
			return nil, fmt.Errorf("origin tool id must differ from tool id for cache hits")
		}
	}

	// Resolve the publisher from the registry and ensure the request matches.
	if keeper.registryKeeper == nil {
		return nil, fmt.Errorf("registry keeper unavailable: cannot verify publisher")
	}
	expectedPublisherAddr, err := keeper.registryKeeper.GetToolPublisher(sdkCtx, lockToolID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve publisher for tool %s: %w", lockToolID, err)
	}
	if expectedPublisherAddr == nil {
		return nil, fmt.Errorf("publisher address missing for tool %s", lockToolID)
	}

	msgPublisherID, err := validateCanonicalMsgID("publisher", msg.Publisher)
	if err != nil {
		return nil, err
	}
	msgPublisherAddr, err := sdk.AccAddressFromBech32(msgPublisherID)
	if err != nil {
		return nil, fmt.Errorf("invalid publisher address: %w", err)
	}
	if !msgPublisherAddr.Equals(expectedPublisherAddr) {
		return nil, fmt.Errorf("publisher mismatch: tool=%s expected=%s got=%s", lockToolID, expectedPublisherAddr.String(), msgPublisherAddr.String())
	}

	policySnapshot := lock.GetPolicyVersion()

	// Process the settlement using SettleLock to ensure lock status is updated.
	request := SettlementRequest{
		ReceiptID:      receiptID,
		ToolID:         lockToolID,
		PublisherAddr:  expectedPublisherAddr,
		RouterAddr:     routerAddr,
		ReferrerAddr:   referrerAddr,
		CacheHit:       msg.CacheHit,
		OriginToolID:   strings.TrimSpace(msg.OriginToolId),
		PublisherID:    expectedPublisherAddr.String(),
		UserID:         lock.Router,
		RouterID:       lock.Router,
		ReferrerID:     referrerID,
		PolicySnapshot: policySnapshot,
		ToolpackID:     lock.GetToolpackId(),
		Stage:          msg.Stage,
		ActionID:       "", // ActionID not currently passed in MsgSettleCredits
	}

	if strings.TrimSpace(msg.ToolpackId) != "" {
		request.ToolpackID = msg.ToolpackId
	}

	result, err := keeper.SettleLock(goCtx, lockID, actualCost, request)
	if err != nil {
		return nil, fmt.Errorf("failed to settle lock: %w", err)
	}

	if result == nil {
		return nil, fmt.Errorf("settlement failed to produce result")
	}

	// Prepare response from actual distributed amounts
	// Helper to extract amount for denom
	getAmount := func(coins sdk.Coins, denom string) sdkmath.Int {
		return coins.AmountOf(denom)
	}

	resp = &types.MsgSettleCreditsResponse{
		BurnAmount:      types.CoinToProto(sdk.NewCoin(actualCost.Denom, getAmount(result.BurnAmount, actualCost.Denom))),
		PublisherAmount: types.CoinToProto(sdk.NewCoin(actualCost.Denom, getAmount(result.PublisherAmount, actualCost.Denom))),
		RouterAmount:    types.CoinToProto(sdk.NewCoin(actualCost.Denom, getAmount(result.RouterAmount, actualCost.Denom))),
		ReferrerAmount:  types.CoinToProto(sdk.NewCoin(actualCost.Denom, getAmount(result.ReferrerAmount, actualCost.Denom))),
		RefundAmount:    types.CoinToProto(sdk.NewCoin(actualCost.Denom, getAmount(result.RefundAmount, actualCost.Denom))),
	}
	return resp, nil
}

func (s *msgServer) SettleOverdraft(goCtx context.Context, msg *types.MsgSettleOverdraft) (resp *types.MsgSettleOverdraftResponse, err error) {
	defer recoverCredits("credits/SettleOverdraft", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	params := keeper.GetParams(sdkCtx)
	if params.OverdraftMaxCreditLineToBondBps == 0 || params.OverdraftLiquidationThresholdBps == 0 {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("overdraft settlement disabled: fall back to lock-per-call funding")
	}

	creditLimit, err := types.CoinFromProtoSafe(msg.CreditLimit)
	if err != nil {
		return nil, fmt.Errorf("invalid credit limit: %w", err)
	}
	if strings.TrimSpace(params.CreditDenom) != "" && creditLimit.Denom != params.CreditDenom {
		return nil, fmt.Errorf("expected denom %s got %s", params.CreditDenom, creditLimit.Denom)
	}
	if msg.LiquidationThresholdBps != params.OverdraftLiquidationThresholdBps {
		return nil, sdkerrors.ErrInvalidRequest.Wrapf(
			"stale overdraft liquidation threshold: expected %d got %d",
			params.OverdraftLiquidationThresholdBps,
			msg.LiquidationThresholdBps,
		)
	}

	return nil, sdkerrors.ErrInvalidRequest.Wrap("overdraft settlement execution not wired: fall back to lock-per-call funding")
}

func (s *msgServer) UnlockCredits(goCtx context.Context, msg *types.MsgUnlockCredits) (resp *types.MsgUnlockCreditsResponse, err error) {
	defer recoverCredits("credits/UnlockCredits", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	if err := keeper.EnsureModuleAccount(sdkCtx); err != nil {
		return nil, err
	}

	lockID, err := validateCanonicalMsgID("lock_id", msg.LockId)
	if err != nil {
		return nil, err
	}

	lock, found := keeper.GetLock(sdkCtx, lockID)
	if !found || lock == nil {
		return nil, fmt.Errorf("lock %s not found", lockID)
	}
	if strings.TrimSpace(lock.Router) == "" {
		return nil, fmt.Errorf("lock %s has empty router", lockID)
	}
	if !constantTimeStringEqual(msg.Router, lock.Router) {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("router %s cannot unlock lock %s", msg.Router, lockID)
	}

	// Use the keeper's UnlockCredits method.
	if err := keeper.UnlockCredits(goCtx, lockID, msg.Reason); err != nil {
		return nil, err
	}

	// Get the lock to return the amount unlocked
	lock, found = keeper.GetLock(sdkCtx, lockID)
	if !found || lock == nil {
		return nil, fmt.Errorf("lock %s not found after unlock", lockID)
	}

	amountUnlocked := types.CoinFromProto(lock.Amount)

	resp = &types.MsgUnlockCreditsResponse{
		AmountUnlocked: types.CoinToProto(amountUnlocked),
	}
	return resp, nil
}

func (s *msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverCredits("credits/UpdateParams", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.Authority(), msg.Authority)
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	params := keeper.GetParams(sdkCtx)

	if msg.MaxLockTtlSeconds > 0 {
		params.MaxLockTtlSeconds = msg.MaxLockTtlSeconds
	}
	if msg.BurnRateSpendBps > 0 {
		params.BurnRateSpendBps = msg.BurnRateSpendBps
	}
	if msg.BurnRateAcqBps > 0 {
		params.BurnRateAcqBps = msg.BurnRateAcqBps
	}
	if msg.TargetAnnualDeflationBps > 0 {
		params.TargetAnnualDeflationBps = msg.TargetAnnualDeflationBps
	}
	if msg.MinBurnRateSpendBps > 0 {
		params.MinBurnRateSpendBps = msg.MinBurnRateSpendBps
	}
	if msg.MaxBurnRateSpendBps > 0 {
		params.MaxBurnRateSpendBps = msg.MaxBurnRateSpendBps
	}
	// Zero means "leave unchanged" for the numeric fields, so disabling the
	// adaptive burn controller (epoch zero) is only reachable via the
	// explicit flag.
	if msg.DisableBurnRateAdjustment {
		params.BurnRateAdjustmentEpoch = 0
	}
	if msg.BurnRateAdjustmentEpoch > 0 {
		params.BurnRateAdjustmentEpoch = msg.BurnRateAdjustmentEpoch
	}
	if msg.DeathSpiralSupplyContractionBps > 0 {
		params.DeathSpiralSupplyContractionBps = msg.DeathSpiralSupplyContractionBps
	}
	if msg.DeathSpiralBurnRateCapBps > 0 {
		params.DeathSpiralBurnRateCapBps = msg.DeathSpiralBurnRateCapBps
	}
	// Zero means "leave unchanged" for the numeric fields, so the disabled
	// overdraft state (both params zero) is only reachable via the explicit
	// disable flag.
	if msg.DisableOverdraft {
		params.OverdraftMaxCreditLineToBondBps = 0
		params.OverdraftLiquidationThresholdBps = 0
	}
	if msg.OverdraftMaxCreditLineToBondBps > 0 {
		params.OverdraftMaxCreditLineToBondBps = msg.OverdraftMaxCreditLineToBondBps
	}
	if msg.OverdraftLiquidationThresholdBps > 0 {
		params.OverdraftLiquidationThresholdBps = msg.OverdraftLiquidationThresholdBps
	}
	// A zero dispute window defers to the registry's canonical window; that
	// state is only reachable via the explicit reset flag.
	if msg.ResetDisputeWindow {
		params.DisputeWindowHours = 0
	}
	if msg.DisputeWindowHours > 0 {
		params.DisputeWindowHours = msg.DisputeWindowHours
	}

	if err := keeper.SetParams(sdkCtx, params); err != nil {
		return nil, err
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.TypeMsgUpdateParams,
			sdk.NewAttribute("authority", msg.Authority),
		),
	)

	resp = &types.MsgUpdateParamsResponse{}
	return resp, nil
}

// SwapLUMEtoLAC handles LUME to LAC swap requests
func (s *msgServer) SwapLUMEtoLAC(goCtx context.Context, msg *types.MsgSwapLUMEtoLAC) (resp *types.MsgSwapLUMEtoLACResponse, err error) {
	defer recoverCredits("credits/SwapLUMEtoLAC", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	if err := keeper.EnsureModuleAccount(sdkCtx); err != nil {
		return nil, err
	}

	// Parse sender address
	sender, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, fmt.Errorf("invalid sender address: %w", err)
	}

	// Validate LUME amount
	if msg.LumeAmount.Amount.IsNil() {
		return nil, fmt.Errorf("lume amount is required")
	}
	lumeAmount, err := types.CoinFromProtoSafe(msg.LumeAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid LUME amount: %w", err)
	}
	if !lumeAmount.IsValid() || !lumeAmount.IsPositive() {
		return nil, fmt.Errorf("invalid LUME amount")
	}

	if lumeAmount.Denom != types.DefaultLumeDenom {
		return nil, fmt.Errorf("expected LUME token, got %s", lumeAmount.Denom)
	}
	minLacOut, hasMinLacOut, err := types.ParseSwapMinOut("min_lac_out", msg.MinLacOut)
	if err != nil {
		return nil, err
	}
	params := keeper.GetParams(goCtx)
	_, expectedNetLac, err := CalculateBurnAmount(lumeAmount.Amount, params.BurnRateAcqBps)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate minimum LAC output: %w", err)
	}
	if hasMinLacOut && expectedNetLac.LT(minLacOut) {
		return nil, fmt.Errorf(
			"minimum LAC output not met: expected %s%s, minimum %s%s",
			expectedNetLac,
			params.CreditDenom,
			minLacOut,
			params.CreditDenom,
		)
	}

	// Perform the swap
	netLacAmount, burnAmount, err := keeper.SwapLUMEtoLAC(goCtx, sender, lumeAmount)
	if err != nil {
		return nil, fmt.Errorf("swap failed: %w", err)
	}

	// Compute the EFFECTIVE exchange rate as (net_lac_out / gross_lume_in).
	// Defaults to "1.0" when lumeAmount is zero (avoiding a divide-by-zero)
	// but the real value reflects the burn deduction: if burn_bps > 0,
	// netLacAmount < lumeAmount.Amount, so the effective rate is < 1.0.
	// This is the rate the CALLER actually received — not the
	// pre-burn notional 1:1 (see keeper.SwapLUMEtoLAC DESIGN NOTE for
	// why the underlying asset-rate is 1:1 but the realized rate
	// after acquisition burn is not).
	effectiveRate := "1.0"
	if lumeAmount.IsPositive() {
		effectiveRate = formatRatioFixed4(netLacAmount.Amount, lumeAmount.Amount)
	}

	resp = &types.MsgSwapLUMEtoLACResponse{
		LacReceived:  types.CoinToProto(netLacAmount),
		BurnAmount:   types.CoinToProto(burnAmount),
		ExchangeRate: effectiveRate,
	}
	return resp, nil
}

// SwapLACtoLUME handles LAC to LUME swap requests
func (s *msgServer) SwapLACtoLUME(goCtx context.Context, msg *types.MsgSwapLACtoLUME) (resp *types.MsgSwapLACtoLUMEResponse, err error) {
	defer recoverCredits("credits/SwapLACtoLUME", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	if err := keeper.EnsureModuleAccount(sdkCtx); err != nil {
		return nil, err
	}

	// Parse sender address
	sender, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, fmt.Errorf("invalid sender address: %w", err)
	}

	// Validate LAC amount
	if msg.LacAmount.Amount.IsNil() {
		return nil, fmt.Errorf("lac amount is required")
	}
	lacAmount, err := types.CoinFromProtoSafe(msg.LacAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid LAC amount: %w", err)
	}
	if !lacAmount.IsValid() || !lacAmount.IsPositive() {
		return nil, fmt.Errorf("invalid LAC amount")
	}
	minLumeOut, hasMinLumeOut, err := types.ParseSwapMinOut("min_lume_out", msg.MinLumeOut)
	if err != nil {
		return nil, err
	}
	_, expectedNetLume, err := CalculateBurnAmount(lacAmount.Amount, uint32(RedemptionBurnRateBPS))
	if err != nil {
		return nil, fmt.Errorf("failed to calculate minimum LUME output: %w", err)
	}
	if hasMinLumeOut && expectedNetLume.LT(minLumeOut) {
		return nil, fmt.Errorf(
			"minimum LUME output not met: expected %s%s, minimum %s%s",
			expectedNetLume,
			types.DefaultLumeDenom,
			minLumeOut,
			types.DefaultLumeDenom,
		)
	}

	// Perform the swap via keeper
	netLumeAmount, burnAmount, err := keeper.SwapLACtoLUME(goCtx, sender, lacAmount)
	if err != nil {
		return nil, fmt.Errorf("swap failed: %w", err)
	}

	// Compute the EFFECTIVE exchange rate as (net_lume_out / gross_lac_in).
	// Sibling of the SwapLUMEtoLAC branch: the underlying asset rate
	// is 1:1 but the realized rate after redemption burn is < 1.0
	// when burn_bps > 0. "1.0" default is the zero-input guard; the
	// real value is computed from netLumeAmount + lacAmount.
	effectiveRate := "1.0"
	if lacAmount.IsPositive() {
		effectiveRate = formatRatioFixed4(netLumeAmount.Amount, lacAmount.Amount)
	}

	resp = &types.MsgSwapLACtoLUMEResponse{
		LumeReceived: types.CoinToProto(netLumeAmount),
		BurnAmount:   types.CoinToProto(burnAmount),
		ExchangeRate: effectiveRate,
	}
	return resp, nil
}
