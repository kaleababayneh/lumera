//go:build cosmos

// Package types holds shared types and helpers for the registry module.
//
//revive:disable:var-naming // Cosmos module conventions use the `types` package name.
package types

import (
	"fmt"
	"time"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/shopspring/decimal"
)

// SecurityParams captures registry security-related tuning knobs managed via governance.
type SecurityParams struct {
	AttestationTtlSeconds uint32 //nolint:staticcheck // ST1003: name matches protobuf field convention
	SbomStalenessSeconds  uint32
}

const (
	// DefaultMaxSettlementsPerBlock defines the default number of settlements processed per block.
	DefaultMaxSettlementsPerBlock = 100
	// DefaultMaxReceiptsScanPerBlock defines the default number of receipts scanned per block.
	DefaultMaxReceiptsScanPerBlock = 250
	// DefaultRecursiveRoyaltyMaxDepth bounds sub-tool royalty lineage traversal.
	DefaultRecursiveRoyaltyMaxDepth = 5
	// DefaultRecursiveRoyaltyMaxAggregateBps caps total recursive royalty at 20% of publisher share.
	DefaultRecursiveRoyaltyMaxAggregateBps = 2000
	// DefaultDisputeWindowSeconds defines the canonical registry dispute window.
	DefaultDisputeWindowSeconds      = 600
	maxRecursiveRoyaltyDepth         = 32
	maxSafeDurationSeconds           = int64(1<<63-1) / int64(time.Second)
	maxRegistryParamsDurationSeconds = int64(^uint32(0))
	settlementBlockSeconds           = int64(6)
	maxSafeSettlementPeriodBlocks    = maxSafeDurationSeconds / settlementBlockSeconds
)

// DefaultVerifiedBadgeParams returns the canonical verified badge scoring model.
func DefaultVerifiedBadgeParams() *VerifiedBadgeParams {
	return &VerifiedBadgeParams{
		SbomWeightBps:      2500,
		BondWeightBps:      2500,
		ChallengeWeightBps: 2500,
		UptimeWeightBps:    2500,

		BronzeCompositeBps: 6000,
		SilverCompositeBps: 7500,
		GoldCompositeBps:   8500,

		SbomBronzeBps:      5000,
		SbomSilverBps:      7500,
		SbomGoldBps:        10000,
		BondBronzeBps:      10000,
		BondSilverBps:      12500,
		BondGoldBps:        15000,
		ChallengeBronzeBps: 9000,
		ChallengeSilverBps: 9500,
		ChallengeGoldBps:   9900,
		UptimeBronzeBps:    9500,
		UptimeSilverBps:    9750,
		UptimeGoldBps:      9900,
	}
}

// DefaultParams returns the canonical default parameter set for the registry module.
func DefaultParams() *Params {
	return &Params{
		MinBondAmount:           "1000000",
		MaxActiveTools:          100,
		InsuranceBps:            300,
		CacRoyaltyBps:           100,
		QuoteTtlSeconds:         600,
		SettlementPeriodBlocks:  14400,
		MaxToolsPerCategory:     20,
		MinReputation:           0.5,
		SlashingGracePeriod:     100,
		MaxJurisdictions:        10,
		DisputeWindowSeconds:    DefaultDisputeWindowSeconds,
		BurnRateSpendBps:        300,
		BurnRateAcqBps:          100,
		QualityRebateBps:        50,
		InsuranceTargetUtil:     "0.3",
		PremiumAdjustmentBps:    25,
		RevenueSplits:           &RevenueSplits{PublisherShareBps: 7000, RouterShareBps: 2000, ReferrerShareBps: 1000},
		CacheFeeSplit:           &CacheFeeSplit{Origin: "0.60", Serving: "0.35", Router: "0.04", Burn: "0.01"},
		MaxSettlementsPerBlock:  DefaultMaxSettlementsPerBlock,
		MaxReceiptsScanPerBlock: DefaultMaxReceiptsScanPerBlock,

		// SLA slashing defaults.
		SlaSlashGammaBps:           1000,  // 10% of trailing receipts
		SlaP95LatencyConsecutive:   5,     // sustained p95 breaches before slashing
		SlaDisputeRateBps:          200,   // 2% disputes
		SlaMinCalls:                50,    // avoid slashing on tiny sample sizes
		SlaMaxSlashPerEpochBps:     3000,  // 30% max per evaluation epoch
		SlaRecidivistMultiplierBps: 15000, // 1.5x multiplier once previously slashed

		// Challenge resolution deadline defaults.
		ChallengeResolutionDeadlineSeconds: 86400, // 24 hours

		// Settled receipt retention defaults.
		SettledReceiptRetentionSeconds: 86400, // 24 hours

		// Verified badge scoring defaults.
		VerifiedBadge: DefaultVerifiedBadgeParams(),

		// Recursive royalty governance defaults.
		RecursiveRoyaltyMaxDepth:        DefaultRecursiveRoyaltyMaxDepth,
		RecursiveRoyaltyMaxAggregateBps: DefaultRecursiveRoyaltyMaxAggregateBps,
	}
}

