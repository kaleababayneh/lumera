
package types

// Event types for the registry module
const (
	// EventTypeToolRegistered is emitted when a new tool is registered
	EventTypeToolRegistered = "tool_registered"

	// EventTypeToolUpdated is emitted when a tool is updated
	EventTypeToolUpdated = "tool_updated"

	// EventTypeToolDelisted is emitted when a tool is delisted
	EventTypeToolDelisted = "tool_delisted"

	// EventTypeBondCreated is emitted when a bond is created for a tool
	EventTypeBondCreated = "bond_created"

	// EventTypeBondToppedUp is emitted when an existing bond is incremented
	EventTypeBondToppedUp = "bond_topped_up"

	// EventTypeBondWithdrawn is emitted when a portion of a bond is withdrawn
	EventTypeBondWithdrawn = "bond_withdrawn"

	// EventTypeBondLocked is emitted when a bond amount is locked for disputes or slashing
	EventTypeBondLocked = "bond_locked"

	// EventTypeBondUnlocked is emitted when a previously locked bond amount is released
	EventTypeBondUnlocked = "bond_unlocked"

	// EventTypeReceiptSubmitted is emitted when a receipt is submitted
	EventTypeReceiptSubmitted = "receipt_submitted"

	// EventTypeReceiptChallenged is emitted when a receipt is challenged
	EventTypeReceiptChallenged = "receipt_challenged"

	// EventTypeSettlement is emitted when a receipt is settled
	EventTypeSettlement = "settlement"

	// EventTypeSlash is emitted when a publisher is slashed
	EventTypeSlash = "slash"

	// EventTypeChallengeResolved is emitted when a challenge is resolved
	EventTypeChallengeResolved = "challenge_resolved"

	// EventTypeParamsUpdated is emitted when module params are updated
	EventTypeParamsUpdated = "params_updated"

	// EventTypeReceiptQueued is emitted when a receipt is added to queue
	EventTypeReceiptQueued = "receipt_queued"

	// EventTypeReceiptSettled is emitted when a receipt is settled from queue
	EventTypeReceiptSettled = "receipt_settled"

	// EventTypeReceiptFailed is emitted when receipt processing fails
	EventTypeReceiptFailed = "receipt_failed"

	// EventTypeFinalizeBlockSettlementReceipt is emitted for each receipt
	// observed by FinalizeBlock settlement batch execution.
	EventTypeFinalizeBlockSettlementReceipt = "finalize_block_settlement_receipt"

	// EventTypeQualityScore is emitted when a tool's quality score is updated
	EventTypeQualityScore = "quality_score"

	// EventTypeQualityRebate is emitted when a quality rebate is applied
	EventTypeQualityRebate = "quality_rebate"

	// EventTypeGovernanceParamChange is emitted when params are updated via governance
	// This provides governance audit trail linking param changes to governance authority
	EventTypeGovernanceParamChange = "governance_param_change"

	// EventTypeVerifiedStatusChanged is emitted when a tool's canonical verified badge tier changes.
	EventTypeVerifiedStatusChanged = "verified_status_changed"

	// EventTypeWatcherRegistered is emitted when a watcher registers or tops up stake.
	EventTypeWatcherRegistered = "watcher_registered"

	// EventTypeWatcherUnregistered is emitted when a watcher withdraws stake.
	EventTypeWatcherUnregistered = "watcher_unregistered"

	// EventTypeWatcherSlashed is emitted when a watcher is slashed.
	EventTypeWatcherSlashed = "watcher_slashed"

	// EventTypeSLOProbeSubmitted is emitted when a watcher submits a probe receipt.
	EventTypeSLOProbeSubmitted = "slo_probe_submitted"

	// EventTypeSLOProbeAggregated is emitted when probe receipts are aggregated.
	EventTypeSLOProbeAggregated = "slo_probe_aggregated"

	// EventTypeSLOProbeChallenged is emitted when x/challenges issues an SLO probe challenge.
	EventTypeSLOProbeChallenged = "slo_probe_challenged"

	// EventTypeSLOProbeDisputeResolved is emitted when x/challenges reports an SLO probe challenge outcome.
	EventTypeSLOProbeDisputeResolved = "slo_probe_dispute_resolved"

	// EventTypeReceiptMirrorQueued is emitted when a receipt hash mirror packet is enqueued/sent.
	EventTypeReceiptMirrorQueued = "receipt_mirror_queued"

	// EventTypeReceiptMirrorAck is emitted when a mirror packet acknowledgement is recorded.
	EventTypeReceiptMirrorAck = "receipt_mirror_ack"

	// EventTypeReceiptMirrorTimeout is emitted when a mirror packet timeout is recorded.
	EventTypeReceiptMirrorTimeout = "receipt_mirror_timeout"

	// EventTypeEvidenceBundleMirrorQueued is emitted when an evidence bundle hash mirror packet is enqueued/sent.
	EventTypeEvidenceBundleMirrorQueued = "evidence_bundle_mirror_queued"

	// EventTypeEvidenceBundleMirrorAck is emitted when an evidence bundle mirror packet acknowledgement is recorded.
	EventTypeEvidenceBundleMirrorAck = "evidence_bundle_mirror_ack"

	// EventTypeEvidenceBundleMirrorTimeout is emitted when an evidence bundle mirror packet timeout is recorded.
	EventTypeEvidenceBundleMirrorTimeout = "evidence_bundle_mirror_timeout"

	// EventTypeOriginShareRouted is emitted for every origin share routing decision.
	EventTypeOriginShareRouted = "origin_share_routed"

	// EventTypeOriginRoutingConfigSet is emitted when an origin routing config is created/updated.
	EventTypeOriginRoutingConfigSet = "origin_routing_config_set"
)

