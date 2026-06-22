// Package keeper manages state and message handlers for the vaults module.
package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"

	reserve "github.com/LumeraProtocol/lumera/x/reserve/types"
	"github.com/LumeraProtocol/lumera/x/vaults/types"
)

// Keeper manages prepaid vault commitments.
type Keeper struct {
	cdc           codec.BinaryCodec
	storeService  corestore.KVStoreService
	bankKeeper    types.BankKeeper
	reserveKeeper types.ReserveKeeper
	authority     string
	logger        log.Logger

	schema collections.Schema
	vaults collections.Map[string, *types.Vault]
	seq    collections.Sequence
	index  collections.KeySet[collections.Pair[string, string]]
}

// VaultSpendResult records a successful prepaid vault spend.
type VaultSpendResult struct {
	VaultID         string
	CommitmentID    string
	Amount          sdk.Coin
	RecipientModule string
	Allocation      reserve.ReserveAllocation
	RemainingAmount sdk.Coin
}

// NewKeeper constructs a keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	bankKeeper types.BankKeeper,
	reserveKeeper types.ReserveKeeper,
	authority string,
	logger log.Logger,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	vaults := collections.NewMap(
		sb,
		collections.NewPrefix(types.VaultKeyPrefix),
		"vaults",
		collections.StringKey,
		collPtrValue[types.Vault](cdc),
	)
	seq := collections.NewSequence(
		sb,
		collections.NewPrefix(types.VaultSeqKeyPrefix),
		"vault_seq",
	)
	index := collections.NewKeySet(
		sb,
		collections.NewPrefix(types.VaultIndexKeyPrefix),
		"vaults_by_owner",
		collections.PairKeyCodec(collections.StringKey, collections.StringKey),
	)

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build vault schema: %w", err))
	}

	return &Keeper{
		cdc:           cdc,
		storeService:  storeService,
		bankKeeper:    bankKeeper,
		reserveKeeper: reserveKeeper,
		authority:     authority,
		logger:        logger.With("module", fmt.Sprintf("x/%s", types.ModuleName)),
		schema:        schema,
		vaults:        vaults,
		seq:           seq,
		index:         index,
	}
}

// Logger returns the module logger.
func (k *Keeper) Logger() log.Logger { return k.logger }

// CreateVault provisions a new prepaid vault commitment.
func (k *Keeper) CreateVault(ctx sdk.Context, msg *types.MsgCreateVault) (*types.Vault, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is required")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	ownerAddr, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return nil, types.ErrInvalidOwner
	}

	prepaidCoin := msg.PrepaidAmount
	if !prepaidCoin.IsValid() || !prepaidCoin.IsPositive() {
		return nil, types.ErrInvalidAmount
	}

	duration := time.Duration(0)
	if end := msg.CommitmentEndTime; !end.IsZero() {
		blockTime := ctx.BlockTime()
		if blockTime.IsZero() {
			return nil, fmt.Errorf("vaults: block time must be set when commitment_end_time is provided")
		}
		if !end.After(blockTime) {
			return nil, types.ErrInvalidCommitmentEndTime
		}
		duration = end.Sub(blockTime)
	}

	coins := sdk.NewCoins(prepaidCoin)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, ownerAddr, types.ModuleName, coins); err != nil {
		return nil, fmt.Errorf("transfer prepaid amount: %w", err)
	}

	reserveReq := reserve.ReserveRequest{
		Owner:    msg.Owner,
		PolicyID: msg.PolicyId,
		ToolID:   msg.ToolId,
		Tier:     msg.Tier,
		Amount:   prepaidCoin,
		Duration: duration,
	}

	commitment, err := k.reserveKeeper.CreateCommitment(ctx, reserveReq)
	if err != nil {
		return nil, types.ErrCommitmentCreation
	}

	id, err := k.seq.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("allocate vault id: %w", err)
	}

	vaultID := fmt.Sprintf("vault-%d", id)
	vault := &types.Vault{
		Id:              vaultID,
		Owner:           msg.Owner,
		PolicyId:        msg.PolicyId,
		ToolId:          commitment.ToolID,
		Tier:            commitment.Tier,
		PrepaidAmount:   msg.PrepaidAmount,
		RemainingAmount: commitment.RemainingAmount,
		DiscountBps:     commitment.DiscountBps,
		StartTime:       commitment.StartTime,
		ExpireTime:      commitment.ExpireTime,
		RolloverAllowed: commitment.RolloverAllowed,
		CommitmentId:    commitment.ID,
	}

	if err := k.vaults.Set(ctx, vaultID, vault); err != nil {
		return nil, err
	}
	if err := k.index.Set(ctx, collections.Join(msg.Owner, vaultID)); err != nil {
		return nil, err
	}

	return vault, nil
}

