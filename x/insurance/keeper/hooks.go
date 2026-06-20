
package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// Hooks defines the insurance module hooks for anomaly publication
// These hooks allow governance and external modules to report publisher anomalies
// that affect insurance premiums, bond slashing, and pool risk management
type Hooks struct {
	k Keeper
}

// NewHooks creates a new Hooks instance
func NewHooks(k Keeper) Hooks {
	return Hooks{k: k}
}

// AnomalySeverity defines the severity levels for anomaly reporting
type AnomalySeverity string

const (
	// SeverityCritical marks an outage that requires immediate remediation and potential slashing.
	SeverityCritical AnomalySeverity = "critical"
	// SeverityHigh captures severe degradations that still satisfy partial service guarantees.
	SeverityHigh AnomalySeverity = "high"
	// SeverityMedium reports moderate anomalies with limited customer impact.
	SeverityMedium AnomalySeverity = "medium"
	// SeverityLow signals minor blips useful for trend analysis but not enforcement.
	SeverityLow AnomalySeverity = "low"
)

// AnomalyReport encapsulates anomaly details reported by governance or validators
type AnomalyReport struct {
	Severity      AnomalySeverity
	PublisherID   string
	ToolID        string
	Description   string
	Evidence      []string // Evidence URLs or hashes
	ReportedBy    string   // Address of reporter (governance module or validator)
	AutoRemediate bool     // Whether to apply automatic remediation
}

// PublishAnomaly processes an anomaly report from governance or validators
// This is the main entry point for anomaly publication hooks
func (h Hooks) PublishAnomaly(ctx context.Context, report AnomalyReport) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Validate anomaly report
	if report.PublisherID == "" {
		return types.ErrInvalidPublisher.Wrap("publisher ID is required")
	}
	if report.ToolID == "" {
		return types.ErrInvalidPublisher.Wrap("tool ID is required")
	}
	if report.Description == "" {
		return fmt.Errorf("anomaly description is required")
	}

	// Get or create publisher risk profile.
	// The length-prefixed format ensures publisher IDs with colons cannot collide with tool IDs.
	publisherRiskKey := fmt.Sprintf("%d:%s:%s", len(report.PublisherID), report.PublisherID, report.ToolID)
	risk, err := h.k.state.PublisherRisks.Get(sdkCtx, publisherRiskKey)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		// Distinguish "not found" from real storage errors — silently
		// treating a decode/backend failure as "not found" would
		// overwrite an existing risk profile with defaults and
		// erase the accumulated DisputeCount / PremiumTier state.
		return types.ErrInternalError.Wrapf("failed to load publisher risk: %s", err)
	}
	if err != nil || risk == nil {
		risk = &types.PublisherRisk{
			PublisherId:       report.PublisherID,
			ToolId:            report.ToolID,
			PremiumTier:       types.PremiumTier_PREMIUM_TIER_STANDARD,
			PremiumMultiplier: "1.0",
			DisputeCount:      0,
			ClaimCount:        0,
			SuccessRate:       "1.0",
			RiskScore:         "0",
			LastEvaluated:     timePtr(sdkCtx.BlockTime()),
		}
	}

	// Apply severity-based risk adjustments
	switch report.Severity {
	case SeverityCritical:
		// Critical: Immediate quarantine + insurance refunds
		risk.PremiumTier = types.PremiumTier_PREMIUM_TIER_EXTREME
		risk.PremiumMultiplier = "5.0" // 5x premium multiplier
		risk.DisputeCount++
		if err := h.triggerAutoRefunds(ctx, report.PublisherID, report.ToolID); err != nil {
			sdkCtx.Logger().Error("failed to trigger auto refunds", "error", err)
		}
	case SeverityHigh:
		// High: Premium hike, require remediation plan
		if risk.PremiumTier < types.PremiumTier_PREMIUM_TIER_HIGH {
			risk.PremiumTier = types.PremiumTier_PREMIUM_TIER_HIGH
		}
		risk.PremiumMultiplier = "2.5" // 2.5x premium multiplier
		risk.DisputeCount++
	case SeverityMedium:
		// Medium: Notify publisher, increase monitoring
		if risk.PremiumTier < types.PremiumTier_PREMIUM_TIER_STANDARD {
			risk.PremiumTier = types.PremiumTier_PREMIUM_TIER_STANDARD
		}
		risk.PremiumMultiplier = "1.5" // 1.5x premium multiplier
		risk.DisputeCount++
	case SeverityLow:
		// Low: Issue warning, schedule harness re-run
		// No tier change, just increment dispute count
		risk.DisputeCount++
	}

	// Update risk profile
	risk.LastEvaluated = timePtr(sdkCtx.BlockTime())
	if err := h.k.state.PublisherRisks.Set(sdkCtx, publisherRiskKey, risk); err != nil {
		return types.ErrInternalError.Wrapf("failed to update publisher risk: %s", err)
	}

	// Emit anomaly event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"insurance_anomaly_published",
			sdk.NewAttribute("severity", string(report.Severity)),
			sdk.NewAttribute("publisher_id", report.PublisherID),
			sdk.NewAttribute("tool_id", report.ToolID),
			sdk.NewAttribute("description", report.Description),
			sdk.NewAttribute("reported_by", report.ReportedBy),
			sdk.NewAttribute("premium_tier", risk.PremiumTier.String()),
			sdk.NewAttribute("premium_multiplier", risk.PremiumMultiplier),
		),
	)

	sdkCtx.Logger().Info(
		"anomaly published",
		"severity", report.Severity,
		"publisher_id", report.PublisherID,
		"tool_id", report.ToolID,
		"new_tier", risk.PremiumTier.String(),
	)

	return nil
}

