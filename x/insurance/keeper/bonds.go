//go:build cosmos && cosmos_full && todo_bonds

package keeper

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
)

// SLABond represents a service level agreement bond posted by a tool publisher
type SLABond struct {
	BondID      string
	ToolID      string
	PublisherID string
	Amount      sdk.Coins
	SLOTargets  SLOTargets
	PostedAt    time.Time
	ExpiresAt   time.Time
	Status      string // "active", "slashed", "withdrawn"
	SlashRecord []SlashEvent
}

// SLOTargets defines the service level objectives for a tool.
// All percentage/ratio fields use basis points (1/10000) for deterministic integer arithmetic.
type SLOTargets struct {
	MaxLatencyMs       uint32 // P95 latency target in milliseconds
	MinAvailabilityBps uint32 // Minimum uptime in basis points (e.g., 9990 for 99.9%)
	MaxErrorRateBps    uint32 // Maximum error rate in basis points (e.g., 100 for 1%)
	MaxCostVarBps      uint32 // Maximum cost variance from quote in bps (e.g., 1000 for 10%)
}

// SlashEvent records when a bond was slashed for SLA violation
type SlashEvent struct {
	Timestamp     time.Time
	ViolationType string // "latency", "availability", "error_rate", "cost_overrun"
	Amount        sdk.Coins
	ClaimID       string
	Evidence      string
}

// PostBond allows a publisher to post an SLA bond for their tool
func (k Keeper) PostBond(ctx context.Context, toolID, publisherID string, amount sdk.Coins, targets SLOTargets) (*SLABond, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := k.storeService.OpenKVStore(ctx)

	// Validate minimum bond amount (e.g., 1000 LAC)
	minBond := sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1000000000))) // 1000 LAC
	if amount.IsAllLT(minBond) {
		return nil, fmt.Errorf("bond amount below minimum: %s < %s", amount, minBond)
	}

	// Validate the publisher address is a parseable bech32 before
	// accepting the bond. We discard the decoded AccAddress because
	// the actual token movement is deferred (see below); the parse
	// itself is the validation side-effect we want to keep so a
	// malformed publisher ID gets rejected at the entry point
	// rather than by a downstream consumer reading the stored bond.
	if _, err := sdk.AccAddressFromBech32(publisherID); err != nil {
		return nil, fmt.Errorf("invalid publisher address: %w", err)
	}

	// DEFERRED BANK WIRING (gated by //go:build todo_bonds at the
	// top of this file — the entire bonds.go is behind that tag
	// precisely because this call is not yet wired):
	//
	//   k.bankKeeper.SendCoinsFromAccountToModule(
	//       ctx, publisherAddr, types.ModuleName, amount)
	//
	// Adding bankKeeper to the insurance keeper is the concrete
	// follow-up. Without it, bonds are record-only and the amount
	// stored on-chain is not backed by an actual treasury transfer
	// — the reason this file stays behind the todo_bonds build tag
	// and is not compiled into production builds today.

	// Create bond record
	bondID := fmt.Sprintf("bond-%s-%d", toolID, sdkCtx.BlockTime().Unix())
	bond := &SLABond{
		BondID:      bondID,
		ToolID:      toolID,
		PublisherID: publisherID,
		Amount:      amount,
		SLOTargets:  targets,
		PostedAt:    sdkCtx.BlockTime(),
		ExpiresAt:   sdkCtx.BlockTime().Add(365 * 24 * time.Hour), // 1 year default
		Status:      "active",
		SlashRecord: []SlashEvent{},
	}

	// Save bond
	bondKey := append(types.BondPrefix, []byte(bondID)...)
	bz, err := marshalBond(bond)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bond: %w", err)
	}

	if err := store.Set(bondKey, bz); err != nil {
		return nil, fmt.Errorf("failed to save bond: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bond_posted",
			sdk.NewAttribute("bond_id", bondID),
			sdk.NewAttribute("tool_id", toolID),
			sdk.NewAttribute("publisher", publisherID),
			sdk.NewAttribute("amount", amount.String()),
		),
	)

	return bond, nil
}

