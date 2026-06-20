//go:build cosmos

package keeper

import (
	"strings"
	"testing"
	"time"
)

// Tests for the security-critical CanonicalKey primitive and its
// building blocks: writeProviderCanonicalPart, OracleProviderSample
// Validate, parseProviderDec, DefaultProviderAggregationConfig.
//
// CanonicalKey is used for duplicate detection and surfaces in
// diagnostics keyed off the stable identity of a sample. Any
// ambiguity in its encoding lets an attacker craft two distinct
// samples that hash to the same canonical key — collapsing them at
// dedup and potentially bypassing sample-count-based rate limits.
// The encoding is length-prefixed ("|<len>:<value>") specifically
// to prevent injection across field boundaries. These tests pin:
//
//   CanonicalKey
//     • deterministic (same sample → same key every time)
//     • reflects ProviderID, AssetPair, ObservedAt, Price
//     • IGNORES Volume24H, ConfidenceScore, Signature
//       (these are not part of the canonical identity)
//     • trims leading/trailing whitespace on ProviderID/AssetPair/Price
//     • does NOT trim ObservedAt (formatted via RFC3339Nano)
//     • injection-resistant: moving a "|N:" prefix from one field into
//       another cannot produce the same output
//
//   writeProviderCanonicalPart
//     • builds "|<len>:<value>" exactly
//     • empty value becomes "|0:"
//     • accepts values containing "|", ":", and digits without ambiguity
//
//   OracleProviderSample.Validate
//     • empty ProviderID / AssetPair → error
//     • empty Price → parseProviderDec returns "price missing"
//     • non-positive price rejected
//     • empty Volume24H allowed (skipped)
//     • negative Volume24H rejected
//     • confidence < 0 or > 1 rejected (inclusive 0 and 1)
//     • zero ObservedAt rejected
//     • ObservedAt > snapshotTime rejected (only when snapshot non-zero)
//
//   parseProviderDec
//     • empty input → "{field} missing"
//     • malformed → "invalid {field}: ..."
//     • valid → parsed, no error
//
//   DefaultProviderAggregationConfig
//     • MaxProviders == defaultMaxProviderInputs (8)
//     • AggregationMethod == median

// ---------------------------------------------------------------------------
// writeProviderCanonicalPart
// ---------------------------------------------------------------------------

func TestWriteProviderCanonicalPart_Shape(t *testing.T) {
	var b strings.Builder
	writeProviderCanonicalPart(&b, "hello")
	if got := b.String(); got != "|5:hello" {
		t.Errorf("got %q; want %q", got, "|5:hello")
	}
}

func TestWriteProviderCanonicalPart_Empty(t *testing.T) {
	var b strings.Builder
	writeProviderCanonicalPart(&b, "")
	if got := b.String(); got != "|0:" {
		t.Errorf("got %q; want %q", got, "|0:")
	}
}

func TestWriteProviderCanonicalPart_HandlesPipeAndColon(t *testing.T) {
	// Values containing "|" and ":" are not escaped — the length
	// prefix disambiguates them. Regression guard: a refactor that
	// tried to escape or replace pipes/colons would break
	// collision-resistance.
	var b strings.Builder
	writeProviderCanonicalPart(&b, "a|b:c")
	if got := b.String(); got != "|5:a|b:c" {
		t.Errorf("got %q; want %q", got, "|5:a|b:c")
	}
}

// ---------------------------------------------------------------------------
// CanonicalKey
// ---------------------------------------------------------------------------

func TestCanonicalKey_Deterministic(t *testing.T) {
	s := OracleProviderSample{
		ProviderID: "coinbase",
		AssetPair:  "BTC/USD",
		ObservedAt: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		Price:      "65000",
	}
	if s.CanonicalKey() != s.CanonicalKey() {
		t.Error("CanonicalKey is non-deterministic")
	}
}

func TestCanonicalKey_ReflectsAllCanonicalFields(t *testing.T) {
	base := OracleProviderSample{
		ProviderID: "coinbase",
		AssetPair:  "BTC/USD",
		ObservedAt: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		Price:      "65000",
	}
	baseKey := base.CanonicalKey()

	// Changing each of the four canonical fields must change the key.
	providerChanged := base
	providerChanged.ProviderID = "binance"
	if providerChanged.CanonicalKey() == baseKey {
		t.Error("CanonicalKey unchanged after ProviderID change")
	}

	assetChanged := base
	assetChanged.AssetPair = "ETH/USD"
	if assetChanged.CanonicalKey() == baseKey {
		t.Error("CanonicalKey unchanged after AssetPair change")
	}

	timeChanged := base
	timeChanged.ObservedAt = base.ObservedAt.Add(time.Second)
	if timeChanged.CanonicalKey() == baseKey {
		t.Error("CanonicalKey unchanged after ObservedAt change")
	}

	priceChanged := base
	priceChanged.Price = "65001"
	if priceChanged.CanonicalKey() == baseKey {
		t.Error("CanonicalKey unchanged after Price change")
	}
}

