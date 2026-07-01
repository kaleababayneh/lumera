package keeper

import (
	"context"
	"time"

	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	creditskeeper "github.com/LumeraProtocol/lumera/x/credits/keeper"
	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
	"github.com/LumeraProtocol/lumera/x/router/types"
)

// Keeper maintains the router module state
type Keeper struct {
	types.UnimplementedRouterServer
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	logger       log.Logger
	authority    string // governance module account

	// Collections-based state
	state State

	// Dependencies for router functionality
	registryKeeper RegistryKeeper
	creditsKeeper  CreditsKeeper
	cacheKeeper    CacheKeeper
	lumeraIDKeeper LumeraIDKeeper
}

// RegistryKeeper defines the subset of registry functionality required by the router module.
//
// The adapter provided in app wiring (`app/app_config.go`) satisfies this interface by
// delegating to the registry keeper while ensuring deterministic iteration order.
type RegistryKeeper interface {
	// GetToolCard returns the canonical ToolCard for the supplied identifier when present.
	GetToolCard(ctx sdk.Context, toolID string) (*registrytypes.ToolCard, bool)

	// GetAllTools returns all registered ToolCards with deterministic ordering by tool ID.
	GetAllTools(ctx sdk.Context) []*registrytypes.ToolCard

	// SubmitReceipt forwards a usage receipt into the registry module for settlement.
	SubmitReceipt(ctx sdk.Context, receipt *registrytypes.UsageReceipt, signature []byte) error

	// GetToolMetrics retrieves the latest registry tool metrics snapshot if available.
	GetToolMetrics(ctx sdk.Context, toolID string) (*registrytypes.ToolMetrics, bool)
}

// CreditsKeeper defines credits module interface
type CreditsKeeper interface {
	LockCredits(ctx context.Context, router string, sessionID string, amount sdk.Coin,
		toolID string, quoteID string, policyVersion string, intentHash string, toolpackID ...string) (string, error)
	UnlockCredits(ctx context.Context, lockID string, reason string) error
	SettleLock(ctx context.Context, lockID string, actualCost sdk.Coin, receipt creditskeeper.SettlementRequest) (*creditskeeper.SettlementResult, error)
}

// CacheKeeper defines cache functionality
type CacheKeeper interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	RecordHit(ctx context.Context, toolID string, originToolID string) error
}

// LumeraIDKeeper defines the subset of lumeraid keeper functionality required by the router module.
// This enables Ed448-based identity verification and nonce-based session attestations.
type LumeraIDKeeper interface {
	// GetIdentity returns the identity record for a given LumeraID if it exists and is active.
	GetIdentity(ctx context.Context, lumeraID string) (*LumeraIDIdentity, bool, error)

	// IssueNonce creates a time-bound nonce for the specified LumeraID and purpose.
	IssueNonce(ctx context.Context, lumeraID string, purpose string, ttl time.Duration) (*LumeraIDNonce, error)

	// VerifyAndConsumeNonceSignature verifies an Ed448 signature and marks the nonce as used.
	VerifyAndConsumeNonceSignature(ctx context.Context, lumeraID string, nonce string, signature string) error
}

// LumeraIDIdentity is a simplified view of the lumeraid identity record for router use.
type LumeraIDIdentity struct {
	LumeraID  string
	PubKeyHex string
	Status    string
}

// LumeraIDNonce is a simplified view of the lumeraid nonce record for router use.
type LumeraIDNonce struct {
	Nonce     string
	Subject   string
	Purpose   string
	ExpiresAt time.Time
}

// NewKeeper creates a new router keeper
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	logger log.Logger,
	authority string,
	registryKeeper RegistryKeeper,
	creditsKeeper CreditsKeeper,
) *Keeper {
	// Initialize collections state
	state := NewState(cdc, storeService)

	return &Keeper{
		cdc:            cdc,
		storeService:   storeService,
		logger:         logger,
		authority:      authority,
		state:          state,
		registryKeeper: registryKeeper,
		creditsKeeper:  creditsKeeper,
	}
}

// Logger returns the logger
func (k Keeper) Logger(_ sdk.Context) log.Logger {
	return k.logger.With("module", "router")
}

// GetAuthority returns the module authority address
func (k Keeper) GetAuthority() string {
	return k.authority
}

// SetCacheKeeper sets the cache keeper (optional)
func (k *Keeper) SetCacheKeeper(ck CacheKeeper) {
	k.cacheKeeper = ck
}

// SetLumeraIDKeeper sets the lumeraid keeper for identity verification (optional)
func (k *Keeper) SetLumeraIDKeeper(lk LumeraIDKeeper) {
	k.lumeraIDKeeper = lk
}

// GetLumeraIDKeeper returns the lumeraid keeper if configured
func (k *Keeper) GetLumeraIDKeeper() LumeraIDKeeper {
	return k.lumeraIDKeeper
}
