package keeper

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/gogoproto/proto"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

type queryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

// NewQueryServerImpl returns an implementation of the challenges Query service.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

func (s *queryServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("challenges keeper not initialized")
	}
	return s.keeper, nil
}

// Params returns the module parameters.
func (s *queryServer) Params(goCtx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	params, err := cloneParams(keeper.GetParams(goCtx))
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

// Challenge returns a single challenge by ID.
func (s *queryServer) Challenge(goCtx context.Context, req *types.QueryChallengeRequest) (*types.QueryChallengeResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}
	id, err := validateQueryIdentifier("challenge_id", req.ChallengeId)
	if err != nil {
		return nil, err
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	ch, err := keeper.GetChallenge(goCtx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get challenge: %w", err)
	}
	clonedChallenge, err := cloneChallenge(ch)
	if err != nil {
		return nil, err
	}
	return &types.QueryChallengeResponse{Challenge: clonedChallenge}, nil
}

// Challenges returns challenges filtered by status and/or type with pagination.
func (s *queryServer) Challenges(goCtx context.Context, req *types.QueryChallengesRequest) (*types.QueryChallengesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := validatePaginationRequest(req.Pagination); err != nil {
		return nil, err
	}
	if err := validateChallengeQueryEnums(req); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// If filtering by status, use the status index for efficiency.
	if req.Status != types.ChallengeStatus_CHALLENGE_STATUS_UNSPECIFIED {
		challenges, err := keeper.GetChallengesByStatus(goCtx, req.Status)
		if err != nil {
			return nil, fmt.Errorf("failed to query challenges by status: %w", err)
		}

		// Apply type filter if specified.
		if req.ChallengeType != types.ChallengeType_CHALLENGE_TYPE_UNSPECIFIED {
			filtered := make([]*types.Challenge, 0, len(challenges))
			for _, ch := range challenges {
				if ch.ChallengeType == req.ChallengeType {
					filtered = append(filtered, ch)
				}
			}
			challenges = filtered
		}

		paged, pageRes, err := paginateChallenges(challenges, req.Pagination)
		if err != nil {
			return nil, err
		}
		clonedPaged, err := cloneChallenges(paged)
		if err != nil {
			return nil, err
		}

		return &types.QueryChallengesResponse{Challenges: clonedPaged, Pagination: pageRes}, nil
	}

	// No status filter: walk all challenges.
	all, err := keeper.GetAllChallenges(goCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to list challenges: %w", err)
	}

	// Apply type filter if specified.
	if req.ChallengeType != types.ChallengeType_CHALLENGE_TYPE_UNSPECIFIED {
		filtered := make([]*types.Challenge, 0, len(all))
		for _, ch := range all {
			if ch.ChallengeType == req.ChallengeType {
				filtered = append(filtered, ch)
			}
		}
		all = filtered
	}

	paged, pageRes, err := paginateChallenges(all, req.Pagination)
	if err != nil {
		return nil, err
	}
	clonedPaged, err := cloneChallenges(paged)
	if err != nil {
		return nil, err
	}

	return &types.QueryChallengesResponse{Challenges: clonedPaged, Pagination: pageRes}, nil
}

// Participants returns all participants for a challenge.
func (s *queryServer) Participants(goCtx context.Context, req *types.QueryParticipantsRequest) (*types.QueryParticipantsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}
	challengeID, err := validateQueryIdentifier("challenge_id", req.ChallengeId)
	if err != nil {
		return nil, err
	}
	if err := validatePaginationRequest(req.Pagination); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Verify the challenge exists.
	if _, err := keeper.GetChallenge(goCtx, challengeID); err != nil {
		return nil, fmt.Errorf("challenge not found: %w", err)
	}

	participants, err := keeper.GetParticipants(goCtx, challengeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list participants: %w", err)
	}

	paged, pageRes, err := paginateParticipants(participants, req.Pagination)
	if err != nil {
		return nil, err
	}
	clonedPaged, err := cloneParticipants(paged)
	if err != nil {
		return nil, err
	}

	return &types.QueryParticipantsResponse{Participants: clonedPaged, Pagination: pageRes}, nil
}

