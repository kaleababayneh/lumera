package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/google/uuid"
	metrics "github.com/hashicorp/go-metrics"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
	"github.com/LumeraProtocol/lumera/x/router/types"
)

// Ensure Keeper implements the Router gRPC service
var _ types.RouterServer = (*Keeper)(nil)

// Discover finds and ranks tools for a given intent (whitepaper section 2.2)
func (k Keeper) Discover(goCtx context.Context, req *types.DiscoverRequest) (resp *types.DiscoverResponse, err error) {
	start := telemetryNow()
	defer func() {
		code := telemetryCodeFromError(err)
		telemetryRecordGRPC("discover", code, start, nil)
	}()

	if req == nil || req.Intent == nil {
		return nil, status.Error(codes.InvalidArgument, "intent required")
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	traceID := uuid.NewString()
	logger := k.Logger(sdkCtx).With(
		"method", "Discover",
		"height", sdkCtx.BlockHeight(),
		"trace_id", traceID,
	)

	allowedCategories := normalizeStrings(req.GetConstraints().GetAllowedCategories())
	toolCards := k.registryKeeper.GetAllTools(sdkCtx)
	if len(allowedCategories) > 0 {
		toolCards = filterToolsByCategories(toolCards, allowedCategories)
	}

	budget, err := types.ParseDecimal(strings.TrimSpace(req.Intent.BudgetMax))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid budget_max: %v", err)
	}

	deniedMatchers := compileDeniedPatterns(req.GetConstraints().GetDeniedTools())

	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		logger.Error("failed to load router params", "error", err)
		return nil, status.Error(codes.Internal, "unable to load router parameters")
	}

	candidates := make([]*types.ToolCandidate, 0, len(toolCards))

	for _, card := range toolCards {
		if card == nil {
			continue
		}

		toolID := strings.TrimSpace(card.ToolId)
		if toolID == "" {
			continue
		}
		if matchesDenied(toolID, deniedMatchers) {
			continue
		}

		estCost := deriveToolCost(card)
		if budget.GreaterThan(decimal.Zero) && estCost.GreaterThan(budget) {
			continue
		}

		metrics := k.lookupToolMetrics(goCtx, sdkCtx, toolID)
		p95 := deriveP95Latency(card, metrics)

		score := k.computeDiscoveryScore(sdkCtx, toolID, metrics, estCost, budget)
		verified := isToolVerified(card, metrics, params)
		rationale := buildDiscoveryRationale(estCost, verified, metrics)
		cacheStatus := k.checkCacheStatus(sdkCtx, toolID, req.Intent)

		candidates = append(candidates, &types.ToolCandidate{
			ToolId:      toolID,
			Rationale:   rationale,
			EstCost:     estCost.String(),
			P95Ms:       p95,
			Score:       score,
			Verified:    verified,
			CacheStatus: cacheStatus,
		})
	}

	sortDiscoveryCandidates(candidates)

	maxCandidates := int(params.GetActiveSetLimit())
	if maxCandidates <= 0 {
		maxCandidates = 8
	}
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	logger.Info("discovery complete", "candidate_count", len(candidates), "allowed_categories", allowedCategories)

	resp = &types.DiscoverResponse{Candidates: candidates, TraceId: traceID}
	return resp, nil
}

