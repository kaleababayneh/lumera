//go:build !cosmos

package types

import (
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

const (
	// ModuleName defines the module name
	ModuleName = "registry"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName
)

// ParamSubspace alias for stub build
type ParamSubspace = paramtypes.Subspace

// AccountKeeper stub interface
type AccountKeeper interface{}

// BankKeeper stub interface
type BankKeeper interface{}

// GenesisState stub for the registry module
type GenesisState struct{}

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{}
}

// Params stub for module parameters
type Params struct{}

// DefaultParams returns default module parameters
func DefaultParams() *Params {
	return &Params{}
}

// Validate performs basic validation of params
func (p *Params) Validate() error {
	if p == nil {
		return nil
	}
	return nil
}

// ParamSetPairs implements the ParamSet interface
func (p *Params) ParamSetPairs() ParamSetPairs {
	return ParamSetPairs{}
}

// ParamKeyTable returns the parameter key table
func ParamKeyTable() KeyTable {
	return NewKeyTable()
}

// KeyTable represents a parameter key table
type KeyTable struct{}

// NewKeyTable creates a new KeyTable
func NewKeyTable() KeyTable {
	return KeyTable{}
}

// RegisterParamSet registers a ParamSet with the KeyTable
func (kt KeyTable) RegisterParamSet(ps ParamSet) KeyTable {
	return kt
}

// ParamSet defines the interface for parameter sets
type ParamSet interface {
	ParamSetPairs() ParamSetPairs
}

// ParamSetPairs represents a slice of key-value pairs for parameters
type ParamSetPairs []ParamSetPair

// ParamSetPair represents a key-value pair for a parameter
type ParamSetPair struct {
	Key         []byte
	Value       interface{}
	ValidatorFn func(interface{}) error
}

// ToolVerifiedBadge mirrors the generated enum shape used by non-cosmos
// discovery stubs so router packages can compile without the protobuf build.
type ToolVerifiedBadge int32

const (
	ToolVerifiedBadge_TOOL_VERIFIED_BADGE_UNSPECIFIED ToolVerifiedBadge = 0
	ToolVerifiedBadge_TOOL_VERIFIED_BADGE_BRONZE      ToolVerifiedBadge = 1
	ToolVerifiedBadge_TOOL_VERIFIED_BADGE_SILVER      ToolVerifiedBadge = 2
	ToolVerifiedBadge_TOOL_VERIFIED_BADGE_GOLD        ToolVerifiedBadge = 3
)

// ToolCard is the minimal non-cosmos placeholder required by registry type
// stubs and router callers that only need the identifier surface.
type ToolCard struct {
	ToolId string
}
