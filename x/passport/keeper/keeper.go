
// Package keeper provides state access for the passport module.
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
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/passport/types"
)

// ConsensusVersion defines the module consensus version for migrations.
const ConsensusVersion = 1

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
	Schema      collections.Schema
	Params      collections.Item[*types.Params]
	Passports   collections.Map[string, *types.AgentPassport]
	PassportSeq collections.Sequence
	AgentIndex  collections.Map[string, string] // agent_pubkey -> passport_id
}

// Keeper provides the module's state access layer.
type Keeper struct {
	cdc           codec.BinaryCodec
	storeService  corestore.KVStoreService
	bankKeeper    types.BankKeeper
	accountKeeper types.AccountKeeper
	authority     string
	state         State
}

// NewKeeper constructs a Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	authority string,
) Keeper {
	if bankKeeper == nil {
		panic("passport keeper requires bank keeper")
	}
	if accountKeeper == nil {
		panic("passport keeper requires account keeper")
	}

	sb := collections.NewSchemaBuilder(storeService)
	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsPrefix),
			"params",
			collPtrValue[types.Params](cdc),
		),
		Passports: collections.NewMap(
			sb,
			collections.NewPrefix(types.PassportsPrefix),
			"passports",
			collections.StringKey,
			collPtrValue[types.AgentPassport](cdc),
		),
		PassportSeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.PassportSeqKeyPrefix),
			"passport_seq",
		),
		AgentIndex: collections.NewMap(
			sb,
			collections.NewPrefix(types.AgentIndexPrefix),
			"agent_index",
			collections.StringKey,
			collections.StringValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build passport schema: %w", err))
	}
	state.Schema = schema

	return Keeper{
		cdc:           cdc,
		storeService:  storeService,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
		authority:     authority,
		state:         state,
	}
}

// Schema returns the underlying collections schema.
func (k Keeper) Schema() collections.Schema { return k.state.Schema }

// Logger returns a module-prefixed logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/passport")
}

// Authority exposes the address allowed to perform governance actions.
func (k Keeper) Authority() string { return k.authority }

// canonicalAgentPubkey normalizes an agent public key string to its
// storage/lookup form. Ed448 / Ed25519 / secp256k1 pubkeys are hex-
// encoded and hex is case-insensitive, so "ed25519:ABCDEF" and
// "ed25519:abcdef" describe the SAME public key material. Without
// case normalization here, a caller could register a passport under
// "ed25519:ABCDEF…" and a second caller (or the same caller via
// MsgRegisterPassport from a different SDK that emits lowercase)
// could register a second passport for "ed25519:abcdef…", holding
// two stakes and two reputation vectors for one real agent; or the
// router/storefront lookup path — which routes through
// normalizeAgentSubject (internal/auth/jwt.go) and emits lowercase
// hex — would silently fail to find a passport registered with
// uppercase hex, treating the agent as unknown.
//
// Lowercasing unifies both cases under one canonical form. The algo
// prefix ("ed25519:", etc.) is already expected to be lowercase by
// normalizeAgentSubject, so this is idempotent for any properly-
// formed subject.
func canonicalAgentPubkey(agentPubkey string) string {
	return strings.ToLower(strings.TrimSpace(agentPubkey))
}

// GetParams retrieves module parameters.
func (k Keeper) GetParams(ctx context.Context) *types.Params {
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams()
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		k.Logger(sdkCtx).Error("passport params load failed, returning defaults", "error", err)
		return types.DefaultParams()
	}
	if params == nil {
		return types.DefaultParams()
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
	return k.state.Params.Set(ctx, params)
}

// NextPassportID computes the next deterministic passport identifier.
func (k Keeper) NextPassportID(ctx context.Context) (string, error) {
	id, err := k.state.PassportSeq.Next(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to allocate passport id: %w", err)
	}
	return fmt.Sprintf("passport-%d", id), nil
}

