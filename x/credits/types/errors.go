
// Package types holds shared types and helpers for the credits module.
//
//revive:disable:var-naming // Shared with other credits module type helpers.
package types

import (
	"cosmossdk.io/errors"
)

// Module errors
var (
	ErrInvalidParams     = errors.Register(ModuleName, 2, "invalid parameters")
	ErrInsufficientFunds = errors.Register(ModuleName, 3, "insufficient funds")
	ErrLockNotFound      = errors.Register(ModuleName, 4, "lock not found")
	ErrLockExpired       = errors.Register(ModuleName, 5, "lock expired")
	ErrLockInactive      = errors.Register(ModuleName, 6, "lock is not active")
	ErrSettlementFailed  = errors.Register(ModuleName, 7, "settlement failed")
	ErrDisputeFailed     = errors.Register(ModuleName, 8, "dispute failed")
	ErrReleaseFailed     = errors.Register(ModuleName, 9, "release failed")
)
