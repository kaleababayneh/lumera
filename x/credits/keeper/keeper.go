package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	metrics "github.com/hashicorp/go-metrics"

	"github.com/LumeraProtocol/lumera/internal/logging"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

const (
	// GasPerSettlementScan accounts for scanning pending settlements per block.
	GasPerSettlementScan = 1_000
	// GasPerSettlementProcess accounts for processing a settlement entry.
	GasPerSettlementProcess = 5_000
	// GasPerExpiredLockProcess accounts for processing an expired lock.
	GasPerExpiredLockProcess = 2_000
	// GasPerPrunedSettlement accounts for pruning historical settlements.
	GasPerPrunedSettlement = 2_500
	// RedemptionBurnRateBPS defines the burn rate for LAC->LUME swaps (1.5%).
	RedemptionBurnRateBPS = 150
)

// jsonValueCodec implements collections.ValueCodec for JSON-serializable types
type jsonValueCodec[T any] struct{}

func (j jsonValueCodec[T]) Encode(value *T) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func (j jsonValueCodec[T]) Decode(b []byte) (*T, error) {
	if b == nil {
		return nil, nil
	}
	var value T
	if err := json.Unmarshal(b, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (j jsonValueCodec[T]) EncodeJSON(value *T) ([]byte, error) {
	return j.Encode(value)
}

func (j jsonValueCodec[T]) DecodeJSON(b []byte) (*T, error) {
	return j.Decode(b)
}

func (j jsonValueCodec[T]) Stringify(value *T) string {
	bz, err := j.Encode(value)
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return string(bz)
}

func (j jsonValueCodec[T]) ValueType() string {
	var zero T
	return fmt.Sprintf("%T", zero)
}

// newJSONValueCodec creates a new JSON value codec for type T
func newJSONValueCodec[T any]() jsonValueCodec[T] {
	return jsonValueCodec[T]{}
}

// State encapsulates the module collections state.
type State struct {
	Schema       collections.Schema
	Params       collections.Item[*types.Params]
	Locks        collections.Map[string, *types.Lock]
	LockSeq      collections.Sequence
	Settlements  collections.Map[string, *types.SettlementRecord]
	Disputes     collections.Map[string, *types.DisputeRecord]
	Metrics      collections.Item[*types.SettlementMetrics]
	CACRoyalties collections.Map[string, *types.CACRoyaltyRecord] // CAC royalty records by recordID
	CACStats     collections.Map[string, *types.CACRoyaltyStats]  // CAC stats by toolID
	CACSeq       collections.Sequence
	LockExpiry   collections.KeySet[collections.Pair[time.Time, string]]

	// Indexes
	PendingSettlements collections.KeySet[string]
	SettlementsByTime  collections.KeySet[collections.Pair[time.Time, string]]
	FinalizedLocks     collections.KeySet[collections.Pair[time.Time, string]]
	LockReceipts       collections.Map[string, string] // Maps LockID -> ReceiptID to enforce 1-to-1 binding
	LocksByQuote       collections.Map[string, string] // Maps QuoteID -> LockID for idempotency
}

// Keeper provides the module's state access layer.
type Keeper struct {
	cdc             codec.BinaryCodec
	storeService    corestore.KVStoreService
	bankKeeper      types.BankKeeper
	accountKeeper   types.AccountKeeper
	insuranceKeeper types.InsuranceKeeper
	registryKeeper  types.RegistryKeeper
	reserveKeeper   types.ReserveKeeper
	nftKeeper       types.NFTKeeper
	authority       string
	state           State
	paramCache      *keeperParamCache
}

type keeperParamCache struct {
	creditDenom string
	initialized bool
}

func normalizeCreditDenom(denom string) string {
	denom = strings.TrimSpace(denom)
	if denom == "" {
		return types.DefaultCreditDenom
	}
	return denom
}

func (c *keeperParamCache) setCreditDenom(denom string) {
	if c == nil {
		return
	}
	c.creditDenom = normalizeCreditDenom(denom)
	c.initialized = true
}

func (c *keeperParamCache) getCreditDenom() (string, bool) {
	if c == nil || !c.initialized || c.creditDenom == "" {
		return "", false
	}
	return c.creditDenom, true
}

// NewKeeper constructs a Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	insuranceKeeper types.InsuranceKeeper,
	registryKeeper types.RegistryKeeper,
	reserveKeeper types.ReserveKeeper,
	nftKeeper types.NFTKeeper,
	authority string,
) Keeper {
	if bankKeeper == nil {
		panic("credits keeper requires bank keeper")
	}
	if accountKeeper == nil {
		panic("credits keeper requires account keeper")
	}
	// Insurance keeper is intentionally OPTIONAL (nil-checked at every
	// call site via isInsuranceActive / insuranceRedistribution). The
	// credits keeper predates the insurance module and several chain
	// configurations (devnet, router-only builds, older testnet images)
	// still wire a credits keeper without an insurance counterpart. If
	// a future chain governance decision makes insurance a mandatory
	// dependency, promote this to a panic here AND update all call
	// sites that currently short-circuit on a nil insurance keeper; do
	// NOT add the panic without also walking those callers, or you'll
	// flip working chains into a boot loop.

	sb := collections.NewSchemaBuilder(storeService)
	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsPrefix),
			"params",
			collPtrValue[types.Params](cdc),
		),
		Locks: collections.NewMap(
			sb,
			collections.NewPrefix(types.LocksPrefix),
			"locks",
			collections.StringKey,
			collPtrValue[types.Lock](cdc),
		),
		LockSeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.LockSeqKeyPrefix),
			"lock_seq",
		),
		Settlements: collections.NewMap(
			sb,
			collections.NewPrefix(types.SettlementPrefix),
			"settlements",
			collections.StringKey,
			collPtrValue[types.SettlementRecord](cdc),
		),
		Disputes: collections.NewMap(
			sb,
			collections.NewPrefix(types.DisputePrefix),
			"disputes",
			collections.StringKey,
			collPtrValue[types.DisputeRecord](cdc),
		),
		Metrics: collections.NewItem(
			sb,
			collections.NewPrefix(types.MetricsPrefix),
			"metrics",
			collPtrValue[types.SettlementMetrics](cdc),
		),
		CACRoyalties: collections.NewMap(
			sb,
			collections.NewPrefix(types.CACRoyaltyPrefix),
			"cac_royalties",
			collections.StringKey,
			collPtrValue[types.CACRoyaltyRecord](cdc),
		),
		CACStats: collections.NewMap(
			sb,
			collections.NewPrefix(types.CACStatsPrefix),
			"cac_stats",
			collections.StringKey,
			collPtrValue[types.CACRoyaltyStats](cdc),
		),
		CACSeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.CACSeqPrefix),
			"cac_seq",
		),
		LockExpiry: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.LockExpiryPrefix),
			"lock_expiry",
			collections.PairKeyCodec(sdk.TimeKey, collections.StringKey),
		),
		PendingSettlements: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.PendingSettlementsPrefix),
			"pending_settlements",
			collections.StringKey,
		),
		SettlementsByTime: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.SettlementsByTimePrefix),
			"settlements_by_time",
			collections.PairKeyCodec(sdk.TimeKey, collections.StringKey),
		),
		FinalizedLocks: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.FinalizedLocksPrefix),
			"finalized_locks",
			collections.PairKeyCodec(sdk.TimeKey, collections.StringKey),
		),
		LockReceipts: collections.NewMap(
			sb,
			collections.NewPrefix(types.LockReceiptsPrefix),
			"lock_receipts",
			collections.StringKey,
			collections.StringValue,
		),
		LocksByQuote: collections.NewMap(
			sb,
			collections.NewPrefix(types.LocksByQuotePrefix),
			"locks_by_quote",
			collections.StringKey,
			collections.StringValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build credits schema: %w", err))
	}
	state.Schema = schema

	return Keeper{
		cdc:             cdc,
		storeService:    storeService,
		bankKeeper:      bankKeeper,
		accountKeeper:   accountKeeper,
		insuranceKeeper: insuranceKeeper,
		registryKeeper:  registryKeeper,
		reserveKeeper:   reserveKeeper,
		nftKeeper:       nftKeeper,
		authority:       authority,
		state:           state,
		paramCache:      &keeperParamCache{},
	}
}

// Schema returns the underlying collections schema.
func (k Keeper) Schema() collections.Schema { return k.state.Schema }

// Logger returns a module-prefixed logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/credits")
}

// GetParams retrieves module parameters.
func (k Keeper) GetParams(ctx context.Context) *types.Params {
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			params := types.DefaultParams()
			if k.paramCache != nil {
				k.paramCache.setCreditDenom(params.CreditDenom)
			}
			return params
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		k.Logger(sdkCtx).Error("credits params load failed, returning defaults", "error", err)
		params := types.DefaultParams()
		if k.paramCache != nil {
			k.paramCache.setCreditDenom(params.CreditDenom)
		}
		return params
	}
	if params == nil {
		params := types.DefaultParams()
		if k.paramCache != nil {
			k.paramCache.setCreditDenom(params.CreditDenom)
		}
		return params
	}
	if k.paramCache != nil {
		k.paramCache.setCreditDenom(params.CreditDenom)
	}
	return params
}

// SetParams updates module parameters.
func (k Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := params.Validate(); err != nil {
		return err
	}
	if err := k.state.Params.Set(ctx, params); err != nil {
		return err
	}
	if k.paramCache != nil {
		k.paramCache.setCreditDenom(params.CreditDenom)
	}
	return nil
}

func (k Keeper) creditDenomForLocks(ctx context.Context) string {
	if denom, ok := k.paramCache.getCreditDenom(); ok {
		return denom
	}
	params := k.GetParams(ctx)
	return normalizeCreditDenom(params.CreditDenom)
}

// MintCredits mints LAC credits to a recipient account.
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
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleAccountName, sdk.NewCoins(amount)); err != nil {
		return fmt.Errorf("mint credits failed: %w", err)
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, recipient, sdk.NewCoins(amount)); err != nil {
		return fmt.Errorf("send minted credits failed: %w", err)
	}
	_ = reason
	return nil
}

// BurnCreditsFromAccount burns LAC credits from a user account.
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
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleAccountName, sdk.NewCoins(amount)); err != nil {
		return fmt.Errorf("collect credits failed: %w", err)
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleAccountName, sdk.NewCoins(amount)); err != nil {
		return fmt.Errorf("burn credits failed: %w", err)
	}
	// Best-effort: burn the corresponding LUME to maintain the 1:1 invariant.
	// LUME backing may not exist if credits were minted directly (e.g. governance, tests).
	lumeEquivalent := sdk.NewCoin(types.DefaultLumeDenom, amount.Amount)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	bal := k.bankKeeper.GetBalance(ctx, moduleAddr, lumeEquivalent.Denom)
	if bal.Amount.GTE(lumeEquivalent.Amount) {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleAccountName, sdk.NewCoins(lumeEquivalent)); err != nil {
			return fmt.Errorf("burn corresponding LUME failed: %w", err)
		}
	}
	_ = reason
	return nil
}

// NextLockID computes the next deterministic lock identifier.
func (k Keeper) NextLockID(ctx context.Context) (string, error) {
	id, err := k.state.LockSeq.Next(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to allocate lock id: %w", err)
	}
	return fmt.Sprintf("lock-%d", id), nil
}

// GetLock fetches a lock by ID.
func (k Keeper) GetLock(ctx context.Context, lockID string) (*types.Lock, bool) {
	lock, err := k.state.Locks.Get(ctx, lockID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(sdk.UnwrapSDKContext(ctx)).Error("failed to read lock", "lock_id", lockID, "error", err)
		return nil, false
	}
	if lock == nil {
		return nil, false
	}
	return lock, true
}

// SaveLock writes a lock record to state.
func (k Keeper) SaveLock(ctx context.Context, lock *types.Lock) error {
	if lock == nil {
		return fmt.Errorf("lock cannot be nil")
	}
	if err := k.state.Locks.Set(ctx, lock.LockId, lock); err != nil {
		return fmt.Errorf("failed to store lock %s: %w", lock.LockId, err)
	}
	return nil
}

// DeleteLock removes a lock from state.
func (k Keeper) DeleteLock(ctx context.Context, lockID string) error {
	if err := k.state.Locks.Remove(ctx, lockID); err != nil {
		return fmt.Errorf("failed to remove lock %s: %w", lockID, err)
	}
	return nil
}

