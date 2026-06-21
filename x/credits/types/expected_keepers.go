package types

import (
	"context"

	nfttypes "github.com/LumeraProtocol/lumera/x/nft/types"
	reserve "github.com/LumeraProtocol/lumera/x/reserve/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper defines the contract expected from the bank module keeper.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, name string, amt sdk.Coins) error
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

// AccountKeeper defines the subset of account keeper functionality required.
type AccountKeeper interface {
	GetModuleAddress(moduleName string) sdk.AccAddress
	IterateAccounts(ctx context.Context, cb func(account sdk.AccountI) (stop bool))
}

// InsuranceKeeper defines the expected interface for the insurance module
type InsuranceKeeper interface {
	ContributeToPool(ctx context.Context, receiptID, toolID, publisherID, policyVersion, userID string, amount sdk.Coins) error
	GetPoolBalance(ctx context.Context) (sdk.Coins, error)
}

// RegistryKeeper defines the expected interface for the registry module
type RegistryKeeper interface {
	GetToolPublisher(ctx context.Context, toolID string) (sdk.AccAddress, error)
	// ValidateReceipt gates settlement on a verifiable Proof-of-Service receipt:
	// it returns nil iff a receipt with receiptID was anchored for toolID and is
	// bound to the lock being settled (lockID).
	ValidateReceipt(ctx sdk.Context, receiptID, toolID, lockID string) error
}

// ReserveKeeper exposes reserve tier allocation to the credits module.
type ReserveKeeper interface {
	AllocateReserve(ctx context.Context, owner, policyID, toolID string, amount sdk.Coin) (reserve.ReserveAllocation, error)
}

// NFTKeeper exposes curated Toolpack metadata for settlement royalties.
type NFTKeeper interface {
	GetToolpack(ctx context.Context, id string) (*nfttypes.ToolpackNFT, bool, error)
	RecordRoyaltyPayout(ctx context.Context, authority string, toolpackID string, amount sdk.Coin) error
}
