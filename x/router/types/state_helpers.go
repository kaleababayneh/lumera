package types

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
)

const (
	defaultActiveSetLimit        = uint32(8)
	defaultCooldownSeconds       = uint32(3600)  // 1 hour hysteresis per README §5.3
	defaultSessionTTLSeconds     = uint32(86400) // 24 hours per docs/api/lumera-router-conformance.md
	defaultMetricsIntervalBlocks = uint32(100)
	defaultCacRoyaltyBps         = uint32(500)
	defaultMaxToolsPerCategory   = uint32(20)
	defaultProcessingCap         = uint32(100)
	defaultMaxRPS                = uint32(25)
	defaultBurst                 = uint32(50)
	defaultExplorationSessionBps = uint32(500)
	defaultExplorationTenantBps  = uint32(2_000)
	defaultQuorumMaxMembers      = uint32(0)
	defaultQuorumMinAgreement    = uint32(0)
	defaultQuorumToleranceBps    = uint32(0)

	maxActiveSetLimit         = uint32(100)
	maxCooldownSeconds        = uint32(7 * 86400)
	maxSessionTTLSeconds      = uint32(30 * 86400)
	maxMetricsIntervalBlocks  = uint32(10000)
	maxProcessingBounds       = uint32(10000)
	maxRateLimitRPS           = uint32(1_000_000)
	maxRateLimitBurst         = uint32(1_000_000)
	maxToolRateLimitOverrides = 1024
	maxQuorumMembers          = uint32(32)
)

//revive:disable

const decimalZeroString = "0"

func decimalToString(d decimal.Decimal) string {
	return d.String()
}

func decimalFromString(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	dec, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, err
	}
	// Exponent-bomb guard. shopspring parses "1e11100100" in O(1)
	// (symbolic exponent), but any downstream Mul/Div/Sub/Cmp/String
	// on the result forces big.Int alignment and expands to multi-
	// million-digit representations. This helper feeds every router
	// proto accessor (TotalVolume, SuccessRate, TotalSpent, Royalty*,
	// ReputationScore, ...) — one poisoned persisted field would
	// hang every read that does arithmetic on it. Treat absurd
	// exponents as parse errors — mustDecimal degrades to zero, the
	// Safe accessors surface the error. Same DoS class as 8438b6354,
	// 25d34d734 (cache.go), c1ec4b822, b21923578, 35a96822d.
	if !moneyguard.IsSafeExponent(dec) {
		return decimal.Zero, fmt.Errorf("decimal %q exponent out of safe range", s)
	}
	return dec, nil
}

// ParseDecimal is a panic-free helper that normalizes optional decimal strings into
// a decimal.Decimal value. Empty strings are treated as zero.
func ParseDecimal(s string) (decimal.Decimal, error) {
	return decimalFromString(s)
}

// mustDecimal parses a decimal string, returning zero on malformed input.
// Callers that need error handling should use decimalFromString or the *Safe() accessors.
func mustDecimal(s string) decimal.Decimal {
	dec, err := decimalFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return dec
}

// timestampFromTime is an identity helper retained at call sites; the proto
// timestamp fields are now gogoproto stdtime value time.Time.
func timestampFromTime(t time.Time) time.Time { return t }

// timeFromTimestamp returns a pointer to the stored time, or nil when unset
// (zero), preserving the optional-timestamp accessor contract.
func timeFromTimestamp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	tt := t
	return &tt
}

// unixNanoOrZero encodes a time as unix-nanoseconds for the int64 session maps
// (gogoproto cannot stdtime-annotate map values), with the zero time mapping
// to 0 (== unset).
func unixNanoOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// timeFromUnixNano is the inverse of unixNanoOrZero.
func timeFromUnixNano(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}

// DefaultParams returns router parameters with production defaults.
func DefaultParams() *Params {
	return &Params{
		ActiveSetLimit:               defaultActiveSetLimit,
		CooldownSeconds:              defaultCooldownSeconds,
		SessionTtlSeconds:            defaultSessionTTLSeconds,
		MetricsIntervalBlocks:        defaultMetricsIntervalBlocks,
		CacRoyaltyBps:                defaultCacRoyaltyBps,
		MaxToolsPerCategory:          defaultMaxToolsPerCategory,
		MinReputationScore:           decimal.RequireFromString("60").String(),
		MetricsEnabled:               true,
		CacEnabled:                   true,
		MaxToolsProcessedPerBlock:    defaultProcessingCap, // Phase 2.13.2.2 - prevent chain halt
		MaxSessionsProcessedPerBlock: defaultProcessingCap, // Phase 2.13.2.2 - prevent chain halt
		MaxRps:                       defaultMaxRPS,
		Burst:                        defaultBurst,
		AuthAudience:                 "",
		AuthIssuer:                   "",
		AllowedEndpoints:             nil,
		ExplorationSessionBudgetBps:  defaultExplorationSessionBps,
		ExplorationTenantBudgetBps:   defaultExplorationTenantBps,
		DiscoverySubsidyBps:          0,
		DiscoverySubsidyPoolCap:      decimalZeroString,
		DiscoverySubsidyPeriodBlocks: 0,
		TrustTieredQuotingEnabled:    false,
		TrustTieredMaxDriftBps:       0,
		QuorumExecutionEnabled:       false,
		QuorumMaxMembers:             defaultQuorumMaxMembers,
		QuorumMinRequiredAgreement:   defaultQuorumMinAgreement,
		QuorumNumericToleranceBps:    defaultQuorumToleranceBps,
	}
}