// SpendVault consumes prepaid capacity from one exact vault commitment and
// moves the matching funds out of the vault module. It is intentionally exact
// by vault id so future zero-lock settlement cannot debit a different
// owner/policy/tool-compatible reserve commitment.
func (k *Keeper) SpendVault(ctx context.Context, vaultID string, amount sdk.Coin, recipientModule string) (*VaultSpendResult, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()

	result, err := k.spendVault(cacheCtx, vaultID, amount, recipientModule)
	if err != nil {
		return nil, err
	}
	write()
	return result, nil
}

func (k *Keeper) spendVault(ctx context.Context, vaultID string, amount sdk.Coin, recipientModule string) (*VaultSpendResult, error) {
	vaultID = strings.TrimSpace(vaultID)
	if vaultID == "" {
		return nil, types.ErrVaultNotFound.Wrap("vault id required")
	}
	if !amount.IsValid() || !amount.IsPositive() {
		return nil, types.ErrInvalidAmount.Wrapf("spend amount must be positive: %s", amount)
	}
	recipientModule = strings.TrimSpace(recipientModule)
	if recipientModule == "" {
		return nil, fmt.Errorf("recipient module required")
	}

	vault, found, err := k.GetVault(ctx, vaultID)
	if err != nil {
		return nil, err
	}
	if !found || vault == nil {
		return nil, types.ErrVaultNotFound.Wrapf("%s", vaultID)
	}
	commitmentID := strings.TrimSpace(vault.CommitmentId)
	if commitmentID == "" {
		return nil, fmt.Errorf("vault %s has no reserve commitment", vaultID)
	}
	if vault.RemainingAmount.Amount.IsNil() {
		return nil, fmt.Errorf("vault %s has no remaining amount", vaultID)
	}
	remainingAmount := vault.RemainingAmount
	if remainingAmount.Denom != amount.Denom {
		return nil, reserve.ErrCreditDenomMismatch
	}
	if remainingAmount.Amount.LT(amount.Amount) {
		return nil, reserve.ErrInsufficientCapacity
	}
	remainingAfterSpend := remainingAmount.Sub(amount)

	allocation, err := k.reserveKeeper.AllocateCommitment(ctx, commitmentID, amount)
	if err != nil {
		return nil, fmt.Errorf("debit vault %s commitment %s: %w", vaultID, commitmentID, err)
	}
	if !allocation.Applied {
		return nil, reserve.ErrInsufficientCapacity.Wrapf("vault %s commitment %s", vaultID, commitmentID)
	}

	if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, recipientModule, sdk.NewCoins(amount)); err != nil {
		return nil, fmt.Errorf("transfer vault spend %s to module %s: %w", vaultID, recipientModule, err)
	}

	vault.RemainingAmount = remainingAfterSpend
	if err := k.vaults.Set(ctx, vaultID, vault); err != nil {
		return nil, fmt.Errorf("update vault remaining amount: %w", err)
	}

	return &VaultSpendResult{
		VaultID:         vaultID,
		CommitmentID:    commitmentID,
		Amount:          amount,
		RecipientModule: recipientModule,
		Allocation:      allocation,
		RemainingAmount: remainingAfterSpend,
	}, nil
}

