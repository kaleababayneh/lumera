
// Package keeper implements the reserve module storage and business logic for commitments.
package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/reserve/types"
)

func marshalParams(params *types.Params) ([]byte, error) {
	return json.Marshal(params)
}

func unmarshalParams(bz []byte) (*types.Params, error) {
	if len(bz) == 0 {
		return types.DefaultParams(), nil
	}
	var params types.Params
	if err := json.Unmarshal(bz, &params); err != nil {
		return nil, err
	}
	return &params, nil
}

func marshalCommitment(c types.ReserveCommitment) ([]byte, error) {
	return json.Marshal(c)
}

func unmarshalCommitment(bz []byte) (types.ReserveCommitment, error) {
	var commitment types.ReserveCommitment
	if err := json.Unmarshal(bz, &commitment); err != nil {
		return types.ReserveCommitment{}, err
	}
	return commitment, nil
}

// State wraps module collections.
type State struct {
	Schema              collections.Schema
	Params              collections.Item[[]byte]
	Commitments         collections.Map[string, []byte]
	CommitmentSeq       collections.Sequence
	CommitmentsByPolicy collections.KeySet[collections.Pair[string, string]]
	CommitmentExpiry    collections.KeySet[collections.Pair[time.Time, string]]
	CommitmentsByOwner  collections.KeySet[collections.Pair[string, string]]
}

// Keeper manages reserve commitments.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService corestore.KVStoreService
	authority    string
	logger       log.Logger
	state        State
}

const maxCommitmentListLimit = 1000

// reserveEndBlockerSweepLimit bounds how many expired commitments the per-block
// EndBlocker sweep releases. It matches AllocateReserve's lazy-sweep cap so a
// large expiry backlog drains over several blocks instead of doing unbounded
// work in a single EndBlock.
const reserveEndBlockerSweepLimit = 50

// NewKeeper builds the reserve keeper instance.
func NewKeeper(cdc codec.BinaryCodec, storeService corestore.KVStoreService, authority string, logger log.Logger) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsKeyPrefix),
			"params",
			collections.BytesValue,
		),
		Commitments: collections.NewMap(
			sb,
			collections.NewPrefix(types.CommitmentKeyPrefix),
			"commitments",
			collections.StringKey,
			collections.BytesValue,
		),
		CommitmentSeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.CommitmentSeqKeyPrefix),
			"commitment_seq",
		),
		CommitmentsByPolicy: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.CommitmentByPolicyKeyPrefix),
			"commitments_by_policy",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		CommitmentExpiry: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.CommitmentExpiryKeyPrefix),
			"commitment_expiry",
			collections.PairKeyCodec(sdk.TimeKey, collections.StringKey),
		),
		CommitmentsByOwner: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.CommitmentByOwnerKeyPrefix),
			"commitments_by_owner",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("reserve schema build failed: %w", err))
	}
	state.Schema = schema

	return &Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,
		logger:       logger.With("module", fmt.Sprintf("x/%s", types.ModuleName)),
		state:        state,
	}
}

// Logger returns module logger.
func (k *Keeper) Logger() log.Logger { return k.logger }

// GetParams fetches current params (defaults when unset).
func (k *Keeper) GetParams(ctx context.Context) (*types.Params, error) {
	bz, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return nil, err
	}
	return unmarshalParams(bz)
}

// SetParams stores parameters after validation.
func (k *Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if err := params.ValidateBasic(); err != nil {
		return err
	}
	bz, err := marshalParams(params)
	if err != nil {
		return err
	}
	return k.state.Params.Set(ctx, bz)
}

func isReserveRequestIdentifierSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func validateReserveRequestIdentifier(field, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s required", field)
		}
		return nil
	}
	if isReserveRequestIdentifierSpace(value[0]) || isReserveRequestIdentifierSpace(value[len(value)-1]) {
		for i := 0; i < len(value); i++ {
			if !isReserveRequestIdentifierSpace(value[i]) {
				return fmt.Errorf("%s cannot contain leading or trailing whitespace", field)
			}
		}
		if required {
			return fmt.Errorf("%s required", field)
		}
		return fmt.Errorf("%s cannot contain only whitespace", field)
	}
	if len(value) > types.MaxReserveIdentifierLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", field, types.MaxReserveIdentifierLen, len(value))
	}
	return nil
}

