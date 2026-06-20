
// Package keeper handles price aggregation for the oracle module.
package keeper

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// ProviderAggregationMethod selects the reduction used for provider-level
// price aggregation before a validator emits its vote extension.
type ProviderAggregationMethod string

const (
	ProviderAggregationMedian         ProviderAggregationMethod = "median"
	ProviderAggregationWeightedMedian ProviderAggregationMethod = "weighted_median"

	defaultMaxProviderInputs = 8
)

// OracleProviderSample is the normalized local provider payload consumed prior
// to consensus-facing vote extension encoding.
type OracleProviderSample struct {
	ProviderID      string
	AssetPair       string
	ObservedAt      time.Time
	Price           string
	Volume24H       string
	ConfidenceScore string
	Signature       string
}

// CanonicalKey returns a deterministic identity for diagnostics and duplicate
// detection.
func (s OracleProviderSample) CanonicalKey() string {
	var builder strings.Builder
	writeProviderCanonicalPart(&builder, strings.TrimSpace(s.ProviderID))
	writeProviderCanonicalPart(&builder, strings.TrimSpace(s.AssetPair))
	writeProviderCanonicalPart(&builder, s.ObservedAt.UTC().Format(time.RFC3339Nano))
	writeProviderCanonicalPart(&builder, strings.TrimSpace(s.Price))
	return builder.String()
}

// Validate enforces the provider sample shape contract.
func (s OracleProviderSample) Validate(snapshotTime time.Time) error {
	if strings.TrimSpace(s.ProviderID) == "" {
		return fmt.Errorf("provider id required")
	}
	if strings.TrimSpace(s.AssetPair) == "" {
		return fmt.Errorf("asset pair required")
	}
	price, err := parseProviderDec("price", s.Price)
	if err != nil {
		return err
	}
	if !price.IsPositive() {
		return fmt.Errorf("price must be positive")
	}
	if strings.TrimSpace(s.Volume24H) != "" {
		volume, err := parseProviderDec("volume_24h", s.Volume24H)
		if err != nil {
			return err
		}
		if volume.IsNegative() {
			return fmt.Errorf("volume_24h must be non-negative")
		}
	}
	confidence, err := parseProviderDec("confidence_score", s.ConfidenceScore)
	if err != nil {
		return err
	}
	if confidence.IsNegative() || confidence.GT(sdkmath.LegacyOneDec()) {
		return fmt.Errorf("confidence_score must be between 0 and 1")
	}
	if s.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at required")
	}
	if !snapshotTime.IsZero() && s.ObservedAt.After(snapshotTime) {
		return fmt.Errorf("observed_at %s is after snapshot time %s", s.ObservedAt.UTC().Format(time.RFC3339Nano), snapshotTime.UTC().Format(time.RFC3339Nano))
	}
	return nil
}

// OracleProviderConfig records deterministic provider-local normalization
// settings.
type OracleProviderConfig struct {
	CanonicalAssetPairs map[string]string
	Weight              string
}

// OracleProviderSignatureVerifier verifies provider identities/signatures.
type OracleProviderSignatureVerifier func(sample OracleProviderSample, provider OracleProviderConfig) error

// ProviderAggregationConfig controls provider-level normalization and bounded
// aggregation.
type ProviderAggregationConfig struct {
	MaxProviders      int
	MaxSampleAge      time.Duration
	MaxPriceDeviation string
	AggregationMethod ProviderAggregationMethod
	ProviderConfigs   map[string]OracleProviderConfig
	SignatureVerifier OracleProviderSignatureVerifier
}

// DefaultProviderAggregationConfig returns deterministic defaults for local
// provider reduction.
func DefaultProviderAggregationConfig() ProviderAggregationConfig {
	return ProviderAggregationConfig{
		MaxProviders:      defaultMaxProviderInputs,
		AggregationMethod: ProviderAggregationMedian,
	}
}

// ProviderSampleDiagnostic captures why a provider sample was excluded or
// degraded.
type ProviderSampleDiagnostic struct {
	ProviderID   string
	AssetPair    string
	Code         string
	Message      string
	CanonicalKey string
}

// ProviderFeedAggregate captures one canonical per-asset retained provider set
// after local normalization and outlier filtering.
type ProviderFeedAggregate struct {
	AssetPair       string
	Price           string
	Volume24H       string
	ConfidenceScore string
	Sources         []string
	Samples         []OracleProviderSample
}

// ProviderAggregationReport contains the canonical feeds plus rejection
// diagnostics from the provider normalization pipeline.
type ProviderAggregationReport struct {
	Feeds              []*types.PriceFeed
	FeedAggregates     []ProviderFeedAggregate
	Diagnostics        []ProviderSampleDiagnostic
	AcceptedSamples    int
	RetainedSamples    int
	ProcessedAssets    int
	FilteredOutliers   int
	BoundedDropCount   int
	DuplicateDropCount int
}

type normalizedProviderConfig struct {
	raw                 OracleProviderConfig
	canonicalAssetPairs map[string]string
	weight              sdkmath.LegacyDec
}

type normalizedProviderAggregationConfig struct {
	maxProviders      int
	maxSampleAge      time.Duration
	maxPriceDeviation sdkmath.LegacyDec
	aggregationMethod ProviderAggregationMethod
	providerConfigs   map[string]normalizedProviderConfig
	signatureVerifier OracleProviderSignatureVerifier
}

