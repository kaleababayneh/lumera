package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/Masterminds/semver/v3"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/internal/logging"
	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

// PutBundleQuote stores workflow quote replay state.
func (k *Keeper) PutBundleQuote(ctx context.Context, quote *types.BundleQuoteRecord) error {
	if quote == nil {
		return types.ErrInvalidWorkflow.Wrap("bundle quote record cannot be nil")
	}
	if err := quote.Validate(); err != nil {
		return err
	}
	return k.state.BundleQuotes.Set(ctx, quote.BundleID, quote)
}

// GetBundleQuote loads workflow quote replay state.
func (k *Keeper) GetBundleQuote(ctx context.Context, bundleID string) (*types.BundleQuoteRecord, bool, error) {
	bundleID = strings.TrimSpace(bundleID)
	if bundleID == "" {
		return nil, false, types.ErrInvalidWorkflow.Wrap("bundle_id cannot be empty")
	}
	quote, err := k.state.BundleQuotes.Get(ctx, bundleID)
	if err != nil {
		if errorsIsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load bundle quote %s: %w", bundleID, err)
	}
	return quote, true, nil
}

// QuoteWorkflow creates and stores a deterministic signed BundleQuote for a workflow version.
func (k *Keeper) QuoteWorkflow(ctx context.Context, req *types.QuoteWorkflowRequest) (*types.BundleQuote, error) {
	if req == nil {
		return nil, types.ErrInvalidWorkflow.Wrap("quote workflow request cannot be nil")
	}
	workflowID := strings.TrimSpace(req.WorkflowID)
	version := strings.TrimSpace(req.Version)
	nonce := strings.TrimSpace(req.Nonce)
	if workflowID == "" {
		return nil, types.ErrInvalidWorkflow.Wrap("workflow_id cannot be empty")
	}
	if workflowID != req.WorkflowID {
		return nil, types.ErrInvalidWorkflow.Wrapf("quote workflow workflow_id must be canonical: %q", req.WorkflowID)
	}
	if version != req.Version {
		return nil, types.ErrInvalidWorkflow.Wrapf("quote workflow version must be canonical: %q", req.Version)
	}
	if nonce == "" {
		return nil, types.ErrInvalidWorkflow.Wrap("quote nonce cannot be empty")
	}
	if len(nonce) != len(req.Nonce) {
		return nil, types.ErrInvalidWorkflow.Wrapf("quote workflow nonce must be canonical: %q", req.Nonce)
	}
	if version == "" {
		latest, err := k.latestActiveWorkflow(ctx, workflowID)
		if err != nil {
			return nil, err
		}
		version = latest.Version
	}

	workflow, found, err := k.GetWorkflow(ctx, workflowID, version)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, types.ErrInvalidWorkflow.Wrapf("workflow version not found: %s/%s", workflowID, version)
	}
	if workflow.Status != types.WorkflowStatusActive {
		return nil, types.ErrInvalidWorkflow.Wrapf("workflow version is not active: %s/%s", workflowID, version)
	}
	if workflow.Card == nil {
		return nil, types.ErrInvalidWorkflow.Wrapf("workflow version has no card: %s/%s", workflowID, version)
	}

	callerPassport, err := normalizeCallerPassportEvidence(req, workflow.Card.GetPassportRequirements())
	if err != nil {
		return nil, err
	}
	inputsHash, err := types.QuoteInputsHash(req.Inputs)
	if err != nil {
		return nil, err
	}
	routerPubkey, err := types.RouterPubkeyFromPrivateKey(req.RouterPrivateKey)
	if err != nil {
		return nil, err
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	stepQuotes, baseCost, latency, err := buildBundleStepQuotes(workflow.Card, params.BondDenom)
	if err != nil {
		return nil, err
	}
	totalCost, err := quoteTotalCost(baseCost, workflow.Card.GetPricing(), params)
	if err != nil {
		return nil, err
	}
	totalCoin, err := types.NewQuoteCoin(params.BondDenom, totalCost.String())
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	validity := req.Validity
	if validity <= 0 {
		validity = types.DefaultBundleQuoteTTL
	}
	expiresAt := sdkCtx.BlockTime().UTC().Add(validity).Format(time.RFC3339Nano)
	quote := &types.BundleQuote{
		WorkflowID:            workflow.WorkflowID,
		Version:               workflow.Version,
		InputsHash:            inputsHash,
		CallerPassportTier:    callerPassport.tier,
		CallerPassportActive:  callerPassport.active,
		CallerReputationScore: callerPassport.reputationScore,
		CallerPassportBadges:  callerPassport.badges,
		Nonce:                 nonce,
		StepQuotes:            stepQuotes,
		TotalMaxCost:          totalCoin,
		TotalSloP95Ms:         latency.computedP95MS,
		AnchoredHeight:        sdkCtx.BlockHeight(),
		ExpiresAt:             expiresAt,
		RouterPubkey:          routerPubkey,
	}
	quote.BundleID, err = types.ComputeBundleQuoteID(quote)
	if err != nil {
		return nil, err
	}
	if _, found, err := k.GetBundleQuote(ctx, quote.BundleID); err != nil {
		return nil, err
	} else if found {
		return nil, types.ErrInvalidWorkflow.Wrapf("bundle quote replay: %s", quote.BundleID)
	}
	if err := types.SignBundleQuote(quote, req.RouterPrivateKey); err != nil {
		return nil, err
	}
	record := &types.BundleQuoteRecord{
		BundleID:      quote.BundleID,
		Quote:         quote,
		ExpiresAt:     quote.ExpiresAt,
		UpdatedHeight: sdkCtx.BlockHeight(),
	}
	if err := k.PutBundleQuote(ctx, record); err != nil {
		return nil, err
	}
	k.logAndEmitBundleQuote(ctx, quote, latency)
	return quote, nil
}

