
package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	creditsTypes "github.com/LumeraProtocol/lumera/x/credits/types"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// Keeper provides the insurance module state management with collections-based storage
type Keeper struct {
	cdc           codec.BinaryCodec
	storeService  store.KVStoreService
	authority     string
	bankKeeper    types.BankKeeper
	accountKeeper types.AccountKeeper
	state         State // Collections-based state management
}

// NewKeeper wires the insurance keeper with bank keeper for managing pool funds
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	authority string,
) Keeper {
	if bankKeeper == nil {
		panic("insurance keeper requires bank keeper")
	}
	if accountKeeper == nil {
		panic("insurance keeper requires account keeper")
	}

	// Initialize collections-based state
	state := NewState(cdc, storeService)

	return Keeper{
		cdc:           cdc,
		storeService:  storeService,
		authority:     authority,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
		state:         state,
	}
}

// Logger returns a module-prefixed logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", types.ModuleName)
}

// Authority returns the address with parameter update rights.
func (k Keeper) Authority() string {
	return k.authority
}

// AccountKeeper returns the account keeper for simulation purposes.
func (k Keeper) AccountKeeper() types.AccountKeeper {
	return k.accountKeeper
}

// BankKeeper returns the bank keeper for simulation purposes.
func (k Keeper) BankKeeper() types.BankKeeper {
	return k.bankKeeper
}

// InitGenesis initialises state from genesis. The wire format is defined in
// x/insurance/types and will evolve alongside keeper implementation.
func (k Keeper) InitGenesis(ctx sdk.Context, genesis *types.GenesisState) {
	if genesis == nil {
		genesis = types.DefaultGenesis()
	}
	normalizeGenesisDefaults(genesis)
	if err := genesis.Validate(); err != nil {
		panic(fmt.Errorf("failed to validate insurance genesis: %w", err))
	}

	// Set parameters
	params := genesis.Params
	if params == nil {
		params = types.DefaultParams()
	}
	if err := params.ValidateBasic(); err != nil {
		panic(fmt.Errorf("failed to validate insurance genesis params: %w", err))
	}
	if err := k.state.Params.Set(ctx, params); err != nil {
		panic(fmt.Errorf("failed to import insurance params: %w", err))
	}

	// Initialize pool state if provided
	if genesis.Pool != nil {
		if err := k.state.PoolBalance.Set(ctx, genesis.Pool); err != nil {
			panic(fmt.Errorf("failed to import insurance pool balance: %w", err))
		}
	}

	// Initialize pool metrics if provided
	if genesis.Metrics != nil {
		if err := k.state.PoolMetrics.Set(ctx, genesis.Metrics); err != nil {
			panic(fmt.Errorf("failed to import insurance pool metrics: %w", err))
		}
	}

	// Import claims
	for _, claim := range genesis.Claims {
		if claim == nil {
			continue
		}
		if err := k.state.ClaimsByReceipt.Set(ctx, claim.Id, claim); err != nil {
			panic(fmt.Errorf("failed to import claim %s: %w", claim.Id, err))
		}
		// Rebuild receipt ownership from claims
		if claim.ReceiptId != "" && claim.ClaimantId != "" {
			if err := k.state.ReceiptOwners.Set(ctx, claim.ReceiptId, claim.ClaimantId); err != nil {
				panic(fmt.Errorf("failed to set receipt owner for %s: %w", claim.ReceiptId, err))
			}
		}
	}

	// Restore claim sequence
	if genesis.ClaimSequence > 0 {
		if err := k.state.ClaimCounter.Set(ctx, genesis.ClaimSequence); err != nil {
			panic(fmt.Errorf("failed to restore claim sequence: %w", err))
		}
	}

	// Import contributions
	for _, contrib := range genesis.Contributions {
		if contrib == nil {
			continue
		}
		if err := k.state.ContribByReceipt.Set(ctx, contrib.Id, contrib); err != nil {
			panic(fmt.Errorf("failed to import contribution %s: %w", contrib.Id, err))
		}
	}

	// Set contrib counter from imported contributions
	if len(genesis.Contributions) > 0 {
		if err := k.state.ContribCounter.Set(ctx, uint64(len(genesis.Contributions))); err != nil {
			panic(fmt.Errorf("failed to restore contrib counter: %w", err))
		}
	}

	// Import publisher risks
	for _, risk := range genesis.PublisherRisks {
		if risk == nil {
			continue
		}
		key := fmt.Sprintf("%d:%s:%s", len(risk.PublisherId), risk.PublisherId, risk.ToolId)
		if err := k.state.PublisherRisks.Set(ctx, key, risk); err != nil {
			panic(fmt.Errorf("failed to import publisher risk %s: %w", key, err))
		}
	}

	// Import payouts
	for _, payout := range genesis.Payouts {
		if payout == nil {
			continue
		}
		if err := k.state.PayoutsByClaimID.Set(ctx, payout.Id, payout); err != nil {
			panic(fmt.Errorf("failed to import payout %s: %w", payout.Id, err))
		}
	}

	// Restore payout sequence
	if genesis.PayoutSequence > 0 {
		if err := k.state.PayoutCounter.Set(ctx, genesis.PayoutSequence); err != nil {
			panic(fmt.Errorf("failed to restore payout sequence: %w", err))
		}
	}
}

func normalizeGenesisDefaults(genesis *types.GenesisState) {
	if genesis.Params == nil {
		genesis.Params = types.DefaultParams()
	}
	if genesis.ClaimSequence == 0 && len(genesis.Claims) == 0 {
		genesis.ClaimSequence = 1
	}
	if genesis.PayoutSequence == 0 && len(genesis.Payouts) == 0 {
		genesis.PayoutSequence = 1
	}
}