// HasPassport checks if an agent already has a passport.
func (k Keeper) HasPassport(ctx context.Context, agentPubkey string) bool {
	agentPubkey = canonicalAgentPubkey(agentPubkey)
	if agentPubkey == "" {
		return false
	}
	_, err := k.state.AgentIndex.Get(ctx, agentPubkey)
	return err == nil
}

// GetPassport fetches a passport by ID.
func (k Keeper) GetPassport(ctx context.Context, passportID string) (*types.AgentPassport, bool) {
	passport, err := k.state.Passports.Get(ctx, passportID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(sdk.UnwrapSDKContext(ctx)).Error("failed to read passport", "passport_id", passportID, "error", err)
		return nil, false
	}
	if passport == nil {
		return nil, false
	}
	normalizePassport(passport)
	return passport, true
}

// GetPassportByAgent fetches a passport by agent public key.
func (k Keeper) GetPassportByAgent(ctx context.Context, agentPubkey string) (*types.AgentPassport, bool) {
	agentPubkey = canonicalAgentPubkey(agentPubkey)
	if agentPubkey == "" {
		return nil, false
	}
	passportID, err := k.state.AgentIndex.Get(ctx, agentPubkey)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(sdk.UnwrapSDKContext(ctx)).Error("failed to read agent index", "agent_pubkey", agentPubkey, "error", err)
		return nil, false
	}
	return k.GetPassport(ctx, passportID)
}

// SavePassport writes a passport record to state.
func (k Keeper) SavePassport(ctx context.Context, passport *types.AgentPassport) error {
	if passport == nil {
		return fmt.Errorf("passport cannot be nil")
	}
	passport.AgentPubkey = canonicalAgentPubkey(passport.AgentPubkey)
	if passport.AgentPubkey == "" {
		return types.ErrInvalidAgentPubkey
	}
	normalizePassport(passport)

	existing, err := k.state.Passports.Get(ctx, passport.PassportId)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("failed to load existing passport %s: %w", passport.PassportId, err)
	}

	indexedPassportID, err := k.state.AgentIndex.Get(ctx, passport.AgentPubkey)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("failed to read agent index: %w", err)
	}
	if err == nil && indexedPassportID != passport.PassportId {
		return fmt.Errorf("agent pubkey %s already belongs to passport %s", passport.AgentPubkey, indexedPassportID)
	}

	if existing != nil {
		previousAgentPubkey := canonicalAgentPubkey(existing.AgentPubkey)
		if previousAgentPubkey != "" && previousAgentPubkey != passport.AgentPubkey {
			if err := k.state.AgentIndex.Remove(ctx, previousAgentPubkey); err != nil && !errors.Is(err, collections.ErrNotFound) {
				return fmt.Errorf("failed to remove stale agent index %s: %w", previousAgentPubkey, err)
			}
		}
	}

	if err := k.state.Passports.Set(ctx, passport.PassportId, passport); err != nil {
		return fmt.Errorf("failed to store passport %s: %w", passport.PassportId, err)
	}
	if err := k.state.AgentIndex.Set(ctx, passport.AgentPubkey, passport.PassportId); err != nil {
		return fmt.Errorf("failed to update agent index: %w", err)
	}
	return nil
}

