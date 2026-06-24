// Package keeper provides the state management and business logic for the payment_rails module.
package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	oracletypes "github.com/LumeraProtocol/lumera/x/oracle/types"
	"github.com/LumeraProtocol/lumera/x/payment_rails/types"
)

// ConsensusVersion defines the current module consensus version.
const ConsensusVersion = 1

// LacPrecision is the number of micro-LAC per 1 LAC (6 decimal places).
const LacPrecision = int64(1_000_000)

// State encapsulates the module collections state.
type State struct {
	Schema collections.Schema
	Params collections.Item[*types.Params]

	Deposits       collections.Map[string, *types.DepositRecord]
	Pricings       collections.Map[string, *types.PricingRecord]
	Mints          collections.Map[string, *types.MintRecord]
	Withdrawals    collections.Map[string, *types.WithdrawRecord]
	IBCSettlements collections.Map[string, *types.IBCSettlementRecord]

	DepositSeq       collections.Sequence
	WithdrawSeq      collections.Sequence
	IBCSettlementSeq collections.Sequence

	DepositByRequest       collections.Map[string, string]
	WithdrawByRequest      collections.Map[string, string]
	IBCSettlementByRef     collections.Map[string, string]
	IBCSettlementByRequest collections.Map[string, string]

	DepositsByUser    collections.KeySet[collections.Pair[string, string]]
	WithdrawalsByUser collections.KeySet[collections.Pair[string, string]]

	UserHourly collections.Map[collections.Pair[string, time.Time], *types.TopupWindow]
	UserDaily  collections.Map[collections.Pair[string, time.Time], *types.TopupWindow]
}

// Keeper provides the module's state access layer.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService corestore.KVStoreService
	authority    string
	logger       log.Logger
	state        State

	bankKeeper    types.BankKeeper
	creditsKeeper types.CreditsKeeper
	oracleKeeper  types.OracleKeeper
}

// NewKeeper constructs a Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	authority string,
	logger log.Logger,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsPrefix),
			"params",
			jsonValueCodec[types.Params]{},
		),
		Deposits: collections.NewMap(
			sb,
			collections.NewPrefix(types.DepositPrefix),
			"deposits",
			collections.StringKey,
			jsonValueCodec[types.DepositRecord]{},
		),
		Pricings: collections.NewMap(
			sb,
			collections.NewPrefix(types.PricingPrefix),
			"pricings",
			collections.StringKey,
			jsonValueCodec[types.PricingRecord]{},
		),
		Mints: collections.NewMap(
			sb,
			collections.NewPrefix(types.MintPrefix),
			"mints",
			collections.StringKey,
			jsonValueCodec[types.MintRecord]{},
		),
		Withdrawals: collections.NewMap(
			sb,
			collections.NewPrefix(types.WithdrawPrefix),
			"withdrawals",
			collections.StringKey,
			jsonValueCodec[types.WithdrawRecord]{},
		),
		DepositSeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.DepositSeqPrefix),
			"deposit_seq",
		),
		WithdrawSeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.WithdrawSeqPrefix),
			"withdraw_seq",
		),
		DepositByRequest: collections.NewMap(
			sb,
			collections.NewPrefix(types.DepositRequestPrefix),
			"deposit_by_request",
			collections.StringKey,
			collections.StringValue,
		),
		WithdrawByRequest: collections.NewMap(
			sb,
			collections.NewPrefix(types.WithdrawRequestPrefix),
			"withdraw_by_request",
			collections.StringKey,
			collections.StringValue,
		),
		DepositsByUser: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.DepositsByUserPrefix),
			"deposits_by_user",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		WithdrawalsByUser: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.WithdrawalsByUserPrefix),
			"withdrawals_by_user",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		UserHourly: collections.NewMap(
			sb,
			collections.NewPrefix(types.UserHourlyPrefix),
			"user_hourly",
			collections.PairKeyCodec(collections.StringKey, sdk.TimeKey),
			jsonValueCodec[types.TopupWindow]{},
		),
		UserDaily: collections.NewMap(
			sb,
			collections.NewPrefix(types.UserDailyPrefix),
			"user_daily",
			collections.PairKeyCodec(collections.StringKey, sdk.TimeKey),
			jsonValueCodec[types.TopupWindow]{},
		),
		IBCSettlements: collections.NewMap(
			sb,
			collections.NewPrefix(types.IBCSettlementPrefix),
			"ibc_settlements",
			collections.StringKey,
			jsonValueCodec[types.IBCSettlementRecord]{},
		),
		IBCSettlementSeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.IBCSettlementSeqPrefix),
			"ibc_settlement_seq",
		),
		IBCSettlementByRef: collections.NewMap(
			sb,
			collections.NewPrefix(types.IBCSettlementByRefPrefix),
			"ibc_settlement_by_ref",
			collections.StringKey,
			collections.StringValue,
		),
		IBCSettlementByRequest: collections.NewMap(
			sb,
			collections.NewPrefix(types.IBCSettlementByRequestPrefix),
			"ibc_settlement_by_request",
			collections.StringKey,
			collections.StringValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build payment_rails schema: %w", err))
	}
	state.Schema = schema

	return &Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,
		logger:       logger.With("module", fmt.Sprintf("x/%s", types.ModuleName)),
		state:        state,
	}
}