// CreateCommitment provisions a new reserve commitment and returns the stored record.
func (k *Keeper) CreateCommitment(ctx context.Context, req types.ReserveRequest) (*types.ReserveCommitment, error) {
	if err := sdk.ValidateDenom(req.Amount.Denom); err != nil {
		return nil, fmt.Errorf("invalid amount denom: %w", err)
	}
	if _, err := sdk.AccAddressFromBech32(req.Owner); err != nil {
		return nil, fmt.Errorf("invalid owner: %w", err)
	}
	if err := validateReserveRequestIdentifier("policy id", req.PolicyID, true); err != nil {
		return nil, err
	}
	if err := validateReserveRequestIdentifier("tool id", req.ToolID, false); err != nil {
		return nil, err
	}
	if err := validateReserveRequestIdentifier("tier", req.Tier, true); err != nil {
		return nil, err
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if req.Amount.Denom != params.CreditDenom {
		return nil, types.ErrCreditDenomMismatch
	}
	tier, found := params.FindTier(req.Tier)
	if !found {
		return nil, types.ErrTierNotFound
	}
	if !req.Amount.Amount.GTE(tier.MinCommitmentAmount) {
		return nil, fmt.Errorf("amount below tier minimum: need >= %s", tier.MinCommitmentAmount)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return nil, fmt.Errorf("reserve: block time must be set")
	}
	duration := req.Duration
	if duration <= 0 {
		duration = time.Duration(tier.DefaultDurationSec) * time.Second //#nosec G115 -- duration bounded by tier configuration
	}

	// Count currently-active commitments on the same tier for this
	// policy, enforcing tier.MaxActivePerPolicy. A commitment is
	// "active" if its ExpireTime is strictly after now. Equality is
	// expired, matching releaseExpired's [Min, now] sweep and the usual
	// `now < expires_at` contract for time-bounded entitlements.
	// Without the expiry filter here, expired-but-not-yet-swept
	// commitments would count toward the cap and block legitimate
	// new commitments until the next EndBlocker releaseExpired sweep:
	// a user who had a valid commitment that expired at block T would
	// be unable to CreateCommitment at block T+1 for a tier with
	// MaxActivePerPolicy == <current-active-count-including-expired>.
	policyActive := uint32(0)
	rng := collections.NewPrefixedPairRange[string, string](req.PolicyID)
	err = k.state.CommitmentsByPolicy.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		commitID := key.K2()
		bz, lookupErr := k.state.Commitments.Get(ctx, commitID)
		if lookupErr != nil {
			return true, lookupErr
		}
		commit, unmarshalErr := unmarshalCommitment(bz)
		if unmarshalErr != nil {
			return true, unmarshalErr
		}
		if commit.Tier != tier.Name {
			return false, nil
		}
		if !commit.ExpireTime.After(now) {
			// Expired: not counted against the active cap. Release
			// sweep (EndBlocker / AllocateReserve's lazy sweep) will
			// clear these on the next opportunity.
			return false, nil
		}
		// Stale-denom: a governance MsgUpdateParams(CreditDenom=X)
		// leaves existing commitments carrying the OLD denom; those
		// commitments are unusable by AllocateReserve (which skips
		// them at :322-324) so they must not count toward
		// MaxActivePerPolicy either. Without this filter, users with
		// the new CreditDenom cannot CreateCommitment until stale
		// commitments expire, even though they're uncollectable.
		if commit.RemainingAmount.Denom != params.CreditDenom {
			return false, nil
		}
		policyActive++
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	if policyActive >= tier.MaxActivePerPolicy {
		return nil, fmt.Errorf("policy %s already has %d active commitments for tier %s", req.PolicyID, policyActive, tier.Name)
	}

	seq, err := k.state.CommitmentSeq.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("allocate commitment id: %w", err)
	}
	commitment := &types.ReserveCommitment{
		ID:              fmt.Sprintf("resv-%d", seq),
		Owner:           req.Owner,
		PolicyID:        req.PolicyID,
		ToolID:          req.ToolID,
		Tier:            tier.Name,
		TotalAmount:     req.Amount,
		RemainingAmount: req.Amount,
		DiscountBps:     tier.DiscountBps,
		StartTime:       now,
		ExpireTime:      now.Add(duration),
		RolloverAllowed: tier.RolloverAllowed,
	}
	if err := commitment.ValidateBasic(); err != nil {
		return nil, err
	}

	commitBytes, err := marshalCommitment(*commitment)
	if err != nil {
		return nil, err
	}
	if err := k.state.Commitments.Set(ctx, commitment.ID, commitBytes); err != nil {
		return nil, err
	}
	if err := k.state.CommitmentsByPolicy.Set(ctx, collections.Join(req.PolicyID, commitment.ID)); err != nil {
		return nil, err
	}
	if err := k.state.CommitmentsByOwner.Set(ctx, collections.Join(req.Owner, commitment.ID)); err != nil {
		return nil, err
	}
	if err := k.state.CommitmentExpiry.Set(ctx, collections.Join(commitment.ExpireTime, commitment.ID)); err != nil {
		return nil, err
	}

	return commitment, nil
}

