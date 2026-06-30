package types

import (
	"fmt"
	"math"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const maxAuctionTTLSeconds = uint64(math.MaxInt64 / int64(time.Second))

// Params define configurable parameters for SpotCall auctions.
type Params struct {
	// CreditDenom is the denom bids must use (defaults to ulac).
	CreditDenom string `json:"credit_denom"`
	// DefaultAuctionTTLSeconds is the default lifetime for an auction from creation to auto-expiry.
	DefaultAuctionTTLSeconds uint64 `json:"default_auction_ttl_seconds"`
	// MaxActiveAuctions caps concurrent open auctions to guard against spam.
	MaxActiveAuctions uint32 `json:"max_active_auctions"`
	// MinBidDecrementBps enforces how much lower a bid must be versus the current best bid (in basis points).
	MinBidDecrementBps uint32 `json:"min_bid_decrement_bps"`
	// MaxBidLatencyMs bounds the latency a bidder may commit to.
	MaxBidLatencyMs uint32 `json:"max_bid_latency_ms"`
}

// DefaultParams returns sane defaults aligned with the economics spec.
func DefaultParams() Params {
	return Params{
		CreditDenom:              DefaultCreditDenom,
		DefaultAuctionTTLSeconds: 30,
		MaxActiveAuctions:        1024,
		MinBidDecrementBps:       100, // bids must be at least 1% cheaper than current best
		MaxBidLatencyMs:          5_000,
	}
}

// ValidateBasic performs basic parameter validation.
func (p Params) ValidateBasic() error {
	if err := sdk.ValidateDenom(p.CreditDenom); err != nil {
		return fmt.Errorf("invalid credit denom: %w", err)
	}
	if p.DefaultAuctionTTLSeconds == 0 {
		return fmt.Errorf("default auction TTL must be > 0")
	}
	if p.DefaultAuctionTTLSeconds > maxAuctionTTLSeconds {
		return fmt.Errorf("default auction TTL exceeds maximum safe duration")
	}
	if p.MaxActiveAuctions == 0 {
		return fmt.Errorf("max active auctions must be > 0")
	}
	if p.MinBidDecrementBps > 10_000 {
		return fmt.Errorf("min bid decrement cannot exceed 10000 bps")
	}
	if p.MaxBidLatencyMs == 0 {
		return fmt.Errorf("max bid latency must be > 0")
	}
	return nil
}

// AuctionTTL converts TTL into a duration.
func (p Params) AuctionTTL() time.Duration {
	return time.Duration(p.DefaultAuctionTTLSeconds) * time.Second //#nosec G115 -- TTL bounded by governance params
}
