package types

import (
	"cosmossdk.io/errors"
)

// Sentinel errors for the passport module.
var (
	ErrPassportNotFound   = errors.Register(ModuleName, 2, "passport not found")
	ErrPassportExists     = errors.Register(ModuleName, 3, "passport already exists for this agent")
	ErrInsufficientStake  = errors.Register(ModuleName, 4, "stake amount below minimum")
	ErrInvalidAgentPubkey = errors.Register(ModuleName, 5, "invalid agent public key format")
	ErrPassportSuspended  = errors.Register(ModuleName, 6, "passport is suspended")
	ErrPassportRevoked    = errors.Register(ModuleName, 7, "passport is revoked")
	ErrUnauthorized       = errors.Register(ModuleName, 8, "unauthorized action")
	ErrInvalidStatus      = errors.Register(ModuleName, 9, "invalid passport status for operation")
	ErrSlashExceedsStake  = errors.Register(ModuleName, 10, "slash amount exceeds available stake")
	ErrInvalidPassportID  = errors.Register(ModuleName, 11, "invalid passport ID")
	ErrPassportNotActive  = errors.Register(ModuleName, 12, "passport is not active")
	ErrCannotReactivate   = errors.Register(ModuleName, 13, "passport cannot be reactivated")
)