type normalizedProviderSample struct {
	sample          OracleProviderSample
	canonicalKey    string
	providerAssetID string
	price           sdkmath.LegacyDec
	volume          sdkmath.LegacyDec
	confidence      sdkmath.LegacyDec
	weight          sdkmath.LegacyDec
}

// BuildCanonicalPriceFeeds reduces normalized provider samples into one
// deterministic price feed per asset pair, bounded by max providers and
// protected by signature, freshness, and outlier validation.
func BuildCanonicalPriceFeeds(
	snapshotTime time.Time,
	allowedAssetPairs []string,
	samples []OracleProviderSample,
	cfg ProviderAggregationConfig,
) (ProviderAggregationReport, error) {
	report := ProviderAggregationReport{
		Feeds:          make([]*types.PriceFeed, 0, len(allowedAssetPairs)),
		FeedAggregates: make([]ProviderFeedAggregate, 0, len(allowedAssetPairs)),
		Diagnostics:    make([]ProviderSampleDiagnostic, 0),
	}

	if snapshotTime.IsZero() {
		return report, fmt.Errorf("snapshot time required")
	}

	normalizedCfg, err := normalizeProviderAggregationConfig(cfg)
	if err != nil {
		return report, err
	}

	allowed := make(map[string]struct{}, len(allowedAssetPairs))
	for _, pair := range allowedAssetPairs {
		trimmed := strings.TrimSpace(pair)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}

	candidatesByProviderAsset := make(map[string][]normalizedProviderSample)
	for _, sample := range samples {
		normalized, diagnostic, ok, err := normalizeProviderSample(snapshotTime, allowed, sample, normalizedCfg)
		if err != nil {
			return report, err
		}
		if !ok {
			report.Diagnostics = append(report.Diagnostics, diagnostic)
			continue
		}
		candidatesByProviderAsset[normalized.providerAssetID] = append(candidatesByProviderAsset[normalized.providerAssetID], normalized)
	}

	retainedByAsset := make(map[string][]normalizedProviderSample)
	providerAssetKeys := make([]string, 0, len(candidatesByProviderAsset))
	for key := range candidatesByProviderAsset {
		providerAssetKeys = append(providerAssetKeys, key)
	}
	sort.Strings(providerAssetKeys)

	for _, key := range providerAssetKeys {
		group := candidatesByProviderAsset[key]
		if len(group) > 1 {
			sort.Slice(group, func(i, j int) bool {
				return group[i].canonicalKey < group[j].canonicalKey
			})
			for _, duplicate := range group {
				report.Diagnostics = append(report.Diagnostics, newProviderDiagnostic(
					duplicate.sample,
					"duplicate_provider_asset",
					fmt.Sprintf("duplicate provider sample for %s", key),
				))
			}
			report.DuplicateDropCount += len(group)
			continue
		}

		sample := group[0]
		retainedByAsset[strings.TrimSpace(sample.sample.AssetPair)] = append(retainedByAsset[strings.TrimSpace(sample.sample.AssetPair)], sample)
		report.AcceptedSamples++
	}

	assetPairs := make([]string, 0, len(retainedByAsset))
	for assetPair := range retainedByAsset {
		assetPairs = append(assetPairs, assetPair)
	}
	sort.Strings(assetPairs)

	for _, assetPair := range assetPairs {
		assetSamples := retainedByAsset[assetPair]
		sort.Slice(assetSamples, func(i, j int) bool {
			leftProvider := strings.TrimSpace(assetSamples[i].sample.ProviderID)
			rightProvider := strings.TrimSpace(assetSamples[j].sample.ProviderID)
			if leftProvider == rightProvider {
				return assetSamples[i].canonicalKey < assetSamples[j].canonicalKey
			}
			return leftProvider < rightProvider
		})

		if normalizedCfg.maxProviders > 0 && len(assetSamples) > normalizedCfg.maxProviders {
			for _, dropped := range assetSamples[normalizedCfg.maxProviders:] {
				report.Diagnostics = append(report.Diagnostics, newProviderDiagnostic(
					dropped.sample,
					"provider_limit_exceeded",
					fmt.Sprintf("provider dropped because max providers is %d", normalizedCfg.maxProviders),
				))
			}
			report.BoundedDropCount += len(assetSamples) - normalizedCfg.maxProviders
			assetSamples = assetSamples[:normalizedCfg.maxProviders]
		}
		if len(assetSamples) == 0 {
			continue
		}

		referencePrices := make([]sdkmath.LegacyDec, len(assetSamples))
		for i, sample := range assetSamples {
			referencePrices[i] = sample.price
		}
		referenceMedian := medianDec(referencePrices)
		retained := filterProviderOutliers(assetSamples, referenceMedian, normalizedCfg.maxPriceDeviation)
		if len(retained) == 0 {
			report.Diagnostics = append(report.Diagnostics, ProviderSampleDiagnostic{
				AssetPair: assetPair,
				Code:      "outlier_filter_fallback",
				Message:   "all provider samples were filtered as outliers; falling back to unfiltered set",
			})
			retained = assetSamples
		} else {
			report.FilteredOutliers += len(assetSamples) - len(retained)
		}

		price := aggregateProviderPrice(retained, normalizedCfg.aggregationMethod)
		volume := sdkmath.LegacyZeroDec()
		confidenceSum := sdkmath.LegacyZeroDec()
		sources := make([]string, 0, len(retained))
		retainedSamples := make([]OracleProviderSample, 0, len(retained))
		for _, sample := range retained {
			volume = volume.Add(sample.volume)
			confidenceSum = confidenceSum.Add(sample.confidence)
			sources = append(sources, strings.TrimSpace(sample.sample.ProviderID))
			retainedSamples = append(retainedSamples, sample.sample)
		}
		sort.Strings(sources)
		sort.Slice(retainedSamples, func(i, j int) bool {
			return retainedSamples[i].CanonicalKey() < retainedSamples[j].CanonicalKey()
		})
		confidenceMean := confidenceSum.Quo(sdkmath.LegacyNewDec(int64(len(retained))))
		priceText := price.String()
		volumeText := volume.String()
		confidenceText := confidenceMean.String()

		report.Feeds = append(report.Feeds, &types.PriceFeed{
			AssetPair:       assetPair,
			Price:           priceText,
			Volume_24H:      volumeText,
			Timestamp:       snapshotTime.UTC(),
			Sources:         sources,
			ConfidenceScore: confidenceText,
		})
		report.FeedAggregates = append(report.FeedAggregates, ProviderFeedAggregate{
			AssetPair:       assetPair,
			Price:           priceText,
			Volume24H:       volumeText,
			ConfidenceScore: confidenceText,
			Sources:         append([]string(nil), sources...),
			Samples:         retainedSamples,
		})
		report.ProcessedAssets++
		report.RetainedSamples += len(retained)
	}

	sort.Slice(report.Feeds, func(i, j int) bool {
		return strings.TrimSpace(report.Feeds[i].AssetPair) < strings.TrimSpace(report.Feeds[j].AssetPair)
	})

	return report, nil
}