// Quote gets cost and performance estimates for a tool invocation
func (k Keeper) Quote(goCtx context.Context, req *types.QuoteRequest) (resp *types.QuoteResponse, err error) {
	start := telemetryNow()
	var labels []metrics.Label
	defer func() {
		code := telemetryCodeFromError(err)
		telemetryRecordGRPC("quote", code, start, labels)
	}()

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	labels = telemetryLabelsForTool(req.GetToolId())

	// Reject oversized args BEFORE any args-iterating work below
	// (ComputeCacheKey sorts + sha256s every entry via json.Marshal's
	// deterministic map-key ordering). Invoke is protected by the same
	// cap via CheckInvocationReplay → CheckAndRecordInvocation
	// (replay.go:MaxInvokeArgs), but Quote never calls the replay
	// path and LockCredits below writes state on every validator —
	// leaving Quote's args-hash path as an unbounded-compute DoS
	// surface with the same amplification profile as lumera_ai-o5xc1.
	// Reuse the same MaxInvokeArgs constant since the bound reasoning
	// (realistic args ≪ 128, tx-size cap ~10MB admits 100k+ tiny
	// entries) is identical for QuoteRequest.
	if len(req.Args) > MaxInvokeArgs {
		return nil, status.Errorf(codes.InvalidArgument,
			"quote args count %d exceeds maximum %d",
			len(req.Args), MaxInvokeArgs)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	logger := k.Logger(ctx).With(
		"method", "Quote",
		"height", ctx.BlockHeight(),
		"tool_id", strings.TrimSpace(req.GetToolId()),
		"session_id", strings.TrimSpace(req.GetSessionId()),
	)

	// Verify tool exists
	tool, exists := k.registryKeeper.GetToolCard(ctx, req.ToolId)
	if !exists || tool == nil {
		logger.Warn("tool not found", "tool_id", req.ToolId)
		return nil, status.Error(codes.NotFound, "tool not found")
	}

	// Generate quote ID
	quoteID := uuid.New().String()

	// Calculate estimated cost
	estCost := k.calculateQuoteCost(ctx, tool, req.Args)

	// Check if cost exceeds max.
	//
	// req.MaxCost is a caller-supplied string field on the Quote gRPC
	// request. shopspring.decimal.NewFromString parses "1e11100100"
	// instantly (exponent stored symbolically, no big.Int expansion
	// at parse time), but the subsequent estCost.GreaterThan(maxCost)
	// Cmp-family op must align exponents and expand the big.Int to
	// the full decimal representation — a ~1.3s hang per op on
	// adversarial exponents, measured on sibling sites in this sweep
	// (internal/strategy/execute receipt.go commit c1ec4b822 and
	// internet/manifest parseDecimal commit b21923578). Keeper gRPC
	// runs on validators, so a hung call blocks consensus state
	// transitions — strictly a halt-vector, not just a tool-side DoS.
	// Gate via moneyguard.IsSafeExponent before any arithmetic to
	// match the pattern used across the codebase.
	if req.MaxCost != "" {
		maxCost, parseErr := parseRequestMaxCost(req.MaxCost)
		if parseErr != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid max_cost: %v", parseErr)
		}
		if estCost.GreaterThan(maxCost) {
			logger.Warn("estimated cost exceeds maximum", "estimated_cost", estCost.String(), "max_cost", maxCost.String())
			return nil, status.Error(codes.ResourceExhausted, "estimated cost exceeds maximum")
		}
	}

	// Get performance metrics
	p95Ms := k.getP95Latency(ctx, req.ToolId)

	// Check cache eligibility
	cacheEligible := k.isCacheEligible(ctx, req.ToolId, req.Args)

	var sessionState *types.SessionState
	if sessionID := strings.TrimSpace(req.GetSessionId()); sessionID != "" {
		state, loadErr := k.GetOrCreateSession(goCtx, sessionID)
		if loadErr != nil {
			return nil, status.Errorf(codes.Internal, "failed to load session: %v", loadErr)
		}
		sessionState = state
	}

	lockedCoin, convertErr := decimalToCreditCoin(estCost)
	if convertErr != nil {
		logger.Error("failed to convert estimate to coin", "error", convertErr)
		return nil, status.Errorf(codes.Internal, "failed to calculate locked amount: %v", convertErr)
	}

	intentHash := k.ComputeCacheKey(req.ToolId, req.Args)

	lockID, lockErr := k.creditsKeeper.LockCredits(
		goCtx,
		routerLockAddress(strings.TrimSpace(k.authority), sessionState),
		req.GetSessionId(),
		lockedCoin,
		req.ToolId,
		quoteID,
		sessionPolicyVersion(sessionState),
		intentHash,
		"",
	)
	if lockErr != nil {
		logger.Error("lock credits failed", "error", lockErr, "quote_id", quoteID, "intent_hash", intentHash)
		return nil, statusFromRouterError(lockErr, codes.Internal, "failed to lock credits")
	}

	labels = telemetryAppendLabel(labels, "cache_eligible", strconv.FormatBool(cacheEligible))

	traceParts := []string{
		fmt.Sprintf("quote=%s", quoteID),
		fmt.Sprintf("lock=%s", lockID),
		fmt.Sprintf("cache_eligible=%t", cacheEligible),
		fmt.Sprintf("intent_hash=%s", intentHash),
	}

	resp = &types.QuoteResponse{
		QuoteId:       quoteID,
		EstCost:       estCost.String(),
		P95Ms:         uint32(p95Ms),
		ValidityS:     120,
		LockedAmount:  lockedCoin.String(),
		CacheEligible: cacheEligible,
		Trace:         strings.Join(traceParts, " "),
	}

	return resp, nil
}