func normalizePassport(passport *types.AgentPassport) {
	if passport.Summary == nil {
		passport.Summary = &types.PassportSummary{}
	}
	denom := types.DefaultMinStakeDenom
	if !passport.Stake.Amount.IsNil() && passport.Stake.Denom != "" {
		denom = passport.Stake.Denom
	}
	if !passport.Summary.TotalSpend.Amount.IsNil() && passport.Summary.TotalSpend.Denom != "" {
		denom = passport.Summary.TotalSpend.Denom
	}
	if passport.Summary.TotalSpend.Amount.IsNil() {
		passport.Summary.TotalSpend = sdk.NewCoin(denom, sdkmath.ZeroInt())
	}
	if passport.Summary.SettledSpend.Amount.IsNil() {
		passport.Summary.SettledSpend = sdk.NewCoin(denom, sdkmath.ZeroInt())
	}
	if passport.Summary.RefundedSpend.Amount.IsNil() {
		passport.Summary.RefundedSpend = sdk.NewCoin(denom, sdkmath.ZeroInt())
	}
	if passport.TierState == nil {
		enteredAt := time.Time{}
		if passport.CreatedTs > 0 {
			enteredAt = time.Unix(passport.CreatedTs, 0).UTC()
		}
		passport.TierState = types.InitialPassportTierState(enteredAt)
	}
	if passport.ScoreBreakdown == nil {
		passport.ScoreBreakdown = types.ScoreBreakdownFromProto(passport.Reputation, passport.Summary)
	}
	if len(passport.TierHistory) == 0 {
		enteredAt := time.Time{}
		if passport.CreatedTs > 0 {
			enteredAt = time.Unix(passport.CreatedTs, 0).UTC()
		}
		passport.TierHistory = []*types.PassportTierHistoryEntry{
			types.InitialPassportTierHistoryEntry(enteredAt, passport.ScoreBreakdown),
		}
	}
}

// DeletePassport removes a passport from state.
func (k Keeper) DeletePassport(ctx context.Context, passportID string) error {
	passport, found := k.GetPassport(ctx, passportID)
	if !found {
		return types.ErrPassportNotFound
	}
	// Remove from agent index
	if err := k.state.AgentIndex.Remove(ctx, passport.AgentPubkey); err != nil {
		return fmt.Errorf("failed to remove agent index: %w", err)
	}
	if err := k.state.Passports.Remove(ctx, passportID); err != nil {
		return fmt.Errorf("failed to remove passport %s: %w", passportID, err)
	}
	return nil
}

// IteratePassports walks through all passport records invoking the callback for each entry.
func (k Keeper) IteratePassports(ctx context.Context, cb func(*types.AgentPassport) bool) error {
	return k.state.Passports.Walk(ctx, nil, func(_ string, passport *types.AgentPassport) (bool, error) {
		if passport == nil {
			return false, nil
		}
		normalizePassport(passport)
		return cb(passport), nil
	})
}

// ListPassportsByAgent returns passports ordered by agent pubkey with cursor-based pagination.
func (k Keeper) ListPassportsByAgent(ctx context.Context, cursor string, limit uint64) (passports []*types.AgentPassport, nextCursor string, err error) {
	cursor = strings.TrimSpace(cursor)
	if limit == 0 {
		return []*types.AgentPassport{}, "", nil
	}

	var rng collections.Ranger[string]
	if cursor != "" {
		rng = new(collections.Range[string]).StartExclusive(cursor)
	}

	iter, err := k.state.AgentIndex.Iterate(ctx, rng)
	if err != nil {
		return nil, "", fmt.Errorf("failed to iterate passport agent index: %w", err)
	}
	defer func() {
		if cerr := iter.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close passport agent iterator: %w", cerr)
		}
	}()

	passports = make([]*types.AgentPassport, 0, limit)
	lastCursor := ""
	hasMore := false

	for ; iter.Valid(); iter.Next() {
		if uint64(len(passports)) >= limit {
			hasMore = true
			break
		}

		agentPubkey, keyErr := iter.Key()
		if keyErr != nil {
			return nil, "", fmt.Errorf("failed to read passport agent key: %w", keyErr)
		}
		passportID, valueErr := iter.Value()
		if valueErr != nil {
			return nil, "", fmt.Errorf("failed to read passport agent index value: %w", valueErr)
		}

		passport, found := k.GetPassport(ctx, passportID)
		if !found || passport == nil {
			continue
		}

		passports = append(passports, passport)
		lastCursor = strings.TrimSpace(agentPubkey)
	}

	if hasMore && lastCursor != "" {
		nextCursor = lastCursor
	}

	return passports, nextCursor, nil
}

