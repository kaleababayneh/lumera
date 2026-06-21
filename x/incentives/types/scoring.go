package types

import "sort"

// ScoringWeights defines the weights for each scoring component.
// Values are in basis points (10000 = 100%).
type ScoringWeights struct {
	Reliability uint32 // 35% = 3500 bps
	Economic    uint32 // 25% = 2500 bps
	Security    uint32 // 20% = 2000 bps
	Performance uint32 // 20% = 2000 bps
	Governance  uint32 // 0% = 0 bps (default, can be configured)
}

// DefaultScoringWeights returns the default scoring weights per the lumera-verified spec.
func DefaultScoringWeights() ScoringWeights {
	return ScoringWeights{
		Reliability: 3000, // 30%
		Economic:    2000, // 20%
		Security:    2000, // 20%
		Performance: 2000, // 20%
		Governance:  1000, // 10%
	}
}

// Scorer calculates composite scores and determines badge tier eligibility.
type Scorer struct {
	weights ScoringWeights
}

// NewScorer creates a new Scorer with the given weights.
func NewScorer(weights ScoringWeights) *Scorer {
	return &Scorer{weights: weights}
}

// NewDefaultScorer creates a Scorer with default weights.
func NewDefaultScorer() *Scorer {
	return NewScorer(DefaultScoringWeights())
}

// CalculateCompositeScore computes the weighted composite score from component scores.
// All input scores are expected to be in range [0, 10000] (0-100.00%).
// Returns a score in range [0, 10000].
func (s *Scorer) CalculateCompositeScore(components *ComponentScores) uint32 {
	if components == nil {
		return 0
	}

	// Calculate weighted sum
	// Each component is 0-10000, weight is in bps (0-10000)
	// Total weight is 10000 (100%), so we divide by 10000 to normalize
	totalWeight := s.weights.Reliability + s.weights.Economic + s.weights.Security + s.weights.Performance + s.weights.Governance
	if totalWeight == 0 {
		return 0
	}

	weighted := uint64(components.Reliability)*uint64(s.weights.Reliability) +
		uint64(components.Economic)*uint64(s.weights.Economic) +
		uint64(components.Security)*uint64(s.weights.Security) +
		uint64(components.Performance)*uint64(s.weights.Performance) +
		uint64(components.Governance)*uint64(s.weights.Governance)

	// Normalize by total weight
	score := weighted / uint64(totalWeight)

	// Cap at MaxScore
	if score > MaxScore {
		score = MaxScore
	}

	return uint32(score)
}

// CalculateReliabilityScore computes the reliability component score from metrics.
// Returns a score in range [0, 10000].
func (s *Scorer) CalculateReliabilityScore(metrics *MetricSnapshot) uint32 {
	if metrics == nil {
		return 0
	}

	// Uptime score (35% of reliability) - quadratic penalty for downtime
	uptimeScore := quadraticScore(metrics.UptimeBps, BasisPointsScale)

	// Success rate score (30% of reliability) - cubic penalty for failures
	successScore := cubicScore(metrics.SuccessRateBps, BasisPointsScale)

	// Latency score (25% of reliability) - inverse relationship
	// score = max(0, 1 - p95/SLO) = max(0, (SLO - p95) * BPS / SLO)
	sloLatencyMs := uint32(1000)
	latencyScore := uint32(0)
	if metrics.P95LatencyMs < sloLatencyMs {
		latencyScore = (sloLatencyMs - metrics.P95LatencyMs) * BasisPointsScale / sloLatencyMs
	}

	// Consistency bonus (10% of reliability) - low variance is good
	// score = BPS / (1 + variance/1000) = BPS * 1000 / (1000 + variance)
	consistencyScore := uint32(BasisPointsScale)
	if metrics.LatencyVariance > 0 {
		denom := uint32(1000) + metrics.LatencyVariance
		consistencyScore = BasisPointsScale * 1000 / denom
	}

	// Weighted average
	total := uint64(uptimeScore)*35 +
		uint64(successScore)*30 +
		uint64(latencyScore)*25 +
		uint64(consistencyScore)*10

	return uint32(total / 100)
}