// ExportGenesis exports the module state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	genesis := types.DefaultGenesis()

	// Export parameters
	params, err := k.state.Params.Get(ctx)
	if err == nil {
		genesis.Params = params
	}

	// Export pool state
	poolState, err := k.state.PoolBalance.Get(ctx)
	if err == nil {
		genesis.Pool = poolState
	}

	// Export pool metrics
	poolMetrics, err := k.state.PoolMetrics.Get(ctx)
	if err == nil {
		genesis.Metrics = poolMetrics
	}

	// Export claims
	if err := k.state.ClaimsByReceipt.Walk(ctx, nil, func(_ string, claim *types.Claim) (bool, error) {
		if claim != nil {
			genesis.Claims = append(genesis.Claims, claim)
		}
		return false, nil
	}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk claims: %w", err))
	}

	// Export claim sequence
	claimSeq, err := k.state.ClaimCounter.Peek(ctx)
	if err == nil {
		genesis.ClaimSequence = claimSeq
	}

	// Export contributions
	if err := k.state.ContribByReceipt.Walk(ctx, nil, func(_ string, contrib *types.Contribution) (bool, error) {
		if contrib != nil {
			genesis.Contributions = append(genesis.Contributions, contrib)
		}
		return false, nil
	}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk contributions: %w", err))
	}

	// Export publisher risks
	if err := k.state.PublisherRisks.Walk(ctx, nil, func(_ string, risk *types.PublisherRisk) (bool, error) {
		if risk != nil {
			genesis.PublisherRisks = append(genesis.PublisherRisks, risk)
		}
		return false, nil
	}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk publisher risks: %w", err))
	}

	// Export payouts
	if err := k.state.PayoutsByClaimID.Walk(ctx, nil, func(_ string, payout *types.Payout) (bool, error) {
		if payout != nil {
			genesis.Payouts = append(genesis.Payouts, payout)
		}
		return false, nil
	}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk payouts: %w", err))
	}

	// Export payout sequence
	payoutSeq, err := k.state.PayoutCounter.Peek(ctx)
	if err == nil {
		genesis.PayoutSequence = payoutSeq
	}

	return genesis
}

// BeginBlocker executes insurance-specific begin blocker hooks.
func (k Keeper) BeginBlocker(ctx sdk.Context) error {
	_ = ctx
	return nil
}

// EndBlocker executes insurance-specific end blocker hooks.
// It auto-processes pending claims that have passed the review window with bounded gas.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	params := k.GetParams(ctx)
	if params == nil || !params.Enabled {
		return nil
	}

	// Auto-process pending claims past review window
	if err := k.processExpiredClaims(ctx, params); err != nil {
		k.Logger(ctx).Error("failed to process expired claims", "error", err)
		// Continue rather than fail the block
	}

	// Update pool metrics and emit telemetry
	if err := k.updatePoolMetricsEndBlock(ctx); err != nil {
		k.Logger(ctx).Error("failed to update pool metrics", "error", err)
	}

	// Emit Prometheus metrics for monitoring
	k.EmitPoolMetrics(ctx)

	return nil
}