// IterateLocks walks through all lock records invoking the callback for each entry.
// If the callback returns true the iteration stops early.
func (k Keeper) IterateLocks(ctx context.Context, cb func(*types.Lock) bool) error {
	return k.state.Locks.Walk(ctx, nil, func(_ string, lock *types.Lock) (bool, error) {
		if lock == nil {
			return false, nil
		}
		return cb(lock), nil
	})
}

// SetLockSequence hard-sets the next lock sequence value.
func (k Keeper) SetLockSequence(ctx context.Context, value uint64) error {
	return k.state.LockSeq.Set(ctx, value)
}

// ModuleAddress returns the module account address.
func (k Keeper) ModuleAddress() sdk.AccAddress {
	return k.accountKeeper.GetModuleAddress(types.ModuleAccountName)
}

// EnsureModuleAccount returns an error if the module account has not been set up.
func (k Keeper) EnsureModuleAccount(_ sdk.Context) error {
	if addr := k.ModuleAddress(); addr == nil {
		return fmt.Errorf("credits module account has not been set")
	}
	return nil
}

// SanitizeLockTTL clamps requested TTL against module parameters.
func (k Keeper) SanitizeLockTTL(ctx context.Context, requested time.Duration) time.Duration {
	params := k.GetParams(ctx)
	return params.LockTTL(requested)
}

// Authority exposes the address allowed to perform parameter changes.
func (k Keeper) Authority() string { return k.authority }

// BankKeeper exposes the bank keeper dependency (useful for servers and tests).
func (k Keeper) BankKeeper() types.BankKeeper { return k.bankKeeper }

// AccountKeeper exposes the account keeper dependency.
func (k Keeper) AccountKeeper() types.AccountKeeper { return k.accountKeeper }

// StoreService returns the store service backing the keeper.
func (k Keeper) StoreService() corestore.KVStoreService { return k.storeService }

// RevenueSplits defines the revenue distribution ratios
type RevenueSplits struct {
	PublisherBPS     uint32 // Basis points for publisher (7000 = 70%)
	RouterBPS        uint32 // Basis points for router (2000 = 20%) before partner/treasury carve-outs
	OriginSurfaceBPS uint32 // Basis points for origin surface (toolpack curator)
	TreasuryBPS      uint32 // Basis points for protocol treasury
	ReferrerBPS      uint32 // Basis points for referrer (1000 = 10%)
}

// SettlementRequest contains the data for processing a settlement
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
	ActionID       string
	Stage          string
	FillInfo       *FillInfo
	RebateAmount   sdk.Coins
	SessionID      string
	QuoteID        string
	LockID         string
	IntentHash     string
}

// FillInfo contains details about a partial fill
type FillInfo struct {
	FillID           string
	FillAmount       sdk.Coin
	FillPrice        string
	CumulativeFilled sdk.Coin
	Timestamp        time.Time
}

const lacPrecision = int64(1_000_000)

func derivePolicyID(snapshot string) string {
	trimmed := strings.TrimSpace(snapshot)
	if trimmed == "" {
		return ""
	}
	parts := strings.SplitN(trimmed, "@", 2)
	policy := strings.TrimSpace(parts[0])
	if policy == "" {
		return ""
	}
	return strings.ToLower(policy)
}

func sanitizeLabel(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "unknown"
	}
	if len(trimmed) > 48 {
		return trimmed[:48]
	}
	return trimmed
}

func normalizeOriginID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	normalized := strings.ToLower(trimmed)
	if len(normalized) > 64 {
		return "", fmt.Errorf("origin_id too long (max 64 chars)")
	}
	parts := strings.Split(normalized, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("origin_id must be <namespace>:<surface>")
	}
	if err := validateOriginPart(parts[0], "origin_id namespace"); err != nil {
		return "", err
	}
	if err := validateOriginPart(parts[1], "origin_id surface"); err != nil {
		return "", err
	}
	return parts[0] + ":" + parts[1], nil
}

func validateOriginPart(part string, field string) error {
	if part == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}
	if len(part) > 32 {
		return fmt.Errorf("%s too long (max 32 chars)", field)
	}
	for i := 0; i < len(part); i++ {
		ch := part[i]
		isAlpha := ch >= 'a' && ch <= 'z'
		isNum := ch >= '0' && ch <= '9'
		isPunct := ch == '-' || ch == '_'
		if !isAlpha && !isNum && !isPunct {
			return fmt.Errorf("%s contains invalid character %q", field, ch)
		}
		if i == 0 && isPunct {
			return fmt.Errorf("%s must start with [a-z0-9]", field)
		}
	}
	return nil
}

