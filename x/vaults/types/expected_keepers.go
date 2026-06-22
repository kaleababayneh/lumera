package types

import (
	"context"

	reserve "github.com/LumeraProtocol/lumera/x/reserve/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper defines the expected bank keeper functionality.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, addr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// ReserveKeeper exposes reserve commitment provisioning to the vaults module.
type ReserveKeeper interface {
	CreateCommitment(ctx context.Context, req reserve.ReserveRequest) (*reserve.ReserveCommitment, error)
	GetCommitment(ctx context.Context, id string) (*reserve.ReserveCommitment, bool, error)
	AllocateCommitment(ctx context.Context, commitmentID string, amount sdk.Coin) (reserve.ReserveAllocation, error)
}
