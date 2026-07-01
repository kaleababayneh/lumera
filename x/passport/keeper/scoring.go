package keeper

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/passport/types"
)

// ComputeReputation calculates a deterministic 7-dimensional reputation vector.
// All time-dependent calculations use blockTime (never time.Now) to ensure
// determinism across validators.
//
// Gas usage is bounded by O(len(receipts) + len(violations)).
func ComputeReputation(
	cfg types.ScoringConfig,
	receipts []types.ReceiptInput,
	violations []types.ViolationInput,
	summary *types.SummaryInput,
	blockTime time.Time,
) *types.ReputationResult {
	totalReceipts := totalReceiptCount(receipts, summary)
	eligible := cfg.MinReceiptCount == 0 || totalReceipts >= cfg.MinReceiptCount

	result := &types.ReputationResult{
		Eligible:  eligible,
		UpdatedAt: blockTime,
	}

	if !eligible {
		result.Reliability = cfg.NeutralReliability
		result.Safety = cfg.DefaultSafety
		result.Latency = cfg.DefaultLatency
		result.CostDiscipline = cfg.DefaultCostDiscipline
		result.Dispute = cfg.DefaultDispute
		result.Longevity = cfg.DefaultLongevity
		result.Privacy = cfg.DefaultPrivacy
		return result
	}

	agCtx := computeAntiGaming(cfg, receipts)
	ecoWeights, activityWeights := receiptWeights(cfg, receipts, agCtx)

	result.Reliability = calcReliability(cfg, receipts, ecoWeights, blockTime)
	result.Safety = calcSafety(cfg, receipts, violations, activityWeights)
	result.Latency = calcLatency(cfg, receipts, ecoWeights)
	result.CostDiscipline = calcCostDiscipline(cfg, receipts, ecoWeights)
	result.Dispute = calcDispute(cfg, summary)
	result.Longevity = calcLongevity(cfg, receipts, summary)
	result.Privacy = calcPrivacy(cfg, receipts, ecoWeights)

	return result
}

// UpdatePassportReputation recomputes and stores the reputation for a passport.
func (k Keeper) UpdatePassportReputation(
	ctx context.Context,
	agentPubkey string,
	receipts []types.ReceiptInput,
	violations []types.ViolationInput,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	passport, found := k.GetPassportByAgent(ctx, agentPubkey)
	if !found {
		return types.ErrPassportNotFound
	}

	cfg := types.ScoringConfigFromParams(k.GetParams(ctx))
	summary := types.SummaryInputFromProto(passport.Summary)
	blockTime := sdkCtx.BlockTime()
	result := ComputeReputation(cfg, receipts, violations, summary, blockTime)
	defs := types.DefaultTierDefinitions()
	previousTierState := types.TierStateFromProto(passport.TierState, blockTime)
	tierResult := types.EvaluateTier(types.TierEvaluationInput{
		Reputation:     result,
		ReceiptCount:   totalReceiptCount(receipts, summary),
		DisputeRate30d: types.Clamp01(1.0 - result.Dispute),
		Now:            blockTime,
		State:          previousTierState,
		Definitions:    defs,
	})

	passport.Reputation = result.ToProto(blockTime)
	passport.ScoreBreakdown = result.ToScoreBreakdown(blockTime)
	passport.TierState = types.TierEvaluationResultToProto(tierResult, defs)
	if tierResult.Promoted || tierResult.Demoted || previousTierState.CurrentTier != tierResult.CurrentTier {
		passport.TierHistory = append(passport.TierHistory, types.TierHistoryEntryFromEvaluation(
			previousTierState.CurrentTier,
			tierResult,
			passport.ScoreBreakdown,
			blockTime,
		))
	}

	if err := k.SavePassport(ctx, passport); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"passport_reputation_updated",
			sdk.NewAttribute("passport_id", passport.PassportId),
			sdk.NewAttribute("agent_pubkey", passport.AgentPubkey),
			sdk.NewAttribute("eligible", boolStr(result.Eligible)),
			sdk.NewAttribute("current_tier", passport.TierState.CurrentTier.String()),
			sdk.NewAttribute("eligible_tier", passport.TierState.EligibleTier.String()),
		),
	)

	return nil
}