// ValidateBundleQuote verifies signature, expiry, and keeper replay state.
func (k *Keeper) ValidateBundleQuote(ctx context.Context, quote *types.BundleQuote, expectedRouterPubkey string, now time.Time) error {
	if quote == nil {
		return types.ErrInvalidWorkflow.Wrap("bundle quote cannot be nil")
	}
	if err := quote.ValidateBasic(); err != nil {
		return err
	}
	if err := types.VerifyBundleQuoteSignature(quote, expectedRouterPubkey); err != nil {
		return err
	}
	record, found, err := k.GetBundleQuote(ctx, quote.BundleID)
	if err != nil {
		return err
	}
	if !found {
		return types.ErrInvalidWorkflow.Wrapf("bundle quote not found: %s", quote.BundleID)
	}
	if record.Consumed {
		return types.ErrInvalidWorkflow.Wrapf("bundle quote replay: %s", quote.BundleID)
	}
	if err := storedQuoteMatches(record.Quote, quote); err != nil {
		return err
	}
	if now.IsZero() {
		now = sdk.UnwrapSDKContext(ctx).BlockTime()
	}
	expiresAt, err := quote.ExpiresAtTime()
	if err != nil {
		return err
	}
	if !now.UTC().Before(expiresAt) {
		return types.ErrInvalidWorkflow.Wrapf("bundle quote expired: %s", quote.BundleID)
	}
	return nil
}

// ConsumeBundleQuote marks a valid workflow quote as consumed.
func (k *Keeper) ConsumeBundleQuote(ctx context.Context, quote *types.BundleQuote, expectedRouterPubkey string, now time.Time) error {
	if err := k.ValidateBundleQuote(ctx, quote, expectedRouterPubkey, now); err != nil {
		return err
	}
	record, found, err := k.GetBundleQuote(ctx, quote.BundleID)
	if err != nil {
		return err
	}
	if !found {
		return types.ErrInvalidWorkflow.Wrapf("bundle quote not found: %s", quote.BundleID)
	}
	record.Consumed = true
	record.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
	if err := k.PutBundleQuote(ctx, record); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeBundleQuoteConsumed,
			sdk.NewAttribute(types.AttributeKeyBundleID, quote.BundleID),
			sdk.NewAttribute(types.AttributeKeyWorkflowID, quote.WorkflowID),
			sdk.NewAttribute(types.AttributeKeyVersion, quote.Version),
		),
	)
	return nil
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, collections.ErrNotFound)
}

func (k *Keeper) latestActiveWorkflow(ctx context.Context, workflowID string) (*types.WorkflowRecord, error) {
	versions, err := k.ListWorkflowVersions(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i] != nil && versions[i].Status == types.WorkflowStatusActive {
			return versions[i], nil
		}
	}
	return nil, types.ErrInvalidWorkflow.Wrapf("no active workflow version found: %s", workflowID)
}