func normalizeProviderAggregationConfig(cfg ProviderAggregationConfig) (normalizedProviderAggregationConfig, error) {
	method := cfg.AggregationMethod
	if method == "" {
		method = ProviderAggregationMedian
	}
	switch method {
	case ProviderAggregationMedian, ProviderAggregationWeightedMedian:
	default:
		return normalizedProviderAggregationConfig{}, fmt.Errorf("unsupported provider aggregation method %q", method)
	}

	maxProviders := cfg.MaxProviders
	if maxProviders <= 0 {
		maxProviders = defaultMaxProviderInputs
	}

	maxDeviation := sdkmath.LegacyZeroDec()
	if raw := strings.TrimSpace(cfg.MaxPriceDeviation); raw != "" {
		dec, err := sdkmath.LegacyNewDecFromStr(raw)
		if err != nil {
			return normalizedProviderAggregationConfig{}, fmt.Errorf("invalid provider max price deviation %q: %w", raw, err)
		}
		if dec.IsNegative() {
			return normalizedProviderAggregationConfig{}, fmt.Errorf("provider max price deviation cannot be negative: %s", raw)
		}
		maxDeviation = dec
	}

	providerConfigs := make(map[string]normalizedProviderConfig, len(cfg.ProviderConfigs))
	for providerID, providerCfg := range cfg.ProviderConfigs {
		trimmedProviderID := strings.TrimSpace(providerID)
		if trimmedProviderID == "" {
			return normalizedProviderAggregationConfig{}, fmt.Errorf("provider config key cannot be empty")
		}

		weight := sdkmath.LegacyOneDec()
		if raw := strings.TrimSpace(providerCfg.Weight); raw != "" {
			parsedWeight, err := sdkmath.LegacyNewDecFromStr(raw)
			if err != nil {
				return normalizedProviderAggregationConfig{}, fmt.Errorf("invalid provider weight for %s: %w", trimmedProviderID, err)
			}
			if !parsedWeight.IsPositive() {
				return normalizedProviderAggregationConfig{}, fmt.Errorf("provider weight must be positive for %s", trimmedProviderID)
			}
			weight = parsedWeight
		}

		aliases := make(map[string]string, len(providerCfg.CanonicalAssetPairs))
		for rawAsset, canonicalAsset := range providerCfg.CanonicalAssetPairs {
			trimmedRaw := strings.TrimSpace(rawAsset)
			trimmedCanonical := strings.TrimSpace(canonicalAsset)
			if trimmedRaw == "" || trimmedCanonical == "" {
				return normalizedProviderAggregationConfig{}, fmt.Errorf("provider %s has empty asset mapping", trimmedProviderID)
			}
			aliases[trimmedRaw] = trimmedCanonical
		}

		providerConfigs[trimmedProviderID] = normalizedProviderConfig{
			raw:                 providerCfg,
			canonicalAssetPairs: aliases,
			weight:              weight,
		}
	}

	return normalizedProviderAggregationConfig{
		maxProviders:      maxProviders,
		maxSampleAge:      cfg.MaxSampleAge,
		maxPriceDeviation: maxDeviation,
		aggregationMethod: method,
		providerConfigs:   providerConfigs,
		signatureVerifier: cfg.SignatureVerifier,
	}, nil
}

