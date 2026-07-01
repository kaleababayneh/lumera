package types

import (
	"math"
	"strings"
	"time"
)

// ReceiptInput holds the minimal data needed from a receipt for reputation scoring.
// This mirrors internal/reputation.ReceiptData but is self-contained for chain use.
type ReceiptInput struct {
	ActionID          string
	ToolID            string
	PublisherID       string
	PayerID           string
	Success           bool
	Timestamp         time.Time
	LatencyMs         float64
	LatencySLOMs      float64
	QuotedCost        float64
	ActualCost        float64
	Confidential      bool
	NetPaid           float64
	Verified          bool
	VerificationKnown bool
	SelfPublished     bool
	SettlementStatus  string // settled|refunded|credited|chargeback
}

// ViolationInput holds a policy violation event for scoring.
type ViolationInput struct {
	Severity  float64 // 0.1 (minor) to 1.0 (critical)
	Timestamp time.Time
}

// ScoringConfig holds scoring configuration parameters.
// Values match PASSPORT_V01.md spec defaults.
type ScoringConfig struct {
	DecayDays                       float64
	DecayStartDays                  float64
	MinReceiptCount                 uint64
	UniquePayerMin                  float64
	ToolConcentrationFloor          float64
	FrequencyMaxPerDay              int
	FrequencyTau                    float64
	CollusionRiskThreshold          float64
	CollusionVerificationPenalty    float64
	CollusionMaxPayerShare          float64
	CollusionMaxPublisherShare      float64
	CollusionMaxToolShare           float64
	DefaultNetPaid                  float64
	NeutralReliability              float64
	DefaultSafety                   float64
	DefaultLatency                  float64
	DefaultCostDiscipline           float64
	DefaultDispute                  float64
	DefaultLongevity                float64
	DefaultPrivacy                  float64
	DefaultWorkflowConformance      float64
	WorkflowConformanceWeightBPS    uint32
	WorkflowAuthorMinBondPenaltyBPS uint32
	MaxLongevityDays                float64
}

// DefaultScoringConfig returns the spec-default scoring configuration.
func DefaultScoringConfig() ScoringConfig {
	return ScoringConfig{
		DecayDays:                       30.0,
		DecayStartDays:                  90.0,
		MinReceiptCount:                 50,
		UniquePayerMin:                  20,
		ToolConcentrationFloor:          0.35,
		FrequencyMaxPerDay:              10,
		FrequencyTau:                    5.0,
		CollusionRiskThreshold:          0.7,
		CollusionVerificationPenalty:    0.5,
		CollusionMaxPayerShare:          0.6,
		CollusionMaxPublisherShare:      0.6,
		CollusionMaxToolShare:           0.7,
		DefaultNetPaid:                  1.0,
		NeutralReliability:              0.5,
		DefaultSafety:                   1.0,
		DefaultLatency:                  1.0,
		DefaultCostDiscipline:           1.0,
		DefaultDispute:                  1.0,
		DefaultLongevity:                0.0,
		DefaultPrivacy:                  0.0,
		DefaultWorkflowConformance:      1.0,
		WorkflowConformanceWeightBPS:    1000,
		WorkflowAuthorMinBondPenaltyBPS: 5000,
		MaxLongevityDays:                365.0,
	}
}

// ScoringConfigFromParams overlays governable Passport Params on the spec-default
// scorer config. Zero or out-of-range values fall back to defaults so legacy
// pre-migration state cannot silently disable anti-gaming protections.
func ScoringConfigFromParams(params *Params) ScoringConfig {
	cfg := DefaultScoringConfig()
	if params == nil {
		return cfg
	}
	if validBPS(params.CollusionRiskThresholdBps) {
		cfg.CollusionRiskThreshold = bpsToRatio(params.CollusionRiskThresholdBps)
	}
	if validBPS(params.CollusionVerificationPenaltyBps) {
		cfg.CollusionVerificationPenalty = bpsToRatio(params.CollusionVerificationPenaltyBps)
	}
	if validBPS(params.CollusionMaxPayerShareBps) {
		cfg.CollusionMaxPayerShare = bpsToRatio(params.CollusionMaxPayerShareBps)
	}
	if validBPS(params.CollusionMaxPublisherShareBps) {
		cfg.CollusionMaxPublisherShare = bpsToRatio(params.CollusionMaxPublisherShareBps)
	}
	if validBPS(params.CollusionMaxToolShareBps) {
		cfg.CollusionMaxToolShare = bpsToRatio(params.CollusionMaxToolShareBps)
	}
	return cfg
}