// Schema returns the underlying collections schema.
func (k Keeper) Schema() collections.Schema { return k.state.Schema }

// Logger returns a module-prefixed logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger { return k.logger }

// Authority returns the module authority address.
func (k Keeper) Authority() string { return k.authority }

// BankKeeper returns the bank keeper dependency.
func (k Keeper) BankKeeper() types.BankKeeper { return k.bankKeeper }

// SetCreditsKeeper sets the credits keeper dependency.
func (k *Keeper) SetCreditsKeeper(ck types.CreditsKeeper) { k.creditsKeeper = ck }

// SetBankKeeper sets the bank keeper dependency.
func (k *Keeper) SetBankKeeper(bk types.BankKeeper) { k.bankKeeper = bk }

// SetOracleKeeper sets the oracle keeper dependency.
func (k *Keeper) SetOracleKeeper(ok types.OracleKeeper) { k.oracleKeeper = ok }

// GetParams retrieves module parameters.
func (k Keeper) GetParams(ctx context.Context) *types.Params {
	params, err := k.state.Params.Get(ctx)
	if err != nil || params == nil {
		return types.DefaultParams()
	}
	return params
}

// SetParams updates module parameters.
func (k Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := params.ValidateBasic(); err != nil {
		return err
	}
	return k.state.Params.Set(ctx, params)
}

// CreateDeposit validates and processes a deposit request.
func (k *Keeper) CreateDeposit(ctx context.Context, req types.DepositRequest) (*types.DepositRecord, error) {
	if k.bankKeeper == nil || k.creditsKeeper == nil || k.oracleKeeper == nil {
		return nil, types.ErrInvalidRequest.Wrap("missing keeper dependencies")
	}
	if strings.TrimSpace(req.User) == "" {
		return nil, types.ErrInvalidRequest.Wrap("user required")
	}
	if !req.Amount.IsValid() || !req.Amount.Amount.IsPositive() {
		return nil, types.ErrInvalidAmount
	}
	if err := sdk.ValidateDenom(req.Amount.Denom); err != nil {
		return nil, types.ErrInvalidDenom.Wrap(err.Error())
	}
	if err := validateKeeperID("tx_hash", req.TxHash); err != nil {
		return nil, err
	}
	if err := validateKeeperID("request_id", req.RequestID); err != nil {
		return nil, err
	}
	if err := validateSettlementRoute(req.SettlementChannelID, req.SettlementPortID); err != nil {
		return nil, err
	}

	if existingID, err := k.state.DepositByRequest.Get(ctx, req.RequestID); err == nil && existingID != "" {
		existing, err := k.GetDeposit(ctx, existingID)
		if err != nil {
			return nil, err
		}
		existingAmount := types.CoinFromProto(existing.Amount)
		if existing.User != req.User || existing.Denom != req.Amount.Denom || !existingAmount.Amount.Equal(req.Amount.Amount) {
			return nil, types.ErrDuplicateRequest
		}
		if existing.TxHash != req.TxHash {
			return nil, types.ErrDuplicateRequest
		}
		return existing, nil
	}

	params := k.GetParams(ctx)
	if params.PauseConversions {
		return nil, types.ErrPaused
	}
	if !params.IsAcceptedDenom(req.Amount.Denom) {
		return nil, types.ErrUnsupportedDenom
	}
	if req.Confirmations < params.MinConfirmations {
		return nil, types.ErrMinConfirmations
	}

	price, err := k.lookupPrice(ctx, req.Amount.Denom)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return nil, types.ErrInvalidRequest.Wrap("block time not set")
	}

	lacCoin, feeCoin, pricing, err := k.computeLacMint(ctx, req.Amount, price, req.QuotedPrice)
	if err != nil {
		return nil, err
	}
	maxDepositLac := params.MaxDepositLacPerAssetInt()
	if maxDepositLac.IsPositive() && lacCoin.Amount.GT(maxDepositLac) {
		return nil, types.ErrRateLimitExceeded
	}

	userAddr, err := sdk.AccAddressFromBech32(req.User)
	if err != nil {
		return nil, types.ErrInvalidRequest.Wrap("invalid user address")
	}

	cacheCtx, write := sdkCtx.CacheContext()
	if err := k.enforceRateLimits(cacheCtx, req.User, now, lacCoin.Amount); err != nil {
		return nil, err
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(cacheCtx, userAddr, types.ModuleAccountName, sdk.NewCoins(req.Amount)); err != nil {
		return nil, fmt.Errorf("failed to transfer assets: %w", err)
	}

	seq, err := k.state.DepositSeq.Next(cacheCtx)
	if err != nil {
		return nil, fmt.Errorf("allocate deposit id: %w", err)
	}
	depositID := fmt.Sprintf("dep-%d", seq)

	if err := k.creditsKeeper.MintCredits(cacheCtx, userAddr, lacCoin, "payment_rails_deposit"); err != nil {
		return nil, err
	}

	pricing.DepositId = depositID
	deposit := &types.DepositRecord{
		DepositId: depositID,
		User:      req.User,
		Denom:     req.Amount.Denom,
		Amount:    types.CoinToProto(req.Amount),
		TxHash:    req.TxHash,
		Status:    types.DepositStatus_DEPOSIT_STATUS_MINTED,
		RequestId: req.RequestID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := k.state.Deposits.Set(cacheCtx, depositID, deposit); err != nil {
		return nil, err
	}
	if err := k.state.Pricings.Set(cacheCtx, depositID, pricing); err != nil {
		return nil, err
	}
	mintRecord := &types.MintRecord{
		DepositId:  depositID,
		LacMinted:  types.CoinToProto(lacCoin),
		FeeLac:     types.CoinToProto(feeCoin),
		MintTxHash: "",
		Timestamp:  now,
	}
	if err := k.state.Mints.Set(cacheCtx, depositID, mintRecord); err != nil {
		return nil, err
	}
	if err := k.state.DepositByRequest.Set(cacheCtx, req.RequestID, depositID); err != nil {
		return nil, fmt.Errorf("index deposit by request: %w", err)
	}
	if err := k.state.DepositsByUser.Set(cacheCtx, collections.Join(req.User, depositID)); err != nil {
		return nil, fmt.Errorf("index deposit by user: %w", err)
	}
	if _, err := k.ensureDepositSettlement(cacheCtx, deposit, lacCoin, req.SettlementChannelID, req.SettlementPortID, req.RequestID); err != nil {
		return nil, err
	}

	cacheCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_deposit_created",
			sdk.NewAttribute("deposit_id", depositID),
			sdk.NewAttribute("user", req.User),
			sdk.NewAttribute("amount", req.Amount.String()),
		),
	)
	if pricing != nil {
		cacheCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"payment_rails_pricing_applied",
				sdk.NewAttribute("deposit_id", depositID),
				sdk.NewAttribute("oracle_price", pricing.OraclePrice),
			),
		)
	}
	cacheCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_mint_completed",
			sdk.NewAttribute("deposit_id", depositID),
			sdk.NewAttribute("lac_minted", lacCoin.String()),
		),
	)
	write()
	sdkCtx.EventManager().EmitEvents(cacheCtx.EventManager().Events())

	return deposit, nil
}