type callerPassportEvidence struct {
	tier            string
	active          bool
	reputationScore uint32
	badges          []string
}

func normalizeCallerPassportEvidence(req *types.QuoteWorkflowRequest, requirements *types.PassportRequirements) (callerPassportEvidence, error) {
	if req.CallerReputationScore > types.MaxPassportReputationScore {
		return callerPassportEvidence{}, types.ErrInvalidWorkflow.Wrapf("caller reputation score exceeds %d", types.MaxPassportReputationScore)
	}
	if requirements.GetMinReputationScore() > types.MaxPassportReputationScore {
		return callerPassportEvidence{}, types.ErrInvalidWorkflow.Wrapf("workflow min_reputation_score exceeds %d", types.MaxPassportReputationScore)
	}

	callerTier, err := normalizeCallerPassportTier(req.CallerPassportTier, requirements.GetMinTier())
	if err != nil {
		return callerPassportEvidence{}, err
	}
	if requirements.GetRequireActivePassport() && !req.CallerPassportActive {
		return callerPassportEvidence{}, types.ErrInvalidWorkflow.Wrap("caller passport must be active")
	}
	if minReputation := requirements.GetMinReputationScore(); minReputation > 0 && req.CallerReputationScore < minReputation {
		return callerPassportEvidence{}, types.ErrInvalidWorkflow.Wrapf("caller reputation score %d below required %d", req.CallerReputationScore, minReputation)
	}

	callerBadges := types.NormalizePassportBadges(req.CallerPassportBadges)
	requiredBadges, err := normalizeRequiredPassportBadges(requirements.GetRequiredBadges())
	if err != nil {
		return callerPassportEvidence{}, err
	}
	if len(requiredBadges) > 0 {
		callerBadgeSet := make(map[string]struct{}, len(callerBadges))
		for _, badge := range callerBadges {
			callerBadgeSet[badge] = struct{}{}
		}
		for _, badge := range requiredBadges {
			if _, ok := callerBadgeSet[badge]; !ok {
				return callerPassportEvidence{}, types.ErrInvalidWorkflow.Wrapf("caller passport missing required badge %s", badge)
			}
		}
	}

	return callerPassportEvidence{
		tier:            callerTier,
		active:          req.CallerPassportActive,
		reputationScore: req.CallerReputationScore,
		badges:          callerBadges,
	}, nil
}

func normalizeCallerPassportTier(raw string, min types.PassportTier) (string, error) {
	caller := normalizePassportTierString(raw)
	callerRank := passportTierRank(caller)
	minTier := passportTierName(min)
	minRank := passportTierRank(minTier)
	if minRank > 0 && callerRank < minRank {
		return "", types.ErrInvalidWorkflow.Wrapf("caller passport tier %s below required %s", caller, minTier)
	}
	return caller, nil
}

func normalizeRequiredPassportBadges(raw []string) ([]string, error) {
	for _, badge := range raw {
		if strings.TrimSpace(badge) == "" {
			return nil, types.ErrInvalidWorkflow.Wrap("workflow required passport badge cannot be empty")
		}
	}
	return types.NormalizePassportBadges(raw), nil
}

type bundleLatencyPlan struct {
	topoLevels        [][]string
	criticalPathSteps []string
	computedP95MS     uint32
}

type normalizedBundleStep struct {
	id          string
	toolID      string
	toolVersion string
	coin        *sdk.Coin
	subSLO      uint32
	dependsOn   []string
}