// CalculateEconomicScore computes the economic integrity component score.
// Returns a score in range [0, 10000].
func (s *Scorer) CalculateEconomicScore(metrics *MetricSnapshot) uint32 {
	if metrics == nil {
		return 0
	}

	// Quote accuracy score (30%) - asymmetric penalty (underquoting worse)
	quoteScore := calculateQuoteScore(metrics.QuoteDeviationBps)

	// Receipt validity score (25%) - linear relationship
	receiptScore := metrics.ReceiptValidityBps

	// Settlement accuracy score (25%) - quadratic bonus
	settlementScore := quadraticScore(metrics.SettlementAccuracyBps, BasisPointsScale)

	// Insurance utilization score (20%) - target is 50%, penalize deviation
	targetUtil := uint32(5000)                                                     // 50%
	insuranceScore := calculateDeviationScore(metrics.CacheHitRateBps, targetUtil) // Using cache as proxy

	// Weighted average
	total := uint64(quoteScore)*30 +
		uint64(receiptScore)*25 +
		uint64(settlementScore)*25 +
		uint64(insuranceScore)*20

	return uint32(total / 100)
}

// CalculateSecurityScore computes the security posture component score.
// Returns a score in range [0, 10000].
func (s *Scorer) CalculateSecurityScore(metrics *MetricSnapshot) uint32 {
	if metrics == nil {
		return 0
	}

	// SBOM freshness score (20%) - exponential decay with week half-life
	sbomScore := calculateExponentialDecay(metrics.SbomAgeHours, 168) // 168 hours = 1 week

	// SLSA level score (20%) - discrete steps
	slsaScore := uint32(0)
	switch metrics.SlsaLevel {
	case 0:
		slsaScore = 0
	case 1:
		slsaScore = 5000 // 50%
	case 2:
		slsaScore = 7500 // 75%
	case 3:
		slsaScore = BasisPointsScale // 100%
	}

	// Vulnerability score (35%) - heavy penalty for criticals
	vulnScore := calculateVulnerabilityScore(metrics.CriticalVulnerabilities, metrics.HighVulnerabilities)

	// Remaining (25%) baseline security
	baselineScore := uint32(BasisPointsScale) // 100% baseline

	// Weighted average
	total := uint64(sbomScore)*20 +
		uint64(slsaScore)*20 +
		uint64(vulnScore)*35 +
		uint64(baselineScore)*25

	return uint32(total / 100)
}

// CalculatePerformanceScore computes the performance component score.
// Returns a score in range [0, 10000].
func (s *Scorer) CalculatePerformanceScore(metrics *MetricSnapshot) uint32 {
	if metrics == nil {
		return 0
	}

	// Throughput vs capacity score (40%)
	// score = min(RPS/capacity, 1.0) * BPS
	throughputScore := uint32(0)
	if metrics.DeclaredCapacity > 0 {
		rps := metrics.RequestsPerSecond
		if rps > metrics.DeclaredCapacity {
			rps = metrics.DeclaredCapacity
		}
		throughputScore = uint32(uint64(rps) * uint64(BasisPointsScale) / uint64(metrics.DeclaredCapacity))
	}

	// Cache hit rate score (30%) - for cache-eligible tools
	cacheScore := metrics.CacheHitRateBps

	// Efficiency baseline (30%) - assume good efficiency if invocations are successful
	efficiencyScore := uint32(BasisPointsScale) // 100% baseline

	// Weighted average
	total := uint64(throughputScore)*40 +
		uint64(cacheScore)*30 +
		uint64(efficiencyScore)*30

	return uint32(total / 100)
}

