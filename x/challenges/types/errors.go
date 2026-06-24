package types

import (
	"cosmossdk.io/errors"
)

var (
	ErrChallengeNotFound   = errors.Register(ModuleName, 2, "challenge not found")
	ErrInvalidStatus       = errors.Register(ModuleName, 3, "invalid challenge status")
	ErrInvalidTransition   = errors.Register(ModuleName, 4, "invalid status transition")
	ErrParticipantExists   = errors.Register(ModuleName, 5, "participant already registered")
	ErrParticipantNotFound = errors.Register(ModuleName, 6, "participant not found")
	ErrChallengeFull       = errors.Register(ModuleName, 7, "challenge at max participants")
	ErrNotCreator          = errors.Register(ModuleName, 8, "caller is not the challenge creator")
	ErrSubmissionNotFound  = errors.Register(ModuleName, 9, "submission not found")
	ErrRankingNotFound     = errors.Register(ModuleName, 10, "ranking not found")
	ErrNilChallenge        = errors.Register(ModuleName, 11, "challenge cannot be nil")
	ErrMissingChallengeID  = errors.Register(ModuleName, 12, "challenge missing id")
	ErrChallengeNotActive  = errors.Register(ModuleName, 13, "challenge is not active")
	ErrUnauthorized        = errors.Register(ModuleName, 14, "unauthorized")
	ErrPrizeBelowMinimum   = errors.Register(ModuleName, 15, "prize pool below minimum")
	ErrInsufficientFee     = errors.Register(ModuleName, 16, "insufficient entry fee")
	ErrChallengeNotStarted = errors.Register(ModuleName, 17, "challenge has not started yet")
	ErrBadgeTierTooLow     = errors.Register(ModuleName, 18, "tool badge tier below challenge minimum")
	ErrMissingCategory     = errors.Register(ModuleName, 19, "tool missing required category")

	// Dispute resolution (lumera_ai-x2jq4 scaffolding).
	ErrDisputeNotFound          = errors.Register(ModuleName, 20, "dispute not found")
	ErrInvalidDisputeStatus     = errors.Register(ModuleName, 21, "invalid dispute status")
	ErrInvalidDisputeTransition = errors.Register(ModuleName, 22, "invalid dispute status transition")
	ErrDisputeAlreadyResolved   = errors.Register(ModuleName, 23, "dispute is already in a terminal state")
	ErrDisputeReasonRequired    = errors.Register(ModuleName, 24, "dispute reason is required")
	ErrNilDispute               = errors.Register(ModuleName, 25, "dispute cannot be nil")
)
