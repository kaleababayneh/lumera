package types

const (
	// EventTypeLifecycle is emitted when the module runs a lifecycle hook.
	EventTypeLifecycle = "workflows_lifecycle"
	// EventTypeParamsUpdated is emitted when governance updates workflow params.
	EventTypeParamsUpdated = "workflows_params_updated"
	// EventTypeWorkflowPublished is emitted when an author publishes a workflow version.
	EventTypeWorkflowPublished = "workflows_published"
	// EventTypeWorkflowUpgraded is emitted when an author publishes a newer workflow version.
	EventTypeWorkflowUpgraded = "workflows_upgraded"
	// EventTypeWorkflowDeactivated is emitted when an author deactivates a workflow version.
	EventTypeWorkflowDeactivated = "workflows_deactivated"
	// EventTypeAuthorBondToppedUp is emitted when an author adds unlocked bond balance.
	EventTypeAuthorBondToppedUp = "workflows_author_bond_topped_up"
	// EventTypeAuthorBondWithdrawn is emitted when unlocked author bond is withdrawn.
	EventTypeAuthorBondWithdrawn = "workflows_author_bond_withdrawn"
	// EventTypeAuthorBondSlashed is emitted when author bond is slashed.
	EventTypeAuthorBondSlashed = "workflows_author_bond_slashed"
	// EventTypeBundleQuoted is emitted when a workflow bundle quote is signed and stored.
	EventTypeBundleQuoted = "workflows_bundle_quoted"
	// EventTypeBundleQuoteConsumed is emitted when a workflow bundle quote is consumed.
	EventTypeBundleQuoteConsumed = "workflows_bundle_quote_consumed"
	// EventTypeBundleInvoked is emitted when a workflow bundle invocation finalizes or reverts.
	EventTypeBundleInvoked = "workflows_bundle_invoked"
)

const (
	// AttributeKeyModule identifies the emitting module.
	AttributeKeyModule = "module"
	// AttributeKeyPhase identifies the lifecycle phase.
	AttributeKeyPhase = "phase"
	// AttributeKeyStatus identifies the phase status.
	AttributeKeyStatus = "status"
	// AttributeKeyMinAuthorBond captures the configured minimum workflow-author bond.
	AttributeKeyMinAuthorBond = "min_author_bond"
	// AttributeKeyWastedWorkBPS captures the configured wasted-work fee in bps.
	AttributeKeyWastedWorkBPS = "wasted_work_bps"
	// AttributeKeyWorkflowID identifies a workflow.
	AttributeKeyWorkflowID = "workflow_id"
	// AttributeKeyPrevVersion identifies a previous workflow version.
	AttributeKeyPrevVersion = "prev_version"
	// AttributeKeyNewVersion identifies a new workflow version.
	AttributeKeyNewVersion = "new_version"
	// AttributeKeyVersion identifies a workflow version.
	AttributeKeyVersion = "version"
	// AttributeKeyActor identifies the actor performing a state transition.
	AttributeKeyActor = "actor"
	// AttributeKeyBondDelta captures bond amount movement.
	AttributeKeyBondDelta = "bond_delta"
	// AttributeKeyAmount captures a coin amount.
	AttributeKeyAmount = "amount"
	// AttributeKeyRemaining captures remaining bond amount.
	AttributeKeyRemaining = "remaining"
	// AttributeKeyReason captures a human-readable reason.
	AttributeKeyReason = "reason"
	// AttributeKeyOutcome captures bundle or step outcome.
	AttributeKeyOutcome = "outcome"
	// AttributeKeyLockID identifies a credits lock.
	AttributeKeyLockID = "lock_id"
	// AttributeKeyTraceID captures the invoke trace identifier.
	AttributeKeyTraceID = "trace_id"
	// AttributeKeyBundleID identifies a workflow bundle quote.
	AttributeKeyBundleID = "bundle_id"
	// AttributeKeyTotalMaxCost captures the maximum total quote cost.
	AttributeKeyTotalMaxCost = "total_max_cost"
	// AttributeKeyTotalSLOP95MS captures quote latency budget.
	AttributeKeyTotalSLOP95MS = "total_slo_p95_ms"
	// AttributeKeyTopoLevels captures deterministic DAG execution levels.
	AttributeKeyTopoLevels = "topo_levels"
	// AttributeKeyCriticalPathSteps captures the step ids on the quote critical path.
	AttributeKeyCriticalPathSteps = "critical_path_steps"
	// AttributeKeyComputedP95MS captures the computed critical-path p95 latency.
	AttributeKeyComputedP95MS = "computed_p95_ms"
	// AttributeKeyAnchoredHeight captures the chain height used to produce the quote.
	AttributeKeyAnchoredHeight = "anchored_height"
	// AttributeKeyExpiresAt captures an RFC3339 quote expiry timestamp.
	AttributeKeyExpiresAt = "expires_at"
)
