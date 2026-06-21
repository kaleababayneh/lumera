package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	insurancetypes "github.com/LumeraProtocol/lumera/x/insurance/types"
	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// Slash restitution destinations. String literals (not module-name imports)
// keep this from pulling auth/insurance keeper deps; the names are stable
// contracts established in app/app_config.go.
const (
	slashInsuranceModuleName = "insurance"
	slashTreasuryModuleName  = "fee_collector"
)

// Bond slice — publisher skin-in-the-game.
//
// This is the focused first bond slice: a tool publisher escrows a bond into the
// registry module account when they register a tool, and may top it up or
// withdraw the excess above the minimum. The bond is the trust primitive — it
// makes "who published this tool" a staked claim rather than a free assertion,
// and gives later slices (disputes, SLA slashing, restitution) something to
// slash. Those later behaviours (LockBond/SlashBond/EvaluateSLASlashing,
// restitution routing, badge refresh, per-category overrides) are intentionally
// NOT ported here; the BondRecord carries their fields zero-initialised so the
// state shape is forward-compatible.

// SetBondRecord stores a bond record keyed by its tool id.
func (k Keeper) SetBondRecord(ctx sdk.Context, bond *types.BondRecord) error {
	if bond == nil {
		return fmt.Errorf("SetBondRecord: bond record cannot be nil")
	}
	if bond.ToolId == "" {
		return fmt.Errorf("SetBondRecord: bond record missing tool id")
	}
	return k.bondRecords.Set(ctx, bond.ToolId, bond)
}

// GetBondRecord retrieves a bond record by tool id.
func (k Keeper) GetBondRecord(ctx sdk.Context, toolID string) (*types.BondRecord, bool) {
	bond, err := k.bondRecords.Get(ctx, toolID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(ctx).Error("failed to load bond record", "tool_id", toolID, "error", err)
		return nil, false
	}
	return bond, true
}

// RemoveBondRecord deletes a bond record.
func (k Keeper) RemoveBondRecord(ctx sdk.Context, toolID string) error {
	if err := k.bondRecords.Remove(ctx, toolID); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("remove bond record %s: %w", toolID, err)
	}
	return nil
}

// GetAllBonds returns all bond records (used by genesis export).
func (k Keeper) GetAllBonds(ctx sdk.Context) []*types.BondRecord {
	bonds := make([]*types.BondRecord, 0)
	if err := k.bondRecords.Walk(ctx, nil, func(_ string, bond *types.BondRecord) (bool, error) {
		if bond != nil {
			bonds = append(bonds, bond)
		}
		return false, nil
	}); err != nil {
		k.Logger(ctx).Error("failed to iterate bond records", "error", err)
	}
	return bonds
}

// CreateBond creates a new bond for a tool, or tops up an existing one. The
// coins are escrowed from the owner into the registry module account; the new
// total must satisfy the minimum requirement.
func (k Keeper) CreateBond(ctx sdk.Context, toolID string, owner sdk.AccAddress, amount sdk.Coins) error {
	tool, found := k.GetToolCard(ctx, toolID)
	if !found {
		return types.ErrToolNotFound.Wrapf("tool %s not found", toolID)
	}
	if tool.Owner != owner.String() {
		return types.ErrUnauthorized.Wrap("only the tool owner may manage the bond")
	}

	clean, err := sanitizeBondCoins(amount)
	if err != nil {
		return err
	}

	required, err := k.requiredBondAmount(ctx)
	if err != nil {
		return err
	}

	// Top-up path.
	if bond, exists := k.GetBondRecord(ctx, toolID); exists {
		newTotal := bond.BondedAmount.Add(clean...)
		minReq := maxBondRequirement(required, bond.MinimumRequired)
		if !meetsRequirement(newTotal, minReq) {
			return types.ErrInsufficientBond.Wrapf("bond for %s must remain >= %s", toolID, minReq.String())
		}
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, owner, types.ModuleName, clean); err != nil {
			return err
		}
		bond.BondedAmount = newTotal
		bond.MinimumRequired = minReq
		bond.LastUpdatedAt = ctx.BlockTime()
		if err := k.SetBondRecord(ctx, bond); err != nil {
			return err
		}
		k.emitBondEvent(ctx, types.EventTypeBondToppedUp, toolID, owner, clean, newTotal)
		return nil
	}

	// First-bond path.
	if !required.IsZero() && !meetsRequirement(clean, required) {
		return types.ErrInsufficientBond.Wrapf("initial bond for %s must be >= %s", toolID, required.String())
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, owner, types.ModuleName, clean); err != nil {
		return err
	}

	now := ctx.BlockTime()
	bond := &types.BondRecord{
		ToolId:                     toolID,
		Owner:                      owner.String(),
		BondedAmount:               clean,
		MinimumRequired:            required,
		LockedAmount:               sdk.NewCoins(),
		TotalSlashed:               sdk.NewCoins(),
		InsuranceContributions:     sdk.NewCoins(),
		InsurancePremiumMultiplier: types.DefaultInsuranceMultiplier,
		Status:                     types.BondStatusActive,
		BondedAt:                   now,
		LastUpdatedAt:              now,
	}
	if err := k.SetBondRecord(ctx, bond); err != nil {
		return err
	}
	k.emitBondEvent(ctx, types.EventTypeBondCreated, toolID, owner, clean, clean)
	return nil
}

