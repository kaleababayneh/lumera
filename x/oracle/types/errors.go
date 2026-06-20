// Package types defines shared oracle error helpers and sentinels.
package types

import (
	"cosmossdk.io/errors"
)

// Sentinel errors for the oracle module
var (
	// ErrInvalidAssetPair is returned when an asset pair is invalid
	ErrInvalidAssetPair = errors.Register(ModuleName, 2, "invalid asset pair")

	// ErrInvalidPrice is returned when a price value is invalid
	ErrInvalidPrice = errors.Register(ModuleName, 3, "invalid price")

	// ErrPriceFeedNotFound is returned when a price feed is not found
	ErrPriceFeedNotFound = errors.Register(ModuleName, 4, "price feed not found")

	// ErrInsufficientVotes is returned when there are not enough validator votes
	ErrInsufficientVotes = errors.Register(ModuleName, 5, "insufficient validator votes")

	// ErrPriceDeviation is returned when a price deviates too much from median
	ErrPriceDeviation = errors.Register(ModuleName, 6, "price deviation exceeds threshold")

	// ErrStaleVote is returned when a vote is too old
	ErrStaleVote = errors.Register(ModuleName, 7, "vote is too old")

	// ErrInvalidVoteExtension is returned when a vote extension is invalid
	ErrInvalidVoteExtension = errors.Register(ModuleName, 8, "invalid vote extension")

	// ErrInvalidParameters is returned when module parameters are invalid
	ErrInvalidParameters = errors.Register(ModuleName, 9, "invalid parameters")

	// ErrUnauthorized is returned when a caller is not authorized
	ErrUnauthorized = errors.Register(ModuleName, 10, "unauthorized")

	// ErrInternalError is returned for internal errors
	ErrInternalError = errors.Register(ModuleName, 11, "internal error")

	// ErrInvalidRewardAddress is returned when a reward address is invalid.
	ErrInvalidRewardAddress = errors.Register(ModuleName, 12, "invalid reward address")
)