// CalculateGovernanceScore computes the governance/compliance component score.
// Returns a score in range [0, 10000]. This is used as a bonus/penalty modifier.
func (s *Scorer) CalculateGovernanceScore(metrics *MetricSnapshot) uint32 {
	if metrics == nil {
		return 5000 // Neutral 50%
	}

	// Dispute rate score (50%) - linear penalty
	disputeScore := uint32(0)
	if metrics.DisputeRateBps < BasisPointsScale {
		disputeScore = BasisPointsScale - metrics.DisputeRateBps
	}

	// Governance participation score (50%)
	participationScore := metrics.GovernanceParticipationBps

	// Average
	return (disputeScore + participationScore) / 2
}

// CalculateAllComponentScores calculates all component scores from metrics.
func (s *Scorer) CalculateAllComponentScores(metrics *MetricSnapshot) *ComponentScores {
	if metrics == nil {
		return &ComponentScores{}
	}

	return &ComponentScores{
		Reliability: s.CalculateReliabilityScore(metrics),
		Economic:    s.CalculateEconomicScore(metrics),
		Security:    s.CalculateSecurityScore(metrics),
		Performance: s.CalculatePerformanceScore(metrics),
		Governance:  s.CalculateGovernanceScore(metrics),
	}
}

// DetermineTier determines the badge tier based on composite score.
// Configs are sorted by MinimumScore descending and the first entry
// whose threshold the score clears wins. Relying on BadgeTier enum
// numeric order (as the prior implementation did) silently broke if a
// new tier was inserted out of numeric order; sorting on the actual
// MinimumScore field pins the contract to the data, not the enum
// layout (lumera_ai-aya8r).
func (s *Scorer) DetermineTier(score uint32, tierConfigs []*TierConfig) BadgeTier {
	if len(tierConfigs) == 0 {
		return BadgeTier_BADGE_TIER_NONE
	}

	ordered := make([]*TierConfig, 0, len(tierConfigs))
	for _, cfg := range tierConfigs {
		if cfg == nil {
			continue
		}
		ordered = append(ordered, cfg)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].MinimumScore > ordered[j].MinimumScore
	})

	for _, config := range ordered {
		if score >= config.MinimumScore {
			return config.Tier
		}
	}

	return BadgeTier_BADGE_TIER_NONE
}

// Helper functions — all use pure integer arithmetic for consensus determinism.

// quadraticScore applies quadratic scaling: (value/max)^2 * max = value^2 / max.
// Uses nearest-integer rounding (add max/2 before dividing) instead of truncation
// so inputs between sqrt(max)/2 and sqrt(max) round to 1 rather than collapsing
// to 0. Rounding is still deterministic across validators.
func quadraticScore(value, max uint32) uint32 {
	if max == 0 {
		return 0
	}
	num := uint64(value) * uint64(value)
	den := uint64(max)
	return uint32((num + den/2) / den)
}

// cubicScore applies cubic scaling: (value/max)^3 * max = value^3 / max^2.
// Same rounding rationale as quadraticScore — the cubic curve is even steeper,
// so truncation erased ordering information for inputs well above zero.
func cubicScore(value, max uint32) uint32 {
	if max == 0 {
		return 0
	}
	num := uint64(value) * uint64(value) * uint64(value)
	den := uint64(max) * uint64(max)
	return uint32((num + den/2) / den)
}

// calculateQuoteScore calculates quote accuracy score with asymmetric penalty.
// Negative deviation (underquoting) is penalized 2x compared to positive (overquoting).
// score = max(0, BPS - penalty), where penalty = |dev|*multiplier.
func calculateQuoteScore(deviationBps int32) uint32 {
	if deviationBps < 0 {
		// Underquoting - double penalty: penalty = |dev| * 2
		// Use int64 to avoid overflow on math.MinInt32.
		absDev := int64(-int64(deviationBps))
		penaltyBps := uint64(absDev) * 2
		if penaltyBps >= uint64(BasisPointsScale) {
			return 0
		}
		return BasisPointsScale - uint32(penaltyBps)
	}
	// Overquoting - half penalty: penalty = dev / 2
	penaltyBps := uint32(deviationBps) / 2
	if penaltyBps >= BasisPointsScale {
		return 0
	}
	return BasisPointsScale - penaltyBps
}