// Validate performs stateless validation for router parameters.
func (p *Params) Validate() error {
	if p == nil {
		return ErrInvalidParams.Wrap("params cannot be nil")
	}
	if p.ActiveSetLimit == 0 || p.ActiveSetLimit > maxActiveSetLimit {
		return ErrInvalidParams.Wrap("active_set_limit must be between 1 and 100")
	}
	if p.CooldownSeconds > maxCooldownSeconds {
		return ErrInvalidParams.Wrap("cooldown_seconds cannot exceed 7 days")
	}
	if p.SessionTtlSeconds == 0 || p.SessionTtlSeconds > maxSessionTTLSeconds {
		return ErrInvalidParams.Wrap("session_ttl_seconds must be between 1 and 30 days")
	}
	if p.CooldownSeconds > 0 && p.SessionTtlSeconds > 0 && p.CooldownSeconds > p.SessionTtlSeconds {
		return ErrInvalidParams.Wrap("cooldown_seconds cannot exceed session_ttl_seconds")
	}
	if p.MetricsIntervalBlocks == 0 || p.MetricsIntervalBlocks > maxMetricsIntervalBlocks {
		return ErrInvalidParams.Wrap("metrics_interval_blocks must be between 1 and 10000")
	}
	if p.CacRoyaltyBps > 10000 {
		return ErrInvalidParams.Wrap("cac_royalty_bps cannot exceed 10000 (100%)")
	}
	if p.MaxToolsPerCategory == 0 {
		return ErrInvalidParams.Wrap("max_tools_per_category must be at least 1")
	}
	if p.MaxToolsPerCategory < p.ActiveSetLimit {
		return ErrInvalidParams.Wrap("max_tools_per_category cannot be less than active_set_limit")
	}
	minRep, err := decimalFromString(p.MinReputationScore)
	if err != nil {
		return ErrInvalidParams.Wrapf("invalid min_reputation_score: %v", err)
	}
	if minRep.IsNegative() || minRep.GreaterThan(decimal.NewFromInt(100)) {
		return ErrInvalidParams.Wrap("min_reputation_score must be between 0 and 100")
	}
	// Phase 2.13.2.2 - validate bounds for chain halt prevention
	if p.MaxToolsProcessedPerBlock == 0 || p.MaxToolsProcessedPerBlock > maxProcessingBounds {
		return ErrInvalidParams.Wrap("max_tools_processed_per_block must be between 1 and 10000")
	}
	if p.MaxSessionsProcessedPerBlock == 0 || p.MaxSessionsProcessedPerBlock > maxProcessingBounds {
		return ErrInvalidParams.Wrap("max_sessions_processed_per_block must be between 1 and 10000")
	}

	// Rate limiting bounds (DDoS protection). 0 max_rps disables rate limiting.
	if p.MaxRps > maxRateLimitRPS {
		return ErrInvalidParams.Wrap("max_rps is out of bounds")
	}
	if p.Burst > maxRateLimitBurst {
		return ErrInvalidParams.Wrap("burst is out of bounds")
	}
	if p.MaxRps == 0 && p.Burst != 0 {
		return ErrInvalidParams.Wrap("burst must be 0 when max_rps is 0 (rate limiting disabled)")
	}
	if p.ExplorationSessionBudgetBps == 0 || p.ExplorationSessionBudgetBps > 10000 {
		return ErrInvalidParams.Wrap("exploration_session_budget_bps must be between 1 and 10000")
	}
	if p.ExplorationTenantBudgetBps == 0 || p.ExplorationTenantBudgetBps > 10000 {
		return ErrInvalidParams.Wrap("exploration_tenant_budget_bps must be between 1 and 10000")
	}
	if p.ExplorationTenantBudgetBps < p.ExplorationSessionBudgetBps {
		return ErrInvalidParams.Wrap("exploration_tenant_budget_bps cannot be less than exploration_session_budget_bps")
	}
	if p.DiscoverySubsidyBps > 10000 {
		return ErrInvalidParams.Wrap("discovery_subsidy_bps cannot exceed 10000 (100%)")
	}
	subsidyPoolCap, err := decimalFromString(p.GetDiscoverySubsidyPoolCap())
	if err != nil {
		return ErrInvalidParams.Wrapf("invalid discovery_subsidy_pool_cap: %v", err)
	}
	if subsidyPoolCap.IsNegative() {
		return ErrInvalidParams.Wrap("discovery_subsidy_pool_cap cannot be negative")
	}
	if p.DiscoverySubsidyBps > 0 && subsidyPoolCap.IsZero() {
		return ErrInvalidParams.Wrap("discovery_subsidy_pool_cap must be positive when discovery_subsidy_bps is enabled")
	}
	if p.DiscoverySubsidyBps == 0 && !subsidyPoolCap.IsZero() {
		return ErrInvalidParams.Wrap("discovery_subsidy_bps must be enabled when discovery_subsidy_pool_cap is positive")
	}
	if p.DiscoverySubsidyBps > 0 && p.DiscoverySubsidyPeriodBlocks == 0 {
		return ErrInvalidParams.Wrap("discovery_subsidy_period_blocks must be positive when discovery_subsidy_bps is enabled")
	}
	if p.DiscoverySubsidyBps == 0 && p.DiscoverySubsidyPeriodBlocks != 0 {
		return ErrInvalidParams.Wrap("discovery_subsidy_period_blocks must be 0 when discovery_subsidy_bps is disabled")
	}
	if p.TrustTieredMaxDriftBps > 10000 {
		return ErrInvalidParams.Wrap("trust_tiered_max_drift_bps cannot exceed 10000 (100%)")
	}
	if p.TrustTieredQuotingEnabled && p.TrustTieredMaxDriftBps == 0 {
		return ErrInvalidParams.Wrap("trust_tiered_max_drift_bps must be positive when trust_tiered_quoting_enabled is true")
	}
	if p.QuorumNumericToleranceBps > 10000 {
		return ErrInvalidParams.Wrap("quorum_numeric_tolerance_bps cannot exceed 10000 (100%)")
	}
	if p.QuorumMaxMembers == 0 {
		if p.QuorumExecutionEnabled {
			return ErrInvalidParams.Wrap("quorum_max_members must be at least 3 when quorum_execution_enabled is true")
		}
		if p.QuorumMinRequiredAgreement != 0 {
			return ErrInvalidParams.Wrap("quorum_max_members must be configured when quorum_min_required_agreement is positive")
		}
	} else {
		if p.QuorumMaxMembers < 3 || p.QuorumMaxMembers > maxQuorumMembers {
			return ErrInvalidParams.Wrap("quorum_max_members must be between 3 and 32")
		}
		if p.QuorumMinRequiredAgreement < 2 {
			return ErrInvalidParams.Wrap("quorum_min_required_agreement must be at least 2 when quorum_max_members is configured")
		}
		if p.QuorumMinRequiredAgreement > p.QuorumMaxMembers {
			return ErrInvalidParams.Wrap("quorum_min_required_agreement cannot exceed quorum_max_members")
		}
	}

	if len(p.ToolRateLimits) > maxToolRateLimitOverrides {
		return ErrInvalidParams.Wrap("tool_rate_limits exceeds maximum supported entries")
	}
	seen := make(map[string]struct{}, len(p.ToolRateLimits))
	for _, override := range p.ToolRateLimits {
		if override == nil {
			return ErrInvalidParams.Wrap("tool_rate_limits entries must not be nil")
		}
		toolID := strings.TrimSpace(override.ToolId)
		if toolID == "" {
			return ErrInvalidParams.Wrap("tool_rate_limits.tool_id is required")
		}
		if hasOuterWhitespace(override.ToolId) {
			return ErrInvalidParams.Wrap("tool_rate_limits.tool_id must not contain leading or trailing whitespace")
		}
		if _, ok := seen[toolID]; ok {
			return ErrInvalidParams.Wrap("tool_rate_limits contains duplicate tool_id entries")
		}
		seen[toolID] = struct{}{}
		if override.MaxRps == 0 || override.MaxRps > maxRateLimitRPS {
			return ErrInvalidParams.Wrap("tool_rate_limits.max_rps must be within bounds")
		}
		if override.Burst > maxRateLimitBurst {
			return ErrInvalidParams.Wrap("tool_rate_limits.burst must be within bounds")
		}
	}

	authAudience := strings.TrimSpace(p.GetAuthAudience())
	authIssuer := strings.TrimSpace(p.GetAuthIssuer())
	if hasOuterWhitespace(p.GetAuthAudience()) {
		return ErrInvalidParams.Wrap("auth_audience must not contain leading or trailing whitespace")
	}
	if hasOuterWhitespace(p.GetAuthIssuer()) {
		return ErrInvalidParams.Wrap("auth_issuer must not contain leading or trailing whitespace")
	}
	if authAudience != "" || authIssuer != "" {
		if authAudience == "" || authIssuer == "" {
			return ErrInvalidParams.Wrap("auth_audience and auth_issuer must be set together")
		}
		if len(authAudience) > 256 {
			return ErrInvalidParams.Wrap("auth_audience exceeds 256 characters")
		}
		if len(authIssuer) > 256 {
			return ErrInvalidParams.Wrap("auth_issuer exceeds 256 characters")
		}
	}

	if len(p.GetAllowedEndpoints()) > 0 {
		seenEndpoints := make(map[string]struct{}, len(p.GetAllowedEndpoints()))
		for _, endpoint := range p.GetAllowedEndpoints() {
			trimmed := strings.TrimSpace(endpoint)
			if trimmed == "" {
				return ErrInvalidParams.Wrap("allowed_endpoints entries must not be empty")
			}
			if hasOuterWhitespace(endpoint) {
				return ErrInvalidParams.Wrap("allowed_endpoints entries must not contain leading or trailing whitespace")
			}
			if hasWhitespaceOrControl(trimmed) {
				return ErrInvalidParams.Wrap("allowed_endpoints entries must not contain whitespace or control characters")
			}
			if _, ok := seenEndpoints[trimmed]; ok {
				return ErrInvalidParams.Wrap("allowed_endpoints contains duplicate entries")
			}
			if urlAuthorityContainsUserinfo(trimmed) {
				return ErrInvalidParams.Wrap("allowed_endpoints entries must not include userinfo")
			}
			parsed, err := url.Parse(trimmed)
			if err != nil || parsed == nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Hostname()) == "" {
				return ErrInvalidParams.Wrap("allowed_endpoints entries must be valid URLs")
			}
			if parsed.User != nil {
				return ErrInvalidParams.Wrap("allowed_endpoints entries must not include userinfo")
			}
			seenEndpoints[trimmed] = struct{}{}
		}
	}

	return nil
}