func validBPS(value uint32) bool {
	return value > 0 && value <= maxBasisPoints
}

func bpsToRatio(value uint32) float64 {
	return float64(value) / float64(maxBasisPoints)
}

// ReputationResult holds the 7-dimensional reputation vector.
// All dimension values are in [0.0, 1.0].
type ReputationResult struct {
	Reliability                  float64
	Safety                       float64
	Latency                      float64
	CostDiscipline               float64
	Dispute                      float64
	Longevity                    float64
	Privacy                      float64
	WorkflowConformance          float64
	WorkflowConformanceWeightBPS uint32
	Eligible                     bool
	UpdatedAt                    time.Time
}

// WorkflowOutcomeInput holds one authored workflow bundle outcome for author scoring.
type WorkflowOutcomeInput struct {
	WorkflowID     string
	Outcome        string
	Timestamp      time.Time
	SLOConformant  bool
	EconomicWeight float64
}

// WorkflowAuthorPolicy captures governance-tunable workflow-author scoring knobs.
type WorkflowAuthorPolicy struct {
	ConformanceWeightBPS uint32
	MinBondPenaltyBPS    uint32
	DefaultConformance   float64
}

// WorkflowAuthorTierEvaluationInput evaluates a workflow author against base Passport state.
type WorkflowAuthorTierEvaluationInput struct {
	TierInput      TierEvaluationInput
	Outcomes       []WorkflowOutcomeInput
	Policy         WorkflowAuthorPolicy
	BaseMinBondLAC uint64
}

// WorkflowAuthorTierEvaluationResult reports Passport and bond effects for a workflow author.
type WorkflowAuthorTierEvaluationResult struct {
	Tier                 TierEligibilityResult
	WorkflowConformance  float64
	Composite            float64
	MinBondMultiplierBPS uint32
	MinBondLAC           uint64
}

// TierLevel is the Passport permission tier defined by PASSPORT_V01.md §7.
type TierLevel uint8

const (
	TierProbationary TierLevel = iota
	TierStandard
	TierTrusted
	TierPremium
)

const (
	TierBlockerNone                 = ""
	TierBlockerMinReceipts          = "min_receipts"
	TierBlockerReputationIneligible = "reputation_ineligible"
	TierBlockerMinScore             = "min_score"
	TierBlockerMinReliability       = "min_reliability"
	TierBlockerMinSafety            = "min_safety"
	TierBlockerMinLongevity         = "min_longevity"
	TierBlockerDisputeRate          = "dispute_rate"
	TierBlockerPromotionLockup      = "promotion_lockup"
	TierBlockerSlash                = "slash"
)

// TierDefinition captures governance-tunable thresholds for one Passport tier.
type TierDefinition struct {
	Level          TierLevel
	Name           string
	MinScore       float64
	MinReliability float64
	MinSafety      float64
	MinLongevity   float64
	MinReceipts    uint64
	Lockup         time.Duration
	// MaxDisputeRate is disabled when set to 1.0 or greater. PASSPORT_V01.md
	// does not define defaults, but the evaluator supports the strengthened
	// parent-bead contract where governance may supply a 30d dispute ceiling.
	MaxDisputeRate float64
}

// TierState is the consensus-visible state needed to evaluate promotions.
type TierState struct {
	CurrentTier        TierLevel
	TierEnteredAt      time.Time
	PromotionPendingTo TierLevel
	PromotionStartedAt time.Time
}

// TierEvaluationInput supplies all dynamic values for deterministic tier evaluation.
type TierEvaluationInput struct {
	Reputation     *ReputationResult
	ReceiptCount   uint64
	DisputeRate30d float64
	Slashed        bool
	Now            time.Time
	State          TierState
	Definitions    []TierDefinition
}

// TierEligibilityResult reports the evaluated tier decision and next durable state.
type TierEligibilityResult struct {
	EligibleTier     TierLevel
	CurrentTier      TierLevel
	CanPromote       bool
	Promoted         bool
	Demoted          bool
	PromotionBlocker string
	NextState        TierState
	Permissions      TierPermissions
}

// TierPermissions describes the router-facing capabilities unlocked by a tier.
type TierPermissions struct {
	Tier                 TierLevel
	MaxBudgetPerCallLAC  uint64
	MaxBudgetPerDayLAC   uint64
	MaxBudgetPerMonthLAC uint64
	ConfidentialAllowed  bool
	AllowedLanePatterns  []string
}