func buildBundleStepQuotes(card *types.WorkflowCard, denom string) ([]types.BundleStepQuote, math.Int, bundleLatencyPlan, error) {
	steps := append([]*types.Step(nil), card.GetDag()...)
	if len(steps) == 0 {
		return nil, math.ZeroInt(), bundleLatencyPlan{}, types.ErrInvalidWorkflow.Wrap("workflow DAG cannot be empty")
	}
	sort.SliceStable(steps, func(i, j int) bool {
		left, right := "", ""
		if steps[i] != nil {
			left = strings.TrimSpace(steps[i].GetStepId())
		}
		if steps[j] != nil {
			right = strings.TrimSpace(steps[j].GetStepId())
		}
		return left < right
	})
	stepsByID := make(map[string]normalizedBundleStep, len(steps))
	totalCost := math.ZeroInt()
	for _, step := range steps {
		if step == nil {
			return nil, math.Int{}, bundleLatencyPlan{}, types.ErrInvalidWorkflow.Wrap("workflow step cannot be nil")
		}
		stepID := strings.TrimSpace(step.GetStepId())
		if stepID == "" {
			return nil, math.Int{}, bundleLatencyPlan{}, types.ErrInvalidWorkflow.Wrap("workflow step_id cannot be empty")
		}
		if _, ok := stepsByID[stepID]; ok {
			return nil, math.Int{}, bundleLatencyPlan{}, types.ErrInvalidWorkflow.Wrapf("duplicate workflow step_id: %s", stepID)
		}
		toolID := strings.TrimSpace(step.GetToolId())
		if toolID == "" {
			return nil, math.Int{}, bundleLatencyPlan{}, types.ErrInvalidWorkflow.Wrapf("workflow step %s missing tool_id", stepID)
		}
		toolVersion, err := resolvePinnedToolVersion(step.GetToolVersionConstraint())
		if err != nil {
			return nil, math.Int{}, bundleLatencyPlan{}, types.ErrInvalidWorkflow.Wrapf("workflow step %s: %v", stepID, err)
		}
		coin, err := normalizeWorkflowCoin(workflowCoinPtr(step.GetMaxSubCost()), denom)
		if err != nil {
			return nil, math.Int{}, bundleLatencyPlan{}, err
		}
		if coin == nil {
			coin = zeroWorkflowCoin(denom)
		}
		amount, err := workflowCoinAmount(coin)
		if err != nil {
			return nil, math.Int{}, bundleLatencyPlan{}, err
		}
		totalCost = totalCost.Add(amount)
		stepsByID[stepID] = normalizedBundleStep{
			id:          stepID,
			toolID:      toolID,
			toolVersion: toolVersion,
			coin:        coin,
			subSLO:      step.GetSubSloP95Ms(),
			dependsOn:   normalizedStepDependencies(step.GetDependsOn()),
		}
	}
	if err := applyFallbackStepDependencies(stepsByID, card.GetDag()); err != nil {
		return nil, math.Int{}, bundleLatencyPlan{}, err
	}
	if err := applyConditionStepDependencies(stepsByID, card.GetDag()); err != nil {
		return nil, math.Int{}, bundleLatencyPlan{}, err
	}
	order, latency, err := computeBundleLatencyPlan(stepsByID)
	if err != nil {
		return nil, math.Int{}, bundleLatencyPlan{}, err
	}
	stepQuotes := make([]types.BundleStepQuote, 0, len(order))
	for _, stepID := range order {
		step := stepsByID[stepID]
		quoteCoin, err := types.NewQuoteCoin(step.coin.Denom, step.coin.Amount.String())
		if err != nil {
			return nil, math.Int{}, bundleLatencyPlan{}, err
		}
		stepQuotes = append(stepQuotes, types.BundleStepQuote{
			StepID:      step.id,
			ToolID:      step.toolID,
			ToolVersion: step.toolVersion,
			SubMaxCost:  quoteCoin,
			SubSloP95Ms: step.subSLO,
		})
	}
	return stepQuotes, totalCost, latency, nil
}

func normalizedStepDependencies(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, dep := range raw {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if _, ok := seen[dep]; ok {
			continue
		}
		seen[dep] = struct{}{}
		out = append(out, dep)
	}
	sort.Strings(out)
	return out
}

func applyFallbackStepDependencies(stepsByID map[string]normalizedBundleStep, steps []*types.Step) error {
	for _, step := range steps {
		if step == nil || step.GetFailureAction() != types.FailureAction_FAILURE_ACTION_FALLBACK_STEP {
			continue
		}
		stepID := strings.TrimSpace(step.GetStepId())
		fallbackID := strings.TrimSpace(step.GetFallbackStepId())
		if stepID == "" || fallbackID == "" {
			return types.ErrInvalidWorkflow.Wrapf("workflow step %s fallback_step requires fallback_step_id", stepID)
		}
		if stepID == fallbackID {
			return types.ErrInvalidWorkflow.Wrapf("workflow step %s cannot fall back to itself", stepID)
		}
		fallback, ok := stepsByID[fallbackID]
		if !ok {
			return types.ErrInvalidWorkflow.Wrapf("workflow step %s falls back to unknown step %s", stepID, fallbackID)
		}
		fallback.dependsOn = appendUniqueStepDependency(fallback.dependsOn, stepID)
		stepsByID[fallbackID] = fallback
	}
	return nil
}