// WithdrawBond returns part (or all) of a bond to its owner. The remaining
// bond must still satisfy the minimum requirement, so while a tool keeps a
// non-zero MinBondAmount its owner can only reclaim the excess above the
// minimum — full release is a later (delist) slice.
func (k Keeper) WithdrawBond(ctx sdk.Context, toolID string, owner sdk.AccAddress, amount sdk.Coins) error {
	bond, found := k.GetBondRecord(ctx, toolID)
	if !found {
		return types.ErrBondNotFound.Wrapf("bond for tool %s not found", toolID)
	}
	if bond.Owner != owner.String() {
		return types.ErrUnauthorized.Wrap("only the bond owner may withdraw")
	}
	if len(bond.PendingSlashes) > 0 {
		return types.ErrInvalidState.Wrap("pending slashes must resolve before withdrawing bond")
	}

	clean, err := sanitizeBondCoins(amount)
	if err != nil {
		return err
	}

	current := bond.BondedAmount
	locked := bond.LockedAmount
	if !current.IsAllGTE(clean) {
		return types.ErrInsufficientBond.Wrapf("withdrawal exceeds bonded amount for %s", toolID)
	}

	required, err := k.requiredBondAmount(ctx)
	if err != nil {
		return err
	}
	minReq := maxBondRequirement(required, bond.MinimumRequired)

	remaining := current.Sub(clean...)
	if !meetsRequirement(remaining, minReq) {
		return types.ErrInsufficientBond.Wrapf("withdrawal would violate minimum requirement (%s)", minReq.String())
	}

	if !current.IsAllGTE(locked) {
		return types.ErrInvalidState.Wrap("locked amount exceeds bonded amount")
	}
	available := current.Sub(locked...)
	if !available.IsAllGTE(clean) {
		return types.ErrInvalidState.Wrap("insufficient available bond (some amount is locked)")
	}

	if remaining.IsZero() {
		bond.Status = types.BondStatusWithdrawn
		if err := k.RemoveBondRecord(ctx, toolID); err != nil {
			return err
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, clean); err != nil {
			return err
		}
		k.emitBondEvent(ctx, types.EventTypeBondWithdrawn, toolID, owner, clean, sdk.NewCoins())
		return nil
	}

	bond.BondedAmount = remaining
	bond.MinimumRequired = minReq
	bond.LastUpdatedAt = ctx.BlockTime()
	if err := k.SetBondRecord(ctx, bond); err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, clean); err != nil {
		return err
	}
	k.emitBondEvent(ctx, types.EventTypeBondWithdrawn, toolID, owner, clean, remaining)
	return nil
}

// requiredBondAmount returns the flat minimum bond required by params, in the
// bond denom. (Per-category overrides are a later slice.)
func (k Keeper) requiredBondAmount(ctx sdk.Context) (sdk.Coins, error) {
	minStr := k.GetParams(ctx).MinBondAmount
	if minStr == "" {
		return sdk.NewCoins(), nil
	}
	amt, ok := sdkmath.NewIntFromString(minStr)
	if !ok {
		return nil, types.ErrInvalidAmount.Wrapf("invalid min bond amount: %s", minStr)
	}
	if amt.IsNegative() {
		return nil, types.ErrInvalidAmount.Wrap("min bond amount cannot be negative")
	}
	if amt.IsZero() {
		return sdk.NewCoins(), nil
	}
	return sdk.NewCoins(sdk.NewCoin(types.BondDenom, amt)), nil
}

