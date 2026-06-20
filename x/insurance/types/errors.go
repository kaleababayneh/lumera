
package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// Module error codes
var (
	ErrInsufficientFunds             = sdkerrors.Register(ModuleName, 1100, "insufficient funds in insurance pool")
	ErrClaimNotFound                 = sdkerrors.Register(ModuleName, 1101, "claim not found")
	ErrClaimAlreadyResolved          = sdkerrors.Register(ModuleName, 1102, "claim already resolved")
	ErrInvalidClaimRequest           = sdkerrors.Register(ModuleName, 1103, "invalid claim request")
	ErrInvalidAmount                 = sdkerrors.Register(ModuleName, 1104, "invalid amount")
	ErrClaimWindowExpired            = sdkerrors.Register(ModuleName, 1105, "claim window expired")
	ErrDuplicateClaim                = sdkerrors.Register(ModuleName, 1106, "duplicate claim for receipt")
	ErrPoolUnavailable               = sdkerrors.Register(ModuleName, 1107, "insurance pool unavailable")
	ErrExceedsMaxClaim               = sdkerrors.Register(ModuleName, 1108, "claim exceeds maximum allowed percentage")
	ErrInvalidEvidence               = sdkerrors.Register(ModuleName, 1109, "invalid or insufficient evidence")
	ErrInvalidContribution           = sdkerrors.Register(ModuleName, 1110, "invalid contribution request")
	ErrInvalidPayout                 = sdkerrors.Register(ModuleName, 1111, "invalid payout request")
	ErrClaimAlreadyPaid              = sdkerrors.Register(ModuleName, 1112, "claim already paid")
	ErrClaimNotApproved              = sdkerrors.Register(ModuleName, 1113, "claim not approved for payout")
	ErrInvalidPublisher              = sdkerrors.Register(ModuleName, 1114, "invalid publisher")
	ErrInvalidReceipt                = sdkerrors.Register(ModuleName, 1115, "invalid receipt")
	ErrRateLimitExceeded             = sdkerrors.Register(ModuleName, 1116, "rate limit exceeded")
	ErrInvalidParameters             = sdkerrors.Register(ModuleName, 1117, "invalid parameters")
	ErrUnauthorized                  = sdkerrors.Register(ModuleName, 1118, "unauthorized")
	ErrInternalError                 = sdkerrors.Register(ModuleName, 1120, "internal error")
	ErrModuleAccountNotFound         = sdkerrors.Register(ModuleName, 1121, "module account not found")
	ErrClaimAlreadyProcessed         = sdkerrors.Register(ModuleName, 1122, "claim already processed")
	ErrInvalidClaimResolution        = sdkerrors.Register(ModuleName, 1123, "invalid claim resolution")
	ErrRateLimitCheckFailed          = sdkerrors.Register(ModuleName, 1124, "rate limit check failed")
	ErrClaimRateLimitExceeded        = sdkerrors.Register(ModuleName, 1125, "claim rate limit exceeded")
	ErrContributionRateLimitExceeded = sdkerrors.Register(ModuleName, 1126, "contribution rate limit exceeded")
	ErrGlobalClaimRateLimitExceeded  = sdkerrors.Register(ModuleName, 1127, "global claim rate limit exceeded")
)