// BurnLAC burns LAC tokens according to the burn rate
// This implements the β_spend burn fraction from the whitepaper section 8
func (k Keeper) BurnLAC(ctx context.Context, amount sdk.Coins, burnRateBPS uint32) error {
	if burnRateBPS == 0 {
		return nil // No burn if rate is 0
	}

	params := k.GetParams(ctx)
	creditDenom := params.CreditDenom
	if creditDenom == "" {
		creditDenom = types.DefaultCreditDenom
	}

	// Calculate burn amount (burnRateBPS is in basis points, so divide by 10000)
	burnAmount := sdk.NewCoins()
	lumeBurnAmount := sdk.NewCoins()
	for _, coin := range amount {
		if coin.Denom == creditDenom {
			burnQty, err := SafePercentage(coin.Amount, burnRateBPS)
			if err != nil {
				return err
			}
			if burnQty.IsPositive() {
				burnAmount = burnAmount.Add(sdk.NewCoin(creditDenom, burnQty))
				lumeBurnAmount = lumeBurnAmount.Add(sdk.NewCoin(types.DefaultLumeDenom, burnQty))
			}
		}
	}

	if burnAmount.IsZero() {
		return nil
	}

	// Burn the LAC coins
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleAccountName, burnAmount); err != nil {
		return err
	}
	// Best-effort: burn the corresponding LUME from the escrow to maintain the 1:1 invariant.
	// LUME backing may not exist if credits were minted directly (e.g. governance, tests).
	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	for _, coin := range lumeBurnAmount {
		bal := k.bankKeeper.GetBalance(ctx, moduleAddr, coin.Denom)
		if bal.Amount.GTE(coin.Amount) {
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleAccountName, sdk.NewCoins(coin)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (k Keeper) distributeCuratorRoyalty(_ context.Context, _ string, _ sdk.Coins) (sdk.Coins, error) {
	// Deprecated: curator royalties are now handled via origin-surface settlement splits.
	_ = k
	return sdk.NewCoins(), nil
}

// SettlementResult contains the actual amounts distributed during settlement
type SettlementResult struct {
	BurnAmount          sdk.Coins
	PublisherAmount     sdk.Coins
	RouterAmount        sdk.Coins
	OriginSurfaceAmount sdk.Coins
	TreasuryAmount      sdk.Coins
	ReferrerAmount      sdk.Coins
	RefundAmount        sdk.Coins
}

const (
	defaultPublisherShareBPS = uint32(7000)
	defaultRouterShareBPS    = uint32(2000)
	defaultReferrerShareBPS  = uint32(1000)

	// DefaultTreasuryContributionBPS matches the typical governance range in
	// specs/governance/parameters.md (100–300 bps) and the Injective partnership
	// illustrative split manifest (300 bps).
	DefaultTreasuryContributionBPS = uint32(300)
)

func (k Keeper) resolveOriginSurface(ctx context.Context, toolpackID string) (sdk.AccAddress, uint32, error) {
	if strings.TrimSpace(toolpackID) == "" || k.nftKeeper == nil {
		return nil, 0, nil
	}

	pack, found, err := k.nftKeeper.GetToolpack(ctx, toolpackID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch toolpack %s: %w", toolpackID, err)
	}
	if !found || pack == nil || !pack.Active {
		return nil, 0, nil
	}
	if pack.RoyaltyBps == 0 {
		return nil, 0, nil
	}

	curatorAddr, err := sdk.AccAddressFromBech32(pack.Curator)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid curator address for toolpack %s: %w", toolpackID, err)
	}

	return curatorAddr, pack.RoyaltyBps, nil
}

// ProcessSettlement processes a complete settlement with burn and distribution
// This is the main entry point for processing receipts from the router
func (k Keeper) ProcessSettlement(ctx context.Context, receipt SettlementRequest) (*SettlementResult, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(ctx)
	creditDenom := params.CreditDenom
	if creditDenom == "" {
		creditDenom = types.DefaultCreditDenom
	}

	receiptID := strings.TrimSpace(receipt.ReceiptID)
	if receiptID == "" {
		return nil, fmt.Errorf("receipt id cannot be empty")
	}
	receipt.ReceiptID = receiptID
	receipt.Stage = strings.TrimSpace(receipt.Stage)

	// Determine if this is a final settlement
	isFinal := receipt.Stage == "" || strings.EqualFold(receipt.Stage, "finalized")

	// Accumulate if existing record found (for async actions)
	var existingRecord *types.SettlementRecord
	if existing, found := k.GetSettlement(ctx, receiptID); found {
		if existing.Status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
			return nil, types.ErrSettlementFailed.Wrapf("already completed: %s", receiptID)
		}
		existingRecord = existing

		// Treat input as delta and accumulate from existing record
		existingCoins := sdk.NewCoins(existing.TotalCost...)
		receipt.TotalAmount = receipt.TotalAmount.Add(existingCoins...)

		// Preserve metadata from existing record if not provided
		if receipt.ToolID == "" {
			receipt.ToolID = existing.ToolId
		}
		if receipt.UserID == "" {
			receipt.UserID = existing.UserId
		}
		if receipt.PublisherID == "" {
			receipt.PublisherID = existing.PublisherId
		}
		if receipt.ActionID == "" {
			receipt.ActionID = existing.ActionId
		}
		if receipt.RouterID == "" {
			receipt.RouterID = existing.RouterId
		}
		if receipt.ReferrerID == "" {
			receipt.ReferrerID = existing.ReferrerId
		}
		if receipt.ToolpackID == "" {
			receipt.ToolpackID = existing.ToolpackId
		}
		if receipt.OriginID == "" {
			receipt.OriginID = existing.OriginId
		}

		// Restore addresses from IDs if missing (critical for distribution validation)
		if receipt.PublisherAddr == nil && existing.PublisherId != "" {
			if addr, err := sdk.AccAddressFromBech32(existing.PublisherId); err == nil {
				receipt.PublisherAddr = addr
			}
		}
		if receipt.RouterAddr == nil && existing.RouterId != "" {
			if addr, err := sdk.AccAddressFromBech32(existing.RouterId); err == nil {
				receipt.RouterAddr = addr
			}
		}
		if receipt.ReferrerAddr == nil && existing.ReferrerId != "" {
			if addr, err := sdk.AccAddressFromBech32(existing.ReferrerId); err == nil {
				receipt.ReferrerAddr = addr
			}
		}
	}

	if strings.TrimSpace(receipt.OriginID) != "" {
		normalized, err := normalizeOriginID(receipt.OriginID)
		if err != nil {
			return nil, fmt.Errorf("invalid origin_id: %w", err)
		}
		receipt.OriginID = normalized
	}

	// 1. Validate settlement request
	// We allow zero amounts for fully discounted/prepaid transactions (Vaults).
	if !receipt.TotalAmount.IsValid() {
		return nil, fmt.Errorf("invalid settlement amount")
	}

	// 2. Get burn and insurance rates from params (β_spend from whitepaper section 8.2)
	burnRate := params.BurnRateSpendBps
	insuranceRate := params.InsuranceBps

	// Validate rates using safe math helper.
	// NOTE: Origin-surface and treasury shares are carved out of the router share so the
	// default publisher/referrer economics remain stable when these beneficiaries are enabled.
	splits := RevenueSplits{
		PublisherBPS: defaultPublisherShareBPS,
		RouterBPS:    defaultRouterShareBPS,
		ReferrerBPS:  defaultReferrerShareBPS,
	}

	treasuryAddr := sdk.AccAddress(nil)
	treasuryBPS := uint32(0)
	if strings.TrimSpace(params.TreasuryAddress) != "" {
		addr, err := sdk.AccAddressFromBech32(params.TreasuryAddress)
		if err != nil {
			return nil, fmt.Errorf("invalid treasury address: %w", err)
		}
		treasuryAddr = addr
		treasuryBPS = DefaultTreasuryContributionBPS
	}

	originSurfaceAddr, originSurfaceBPS, err := k.resolveOriginSurface(ctx, receipt.ToolpackID)
	if err != nil {
		return nil, err
	}

	// If no referrer is provided, the referrer share is reallocated to the router.
	if receipt.ReferrerAddr == nil && splits.ReferrerBPS > 0 {
		splits.RouterBPS += splits.ReferrerBPS
		splits.ReferrerBPS = 0
		receipt.ReferrerID = ""
	}

	// Carve origin-surface and treasury shares out of router share.
	// This keeps total shares at 10_000 bps without changing publisher/referrer defaults.
	if originSurfaceBPS+treasuryBPS > splits.RouterBPS {
		return nil, fmt.Errorf(
			"origin-surface and treasury shares exceed router share: origin_surface=%d treasury=%d router=%d",
			originSurfaceBPS, treasuryBPS, splits.RouterBPS,
		)
	}
	splits.OriginSurfaceBPS = originSurfaceBPS
	splits.TreasuryBPS = treasuryBPS
	splits.RouterBPS = splits.RouterBPS - originSurfaceBPS - treasuryBPS

	if splits.RouterBPS > 0 && receipt.RouterAddr == nil {
		return nil, fmt.Errorf("router address is required when router share is non-zero")
	}
	if splits.OriginSurfaceBPS > 0 && originSurfaceAddr == nil {
		return nil, fmt.Errorf("origin-surface address is required when origin-surface share is non-zero")
	}
	if splits.TreasuryBPS > 0 && treasuryAddr == nil {
		return nil, fmt.Errorf("treasury address is required when treasury share is non-zero")
	}

	if err := ValidateRates(
		burnRate, insuranceRate,
		splits.PublisherBPS, splits.RouterBPS, splits.OriginSurfaceBPS, splits.TreasuryBPS, splits.ReferrerBPS,
	); err != nil {
		return nil, fmt.Errorf("invalid rates: %w", err)
	}

	// 3. Validate all coins are credit denom BEFORE any state changes
	for _, coin := range receipt.TotalAmount {
		if coin.Denom != creditDenom {
			return nil, fmt.Errorf("invalid denom in settlement: expected %s, got %s", creditDenom, coin.Denom)
		}
	}

	// 4. Calculate net amount after burn AND insurance before any balance mutation.
	netAmount := sdk.NewCoins()
	insuranceAmount := sdk.NewCoins()
	burnAmountCoins := sdk.NewCoins()
	publisherAmount := sdk.NewCoins()
	routerAmount := sdk.NewCoins()
	originSurfaceAmount := sdk.NewCoins()
	treasuryAmount := sdk.NewCoins()
	referrerAmount := sdk.NewCoins()
	workflowAuthorAmount := sdk.NewCoins()
	var publisherCoins sdk.Coins
	routerPayoutAmount := sdk.NewCoins()

	if isFinal {
		if insuranceRate == 0 {
			k.Logger(sdkCtx).Info(
				"insurance premium collection disabled pending claims rollout",
				"insurance_bps", insuranceRate,
				"receipt_id", receipt.ReceiptID,
			)
		}

		for _, coin := range receipt.TotalAmount {
			if coin.Denom == creditDenom {
				burn, err := SafePercentage(coin.Amount, burnRate)
				if err != nil {
					return nil, fmt.Errorf("burn calculation failed: %w", err)
				}
				ins, err := SafePercentage(coin.Amount, insuranceRate)
				if err != nil {
					return nil, fmt.Errorf("insurance calculation failed: %w", err)
				}

				// net = coin - burn - ins
				// We use direct subtraction here as we've validated rates <= 100%
				net := coin.Amount.Sub(burn).Sub(ins)

				if net.IsPositive() {
					netAmount = netAmount.Add(sdk.NewCoin(creditDenom, net))
				}
				if ins.IsPositive() {
					insuranceAmount = insuranceAmount.Add(sdk.NewCoin(creditDenom, ins))
				}
				if burn.IsPositive() {
					burnAmountCoins = burnAmountCoins.Add(sdk.NewCoin(creditDenom, burn))
				}
			}
		}

		calculateDistributionAmounts := func(amount sdk.Coins) (sdk.Coins, sdk.Coins, sdk.Coins, sdk.Coins, sdk.Coins, error) {
			pubAmount := sdk.NewCoins()
			routerShareAmount := sdk.NewCoins()
			originAmount := sdk.NewCoins()
			treasuryShareAmount := sdk.NewCoins()
			refAmount := sdk.NewCoins()

			for _, coin := range amount {
				if coin.Denom != creditDenom {
					return nil, nil, nil, nil, nil, fmt.Errorf("invalid denom in settlement split: expected %s, got %s", creditDenom, coin.Denom)
				}

				pub, router, origin, treasury, ref, err := CalculateSplit(
					coin.Amount,
					splits.PublisherBPS,
					splits.RouterBPS,
					splits.OriginSurfaceBPS,
					splits.TreasuryBPS,
					splits.ReferrerBPS,
				)
				if err != nil {
					return nil, nil, nil, nil, nil, fmt.Errorf("split calculation failed: %w", err)
				}

				if pub.IsPositive() {
					pubAmount = pubAmount.Add(sdk.NewCoin(coin.Denom, pub))
				}
				if router.IsPositive() {
					routerShareAmount = routerShareAmount.Add(sdk.NewCoin(coin.Denom, router))
				}
				if origin.IsPositive() {
					originAmount = originAmount.Add(sdk.NewCoin(coin.Denom, origin))
				}
				if treasury.IsPositive() {
					treasuryShareAmount = treasuryShareAmount.Add(sdk.NewCoin(coin.Denom, treasury))
				}
				if ref.IsPositive() {
					refAmount = refAmount.Add(sdk.NewCoin(coin.Denom, ref))
				}
			}

			return pubAmount, routerShareAmount, originAmount, treasuryShareAmount, refAmount, nil
		}

		isCACRoyaltySettlement := receipt.CacheHit && receipt.OriginToolID != "" && receipt.OriginToolID != receipt.ToolID
		validateDistributionRecipients := func(pubAmount, routerShareAmount, originAmount, treasuryShareAmount, refAmount sdk.Coins) error {
			if !originAmount.IsZero() && originSurfaceAddr == nil {
				return fmt.Errorf("origin-surface amount is non-zero but curator address is missing")
			}
			if !treasuryShareAmount.IsZero() && treasuryAddr == nil {
				return fmt.Errorf("treasury amount is non-zero but treasury address is missing")
			}
			if !routerShareAmount.IsZero() && receipt.RouterAddr == nil {
				return fmt.Errorf("router amount is non-zero but router address is missing")
			}
			if !refAmount.IsZero() && receipt.ReferrerAddr == nil {
				return fmt.Errorf("referrer amount is non-zero but referrer address is missing")
			}
			if !pubAmount.IsZero() && !isCACRoyaltySettlement && receipt.PublisherAddr == nil {
				return fmt.Errorf("publisher amount is non-zero but publisher address is missing")
			}
			if !receipt.RebateAmount.IsZero() &&
				!routerShareAmount.IsZero() &&
				receipt.RebateAmount[0].Denom == routerShareAmount[0].Denom &&
				receipt.RebateAmount.IsAllLTE(routerShareAmount) &&
				receipt.PublisherAddr == nil {
				return fmt.Errorf("rebate amount is non-zero but publisher address is missing")
			}
			return nil
		}

		preflightNetAmount := netAmount
		if !insuranceAmount.IsZero() {
			preflightNetAmount = preflightNetAmount.Add(insuranceAmount...)
		}
		preflightPublisherAmount, preflightRouterAmount, preflightOriginAmount, preflightTreasuryAmount, preflightReferrerAmount, err := calculateDistributionAmounts(preflightNetAmount)
		if err != nil {
			return nil, err
		}
		if err := validateDistributionRecipients(preflightPublisherAmount, preflightRouterAmount, preflightOriginAmount, preflightTreasuryAmount, preflightReferrerAmount); err != nil {
			return nil, err
		}

		// 5. Burn the protocol portion after validating all deterministic payout prerequisites.
		if err := k.BurnLAC(ctx, receipt.TotalAmount, burnRate); err != nil {
			return nil, fmt.Errorf("failed to burn LAC: %w", err)
		}

		// Send insurance contribution to insurance module.
		if !insuranceAmount.IsZero() {
			if k.insuranceKeeper == nil {
				// Insurance keeper not available - redistribute to revenue splits
				k.Logger(sdkCtx).Warn("insurance keeper unavailable, redistributing insurance to revenue splits",
					"amount", insuranceAmount.String())
				netAmount = netAmount.Add(insuranceAmount...)
			} else {
				// We pass the original receipt ID (not settlement ID) so claims can be filed against it.
				// Claims use the original receipt ID for verification.
				if err := k.insuranceKeeper.ContributeToPool(ctx, receipt.ReceiptID, receipt.ToolID, receipt.PublisherID, receipt.PolicySnapshot, receipt.UserID, insuranceAmount); err != nil {
					// If insurance contribution fails, redistribute the insurance amount back to net amount
					k.Logger(sdkCtx).Warn("failed to contribute to insurance pool, redistributing to revenue splits",
						"error", err,
						"amount", insuranceAmount.String(),
						"receipt_id", receipt.ReceiptID)
					netAmount = netAmount.Add(insuranceAmount...)
				} else {
					sdkCtx.EventManager().EmitEvent(
						sdk.NewEvent(
							"insurance_contribution_sent",
							sdk.NewAttribute("receipt_id", receipt.ReceiptID),
							sdk.NewAttribute("amount", insuranceAmount.String()),
						),
					)
				}
			}
		}

		// 6. Distribute the net amount according to splits.
		publisherAmount, routerAmount, originSurfaceAmount, treasuryAmount, referrerAmount, err = calculateDistributionAmounts(netAmount)
		if err != nil {
			return nil, err
		}
		routerPayoutAmount = routerAmount

		if err := validateDistributionRecipients(publisherAmount, routerAmount, originSurfaceAmount, treasuryAmount, referrerAmount); err != nil {
			return nil, err
		}
		// Distribute origin surface share (toolpack curator) and record in the NFT module for observability.
		if !originSurfaceAmount.IsZero() {
			if originSurfaceAddr == nil {
				return nil, fmt.Errorf("origin-surface amount is non-zero but curator address is missing")
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, originSurfaceAddr, originSurfaceAmount); err != nil {
				return nil, fmt.Errorf("failed to send to origin surface: %w", err)
			}
			if k.nftKeeper != nil && strings.TrimSpace(receipt.ToolpackID) != "" {
				for _, coin := range originSurfaceAmount {
					if err := k.nftKeeper.RecordRoyaltyPayout(ctx, k.authority, receipt.ToolpackID, coin); err != nil {
						return nil, fmt.Errorf("failed to record origin-surface royalty payout: %w", err)
					}
				}
			}
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					types.EventTypeDistribute,
					sdk.NewAttribute(types.AttributeKeyToolpackID, receipt.ToolpackID),
					sdk.NewAttribute(types.AttributeKeyAmount, originSurfaceAmount.String()),
					sdk.NewAttribute("recipient_role", "origin_surface"),
					sdk.NewAttribute("recipient", originSurfaceAddr.String()),
				),
			)
		}

		// Distribute treasury share (if configured).
		if !treasuryAmount.IsZero() {
			if treasuryAddr == nil {
				return nil, fmt.Errorf("treasury amount is non-zero but treasury address is missing")
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, treasuryAddr, treasuryAmount); err != nil {
				return nil, fmt.Errorf("failed to send to treasury: %w", err)
			}
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					types.EventTypeDistribute,
					sdk.NewAttribute(types.AttributeKeyAmount, treasuryAmount.String()),
					sdk.NewAttribute("recipient_role", "treasury"),
					sdk.NewAttribute("recipient", treasuryAddr.String()),
				),
			)
		}

		// Handle CAC (Content-Addressed Cache) royalties
		if receipt.CacheHit && receipt.OriginToolID != "" && receipt.OriginToolID != receipt.ToolID {
			if publisherAmount.IsZero() {
				k.Logger(sdkCtx).Info("skipping CAC processing for zero publisher amount (tiny settlement)",
					"net_amount", netAmount.String(),
					"origin_tool", receipt.OriginToolID,
					"serving_tool", receipt.ToolID)
				publisherCoins = sdk.NewCoins()
			} else {
				originAmt, servingAmt, err := k.ProcessCACRoyalty(ctx, receipt.OriginToolID, receipt.ToolID, publisherAmount)
				if err != nil {
					k.Logger(sdkCtx).Error("CAC royalty distribution failed completely", "error", err)
					return nil, fmt.Errorf("failed to process CAC royalty distribution: %w", err)
				}

				publisherCoins = originAmt.Add(servingAmt...)

				if !originAmt.IsZero() {
					if err := k.UpdateCACRoyaltyStats(ctx, receipt.OriginToolID, true, originAmt); err != nil {
						k.Logger(sdkCtx).Error("failed to update origin CAC stats", "tool_id", receipt.OriginToolID, "error", err)
					}
				}
				if !servingAmt.IsZero() {
					paidToOrigin := publisherAmount.Sub(servingAmt...)
					if err := k.UpdateCACRoyaltyStats(ctx, receipt.ToolID, false, paidToOrigin); err != nil {
						k.Logger(sdkCtx).Error("failed to update serving CAC stats", "tool_id", receipt.ToolID, "error", err)
					}
				}
			}
		} else {
			publisherCoins = sdk.NewCoins()
			if !publisherAmount.IsZero() {
				if receipt.PublisherAddr == nil {
					return nil, fmt.Errorf("publisher amount is non-zero but publisher address is missing")
				}
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, receipt.PublisherAddr, publisherAmount); err != nil {
					return nil, fmt.Errorf("failed to send to publisher: %w", err)
				}
				publisherCoins = publisherAmount
			}
		}

		// Distribute router share
		if !routerAmount.IsZero() && receipt.RouterAddr != nil {
			// Apply quality rebate if present: deduct from router, add to publisher
			if !receipt.RebateAmount.IsZero() {
				// Single-denom settlement assumption: the rebate and the
				// router share must be in the same denom. Multi-denom
				// settlements would require a per-denom match loop (and
				// a decision about partial rebates across denoms), which
				// no live chain configuration needs today — the whole
				// credits pipeline uses ulac. If multi-denom settlement
				// becomes a requirement, this branch must grow a loop
				// over receipt.RebateAmount coins and a matching lookup
				// against routerPayoutAmount; the current single-index
				// access [0] would silently drop subsequent rebate coins.
				if receipt.RebateAmount[0].Denom == routerPayoutAmount[0].Denom {
					if receipt.RebateAmount.IsAllLTE(routerPayoutAmount) {
						routerPayoutAmount = routerPayoutAmount.Sub(receipt.RebateAmount...)

						// Pay rebate to publisher
						if receipt.PublisherAddr == nil {
							return nil, fmt.Errorf("rebate amount is non-zero but publisher address is missing")
						}
						if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, receipt.PublisherAddr, receipt.RebateAmount); err != nil {
							return nil, fmt.Errorf("failed to send rebate to publisher: %w", err)
						}
						// Update publisherCoins for event emission
						publisherCoins = publisherCoins.Add(receipt.RebateAmount...)
					} else {
						k.Logger(sdkCtx).Error("rebate exceeds router share, skipping rebate", "rebate", receipt.RebateAmount, "router_share", routerAmount)
					}
				} else {
					k.Logger(sdkCtx).Error("rebate denom does not match router share, skipping rebate",
						"rebate_denom", receipt.RebateAmount[0].Denom,
						"router_denom", routerPayoutAmount[0].Denom,
						"rebate", receipt.RebateAmount,
						"router_share", routerAmount)
				}
			}

			if !routerPayoutAmount.IsZero() {
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, receipt.RouterAddr, routerPayoutAmount); err != nil {
					return nil, fmt.Errorf("failed to send to router: %w", err)
				}
			}
		}

		// Distribute referrer share
		if !referrerAmount.IsZero() && receipt.ReferrerAddr != nil {
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, receipt.ReferrerAddr, referrerAmount); err != nil {
				return nil, fmt.Errorf("failed to send to referrer: %w", err)
			}
		}
	}

	k.Logger(sdkCtx).Debug(
		"settlement split breakdown",
		"receipt_id", receipt.ReceiptID,
		"is_final", isFinal,
		"total_amount", receipt.TotalAmount.String(),
		"net_amount", netAmount.String(),
		"burn_amount", burnAmountCoins.String(),
		"insurance_amount", insuranceAmount.String(),
		"publisher_amount", publisherAmount.String(),
		"router_amount", routerPayoutAmount.String(),
		"origin_surface_amount", originSurfaceAmount.String(),
		"treasury_amount", treasuryAmount.String(),
		"referrer_amount", referrerAmount.String(),
		"workflow_author_amount", workflowAuthorAmount.String(),
		"publisher_bps", splits.PublisherBPS,
		"router_bps", splits.RouterBPS,
		"origin_surface_bps", splits.OriginSurfaceBPS,
		"treasury_bps", splits.TreasuryBPS,
		"referrer_bps", splits.ReferrerBPS,
		"workflow_author_bps", uint32(0),
	)

	// 7. Events and Record keeping
	if !burnAmountCoins.IsZero() {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeBurn,
				sdk.NewAttribute(types.AttributeKeySettlementID, receipt.ReceiptID),
				sdk.NewAttribute(types.AttributeKeyAmount, burnAmountCoins.String()),
				sdk.NewAttribute("burn_rate_bps", fmt.Sprintf("%d", burnRate)),
			),
		)
	}

	if !publisherCoins.IsZero() || !routerPayoutAmount.IsZero() || !originSurfaceAmount.IsZero() || !treasuryAmount.IsZero() || !referrerAmount.IsZero() {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeDistribute,
				sdk.NewAttribute(types.AttributeKeySettlementID, receipt.ReceiptID),
				sdk.NewAttribute(types.AttributeKeyPublisher, receipt.PublisherID),
				sdk.NewAttribute("publisher_amount", publisherCoins.String()),
				sdk.NewAttribute(types.AttributeKeyRouter, receipt.RouterID),
				sdk.NewAttribute("router_amount", routerPayoutAmount.String()),
				sdk.NewAttribute("origin_surface_amount", originSurfaceAmount.String()),
				sdk.NewAttribute("treasury_amount", treasuryAmount.String()),
				sdk.NewAttribute("referrer_id", receipt.ReferrerID),
				sdk.NewAttribute("referrer_amount", referrerAmount.String()),
			),
		)
	}

	// Determine status and fill count
	fillCount := uint64(1)
	if existingRecord != nil {
		fillCount = existingRecord.FillCount + 1
	}

	status := types.SettlementStatus_SETTLEMENT_STATUS_PENDING
	if isFinal {
		status = types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED
	}

	// Anchor the dispute window to the FIRST PENDING attempt.
	// On PENDING→PENDING re-settles (async partial fills, router retries),
	// preserve the earliest Timestamp so BeginBlocker's
	// `currentTime - settlementTime >= disputeWindow` eligibility check
	// cannot be reset by a router that re-calls SettleCredits each block
	// (lumera_ai-1y27e: otherwise a malicious or compromised router keeps
	// the settlement perpetually fresh, stranding user credits behind
	// UnlockCredits' refusal to release while PENDING).
	// Transitions into COMPLETED still advance the Timestamp so it
	// aligns with CompletedAt for downstream pruning.
	timestamp := sdkCtx.BlockTime()
	if existingRecord != nil &&
		!existingRecord.Timestamp.IsZero() &&
		existingRecord.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING &&
		status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
		timestamp = existingRecord.Timestamp
	}

	settlementRecord := &types.SettlementRecord{
		Id:           receipt.ReceiptID,
		ToolId:       receipt.ToolID,
		PublisherId:  receipt.PublisherID,
		UserId:       receipt.UserID,
		RouterId:     receipt.RouterID,
		ReferrerId:   receipt.ReferrerID,
		TotalCost:    types.CoinsToProto(receipt.TotalAmount),
		BurnAmount:   types.CoinsToProto(burnAmountCoins),
		NetAmount:    types.CoinsToProto(netAmount),
		CacheHit:     receipt.CacheHit,
		OriginToolId: receipt.OriginToolID,
		OriginId:     strings.TrimSpace(receipt.OriginID),
		OriginShare:  types.CoinsToProto(originSurfaceAmount),
		Status:       status,
		Timestamp:    timestamp,
		CompletedAt:  nil,
		ToolpackId:   receipt.ToolpackID,
		ActionId:     receipt.ActionID,
		Stage:        receipt.Stage,
		FillCount:    fillCount,
		LockId:       receipt.LockID,
	}
	if isFinal {
		completedAt := sdkCtx.BlockTime()
		settlementRecord.CompletedAt = &completedAt
	}
	if receipt.FillInfo != nil {
		settlementRecord.CumulativeFilled = receipt.FillInfo.CumulativeFilled
	}

	if err := k.UpdateSettlement(ctx, settlementRecord); err != nil {
		k.Logger(sdkCtx).Error("failed to persist settlement record", "receipt_id", receipt.ReceiptID, "error", err)
		return nil, fmt.Errorf("failed to persist settlement record: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSettlement,
			sdk.NewAttribute(types.AttributeKeySettlementID, receipt.ReceiptID),
			sdk.NewAttribute(types.AttributeKeyToolID, receipt.ToolID),
			sdk.NewAttribute(types.AttributeKeyAmount, receipt.TotalAmount.String()),
			sdk.NewAttribute("net_amount", netAmount.String()),
			sdk.NewAttribute("cache_hit", fmt.Sprintf("%t", receipt.CacheHit)),
			sdk.NewAttribute(types.AttributeKeyStatus, status.String()),
		),
	)

	// Lock finalization (refund of over-locked funds) is handled by the
	// caller — SettleLock — after ProcessSettlement returns, so RefundAmount
	// is left nil here and populated by SettleLock.

	return &SettlementResult{
		BurnAmount:          burnAmountCoins,
		PublisherAmount:     publisherCoins,
		RouterAmount:        routerPayoutAmount,
		OriginSurfaceAmount: originSurfaceAmount,
		TreasuryAmount:      treasuryAmount,
		ReferrerAmount:      referrerAmount,
	}, nil
}

