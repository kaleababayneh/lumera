package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	"github.com/cosmos/gogoproto/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

type queryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

// NewQueryServer constructs a Query service implementation.
func NewQueryServer(k *Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

func (q *queryServer) requireKeeper() (*Keeper, error) {
	if q == nil || q.keeper == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "credits keeper not initialized")
	}
	return q.keeper, nil
}

func validateQueryRequiredID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return grpcstatus.Errorf(codes.InvalidArgument, "%s is required", field)
	}
	if trimmed != value {
		return grpcstatus.Errorf(codes.InvalidArgument, "%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func validateOptionalQueryIDFilter(field, value string) error {
	if value == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return grpcstatus.Errorf(codes.InvalidArgument, "%s must not be blank when provided", field)
	}
	if trimmed != value {
		return grpcstatus.Errorf(codes.InvalidArgument, "%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func (q *queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	params := keeper.GetParams(sdkCtx)
	return &types.QueryParamsResponse{Params: cloneParams(params)}, nil
}

func (q *queryServer) Lock(goCtx context.Context, req *types.QueryLockRequest) (*types.QueryLockResponse, error) {
	if req == nil {
		return nil, grpcstatus.Error(codes.InvalidArgument, "lock_id is required")
	}
	if err := validateQueryRequiredID("lock_id", req.LockId); err != nil {
		return nil, err
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	lock, found := keeper.GetLock(sdkCtx, req.LockId)
	if !found {
		return nil, fmt.Errorf("lock %s not found", req.LockId)
	}
	return &types.QueryLockResponse{Lock: cloneLock(lock)}, nil
}

func (q *queryServer) Hold(goCtx context.Context, req *types.QueryHoldRequest) (*types.QueryHoldResponse, error) {
	if req == nil {
		return nil, grpcstatus.Error(codes.InvalidArgument, "hold_id is required")
	}
	if err := validateQueryRequiredID("hold_id", req.HoldId); err != nil {
		return nil, err
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	lock, found := keeper.GetLock(sdkCtx, req.HoldId)
	if !found {
		return nil, grpcstatus.Errorf(codes.NotFound, "hold %s not found", req.HoldId)
	}

	hold, err := q.buildHoldView(sdkCtx, lock)
	if err != nil {
		return nil, err
	}
	return &types.QueryHoldResponse{Hold: hold}, nil
}

// Locks returns paginated locks with optional filtering by router.
// Uses Collections range iteration for efficient, deterministic ordering.
func (q *queryServer) Locks(goCtx context.Context, req *types.QueryLocksRequest) (resp *types.QueryLocksResponse, err error) {
	if req == nil {
		req = &types.QueryLocksRequest{}
	}

	// Parse pagination parameters
	var (
		offset     uint64
		limit      uint64 = 50
		countTotal bool
		startKey   string
	)
	const maxLimit uint64 = 1000
	if req.Pagination != nil {
		offset = req.Pagination.Offset
		if req.Pagination.Limit > 0 {
			limit = req.Pagination.Limit
		}
		if limit > maxLimit {
			limit = maxLimit
		}
		countTotal = req.Pagination.CountTotal
		if req.Pagination.Reverse {
			return nil, grpcstatus.Error(codes.InvalidArgument, "reverse pagination not supported")
		}
		if len(req.Pagination.Key) > 0 {
			if offset > 0 {
				return nil, grpcstatus.Error(codes.InvalidArgument, "pagination key and offset are mutually exclusive")
			}
			// Key is the lock ID to start from (exclusive)
			startKey = string(req.Pagination.Key)
		}
	}
	if err := validateOptionalQueryIDFilter("router", req.Router); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	// Build range for iteration
	var rng collections.Ranger[string]
	if startKey != "" {
		// Start after the given key for cursor-based pagination
		rng = new(collections.Range[string]).StartExclusive(startKey)
	}

	// Iterate using Collections' native iteration (already ordered by key)
	iter, err := keeper.state.Locks.Iterate(sdkCtx, rng)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate locks: %w", err)
	}
	defer func() {
		if cerr := iter.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close locks iterator: %w", cerr)
		}
	}()

	locks := make([]*types.Lock, 0)
	var (
		total   uint64 // Total matching filter criteria
		skipped uint64 // Items skipped for offset
		emitted uint64 // Items added to result
		lastKey string // Last key added to results
		hasMore bool   // Whether there are more results
	)

	for ; iter.Valid(); iter.Next() {
		lock, err := iter.Value()
		if err != nil {
			return nil, fmt.Errorf("failed to read lock: %w", err)
		}
		if lock == nil {
			continue
		}

		// Apply router filter
		if req.Router != "" && lock.Router != req.Router {
			continue
		}

		// Handle offset (only when not using key-based pagination)
		if startKey == "" && skipped < offset {
			skipped++
			total++ // Count for total but skip for results
			continue
		}

		// Check if we've hit the limit
		if emitted >= limit {
			hasMore = true
			// Don't count this item here - it will be counted in countTotal loop
			break
		}

		locks = append(locks, lock)
		lastKey = lock.LockId
		emitted++
		total++
	}

	// If countTotal requested, we need to count remaining items
	if countTotal && hasMore {
		for ; iter.Valid(); iter.Next() {
			lock, err := iter.Value()
			if err != nil {
				return nil, fmt.Errorf("failed to read lock during count: %w", err)
			}
			if lock == nil {
				continue
			}
			// Apply same filter
			if req.Router != "" && lock.Router != req.Router {
				continue
			}
			total++
		}
	}

	// Build pagination response
	var pageRes *query.PageResponse
	if req.Pagination != nil {
		pageRes = &query.PageResponse{}
		if countTotal {
			pageRes.Total = total
		}
		if hasMore && lastKey != "" {
			pageRes.NextKey = []byte(lastKey)
		}
	}

	return &types.QueryLocksResponse{Locks: cloneLocks(locks), Pagination: pageRes}, nil
}

// Holds returns joined hold views with optional filtering by router, session,
// and active status. Ordering and pagination follow the underlying lock IDs.
func (q *queryServer) Holds(goCtx context.Context, req *types.QueryHoldsRequest) (resp *types.QueryHoldsResponse, err error) {
	if req == nil {
		req = &types.QueryHoldsRequest{}
	}

	var (
		offset     uint64
		limit      uint64 = 50
		countTotal bool
		startKey   string
	)
	const maxLimit uint64 = 1000
	if req.Pagination != nil {
		offset = req.Pagination.Offset
		if req.Pagination.Limit > 0 {
			limit = req.Pagination.Limit
		}
		if limit > maxLimit {
			limit = maxLimit
		}
		countTotal = req.Pagination.CountTotal
		if req.Pagination.Reverse {
			return nil, grpcstatus.Error(codes.InvalidArgument, "reverse pagination not supported")
		}
		if len(req.Pagination.Key) > 0 {
			if offset > 0 {
				return nil, grpcstatus.Error(codes.InvalidArgument, "pagination key and offset are mutually exclusive")
			}
			startKey = string(req.Pagination.Key)
		}
	}
	if err := validateOptionalQueryIDFilter("router", req.Router); err != nil {
		return nil, err
	}
	if err := validateOptionalQueryIDFilter("session_id", req.SessionId); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	var rng collections.Ranger[string]
	if startKey != "" {
		rng = new(collections.Range[string]).StartExclusive(startKey)
	}

	iter, err := keeper.state.Locks.Iterate(sdkCtx, rng)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate holds: %w", err)
	}
	defer func() {
		if cerr := iter.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close holds iterator: %w", cerr)
		}
	}()

	holds := make([]*types.HoldView, 0)
	var (
		total   uint64
		skipped uint64
		emitted uint64
		lastKey string
		hasMore bool
	)

	for ; iter.Valid(); iter.Next() {
		lock, err := iter.Value()
		if err != nil {
			return nil, fmt.Errorf("failed to read hold lock: %w", err)
		}
		if !matchesHoldFilters(lock, req) {
			continue
		}

		if startKey == "" && skipped < offset {
			skipped++
			total++
			continue
		}

		if emitted >= limit {
			hasMore = true
			break
		}

		hold, err := q.buildHoldView(sdkCtx, lock)
		if err != nil {
			return nil, err
		}

		holds = append(holds, hold)
		lastKey = lock.LockId
		emitted++
		total++
	}

	if countTotal && hasMore {
		for ; iter.Valid(); iter.Next() {
			lock, err := iter.Value()
			if err != nil {
				return nil, fmt.Errorf("failed to read hold lock during count: %w", err)
			}
			if !matchesHoldFilters(lock, req) {
				continue
			}
			total++
		}
	}

	var pageRes *query.PageResponse
	if req.Pagination != nil {
		pageRes = &query.PageResponse{}
		if countTotal {
			pageRes.Total = total
		}
		if hasMore && lastKey != "" {
			pageRes.NextKey = []byte(lastKey)
		}
	}

	return &types.QueryHoldsResponse{Holds: holds, Pagination: pageRes}, nil
}