// AllocateReserve attempts to consume reserve balance for the policy/tool combination.
func (k *Keeper) AllocateReserve(ctx context.Context, owner, policyID, toolID string, amount sdk.Coin) (types.ReserveAllocation, error) {
	allocation := types.ReserveAllocation{Applied: false, DiscountedPrice: amount}
	if amount.Amount.IsZero() {
		return allocation, nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return allocation, err
	}
	if amount.Denom != params.CreditDenom {
		return allocation, types.ErrCreditDenomMismatch
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return allocation, fmt.Errorf("reserve: block time must be set")
	}

	if err := k.releaseExpired(ctx, now, 50); err != nil {
		return allocation, err
	}

	var (
		exactCommit    types.ReserveCommitment
		exactPair      collections.Pair[string, string]
		exactSet       bool
		fallbackCommit types.ReserveCommitment
		fallbackPair   collections.Pair[string, string]
		fallbackSet    bool
	)

	rng := collections.NewPrefixedPairRange[string, string](policyID)
	walkErr := k.state.CommitmentsByPolicy.Walk(ctx, rng, func(pair collections.Pair[string, string]) (bool, error) {
		bz, err := k.state.Commitments.Get(ctx, pair.K2())
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		commit, err := unmarshalCommitment(bz)
		if err != nil {
			return false, err
		}
		// Enforce ownership
		if commit.Owner != owner {
			return false, nil
		}
		if !commit.ExpireTime.After(now) {
			return false, nil
		}
		// Skip commitments whose denom no longer matches the current
		// CreditDenom. A governance MsgUpdateParams that changes
		// CreditDenom would leave existing commitments carrying the OLD
		// denom in RemainingAmount; without this filter the line-below
		// .Sub(amount) would panic on coin-denom mismatch (sdk.Coin.Sub
		// panics rather than erroring). baseapp recovers such panics into
		// tx errors, so this isn't a chain-halt, but every subsequent
		// AllocateReserve that picks a stale commitment would fail
		// cryptically until the commitment expires or is released.
		if commit.RemainingAmount.Denom != amount.Denom {
			return false, nil
		}
		if !commit.RemainingAmount.Amount.GTE(amount.Amount) {
			return false, nil
		}
		if toolID != "" && commit.ToolID == toolID {
			if !exactSet || preferReserveCommit(commit, exactCommit) {
				exactCommit = commit
				exactPair = pair
				exactSet = true
			}
			return false, nil
		}
		if commit.ToolID != "" {
			// If the commitment is for a specific tool, and we didn't match it exactly above, it's not a valid fallback.
			return false, nil
		}
		// Empty commitment ToolID is the wildcard fallback.
		if !fallbackSet || preferReserveCommit(commit, fallbackCommit) {
			fallbackCommit = commit
			fallbackPair = pair
			fallbackSet = true
		}
		return false, nil
	})
	if walkErr != nil {
		return allocation, walkErr
	}

	var (
		chosenCommit types.ReserveCommitment
		chosenPair   collections.Pair[string, string]
		chosenSet    bool
	)
	if exactSet {
		chosenCommit = exactCommit
		chosenPair = exactPair
		chosenSet = true
	} else if fallbackSet {
		chosenCommit = fallbackCommit
		chosenPair = fallbackPair
		chosenSet = true
	}

	if !chosenSet {
		return allocation, nil
	}

	remaining := chosenCommit.RemainingAmount.Sub(amount)
	if remaining.IsNegative() {
		return allocation, types.ErrInsufficientCapacity
	}
	chosenCommit.RemainingAmount = remaining

	updatedBytes, marshalErr := marshalCommitment(chosenCommit)
	if marshalErr != nil {
		return allocation, marshalErr
	}

	if remaining.Amount.IsZero() {
		if rmErr := k.state.Commitments.Remove(ctx, chosenCommit.ID); rmErr != nil {
			return allocation, rmErr
		}
		if rmErr := k.state.CommitmentsByPolicy.Remove(ctx, chosenPair); rmErr != nil {
			return allocation, rmErr
		}
		if rmErr := k.state.CommitmentsByOwner.Remove(ctx, collections.Join(chosenCommit.Owner, chosenCommit.ID)); rmErr != nil && !errors.Is(rmErr, collections.ErrNotFound) {
			return allocation, rmErr
		}
		if rmErr := k.state.CommitmentExpiry.Remove(ctx, collections.Join(chosenCommit.ExpireTime, chosenCommit.ID)); rmErr != nil {
			return allocation, rmErr
		}
	} else {
		if setErr := k.state.Commitments.Set(ctx, chosenCommit.ID, updatedBytes); setErr != nil {
			return allocation, setErr
		}
	}

	discounted := types.ApplyDiscount(amount, chosenCommit.DiscountBps)
	allocation.Applied = true
	allocation.CommitmentID = chosenCommit.ID
	allocation.DiscountedPrice = discounted
	return allocation, nil
}