// RegisterPassport creates a new passport for an agent with stake.
func (k Keeper) RegisterPassport(ctx context.Context, msg *types.MsgRegisterPassport) (*types.AgentPassport, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(ctx)
	agentPubkey := canonicalAgentPubkey(msg.AgentPubkey)
	if agentPubkey == "" {
		return nil, types.ErrInvalidAgentPubkey
	}

	// 1. Validate stake amount
	stake := msg.Stake
	minStake := params.MinStakeCoin()
	if stake.Amount.LT(minStake.Amount) {
		return nil, types.ErrInsufficientStake
	}
	if stake.Denom != minStake.Denom {
		return nil, fmt.Errorf("stake denom must be %s", minStake.Denom)
	}

	// 2. Check agent not already registered
	if k.HasPassport(ctx, agentPubkey) {
		return nil, types.ErrPassportExists
	}

	// 3. Lock stake
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, fmt.Errorf("invalid creator address: %w", err)
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creator, types.ModuleName, sdk.NewCoins(stake)); err != nil {
		return nil, fmt.Errorf("failed to lock stake: %w", err)
	}

	// 4. Generate passport ID
	passportID, err := k.NextPassportID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate passport ID: %w", err)
	}

	// 5. Create passport
	blockTime := sdkCtx.BlockTime()
	passport := &types.AgentPassport{
		PassportId:   passportID,
		AgentPubkey:  agentPubkey,
		OwnerAddress: msg.Creator,
		CreatedTs:    blockTime.Unix(),
		Issuer:       k.authority,
		Stake:        stake,
		Status:       types.PassportStatus_PASSPORT_STATUS_ACTIVE,
		Summary: &types.PassportSummary{
			TotalSpend:    sdk.NewCoin(stake.Denom, sdkmath.ZeroInt()),
			SettledSpend:  sdk.NewCoin(stake.Denom, sdkmath.ZeroInt()),
			RefundedSpend: sdk.NewCoin(stake.Denom, sdkmath.ZeroInt()),
		},
		Reputation: &types.ReputationVector{
			Reliability:     500, // Start at neutral
			Quality:         500,
			Trustworthiness: 500,
			Composite:       500,
		},
		BundleAnchors:  []*types.BundleAnchor{},
		TierState:      types.InitialPassportTierState(blockTime),
		ScoreBreakdown: types.NeutralPassportScoreBreakdown(blockTime),
	}
	passport.TierHistory = []*types.PassportTierHistoryEntry{
		types.InitialPassportTierHistoryEntry(blockTime, passport.ScoreBreakdown),
	}

	// 6. Store passport
	if err := k.SavePassport(ctx, passport); err != nil {
		// Rollback stake if save fails
		if rollbackErr := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creator, sdk.NewCoins(stake)); rollbackErr != nil {
			k.Logger(sdkCtx).Error("failed to rollback stake", "error", rollbackErr)
			return nil, fmt.Errorf("failed to save passport: %w; additionally failed to rollback stake: %v", err, rollbackErr)
		}
		return nil, fmt.Errorf("failed to save passport: %w", err)
	}

	// 7. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"passport_registered",
			sdk.NewAttribute("passport_id", passport.PassportId),
			sdk.NewAttribute("agent_pubkey", passport.AgentPubkey),
			sdk.NewAttribute("owner", passport.OwnerAddress),
			sdk.NewAttribute("stake", stake.String()),
		),
	)

	return passport, nil
}