// DefaultTierDefinitions returns PASSPORT_V01.md §7 threshold defaults.
func DefaultTierDefinitions() []TierDefinition {
	return []TierDefinition{
		{
			Level:          TierProbationary,
			Name:           "Probationary",
			MinScore:       0.00,
			MinReliability: 0.00,
			MinSafety:      0.00,
			MinLongevity:   0.00,
			MaxDisputeRate: 1.0,
		},
		{
			Level:          TierStandard,
			Name:           "Standard",
			MinScore:       0.40,
			MinReliability: 0.50,
			MinSafety:      0.60,
			MinLongevity:   0.10,
			MinReceipts:    50,
			Lockup:         24 * time.Hour,
			MaxDisputeRate: 1.0,
		},
		{
			Level:          TierTrusted,
			Name:           "Trusted",
			MinScore:       0.60,
			MinReliability: 0.70,
			MinSafety:      0.80,
			MinLongevity:   0.30,
			MinReceipts:    50,
			Lockup:         7 * 24 * time.Hour,
			MaxDisputeRate: 1.0,
		},
		{
			Level:          TierPremium,
			Name:           "Premium",
			MinScore:       0.80,
			MinReliability: 0.85,
			MinSafety:      0.95,
			MinLongevity:   0.50,
			MinReceipts:    50,
			Lockup:         30 * 24 * time.Hour,
			MaxDisputeRate: 1.0,
		},
	}
}

// EvaluateTier applies deterministic Passport tier promotion and demotion rules.
func EvaluateTier(input TierEvaluationInput) TierEligibilityResult {
	now := input.Now
	if now.IsZero() && input.Reputation != nil {
		now = input.Reputation.UpdatedAt
	}
	defs := normalizeTierDefinitions(input.Definitions)
	state := normalizeTierState(input.State, now)
	result := TierEligibilityResult{
		CurrentTier: state.CurrentTier,
		NextState:   state,
	}
	if input.Slashed && state.CurrentTier > TierProbationary {
		nextTier := state.CurrentTier - 1
		result.Demoted = true
		result.CurrentTier = nextTier
		result.PromotionBlocker = TierBlockerSlash
		result.NextState = TierState{CurrentTier: nextTier, TierEnteredAt: now}
		result.EligibleTier = highestEligibleTier(defs, input.Reputation, input.ReceiptCount, input.DisputeRate30d)
		result.Permissions = TierPermissionsFor(nextTier)
		return result
	}

	eligibleTier := highestEligibleTier(defs, input.Reputation, input.ReceiptCount, input.DisputeRate30d)
	result.EligibleTier = eligibleTier
	if eligibleTier < state.CurrentTier {
		result.Demoted = true
		result.CurrentTier = eligibleTier
		result.NextState = TierState{CurrentTier: eligibleTier, TierEnteredAt: now}
		result.PromotionBlocker = firstTierBlocker(defs[state.CurrentTier], input.Reputation, input.ReceiptCount, input.DisputeRate30d)
		result.Permissions = TierPermissionsFor(eligibleTier)
		return result
	}

	result.CurrentTier = state.CurrentTier
	result.Permissions = TierPermissionsFor(state.CurrentTier)
	if state.CurrentTier >= TierPremium {
		return result
	}

	target := state.CurrentTier + 1
	blocker := firstTierBlocker(defs[target], input.Reputation, input.ReceiptCount, input.DisputeRate30d)
	if blocker != TierBlockerNone {
		result.PromotionBlocker = blocker
		result.NextState.PromotionPendingTo = TierProbationary
		result.NextState.PromotionStartedAt = time.Time{}
		return result
	}

	lockup := defs[target].Lockup
	if state.PromotionPendingTo != target || state.PromotionStartedAt.IsZero() {
		result.CanPromote = lockup <= 0
		if result.CanPromote {
			result.Promoted = true
			result.CurrentTier = target
			result.NextState = TierState{CurrentTier: target, TierEnteredAt: now}
			result.Permissions = TierPermissionsFor(target)
			return result
		}
		result.PromotionBlocker = TierBlockerPromotionLockup
		result.NextState.PromotionPendingTo = target
		result.NextState.PromotionStartedAt = now
		return result
	}

	if now.Sub(state.PromotionStartedAt) < lockup {
		result.PromotionBlocker = TierBlockerPromotionLockup
		return result
	}

	result.CanPromote = true
	result.Promoted = true
	result.CurrentTier = target
	result.NextState = TierState{CurrentTier: target, TierEnteredAt: now}
	result.Permissions = TierPermissionsFor(target)
	return result
}

