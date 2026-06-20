
package types

import (
	"fmt"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/shopspring/decimal"
)

const (
	// MaxAssetPairs caps the number of AssetPair entries governance
	// can set via Params. Each pair is iterated per-vote during
	// aggregation; realistic values are a handful of major pairs
	// (LAC/USD, ETH/USD, BTC/USD, plus a few more). 256 is ~20x
	// realistic and bounds both state bloat and per-block aggregation
	// work.
	MaxAssetPairs = 256
	// MaxAssetPairLen caps the length of each pair string. Real
	// values are short slugs like "LAC/USD" (7 bytes); 64 is
	// generous for longer derivative pair names.
	MaxAssetPairLen = 64
	// MaxDecimalStrLen bounds the VoteThreshold and MaxPriceDeviation
	// decimal-as-string fields. A real decimal is <20 chars; 64 gives
	// headroom without allowing megabyte payloads to reach
	// sdkmath.LegacyNewDecFromStr.
	MaxDecimalStrLen = 64
	// MaxVoteAgeSeconds is the largest max_vote_age that can be safely
	// converted with time.Duration(seconds) * time.Second.
	MaxVoteAgeSeconds = int64(1<<63-1) / int64(time.Second)
)

// Validate validates the params
func (p *Params) Validate() error {
	if p.VotePeriod <= 0 {
		return fmt.Errorf("vote period must be positive: %d", p.VotePeriod)
	}

	// Validate VoteThreshold
	if p.VoteThreshold == "" {
		return fmt.Errorf("vote threshold cannot be empty")
	}
	if len(p.VoteThreshold) > MaxDecimalStrLen {
		return fmt.Errorf("vote threshold exceeds %d-byte cap (got %d)",
			MaxDecimalStrLen, len(p.VoteThreshold))
	}
	threshold, err := sdkmath.LegacyNewDecFromStr(p.VoteThreshold)
	if err != nil {
		return fmt.Errorf("invalid vote threshold: %w", err)
	}
	if threshold.IsNegative() || threshold.GT(sdkmath.LegacyOneDec()) {
		return fmt.Errorf("vote threshold must be between 0 and 1: %s", p.VoteThreshold)
	}

	// Validate MaxPriceDeviation
	if p.MaxPriceDeviation == "" {
		return fmt.Errorf("max price deviation cannot be empty")
	}
	if len(p.MaxPriceDeviation) > MaxDecimalStrLen {
		return fmt.Errorf("max price deviation exceeds %d-byte cap (got %d)",
			MaxDecimalStrLen, len(p.MaxPriceDeviation))
	}
	deviation, err := sdkmath.LegacyNewDecFromStr(p.MaxPriceDeviation)
	if err != nil {
		return fmt.Errorf("invalid max price deviation: %w", err)
	}
	if deviation.IsNegative() {
		return fmt.Errorf("max price deviation cannot be negative: %s", p.MaxPriceDeviation)
	}

	// Validate AssetPairs
	if len(p.AssetPairs) == 0 {
		return fmt.Errorf("asset pairs cannot be empty")
	}
	if len(p.AssetPairs) > MaxAssetPairs {
		return fmt.Errorf("asset pairs exceeds %d-entry cap (got %d)",
			MaxAssetPairs, len(p.AssetPairs))
	}
	seenPairs := make(map[string]bool)
	for i, pair := range p.AssetPairs {
		trimmed := strings.TrimSpace(pair)
		if trimmed == "" {
			return fmt.Errorf("asset_pairs[%d] cannot be empty", i)
		}
		if pair != trimmed {
			return fmt.Errorf("asset_pairs[%d] must not have leading or trailing whitespace", i)
		}
		if len(pair) > MaxAssetPairLen {
			return fmt.Errorf("asset_pairs[%d] exceeds %d-byte cap (got %d)",
				i, MaxAssetPairLen, len(pair))
		}
		if seenPairs[trimmed] {
			return fmt.Errorf("duplicate asset pair: %s", trimmed)
		}
		seenPairs[trimmed] = true
	}

	if p.MaxVoteAge < 0 {
		return fmt.Errorf("max vote age cannot be negative: %d", p.MaxVoteAge)
	}
	if p.MaxVoteAge > MaxVoteAgeSeconds {
		return fmt.Errorf("max vote age cannot exceed %d seconds: %d", MaxVoteAgeSeconds, p.MaxVoteAge)
	}

	return nil
}

// DefaultParams returns default parameters
func DefaultParams() *Params {
	return &Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0.10",
		AssetPairs:        []string{"LAC/USD", "ETH/USD", "BTC/USD"},
		MaxVoteAge:        300,
	}
}

// DecimalFromStr helper to parse decimal strings safely
func DecimalFromStr(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	value, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, err
	}
	if !moneyguard.IsSafeExponent(value) {
		return decimal.Zero, fmt.Errorf("decimal magnitude out of range (exponent=%d)", value.Exponent())
	}
	return value, nil
}
