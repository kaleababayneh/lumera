package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	oracletypes "github.com/LumeraProtocol/lumera/x/oracle/types"
)

// BankKeeper defines the expected bank keeper functionality.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, addr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// CreditsKeeper exposes credit mint/burn helpers to the payment rails module.
type CreditsKeeper interface {
	GetParams(ctx context.Context) *creditstypes.Params
	MintCredits(ctx context.Context, recipient sdk.AccAddress, amount sdk.Coin, reason string) error
	BurnCreditsFromAccount(ctx context.Context, sender sdk.AccAddress, amount sdk.Coin, reason string) error
}

// OracleKeeper exposes aggregated price lookups.
type OracleKeeper interface {
	GetAggregatedPrice(ctx context.Context, assetPair string) (*oracletypes.AggregatedPrice, error)
}
