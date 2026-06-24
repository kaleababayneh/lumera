
package keeper

import (
	"context"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/cosmos/gogoproto/proto"

	"github.com/LumeraProtocol/lumera/x/passport/types"
)

// queryServer implements the QueryServer interface.
type queryServer struct {
	types.UnimplementedQueryServer
	k *Keeper
}

// NewQueryServer returns an implementation of the QueryServer interface.
func NewQueryServer(keeper *Keeper) types.QueryServer {
	return &queryServer{k: keeper}
}

var _ types.QueryServer = queryServer{}

func (s queryServer) requireKeeper() (*Keeper, error) {
	if s.k == nil {
		return nil, status.Error(codes.Internal, "passport keeper not initialized")
	}
	return s.k, nil
}

func validateQueryPassportID(passportID string) error {
	trimmed := strings.TrimSpace(passportID)
	if trimmed == "" || trimmed != passportID || len(passportID) > types.MaxPassportIDLen {
		return types.ErrInvalidPassportID
	}
	return nil
}

func validateQueryPassportStatusFilter(filter types.PassportStatus) error {
	if filter == types.PassportStatus_PASSPORT_STATUS_UNSPECIFIED {
		return nil
	}
	if _, ok := types.PassportStatus_name[int32(filter)]; !ok {
		return status.Errorf(codes.InvalidArgument, "status_filter has invalid value %d", filter)
	}
	return nil
}

// Passport handles QueryPassportRequest.
func (s queryServer) Passport(ctx context.Context, req *types.QueryPassportRequest) (*types.QueryPassportResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidPassportID
	}
	if err := validateQueryPassportID(req.PassportId); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	passport, found := keeper.GetPassport(ctx, req.PassportId)
	if !found {
		return nil, types.ErrPassportNotFound
	}

	return &types.QueryPassportResponse{
		Passport: clonePassport(passport),
	}, nil
}

// PassportByAgent handles QueryPassportByAgentRequest.
func (s queryServer) PassportByAgent(ctx context.Context, req *types.QueryPassportByAgentRequest) (*types.QueryPassportByAgentResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidAgentPubkey
	}
	if strings.TrimSpace(req.AgentPubkey) == "" {
		return nil, types.ErrInvalidAgentPubkey
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	passport, found := keeper.GetPassportByAgent(ctx, req.AgentPubkey)
	if !found {
		return nil, types.ErrPassportNotFound
	}

	return &types.QueryPassportByAgentResponse{
		Passport: clonePassport(passport),
	}, nil
}

// maxPassportQueryLimit caps results returned by list queries to prevent DoS.
const maxPassportQueryLimit = 1000

// Passports handles QueryPassportsRequest.
func (s queryServer) Passports(ctx context.Context, req *types.QueryPassportsRequest) (*types.QueryPassportsResponse, error) {
	if req == nil {
		req = &types.QueryPassportsRequest{}
	}
	pagination, err := newPassportPagination(req.Pagination)
	if err != nil {
		return nil, err
	}
	if err := validateQueryPassportStatusFilter(req.StatusFilter); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	passports := make([]*types.AgentPassport, 0, maxPassportQueryLimit)
	var matched uint64
	var hasMore bool
	err = keeper.IteratePassports(ctx, func(passport *types.AgentPassport) bool {
		if req.StatusFilter != types.PassportStatus_PASSPORT_STATUS_UNSPECIFIED {
			if passport.Status != req.StatusFilter {
				return false
			}
		}

		index := matched
		matched++
		if index < pagination.offset {
			return false
		}

		if uint64(len(passports)) < pagination.limit {
			passports = append(passports, passport)
			return !pagination.paginated && uint64(len(passports)) == pagination.limit
		}

		hasMore = true
		return !pagination.countTotal
	})
	if err != nil {
		return nil, err
	}

	pageRes := pagination.response(len(passports), matched, hasMore)

	return &types.QueryPassportsResponse{
		Passports:  clonePassports(passports),
		Pagination: pageRes,
	}, nil
}

type passportPagination struct {
	offset     uint64
	limit      uint64
	countTotal bool
	paginated  bool
}

func newPassportPagination(page *query.PageRequest) (passportPagination, error) {
	pagination := passportPagination{limit: maxPassportQueryLimit}
	if page == nil {
		return pagination, nil
	}

	pagination.paginated = true
	pagination.offset = page.GetOffset()
	pagination.countTotal = page.GetCountTotal()

	if page.GetReverse() {
		return passportPagination{}, status.Error(codes.InvalidArgument, "reverse pagination not supported")
	}

	if key := page.GetKey(); len(key) > 0 {
		if pagination.offset > 0 {
			return passportPagination{}, status.Error(codes.InvalidArgument, "pagination key and offset are mutually exclusive")
		}
		parsed, err := strconv.ParseUint(string(key), 10, 64)
		if err != nil {
			return passportPagination{}, status.Errorf(codes.InvalidArgument, "invalid pagination key: %v", err)
		}
		pagination.offset = parsed
	}

	if limit := page.GetLimit(); limit > 0 && limit < maxPassportQueryLimit {
		pagination.limit = limit
	}

	return pagination, nil
}

func (p passportPagination) response(collected int, matched uint64, hasMore bool) *query.PageResponse {
	if !p.paginated {
		return nil
	}

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

// Params handles QueryParamsRequest.
func (s queryServer) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	params := keeper.GetParams(ctx)
	return &types.QueryParamsResponse{
		Params: cloneParams(params),
	}, nil
}

// deepCopyProto copies src into dst via a gogo marshal/unmarshal round-trip.
// proto.Clone panics on gogo customtype fields (e.g. math.Int inside sdk.Coin:
// "merger not found for type:big.Word"), so we cannot use it here.
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

func clonePassport(passport *types.AgentPassport) *types.AgentPassport {
	if passport == nil {
		return nil
	}
	dst := &types.AgentPassport{}
	if !deepCopyProto(passport, dst) {
		return passport
	}
	return dst
}

func clonePassports(passports []*types.AgentPassport) []*types.AgentPassport {
	if len(passports) == 0 {
		return passports
	}
	cloned := make([]*types.AgentPassport, 0, len(passports))
	for _, passport := range passports {
		cloned = append(cloned, clonePassport(passport))
	}
	return cloned
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