// UpdatePassportWorkflowAuthorConformance applies authored workflow outcomes to Passport tiering.
func (k Keeper) UpdatePassportWorkflowAuthorConformance(
	ctx context.Context,
	agentPubkey string,
	outcomes []types.WorkflowOutcomeInput,
	policy types.WorkflowAuthorPolicy,
	baseMinBondLAC uint64,
) (*types.WorkflowAuthorTierEvaluationResult, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	passport, found := k.GetPassportByAgent(ctx, agentPubkey)
	if !found {
		return nil, types.ErrPassportNotFound
	}

	blockTime := sdkCtx.BlockTime()
	baseRep := types.ReputationResultFromScoreBreakdown(passport.ScoreBreakdown)
	if baseRep == nil {
		baseRep = types.ReputationResultFromScoreBreakdown(types.ScoreBreakdownFromProto(passport.Reputation, passport.Summary))
	}
	if baseRep == nil {
		baseRep = &types.ReputationResult{UpdatedAt: blockTime}
	}
	baseRep.UpdatedAt = blockTime

	effectivePolicy := normalizeWorkflowAuthorPolicyForKeeper(policy)
	previousTierState := types.TierStateFromProto(passport.TierState, blockTime)
	receiptCount := uint64(0)
	if passport.Summary != nil {
		receiptCount = passport.Summary.TotalReceipts
	}
	result := types.EvaluateWorkflowAuthorTier(types.WorkflowAuthorTierEvaluationInput{
		TierInput: types.TierEvaluationInput{
			Reputation:     baseRep,
			ReceiptCount:   receiptCount,
			DisputeRate30d: types.Clamp01(1.0 - baseRep.Dispute),
			Now:            blockTime,
			State:          previousTierState,
			Definitions:    types.DefaultTierDefinitions(),
		},
		Outcomes:       outcomes,
		Policy:         effectivePolicy,
		BaseMinBondLAC: baseMinBondLAC,
	})

	baseRep.WorkflowConformance = result.WorkflowConformance
	baseRep.WorkflowConformanceWeightBPS = effectivePolicy.ConformanceWeightBPS
	passport.Reputation = baseRep.ToProto(blockTime)
	passport.ScoreBreakdown = baseRep.ToScoreBreakdown(blockTime)
	defs := types.DefaultTierDefinitions()
	passport.TierState = types.TierEvaluationResultToProto(result.Tier, defs)
	if result.Tier.Promoted || result.Tier.Demoted || previousTierState.CurrentTier != result.Tier.CurrentTier {
		passport.TierHistory = append(passport.TierHistory, types.TierHistoryEntryFromEvaluation(
			previousTierState.CurrentTier,
			result.Tier,
			passport.ScoreBreakdown,
			blockTime,
		))
	}

	if err := k.SavePassport(ctx, passport); err != nil {
		return nil, err
	}

	conformanceDelta := result.WorkflowConformance - effectivePolicy.DefaultConformance
	for i := range outcomes {
		outcome := &outcomes[i]
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"passport_workflow_author_outcome",
				sdk.NewAttribute("ts", workflowAuthorEventTimestamp(blockTime, outcome.Timestamp)),
				sdk.NewAttribute("author", passport.AgentPubkey),
				sdk.NewAttribute("workflow_id", strings.TrimSpace(outcome.WorkflowID)),
				sdk.NewAttribute("outcome", strings.TrimSpace(outcome.Outcome)),
				sdk.NewAttribute("conformance_delta", fmt.Sprintf("%.6f", conformanceDelta)),
				sdk.NewAttribute("new_composite", fmt.Sprintf("%.6f", result.Composite)),
				sdk.NewAttribute("new_tier", passport.TierState.CurrentTier.String()),
			),
		)
	}

	return &result, nil
}

// AntiGamingStats returns the anti-gaming metrics computed from a receipt stream.
// Useful for populating PassportSummary fields.
func AntiGamingStats(cfg types.ScoringConfig, receipts []types.ReceiptInput) (
	toolDiversity float64,
	verifiedShare float64,
	collusionRisk float64,
	collusionFlags []string,
) {
	agCtx := computeAntiGaming(cfg, receipts)
	return agCtx.toolDiversity, agCtx.verifiedShare, agCtx.collusionRisk, agCtx.collusionFlags
}

