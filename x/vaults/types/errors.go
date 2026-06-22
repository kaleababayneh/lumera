package types

import "cosmossdk.io/errors"

var (
	// ErrInvalidOwner signals that the supplied vault owner address is malformed.
	ErrInvalidOwner = errors.Register(ModuleName, 1500, "invalid vault owner")
	// ErrInvalidPolicy is returned when a policy identifier is missing.
	ErrInvalidPolicy = errors.Register(ModuleName, 1501, "policy id required")
	// ErrInvalidTier is raised for unsupported reserve tiers.
	ErrInvalidTier = errors.Register(ModuleName, 1502, "tier not supported")
	// ErrInvalidAmount indicates the prepaid amount was zero or negative.
	ErrInvalidAmount = errors.Register(ModuleName, 1503, "prepaid amount must be positive")
	// ErrVaultNotFound is returned when a requested vault id does not exist.
	ErrVaultNotFound = errors.Register(ModuleName, 1504, "vault not found")
	// ErrCommitmentCreation wraps failures coming from the reserve keeper when provisioning commitments.
	ErrCommitmentCreation = errors.Register(ModuleName, 1505, "failed to create reserve commitment")
	// ErrInvalidCommitmentEndTime indicates the optional end time is malformed or not after the current block time.
	ErrInvalidCommitmentEndTime = errors.Register(ModuleName, 1506, "invalid commitment end time")
)