// DefaultWorkflowAuthorPolicy returns the default Agent Contracts passport policy.
func DefaultWorkflowAuthorPolicy() WorkflowAuthorPolicy {
	cfg := DefaultScoringConfig()
	return WorkflowAuthorPolicy{
		ConformanceWeightBPS: cfg.WorkflowConformanceWeightBPS,
		MinBondPenaltyBPS:    cfg.WorkflowAuthorMinBondPenaltyBPS,
		DefaultConformance:   cfg.DefaultWorkflowConformance,
	}
}

// ComputeWorkflowConformance calculates weighted SLO conformance for authored bundles.
func ComputeWorkflowConformance(cfg ScoringConfig, outcomes []WorkflowOutcomeInput, blockTime time.Time) float64 {
	defaultScore := cfg.DefaultWorkflowConformance
	if defaultScore == 0 {
		defaultScore = 1.0
	}
	if len(outcomes) == 0 {
		return Clamp01(defaultScore)
	}

	var conforming, total float64
	for i := range outcomes {
		outcome := &outcomes[i]
		weight := workflowOutcomeWeight(cfg, outcome, blockTime)
		if weight <= 0 {
			continue
		}
		total += weight
		if workflowOutcomeConforms(outcome) {
			conforming += weight
		}
	}
	if total == 0 {
		return Clamp01(defaultScore)
	}
	return Clamp01(conforming / total)
}

// EvaluateWorkflowAuthorTier applies workflow-author conformance to Passport tiering.
func EvaluateWorkflowAuthorTier(input WorkflowAuthorTierEvaluationInput) WorkflowAuthorTierEvaluationResult {
	policy := normalizeWorkflowAuthorPolicy(input.Policy)
	cfg := DefaultScoringConfig()
	cfg.DefaultWorkflowConformance = policy.DefaultConformance
	conformance := ComputeWorkflowConformance(cfg, input.Outcomes, input.TierInput.Now)

	tierInput := input.TierInput
	rep := cloneReputationResult(tierInput.Reputation)
	if rep != nil {
		rep.WorkflowConformance = conformance
		rep.WorkflowConformanceWeightBPS = policy.ConformanceWeightBPS
	}
	tierInput.Reputation = rep

	tier := EvaluateTier(tierInput)
	composite := 0.0
	if rep != nil {
		composite = rep.BalancedComposite()
	}
	return WorkflowAuthorTierEvaluationResult{
		Tier:                 tier,
		WorkflowConformance:  conformance,
		Composite:            composite,
		MinBondMultiplierBPS: WorkflowAuthorMinBondMultiplierBPS(conformance, policy.MinBondPenaltyBPS),
		MinBondLAC:           WorkflowAuthorMinBondLAC(input.BaseMinBondLAC, conformance, policy.MinBondPenaltyBPS),
	}
}

// WorkflowAuthorMinBondMultiplierBPS returns the conformance-adjusted bond multiplier.
func WorkflowAuthorMinBondMultiplierBPS(conformance float64, maxPenaltyBPS uint32) uint32 {
	penalty := uint32(math.Round((1.0 - Clamp01(conformance)) * float64(clampBPS(maxPenaltyBPS))))
	return 10_000 + penalty
}

// WorkflowAuthorMinBondLAC applies the conformance multiplier to a base author bond.
func WorkflowAuthorMinBondLAC(base uint64, conformance float64, maxPenaltyBPS uint32) uint64 {
	multiplier := uint64(WorkflowAuthorMinBondMultiplierBPS(conformance, maxPenaltyBPS))
	const denom = uint64(10_000)
	if base == 0 {
		return 0
	}
	maxUint := ^uint64(0)
	if base > maxUint/multiplier {
		return maxUint
	}
	product := base * multiplier
	return (product + denom - 1) / denom
}