// DiscoverySubsidyRebate returns the LAC rebate allowed for an exploratory
// call under the governance subsidy params and the remaining pool cap.
func (p *Params) DiscoverySubsidyRebate(actualCost, subsidySpent decimal.Decimal, explorationPick bool) (decimal.Decimal, error) {
	if p == nil {
		return decimal.Zero, ErrInvalidParams.Wrap("params cannot be nil")
	}
	if err := p.Validate(); err != nil {
		return decimal.Zero, err
	}
	if !moneyguard.IsSafeExponent(actualCost) {
		return decimal.Zero, ErrInvalidParams.Wrap("actual_cost exponent out of safe range")
	}
	if !moneyguard.IsSafeExponent(subsidySpent) {
		return decimal.Zero, ErrInvalidParams.Wrap("discovery_subsidy_spent exponent out of safe range")
	}
	if actualCost.IsNegative() {
		return decimal.Zero, ErrInvalidParams.Wrap("actual_cost cannot be negative")
	}
	if subsidySpent.IsNegative() {
		return decimal.Zero, ErrInvalidParams.Wrap("discovery_subsidy_spent cannot be negative")
	}
	if !explorationPick || actualCost.IsZero() || p.GetDiscoverySubsidyBps() == 0 {
		return decimal.Zero, nil
	}

	poolCap, err := decimalFromString(p.GetDiscoverySubsidyPoolCap())
	if err != nil {
		return decimal.Zero, ErrInvalidParams.Wrapf("invalid discovery_subsidy_pool_cap: %v", err)
	}
	remaining := poolCap.Sub(subsidySpent)
	if !remaining.IsPositive() {
		return decimal.Zero, nil
	}

	rebate := actualCost.Mul(decimal.NewFromInt(int64(p.GetDiscoverySubsidyBps()))).Div(decimal.NewFromInt(10000))
	if rebate.GreaterThan(remaining) {
		return remaining, nil
	}
	return rebate, nil
}