// Validate performs basic sanity checks on the parameter set.
func (p *Params) Validate() error {
	if p == nil {
		return fmt.Errorf("params cannot be nil")
	}
	minBond, err := decimal.NewFromString(p.MinBondAmount)
	if err != nil {
		return fmt.Errorf("invalid min bond amount: %w", err)
	}
	// Defense-in-depth: MinBondAmount flows into bond/slash arithmetic
	// elsewhere (GreaterThan, Sub, Mul on slashing paths). Reject
	// symbolic exponents at param-validation time.
	if !moneyguard.IsSafeExponent(minBond) {
		return fmt.Errorf("min bond amount magnitude out of range")
	}
	if minBond.IsNegative() {
		return fmt.Errorf("min bond amount cannot be negative")
	}
	if p.MaxActiveTools <= 0 {
		return fmt.Errorf("max active tools must be positive")
	}
	if p.InsuranceBps < 0 || p.InsuranceBps > 10000 {
		return fmt.Errorf("insurance BPS must be between 0 and 10000")
	}
	if p.CacRoyaltyBps < 0 || p.CacRoyaltyBps > 10000 {
		return fmt.Errorf("CAC royalty BPS must be between 0 and 10000")
	}
	if p.QuoteTtlSeconds <= 0 {
		return fmt.Errorf("quote TTL must be positive")
	}
	if err := validatePositiveSettlementPeriodBlocks("settlement period", p.SettlementPeriodBlocks); err != nil {
		return err
	}
	if p.MaxToolsPerCategory <= 0 {
		return fmt.Errorf("max tools per category must be positive")
	}
	if p.MinReputation < 0 || p.MinReputation > 1 {
		return fmt.Errorf("min reputation must be between 0 and 1")
	}
	if err := validatePositiveDurationSeconds("slashing grace period", p.SlashingGracePeriod); err != nil {
		return err
	}
	if p.MaxJurisdictions <= 0 {
		return fmt.Errorf("max jurisdictions must be positive")
	}
	if err := validateRegistryParamsDurationSeconds("dispute window", p.DisputeWindowSeconds); err != nil {
		return err
	}
	if p.BurnRateSpendBps > 10000 || p.BurnRateAcqBps > 10000 {
		return fmt.Errorf("burn rates must be <= 10000 bps")
	}
	if p.QualityRebateBps > 10000 {
		return fmt.Errorf("quality rebate must be <= 10000 bps")
	}
	if p.PremiumAdjustmentBps < 0 {
		return fmt.Errorf("premium adjustment must be non-negative")
	}
	if err := validateRevenueSplits(p.RevenueSplits); err != nil {
		return err
	}
	if err := validateCacheSplit(p.CacheFeeSplit); err != nil {
		return err
	}
	insTarget, err := decimal.NewFromString(p.InsuranceTargetUtil)
	if err != nil {
		return fmt.Errorf("invalid insurance target util: %w", err)
	}
	// Consensus-halt guard: params are set via governance (MsgUpdateParams)
	// which runs on every validator. A symbolic exponent here would expand
	// the big.Int on the GreaterThan comparison below.
	if !moneyguard.IsSafeExponent(insTarget) {
		return fmt.Errorf("insurance target utilization magnitude out of range")
	}
	if insTarget.IsNegative() || insTarget.GreaterThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("insurance target utilization must be between 0 and 1")
	}
	if p.MaxSettlementsPerBlock == 0 {
		return fmt.Errorf("max settlements per block must be > 0")
	}
	if p.MaxReceiptsScanPerBlock == 0 {
		return fmt.Errorf("max receipts scan per block must be > 0")
	}
	if p.SlaSlashGammaBps > 10000 {
		return fmt.Errorf("sla slash gamma bps must be <= 10000")
	}
	if p.SlaDisputeRateBps > 10000 {
		return fmt.Errorf("sla dispute rate bps must be <= 10000")
	}
	if p.SlaMaxSlashPerEpochBps > 10000 {
		return fmt.Errorf("sla max slash per epoch bps must be <= 10000")
	}
	if p.SlaP95LatencyConsecutive > 1000 {
		return fmt.Errorf("sla p95 latency consecutive threshold must be <= 1000")
	}
	if p.SlaMinCalls > 10_000_000 {
		return fmt.Errorf("sla min calls out of range: %d", p.SlaMinCalls)
	}
	if p.SlaRecidivistMultiplierBps != 0 && p.SlaRecidivistMultiplierBps < 10_000 {
		return fmt.Errorf("sla recidivist multiplier bps must be >= 10000 or 0 to disable")
	}
	if p.SlaRecidivistMultiplierBps > 50_000 {
		return fmt.Errorf("sla recidivist multiplier bps must be <= 50000")
	}
	if err := validateRegistryParamsDurationSeconds("challenge resolution deadline", p.ChallengeResolutionDeadlineSeconds); err != nil {
		return err
	}
	if err := validateRegistryParamsDurationSeconds("settled receipt retention", p.SettledReceiptRetentionSeconds); err != nil {
		return err
	}
	if err := validateVerifiedBadgeParams(p.VerifiedBadge); err != nil {
		return fmt.Errorf("invalid verified badge params: %w", err)
	}
	if err := validateRecursiveRoyaltyMaxDepthValue(p.RecursiveRoyaltyMaxDepth); err != nil {
		return err
	}
	if err := validateRecursiveRoyaltyMaxAggregateBpsValue(p.RecursiveRoyaltyMaxAggregateBps); err != nil {
		return err
	}
	return nil
}