// TierPermissionsFor returns the PASSPORT_V01.md budget and lane defaults for a tier.
func TierPermissionsFor(tier TierLevel) TierPermissions {
	switch normalizeTier(tier) {
	case TierPremium:
		return TierPermissions{
			Tier:                 TierPremium,
			MaxBudgetPerCallLAC:  2000,
			MaxBudgetPerDayLAC:   20000,
			MaxBudgetPerMonthLAC: 100000,
			ConfidentialAllowed:  true,
			AllowedLanePatterns:  []string{"*"},
		}
	case TierTrusted:
		return TierPermissions{
			Tier:                 TierTrusted,
			MaxBudgetPerCallLAC:  500,
			MaxBudgetPerDayLAC:   5000,
			MaxBudgetPerMonthLAC: 25000,
			AllowedLanePatterns:  []string{"public-*", "demo-*", "standard-*"},
		}
	case TierStandard:
		return TierPermissions{
			Tier:                 TierStandard,
			MaxBudgetPerCallLAC:  100,
			MaxBudgetPerDayLAC:   1000,
			MaxBudgetPerMonthLAC: 5000,
			AllowedLanePatterns:  []string{"public-*", "demo-*", "standard-*"},
		}
	default:
		return TierPermissions{
			Tier:                 TierProbationary,
			MaxBudgetPerCallLAC:  10,
			MaxBudgetPerDayLAC:   100,
			MaxBudgetPerMonthLAC: 500,
			AllowedLanePatterns:  []string{"public-*", "demo-*"},
		}
	}
}

func normalizeTierDefinitions(defs []TierDefinition) []TierDefinition {
	if len(defs) < 4 {
		return DefaultTierDefinitions()
	}
	out := make([]TierDefinition, 4)
	copy(out, defs[:4])
	for i := range out {
		out[i].Level = TierLevel(i)
		out[i].MinScore = Clamp01(out[i].MinScore)
		out[i].MinReliability = Clamp01(out[i].MinReliability)
		out[i].MinSafety = Clamp01(out[i].MinSafety)
		out[i].MinLongevity = Clamp01(out[i].MinLongevity)
		out[i].MaxDisputeRate = Clamp01(out[i].MaxDisputeRate)
	}
	return out
}

func normalizeTierState(state TierState, now time.Time) TierState {
	state.CurrentTier = normalizeTier(state.CurrentTier)
	state.PromotionPendingTo = normalizeTier(state.PromotionPendingTo)
	if state.TierEnteredAt.IsZero() {
		state.TierEnteredAt = now
	}
	if state.PromotionPendingTo <= state.CurrentTier {
		state.PromotionPendingTo = TierProbationary
		state.PromotionStartedAt = time.Time{}
	}
	return state
}

func normalizeTier(tier TierLevel) TierLevel {
	if tier > TierPremium {
		return TierProbationary
	}
	return tier
}

func highestEligibleTier(defs []TierDefinition, rep *ReputationResult, receiptCount uint64, disputeRate float64) TierLevel {
	for tier := TierPremium; tier > TierProbationary; tier-- {
		if firstTierBlocker(defs[tier], rep, receiptCount, disputeRate) == TierBlockerNone {
			return tier
		}
	}
	return TierProbationary
}

func firstTierBlocker(def TierDefinition, rep *ReputationResult, receiptCount uint64, disputeRate float64) string {
	if def.Level == TierProbationary {
		return TierBlockerNone
	}
	if rep == nil {
		return TierBlockerReputationIneligible
	}
	if receiptCount < def.MinReceipts {
		return TierBlockerMinReceipts
	}
	if !rep.Eligible {
		return TierBlockerReputationIneligible
	}
	if Clamp01(rep.BalancedComposite()) < def.MinScore {
		return TierBlockerMinScore
	}
	if Clamp01(rep.Reliability) < def.MinReliability {
		return TierBlockerMinReliability
	}
	if Clamp01(rep.Safety) < def.MinSafety {
		return TierBlockerMinSafety
	}
	if Clamp01(rep.Longevity) < def.MinLongevity {
		return TierBlockerMinLongevity
	}
	if def.MaxDisputeRate < 1.0 && Clamp01(disputeRate) > def.MaxDisputeRate {
		return TierBlockerDisputeRate
	}
	return TierBlockerNone
}

// BalancedComposite calculates the weighted composite score using the balanced profile.
// Weights: reliability=0.25, safety=0.30, latency=0.10, cost=0.10, dispute=0.15, longevity=0.10, privacy=0.00.
func (r *ReputationResult) BalancedComposite() float64 {
	if r == nil {
		return 0
	}
	base := r.Reliability*0.25 +
		r.Safety*0.30 +
		r.Latency*0.10 +
		r.CostDiscipline*0.10 +
		r.Dispute*0.15 +
		r.Longevity*0.10 +
		r.Privacy*0.00
	weight := float64(clampBPS(r.WorkflowConformanceWeightBPS)) / 10_000.0
	if weight == 0 {
		return base
	}
	return base*(1.0-weight) + Clamp01(r.WorkflowConformance)*weight
}

