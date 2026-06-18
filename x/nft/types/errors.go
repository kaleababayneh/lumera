//go:build cosmos

package types

import "cosmossdk.io/errors"

var (
	// ErrInvalidCurator indicates the provided curator address is malformed.
	ErrInvalidCurator = errors.Register(ModuleName, 1300, "invalid curator address")
	// ErrInvalidToolpackID signals an empty or malformed toolpack identifier.
	ErrInvalidToolpackID = errors.Register(ModuleName, 1301, "invalid toolpack id")
	// ErrDuplicateToolpack is raised when attempting to mint an existing toolpack.
	ErrDuplicateToolpack = errors.Register(ModuleName, 1302, "toolpack already exists")
	// ErrToolpackNotFound denotes the requested toolpack cannot be located.
	ErrToolpackNotFound = errors.Register(ModuleName, 1303, "toolpack not found")
	// ErrInactiveToolpack indicates the toolpack has been deactivated.
	ErrInactiveToolpack = errors.Register(ModuleName, 1304, "toolpack is inactive")
	// ErrInvalidRoyalty reports royalty basis points outside allowed range.
	ErrInvalidRoyalty = errors.Register(ModuleName, 1305, "invalid royalty basis points")
	// ErrInvalidTools is returned when the tool list is empty.
	ErrInvalidTools = errors.Register(ModuleName, 1306, "tool list cannot be empty")
	// ErrUnauthorized signals the caller lacks permission to mutate the toolpack.
	ErrUnauthorized = errors.Register(ModuleName, 1307, "not authorized to modify toolpack")
)