// SwapLUMEtoLAC swaps LUME tokens for LAC credits at the current rate
// Implements the token swap mechanism from the whitepaper
func (k Keeper) SwapLUMEtoLAC(ctx context.Context, sender sdk.AccAddress, lumeAmount sdk.Coin) (sdk.Coin, sdk.Coin, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(ctx)

	if lumeAmount.Denom != types.DefaultLumeDenom {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("expected %s token, got %s", types.DefaultLumeDenom, lumeAmount.Denom)
	}

	// LUME:LAC exchange rate is pinned at 1:1 by design. The two
	// tokens are the same economic unit at different scopes (LUME
	// for token-holder accounting, LAC for intra-module credit
	// accounting) — NOT separate assets whose rate should float.
	// If a future governance decision introduces rate dynamics
	// (e.g., a time-varying peg, multi-asset backing), replace the
	// direct .Amount copy below with a rate lookup from params and
	// thread it through CalculateBurnAmount so the burn is computed
	// on the post-rate LAC amount, not the pre-rate LUME amount.
	_ = sdk.NewCoin(params.CreditDenom, lumeAmount.Amount) // lacAmount placeholder for a future non-1:1 rate

	// Apply acquisition burn (β_acq) if configured
	acqBurnRate := params.BurnRateAcqBps

	// Calculate burn amount on acquisition
	burnVal, netLacVal, err := CalculateBurnAmount(lumeAmount.Amount, acqBurnRate)
	if err != nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to calculate burn amount: %w", err)
	}

	burnAmount := sdk.NewCoin(lumeAmount.Denom, burnVal)
	lacToMint := sdk.NewCoin(params.CreditDenom, netLacVal)

	// Transfer LUME from user to module
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleAccountName, sdk.NewCoins(lumeAmount)); err != nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to receive LUME: %w", err)
	}

	// Mint LAC for the user (net amount after acquisition burn)
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleAccountName, sdk.NewCoins(lacToMint)); err != nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to mint LAC: %w", err)
	}

	// Send LAC to user
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, sender, sdk.NewCoins(lacToMint)); err != nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to send LAC: %w", err)
	}

	// Burn the acquisition burn of LUME to remove it from total supply
	if burnVal.IsPositive() {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleAccountName, sdk.NewCoins(sdk.NewCoin(lumeAmount.Denom, burnVal))); err != nil {
			return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to burn LUME: %w", err)
		}
	}

	// Emit swap event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"lume_lac_swap",
			sdk.NewAttribute("sender", sender.String()),
			sdk.NewAttribute("lume_amount", lumeAmount.String()),
			sdk.NewAttribute("lac_amount", lacToMint.String()),
			sdk.NewAttribute("acq_burn_rate", fmt.Sprintf("%d", acqBurnRate)),
		),
	)

	return lacToMint, burnAmount, nil
}

