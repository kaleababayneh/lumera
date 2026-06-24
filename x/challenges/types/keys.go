package types

const (
	ModuleName  = "challenges"
	StoreKey    = ModuleName
	MemStoreKey = "mem_" + ModuleName

	// Collection prefixes for state storage.
	ParamsPrefix           = 0x00
	ChallengePrefix        = 0x01
	ParticipantPrefix      = 0x02
	SubmissionPrefix       = 0x03
	RankingPrefix          = 0x04
	SequencePrefix         = 0x05
	StatusIndexPrefix      = 0x06
	CreatorIndexPrefix     = 0x07
	EventPrefix            = 0x08
	ToolIndexPrefix        = 0x09
	ScoringEnteredAtPrefix = 0x0A

	// Dispute resolution prefixes (lumera_ai-x2jq4 scaffolding).
	// DisputePrefix stores Dispute records keyed by dispute ID.
	DisputePrefix = 0x0B
	// DisputeSubmissionIndexPrefix indexes disputes by submission ID
	// so the challenges keeper can efficiently enumerate all
	// disputes filed against a given ranking.
	DisputeSubmissionIndexPrefix = 0x0C
	// DisputeFilerIndexPrefix indexes disputes by filer address so
	// a user can list their own disputes.
	DisputeFilerIndexPrefix = 0x0D
	// DisputeSequencePrefix tracks the next dispute ID.
	DisputeSequencePrefix = 0x0E
)