// Leaderboard returns rankings for a challenge, sorted by rank.
func (s *queryServer) Leaderboard(goCtx context.Context, req *types.QueryLeaderboardRequest) (*types.QueryLeaderboardResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}
	challengeID, err := validateQueryIdentifier("challenge_id", req.ChallengeId)
	if err != nil {
		return nil, err
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	rankings, err := keeper.GetRankings(goCtx, challengeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get rankings: %w", err)
	}

	// Sort by rank ascending.
	sort.Slice(rankings, func(i, j int) bool {
		return rankings[i].Rank < rankings[j].Rank
	})

	// Apply limit.
	if req.Limit > 0 && uint32(len(rankings)) > req.Limit {
		rankings = rankings[:req.Limit]
	}
	clonedRankings, err := cloneRankings(rankings)
	if err != nil {
		return nil, err
	}

	return &types.QueryLeaderboardResponse{Rankings: clonedRankings}, nil
}

// ToolChallenges returns all challenges a tool has participated in, along with
// any associated rankings.
func (s *queryServer) ToolChallenges(goCtx context.Context, req *types.QueryToolChallengesRequest) (*types.QueryToolChallengesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}
	toolID, err := validateQueryIdentifier("tool_id", req.ToolId)
	if err != nil {
		return nil, err
	}
	if err := validatePaginationRequest(req.Pagination); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Use tool index for efficient lookup instead of full participant table scan.
	challengeIDs, err := keeper.GetChallengeIDsByTool(goCtx, toolID)
	if err != nil {
		return nil, fmt.Errorf("failed to search tool index: %w", err)
	}

	type toolChallengeResult struct {
		challenge *types.Challenge
		ranking   *types.Ranking
	}

	results := make([]toolChallengeResult, 0, len(challengeIDs))
	for _, cid := range challengeIDs {
		ch, err := keeper.GetChallenge(goCtx, cid)
		if err != nil {
			continue
		}
		result := toolChallengeResult{challenge: ch}

		r, err := keeper.GetRanking(goCtx, cid, toolID)
		if err == nil && r != nil {
			result.ranking = r
		}
		results = append(results, result)
	}

	paged, pageRes, err := paginateSlice(results, req.Pagination)
	if err != nil {
		return nil, err
	}

	challenges := make([]*types.Challenge, 0, len(paged))
	rankings := make([]*types.Ranking, 0, len(paged))
	for _, result := range paged {
		challenge, err := cloneChallenge(result.challenge)
		if err != nil {
			return nil, err
		}
		challenges = append(challenges, challenge)
		if result.ranking != nil {
			ranking, err := cloneRanking(result.ranking)
			if err != nil {
				return nil, err
			}
			rankings = append(rankings, ranking)
		}
	}

	return &types.QueryToolChallengesResponse{
		Challenges: challenges,
		Rankings:   rankings,
		Pagination: pageRes,
	}, nil
}

// paginateChallenges applies offset/key/limit pagination from the Cosmos
// PageRequest to a slice of challenges.
func paginateChallenges(items []*types.Challenge, page *sdkquery.PageRequest) ([]*types.Challenge, *sdkquery.PageResponse, error) {
	return paginateSlice(items, page)
}

func paginateParticipants(items []*types.Participant, page *sdkquery.PageRequest) ([]*types.Participant, *sdkquery.PageResponse, error) {
	return paginateSlice(items, page)
}

func validatePaginationRequest(page *sdkquery.PageRequest) error {
	if page == nil {
		return nil
	}
	if page.GetReverse() {
		return fmt.Errorf("reverse pagination not supported")
	}
	if len(page.GetKey()) == 0 {
		return nil
	}
	if page.GetOffset() > 0 {
		return fmt.Errorf("invalid pagination request: key and offset are mutually exclusive")
	}
	if _, err := strconv.ParseUint(string(page.GetKey()), 10, 64); err != nil {
		return fmt.Errorf("invalid pagination key: %w", err)
	}
	return nil
}

