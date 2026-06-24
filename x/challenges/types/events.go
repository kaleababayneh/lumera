package types

const (
	// EventTypeChallengeCreated is emitted when a new challenge is created.
	EventTypeChallengeCreated = "challenge_created"
	// EventTypeChallengeStatusChanged is emitted when a challenge transitions status.
	EventTypeChallengeStatusChanged = "challenge_status_changed"
	// EventTypeChallengeJoined is emitted when a tool joins a challenge.
	EventTypeChallengeJoined = "challenge_joined"
	// EventTypeSubmissionRecorded is emitted when a result submission is recorded.
	EventTypeSubmissionRecorded = "submission_recorded"
	// EventTypeChallengeScored is emitted when a challenge is scored and ranked.
	EventTypeChallengeScored = "challenge_scored"
	// EventTypePrizeEscrowed is emitted when the prize pool is escrowed.
	EventTypePrizeEscrowed = "challenge_prize_escrowed"
	// EventTypePrizePaid is emitted when a winner receives their prize.
	EventTypePrizePaid = "challenge_prize_paid"
	// EventTypePlatformFee is emitted when a platform fee is collected.
	EventTypePlatformFee = "challenge_platform_fee"
	// EventTypePayoutSkipped is emitted when a payout is skipped (bad address).
	EventTypePayoutSkipped = "challenge_payout_skipped"
	// EventTypePrizeRefunded is emitted when a cancelled challenge's prize is refunded.
	EventTypePrizeRefunded = "challenge_prize_refunded"
	// EventTypeParamsUpdated is emitted when module params are updated.
	EventTypeParamsUpdated = "update_params"
	// EventTypeProtocolChallengeIssued is emitted when a protocol challenge is issued.
	EventTypeProtocolChallengeIssued = "protocol_challenge_issued"
	// EventTypeProtocolChallengeResponded is emitted when a protocol challenge receives a response.
	EventTypeProtocolChallengeResponded = "protocol_challenge_responded"
	// EventTypeProtocolChallengeExpired is emitted when a protocol challenge expires.
	EventTypeProtocolChallengeExpired = "protocol_challenge_expired"

	// Dispute resolution events (lumera_ai-x2jq4 scaffolding).
	// EventTypeDisputeFiled is emitted when a dispute is initially filed.
	EventTypeDisputeFiled = "dispute_filed"
	// EventTypeDisputeStatusChanged is emitted on every valid dispute
	// lifecycle transition (filed → under_review → upheld/dismissed,
	// or any → withdrawn).
	EventTypeDisputeStatusChanged = "dispute_status_changed"
	// EventTypeDisputeResolved is emitted when a dispute reaches an
	// arbitrator-driven terminal state (upheld or dismissed).
	EventTypeDisputeResolved = "dispute_resolved"
	// EventTypeDisputeWithdrawn is emitted when the filer withdraws a dispute.
	EventTypeDisputeWithdrawn = "dispute_withdrawn"

	// Attribute keys used across challenge events.
	AttributeKeyChallengeID    = "challenge_id"
	AttributeKeyCreator        = "creator"
	AttributeKeyTitle          = "title"
	AttributeKeyToolID         = "tool_id"
	AttributeKeyPublisher      = "publisher"
	AttributeKeySubmitter      = "submitter"
	AttributeKeyCompositeScore = "composite_score"
	AttributeKeyChallengeClass = "challenge_class"
	AttributeKeyBlockHeight    = "block_height"
	AttributeKeyDeadlineUnix   = "deadline_unix"
	AttributeKeyNewStatus      = "new_status"
	AttributeKeyParticipants   = "participants_scored"
	AttributeKeyAmount         = "amount"
	AttributeKeyRank           = "rank"
	AttributeKeyRecipient      = "recipient"
	AttributeKeyReason         = "reason"
	AttributeKeyAuthority      = "authority"
	AttributeKeyAlreadyClaimed = "already_claimed"
	AttributeKeyIssuer         = "issuer"
	AttributeKeyTarget         = "target"
	AttributeKeyIssueHeight    = "issue_height"
	AttributeKeyDeadlineHeight = "deadline_height"
	AttributeKeyEvidenceDigest = "evidence_digest"
	AttributeKeyResponseDigest = "response_digest"

	// Dispute event attribute keys.
	AttributeKeyDisputeID    = "dispute_id"
	AttributeKeySubmissionID = "submission_id"
	AttributeKeyFiler        = "filer"
	AttributeKeyResolvedBy   = "resolved_by"
	AttributeKeyFromStatus   = "from_status"
	AttributeKeyToStatus     = "to_status"
	AttributeKeyOutcome      = "outcome"
)