func normalizeProviderSample(
	snapshotTime time.Time,
	allowed map[string]struct{},
	sample OracleProviderSample,
	cfg normalizedProviderAggregationConfig,
) (normalizedProviderSample, ProviderSampleDiagnostic, bool, error) {
	normalized := sample
	normalized.ProviderID = strings.TrimSpace(normalized.ProviderID)
	normalized.AssetPair = strings.TrimSpace(normalized.AssetPair)
	normalized.Signature = strings.TrimSpace(normalized.Signature)

	providerCfg, hasProviderCfg := cfg.providerConfigs[normalized.ProviderID]
	if len(cfg.providerConfigs) > 0 && !hasProviderCfg {
		return normalizedProviderSample{}, newProviderDiagnostic(normalized, "unknown_provider", "provider is not in the approved provider allowlist"), false, nil
	}
	if hasProviderCfg {
		if canonicalAsset, ok := providerCfg.canonicalAssetPairs[normalized.AssetPair]; ok {
			normalized.AssetPair = canonicalAsset
		}
	}

	if err := normalized.Validate(snapshotTime); err != nil {
		return normalizedProviderSample{}, newProviderDiagnostic(normalized, "invalid_sample", err.Error()), false, nil
	}

	if len(allowed) > 0 {
		if _, ok := allowed[normalized.AssetPair]; !ok {
			return normalizedProviderSample{}, newProviderDiagnostic(normalized, "asset_pair_not_allowed", "asset pair is not in the oracle allowlist"), false, nil
		}
	}

	if cfg.maxSampleAge > 0 && snapshotTime.Sub(normalized.ObservedAt) > cfg.maxSampleAge {
		return normalizedProviderSample{}, newProviderDiagnostic(normalized, "stale_sample", fmt.Sprintf("provider sample exceeded max age of %s", cfg.maxSampleAge)), false, nil
	}

	if cfg.signatureVerifier != nil {
		rawCfg := OracleProviderConfig{}
		if hasProviderCfg {
			rawCfg = providerCfg.raw
		}
		if err := cfg.signatureVerifier(normalized, rawCfg); err != nil {
			return normalizedProviderSample{}, newProviderDiagnostic(normalized, "invalid_signature", err.Error()), false, nil
		}
	}

	price, err := parseProviderDec("price", normalized.Price)
	if err != nil {
		return normalizedProviderSample{}, ProviderSampleDiagnostic{}, false, err
	}

	volume := sdkmath.LegacyZeroDec()
	if raw := strings.TrimSpace(normalized.Volume24H); raw != "" {
		volume, err = parseProviderDec("volume_24h", raw)
		if err != nil {
			return normalizedProviderSample{}, ProviderSampleDiagnostic{}, false, err
		}
	}

	confidence, err := parseProviderDec("confidence_score", normalized.ConfidenceScore)
	if err != nil {
		return normalizedProviderSample{}, ProviderSampleDiagnostic{}, false, err
	}

	weight := sdkmath.LegacyOneDec()
	if hasProviderCfg {
		weight = providerCfg.weight
	}

	return normalizedProviderSample{
		sample:          normalized,
		canonicalKey:    normalized.CanonicalKey(),
		providerAssetID: normalized.ProviderID + "|" + normalized.AssetPair,
		price:           price,
		volume:          volume,
		confidence:      confidence,
		weight:          weight,
	}, ProviderSampleDiagnostic{}, true, nil
}

func newProviderDiagnostic(sample OracleProviderSample, code, message string) ProviderSampleDiagnostic {
	return ProviderSampleDiagnostic{
		ProviderID:   strings.TrimSpace(sample.ProviderID),
		AssetPair:    strings.TrimSpace(sample.AssetPair),
		Code:         code,
		Message:      message,
		CanonicalKey: sample.CanonicalKey(),
	}
}

func filterProviderOutliers(
	samples []normalizedProviderSample,
	median sdkmath.LegacyDec,
	maxDeviation sdkmath.LegacyDec,
) []normalizedProviderSample {
	if !median.IsPositive() || maxDeviation.IsZero() {
		return append([]normalizedProviderSample(nil), samples...)
	}

	filtered := make([]normalizedProviderSample, 0, len(samples))
	for _, sample := range samples {
		diff := sample.price.Sub(median).Abs()
		deviation := diff.Quo(median)
		if deviation.LTE(maxDeviation) {
			filtered = append(filtered, sample)
		}
	}

	return filtered
}

func aggregateProviderPrice(samples []normalizedProviderSample, method ProviderAggregationMethod) sdkmath.LegacyDec {
	if len(samples) == 0 {
		return sdkmath.LegacyZeroDec()
	}

	switch method {
	case ProviderAggregationWeightedMedian:
		return weightedMedianDec(samples)
	default:
		prices := make([]sdkmath.LegacyDec, len(samples))
		for i, sample := range samples {
			prices[i] = sample.price
		}
		return medianDec(prices)
	}
}

func weightedMedianDec(samples []normalizedProviderSample) sdkmath.LegacyDec {
	if len(samples) == 0 {
		return sdkmath.LegacyZeroDec()
	}

	sorted := append([]normalizedProviderSample(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].price.Equal(sorted[j].price) {
			leftProvider := strings.TrimSpace(sorted[i].sample.ProviderID)
			rightProvider := strings.TrimSpace(sorted[j].sample.ProviderID)
			if leftProvider == rightProvider {
				return sorted[i].canonicalKey < sorted[j].canonicalKey
			}
			return leftProvider < rightProvider
		}
		return sorted[i].price.LT(sorted[j].price)
	})

	totalWeight := sdkmath.LegacyZeroDec()
	for _, sample := range sorted {
		totalWeight = totalWeight.Add(sample.weight)
	}
	if !totalWeight.IsPositive() {
		prices := make([]sdkmath.LegacyDec, len(sorted))
		for i, sample := range sorted {
			prices[i] = sample.price
		}
		return medianDec(prices)
	}

	threshold := totalWeight.Quo(sdkmath.LegacyNewDec(2))
	cumulative := sdkmath.LegacyZeroDec()
	for _, sample := range sorted {
		cumulative = cumulative.Add(sample.weight)
		if cumulative.GTE(threshold) {
			return sample.price
		}
	}

	return sorted[len(sorted)-1].price
}