func applyConditionStepDependencies(stepsByID map[string]normalizedBundleStep, steps []*types.Step) error {
	for _, step := range steps {
		if step == nil || strings.TrimSpace(step.GetCondition()) == "" {
			continue
		}
		stepID := strings.TrimSpace(step.GetStepId())
		if stepID == "" {
			return types.ErrInvalidWorkflow.Wrap("workflow condition step_id cannot be empty")
		}
		normalized, ok := stepsByID[stepID]
		if !ok {
			return types.ErrInvalidWorkflow.Wrapf("workflow condition references unknown step owner %s", stepID)
		}
		deps, err := types.WorkflowConditionStepReferences(step.GetCondition())
		if err != nil {
			return types.ErrInvalidWorkflow.Wrapf("workflow step %s condition is invalid: %v", stepID, err)
		}
		for _, dep := range deps {
			if dep == stepID {
				return types.ErrInvalidWorkflow.Wrapf("workflow step %s condition cannot depend on itself", stepID)
			}
			if _, ok := stepsByID[dep]; !ok {
				return types.ErrInvalidWorkflow.Wrapf("workflow step %s condition references unknown step %s", stepID, dep)
			}
			normalized.dependsOn = appendUniqueStepDependency(normalized.dependsOn, dep)
		}
		stepsByID[stepID] = normalized
	}
	return nil
}

func appendUniqueStepDependency(deps []string, dep string) []string {
	dep = strings.TrimSpace(dep)
	if dep == "" {
		return deps
	}
	for _, got := range deps {
		if got == dep {
			return deps
		}
	}
	out := append(append([]string(nil), deps...), dep)
	sort.Strings(out)
	return out
}

func computeBundleLatencyPlan(steps map[string]normalizedBundleStep) ([]string, bundleLatencyPlan, error) {
	ids := make([]string, 0, len(steps))
	indegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))
	for id, step := range steps {
		ids = append(ids, id)
		indegree[id] = 0
		for _, dep := range step.dependsOn {
			if _, ok := steps[dep]; !ok {
				return nil, bundleLatencyPlan{}, types.ErrInvalidWorkflow.Wrapf("workflow step %s depends on unknown step %s", id, dep)
			}
			indegree[id]++
			dependents[dep] = append(dependents[dep], id)
		}
	}
	sort.Strings(ids)
	for dep := range dependents {
		sort.Strings(dependents[dep])
	}

	ready := make([]string, 0, len(ids))
	longest := make(map[string]uint32, len(steps))
	paths := make(map[string][]string, len(steps))
	for _, id := range ids {
		if indegree[id] == 0 {
			ready = append(ready, id)
			longest[id] = steps[id].subSLO
			paths[id] = []string{id}
		}
	}

	order := make([]string, 0, len(steps))
	levels := make([][]string, 0, len(steps))
	for len(ready) > 0 {
		sort.Strings(ready)
		level := append([]string(nil), ready...)
		levels = append(levels, level)
		next := make([]string, 0)
		for _, id := range level {
			order = append(order, id)
			for _, childID := range dependents[id] {
				candidate, err := addStepSLO(longest[id], steps[childID].subSLO)
				if err != nil {
					return nil, bundleLatencyPlan{}, err
				}
				candidatePath := append(append([]string(nil), paths[id]...), childID)
				if candidate > longest[childID] || len(paths[childID]) == 0 || (candidate == longest[childID] && lexicographicPathLess(candidatePath, paths[childID])) {
					longest[childID] = candidate
					paths[childID] = candidatePath
				}
				indegree[childID]--
				if indegree[childID] == 0 {
					next = append(next, childID)
				}
			}
		}
		ready = next
	}
	if len(order) != len(steps) {
		return nil, bundleLatencyPlan{}, types.ErrInvalidWorkflow.Wrap("workflow DAG contains cycle")
	}

	var criticalP95 uint32
	var criticalPath []string
	for _, id := range ids {
		if longest[id] > criticalP95 || (longest[id] == criticalP95 && lexicographicPathLess(paths[id], criticalPath)) {
			criticalP95 = longest[id]
			criticalPath = append([]string(nil), paths[id]...)
		}
	}
	return order, bundleLatencyPlan{
		topoLevels:        levels,
		criticalPathSteps: criticalPath,
		computedP95MS:     criticalP95,
	}, nil
}

