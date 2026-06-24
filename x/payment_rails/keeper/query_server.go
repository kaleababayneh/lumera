package keeper

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/gogoproto/proto"

	"github.com/LumeraProtocol/lumera/x/payment_rails/types"
)

const (
	defaultPaymentQueryLimit = 100
	// maxPaymentQueryLimit caps records returned by list queries to prevent DoS.
	maxPaymentQueryLimit = 1000
)

type queryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

// NewQueryServerImpl returns an implementation of the payment_rails Query service.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

func (s *queryServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("payment_rails keeper not initialized")
	}
	return s.keeper, nil
}

func validatePaymentQueryID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return types.ErrInvalidRequest.Wrapf("%s required", field)
	}
	if trimmed != value {
		return types.ErrInvalidRequest.Wrapf("%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > types.MaxIDLen {
		return types.ErrInvalidRequest.Wrapf("%s exceeds %d-byte cap (got %d)", field, types.MaxIDLen, len(value))
	}
	return nil
}

// Params returns the module parameters.
func (s *queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	params := keeper.GetParams(ctx)
	return &types.QueryParamsResponse{Params: cloneParams(params)}, nil
}

// Deposit returns a deposit by ID.
func (s *queryServer) Deposit(ctx context.Context, req *types.QueryDepositRequest) (*types.QueryDepositResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := validatePaymentQueryID("deposit_id", req.DepositId); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	deposit, err := keeper.GetDeposit(ctx, req.DepositId)
	if err != nil {
		return nil, err
	}
	return &types.QueryDepositResponse{Deposit: cloneDepositRecord(deposit)}, nil
}

