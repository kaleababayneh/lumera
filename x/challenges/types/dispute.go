package types

import (
	"errors"
	"fmt"
	"strings"
)

// Dispute domain model for the challenges module.
//
// A dispute is a user-initiated contest of a challenge submission's
// ranking or scoring outcome. Disputes have an explicit lifecycle so
// the module can:
//
//   - record that a ranking is under contestation,
//   - let an arbitrator (module authority or governance) resolve it,
//   - surface aggregate dispute rates to the registry module
//     (which feeds the policy enforcer's MaxDisputeRateBps filter),
//   - provide a durable audit trail for off-chain adjudication.
//
// This file defines the pure-Go domain model (status enum, lifecycle
// transitions, Dispute struct, validation). Keeper storage, msg
// handlers, and proto wire definitions are follow-up work tracked
// under the parent bead (lumera_ai-x2jq4) — the domain model is
// deliberately additive so those follow-ups can layer cleanly on top
// without touching this file's invariants.

// DisputeStatus enumerates the lifecycle states of a dispute.
type DisputeStatus int32

const (
	// DisputeStatusUnspecified is the zero value and is never valid
	// for a persisted Dispute.
	DisputeStatusUnspecified DisputeStatus = 0

	// DisputeStatusFiled is the initial state when a filer first
	// submits the dispute. No arbitrator has acknowledged it yet.
	DisputeStatusFiled DisputeStatus = 1

	// DisputeStatusUnderReview indicates an arbitrator has begun
	// reviewing the dispute. The submission remains contested.
	DisputeStatusUnderReview DisputeStatus = 2

	// DisputeStatusUpheld is a terminal state — the dispute
	// succeeded and the arbitrator decided in the filer's favor.
	// Downstream: the challenges keeper updates rankings and the
	// registry keeper increments the tool's DisputeRateBps.
	DisputeStatusUpheld DisputeStatus = 3

	// DisputeStatusDismissed is a terminal state — the dispute was
	// rejected; the original result stands. Downstream: the
	// registry keeper may still track the attempt on the dispute
	// rate (dismissed disputes count against the filer, not the
	// disputed tool, depending on policy).
	DisputeStatusDismissed DisputeStatus = 4

	// DisputeStatusWithdrawn is a terminal state — the filer
	// withdrew the dispute before a final decision.
	DisputeStatusWithdrawn DisputeStatus = 5
)

// String returns the canonical lowercase form of the status. Used in
// events, API responses, and error messages. Unknown statuses render
// as "dispute_status_unknown_N" so a log reader can still correlate
// an enum drift.
func (s DisputeStatus) String() string {
	switch s {
	case DisputeStatusUnspecified:
		return "unspecified"
	case DisputeStatusFiled:
		return "filed"
	case DisputeStatusUnderReview:
		return "under_review"
	case DisputeStatusUpheld:
		return "upheld"
	case DisputeStatusDismissed:
		return "dismissed"
	case DisputeStatusWithdrawn:
		return "withdrawn"
	default:
		return fmt.Sprintf("dispute_status_unknown_%d", int32(s))
	}
}

// IsTerminal reports whether the status is a terminal state — no
// further transitions are valid from here.
func (s DisputeStatus) IsTerminal() bool {
	switch s {
	case DisputeStatusUpheld, DisputeStatusDismissed, DisputeStatusWithdrawn:
		return true
	default:
		return false
	}
}

// IsValid reports whether the status is one of the defined enum
// values (and not the unspecified zero).
func (s DisputeStatus) IsValid() bool {
	switch s {
	case DisputeStatusFiled,
		DisputeStatusUnderReview,
		DisputeStatusUpheld,
		DisputeStatusDismissed,
		DisputeStatusWithdrawn:
		return true
	default:
		return false
	}
}

// ParseDisputeStatus parses the canonical string form produced by
// String. Returns DisputeStatusUnspecified with a false ok when the
// input is not a recognized status. "unspecified" round-trips to
// DisputeStatusUnspecified for completeness even though it's not a
// valid persisted state.
func ParseDisputeStatus(raw string) (DisputeStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "unspecified", "":
		return DisputeStatusUnspecified, true
	case "filed":
		return DisputeStatusFiled, true
	case "under_review":
		return DisputeStatusUnderReview, true
	case "upheld":
		return DisputeStatusUpheld, true
	case "dismissed":
		return DisputeStatusDismissed, true
	case "withdrawn":
		return DisputeStatusWithdrawn, true
	default:
		return DisputeStatusUnspecified, false
	}
}