// Invoke executes a tool and returns signed receipt
func (k Keeper) Invoke(goCtx context.Context, req *types.InvokeRequest) (resp *types.InvokeResponse, err error) {
	start := telemetryNow()
	var labels []metrics.Label
	defer func() {
		code := telemetryCodeFromError(err)
		telemetryRecordGRPC("invoke", code, start, labels)
	}()

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	labels = telemetryLabelsForTool(req.ToolId)

	ctx := sdk.UnwrapSDKContext(goCtx)
	logger := k.Logger(ctx).With(
		"method", "Invoke",
		"height", ctx.BlockHeight(),
		"tool_id", strings.TrimSpace(req.GetToolId()),
		"session_id", strings.TrimSpace(req.GetSessionId()),
		"quote_id", strings.TrimSpace(req.GetQuoteId()),
	)

	// Verify tool exists
	toolCard, exists := k.registryKeeper.GetToolCard(ctx, req.ToolId)
	if !exists || toolCard == nil {
		logger.Warn("tool not found", "tool_id", req.ToolId)
		return nil, status.Error(codes.NotFound, "tool not found")
	}

	// Enforce replay protection before executing the tool (Phase 2.13.2.4)
	if err := k.CheckInvocationReplay(goCtx, req); err != nil {
		logger.Error("replay protection failed", "error", err)
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}

	sessionUser := ""
	if sessionID := strings.TrimSpace(req.SessionId); sessionID != "" {
		if session, err := k.GetSession(goCtx, sessionID); err == nil && session != nil {
			sessionUser = strings.TrimSpace(session.UserAddr)
		} else if err != nil && !errors.Is(err, types.ErrSessionNotFound) {
			logger.Error("failed to load session", "session_id", sessionID, "error", err)
			return nil, status.Errorf(codes.Internal, "failed to load session: %v", err)
		}
	}

	// Check cache first if allowed
	var cacheHit bool
	var output string
	var originToolID string

	if req.AcceptCached {
		// Use GetCachedResult to retrieve full metadata including origin tool ID
		if cachedEntry, found := k.GetCachedResult(goCtx, req.ToolId, req.Args); found {
			cacheHit = true
			output = cachedEntry.Response
			// Get origin tool ID from cache metadata for CAC royalty distribution
			originToolID = cachedEntry.OriginToolId
			if originToolID == "" {
				// This should not happen with properly cached entries
				// Log warning and treat as no origin (no CAC royalties)
				logger.Warn("cache entry missing originToolID metadata", "tool", req.ToolId, "cache_key", cachedEntry.CacheKey)
				// Don't set originToolID - let it stay empty so no CAC split happens
			}
		}
	}

	// If not cached, record a synthesized execution placeholder.
	//
	// This is a KEEPER-SCOPE invocation path — Cosmos keepers cannot
	// do network I/O (consensus-determinism requires identical state
	// transitions across all validators). Real tool execution happens
	// OFF-CHAIN via routerd (internal/router/executor_local.go) which
	// posts signed receipts back for on-chain settlement; the keeper
	// only records that an invocation was authorized and computes
	// credit debits. The placeholder string below exists so receipt
	// tests that run against the keeper alone have a deterministic
	// output-shape to assert on.
	//
	// A caller that routes through the keeper's Invoke directly
	// (rather than through routerd's HTTP/gRPC surface) MUST treat
	// this placeholder as "execution authorized, real output not
	// available here" — do NOT parse the result as a tool response.
	if !cacheHit {
		output = fmt.Sprintf(`{"result": "executed %s"}`, req.ToolId)

		// Store in cache if deterministic (with full metadata including originToolID)
		// When first caching, the executing tool IS the origin tool
		if k.isCacheable(ctx, req.ToolId) {
			if err := k.CacheResult(ctx, req.ToolId, req.Args, output, req.ToolId); err != nil {
				logger.Error("failed to cache invocation output", "tool", req.ToolId, "error", err)
			}
		}

		// Set originToolID for the receipt since this is a fresh execution
		originToolID = req.ToolId
	}

	if cacheHit {
		if err := k.RecordCacheHit(goCtx, req.ToolId); err != nil {
			return nil, fmt.Errorf("record cache hit: %w", err)
		}
	} else {
		if err := k.RecordCacheMiss(goCtx, req.ToolId); err != nil {
			return nil, fmt.Errorf("record cache miss: %w", err)
		}
	}

	labels = telemetryAppendLabel(labels, "cache_hit", strconv.FormatBool(cacheHit))

	// Calculate actual cost
	actualCost := k.calculateActualCost(ctx, toolCard, req.Args, cacheHit)
	quotedCost := actualCost // Would come from quote if provided

	var quoteLockID string
	var quoteIntentHash string
	if req.QuoteId != "" {
		// Validate against quote
		quoteRecord, err := k.GetQuote(goCtx, req.QuoteId)
		if err != nil {
			logger.Error("quote lookup failed", "quote_id", req.QuoteId, "error", err)
			return nil, status.Errorf(codes.NotFound, "quote not found: %s", req.QuoteId)
		}
		if quoteToolID := strings.TrimSpace(quoteRecord.GetToolId()); quoteToolID != strings.TrimSpace(req.GetToolId()) {
			logger.Warn("quote tool mismatch", "quote_tool", quoteToolID, "request_tool", req.GetToolId(), "quote_id", req.QuoteId)
			return nil, status.Error(codes.InvalidArgument, "quote was issued for a different tool")
		}
		if ctx.BlockTime().After(quoteRecord.ValidUntilTime()) {
			logger.Warn("quote expired", "quote_id", req.QuoteId)
			return nil, status.Error(codes.DeadlineExceeded, "quote expired")
		}
		quotedCost = quoteRecord.EstCostDecimal()
		quoteLockID = strings.TrimSpace(quoteRecord.LockId)
		quoteIntentHash = strings.TrimSpace(quoteRecord.IntentHash)
	}

	// Check max cost. Same DoS gate as Quote — req.MaxCost is
	// caller-controlled and GreaterThan on an absurd-exponent value
	// expands the big.Int mid-handler, blocking consensus.
	if req.MaxCost != "" {
		maxCost, err := parseRequestMaxCost(req.MaxCost)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid max_cost: %v", err)
		}
		if actualCost.GreaterThan(maxCost) {
			logger.Warn("actual cost exceeds maximum", "actual_cost", actualCost.String(), "max_cost", maxCost.String())
			return nil, status.Error(codes.ResourceExhausted, "actual cost exceeds maximum")
		}
	}

	latencyMs := uint32(100)

	// Use tool card's owner pubkey for the simulated receipt
	publisherPubkey := "ed448:simulated"
	if toolCard != nil && toolCard.GetOwnerPubkey() != "" {
		publisherPubkey = toolCard.GetOwnerPubkey()
	}

	// Create receipt
	receipt := &types.Receipt{
		ToolId:          req.ToolId,
		RequestId:       uuid.New().String(),
		RequestHash:     k.hashRequest(req),
		UnitsUsed:       "1.0", // Would come from tool response
		Unit:            "req",
		PricePerUnit:    actualCost.String(),
		Ts:              ctx.BlockTime().Unix(),
		PublisherPubkey: publisherPubkey,
		ActualCost:      actualCost.String(),
		QuotedCost:      quotedCost.String(),
		CacheHit:        cacheHit,
		OriginToolId:    originToolID,
	}

	publisherSigHex := hex.EncodeToString([]byte("publisher_signature"))
	routerSigHex := hex.EncodeToString([]byte("router_signature"))

	// Sign the receipt
	signedReceipt := &types.SignedReceipt{
		Receipt:      receipt,
		PublisherSig: publisherSigHex,
		RouterSig:    routerSigHex,
	}

	// Submit receipt to registry for settlement
	routerAddr := strings.TrimSpace(k.authority)
	if routerAddr == "" && toolCard != nil {
		routerAddr = strings.TrimSpace(toolCard.GetOwner())
	}
	if registryReceipt := toRegistryUsageReceipt(
		receipt,
		routerAddr,
		sessionUser,
		strings.TrimSpace(req.SessionId),
		strings.TrimSpace(req.GetQuoteId()),
		quoteLockID,
		quoteIntentHash,
		signedReceipt,
	); registryReceipt != nil {
		if len(registryReceipt.GetAttestationProof()) == 0 {
			logger.Error("refusing to submit receipt with empty attestation proof", "tool", req.ToolId, "request_id", registryReceipt.GetRequestId())
			return nil, status.Error(codes.Internal, "attestation proof missing from canonical receipt")
		}
		if err := k.registryKeeper.SubmitReceipt(ctx, registryReceipt, registryReceipt.GetPublisherSig()); err != nil {
			logger.Error("failed to submit receipt", "tool", req.ToolId, "error", err)
			return nil, status.Errorf(codes.Internal, "failed to submit receipt: %v", err)
		}
	}

	// Create execution trace
	trace := &types.ExecutionTrace{
		TraceId:           uuid.New().String(),
		Rationale:         fmt.Sprintf("Selected %s based on score", req.ToolId),
		Price:             actualCost.String(),
		LatencyMs:         latencyMs,
		PolicyFlags:       []string{"verified", "jurisdiction_ok"},
		CacheHit:          cacheHit,
		InsuranceEstimate: k.calculateInsurance(actualCost).String(),
	}

	if err := k.RecordInvocation(goCtx, req.ToolId, req.SessionId, sessionUser, actualCost, latencyMs, true, cacheHit, originToolID); err != nil && !errors.Is(err, types.ErrMetricsDisabled) {
		logger.Error("failed to record invocation", "tool", req.ToolId, "error", err)
	}

	// Calculate refund if applicable
	refundAmount := "0"
	if quotedCost.GreaterThan(actualCost) {
		refundAmount = quotedCost.Sub(actualCost).String()
	}

	logger.Info("invoke handled",
		"cache_hit", cacheHit,
		"actual_cost", actualCost.String(),
		"quoted_cost", quotedCost.String(),
		"origin_tool_id", originToolID,
	)

	resp = &types.InvokeResponse{
		Output:       output,
		Receipt:      signedReceipt,
		Trace:        trace,
		RefundAmount: refundAmount,
	}

	return resp, nil
}