func TestCanonicalKey_IgnoresNonCanonicalFields(t *testing.T) {
	// Volume, ConfidenceScore, and Signature are NOT part of the
	// identity. Two samples differing only in these fields must
	// produce the same key — the key represents "which observation
	// is this?", not "what metadata did the provider attach?".
	base := OracleProviderSample{
		ProviderID: "coinbase",
		AssetPair:  "BTC/USD",
		ObservedAt: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		Price:      "65000",
	}

	withVolume := base
	withVolume.Volume24H = "1000000"
	if withVolume.CanonicalKey() != base.CanonicalKey() {
		t.Error("CanonicalKey changed due to Volume24H; must ignore")
	}

	withConfidence := base
	withConfidence.ConfidenceScore = "0.95"
	if withConfidence.CanonicalKey() != base.CanonicalKey() {
		t.Error("CanonicalKey changed due to ConfidenceScore; must ignore")
	}

	withSignature := base
	withSignature.Signature = "deadbeef"
	if withSignature.CanonicalKey() != base.CanonicalKey() {
		t.Error("CanonicalKey changed due to Signature; must ignore")
	}
}

func TestCanonicalKey_TrimsWhitespaceOnStringFields(t *testing.T) {
	clean := OracleProviderSample{
		ProviderID: "coinbase",
		AssetPair:  "BTC/USD",
		ObservedAt: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		Price:      "65000",
	}
	padded := OracleProviderSample{
		ProviderID: "  coinbase  ",
		AssetPair:  "\tBTC/USD\n",
		ObservedAt: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		Price:      "  65000  ",
	}
	if clean.CanonicalKey() != padded.CanonicalKey() {
		t.Errorf("whitespace-padded sample did not produce same key.\nclean=%q\npadded=%q",
			clean.CanonicalKey(), padded.CanonicalKey())
	}
}

func TestCanonicalKey_InjectionResistant(t *testing.T) {
	// Security invariant: an attacker who controls field contents
	// cannot craft two distinct samples that produce the same
	// CanonicalKey. The length-prefixed encoding prevents moving
	// data across field boundaries.
	//
	// Without length-prefixing, ProviderID="a" + AssetPair="|b:c"
	// might look like ProviderID="a|b" + AssetPair="c" after
	// concatenation. The "|<len>:" prefix forces the reader to
	// consume exactly <len> bytes, which would then fail the shape
	// check. Here we pin that the resulting keys differ.
	ts := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	sample1 := OracleProviderSample{
		ProviderID: "a",
		AssetPair:  "|1:b",
		ObservedAt: ts,
		Price:      "1",
	}
	sample2 := OracleProviderSample{
		ProviderID: "a|1:b",
		AssetPair:  "",
		ObservedAt: ts,
		Price:      "1",
	}
	if sample1.CanonicalKey() == sample2.CanonicalKey() {
		t.Errorf("injection collision: key=%q", sample1.CanonicalKey())
	}
}

// ---------------------------------------------------------------------------
// OracleProviderSample.Validate
// ---------------------------------------------------------------------------

func validSample() OracleProviderSample {
	return OracleProviderSample{
		ProviderID:      "coinbase",
		AssetPair:       "BTC/USD",
		ObservedAt:      time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		Price:           "65000",
		Volume24H:       "1000000",
		ConfidenceScore: "0.95",
	}
}

func TestValidate_HappyPath(t *testing.T) {
	s := validSample()
	if err := s.Validate(time.Time{}); err != nil {
		t.Errorf("valid sample rejected: %v", err)
	}
}

func TestValidate_EmptyProviderID(t *testing.T) {
	s := validSample()
	s.ProviderID = "   "
	err := s.Validate(time.Time{})
	if err == nil || !strings.Contains(err.Error(), "provider id") {
		t.Errorf("expected provider id error, got %v", err)
	}
}

func TestValidate_EmptyAssetPair(t *testing.T) {
	s := validSample()
	s.AssetPair = ""
	err := s.Validate(time.Time{})
	if err == nil || !strings.Contains(err.Error(), "asset pair") {
		t.Errorf("expected asset pair error, got %v", err)
	}
}

func TestValidate_EmptyPrice(t *testing.T) {
	s := validSample()
	s.Price = ""
	err := s.Validate(time.Time{})
	if err == nil || !strings.Contains(err.Error(), "price") {
		t.Errorf("expected price missing error, got %v", err)
	}
}

func TestValidate_NonPositivePriceRejected(t *testing.T) {
	cases := []string{"0", "-1", "-0.001"}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			s := validSample()
			s.Price = p
			err := s.Validate(time.Time{})
			if err == nil || !strings.Contains(err.Error(), "positive") {
				t.Errorf("expected positive-price error for %q, got %v", p, err)
			}
		})
	}
}