// triggerAutoRefunds automatically processes refunds for critical anomalies
// This is called for critical severity anomalies to immediately refund affected users
func (h Hooks) triggerAutoRefunds(ctx context.Context, publisherID, toolID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Find all pending claims for this publisher+tool combination
	var claimsToApprove []string
	err := h.k.state.ClaimsByReceipt.Walk(sdkCtx, nil, func(claimID string, claim *types.Claim) (stop bool, err error) {
		// Check if claim matches publisher/tool and is pending
		if claim.PublisherId == publisherID &&
			claim.ToolId == toolID &&
			claim.Status == types.ClaimStatus_CLAIM_STATUS_PENDING {
			claimsToApprove = append(claimsToApprove, claimID)
		}
		return false, nil
	})
	if err != nil {
		return types.ErrInternalError.Wrapf("failed to walk claims: %s", err)
	}

	// Auto-approve claims through the canonical processClaim path rather
	// than direct Set of Status=APPROVED. The manual Set shortcut skipped
	// reserveApprovedAmount (keeper.go:798 — inside processClaim on the
	// approve branch), which is what moves funds from AvailableFunds to
	// ReservedFunds in poolState. Without that pool-state update, a
	// subsequent MsgProcessPayout for this claim would call
	// releaseReservedAmount (keeper.go:962) which asserts
	// `reservedDec.LessThan(reservedAmt)` returns false (keeper.go:1043);
	// on an auto-approved-but-not-reserved claim that assertion fails
	// with ErrInsufficientFunds "reserved funds insufficient" and the
	// claim is permanently stuck in APPROVED state — never actually
	// payable. processClaim("approve", ...) performs the reservation and
	// the status transition atomically, which is the invariant every
	// downstream payout path assumes.
	for _, claimID := range claimsToApprove {
		claim, err := h.k.state.ClaimsByReceipt.Get(sdkCtx, claimID)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue // Skip claims that no longer exist
			}
			return fmt.Errorf("failed to get claim %s: %w", claimID, err)
		}
		// Re-check status under the single read: a claim may have moved
		// out of PENDING between the Walk snapshot and here (governance
		// approve/reject, expire-sweep). processClaim enforces its own
		// PENDING/EXPIRED precondition but erroring out of this sweep
		// for a benign race would stall every later claimID in the list.
		if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
			continue
		}
		claimedCoin, err := protoCoinToSDK(claim.ClaimedAmount)
		if err != nil {
			return fmt.Errorf("auto-approve claim %s: invalid claimed amount: %w", claimID, err)
		}
		if err := h.k.processClaim(sdkCtx, claimID, "approve", &claimedCoin,
			fmt.Sprintf("auto-approved: critical anomaly for publisher=%s tool=%s", publisherID, toolID)); err != nil {
			return fmt.Errorf("auto-approve claim %s: %w", claimID, err)
		}
	}

	sdkCtx.Logger().Info(
		"auto-approved claims for critical anomaly",
		"publisher_id", publisherID,
		"tool_id", toolID,
		"count", len(claimsToApprove),
	)

	return nil
}