func matchesHoldFilters(lock *types.Lock, req *types.QueryHoldsRequest) bool {
	if lock == nil {
		return false
	}
	if req.Router != "" && lock.Router != req.Router {
		return false
	}
	if req.SessionId != "" && lock.SessionId != req.SessionId {
		return false
	}
	if req.ActiveOnly && lock.Status != types.LockStatus_LOCK_STATUS_ACTIVE {
		return false
	}
	return true
}

// deepCopyProto returns an independent copy of a gogo message via a
// marshal/unmarshal round-trip. proto.Clone must NOT be used: its reflective
// table-merge panics ("merger not found for type:big.Word") on gogoproto
// customtype fields (e.g. sdk.Coin.Amount = math.Int) once the message carries a
// populated value — which would crash every Lock/Locks query in production.
func deepCopyProto(src, dst proto.Message) bool {
	bz, err := proto.Marshal(src)
	if err != nil {
		return false
	}
	return proto.Unmarshal(bz, dst) == nil
}

func cloneParams(params *types.Params) *types.Params {
	if params == nil {
		return nil
	}
	out := &types.Params{}
	if !deepCopyProto(params, out) {
		return params
	}
	return out
}

func cloneLock(lock *types.Lock) *types.Lock {
	if lock == nil {
		return nil
	}
	out := &types.Lock{}
	if !deepCopyProto(lock, out) {
		return lock
	}
	return out
}

