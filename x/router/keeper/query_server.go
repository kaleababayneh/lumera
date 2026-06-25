package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/cosmos/gogoproto/proto"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// maxRouterQueryLimit caps results collected by load-all-then-paginate queries.
const maxRouterQueryLimit = 1000

// queryServer implements the lumera.router Query service using the keeper state.
type queryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

// NewQueryServer constructs a gRPC Query service backed by the keeper.
func NewQueryServer(k *Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

func (q *queryServer) requireKeeper() (*Keeper, error) {
	if q == nil || q.keeper == nil {
		return nil, status.Error(codes.Internal, "router keeper not initialized")
	}
	return q.keeper, nil
}

func validateQueryIdentifier(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return status.Errorf(codes.InvalidArgument, "%s is required", field)
	}
	if trimmed != value {
		return status.Errorf(codes.InvalidArgument, "%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func validateOptionalQueryIdentifier(field, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return status.Errorf(codes.InvalidArgument, "%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func validateToolRankingCriteria(criteria types.RankingCriteria) error {
	switch criteria {
	case types.RankingCriteria_RANKING_CRITERIA_UNSPECIFIED,
		types.RankingCriteria_RANKING_CRITERIA_VOLUME,
		types.RankingCriteria_RANKING_CRITERIA_INVOCATIONS,
		types.RankingCriteria_RANKING_CRITERIA_SUCCESS_RATE,
		types.RankingCriteria_RANKING_CRITERIA_LATENCY,
		types.RankingCriteria_RANKING_CRITERIA_COST_EFFICIENCY,
		types.RankingCriteria_RANKING_CRITERIA_OVERALL_SCORE:
		return nil
	default:
		return status.Errorf(codes.InvalidArgument, "criteria has invalid value %d", criteria)
	}
}

func (q *queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	params, err := keeper.state.Params.Get(sdkCtx)
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: *cloneParams(params)}, nil
}

func (q *queryServer) GlobalMetrics(goCtx context.Context, _ *types.QueryGlobalMetricsRequest) (*types.QueryGlobalMetricsResponse, error) {
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	metrics, err := keeper.state.GlobalMetrics.Get(sdkCtx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			metrics = &types.GlobalMetrics{}
		} else {
			return nil, err
		}
	}
	return &types.QueryGlobalMetricsResponse{Metrics: cloneGlobalMetrics(metrics)}, nil
}

func (q *queryServer) ToolMetrics(goCtx context.Context, req *types.QueryToolMetricsRequest) (*types.QueryToolMetricsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "tool_id is required")
	}
	if err := validateQueryIdentifier("tool_id", req.ToolId); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	metrics, err := keeper.state.ToolMetrics.Get(sdkCtx, req.ToolId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "tool %s metrics not found", req.ToolId)
		}
		return nil, err
	}

	score, err := keeper.state.SelectionScores.Get(sdkCtx, req.ToolId)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return nil, err
		}
		score = nil
	}

	return &types.QueryToolMetricsResponse{
		Metrics: cloneActivationMetrics(metrics),
		Score:   cloneToolSelectionScore(score),
	}, nil
}

func (q *queryServer) CategoryMetrics(goCtx context.Context, req *types.QueryCategoryMetricsRequest) (*types.QueryCategoryMetricsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "category is required")
	}
	if err := validateQueryIdentifier("category", req.Category); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	metrics, err := keeper.state.CategoryMetrics.Get(sdkCtx, req.Category)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "category %s metrics not found", req.Category)
		}
		return nil, err
	}

	return &types.QueryCategoryMetricsResponse{Metrics: cloneCategoryMetrics(metrics)}, nil
}

func (q *queryServer) SessionMetrics(goCtx context.Context, req *types.QuerySessionMetricsRequest) (*types.QuerySessionMetricsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := validateQueryIdentifier("session_id", req.SessionId); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	metrics, err := keeper.state.SessionMetrics.Get(sdkCtx, req.SessionId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "session %s metrics not found", req.SessionId)
		}
		return nil, err
	}
	return &types.QuerySessionMetricsResponse{Metrics: cloneSessionMetrics(metrics)}, nil
}