// calculateDeviationScore calculates a score based on deviation from target.
// score = max(0, BPS - |value-target| * BPS / target)
func calculateDeviationScore(value, target uint32) uint32 {
	if target == 0 {
		return BasisPointsScale
	}
	var diff uint32
	if value > target {
		diff = value - target
	} else {
		diff = target - value
	}
	penaltyBps := uint64(diff) * uint64(BasisPointsScale) / uint64(target)
	if penaltyBps >= uint64(BasisPointsScale) {
		return 0
	}
	return BasisPointsScale - uint32(penaltyBps)
}

// expDecayBps is a precomputed lookup table for exp(-x/10) * 10000 (BPS).
// Index i corresponds to x = i/10, so expDecayBps[0] = exp(0)*10000 = 10000,
// expDecayBps[10] = exp(-1)*10000 = 3679, etc.
// Values are rounded to nearest integer. For indices >= len, return 0.
var expDecayBps = [93]uint32{
	10000, 9048, 8187, 7408, 6703, 6065, 5488, 4966, 4493, 4066, // x = 0.0–0.9
	3679, 3329, 3012, 2725, 2466, 2231, 2019, 1827, 1653, 1496, // x = 1.0–1.9
	1353, 1225, 1108, 1003, 907, 821, 743, 672, 608, 550, //        x = 2.0–2.9
	498, 450, 408, 369, 334, 302, 274, 247, 224, 202, //            x = 3.0–3.9
	183, 166, 150, 136, 123, 111, 101, 91, 82, 74, //               x = 4.0–4.9
	67, 61, 55, 50, 45, 41, 37, 33, 30, 27, //                      x = 5.0–5.9
	25, 22, 20, 18, 17, 15, 14, 12, 11, 10, //                      x = 6.0–6.9
	9, 8, 7, 7, 6, 6, 5, 5, 4, 4, //                                x = 7.0–7.9
	3, 3, 3, 2, 2, 2, 2, 2, 2, 1, //                                x = 8.0–8.9
	1, 1, 1, //                                                      x = 9.0–9.2
}

// calculateExponentialDecay calculates score with exponential decay based on age.
// Uses a precomputed lookup table with linear interpolation for deterministic results.
func calculateExponentialDecay(ageHours, halfLifeHours uint32) uint32 {
	if halfLifeHours == 0 {
		return BasisPointsScale
	}

	// Compute table index: k = ageHours * 10 / halfLifeHours (represents x * 10)
	k := uint64(ageHours) * 10 / uint64(halfLifeHours)
	tableLen := uint64(len(expDecayBps))
	if k >= tableLen {
		return 0
	}

	lo := uint64(expDecayBps[k])

	// Linear interpolation for sub-step precision.
	if k+1 < tableLen {
		hi := uint64(expDecayBps[k+1])
		// Fractional position within [k, k+1): remainder / halfLifeHours
		remainder := uint64(ageHours)*10 - k*uint64(halfLifeHours)
		// Interpolate: result = lo - (lo - hi) * remainder / halfLifeHours
		lo -= (lo - hi) * remainder / uint64(halfLifeHours)
	}

	return uint32(lo)
}

// calculateVulnerabilityScore calculates security score based on vulnerabilities.
// Critical vulnerabilities have 10x the weight of high vulnerabilities.
// Score = BPS / (1 + critical*10 + high)
func calculateVulnerabilityScore(critical, high uint32) uint32 {
	denom := uint64(1) + uint64(critical)*10 + uint64(high)
	return uint32(uint64(BasisPointsScale) / denom)
}