// ToProto maps the 7-dimensional result onto the existing 4-field proto ReputationVector.
//
// Proto mapping (until proto is extended to 7+ fields):
//
//	reliability     -> reliability  (direct)
//	safety          -> quality      (safety = quality of service)
//	dispute         -> trustworthiness (dispute handling = trust)
//	balanced composite -> composite
func (r *ReputationResult) ToProto(blockTime time.Time) *ReputationVector {
	toU32 := func(f float64) uint32 {
		return uint32(math.Round(Clamp01(f) * 1000))
	}
	updatedAt := r.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = blockTime
	}
	rv := &ReputationVector{
		Reliability:     toU32(r.Reliability),
		Quality:         toU32(r.Safety),
		Trustworthiness: toU32(r.Dispute),
		Composite:       toU32(r.BalancedComposite()),
		UpdatedAt:       timestampProto(updatedAt),
	}
	return rv
}

// ToScoreBreakdown maps the full 7-dimensional scoring result to query state.
func (r *ReputationResult) ToScoreBreakdown(blockTime time.Time) *PassportScoreBreakdown {
	toU32 := func(f float64) uint32 {
		return uint32(math.Round(Clamp01(f) * 1000))
	}
	updatedAt := blockTime
	if r != nil && !r.UpdatedAt.IsZero() {
		updatedAt = r.UpdatedAt
	}
	if r == nil {
		return &PassportScoreBreakdown{UpdatedAt: timestampProto(updatedAt)}
	}
	return &PassportScoreBreakdown{
		Reliability:    toU32(r.Reliability),
		Safety:         toU32(r.Safety),
		Latency:        toU32(r.Latency),
		CostDiscipline: toU32(r.CostDiscipline),
		Dispute:        toU32(r.Dispute),
		Longevity:      toU32(r.Longevity),
		Privacy:        toU32(r.Privacy),
		Composite:      toU32(r.BalancedComposite()),
		Eligible:       r.Eligible,
		UpdatedAt:      timestampProto(updatedAt),
	}
}

// ScoreBreakdownFromProto backfills the 7-dimensional query view for old state.
func ScoreBreakdownFromProto(rep *ReputationVector, summary *PassportSummary) *PassportScoreBreakdown {
	if rep == nil {
		return NeutralPassportScoreBreakdown(time.Time{})
	}
	eligible := summary != nil && summary.TotalReceipts >= DefaultScoringConfig().MinReceiptCount
	return &PassportScoreBreakdown{
		Reliability:    rep.Reliability,
		Safety:         rep.Quality,
		Latency:        1000,
		CostDiscipline: 1000,
		Dispute:        rep.Trustworthiness,
		Longevity:      0,
		Privacy:        0,
		Composite:      rep.Composite,
		Eligible:       eligible,
		UpdatedAt:      rep.UpdatedAt,
	}
}

// NeutralPassportScoreBreakdown returns a bounded bootstrap score surface.
func NeutralPassportScoreBreakdown(blockTime time.Time) *PassportScoreBreakdown {
	return &PassportScoreBreakdown{
		Reliability:    500,
		Safety:         500,
		Latency:        500,
		CostDiscipline: 500,
		Dispute:        500,
		Longevity:      0,
		Privacy:        0,
		Composite:      500,
		UpdatedAt:      timestampProto(blockTime),
	}
}

// InitialPassportTierState returns the default Probationary state for new records.
func InitialPassportTierState(blockTime time.Time) *PassportTierState {
	permissions := TierPermissionsFor(TierProbationary)
	return &PassportTierState{
		CurrentTier:   PassportTierFromLevel(TierProbationary),
		EligibleTier:  PassportTierFromLevel(TierProbationary),
		TierEnteredAt: timestampProto(blockTime),
		Permissions:   TierPermissionsToProto(permissions),
	}
}

// InitialPassportTierHistoryEntry records the bootstrap Probationary assignment.
func InitialPassportTierHistoryEntry(blockTime time.Time, score *PassportScoreBreakdown) *PassportTierHistoryEntry {
	return &PassportTierHistoryEntry{
		PreviousTier:   PassportTier_PASSPORT_TIER_UNSPECIFIED,
		NewTier:        PassportTierFromLevel(TierProbationary),
		Reason:         "registered",
		ScoreBreakdown: cloneScoreBreakdown(score),
		TransitionedAt: timestampProto(blockTime),
	}
}