// SlashBond slashes a bond for SLA violation
func (k Keeper) SlashBond(ctx context.Context, bondID string, violationType string, slashAmount sdk.Coins, claimID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := k.storeService.OpenKVStore(ctx)

	// Get bond
	bondKey := append(types.BondPrefix, []byte(bondID)...)
	bz, err := store.Get(bondKey)
	if err != nil || bz == nil {
		return fmt.Errorf("bond not found: %s", bondID)
	}

	var bond SLABond
	if err := unmarshalBond(bz, &bond); err != nil {
		return fmt.Errorf("failed to unmarshal bond: %w", err)
	}

	if bond.Status != "active" {
		return fmt.Errorf("bond not active: %s", bond.Status)
	}

	// Check if slash amount exceeds remaining bond
	if slashAmount.IsAnyGT(bond.Amount) {
		slashAmount = bond.Amount
	}

	// Record slash event
	slash := SlashEvent{
		Timestamp:     sdkCtx.BlockTime(),
		ViolationType: violationType,
		Amount:        slashAmount,
		ClaimID:       claimID,
		Evidence:      fmt.Sprintf("SLA violation: %s", violationType),
	}
	bond.SlashRecord = append(bond.SlashRecord, slash)

	// Reduce bond amount
	bond.Amount = bond.Amount.Sub(slashAmount...)

	// If bond depleted, mark as slashed
	if bond.Amount.IsZero() {
		bond.Status = "slashed"
	}

	// Save updated bond
	bz, err = marshalBond(&bond)
	if err != nil {
		return fmt.Errorf("failed to marshal updated bond: %w", err)
	}

	if err := store.Set(bondKey, bz); err != nil {
		return fmt.Errorf("failed to save updated bond: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bond_slashed",
			sdk.NewAttribute("bond_id", bondID),
			sdk.NewAttribute("violation_type", violationType),
			sdk.NewAttribute("amount", slashAmount.String()),
			sdk.NewAttribute("claim_id", claimID),
		),
	)

	return nil
}

// WithdrawBond allows a publisher to withdraw their bond (if no active claims)
func (k Keeper) WithdrawBond(ctx context.Context, bondID string, publisherID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := k.storeService.OpenKVStore(ctx)

	// Get bond
	bondKey := append(types.BondPrefix, []byte(bondID)...)
	bz, err := store.Get(bondKey)
	if err != nil || bz == nil {
		return fmt.Errorf("bond not found: %s", bondID)
	}

	var bond SLABond
	if err := unmarshalBond(bz, &bond); err != nil {
		return fmt.Errorf("failed to unmarshal bond: %w", err)
	}

	// Verify ownership
	if bond.PublisherID != publisherID {
		return fmt.Errorf("unauthorized: bond belongs to %s", bond.PublisherID)
	}

	if bond.Status != "active" {
		return fmt.Errorf("bond not active: %s", bond.Status)
	}

	// Check for active claims against this bond
	if hasActiveClaims, err := k.HasActiveClaims(ctx, bond.ToolID); err != nil {
		return fmt.Errorf("failed to check active claims: %w", err)
	} else if hasActiveClaims {
		return fmt.Errorf("cannot withdraw bond with active claims")
	}

	// DEFERRED: return remaining bond to publisher.
	//
	// Mirrors the PostBond side deferred at line ~66: this entire
	// file is gated by //go:build todo_bonds precisely because
	// bond-related token movement is not yet wired. Adding a
	// bankKeeper to the insurance keeper will unlock both ends
	// (PostBond SendCoinsFromAccountToModule + this WithdrawBond
	// SendCoinsFromModuleToAccount) and the file can drop the
	// todo_bonds tag at the same time.
	//
	// Until then WithdrawBond is record-only — it flips the bond
	// status to 'withdrawn' on-chain but does not move any treasury
	// balance. Do NOT remove this note by flipping the status to
	// 'withdrawn' above without also wiring the transfer; doing so
	// would silently tell callers the bond returned when no funds
	// actually did.

	// Update bond status
	bond.Status = "withdrawn"

	// Save updated bond
	bz, err = marshalBond(&bond)
	if err != nil {
		return fmt.Errorf("failed to marshal updated bond: %w", err)
	}

	if err := store.Set(bondKey, bz); err != nil {
		return fmt.Errorf("failed to save updated bond: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"bond_withdrawn",
			sdk.NewAttribute("bond_id", bondID),
			sdk.NewAttribute("publisher", publisherID),
			sdk.NewAttribute("amount", bond.Amount.String()),
		),
	)

	return nil
}

