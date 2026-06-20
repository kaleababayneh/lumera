//go:build cosmos

package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Golden artifact tests for x/oracle price-feed response JSON wire format.
//
// Why this matters
// ----------------
// Oracle price-feed responses are consumed by external systems over gRPC
// and REST:
//   - DEX frontends quoting live prices to traders
//   - On-chain dApps computing liquidation thresholds
//   - Block explorers displaying oracle health
//   - Data pipelines feeding external risk models
//   - Third-party indexers (Numia, Mintscan) archiving price history
//
// None of these consumers share code with the oracle module. They parse
// JSON field-by-field against the documented schema. Any silent rename,
// reorder, or omit-empty flip in PriceFeed / AggregatedPrice /
// QueryPriceFeedResponse would break every external dashboard reading
// Lumera prices.
//
// This file freezes:
//   - Field names on the wire (snake_case from proto definitions)
//   - Value serialization (decimal precision, timestamp format)
//   - Response envelope shape (QueryPriceFeedResponse wraps PriceFeed etc.)
//   - Multi-feed ordering and array encoding

// fixedOracleSnapshot is a deterministic timestamp used across goldens
// so timestamp value stability is testable.
var fixedOracleSnapshot = time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)

// marshalCanonicalJSON produces byte-comparable JSON from a proto
// message by round-tripping protojson output through encoding/json,
// which sorts map keys lexicographically. This removes protojson's
// intentional non-determinism (random whitespace injected to deter
// byte-stability assumptions) while preserving all field content.
func marshalCanonicalJSON(t *testing.T, msg proto.Message) []byte {
	t.Helper()

	opts := protojson.MarshalOptions{
		UseProtoNames:   true, // emit snake_case field names matching proto defs
		EmitUnpopulated: false,
	}
	raw, err := opts.Marshal(msg)
	require.NoError(t, err)

	var intermediate map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &intermediate))

	canonical, err := json.Marshal(intermediate)
	require.NoError(t, err)
	return canonical
}

func loadOracleGolden(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file %s", path)
	return data
}

// assertOracleResponseMatchesGolden compares a proto message's canonical
// JSON against a golden file via structural JSON equality.
func assertOracleResponseMatchesGolden(t *testing.T, msg proto.Message, goldenFile string) {
	t.Helper()

	got := marshalCanonicalJSON(t, msg)
	want := loadOracleGolden(t, goldenFile)

	var gotObj, wantObj interface{}
	require.NoError(t, json.Unmarshal(got, &gotObj))
	require.NoError(t, json.Unmarshal(want, &wantObj))

	require.Equal(t, wantObj, gotObj,
		"oracle response wire format drift in %s — a change here "+
			"breaks every external consumer (dashboards, indexers, "+
			"dApps) that parses oracle JSON. Review the diff carefully "+
			"before regenerating the golden.",
		goldenFile)
}

// TestPriceFeed_WireFormat_FullFields pins the canonical PriceFeed JSON
// with every field populated. This is what a DEX frontend or block
// explorer parses against the documented oracle schema.
func TestPriceFeed_WireFormat_FullFields(t *testing.T) {
	t.Parallel()

	feed := &PriceFeed{
		AssetPair:       "BTC/USD",
		Price:           "42000.500000000000000000",
		Volume_24H:      "1000000000",
		Timestamp:       timestamppb.New(fixedOracleSnapshot),
		Sources:         []string{"coinbase", "binance", "kraken"},
		ConfidenceScore: "0.950000000000000000",
	}

	assertOracleResponseMatchesGolden(t, feed, "price_feed_full.golden.json")
}

// TestPriceFeed_WireFormat_MinimalFields pins the minimal PriceFeed
// shape (asset_pair + price only). Catches regressions where empty
// optional fields flip from omitted to emitted.
func TestPriceFeed_WireFormat_MinimalFields(t *testing.T) {
	t.Parallel()

	feed := &PriceFeed{
		AssetPair: "LAC/USD",
		Price:     "1.250000000000000000",
	}

	assertOracleResponseMatchesGolden(t, feed, "price_feed_minimal.golden.json")
}