// TierStateFromProto converts stored query state to the evaluator state.
func TierStateFromProto(state *PassportTierState, now time.Time) TierState {
	if state == nil {
		return TierState{CurrentTier: TierProbationary, TierEnteredAt: now}
	}
	return TierState{
		CurrentTier:        TierLevelFromPassportTier(state.CurrentTier),
		TierEnteredAt:      timestampTimeOr(state.TierEnteredAt, now),
		PromotionPendingTo: TierLevelFromPassportTier(state.PromotionPendingTo),
		PromotionStartedAt: timestampTimeOr(state.PromotionStartedAt, time.Time{}),
	}
}

// TierEvaluationResultToProto converts an evaluator result to queryable state.
func TierEvaluationResultToProto(result TierEligibilityResult, definitions []TierDefinition) *PassportTierState {
	state := result.NextState
	out := &PassportTierState{
		CurrentTier:        PassportTierFromLevel(result.CurrentTier),
		EligibleTier:       PassportTierFromLevel(result.EligibleTier),
		PromotionPendingTo: PassportTierFromLevel(state.PromotionPendingTo),
		TierEnteredAt:      timestampProto(state.TierEnteredAt),
		PromotionStartedAt: timestampProto(state.PromotionStartedAt),
		CanPromote:         result.CanPromote,
		Promoted:           result.Promoted,
		Demoted:            result.Demoted,
		PromotionBlocker:   result.PromotionBlocker,
		Permissions:        TierPermissionsToProto(result.Permissions),
	}
	if !state.PromotionStartedAt.IsZero() && state.PromotionPendingTo > TierProbationary {
		defs := normalizeTierDefinitions(definitions)
		lockup := defs[state.PromotionPendingTo].Lockup
		if lockup > 0 {
			out.LockupExpiresAt = timestampProto(state.PromotionStartedAt.Add(lockup))
		}
	}
	return out
}

// TierHistoryEntryFromEvaluation records an actual promotion or demotion.
func TierHistoryEntryFromEvaluation(previous TierLevel, result TierEligibilityResult, score *PassportScoreBreakdown, blockTime time.Time) *PassportTierHistoryEntry {
	reason := "updated"
	if result.Promoted {
		reason = "promoted"
	} else if result.Demoted {
		reason = "demoted"
	}
	return &PassportTierHistoryEntry{
		PreviousTier:     PassportTierFromLevel(previous),
		NewTier:          PassportTierFromLevel(result.CurrentTier),
		Reason:           reason,
		PromotionBlocker: result.PromotionBlocker,
		ScoreBreakdown:   cloneScoreBreakdown(score),
		Promoted:         result.Promoted,
		Demoted:          result.Demoted,
		TransitionedAt:   timestampProto(blockTime),
	}
}

func TierPermissionsToProto(permissions TierPermissions) *PassportTierPermissions {
	return &PassportTierPermissions{
		Tier:                 PassportTierFromLevel(permissions.Tier),
		MaxBudgetPerCallLac:  permissions.MaxBudgetPerCallLAC,
		MaxBudgetPerDayLac:   permissions.MaxBudgetPerDayLAC,
		MaxBudgetPerMonthLac: permissions.MaxBudgetPerMonthLAC,
		ConfidentialAllowed:  permissions.ConfidentialAllowed,
		AllowedLanePatterns:  append([]string(nil), permissions.AllowedLanePatterns...),
	}
}

func PassportTierFromLevel(tier TierLevel) PassportTier {
	switch normalizeTier(tier) {
	case TierPremium:
		return PassportTier_PASSPORT_TIER_PREMIUM
	case TierTrusted:
		return PassportTier_PASSPORT_TIER_TRUSTED
	case TierStandard:
		return PassportTier_PASSPORT_TIER_STANDARD
	default:
		return PassportTier_PASSPORT_TIER_PROBATIONARY
	}
}

func TierLevelFromPassportTier(tier PassportTier) TierLevel {
	switch tier {
	case PassportTier_PASSPORT_TIER_PREMIUM:
		return TierPremium
	case PassportTier_PASSPORT_TIER_TRUSTED:
		return TierTrusted
	case PassportTier_PASSPORT_TIER_STANDARD:
		return TierStandard
	default:
		return TierProbationary
	}
}

