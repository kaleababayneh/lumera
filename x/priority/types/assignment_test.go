package types

import (
	"testing"
	"time"
)

func TestPriorityAssignmentValidateBasic(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	valid := PriorityAssignment{
		PolicyID:   "policy-1",
		Tier:       "standard",
		AssignedAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := valid.ValidateBasic(); err != nil {
		t.Fatalf("expected valid assignment, got %v", err)
	}

	missingPolicy := valid
	missingPolicy.PolicyID = ""
	if err := missingPolicy.ValidateBasic(); err == nil {
		t.Fatalf("expected error for missing policy id")
	}

	paddedPolicy := valid
	paddedPolicy.PolicyID = " policy-1 "
	if err := paddedPolicy.ValidateBasic(); err == nil {
		t.Fatalf("expected error for padded policy id")
	}

	missingTier := valid
	missingTier.Tier = ""
	if err := missingTier.ValidateBasic(); err == nil {
		t.Fatalf("expected error for missing tier")
	}

	paddedTier := valid
	paddedTier.Tier = " standard "
	if err := paddedTier.ValidateBasic(); err == nil {
		t.Fatalf("expected error for padded tier")
	}

	missingAssignedAt := valid
	missingAssignedAt.AssignedAt = time.Time{}
	if err := missingAssignedAt.ValidateBasic(); err == nil {
		t.Fatalf("expected error for missing assigned_at")
	}

	expired := valid
	expired.ExpiresAt = valid.AssignedAt.Add(-1 * time.Second)
	if err := expired.ValidateBasic(); err == nil {
		t.Fatalf("expected error for expires_at before assigned_at")
	}
}

// TestPriorityAssignmentValidateBasic_ZeroExpiresAtIsValid pins that
// a zero ExpiresAt is a sentinel meaning "no expiry", NOT an invalid
// state. The validator's `!ExpiresAt.IsZero()` guard makes zero a
// pass-through, which callers rely on to signal permanent assignments.
// Without this test, a refactor that flipped the guard (e.g. dropped
// the IsZero check) would silently reject permanent assignments.
func TestPriorityAssignmentValidateBasic_ZeroExpiresAtIsValid(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	a := PriorityAssignment{
		PolicyID:   "policy-perm",
		Tier:       "standard",
		AssignedAt: now,
		// ExpiresAt intentionally left zero — sentinel for permanent.
	}
	if err := a.ValidateBasic(); err != nil {
		t.Fatalf("zero ExpiresAt (permanent sentinel) must be valid: %v", err)
	}
}

// TestPriorityAssignmentValidateBasic_ExpiresEqualAssignedAtIsValid
// pins the Before-not-LTE boundary: an assignment where ExpiresAt
// equals AssignedAt (zero-duration but technically not "before")
// must pass. Regression guard against a refactor flipping the
// comparison to `!Before || After` semantics, which would reject
// same-instant edge cases that sneak in on fast block chains.
func TestPriorityAssignmentValidateBasic_ExpiresEqualAssignedAtIsValid(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	a := PriorityAssignment{
		PolicyID:   "policy-edge",
		Tier:       "standard",
		AssignedAt: now,
		ExpiresAt:  now, // exactly equal
	}
	if err := a.ValidateBasic(); err != nil {
		t.Fatalf("ExpiresAt == AssignedAt (boundary) must be valid: %v", err)
	}
}