func (q *queryServer) SelectionScores(goCtx context.Context, req *types.QuerySelectionScoresRequest) (*types.QuerySelectionScoresResponse, error) {
	if req == nil {
		req = &types.QuerySelectionScoresRequest{}
	}

	minScore := decimal.Zero
	if req.MinScore != "" {
		parsed, err := types.ParseDecimal(req.MinScore)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid min_score: %v", err)
		}
		minScore = parsed
	}
	if err := validatePaginationRequest(req.Pagination); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	scores := make([]*types.ToolSelectionScore, 0)
	err = keeper.state.SelectionScores.Walk(sdkCtx, nil, func(toolID string, score *types.ToolSelectionScore) (bool, error) {
		cloned := &types.ToolSelectionScore{}
	ok := deepCopyProto(score, cloned)
		if !ok {
			return true, fmt.Errorf("failed to clone selection score for %s", toolID)
		}
		overall, err := cloned.OverallScoreDecimalSafe()
		if err != nil {
			return true, err
		}
		if overall.LessThan(minScore) {
			return false, nil
		}
		scores = append(scores, cloned)
		return len(scores) >= maxRouterQueryLimit, nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].GetToolId() == scores[j].GetToolId() {
			return false
		}
		return scores[i].GetToolId() < scores[j].GetToolId()
	})

	sliced, pageRes, err := applyPagination(scores, req.Pagination)
	if err != nil {
		return nil, err
	}

	return &types.QuerySelectionScoresResponse{Scores: sliced, Pagination: pageRes}, nil
}

func (q *queryServer) PolicyUpdates(goCtx context.Context, req *types.QueryPolicyUpdatesRequest) (*types.QueryPolicyUpdatesResponse, error) {
	if req == nil {
		req = &types.QueryPolicyUpdatesRequest{}
	}
	if err := validatePaginationRequest(req.Pagination); err != nil {
		return nil, err
	}
	if err := validateOptionalQueryIdentifier("policy_id", req.PolicyId); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	updates := make([]*types.PolicyUpdate, 0)
	err = keeper.state.PolicyUpdates.Walk(sdkCtx, nil, func(_ uint64, update *types.PolicyUpdate) (bool, error) {
		if req.PolicyId != "" && update.GetPolicyId() != req.PolicyId {
			return false, nil
		}
		clonedUpdate := &types.PolicyUpdate{}
	ok := deepCopyProto(update, clonedUpdate)
		if !ok {
			return false, nil
		}
		updates = append(updates, clonedUpdate)
		return len(updates) >= maxRouterQueryLimit, nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(updates, func(i, j int) bool {
		if updates[i].GetBlockHeight() == updates[j].GetBlockHeight() {
			return updates[i].GetUpdatedAt().After(updates[j].GetUpdatedAt())
		}
		return updates[i].GetBlockHeight() > updates[j].GetBlockHeight()
	})

	sliced, pageRes, err := applyPagination(updates, req.Pagination)
	if err != nil {
		return nil, err
	}

	if req.Limit > 0 && uint32(len(sliced)) > req.Limit { //#nosec G115 -- len(sliced) bounded by pagination limits
		sliced = sliced[:req.Limit]
	}

	return &types.QueryPolicyUpdatesResponse{Updates: sliced, Pagination: pageRes}, nil
}

func (q *queryServer) CACRoyalties(goCtx context.Context, req *types.QueryCACRoyaltiesRequest) (*types.QueryCACRoyaltiesResponse, error) {
	if req == nil {
		req = &types.QueryCACRoyaltiesRequest{}
	}
	if err := validatePaginationRequest(req.Pagination); err != nil {
		return nil, err
	}
	if err := validateOptionalQueryIdentifier("origin_tool_id", req.OriginToolId); err != nil {
		return nil, err
	}
	if err := validateOptionalQueryIdentifier("consuming_tool_id", req.ConsumingToolId); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	records := make([]*types.CACRoyaltyRecord, 0)
	total := decimal.Zero

	processRecord := func(record *types.CACRoyaltyRecord) error {
		if req.OriginToolId != "" && record.GetOriginToolId() != req.OriginToolId {
			return nil
		}
		if req.ConsumingToolId != "" && record.GetConsumingToolId() != req.ConsumingToolId {
			return nil
		}
		cloned := &types.CACRoyaltyRecord{}
	ok := deepCopyProto(record, cloned)
		if !ok {
			return nil
		}
		amount, err := cloned.RoyaltyAmountDecimalSafe()
		if err != nil {
			return err
		}
		total = total.Add(amount)
		records = append(records, cloned)
		return nil
	}

	switch {
	case req.OriginToolId != "":
		iter, err := keeper.state.CACRecords.Indexes.Origin.MatchExact(sdkCtx, req.OriginToolId)
		if err == nil {
			defer func() { _ = iter.Close() }()
			for ; iter.Valid() && len(records) < maxRouterQueryLimit; iter.Next() {
				pk, pkErr := iter.PrimaryKey()
				if pkErr != nil {
					// Index entry exists but the primary-key extraction
					// failed. Log so operators see the divergence —
					// otherwise the CAC royalty totals under-report by
					// the missing record's amount with zero signal.
					keeper.Logger(sdkCtx).Error("query CAC records by origin: failed to extract primary key from index entry",
						"origin_tool_id", req.OriginToolId, "error", pkErr)
					continue
				}
				record, recErr := keeper.state.CACRecords.Get(sdkCtx, pk)
				if recErr != nil {
					// Index points at a primary row that failed to load
					// (orphaned index entry or transient store fault).
					// Same class as 03416b0af (insurance processExpired
					// Claims orphan) and 5cdeb42a9 (reserve commitment
					// orphan). Log so the caller's under-reported total
					// is visible to operators.
					keeper.Logger(sdkCtx).Error("query CAC records by origin: index entry points at missing/unloadable primary row",
						"origin_tool_id", req.OriginToolId, "primary_key", pk, "error", recErr)
					continue
				}
				if err := processRecord(record); err != nil {
					return nil, err
				}
			}
		} else if !errors.Is(err, collections.ErrNotFound) {
			return nil, err
		}
	case req.ConsumingToolId != "":
		iter, err := keeper.state.CACRecords.Indexes.Consumer.MatchExact(sdkCtx, req.ConsumingToolId)
		if err == nil {
			defer func() { _ = iter.Close() }()
			for ; iter.Valid() && len(records) < maxRouterQueryLimit; iter.Next() {
				pk, pkErr := iter.PrimaryKey()
				if pkErr != nil {
					keeper.Logger(sdkCtx).Error("query CAC records by consumer: failed to extract primary key from index entry",
						"consuming_tool_id", req.ConsumingToolId, "error", pkErr)
					continue
				}
				record, recErr := keeper.state.CACRecords.Get(sdkCtx, pk)
				if recErr != nil {
					keeper.Logger(sdkCtx).Error("query CAC records by consumer: index entry points at missing/unloadable primary row",
						"consuming_tool_id", req.ConsumingToolId, "primary_key", pk, "error", recErr)
					continue
				}
				if err := processRecord(record); err != nil {
					return nil, err
				}
			}
		} else if !errors.Is(err, collections.ErrNotFound) {
			return nil, err
		}
	default:
		if err := keeper.state.CACRecords.Walk(sdkCtx, nil, func(_ string, record *types.CACRoyaltyRecord) (bool, error) {
			if perr := processRecord(record); perr != nil {
				return true, perr
			}
			return len(records) >= maxRouterQueryLimit, nil
		}); err != nil {
			return nil, err
		}
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].GetBlockHeight() > records[j].GetBlockHeight()
	})

	sliced, pageRes, err := applyPagination(records, req.Pagination)
	if err != nil {
		return nil, err
	}

	return &types.QueryCACRoyaltiesResponse{
		Records:        sliced,
		TotalRoyalties: total.String(),
		Pagination:     pageRes,
	}, nil
}

