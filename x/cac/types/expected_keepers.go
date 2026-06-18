//go:build cosmos

package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper defines the contract expected from the bank module keeper.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// AccountKeeper defines the subset of account keeper functionality required.
type AccountKeeper interface {
	GetModuleAddress(moduleName string) sdk.AccAddress
}

// CreditsKeeper defines the expected interface for the credits module for royalty distribution.
type CreditsKeeper interface {
	// ProcessCACRoyalty handles the royalty split between origin and serving tools.
	ProcessCACRoyalty(ctx context.Context, originToolID, servingToolID string, publisherAmount sdk.Coins) (originAmount, servingAmount sdk.Coins, err error)
}

// RegistryKeeper defines the expected interface for the registry module.
type RegistryKeeper interface {
	// GetToolPublisher returns the publisher address for a tool.
	GetToolPublisher(ctx context.Context, toolID string) (sdk.AccAddress, error)
	// IsDeterministicTool returns whether a tool produces deterministic output.
	IsDeterministicTool(ctx context.Context, toolID string) (bool, error)
}