// HasActiveCommitment reports whether the policy has at least one non-expired
// commitment. A non-empty toolID matches either that exact tool or a wildcard
// commitment; an empty toolID asks whether the policy has any active
// commitment.
func (k *Keeper) HasActiveCommitment(ctx context.Context, policyID, toolID string) (bool, error) {
	if policyID == "" {
		return false, nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return false, fmt.Errorf("reserve: block time must be set")
	}

	// Fetch current CreditDenom so we can skip stale-denom
	// commitments (governance MsgUpdateParams leaves OLD-denom
	// records in state that AllocateReserve correctly refuses at
	// :322-324; HasActiveCommitment must match — otherwise it
	// reports 'active=true' for commits AllocateReserve then
	// skips, creating a cross-function semantics asymmetry).
	params, err := k.GetParams(ctx)
	if err != nil {
		return false, err
	}

	found := false
	rng := collections.NewPrefixedPairRange[string, string](policyID)
	err = k.state.CommitmentsByPolicy.Walk(ctx, rng, func(pair collections.Pair[string, string]) (bool, error) {
		bz, err := k.state.Commitments.Get(ctx, pair.K2())
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		commitment, err := unmarshalCommitment(bz)
		if err != nil {
			return false, err
		}
		if !commitment.ExpireTime.After(now) {
			return false, nil
		}
		if toolID != "" && commitment.ToolID != "" && commitment.ToolID != toolID {
			return false, nil
		}
		// Skip stale-denom commits (see note above).
		if commitment.RemainingAmount.Denom != params.CreditDenom {
			return false, nil
		}
		if commitment.RemainingAmount.Amount.IsPositive() {
			found = true
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

// ReleaseExpired removes commitments whose expiry passed (best-effort cleanup).
func (k *Keeper) ReleaseExpired(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return fmt.Errorf("reserve: block time must be set")
	}
	return k.releaseExpired(ctx, now, 0)
}

// EndBlocker releases a bounded batch of expired commitments each block. Without
// it, commitments on policies that see no further AllocateReserve traffic (and
// no manual MsgReleaseExpired) would never be swept and would accumulate
// indefinitely in the module indexes. Block time is always set during EndBlock;
// the zero-time guard simply makes the sweep a no-op at genesis rather than
// halting the chain.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	now := sdk.UnwrapSDKContext(ctx).BlockTime()
	if now.IsZero() {
		return nil
	}
	return k.releaseExpired(ctx, now, reserveEndBlockerSweepLimit)
}

func (k *Keeper) releaseExpired(ctx context.Context, now time.Time, limit int) error {
	// Iterate efficient secondary index: [Min, now]
	rng := new(collections.Range[collections.Pair[time.Time, string]]).
		EndExclusive(collections.Join(now.Add(time.Nanosecond), ""))

	type expiredCommitment struct {
		timestamp time.Time
		id        string
	}
	toRemove := []expiredCommitment{}
	err := k.state.CommitmentExpiry.Walk(ctx, rng, func(key collections.Pair[time.Time, string]) (bool, error) {
		toRemove = append(toRemove, expiredCommitment{
			timestamp: key.K1(),
			id:        key.K2(),
		})
		if limit > 0 && len(toRemove) >= limit {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, expired := range toRemove {
		id := expired.id
		bz, err := k.state.Commitments.Get(ctx, id)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				k.Logger().Warn("releaseExpired: pruned orphaned expiry index", "id", id)
				// Orphan-index cleanup: log but don't abort. A silent
				// failure here would leave the expiry index pointing at a
				// missing commitment row, so every subsequent sweep pass
				// would retry the same orphan and log the same warning.
				// Same bug class as ce940f443 (x/incentives cursor stall).
				if remErr := k.state.CommitmentExpiry.Remove(ctx, collections.Join(expired.timestamp, id)); remErr != nil {
					panic(fmt.Errorf("releaseExpired: failed to prune orphaned expiry index: %w", remErr))
				}
			} else {
				panic(fmt.Errorf("releaseExpired: failed to get commitment: %w", err))
			}
			continue
		}
		commitment, err := unmarshalCommitment(bz)
		if err != nil {
			k.Logger().Error("releaseExpired: failed to unmarshal commitment", "id", id, "error", err)
			if remErr := k.state.CommitmentExpiry.Remove(ctx, collections.Join(expired.timestamp, id)); remErr != nil {
				panic(fmt.Errorf("releaseExpired: failed to prune expiry index for unparseable commitment: %w", remErr))
			}
			continue
		}

		if err := k.state.Commitments.Remove(ctx, id); err != nil {
			return err
		}
		pair := collections.Join(commitment.PolicyID, id)
		if err := k.state.CommitmentsByPolicy.Remove(ctx, pair); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		ownerPair := collections.Join(commitment.Owner, id)
		if err := k.state.CommitmentsByOwner.Remove(ctx, ownerPair); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		if err := k.state.CommitmentExpiry.Remove(ctx, collections.Join(commitment.ExpireTime, id)); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return err
		}
	}
	return nil
}

// GetCommitment returns a stored commitment by id.
func (k *Keeper) GetCommitment(ctx context.Context, id string) (*types.ReserveCommitment, bool, error) {
	bz, err := k.state.Commitments.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	commitment, err := unmarshalCommitment(bz)
	if err != nil {
		return nil, false, err
	}
	return &commitment, true, nil
}

func normalizeCommitmentListLimit(limit uint64) (uint64, error) {
	if limit == 0 {
		return 0, fmt.Errorf("commitment list limit required")
	}
	if limit > maxCommitmentListLimit {
		return 0, fmt.Errorf("commitment list limit exceeds %d", maxCommitmentListLimit)
	}
	return limit, nil
}

// ListCommitmentsByPolicy returns commitments indexed under a policy ID.
func (k *Keeper) ListCommitmentsByPolicy(ctx context.Context, policyID string, limit uint64) ([]types.ReserveCommitment, error) {
	if err := validateReserveRequestIdentifier("policy id", policyID, true); err != nil {
		return nil, err
	}
	limit, err := normalizeCommitmentListLimit(limit)
	if err != nil {
		return nil, err
	}

	commitments := []types.ReserveCommitment{}
	rng := collections.NewPrefixedPairRange[string, string](policyID)
	err = k.state.CommitmentsByPolicy.Walk(ctx, rng, func(pair collections.Pair[string, string]) (bool, error) {
		bz, err := k.state.Commitments.Get(ctx, pair.K2())
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		commitment, err := unmarshalCommitment(bz)
		if err != nil {
			return false, err
		}
		commitments = append(commitments, commitment)
		if uint64(len(commitments)) >= limit {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return commitments, nil
}

// ListCommitmentsByOwner returns commitments indexed under an owner address.
func (k *Keeper) ListCommitmentsByOwner(ctx context.Context, owner string, limit uint64) ([]types.ReserveCommitment, error) {
	if _, err := sdk.AccAddressFromBech32(owner); err != nil {
		return nil, fmt.Errorf("invalid owner: %w", err)
	}
	limit, err := normalizeCommitmentListLimit(limit)
	if err != nil {
		return nil, err
	}

	commitments := []types.ReserveCommitment{}
	rng := collections.NewPrefixedPairRange[string, string](owner)
	err = k.state.CommitmentsByOwner.Walk(ctx, rng, func(pair collections.Pair[string, string]) (bool, error) {
		bz, err := k.state.Commitments.Get(ctx, pair.K2())
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		commitment, err := unmarshalCommitment(bz)
		if err != nil {
			return false, err
		}
		commitments = append(commitments, commitment)
		if uint64(len(commitments)) >= limit {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return commitments, nil
}

func preferReserveCommit(candidate, current types.ReserveCommitment) bool {
	if current.ID == "" {
		return true
	}
	if candidate.ExpireTime.Before(current.ExpireTime) {
		return true
	}
	if candidate.ExpireTime.After(current.ExpireTime) {
		return false
	}
	if candidate.DiscountBps > current.DiscountBps {
		return true
	}
	if candidate.DiscountBps < current.DiscountBps {
		return false
	}
	return candidate.TotalAmount.Amount.GT(current.TotalAmount.Amount)
}