// AllowedNextDisputeStatuses returns the set of statuses that `from`
// can legally transition to. Terminal statuses return an empty slice.
// The order is stable (matches the transition-matrix ordering in the
// IsValidDisputeTransition doc comment) so callers can deterministically
// render the set in events and UI.
func AllowedNextDisputeStatuses(from DisputeStatus) []DisputeStatus {
	switch from {
	case DisputeStatusFiled:
		return []DisputeStatus{
			DisputeStatusUnderReview,
			DisputeStatusUpheld,
			DisputeStatusDismissed,
			DisputeStatusWithdrawn,
		}
	case DisputeStatusUnderReview:
		return []DisputeStatus{
			DisputeStatusUpheld,
			DisputeStatusDismissed,
			DisputeStatusWithdrawn,
		}
	default:
		return nil
	}
}

// IsValidDisputeTransition reports whether a status transition is
// permitted. The transition matrix is:
//
//	FILED          → UNDER_REVIEW, UPHELD, DISMISSED, WITHDRAWN
//	UNDER_REVIEW   → UPHELD, DISMISSED, WITHDRAWN
//	UPHELD         → (terminal)
//	DISMISSED      → (terminal)
//	WITHDRAWN      → (terminal)
//	UNSPECIFIED    → (nothing; unspecified is the zero value and not
//	                  persisted)
//
// FILED → UPHELD and FILED → DISMISSED are allowed because an
// arbitrator may issue a summary judgment without a formal
// under-review state (e.g., trivially-invalid disputes).
func IsValidDisputeTransition(from, to DisputeStatus) bool {
	if from.IsTerminal() {
		return false
	}
	switch from {
	case DisputeStatusFiled:
		return to == DisputeStatusUnderReview ||
			to == DisputeStatusUpheld ||
			to == DisputeStatusDismissed ||
			to == DisputeStatusWithdrawn
	case DisputeStatusUnderReview:
		return to == DisputeStatusUpheld ||
			to == DisputeStatusDismissed ||
			to == DisputeStatusWithdrawn
	default:
		return false
	}
}

// MaxDisputeReasonLength caps the dispute reason string. Keeps the
// proto/state size bounded so a spammer cannot balloon storage via
// oversized reasons. Chosen to accommodate a brief human narrative
// (≈3-5 sentences) while rejecting copy-pasted log dumps.
const MaxDisputeReasonLength = 500

// MaxDisputeEvidenceRefLength caps the evidence-reference string —
// typically an IPFS CID, content hash, or URL.
const MaxDisputeEvidenceRefLength = 512

// MaxDisputeOutcomeLength caps the arbitrator's outcome narrative
// appended on resolution. Same rationale as the reason length cap.
const MaxDisputeOutcomeLength = 2048

// Dispute records a contested submission ranking for audit, enforcement,
// and registry dispute-rate aggregation.
//
// Fields are kept as pure Go types so this file can be reviewed and
// tested without proto regeneration. When the module introduces a
// proto Dispute message, this struct becomes the canonical in-memory
// representation and the proto type is a marshaling wrapper; the
// validation below is the authoritative source of truth.
type Dispute struct {
	// ID is the globally unique dispute identifier within the
	// challenges module. Zero is reserved for "unset".
	ID uint64

	// ChallengeID identifies the parent challenge. Must be non-empty.
	ChallengeID string

	// ToolID identifies the disputed tool submission/ranking within
	// the challenge. The challenges module stores submissions and
	// rankings by (challenge_id, tool_id), so this is the canonical
	// submission key.
	ToolID string

	// FiledBy is the bech32 address of the user who filed the dispute.
	// Non-empty.
	FiledBy string

	// Reason is the filer's short narrative explaining why the
	// ranking is being contested. 1..MaxDisputeReasonLength chars.
	Reason string

	// EvidenceRef is an optional content-addressed pointer to
	// supporting evidence (IPFS CID, Arweave tx, HTTPS URL, etc.).
	// Bounded by MaxDisputeEvidenceRefLength when present.
	EvidenceRef string

	// Status is the current lifecycle state. Must be a valid
	// non-zero DisputeStatus.
	Status DisputeStatus

	// FiledAt is the unix-millisecond timestamp at which the filer
	// submitted the dispute. Must be > 0.
	FiledAt int64

	// ResolvedAt is the unix-millisecond timestamp of the resolution
	// decision. Required when Status is a terminal state; must be
	// zero when Status is non-terminal.
	ResolvedAt int64

	// ResolvedBy is the bech32 address of the arbitrator who
	// resolved the dispute. Required when Status is terminal and
	// not Withdrawn; ignored on withdrawal.
	ResolvedBy string

	// Outcome is the arbitrator's short narrative explaining the
	// resolution. Bounded by MaxDisputeOutcomeLength.
	Outcome string
}

