package creditsmodule

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	nfttypes "github.com/LumeraProtocol/lumera/x/nft/types"
	reservetypes "github.com/LumeraProtocol/lumera/x/reserve/types"
)

// TEMPORARY dependency-keeper stubs for the credits module.
//
// Credits is not standalone — its keeper needs Registry, Reserve and NFT keepers.
// These stubs let credits build and run on the chain BEFORE those modules are
// ported, so we can integrate one module at a time. Each stub is replaced by the
// real module keeper as it lands. (Insurance is DONE — x/insurance is ported and
// wired; its real keeper is passed in `ProvideModule`.)
//
// MUST NOT ship to testnet/mainnet. See the "Lumera AI Module Port" progress log
// in CLAUDE.md. Methods that need real data fail loudly rather than mis-settle.

// stubRegistryKeeper — settlement needs the real tool publisher; fail loudly.
type stubRegistryKeeper struct{}

func (stubRegistryKeeper) GetToolPublisher(_ context.Context, _ string) (sdk.AccAddress, error) {
	return nil, fmt.Errorf("credits: registry keeper not wired (TEMPORARY stub)")
}

// stubReserveKeeper — reserve allocation needs the real keeper; fail loudly.
type stubReserveKeeper struct{}

func (stubReserveKeeper) AllocateReserve(_ context.Context, _, _, _ string, _ sdk.Coin) (reservetypes.ReserveAllocation, error) {
	return reservetypes.ReserveAllocation{}, fmt.Errorf("credits: reserve keeper not wired (TEMPORARY stub)")
}

// stubNFTKeeper — no curated toolpacks until x/nft is ported: report "not found"
// (royalty step is skipped) and no-op the royalty record.
type stubNFTKeeper struct{}

func (stubNFTKeeper) GetToolpack(_ context.Context, _ string) (*nfttypes.ToolpackNFT, bool, error) {
	return nil, false, nil
}

func (stubNFTKeeper) RecordRoyaltyPayout(_ context.Context, _, _ string, _ sdk.Coin) error {
	return nil
}