// GetVault fetches a vault by id.
func (k *Keeper) GetVault(ctx context.Context, id string) (*types.Vault, bool, error) {
	vault, err := k.vaults.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	hydrated, err := k.hydrateVault(ctx, vault)
	if err != nil {
		return nil, false, err
	}
	if hydrated == nil {
		return nil, false, nil
	}

	return hydrated, true, nil
}

func (k *Keeper) hydrateVault(ctx context.Context, vault *types.Vault) (*types.Vault, error) {
	if vault == nil {
		return nil, nil
	}

	cloned := deepCopyVault(vault)
	if cloned == nil {
		return nil, fmt.Errorf("unable to clone vault %s", vault.Id)
	}

	commitmentID := cloned.CommitmentId
	trimmedCommitmentID := strings.TrimSpace(commitmentID)
	if trimmedCommitmentID == "" {
		if commitmentID != "" {
			return nil, fmt.Errorf("commitment_id must not contain only whitespace")
		}
		return cloned, nil
	}
	if trimmedCommitmentID != commitmentID {
		return nil, fmt.Errorf("commitment_id must not contain leading or trailing whitespace")
	}

	commitment, found, err := k.reserveKeeper.GetCommitment(ctx, commitmentID)
	if err != nil {
		return nil, err
	}
	if !found || commitment == nil {
		return cloned, nil
	}

	cloned.RemainingAmount = commitment.RemainingAmount
	cloned.DiscountBps = commitment.DiscountBps
	cloned.StartTime = commitment.StartTime
	cloned.ExpireTime = commitment.ExpireTime
	cloned.RolloverAllowed = commitment.RolloverAllowed

	return cloned, nil
}

// IterateVaultsByOwner iterates vaults belonging to owner. Orphaned index
// entries (pointing at a vault row that no longer exists in k.vaults) are
// collected during iteration and removed AFTER the Walk completes, rather
// than removed inline from the callback. Mutating the same collection that
// is being walked is undefined behaviour under cosmos-sdk collections /
// iavl semantics even when the mutation is on the key the cursor is
// currently positioned at — the prior inline-remove form happened to work
// under the current iavl iterator snapshot behaviour but was a latent
// future-fragility hazard. Aligns with the correct collect-then-delete
// pattern used by x/reserve/keeper/keeper.go:releaseExpired and
// x/registry/keeper/receipt_queue.go:PruneSettledReceipts.
func (k *Keeper) IterateVaultsByOwner(ctx context.Context, owner string, fn func(*types.Vault) bool) error {
	rng := collections.NewPrefixedPairRange[string, string](owner)
	var orphans []collections.Pair[string, string]
	walkErr := k.index.Walk(ctx, rng, func(pair collections.Pair[string, string]) (bool, error) {
		vault, err := k.vaults.Get(ctx, pair.K2())
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				orphans = append(orphans, pair)
				return false, nil
			}
			return false, err
		}
		if vault == nil {
			return false, nil
		}
		hydrated, err := k.hydrateVault(ctx, vault)
		if err != nil {
			return false, err
		}
		if hydrated == nil {
			return false, nil
		}
		if stop := fn(hydrated); stop {
			return true, nil
		}
		return false, nil
	})

	// Remove orphaned index entries AFTER the Walk has released its cursor,
	// regardless of whether the Walk itself errored partway through — a
	// partial-walk state is still free to surface its known-orphan subset to
	// the next caller, and deferring the Remove until after Walk returns
	// matches the collect-then-delete pattern used across the codebase.
	for _, pair := range orphans {
		if rmErr := k.index.Remove(ctx, pair); rmErr != nil {
			k.Logger().Error("failed to remove orphaned vault index", "owner", pair.K1(), "id", pair.K2(), "error", rmErr)
		}
	}
	return walkErr
}

// deepCopyVault returns an independent copy of a Vault via a gogo
// marshal/unmarshal round-trip (proto.Clone panics on the sdk.Coin customtype
// fields).
func deepCopyVault(v *types.Vault) *types.Vault {
	if v == nil {
		return nil
	}
	bz, err := proto.Marshal(v)
	if err != nil {
		return v
	}
	out := &types.Vault{}
	if proto.Unmarshal(bz, out) != nil {
		return v
	}
	return out
}