// RequestWithdraw burns LAC and records a withdrawal request.
func (k *Keeper) RequestWithdraw(ctx context.Context, req types.WithdrawRequest) (*types.WithdrawRecord, error) {
	if k.creditsKeeper == nil || k.oracleKeeper == nil {
		return nil, types.ErrInvalidRequest.Wrap("missing keeper dependencies")
	}
	if strings.TrimSpace(req.User) == "" {
		return nil, types.ErrInvalidRequest.Wrap("user required")
	}
	if !req.LacAmount.IsValid() || !req.LacAmount.Amount.IsPositive() {
		return nil, types.ErrInvalidAmount
	}
	if err := sdk.ValidateDenom(req.LacAmount.Denom); err != nil {
		return nil, types.ErrInvalidDenom.Wrap(err.Error())
	}
	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, types.ErrInvalidDenom.Wrap(err.Error())
	}
	if err := validateKeeperID("request_id", req.RequestID); err != nil {
		return nil, err
	}
	if err := validateSettlementRoute(req.SettlementChannelID, req.SettlementPortID); err != nil {
		return nil, err
	}

	if existingID, err := k.state.WithdrawByRequest.Get(ctx, req.RequestID); err == nil && existingID != "" {
		existing, err := k.GetWithdraw(ctx, existingID)
		if err != nil {
			return nil, err
		}
		existingLacBurned := types.CoinFromProto(existing.LacBurned)
		if existing.User != req.User || existing.Denom != req.Denom || !existingLacBurned.Amount.Equal(req.LacAmount.Amount) {
			return nil, types.ErrDuplicateRequest
		}
		return existing, nil
	}

	params := k.GetParams(ctx)
	if params.PauseConversions {
		return nil, types.ErrPaused
	}
	if !params.IsAcceptedDenom(req.Denom) {
		return nil, types.ErrUnsupportedDenom
	}
	if req.LacAmount.Denom != params.CreditDenom {
		return nil, types.ErrInvalidDenom.Wrapf("expected %s", params.CreditDenom)
	}

	price, err := k.lookupPrice(ctx, req.Denom)
	if err != nil {
		return nil, err
	}

	assetCoin, lacConsumed, pricing, err := k.computeAssetRelease(ctx, req.Denom, req.LacAmount, price, req.QuotedPrice)
	if err != nil {
		return nil, err
	}
	_ = pricing // pricing is used for audit but not stored for withdrawals currently

	userAddr, err := sdk.AccAddressFromBech32(req.User)
	if err != nil {
		return nil, types.ErrInvalidRequest.Wrap("invalid user address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return nil, types.ErrInvalidRequest.Wrap("block time not set")
	}

	cacheCtx, write := sdkCtx.CacheContext()

	// Burn only the LAC actually consumed by the released asset; the
	// (sub-unit) truncation residual stays in the user's credit balance
	// rather than being silently confiscated by the protocol.
	if err := k.creditsKeeper.BurnCreditsFromAccount(cacheCtx, userAddr, lacConsumed, "payment_rails_withdraw"); err != nil {
		return nil, types.ErrInsufficientCredits.Wrap(err.Error())
	}

	seq, err := k.state.WithdrawSeq.Next(cacheCtx)
	if err != nil {
		return nil, fmt.Errorf("allocate withdraw id: %w", err)
	}
	withdrawID := fmt.Sprintf("wd-%d", seq)

	record := &types.WithdrawRecord{
		WithdrawId:    withdrawID,
		User:          req.User,
		Denom:         req.Denom,
		LacBurned:     types.CoinToProto(lacConsumed),
		AssetReleased: types.CoinToProto(assetCoin),
		Status:        types.WithdrawStatus_WITHDRAW_STATUS_REQUESTED,
		RequestId:     req.RequestID,
		RequestedAt:   now,
		CompletedAt:   time.Time{},
	}

	if err := k.state.Withdrawals.Set(cacheCtx, withdrawID, record); err != nil {
		return nil, err
	}
	if err := k.state.WithdrawByRequest.Set(cacheCtx, req.RequestID, withdrawID); err != nil {
		return nil, fmt.Errorf("index withdraw by request: %w", err)
	}
	if err := k.state.WithdrawalsByUser.Set(cacheCtx, collections.Join(req.User, withdrawID)); err != nil {
		return nil, fmt.Errorf("index withdraw by user: %w", err)
	}
	if _, err := k.ensureWithdrawSettlement(cacheCtx, record, req.SettlementChannelID, req.SettlementPortID, req.RequestID); err != nil {
		return nil, err
	}

	cacheCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_withdraw_requested",
			sdk.NewAttribute("withdraw_id", withdrawID),
			sdk.NewAttribute("lac_burned", lacConsumed.String()),
			sdk.NewAttribute("asset_release", assetCoin.String()),
		),
	)
	write()
	sdkCtx.EventManager().EmitEvents(cacheCtx.EventManager().Events())

	return record, nil
}

