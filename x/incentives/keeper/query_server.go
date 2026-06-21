package keeper

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/incentives/types"
)

var _ types.QueryServer = (*queryServer)(nil)

type queryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

// NewQueryServer returns an implementation of the incentives Query service.
func NewQueryServer(k *Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

func (s *queryServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, status.Error(codes.Internal, "incentives keeper not initialized")
	}
	return s.keeper, nil
}

// maxBadgeQueryLimit caps badge list responses to keep queries bounded.
const maxBadgeQueryLimit = 1000

func validateQueryToolID(toolID string) (string, error) {
	trimmed := strings.TrimSpace(toolID)
	if trimmed == "" {
		return "", status.Error(codes.InvalidArgument, "tool_id is required")
	}
	if trimmed != toolID {
		return "", status.Error(codes.InvalidArgument, "tool_id must not have leading or trailing whitespace")
	}
	return toolID, nil
}

// Params returns the current incentives module parameters.
func (s *queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	params, err := cloneParams(keeper.GetParams(ctx))
	if err != nil {
		return nil, err
	}

	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}

// Badge returns the active badge for a tool.
func (s *queryServer) Badge(ctx context.Context, req *types.QueryBadgeRequest) (*types.QueryBadgeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	toolID, err := validateQueryToolID(req.ToolId)
	if err != nil {
		return nil, err
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	badge, active := keeper.isBadgeActive(ctx, toolID)
	if !active {
		return nil, types.ErrBadgeNotFound.Wrapf("active badge not found for tool %s", toolID)
	}
	clonedBadge, err := cloneBadge(badge)
	if err != nil {
		return nil, err
	}

	return &types.QueryBadgeResponse{
		Badge: clonedBadge,
	}, nil
}

// Badges lists active badges, optionally filtered by tier and publisher.
func (s *queryServer) Badges(ctx context.Context, req *types.QueryBadgesRequest) (*types.QueryBadgesResponse, error) {
	if req == nil {
		req = &types.QueryBadgesRequest{}
	}
	pagination, err := newBadgePagination(req.Pagination)
	if err != nil {
		return nil, err
	}
	if err := validateKnownBadgeTier(req.Tier); err != nil {
		return nil, err
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	var badges []*types.Badge
	var matched uint64
	var hasMore bool
	if err := keeper.state.Badges.Walk(ctx, nil, func(_ string, badge *types.Badge) (bool, error) {
		if !s.badgeMatchesQuery(ctx, keeper, badge, req) {
			return false, nil
		}

		index := matched
		matched++
		if index < pagination.offset {
			return false, nil
		}

		if uint64(len(badges)) < pagination.limit {
			if badges == nil {
				badges = make([]*types.Badge, 0, maxBadgeQueryLimit)
			}
			badges = append(badges, badge)
			return false, nil
		}

		hasMore = true
		return !pagination.countTotal, nil
	}); err != nil {
		return nil, fmt.Errorf("walk badges: %w", err)
	}

	clonedBadges, err := cloneBadges(badges)
	if err != nil {
		return nil, err
	}

	return &types.QueryBadgesResponse{
		Badges:     clonedBadges,
		Pagination: pagination.response(len(badges), matched, hasMore),
	}, nil
}

// TierConfig returns the configured benefits and threshold for a badge tier.
func (s *queryServer) TierConfig(ctx context.Context, req *types.QueryTierConfigRequest) (*types.QueryTierConfigResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if err := validateKnownBadgeTier(req.Tier); err != nil {
		return nil, err
	}
	if req.Tier == types.BadgeTier_BADGE_TIER_UNSPECIFIED || req.Tier == types.BadgeTier_BADGE_TIER_NONE {
		return nil, types.ErrInvalidTierConfig.Wrap("tier must be a configured badge tier")
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	config, found := keeper.GetTierConfig(ctx, req.Tier)
	if !found {
		return nil, types.ErrTierConfigNotFound.Wrapf("tier %s", req.Tier.String())
	}
	clonedConfig, err := cloneTierConfig(config)
	if err != nil {
		return nil, err
	}

	return &types.QueryTierConfigResponse{
		Config: clonedConfig,
	}, nil
}

// Score calculates the current score implied by the latest metric snapshot.
func (s *queryServer) Score(ctx context.Context, req *types.QueryScoreRequest) (*types.QueryScoreResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	toolID, err := validateQueryToolID(req.ToolId)
	if err != nil {
		return nil, err
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	metrics, found := keeper.GetMetrics(ctx, toolID)
	if !found {
		return nil, types.ErrMetricSnapshotNotFound.Wrapf("tool %s", toolID)
	}

	components := keeper.scorer.CalculateAllComponentScores(metrics)
	compositeScore := keeper.scorer.CalculateCompositeScore(components)

	tierConfigs := keeper.GetAllTierConfigs(ctx)
	if len(tierConfigs) == 0 {
		tierConfigs = types.DefaultTierConfigs()
	}

	currentTier := types.BadgeTier_BADGE_TIER_NONE
	if badge, active := keeper.isBadgeActive(ctx, toolID); active {
		currentTier = badge.Tier
	}

	return &types.QueryScoreResponse{
		ToolId:          toolID,
		CompositeScore:  compositeScore,
		ComponentScores: components,
		EligibleTier:    s.keeper.scorer.DetermineTier(compositeScore, tierConfigs),
		CurrentTier:     currentTier,
	}, nil
}

func (s *queryServer) badgeMatchesQuery(ctx context.Context, keeper *Keeper, badge *types.Badge, req *types.QueryBadgesRequest) bool {
	if badge == nil {
		return false
	}
	if badge.ToolId == "" {
		return false
	}
	if badge.Tier == types.BadgeTier_BADGE_TIER_UNSPECIFIED || badge.Tier == types.BadgeTier_BADGE_TIER_NONE {
		return false
	}
	if _, active := keeper.isBadgeActive(ctx, badge.ToolId); !active {
		return false
	}
	if req.Tier != types.BadgeTier_BADGE_TIER_UNSPECIFIED && badge.Tier != req.Tier {
		return false
	}
	if req.PublisherId != "" && badge.PublisherId != req.PublisherId {
		return false
	}
	return true
}

func validateKnownBadgeTier(tier types.BadgeTier) error {
	switch tier {
	case types.BadgeTier_BADGE_TIER_UNSPECIFIED,
		types.BadgeTier_BADGE_TIER_NONE,
		types.BadgeTier_BADGE_TIER_BRONZE,
		types.BadgeTier_BADGE_TIER_SILVER,
		types.BadgeTier_BADGE_TIER_GOLD,
		types.BadgeTier_BADGE_TIER_PLATINUM:
		return nil
	default:
		return status.Errorf(codes.InvalidArgument, "tier has invalid value %d", tier)
	}
}

type badgePagination struct {
	offset     uint64
	limit      uint64
	countTotal bool
}

func newBadgePagination(page *query.PageRequest) (badgePagination, error) {
	pagination := badgePagination{limit: maxBadgeQueryLimit}
	if page == nil {
		return pagination, nil
	}

	pagination.offset = page.GetOffset()
	pagination.countTotal = page.GetCountTotal()

	if page.GetReverse() {
		return badgePagination{}, status.Error(codes.Unimplemented, "reverse pagination not supported")
	}

	if key := page.GetKey(); len(key) > 0 {
		if pagination.offset > 0 {
			return badgePagination{}, status.Error(codes.InvalidArgument, "pagination key and offset are mutually exclusive")
		}
		parsed, err := strconv.ParseUint(string(key), 10, 64)
		if err != nil {
			return badgePagination{}, status.Errorf(codes.InvalidArgument, "invalid pagination key: %v", err)
		}
		pagination.offset = parsed
	}

	if limit := page.GetLimit(); limit > 0 && limit < maxBadgeQueryLimit {
		pagination.limit = limit
	}

	return pagination, nil
}

func (p badgePagination) response(collected int, matched uint64, hasMore bool) *query.PageResponse {
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

func cloneParams(params *types.Params) (*types.Params, error) {
	if params == nil {
		return nil, nil
	}
	cloned, ok := proto.Clone(params).(*types.Params)
	if !ok {
		return nil, fmt.Errorf("unable to clone incentives params")
	}
	return cloned, nil
}

func cloneBadge(badge *types.Badge) (*types.Badge, error) {
	if badge == nil {
		return nil, nil
	}
	cloned, ok := proto.Clone(badge).(*types.Badge)
	if !ok {
		return nil, fmt.Errorf("unable to clone badge %s", badge.ToolId)
	}
	return cloned, nil
}

func cloneBadges(badges []*types.Badge) ([]*types.Badge, error) {
	if badges == nil {
		return nil, nil
	}
	cloned := make([]*types.Badge, 0, len(badges))
	for _, badge := range badges {
		clonedBadge, err := cloneBadge(badge)
		if err != nil {
			return nil, err
		}
		cloned = append(cloned, clonedBadge)
	}
	return cloned, nil
}

func cloneTierConfig(config *types.TierConfig) (*types.TierConfig, error) {
	if config == nil {
		return nil, nil
	}
	cloned, ok := proto.Clone(config).(*types.TierConfig)
	if !ok {
		return nil, fmt.Errorf("unable to clone tier config %s", config.Tier.String())
	}
	return cloned, nil
}