func hasOuterWhitespace(value string) bool {
	return len(value) != len(strings.TrimSpace(value))
}

func hasWhitespaceOrControl(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
}

func urlAuthorityContainsUserinfo(raw string) bool {
	authorityStart := -1
	switch {
	case strings.HasPrefix(raw, "//"):
		authorityStart = len("//")
	case strings.Contains(raw, "://"):
		authorityStart = strings.Index(raw, "://") + len("://")
	default:
		return false
	}
	authority := raw[authorityStart:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	return strings.Contains(authority, "@")
}

// ToolRateLimitFor returns a per-tool rate limit override when configured.
func (p *Params) ToolRateLimitFor(toolID string) (maxRps uint32, burst uint32, ok bool) {
	if p == nil {
		return 0, 0, false
	}
	needle := strings.TrimSpace(toolID)
	if needle == "" {
		return 0, 0, false
	}
	for _, override := range p.ToolRateLimits {
		if override == nil {
			continue
		}
		if strings.TrimSpace(override.ToolId) == needle {
			return override.MaxRps, override.Burst, true
		}
	}
	return 0, 0, false
}

func (p *Params) MinReputationScoreDecimal() decimal.Decimal {
	return mustDecimal(p.GetMinReputationScore())
}

func (p *Params) SetMinReputationScoreDecimal(d decimal.Decimal) {
	p.MinReputationScore = decimalToString(d)
}

func NewActivationMetrics(toolID string) *ActivationMetrics {
	return &ActivationMetrics{
		ToolId:           toolID,
		ActiveSessions:   []string{},
		TotalVolume:      decimalZeroString,
		AverageLatencyMs: decimalZeroString,
		SuccessRate:      decimal.NewFromInt(100).String(),
	}
}

func (m *ActivationMetrics) TotalVolumeDecimal() decimal.Decimal {
	return mustDecimal(m.GetTotalVolume())
}

// TotalVolumeDecimalSafe mirrors TotalVolumeDecimal but returns an error instead of panicking.
func (m *ActivationMetrics) TotalVolumeDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetTotalVolume())
}

func (m *ActivationMetrics) SetTotalVolumeDecimal(d decimal.Decimal) {
	m.TotalVolume = decimalToString(d)
}

func (m *ActivationMetrics) AverageLatencyDecimal() decimal.Decimal {
	return mustDecimal(m.GetAverageLatencyMs())
}

// AverageLatencyDecimalSafe mirrors AverageLatencyDecimal but returns an error instead of panicking.
func (m *ActivationMetrics) AverageLatencyDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetAverageLatencyMs())
}

func (m *ActivationMetrics) SetAverageLatencyDecimal(d decimal.Decimal) {
	m.AverageLatencyMs = decimalToString(d)
}

func (m *ActivationMetrics) SuccessRateDecimal() decimal.Decimal {
	return mustDecimal(m.GetSuccessRate())
}

// SuccessRateDecimalSafe mirrors SuccessRateDecimal but returns an error instead of panicking.
func (m *ActivationMetrics) SuccessRateDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetSuccessRate())
}

func (m *ActivationMetrics) SetSuccessRateDecimal(d decimal.Decimal) {
	m.SuccessRate = decimalToString(d)
}

func (m *ActivationMetrics) SetLastActivated(t time.Time) {
	m.LastActivated = timestampFromTime(t)
}

func (m *ActivationMetrics) SetLastInvoked(t time.Time) {
	m.LastInvoked = timestampFromTime(t)
}

func (m *ActivationMetrics) LastActivatedTime() *time.Time {
	return timeFromTimestamp(m.GetLastActivated())
}

func (m *ActivationMetrics) LastInvokedTime() *time.Time {
	return timeFromTimestamp(m.GetLastInvoked())
}

func NewSessionMetrics(sessionID, userAddress string, start time.Time) *SessionMetrics {
	return &SessionMetrics{
		SessionId:        sessionID,
		UserAddress:      userAddress,
		ToolsUsed:        []string{},
		TotalSpent:       decimalZeroString,
		TotalRefunded:    decimalZeroString,
		StartedAt:        timestampFromTime(start),
		AverageLatencyMs: 0,
		CacheHitRate:     0,
	}
}

func (m *SessionMetrics) TotalSpentDecimal() decimal.Decimal {
	return mustDecimal(m.GetTotalSpent())
}

// TotalSpentDecimalSafe mirrors TotalSpentDecimal but returns an error instead of panicking.
func (m *SessionMetrics) TotalSpentDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetTotalSpent())
}

func (m *SessionMetrics) SetTotalSpentDecimal(d decimal.Decimal) {
	m.TotalSpent = decimalToString(d)
}