// ReportSecurityAnomaly is a convenience method for security-related anomalies
// This maps to common security threats defined in specs/SECURITY.md
func (h Hooks) ReportSecurityAnomaly(
	ctx context.Context,
	publisherID string,
	toolID string,
	threatType string,
	description string,
	reportedBy string,
) error {
	// Map threat types to severity levels based on specs/SECURITY.md
	severity := SeverityMedium
	autoRemediate := false

	switch threatType {
	case "enclave_attestation_failure", "exploit_confirmed", "mass_dispute", "zero_day":
		severity = SeverityCritical
		autoRemediate = true
	case "slo_breach", "dp_violation", "suspicious_validator":
		severity = SeverityHigh
	case "elevated_latency", "anomaly_warning", "unusual_spending":
		severity = SeverityMedium
	case "telemetry_drift", "outdated_sbom", "minor_policy_violation":
		severity = SeverityLow
	}

	report := AnomalyReport{
		Severity:      severity,
		PublisherID:   publisherID,
		ToolID:        toolID,
		Description:   fmt.Sprintf("%s: %s", threatType, description),
		ReportedBy:    reportedBy,
		AutoRemediate: autoRemediate,
	}

	return h.PublishAnomaly(ctx, report)
}

// GetPublisherRisk retrieves the current risk profile for a
// publisher+tool combination.
// Key format: see the PublishAnomaly comment above; same
// ':'-separator + charset-safety argument.
func (h Hooks) GetPublisherRisk(ctx context.Context, publisherID, toolID string) (*types.PublisherRisk, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	publisherRiskKey := fmt.Sprintf("%d:%s:%s", len(publisherID), publisherID, toolID)

	risk, err := h.k.state.PublisherRisks.Get(sdkCtx, publisherRiskKey)
	if err != nil {
		return nil, fmt.Errorf("publisher risk not found: %w", err)
	}

	return risk, nil
}

// AdjustPremiumTier manually adjusts a publisher's premium tier (governance only).
// Key format: see the PublishAnomaly comment above; same
// ':'-separator + charset-safety argument.
func (h Hooks) AdjustPremiumTier(
	ctx context.Context,
	publisherID string,
	toolID string,
	newTier types.PremiumTier,
	multiplier string,
	notes string,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	publisherRiskKey := fmt.Sprintf("%d:%s:%s", len(publisherID), publisherID, toolID)

	risk, err := h.k.state.PublisherRisks.Get(sdkCtx, publisherRiskKey)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		// Distinguish "not found" from real storage errors — treating a
		// decode/backend failure as absent here would overwrite any
		// existing tier/multiplier state (e.g., prior escalations) with
		// the governance-supplied values as if the record never existed.
		return types.ErrInternalError.Wrapf("failed to load publisher risk: %s", err)
	}
	if err != nil || risk == nil {
		risk = &types.PublisherRisk{
			PublisherId:       publisherID,
			ToolId:            toolID,
			PremiumTier:       newTier,
			PremiumMultiplier: multiplier,
			SuccessRate:       "1.0",
			RiskScore:         "0",
			LastEvaluated:     timePtr(sdkCtx.BlockTime()),
		}
	} else {
		risk.PremiumTier = newTier
		risk.PremiumMultiplier = multiplier
		risk.LastEvaluated = timePtr(sdkCtx.BlockTime())
	}

	if err := h.k.state.PublisherRisks.Set(sdkCtx, publisherRiskKey, risk); err != nil {
		return types.ErrInternalError.Wrapf("failed to adjust premium tier: %s", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"insurance_premium_adjusted",
			sdk.NewAttribute("publisher_id", publisherID),
			sdk.NewAttribute("tool_id", toolID),
			sdk.NewAttribute("new_tier", newTier.String()),
			sdk.NewAttribute("multiplier", multiplier),
			sdk.NewAttribute("notes", notes),
		),
	)

	return nil
}