// Activate adds a tool to the session's active set via the router service.
func (k Keeper) Activate(goCtx context.Context, req *types.ActivateRequest) (resp *types.ActivateResponse, err error) {
	start := telemetryNow()
	var labels []metrics.Label
	defer func() {
		code := telemetryCodeFromError(err)
		if code == codes.OK && resp != nil && !resp.Success {
			code = codes.FailedPrecondition
		}
		telemetryRecordGRPC("activate", code, start, labels)
	}()

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	sessionID := strings.TrimSpace(req.SessionId)
	toolID := strings.TrimSpace(req.ToolId)
	labels = telemetryLabelsForTool(toolID)
	if sessionID == "" || toolID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id and tool_id are required")
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	logger := k.Logger(sdkCtx).With(
		"method", "Activate",
		"height", sdkCtx.BlockHeight(),
		"session_id", sessionID,
		"tool_id", toolID,
	)

	session, err := k.GetOrCreateSession(goCtx, sessionID)
	if err != nil {
		logger.Error("failed to load session", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to load session: %v", err)
	}

	alreadyActive := false
	for _, activeTool := range session.ActiveTools {
		if activeTool == toolID {
			alreadyActive = true
			break
		}
	}

	if !alreadyActive {
		if err := k.AddToolToActiveSet(goCtx, sessionID, toolID); err != nil {
			logger.Info("activation rejected", "reason", err.Error())
			return k.activationErrorResponse(goCtx, sdkCtx, sessionID, err)
		}
		session, err = k.GetOrCreateSession(goCtx, sessionID)
		if err != nil {
			logger.Error("failed to refresh session", "error", err)
			return nil, status.Errorf(codes.Internal, "failed to refresh session: %v", err)
		}
	}

	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load params: %v", err)
	}

	limit := int(params.GetActiveSetLimit())
	if limit <= 0 {
		limit = int(types.DefaultParams().GetActiveSetLimit())
	}
	remaining := limit - len(session.ActiveTools)
	if remaining < 0 {
		remaining = 0
	}

	message := "Tool activated successfully"
	labels = telemetryAppendLabel(labels, "already_active", strconv.FormatBool(alreadyActive))
	if alreadyActive {
		message = "Tool already active"
	}

	resp = &types.ActivateResponse{
		Success:     true,
		Message:     message,
		ActiveTools: session.ActiveTools,
		// #nosec G115 -- remaining is bounded by active set limit param
		RemainingSlots: uint32(remaining),
	}

	logger.Info("activation handled", "success", resp.Success, "remaining_slots", resp.RemainingSlots)

	return resp, nil
}

func (k Keeper) activationErrorResponse(goCtx context.Context, sdkCtx sdk.Context, sessionID string, err error) (*types.ActivateResponse, error) {
	switch {
	case errors.Is(err, types.ErrActiveSetFull),
		errors.Is(err, types.ErrCooldownActive),
		errors.Is(err, types.ErrInvalidScore):
		session, loadErr := k.GetOrCreateSession(goCtx, sessionID)
		if loadErr != nil {
			return nil, status.Errorf(codes.Internal, "failed to load session: %v", loadErr)
		}
		params, paramsErr := k.paramsOrDefault(sdkCtx)
		if paramsErr != nil {
			return nil, status.Errorf(codes.Internal, "failed to load params: %v", paramsErr)
		}
		limit := int(params.GetActiveSetLimit())
		if limit <= 0 {
			limit = int(types.DefaultParams().GetActiveSetLimit())
		}
		remaining := limit - len(session.ActiveTools)
		if remaining < 0 {
			remaining = 0
		}
		return &types.ActivateResponse{
			Success:     false,
			Message:     err.Error(),
			ActiveTools: session.ActiveTools,
			// #nosec G115 -- remaining is bounded by active set limit param
			RemainingSlots: uint32(remaining),
		}, nil
	case errors.Is(err, types.ErrToolNotFound):
		return nil, status.Error(codes.NotFound, err.Error())
	case errors.Is(err, types.ErrInvalidParams):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	default:
		return nil, status.Errorf(codes.Internal, "failed to activate tool: %v", err)
	}
}