// ---- Anti-gaming context ----

type antiGamingCtx struct {
	uniquePayerFactor float64
	diversityFactor   float64
	toolDiversity     float64
	verifiedShare     float64
	collusionRisk     float64
	collusionFlags    []string
	frequencyPenalty  map[int]float64
}

func computeAntiGaming(cfg types.ScoringConfig, receipts []types.ReceiptInput) antiGamingCtx {
	payers := make(map[string]struct{})
	payerTotals := make(map[string]float64)
	toolTotals := make(map[string]float64)
	publisherTotals := make(map[string]float64)
	totalPaid := 0.0
	verifiedPaid := 0.0

	for i := range receipts {
		r := &receipts[i]
		if r.PayerID != "" {
			payers[r.PayerID] = struct{}{}
		}
		paid := effectiveNetPaid(cfg, r)
		totalPaid += paid
		if r.PayerID != "" {
			payerTotals[r.PayerID] += paid
		}
		if r.ToolID != "" {
			toolTotals[r.ToolID] += paid
		}
		if r.PublisherID != "" {
			publisherTotals[r.PublisherID] += paid
		}
		if r.Verified {
			verifiedPaid += paid
		}
	}

	uniqueFactor := uniquePayerFactor(cfg, payers)
	_, maxToolShare := toolDiversityIndex(toolTotals, totalPaid)
	diversityFac := diversityFactor(cfg, maxToolShare)
	colRisk, colFlags := collusionRiskScore(cfg, totalPaid, toolTotals, payerTotals, publisherTotals)
	freqPenalty := frequencyPenalty(cfg, receipts)

	vs := 0.0
	if totalPaid > 0 {
		vs = verifiedPaid / totalPaid
	}

	divIdx, _ := toolDiversityIndex(toolTotals, totalPaid)

	return antiGamingCtx{
		uniquePayerFactor: uniqueFactor,
		diversityFactor:   diversityFac,
		toolDiversity:     divIdx,
		verifiedShare:     vs,
		collusionRisk:     colRisk,
		collusionFlags:    colFlags,
		frequencyPenalty:  freqPenalty,
	}
}

func receiptWeights(cfg types.ScoringConfig, receipts []types.ReceiptInput, agCtx antiGamingCtx) ([]float64, []float64) {
	ecoWeights := make([]float64, len(receipts))
	activityWeights := make([]float64, len(receipts))
	for i := range receipts {
		ew, aw := receiptWeightPair(cfg, &receipts[i], i, agCtx)
		ecoWeights[i] = ew
		activityWeights[i] = aw
	}
	return ecoWeights, activityWeights
}

func receiptWeightPair(cfg types.ScoringConfig, r *types.ReceiptInput, idx int, agCtx antiGamingCtx) (float64, float64) {
	netPaid := effectiveNetPaid(cfg, r)
	if netPaid <= 0 {
		return 0, 0
	}

	vf := verificationFactor(cfg, r, agCtx)
	rp := refundPenalty(r.SettlementStatus)
	fp := 1.0
	if p, ok := agCtx.frequencyPenalty[idx]; ok {
		fp = p
	}

	multiplier := agCtx.uniquePayerFactor *
		agCtx.diversityFactor *
		vf *
		rp *
		fp

	ecoWeight := math.Sqrt(netPaid) * multiplier
	return ecoWeight, multiplier
}

// ---- Dimension calculators ----

func calcReliability(cfg types.ScoringConfig, receipts []types.ReceiptInput, weights []float64, now time.Time) float64 {
	if len(receipts) == 0 {
		return cfg.NeutralReliability
	}

	var weightedSuccesses, totalWeight float64
	for i := range receipts {
		r := &receipts[i]
		ageDays := now.Sub(r.Timestamp).Hours() / 24.0
		if ageDays < 0 {
			ageDays = 0
		}
		decay := decayWeight(cfg, ageDays)
		w := decay
		if i < len(weights) {
			w *= weights[i]
		}
		totalWeight += w
		if r.Success {
			weightedSuccesses += w
		}
	}

	if totalWeight == 0 {
		return cfg.NeutralReliability
	}
	return weightedSuccesses / totalWeight
}