func cloneLocks(locks []*types.Lock) []*types.Lock {
	if len(locks) == 0 {
		return locks
	}
	cloned := make([]*types.Lock, 0, len(locks))
	for _, lock := range locks {
		cloned = append(cloned, cloneLock(lock))
	}
	return cloned
}

func (q *queryServer) buildHoldView(ctx sdk.Context, lock *types.Lock) (*types.HoldView, error) {
	if lock == nil {
		return nil, grpcstatus.Error(codes.Internal, "hold lock cannot be nil")
	}

	creditDenom := types.DefaultCreditDenom
	if params := q.keeper.GetParams(ctx); params != nil && params.CreditDenom != "" {
		creditDenom = params.CreditDenom
	}

	reservedAmount, currency, err := holdCostValueFromProtoCoin(lock.GetAmount(), creditDenom)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Internal, "invalid reserved amount for hold %s: %v", lock.LockId, err)
	}

	view := &types.HoldView{
		HoldId:           lock.LockId,
		RouterId:         lock.Router,
		SessionId:        lock.SessionId,
		ToolId:           lock.ToolId,
		QuoteId:          lock.QuoteId,
		PolicyVersion:    lock.PolicyVersion,
		IntentHash:       lock.IntentHash,
		ToolpackId:       lock.ToolpackId,
		CreatedAtRfc3339: timestampRFC3339(lock.GetCreatedAt()),
		ExpiresAtRfc3339: timestampRFC3339(lock.GetExpiresAt()),
		Status:           holdStatusFromLockStatus(lock.Status),
		LastError:        lock.LastError,
		Economics: &types.HoldEconomicsContext{
			ReservedAmount: newHoldCost(reservedAmount, currency),
		},
	}

	var (
		chargedAmount decimal.Decimal
		hasCharged    bool
	)

	receiptID, err := q.keeper.state.LockReceipts.Get(ctx, lock.LockId)
	switch {
	case err == nil:
		settlementCtx := &types.HoldSettlementContext{
			ReceiptId: receiptID,
			Status:    types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_UNKNOWN,
		}

		if settlement, found := q.keeper.GetSettlement(ctx, receiptID); found {
			settlementCtx.SettlementId = settlement.Id
			settlementCtx.Status = holdSettlementStatusFromSettlement(settlement.Status)
			settlementCtx.Stage = settlement.Stage
			settlementCtx.CacheHit = settlement.CacheHit
			settlementCtx.ActionId = settlement.ActionId
			settlementCtx.DisputeId = settlement.DisputeId
			settlementCtx.FailureReason = settlement.FailureReason
			settlementCtx.FillCount = settlement.FillCount
			settlementCtx.CompletedAtRfc3339 = timestampPtrRFC3339(settlement.GetCompletedAt())

			if amount, _, ok, err := holdCostValueFromProtoCoins(settlement.GetTotalCost(), creditDenom); err != nil {
				return nil, grpcstatus.Errorf(codes.Internal, "invalid charged amount for hold %s: %v", lock.LockId, err)
			} else if ok {
				chargedAmount = amount
				hasCharged = true
				view.Economics.ChargedAmount = newHoldCost(amount, currency)
			} else {
				chargedAmount = decimal.Zero
				hasCharged = true
				view.Economics.ChargedAmount = newHoldCost(decimal.Zero, currency)
			}
			if amount, _, ok, err := holdCostValueFromProtoCoins(settlement.GetBurnAmount(), creditDenom); err != nil {
				return nil, grpcstatus.Errorf(codes.Internal, "invalid burned amount for hold %s: %v", lock.LockId, err)
			} else if ok {
				view.Economics.BurnedAmount = newHoldCost(amount, currency)
			} else {
				view.Economics.BurnedAmount = newHoldCost(decimal.Zero, currency)
			}
			if amount, _, ok, err := holdCostValueFromProtoCoins(settlement.GetNetAmount(), creditDenom); err != nil {
				return nil, grpcstatus.Errorf(codes.Internal, "invalid net amount for hold %s: %v", lock.LockId, err)
			} else if ok {
				view.Economics.NetAmount = newHoldCost(amount, currency)
			} else {
				view.Economics.NetAmount = newHoldCost(decimal.Zero, currency)
			}
		}

		view.Settlement = settlementCtx
	case errors.Is(err, collections.ErrNotFound):
	default:
		return nil, grpcstatus.Errorf(codes.Internal, "failed to load hold receipt binding for %s: %v", lock.LockId, err)
	}

	remainingAmount := decimal.Zero
	if lock.Status == types.LockStatus_LOCK_STATUS_ACTIVE {
		remainingAmount = reservedAmount
		if hasCharged {
			remainingAmount = reservedAmount.Sub(chargedAmount)
			if remainingAmount.IsNegative() {
				remainingAmount = decimal.Zero
			}
		}
	}
	view.Economics.RemainingReservedAmount = newHoldCost(remainingAmount, currency)

	if lock.Status != types.LockStatus_LOCK_STATUS_ACTIVE {
		refundAmount := reservedAmount
		if hasCharged {
			refundAmount = reservedAmount.Sub(chargedAmount)
			if refundAmount.IsNegative() {
				refundAmount = decimal.Zero
			}
		}
		if refundAmount.IsPositive() {
			view.Economics.RefundAmount = newHoldCost(refundAmount, currency)
		}
	}

	return view, nil
}