// Deposits returns deposits with optional filters.
func (s *queryServer) Deposits(ctx context.Context, req *types.QueryDepositsRequest) (*types.QueryDepositsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	pagination, err := newPaymentPagination(req.Pagination)
	if err != nil {
		return nil, err
	}
	if err := validateDepositStatusFilter(req.Status); err != nil {
		return nil, err
	}
	if req.User != "" {
		if _, err := sdk.AccAddressFromBech32(req.User); err != nil {
			return nil, types.ErrInvalidRequest.Wrap("invalid user address")
		}
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	deposits := make([]*types.DepositRecord, 0, int(pagination.limit))
	var matched uint64
	var hasMore bool
	if req.User != "" {
		prefix := collections.NewPrefixedPairRange[string, string](req.User)
		err = keeper.state.DepositsByUser.Walk(ctx, prefix, func(key collections.Pair[string, string]) (bool, error) {
			depositID := key.K2()
			deposit, err := keeper.state.Deposits.Get(ctx, depositID)
			if err != nil {
				return true, err
			}
			if !depositMatchesQuery(deposit, req.Status) {
				return false, nil
			}
			return collectPaymentPageRecord(&deposits, pagination, &matched, &hasMore, deposit), nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		err = keeper.state.Deposits.Walk(ctx, nil, func(_ string, deposit *types.DepositRecord) (bool, error) {
			if !depositMatchesQuery(deposit, req.Status) {
				return false, nil
			}
			return collectPaymentPageRecord(&deposits, pagination, &matched, &hasMore, deposit), nil
		})
		if err != nil {
			return nil, err
		}
	}

	return &types.QueryDepositsResponse{
		Deposits:   cloneDepositRecords(deposits),
		Pagination: pagination.response(len(deposits), matched, hasMore),
	}, nil
}

// Withdraw returns a withdrawal by ID.
func (s *queryServer) Withdraw(ctx context.Context, req *types.QueryWithdrawRequest) (*types.QueryWithdrawResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := validatePaymentQueryID("withdraw_id", req.WithdrawId); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	withdraw, err := keeper.GetWithdraw(ctx, req.WithdrawId)
	if err != nil {
		return nil, err
	}
	return &types.QueryWithdrawResponse{Withdraw: cloneWithdrawRecord(withdraw)}, nil
}

// Withdrawals returns withdrawals with optional filters.
func (s *queryServer) Withdrawals(ctx context.Context, req *types.QueryWithdrawalsRequest) (*types.QueryWithdrawalsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	pagination, err := newPaymentPagination(req.Pagination)
	if err != nil {
		return nil, err
	}
	if err := validateWithdrawStatusFilter(req.Status); err != nil {
		return nil, err
	}
	if req.User != "" {
		if _, err := sdk.AccAddressFromBech32(req.User); err != nil {
			return nil, types.ErrInvalidRequest.Wrap("invalid user address")
		}
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	withdrawals := make([]*types.WithdrawRecord, 0, int(pagination.limit))
	var matched uint64
	var hasMore bool
	if req.User != "" {
		prefix := collections.NewPrefixedPairRange[string, string](req.User)
		err = keeper.state.WithdrawalsByUser.Walk(ctx, prefix, func(key collections.Pair[string, string]) (bool, error) {
			withdrawID := key.K2()
			withdraw, err := keeper.state.Withdrawals.Get(ctx, withdrawID)
			if err != nil {
				return true, err
			}
			if !withdrawMatchesQuery(withdraw, req.Status) {
				return false, nil
			}
			return collectPaymentPageRecord(&withdrawals, pagination, &matched, &hasMore, withdraw), nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		err = keeper.state.Withdrawals.Walk(ctx, nil, func(_ string, withdraw *types.WithdrawRecord) (bool, error) {
			if !withdrawMatchesQuery(withdraw, req.Status) {
				return false, nil
			}
			return collectPaymentPageRecord(&withdrawals, pagination, &matched, &hasMore, withdraw), nil
		})
		if err != nil {
			return nil, err
		}
	}

	return &types.QueryWithdrawalsResponse{
		Withdrawals: cloneWithdrawRecords(withdrawals),
		Pagination:  pagination.response(len(withdrawals), matched, hasMore),
	}, nil
}

// Pricing returns pricing record for a deposit.
func (s *queryServer) Pricing(ctx context.Context, req *types.QueryPricingRequest) (*types.QueryPricingResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidRequest.Wrap("empty request")
	}
	if err := validatePaymentQueryID("deposit_id", req.DepositId); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	pricing, err := keeper.GetPricing(ctx, req.DepositId)
	if err != nil {
		return nil, err
	}
	return &types.QueryPricingResponse{Pricing: clonePricingRecord(pricing)}, nil
}

type paymentPagination struct {
	limit      uint64
	offset     uint64
	countTotal bool
}

func newPaymentPagination(pageReq *query.PageRequest) (paymentPagination, error) {
	pagination := paymentPagination{limit: defaultPaymentQueryLimit}
	if pageReq == nil {
		return pagination, nil
	}
	if pageReq.Reverse {
		return paymentPagination{}, types.ErrInvalidRequest.Wrap("reverse pagination not supported")
	}

	limit := pageReq.Limit
	if limit > 0 && limit <= maxPaymentQueryLimit {
		pagination.limit = limit
	}
	pagination.countTotal = pageReq.CountTotal

	pagination.offset = pageReq.Offset
	if len(pageReq.Key) > 0 {
		if pagination.offset > 0 {
			return paymentPagination{}, types.ErrInvalidRequest.Wrap("pagination key and offset are mutually exclusive")
		}
		parsed, err := strconv.ParseUint(string(pageReq.Key), 10, 64)
		if err != nil {
			return paymentPagination{}, types.ErrInvalidRequest.Wrap("invalid pagination key")
		}
		pagination.offset = parsed
	}

	return pagination, nil
}

func validateDepositStatusFilter(status types.DepositStatus) error {
	if _, ok := types.DepositStatus_name[int32(status)]; !ok {
		return types.ErrInvalidRequest.Wrapf("deposit status has invalid value %d", status)
	}
	return nil
}

func validateWithdrawStatusFilter(status types.WithdrawStatus) error {
	if _, ok := types.WithdrawStatus_name[int32(status)]; !ok {
		return types.ErrInvalidRequest.Wrapf("withdraw status has invalid value %d", status)
	}
	return nil
}

func collectPaymentPageRecord[T any](records *[]T, pagination paymentPagination, matched *uint64, hasMore *bool, record T) bool {
	index := *matched
	*matched = index + 1
	if index < pagination.offset {
		return false
	}

	if uint64(len(*records)) < pagination.limit {
		*records = append(*records, record)
		return false
	}

	*hasMore = true
	return !pagination.countTotal
}

func (p paymentPagination) response(collected int, matched uint64, hasMore bool) *query.PageResponse {
	pageRes := &query.PageResponse{}
	if p.countTotal {
		pageRes.Total = matched
	}
	if hasMore {
		nextOffset := p.offset + uint64(collected)
		pageRes.NextKey = []byte(strconv.FormatUint(nextOffset, 10))
	}
	return pageRes
}

func depositMatchesQuery(deposit *types.DepositRecord, status types.DepositStatus) bool {
	if deposit == nil {
		return false
	}
	return status == types.DepositStatus_DEPOSIT_STATUS_UNSPECIFIED || deposit.Status == status
}

func withdrawMatchesQuery(withdraw *types.WithdrawRecord, status types.WithdrawStatus) bool {
	if withdraw == nil {
		return false
	}
	return status == types.WithdrawStatus_WITHDRAW_STATUS_UNSPECIFIED || withdraw.Status == status
}

func cloneParams(params *types.Params) *types.Params {
	if params == nil {
		return nil
	}
	dst := &types.Params{}
	if !deepCopyProto(params, dst) {
		return params
	}
	return dst
}

func cloneDepositRecord(record *types.DepositRecord) *types.DepositRecord {
	if record == nil {
		return nil
	}
	dst := &types.DepositRecord{}
	if !deepCopyProto(record, dst) {
		return record
	}
	return dst
}

func cloneDepositRecords(records []*types.DepositRecord) []*types.DepositRecord {
	if len(records) == 0 {
		return records
	}
	cloned := make([]*types.DepositRecord, 0, len(records))
	for _, record := range records {
		cloned = append(cloned, cloneDepositRecord(record))
	}
	return cloned
}

func cloneWithdrawRecord(record *types.WithdrawRecord) *types.WithdrawRecord {
	if record == nil {
		return nil
	}
	dst := &types.WithdrawRecord{}
	if !deepCopyProto(record, dst) {
		return record
	}
	return dst
}

func cloneWithdrawRecords(records []*types.WithdrawRecord) []*types.WithdrawRecord {
	if len(records) == 0 {
		return records
	}
	cloned := make([]*types.WithdrawRecord, 0, len(records))
	for _, record := range records {
		cloned = append(cloned, cloneWithdrawRecord(record))
	}
	return cloned
}

func clonePricingRecord(record *types.PricingRecord) *types.PricingRecord {
	if record == nil {
		return nil
	}
	dst := &types.PricingRecord{}
	if !deepCopyProto(record, dst) {
		return record
	}
	return dst
}

// deepCopyProto copies src into dst via a gogo marshal/unmarshal round-trip.
// proto.Clone panics on gogo customtype fields (math.Int inside sdk.Coin), so
// it cannot be used here.
func deepCopyProto(src, dst proto.Message) bool {
	raw, err := proto.Marshal(src)
	if err != nil {
		return false
	}
	if err := proto.Unmarshal(raw, dst); err != nil {
		return false
	}
	return true
}
