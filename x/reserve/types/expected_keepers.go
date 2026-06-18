package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReserveKeeper exposes functionality consumed by other modules (e.g., auctions, router).
type ReserveKeeper interface {
	AllocateReserve(ctx context.Context, owner, policyID, toolID string, amount sdk.Coin) (ReserveAllocation, error)
	ReleaseExpired(ctx context.Context) error
	CreateCommitment(ctx context.Context, req ReserveRequest) (*ReserveCommitment, error)
	HasActiveCommitment(ctx context.Context, policyID, toolID string) (bool, error)
}