func holdStatusFromLockStatus(status types.LockStatus) types.HoldStatus {
	switch status {
	case types.LockStatus_LOCK_STATUS_ACTIVE:
		return types.HoldStatus_HOLD_STATUS_HELD
	case types.LockStatus_LOCK_STATUS_BURNED:
		return types.HoldStatus_HOLD_STATUS_CAPTURED
	case types.LockStatus_LOCK_STATUS_RELEASED:
		return types.HoldStatus_HOLD_STATUS_RELEASED
	case types.LockStatus_LOCK_STATUS_EXPIRED:
		return types.HoldStatus_HOLD_STATUS_EXPIRED
	default:
		return types.HoldStatus_HOLD_STATUS_UNSPECIFIED
	}
}

func holdSettlementStatusFromSettlement(status types.SettlementStatus) types.HoldSettlementStatus {
	switch status {
	case types.SettlementStatus_SETTLEMENT_STATUS_PENDING:
		return types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_PENDING
	case types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED:
		return types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_COMPLETED
	case types.SettlementStatus_SETTLEMENT_STATUS_FAILED:
		return types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_FAILED
	case types.SettlementStatus_SETTLEMENT_STATUS_DISPUTED:
		return types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_DISPUTED
	case types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED:
		return types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_REFUNDED
	default:
		return types.HoldSettlementStatus_HOLD_SETTLEMENT_STATUS_UNKNOWN
	}
}

func newHoldCost(amount decimal.Decimal, currency string) *types.HoldCost {
	return &types.HoldCost{
		Amount:   amount.String(),
		Currency: currency,
	}
}

func holdCostValueFromProtoCoins(coins sdk.Coins, creditDenom string) (decimal.Decimal, string, bool, error) {
	for _, coin := range coins {
		if coin.Denom == creditDenom {
			amount, currency, err := holdCostValueFromProtoCoin(coin, creditDenom)
			return amount, currency, true, err
		}
	}
	for _, coin := range coins {
		amount, currency, err := holdCostValueFromProtoCoin(coin, creditDenom)
		return amount, currency, true, err
	}
	return decimal.Zero, "", false, nil
}

func holdCostValueFromProtoCoin(coin sdk.Coin, creditDenom string) (decimal.Decimal, string, error) {
	if coin.Amount.IsNil() {
		return decimal.Zero, "LAC", nil
	}

	sdkCoin, err := types.CoinFromProtoSafe(coin)
	if err != nil {
		return decimal.Zero, "", err
	}

	amount := decimal.NewFromBigInt(sdkCoin.Amount.BigInt(), 0)
	currency := sdkCoin.Denom
	if currency == "" {
		currency = "LAC"
	}
	if creditDenom != "" && sdkCoin.Denom == creditDenom {
		amount = amount.Div(decimal.NewFromInt(lacPrecision))
		currency = "LAC"
	}
	return amount, currency, nil
}

func timestampRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func timestampPtrRFC3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return timestampRFC3339(*t)
}
