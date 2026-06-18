
// Package types holds shared types and helpers for the registry module.
//
//revive:disable:var-naming // Cosmos modules conventionally expose a types package.
package types

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper defines the expected account keeper
type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
	SetAccount(ctx context.Context, acc sdk.AccountI)
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
	GetModuleAddress(moduleName string) sdk.AccAddress
	NewAccountWithAddress(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
	HasAccount(ctx context.Context, addr sdk.AccAddress) bool
	AddressCodec() address.Codec
}

// BankKeeper defines the expected bank keeper
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amounts sdk.Coins) error
	MintCoins(ctx context.Context, moduleName string, amounts sdk.Coins) error
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	LockedCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	BlockedAddr(addr sdk.AccAddress) bool
	HasSupply(ctx context.Context, denom string) bool
}

// ParamSubspace defines the expected param subspace
type ParamSubspace interface {
	Get(ctx sdk.Context, key []byte, ptr interface{})
	Set(ctx sdk.Context, key []byte, param interface{})
	Has(ctx sdk.Context, key []byte) bool
	GetParamSet(ctx sdk.Context, ps ParamSet)
	SetParamSet(ctx sdk.Context, ps ParamSet)
	WithKeyTable(table KeyTable) ParamSubspace
	HasKeyTable() bool
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

// KeyTable represents a parameter key table
type KeyTable struct {
	pairs map[string]ParamSetPair
}

// NewKeyTable creates a new KeyTable
func NewKeyTable() KeyTable {
	return KeyTable{
		pairs: make(map[string]ParamSetPair),
	}
}

// RegisterParamSet registers a ParamSet with the KeyTable
func (kt KeyTable) RegisterParamSet(ps ParamSet) KeyTable {
	for _, pair := range ps.ParamSetPairs() {
		kt.pairs[string(pair.Key)] = pair
	}
	return kt
}

// InsuranceHooks defines the hooks for insurance module integration
type InsuranceHooks interface {
	RecordContribution(ctx context.Context, toolID string, amount interface{}) error
	GetUtilization(ctx context.Context) (interface{}, error)
}

// CreditsHooks defines the hooks for credits module integration
type CreditsHooks interface {
	ProcessSettlement(ctx context.Context, settlement interface{}) error
}

// DiscoveryHooks defines hooks for cross-chain discovery updates.
type DiscoveryHooks interface {
	OnToolCardUpdate(ctx context.Context, tool *ToolCard, op string) error
}

// ObservabilityHook defines the hook for telemetry and monitoring
type ObservabilityHook interface {
	EmitEvent(ctx context.Context, eventType string, attributes map[string]string)
	RecordMetric(ctx context.Context, metric string, value float64, tags map[string]string)
}