func addStepSLO(left, right uint32) (uint32, error) {
	if left > ^uint32(0)-right {
		return 0, types.ErrInvalidWorkflow.Wrap("workflow total_slo_p95_ms overflows uint32")
	}
	return left + right, nil
}

func lexicographicPathLess(left, right []string) bool {
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return len(left) < len(right)
}

func resolvePinnedToolVersion(constraint string) (string, error) {
	if strings.TrimSpace(constraint) != constraint {
		return "", fmt.Errorf("tool_version_constraint must pin a canonical exact semver, got %q", workflowQuoteDiagnostic(constraint))
	}
	if constraint == "" {
		return "", fmt.Errorf("tool_version_constraint must pin an exact semver")
	}
	pinned := strings.TrimPrefix(constraint, "=")
	if pinned == "" {
		return "", fmt.Errorf("tool_version_constraint must pin an exact semver")
	}
	if strings.ContainsAny(pinned, "<>^~*=, ") {
		return "", fmt.Errorf("tool_version_constraint must pin an exact semver, got %q", workflowQuoteDiagnostic(constraint))
	}
	version, err := semver.NewVersion(pinned)
	if err != nil {
		return "", fmt.Errorf("tool_version_constraint must pin an exact semver: %s", workflowQuoteDiagnostic(err.Error()))
	}
	canonical := version.String()
	if canonical != pinned {
		return "", fmt.Errorf("tool_version_constraint must pin a canonical exact semver, got %q", workflowQuoteDiagnostic(constraint))
	}
	return canonical, nil
}

func workflowQuoteDiagnostic(value string) string {
	return logging.RedactPII(value)
}

func quoteTotalCost(base math.Int, pricing *types.WorkflowPricing, params *types.Params) (math.Int, error) {
	total := base
	if pricing != nil {
		total = total.Add(ceilBPS(base, pricing.GetAuthorMarginBps()))
		total = total.Add(ceilBPS(base, pricing.GetInsuranceBps()))
	}
	if params != nil {
		total = total.Add(ceilBPS(base, params.WastedWorkBPS))
	}
	return applyWorkflowPricingBounds(total, pricing, params)
}

func applyWorkflowPricingBounds(total math.Int, pricing *types.WorkflowPricing, params *types.Params) (math.Int, error) {
	if pricing == nil {
		return total, nil
	}
	denom := ""
	if params != nil {
		denom = params.BondDenom
	}
	minimum, hasMinimum, err := workflowPricingBoundAmount(pricing.GetMinimumCost(), denom, "minimum_cost")
	if err != nil {
		return math.Int{}, err
	}
	maximum, hasMaximum, err := workflowPricingBoundAmount(pricing.GetMaximumCost(), denom, "maximum_cost")
	if err != nil {
		return math.Int{}, err
	}
	if hasMinimum && hasMaximum && minimum.GT(maximum) {
		return math.Int{}, types.ErrInvalidWorkflow.Wrap("workflow pricing minimum_cost exceeds maximum_cost")
	}
	if hasMinimum && total.LT(minimum) {
		total = minimum
	}
	if hasMaximum && total.GT(maximum) {
		return math.Int{}, types.ErrInvalidWorkflow.Wrap("workflow pricing total_max_cost exceeds maximum_cost")
	}
	return total, nil
}

func workflowPricingBoundAmount(coin sdk.Coin, defaultDenom string, field string) (math.Int, bool, error) {
	if workflowCoinIsUnset(coin) {
		return math.ZeroInt(), false, nil
	}
	normalized, err := normalizeWorkflowCoin(&coin, defaultDenom)
	if err != nil {
		return math.Int{}, false, err
	}
	if defaultDenom != "" && strings.TrimSpace(normalized.GetDenom()) != strings.TrimSpace(defaultDenom) {
		return math.Int{}, false, types.ErrInvalidWorkflow.Wrapf("workflow pricing %s denom %s does not match quote denom %s", field, normalized.GetDenom(), defaultDenom)
	}
	amount, err := workflowCoinAmount(normalized)
	if err != nil {
		return math.Int{}, false, err
	}
	return amount, true, nil
}