func (q *queryServer) ActiveTools(goCtx context.Context, req *types.QueryActiveToolsRequest) (*types.QueryActiveToolsResponse, error) {
	if req == nil {
		req = &types.QueryActiveToolsRequest{}
	}
	if err := validateOptionalQueryIdentifier("session_id", req.SessionId); err != nil {
		return nil, err
	}
	if err := validateOptionalQueryIdentifier("category", req.Category); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	toolIDs := make([]string, 0)
	metricsMap := make(map[string]*types.ActivationMetrics)

	iter, err := keeper.state.ActiveTools.Indexes.Active.MatchExact(sdkCtx, true)
	if err == nil {
		defer func() { _ = iter.Close() }()
		for ; iter.Valid() && len(toolIDs) < maxRouterQueryLimit; iter.Next() {
			pk, pkErr := iter.PrimaryKey()
			if pkErr != nil {
				continue
			}
			activation, actErr := keeper.state.ActiveTools.Get(sdkCtx, pk)
			if actErr != nil {
				continue
			}
			if !matchesOptionalFilter(activation.GetSessionId(), req.SessionId) {
				continue
			}
			if req.Category != "" {
				category := keeper.getToolCategory(sdkCtx, activation.GetToolId())
				if category != req.Category {
					continue
				}
			}
			toolIDs = append(toolIDs, activation.GetToolId())
			if metrics, getErr := keeper.state.ToolMetrics.Get(sdkCtx, activation.GetToolId()); getErr == nil {
				metricsMap[activation.GetToolId()] = cloneActivationMetrics(metrics)
			}
		}
	} else if !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}

	sort.Strings(toolIDs)

	return &types.QueryActiveToolsResponse{ToolIds: toolIDs, Metrics: metricsMap}, nil
}

