package types

import (
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuctionStatus enumerates lifecycle states for SpotCall auctions.
type AuctionStatus string

const (
	// AuctionStatusPending indicates an auction has been created but not yet opened.
	AuctionStatusPending AuctionStatus = "PENDING"
	// AuctionStatusActive marks an auction as accepting bids.
	AuctionStatusActive AuctionStatus = "ACTIVE"
	// AuctionStatusSettled marks an auction whose winning bid has been processed.
	AuctionStatusSettled AuctionStatus = "SETTLED"
	// AuctionStatusExpired denotes an auction that timed out without settlement.
	AuctionStatusExpired AuctionStatus = "EXPIRED"
	// AuctionStatusCanceled identifies auctions explicitly cancelled by router policy.
	AuctionStatusCanceled AuctionStatus = "CANCELED"
)

// MaxSpotCallIDLen caps persisted auction, request, tool, bid, and reserve IDs.
// These strings become collection keys and auction record fields; 256 matches
// sibling module persisted ID ceilings while leaving room for hashes/slugs.
const MaxSpotCallIDLen = 256

// SpotAuction represents a router initiated reverse auction for a single invocation.
type SpotAuction struct {
	ID                  string        `json:"id"`
	Owner               string        `json:"owner"`
	RequestID           string        `json:"request_id"`
	ToolID              string        `json:"tool_id"`
	PolicyID            string        `json:"policy_id"`
	MaxPrice            sdk.Coin      `json:"max_price"`
	MaxLatencyMs        uint32        `json:"max_latency_ms"`
	CreatedAt           time.Time     `json:"created_at"`
	ExpiresAt           time.Time     `json:"expires_at"`
	Status              AuctionStatus `json:"status"`
	PriorityTier        string        `json:"priority_tier"`
	PriorityDiscountBps uint32        `json:"priority_discount_bps"`
	BestBidID           string        `json:"best_bid_id"`
	BestBidPrice        sdk.Coin      `json:"best_bid_price"`
	BestBidLatencyMs    uint32        `json:"best_bid_latency_ms"`
	BestBidSubmittedAt  time.Time     `json:"best_bid_submitted_at"`
	ReserveCommitmentID string        `json:"reserve_commitment_id"`
	ReserveApplied      bool          `json:"reserve_applied"`
	WinnerBidID         string        `json:"winner_bid_id"`
}

// SpotBid represents a validator bid in a SpotCall auction.
type SpotBid struct {
	ID          string    `json:"id"`
	AuctionID   string    `json:"auction_id"`
	Bidder      string    `json:"bidder"`
	Price       sdk.Coin  `json:"price"`
	LatencyMs   uint32    `json:"latency_ms"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// CreateAuctionRequest encapsulates the inputs required to open an auction.
type CreateAuctionRequest struct {
	Owner        string
	RequestID    string
	ToolID       string
	PolicyID     string
	MaxPrice     sdk.Coin
	MaxLatencyMs uint32
}

// SubmitBidRequest encapsulates a bid submission.
type SubmitBidRequest struct {
	AuctionID string
	Bidder    string
	Price     sdk.Coin
	LatencyMs uint32
}

// ValidateBasic performs stateless validation of a spot auction payload.
func (a SpotAuction) ValidateBasic() error {
	if err := validateRequiredIdentifier("auction id", a.ID); err != nil {
		return err
	}
	if a.Owner == "" {
		return fmt.Errorf("owner cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(a.Owner); err != nil {
		return fmt.Errorf("invalid owner address: %w", err)
	}
	if err := validateRequiredIdentifier("request id", a.RequestID); err != nil {
		return err
	}
	if err := validateRequiredIdentifier("tool id", a.ToolID); err != nil {
		return err
	}
	if err := validatePolicyID(a.PolicyID); err != nil {
		return err
	}
	if err := validateCoin(a.MaxPrice); err != nil {
		return fmt.Errorf("invalid max price: %w", err)
	}
	if a.MaxLatencyMs == 0 {
		return fmt.Errorf("max latency must be > 0")
	}
	if a.CreatedAt.IsZero() {
		return fmt.Errorf("created at must be set")
	}
	if a.ExpiresAt.IsZero() || a.ExpiresAt.Before(a.CreatedAt) {
		return fmt.Errorf("expires at must be on or after created at")
	}
	if err := validateStatus(a.Status); err != nil {
		return err
	}
	if a.PriorityDiscountBps > 10_000 {
		return fmt.Errorf("priority discount exceeds 100%%")
	}
	if err := validateOptionalIdentifier("reserve commitment id", a.ReserveCommitmentID); err != nil {
		return err
	}
	if a.ReserveApplied && strings.TrimSpace(a.ReserveCommitmentID) == "" {
		return fmt.Errorf("reserve commitment id required when reserve applied")
	}
	if a.BestBidID == "" {
		if !coinIsUnsetOrZero(a.BestBidPrice) {
			return fmt.Errorf("best bid price must be zero when no bid set")
		}
	} else {
		if err := validateRequiredIdentifier("best bid id", a.BestBidID); err != nil {
			return err
		}
		if err := validateCoinNonNegative(a.BestBidPrice); err != nil {
			return fmt.Errorf("invalid best bid price: %w", err)
		}
		if a.BestBidLatencyMs == 0 {
			return fmt.Errorf("best bid latency must be > 0")
		}
		if a.BestBidSubmittedAt.IsZero() {
			return fmt.Errorf("best bid submitted at must be set")
		}
	}
	if err := validateOptionalIdentifier("winner bid id", a.WinnerBidID); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs validation on the bid fields.
func (b SpotBid) ValidateBasic() error {
	if err := validateRequiredIdentifier("bid id", b.ID); err != nil {
		return err
	}
	if err := validateRequiredIdentifier("auction id", b.AuctionID); err != nil {
		return err
	}
	if _, err := sdk.AccAddressFromBech32(b.Bidder); err != nil {
		return fmt.Errorf("invalid bidder address: %w", err)
	}
	if err := validateCoin(b.Price); err != nil {
		return fmt.Errorf("invalid bid price: %w", err)
	}
	if b.LatencyMs == 0 {
		return fmt.Errorf("latency must be > 0")
	}
	if b.SubmittedAt.IsZero() {
		return fmt.Errorf("submitted at must be set")
	}
	return nil
}

func validateRequiredIdentifier(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > MaxSpotCallIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", field, MaxSpotCallIDLen, len(value))
	}
	return nil
}

func validateOptionalIdentifier(field, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > MaxSpotCallIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", field, MaxSpotCallIDLen, len(value))
	}
	return nil
}

// IsExpired returns true if the provided time is past the auction expiry timestamp.
func (a SpotAuction) IsExpired(now time.Time) bool {
	return now.After(a.ExpiresAt)
}

// IsActive reports whether the auction is still open for bids at the given time.
func (a SpotAuction) IsActive(now time.Time) bool {
	return a.Status == AuctionStatusActive && !a.IsExpired(now)
}

func validateStatus(status AuctionStatus) error {
	switch status {
	case AuctionStatusPending, AuctionStatusActive, AuctionStatusSettled, AuctionStatusExpired, AuctionStatusCanceled:
		return nil
	default:
		return fmt.Errorf("invalid auction status: %s", status)
	}
}

func validateCoin(c sdk.Coin) error {
	if c.Amount.IsNil() {
		return fmt.Errorf("coin amount is nil")
	}
	if !c.IsValid() {
		return fmt.Errorf("coin invalid: %s", c.String())
	}
	if !c.Amount.IsPositive() {
		return fmt.Errorf("coin amount must be positive")
	}
	return nil
}

func validateCoinNonNegative(c sdk.Coin) error {
	if c.Amount.IsNil() {
		return fmt.Errorf("coin amount is nil")
	}
	if !c.IsValid() {
		return fmt.Errorf("coin invalid: %s", c.String())
	}
	if c.Amount.IsNegative() {
		return fmt.Errorf("coin amount cannot be negative")
	}
	return nil
}

func coinIsUnsetOrZero(c sdk.Coin) bool {
	if c.Amount.IsNil() {
		return strings.TrimSpace(c.Denom) == ""
	}
	return c.Amount.IsZero()
}

func validatePolicyID(policyID string) error {
	if policyID == "" {
		return nil
	}
	if err := validateOptionalIdentifier("policy id", policyID); err != nil {
		return err
	}
	// minimal validation: ensure contains version marker if present (id@version)
	if len(policyID) > 128 {
		return fmt.Errorf("policy id too long")
	}
	return nil
}

// BetterThan returns true if bid b is better (cheaper / lower latency) than other.
func (b SpotBid) BetterThan(other SpotBid) bool {
	if !other.Price.IsValid() || !other.Price.Amount.IsPositive() {
		return true
	}
	if !b.Price.IsValid() || !b.Price.Amount.IsPositive() {
		return false
	}
	if b.Price.Amount.LT(other.Price.Amount) {
		return true
	}
	if b.Price.Amount.GT(other.Price.Amount) {
		return false
	}
	// Tie-breaker: latency
	if b.LatencyMs < other.LatencyMs {
		return true
	}
	if b.LatencyMs > other.LatencyMs {
		return false
	}
	// Final tie-breaker: earliest submission wins
	return b.SubmittedAt.Before(other.SubmittedAt)
}
