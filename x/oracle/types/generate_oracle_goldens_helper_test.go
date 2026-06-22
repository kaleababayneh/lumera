//go:build cosmos && generate_goldens

package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bytes"

	"github.com/cosmos/gogoproto/jsonpb"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

// TestGenerateOracleGoldens is gated by the generate_goldens build tag.
// Run with: go test -tags='cosmos generate_goldens' -run TestGenerateOracleGoldens ./x/oracle/types/
func TestGenerateOracleGoldens(t *testing.T) {
	emit := func(filename string, msg proto.Message) {
		m := jsonpb.Marshaler{OrigName: true, EmitDefaults: false}
		var buf bytes.Buffer
		require.NoError(t, m.Marshal(&buf, msg))
		raw := buf.Bytes()
		var intermediate map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &intermediate))
		canonical, err := json.Marshal(intermediate)
		require.NoError(t, err)
		path := filepath.Join("testdata", filename)
		require.NoError(t, os.WriteFile(path, canonical, 0644))
		t.Logf("wrote %s (%d bytes)", path, len(canonical))
	}

	emit("price_feed_full.golden.json", &PriceFeed{
		AssetPair:       "BTC/USD",
		Price:           "42000.500000000000000000",
		Volume_24H:      "1000000000",
		Timestamp:       fixedOracleSnapshot,
		Sources:         []string{"coinbase", "binance", "kraken"},
		ConfidenceScore: "0.950000000000000000",
	})

	emit("price_feed_minimal.golden.json", &PriceFeed{
		AssetPair: "LAC/USD",
		Price:     "1.250000000000000000",
	})

	emit("query_price_feed_response.golden.json", &QueryPriceFeedResponse{
		PriceFeed: &PriceFeed{
			AssetPair:       "ETH/USD",
			Price:           "2500.750000000000000000",
			Volume_24H:      "500000000",
			Timestamp:       fixedOracleSnapshot,
			Sources:         []string{"binance", "coinbase"},
			ConfidenceScore: "0.920000000000000000",
		},
	})

	emit("query_all_price_feeds_response.golden.json", &QueryAllPriceFeedsResponse{
		PriceFeeds: []*PriceFeed{
			{
				AssetPair:       "BTC/USD",
				Price:           "42000.500000000000000000",
				Volume_24H:      "1000000000",
				Timestamp:       fixedOracleSnapshot,
				Sources:         []string{"binance", "coinbase", "kraken"},
				ConfidenceScore: "0.950000000000000000",
			},
			{
				AssetPair:       "ETH/USD",
				Price:           "2500.750000000000000000",
				Volume_24H:      "500000000",
				Timestamp:       fixedOracleSnapshot,
				Sources:         []string{"binance", "coinbase"},
				ConfidenceScore: "0.920000000000000000",
			},
			{
				AssetPair:       "LAC/USD",
				Price:           "1.250000000000000000",
				Volume_24H:      "10000000",
				Timestamp:       fixedOracleSnapshot,
				Sources:         []string{"kraken"},
				ConfidenceScore: "0.880000000000000000",
			},
		},
	})

	emit("aggregated_price.golden.json", &AggregatedPrice{
		AssetPair:         "BTC/USD",
		MedianPrice:       "42000.500000000000000000",
		MeanPrice:         "42001.250000000000000000",
		StandardDeviation: "12.500000000000000000",
		NumValidators:     7,
		BlockHeight:       1_000_000,
		Timestamp:         fixedOracleSnapshot,
	})

	emit("query_aggregated_price_response.golden.json", &QueryAggregatedPriceResponse{
		AggregatedPrice: &AggregatedPrice{
			AssetPair:         "ETH/USD",
			MedianPrice:       "2500.750000000000000000",
			MeanPrice:         "2501.100000000000000000",
			StandardDeviation: "5.250000000000000000",
			NumValidators:     5,
			BlockHeight:       1_000_000,
			Timestamp:         fixedOracleSnapshot,
		},
	})
}
