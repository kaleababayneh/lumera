// Package types defines router module errors and helper utilities.
package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// x/router module sentinel errors
var (
	ErrInvalidParams     = sdkerrors.Register(ModuleName, 1100, "invalid parameters")
	ErrToolNotFound      = sdkerrors.Register(ModuleName, 1101, "tool not found")
	ErrSessionNotFound   = sdkerrors.Register(ModuleName, 1102, "session not found")
	ErrActiveSetFull     = sdkerrors.Register(ModuleName, 1103, "active set is full")
	ErrToolNotActive     = sdkerrors.Register(ModuleName, 1104, "tool is not active")
	ErrCooldownActive    = sdkerrors.Register(ModuleName, 1105, "cooldown period active")
	ErrInvalidAmount     = sdkerrors.Register(ModuleName, 1106, "invalid amount")
	ErrInvalidMetrics    = sdkerrors.Register(ModuleName, 1107, "invalid metrics")
	ErrDuplicateEntry    = sdkerrors.Register(ModuleName, 1108, "duplicate entry")
	ErrUnauthorized      = sdkerrors.Register(ModuleName, 1109, "unauthorized")
	ErrMetricsDisabled   = sdkerrors.Register(ModuleName, 1110, "metrics collection disabled")
	ErrCACDisabled       = sdkerrors.Register(ModuleName, 1111, "CAC royalties disabled")
	ErrPolicyNotFound    = sdkerrors.Register(ModuleName, 1112, "policy not found")
	ErrInvalidScore      = sdkerrors.Register(ModuleName, 1113, "invalid selection score")
	ErrAggregationFailed = sdkerrors.Register(ModuleName, 1114, "metrics aggregation failed")
	ErrInvalidAddress    = sdkerrors.Register(ModuleName, 1115, "invalid address")
)