// processExpiredClaims auto-approves claims past the review window up to MaxClaimsPerBlock.
// Claims under AutoApproveThreshold are auto-approved; larger claims are marked for manual review.
func (k Keeper) processExpiredClaims(ctx sdk.Context, params *types.Params) error {
	reviewPeriod, err := types.ClaimWindowDuration(params.ClaimWindowSeconds)
	if err != nil {
		return fmt.Errorf("invalid claim review window: %w", err)
	}
	cutoffTime := ctx.BlockTime().Add(-reviewPeriod)
	maxClaimsPerBlock := params.MaxClaimsPerBlock
	if maxClaimsPerBlock == 0 {
		maxClaimsPerBlock = 100 // Default
	}

	autoApproveThreshold := insuranceParamDecimalOrDefault(params.AutoApproveThreshold, decimal.NewFromInt(10))

	var processedCount uint32
	var claimsToProcess []string

	// Walk pending claims and collect those past review window using efficient index.
	// The index key set is Pair[Pair[string, time.Time], string] (refKey, primaryKey).
	// Prefix on PENDING status; filter by cutoff time in the walk callback since the
	// index is ordered by (status, createdAt) — we stop once we pass the cutoff.
	pendingPrefix := collections.PairPrefix[string, time.Time](types.ClaimStatus_CLAIM_STATUS_PENDING.String())
	rng := collections.NewPrefixedPairRange[collections.Pair[string, time.Time], string](pendingPrefix)

	if err := k.state.ClaimsByReceipt.Indexes.Status.Walk(ctx, rng,
		func(refKey collections.Pair[string, time.Time], primaryKey string) (stop bool, err error) {
			if processedCount >= maxClaimsPerBlock {
				return true, nil // Stop at limit
			}
			// Index is ordered by time within status; stop once past cutoff
			if refKey.K2().After(cutoffTime) {
				return true, nil
			}
			claimsToProcess = append(claimsToProcess, primaryKey)
			processedCount++
			return false, nil
		}); err != nil {
		return fmt.Errorf("failed to walk pending claims: %w", err)
	}

	// Process collected claims
	for _, claimID := range claimsToProcess {
		// Use CacheContext to ensure atomicity of claim processing
		cacheCtx, write := ctx.CacheContext()

		claim, err := k.state.ClaimsByReceipt.Get(cacheCtx, claimID)
		if err != nil {
			// The claimID was surfaced from the status index walk just
			// above, so the index points at a primary-store row that
			// either went missing or has a transient load fault. The
			// index-vs-primary-row divergence is never safe to ignore
			// silently — operators need visibility so they can
			// investigate whether the claim was orphaned (index needs
			// pruning) or the store is returning transient failures
			// (no-op, retry next block). Same bug class as ce940f443
			// (cursor stall) and 5cdeb42a9 (sweep-loop error swallow).
			k.Logger(ctx).Error("processExpiredClaims: pending claim found in status index but not in primary store — orphan index row or transient fault; skipping this pass",
				"claim_id", claimID, "error", err)
			continue
		}

		// Determine if auto-approve or mark for manual review
		claimedAmt := decimal.Zero
		if !claim.ClaimedAmount.Amount.IsNil() {
			claimedAmt = decimal.NewFromBigInt(claim.ClaimedAmount.Amount.BigInt(), 0)
		}

		if claimedAmt.LessThanOrEqual(autoApproveThreshold) {
			// Check leverage: Auto-approve only if Claim <= Contribution * 4
			// This prevents micro-transaction fraud where attackers profit from
			// the spread between insurance cost and auto-approve threshold.
			coverage, err := k.getCoverageForReceipt(cacheCtx, claim.ReceiptId)
			if err != nil {
				k.Logger(ctx).Error("failed to get coverage", "claim_id", claimID, "error", err)
				continue
			}

			// Resolve the coverage denom to match against. The
			// insurance module assumes SINGLE-DENOM claims: the
			// coverage map is queried for exactly one denom per
			// claim. This matches the credits-pipeline single-denom
			// settlement assumption (see x/credits/keeper/keeper.go
			// rebate-branch note at line ~1043). A multi-denom claim
			// would require iterating denoms and aggregating matches;
			// if that becomes necessary, replace this single
			// coverage.AmountOf(denom) lookup with a per-denom loop
			// over claim.ClaimedAmount and sum the matches.
			denom := "ulac"
			if claim.ClaimedAmount.Denom != "" {
				denom = claim.ClaimedAmount.Denom
			}

			contribAmt := coverage.AmountOf(denom)
			contribDec := decimal.NewFromBigInt(contribAmt.BigInt(), 0)

			// If contribution is zero for the claim denom, always require manual review
			if contribDec.IsZero() {
				if err := k.expireClaimForManualReview(cacheCtx, claimID, claim,
					fmt.Sprintf("manual review required: no insurance coverage for denom %s", denom)); err != nil {
					k.Logger(ctx).Error("failed to move no-coverage claim to expired review",
						"claim_id", claimID, "error", err)
					continue
				}
				cacheCtx.EventManager().EmitEvent(sdk.NewEvent(
					"insurance_claim_review_no_coverage",
					sdk.NewAttribute(types.AttributeKeyClaimID, claimID),
					sdk.NewAttribute(types.AttributeKeyAmount, claimedAmt.String()),
					sdk.NewAttribute("denom", denom),
					sdk.NewAttribute(types.AttributeKeyStatus, types.ClaimStatus_CLAIM_STATUS_EXPIRED.String()),
				))
				write()
				ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
				continue
			}

			// Max leverage for auto-approval = 4x
			// If claim > 4 * contribution, require manual review
			maxAutoClaim := contribDec.Mul(decimal.NewFromInt(4))

			if claimedAmt.GreaterThan(maxAutoClaim) {
				// High leverage micro-claim: mark for review
				if err := k.expireClaimForManualReview(cacheCtx, claimID, claim,
					"manual review required: claim leverage exceeds auto-approval cap"); err != nil {
					k.Logger(ctx).Error("failed to move high-leverage claim to expired review",
						"claim_id", claimID, "error", err)
					continue
				}
				cacheCtx.EventManager().EmitEvent(sdk.NewEvent(
					"insurance_claim_review_high_leverage",
					sdk.NewAttribute(types.AttributeKeyClaimID, claimID),
					sdk.NewAttribute(types.AttributeKeyAmount, claimedAmt.String()),
					sdk.NewAttribute("contribution", contribDec.String()),
					sdk.NewAttribute("leverage", claimedAmt.Div(contribDec).StringFixed(2)),
					sdk.NewAttribute(types.AttributeKeyStatus, types.ClaimStatus_CLAIM_STATUS_EXPIRED.String()),
				))
				write()
				ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
				continue
			}

			// Auto-approve small claims
			approvedCoin, err := protoCoinToSDK(claim.ClaimedAmount)
			if err != nil {
				k.Logger(ctx).Error("failed to convert claim amount", "claim_id", claimID, "error", err)
				continue
			}
			if err := k.processClaim(cacheCtx, claimID, "approve", &approvedCoin,
				"auto-approved: claim under threshold and past review window"); err != nil {
				k.Logger(ctx).Error("failed to auto-approve claim", "claim_id", claimID, "error", err)
				// Do not write changes to main context
			} else {
				cacheCtx.EventManager().EmitEvent(sdk.NewEvent(
					"insurance_claim_auto_approved",
					sdk.NewAttribute(types.AttributeKeyClaimID, claimID),
					sdk.NewAttribute(types.AttributeKeyAmount, approvedCoin.String()),
				))
				// Emit telemetry for auto-approved claim
				k.EmitClaimAutoApproved(cacheCtx)

				// Commit successful state changes
				write()
				ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
			}
		} else {
			// Large claims require manual review and must leave the pending queue.
			if err := k.expireClaimForManualReview(cacheCtx, claimID, claim,
				"manual review required: claim exceeds auto-approve threshold"); err != nil {
				k.Logger(ctx).Error("failed to move oversized claim to expired review",
					"claim_id", claimID, "error", err)
				continue
			}
			cacheCtx.EventManager().EmitEvent(sdk.NewEvent(
				"insurance_claim_review_expired",
				sdk.NewAttribute(types.AttributeKeyClaimID, claimID),
				sdk.NewAttribute(types.AttributeKeyAmount, claimedAmt.String()),
				sdk.NewAttribute("review_required", "true"),
				sdk.NewAttribute(types.AttributeKeyStatus, types.ClaimStatus_CLAIM_STATUS_EXPIRED.String()),
			))
			write()
			ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
		}
	}

	if len(claimsToProcess) > 0 {
		k.Logger(ctx).Info("processed expired claims in EndBlocker",
			"count", len(claimsToProcess),
			"block_height", ctx.BlockHeight())
	}

	return nil
}

// updatePoolMetricsEndBlock updates pool metrics at end of block for telemetry.
func (k Keeper) updatePoolMetricsEndBlock(ctx sdk.Context) error {
	poolState, err := k.state.PoolBalance.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil // No pool state yet
		}
		return err
	}

	metrics, err := k.state.PoolMetrics.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		metrics = types.DefaultPoolMetrics()
	} else if err != nil {
		return err
	}

	// Calculate utilization
	totalFunds, err := parsePoolMetricDecimal("TotalFunds", poolState.TotalFunds)
	if err != nil {
		return err
	}
	reservedFunds, err := parsePoolMetricDecimal("ReservedFunds", poolState.ReservedFunds)
	if err != nil {
		return err
	}
	if !totalFunds.IsZero() {
		utilization := reservedFunds.Div(totalFunds)
		poolState.CurrentUtilization = utilization.String()
	}

	// Update pool health status
	poolState.Status = types.EvaluatePoolHealth(poolState)

	// Update coverage ratio
	if !reservedFunds.IsZero() {
		availableFunds, err := parsePoolMetricDecimal("AvailableFunds", poolState.AvailableFunds)
		if err != nil {
			return err
		}
		coverageRatio := availableFunds.Div(reservedFunds)
		metrics.CoverageRatio = coverageRatio.StringFixed(4)
	}

	// Increment samples counter
	metrics.Samples++

	if err := k.state.PoolBalance.Set(ctx, poolState); err != nil {
		return err
	}
	if err := k.state.PoolMetrics.Set(ctx, metrics); err != nil {
		return err
	}

	// Emit pool metrics event for telemetry/explorer
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypePoolMetricsUpdated,
		sdk.NewAttribute(types.AttributeKeyPoolBalance, poolState.TotalFunds),
		sdk.NewAttribute(types.AttributeKeyUtilization, poolState.CurrentUtilization),
		sdk.NewAttribute("pending_claims", fmt.Sprintf("%d", metrics.PendingClaims)),
		sdk.NewAttribute("coverage_ratio", metrics.CoverageRatio),
	))

	return nil
}