// SwapLACtoLUME swaps LAC credits for LUME tokens
func (k Keeper) SwapLACtoLUME(ctx context.Context, sender sdk.AccAddress, lacAmount sdk.Coin) (sdk.Coin, sdk.Coin, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(ctx)

	if lacAmount.Denom != params.CreditDenom {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("expected %s token, got %s", params.CreditDenom, lacAmount.Denom)
	}

	// Check LAC balance
	lacBalance := k.bankKeeper.GetBalance(ctx, sender, params.CreditDenom)
	if lacBalance.IsLT(lacAmount) {
		return sdk.Coin{}, sdk.Coin{}, types.ErrInsufficientFunds.Wrapf("LAC balance %s < %s", lacBalance, lacAmount)
	}

	// LAC:LUME exchange rate is pinned at 1:1 by design — see the
	// matching comment in SwapLUMEtoLAC above for the rationale and
	// the exact steps to add rate dynamics if a governance decision
	// ever introduces them.
	_ = sdk.NewCoin(types.DefaultLumeDenom, lacAmount.Amount) // lumeAmount placeholder for a future non-1:1 rate

	// Apply redemption burn (1.5% for redemption)
	burnRateBPS := uint32(RedemptionBurnRateBPS)

	// Calculate burn using safe math helper
	burnVal, netLumeVal, err := CalculateBurnAmount(lacAmount.Amount, burnRateBPS)
	if err != nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to calculate burn amount: %w", err)
	}

	burnAmount := sdk.NewCoin(params.CreditDenom, burnVal)
	netLumeAmount := sdk.NewCoin(types.DefaultLumeDenom, netLumeVal)

	// Pre-flight: verify module has enough LUME reserve before burning LAC.
	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleAccountName)
	if moduleAddr == nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("module account %s not found", types.ModuleAccountName)
	}
	lumeReserve := k.bankKeeper.GetBalance(ctx, moduleAddr, types.DefaultLumeDenom)
	requiredLumeReserve := sdk.NewCoin(types.DefaultLumeDenom, lacAmount.Amount)
	if lumeReserve.IsLT(requiredLumeReserve) {
		return sdk.Coin{}, sdk.Coin{}, types.ErrInsufficientFunds.Wrapf(
			"LUME reserve %s insufficient for redemption (need %s to cover payout and burn)", lumeReserve, requiredLumeReserve)
	}

	// Burn LAC from user account
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sender, types.ModuleAccountName, sdk.NewCoins(lacAmount)); err != nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to receive LAC: %w", err)
	}

	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleAccountName, sdk.NewCoins(lacAmount)); err != nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to burn LAC: %w", err)
	}

	// Send LUME from module to user (using the balance accumulated from SwapLUMEtoLAC)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, sender, sdk.NewCoins(netLumeAmount)); err != nil {
		return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to send LUME: %w", err)
	}

	// Burn the redemption burn of LUME to remove it from total supply
	if burnVal.IsPositive() {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleAccountName, sdk.NewCoins(sdk.NewCoin(types.DefaultLumeDenom, burnVal))); err != nil {
			return sdk.Coin{}, sdk.Coin{}, fmt.Errorf("failed to burn LUME: %w", err)
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSwap,
			sdk.NewAttribute("sender", sender.String()),
			sdk.NewAttribute("lac_in", lacAmount.String()),
			sdk.NewAttribute("lume_out", netLumeAmount.String()),
			sdk.NewAttribute("direction", "lac_to_lume"),
		),
	)

	return netLumeAmount, burnAmount, nil
}

// LockCredits locks LAC credits for a tool invocation.
// Returns a lock ID that must be used to unlock or settle.
func (k Keeper) LockCredits(ctx context.Context, routerAddr string, sessionID string, amount sdk.Coin, toolID string, quoteID string, policyVersion string, intentHash string, toolpackID ...string) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()

	lockID, err := k.lockCreditsWithTTL(cacheCtx, routerAddr, sessionID, amount, toolID, quoteID, policyVersion, intentHash, k.legacyLockTTL(ctx), toolpackID...)
	if err != nil {
		return "", err
	}

	write()

	return lockID, nil
}

// LockCreditsWithTTL locks LAC credits using the module TTL sanitizer for callers
// that carry an explicit quote TTL, such as MsgLockCredits.
func (k Keeper) LockCreditsWithTTL(ctx context.Context, routerAddr string, sessionID string, amount sdk.Coin, toolID string, quoteID string, policyVersion string, intentHash string, requestedTTL time.Duration, toolpackID ...string) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()

	ttl := k.SanitizeLockTTL(cacheCtx, requestedTTL)
	if ttl <= 0 {
		return "", fmt.Errorf("unable to determine lock ttl")
	}

	lockID, err := k.lockCreditsWithTTL(cacheCtx, routerAddr, sessionID, amount, toolID, quoteID, policyVersion, intentHash, ttl, toolpackID...)
	if err != nil {
		return "", err
	}

	write()

	return lockID, nil
}

func (k Keeper) legacyLockTTL(ctx context.Context) time.Duration {
	params := k.GetParams(ctx)
	ttl := time.Duration(params.MaxLockTtlSeconds) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}
	return ttl
}

func lockMatchesLockRequest(lock *types.Lock, routerAddr string, sessionID string, amount sdk.Coin, toolID string, quoteID string, policyVersion string, intentHash string, toolpackID string) bool {
	if lock == nil {
		return false
	}
	if !constantTimeStringEqual(lock.Router, routerAddr) ||
		!constantTimeStringEqual(lock.SessionId, sessionID) ||
		!constantTimeStringEqual(lock.ToolId, toolID) ||
		!constantTimeStringEqual(lock.QuoteId, quoteID) ||
		!constantTimeStringEqual(lock.PolicyVersion, policyVersion) ||
		!constantTimeStringEqual(lock.IntentHash, intentHash) ||
		!constantTimeStringEqual(lock.ToolpackId, toolpackID) {
		return false
	}
	return types.CoinFromProto(lock.Amount).IsEqual(amount)
}

func (k Keeper) lockCreditsWithTTL(ctx context.Context, routerAddr string, sessionID string, amount sdk.Coin, toolID string, quoteID string, policyVersion string, intentHash string, ttl time.Duration, toolpackID ...string) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if ttl <= 0 {
		return "", fmt.Errorf("unable to determine lock ttl")
	}

	var packID string
	if len(toolpackID) > 0 {
		packID = toolpackID[0]
	}
	var err error
	if packID, err = validateOptionalCanonicalMsgID("toolpack_id", packID); err != nil {
		return "", err
	}

	// Validate router address
	if routerAddr == "" {
		return "", fmt.Errorf("router address required")
	}
	if _, err := validateCanonicalMsgID("session_id", sessionID); err != nil {
		return "", err
	}
	if _, err := validateCanonicalMsgID("tool_id", toolID); err != nil {
		return "", err
	}

	creditDenom := k.creditDenomForLocks(ctx)
	if !amount.IsValid() {
		return "", fmt.Errorf("invalid amount")
	}
	if !amount.IsPositive() {
		return "", fmt.Errorf("amount must be positive")
	}
	if amount.Denom != creditDenom {
		return "", fmt.Errorf("expected denom %s got %s", creditDenom, amount.Denom)
	}

	// Check router has sufficient balance (assuming router manages user balances)
	routerAcct, err := sdk.AccAddressFromBech32(routerAddr)
	if err != nil {
		return "", fmt.Errorf("invalid router address: %w", err)
	}

	balance := k.bankKeeper.GetBalance(ctx, routerAcct, amount.Denom)
	if balance.IsLT(amount) {
		return "", types.ErrInsufficientFunds.Wrapf("have %s, need %s", balance, amount)
	}

	// Check idempotency via quote ID
	if quoteID != "" {
		if existingID, err := k.state.LocksByQuote.Get(ctx, quoteID); err == nil {
			if lock, found := k.GetLock(ctx, existingID); found {
				if lock.Status == types.LockStatus_LOCK_STATUS_ACTIVE {
					if !lockMatchesLockRequest(lock, routerAddr, sessionID, amount, toolID, quoteID, policyVersion, intentHash, packID) {
						return "", types.ErrInvalidParams.Wrapf("quote %s already bound to a different active lock request", quoteID)
					}
					return existingID, nil
				}
				return "", types.ErrLockInactive.Wrapf("quote %s already used (lock %s status %s)", quoteID, existingID, lock.Status)
			}
			// Index exists but lock missing? Cleanup stale index before proceeding.
			if rmErr := k.state.LocksByQuote.Remove(ctx, quoteID); rmErr != nil {
				return "", fmt.Errorf("failed to clean stale quote index %s: %w", quoteID, rmErr)
			}
		}
	}

	// Generate lock ID
	lockID, err := k.NextLockID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to generate lock ID: %w", err)
	}

	expiresAt := sdkCtx.BlockTime().Add(ttl)

	// Create lock record using the protobuf struct
	lock := &types.Lock{
		LockId:        lockID,
		Router:        routerAddr,
		SessionId:     sessionID,
		ToolId:        toolID,
		QuoteId:       quoteID,
		PolicyVersion: policyVersion,
		IntentHash:    intentHash,
		Amount:        amount,
		CreatedAt:     sdkCtx.BlockTime(),
		ExpiresAt:     expiresAt,
		Status:        types.LockStatus_LOCK_STATUS_ACTIVE,
		ToolpackId:    packID,
	}

	// Transfer coins from router to module (escrow)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, routerAcct, types.ModuleAccountName, sdk.NewCoins(amount)); err != nil {
		return "", fmt.Errorf("failed to lock credits: %w", err)
	}

	// Save lock
	if err := k.SaveLock(ctx, lock); err != nil {
		// Rollback transfer
		if rollbackErr := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, routerAcct, sdk.NewCoins(amount)); rollbackErr != nil {
			k.Logger(sdkCtx).Error("failed to rollback locked funds", "lock_id", lockID, "error", rollbackErr)
		}
		return "", fmt.Errorf("failed to save lock: %w", err)
	}

	// Index by quote ID
	if quoteID != "" {
		if err := k.state.LocksByQuote.Set(ctx, quoteID, lockID); err != nil {
			return "", fmt.Errorf("failed to index lock by quote: %w", err)
		}
	}

	// Index expiration for O(1) expiry processing
	if err := k.state.LockExpiry.Set(ctx, collections.Join(expiresAt, lockID)); err != nil {
		return "", fmt.Errorf("failed to index lock expiry: %w", err)
	}

	// Emit event for observability tooling
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLock,
			sdk.NewAttribute(types.AttributeKeyLockID, lockID),
			sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
			sdk.NewAttribute(types.AttributeKeyStatus, lock.Status.String()),
			sdk.NewAttribute(types.AttributeKeyToolID, toolID),
			sdk.NewAttribute(types.AttributeKeyRouter, routerAddr),
			sdk.NewAttribute(types.AttributeKeySessionID, sessionID),
			sdk.NewAttribute("quote_id", quoteID),
			sdk.NewAttribute("policy_version", policyVersion),
			sdk.NewAttribute("intent_hash", intentHash),
		),
	)

	return lockID, nil
}

// UnlockCredits releases locked credits back to the router
// Used when an invocation fails or is cancelled
func (k Keeper) UnlockCredits(ctx context.Context, lockID string, reason string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()

	if err := k.unlockCredits(cacheCtx, lockID, reason); err != nil {
		return err
	}

	write()

	return nil
}