func validateRevenueSplits(s *RevenueSplits) error {
	if s == nil {
		return fmt.Errorf("revenue splits cannot be nil")
	}
	total := s.PublisherShareBps + s.RouterShareBps + s.ReferrerShareBps
	if total != BPSDenominator {
		return fmt.Errorf("revenue splits must sum to %d bps, got %d", BPSDenominator, total)
	}
	if s.OriginShareBps > 5000 {
		return fmt.Errorf("origin share bps must be <= 5000, got %d", s.OriginShareBps)
	}
	return nil
}

func validateCacheSplit(split *CacheFeeSplit) error {
	if split == nil {
		return fmt.Errorf("cache fee split cannot be nil")
	}
	// Consensus-halt guard: validateCacheSplit runs inside Params.Validate
	// on the MsgUpdateParams governance path — every validator executes
	// this on every block that contains a param update. The chained
	// origin.Add(serving).Add(router).Add(burn) below, plus the
	// .Sub(DecimalOne).Abs().GreaterThan(DecimalTolerance) check, would
	// force shopspring to expand a symbolic exponent (e.g. "1e11100100")
	// to a multi-million-digit big.Int and hang block production on
	// every validator. Reject at parse time so the vulnerable chain
	// downstream never sees a bomb. Same DoS class as sibling guards:
	// 8438b6354, 25d34d734, 5c237b056, cbbaba3cb, c1ec4b822, b21923578.
	origin, err := decimal.NewFromString(split.Origin)
	if err != nil {
		return fmt.Errorf("invalid origin: %w", err)
	}
	if !moneyguard.IsSafeExponent(origin) {
		return fmt.Errorf("cache split origin magnitude out of range")
	}
	serving, err := decimal.NewFromString(split.Serving)
	if err != nil {
		return fmt.Errorf("invalid serving: %w", err)
	}
	if !moneyguard.IsSafeExponent(serving) {
		return fmt.Errorf("cache split serving magnitude out of range")
	}
	router, err := decimal.NewFromString(split.Router)
	if err != nil {
		return fmt.Errorf("invalid router: %w", err)
	}
	if !moneyguard.IsSafeExponent(router) {
		return fmt.Errorf("cache split router magnitude out of range")
	}
	burn, err := decimal.NewFromString(split.Burn)
	if err != nil {
		return fmt.Errorf("invalid burn: %w", err)
	}
	if !moneyguard.IsSafeExponent(burn) {
		return fmt.Errorf("cache split burn magnitude out of range")
	}

	total := origin.Add(serving).Add(router).Add(burn)
	if total.Sub(DecimalOne).Abs().GreaterThan(DecimalTolerance) {
		return fmt.Errorf("cache royalty split must sum to 1.0, got %s", total.String())
	}
	if origin.IsNegative() || serving.IsNegative() || router.IsNegative() || burn.IsNegative() {
		return fmt.Errorf("cache royalty split cannot contain negative values")
	}
	return nil
}