func (m *SessionMetrics) TotalRefundedDecimal() decimal.Decimal {
	return mustDecimal(m.GetTotalRefunded())
}

// TotalRefundedDecimalSafe mirrors TotalRefundedDecimal but returns an error instead of panicking.
func (m *SessionMetrics) TotalRefundedDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetTotalRefunded())
}

func (m *SessionMetrics) SetTotalRefundedDecimal(d decimal.Decimal) {
	m.TotalRefunded = decimalToString(d)
}

func (m *SessionMetrics) SetEndedAt(t time.Time) {
	m.EndedAt = timestampFromTime(t)
}

func (m *SessionMetrics) EndedAtTime() *time.Time {
	return timeFromTimestamp(m.GetEndedAt())
}

func NewCategoryMetrics(category string) *CategoryMetrics {
	return &CategoryMetrics{
		Category:    category,
		TopTools:    []string{},
		TotalVolume: decimalZeroString,
		AverageCost: decimalZeroString,
	}
}

func (m *CategoryMetrics) TotalVolumeDecimal() decimal.Decimal {
	return mustDecimal(m.GetTotalVolume())
}

// TotalVolumeDecimalSafe mirrors TotalVolumeDecimal but returns an error instead of panicking.
func (m *CategoryMetrics) TotalVolumeDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetTotalVolume())
}

func (m *CategoryMetrics) SetTotalVolumeDecimal(d decimal.Decimal) {
	m.TotalVolume = decimalToString(d)
}

func (m *CategoryMetrics) AverageCostDecimal() decimal.Decimal {
	return mustDecimal(m.GetAverageCost())
}

// AverageCostDecimalSafe mirrors AverageCostDecimal but returns an error instead of panicking.
func (m *CategoryMetrics) AverageCostDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetAverageCost())
}

func (m *CategoryMetrics) SetAverageCostDecimal(d decimal.Decimal) {
	m.AverageCost = decimalToString(d)
}

func (m *CategoryMetrics) SetLastUpdated(t time.Time) {
	m.LastUpdated = timestampFromTime(t)
}

func (m *CategoryMetrics) LastUpdatedTime() *time.Time {
	return timeFromTimestamp(m.GetLastUpdated())
}

func (m *GlobalMetrics) TotalVolumeDecimal() decimal.Decimal {
	return mustDecimal(m.GetTotalVolume())
}

// TotalVolumeDecimalSafe mirrors TotalVolumeDecimal but returns an error instead of panicking.
func (m *GlobalMetrics) TotalVolumeDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetTotalVolume())
}

func (m *GlobalMetrics) SetTotalVolumeDecimal(d decimal.Decimal) {
	m.TotalVolume = decimalToString(d)
}

func (m *GlobalMetrics) TotalInsuranceContributionsDecimal() decimal.Decimal {
	return mustDecimal(m.GetTotalInsuranceContributions())
}

// TotalInsuranceContributionsDecimalSafe mirrors TotalInsuranceContributionsDecimal but returns an error instead of panicking.
func (m *GlobalMetrics) TotalInsuranceContributionsDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetTotalInsuranceContributions())
}

func (m *GlobalMetrics) SetTotalInsuranceContributionsDecimal(d decimal.Decimal) {
	m.TotalInsuranceContributions = decimalToString(d)
}

func (m *GlobalMetrics) TotalCACRoyaltiesDecimal() decimal.Decimal {
	return mustDecimal(m.GetTotalCacRoyalties())
}

// TotalCACRoyaltiesDecimalSafe mirrors TotalCACRoyaltiesDecimal but returns an error instead of panicking.
func (m *GlobalMetrics) TotalCACRoyaltiesDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(m.GetTotalCacRoyalties())
}

func (m *GlobalMetrics) SetTotalCACRoyaltiesDecimal(d decimal.Decimal) {
	m.TotalCacRoyalties = decimalToString(d)
}

func (m *GlobalMetrics) SetMetricsStart(t time.Time) {
	m.MetricsStart = timestampFromTime(t)
}

func (m *GlobalMetrics) SetLastUpdated(t time.Time) {
	m.LastUpdated = timestampFromTime(t)
}

func (m *GlobalMetrics) MetricsStartTime() *time.Time {
	return timeFromTimestamp(m.GetMetricsStart())
}

func (m *GlobalMetrics) LastUpdatedTime() *time.Time {
	return timeFromTimestamp(m.GetLastUpdated())
}

func (r *CACRoyaltyRecord) RoyaltyAmountDecimal() decimal.Decimal {
	return mustDecimal(r.GetRoyaltyAmount())
}

// RoyaltyAmountDecimalSafe mirrors RoyaltyAmountDecimal but returns an error instead of panicking.
func (r *CACRoyaltyRecord) RoyaltyAmountDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(r.GetRoyaltyAmount())
}

func (r *CACRoyaltyRecord) SetRoyaltyAmountDecimal(d decimal.Decimal) {
	r.RoyaltyAmount = decimalToString(d)
}

func (r *CACRoyaltyRecord) TotalRoyaltiesEarnedDecimal() decimal.Decimal {
	return mustDecimal(r.GetTotalRoyaltiesEarned())
}

// TotalRoyaltiesEarnedDecimalSafe mirrors TotalRoyaltiesEarnedDecimal but returns an error instead of panicking.
func (r *CACRoyaltyRecord) TotalRoyaltiesEarnedDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(r.GetTotalRoyaltiesEarned())
}

func (r *CACRoyaltyRecord) SetTotalRoyaltiesEarnedDecimal(d decimal.Decimal) {
	r.TotalRoyaltiesEarned = decimalToString(d)
}