// FinalizeWithdraw marks a withdrawal as completed (governance controlled).
func (k *Keeper) FinalizeWithdraw(ctx context.Context, authority string, withdrawID string) (*types.WithdrawRecord, error) {
	if k.bankKeeper == nil {
		return nil, types.ErrInvalidRequest.Wrap("missing keeper dependencies")
	}
	if err := k.assertAuthority(authority); err != nil {
		return nil, err
	}
	record, err := k.GetWithdraw(ctx, withdrawID)
	if err != nil {
		return nil, err
	}
	if record.Status == types.WithdrawStatus_WITHDRAW_STATUS_COMPLETED {
		return record, nil
	}
	if record.Status != types.WithdrawStatus_WITHDRAW_STATUS_REQUESTED {
		return nil, types.ErrInvalidState.Wrapf("withdrawal is not in requested state (current: %s)", record.Status)
	}
	params := k.GetParams(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	withdrawDelay, err := types.WithdrawDelayDuration(params.WithdrawDelaySec)
	if err != nil {
		return nil, types.ErrInvalidState.Wrap(err.Error())
	}
	if withdrawDelay > 0 {
		requestedAt := types.ProtoTimestampOrZero(record.RequestedAt)
		readyAt := requestedAt.Add(withdrawDelay)
		if now.Before(readyAt) {
			return nil, types.ErrInvalidState.Wrap("withdrawal delay not elapsed")
		}
	}

	record.Status = types.WithdrawStatus_WITHDRAW_STATUS_COMPLETED
	record.CompletedAt = now

	assetReleased := types.CoinFromProto(record.AssetReleased)
	if assetReleased.IsPositive() {
		userAddr, err := sdk.AccAddressFromBech32(record.User)
		if err != nil {
			return nil, fmt.Errorf("invalid user address: %w", err)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, userAddr, sdk.NewCoins(assetReleased)); err != nil {
			return nil, fmt.Errorf("failed to release assets: %w", err)
		}
	}

	if err := k.state.Withdrawals.Set(ctx, withdrawID, record); err != nil {
		return nil, err
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_withdraw_completed",
			sdk.NewAttribute("withdraw_id", withdrawID),
			sdk.NewAttribute("user", record.User),
		),
	)
	return record, nil
}