// SuspendPassport suspends an active passport.
func (k Keeper) SuspendPassport(ctx context.Context, msg *types.MsgSuspendPassport) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority
	if msg.Authority != k.authority {
		return types.ErrUnauthorized
	}

	passport, found := k.GetPassport(ctx, msg.PassportId)
	if !found {
		return types.ErrPassportNotFound
	}

	if passport.Status != types.PassportStatus_PASSPORT_STATUS_ACTIVE {
		return types.ErrPassportNotActive
	}

	passport.Status = types.PassportStatus_PASSPORT_STATUS_SUSPENDED
	passport.SuspensionReason = msg.Reason

	if err := k.SavePassport(ctx, passport); err != nil {
		return fmt.Errorf("failed to update passport: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"passport_suspended",
			sdk.NewAttribute("passport_id", passport.PassportId),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return nil
}

// RevokePassport permanently revokes a passport.
func (k Keeper) RevokePassport(ctx context.Context, msg *types.MsgRevokePassport) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority
	if msg.Authority != k.authority {
		return types.ErrUnauthorized
	}

	passport, found := k.GetPassport(ctx, msg.PassportId)
	if !found {
		return types.ErrPassportNotFound
	}

	if passport.Status == types.PassportStatus_PASSPORT_STATUS_REVOKED {
		return types.ErrPassportRevoked
	}

	passport.Status = types.PassportStatus_PASSPORT_STATUS_REVOKED
	passport.RevocationReason = msg.Reason

	if err := k.SavePassport(ctx, passport); err != nil {
		return fmt.Errorf("failed to update passport: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"passport_revoked",
			sdk.NewAttribute("passport_id", passport.PassportId),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return nil
}

// ReactivatePassport reactivates a suspended passport.
func (k Keeper) ReactivatePassport(ctx context.Context, msg *types.MsgReactivatePassport) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	passport, found := k.GetPassport(ctx, msg.PassportId)
	if !found {
		return types.ErrPassportNotFound
	}

	// Only module authority can lift a suspension.
	if msg.Owner != k.authority {
		return types.ErrUnauthorized
	}

	// Only suspended passports can be reactivated
	if passport.Status != types.PassportStatus_PASSPORT_STATUS_SUSPENDED {
		return types.ErrCannotReactivate
	}

	// Verify stake meets minimum requirement
	params := k.GetParams(ctx)
	minStake := params.MinStakeCoin()
	currentStake := passport.Stake
	if currentStake.Amount.LT(minStake.Amount) {
		return types.ErrInsufficientStake.Wrapf("stake %s < min %s", currentStake, minStake)
	}

	passport.Status = types.PassportStatus_PASSPORT_STATUS_ACTIVE
	passport.SuspensionReason = ""

	if err := k.SavePassport(ctx, passport); err != nil {
		return fmt.Errorf("failed to update passport: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"passport_reactivated",
			sdk.NewAttribute("passport_id", passport.PassportId),
		),
	)

	return nil
}

// TopUpStake adds stake to an existing non-revoked passport.
func (k Keeper) TopUpStake(ctx context.Context, msg *types.MsgTopUpStake) (sdk.Coin, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	passport, found := k.GetPassport(ctx, msg.PassportId)
	if !found {
		return sdk.Coin{}, types.ErrPassportNotFound
	}
	if passport.OwnerAddress != msg.Owner {
		return sdk.Coin{}, types.ErrUnauthorized
	}
	if passport.Status == types.PassportStatus_PASSPORT_STATUS_REVOKED {
		return sdk.Coin{}, types.ErrPassportRevoked
	}

	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid owner address: %w", err)
	}
	topUp := msg.Amount
	if topUp.Denom == "" || topUp.Amount.IsNil() || !topUp.Amount.IsPositive() {
		return sdk.Coin{}, types.ErrInsufficientStake
	}
	currentStake := passport.Stake
	if currentStake.Denom == "" || currentStake.Amount.IsNil() {
		return sdk.Coin{}, types.ErrInsufficientStake
	}
	if topUp.Denom != currentStake.Denom {
		return sdk.Coin{}, fmt.Errorf("top-up denom %s does not match stake denom %s", topUp.Denom, currentStake.Denom)
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, owner, types.ModuleName, sdk.NewCoins(topUp)); err != nil {
		return sdk.Coin{}, fmt.Errorf("failed to lock top-up stake: %w", err)
	}

	updatedStake := sdk.NewCoin(currentStake.Denom, currentStake.Amount.Add(topUp.Amount))
	passport.Stake = updatedStake

	if err := k.SavePassport(ctx, passport); err != nil {
		if rollbackErr := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, sdk.NewCoins(topUp)); rollbackErr != nil {
			k.Logger(sdkCtx).Error("failed to rollback top-up stake", "error", rollbackErr)
			return sdk.Coin{}, fmt.Errorf("failed to save passport: %w; additionally failed to rollback top-up stake: %v", err, rollbackErr)
		}
		return sdk.Coin{}, fmt.Errorf("failed to save passport: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"stake_topped_up",
			sdk.NewAttribute("passport_id", passport.PassportId),
			sdk.NewAttribute("owner", passport.OwnerAddress),
			sdk.NewAttribute("amount", topUp.String()),
			sdk.NewAttribute("stake", updatedStake.String()),
		),
	)

	return updatedStake, nil
}