func (k Keeper) unlockCredits(ctx context.Context, lockID string, reason string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get lock
	lock, found := k.GetLock(ctx, lockID)
	if !found {
		return types.ErrLockNotFound.Wrapf("%s", lockID)
	}

	// Check if already processed
	if lock.Status != types.LockStatus_LOCK_STATUS_ACTIVE {
		return types.ErrLockInactive.Wrapf("status=%v", lock.Status)
	}

	// Prevent unlocking if there is a pending settlement
	if existingReceiptID, err := k.state.LockReceipts.Get(ctx, lockID); err == nil {
		if settlement, found := k.GetSettlement(ctx, existingReceiptID); found {
			if settlement.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
				return fmt.Errorf("cannot unlock credits: lock is bound to pending settlement %s", existingReceiptID)
			}
		}
	}

	// Parse router address
	routerAddr, err := sdk.AccAddressFromBech32(lock.Router)
	if err != nil {
		return fmt.Errorf("invalid router address: %w", err)
	}

	// Return coins to router
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, routerAddr, sdk.NewCoins(types.CoinFromProto(lock.Amount))); err != nil {
		return fmt.Errorf("failed to unlock credits: %w", err)
	}

	redactedReason := redactUnlockReason(reason)

	// Update lock status
	lock.Status = types.LockStatus_LOCK_STATUS_RELEASED
	lock.LastError = redactedReason
	if err := k.SaveLock(ctx, lock); err != nil {
		return fmt.Errorf("failed to update lock status: %w", err)
	}

	// Remove from expiration index
	if !lock.ExpiresAt.IsZero() {
		if err := k.state.LockExpiry.Remove(ctx, collections.Join(lock.ExpiresAt, lockID)); err != nil {
			return fmt.Errorf("failed to remove lock from index: %w", err)
		}
	}

	// Index finalized lock for pruning
	if err := k.state.FinalizedLocks.Set(ctx, collections.Join(sdkCtx.BlockTime(), lockID)); err != nil {
		return fmt.Errorf("failed to index finalized lock: %w", err)
	}

	// Emit unlock event for downstream analytics
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUnlock,
			sdk.NewAttribute(types.AttributeKeyLockID, lockID),
			sdk.NewAttribute(types.AttributeKeyAmount, types.CoinFromProto(lock.Amount).String()),
			sdk.NewAttribute(types.AttributeKeyStatus, lock.Status.String()),
			sdk.NewAttribute(types.AttributeKeyRouter, lock.Router),
			sdk.NewAttribute("reason", redactedReason),
		),
	)

	return nil
}

func redactUnlockReason(reason string) string {
	if !unlockReasonMayContainSensitiveMaterial(reason) {
		return reason
	}
	return logging.RedactPII(reason)
}

func unlockReasonMayContainSensitiveMaterial(reason string) bool {
	return strings.Contains(reason, "Bearer ") ||
		strings.Contains(reason, "bearer ") ||
		strings.Contains(reason, "api_key") ||
		strings.Contains(reason, "api-key") ||
		strings.Contains(reason, "apikey") ||
		strings.Contains(reason, "access_token") ||
		strings.Contains(reason, "access-token") ||
		strings.Contains(reason, "client_secret") ||
		strings.Contains(reason, "client-secret") ||
		strings.Contains(reason, "authorization") ||
		strings.Contains(reason, "Authorization") ||
		strings.Contains(reason, "password") ||
		strings.Contains(reason, "secret") ||
		strings.Contains(reason, "token=") ||
		strings.Contains(reason, "sk-")
}

// SettleLock processes a lock for settlement after successful tool invocation
// This deducts the actual cost and returns any excess
func (k Keeper) SettleLock(ctx context.Context, lockID string, actualCost sdk.Coin, receipt SettlementRequest) (*SettlementResult, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()

	result, err := k.settleLock(cacheCtx, lockID, actualCost, receipt)
	if err != nil {
		return nil, err
	}

	write()

	return result, nil
}

func (k Keeper) settleLock(ctx context.Context, lockID string, actualCost sdk.Coin, receipt SettlementRequest) (*SettlementResult, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get lock
	lock, found := k.GetLock(ctx, lockID)
	if !found {
		return nil, types.ErrLockNotFound.Wrapf("%s", lockID)
	}

	// Check if already processed
	if lock.Status != types.LockStatus_LOCK_STATUS_ACTIVE {
		return nil, types.ErrLockInactive.Wrapf("status=%v", lock.Status)
	}

	receiptID := strings.TrimSpace(receipt.ReceiptID)
	if receiptID == "" {
		return nil, fmt.Errorf("receipt id is required")
	}
	receipt.ReceiptID = receiptID

	// Enforce 1 Lock = 1 Receipt ID (prevent double spend via multiple receipt IDs against same lock)
	existingReceiptID, err := k.state.LockReceipts.Get(ctx, lockID)
	switch {
	case err == nil:
		if existingReceiptID != receiptID {
			return nil, fmt.Errorf("lock %s is already bound to receipt %s; cannot use with %s", lockID, existingReceiptID, receiptID)
		}
	case errors.Is(err, collections.ErrNotFound):
		if err := k.state.LockReceipts.Set(ctx, lockID, receiptID); err != nil {
			return nil, fmt.Errorf("failed to bind lock to receipt: %w", err)
		}
	default:
		return nil, fmt.Errorf("failed to check lock receipt binding: %w", err)
	}

	// Parse router address from the lock and ensure the receipt cannot redirect router share.
	routerAddr, err := sdk.AccAddressFromBech32(lock.Router)
	if err != nil {
		return nil, fmt.Errorf("invalid router address: %w", err)
	}
	if receipt.RouterAddr == nil {
		receipt.RouterAddr = routerAddr
	} else if !receipt.RouterAddr.Equals(routerAddr) {
		return nil, fmt.Errorf("router address mismatch: lock=%s receipt=%s", lock.Router, receipt.RouterAddr.String())
	}
	if strings.TrimSpace(receipt.RouterID) == "" {
		receipt.RouterID = lock.Router
	} else if receipt.RouterID != lock.Router {
		return nil, fmt.Errorf("router id mismatch: lock=%s receipt=%s", lock.Router, receipt.RouterID)
	}
	if strings.TrimSpace(receipt.UserID) == "" {
		receipt.UserID = lock.Router
	}

	lockedAmount := types.CoinFromProto(lock.Amount)
	// Verify actual cost doesn't exceed locked amount
	if actualCost.Amount.GT(lockedAmount.Amount) {
		return nil, fmt.Errorf("actual cost exceeds locked amount: %s > %s", actualCost, lockedAmount)
	}

	chargeAmount := actualCost
	discountCoin := sdk.NewCoin(actualCost.Denom, sdkmath.ZeroInt())
	commitmentID := ""
	policyID := derivePolicyID(receipt.PolicySnapshot)
	if policyID == "" {
		policyID = derivePolicyID(lock.PolicyVersion)
	}
	lockToolID := strings.TrimSpace(lock.ToolId)
	toolID := strings.TrimSpace(receipt.ToolID)
	if toolID == "" {
		toolID = lockToolID
	} else if lockToolID != "" && toolID != lockToolID {
		return nil, fmt.Errorf("tool id mismatch: lock=%s receipt=%s", lockToolID, toolID)
	}
	if toolID == "" {
		return nil, fmt.Errorf("tool id is required")
	}
	receipt.ToolID = toolID

	if strings.TrimSpace(receipt.ToolpackID) == "" && strings.TrimSpace(lock.ToolpackId) != "" {
		receipt.ToolpackID = lock.ToolpackId
	}
	receipt.Stage = strings.TrimSpace(receipt.Stage)

	isFinal := receipt.Stage == "" || strings.EqualFold(receipt.Stage, "finalized")

	// For partial fills, we must check cumulative cost against the lock.
	// actualCost is the delta for this specific fill.
	previousUsed := sdk.NewCoins()
	if existing, found := k.GetSettlement(ctx, receiptID); found {
		previousUsed = previousUsed.Add(existing.TotalCost...)
	}

	totalUsed := previousUsed.Add(actualCost)
	// lockedAmount already defined above from lock.Amount

	// Zero-cost settlements (actualCost == 0 && previousUsed == 0)
	// are INTENTIONALLY allowed — free tools and zero-fee invocations
	// still need a SettlementRecord so the verification, metrics,
	// and audit-trail pipelines observe them. An earlier draft of
	// this path rejected zero-cost settlements in favour of routing
	// them to UnlockCredits (cache-hit path), but that forked the
	// bookkeeping and made free-tool usage invisible to downstream
	// consumers; the current policy keeps them on the settle path.

	// Verify total cost doesn't exceed locked amount
	if totalUsed.AmountOf(actualCost.Denom).GT(lockedAmount.Amount) {
		return nil, fmt.Errorf("total actual cost exceeds locked amount: %s > %s", totalUsed, lockedAmount)
	}

	if k.registryKeeper != nil {
		expectedPublisher, err := k.registryKeeper.GetToolPublisher(sdkCtx, toolID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve publisher for tool %s: %w", toolID, err)
		}
		if expectedPublisher == nil {
			return nil, fmt.Errorf("publisher address missing for tool %s", toolID)
		}
		if receipt.PublisherAddr == nil {
			receipt.PublisherAddr = expectedPublisher
		} else if !receipt.PublisherAddr.Equals(expectedPublisher) {
			return nil, fmt.Errorf("publisher address mismatch: tool=%s expected=%s got=%s", toolID, expectedPublisher.String(), receipt.PublisherAddr.String())
		}
	}
	if receipt.PublisherAddr != nil && strings.TrimSpace(receipt.PublisherID) == "" {
		receipt.PublisherID = receipt.PublisherAddr.String()
	}
	if receipt.ReferrerAddr != nil && strings.TrimSpace(receipt.ReferrerID) == "" {
		receipt.ReferrerID = receipt.ReferrerAddr.String()
	}

	if k.reserveKeeper != nil && policyID != "" && toolID != "" {
		owner := strings.TrimSpace(receipt.UserID)
		if owner == "" {
			owner = strings.TrimSpace(lock.Router)
		}
		allocation, err := k.reserveKeeper.AllocateReserve(ctx, owner, policyID, toolID, actualCost)
		if err != nil {
			k.Logger(sdkCtx).Error("reserve allocation failed", "policy_id", policyID, "tool_id", toolID, "error", err)
		} else if allocation.Applied {
			commitmentID = allocation.CommitmentID
			if allocation.DiscountedPrice.Denom != actualCost.Denom {
				allocation.DiscountedPrice = sdk.NewCoin(actualCost.Denom, allocation.DiscountedPrice.Amount)
			}
			if allocation.DiscountedPrice.Amount.LT(actualCost.Amount) {
				discountAmt := actualCost.Amount.Sub(allocation.DiscountedPrice.Amount)
				discountCoin = sdk.NewCoin(actualCost.Denom, discountAmt)
				chargeAmount = sdk.NewCoin(actualCost.Denom, allocation.DiscountedPrice.Amount)
			} else {
				chargeAmount = actualCost
			}
		}
	}

	// Process the settlement with the discounted charge amount (if any)
	// ProcessSettlement expects 'TotalAmount' to be the DELTA for this request (see its implementation).
	receipt.TotalAmount = sdk.NewCoins(chargeAmount)
	receipt.LockID = lockID
	receipt.QuoteID = lock.QuoteId
	receipt.SessionID = lock.SessionId
	receipt.IntentHash = lock.IntentHash

	result, err := k.ProcessSettlement(ctx, receipt)
	if err != nil {
		return nil, types.ErrSettlementFailed.Wrapf("failed to process: %s", err)
	}

	// Finalize lock if settlement is complete
	if isFinal {
		// Calculate remaining balance to refund
		// lockedAmount is the initial lock amount
		// The refund is based on the actual charged amounts (post-discount), not the
		// raw service cost. previousUsed already reflects charged amounts from prior
		// ProcessSettlement calls; chargeAmount is the current (possibly discounted) charge.
		totalCharged := previousUsed.AmountOf(lockedAmount.Denom).Add(chargeAmount.Amount)
		remaining := lockedAmount.Amount.Sub(totalCharged)

		if remaining.IsPositive() {
			refundCoin := sdk.NewCoin(lockedAmount.Denom, remaining)
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, routerAddr, sdk.NewCoins(refundCoin)); err != nil {
				return nil, fmt.Errorf("failed to refund remaining credits: %w", err)
			}
			// Update result for caller visibility
			result.RefundAmount = sdk.NewCoins(refundCoin)
		}

		// Update Lock Status to prevent double-spending/double-refunds
		lock.Status = types.LockStatus_LOCK_STATUS_BURNED

		if err := k.SaveLock(ctx, lock); err != nil {
			return nil, fmt.Errorf("failed to update lock status: %w", err)
		}

		// Remove from Expiry Index
		if !lock.ExpiresAt.IsZero() {
			if err := k.state.LockExpiry.Remove(ctx, collections.Join(lock.ExpiresAt, lockID)); err != nil {
				return nil, fmt.Errorf("failed to remove lock from expiry index: %w", err)
			}
		}

		// Add to Finalized Index for pruning
		if err := k.state.FinalizedLocks.Set(ctx, collections.Join(sdkCtx.BlockTime(), lockID)); err != nil {
			return nil, fmt.Errorf("failed to index finalized lock: %w", err)
		}

		// Emit explicit unlock event for the refund/closure
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeUnlock,
				sdk.NewAttribute(types.AttributeKeyLockID, lockID),
				sdk.NewAttribute(types.AttributeKeyAmount, result.RefundAmount.String()),
				sdk.NewAttribute(types.AttributeKeyStatus, lock.Status.String()),
				sdk.NewAttribute(types.AttributeKeyRouter, lock.Router),
				sdk.NewAttribute("reason", "settled"),
			),
		)
	} else {
		// Partial fill: extend lock expiration to outlive the dispute window
		disputeWindow := k.SettlementDisputeWindow(ctx)

		// Extend to dispute window + 1 hour buffer
		newExpiresAt := sdkCtx.BlockTime().Add(disputeWindow).Add(time.Hour)

		if !lock.ExpiresAt.IsZero() && newExpiresAt.After(lock.ExpiresAt) {
			if err := k.state.LockExpiry.Remove(ctx, collections.Join(lock.ExpiresAt, lockID)); err != nil {
				return nil, fmt.Errorf("failed to remove old lock expiry index: %w", err)
			}
			lock.ExpiresAt = newExpiresAt
			if err := k.SaveLock(ctx, lock); err != nil {
				return nil, fmt.Errorf("failed to update lock expires at: %w", err)
			}
			if err := k.state.LockExpiry.Set(ctx, collections.Join(newExpiresAt, lockID)); err != nil {
				return nil, fmt.Errorf("failed to set new lock expiry index: %w", err)
			}
		}
	}

	if discountCoin.Amount.IsPositive() {
		labels := []metrics.Label{telemetry.NewLabel("policy_id", sanitizeLabel(policyID))}
		if commitmentID != "" {
			labels = append(labels, telemetry.NewLabel("commitment_id", sanitizeLabel(commitmentID)))
		}
		telemetry.IncrCounterWithLabels([]string{types.ModuleName, "vault", "allocations_total"}, float32(1), labels)
		discountDec := sdkmath.LegacyNewDecFromInt(discountCoin.Amount).QuoInt64(lacPrecision)
		telemetry.IncrCounterWithLabels([]string{types.ModuleName, "vault", "discount_lac"}, float32(discountDec.MustFloat64()), labels)
		k.Logger(sdkCtx).Info("applied reserve discount", "policy_id", policyID, "tool_id", toolID, "discount", discountCoin.String(), "commitment_id", commitmentID)
	}

	return result, nil
}