func parsePoolMetricDecimal(field, raw string) (decimal.Decimal, error) {
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid %s %q: %w", field, raw, err)
	}
	if !moneyguard.IsSafeExponent(value) {
		return decimal.Zero, fmt.Errorf("invalid %s %q: magnitude out of range", field, raw)
	}
	return value, nil
}

// RegisterInvariants is left empty until the keeper persists state that needs
// invariant checks.
//
//nolint:staticcheck // sdk.InvariantRegistry is required for the current SDK interface.
func RegisterInvariants(_ sdk.InvariantRegistry, _ Keeper) {
}

// WithAuthority returns a copy of the keeper with a new authority. Useful in
// tests when fabricating keeper instances.
func (k Keeper) WithAuthority(authority string) Keeper {
	k.authority = authority
	return k
}

// WithStoreService returns a copy of the keeper using the provided store service.
func (k Keeper) WithStoreService(service store.KVStoreService) Keeper {
	k.storeService = service
	return k
}

// WithCodec returns a copy of the keeper using the provided codec.
func (k Keeper) WithCodec(cdc codec.BinaryCodec) Keeper {
	k.cdc = cdc
	return k
}

// ValidateAuthority ensures the message signer matches the keeper authority.
func (k Keeper) ValidateAuthority(authority string) error {
	if k.authority == "" {
		return nil
	}
	if authority != k.authority {
		return types.ErrUnauthorized.Wrapf("expected %s, got %s", k.authority, authority)
	}
	return nil
}

func canonicalContributionIdentifier(field, value string, required bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if required {
			return "", types.ErrInvalidContribution.Wrapf("%s is required", field)
		}
		if value == "" {
			return "", nil
		}
		return "", types.ErrInvalidContribution.Wrapf("%s must be canonical", field)
	}
	if trimmed != value {
		return "", types.ErrInvalidContribution.Wrapf("%s must be canonical", field)
	}
	if len(value) > types.MaxInsuranceIDLen {
		return "", types.ErrInvalidContribution.Wrapf("%s exceeds %d-byte cap (got %d)", field, types.MaxInsuranceIDLen, len(value))
	}
	return value, nil
}

// ContributeToPool adds funds to the insurance pool from a settlement
func (k Keeper) ContributeToPool(ctx context.Context, receiptID, toolID, publisherID, policyVersion, userID string, amount sdk.Coins) error {
	if amount.IsZero() {
		return nil // No contribution needed
	}
	var err error
	if receiptID, err = canonicalContributionIdentifier("receipt_id", receiptID, true); err != nil {
		return err
	}
	if toolID, err = canonicalContributionIdentifier("tool_id", toolID, false); err != nil {
		return err
	}
	if publisherID, err = canonicalContributionIdentifier("publisher_id", publisherID, false); err != nil {
		return err
	}
	if userID, err = canonicalContributionIdentifier("user_id", userID, false); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Record ownership mapping for claim verification
	if userID != "" {
		if err := k.state.ReceiptOwners.Set(sdkCtx, receiptID, userID); err != nil {
			return types.ErrInternalError.Wrapf("failed to record receipt owner: %s", err)
		}
	}

	alreadyRecorded, err := k.hasContributionForReceipt(sdkCtx, receiptID)
	if err != nil {
		return err
	}
	if alreadyRecorded {
		return types.ErrInvalidContribution.Wrapf("contribution already recorded for receipt %s", receiptID)
	}

	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleAccountName)
	if moduleAddr == nil {
		return types.ErrModuleAccountNotFound
	}
	if k.accountKeeper.GetAccount(sdkCtx, moduleAddr) == nil {
		return types.ErrModuleAccountNotFound
	}

	// Transfer coins from credits module to insurance module. The credits module
	// should have already received these coins from the user.
	if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, creditsTypes.ModuleAccountName, types.ModuleAccountName, amount); err != nil {
		return types.ErrInsufficientFunds.Wrapf("failed to transfer coins to insurance pool: %s", err)
	}

	// Record the contribution so it can be verified during claim filing.
	//
	// Failure here is ATOMIC with the SendCoinsFromModuleToModule
	// above because the enclosing Tx runs against a CacheMultiStore:
	// returning err from this keeper method causes BaseApp's deferred
	// runTx path to discard the cached writes, including the bank
	// transfer. So a recordContribution failure (sequence exhaustion
	// or storage fault) rolls back the coin movement too — funds do
	// NOT end up in the insurance module without a matching
	// contribution record.
	//
	// An earlier version of this comment warned that "funds moved,
	// failing here is bad" — that framing was misleading: the
	// moved-but-not-recorded hazard only exists OUTSIDE a keeper call
	// context (e.g., direct Tx-less manipulation), which is not a
	// reachable path. In a normal SubmitReceipt → MintCoins →
	// Contribute chain, Cosmos SDK's store cache makes the
	// atomic-rollback semantics load-bearing here.
	for _, coin := range amount {
		if _, err := k.recordContribution(sdkCtx, receiptID, toolID, publisherID, policyVersion, coin); err != nil {
			return err
		}
	}

	return nil
}

