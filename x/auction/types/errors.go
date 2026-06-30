// Package types defines reusable error values for the auction module.
package types

import "cosmossdk.io/errors"

var (
	// ErrAuctionExists indicates a duplicate auction was detected for the request.
	ErrAuctionExists = errors.Register(ModuleName, 1100, "auction already exists for request")
	// ErrAuctionNotFound indicates an auction lookup failed.
	ErrAuctionNotFound = errors.Register(ModuleName, 1101, "auction not found")
	// ErrAuctionClosed indicates the auction is no longer accepting bids.
	ErrAuctionClosed = errors.Register(ModuleName, 1102, "auction already closed")
	// ErrBidTooExpensive indicates a bid exceeded the configured price ceiling.
	ErrBidTooExpensive = errors.Register(ModuleName, 1103, "bid exceeds maximum price")
	// ErrBidLatencyExceeded indicates a bid exceeded the latency target.
	ErrBidLatencyExceeded = errors.Register(ModuleName, 1104, "bid exceeds latency target")
	// ErrBidInvalidDenom indicates the bid denomination is not allowed.
	ErrBidInvalidDenom = errors.Register(ModuleName, 1105, "bid denom mismatch")
	// ErrBidDuplicate indicates the bidder already submitted an equivalent bid.
	ErrBidDuplicate = errors.Register(ModuleName, 1106, "bid already submitted by bidder")
	// ErrAuctionExpired indicates the auction timed out before settlement.
	ErrAuctionExpired = errors.Register(ModuleName, 1107, "auction has expired")
	// ErrNoBids indicates no bids were received before expiry.
	ErrNoBids = errors.Register(ModuleName, 1108, "no bids submitted")
	// ErrInvalidParams indicates malformed module parameters were supplied.
	ErrInvalidParams = errors.Register(ModuleName, 1109, "invalid auction parameters")
	// ErrInvalidBid indicates a bid failed structural validation.
	ErrInvalidBid = errors.Register(ModuleName, 1110, "invalid bid")
)