// RefundDeposit marks a deposit as refunded (governance controlled).
func (k *Keeper) RefundDeposit(ctx context.Context, authority string, depositID string) (*types.DepositRecord, error) {
	if k.creditsKeeper == nil || k.bankKeeper == nil {
		return nil, types.ErrInvalidRequest.Wrap("missing keeper dependencies")
	}
	if err := k.assertAuthority(authority); err != nil {
		return nil, err
	}
	deposit, err := k.GetDeposit(ctx, depositID)
	if err != nil {
		return nil, err
	}
	if deposit.Status == types.DepositStatus_DEPOSIT_STATUS_REFUNDED {
		return deposit, nil
	}
	mint, err := k.state.Mints.Get(ctx, depositID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			if deposit.Status == types.DepositStatus_DEPOSIT_STATUS_MINTED || deposit.Status == types.DepositStatus_DEPOSIT_STATUS_FINALIZED {
				return nil, types.ErrInvalidState.Wrapf("missing mint record for deposit %s", depositID)
			}
		} else {
			return nil, types.ErrInvalidRequest.Wrapf("failed to load mint record for deposit %s: %v", depositID, err)
		}
	}
	if err == nil && mint == nil && (deposit.Status == types.DepositStatus_DEPOSIT_STATUS_MINTED || deposit.Status == types.DepositStatus_DEPOSIT_STATUS_FINALIZED) {
		return nil, types.ErrInvalidState.Wrapf("missing mint record for deposit %s", depositID)
	}
	if err == nil && mint != nil {
		userAddr, addrErr := sdk.AccAddressFromBech32(deposit.User)
		if addrErr != nil {
			return nil, types.ErrInvalidRequest.Wrap("invalid user address")
		}
		lacCoin := types.CoinFromProto(mint.LacMinted)
		if lacCoin.IsPositive() {
			if burnErr := k.creditsKeeper.BurnCreditsFromAccount(ctx, userAddr, lacCoin, "payment_rails_refund"); burnErr != nil {
				return nil, types.ErrInsufficientCredits.Wrapf("refund burn failed: %v", burnErr)
			}
		}
	}

	// Return original deposited assets to user
	userAddr, err := sdk.AccAddressFromBech32(deposit.User)
	if err != nil {
		return nil, types.ErrInvalidRequest.Wrap("invalid user address")
	}
	// Convert proto coin to sdk.Coin
	originalAmount := types.CoinFromProto(deposit.Amount)
	if originalAmount.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, userAddr, sdk.NewCoins(originalAmount)); err != nil {
			return nil, fmt.Errorf("failed to return deposited assets: %w", err)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	deposit.Status = types.DepositStatus_DEPOSIT_STATUS_REFUNDED
	deposit.UpdatedAt = sdkCtx.BlockTime()
	if err := k.state.Deposits.Set(ctx, depositID, deposit); err != nil {
		return nil, err
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_refund_completed",
			sdk.NewAttribute("deposit_id", depositID),
			sdk.NewAttribute("user", deposit.User),
		),
	)
	return deposit, nil
}

// GetDeposit fetches a deposit by id.
func (k *Keeper) GetDeposit(ctx context.Context, depositID string) (*types.DepositRecord, error) {
	deposit, err := k.state.Deposits.Get(ctx, depositID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrDepositNotFound
		}
		return nil, err
	}
	if deposit == nil {
		return nil, types.ErrDepositNotFound
	}
	return deposit, nil
}

// GetWithdraw fetches a withdrawal by id.
func (k *Keeper) GetWithdraw(ctx context.Context, withdrawID string) (*types.WithdrawRecord, error) {
	record, err := k.state.Withdrawals.Get(ctx, withdrawID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrWithdrawNotFound
		}
		return nil, err
	}
	if record == nil {
		return nil, types.ErrWithdrawNotFound
	}
	return record, nil
}

// GetPricing fetches the pricing record for a deposit.
func (k *Keeper) GetPricing(ctx context.Context, depositID string) (*types.PricingRecord, error) {
	pricing, err := k.state.Pricings.Get(ctx, depositID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrDepositNotFound.Wrap("pricing not found")
		}
		return nil, err
	}
	return pricing, nil
}

func (k *Keeper) assertAuthority(authority string) error {
	if strings.TrimSpace(authority) == "" {
		return types.ErrUnauthorized
	}
	if authority != k.authority {
		return types.ErrUnauthorized
	}
	return nil
}

func validateKeeperID(field string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return types.ErrInvalidRequest.Wrapf("%s is required", field)
	}
	if trimmed != value {
		return types.ErrInvalidRequest.Wrapf("%s must be canonical", field)
	}
	if len(value) > types.MaxIDLen {
		return types.ErrInvalidRequest.Wrapf("%s exceeds %d-byte cap (got %d)", field, types.MaxIDLen, len(value))
	}
	return nil
}

func validateSettlementRoute(channelID string, portID string) error {
	if strings.TrimSpace(channelID) != channelID {
		return types.ErrInvalidRequest.Wrap("settlement_channel_id must be canonical")
	}
	if strings.TrimSpace(portID) != portID {
		return types.ErrInvalidRequest.Wrap("settlement_port_id must be canonical")
	}
	if channelID == "" && portID == "" {
		return nil
	}
	if channelID == "" || portID == "" {
		return types.ErrInvalidRequest.Wrap("settlement channel_id and port_id must be provided together")
	}
	return nil
}

func settlementRequestID(base string, suffix string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", base, suffix)
}