// CreditWorkflowWastedFeeToPool records a slashed workflow-author wasted-work
// fee as insurance-pool coverage for a failed workflow bundle.
func (k Keeper) CreditWorkflowWastedFeeToPool(ctx context.Context, receiptID, workflowID, authorID, policyVersion string, amount sdk.Coins) error {
	if amount.IsZero() {
		return nil
	}
	var err error
	if receiptID, err = canonicalContributionIdentifier("receipt_id", receiptID, true); err != nil {
		return err
	}
	if workflowID, err = canonicalContributionIdentifier("workflow_id", workflowID, true); err != nil {
		return err
	}
	if authorID, err = canonicalContributionIdentifier("author_id", authorID, true); err != nil {
		return err
	}
	if !amount.IsValid() {
		return types.ErrInvalidContribution.Wrapf("invalid amount: %s", amount.String())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()

	alreadyRecorded, err := k.hasContributionForReceipt(cacheCtx, receiptID)
	if err != nil {
		return err
	}
	if alreadyRecorded {
		return types.ErrInvalidContribution.Wrapf("contribution already recorded for receipt %s", receiptID)
	}

	creditsAddr := k.accountKeeper.GetModuleAddress(creditsTypes.ModuleAccountName)
	if creditsAddr == nil {
		return types.ErrModuleAccountNotFound
	}
	if k.accountKeeper.GetAccount(cacheCtx, creditsAddr) == nil {
		return types.ErrModuleAccountNotFound
	}
	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleAccountName)
	if moduleAddr == nil {
		return types.ErrModuleAccountNotFound
	}
	if k.accountKeeper.GetAccount(cacheCtx, moduleAddr) == nil {
		return types.ErrModuleAccountNotFound
	}
	minter, ok := k.bankKeeper.(interface {
		MintCoins(context.Context, string, sdk.Coins) error
	})
	if !ok {
		return types.ErrInternalError.Wrap("bank keeper cannot mint workflow wasted-work fees")
	}
	if err := minter.MintCoins(cacheCtx, creditsTypes.ModuleAccountName, amount); err != nil {
		return types.ErrInternalError.Wrapf("failed to materialize workflow wasted-work fee in insurance pool: %s", err)
	}
	if err := k.bankKeeper.SendCoinsFromModuleToModule(cacheCtx, creditsTypes.ModuleAccountName, types.ModuleAccountName, amount); err != nil {
		return types.ErrInternalError.Wrapf("failed to transfer workflow wasted-work fee to insurance pool: %s", err)
	}

	for _, coin := range amount {
		if coin.IsZero() {
			continue
		}
		if _, err := k.recordContribution(cacheCtx, receiptID, workflowID, authorID, policyVersion, coin); err != nil {
			return err
		}
	}
	cacheCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeContribution,
			sdk.NewAttribute(types.AttributeKeyReceiptID, receiptID),
			sdk.NewAttribute(types.AttributeKeyToolID, workflowID),
			sdk.NewAttribute(types.AttributeKeyPublisher, authorID),
			sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
			sdk.NewAttribute("source", "workflow_wasted_fee"),
		),
	)

	write()
	sdkCtx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	return nil
}

// recordContribution persists a contribution record and updates aggregate
// metrics. It expects the coins to have already been transferred into the pool
// via ContributeToPool.
func (k Keeper) recordContribution(
	ctx sdk.Context,
	receiptID, toolID, publisherID, policyVersion string,
	amount sdk.Coin,
) (string, error) {
	seq, err := k.state.ContribCounter.Next(ctx)
	if err != nil {
		return "", types.ErrInternalError.Wrapf("failed to increment contribution sequence: %s", err)
	}

	contributionID := fmt.Sprintf("contrib-%d", seq)
	contribution := &types.Contribution{
		Id:            contributionID,
		ReceiptId:     receiptID,
		ToolId:        toolID,
		PublisherId:   publisherID,
		Amount:        amount,
		PolicyVersion: policyVersion,
		Timestamp:     timePtr(ctx.BlockTime()),
		BlockHeight:   ctx.BlockHeight(),
	}

	if err := k.state.ContribByReceipt.Set(ctx, contributionID, contribution); err != nil {
		if errors.Is(err, collections.ErrConflict) {
			return "", types.ErrInvalidContribution.Wrapf("contribution already recorded for receipt %s", receiptID)
		}
		return "", types.ErrInternalError.Wrapf("failed to store contribution: %s", err)
	}

	if err := k.applyContributionTotals(ctx, amount); err != nil {
		return "", err
	}

	return contributionID, nil
}

// getCoverageForReceipt returns the total contributed amount for a receipt
func (k Keeper) getCoverageForReceipt(ctx sdk.Context, receiptID string) (sdk.Coins, error) {
	total := sdk.NewCoins()
	iter, err := k.state.ContribByReceipt.Indexes.Receipt.MatchExact(ctx, receiptID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	for ; iter.Valid(); iter.Next() {
		primaryKey, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		contrib, err := k.state.ContribByReceipt.Get(ctx, primaryKey)
		if err != nil {
			return nil, err
		}
		coin, err := protoCoinToSDK(contrib.Amount)
		if err != nil {
			return nil, err
		}
		total = total.Add(coin)
	}
	return total, nil
}

// publisherIDForReceipt returns the PublisherId of the first contribution
// recorded for the given receipt. Contributions are written by
// MsgProcessContribution which has an authority gate, so the publisher
// stored there is authoritative — it cannot be spoofed by the claimant.
// Returns an empty string (no error) if the receipt has no contributions
// or if the stored contribution has an empty publisher.
//
// Callers of FileClaim use this to reject claims where msg.PublisherId
// doesn't match the authoritative publisher — otherwise a claimant who
// owns a receipt could file a tiny auto-approve claim with a spoofed
// msg.PublisherId and silently inflate the VICTIM publisher's
// PublisherRisks.ClaimCount, eventually triggering recidivist payout
// reductions on the victim's own legitimate claims.
func (k Keeper) publisherIDForReceipt(ctx sdk.Context, receiptID string) (string, error) {
	iter, err := k.state.ContribByReceipt.Indexes.Receipt.MatchExact(ctx, receiptID)
	if err != nil {
		return "", err
	}
	defer func() { _ = iter.Close() }()

	if !iter.Valid() {
		return "", nil
	}
	primaryKey, err := iter.PrimaryKey()
	if err != nil {
		return "", err
	}
	contrib, err := k.state.ContribByReceipt.Get(ctx, primaryKey)
	if err != nil {
		return "", err
	}
	if contrib == nil {
		return "", nil
	}
	return contrib.PublisherId, nil
}

// hasContributionForReceipt checks if any contribution exists for a receipt
func (k Keeper) hasContributionForReceipt(ctx sdk.Context, receiptID string) (bool, error) {
	iter, err := k.state.ContribByReceipt.Indexes.Receipt.MatchExact(ctx, receiptID)
	if err != nil {
		return false, types.ErrRateLimitCheckFailed.Wrapf("failed to check contribution rate: %s", err)
	}
	defer func() { _ = iter.Close() }()

	return iter.Valid(), nil
}

func (k Keeper) expireClaimForManualReview(ctx sdk.Context, claimID string, claim *types.Claim, notes string) error {
	if claim == nil {
		return types.ErrInvalidClaimRequest.Wrap("claim is required")
	}
	if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
		return nil
	}

	claim.Status = types.ClaimStatus_CLAIM_STATUS_EXPIRED
	claim.UpdatedAt = timePtr(ctx.BlockTime())
	claim.ResolutionNotes = notes
	claim.ApprovedAmount = sdk.Coin{}

	if err := k.state.ClaimsByReceipt.Set(ctx, claimID, claim); err != nil {
		return types.ErrInternalError.Wrapf("failed to store expired claim: %s", err)
	}

	return k.updatePendingClaimsCount(ctx, -1)
}

