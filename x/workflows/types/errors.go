package types

import errorsmod "cosmossdk.io/errors"

var (
	// ErrInvalidParams reports invalid workflows module parameters.
	ErrInvalidParams = errorsmod.Register(ModuleName, 2, "invalid workflows params")
	// ErrInvalidWorkflow reports malformed workflow scaffold state.
	ErrInvalidWorkflow = errorsmod.Register(ModuleName, 3, "invalid workflow")
)