func (k *Keeper) ensureDepositSettlement(
	ctx context.Context,
	deposit *types.DepositRecord,
	creditAmount sdk.Coin,
	channelID string,
	portID string,
	requestID string,
) (*types.IBCSettlementRecord, error) {
	if err := validateSettlementRoute(channelID, portID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(channelID) == "" {
		return nil, nil
	}
	existing, err := k.GetIBCSettlementByReference(ctx, deposit.DepositId)
	if err == nil {
		if existing.PacketType != types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_DEPOSIT_FINALIZE {
			return nil, types.ErrInvalidState.Wrap("existing settlement packet type mismatch for deposit")
		}
		return existing, nil
	}
	if !errors.Is(err, types.ErrSettlementNotFound) {
		return nil, err
	}
	return k.CreateIBCSettlement(
		ctx,
		types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_DEPOSIT_FINALIZE,
		deposit.User,
		deposit.DepositId,
		types.CoinFromProto(deposit.Amount),
		creditAmount,
		settlementRequestID(requestID, "deposit"),
		channelID,
		portID,
	)
}

func (k *Keeper) ensureWithdrawSettlement(
	ctx context.Context,
	withdraw *types.WithdrawRecord,
	channelID string,
	portID string,
	requestID string,
) (*types.IBCSettlementRecord, error) {
	if err := validateSettlementRoute(channelID, portID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(channelID) == "" {
		return nil, nil
	}
	existing, err := k.GetIBCSettlementByReference(ctx, withdraw.WithdrawId)
	if err == nil {
		if existing.PacketType != types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_WITHDRAW_COMPLETE {
			return nil, types.ErrInvalidState.Wrap("existing settlement packet type mismatch for withdrawal")
		}
		return existing, nil
	}
	if !errors.Is(err, types.ErrSettlementNotFound) {
		return nil, err
	}
	return k.CreateIBCSettlement(
		ctx,
		types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_WITHDRAW_COMPLETE,
		withdraw.User,
		withdraw.WithdrawId,
		types.CoinFromProto(withdraw.AssetReleased),
		types.CoinFromProto(withdraw.LacBurned),
		settlementRequestID(requestID, "withdraw"),
		channelID,
		portID,
	)
}

func (k *Keeper) lookupPrice(ctx context.Context, denom string) (*oracletypes.AggregatedPrice, error) {
	params := k.GetParams(ctx)
	pair, ok := params.FindOraclePair(denom)
	if !ok {
		return nil, types.ErrUnsupportedDenom
	}
	price, err := k.oracleKeeper.GetAggregatedPrice(ctx, pair)
	if err != nil {
		return nil, types.ErrOraclePriceNotFound.Wrap(err.Error())
	}
	if price == nil {
		return nil, types.ErrOraclePriceNotFound
	}

	oracleStaleness, err := types.OracleStalenessDuration(params.OracleStalenessSec)
	if err != nil {
		return nil, types.ErrOracleStale.Wrap(err.Error())
	}
	if oracleStaleness > 0 {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		now := sdkCtx.BlockTime()
		if price.Timestamp.IsZero() {
			return nil, types.ErrOracleStale
		}
		if now.Sub(price.Timestamp) > oracleStaleness {
			return nil, types.ErrOracleStale
		}
	}
	return price, nil
}

func (k *Keeper) computeLacMint(ctx context.Context, amount sdk.Coin, price *oracletypes.AggregatedPrice, quotedPrice string) (sdk.Coin, sdk.Coin, *types.PricingRecord, error) {
	params := k.GetParams(ctx)
	spot, twap, err := parsePrices(price)
	if err != nil {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrPricingUnavailable.Wrap(err.Error())
	}
	deviationBps, err := deviationBps(spot, twap)
	if err != nil {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrPricingUnavailable.Wrap(err.Error())
	}
	if params.MaxOracleDeviationBps > 0 && deviationBps > params.MaxOracleDeviationBps {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrOracleDeviation
	}

	// Enforce slippage cap if quoted price was provided.
	var slippageBps uint32
	if quotedPrice != "" {
		quoted, err := sdkmath.LegacyNewDecFromStr(quotedPrice)
		if err != nil {
			return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidRequest.Wrap("invalid quoted_price format")
		}
		if !quoted.IsPositive() {
			return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidRequest.Wrap("quoted_price must be positive")
		}
		slippageBps, err = computeSlippageBps(spot, quoted)
		if err != nil {
			return sdk.Coin{}, sdk.Coin{}, nil, types.ErrPricingUnavailable.Wrap(err.Error())
		}
		if params.MaxSlippageBps > 0 && slippageBps > params.MaxSlippageBps {
			return sdk.Coin{}, sdk.Coin{}, nil, types.ErrSlippageExceeded.Wrapf(
				"slippage %d bps exceeds max %d bps (spot=%s, quoted=%s)",
				slippageBps, params.MaxSlippageBps, spot.String(), quoted.String(),
			)
		}
	}

	amountDec := sdkmath.LegacyNewDecFromInt(amount.Amount)
	usdValue := amountDec.Mul(spot)
	if !usdValue.IsPositive() {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidAmount
	}
	feeMultiplier := sdkmath.LegacyNewDec(10_000 - int64(params.AcqFeeBps)).QuoInt64(10_000)
	if feeMultiplier.IsNegative() {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidAmount
	}
	lacDec := usdValue.Mul(feeMultiplier)
	lacAmount := lacDec.TruncateInt()
	if !lacAmount.IsPositive() {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidAmount
	}
	// Derive the fee as the residual of the (single-truncated) usd value so
	// that lacAmount + feeAmount == TruncateInt(usdValue). Truncating lacDec
	// and feeDec independently loses up to ~2 integer units of value per
	// deposit; the residual form truncates only once.
	usdValueInt := usdValue.TruncateInt()
	feeAmount := usdValueInt.Sub(lacAmount)
	if feeAmount.IsNegative() {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidAmount
	}

	pricing := &types.PricingRecord{
		DepositId:   "",
		OraclePrice: spot.String(),
		TwapPrice:   twap.String(),
		QuotedPrice: quotedPrice,
		SlippageBps: slippageBps,
		Timestamp:   sdk.UnwrapSDKContext(ctx).BlockTime(),
	}

	lacCoin := sdk.NewCoin(params.CreditDenom, lacAmount)
	feeCoin := sdk.NewCoin(params.CreditDenom, feeAmount)
	return lacCoin, feeCoin, pricing, nil
}

// computeAssetRelease returns the base-asset coin to release, the LAC amount
// actually consumed (which may be less than lacAmount because base-asset
// delivery is truncated to whole units), and a pricing record.
func (k *Keeper) computeAssetRelease(ctx context.Context, denom string, lacAmount sdk.Coin, price *oracletypes.AggregatedPrice, quotedPrice string) (sdk.Coin, sdk.Coin, *types.PricingRecord, error) {
	params := k.GetParams(ctx)
	spot, twap, err := parsePrices(price)
	if err != nil {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrPricingUnavailable.Wrap(err.Error())
	}
	deviationBps, err := deviationBps(spot, twap)
	if err != nil {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrPricingUnavailable.Wrap(err.Error())
	}
	if params.MaxOracleDeviationBps > 0 && deviationBps > params.MaxOracleDeviationBps {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrOracleDeviation
	}

	// Enforce slippage cap if quoted price was provided.
	var slippageBps uint32
	if quotedPrice != "" {
		quoted, err := sdkmath.LegacyNewDecFromStr(quotedPrice)
		if err != nil {
			return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidRequest.Wrap("invalid quoted_price format")
		}
		if !quoted.IsPositive() {
			return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidRequest.Wrap("quoted_price must be positive")
		}
		slippageBps, err = computeSlippageBps(spot, quoted)
		if err != nil {
			return sdk.Coin{}, sdk.Coin{}, nil, types.ErrPricingUnavailable.Wrap(err.Error())
		}
		if params.MaxSlippageBps > 0 && slippageBps > params.MaxSlippageBps {
			return sdk.Coin{}, sdk.Coin{}, nil, types.ErrSlippageExceeded.Wrapf(
				"slippage %d bps exceeds max %d bps (spot=%s, quoted=%s)",
				slippageBps, params.MaxSlippageBps, spot.String(), quoted.String(),
			)
		}
	}

	lacDec := sdkmath.LegacyNewDecFromInt(lacAmount.Amount)
	if spot.IsZero() {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrPricingUnavailable
	}
	assetDec := lacDec.Quo(spot)
	assetAmt := assetDec.TruncateInt()
	if !assetAmt.IsPositive() {
		return sdk.Coin{}, sdk.Coin{}, nil, types.ErrInvalidAmount
	}

	// Base-asset delivery is truncated to whole units. Previously the caller
	// burned the full requested lacAmount even though the released asset was
	// worth at most assetAmt*spot (≤ lacAmount), silently confiscating the
	// rounding residual. Compute the LAC actually consumed as
	// ceil(assetAmt * spot) so the user is charged for (and never less than)
	// the value received, and the (typically sub-unit) residual stays in the
	// user's credit balance. Ceil rather than floor so the protocol can never
	// hand out asset worth more than the LAC burned.
	lacConsumedDec := sdkmath.LegacyNewDecFromInt(assetAmt).Mul(spot)
	lacConsumedInt := lacConsumedDec.Ceil().TruncateInt()
	if lacConsumedInt.GT(lacAmount.Amount) {
		// Defensive clamp — Ceil against Dec precision should not overshoot,
		// but if it ever did we must not burn more than the user requested.
		lacConsumedInt = lacAmount.Amount
	}
	lacConsumed := sdk.NewCoin(lacAmount.Denom, lacConsumedInt)

	pricing := &types.PricingRecord{
		DepositId:   "",
		OraclePrice: spot.String(),
		TwapPrice:   twap.String(),
		QuotedPrice: quotedPrice,
		SlippageBps: slippageBps,
		Timestamp:   sdk.UnwrapSDKContext(ctx).BlockTime(),
	}

	return sdk.NewCoin(denom, assetAmt), lacConsumed, pricing, nil
}

func (k *Keeper) enforceRateLimits(ctx context.Context, user string, now time.Time, lacAmount sdkmath.Int) error {
	params := k.GetParams(ctx)
	if params.MaxTopupsPerHour > 0 {
		hourStart := now.UTC().Truncate(time.Hour)
		key := collections.Join(user, hourStart)
		window, err := k.state.UserHourly.Get(ctx, key)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		if window == nil {
			window = &types.TopupWindow{Count: 0, LacTotal: "0"}
		}
		if window.Count+1 > params.MaxTopupsPerHour {
			return types.ErrRateLimitExceeded
		}
		window.Count++
		// A malformed LacTotal indicates corrupted rate-limit state. Silently
		// resetting it to zero would reopen the hourly budget for abuse, so
		// fail closed and surface the error instead.
		currentTotal, ok := sdkmath.NewIntFromString(window.LacTotal)
		if !ok {
			return fmt.Errorf("rate limit state corrupt: invalid hourly lac_total %q for user %s", window.LacTotal, user)
		}
		window.LacTotal = currentTotal.Add(lacAmount).String()
		if err := k.state.UserHourly.Set(ctx, key, window); err != nil {
			return err
		}
	}

	maxLacPerDay := params.MaxLacPerDayInt()
	if maxLacPerDay.IsPositive() {
		dayStart := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
		key := collections.Join(user, dayStart)
		window, err := k.state.UserDaily.Get(ctx, key)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		if window == nil {
			window = &types.TopupWindow{Count: 0, LacTotal: "0"}
		}
		currentTotal, ok := sdkmath.NewIntFromString(window.LacTotal)
		if !ok {
			return fmt.Errorf("rate limit state corrupt: invalid daily lac_total %q for user %s", window.LacTotal, user)
		}
		if currentTotal.Add(lacAmount).GT(maxLacPerDay) {
			return types.ErrRateLimitExceeded
		}
		window.LacTotal = currentTotal.Add(lacAmount).String()
		window.Count++
		if err := k.state.UserDaily.Set(ctx, key, window); err != nil {
			return err
		}
	}
	return nil
}

func parsePrices(price *oracletypes.AggregatedPrice) (sdkmath.LegacyDec, sdkmath.LegacyDec, error) {
	if price == nil {
		return sdkmath.LegacyDec{}, sdkmath.LegacyDec{}, fmt.Errorf("price is nil")
	}
	spotStr := strings.TrimSpace(price.MedianPrice)
	if spotStr == "" {
		spotStr = strings.TrimSpace(price.MeanPrice)
	}
	if spotStr == "" {
		return sdkmath.LegacyDec{}, sdkmath.LegacyDec{}, fmt.Errorf("missing oracle price")
	}
	spot, err := sdkmath.LegacyNewDecFromStr(spotStr)
	if err != nil {
		return sdkmath.LegacyDec{}, sdkmath.LegacyDec{}, err
	}
	if !spot.IsPositive() {
		return sdkmath.LegacyDec{}, sdkmath.LegacyDec{}, fmt.Errorf("price must be positive")
	}
	// Use mean as a TWAP proxy when available.
	twapStr := strings.TrimSpace(price.MeanPrice)
	if twapStr == "" {
		twapStr = spotStr
	}
	twap, err := sdkmath.LegacyNewDecFromStr(twapStr)
	if err != nil {
		return sdkmath.LegacyDec{}, sdkmath.LegacyDec{}, err
	}
	if !twap.IsPositive() {
		return sdkmath.LegacyDec{}, sdkmath.LegacyDec{}, fmt.Errorf("twap must be positive")
	}
	return spot, twap, nil
}

func deviationBps(spot, twap sdkmath.LegacyDec) (uint32, error) {
	if twap.IsZero() {
		return 0, fmt.Errorf("twap is zero")
	}
	diff := spot.Sub(twap)
	if diff.IsNegative() {
		diff = diff.Neg()
	}
	bps := diff.Quo(twap).MulInt64(10_000).TruncateInt64()
	if bps < 0 {
		bps = 0
	}
	if bps > 10000 {
		bps = 10000
	}
	return uint32(bps), nil
}

// computeSlippageBps calculates slippage between actual (spot) and expected (quoted) prices.
// Slippage = |spot - quoted| / quoted * 10000 (in basis points).
func computeSlippageBps(spot, quoted sdkmath.LegacyDec) (uint32, error) {
	if quoted.IsZero() {
		return 0, fmt.Errorf("quoted price is zero")
	}
	diff := spot.Sub(quoted)
	if diff.IsNegative() {
		diff = diff.Neg()
	}
	bps := diff.Quo(quoted).MulInt64(10_000).TruncateInt64()
	if bps < 0 {
		bps = 0
	}
	if bps > 10000 {
		bps = 10000
	}
	return uint32(bps), nil
}

func (k *Keeper) deriveCreditDenom(ctx context.Context) string {
	if k.creditsKeeper == nil {
		return creditstypes.DefaultCreditDenom
	}
	params := k.creditsKeeper.GetParams(ctx)
	if params == nil || params.CreditDenom == "" {
		return creditstypes.DefaultCreditDenom
	}
	return params.CreditDenom
}