// ParamKeyTable returns the parameter key table for the registry module
func ParamKeyTable() KeyTable {
	return NewKeyTable().
		RegisterParamSet(&Params{}).
		RegisterParamSet(&SecurityParams{})
}

// ParamSetPairs returns the parameter set pairs for the registry module
func (p *Params) ParamSetPairs() ParamSetPairs {
	if p == nil {
		return ParamSetPairs{}
	}
	return ParamSetPairs{
		ParamSetPair{Key: KeyMinBondAmount, Value: &p.MinBondAmount, ValidatorFn: validateMinBondAmount},
		ParamSetPair{Key: KeyMaxActiveTools, Value: &p.MaxActiveTools, ValidatorFn: validateMaxActiveTools},
		ParamSetPair{Key: KeyInsuranceBPS, Value: &p.InsuranceBps, ValidatorFn: validateBPS},
		ParamSetPair{Key: KeyCACRoyaltyBPS, Value: &p.CacRoyaltyBps, ValidatorFn: validateBPS},
		ParamSetPair{Key: KeyQuoteTTLSeconds, Value: &p.QuoteTtlSeconds, ValidatorFn: validatePositiveInt32},
		ParamSetPair{Key: KeySettlementPeriodBlocks, Value: &p.SettlementPeriodBlocks, ValidatorFn: validatePositiveSettlementPeriodBlocksInt64},
		ParamSetPair{Key: KeyMaxToolsPerCategory, Value: &p.MaxToolsPerCategory, ValidatorFn: validatePositiveInt32},
		ParamSetPair{Key: KeyMinReputation, Value: &p.MinReputation, ValidatorFn: validateReputation},
		ParamSetPair{Key: KeySlashingGracePeriod, Value: &p.SlashingGracePeriod, ValidatorFn: validatePositiveDurationSecondsInt64},
		ParamSetPair{Key: KeyMaxJurisdictions, Value: &p.MaxJurisdictions, ValidatorFn: validatePositiveInt32},
		ParamSetPair{Key: KeyDisputeWindowSeconds, Value: &p.DisputeWindowSeconds, ValidatorFn: validateRegistryParamsDurationSecondsInt64},
		ParamSetPair{Key: KeyBurnRateSpendBPS, Value: &p.BurnRateSpendBps, ValidatorFn: validateUint32Bps},
		ParamSetPair{Key: KeyBurnRateAcqBPS, Value: &p.BurnRateAcqBps, ValidatorFn: validateUint32Bps},
		ParamSetPair{Key: KeyQualityRebateBPS, Value: &p.QualityRebateBps, ValidatorFn: validateUint32Bps},
		ParamSetPair{Key: KeyInsuranceTargetUtil, Value: &p.InsuranceTargetUtil, ValidatorFn: validateInsuranceTarget},
		ParamSetPair{Key: KeyPremiumAdjustmentBPS, Value: &p.PremiumAdjustmentBps, ValidatorFn: validateNonNegativeInt32},
		ParamSetPair{Key: KeyRevenueSplits, Value: &p.RevenueSplits, ValidatorFn: validateRevenueSplitsParam},
		ParamSetPair{Key: KeyCacheFeeSplit, Value: &p.CacheFeeSplit, ValidatorFn: validateCacheSplitParam},
		ParamSetPair{Key: KeyMaxSettlementsPerBlock, Value: &p.MaxSettlementsPerBlock, ValidatorFn: validatePositiveUint32},
		ParamSetPair{Key: KeyMaxReceiptsScanPerBlock, Value: &p.MaxReceiptsScanPerBlock, ValidatorFn: validatePositiveUint32},

		ParamSetPair{Key: KeySLASlashGammaBps, Value: &p.SlaSlashGammaBps, ValidatorFn: validateUint32Bps},
		ParamSetPair{Key: KeySLAP95LatencyConsecutive, Value: &p.SlaP95LatencyConsecutive, ValidatorFn: validateSLAP95LatencyConsecutive},
		ParamSetPair{Key: KeySLADisputeRateBps, Value: &p.SlaDisputeRateBps, ValidatorFn: validateUint32Bps},
		ParamSetPair{Key: KeySLAMinCalls, Value: &p.SlaMinCalls, ValidatorFn: validateSLAMinCalls},
		ParamSetPair{Key: KeySLAMaxSlashPerEpochBps, Value: &p.SlaMaxSlashPerEpochBps, ValidatorFn: validateUint32Bps},
		ParamSetPair{Key: KeySLARecidivistMultiplierBps, Value: &p.SlaRecidivistMultiplierBps, ValidatorFn: validateRecidivistMultiplierBps},
		ParamSetPair{Key: KeyChallengeResolutionDeadlineSeconds, Value: &p.ChallengeResolutionDeadlineSeconds, ValidatorFn: validateRegistryParamsDurationSecondsInt64},
		ParamSetPair{Key: KeySettledReceiptRetentionSeconds, Value: &p.SettledReceiptRetentionSeconds, ValidatorFn: validateRegistryParamsDurationSecondsInt64},
		ParamSetPair{Key: KeyVerifiedBadgeParams, Value: &p.VerifiedBadge, ValidatorFn: validateVerifiedBadgeParamsParam},
		ParamSetPair{Key: KeyRecursiveRoyaltyMaxDepth, Value: &p.RecursiveRoyaltyMaxDepth, ValidatorFn: validateRecursiveRoyaltyMaxDepth},
		ParamSetPair{Key: KeyRecursiveRoyaltyMaxAggregateBps, Value: &p.RecursiveRoyaltyMaxAggregateBps, ValidatorFn: validateRecursiveRoyaltyMaxAggregateBps},
	}
}