func (k Keeper) processClaim(ctx sdk.Context, claimID, resolution string, approvedCoin *sdk.Coin, notes string) error {
	claim, err := k.state.ClaimsByReceipt.Get(ctx, claimID)
	if err != nil {
		return types.ErrClaimNotFound.Wrapf("claim %s not found", claimID)
	}

	wasPending := claim.Status == types.ClaimStatus_CLAIM_STATUS_PENDING
	if !wasPending && claim.Status != types.ClaimStatus_CLAIM_STATUS_EXPIRED {
		return types.ErrClaimAlreadyProcessed.Wrapf("claim %s already processed", claimID)
	}

	now := ctx.BlockTime()
	claim.UpdatedAt = timePtr(now)
	claim.ResolvedAt = timePtr(now)
	claim.ResolutionNotes = notes

	switch resolution {
	case "approve", "partial":
		if approvedCoin == nil {
			return types.ErrInvalidAmount.Wrap("approved amount required for approval")
		}
		if err := k.reserveApprovedAmount(ctx, *approvedCoin); err != nil {
			return err
		}
		claim.Status = types.ClaimStatus_CLAIM_STATUS_APPROVED
		claim.ApprovedAmount = *approvedCoin
	case "reject":
		claim.Status = types.ClaimStatus_CLAIM_STATUS_REJECTED
		claim.ApprovedAmount = sdk.Coin{}
	default:
		return types.ErrInvalidClaimResolution.Wrapf("unknown resolution: %s", resolution)
	}

	if err := k.state.ClaimsByReceipt.Set(ctx, claimID, claim); err != nil {
		return types.ErrInternalError.Wrapf("failed to store claim: %s", err)
	}

	if wasPending {
		if err := k.updatePendingClaimsCount(ctx, -1); err != nil {
			return err
		}
	}

	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyClaimID, claimID),
		sdk.NewAttribute(types.AttributeKeyResolution, resolution),
		sdk.NewAttribute(types.AttributeKeyStatus, claim.Status.String()),
	}
	if !claim.ApprovedAmount.Amount.IsNil() {
		coinAttr := fmt.Sprintf("%s%s", claim.ApprovedAmount.Amount, claim.ApprovedAmount.Denom)
		if coin, convErr := protoCoinToSDK(claim.ApprovedAmount); convErr == nil {
			coinAttr = coin.String()
		}
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyAmount, coinAttr))
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeClaimProcessed, attrs...))

	return nil
}

func (k Keeper) processPayout(ctx sdk.Context, claimID string, recipient sdk.AccAddress, override *sdk.Coin, txHash string) error {
	claim, err := k.state.ClaimsByReceipt.Get(ctx, claimID)
	if err != nil {
		return types.ErrClaimNotFound.Wrapf("claim %s not found", claimID)
	}

	if claim.Status == types.ClaimStatus_CLAIM_STATUS_PAID {
		return types.ErrClaimAlreadyPaid.Wrapf("claim %s already paid", claimID)
	}
	if claim.Status != types.ClaimStatus_CLAIM_STATUS_APPROVED {
		return types.ErrClaimNotApproved.Wrapf("claim %s not approved", claimID)
	}

	approvedCoin, err := protoCoinToSDK(claim.ApprovedAmount)
	if err != nil {
		return err
	}

	var payoutCoin sdk.Coin
	if override != nil {
		if override.Denom != approvedCoin.Denom {
			return types.ErrInvalidAmount.Wrap("override denom must match approved amount")
		}
		if override.Amount.GT(approvedCoin.Amount) {
			return types.ErrInvalidAmount.Wrap("override amount exceeds approved amount")
		}
		payoutCoin = *override
	} else {
		payoutCoin = approvedCoin
	}

	if !payoutCoin.IsPositive() {
		return types.ErrInvalidAmount.Wrap("payout amount must be positive")
	}

	// Enforce payout caps based on MaxClaimPercent parameter
	params := k.GetParams(ctx)
	poolState, err := k.state.PoolBalance.Get(ctx)
	if err == nil && params != nil {
		maxClaimPercent, ok := insuranceParamDecimal(params.MaxClaimPercent)
		if ok && !maxClaimPercent.IsZero() {
			totalFunds, fundsErr := parsePoolMetricDecimal("TotalFunds", poolState.TotalFunds)
			if fundsErr != nil {
				k.Logger(ctx).Error("invalid TotalFunds in pool state, skipping payout cap",
					"total_funds", poolState.TotalFunds, "error", fundsErr)
			} else {
				maxPayoutAmount := totalFunds.Mul(maxClaimPercent)
				payoutAmountDec := decimal.NewFromBigInt(payoutCoin.Amount.BigInt(), 0)

				if payoutAmountDec.GreaterThan(maxPayoutAmount) {
					// Cap payout at maximum allowed
					maxPayoutInt := maxPayoutAmount.BigInt()
					payoutCoin = sdk.NewCoin(payoutCoin.Denom, sdkmath.NewIntFromBigInt(maxPayoutInt))
					k.Logger(ctx).Info("payout capped at MaxClaimPercent",
						"claim_id", claimID,
						"requested", approvedCoin.String(),
						"capped_to", payoutCoin.String(),
						"max_claim_percent", params.MaxClaimPercent)
					// Emit telemetry for capped payout
					k.EmitPayoutCapped(ctx, claimID)
				}
			}
		}
	}

	// Apply recidivist multiplier for repeat offender publishers.
	//
	// KEY FORMAT: the PublisherRisks map is keyed by a length-prefixed string
	// "%d:%s:%s" (len(publisherId), publisherId, toolId).
	// This prevents collision attacks where a legitimate (publisherId, toolId)
	// pair might collide with a malicious one if the ':' shifts between the two halves.
	if claim.PublisherId != "" {
		publisherRiskKey := fmt.Sprintf("%d:%s:%s", len(claim.PublisherId), claim.PublisherId, claim.ToolId)
		risk, riskErr := k.state.PublisherRisks.Get(ctx, publisherRiskKey)

		// Create new risk entry if one doesn't exist
		if riskErr != nil && !errors.Is(riskErr, collections.ErrNotFound) {
			return fmt.Errorf("failed to get publisher risk: %w", riskErr)
		}
		if riskErr != nil || risk == nil {
			risk = &types.PublisherRisk{
				PublisherId:       claim.PublisherId,
				ToolId:            claim.ToolId,
				PremiumMultiplier: "1.0",
				ClaimCount:        0,
				SuccessRate:       "1.0",
				LastEvaluated:     timePtr(ctx.BlockTime()),
			}
		}

		// Apply recidivist penalty if publisher has >3 claims
		if risk.ClaimCount > 3 {
			// Recidivist publisher: reduce payout by a multiplier based on claim history
			var recidivistReduction decimal.Decimal
			switch {
			case risk.ClaimCount > 10:
				recidivistReduction = decimal.RequireFromString("0.5") // 50% reduction
			case risk.ClaimCount > 5:
				recidivistReduction = decimal.RequireFromString("0.75") // 25% reduction
			default:
				recidivistReduction = decimal.RequireFromString("0.9") // 10% reduction
			}
			payoutAmountDec := decimal.NewFromBigInt(payoutCoin.Amount.BigInt(), 0)
			adjustedAmount := payoutAmountDec.Mul(recidivistReduction)
			adjustedInt := adjustedAmount.BigInt()
			adjustedCoin := sdk.NewCoin(payoutCoin.Denom, sdkmath.NewIntFromBigInt(adjustedInt))

			k.Logger(ctx).Info("payout adjusted for recidivist publisher",
				"claim_id", claimID,
				"publisher_id", claim.PublisherId,
				"claim_count", risk.ClaimCount,
				"original", payoutCoin.String(),
				"adjusted", adjustedCoin.String())
			// Emit telemetry for recidivist penalty
			k.EmitRecidivistPenalty(ctx, claim.PublisherId, risk.ClaimCount, recidivistReduction.InexactFloat64())
			payoutCoin = adjustedCoin
		}

		// Increment publisher's claim count and save
		risk.ClaimCount++
		risk.LastEvaluated = timePtr(ctx.BlockTime())
		if err := k.state.PublisherRisks.Set(ctx, publisherRiskKey, risk); err != nil {
			return fmt.Errorf("update publisher risk %s: %w", publisherRiskKey, err)
		}
	}

	// Ensure payout is still positive after adjustments
	if !payoutCoin.IsPositive() {
		payoutCoin = sdk.NewCoin(payoutCoin.Denom, sdkmath.OneInt())
	}

	if err := k.releaseReservedAmount(ctx, approvedCoin, payoutCoin); err != nil {
		return err
	}

	if err := k.applyPayoutTotals(ctx, payoutCoin); err != nil {
		return err
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, recipient, sdk.NewCoins(payoutCoin)); err != nil {
		return types.ErrInsufficientFunds.Wrapf("failed to send payout: %s", err)
	}

	payoutID, err := k.recordPayout(ctx, claimID, recipient, payoutCoin, txHash)
	if err != nil {
		return err
	}

	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	claim.UpdatedAt = timePtr(ctx.BlockTime())
	if err := k.state.ClaimsByReceipt.Set(ctx, claimID, claim); err != nil {
		return types.ErrInternalError.Wrapf("failed to update claim: %s", err)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeClaimPaid,
		sdk.NewAttribute(types.AttributeKeyClaimID, claimID),
		sdk.NewAttribute(types.AttributeKeyAmount, payoutCoin.String()),
		sdk.NewAttribute(types.AttributeKeyPayoutID, payoutID),
	))

	// Emit telemetry for claim paid
	k.EmitClaimProcessed(ctx, claimID, "paid", payoutCoin)

	return nil
}