// Validate enforces the structural invariants of a Dispute. This is
// the authoritative validation used by both the msg handler (at
// filing/resolution boundaries) and by the keeper (on store write as
// defense-in-depth).
func (d *Dispute) Validate() error {
	if d == nil {
		return errors.New("dispute is nil")
	}
	if d.ID == 0 {
		return errors.New("dispute id must be > 0")
	}
	if err := validateRequiredCanonicalDisputeField("challenge_id", d.ChallengeID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalDisputeField("tool_id", d.ToolID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalDisputeField("filed_by", d.FiledBy); err != nil {
		return err
	}
	if err := validateRequiredCanonicalDisputeField("reason", d.Reason); err != nil {
		return err
	}
	if len(d.Reason) > MaxDisputeReasonLength {
		return fmt.Errorf("dispute reason exceeds %d chars (got %d)", MaxDisputeReasonLength, len(d.Reason))
	}
	if err := validateOptionalCanonicalDisputeField("evidence_ref", d.EvidenceRef); err != nil {
		return err
	}
	if len(d.EvidenceRef) > MaxDisputeEvidenceRefLength {
		return fmt.Errorf("dispute evidence_ref exceeds %d chars (got %d)", MaxDisputeEvidenceRefLength, len(d.EvidenceRef))
	}
	if !d.Status.IsValid() {
		return fmt.Errorf("dispute status %q is not a valid status", d.Status.String())
	}
	if d.FiledAt <= 0 {
		return errors.New("dispute filed_at must be > 0 (unix millis)")
	}
	if d.Status.IsTerminal() {
		if d.ResolvedAt <= 0 {
			return fmt.Errorf("dispute in terminal status %q requires resolved_at", d.Status.String())
		}
		if d.ResolvedAt < d.FiledAt {
			return fmt.Errorf("dispute resolved_at (%d) must be >= filed_at (%d)", d.ResolvedAt, d.FiledAt)
		}
		// Arbitrator identity is required for arbitrator-driven
		// terminal states; withdrawal is filer-initiated so no
		// arbitrator is required there.
		if d.Status != DisputeStatusWithdrawn {
			if err := validateRequiredCanonicalDisputeField("resolved_by", d.ResolvedBy); err != nil {
				return err
			}
		}
	} else {
		if d.ResolvedAt != 0 {
			return fmt.Errorf("dispute in non-terminal status %q must not have resolved_at set", d.Status.String())
		}
		if d.ResolvedBy != "" {
			return fmt.Errorf("dispute in non-terminal status %q must not have resolved_by set", d.Status.String())
		}
	}
	if len(d.Outcome) > MaxDisputeOutcomeLength {
		return fmt.Errorf("dispute outcome exceeds %d chars (got %d)", MaxDisputeOutcomeLength, len(d.Outcome))
	}
	return nil
}

func validateRequiredCanonicalDisputeField(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("dispute %s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("dispute %s must not have leading or trailing whitespace", field)
	}
	return nil
}

func validateOptionalCanonicalDisputeField(field, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("dispute %s must not have leading or trailing whitespace", field)
	}
	return nil
}

// ApplyResolution atomically transitions the dispute to a terminal
// arbitrator-driven status (Upheld or Dismissed), stamping the
// resolver, timestamp, and optional outcome. On success, the
// dispute's in-memory fields reflect the new state AND the result
// passes Validate. On any validation failure (invalid transition,
// missing resolver, backdated timestamp, outcome over cap) the
// dispute is left unchanged.
//
// This is the authoritative way for the msg handler (and any future
// callers) to move a dispute into Upheld/Dismissed — hand-rolling
// the transition elsewhere risks inconsistent partial writes.
func (d *Dispute) ApplyResolution(to DisputeStatus, resolvedBy string, resolvedAt int64, outcome string) error {
	if d == nil {
		return errors.New("dispute is nil")
	}
	if to != DisputeStatusUpheld && to != DisputeStatusDismissed {
		return fmt.Errorf("ApplyResolution target must be Upheld or Dismissed, got %q", to.String())
	}
	if !IsValidDisputeTransition(d.Status, to) {
		return fmt.Errorf("cannot transition dispute from %q to %q", d.Status.String(), to.String())
	}
	// Snapshot for rollback on Validate failure.
	snapshot := *d
	d.Status = to
	d.ResolvedAt = resolvedAt
	d.ResolvedBy = resolvedBy
	d.Outcome = outcome
	if err := d.Validate(); err != nil {
		*d = snapshot
		return err
	}
	return nil
}

// Withdraw atomically transitions the dispute into the Withdrawn
// terminal state. Withdrawal is filer-initiated, so no resolver is
// required; ResolvedBy is cleared on successful withdrawal.
//
// withdrawnAt must be >= FiledAt. Any existing outcome is preserved
// (it may carry a filer-supplied note explaining the withdrawal).
func (d *Dispute) Withdraw(withdrawnAt int64) error {
	if d == nil {
		return errors.New("dispute is nil")
	}
	if !IsValidDisputeTransition(d.Status, DisputeStatusWithdrawn) {
		return fmt.Errorf("cannot withdraw dispute in status %q", d.Status.String())
	}
	snapshot := *d
	d.Status = DisputeStatusWithdrawn
	d.ResolvedAt = withdrawnAt
	d.ResolvedBy = ""
	if err := d.Validate(); err != nil {
		*d = snapshot
		return err
	}
	return nil
}