// GetUserBalance returns the LAC balance for a user
func (k Keeper) GetUserBalance(ctx context.Context, userAddr sdk.AccAddress) sdk.Coin {
	params := k.GetParams(ctx)
	return k.bankKeeper.GetBalance(ctx, userAddr, params.CreditDenom)
}

// GetLockedAmount returns the total amount of LAC locked by a router
func (k Keeper) GetLockedAmount(ctx context.Context, routerAddr string) sdk.Coins {
	total := sdk.NewCoins()

	// Iterate through all locks for this router
	err := k.IterateLocks(ctx, func(lock *types.Lock) bool {
		if lock == nil {
			return false
		}
		if lock.Router == routerAddr && lock.Status == types.LockStatus_LOCK_STATUS_ACTIVE {
			total = total.Add(types.CoinFromProto(lock.Amount))
		}
		return false // Continue iteration
	})

	if err != nil {
		k.Logger(sdk.UnwrapSDKContext(ctx)).Error("failed to iterate locks", "error", err)
	}

	return total
}

// ExpireLocks expires any locks that have passed their TTL
// This should be called periodically (e.g., in BeginBlock)
func (k Keeper) ExpireLocks(ctx context.Context, limit int) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentTime := sdkCtx.BlockTime()

	if limit <= 0 {
		limit = int(types.DefaultMaxExpiredLocksPerBlock)
	}

	type expiredLock struct {
		timestamp time.Time
		lockID    string
	}
	var expiredLocks []expiredLock

	// Iterate efficient secondary index.
	// Range: [Min, currentTime) at the timestamp level. The empty
	// lock-id suffix is the lowest key for currentTime, so this excludes
	// all locks that expire exactly at the current block time while
	// including every earlier expiry timestamp.
	rng := new(collections.Range[collections.Pair[time.Time, string]]).
		EndExclusive(collections.Join(currentTime, ""))

	err := k.state.LockExpiry.Walk(ctx, rng, func(key collections.Pair[time.Time, string]) (bool, error) {
		sdkCtx.GasMeter().ConsumeGas(GasPerExpiredLockProcess, "credits/expire-lock")
		expiredLocks = append(expiredLocks, expiredLock{
			timestamp: key.K1(),
			lockID:    key.K2(),
		})
		return len(expiredLocks) >= limit, nil
	})

	if err != nil {
		return fmt.Errorf("failed to find expired locks: %w", err)
	}

	// Unlock expired locks
	// No need to sort as index iteration is already sorted by time
	for _, expired := range expiredLocks {
		lockID := expired.lockID

		// Prevent unlocking if there is a pending settlement
		isPending := false
		if existingReceiptID, err := k.state.LockReceipts.Get(ctx, lockID); err == nil {
			if settlement, found := k.GetSettlement(ctx, existingReceiptID); found {
				if settlement.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
					isPending = true
				}
			}
		}

		if isPending {
			// Lock is waiting for BeginBlocker to finalize its settlement.
			// Bump the expiry by 1 hour to get it out of the current batch.
			//
			// Wrap the three-write sequence (LockExpiry.Remove(old) +
			// SaveLock + LockExpiry.Set(new)) in a per-lock CacheContext so
			// a partial-failure cannot orphan the lock. The prior code did
			// the Remove first with the primary ctx, then logged-and-
			// continued on SaveLock / Set failure — leaving the lock
			// missing from BOTH the old and the new expiry index slots,
			// permanently invisible to every subsequent ExpireLocks sweep
			// (expiry index is the ONLY sweep input). BeginBlocker cannot
			// return error without halting the chain, so rolling back to
			// the old state via CacheContext.discard is the only path that
			// preserves the sweep invariant while keeping the BeginBlocker
			// non-halting.
			cacheCtx, commit := sdkCtx.CacheContext()
			lock, found := k.GetLock(cacheCtx, lockID)
			if !found || lock == nil {
				continue
			}
			if lock.ExpiresAt.IsZero() {
				// No expiry index entry to remove; skip the bump — the
				// lock will be picked up once it's indexed again by a
				// later write path.
				continue
			}
			oldKey := collections.Join(lock.ExpiresAt, lockID)
			if err := k.state.LockExpiry.Remove(cacheCtx, oldKey); err != nil {
				k.Logger(sdkCtx).Error("failed to remove old lock expiry index entry; skipping bump",
					"lock_id", lockID, "error", err)
				continue
			}
			newExpiresAt := sdkCtx.BlockTime().Add(time.Hour)
			lock.ExpiresAt = newExpiresAt
			if saveErr := k.SaveLock(cacheCtx, lock); saveErr != nil {
				k.Logger(sdkCtx).Error("failed to save bumped lock expiry",
					"lock_id", lockID, "new_expires_at", newExpiresAt, "error", saveErr)
				continue
			}
			if setErr := k.state.LockExpiry.Set(cacheCtx, collections.Join(newExpiresAt, lockID)); setErr != nil {
				k.Logger(sdkCtx).Error("failed to reinsert lock expiry index",
					"lock_id", lockID, "new_expires_at", newExpiresAt, "error", setErr)
				continue
			}
			commit()
			continue
		}

		if err := k.UnlockCredits(ctx, lockID, "expired"); err != nil {
			k.Logger(sdkCtx).Error("failed to expire lock", "lock_id", lockID, "error", err)

			if errors.Is(err, types.ErrLockNotFound) || errors.Is(err, types.ErrLockInactive) || strings.Contains(err.Error(), "invalid router address") {
				if rmErr := k.state.LockExpiry.Remove(ctx, collections.Join(expired.timestamp, lockID)); rmErr != nil && !errors.Is(rmErr, collections.ErrNotFound) {
					k.Logger(sdkCtx).Error("failed to remove stale lock expiry index", "lock_id", lockID, "error", rmErr)
				}
			}
		}
	}

	return nil
}

// ProcessExpiredLocks is an alias for ExpireLocks to match abci.go interface
func (k Keeper) ProcessExpiredLocks(ctx context.Context, limit int) error {
	if limit <= 0 {
		params := k.GetParams(ctx)
		limit = int(params.MaxExpiredLocksPerBlock)
		if limit <= 0 {
			limit = int(types.DefaultMaxExpiredLocksPerBlock)
		}
	}

	return k.ExpireLocks(ctx, limit)
}

// GetCodec returns the codec
func (k Keeper) GetCodec() codec.BinaryCodec {
	return k.cdc
}

// GetStoreKey returns the store key (wrapped in service)
func (k Keeper) GetStoreKey() corestore.KVStoreService {
	return k.storeService
}

// UpdateSettlementMetrics updates metrics for settlement processing
func (k Keeper) UpdateSettlementMetrics(ctx context.Context, processed, failed int) error {
	if processed == 0 && failed == 0 {
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get current metrics
	metrics, err := k.state.Metrics.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("load settlement metrics: %w", err)
		}
	}
	if metrics == nil {
		metrics = &types.SettlementMetrics{}
	}

	// Update metrics
	//#nosec G115 -- processed/failed counters starting from 0, always non-negative
	metrics.TotalProcessed += uint64(processed)
	metrics.TotalErrors += uint64(failed)
	metrics.LastProcessedAt = sdkCtx.BlockTime().Format(time.RFC3339)

	// Save updated metrics
	return k.state.Metrics.Set(ctx, metrics)
}

// PruneOldSettlements removes completed settlements older than the given time
func (k Keeper) PruneOldSettlements(ctx context.Context, olderThan time.Time, limit int) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if limit <= 0 {
		limit = int(types.DefaultMaxPrunedSettlementsPerBlock)
	}

	var pruneCount int
	var scanned int
	// Safety limit to prevent O(N) scan on large datasets from exceeding block gas limit
	const scanLimit = 10_000
	keysToDelete := []string{}
	// Map to track timestamp to delete from index correctly
	timestamps := make(map[string]time.Time)

	// Use SettlementsByTime index for efficient range query
	rng := new(collections.Range[collections.Pair[time.Time, string]]).
		EndExclusive(collections.Join(olderThan.Add(time.Nanosecond), ""))

	err := k.state.SettlementsByTime.Walk(ctx, rng, func(key collections.Pair[time.Time, string]) (bool, error) {
		scanned++
		if scanned >= scanLimit {
			return true, nil // Stop scanning
		}

		settlementID := key.K2()
		sdkCtx.GasMeter().ConsumeGas(GasPerPrunedSettlement, "credits/prune-settlement")
		keysToDelete = append(keysToDelete, settlementID)
		timestamps[settlementID] = key.K1()
		pruneCount++

		return len(keysToDelete) >= limit, nil
	})

	if err != nil {
		return fmt.Errorf("failed to iterate old settlements: %w", err)
	}

	for _, id := range keysToDelete {
		// Always remove from ByTime Index using captured timestamp
		ts := timestamps[id]
		if err := k.state.SettlementsByTime.Remove(ctx, collections.Join(ts, id)); err != nil {
			k.Logger(sdkCtx).Error("failed to remove settlement from time index", "id", id, "error", err)
		}

		// Get settlement to remove from Pending Index if needed
		settlement, found := k.GetSettlement(ctx, id)
		if !found {
			// Record not found in main store (dangling index case), but we already cleaned the index.
			continue
		}

		// Remove from Main Store
		if err := k.state.Settlements.Remove(ctx, id); err != nil {
			k.Logger(sdkCtx).Error("failed to delete settlement", "id", id, "error", err)
			continue
		}

		// Remove from Pending Index (unlikely for pruned items, but good hygiene)
		if settlement.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
			if err := k.state.PendingSettlements.Remove(ctx, id); err != nil {
				k.Logger(sdkCtx).Error("failed to remove settlement from pending index", "id", id, "error", err)
			}
		}
	}

	if pruneCount > 0 {
		k.Logger(sdkCtx).Info("pruned old settlements", "count", pruneCount, "scanned", scanned)
	}

	return nil
}