func (r *CACRoyaltyRecord) SetTimestamp(t time.Time) {
	r.Timestamp = timestampFromTime(t)
}

func (r *CACRoyaltyRecord) TimestampTime() *time.Time {
	return timeFromTimestamp(r.GetTimestamp())
}

func (u *PolicyUpdate) SetUpdatedAt(t time.Time) {
	u.UpdatedAt = timestampFromTime(t)
}

func (u *PolicyUpdate) UpdatedAtTime() *time.Time {
	return timeFromTimestamp(u.GetUpdatedAt())
}

// NewToolSelectionScore returns a zero-initialised ToolSelectionScore for the given tool.
func NewToolSelectionScore(toolID string) *ToolSelectionScore {
	return &ToolSelectionScore{
		ToolId:              toolID,
		ReputationScore:     decimalZeroString,
		PerformanceScore:    decimalZeroString,
		ReliabilityScore:    decimalZeroString,
		CostEfficiencyScore: decimalZeroString,
		OverallScore:        decimalZeroString,
	}
}

func (s *ToolSelectionScore) ReputationScoreDecimal() decimal.Decimal {
	return mustDecimal(s.GetReputationScore())
}

// ReputationScoreDecimalSafe mirrors ReputationScoreDecimal but returns an error instead of panicking.
func (s *ToolSelectionScore) ReputationScoreDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(s.GetReputationScore())
}

func (s *ToolSelectionScore) SetReputationScoreDecimal(d decimal.Decimal) {
	s.ReputationScore = decimalToString(d)
}

func (s *ToolSelectionScore) PerformanceScoreDecimal() decimal.Decimal {
	return mustDecimal(s.GetPerformanceScore())
}

// PerformanceScoreDecimalSafe mirrors PerformanceScoreDecimal but returns an error instead of panicking.
func (s *ToolSelectionScore) PerformanceScoreDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(s.GetPerformanceScore())
}

func (s *ToolSelectionScore) SetPerformanceScoreDecimal(d decimal.Decimal) {
	s.PerformanceScore = decimalToString(d)
}

func (s *ToolSelectionScore) ReliabilityScoreDecimal() decimal.Decimal {
	return mustDecimal(s.GetReliabilityScore())
}

// ReliabilityScoreDecimalSafe mirrors ReliabilityScoreDecimal but returns an error instead of panicking.
func (s *ToolSelectionScore) ReliabilityScoreDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(s.GetReliabilityScore())
}

func (s *ToolSelectionScore) SetReliabilityScoreDecimal(d decimal.Decimal) {
	s.ReliabilityScore = decimalToString(d)
}

func (s *ToolSelectionScore) CostEfficiencyScoreDecimal() decimal.Decimal {
	return mustDecimal(s.GetCostEfficiencyScore())
}

// CostEfficiencyScoreDecimalSafe mirrors CostEfficiencyScoreDecimal but returns an error instead of panicking.
func (s *ToolSelectionScore) CostEfficiencyScoreDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(s.GetCostEfficiencyScore())
}

func (s *ToolSelectionScore) SetCostEfficiencyScoreDecimal(d decimal.Decimal) {
	s.CostEfficiencyScore = decimalToString(d)
}

func (s *ToolSelectionScore) OverallScoreDecimal() decimal.Decimal {
	return mustDecimal(s.GetOverallScore())
}

// OverallScoreDecimalSafe mirrors OverallScoreDecimal but returns an error instead of panicking.
func (s *ToolSelectionScore) OverallScoreDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(s.GetOverallScore())
}

func (s *ToolSelectionScore) SetOverallScoreDecimal(d decimal.Decimal) {
	s.OverallScore = decimalToString(d)
}

func (s *ToolSelectionScore) SetLastCalculated(t time.Time) {
	s.LastCalculated = timestampFromTime(t)
}

func (s *ToolSelectionScore) LastCalculatedTime() *time.Time {
	return timeFromTimestamp(s.GetLastCalculated())
}

// DefaultGenesis returns a canonical router genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:          *DefaultParams(),
		State:           nil,
		ToolMetricsList: []*ActivationMetrics{},
		CacRecords:      []*CACRoyaltyRecord{},
		SessionList:     []*SessionMetrics{},
	}
}

// DefaultGenesisState is retained for backwards compatibility with callers referencing the previous helper name.
func DefaultGenesisState() *GenesisState {
	return DefaultGenesis()
}

