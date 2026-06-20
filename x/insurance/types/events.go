
package types

// Event types
const (
	EventTypeClaimFiled           = "insurance_claim_filed"
	EventTypeClaimProcessed       = "insurance_claim_processed"
	EventTypeClaimPaid            = "insurance_claim_paid"
	EventTypeContribution         = "insurance_contribution"
	EventTypeFundsReserved        = "insurance_funds_reserved"
	EventTypeFundsReleased        = "insurance_funds_released"
	EventTypePublisherRiskUpdated = "insurance_publisher_risk_updated"
	EventTypePoolMetricsUpdated   = "insurance_pool_metrics_updated"
	EventTypeParamsUpdated        = "insurance_params_updated"

	// Slash-restitution routing events (lumera_ai-tvanr).
	// EventTypeRestitutionPlanned is emitted when the routing
	// planner produces a non-empty instruction list for a slashed
	// amount, before any bank operations run. Operators can
	// correlate this against the per-leg EventTypeRestitutionRouted
	// events that follow.
	EventTypeRestitutionPlanned = "insurance_restitution_planned"
	// EventTypeRestitutionRouted is emitted once per non-zero
	// routing leg (users / insurance / treasury / burn) after its
	// bank operation completes.
	EventTypeRestitutionRouted = "insurance_restitution_routed"
	// EventTypeRestitutionFailed is emitted when a routing
	// instruction fails mid-execution. Operators treat this as a
	// PagerDuty-grade alert: slashed funds may be stranded in the
	// insurance module account awaiting reconciliation.
	EventTypeRestitutionFailed = "insurance_restitution_failed"
)

// Event attributes
const (
	AttributeKeyClaimID        = "claim_id"
	AttributeKeyReceiptID      = "receipt_id"
	AttributeKeyClaimant       = "claimant"
	AttributeKeyPublisher      = "publisher"
	AttributeKeyToolID         = "tool_id"
	AttributeKeyAmount         = "amount"
	AttributeKeyStatus         = "status"
	AttributeKeyResolution     = "resolution"
	AttributeKeyPayoutID       = "payout_id"
	AttributeKeyRiskScore      = "risk_score"
	AttributeKeyPremiumTier    = "premium_tier"
	AttributeKeyPoolBalance    = "pool_balance"
	AttributeKeyUtilization    = "utilization"
	AttributeKeyAuthority      = "authority"
	AttributeKeyContributionID = "contribution_id"

	// Restitution routing attribute keys.
	AttributeKeyRestitutionTotal     = "restitution_total"
	AttributeKeyRestitutionDestKind  = "restitution_dest_kind"  // one of "users"|"insurance"|"treasury"|"burn"
	AttributeKeyRestitutionDestRef   = "restitution_dest_ref"   // bech32 addr or module name; empty on burn
	AttributeKeyRestitutionLegAmount = "restitution_leg_amount"
	AttributeKeyRestitutionError     = "restitution_error"      // on EventTypeRestitutionFailed
)