// PruneFinalizedLocks removes released or burned locks older than the given time
func (k Keeper) PruneFinalizedLocks(ctx context.Context, olderThan time.Time, limit int) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if limit <= 0 {
		limit = int(types.DefaultMaxPrunedSettlementsPerBlock) // Reuse settlement limit default
	}

	var pruneCount int
	var scanned int
	const scanLimit = 10_000
	keysToDelete := []string{}
	// Map to track timestamp to delete from index correctly
	timestamps := make(map[string]time.Time)

	rng := new(collections.Range[collections.Pair[time.Time, string]]).
		EndExclusive(collections.Join(olderThan.Add(time.Nanosecond), ""))

	err := k.state.FinalizedLocks.Walk(ctx, rng, func(key collections.Pair[time.Time, string]) (bool, error) {
		scanned++
		if scanned >= scanLimit {
			return true, nil
		}

		lockID := key.K2()
		// Reusing gas cost for settlement pruning as it's similar work
		sdkCtx.GasMeter().ConsumeGas(GasPerPrunedSettlement, "credits/prune-lock")
		keysToDelete = append(keysToDelete, lockID)
		timestamps[lockID] = key.K1()
		pruneCount++

		return len(keysToDelete) >= limit, nil
	})

	if err != nil {
		return fmt.Errorf("failed to iterate finalized locks: %w", err)
	}

	for _, id := range keysToDelete {
		// Always remove from FinalizedLocks Index using captured timestamp first to prevent queue stalls
		ts := timestamps[id]
		if err := k.state.FinalizedLocks.Remove(ctx, collections.Join(ts, id)); err != nil && !errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdkCtx).Error("failed to remove lock from finalized index", "id", id, "error", err)
		}

		// Retrieve lock to get QuoteID for index cleanup
		lock, found := k.GetLock(ctx, id)

		// Remove from Main Store
		if err := k.state.Locks.Remove(ctx, id); err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				k.Logger(sdkCtx).Error("failed to delete lock", "id", id, "error", err)
			}
			continue
		}

		if found && lock.QuoteId != "" {
			if err := k.state.LocksByQuote.Remove(ctx, lock.QuoteId); err != nil && !errors.Is(err, collections.ErrNotFound) {
				k.Logger(sdkCtx).Debug("failed to remove lock quote index", "id", id, "quote_id", lock.QuoteId, "error", err)
			}
		}

		// Remove from LockReceipts map
		if err := k.state.LockReceipts.Remove(ctx, id); err != nil && !errors.Is(err, collections.ErrNotFound) {
			// Non-fatal, just log
			k.Logger(sdkCtx).Debug("failed to remove lock receipt binding", "id", id, "error", err)
		}
	}

	if pruneCount > 0 {
		k.Logger(sdkCtx).Info("pruned finalized locks", "count", pruneCount, "scanned", scanned)
	}

	return nil
}

// GetPendingSettlements returns all settlements with PENDING status
func (k Keeper) GetPendingSettlements(ctx context.Context) ([]*types.SettlementRecord, error) {
	settlements := make([]*types.SettlementRecord, 0)

	// Use PendingSettlements index
	err := k.state.PendingSettlements.Walk(ctx, nil, func(id string) (bool, error) {
		settlement, found := k.GetSettlement(ctx, id)
		if found {
			settlements = append(settlements, settlement)
		}
		return false, nil // Continue iteration
	})

	if err != nil {
		return nil, fmt.Errorf("failed to iterate pending settlements: %w", err)
	}

	return settlements, nil
}

// IteratePendingSettlements walks pending settlements, applying the handler until it
// signals to stop or the provided limit is reached.
func (k Keeper) IteratePendingSettlements(
	sdkCtx sdk.Context,
	limit int,
	handler func(*types.SettlementRecord) (consumed bool, stop bool, err error),
) error {
	if limit <= 0 {
		limit = types.DefaultMaxSettlementsPerBlock
	}
	processed := 0
	scanned := 0
	// Safety limit to prevent O(N) scan on large datasets
	const scanLimit = 10_000

	var orphans []string

	// Use PendingSettlements index
	err := k.state.PendingSettlements.Walk(sdkCtx, nil, func(id string) (bool, error) {
		scanned++
		if scanned >= scanLimit {
			return true, nil // Stop scanning
		}

		settlement, found := k.GetSettlement(sdkCtx, id)
		if !found {
			// Index inconsistency? Record for cleanup and skip.
			orphans = append(orphans, id)
			return false, nil
		}

		sdkCtx.GasMeter().ConsumeGas(GasPerSettlementScan, "credits/pending-settlement-scan")

		consumed, stop, handlerErr := handler(settlement)
		if handlerErr != nil {
			return true, handlerErr
		}
		if consumed {
			processed++
			if processed >= limit {
				return true, nil
			}
		}
		if stop {
			return true, nil
		}
		return false, nil
	})

	for _, id := range orphans {
		if rmErr := k.state.PendingSettlements.Remove(sdkCtx, id); rmErr != nil && !errors.Is(rmErr, collections.ErrNotFound) {
			k.Logger(sdkCtx).Error("failed to remove orphaned pending settlement index", "id", id, "error", rmErr)
		} else {
			k.Logger(sdkCtx).Warn("pruned orphaned pending settlement index", "id", id)
		}
	}

	return err
}

// UpdateSettlement updates a settlement record in the store
func (k Keeper) UpdateSettlement(ctx context.Context, settlement *types.SettlementRecord) error {
	if settlement == nil {
		return fmt.Errorf("settlement cannot be nil")
	}

	// Get old record to handle index updates
	oldSettlement, err := k.state.Settlements.Get(ctx, settlement.Id)
	var oldStatus types.SettlementStatus
	var oldCompletedAt *time.Time
	exists := false

	if err == nil {
		exists = true
		oldStatus = oldSettlement.Status
		oldCompletedAt = oldSettlement.CompletedAt
	} else if !errors.Is(err, collections.ErrNotFound) {
		return err
	}

	// Update main store
	if err := k.state.Settlements.Set(ctx, settlement.Id, settlement); err != nil {
		return err
	}

	// Manage Pending Index
	if settlement.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
		// Add to Pending Index if not already there or status changed
		if !exists || oldStatus != types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
			if err := k.state.PendingSettlements.Set(ctx, settlement.Id); err != nil {
				return err
			}
		}
	} else {
		// Remove from Pending Index if it was there
		if exists && oldStatus == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
			if err := k.state.PendingSettlements.Remove(ctx, settlement.Id); err != nil {
				return err
			}
		}
	}

	isTerminal := func(status types.SettlementStatus) bool {
		return status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED || status == types.SettlementStatus_SETTLEMENT_STATUS_FAILED
	}

	// Manage Completed Index
	if isTerminal(settlement.Status) && settlement.CompletedAt != nil {
		newKey := collections.Join(*settlement.CompletedAt, settlement.Id)
		// If time changed, remove old index entry
		if exists && isTerminal(oldStatus) && oldCompletedAt != nil {
			if !oldCompletedAt.Equal(*settlement.CompletedAt) {
				if err := k.state.SettlementsByTime.Remove(ctx, collections.Join(*oldCompletedAt, settlement.Id)); err != nil {
					return err
				}
			}
		}
		// Add/Update new index entry
		if err := k.state.SettlementsByTime.Set(ctx, newKey); err != nil {
			return err
		}
	} else if exists && isTerminal(oldStatus) && oldCompletedAt != nil {
		// Remove from Completed Index if it was there but now status changed or time is nil
		if err := k.state.SettlementsByTime.Remove(ctx, collections.Join(*oldCompletedAt, settlement.Id)); err != nil {
			return err
		}
	}

	return nil
}

// GetSettlement retrieves a settlement record by ID if present.
func (k Keeper) GetSettlement(ctx context.Context, id string) (*types.SettlementRecord, bool) {
	settlement, err := k.state.Settlements.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(sdk.UnwrapSDKContext(ctx)).Error("failed to load settlement", "id", id, "error", err)
		return nil, false
	}
	if settlement == nil {
		return nil, false
	}
	return settlement, true
}

// CreateSettlement creates a new settlement record
func (k Keeper) CreateSettlement(ctx context.Context, settlement *types.SettlementRecord) error {
	if settlement == nil {
		return fmt.Errorf("settlement cannot be nil")
	}
	// Basic validation
	if settlement.Id == "" {
		return fmt.Errorf("invalid settlement: empty settlement ID")
	}

	// Check if already exists
	if _, err := k.state.Settlements.Get(ctx, settlement.Id); err == nil {
		return fmt.Errorf("settlement already exists: %s", settlement.Id)
	}

	// Save settlement
	if err := k.state.Settlements.Set(ctx, settlement.Id, settlement); err != nil {
		return err
	}

	// Update indexes
	if settlement.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
		if err := k.state.PendingSettlements.Set(ctx, settlement.Id); err != nil {
			return err
		}
	} else if (settlement.Status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED || settlement.Status == types.SettlementStatus_SETTLEMENT_STATUS_FAILED) && settlement.CompletedAt != nil {
		if err := k.state.SettlementsByTime.Set(ctx, collections.Join(*settlement.CompletedAt, settlement.Id)); err != nil {
			return err
		}
	}

	return nil
}

// FinalizeSettlementWithLock adapts a pending settlement record into a final SettleLock call.
// This is used by the ABCI BeginBlocker to auto-finalize pending settlements.
func (k Keeper) FinalizeSettlementWithLock(ctx context.Context, record *types.SettlementRecord) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}
	if record.LockId == "" {
		return fmt.Errorf("lock id missing from settlement record")
	}

	// Reconstruct addresses
	publisherAddr, err := sdk.AccAddressFromBech32(record.PublisherId)
	if err != nil {
		return fmt.Errorf("invalid publisher id: %w", err)
	}
	if k.registryKeeper != nil {
		if latestPublisher, err := k.registryKeeper.GetToolPublisher(ctx, record.ToolId); err == nil && latestPublisher != nil {
			publisherAddr = latestPublisher
			record.PublisherId = latestPublisher.String()
		}
	}
	routerAddr, err := sdk.AccAddressFromBech32(record.RouterId)
	if err != nil {
		return fmt.Errorf("invalid router id: %w", err)
	}
	var referrerAddr sdk.AccAddress
	if record.ReferrerId != "" {
		referrerAddr, err = sdk.AccAddressFromBech32(record.ReferrerId)
		if err != nil {
			return fmt.Errorf("invalid referrer id: %w", err)
		}
	}

	totalCost := types.CoinsFromProto(record.TotalCost)
	denom := types.DefaultCreditDenom
	if !totalCost.Empty() {
		denom = totalCost[0].Denom
	}

	req := SettlementRequest{
		ReceiptID:     record.Id,
		ToolID:        record.ToolId,
		TotalAmount:   sdk.NewCoins(), // Delta is zero
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		CacheHit:      record.CacheHit,
		OriginToolID:  record.OriginToolId,
		OriginID:      record.OriginId,
		PublisherID:   record.PublisherId,
		UserID:        record.UserId,
		RouterID:      record.RouterId,
		ReferrerID:    record.ReferrerId,
		ToolpackID:    record.ToolpackId,
		ActionID:      record.ActionId,
		Stage:         "finalized",
		SessionID:     "", // Will be loaded from lock if missing
		QuoteID:       "", // Will be loaded from lock if missing
		LockID:        record.LockId,
	}

	_, err = k.SettleLock(ctx, record.LockId, sdk.NewCoin(denom, sdkmath.ZeroInt()), req)
	return err
}