func TestValidate_EmptyVolumeAllowed(t *testing.T) {
	// Empty Volume24H is the "not reported" signal and must pass.
	s := validSample()
	s.Volume24H = ""
	if err := s.Validate(time.Time{}); err != nil {
		t.Errorf("empty volume rejected: %v", err)
	}
}

func TestValidate_NegativeVolumeRejected(t *testing.T) {
	s := validSample()
	s.Volume24H = "-1"
	err := s.Validate(time.Time{})
	if err == nil || !strings.Contains(err.Error(), "volume") {
		t.Errorf("expected volume error, got %v", err)
	}
}

func TestValidate_ConfidenceBounds(t *testing.T) {
	cases := []struct {
		conf    string
		wantErr bool
	}{
		{"0", false},     // lower bound inclusive
		{"1", false},     // upper bound inclusive
		{"0.5", false},   // middle
		{"-0.01", true},  // below
		{"1.01", true},   // above
	}
	for _, tc := range cases {
		t.Run(tc.conf, func(t *testing.T) {
			s := validSample()
			s.ConfidenceScore = tc.conf
			err := s.Validate(time.Time{})
			if tc.wantErr && err == nil {
				t.Errorf("expected error for confidence=%q", tc.conf)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for confidence=%q: %v", tc.conf, err)
			}
		})
	}
}

func TestValidate_ZeroObservedAtRejected(t *testing.T) {
	s := validSample()
	s.ObservedAt = time.Time{}
	err := s.Validate(time.Time{})
	if err == nil || !strings.Contains(err.Error(), "observed_at") {
		t.Errorf("expected observed_at required error, got %v", err)
	}
}

func TestValidate_FutureObservedAtRejectedAgainstSnapshot(t *testing.T) {
	s := validSample()
	snapshot := s.ObservedAt.Add(-time.Minute) // snapshot is earlier than observed
	err := s.Validate(snapshot)
	if err == nil || !strings.Contains(err.Error(), "after snapshot time") {
		t.Errorf("expected future-observed-at error, got %v", err)
	}
}

func TestValidate_FutureObservedAtOKWhenSnapshotZero(t *testing.T) {
	// When the snapshot is zero time, no upper-bound check runs —
	// validation is "timestamp present and sane", not "fresh".
	s := validSample()
	s.ObservedAt = time.Now().Add(24 * time.Hour)
	if err := s.Validate(time.Time{}); err != nil {
		t.Errorf("unexpected error with zero snapshot: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseProviderDec
// ---------------------------------------------------------------------------

func TestParseProviderDec_EmptyGivesFieldNamedError(t *testing.T) {
	_, err := parseProviderDec("price", "")
	if err == nil {
		t.Fatal("expected error on empty input")
	}
	if !strings.Contains(err.Error(), "price missing") {
		t.Errorf("error %q should contain 'price missing' (field name preserved)", err)
	}
}

func TestParseProviderDec_WhitespaceOnlyTreatedAsEmpty(t *testing.T) {
	_, err := parseProviderDec("volume_24h", "   \t\n")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("whitespace-only input should error as missing, got %v", err)
	}
}

func TestParseProviderDec_MalformedGivesFieldNamedError(t *testing.T) {
	_, err := parseProviderDec("confidence_score", "not-a-number")
	if err == nil {
		t.Fatal("expected error on malformed input")
	}
	if !strings.Contains(err.Error(), "invalid confidence_score") {
		t.Errorf("error %q should contain 'invalid confidence_score'", err)
	}
}

func TestParseProviderDec_ValidParsed(t *testing.T) {
	v, err := parseProviderDec("price", "42.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.Equal(v) || v.IsNil() {
		t.Errorf("unexpected Dec value: %s", v)
	}
	// Spot check: 42.5 parses exactly.
	if v.String() != "42.500000000000000000" {
		t.Errorf("got %s; want 42.500000000000000000", v.String())
	}
}

// ---------------------------------------------------------------------------
// DefaultProviderAggregationConfig
// ---------------------------------------------------------------------------

func TestDefaultProviderAggregationConfig_Values(t *testing.T) {
	cfg := DefaultProviderAggregationConfig()
	if cfg.MaxProviders != defaultMaxProviderInputs {
		t.Errorf("MaxProviders=%d; want %d", cfg.MaxProviders, defaultMaxProviderInputs)
	}
	if cfg.AggregationMethod != ProviderAggregationMedian {
		t.Errorf("AggregationMethod=%q; want %q", cfg.AggregationMethod, ProviderAggregationMedian)
	}
	// ProviderConfigs map is intentionally nil — zero-providers
	// means "accept any provider, bypass allowlist". Regression
	// guard: a refactor that initialized this to empty map would
	// flip enforcement semantics (empty map != nil in the caller's
	// len-check paths).
	if cfg.ProviderConfigs != nil {
		t.Errorf("ProviderConfigs=%v; want nil (allowlist disabled by default)", cfg.ProviderConfigs)
	}
}