// ParamSetPairs returns security-related parameters handled separately from the primary set.
func (p *SecurityParams) ParamSetPairs() ParamSetPairs {
	if p == nil {
		return ParamSetPairs{}
	}
	return ParamSetPairs{
		ParamSetPair{Key: KeyAttestationTTLSeconds, Value: &p.AttestationTtlSeconds, ValidatorFn: validateNonNegativeUint32},
		ParamSetPair{Key: KeySBOMStalenessSeconds, Value: &p.SbomStalenessSeconds, ValidatorFn: validateNonNegativeUint32},
	}
}

// Validation functions
func validateMinBondAmount(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	dec, err := decimal.NewFromString(v)
	if err != nil {
		return fmt.Errorf("invalid decimal: %w", err)
	}
	// Defense-in-depth matching Params.Validate's MinBondAmount guard.
	if !moneyguard.IsSafeExponent(dec) {
		return fmt.Errorf("min bond amount magnitude out of range")
	}
	if dec.IsNegative() {
		return fmt.Errorf("min bond amount cannot be negative")
	}
	return nil
}

func validateMaxActiveTools(i interface{}) error {
	v, ok := i.(int32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("max active tools must be positive")
	}
	return nil
}

func validateBPS(i interface{}) error {
	v, ok := i.(int32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v < 0 || v > 10000 {
		return fmt.Errorf("BPS must be between 0 and 10000")
	}
	return nil
}

func validatePositiveInt32(i interface{}) error {
	v, ok := i.(int32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("value must be positive")
	}
	return nil
}

func validatePositiveSettlementPeriodBlocks(field string, blocks int64) error {
	if blocks <= 0 {
		return fmt.Errorf("%s must be positive", field)
	}
	if blocks > maxSafeSettlementPeriodBlocks {
		return fmt.Errorf("%s exceeds maximum safe duration blocks (%d)", field, maxSafeSettlementPeriodBlocks)
	}
	return nil
}

func validatePositiveSettlementPeriodBlocksInt64(i interface{}) error {
	v, ok := i.(int64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("value must be positive")
	}
	if v > maxSafeSettlementPeriodBlocks {
		return fmt.Errorf("settlement period blocks exceeds maximum safe value (%d)", maxSafeSettlementPeriodBlocks)
	}
	return nil
}

func validatePositiveDurationSeconds(field string, seconds int64) error {
	if seconds <= 0 {
		return fmt.Errorf("%s must be positive", field)
	}
	if seconds > maxSafeDurationSeconds {
		return fmt.Errorf("%s exceeds maximum safe duration seconds (%d)", field, maxSafeDurationSeconds)
	}
	return nil
}

func validateRegistryParamsDurationSeconds(field string, seconds int64) error {
	if err := validatePositiveDurationSeconds(field, seconds); err != nil {
		return err
	}
	if seconds > maxRegistryParamsDurationSeconds {
		return fmt.Errorf("%s exceeds maximum registry params seconds (%d)", field, maxRegistryParamsDurationSeconds)
	}
	return nil
}

func validatePositiveDurationSecondsInt64(i interface{}) error {
	v, ok := i.(int64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("value must be positive")
	}
	if v > maxSafeDurationSeconds {
		return fmt.Errorf("duration seconds exceeds maximum safe value (%d)", maxSafeDurationSeconds)
	}
	return nil
}

func validateRegistryParamsDurationSecondsInt64(i interface{}) error {
	v, ok := i.(int64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("value must be positive")
	}
	if v > maxSafeDurationSeconds {
		return fmt.Errorf("duration seconds exceeds maximum safe value (%d)", maxSafeDurationSeconds)
	}
	if v > maxRegistryParamsDurationSeconds {
		return fmt.Errorf("duration seconds exceeds maximum registry params value (%d)", maxRegistryParamsDurationSeconds)
	}
	return nil
}

func validateReputation(i interface{}) error {
	v, ok := i.(float64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v < 0 || v > 1 {
		return fmt.Errorf("reputation must be between 0 and 1")
	}
	return nil
}

func validateUint32Bps(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v > 10000 {
		return fmt.Errorf("bps must be between 0 and 10000")
	}
	return nil
}

func validateInsuranceTarget(i interface{}) error {
	value, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	dec, err := decimal.NewFromString(value)
	if err != nil {
		return fmt.Errorf("invalid insurance target util: %w", err)
	}
	// Consensus-halt guard: this ParamSetPair validator is invoked on
	// the MsgUpdateParams governance path; the GreaterThan below would
	// explode big.Int on a symbolic exponent. Gate before Cmp.
	if !moneyguard.IsSafeExponent(dec) {
		return fmt.Errorf("insurance target utilization magnitude out of range")
	}
	if dec.IsNegative() || dec.GreaterThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("insurance target utilization must be between 0 and 1")
	}
	return nil
}

func validatePositiveUint32(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == 0 {
		return fmt.Errorf("value must be greater than zero")
	}
	return nil
}

func validateNonNegativeUint32(i interface{}) error {
	if _, ok := i.(uint32); !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}

func validateSLAP95LatencyConsecutive(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v > 1000 {
		return fmt.Errorf("sla p95 latency consecutive threshold must be <= 1000")
	}
	return nil
}

func validateSLAMinCalls(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v > 10_000_000 {
		return fmt.Errorf("sla min calls out of range: %d", v)
	}
	return nil
}

func validateRecidivistMultiplierBps(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == 0 {
		return nil
	}
	if v < 10_000 {
		return fmt.Errorf("recidivist multiplier bps must be >= 10000 or 0 to disable")
	}
	if v > 50_000 {
		return fmt.Errorf("recidivist multiplier bps must be <= 50000")
	}
	return nil
}

func validateNonNegativeInt32(i interface{}) error {
	v, ok := i.(int32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v < 0 {
		return fmt.Errorf("value must be non-negative")
	}
	return nil
}

func validateRevenueSplitsParam(i interface{}) error {
	switch v := i.(type) {
	case *RevenueSplits:
		return validateRevenueSplits(v)
	case RevenueSplits:
		return validateRevenueSplits(&v)
	default:
		return fmt.Errorf("invalid parameter type: %T", i)
	}
}

func validateCacheSplitParam(i interface{}) error {
	switch v := i.(type) {
	case *CacheFeeSplit:
		return validateCacheSplit(v)
	case CacheFeeSplit:
		return validateCacheSplit(&v)
	default:
		return fmt.Errorf("invalid parameter type: %T", i)
	}
}

func validateVerifiedBadgeParamsParam(i interface{}) error {
	switch v := i.(type) {
	case *VerifiedBadgeParams:
		return validateVerifiedBadgeParams(v)
	case VerifiedBadgeParams:
		return validateVerifiedBadgeParams(&v)
	default:
		return fmt.Errorf("invalid parameter type: %T", i)
	}
}

func validateRecursiveRoyaltyMaxDepth(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return validateRecursiveRoyaltyMaxDepthValue(v)
}

func validateRecursiveRoyaltyMaxDepthValue(v uint32) error {
	if v == 0 {
		return fmt.Errorf("recursive royalty max depth must be > 0")
	}
	if v > maxRecursiveRoyaltyDepth {
		return fmt.Errorf("recursive royalty max depth must be <= %d", maxRecursiveRoyaltyDepth)
	}
	return nil
}

func validateRecursiveRoyaltyMaxAggregateBps(i interface{}) error {
	v, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return validateRecursiveRoyaltyMaxAggregateBpsValue(v)
}

func validateRecursiveRoyaltyMaxAggregateBpsValue(v uint32) error {
	if v == 0 {
		return fmt.Errorf("recursive royalty max aggregate bps must be > 0")
	}
	if v > BPSDenominator {
		return fmt.Errorf("recursive royalty max aggregate bps must be <= %d", BPSDenominator)
	}
	return nil
}

func validateVerifiedBadgeParams(p *VerifiedBadgeParams) error {
	if p == nil {
		return fmt.Errorf("verified badge params cannot be nil")
	}

	totalWeight := p.SbomWeightBps + p.BondWeightBps + p.ChallengeWeightBps + p.UptimeWeightBps
	if totalWeight != BPSDenominator {
		return fmt.Errorf("verified badge weights must sum to %d bps, got %d", BPSDenominator, totalWeight)
	}
	if p.SbomWeightBps == 0 || p.BondWeightBps == 0 || p.ChallengeWeightBps == 0 || p.UptimeWeightBps == 0 {
		return fmt.Errorf("verified badge weights must all be > 0")
	}

	if err := validateAscendingThresholds("verified badge composite", p.BronzeCompositeBps, p.SilverCompositeBps, p.GoldCompositeBps, BPSDenominator); err != nil {
		return err
	}
	if err := validateAscendingThresholds("verified badge sbom", p.SbomBronzeBps, p.SbomSilverBps, p.SbomGoldBps, BPSDenominator); err != nil {
		return err
	}
	if err := validateAscendingThresholds("verified badge challenge", p.ChallengeBronzeBps, p.ChallengeSilverBps, p.ChallengeGoldBps, BPSDenominator); err != nil {
		return err
	}
	if err := validateAscendingThresholds("verified badge uptime", p.UptimeBronzeBps, p.UptimeSilverBps, p.UptimeGoldBps, BPSDenominator); err != nil {
		return err
	}
	if err := validateAscendingThresholds("verified badge bond", p.BondBronzeBps, p.BondSilverBps, p.BondGoldBps, 100000); err != nil {
		return err
	}

	return nil
}

func validateAscendingThresholds(name string, bronze, silver, gold, max uint32) error {
	if bronze == 0 {
		return fmt.Errorf("%s bronze threshold must be > 0", name)
	}
	if bronze > silver || silver > gold {
		return fmt.Errorf("%s thresholds must satisfy bronze <= silver <= gold", name)
	}
	if gold > max {
		return fmt.Errorf("%s gold threshold must be <= %d", name, max)
	}
	return nil
}