// UnregisterPassport removes an owner's non-revoked passport and refunds remaining stake.
func (k Keeper) UnregisterPassport(ctx context.Context, msg *types.MsgUnregisterPassport) (sdk.Coin, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	passport, found := k.GetPassport(ctx, msg.PassportId)
	if !found {
		return sdk.Coin{}, types.ErrPassportNotFound
	}
	if passport.OwnerAddress != msg.Owner {
		return sdk.Coin{}, types.ErrUnauthorized
	}
	if passport.Status == types.PassportStatus_PASSPORT_STATUS_REVOKED {
		return sdk.Coin{}, types.ErrPassportRevoked
	}

	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid owner address: %w", err)
	}
	refund := passport.Stake
	if refund.Denom == "" || refund.Amount.IsNil() {
		return sdk.Coin{}, types.ErrInsufficientStake
	}

	if refund.Amount.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, sdk.NewCoins(refund)); err != nil {
			return sdk.Coin{}, fmt.Errorf("failed to refund passport stake: %w", err)
		}
	}

	if err := k.DeletePassport(ctx, passport.PassportId); err != nil {
		return sdk.Coin{}, fmt.Errorf("failed to delete passport after stake refund: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"passport_unregistered",
			sdk.NewAttribute("passport_id", passport.PassportId),
			sdk.NewAttribute("owner", passport.OwnerAddress),
			sdk.NewAttribute("refunded_stake", refund.String()),
		),
	)

	return refund, nil
}

