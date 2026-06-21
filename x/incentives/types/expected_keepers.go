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

// RegistryKeeper defines the expected interface for the registry module.
type RegistryKeeper interface {
	GetToolPublisher(ctx context.Context, toolID string) (sdk.AccAddress, error)
	IsToolRegistered(ctx context.Context, toolID string) (bool, error)
}

// RouterKeeper defines the expected interface for the router module.
type RouterKeeper interface {
	// GetToolMetrics retrieves performance metrics for a tool from the router.
	GetToolMetrics(ctx context.Context, toolID string, windowBlocks uint32) (*MetricSnapshot, error)
}

// InsuranceKeeper defines the expected interface for the insurance module.
type InsuranceKeeper interface {
	// ApplyBadgeDiscount applies a badge-based discount to insurance premiums.
	ApplyBadgeDiscount(ctx context.Context, toolID string, tier BadgeTier, basePremium sdk.Coin) (sdk.Coin, error)
}

// CreditsKeeper defines the expected interface for the credits module.
type CreditsKeeper interface {
	// ApplyLACBonus applies a badge-based LAC bonus during settlement.
	ApplyLACBonus(ctx context.Context, toolID string, tier BadgeTier, baseAmount sdk.Coin) (sdk.Coin, error)
}
