//go:build cosmos && !cosmos_full

package keeper

import (
	"context"
	"fmt"
	"sort"
	"time"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// ConsensusVersion defines the module's consensus version for migrations.
// Version history:
//   - 1: Initial version with basic lock and params support
//   - 2: Adds settlements, disputes, and metrics state
const ConsensusVersion = 2

// RevenueSplits mirrors the structure provided by the full implementation.
type RevenueSplits struct {
	PublisherBPS uint32
	RouterBPS    uint32
	ReferrerBPS  uint32
}

// SettlementRequest mirrors the argument accepted by ProcessSettlement in the
// full implementation. Only the fields needed by callers are maintained.
type SettlementRequest struct {
	ReceiptID      string
	ToolID         string
	TotalAmount    sdk.Coins
	PublisherAddr  sdk.AccAddress
	RouterAddr     sdk.AccAddress
	ReferrerAddr   sdk.AccAddress
	CacheHit       bool
	OriginToolID   string
	OriginID       string
	PublisherID    string
	UserID         string
	RouterID       string
	ReferrerID     string
	PolicySnapshot string
	ToolpackID     string
}

// Keeper is a lightweight in-memory stub that satisfies the keeper contract
// used by the module wiring code. It purposefully avoids persistent state.
type Keeper struct {
	authority string
	logger    log.Logger
	params    *types.Params
	locks     map[string]*types.Lock
	lockSeq   uint64
}

// NewKeeper returns a stub keeper that fulfils the expected constructor
// signature. The codec and store service are ignored in this build.
func NewKeeper(
	_ codec.BinaryCodec,
	_ corestore.KVStoreService,
	_ types.BankKeeper,
	_ types.AccountKeeper,
	_ types.InsuranceKeeper,
	_ types.RegistryKeeper,
	_ types.ReserveKeeper,
	_ types.NFTKeeper,
	authority string,
) Keeper {
	return Keeper{
		authority: authority,
		logger:    log.NewNopLogger(),
		params:    types.DefaultParams(),
		locks:     make(map[string]*types.Lock),
		lockSeq:   1,
	}
}

// Logger returns a module-scoped logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	if ctx.Logger() != nil {
		return ctx.Logger().With("module", types.ModuleName)
	}
	return k.logger.With("module", types.ModuleName)
}

// Authority exposes the parameter authority address.
func (k Keeper) Authority() string { return k.authority }

// GetParams returns the cached module parameters.
func (k Keeper) GetParams(context.Context) *types.Params {
	if k.params == nil || k.params.CreditDenom == "" {
		return types.DefaultParams()
	}
	return proto.Clone(k.params).(*types.Params)
}

// ValidateSchemaVersion is a no-op in the stub build.
func (k Keeper) ValidateSchemaVersion(context.Context) error { return nil }

// SetParams updates the cached module parameters.
func (k *Keeper) SetParams(_ context.Context, params *types.Params) error {
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := params.Validate(); err != nil {
		return err
	}
	k.params = proto.Clone(params).(*types.Params)
	return nil
}

// MintCredits records a mint operation in the stub build.
func (k Keeper) MintCredits(ctx context.Context, recipient sdk.AccAddress, amount sdk.Coin, reason string) error {
	if recipient.Empty() {
		return fmt.Errorf("recipient required")
	}
	if !amount.IsValid() || amount.Amount.IsNegative() {
		return fmt.Errorf("invalid mint amount")
	}
	if amount.Amount.IsZero() {
		return nil
	}
	params := k.GetParams(ctx)
	if amount.Denom != params.CreditDenom {
		return types.ErrInvalidParams.Wrapf("expected denom %s", params.CreditDenom)
	}
	_ = reason
	return nil
}

// BurnCreditsFromAccount records a burn operation in the stub build.
func (k Keeper) BurnCreditsFromAccount(ctx context.Context, sender sdk.AccAddress, amount sdk.Coin, reason string) error {
	if sender.Empty() {
		return fmt.Errorf("sender required")
	}
	if !amount.IsValid() || amount.Amount.IsNegative() {
		return fmt.Errorf("invalid burn amount")
	}
	if amount.Amount.IsZero() {
		return nil
	}
	params := k.GetParams(ctx)
	if amount.Denom != params.CreditDenom {
		return types.ErrInvalidParams.Wrapf("expected denom %s", params.CreditDenom)
	}
	_ = reason
	return nil
}

// NextLockID returns a deterministic lock identifier.
func (k *Keeper) NextLockID(_ context.Context) (string, error) {
	id := fmt.Sprintf("lock-%d", k.lockSeq)
	k.lockSeq++
	return id, nil
}

// SaveLock records a lock in memory.
func (k *Keeper) SaveLock(_ context.Context, lock *types.Lock) error {
	if lock == nil {
		return fmt.Errorf("lock cannot be nil")
	}
	if lock.LockId == "" {
		return fmt.Errorf("lock id required")
	}
	k.locks[lock.LockId] = proto.Clone(lock).(*types.Lock)
	return nil
}

// DeleteLock removes a lock from memory.
func (k *Keeper) DeleteLock(_ context.Context, lockID string) error {
	delete(k.locks, lockID)
	return nil
}

// IterateLocks walks each stored lock and invokes the callback.
func (k Keeper) IterateLocks(_ context.Context, cb func(*types.Lock) bool) error {
	keys := make([]string, 0, len(k.locks))
	for id := range k.locks {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		if cb(k.locks[id]) {
			break
		}
	}
	return nil
}