func validateChallengeQueryEnums(req *types.QueryChallengesRequest) error {
	if _, ok := types.ChallengeStatus_name[int32(req.Status)]; !ok {
		return fmt.Errorf("status has invalid value %d", req.Status)
	}
	if _, ok := types.ChallengeType_name[int32(req.ChallengeType)]; !ok {
		return fmt.Errorf("challenge_type has invalid value %d", req.ChallengeType)
	}
	return nil
}

func validateQueryIdentifier(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return "", fmt.Errorf("%s must not have leading or trailing whitespace", field)
	}
	if len(value) > types.MaxChallengeIDLen {
		return "", fmt.Errorf("%s length %d exceeds maximum %d", field, len(value), types.MaxChallengeIDLen)
	}
	return value, nil
}

func paginateSlice[T any](items []T, page *sdkquery.PageRequest) ([]T, *sdkquery.PageResponse, error) {
	if page == nil {
		return items, nil, nil
	}
	if page.GetReverse() {
		return nil, nil, fmt.Errorf("reverse pagination not supported")
	}

	offset := page.GetOffset()
	if key := page.GetKey(); len(key) > 0 {
		if offset > 0 {
			return nil, nil, fmt.Errorf("invalid pagination request: key and offset are mutually exclusive")
		}
		parsed, err := strconv.ParseUint(string(key), 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid pagination key: %w", err)
		}
		offset = parsed
	}

	total := uint64(len(items))
	pageRes := &sdkquery.PageResponse{}
	if page.GetCountTotal() {
		pageRes.Total = total
	}

	if offset >= total {
		return nil, pageRes, nil
	}

	end := total
	if limit := page.GetLimit(); limit > 0 && limit < total-offset {
		end = offset + limit
		pageRes.NextKey = []byte(strconv.FormatUint(end, 10))
	}

	return items[int(offset):int(end)], pageRes, nil
}

// deepCopyProto copies src into dst via a gogo marshal/unmarshal round-trip.
// proto.Clone panics on gogo customtype fields (math.Int inside sdk.Coin:
// "merger not found for type:big.Word"), so it cannot be used here.
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

func cloneParams(params *types.Params) (*types.Params, error) {
	if params == nil {
		return nil, nil
	}
	cloned := &types.Params{}
	if !deepCopyProto(params, cloned) {
		return nil, fmt.Errorf("failed to clone params")
	}
	return cloned, nil
}

func cloneChallenge(challenge *types.Challenge) (*types.Challenge, error) {
	if challenge == nil {
		return nil, nil
	}
	cloned := &types.Challenge{}
	if !deepCopyProto(challenge, cloned) {
		return nil, fmt.Errorf("failed to clone challenge")
	}
	return cloned, nil
}

func cloneChallenges(challenges []*types.Challenge) ([]*types.Challenge, error) {
	cloned := make([]*types.Challenge, 0, len(challenges))
	for _, challenge := range challenges {
		item, err := cloneChallenge(challenge)
		if err != nil {
			return nil, err
		}
		cloned = append(cloned, item)
	}
	return cloned, nil
}

func cloneParticipant(participant *types.Participant) (*types.Participant, error) {
	if participant == nil {
		return nil, nil
	}
	cloned := &types.Participant{}
	if !deepCopyProto(participant, cloned) {
		return nil, fmt.Errorf("failed to clone participant")
	}
	return cloned, nil
}

func cloneParticipants(participants []*types.Participant) ([]*types.Participant, error) {
	cloned := make([]*types.Participant, 0, len(participants))
	for _, participant := range participants {
		item, err := cloneParticipant(participant)
		if err != nil {
			return nil, err
		}
		cloned = append(cloned, item)
	}
	return cloned, nil
}

func cloneRanking(ranking *types.Ranking) (*types.Ranking, error) {
	if ranking == nil {
		return nil, nil
	}
	cloned := &types.Ranking{}
	if !deepCopyProto(ranking, cloned) {
		return nil, fmt.Errorf("failed to clone ranking")
	}
	return cloned, nil
}

func cloneRankings(rankings []*types.Ranking) ([]*types.Ranking, error) {
	cloned := make([]*types.Ranking, 0, len(rankings))
	for _, ranking := range rankings {
		item, err := cloneRanking(ranking)
		if err != nil {
			return nil, err
		}
		cloned = append(cloned, item)
	}
	return cloned, nil
}