func calcSafety(cfg types.ScoringConfig, _ []types.ReceiptInput, violations []types.ViolationInput, activityWeights []float64) float64 {
	totalActivity := 0.0
	for _, w := range activityWeights {
		totalActivity += w
	}

	if totalActivity == 0 {
		if len(violations) > 0 {
			return 0
		}
		return cfg.DefaultSafety
	}

	var weightedViolations float64
	for i := range violations {
		weightedViolations += violations[i].Severity
	}

	safety := 1.0 - (weightedViolations / totalActivity)
	return math.Max(0, safety)
}

func calcLatency(cfg types.ScoringConfig, receipts []types.ReceiptInput, weights []float64) float64 {
	if len(receipts) == 0 {
		return cfg.DefaultLatency
	}

	var within, total float64
	for i := range receipts {
		r := &receipts[i]
		if r.LatencySLOMs <= 0 {
			continue
		}
		w := 1.0
		if i < len(weights) {
			w = weights[i]
		}
		total += w
		if r.LatencyMs <= r.LatencySLOMs {
			within += w
		}
	}

	if total == 0 {
		return cfg.DefaultLatency
	}
	return within / total
}

func calcCostDiscipline(cfg types.ScoringConfig, receipts []types.ReceiptInput, weights []float64) float64 {
	if len(receipts) == 0 {
		return cfg.DefaultCostDiscipline
	}

	var deviationSum, totalWeight float64
	for i := range receipts {
		r := &receipts[i]
		if r.QuotedCost <= 0 {
			continue
		}
		w := 1.0
		if i < len(weights) {
			w = weights[i]
		}
		diff := math.Abs(r.ActualCost - r.QuotedCost)
		deviationSum += w * (diff / r.QuotedCost)
		totalWeight += w
	}

	if totalWeight == 0 {
		return cfg.DefaultCostDiscipline
	}
	return types.Clamp01(1.0 - (deviationSum / totalWeight))
}

func calcDispute(cfg types.ScoringConfig, summary *types.SummaryInput) float64 {
	if summary == nil || summary.TotalReceipts == 0 {
		return cfg.DefaultDispute
	}
	if summary.DisputeCount == 0 {
		return cfg.DefaultDispute
	}
	lossRate := float64(summary.DisputeLostCount) / float64(summary.TotalReceipts)
	return types.Clamp01(1.0 - lossRate)
}

func calcLongevity(cfg types.ScoringConfig, receipts []types.ReceiptInput, summary *types.SummaryInput) float64 {
	maxDays := cfg.MaxLongevityDays
	if maxDays <= 0 {
		maxDays = 365.0
	}

	first, last := activityWindow(receipts, summary)
	if first.IsZero() || last.IsZero() || last.Before(first) {
		return cfg.DefaultLongevity
	}

	daysActive := last.Sub(first).Hours() / 24.0
	if daysActive < 0 {
		return cfg.DefaultLongevity
	}

	score := types.Log1p(daysActive) / types.Log1p(maxDays)
	return types.Clamp01(score)
}

func calcPrivacy(cfg types.ScoringConfig, receipts []types.ReceiptInput, weights []float64) float64 {
	if len(receipts) == 0 {
		return cfg.DefaultPrivacy
	}

	var confidential, total float64
	for i := range receipts {
		w := 1.0
		if i < len(weights) {
			w = weights[i]
		}
		total += w
		if receipts[i].Confidential {
			confidential += w
		}
	}

	if total == 0 {
		return cfg.DefaultPrivacy
	}
	return confidential / total
}

// ---- Helper functions ----

func decayWeight(cfg types.ScoringConfig, ageDays float64) float64 {
	start := cfg.DecayStartDays
	if start <= 0 {
		start = 90.0
	}
	if ageDays <= start {
		return 1.0
	}
	d := cfg.DecayDays
	if d <= 0 {
		d = 30.0
	}
	return types.ExpNeg(ageDays / d)
}