// TestQueryPriceFeedResponse_WireFormat pins the gRPC/REST response
// envelope for a single price-feed query. Consumers filter on
// `price_feed` key at the top level — renaming breaks them.
func TestQueryPriceFeedResponse_WireFormat(t *testing.T) {
	t.Parallel()

	resp := &QueryPriceFeedResponse{
		PriceFeed: &PriceFeed{
			AssetPair:       "ETH/USD",
			Price:           "2500.750000000000000000",
			Volume_24H:      "500000000",
			Timestamp:       timestamppb.New(fixedOracleSnapshot),
			Sources:         []string{"binance", "coinbase"},
			ConfidenceScore: "0.920000000000000000",
		},
	}

	assertOracleResponseMatchesGolden(t, resp, "query_price_feed_response.golden.json")
}

// TestQueryAllPriceFeedsResponse_WireFormat pins the list-response
// envelope. Catches regressions in array encoding and multi-feed
// ordering.
func TestQueryAllPriceFeedsResponse_WireFormat(t *testing.T) {
	t.Parallel()

	resp := &QueryAllPriceFeedsResponse{
		PriceFeeds: []*PriceFeed{
			{
				AssetPair:       "BTC/USD",
				Price:           "42000.500000000000000000",
				Volume_24H:      "1000000000",
				Timestamp:       timestamppb.New(fixedOracleSnapshot),
				Sources:         []string{"binance", "coinbase", "kraken"},
				ConfidenceScore: "0.950000000000000000",
			},
			{
				AssetPair:       "ETH/USD",
				Price:           "2500.750000000000000000",
				Volume_24H:      "500000000",
				Timestamp:       timestamppb.New(fixedOracleSnapshot),
				Sources:         []string{"binance", "coinbase"},
				ConfidenceScore: "0.920000000000000000",
			},
			{
				AssetPair:       "LAC/USD",
				Price:           "1.250000000000000000",
				Volume_24H:      "10000000",
				Timestamp:       timestamppb.New(fixedOracleSnapshot),
				Sources:         []string{"kraken"},
				ConfidenceScore: "0.880000000000000000",
			},
		},
	}

	assertOracleResponseMatchesGolden(t, resp, "query_all_price_feeds_response.golden.json")
}

// TestAggregatedPrice_WireFormat pins the AggregatedPrice JSON — the
// cross-validator consensus price that downstream risk and liquidation
// logic depends on.
func TestAggregatedPrice_WireFormat(t *testing.T) {
	t.Parallel()

	agg := &AggregatedPrice{
		AssetPair:         "BTC/USD",
		MedianPrice:       "42000.500000000000000000",
		MeanPrice:         "42001.250000000000000000",
		StandardDeviation: "12.500000000000000000",
		NumValidators:     7,
		BlockHeight:       1_000_000,
		Timestamp:         timestamppb.New(fixedOracleSnapshot),
	}

	assertOracleResponseMatchesGolden(t, agg, "aggregated_price.golden.json")
}

// TestQueryAggregatedPriceResponse_WireFormat pins the query envelope
// for the aggregated-price endpoint.
func TestQueryAggregatedPriceResponse_WireFormat(t *testing.T) {
	t.Parallel()

	resp := &QueryAggregatedPriceResponse{
		AggregatedPrice: &AggregatedPrice{
			AssetPair:         "ETH/USD",
			MedianPrice:       "2500.750000000000000000",
			MeanPrice:         "2501.100000000000000000",
			StandardDeviation: "5.250000000000000000",
			NumValidators:     5,
			BlockHeight:       1_000_000,
			Timestamp:         timestamppb.New(fixedOracleSnapshot),
		},
	}

	assertOracleResponseMatchesGolden(t, resp, "query_aggregated_price_response.golden.json")
}

// TestPriceFeed_WireContract_FieldNames pins the exact JSON field names
// for PriceFeed. Every one is part of the external contract with
// dashboards and indexers.
func TestPriceFeed_WireContract_FieldNames(t *testing.T) {
	t.Parallel()

	feed := &PriceFeed{
		AssetPair:       "BTC/USD",
		Price:           "42000",
		Volume_24H:      "1000",
		Timestamp:       timestamppb.New(fixedOracleSnapshot),
		Sources:         []string{"kraken"},
		ConfidenceScore: "0.9",
	}

	raw := marshalCanonicalJSON(t, feed)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	requiredFields := []string{
		"asset_pair",
		"price",
		"volume_24h",
		"timestamp",
		"sources",
		"confidence_score",
	}
	for _, f := range requiredFields {
		require.Contains(t, obj, f,
			"PriceFeed field %q missing from wire — rename breaks every "+
				"external consumer filtering oracle JSON by this key", f)
	}
}