// SetLockSequence sets the next lock sequence number.
func (k *Keeper) SetLockSequence(_ context.Context, seq uint64) error {
	if seq == 0 {
		seq = 1
	}
	k.lockSeq = seq
	return nil
}

// ExportState exports the stub module state for genesis.
func (k Keeper) ExportState(ctx context.Context) (*types.GenesisState, error) {
	params := k.GetParams(ctx)
	locks := make([]*types.Lock, 0, len(k.locks))
	if err := k.IterateLocks(ctx, func(lock *types.Lock) bool {
		if lock == nil {
			return false
		}
		locks = append(locks, proto.Clone(lock).(*types.Lock))
		return false
	}); err != nil {
		return nil, err
	}
	return &types.GenesisState{
		Params:      params,
		Locks:       locks,
		Settlements: nil,
		Disputes:    nil,
		Metrics:     &types.SettlementMetrics{},
	}, nil
}

// ImportState imports genesis state into the stub keeper.
func (k *Keeper) ImportState(ctx context.Context, genesis *types.GenesisState) error {
	if genesis == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if err := genesis.Validate(); err != nil {
		return fmt.Errorf("invalid genesis state: %w", err)
	}
	params := genesis.Params
	if err := k.SetParams(ctx, params); err != nil {
		return err
	}

	k.locks = make(map[string]*types.Lock)
	var nextSeq uint64 = 1
	for _, lock := range genesis.Locks {
		if lock == nil {
			continue
		}
		if err := k.SaveLock(ctx, lock); err != nil {
			return err
		}
		if seq, err := parseLockSequence(lock.LockId); err == nil && seq >= nextSeq {
			nextSeq = seq + 1
		}
	}
	return k.SetLockSequence(ctx, nextSeq)
}

// MigrateV1ToV2 is a no-op in the stub build.
func (Keeper) MigrateV1ToV2(context.Context) error { return nil }

// GetPendingSettlements returns no settlements in the stub build.
func (Keeper) GetPendingSettlements(context.Context) ([]*types.SettlementRecord, error) {
	return nil, nil
}

// IteratePendingSettlements is a no-op stub.
func (Keeper) IteratePendingSettlements(sdk.Context, int, func(*types.SettlementRecord) (bool, bool, error)) error {
	return nil
}

// ProcessSettlement is a no-op stub.
func (Keeper) ProcessSettlement(context.Context, SettlementRequest) error { return nil }

// UpdateSettlement is a no-op stub.
func (Keeper) UpdateSettlement(context.Context, *types.SettlementRecord) error { return nil }

// GetSettlement is a no-op stub.
func (Keeper) GetSettlement(context.Context, string) (*types.SettlementRecord, bool) {
	return nil, false
}

// ProcessExpiredLocks is a no-op stub.
func (Keeper) ProcessExpiredLocks(context.Context, int) error { return nil }

// UpdateSettlementMetrics is a no-op stub.
func (Keeper) UpdateSettlementMetrics(context.Context, int, int) error { return nil }

// PruneOldSettlements is a no-op stub.
func (Keeper) PruneOldSettlements(context.Context, time.Time, int) error { return nil }

// SanitizeLockTTL clamps requested TTL against the configured maximum.
func (k Keeper) SanitizeLockTTL(_ context.Context, requested time.Duration) time.Duration {
	if requested <= 0 {
		return 0
	}
	params := k.params
	if params == nil {
		params = types.DefaultParams()
	}
	max := time.Duration(params.MaxLockTtlSeconds) * time.Second
	if max <= 0 {
		return requested
	}
	if requested > max {
		return max
	}
	return requested
}

func parseLockSequence(lockID string) (uint64, error) {
	const prefix = "lock-"
	if len(lockID) <= len(prefix) || lockID[:len(prefix)] != prefix {
		return 0, fmt.Errorf("unexpected lock id format: %s", lockID)
	}
	var seq uint64
	if _, err := fmt.Sscanf(lockID[len(prefix):], "%d", &seq); err != nil {
		return 0, err
	}
	return seq, nil
}

// MigrateLegacyState is a no-op in the stub build.
func (Keeper) MigrateLegacyState(_ sdk.Context) error { return nil }

// ModuleAddress returns nil in the stub build.
func (Keeper) ModuleAddress() sdk.AccAddress { return nil }

// EnsureModuleAccount is a no-op for the stub build.
func (Keeper) EnsureModuleAccount(sdk.Context) error { return nil }

// BankKeeper returns nil in the stub build.
func (Keeper) BankKeeper() types.BankKeeper { return nil }

// AccountKeeper returns nil in the stub build.
func (Keeper) AccountKeeper() types.AccountKeeper { return nil }

// StoreService returns nil in the stub build.
func (Keeper) StoreService() corestore.KVStoreService { return nil }

type stubMsgServer struct {
	types.UnimplementedMsgServer
}

// NewMsgServerImpl returns a stubbed gRPC message server.
func NewMsgServerImpl(*Keeper) types.MsgServer { return &stubMsgServer{} }

type stubQueryServer struct {
	types.UnimplementedQueryServer
}

// NewQueryServer returns a stubbed query server implementation.
func NewQueryServer(*Keeper) types.QueryServer { return &stubQueryServer{} }