// SlashStake slashes stake from a passport.
func (k Keeper) SlashStake(ctx context.Context, msg *types.MsgSlashStake) (sdk.Coin, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority
	if msg.Authority != k.authority {
		return sdk.Coin{}, types.ErrUnauthorized
	}

	passport, found := k.GetPassport(ctx, msg.PassportId)
	if !found {
		return sdk.Coin{}, types.ErrPassportNotFound
	}

	slashAmount := msg.Amount
	currentStake := passport.Stake

	if slashAmount.Denom != currentStake.Denom {
		return sdk.Coin{}, fmt.Errorf("slash denom %s does not match stake denom %s", slashAmount.Denom, currentStake.Denom)
	}
	if slashAmount.Amount.GT(currentStake.Amount) {
		return sdk.Coin{}, types.ErrSlashExceedsStake
	}

	// Burn the slashed amount
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(slashAmount)); err != nil {
		return sdk.Coin{}, fmt.Errorf("failed to burn slashed stake: %w", err)
	}

	// Update remaining stake
	remaining := sdk.NewCoin(currentStake.Denom, currentStake.Amount.Sub(slashAmount.Amount))
	passport.Stake = remaining
	applySlashTierDemotion(passport, sdkCtx.BlockTime())

	// Suspend if stake drops below minimum
	params := k.GetParams(ctx)
	minStake := params.MinStakeCoin()
	if remaining.Amount.LT(minStake.Amount) {
		passport.Status = types.PassportStatus_PASSPORT_STATUS_SUSPENDED
		passport.SuspensionReason = fmt.Sprintf("insufficient stake after slash: %s < %s", remaining, minStake)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"passport_suspended",
				sdk.NewAttribute("passport_id", passport.PassportId),
				sdk.NewAttribute("reason", passport.SuspensionReason),
			),
		)
	}

	if err := k.SavePassport(ctx, passport); err != nil {
		return sdk.Coin{}, fmt.Errorf("failed to update passport: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"stake_slashed",
			sdk.NewAttribute("passport_id", passport.PassportId),
			sdk.NewAttribute("amount", slashAmount.String()),
			sdk.NewAttribute("remaining", remaining.String()),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return remaining, nil
}

func applySlashTierDemotion(passport *types.AgentPassport, blockTime time.Time) {
	previousTierState := types.TierStateFromProto(passport.TierState, blockTime)
	if previousTierState.CurrentTier <= types.TierProbationary {
		return
	}

	reputation := types.ReputationResultFromScoreBreakdown(passport.ScoreBreakdown)
	if reputation == nil {
		reputation = types.ReputationResultFromScoreBreakdown(types.ScoreBreakdownFromProto(passport.Reputation, passport.Summary))
	}
	if reputation == nil {
		reputation = &types.ReputationResult{}
	}
	reputation.UpdatedAt = blockTime

	receiptCount := uint64(0)
	if passport.Summary != nil {
		receiptCount = passport.Summary.TotalReceipts
	}

	defs := types.DefaultTierDefinitions()
	tierResult := types.EvaluateTier(types.TierEvaluationInput{
		Reputation:     reputation,
		ReceiptCount:   receiptCount,
		DisputeRate30d: types.Clamp01(1.0 - reputation.Dispute),
		Slashed:        true,
		Now:            blockTime,
		State:          previousTierState,
		Definitions:    defs,
	})

	passport.TierState = types.TierEvaluationResultToProto(tierResult, defs)
	if tierResult.Promoted || tierResult.Demoted || previousTierState.CurrentTier != tierResult.CurrentTier {
		passport.TierHistory = append(passport.TierHistory, types.TierHistoryEntryFromEvaluation(
			previousTierState.CurrentTier,
			tierResult,
			passport.ScoreBreakdown,
			blockTime,
		))
	}
}

// ModuleAddress returns the module account address.
func (k Keeper) ModuleAddress() sdk.AccAddress {
	return k.accountKeeper.GetModuleAddress(types.ModuleAccountName)
}

// ImportState imports genesis state.
func (k Keeper) ImportState(ctx sdk.Context, genesis *types.GenesisState) error {
	if genesis == nil {
		genesis = types.DefaultGenesis()
	}
	if err := genesis.Validate(); err != nil {
		return fmt.Errorf("invalid genesis state: %w", err)
	}
	if err := k.SetParams(ctx, genesis.Params); err != nil {
		return fmt.Errorf("failed to set params: %w", err)
	}

	maxSeq := uint64(0)
	hasSeq := false
	for _, passport := range genesis.Passports {
		if err := k.SavePassport(ctx, passport); err != nil {
			return fmt.Errorf("failed to import passport %s: %w", passport.PassportId, err)
		}

		var seq uint64
		if _, err := fmt.Sscanf(passport.PassportId, "passport-%d", &seq); err == nil {
			if !hasSeq || seq > maxSeq {
				maxSeq = seq
				hasSeq = true
			}
		}
	}

	if hasSeq {
		// Set sequence to maxSeq+1 so the next call to Next() returns maxSeq+1,
		// avoiding a collision with the highest imported passport ID.
		if err := k.state.PassportSeq.Set(ctx, maxSeq+1); err != nil {
			return fmt.Errorf("failed to set passport sequence: %w", err)
		}
	}

	return nil
}

// ExportState exports genesis state.
func (k Keeper) ExportState(ctx sdk.Context) (*types.GenesisState, error) {
	params := k.GetParams(ctx)
	passports := make([]*types.AgentPassport, 0)
	err := k.IteratePassports(ctx, func(passport *types.AgentPassport) bool {
		passports = append(passports, passport)
		return false
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate passports: %w", err)
	}
	return &types.GenesisState{
		Params:    params,
		Passports: passports,
	}, nil
}