func ceilBPS(amount math.Int, bps uint32) math.Int {
	if amount.IsZero() || bps == 0 {
		return math.ZeroInt()
	}
	numerator := amount.MulRaw(int64(bps))
	quotient := numerator.QuoRaw(int64(types.BPSDenominator))
	if !numerator.ModRaw(int64(types.BPSDenominator)).IsZero() {
		quotient = quotient.AddRaw(1)
	}
	return quotient
}

func storedQuoteMatches(stored *types.BundleQuote, got *types.BundleQuote) error {
	if stored == nil {
		return types.ErrInvalidWorkflow.Wrap("stored bundle quote cannot be nil")
	}
	storedBytes, err := stored.CanonicalBytes()
	if err != nil {
		return err
	}
	gotBytes, err := got.CanonicalBytes()
	if err != nil {
		return err
	}
	if !bytes.Equal(storedBytes, gotBytes) || strings.TrimSpace(stored.Signed) != strings.TrimSpace(got.Signed) {
		return types.ErrInvalidWorkflow.Wrapf("bundle quote record mismatch: %s", got.BundleID)
	}
	return nil
}

func (k *Keeper) logAndEmitBundleQuote(ctx context.Context, quote *types.BundleQuote, latency bundleLatencyPlan) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	totalMaxCost := quote.TotalMaxCost.Amount + quote.TotalMaxCost.Denom
	topoLevelsJSON := compactJSON(latency.topoLevels)
	criticalPathJSON := compactJSON(latency.criticalPathSteps)
	k.Logger().Info(
		"workflow bundle quote",
		"ts", sdkCtx.BlockTime().UTC().Format(time.RFC3339Nano),
		"workflow_id", quote.WorkflowID,
		"version", quote.Version,
		"caller_passport_tier", quote.CallerPassportTier,
		"total_max_cost", totalMaxCost,
		"total_slo_p95_ms", quote.TotalSloP95Ms,
		"topo_levels", topoLevelsJSON,
		"critical_path_steps", criticalPathJSON,
		"computed_p95_ms", latency.computedP95MS,
		"anchored_height", quote.AnchoredHeight,
		"expires_at", quote.ExpiresAt,
	)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeBundleQuoted,
			sdk.NewAttribute(types.AttributeKeyBundleID, quote.BundleID),
			sdk.NewAttribute(types.AttributeKeyWorkflowID, quote.WorkflowID),
			sdk.NewAttribute(types.AttributeKeyVersion, quote.Version),
			sdk.NewAttribute(types.AttributeKeyTotalMaxCost, totalMaxCost),
			sdk.NewAttribute(types.AttributeKeyTotalSLOP95MS, fmt.Sprintf("%d", quote.TotalSloP95Ms)),
			sdk.NewAttribute(types.AttributeKeyTopoLevels, topoLevelsJSON),
			sdk.NewAttribute(types.AttributeKeyCriticalPathSteps, criticalPathJSON),
			sdk.NewAttribute(types.AttributeKeyComputedP95MS, fmt.Sprintf("%d", latency.computedP95MS)),
			sdk.NewAttribute(types.AttributeKeyAnchoredHeight, fmt.Sprintf("%d", quote.AnchoredHeight)),
			sdk.NewAttribute(types.AttributeKeyExpiresAt, quote.ExpiresAt),
		),
	)
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func normalizePassportTierString(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.TrimPrefix(normalized, "passport_tier_")
	if normalized == "" {
		return "unspecified"
	}
	switch normalized {
	case "basic", "standard", "trusted", "institutional":
		return normalized
	default:
		return normalized
	}
}

func passportTierName(tier types.PassportTier) string {
	switch tier {
	case types.PassportTier_PASSPORT_TIER_BASIC:
		return "basic"
	case types.PassportTier_PASSPORT_TIER_STANDARD:
		return "standard"
	case types.PassportTier_PASSPORT_TIER_TRUSTED:
		return "trusted"
	case types.PassportTier_PASSPORT_TIER_INSTITUTIONAL:
		return "institutional"
	default:
		return "unspecified"
	}
}

func passportTierRank(tier string) int {
	switch normalizePassportTierString(tier) {
	case "basic":
		return 1
	case "standard":
		return 2
	case "trusted":
		return 3
	case "institutional":
		return 4
	default:
		return 0
	}
}