func timestampProto(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func timestampTimeOr(ts *time.Time, fallback time.Time) time.Time {
	if ts == nil {
		return fallback
	}
	return ts.UTC()
}

func cloneScoreBreakdown(score *PassportScoreBreakdown) *PassportScoreBreakdown {
	if score == nil {
		return nil
	}
	clone := *score
	return &clone
}

// ReputationResultFromScoreBreakdown reconstructs the full scorer input from query state.
func ReputationResultFromScoreBreakdown(score *PassportScoreBreakdown) *ReputationResult {
	if score == nil {
		return nil
	}
	fromU32 := func(v uint32) float64 {
		return Clamp01(float64(v) / 1000.0)
	}
	return &ReputationResult{
		Reliability:    fromU32(score.GetReliability()),
		Safety:         fromU32(score.GetSafety()),
		Latency:        fromU32(score.GetLatency()),
		CostDiscipline: fromU32(score.GetCostDiscipline()),
		Dispute:        fromU32(score.GetDispute()),
		Longevity:      fromU32(score.GetLongevity()),
		Privacy:        fromU32(score.GetPrivacy()),
		Eligible:       score.GetEligible(),
		UpdatedAt:      timestampTimeOr(score.GetUpdatedAt(), time.Time{}),
	}
}

// SummaryInput holds aggregated passport summary data for scoring.
type SummaryInput struct {
	TotalReceipts    uint64
	DisputeCount     uint32
	DisputeLostCount uint32
	FirstReceiptAt   time.Time
	LastReceiptAt    time.Time
}

// SummaryInputFromProto extracts SummaryInput from a proto PassportSummary.
func SummaryInputFromProto(s *PassportSummary) *SummaryInput {
	if s == nil {
		return nil
	}
	first := time.Time{}
	last := time.Time{}
	if s.FirstReceiptTs > 0 {
		first = time.Unix(s.FirstReceiptTs, 0).UTC()
	}
	if s.LastReceiptTs > 0 {
		last = time.Unix(s.LastReceiptTs, 0).UTC()
	}
	return &SummaryInput{
		TotalReceipts:    s.TotalReceipts,
		DisputeCount:     s.DisputeCount,
		DisputeLostCount: s.DisputeLostCount,
		FirstReceiptAt:   first,
		LastReceiptAt:    last,
	}
}

// Clamp01 clamps a float64 value to [0, 1].
// NaN is treated as 0 to prevent consensus divergence from non-deterministic
// NaN bit patterns across validator platforms.
func Clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeWorkflowAuthorPolicy(policy WorkflowAuthorPolicy) WorkflowAuthorPolicy {
	if policy.ConformanceWeightBPS == 0 &&
		policy.MinBondPenaltyBPS == 0 &&
		policy.DefaultConformance == 0 {
		return DefaultWorkflowAuthorPolicy()
	}
	policy.ConformanceWeightBPS = clampBPS(policy.ConformanceWeightBPS)
	policy.MinBondPenaltyBPS = clampBPS(policy.MinBondPenaltyBPS)
	if policy.DefaultConformance == 0 {
		policy.DefaultConformance = 1.0
	}
	policy.DefaultConformance = Clamp01(policy.DefaultConformance)
	return policy
}

func workflowOutcomeConforms(outcome *WorkflowOutcomeInput) bool {
	if outcome == nil || !outcome.SLOConformant {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(outcome.Outcome)) {
	case "FINALIZED", "SUCCESS", "SETTLED":
		return true
	default:
		return false
	}
}

func workflowOutcomeWeight(cfg ScoringConfig, outcome *WorkflowOutcomeInput, blockTime time.Time) float64 {
	if outcome == nil {
		return 0
	}
	weight := outcome.EconomicWeight
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
		weight = 1.0
	}
	weight = math.Sqrt(weight)
	if !blockTime.IsZero() && !outcome.Timestamp.IsZero() {
		ageDays := blockTime.Sub(outcome.Timestamp).Hours() / 24.0
		if ageDays < 0 {
			ageDays = 0
		}
		weight *= workflowDecayWeight(cfg, ageDays)
	}
	return weight
}

func workflowDecayWeight(cfg ScoringConfig, ageDays float64) float64 {
	if ageDays <= cfg.DecayStartDays {
		return 1.0
	}
	decayDays := cfg.DecayDays
	if decayDays <= 0 {
		decayDays = 30.0
	}
	return ExpNeg(ageDays / decayDays)
}

func cloneReputationResult(in *ReputationResult) *ReputationResult {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func clampBPS(v uint32) uint32 {
	if v > 10_000 {
		return 10_000
	}
	return v
}