// Deactivate removes a tool from the active set
func (k Keeper) Deactivate(goCtx context.Context, req *types.DeactivateRequest) (resp *types.DeactivateResponse, err error) {
	start := telemetryNow()
	var labels []metrics.Label
	defer func() {
		code := telemetryCodeFromError(err)
		if code == codes.OK && resp != nil && !resp.Success {
			code = codes.FailedPrecondition
		}
		telemetryRecordGRPC("deactivate", code, start, labels)
	}()

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	sessionID := strings.TrimSpace(req.SessionId)
	toolID := strings.TrimSpace(req.ToolId)
	labels = telemetryLabelsForTool(toolID)
	if sessionID == "" || toolID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id and tool_id are required")
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	logger := k.Logger(sdkCtx).With(
		"method", "Deactivate",
		"height", sdkCtx.BlockHeight(),
		"session_id", sessionID,
		"tool_id", toolID,
	)

	if err := k.RemoveToolFromActiveSet(goCtx, sessionID, toolID); err != nil {
		switch {
		case errors.Is(err, types.ErrToolNotActive):
			session, loadErr := k.GetSession(goCtx, sessionID)
			if loadErr != nil {
				logger.Error("failed to load session", "error", loadErr)
				return nil, status.Errorf(codes.Internal, "failed to load session: %v", loadErr)
			}
			return &types.DeactivateResponse{
				Success:     false,
				Message:     err.Error(),
				ActiveTools: session.ActiveTools,
			}, nil
		case errors.Is(err, types.ErrInvalidParams):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		default:
			logger.Error("failed to deactivate tool", "error", err)
			return nil, status.Errorf(codes.Internal, "failed to deactivate tool: %v", err)
		}
	}

	session, err := k.GetSession(goCtx, sessionID)
	if err != nil {
		logger.Error("failed to refresh session", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to refresh session: %v", err)
	}

	var cooldownUnix int64
	if session.CooldownUntil != nil {
		if until, ok := session.CooldownUntilTime(toolID); ok && !until.IsZero() {
			cooldownUnix = until.Unix()
		}
	}

	cooldownActive := cooldownUnix > 0
	labels = telemetryAppendLabel(labels, "cooldown_active", strconv.FormatBool(cooldownActive))

	resp = &types.DeactivateResponse{
		Success:     true,
		Message:     "Tool deactivated successfully",
		ActiveTools: session.ActiveTools,
	}
	if cooldownUnix > 0 {
		resp.CooldownUntil = cooldownUnix
	}

	logger.Info("deactivation handled", "success", resp.Success, "cooldown_until", cooldownUnix)

	return resp, nil
}

// GetActivationPolicy returns current activation constraints
func (k Keeper) GetActivationPolicy(goCtx context.Context, req *types.GetActivationPolicyRequest) (resp *types.GetActivationPolicyResponse, err error) {
	start := telemetryNow()
	var labels []metrics.Label
	defer func() {
		code := telemetryCodeFromError(err)
		telemetryRecordGRPC("get_activation_policy", code, start, labels)
	}()

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	sessionIDLog := ""
	if req != nil {
		sessionIDLog = strings.TrimSpace(req.GetSessionId())
	}
	logger := k.Logger(sdkCtx).With(
		"method", "GetActivationPolicy",
		"height", sdkCtx.BlockHeight(),
		"session_id", sessionIDLog,
	)

	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		logger.Error("failed to load params", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to load params: %v", err)
	}

	resp = &types.GetActivationPolicyResponse{
		ActiveSetLimit:  params.GetActiveSetLimit(),
		CooldownSeconds: params.GetCooldownSeconds(),
		TtlSeconds:      params.GetSessionTtlSeconds(),
		Constraints:     make(map[string]string),
	}

	if resp.ActiveSetLimit == 0 {
		resp.ActiveSetLimit = types.DefaultParams().GetActiveSetLimit()
	}
	if resp.TtlSeconds == 0 {
		resp.TtlSeconds = types.DefaultParams().GetSessionTtlSeconds()
	}

	minRep := params.MinReputationScoreDecimal()
	if minRep.GreaterThan(decimal.Zero) {
		resp.Constraints["min_reputation_score"] = minRep.String()
	}
	resp.Constraints["metrics_enabled"] = strconv.FormatBool(params.GetMetricsEnabled())
	resp.Constraints["cac_enabled"] = strconv.FormatBool(params.GetCacEnabled())
	resp.Constraints["max_tools_per_category"] = strconv.FormatInt(int64(params.GetMaxToolsPerCategory()), 10)

	if req != nil && strings.TrimSpace(req.SessionId) != "" {
		sessionID := strings.TrimSpace(req.SessionId)
		if session, sessionErr := k.GetSession(goCtx, sessionID); sessionErr == nil {
			resp.Constraints["session"] = sessionID
			if len(session.ActiveTools) > 0 {
				resp.Constraints["active_tools"] = strings.Join(session.ActiveTools, ",")
			}
		}
	}

	logger.Info("activation policy fetched", "active_set_limit", resp.ActiveSetLimit, "cooldown_seconds", resp.CooldownSeconds)

	return resp, nil
}

type toolMetricsSnapshot struct {
	activation *types.ActivationMetrics
	registry   *registrytypes.ToolMetrics
}

func sessionPolicyVersion(session *types.SessionState) string {
	if session == nil {
		return "v1"
	}
	if policy := strings.TrimSpace(session.GetPolicyVersion()); policy != "" {
		return policy
	}
	return "v1"
}

func routerLockAddress(defaultAddr string, session *types.SessionState) string {
	if session != nil && strings.TrimSpace(session.GetUserAddr()) != "" {
		return session.GetUserAddr()
	}
	return defaultAddr
}

const creditPrecisionFactor = 1_000_000

var (
	fallbackQuoteCost         = decimal.RequireFromString("0.05")
	fallbackActualCost        = decimal.RequireFromString("0.08")
	insuranceContributionRate = decimal.RequireFromString("0.015")
)

func decimalToCreditCoin(amount decimal.Decimal) (sdk.Coin, error) {
	if amount.IsNegative() {
		return sdk.Coin{}, fmt.Errorf("quote amount cannot be negative")
	}
	scaled := amount.Mul(decimal.NewFromInt(creditPrecisionFactor))
	if scaled.IsZero() {
		scaled = decimal.NewFromInt(1)
	}
	units := scaled.Ceil()
	if units.IsNegative() {
		units = decimal.NewFromInt(1)
	}
	coinAmount := sdkmath.NewIntFromBigInt(units.BigInt())
	if !coinAmount.IsPositive() {
		coinAmount = sdkmath.NewInt(1)
	}
	return sdk.NewCoin(creditstypes.DefaultCreditDenom, coinAmount), nil
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func filterToolsByCategories(tools []*registrytypes.ToolCard, allowed []string) []*registrytypes.ToolCard {
	if len(allowed) == 0 {
		return tools
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, category := range allowed {
		key := strings.ToLower(strings.TrimSpace(category))
		if key == "" {
			continue
		}
		allowedSet[key] = struct{}{}
	}
	if len(allowedSet) == 0 {
		return tools
	}
	filtered := make([]*registrytypes.ToolCard, 0, len(tools))
	for _, card := range tools {
		if card == nil {
			continue
		}
		if toolMatchesAllowedCategories(card, allowedSet) {
			filtered = append(filtered, card)
		}
	}
	return filtered
}

func toolMatchesAllowedCategories(card *registrytypes.ToolCard, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, category := range card.GetCategories() {
		key := strings.ToLower(strings.TrimSpace(category))
		if _, ok := allowed[key]; ok {
			return true
		}
	}
	return false
}

func matchesError(err error, targets ...error) bool {
	if err == nil {
		return false
	}
	if errorsmod.IsOf(err, targets...) {
		return true
	}
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func statusFromRouterError(err error, fallback codes.Code, fallbackMsg string) error {
	if err == nil {
		return nil
	}

	switch {
	case matchesError(err, types.ErrInvalidParams, creditstypes.ErrInvalidParams):
		return status.Error(codes.InvalidArgument, err.Error())
	case matchesError(err, types.ErrToolNotFound, creditstypes.ErrLockNotFound):
		return status.Error(codes.NotFound, err.Error())
	case matchesError(err, types.ErrActiveSetFull):
		return status.Error(codes.ResourceExhausted, err.Error())
	case matchesError(err, types.ErrCooldownActive, creditstypes.ErrLockExpired, creditstypes.ErrLockInactive, types.ErrInvalidScore):
		return status.Error(codes.FailedPrecondition, err.Error())
	case matchesError(err, types.ErrUnauthorized):
		return status.Error(codes.PermissionDenied, err.Error())
	case matchesError(err, creditstypes.ErrInsufficientFunds):
		return status.Error(codes.ResourceExhausted, err.Error())
	case matchesError(err, creditstypes.ErrSettlementFailed, creditstypes.ErrDisputeFailed, creditstypes.ErrReleaseFailed):
		return status.Error(codes.Internal, err.Error())
	}

	if fallbackMsg != "" {
		return status.Errorf(fallback, "%s: %v", fallbackMsg, err)
	}
	return status.Error(fallback, err.Error())
}

func compileDeniedPatterns(values []string) []string {
	patterns := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		patterns = append(patterns, trimmed)
	}
	return patterns
}

func matchesDenied(toolID string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		if matched, err := path.Match(pattern, toolID); err == nil && matched {
			return true
		}
		if strings.EqualFold(pattern, toolID) {
			return true
		}
	}
	return false
}

func toRegistryUsageReceipt(r *types.Receipt, routerAddr, userAddr, sessionID, quoteID, lockID, intentHash string, signed *types.SignedReceipt) *registrytypes.UsageReceipt {
	if r == nil {
		return nil
	}

	timestamp := time.Unix(r.GetTs(), 0).UTC()
	publisherSig, err := hex.DecodeString(signed.GetPublisherSig())
	if err != nil || len(publisherSig) == 0 {
		publisherSig = []byte(signed.GetPublisherSig())
	}
	attestationProof := publisherSig
	if len(attestationProof) == 0 {
		attestationProof = decodeHexOrString(signed.GetRouterSig())
	}
	if len(attestationProof) == 0 {
		// No publisher or router signature available. Bind the attestation
		// proof to the canonical request hash so downstream consumers can
		// distinguish "request witnessed but unsigned" from "attestation
		// present". This is deterministic and request-bound; it is never
		// treated as a cryptographic signature by registry verification.
		if requestHash := strings.TrimSpace(r.GetRequestHash()); requestHash != "" {
			attestationProof = []byte(requestHash)
		}
	}

	routerAddr = strings.TrimSpace(routerAddr)
	userAddr = strings.TrimSpace(userAddr)

	return &registrytypes.UsageReceipt{
		ReceiptId:        r.GetRequestId(),
		ToolId:           r.GetToolId(),
		RequestId:        r.GetRequestId(),
		RequestHash:      []byte(r.GetRequestHash()),
		UnitsUsed:        r.GetUnitsUsed(),
		Unit:             r.GetUnit(),
		PricePerUnit:     r.GetPricePerUnit(),
		QuotedAmount:     r.GetQuotedCost(),
		ActualAmount:     r.GetActualCost(),
		PublisherPubkey:  []byte(r.GetPublisherPubkey()),
		PublisherSig:     publisherSig,
		RouterAddress:    routerAddr,
		UserAddress:      userAddr,
		RouterSig:        decodeHexOrString(signed.GetRouterSig()),
		CacheHit:         r.GetCacheHit(),
		OriginToolId:     r.GetOriginToolId(),
		Timestamp:        timestamp,
		Status:           registrytypes.ReceiptStatusPending,
		AttestationProof: attestationProof,
		SessionId:        strings.TrimSpace(sessionID),
		QuoteId:          strings.TrimSpace(quoteID),
		LockId:           strings.TrimSpace(lockID),
		IntentHash:       strings.TrimSpace(intentHash),
	}
}

func decodeHexOrString(value string) []byte {
	if value == "" {
		return nil
	}
	if decoded, err := hex.DecodeString(value); err == nil {
		return decoded
	}
	return []byte(value)
}

func parseRequestMaxCost(raw string) (decimal.Decimal, error) {
	maxCost, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, err
	}
	if !moneyguard.IsSafeExponent(maxCost) {
		return decimal.Zero, fmt.Errorf("exponent out of safe range")
	}
	return maxCost, nil
}

func deriveToolCost(card *registrytypes.ToolCard) decimal.Decimal {
	if card == nil || card.Pricing == nil {
		return decimal.Zero
	}
	price := strings.TrimSpace(card.Pricing.GetPricePerUnit())
	if price == "" {
		price = strings.TrimSpace(card.Pricing.GetMinimumCost())
	}
	if price == "" {
		return decimal.Zero
	}
	// Tool-card pricing is registered by external publishers via the
	// registry module. The returned value feeds into every quote's
	// cost math (calculateQuoteCost → GreaterThan, Mul), so an
	// adversarial publisher that registered a card with
	// PricePerUnit="1e11100100" would hang every quote request for
	// that tool on the keeper path. Gate at the single parse site to
	// prevent the unsafe value from ever leaving this function.
	if v, err := decimal.NewFromString(price); err == nil && moneyguard.IsSafeExponent(v) && v.IsPositive() {
		return v
	}
	return decimal.Zero
}

func (k Keeper) lookupToolMetrics(_ context.Context, sdkCtx sdk.Context, toolID string) *toolMetricsSnapshot {
	var activation *types.ActivationMetrics
	if m, err := k.state.ToolMetrics.Get(sdkCtx, toolID); err == nil && m != nil {
		activation = m
	} else if err != nil && !errors.Is(err, collections.ErrNotFound) {
		k.Logger(sdkCtx).Error("failed to load activation metrics", "tool", toolID, "error", err)
	}

	registryMetrics, _ := k.registryKeeper.GetToolMetrics(sdkCtx, toolID)
	if activation == nil && registryMetrics == nil {
		return nil
	}
	return &toolMetricsSnapshot{activation: activation, registry: registryMetrics}
}

func deriveP95Latency(card *registrytypes.ToolCard, snapshot *toolMetricsSnapshot) uint32 {
	if snapshot != nil && snapshot.activation != nil && snapshot.activation.P95LatencyMs > 0 {
		return snapshot.activation.P95LatencyMs
	}
	if snapshot != nil && snapshot.registry != nil && snapshot.registry.GetP95LatencyMs() > 0 {
		// #nosec G115 -- P95 latency in ms is bounded by practical API response times
		return uint32(snapshot.registry.GetP95LatencyMs())
	}
	if card != nil && card.Slo != nil && card.Slo.P95LatencyMs > 0 {
		return card.Slo.P95LatencyMs
	}
	return 1500
}

func (k Keeper) computeDiscoveryScore(sdkCtx sdk.Context, toolID string, snapshot *toolMetricsSnapshot, estCost, budget decimal.Decimal) float64 {
	if selection, err := k.state.SelectionScores.Get(sdkCtx, toolID); err == nil && selection != nil {
		overall := selection.OverallScoreDecimal()
		return overall.InexactFloat64()
	}

	one := decimal.NewFromInt(1)
	score := 40.0
	if snapshot != nil && snapshot.activation != nil {
		if snapshot.activation.InvocationCount > 0 {
			if successRate, err := snapshot.activation.SuccessRateDecimalSafe(); err == nil {
				score += successRate.InexactFloat64() * 0.3
			}
		}
		if avgLatency, err := snapshot.activation.AverageLatencyDecimalSafe(); err == nil && !avgLatency.IsZero() {
			latencyPenalty := avgLatency.Div(decimal.NewFromInt(5000)).InexactFloat64() * 20
			score -= latencyPenalty
		}
	}

	if budget.GreaterThan(decimal.Zero) && estCost.GreaterThan(decimal.Zero) {
		ratio := estCost.Div(budget)
		if ratio.LessThan(one) {
			score += one.Sub(ratio).InexactFloat64() * 10
		} else {
			score -= ratio.Sub(one).InexactFloat64() * 10
		}
	}

	if score < 0 {
		score = 0
	}
	return score
}

func buildDiscoveryRationale(estCost decimal.Decimal, verified bool, snapshot *toolMetricsSnapshot) string {
	parts := []string{}
	if !estCost.IsZero() {
		parts = append(parts, fmt.Sprintf("est %s LAC", estCost.StringFixed(2)))
	}
	if verified {
		parts = append(parts, "Lumera Verified")
	}
	if snapshot != nil && snapshot.activation != nil {
		parts = append(parts, fmt.Sprintf("invocations %d", snapshot.activation.InvocationCount))
	}
	if len(parts) == 0 {
		return "eligible tool"
	}
	return strings.Join(parts, ", ")
}

func isToolVerified(card *registrytypes.ToolCard, snapshot *toolMetricsSnapshot, params *types.Params) bool {
	if card != nil {
		for _, tag := range card.Tags {
			if strings.EqualFold(strings.TrimSpace(tag), "verified") {
				return true
			}
		}
		if meta := card.Metadata; meta != nil {
			if v, ok := meta["verified"]; ok && strings.EqualFold(strings.TrimSpace(v), "true") {
				return true
			}
		}
	}
	if snapshot != nil && snapshot.activation != nil {
		minRep := params.MinReputationScoreDecimal()
		if successRate, err := snapshot.activation.SuccessRateDecimalSafe(); err == nil && successRate.GreaterThanOrEqual(minRep) {
			return true
		}
	}
	return false
}

// Helper methods

func (k Keeper) getP95Latency(ctx sdk.Context, toolID string) uint32 {
	card, _ := k.registryKeeper.GetToolCard(ctx, toolID)
	snapshot := k.lookupToolMetrics(ctx, ctx, toolID)
	return deriveP95Latency(card, snapshot)
}

func (k Keeper) checkCacheStatus(ctx sdk.Context, toolID string, intent *types.Intent) string {
	cacheKey := k.computeCacheKeyFromIntent(toolID, intent)

	if k.cacheKeeper != nil {
		// Check if result would be in cache
		if _, found := k.cacheKeeper.Get(ctx, cacheKey); found {
			return "hit"
		}
	}

	// Check local cache
	if _, err := k.state.CacheEntries.Get(ctx, cacheKey); err == nil {
		return "hit"
	}

	if k.isCacheable(ctx, toolID) {
		return "deterministic"
	}

	return "miss"
}

func (k Keeper) calculateQuoteCost(_ sdk.Context, tool *registrytypes.ToolCard, _ map[string]string) decimal.Decimal {
	estimate := deriveToolCost(tool)
	if estimate.IsZero() {
		return fallbackQuoteCost
	}
	return estimate
}

func (k Keeper) isCacheEligible(ctx sdk.Context, toolID string, _ map[string]string) bool {
	return k.isCacheable(ctx, toolID)
}

// isCacheable reports whether invocations of a given tool are
// eligible for the CAC (content-addressed cache) layer.
//
// Returns true unconditionally today — all tools are treated as
// cacheable until a tool-metadata signal declares them otherwise.
// This is a conservative-for-performance default: a tool whose
// output is non-deterministic (e.g., "time", "random", external
// web fetch) will still route through the cache layer and produce
// wrong answers if its output is served from cache.
//
// The correct fix is to read a Cacheable / NonDeterministic flag
// off the ToolCard metadata (registry.types.ToolCard); adding that
// flag + plumbing it here would gate non-deterministic tools out
// of the cache without affecting deterministic ones. Until that
// plumbing lands, publisher best-practice is to NOT ship tools
// whose output varies per-call through lumera_router; there is no
// on-chain enforcement.
func (k Keeper) isCacheable(ctx sdk.Context, toolID string) bool {
	tool, found := k.registryKeeper.GetToolCard(ctx, toolID)
	if !found || tool == nil {
		return false
	}
	if tool.Cache != nil {
		return tool.Cache.Enabled && tool.Cache.Deterministic
	}
	// Default to true if no explicit cache policy is provided, preserving prior behavior
	// but allowing publishers to opt-out via the Deterministic/Enabled flags.
	return true
}

func (k Keeper) computeCacheKeyFromIntent(toolID string, intent *types.Intent) string {
	if intent == nil {
		return k.ComputeCacheKey(toolID, nil)
	}
	return k.ComputeCacheKey(toolID, intent.Inputs)
}

func (k Keeper) calculateActualCost(_ sdk.Context, tool *registrytypes.ToolCard, _ map[string]string, cacheHit bool) decimal.Decimal {
	baseCost := deriveToolCost(tool)
	if baseCost.IsZero() {
		baseCost = fallbackActualCost
	}

	if cacheHit {
		// Cache hit costs DefaultCacheHitCostBPS of base (default 20%).
		// Consistent with ComputeCacheHitCost in cache.go.
		return baseCost.Mul(decimal.NewFromInt(DefaultCacheHitCostBPS)).Div(decimal.NewFromInt(10000))
	}

	return baseCost
}

func (k Keeper) hashRequest(req *types.InvokeRequest) string {
	h := sha256.New()
	h.Write([]byte(req.ToolId))
	if _, err := fmt.Fprintf(h, "%v", req.Args); err != nil {
		k.logger.Error("failed to hash request args", "tool", req.ToolId, "error", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func (k Keeper) calculateInsurance(cost decimal.Decimal) decimal.Decimal {
	// Insurance contribution is typically 1-2% of cost
	return cost.Mul(insuranceContributionRate)
}

// sortDiscoveryCandidates sorts tool candidates by score (descending)
// with lexicographic tie-break on tool_id (ascending). Exposed as a
// package-level function so tests can call the SAME comparator
// production uses, instead of duplicating the sort body in the test
// file (which produced a tautology — the test verified only that Go's
// stdlib sort is deterministic, which is already guaranteed, while
// any drift in the production comparator would go undetected).
//
// Consensus-critical: non-determinism in candidate ordering produces
// different active-set selections across validators → state hash
// divergence. SliceStable is required (not Slice) because equal-score
// pairs must resolve to the same order every call, independent of the
// input's pre-sort ordering.
func sortDiscoveryCandidates(candidates []*types.ToolCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].ToolId < candidates[j].ToolId
		}
		return candidates[i].Score > candidates[j].Score
	})
}