func medianDec(prices []sdkmath.LegacyDec) sdkmath.LegacyDec {
	if len(prices) == 0 {
		return sdkmath.LegacyZeroDec()
	}

	sorted := append([]sdkmath.LegacyDec(nil), prices...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].LT(sorted[j])
	})

	return medianFromSortedPrices(sorted)
}

func medianFromSortedPrices(sorted []sdkmath.LegacyDec) sdkmath.LegacyDec {
	if len(sorted) == 0 {
		return sdkmath.LegacyZeroDec()
	}

	n := len(sorted)
	if n%2 == 0 {
		mid1 := sorted[n/2-1]
		mid2 := sorted[n/2]
		return mid1.Add(mid2).Quo(sdkmath.LegacyNewDec(2))
	}

	return sorted[n/2]
}

func parseProviderDec(field, value string) (sdkmath.LegacyDec, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return sdkmath.LegacyZeroDec(), fmt.Errorf("%s missing", field)
	}
	dec, err := sdkmath.LegacyNewDecFromStr(trimmed)
	if err != nil {
		return sdkmath.LegacyZeroDec(), fmt.Errorf("invalid %s: %w", field, err)
	}
	return dec, nil
}

func writeProviderCanonicalPart(builder *strings.Builder, value string) {
	builder.WriteString("|")
	builder.WriteString(strconv.Itoa(len(value)))
	builder.WriteString(":")
	builder.WriteString(value)
}

// AggregateVotes aggregates validator votes into consensus prices
// This is called during FinalizeBlock to compute the final prices for the block
func (k Keeper) AggregateVotes(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get all validator votes
	votes, err := k.GetAllValidatorVotes(ctx)
	if err != nil {
		return err
	}

	if len(votes) == 0 {
		k.Logger(ctx).Debug("no validator votes to aggregate")
		return nil
	}

	// Get params for validation
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	// Filter stale votes
	now := sdkCtx.BlockTime()
	validVotes := filterStaleVotes(votes, now, params.GetMaxVoteAge())

	if len(validVotes) == 0 {
		k.Logger(ctx).Warn("all votes are stale, clearing and skipping aggregation")
		return k.ClearValidatorVotes(ctx)
	}

	// Group votes by asset pair, surfacing duplicate feed drops so
	// misconfigured or malicious validators can be detected in logs.
	votesByAsset, drops := groupVotesByAssetWithDrops(validVotes)
	for _, d := range drops {
		k.Logger(ctx).Warn("discarded duplicate validator price feed",
			"validator", d.Validator, "asset_pair", d.AssetPair)
	}

	// Sort asset pairs for deterministic iteration (prevent consensus failure)
	assetPairs := make([]string, 0, len(votesByAsset))
	for assetPair := range votesByAsset {
		assetPairs = append(assetPairs, assetPair)
	}
	sort.Strings(assetPairs)

	voterSet := make(map[string]struct{})

	// Aggregate each asset pair in deterministic order
	for _, assetPair := range assetPairs {
		assetVotes := votesByAsset[assetPair]
		aggregated, contributingAddrs, err := k.aggregateAssetPrices(ctx, assetPair, assetVotes, sdkCtx.BlockHeight(), now, params)
		if err != nil {
			k.Logger(ctx).Error("failed to aggregate prices",
				"asset_pair", assetPair,
				"error", err)
			continue
		}

		// Store aggregated price
		if err := k.SetAggregatedPrice(ctx, aggregated); err != nil {
			k.Logger(ctx).Error("failed to store aggregated price",
				"asset_pair", assetPair,
				"error", err)
			continue
		}

		// Reward only the validators whose prices contributed to the median;
		// outliers excluded by filterOutliers must not be paid for an
		// aggregation they did not shape.
		for _, addr := range contributingAddrs {
			voterSet[addr] = struct{}{}
		}

		k.Logger(ctx).Info("aggregated price updated",
			"asset_pair", assetPair,
			"median_price", aggregated.MedianPrice,
			"num_validators", aggregated.NumValidators)

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeAggregatedPrice,
				sdk.NewAttribute(types.AttributeKeyAssetPair, assetPair),
				sdk.NewAttribute(types.AttributeKeyMedianPrice, aggregated.MedianPrice),
				sdk.NewAttribute(types.AttributeKeyNumValidators, strconv.FormatInt(int64(aggregated.NumValidators), 10)),
			),
		)
	}

	voterAddrs := make([]string, 0, len(voterSet))
	for addr := range voterSet {
		voterAddrs = append(voterAddrs, addr)
	}
	sort.Strings(voterAddrs) // CRITICAL: must sort to ensure deterministic reward distribution

	if err := k.distributeVoteRewards(ctx, voterAddrs); err != nil {
		k.Logger(ctx).Error("failed to distribute oracle rewards", "error", err)
	}

	// Clear votes after aggregation
	return k.ClearValidatorVotes(ctx)
}