func effectiveNetPaid(cfg types.ScoringConfig, r *types.ReceiptInput) float64 {
	netPaid := r.NetPaid
	if netPaid <= 0 {
		netPaid = cfg.DefaultNetPaid
	}
	if netPaid < 0 {
		netPaid = 0
	}
	return netPaid
}

func uniquePayerFactor(cfg types.ScoringConfig, payers map[string]struct{}) float64 {
	count := len(payers)
	if count == 0 {
		return 1.0
	}
	minPayers := cfg.UniquePayerMin
	if minPayers <= 0 {
		minPayers = 20
	}
	factor := float64(count) / minPayers
	if factor > 1 {
		factor = 1
	}
	return types.Clamp01(factor)
}

func toolDiversityIndex(toolTotals map[string]float64, totalPaid float64) (float64, float64) {
	if totalPaid <= 0 || len(toolTotals) == 0 {
		return 1.0, 0
	}
	maxShare := 0.0
	for _, amount := range toolTotals {
		share := amount / totalPaid
		if share > maxShare {
			maxShare = share
		}
	}
	return types.Clamp01(1 - maxShare), maxShare
}

func diversityFactor(cfg types.ScoringConfig, maxToolShare float64) float64 {
	c0 := cfg.ToolConcentrationFloor
	if c0 <= 0 {
		c0 = 0.35
	}
	if c0 >= 1.0 {
		return 1.0
	}
	if maxToolShare <= c0 {
		return 1.0
	}
	penalty := (maxToolShare - c0) / (1 - c0)
	return types.Clamp01(1 - penalty)
}

func verificationFactor(cfg types.ScoringConfig, r *types.ReceiptInput, agCtx antiGamingCtx) float64 {
	if r.SelfPublished {
		return 0.2
	}
	vf := 1.0
	if r.VerificationKnown {
		if r.Verified {
			vf = 1.0
		} else {
			vf = 0.5
		}
	}
	threshold := cfg.CollusionRiskThreshold
	if threshold <= 0 {
		threshold = 0.7
	}
	if agCtx.collusionRisk >= threshold {
		penalty := cfg.CollusionVerificationPenalty
		if penalty <= 0 || penalty > 1 {
			penalty = 0.5
		}
		vf *= penalty
	}
	return types.Clamp01(vf)
}

func refundPenalty(status string) float64 {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "refunded", "chargeback":
		return 0
	case "credited":
		return 0.5
	case "settled", "":
		return 1
	default:
		return 1
	}
}

func frequencyPenalty(cfg types.ScoringConfig, receipts []types.ReceiptInput) map[int]float64 {
	maxPerDay := cfg.FrequencyMaxPerDay
	if maxPerDay <= 0 {
		maxPerDay = 10
	}
	tau := cfg.FrequencyTau
	if tau <= 0 {
		tau = float64(maxPerDay)
	}

	type freqKey struct {
		ToolID  string
		PayerID string
	}
	type entry struct {
		index     int
		timestamp time.Time
	}

	penalties := make(map[int]float64)
	byDay := make(map[string]map[freqKey][]entry)

	for idx := range receipts {
		r := &receipts[idx]
		if r.Timestamp.IsZero() {
			continue
		}
		tool := strings.TrimSpace(r.ToolID)
		payer := strings.TrimSpace(r.PayerID)
		if tool == "" || payer == "" {
			continue
		}
		day := r.Timestamp.UTC().Format("2006-01-02")
		if _, ok := byDay[day]; !ok {
			byDay[day] = make(map[freqKey][]entry)
		}
		key := freqKey{ToolID: tool, PayerID: payer}
		byDay[day][key] = append(byDay[day][key], entry{index: idx, timestamp: r.Timestamp})
	}

	for _, perKey := range byDay {
		for _, entries := range perKey {
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].timestamp.Equal(entries[j].timestamp) {
					return entries[i].index < entries[j].index
				}
				return entries[i].timestamp.Before(entries[j].timestamp)
			})
			for pos, e := range entries {
				k := pos + 1
				if k <= maxPerDay {
					penalties[e.index] = 1.0
					continue
				}
				decay := types.ExpNeg(float64(k-maxPerDay) / tau)
				penalties[e.index] = types.Clamp01(decay)
			}
		}
	}

	return penalties
}