// GetBond retrieves a bond by ID
func (k Keeper) GetBond(ctx context.Context, bondID string) (*SLABond, error) {
	store := k.storeService.OpenKVStore(ctx)

	bondKey := append(types.BondPrefix, []byte(bondID)...)
	bz, err := store.Get(bondKey)
	if err != nil || bz == nil {
		return nil, fmt.Errorf("bond not found: %s", bondID)
	}

	var bond SLABond
	if err := unmarshalBond(bz, &bond); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bond: %w", err)
	}

	return &bond, nil
}

// GetToolBonds returns all active bonds for a tool
func (k Keeper) GetToolBonds(ctx context.Context, toolID string) ([]*SLABond, error) {
	store := k.storeService.OpenKVStore(ctx)

	var bonds []*SLABond
	iterator, err := store.Iterator(types.BondPrefix, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var bond SLABond
		if err := unmarshalBond(iterator.Value(), &bond); err != nil {
			continue
		}

		if bond.ToolID == toolID && bond.Status == "active" {
			bonds = append(bonds, &bond)
		}
	}

	return bonds, nil
}

// HasActiveClaims checks if there are any active claims against a tool.
//
// Always returns (false, nil) today — the entire bonds.go file is
// gated by //go:build todo_bonds and is NOT compiled into
// production. When that tag flips (see the DEFERRED BANK WIRING
// note at PostBond and WithdrawBond above), this function MUST be
// wired to query the claims index by ToolID and return true if any
// claim is in the ACTIVE / PENDING states. Leaving it hardcoded to
// false in a compiled-in world would silently permit bond
// withdrawal while claimants are still owed a payout — the exact
// silent-success anti-pattern that the fail-closed gate
// (scripts/gates/fail_closed.sh) exists to catch.
func (k Keeper) HasActiveClaims(ctx context.Context, toolID string) (bool, error) {
	return false, nil
}

// CalculateSlashAmount determines the slash amount based on violation severity.
// severity is expressed as a Decimal multiplier (e.g., 1.5 means 1.5x base rate).
func (k Keeper) CalculateSlashAmount(ctx context.Context, bond *SLABond, violationType string, severity decimal.Decimal) sdk.Coins {
	// Base slash rate in basis points by violation type.
	var baseSlashBps int64
	switch violationType {
	case "latency":
		baseSlashBps = 500 // 5%
	case "availability":
		baseSlashBps = 1000 // 10%
	case "error_rate":
		baseSlashBps = 800 // 8%
	case "cost_overrun":
		baseSlashBps = 1500 // 15%
	default:
		baseSlashBps = 500 // 5%
	}

	// Convert base rate from bps to decimal fraction and multiply by severity.
	bpsDenom := decimal.NewFromInt(10000)
	slashRate := decimal.NewFromInt(baseSlashBps).Mul(severity).Div(bpsDenom)
	maxRate := decimal.NewFromInt(5000).Div(bpsDenom) // 50% cap
	if slashRate.GreaterThan(maxRate) {
		slashRate = maxRate
	}

	// Calculate slash amount. Round the per-coin product up rather than
	// truncating: with truncation, any bond small enough that
	// (amount * slashRate) < 1 would silently slash to zero, letting
	// micro-bonds escape penalty entirely. Ceiling enforces a minimum
	// 1-unit slash whenever both the bond and slash rate are positive.
	// Cap each per-coin slash to the bond's own amount so we never slash
	// more than the publisher actually posted.
	slashCoins := sdk.NewCoins()
	for _, coin := range bond.Amount {
		// Use string conversion to avoid silent int64 overflow on large amounts.
		amtDec, err := decimal.NewFromString(coin.Amount.String())
		if err != nil {
			continue
		}
		if !amtDec.IsPositive() || !slashRate.IsPositive() {
			continue
		}
		slashDec := amtDec.Mul(slashRate).Ceil()
		// Cap per-coin slash to the bond's own amount to avoid slashing more
		// than was posted. Compare in the decimal domain so amounts larger
		// than int64 do not require an unsafe coin.Amount.Int64() call.
		if slashDec.GreaterThan(amtDec) {
			slashDec = amtDec
		}
		slashInt, ok := sdkmath.NewIntFromString(slashDec.String())
		if !ok || !slashInt.IsPositive() {
			continue
		}
		slashCoins = slashCoins.Add(sdk.NewCoin(coin.Denom, slashInt))
	}

	return slashCoins
}

func marshalBond(bond *SLABond) ([]byte, error) {
	return json.Marshal(bond)
}

func unmarshalBond(bz []byte, bond *SLABond) error {
	return json.Unmarshal(bz, bond)
}