// InitGenesis hydrates the keeper state from the supplied genesis struct.
func (k *Keeper) InitGenesis(ctx sdk.Context, genesis *types.GenesisState) error {
	if genesis == nil {
		genesis = types.DefaultGenesis()
	}
	if err := genesis.Validate(); err != nil {
		return err
	}

	for _, vault := range genesis.Vaults {
		if err := k.vaults.Set(ctx, vault.Id, vault); err != nil {
			return err
		}
		if err := k.index.Set(ctx, collections.Join(vault.Owner, vault.Id)); err != nil {
			return err
		}
	}

	nextID := genesis.NextID
	if nextID == 0 {
		nextID = inferNextSequence(genesis.Vaults)
	}
	if err := k.seq.Set(ctx, nextID); err != nil {
		return err
	}

	return nil
}

// ExportGenesis constructs a genesis state from the current keeper state.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	state := types.DefaultGenesis()
	err := k.vaults.Walk(ctx, nil, func(_ string, vault *types.Vault) (bool, error) {
		hydrated, err := k.hydrateVault(ctx, vault)
		if err != nil {
			return false, err
		}
		if hydrated == nil {
			return false, nil
		}
		state.Vaults = append(state.Vaults, hydrated)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(state.Vaults, func(i, j int) bool {
		return state.Vaults[i].Id < state.Vaults[j].Id
	})

	peek, err := k.seq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	state.NextID = peek
	return state, nil
}

func inferNextSequence(vaults []*types.Vault) uint64 {
	var next uint64
	for _, vault := range vaults {
		if vault == nil {
			continue
		}
		if strings.HasPrefix(vault.Id, "vault-") {
			if seq, err := strconv.ParseUint(strings.TrimPrefix(vault.Id, "vault-"), 10, 64); err == nil && seq >= next {
				next = seq + 1
			}
		}
	}
	return next
}

// RefundExpiredVaults sweeps expired vaults, refunds remaining amounts to owners, and prunes them from state.
func (k *Keeper) RefundExpiredVaults(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()
	if blockTime.IsZero() {
		return fmt.Errorf("vaults: block time must be set")
	}

	type expiredVault struct {
		id              string
		owner           string
		remainingAmount sdk.Coin
	}

	var expired []expiredVault

	// Walk vaults to find expired ones
	err := k.vaults.Walk(ctx, nil, func(id string, vault *types.Vault) (bool, error) {
		hydrated, err := k.hydrateVault(ctx, vault)
		if err != nil {
			// Log and skip un-hydratable vaults to avoid halting the sweep
			k.Logger().Error("failed to hydrate vault during sweep", "id", id, "error", err)
			return false, nil
		}
		if hydrated == nil {
			return false, nil
		}

		// A vault is expired if it has an ExpireTime and blockTime >= ExpireTime.
		if !hydrated.ExpireTime.IsZero() && !blockTime.Before(hydrated.ExpireTime) {
			expired = append(expired, expiredVault{
				id:              id,
				owner:           hydrated.Owner,
				remainingAmount: hydrated.RemainingAmount,
			})
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, item := range expired {
		{
			coin := item.remainingAmount
			if !coin.Amount.IsNil() && coin.IsPositive() {
				ownerAddr, err := sdk.AccAddressFromBech32(item.owner)
				if err != nil {
					return fmt.Errorf("parse owner for expired vault %s: %w", item.id, err)
				}
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, ownerAddr, sdk.NewCoins(coin)); err != nil {
					return fmt.Errorf("refund expired vault %s: %w", item.id, err)
				}
			}
		}

		vault, err := k.vaults.Get(ctx, item.id)
		if err == nil && vault != nil {
			_ = k.vaults.Remove(ctx, item.id)
			_ = k.index.Remove(ctx, collections.Join(vault.Owner, item.id))
		}
	}

	return nil
}
