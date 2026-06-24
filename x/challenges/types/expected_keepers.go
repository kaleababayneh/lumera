package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper defines the expected bank module interface for escrow and payouts.
type BankKeeper interface {
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

// AccountKeeper defines the expected account module interface.
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// SLOProbeChallengeReference is the registry-facing projection of an
// x/challenges-owned SLO probe challenge lifecycle.
type SLOProbeChallengeReference struct {
	ChallengeID    string
	ToolID         string
	TargetKind     string
	TargetID       string
	Reason         string
	EvidenceDigest string
	Outcome        string
	ResponseDigest string
}

// RegistryKeeper defines the expected interface for the registry module.
type RegistryKeeper interface {
	GetToolCategories(ctx context.Context, toolID string) ([]string, bool)
	RecordSLOProbeChallengeIssued(ctx context.Context, ref SLOProbeChallengeReference) error
	RecordSLOProbeChallengeOutcome(ctx context.Context, ref SLOProbeChallengeReference) error
}

// LumeraIDKeeper defines the expected interface for identity proof validation.
// x/challenges delegates nonce/signature validation here so it does not
// duplicate LumeraID nonce state or replay semantics.
type LumeraIDKeeper interface {
	VerifyAndConsumeNonceSignature(ctx context.Context, lumeraID string, nonce string, signature string) error
}