// sanitizeBondCoins validates the bond amount: non-zero, valid, and denominated
// solely in the bond denom (single-denom keeps the requirement math simple).
func sanitizeBondCoins(amount sdk.Coins) (sdk.Coins, error) {
	if amount.IsZero() {
		return nil, types.ErrInvalidAmount.Wrap("bond amount cannot be zero")
	}
	if err := amount.Validate(); err != nil {
		return nil, types.ErrInvalidAmount.Wrap(err.Error())
	}
	for _, c := range amount {
		if c.Denom != types.BondDenom {
			return nil, types.ErrInvalidAmount.Wrapf("bond must be denominated in %s, got %s", types.BondDenom, c.Denom)
		}
	}
	return amount, nil
}

// bumpToolStats records per-tool usage on the bond record (consumed by the
// incentives reputation engine): successful Proof-of-Service receipts raise
// reputation; upheld disputes erode it. No-op if the tool has no bond.
func (k Keeper) bumpToolStats(ctx sdk.Context, toolID string, successDelta, disputeDelta uint64) {
	bond, found := k.GetBondRecord(ctx, toolID)
	if !found {
		return
	}
	bond.SuccessfulCalls += successDelta
	bond.DisputeCount += disputeDelta
	bond.LastUpdatedAt = ctx.BlockTime()
	if err := k.SetBondRecord(ctx, bond); err != nil {
		k.Logger(ctx).Error("bump tool stats failed", "tool", toolID, "error", err)
	}
}

// GetToolUsage returns a tool's cumulative (successful receipts, upheld disputes)
// — the on-chain signal the incentives module folds into reputation scoring.
func (k Keeper) GetToolUsage(ctx context.Context, toolID string) (uint64, uint64, error) {
	bond, found := k.GetBondRecord(sdk.UnwrapSDKContext(ctx), toolID)
	if !found {
		return 0, 0, nil
	}
	return bond.SuccessfulCalls, bond.DisputeCount, nil
}

// maxBondRequirement returns the larger of two single-denom requirements.
func maxBondRequirement(a, b sdk.Coins) sdk.Coins {
	amountA := a.AmountOf(types.BondDenom)
	amountB := b.AmountOf(types.BondDenom)
	if amountB.GT(amountA) {
		amountA = amountB
	}
	if amountA.IsZero() {
		return sdk.NewCoins()
	}
	return sdk.NewCoins(sdk.NewCoin(types.BondDenom, amountA))
}

// meetsRequirement reports whether total covers the required bond-denom amount.
func meetsRequirement(total sdk.Coins, required sdk.Coins) bool {
	requiredAmt := required.AmountOf(types.BondDenom)
	if requiredAmt.IsZero() {
		return true
	}
	return total.AmountOf(types.BondDenom).GTE(requiredAmt)
}

// emitBondEvent emits a typed bond audit event.
func (k Keeper) emitBondEvent(ctx sdk.Context, evtType, toolID string, owner sdk.AccAddress, delta, total sdk.Coins) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		evtType,
		sdk.NewAttribute("tool_id", toolID),
		sdk.NewAttribute("owner", owner.String()),
		sdk.NewAttribute("amount", delta.String()),
		sdk.NewAttribute("bonded_total", total.String()),
	))
}

// --- dispute primitives (Step 3 ⊗ Step 4): lock during a dispute, then either
// unlock (challenge rejected) or slash (challenge upheld). ---

// LockBond reserves part of a tool's available bond so it cannot be withdrawn
// while a dispute is open. Locked funds remain bonded until unlocked or slashed.
func (k Keeper) LockBond(ctx sdk.Context, toolID string, amount sdk.Coins) error {
	bond, found := k.GetBondRecord(ctx, toolID)
	if !found {
		return types.ErrBondNotFound.Wrapf("bond for tool %s not found", toolID)
	}
	clean, err := sanitizeBondCoins(amount)
	if err != nil {
		return err
	}
	current, locked := bond.BondedAmount, bond.LockedAmount
	if !current.IsAllGTE(locked) {
		return types.ErrInvalidState.Wrap("locked amount exceeds bonded amount")
	}
	if !current.Sub(locked...).IsAllGTE(clean) {
		return types.ErrInsufficientBond.Wrapf("insufficient available bond to lock for %s", toolID)
	}
	bond.LockedAmount = locked.Add(clean...)
	bond.LastUpdatedAt = ctx.BlockTime()
	if err := k.SetBondRecord(ctx, bond); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeBondLocked,
		sdk.NewAttribute("tool_id", toolID),
		sdk.NewAttribute("amount", clean.String()),
	))
	return nil
}

