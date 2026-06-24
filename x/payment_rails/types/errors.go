package types

import (
	"cosmossdk.io/errors"
)

var (
	ErrInvalidRequest      = errors.Register(ModuleName, 2, "invalid request")
	ErrInvalidAmount       = errors.Register(ModuleName, 3, "invalid amount")
	ErrInvalidDenom        = errors.Register(ModuleName, 4, "invalid denom")
	ErrUnsupportedDenom    = errors.Register(ModuleName, 5, "unsupported denom")
	ErrUnauthorized        = errors.Register(ModuleName, 6, "unauthorized")
	ErrPaused              = errors.Register(ModuleName, 7, "conversions paused")
	ErrRateLimitExceeded   = errors.Register(ModuleName, 8, "rate limit exceeded")
	ErrOraclePriceNotFound = errors.Register(ModuleName, 9, "oracle price not found")
	ErrOracleStale         = errors.Register(ModuleName, 10, "oracle price stale")
	ErrOracleDeviation     = errors.Register(ModuleName, 11, "oracle price deviation exceeded")
	ErrSlippageExceeded    = errors.Register(ModuleName, 12, "slippage exceeded")
	ErrDuplicateRequest    = errors.Register(ModuleName, 13, "duplicate request")
	ErrDepositNotFound     = errors.Register(ModuleName, 14, "deposit not found")
	ErrWithdrawNotFound    = errors.Register(ModuleName, 15, "withdraw not found")
	ErrInvalidState        = errors.Register(ModuleName, 16, "invalid state transition")
	ErrInsufficientCredits = errors.Register(ModuleName, 17, "insufficient credits")
	ErrMinConfirmations    = errors.Register(ModuleName, 18, "insufficient confirmations")
	ErrPricingUnavailable  = errors.Register(ModuleName, 19, "pricing unavailable")
	ErrSettlementNotFound  = errors.Register(ModuleName, 20, "IBC settlement not found")
	ErrIBCChannelNotFound  = errors.Register(ModuleName, 21, "IBC channel not found")
	ErrIBCTimeout          = errors.Register(ModuleName, 22, "IBC packet timeout")
)