// aggregateAssetPrices aggregates prices for a single asset pair
func (k Keeper) aggregateAssetPrices(
	ctx context.Context,
	assetPair string,
	votes []priceVoteData,
	blockHeight int64,
	timestamp time.Time,
	params *types.Params,
) (*types.AggregatedPrice, []string, error) {
	if len(votes) == 0 {
		return nil, nil, types.ErrInsufficientVotes
	}

	// Extract prices and sort for median calculation
	prices := make([]sdkmath.LegacyDec, len(votes))
	for i, v := range votes {
		prices[i] = v.price
	}
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].LT(prices[j])
	})

	// Calculate median
	var median sdkmath.LegacyDec
	n := len(prices)
	if n%2 == 0 {
		// Even number: average of two middle values
		mid1 := prices[n/2-1]
		mid2 := prices[n/2]
		median = mid1.Add(mid2).Quo(sdkmath.LegacyNewDec(2))
	} else {
		// Odd number: middle value
		median = prices[n/2]
	}

	// Calculate mean
	sum := sdkmath.LegacyZeroDec()
	for _, p := range prices {
		sum = sum.Add(p)
	}
	mean := sum.Quo(sdkmath.LegacyNewDec(int64(n)))

	// Calculate standard deviation
	varianceSum := sdkmath.LegacyZeroDec()
	for _, p := range prices {
		diff := p.Sub(mean)
		varianceSum = varianceSum.Add(diff.Mul(diff))
	}
	variance := varianceSum.Quo(sdkmath.LegacyNewDec(int64(n)))

	// Standard deviation (sqrt of variance)
	// Using a simple approximation for sqrt since LegacyDec doesn't have native sqrt
	stdDev := sqrtDec(variance)

	// Filter outliers based on max price deviation
	maxDeviation := sdkmath.LegacyZeroDec()
	if raw := strings.TrimSpace(params.GetMaxPriceDeviation()); raw != "" {
		dec, err := sdkmath.LegacyNewDecFromStr(raw)
		if err != nil {
			k.Logger(ctx).Error("invalid MaxPriceDeviation param, outlier filtering disabled",
				"value", raw, "error", err)
		} else {
			maxDeviation = dec
		}
	}
	filteredPrices := filterOutliers(prices, median, maxDeviation)
	// Filter the votes by the identical predicate so the reward set matches
	// the set of validators that actually contributed to the median (same
	// median, same maxDeviation, same empty→fallback below).
	contributingVotes := filterOutlierVotes(votes, median, maxDeviation)

	// Handle edge case: all prices filtered as outliers
	// This can happen with even number of prices where median is between two middle values
	// and all actual prices deviate too much from that calculated median
	if len(filteredPrices) == 0 {
		// Use unfiltered prices - if all validators are "outliers", the deviation threshold
		// may be too strict or there's high price volatility
		k.Logger(ctx).Warn("all prices filtered as outliers, using unfiltered data",
			"asset_pair", assetPair,
			"price_count", len(prices),
			"max_deviation", strings.TrimSpace(params.GetMaxPriceDeviation()))
		filteredPrices = prices
		contributingVotes = votes
	}

	contributingAddrs := make([]string, 0, len(contributingVotes))
	for _, v := range contributingVotes {
		contributingAddrs = append(contributingAddrs, v.validatorAddr)
	}

	numValidators := int32(len(filteredPrices)) //#nosec G115 -- validator count bounded by practical limits

	// Recalculate median and mean with filtered prices if needed
	if len(filteredPrices) < len(prices) {
		k.Logger(ctx).Info("filtered outlier prices",
			"asset_pair", assetPair,
			"original_count", len(prices),
			"filtered_count", len(filteredPrices))

		// Recalculate with filtered prices
		sort.Slice(filteredPrices, func(i, j int) bool {
			return filteredPrices[i].LT(filteredPrices[j])
		})

		n = len(filteredPrices)
		if n%2 == 0 {
			mid1 := filteredPrices[n/2-1]
			mid2 := filteredPrices[n/2]
			median = mid1.Add(mid2).Quo(sdkmath.LegacyNewDec(2))
		} else {
			median = filteredPrices[n/2]
		}

		sum = sdkmath.LegacyZeroDec()
		for _, p := range filteredPrices {
			sum = sum.Add(p)
		}
		mean = sum.Quo(sdkmath.LegacyNewDec(int64(n)))
		stdDev = stdDevFromPrices(filteredPrices, mean)
	}

	return &types.AggregatedPrice{
		AssetPair:         assetPair,
		MedianPrice:       median.String(),
		MeanPrice:         mean.String(),
		StandardDeviation: stdDev.String(),
		NumValidators:     numValidators,
		BlockHeight:       blockHeight,
		Timestamp:         timestamp,
	}, contributingAddrs, nil
}

func stdDevFromPrices(prices []sdkmath.LegacyDec, mean sdkmath.LegacyDec) sdkmath.LegacyDec {
	if len(prices) == 0 {
		return sdkmath.LegacyZeroDec()
	}

	varianceSum := sdkmath.LegacyZeroDec()
	for _, price := range prices {
		diff := price.Sub(mean)
		varianceSum = varianceSum.Add(diff.Mul(diff))
	}

	variance := varianceSum.Quo(sdkmath.LegacyNewDec(int64(len(prices))))
	return sqrtDec(variance)
}

// priceVoteData represents a single price vote
type priceVoteData struct {
	validatorAddr string
	price         sdkmath.LegacyDec
}

// filterStaleVotes filters out votes that are too old or from the future
func filterStaleVotes(votes []*types.ValidatorVote, now time.Time, maxAge int64) []*types.ValidatorVote {
	if maxAge <= 0 {
		return votes
	}
	var valid []*types.ValidatorVote
	for _, vote := range votes {
		if vote == nil {
			continue
		}
		ts := vote.GetTimestamp()
		if ts.IsZero() {
			continue
		}
		ageDur := now.Sub(ts)
		if ageDur >= 0 && ageDur <= time.Duration(maxAge)*time.Second {
			valid = append(valid, vote)
		}
	}
	return valid
}