func (k Keeper) reserveApprovedAmount(ctx sdk.Context, coin sdk.Coin) error {
	poolState, err := k.state.PoolBalance.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		poolState = types.DefaultPoolState()
	} else if err != nil {
		return types.ErrInternalError.Wrapf("failed to load pool state: %s", err)
	}

	amountDec := decimal.NewFromBigInt(coin.Amount.BigInt(), 0)
	availableDec := poolMetricDecimalOrZero("AvailableFunds", poolState.AvailableFunds)
	if availableDec.LessThan(amountDec) {
		return types.ErrInsufficientFunds.Wrap("insufficient available funds")
	}

	reservedDec := poolMetricDecimalOrZero("ReservedFunds", poolState.ReservedFunds)

	poolState.AvailableFunds = availableDec.Sub(amountDec).String()
	poolState.ReservedFunds = reservedDec.Add(amountDec).String()

	if err := k.state.PoolBalance.Set(ctx, poolState); err != nil {
		return types.ErrInternalError.Wrapf("failed to update pool state: %s", err)
	}

	return nil
}

func (k Keeper) releaseReservedAmount(ctx sdk.Context, reservedCoin, payoutCoin sdk.Coin) error {
	poolState, err := k.state.PoolBalance.Get(ctx)
	if err != nil {
		return types.ErrInternalError.Wrapf("failed to load pool state: %s", err)
	}

	reservedAmt := decimal.NewFromBigInt(reservedCoin.Amount.BigInt(), 0)
	payoutAmt := decimal.NewFromBigInt(payoutCoin.Amount.BigInt(), 0)

	reservedDec := poolMetricDecimalOrZero("ReservedFunds", poolState.ReservedFunds)
	if reservedDec.LessThan(reservedAmt) {
		return types.ErrInsufficientFunds.Wrap("reserved funds insufficient")
	}

	availableDec := poolMetricDecimalOrZero("AvailableFunds", poolState.AvailableFunds)

	totalDec := poolMetricDecimalOrZero("TotalFunds", poolState.TotalFunds)
	if totalDec.LessThan(payoutAmt) {
		return types.ErrInsufficientFunds.Wrap("total funds insufficient")
	}

	// Calculate accounting adjustments:
	// 1. Remove entire reserved amount from ReservedFunds (claim is closed)
	// 2. Remove actual payout from TotalFunds (leaving pool)
	// 3. Return difference (unused reserve) to AvailableFunds
	unusedAmt := reservedAmt.Sub(payoutAmt)

	poolState.ReservedFunds = reservedDec.Sub(reservedAmt).String()
	poolState.TotalFunds = totalDec.Sub(payoutAmt).String()
	poolState.AvailableFunds = availableDec.Add(unusedAmt).String()
	poolState.TotalPayouts = addStringAmounts(poolState.TotalPayouts, payoutCoin.Amount.String())

	if err := k.state.PoolBalance.Set(ctx, poolState); err != nil {
		return types.ErrInternalError.Wrapf("failed to update pool state: %s", err)
	}

	return nil
}