// Validate ensures the genesis state is internally consistent.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return ErrInvalidParams.Wrap("genesis state is nil")
	}
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	toolIDs := make(map[string]bool)
	for _, metrics := range gs.ToolMetricsList {
		if metrics == nil {
			return ErrInvalidMetrics.Wrap("nil tool metrics entry")
		}
		if metrics.GetToolId() == "" {
			return ErrInvalidMetrics.Wrap("tool metrics missing tool_id")
		}
		if toolIDs[metrics.GetToolId()] {
			return ErrDuplicateEntry.Wrapf("duplicate tool metrics for %s", metrics.GetToolId())
		}
		toolIDs[metrics.GetToolId()] = true
		success, err := decimalFromString(metrics.GetSuccessRate())
		if err != nil {
			return ErrInvalidMetrics.Wrapf("invalid success rate format for tool %s: %v", metrics.GetToolId(), err)
		}
		if success.IsNegative() || success.GreaterThan(decimal.NewFromInt(100)) {
			return ErrInvalidMetrics.Wrapf("invalid success rate for tool %s", metrics.GetToolId())
		}
	}

	for _, record := range gs.CacRecords {
		if record == nil {
			return ErrInvalidMetrics.Wrap("nil CAC record")
		}
		royalty, err := record.RoyaltyAmountDecimalSafe()
		if err != nil {
			return ErrInvalidMetrics.Wrapf("invalid royalty amount: %v", err)
		}
		if royalty.IsNegative() {
			return ErrInvalidAmount.Wrap("negative royalty amount")
		}
		total, err := record.TotalRoyaltiesEarnedDecimalSafe()
		if err != nil {
			return ErrInvalidMetrics.Wrapf("invalid total royalties earned: %v", err)
		}
		if total.IsNegative() {
			return ErrInvalidAmount.Wrap("negative total royalties")
		}
	}

	sessionIDs := make(map[string]bool)
	for _, session := range gs.SessionList {
		if session == nil {
			return ErrInvalidMetrics.Wrap("nil session metrics entry")
		}
		if session.GetSessionId() == "" {
			return ErrInvalidMetrics.Wrap("session metrics missing session_id")
		}
		if sessionIDs[session.GetSessionId()] {
			return ErrDuplicateEntry.Wrapf("duplicate session %s", session.GetSessionId())
		}
		sessionIDs[session.GetSessionId()] = true

		totalSpent, err := session.TotalSpentDecimalSafe()
		if err != nil {
			return ErrInvalidMetrics.Wrapf("invalid total spent for session %s: %v", session.GetSessionId(), err)
		}
		if totalSpent.IsNegative() {
			return ErrInvalidAmount.Wrap("negative total spent")
		}
		if session.GetCacheHitRate() > 100 {
			return ErrInvalidMetrics.Wrap("cache hit rate exceeds 100%")
		}
	}

	return nil
}

// NewToolActivation constructs a proto ToolActivation with the given fields.
func NewToolActivation(id, toolID, sessionID, reason string, active bool, timestamp time.Time) *ToolActivation {
	return &ToolActivation{
		Id:        id,
		ToolId:    toolID,
		SessionId: sessionID,
		Active:    active,
		Timestamp: timestampFromTime(timestamp),
		Reason:    reason,
	}
}

