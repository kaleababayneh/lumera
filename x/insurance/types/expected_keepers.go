
package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper defines the expected interface for the bank module.
//
// BurnCoins is required by the slash-restitution routing executor
// (lumera_ai-tvanr) — the spec-defined 5% burn leg is implemented
// as bank.BurnCoins on the insurance module account, not a transfer.
// The real cosmos-sdk BankKeeper satisfies this signature; the
// interface extension is additive so existing callers that don't
// need burn are unaffected.
type BankKeeper interface {
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

// AccountKeeper defines the expected interface for the account module
type AccountKeeper interface {
	GetModuleAddress(moduleName string) sdk.AccAddress
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}