func (k Keeper) applyPayoutTotals(ctx sdk.Context, coin sdk.Coin) error {
	metrics, err := k.state.PoolMetrics.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		metrics = types.DefaultPoolMetrics()
	} else if err != nil {
		return types.ErrInternalError.Wrapf("failed to load pool metrics: %s", err)
	}

	metrics.TotalPayouts_24H = addStringAmounts(metrics.TotalPayouts_24H, coin.Amount.String())

	if err := k.state.PoolMetrics.Set(ctx, metrics); err != nil {
		return types.ErrInternalError.Wrapf("failed to update pool metrics: %s", err)
	}

	return nil
}

func (k Keeper) recordPayout(ctx sdk.Context, claimID string, recipient sdk.AccAddress, amount sdk.Coin, txHash string) (string, error) {
	seq, err := k.state.PayoutCounter.Next(ctx)
	if err != nil {
		return "", types.ErrInternalError.Wrapf("failed to increment payout sequence: %s", err)
	}

	payoutID := fmt.Sprintf("payout-%d", seq)
	payout := &types.Payout{
		Id:          payoutID,
		ClaimId:     claimID,
		RecipientId: recipient.String(),
		Amount:      amount,
		TxHash:      txHash,
		Status:      types.PayoutStatus_PAYOUT_STATUS_COMPLETED,
		PaidAt:      timePtr(ctx.BlockTime()),
		BlockHeight: ctx.BlockHeight(),
	}

	if err := k.state.PayoutsByClaimID.Set(ctx, payoutID, payout); err != nil {
		return "", types.ErrInternalError.Wrapf("failed to store payout: %s", err)
	}

	return payoutID, nil
}

// applyContributionTotals updates pool state and metrics totals using the
// provided contribution amount.
func (k Keeper) applyContributionTotals(ctx sdk.Context, coin sdk.Coin) error {
	poolState, err := k.state.PoolBalance.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		poolState = types.DefaultPoolState()
	} else if err != nil {
		return types.ErrInternalError.Wrapf("failed to load pool state: %s", err)
	}

	amountStr := coin.Amount.String()
	poolState.TotalFunds = addStringAmounts(poolState.TotalFunds, amountStr)
	poolState.AvailableFunds = addStringAmounts(poolState.AvailableFunds, amountStr)
	poolState.TotalContributions = addStringAmounts(poolState.TotalContributions, amountStr)

	if err := k.state.PoolBalance.Set(ctx, poolState); err != nil {
		return types.ErrInternalError.Wrapf("failed to persist pool state: %s", err)
	}

	metrics, err := k.state.PoolMetrics.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		metrics = types.DefaultPoolMetrics()
	} else if err != nil {
		return types.ErrInternalError.Wrapf("failed to load pool metrics: %s", err)
	}

	metrics.TotalContributions_24H = addStringAmounts(metrics.TotalContributions_24H, amountStr)

	if err := k.state.PoolMetrics.Set(ctx, metrics); err != nil {
		return types.ErrInternalError.Wrapf("failed to persist pool metrics: %s", err)
	}

	return nil
}

// GetPoolBalance returns the current insurance pool balance
func (k Keeper) GetPoolBalance(ctx context.Context) (sdk.Coins, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleAccountName)
	if moduleAddr == nil {
		return sdk.NewCoins(), types.ErrModuleAccountNotFound
	}

	// Get all balances from the module account
	balances := k.bankKeeper.GetAllBalances(sdkCtx, moduleAddr)

	return balances, nil
}

// GetParams retrieves module parameters
func (k Keeper) GetParams(ctx context.Context) *types.Params {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.state.Params.Get(sdkCtx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdkCtx).Error("failed to load insurance params", "error", err)
		}
		return types.DefaultParams()
	}
	return params
}

// SetParams updates module parameters
func (k Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if params == nil {
		return types.ErrInvalidParameters.Wrap("params cannot be nil")
	}
	if err := params.ValidateBasic(); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return k.state.Params.Set(sdkCtx, params)
}

func addStringAmounts(current, delta string) string {
	curDec := poolMetricDecimalOrZero("current amount", current)
	deltaDec := poolMetricDecimalOrZero("delta amount", delta)
	return curDec.Add(deltaDec).String()
}

func poolMetricDecimalOrZero(field, raw string) decimal.Decimal {
	value, err := parsePoolMetricDecimal(field, raw)
	if err != nil {
		return decimal.Zero
	}
	return value
}

func insuranceParamDecimal(raw string) (decimal.Decimal, bool) {
	value, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil || !moneyguard.IsSafeExponent(value) {
		return decimal.Zero, false
	}
	return value, true
}

func insuranceParamDecimalOrDefault(raw string, fallback decimal.Decimal) decimal.Decimal {
	value, ok := insuranceParamDecimal(raw)
	if !ok {
		return fallback
	}
	return value
}

// timePtr returns a *time.Time pointing at a copy of t. Insurance's
// timestamp fields are gogoproto stdtime + nullable (optional => *time.Time);
// this mirrors the old timestamppb.New(...) call sites that produced a
// settable pointer from a block time value.
func timePtr(t time.Time) *time.Time {
	return &t
}

// protoCoinToSDK validates a value sdk.Coin pulled off a (gogoproto
// nullable=false) message/state field and returns it normalized.
//
// After the gogoproto migration the stored/received Coin already decodes
// into a cosmossdk.io/types.Coin whose Amount is a math.Int, so this is no
// longer a proto→sdk conversion — it is a stateless guard. We keep it so
// the keeper's denom/positivity error semantics (rather than a panic from
// sdk.NewCoin on an invalid denom) are preserved for user-supplied amounts.
func protoCoinToSDK(coin sdk.Coin) (sdk.Coin, error) {
	if coin.Denom == "" {
		return sdk.Coin{}, types.ErrInvalidAmount.Wrap("coin denom cannot be empty")
	}
	if err := sdk.ValidateDenom(coin.Denom); err != nil {
		return sdk.Coin{}, types.ErrInvalidAmount.Wrapf("coin denom is invalid: %s", err)
	}
	if coin.Amount.IsNil() {
		return sdk.Coin{}, types.ErrInvalidAmount.Wrap("invalid coin amount")
	}
	if !coin.Amount.IsPositive() {
		return sdk.Coin{}, types.ErrInvalidAmount.Wrap("coin amount must be positive")
	}
	return sdk.NewCoin(coin.Denom, coin.Amount), nil
}

// Context unwrap helper so the msg server can operate on sdk.Context.