// TimestampTime returns the activation timestamp as a time.Time.
func (a *ToolActivation) TimestampTime() time.Time {
	if ts := a.GetTimestamp(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

// SetTimestampTime sets the activation timestamp.
func (a *ToolActivation) SetTimestampTime(t time.Time) {
	a.Timestamp = timestampFromTime(t)
}

// EnsureRouterState returns an initialised RouterState with non-nil maps.
func EnsureRouterState(state *RouterState) *RouterState {
	if state == nil {
		state = &RouterState{}
	}
	if state.ToolMetrics == nil {
		state.ToolMetrics = make(map[string]*ActivationMetrics)
	}
	if state.SessionMetrics == nil {
		state.SessionMetrics = make(map[string]*SessionMetrics)
	}
	if state.CategoryMetrics == nil {
		state.CategoryMetrics = make(map[string]*CategoryMetrics)
	}
	if state.SelectionScores == nil {
		state.SelectionScores = make(map[string]*ToolSelectionScore)
	}
	if state.RecentPolicyUpdates == nil {
		state.RecentPolicyUpdates = []*PolicyUpdate{}
	}
	return state
}

// MustDecimalFromString parses a decimal string, returning zero on malformed input.
// Callers that need error handling should use ParseDecimal instead.
func MustDecimalFromString(s string) decimal.Decimal {
	dec, err := decimalFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return dec
}

// ---------------------------------------------------------------------------
// CacheEntry helpers
// ---------------------------------------------------------------------------

func NewCacheEntry(cacheKey, toolID string) *CacheEntry {
	return &CacheEntry{
		CacheKey: cacheKey,
		ToolId:   toolID,
	}
}

func (e *CacheEntry) OriginalCostDecimal() decimal.Decimal {
	return mustDecimal(e.GetOriginalCost())
}

func (e *CacheEntry) OriginalCostDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(e.GetOriginalCost())
}

func (e *CacheEntry) SetOriginalCostDecimal(d decimal.Decimal) {
	e.OriginalCost = decimalToString(d)
}

func (e *CacheEntry) CreatedAtTime() time.Time {
	if ts := e.GetCreatedAt(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

func (e *CacheEntry) SetCreatedAt(t time.Time) {
	e.CreatedAt = timestampFromTime(t)
}

func (e *CacheEntry) ExpiresAtTime() time.Time {
	if ts := e.GetExpiresAt(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

func (e *CacheEntry) SetExpiresAt(t time.Time) {
	e.ExpiresAt = timestampFromTime(t)
}

func (e *CacheEntry) LastHitAtTime() time.Time {
	if ts := e.GetLastHitAt(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

func (e *CacheEntry) SetLastHitAt(t time.Time) {
	e.LastHitAt = timestampFromTime(t)
}

// ---------------------------------------------------------------------------
// QuoteRecord helpers
// ---------------------------------------------------------------------------

func NewQuoteRecord(quoteID, toolID, sessionID string) *QuoteRecord {
	return &QuoteRecord{
		QuoteId:   quoteID,
		ToolId:    toolID,
		SessionId: sessionID,
	}
}

func (q *QuoteRecord) EstCostDecimal() decimal.Decimal {
	return mustDecimal(q.GetEstCost())
}

func (q *QuoteRecord) EstCostDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(q.GetEstCost())
}

func (q *QuoteRecord) SetEstCostDecimal(d decimal.Decimal) {
	q.EstCost = decimalToString(d)
}

func (q *QuoteRecord) MaxCostDecimal() decimal.Decimal {
	return mustDecimal(q.GetMaxCost())
}

func (q *QuoteRecord) MaxCostDecimalSafe() (decimal.Decimal, error) {
	return decimalFromString(q.GetMaxCost())
}

func (q *QuoteRecord) SetMaxCostDecimal(d decimal.Decimal) {
	q.MaxCost = decimalToString(d)
}

func (q *QuoteRecord) ValidUntilTime() time.Time {
	if ts := q.GetValidUntil(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

func (q *QuoteRecord) SetValidUntil(t time.Time) {
	q.ValidUntil = timestampFromTime(t)
}

func (q *QuoteRecord) CreatedAtTime() time.Time {
	if ts := q.GetCreatedAt(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

func (q *QuoteRecord) SetCreatedAt(t time.Time) {
	q.CreatedAt = timestampFromTime(t)
}

// LockedCoin reconstructs an sdk.Coin from the split denom/amount string fields.
func (q *QuoteRecord) LockedCoin() sdk.Coin {
	if q.LockedDenom == "" {
		return sdk.Coin{}
	}
	amt, ok := sdkmath.NewIntFromString(q.LockedAmount)
	if !ok {
		return sdk.NewCoin(q.LockedDenom, sdkmath.ZeroInt())
	}
	return sdk.NewCoin(q.LockedDenom, amt)
}

// SetLockedCoin stores an sdk.Coin as separate denom/amount string fields.
func (q *QuoteRecord) SetLockedCoin(c sdk.Coin) {
	q.LockedDenom = c.Denom
	q.LockedAmount = c.Amount.String()
}

// ---------------------------------------------------------------------------
// SessionState helpers
// ---------------------------------------------------------------------------

func NewSessionState(sessionID, userAddr, policyVersion string, blockTime time.Time) *SessionState {
	if strings.TrimSpace(policyVersion) == "" {
		policyVersion = "v1"
	}
	return &SessionState{
		SessionId:      sessionID,
		UserAddr:       userAddr,
		ActiveTools:    []string{},
		ActivatedAt:    make(map[string]int64),
		DeactivatedAt:  make(map[string]int64),
		CooldownUntil:  make(map[string]int64),
		CreatedAt:      timestampFromTime(blockTime),
		LastAccessedAt: timestampFromTime(blockTime),
		PolicyVersion:  policyVersion,
	}
}

func (s *SessionState) CreatedAtTime() time.Time {
	if ts := s.GetCreatedAt(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

func (s *SessionState) SetCreatedAt(t time.Time) {
	s.CreatedAt = timestampFromTime(t)
}

func (s *SessionState) LastAccessedAtTime() time.Time {
	if ts := s.GetLastAccessedAt(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

func (s *SessionState) SetLastAccessedAt(t time.Time) {
	s.LastAccessedAt = timestampFromTime(t)
}

func (s *SessionState) ActivatedAtTime(toolID string) time.Time {
	if s.ActivatedAt == nil {
		return time.Time{}
	}
	if ns, ok := s.ActivatedAt[toolID]; ok {
		return timeFromUnixNano(ns)
	}
	return time.Time{}
}

func (s *SessionState) SetActivatedAtTime(toolID string, t time.Time) {
	if s.ActivatedAt == nil {
		s.ActivatedAt = make(map[string]int64)
	}
	s.ActivatedAt[toolID] = unixNanoOrZero(t)
}

func (s *SessionState) DeactivatedAtTime(toolID string) time.Time {
	if s.DeactivatedAt == nil {
		return time.Time{}
	}
	if ns, ok := s.DeactivatedAt[toolID]; ok {
		return timeFromUnixNano(ns)
	}
	return time.Time{}
}

func (s *SessionState) SetDeactivatedAtTime(toolID string, t time.Time) {
	if s.DeactivatedAt == nil {
		s.DeactivatedAt = make(map[string]int64)
	}
	s.DeactivatedAt[toolID] = unixNanoOrZero(t)
}

func (s *SessionState) CooldownUntilTime(toolID string) (time.Time, bool) {
	if s.CooldownUntil == nil {
		return time.Time{}, false
	}
	ns, ok := s.CooldownUntil[toolID]
	if !ok || ns == 0 {
		return time.Time{}, false
	}
	return timeFromUnixNano(ns), true
}

func (s *SessionState) SetCooldownUntilTime(toolID string, t time.Time) {
	if s.CooldownUntil == nil {
		s.CooldownUntil = make(map[string]int64)
	}
	s.CooldownUntil[toolID] = unixNanoOrZero(t)
}

func (s *SessionState) DeleteCooldown(toolID string) {
	if s.CooldownUntil != nil {
		delete(s.CooldownUntil, toolID)
	}
}

// ---------------------------------------------------------------------------
// NonceRecord helpers
// ---------------------------------------------------------------------------

// NewNonceRecord returns a NonceRecord with the given nonce, tool, and session.
func NewNonceRecord(nonce, toolID, sessionID string) *NonceRecord {
	return &NonceRecord{
		Nonce:     nonce,
		ToolId:    toolID,
		SessionId: sessionID,
	}
}

func (n *NonceRecord) SetTimestamp(t time.Time) {
	n.Timestamp = timestampFromTime(t)
}

func (n *NonceRecord) TimestampTime() time.Time {
	if ts := n.GetTimestamp(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

func (n *NonceRecord) SetFirstSeen(t time.Time) {
	n.FirstSeen = timestampFromTime(t)
}

func (n *NonceRecord) FirstSeenTime() time.Time {
	if ts := n.GetFirstSeen(); !ts.IsZero() {
		return ts
	}
	return time.Time{}
}

// EnsureMaps initialises nil maps so callers can write to them safely.
func (s *SessionState) EnsureMaps() {
	if s.ActivatedAt == nil {
		s.ActivatedAt = make(map[string]int64)
	}
	if s.DeactivatedAt == nil {
		s.DeactivatedAt = make(map[string]int64)
	}
	if s.CooldownUntil == nil {
		s.CooldownUntil = make(map[string]int64)
	}
}

//revive:enable