func (q *queryServer) ToolRanking(goCtx context.Context, req *types.QueryToolRankingRequest) (*types.QueryToolRankingResponse, error) {
	if req == nil {
		req = &types.QueryToolRankingRequest{}
	}
	if err := validateOptionalQueryIdentifier("category", req.Category); err != nil {
		return nil, err
	}
	if err := validateToolRankingCriteria(req.Criteria); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	criteria := req.Criteria
	if criteria == types.RankingCriteria_RANKING_CRITERIA_UNSPECIFIED {
		criteria = types.RankingCriteria_RANKING_CRITERIA_OVERALL_SCORE
	}

	var cutoff time.Time
	if req.WindowSeconds > 0 {
		cutoff = sdkCtx.BlockTime().Add(-time.Duration(req.WindowSeconds) * time.Second)
	}

	type ranked struct {
		toolID    string
		metrics   *types.ActivationMetrics
		selection *types.ToolSelectionScore
		score     decimal.Decimal
	}

	results := make([]ranked, 0)
	err = keeper.state.ToolMetrics.Walk(sdkCtx, nil, func(toolID string, metrics *types.ActivationMetrics) (bool, error) {
		if req.Category != "" && keeper.getToolCategory(sdkCtx, toolID) != req.Category {
			return false, nil
		}
		if !cutoff.IsZero() {
			last := metrics.LastInvokedTime()
			if last == nil || last.Before(cutoff) {
				return false, nil
			}
		}

		selection, serr := keeper.state.SelectionScores.Get(sdkCtx, toolID)
		if serr != nil && !errors.Is(serr, collections.ErrNotFound) {
			return true, serr
		}
		score, serr := rankingScore(criteria, metrics, selection)
		if serr != nil {
			return true, serr
		}
		clonedMetrics := &types.ActivationMetrics{}
	ok := deepCopyProto(metrics, clonedMetrics)
		if !ok {
			return false, nil
		}
		results = append(results, ranked{
			toolID:    toolID,
			metrics:   clonedMetrics,
			selection: selection,
			score:     score,
		})
		return len(results) >= maxRouterQueryLimit, nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score.Equal(results[j].score) {
			return results[i].toolID < results[j].toolID
		}
		return results[i].score.GreaterThan(results[j].score)
	})

	limit := len(results)
	if req.Limit > 0 && int(req.Limit) < limit {
		limit = int(req.Limit)
	}

	rankedTools := make([]*types.RankedTool, 0, limit)
	for idx := 0; idx < limit; idx++ {
		entry := results[idx]
		rankedTools = append(rankedTools, &types.RankedTool{
			ToolId:  entry.toolID,
			Rank:    uint32(idx + 1), //#nosec G115 -- idx bounded by limit param
			Score:   entry.score.String(),
			Metrics: entry.metrics,
		})
	}

	return &types.QueryToolRankingResponse{
		Tools:     rankedTools,
		Criteria:  criteria,
		Timestamp: sdkCtx.BlockTime().UTC().Format(time.RFC3339),
	}, nil
}

