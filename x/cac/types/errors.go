//go:build cosmos
// +build cosmos

// Package types holds shared types and helpers for the CAC module.
//
//revive:disable:var-naming // Shared with other CAC module type helpers.
package types

import (
	"cosmossdk.io/errors"
)

// Module errors
var (
	ErrInvalidParams        = errors.Register(ModuleName, 2, "invalid parameters")
	ErrEntryNotFound        = errors.Register(ModuleName, 3, "cache entry not found")
	ErrEntryExpired         = errors.Register(ModuleName, 4, "cache entry expired")
	ErrContentTooLarge      = errors.Register(ModuleName, 5, "content exceeds maximum size")
	ErrInvalidContentHash   = errors.Register(ModuleName, 6, "invalid content hash")
	ErrInvalidRequestHash   = errors.Register(ModuleName, 7, "invalid request hash")
	ErrInvalidToolID        = errors.Register(ModuleName, 8, "invalid tool ID")
	ErrDuplicateEntry       = errors.Register(ModuleName, 9, "cache entry already exists")
	ErrInvalidTier          = errors.Register(ModuleName, 10, "invalid cache tier")
	ErrTierCapacityExceeded = errors.Register(ModuleName, 11, "cache tier capacity exceeded")
	ErrInvalidationFailed   = errors.Register(ModuleName, 12, "cache invalidation failed")
	ErrUnauthorized         = errors.Register(ModuleName, 13, "unauthorized operation")
	ErrRoyaltyFailed        = errors.Register(ModuleName, 14, "royalty distribution failed")
	ErrPromotionFailed      = errors.Register(ModuleName, 15, "tier promotion failed")
	ErrInvalidTTL           = errors.Register(ModuleName, 16, "invalid time-to-live value")
)
