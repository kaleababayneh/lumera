// Package types declares reserve module sentinel errors.
package types

import "cosmossdk.io/errors"

var (
	// ErrTierNotFound indicates the requested reserve tier does not exist.
	ErrTierNotFound = errors.Register(ModuleName, 1200, "reserve tier not found")
	// ErrInvalidCommitment surfaces when a commitment request fails validation.
	ErrInvalidCommitment = errors.Register(ModuleName, 1201, "invalid reserve commitment request")
	// ErrCommitmentNotFound is returned when a commitment lookup misses.
	ErrCommitmentNotFound = errors.Register(ModuleName, 1202, "reserve commitment not found")
	// ErrCommitmentExpired flags attempts to use an expired reserve commitment.
	ErrCommitmentExpired = errors.Register(ModuleName, 1203, "reserve commitment expired")
	// ErrInsufficientCapacity indicates a commitment lacks remaining capacity.
	ErrInsufficientCapacity = errors.Register(ModuleName, 1204, "reserve capacity insufficient")
	// ErrCreditDenomMismatch denotes a denom mismatch between params and request.
	ErrCreditDenomMismatch = errors.Register(ModuleName, 1205, "reserve denom mismatch")
)
