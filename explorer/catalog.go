package main

// catalog.go is the static description of every on-chain module: a human title,
// a one-line blurb, the message type URLs it accepts, and the custom events it
// emits. The /api/modules endpoint merges this with live counts so the Modules
// page explains what each module does AND what it is currently doing. Sourced
// from proto/lumera/*/tx.proto and the keepers' EmitEvent calls.

type catalogEntry struct {
	Name       string
	Group      string
	Title      string
	Blurb      string
	MsgTypes   []string
	EventTypes []string
}

var moduleCatalog = []catalogEntry{
	{
		Name: "registry", Group: "lumera", Title: "Registry",
		Blurb: "Tool discovery, publisher bonds, Proof-of-Service receipts, challenges & slashing.",
		MsgTypes: []string{
			"/lumera.registry.v1.MsgRegisterTool", "/lumera.registry.v1.MsgUpdateTool", "/lumera.registry.v1.MsgDelistTool",
			"/lumera.registry.v1.MsgSubmitReceipt", "/lumera.registry.v1.MsgAnchorBundle", "/lumera.registry.v1.MsgChallengeReceipt",
			"/lumera.registry.v1.MsgSettleReceipt", "/lumera.registry.v1.MsgCreateBond", "/lumera.registry.v1.MsgWithdrawBond",
			"/lumera.registry.v1.MsgRegisterWatcher", "/lumera.registry.v1.MsgUnregisterWatcher", "/lumera.registry.v1.MsgSubmitSLOProbeReceipt",
			"/lumera.registry.v1.MsgSetSLATemplate", "/lumera.registry.v1.MsgSetDisputeTerms", "/lumera.registry.v1.MsgSetLaneRegistryEntry",
			"/lumera.registry.v1.MsgSetToolCapsule", "/lumera.registry.v1.MsgSetOriginRoutingConfig", "/lumera.registry.v1.MsgUpdateParams",
		},
		EventTypes: []string{
			"tool_registered", "tool_updated", "tool_delisted", "receipt_submitted", "receipt_settled", "receipt_failed",
			"receipt_challenged", "challenge_resolved", "slash", "bond_created", "bond_topped_up", "bond_locked",
			"bond_unlocked", "bond_withdrawn", "watcher_registered", "watcher_slashed", "slo_probe_submitted",
			"quality_score", "quality_rebate", "verified_status_changed", "origin_share_routed",
		},
	},
	{
		Name: "credits", Group: "lumera", Title: "Credits",
		Blurb: "LUME↔LAC swaps, metered credit locks, and Proof-of-Service-gated settlement.",
		MsgTypes: []string{
			"/lumera.credits.v1.MsgSwapLUMEtoLAC", "/lumera.credits.v1.MsgSwapLACtoLUME", "/lumera.credits.v1.MsgLockCredits",
			"/lumera.credits.v1.MsgUnlockCredits", "/lumera.credits.v1.MsgSettleCredits", "/lumera.credits.v1.MsgSettleOverdraft",
			"/lumera.credits.v1.MsgUpdateParams",
		},
		EventTypes: []string{"lume_lac_swap", "credit_lock", "credit_unlock", "settlement", "settlement_dispute", "lac_burn", "revenue_distribute", "receipt_verified"},
	},
	{
		Name: "supernode", Group: "lumera", Title: "SuperNode",
		Blurb: "SuperNode registration, metrics, and Proof-of-Service attestation.",
		MsgTypes: []string{
			"/lumera.supernode.v1.MsgRegisterSupernode", "/lumera.supernode.v1.MsgDeregisterSupernode", "/lumera.supernode.v1.MsgStartSupernode",
			"/lumera.supernode.v1.MsgStopSupernode", "/lumera.supernode.v1.MsgUpdateSupernode", "/lumera.supernode.v1.MsgReportSupernodeMetrics",
			"/lumera.supernode.v1.MsgUpdateParams",
		},
		EventTypes: []string{
			"supernode_registered", "supernode_deregistered", "supernode_started", "supernode_stopped", "supernode_updated",
			"supernode_postponed", "supernode_recovered", "supernode_metrics_reported", "reward_distribution",
			"supernode_storage_full", "supernode_storage_recovered",
		},
	},
	{
		Name: "incentives", Group: "lumera", Title: "Incentives",
		Blurb: "Reputation scoring and tiered badges (Bronze→Platinum) from metrics + disputes.",
		MsgTypes: []string{
			"/lumera.incentives.v1.MsgRecordMetrics", "/lumera.incentives.v1.MsgRequestEvaluation", "/lumera.incentives.v1.MsgUpdateTierConfig",
			"/lumera.incentives.v1.MsgRevokeBadge", "/lumera.incentives.v1.MsgUpdateParams",
		},
		EventTypes: []string{"badge_awarded", "badge_upgraded", "badge_downgraded", "badge_revoked"},
	},
	{
		Name: "insurance", Group: "lumera", Title: "Insurance",
		Blurb: "Risk pool: contributions, claims, payouts, and slash restitution routing.",
		MsgTypes: []string{
			"/lumera.insurance.v1.MsgProcessContribution", "/lumera.insurance.v1.MsgFileClaim", "/lumera.insurance.v1.MsgProcessClaim",
			"/lumera.insurance.v1.MsgProcessPayout", "/lumera.insurance.v1.MsgUpdatePublisherRisk", "/lumera.insurance.v1.MsgUpdateParams",
		},
		EventTypes: []string{
			"insurance_contribution", "insurance_claim_filed", "insurance_claim_processed", "insurance_claim_paid",
			"insurance_funds_reserved", "insurance_funds_released", "insurance_restitution_routed", "insurance_publisher_risk_updated",
		},
	},
	{
		Name: "oracle", Group: "lumera", Title: "Oracle",
		Blurb:      "Validator price-vote injection and aggregated on-chain price feeds.",
		MsgTypes:   []string{"/lumera.oracle.v1.MsgInjectOracleVotes"},
		EventTypes: []string{"oracle_aggregated_price", "oracle_reward_paid"},
	},
	{
		Name: "policies", Group: "lumera", Title: "Policies",
		Blurb: "Governance-managed policy lifecycle (create→activate→deprecate→archive).",
		MsgTypes: []string{
			"/lumera.policies.v1.MsgCreatePolicy", "/lumera.policies.v1.MsgUpdatePolicy", "/lumera.policies.v1.MsgActivatePolicy",
			"/lumera.policies.v1.MsgDeprecatePolicy", "/lumera.policies.v1.MsgArchivePolicy", "/lumera.policies.v1.MsgUpdateParams",
		},
		EventTypes: []string{"policy_created", "policy_updated", "policy_state_changed", "policy_rollback"},
	},
	{
		Name: "nft", Group: "lumera", Title: "NFT / Toolpacks",
		Blurb: "Composable toolpack NFTs and royalty payouts.",
		MsgTypes: []string{
			"/lumera.nft.v1.MsgMintToolpack", "/lumera.nft.v1.MsgUpdateToolpack", "/lumera.nft.v1.MsgDeactivateToolpack",
			"/lumera.nft.v1.MsgRecordRoyaltyPayout",
		},
		EventTypes: []string{"toolpack_minted", "toolpack_updated", "toolpack_deactivated", "toolpack_royalty_paid"},
	},
	{
		Name: "reserve", Group: "lumera", Title: "Reserve",
		Blurb:      "Discount commitments and reserve-backed credit pricing.",
		MsgTypes:   []string{"/lumera.reserve.v1.MsgCreateCommitment", "/lumera.reserve.v1.MsgReleaseExpired", "/lumera.reserve.v1.MsgUpdateParams"},
		EventTypes: []string{},
	},
	{
		Name: "action", Group: "lumera", Title: "Action",
		Blurb: "Distributed action processing for GPU compute jobs.",
		MsgTypes: []string{
			"/lumera.action.v1.MsgRequestAction", "/lumera.action.v1.MsgFinalizeAction", "/lumera.action.v1.MsgApproveAction",
			"/lumera.action.v1.MsgUpdateParams",
		},
		EventTypes: []string{"action_registered", "action_finalized", "action_approved", "action_failed", "action_expired", "svc_verification_passed", "svc_verification_failed_evidence"},
	},
	{
		Name: "audit", Group: "lumera", Title: "Audit",
		Blurb: "Storage-truth epoch reports, evidence, and heal-operation lifecycle.",
		MsgTypes: []string{
			"/lumera.audit.v1.MsgSubmitEpochReport", "/lumera.audit.v1.MsgSubmitEvidence", "/lumera.audit.v1.MsgSubmitStorageRecheckEvidence",
			"/lumera.audit.v1.MsgClaimHealComplete", "/lumera.audit.v1.MsgSubmitHealVerification", "/lumera.audit.v1.MsgUpdateParams",
		},
		EventTypes: []string{"storage_truth_score_updated", "storage_truth_enforced", "storage_truth_recovered", "storage_truth_heal_op_scheduled", "storage_truth_heal_op_verified"},
	},
	{
		Name: "cac", Group: "lumera", Title: "Cache (CAC)",
		Blurb: "Content-addressed cache: stores, hits, tier promotion, and royalty splits.",
		MsgTypes: []string{
			"/lumera.cac.v1.MsgCacheStore", "/lumera.cac.v1.MsgCacheInvalidate", "/lumera.cac.v1.MsgRecordCacheHit",
			"/lumera.cac.v1.MsgTickDecay", "/lumera.cac.v1.MsgPromoteTier", "/lumera.cac.v1.MsgUpdateParams",
		},
		EventTypes: []string{"cache_store", "cache_hit", "cache_miss", "cache_invalidate", "cache_evict", "decay_tick", "tier_promotion", "royalty_distributed"},
	},
	{
		Name: "vaults", Group: "lumera", Title: "Vaults",
		Blurb: "Prepaid-capacity reserve vaults: escrow a discounted LAC commitment per policy/tool tier.",
		MsgTypes: []string{
			"/lumera.vaults.v1.MsgCreateVault",
		},
		EventTypes: []string{},
	},
	{
		Name: "passport", Group: "lumera", Title: "Passport",
		Blurb: "Stake-backed agent identity: bonded stake, lifecycle status, reputation tier; slashable on disputes.",
		MsgTypes: []string{
			"/lumera.passport.v1.MsgRegisterPassport", "/lumera.passport.v1.MsgSuspendPassport", "/lumera.passport.v1.MsgRevokePassport",
			"/lumera.passport.v1.MsgReactivatePassport", "/lumera.passport.v1.MsgSlashStake", "/lumera.passport.v1.MsgTopUpStake",
			"/lumera.passport.v1.MsgUnregisterPassport", "/lumera.passport.v1.MsgUpdateParams",
		},
		EventTypes: []string{
			"passport_registered", "passport_suspended", "passport_revoked", "passport_reactivated",
			"passport_unregistered", "passport_reputation_updated", "stake_slashed", "stake_topped_up",
		},
	},
	{
		Name: "challenges", Group: "lumera", Title: "Challenges",
		Blurb: "Grand-challenge tournaments: escrowed prize pools, entry fees, scored submissions, ranked payouts.",
		MsgTypes: []string{
			"/lumera.challenges.v1.MsgCreateChallenge", "/lumera.challenges.v1.MsgJoinChallenge", "/lumera.challenges.v1.MsgSubmitResult",
			"/lumera.challenges.v1.MsgActivateChallenge", "/lumera.challenges.v1.MsgCancelChallenge", "/lumera.challenges.v1.MsgUpdateParams",
		},
		EventTypes: []string{
			"challenge_created", "challenge_joined", "challenge_scored", "challenge_status_changed",
			"challenge_prize_escrowed", "challenge_prize_paid", "challenge_prize_refunded", "challenge_platform_fee",
			"challenge_payout_skipped", "submission_recorded", "dispute_filed", "dispute_resolved",
		},
	},
	{
		Name: "payment_rails", Group: "lumera", Title: "Payment Rails",
		Blurb: "Programmable settlement bridge: deposit a bridged asset → oracle-priced LAC mint; withdraw burns LAC.",
		MsgTypes: []string{
			"/lumera.payment_rails.v1.MsgCreateDeposit", "/lumera.payment_rails.v1.MsgRequestWithdraw",
			"/lumera.payment_rails.v1.MsgFinalizeWithdraw", "/lumera.payment_rails.v1.MsgRefundDeposit", "/lumera.payment_rails.v1.MsgUpdateParams",
		},
		EventTypes: []string{
			"payment_rails_deposit_created", "payment_rails_pricing_applied", "payment_rails_mint_completed",
			"payment_rails_withdraw_requested", "payment_rails_withdraw_completed", "payment_rails_refund_completed",
		},
	},
	{
		Name: "router", Group: "lumera", Title: "Router",
		Blurb: "Routing-telemetry & metrics: records activations/invocations/cache-hits and aggregates per-tool/global metrics.",
		MsgTypes: []string{
			"/lumera.router.v1.MsgRecordActivation", "/lumera.router.v1.MsgRecordInvocation", "/lumera.router.v1.MsgRecordPolicyUpdate",
			"/lumera.router.v1.MsgRecordCACHit", "/lumera.router.v1.MsgAggregateMetrics", "/lumera.router.v1.MsgUpdateParams",
		},
		EventTypes: []string{
			"tool_activation", "tool_invocation", "metrics_aggregate", "score_update", "cac_hit",
			"param_update", "discovery_subsidy_rebate", "discovery_subsidy_period_reset",
		},
	},
	{
		Name: "priority", Group: "lumera", Title: "Priority",
		Blurb:      "Latency/queue tier definitions (standard/priority/express/enterprise) for the routing layer.",
		MsgTypes:   []string{},
		EventTypes: []string{},
	},
	{
		Name: "auction", Group: "lumera", Title: "Auction",
		Blurb:      "Spot-call auction economics for routing — bid windows, TTLs, and spot discounts.",
		MsgTypes:   []string{},
		EventTypes: []string{},
	},
	{
		Name: "workflows", Group: "lumera", Title: "Workflows",
		Blurb:    "Composable Intelligence: multi-step workflow bundles, author bonds, signed step receipts, replay protection.",
		MsgTypes: []string{},
		EventTypes: []string{
			"workflows_published", "workflows_upgraded", "workflows_deactivated", "workflows_bundle_quoted",
			"workflows_bundle_quote_consumed", "workflows_bundle_invoked", "workflows_author_bond_topped_up",
			"workflows_author_bond_slashed", "workflows_author_bond_withdrawn", "workflows_lifecycle", "workflows_params_updated",
		},
	},
	{
		Name: "claim", Group: "lumera", Title: "Claim",
		Blurb:      "Bitcoin→Cosmos token claim distribution.",
		MsgTypes:   []string{"/lumera.claim.MsgClaim", "/lumera.claim.MsgDelayedClaim", "/lumera.claim.MsgUpdateParams"},
		EventTypes: []string{"claim_processed", "delayed_claim_processed", "claim_period_end", "burn_unclaimed_tokens"},
	},
	{
		Name: "lumeraid", Group: "lumera", Title: "Lumera ID",
		Blurb:      "Identity management (Lumera ID / PastelID).",
		MsgTypes:   []string{"/lumera.lumeraid.MsgUpdateParams"},
		EventTypes: []string{},
	},
	{
		Name: "evmigration", Group: "lumera", Title: "EVM Migration",
		Blurb:      "Legacy account & validator migration to EVM keys.",
		MsgTypes:   []string{"/lumera.evmigration.MsgClaimLegacyAccount", "/lumera.evmigration.MsgMigrateValidator", "/lumera.evmigration.MsgUpdateParams"},
		EventTypes: []string{"claim_legacy_account", "migrate_validator"},
	},
	// --- standard families (decoded automatically; listed for orientation) ---
	{
		Name: "bank", Group: "cosmos", Title: "Bank",
		Blurb:      "Native token transfers (ulume / ulac).",
		MsgTypes:   []string{"/cosmos.bank.v1beta1.MsgSend", "/cosmos.bank.v1beta1.MsgMultiSend"},
		EventTypes: []string{"transfer", "coin_received", "coin_spent"},
	},
	{
		Name: "staking", Group: "cosmos", Title: "Staking",
		Blurb:      "Validator delegation, redelegation, and unbonding.",
		MsgTypes:   []string{"/cosmos.staking.v1beta1.MsgDelegate", "/cosmos.staking.v1beta1.MsgUndelegate", "/cosmos.staking.v1beta1.MsgBeginRedelegate", "/cosmos.staking.v1beta1.MsgCreateValidator"},
		EventTypes: []string{"delegate", "unbond", "redelegate"},
	},
	{
		Name: "gov", Group: "cosmos", Title: "Governance",
		Blurb:      "Proposals, deposits, and votes.",
		MsgTypes:   []string{"/cosmos.gov.v1.MsgSubmitProposal", "/cosmos.gov.v1.MsgVote", "/cosmos.gov.v1.MsgDeposit"},
		EventTypes: []string{"submit_proposal", "proposal_vote", "proposal_deposit"},
	},
	{
		Name: "vm", Group: "evm", Title: "EVM",
		Blurb:      "Ethereum transactions, contracts, and ERC-20 conversions.",
		MsgTypes:   []string{"/cosmos.evm.vm.v1.MsgEthereumTx", "/cosmos.evm.erc20.v1.MsgConvertERC20", "/cosmos.evm.erc20.v1.MsgConvertCoin"},
		EventTypes: []string{"ethereum_tx", "tx_log", "block_bloom"},
	},
	{
		Name: "ibc", Group: "ibc", Title: "IBC",
		Blurb:      "Cross-chain transfers and packet relaying (IBC v10).",
		MsgTypes:   []string{"/ibc.applications.transfer.v1.MsgTransfer", "/ibc.core.channel.v1.MsgRecvPacket", "/ibc.core.channel.v1.MsgAcknowledgement"},
		EventTypes: []string{"send_packet", "recv_packet", "fungible_token_packet"},
	},
	{
		Name: "wasm", Group: "wasm", Title: "CosmWasm",
		Blurb:      "Smart contract store / instantiate / execute.",
		MsgTypes:   []string{"/cosmwasm.wasm.v1.MsgStoreCode", "/cosmwasm.wasm.v1.MsgInstantiateContract", "/cosmwasm.wasm.v1.MsgExecuteContract"},
		EventTypes: []string{"instantiate", "execute", "store_code"},
	},
}