func collusionRiskScore(cfg types.ScoringConfig, totalPaid float64, toolTotals, payerTotals, publisherTotals map[string]float64) (float64, []string) {
	if totalPaid <= 0 {
		return 0, nil
	}

	maxShareOf := func(m map[string]float64) float64 {
		ms := 0.0
		for _, amt := range m {
			if amt <= 0 {
				continue
			}
			share := amt / totalPaid
			if share > ms {
				ms = share
			}
		}
		return ms
	}

	payerShare := maxShareOf(payerTotals)
	toolShare := maxShareOf(toolTotals)
	publisherShare := maxShareOf(publisherTotals)

	normalize := func(share, threshold float64) float64 {
		if threshold <= 0 {
			threshold = 0.6
		}
		if threshold >= 1.0 {
			return 0
		}
		if share <= threshold {
			return 0
		}
		return types.Clamp01((share - threshold) / (1 - threshold))
	}

	payerThreshold := cfg.CollusionMaxPayerShare
	if payerThreshold <= 0 {
		payerThreshold = 0.6
	}
	publisherThreshold := cfg.CollusionMaxPublisherShare
	if publisherThreshold <= 0 {
		publisherThreshold = 0.6
	}
	toolThreshold := cfg.CollusionMaxToolShare
	if toolThreshold <= 0 {
		toolThreshold = 0.7
	}

	var flags []string
	risk := 0.0
	if payerShare > payerThreshold {
		flags = append(flags, "collusion_payer_concentration")
		risk = math.Max(risk, normalize(payerShare, payerThreshold))
	}
	if publisherShare > publisherThreshold {
		flags = append(flags, "collusion_publisher_concentration")
		risk = math.Max(risk, normalize(publisherShare, publisherThreshold))
	}
	if toolShare > toolThreshold {
		flags = append(flags, "collusion_tool_concentration")
		risk = math.Max(risk, normalize(toolShare, toolThreshold))
	}
	if payerShare > 0.9 && toolShare > 0.9 {
		flags = append(flags, "collusion_one_to_one")
		risk = math.Max(risk, 1)
	}

	return types.Clamp01(risk), flags
}

func totalReceiptCount(receipts []types.ReceiptInput, summary *types.SummaryInput) uint64 {
	if summary != nil && summary.TotalReceipts > 0 {
		return summary.TotalReceipts
	}
	return uint64(len(receipts))
}

func activityWindow(receipts []types.ReceiptInput, summary *types.SummaryInput) (time.Time, time.Time) {
	if summary != nil && !summary.FirstReceiptAt.IsZero() && !summary.LastReceiptAt.IsZero() {
		return summary.FirstReceiptAt, summary.LastReceiptAt
	}

	var first, last time.Time
	for i := range receipts {
		ts := receipts[i].Timestamp
		if ts.IsZero() {
			continue
		}
		if first.IsZero() || ts.Before(first) {
			first = ts
		}
		if last.IsZero() || ts.After(last) {
			last = ts
		}
	}
	return first, last
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func normalizeWorkflowAuthorPolicyForKeeper(policy types.WorkflowAuthorPolicy) types.WorkflowAuthorPolicy {
	if policy.ConformanceWeightBPS == 0 &&
		policy.MinBondPenaltyBPS == 0 &&
		policy.DefaultConformance == 0 {
		return types.DefaultWorkflowAuthorPolicy()
	}
	if policy.ConformanceWeightBPS > 10_000 {
		policy.ConformanceWeightBPS = 10_000
	}
	if policy.MinBondPenaltyBPS > 10_000 {
		policy.MinBondPenaltyBPS = 10_000
	}
	if policy.DefaultConformance == 0 {
		policy.DefaultConformance = 1.0
	}
	policy.DefaultConformance = types.Clamp01(policy.DefaultConformance)
	return policy
}

func workflowAuthorEventTimestamp(blockTime, outcomeTime time.Time) string {
	ts := outcomeTime
	if ts.IsZero() {
		ts = blockTime
	}
	return ts.UTC().Format(time.RFC3339)
}