// Event attribute keys
const (
	AttributeKeyToolID     = "tool_id"
	AttributeKeyOwner      = "owner"
	AttributeKeyVersion    = "version"
	AttributeKeyBond       = "bond"
	AttributeKeySchemaHash = "schema_hash"
	AttributeKeyReceiptID  = "receipt_id"
	AttributeKeyCost       = "cost"
	AttributeKeyChallenger = "challenger"
	AttributeKeyBurn       = "burn"
	AttributeKeyInsurance  = "insurance"
	// AttributeKeyInsuranceRestitution is the portion of a slash routed to
	// insurance-reserve replenishment per specs/governance/slashing-rules.md
	// §"Restitution Routing". Distinct from AttributeKeyInsurance (premium
	// events) so reconcilers can sum slash flows without double-counting.
	AttributeKeyInsuranceRestitution = "insurance_restitution"
	// AttributeKeyTreasuryRestitution is the portion of a slash routed to
	// the governance treasury / fee_collector module account.
	AttributeKeyTreasuryRestitution = "treasury_restitution"
	// AttributeKeyUserRestitution is the portion of a slash staged as
	// impacted-user credit in the insurance pool. Co-locates with the reserve
	// replenishment at the same module address; this attribute preserves the
	// split so downstream claim accounting can debit user credit without
	// touching the reserve.
	AttributeKeyUserRestitution       = "user_restitution"
	AttributeKeySeverity              = "severity"
	AttributeKeyAmount                = "amount"
	AttributeKeyResolution            = "resolution"
	AttributeKeyRefund                = "refund"
	AttributeKeyStatus                = "status"
	AttributeKeyAggregateVersion      = "aggregate_version"
	AttributeKeySupersededByVersion   = "superseded_by_version"
	AttributeKeySupersededByChallenge = "superseded_by_challenge_id"
	AttributeKeyReadyAt               = "ready_at"
	AttributeKeyError                 = "error"
	AttributeKeyRetries               = "retries"
	AttributeKeyProcessedAt           = "processed_at"
	AttributeKeyReason                = "reason"
	AttributeKeyEvidence              = "evidence"
	AttributeKeyBatchSize             = "batch_size"
	AttributeKeyIdempotent            = "idempotent"
	AttributeKeySource                = "source"
	AttributeKeyDedupKey              = "dedup_key"
	AttributeKeyChannelID             = "channel_id"
	AttributeKeyPortID                = "port_id"
	AttributeKeySequence              = "sequence"
	AttributeKeyAckStatus             = "ack_status"
	AttributeKeyAckReason             = "ack_reason"
	AttributeKeyNextRetryAt           = "next_retry_at"
	AttributeKeyBundleHash            = "bundle_hash"
	AttributeKeyBundleVersion         = "bundle_version"
	AttributeKeySubjectKind           = "subject_kind"
	AttributeKeySubjectID             = "subject_id"
	AttributeKeyBundleStatus          = "bundle_status"
	AttributeKeyTraceID               = "trace_id"
	AttributeKeyTimestamp             = "timestamp"
	AttributeKeyBlockHeight           = "block_height"
	AttributeKeyNewTotal              = "new_total"
	AttributeKeyRemaining             = "remaining"
	AttributeKeyLockReason            = "lock_reason"
	AttributeKeyChallengeID           = "challenge_id"
	AttributeKeyStake                 = "stake"
	AttributeKeyOriginTool            = "origin_tool_id"
	AttributeKeyCacheHit              = "cache_hit"
	AttributeKeyCacheRoyalty          = "cache_royalty"
	AttributeKeyPolicy                = "policy_snapshot"
	AttributeKeySplitPublisherOrigin  = "publisher_origin"
	AttributeKeySplitPublisherServing = "publisher_serving"
	AttributeKeyRouterGross           = "router_gross"
	AttributeKeyRouterNet             = "router_net"
	AttributeKeyReferrer              = "referrer_share"
	AttributeKeyRebate                = "rebate"
	AttributeKeyActual                = "actual_settled"
	AttributeKeyLockedQuote           = "locked_quote"

	// Additional attributes for comprehensive event tracking
	AttributeKeyCategories    = "categories"
	AttributeKeyUserAddress   = "user_address"
	AttributeKeyRouterAddress = "router_address"
	AttributeKeyAction        = "action"
	AttributeKeyActor         = "actor"
	AttributeKeyTarget        = "target"
	AttributeKeyDetails       = "details"
	AttributeKeyEntityType    = "entity_type"
	AttributeKeyEntityID      = "entity_id"
	AttributeKeyFromState     = "from_state"
	AttributeKeyToState       = "to_state"

	// Quality rebate attributes
	AttributeKeyQualityScore = "quality_score"
	AttributeKeyRebateTier   = "rebate_tier"
	AttributeKeyRebateBps    = "rebate_bps"
	AttributeKeyRebateAmount = "rebate_amount"

	// Watcher attributes
	AttributeKeyWatcher        = "watcher"
	AttributeKeyWindowStart    = "window_start"
	AttributeKeyWindowEnd      = "window_end"
	AttributeKeyMedianP95      = "median_p95_latency_ms"
	AttributeKeyMedianAvailBps = "median_availability_bps"
	AttributeKeyMedianErrBps   = "median_error_rate_bps"
	AttributeKeyTargetKind     = "target_kind"
	AttributeKeyTargetID       = "target_id"
	AttributeKeyOutcome        = "outcome"
	AttributeKeyEvidenceDigest = "evidence_digest"
	AttributeKeyResponseDigest = "response_digest"

	// Governance attributes
	AttributeKeyGovernanceAuthority = "governance_authority"
	AttributeKeyGovernanceModule    = "governance_module"
	AttributeKeyParamChanges        = "param_changes"

	// Origin routing attributes
	AttributeKeyOriginID       = "origin_id"
	AttributeKeyOriginShare    = "origin_share"
	AttributeKeySettlementID   = "settlement_id"
	AttributeKeyDirectBps      = "direct_bps"
	AttributeKeyBuybackBps     = "buyback_bps"
	AttributeKeyTreasuryBps    = "treasury_bps"
	AttributeKeyInsurBps       = "insurance_bps"
	AttributeKeyDirectAmount   = "direct_amount"
	AttributeKeyBuybackAmount  = "buyback_amount"
	AttributeKeyTreasuryAmount = "treasury_amount"
	AttributeKeyInsurAmount    = "insurance_amount"
	AttributeKeyBeneficiary    = "beneficiary"
	AttributeKeyRoutingStatus  = "routing_status"
)