func (k Keeper) distributeVoteRewards(ctx context.Context, validatorAddrs []string) error {
	if k.bankKeeper == nil {
		return nil
	}
	if len(validatorAddrs) == 0 {
		return nil
	}

	reward := sdk.NewCoin(types.DefaultRewardDenom, sdkmath.NewInt(types.DefaultRewardAmount))
	seen := make(map[string]struct{}, len(validatorAddrs))

	type recipientInfo struct {
		validatorAddr string
		rewardAddr    string
		accountAddr   sdk.AccAddress
	}
	eligible := make([]recipientInfo, 0, len(validatorAddrs))

	for _, validatorAddr := range validatorAddrs {
		validatorAddr = strings.TrimSpace(validatorAddr)
		if validatorAddr == "" {
			continue
		}
		if _, ok := seen[validatorAddr]; ok {
			continue
		}
		seen[validatorAddr] = struct{}{}

		rewardAddr, ok := k.getRewardAddress(ctx, validatorAddr)
		if !ok {
			continue
		}
		accountAddr, err := sdk.AccAddressFromBech32(rewardAddr)
		if err != nil {
			k.Logger(ctx).Error("invalid reward address", "validator", validatorAddr, "address", rewardAddr, "error", err)
			continue
		}
		eligible = append(eligible, recipientInfo{
			validatorAddr: validatorAddr,
			rewardAddr:    rewardAddr,
			accountAddr:   accountAddr,
		})
	}

	if len(eligible) == 0 {
		return nil
	}

	// Calculate total rewards to mint
	totalRewardAmt := sdkmath.NewInt(int64(len(eligible))).Mul(reward.Amount)
	totalReward := sdk.NewCoins(sdk.NewCoin(types.DefaultRewardDenom, totalRewardAmt))

	// Mint total rewards to the oracle module account
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, totalReward); err != nil {
		return fmt.Errorf("failed to mint oracle rewards: %w", err)
	}

	paid := 0
	for _, info := range eligible {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, info.accountAddr, sdk.NewCoins(reward)); err != nil {
			k.Logger(ctx).Error("failed to send oracle reward", "validator", info.validatorAddr, "error", err)
			continue
		}
		paid++

		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeOracleRewardPaid,
				sdk.NewAttribute(types.AttributeKeyValidator, info.validatorAddr),
				sdk.NewAttribute(types.AttributeKeyRewardAddress, info.rewardAddr),
				sdk.NewAttribute(types.AttributeKeyRewardAmount, reward.String()),
			),
		)
	}

	// Burn the undistributed portion so failed sends don't leave stranded
	// coins in the oracle module account, which would inflate token supply
	// and compound into future reward calculations.
	if undistributed := len(eligible) - paid; undistributed > 0 {
		leftover := sdk.NewCoins(sdk.NewCoin(
			types.DefaultRewardDenom,
			sdkmath.NewInt(int64(undistributed)).Mul(reward.Amount),
		))
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, leftover); err != nil {
			k.Logger(ctx).Error("failed to burn undistributed oracle rewards",
				"undistributed_amount", leftover.String(), "error", err)
		}
	}

	return nil
}

// duplicateFeedDrop names a (validator, asset_pair) that was discarded
// because the validator submitted multiple feeds for the same asset in a
// single vote.
type duplicateFeedDrop struct {
	Validator string
	AssetPair string
}

// groupVotesByAsset groups validator votes by asset pair. Duplicate feeds
// from a single validator for the same asset are discarded; callers that
// need visibility into which were dropped should use groupVotesByAssetWithDrops.
func groupVotesByAsset(votes []*types.ValidatorVote) map[string][]priceVoteData {
	result, _ := groupVotesByAssetWithDrops(votes)
	return result
}

// groupVotesByAssetWithDrops is the diagnostic variant that also returns the
// list of duplicate feed drops, so operators can monitor misconfigured or
// malicious validators instead of silently losing data.
func groupVotesByAssetWithDrops(votes []*types.ValidatorVote) (map[string][]priceVoteData, []duplicateFeedDrop) {
	result := make(map[string][]priceVoteData)
	var drops []duplicateFeedDrop

	for _, vote := range votes {
		if vote == nil {
			continue
		}

		uniqueByAsset := make(map[string]priceVoteData)
		duplicateAssets := make(map[string]struct{})

		for _, feed := range vote.GetPriceFeeds() {
			if feed == nil {
				continue
			}
			asset := strings.TrimSpace(feed.AssetPair)
			if asset == "" {
				continue
			}
			priceDec, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(feed.Price))
			if err != nil || !priceDec.IsPositive() {
				continue
			}
			if _, dup := duplicateAssets[asset]; dup {
				drops = append(drops, duplicateFeedDrop{
					Validator: vote.GetValidatorAddress(),
					AssetPair: asset,
				})
				continue
			}
			if _, exists := uniqueByAsset[asset]; exists {
				delete(uniqueByAsset, asset)
				duplicateAssets[asset] = struct{}{}
				// Report both the original (now-dropped) entry and the
				// duplicate that triggered the drop, so the audit log
				// reflects every feed that was silently lost.
				drops = append(drops,
					duplicateFeedDrop{Validator: vote.GetValidatorAddress(), AssetPair: asset},
					duplicateFeedDrop{Validator: vote.GetValidatorAddress(), AssetPair: asset},
				)
				continue
			}
			uniqueByAsset[asset] = priceVoteData{
				validatorAddr: vote.GetValidatorAddress(),
				price:         priceDec,
			}
		}

		assets := make([]string, 0, len(uniqueByAsset))
		for asset := range uniqueByAsset {
			assets = append(assets, asset)
		}
		sort.Strings(assets)

		for _, asset := range assets {
			result[asset] = append(result[asset], uniqueByAsset[asset])
		}
	}

	return result, drops
}