func validatePaginationRequest(pagination *query.PageRequest) error {
	if pagination == nil {
		return nil
	}
	if pagination.Reverse {
		return status.Error(codes.InvalidArgument, "reverse pagination not supported")
	}
	if len(pagination.Key) == 0 {
		return nil
	}
	if pagination.Offset > 0 {
		return status.Error(codes.InvalidArgument, "pagination key and offset are mutually exclusive")
	}
	if _, err := strconv.ParseUint(string(pagination.Key), 10, 64); err != nil {
		return status.Error(codes.InvalidArgument, "invalid pagination key")
	}
	return nil
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

func cloneGlobalMetrics(metrics *types.GlobalMetrics) *types.GlobalMetrics {
	if metrics == nil {
		return nil
	}
	dst := &types.GlobalMetrics{}
	if !deepCopyProto(metrics, dst) {
		return metrics
	}
	return dst
}

func cloneActivationMetrics(metrics *types.ActivationMetrics) *types.ActivationMetrics {
	if metrics == nil {
		return nil
	}
	dst := &types.ActivationMetrics{}
	if !deepCopyProto(metrics, dst) {
		return metrics
	}
	return dst
}

func cloneToolSelectionScore(score *types.ToolSelectionScore) *types.ToolSelectionScore {
	if score == nil {
		return nil
	}
	dst := &types.ToolSelectionScore{}
	if !deepCopyProto(score, dst) {
		return score
	}
	return dst
}

func cloneCategoryMetrics(metrics *types.CategoryMetrics) *types.CategoryMetrics {
	if metrics == nil {
		return nil
	}
	dst := &types.CategoryMetrics{}
	if !deepCopyProto(metrics, dst) {
		return metrics
	}
	return dst
}

func cloneSessionMetrics(metrics *types.SessionMetrics) *types.SessionMetrics {
	if metrics == nil {
		return nil
	}
	dst := &types.SessionMetrics{}
	if !deepCopyProto(metrics, dst) {
		return metrics
	}
	return dst
}

func matchesOptionalFilter(actual, expected string) bool {
	return expected == "" || strings.Compare(actual, expected) == 0
}

func applyPagination[T any](items []T, pagination *query.PageRequest) ([]T, *query.PageResponse, error) {
	total := uint64(len(items))
	if pagination == nil {
		return items, &query.PageResponse{Total: total}, nil
	}
	if pagination.Reverse {
		return nil, nil, status.Error(codes.InvalidArgument, "reverse pagination not supported")
	}
	offset := pagination.Offset
	if len(pagination.Key) > 0 {
		if offset > 0 {
			return nil, nil, status.Error(codes.InvalidArgument, "pagination key and offset are mutually exclusive")
		}
		parsedOffset, err := strconv.ParseUint(string(pagination.Key), 10, 64)
		if err != nil {
			return nil, nil, status.Error(codes.InvalidArgument, "invalid pagination key")
		}
		offset = parsedOffset
	}
	if offset > total {
		offset = total
	}
	limit := pagination.Limit
	if limit == 0 || offset+limit > total {
		limit = total - offset
	}
	end := offset + limit
	sliced := items[offset:end]
	pageRes := &query.PageResponse{}
	if pagination.CountTotal {
		pageRes.Total = total
	}
	if end < total {
		pageRes.NextKey = []byte(strconv.FormatUint(end, 10))
	}
	return sliced, pageRes, nil
}

func rankingScore(criteria types.RankingCriteria, metrics *types.ActivationMetrics, selection *types.ToolSelectionScore) (decimal.Decimal, error) {
	switch criteria {
	case types.RankingCriteria_RANKING_CRITERIA_VOLUME:
		return metrics.TotalVolumeDecimalSafe()
	case types.RankingCriteria_RANKING_CRITERIA_INVOCATIONS:
		return decimalFromUint64(metrics.InvocationCount), nil
	case types.RankingCriteria_RANKING_CRITERIA_SUCCESS_RATE:
		return metrics.SuccessRateDecimalSafe()
	case types.RankingCriteria_RANKING_CRITERIA_LATENCY:
		latency, err := metrics.AverageLatencyDecimalSafe()
		if err != nil {
			return decimal.Zero, err
		}
		if latency.IsZero() {
			return decimal.NewFromInt(100), nil
		}
		return decimal.NewFromInt(1000000).Div(latency), nil
	case types.RankingCriteria_RANKING_CRITERIA_COST_EFFICIENCY:
		volume, err := metrics.TotalVolumeDecimalSafe()
		if err != nil {
			return decimal.Zero, err
		}
		if metrics.InvocationCount == 0 || volume.IsZero() {
			return decimal.NewFromInt(100), nil
		}
		avgCost := volume.Div(decimalFromUint64(metrics.InvocationCount))
		if avgCost.IsZero() {
			return decimal.NewFromInt(100), nil
		}
		return decimal.NewFromInt(1000000).Div(avgCost), nil
	case types.RankingCriteria_RANKING_CRITERIA_OVERALL_SCORE:
		fallthrough
	default:
		if selection != nil {
			return selection.OverallScoreDecimalSafe()
		}
		return metrics.SuccessRateDecimalSafe()
	}
}

// deepCopyProto copies src into dst via a gogo marshal/unmarshal round-trip;
// proto.Clone panics on gogo customtype fields.
func deepCopyProto(src, dst proto.Message) bool {
	raw, err := proto.Marshal(src)
	if err != nil {
		return false
	}
	return proto.Unmarshal(raw, dst) == nil
}