// TestAggregatedPrice_WireContract_FieldNames pins the AggregatedPrice
// field names.
func TestAggregatedPrice_WireContract_FieldNames(t *testing.T) {
	t.Parallel()

	agg := &AggregatedPrice{
		AssetPair:         "BTC/USD",
		MedianPrice:       "42000",
		MeanPrice:         "42000",
		StandardDeviation: "0",
		NumValidators:     1,
		BlockHeight:       1,
		Timestamp:         timestamppb.New(fixedOracleSnapshot),
	}

	raw := marshalCanonicalJSON(t, agg)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	requiredFields := []string{
		"asset_pair",
		"median_price",
		"mean_price",
		"standard_deviation",
		"num_validators",
		"block_height",
		"timestamp",
	}
	for _, f := range requiredFields {
		require.Contains(t, obj, f,
			"AggregatedPrice field %q missing from wire — rename breaks "+
				"risk engines and liquidation logic parsing consensus prices", f)
	}
}

// TestPriceFeed_WireContract_OmitEmpty pins the omit-empty behavior for
// optional fields. Flipping this semantics would cause consumers that
// distinguish "missing" from "present-but-empty" to drop values.
func TestPriceFeed_WireContract_OmitEmpty(t *testing.T) {
	t.Parallel()

	// Minimal feed: only required fields populated.
	feed := &PriceFeed{
		AssetPair: "LAC/USD",
		Price:     "1.0",
	}

	raw := marshalCanonicalJSON(t, feed)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	// These optional fields must NOT appear in the JSON when empty.
	omittedFields := []string{
		"volume_24h",
		"timestamp",
		"sources",
		"confidence_score",
	}
	for _, f := range omittedFields {
		_, present := obj[f]
		require.False(t, present,
			"empty optional PriceFeed field %q serialized when it should "+
				"be omitted — changing omit-empty semantics is a wire "+
				"format change that breaks external consumers", f)
	}
}

// TestQueryResponse_WireContract_EnvelopeKeys pins the top-level keys
// for the three price-feed query responses.
func TestQueryResponse_WireContract_EnvelopeKeys(t *testing.T) {
	t.Parallel()

	t.Run("query_price_feed_response", func(t *testing.T) {
		t.Parallel()
		resp := &QueryPriceFeedResponse{
			PriceFeed: &PriceFeed{AssetPair: "BTC/USD", Price: "1"},
		}
		raw := marshalCanonicalJSON(t, resp)
		var obj map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &obj))
		require.Contains(t, obj, "price_feed",
			"QueryPriceFeedResponse envelope key renamed — breaks REST "+
				"clients that dereference response.price_feed")
	})

	t.Run("query_all_price_feeds_response", func(t *testing.T) {
		t.Parallel()
		resp := &QueryAllPriceFeedsResponse{
			PriceFeeds: []*PriceFeed{
				{AssetPair: "BTC/USD", Price: "1"},
			},
		}
		raw := marshalCanonicalJSON(t, resp)
		var obj map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &obj))
		require.Contains(t, obj, "price_feeds",
			"QueryAllPriceFeedsResponse envelope key renamed — breaks "+
				"REST clients iterating response.price_feeds[]")
	})

	t.Run("query_aggregated_price_response", func(t *testing.T) {
		t.Parallel()
		resp := &QueryAggregatedPriceResponse{
			AggregatedPrice: &AggregatedPrice{AssetPair: "BTC/USD", MedianPrice: "1"},
		}
		raw := marshalCanonicalJSON(t, resp)
		var obj map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &obj))
		require.Contains(t, obj, "aggregated_price",
			"QueryAggregatedPriceResponse envelope key renamed — breaks "+
				"REST clients dereferencing response.aggregated_price")
	})
}

// TestGoldenFiles_AllPresent enforces coverage — all referenced goldens
// must exist on disk.
func TestGoldenFiles_AllPresent(t *testing.T) {
	t.Parallel()

	requiredGoldens := []string{
		"price_feed_full.golden.json",
		"price_feed_minimal.golden.json",
		"query_price_feed_response.golden.json",
		"query_all_price_feeds_response.golden.json",
		"aggregated_price.golden.json",
		"query_aggregated_price_response.golden.json",
	}

	for _, f := range requiredGoldens {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required oracle golden file %s is missing", f)
	}
}