// UnlockBond releases previously locked bond back to the available balance.
func (k Keeper) UnlockBond(ctx sdk.Context, toolID string, amount sdk.Coins) error {
	bond, found := k.GetBondRecord(ctx, toolID)
	if !found {
		return types.ErrBondNotFound.Wrapf("bond for tool %s not found", toolID)
	}
	clean, err := sanitizeBondCoins(amount)
	if err != nil {
		return err
	}
	if !bond.LockedAmount.IsAllGTE(clean) {
		return types.ErrInvalidState.Wrap("insufficient locked amount to unlock")
	}
	bond.LockedAmount = bond.LockedAmount.Sub(clean...)
	bond.LastUpdatedAt = ctx.BlockTime()
	if err := k.SetBondRecord(ctx, bond); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeBondUnlocked,
		sdk.NewAttribute("tool_id", toolID),
		sdk.NewAttribute("amount", clean.String()),
	))
	return nil
}

// SlashBond removes coins from a tool's bond and routes them per the restitution
// policy: 5% burned (immutable), 10% to treasury, 85% to the insurance module
// (reserve replenishment + impacted-user credit). Returns the amount slashed
// (capped by the available, non-locked bond). Callers that locked the at-risk
// amount during a dispute should UnlockBond it first so it becomes slashable.
func (k Keeper) SlashBond(ctx sdk.Context, toolID string, amount sdk.Coins, reason, evidence string) (sdk.Coins, error) {
	bond, found := k.GetBondRecord(ctx, toolID)
	if !found {
		return nil, types.ErrBondNotFound.Wrapf("bond for tool %s not found", toolID)
	}
	clean, err := sanitizeBondCoins(amount)
	if err != nil {
		return nil, err
	}
	current, locked := bond.BondedAmount, bond.LockedAmount
	available := current
	if current.IsAllGTE(locked) {
		available = current.Sub(locked...)
	}
	if !available.IsAllGTE(clean) {
		clean = available // cap to what is actually slashable
	}
	if clean.IsZero() {
		return sdk.NewCoins(), nil
	}

	bond.BondedAmount = current.Sub(clean...)
	bond.TotalSlashed = bond.TotalSlashed.Add(clean...)
	bond.LastSlashEpoch = ctx.BlockHeight()
	bond.LastUpdatedAt = ctx.BlockTime()
	if err := k.SetBondRecord(ctx, bond); err != nil {
		return nil, err
	}

	burn, insurance, treasury := splitSlashCoins(clean)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSlash,
		sdk.NewAttribute("tool_id", toolID),
		sdk.NewAttribute("amount", clean.String()),
		sdk.NewAttribute("reason", reason),
		sdk.NewAttribute("evidence", evidence),
		sdk.NewAttribute("burned", burn.String()),
		sdk.NewAttribute("insurance", insurance.String()),
		sdk.NewAttribute("treasury", treasury.String()),
	))

	if !burn.IsZero() {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, burn); err != nil {
			return nil, err
		}
	}
	if !insurance.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, slashInsuranceModuleName, insurance); err != nil {
			return nil, err
		}
	}
	if !treasury.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, slashTreasuryModuleName, treasury); err != nil {
			return nil, err
		}
	}
	return clean, nil
}

// splitSlashCoins partitions a slash into (burn 5%, insurance 85%, treasury 10%)
// using the immutable restitution bps from x/insurance. Insurance absorbs the
// per-denom rounding residual (it carries the reserve + user-credit share), so
// the immutable 5% burn is never inflated by rounding.
func splitSlashCoins(amount sdk.Coins) (burn, insurance, treasury sdk.Coins) {
	burn, insurance, treasury = sdk.NewCoins(), sdk.NewCoins(), sdk.NewCoins()
	total := int64(insurancetypes.RestitutionTotalBps)
	for _, c := range amount {
		if !c.Amount.IsPositive() {
			continue
		}
		b := c.Amount.MulRaw(int64(insurancetypes.RestitutionBurnBps)).QuoRaw(total)
		t := c.Amount.MulRaw(int64(insurancetypes.RestitutionTreasuryBps)).QuoRaw(total)
		ins := c.Amount.Sub(b).Sub(t) // reserve (25%) + user-credit (60%) + residual
		if b.IsPositive() {
			burn = burn.Add(sdk.NewCoin(c.Denom, b))
		}
		if ins.IsPositive() {
			insurance = insurance.Add(sdk.NewCoin(c.Denom, ins))
		}
		if t.IsPositive() {
			treasury = treasury.Add(sdk.NewCoin(c.Denom, t))
		}
	}
	return burn, insurance, treasury
}