// filterOutliers filters out prices that deviate too much from the median
func filterOutliers(prices []sdkmath.LegacyDec, median sdkmath.LegacyDec, maxDeviation sdkmath.LegacyDec) []sdkmath.LegacyDec {
	var filtered []sdkmath.LegacyDec

	// If median is zero or negative, cannot calculate meaningful deviation - return all prices.
	if !median.IsPositive() {
		return prices
	}

	for _, price := range prices {
		// Calculate percentage deviation from median
		diff := price.Sub(median).Abs()
		deviation := diff.Quo(median)

		if maxDeviation.IsZero() || deviation.LTE(maxDeviation) {
			filtered = append(filtered, price)
		}
	}

	return filtered
}

// filterOutlierVotes applies the same per-price outlier predicate as
// filterOutliers, but to votes — preserving each kept vote's validator
// address so reward distribution can target exactly the validators whose
// prices actually contributed to the aggregated median. Keeping this in
// lockstep with filterOutliers is what prevents outlier (potentially
// manipulative) validators from being rewarded for an aggregation they were
// excluded from.
func filterOutlierVotes(votes []priceVoteData, median sdkmath.LegacyDec, maxDeviation sdkmath.LegacyDec) []priceVoteData {
	if !median.IsPositive() {
		return votes
	}

	filtered := make([]priceVoteData, 0, len(votes))
	for _, v := range votes {
		deviation := v.price.Sub(median).Abs().Quo(median)
		if maxDeviation.IsZero() || deviation.LTE(maxDeviation) {
			filtered = append(filtered, v)
		}
	}

	return filtered
}

// sqrtDec calculates the square root of a LegacyDec using Newton's method
func sqrtDec(x sdkmath.LegacyDec) sdkmath.LegacyDec {
	if x.IsZero() {
		return sdkmath.LegacyZeroDec()
	}
	if x.IsNegative() {
		return sdkmath.LegacyZeroDec()
	}

	// Newton's method for square root
	// Start with initial guess
	guess := x.Quo(sdkmath.LegacyNewDec(2))
	if guess.IsZero() {
		// For very small x where x/2 underflows to zero in fixed-point,
		// use x as the initial guess to avoid division by zero.
		guess = x
	}
	precision := sdkmath.LegacyNewDecWithPrec(1, 9) // 1e-9 precision

	for i := 0; i < 20; i++ { // Max 20 iterations
		// next = (guess + x/guess) / 2
		next := guess.Add(x.Quo(guess)).Quo(sdkmath.LegacyNewDec(2))

		// Check convergence
		diff := next.Sub(guess).Abs()
		if diff.LT(precision) {
			return next
		}

		guess = next
	}

	return guess
}

// ValidateVote validates a validator's vote before accepting it
func (k Keeper) ValidateVote(ctx context.Context, vote *types.ValidatorVote) error {
	if vote == nil {
		return types.ErrUnauthorized.Wrap("nil vote")
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	// Check vote age
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	timestamp := vote.GetTimestamp()
	if timestamp.IsZero() {
		return types.ErrStaleVote.Wrap("vote timestamp missing")
	}
	ageDur := now.Sub(timestamp)

	// Reject future timestamps
	if ageDur < 0 {
		return types.ErrStaleVote.Wrapf("vote timestamp is in the future: %s (now: %s)", timestamp, now)
	}

	// Reject votes older than max age
	maxAge := params.GetMaxVoteAge()
	if maxAge > 0 && ageDur > time.Duration(maxAge)*time.Second {
		return types.ErrStaleVote.Wrapf("vote age %s exceeds max %ds", ageDur, maxAge)
	}

	// Validate each price feed
	seenAssetPairs := make(map[string]struct{}, len(vote.GetPriceFeeds()))
	for _, feed := range vote.GetPriceFeeds() {
		if feed == nil {
			return types.ErrInvalidPrice.Wrap("nil price feed")
		}
		assetPair := strings.TrimSpace(feed.AssetPair)
		if assetPair == "" {
			return types.ErrInvalidAssetPair.Wrap("empty asset pair")
		}
		if _, exists := seenAssetPairs[assetPair]; exists {
			return types.ErrInvalidVoteExtension.Wrapf("duplicate asset pair %s in vote", assetPair)
		}
		seenAssetPairs[assetPair] = struct{}{}

		priceDec, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(feed.Price))
		if err != nil || !priceDec.IsPositive() {
			return types.ErrInvalidPrice.Wrapf("invalid price for %s", assetPair)
		}

		// Check if asset pair is in allowed list
		found := false
		for _, allowed := range params.GetAssetPairs() {
			if assetPair == strings.TrimSpace(allowed) {
				found = true
				break
			}
		}
		if !found {
			return types.ErrInvalidAssetPair.Wrapf("asset pair %s not in allowed list", assetPair)
		}
	}

	return nil
}
