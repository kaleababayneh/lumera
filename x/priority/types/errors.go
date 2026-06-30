package types

import "cosmossdk.io/errors"

var (
	// ErrPriorityTierNotFound indicates that an expected priority tier configuration was missing.
	ErrPriorityTierNotFound = errors.Register(ModuleName, 1300, "priority tier not found")
	// ErrInvalidAssignment signals malformed priority assignment payloads.
	ErrInvalidAssignment = errors.Register(ModuleName, 1301, "invalid priority assignment")
)
