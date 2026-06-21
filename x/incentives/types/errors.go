// Package types holds shared types and helpers for the incentives module.
//
//revive:disable:var-naming // Shared with other incentives module type helpers.
package types

import (
	"cosmossdk.io/errors"
)

// Module errors
var (
	ErrInvalidParams          = errors.Register(ModuleName, 2, "invalid parameters")
	ErrBadgeNotFound          = errors.Register(ModuleName, 3, "badge not found")
	ErrTierConfigNotFound     = errors.Register(ModuleName, 4, "tier configuration not found")
	ErrInsufficientMetrics    = errors.Register(ModuleName, 5, "insufficient metrics for evaluation")
	ErrToolNotRegistered      = errors.Register(ModuleName, 6, "tool not registered")
	ErrUnauthorized           = errors.Register(ModuleName, 7, "unauthorized")
	ErrInvalidTierConfig      = errors.Register(ModuleName, 8, "invalid tier configuration")
	ErrBadgeExpired           = errors.Register(ModuleName, 9, "badge has expired")
	ErrEvaluationTooRecent    = errors.Register(ModuleName, 10, "evaluation requested too recently")
	ErrInvalidMetrics         = errors.Register(ModuleName, 11, "invalid metrics data")
	ErrBadgeAlreadyRevoked    = errors.Register(ModuleName, 12, "badge already revoked")
	ErrInvalidScore           = errors.Register(ModuleName, 13, "invalid score value")
	ErrPublisherMismatch      = errors.Register(ModuleName, 14, "publisher does not match tool owner")
	ErrGracePeriodActive      = errors.Register(ModuleName, 15, "grace period is active")
	ErrMetricSnapshotNotFound = errors.Register(ModuleName, 16, "metric snapshot not found")
)
