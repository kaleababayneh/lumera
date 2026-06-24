package keeper

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"lukechampine.com/blake3"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

const protocolLifecyclePrefix = 0x0F

type ProtocolChallengeOutcome string

const (
	ProtocolChallengeOutcomeIssued    ProtocolChallengeOutcome = "issued"
	ProtocolChallengeOutcomeResponded ProtocolChallengeOutcome = "responded"
	ProtocolChallengeOutcomeExpired   ProtocolChallengeOutcome = "expired"
)

const (
	SLOProbeChallengeTargetSubmission = "submission"
	SLOProbeChallengeTargetAggregate  = "aggregate"
)

const (
	protocolEvidenceKindIdentityAttestation = "identity_attestation"
	protocolEvidenceKindTEEReport           = "tee_report"
	defaultTEEVerifierFreshness             = 24 * time.Hour
)

type ProtocolChallengeIssue struct {
	ChallengeID       string
	Issuer            string
	Target            string
	ChallengeType     types.ChallengeType
	Title             string
	Description       string
	EvidenceDigest    string
	DeadlineHeight    int64
	DeadlineTime      time.Time
	RegistryReference *ProtocolRegistryReference
	EvidenceContract  *ProtocolEvidenceContract
}

type SLOProbeChallengeIssue struct {
	ChallengeID    string
	Issuer         string
	ToolID         string
	TargetKind     string
	TargetID       string
	Reason         string
	Title          string
	Description    string
	EvidenceDigest string
	DeadlineHeight int64
	DeadlineTime   time.Time
}

type IdentityAttestationChallengeIssue struct {
	ChallengeID    string
	Issuer         string
	LumeraID       string
	NonceDigest    string
	Title          string
	Description    string
	DeadlineHeight int64
	DeadlineTime   time.Time
}

type IdentityAttestationChallengeResponse struct {
	ChallengeID  string
	LumeraID     string
	Nonce        string
	SignatureHex string
}

type TEEReportChallengeIssue struct {
	ChallengeID                string
	Issuer                     string
	ToolID                     string
	ReportDigest               string
	VerifierResultDigest       string
	MeasurementDigest          string
	ExpectedMeasurementDigest  string
	Provider                   string
	ExpectedProvider           string
	PolicyDigest               string
	VerifierPolicyDigest       string
	ReceiptID                  string
	TraceID                    string
	VerifiedAt                 time.Time
	VerifierResultExpiresAt    time.Time
	MaxVerifierResultFreshness time.Duration
	Title                      string
	Description                string
	DeadlineHeight             int64
	DeadlineTime               time.Time
}

type ProtocolChallengeResponse struct {
	ChallengeID    string
	Responder      string
	ResponseDigest string
}

type ProtocolRegistryReference struct {
	SourceModule string `json:"source_module"`
	ToolID       string `json:"tool_id"`
	TargetKind   string `json:"target_kind"`
	TargetID     string `json:"target_id"`
	Reason       string `json:"reason"`
}

type ProtocolEvidenceContract struct {
	Kind                 string `json:"kind"`
	SubjectID            string `json:"subject_id,omitempty"`
	NonceDigest          string `json:"nonce_digest,omitempty"`
	ReportDigest         string `json:"report_digest,omitempty"`
	VerifierResultDigest string `json:"verifier_result_digest,omitempty"`
	MeasurementDigest    string `json:"measurement_digest,omitempty"`
	Provider             string `json:"provider,omitempty"`
	PolicyDigest         string `json:"policy_digest,omitempty"`
	ReceiptID            string `json:"receipt_id,omitempty"`
	TraceID              string `json:"trace_id,omitempty"`
	VerifiedAtUnix       int64  `json:"verified_at_unix,omitempty"`
	ResultExpiresAtUnix  int64  `json:"result_expires_at_unix,omitempty"`
}

type ProtocolChallengeRecord struct {
	ChallengeID       string                     `json:"challenge_id"`
	Issuer            string                     `json:"issuer"`
	Target            string                     `json:"target"`
	ChallengeType     types.ChallengeType        `json:"challenge_type"`
	IssueHeight       int64                      `json:"issue_height"`
	IssueTimeUnix     int64                      `json:"issue_time_unix"`
	DeadlineHeight    int64                      `json:"deadline_height"`
	DeadlineTimeUnix  int64                      `json:"deadline_time_unix"`
	EvidenceDigest    string                     `json:"evidence_digest"`
	ResponseDigest    string                     `json:"response_digest,omitempty"`
	RespondedHeight   int64                      `json:"responded_height,omitempty"`
	RespondedAtUnix   int64                      `json:"responded_at_unix,omitempty"`
	Outcome           ProtocolChallengeOutcome   `json:"outcome"`
	RegistryReference *ProtocolRegistryReference `json:"registry_reference,omitempty"`
	EvidenceContract  *ProtocolEvidenceContract  `json:"evidence_contract,omitempty"`
}

func (k *Keeper) IssueSLOProbeChallenge(ctx context.Context, issue SLOProbeChallengeIssue) (string, error) {
	ref, err := normalizeSLOProbeRegistryReference(ProtocolRegistryReference{
		SourceModule: "registry",
		ToolID:       issue.ToolID,
		TargetKind:   issue.TargetKind,
		TargetID:     issue.TargetID,
		Reason:       issue.Reason,
	})
	if err != nil {
		return "", err
	}

	description := strings.TrimSpace(issue.Description)
	if description == "" {
		description = fmt.Sprintf("SLO probe %s challenge for %s", ref.TargetKind, ref.ToolID)
	}

	return k.IssueChallenge(ctx, ProtocolChallengeIssue{
		ChallengeID:       issue.ChallengeID,
		Issuer:            issue.Issuer,
		Target:            ref.ToolID,
		ChallengeType:     types.ChallengeType_CHALLENGE_TYPE_SLO_PROBE,
		Title:             issue.Title,
		Description:       description,
		EvidenceDigest:    issue.EvidenceDigest,
		DeadlineHeight:    issue.DeadlineHeight,
		DeadlineTime:      issue.DeadlineTime,
		RegistryReference: ref,
	})
}

func (k *Keeper) IssueIdentityAttestationChallenge(ctx context.Context, issue IdentityAttestationChallengeIssue) (string, error) {
	nonceDigest, err := normalizeProtocolDigest("nonce_digest", issue.NonceDigest)
	if err != nil {
		return "", err
	}
	lumeraID := strings.TrimSpace(issue.LumeraID)
	if lumeraID == "" || lumeraID != issue.LumeraID {
		return "", fmt.Errorf("lumera_id is required and must be canonical")
	}
	description := strings.TrimSpace(issue.Description)
	if description == "" {
		description = fmt.Sprintf("Identity attestation challenge for %s", lumeraID)
	}

	return k.IssueChallenge(ctx, ProtocolChallengeIssue{
		ChallengeID:    issue.ChallengeID,
		Issuer:         issue.Issuer,
		Target:         lumeraID,
		ChallengeType:  types.ChallengeType_CHALLENGE_TYPE_IDENTITY_ATTESTATION,
		Title:          issue.Title,
		Description:    description,
		EvidenceDigest: nonceDigest,
		DeadlineHeight: issue.DeadlineHeight,
		DeadlineTime:   issue.DeadlineTime,
		EvidenceContract: &ProtocolEvidenceContract{
			Kind:        protocolEvidenceKindIdentityAttestation,
			SubjectID:   lumeraID,
			NonceDigest: nonceDigest,
		},
	})
}

func (k *Keeper) RespondIdentityAttestationChallenge(ctx context.Context, response IdentityAttestationChallengeResponse) error {
	lumeraID := strings.TrimSpace(response.LumeraID)
	if lumeraID == "" || lumeraID != response.LumeraID {
		return fmt.Errorf("lumera_id is required and must be canonical")
	}
	if strings.TrimSpace(response.Nonce) == "" {
		return fmt.Errorf("identity attestation nonce is required")
	}
	if strings.TrimSpace(response.SignatureHex) == "" {
		return fmt.Errorf("identity attestation signature is required")
	}
	if k.lumeraIDKeeper == nil {
		return fmt.Errorf("lumeraid keeper is not configured")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	record, err := k.GetProtocolChallengeRecord(ctx, strings.TrimSpace(response.ChallengeID))
	if err != nil {
		return err
	}
	if record.ChallengeType != types.ChallengeType_CHALLENGE_TYPE_IDENTITY_ATTESTATION {
		return fmt.Errorf("identity attestation response requires identity_attestation challenge")
	}
	if err := k.ensureProtocolResponseOpen(ctx, sdkCtx, record, lumeraID); err != nil {
		return err
	}
	if record.EvidenceContract == nil ||
		record.EvidenceContract.Kind != protocolEvidenceKindIdentityAttestation ||
		record.EvidenceContract.SubjectID != lumeraID {
		return fmt.Errorf("identity attestation evidence contract mismatch")
	}

	if err := k.lumeraIDKeeper.VerifyAndConsumeNonceSignature(ctx, lumeraID, response.Nonce, response.SignatureHex); err != nil {
		return fmt.Errorf("identity attestation evidence rejected: %w", err)
	}

	return k.RespondChallenge(ctx, ProtocolChallengeResponse{
		ChallengeID:    response.ChallengeID,
		Responder:      lumeraID,
		ResponseDigest: identityAttestationResponseDigest(response),
	})
}

func (k *Keeper) IssueTEEReportChallenge(ctx context.Context, issue TEEReportChallengeIssue) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	contract, err := normalizeTEEReportEvidenceContract(sdkCtx, issue)
	if err != nil {
		return "", err
	}
	evidenceDigest, err := protocolEvidenceContractDigest(contract)
	if err != nil {
		return "", err
	}

	description := strings.TrimSpace(issue.Description)
	if description == "" {
		description = fmt.Sprintf("TEE report challenge for %s", contract.SubjectID)
	}

	return k.IssueChallenge(ctx, ProtocolChallengeIssue{
		ChallengeID:      issue.ChallengeID,
		Issuer:           issue.Issuer,
		Target:           contract.SubjectID,
		ChallengeType:    types.ChallengeType_CHALLENGE_TYPE_TEE_REPORT,
		Title:            issue.Title,
		Description:      description,
		EvidenceDigest:   evidenceDigest,
		DeadlineHeight:   issue.DeadlineHeight,
		DeadlineTime:     issue.DeadlineTime,
		EvidenceContract: contract,
	})
}

func (k *Keeper) IssueChallenge(ctx context.Context, issue ProtocolChallengeIssue) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := validateProtocolIssue(sdkCtx, issue); err != nil {
		return "", err
	}
	registryRef, err := normalizeProtocolRegistryReference(issue)
	if err != nil {
		return "", err
	}
	evidenceContract, err := normalizeProtocolEvidenceContract(issue)
	if err != nil {
		return "", err
	}
	if challengeID := strings.TrimSpace(issue.ChallengeID); challengeID != "" {
		if _, err := k.GetChallenge(ctx, challengeID); err == nil {
			return "", fmt.Errorf("protocol challenge %s already exists", challengeID)
		} else if !errors.Is(err, ErrChallengeNotFound) {
			return "", err
		}
	}

	now := sdkCtx.BlockTime()
	ch := &types.Challenge{
		ChallengeId:     strings.TrimSpace(issue.ChallengeID),
		Title:           protocolChallengeTitle(issue),
		Description:     strings.TrimSpace(issue.Description),
		Creator:         strings.TrimSpace(issue.Issuer),
		ChallengeType:   issue.ChallengeType,
		Status:          types.ChallengeStatus_CHALLENGE_STATUS_DRAFT,
		CreatedAt:       now,
		StartsAt:        now,
		EndsAt:          issue.DeadlineTime,
		MaxParticipants: 1,
	}
	challengeID, err := k.CreateChallenge(ctx, ch)
	if err != nil {
		return "", err
	}

	if err := k.RegisterParticipant(ctx, &types.Participant{
		ChallengeId:  challengeID,
		ToolId:       strings.TrimSpace(issue.Target),
		PublisherId:  strings.TrimSpace(issue.Target),
		RegisteredAt: now,
	}); err != nil {
		return "", fmt.Errorf("register protocol challenge target: %w", err)
	}

	stored, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return "", err
	}
	stored.Status = types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE
	if err := k.UpdateChallenge(ctx, stored); err != nil {
		return "", fmt.Errorf("activate protocol challenge: %w", err)
	}

	record := ProtocolChallengeRecord{
		ChallengeID:       challengeID,
		Issuer:            strings.TrimSpace(issue.Issuer),
		Target:            strings.TrimSpace(issue.Target),
		ChallengeType:     issue.ChallengeType,
		IssueHeight:       sdkCtx.BlockHeight(),
		IssueTimeUnix:     sdkCtx.BlockTime().Unix(),
		DeadlineHeight:    issue.DeadlineHeight,
		DeadlineTimeUnix:  issue.DeadlineTime.Unix(),
		EvidenceDigest:    strings.TrimSpace(issue.EvidenceDigest),
		Outcome:           ProtocolChallengeOutcomeIssued,
		RegistryReference: registryRef,
		EvidenceContract:  evidenceContract,
	}
	if err := k.setProtocolChallengeRecord(ctx, record); err != nil {
		return "", err
	}
	if err := k.recordSLOProbeChallengeIssued(ctx, record); err != nil {
		return "", err
	}

	k.emitProtocolChallengeEvent(sdkCtx, types.EventTypeProtocolChallengeIssued, record)
	return challengeID, nil
}

func (k *Keeper) RespondChallenge(ctx context.Context, response ProtocolChallengeResponse) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	record, err := k.GetProtocolChallengeRecord(ctx, strings.TrimSpace(response.ChallengeID))
	if err != nil {
		return err
	}
	if strings.TrimSpace(response.ResponseDigest) == "" {
		return errors.New("response_digest is required")
	}
	if err := k.ensureProtocolResponseOpen(ctx, sdkCtx, record, strings.TrimSpace(response.Responder)); err != nil {
		return err
	}
	ch, err := k.GetChallenge(ctx, record.ChallengeID)
	if err != nil {
		return err
	}

	if err := k.RecordSubmission(ctx, &types.Submission{
		ChallengeId:          record.ChallengeID,
		ToolId:               record.Target,
		BlockHeight:          sdkCtx.BlockHeight(),
		SubmittedAt:          sdkCtx.BlockTime(),
		GoldenTaskResultHash: strings.TrimSpace(response.ResponseDigest),
	}); err != nil {
		return fmt.Errorf("record protocol challenge response: %w", err)
	}

	ch.Status = types.ChallengeStatus_CHALLENGE_STATUS_COMPLETED
	ch.ScoredAt = sdkCtx.BlockTime()
	if err := k.UpdateChallenge(ctx, ch); err != nil {
		return fmt.Errorf("complete protocol challenge: %w", err)
	}

	record.ResponseDigest = strings.TrimSpace(response.ResponseDigest)
	record.RespondedHeight = sdkCtx.BlockHeight()
	record.RespondedAtUnix = sdkCtx.BlockTime().Unix()
	record.Outcome = ProtocolChallengeOutcomeResponded
	if err := k.setProtocolChallengeRecord(ctx, *record); err != nil {
		return err
	}
	if err := k.recordSLOProbeChallengeOutcome(ctx, *record); err != nil {
		return err
	}

	k.emitProtocolChallengeEvent(sdkCtx, types.EventTypeProtocolChallengeResponded, *record)
	return nil
}

func (k *Keeper) ensureProtocolResponseOpen(ctx context.Context, sdkCtx sdk.Context, record *ProtocolChallengeRecord, responder string) error {
	if responder != record.Target {
		return fmt.Errorf("%w: responder is not challenge target", types.ErrUnauthorized)
	}
	if record.ResponseDigest != "" {
		return fmt.Errorf("protocol challenge response already recorded for %s", record.ChallengeID)
	}
	if protocolChallengePastDeadline(sdkCtx, record) {
		return fmt.Errorf("%w: protocol challenge deadline passed", ErrChallengeNotActive)
	}
	ch, err := k.GetChallenge(ctx, record.ChallengeID)
	if err != nil {
		return err
	}
	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE {
		return fmt.Errorf("%w: expected ACTIVE, got %s", ErrChallengeNotActive, ch.Status)
	}
	return nil
}

func (k *Keeper) ExpireChallenge(ctx context.Context, challengeID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	record, err := k.GetProtocolChallengeRecord(ctx, strings.TrimSpace(challengeID))
	if err != nil {
		return err
	}
	if record.ResponseDigest != "" {
		return fmt.Errorf("protocol challenge response already recorded for %s", record.ChallengeID)
	}
	if !protocolChallengePastDeadline(sdkCtx, record) {
		return fmt.Errorf("%w: protocol challenge deadline has not passed", ErrChallengeNotActive)
	}

	ch, err := k.GetChallenge(ctx, record.ChallengeID)
	if err != nil {
		return err
	}
	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE {
		return fmt.Errorf("%w: expected ACTIVE, got %s", ErrChallengeNotActive, ch.Status)
	}

	ch.Status = types.ChallengeStatus_CHALLENGE_STATUS_CANCELLED
	if err := k.UpdateChallenge(ctx, ch); err != nil {
		return fmt.Errorf("expire protocol challenge: %w", err)
	}

	record.Outcome = ProtocolChallengeOutcomeExpired
	if err := k.setProtocolChallengeRecord(ctx, *record); err != nil {
		return err
	}
	if err := k.recordSLOProbeChallengeOutcome(ctx, *record); err != nil {
		return err
	}
	k.observeChallengeExpired(ch)
	k.emitProtocolChallengeEvent(sdkCtx, types.EventTypeProtocolChallengeExpired, *record)
	return nil
}

func (k *Keeper) ListActiveChallenges(ctx context.Context, challengeType types.ChallengeType) ([]ProtocolChallengeRecord, error) {
	if challengeType != types.ChallengeType_CHALLENGE_TYPE_UNSPECIFIED && !types.IsProtocolChallengeType(challengeType) {
		return nil, fmt.Errorf("challenge_type must be unspecified or a protocol challenge type")
	}

	active, err := k.GetChallengesByStatus(ctx, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE)
	if err != nil {
		return nil, err
	}

	records := make([]ProtocolChallengeRecord, 0, len(active))
	for _, ch := range active {
		if ch == nil || !types.IsProtocolChallengeType(ch.ChallengeType) {
			continue
		}
		if challengeType != types.ChallengeType_CHALLENGE_TYPE_UNSPECIFIED && ch.ChallengeType != challengeType {
			continue
		}
		record, recordErr := k.GetProtocolChallengeRecord(ctx, ch.ChallengeId)
		if recordErr != nil {
			if errors.Is(recordErr, collections.ErrNotFound) {
				continue
			}
			return nil, recordErr
		}
		records = append(records, *record)
	}
	return records, nil
}

func (k *Keeper) GetProtocolChallengeRecord(ctx context.Context, challengeID string) (*ProtocolChallengeRecord, error) {
	raw, err := k.protocolLifecycle.Get(ctx, challengeID)
	if err != nil {
		return nil, err
	}
	var record ProtocolChallengeRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil, fmt.Errorf("decode protocol lifecycle record %s: %w", challengeID, err)
	}
	return &record, nil
}

func (k *Keeper) setProtocolChallengeRecord(ctx context.Context, record ProtocolChallengeRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode protocol lifecycle record %s: %w", record.ChallengeID, err)
	}
	return k.protocolLifecycle.Set(ctx, record.ChallengeID, string(raw))
}

func normalizeProtocolRegistryReference(issue ProtocolChallengeIssue) (*ProtocolRegistryReference, error) {
	if issue.RegistryReference == nil {
		return nil, nil
	}
	if issue.ChallengeType != types.ChallengeType_CHALLENGE_TYPE_SLO_PROBE {
		return nil, fmt.Errorf("registry_reference is only supported for slo_probe challenges")
	}
	ref, err := normalizeSLOProbeRegistryReference(*issue.RegistryReference)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(issue.Target) != ref.ToolID {
		return nil, fmt.Errorf("registry_reference tool_id must match target")
	}
	return ref, nil
}

func normalizeProtocolEvidenceContract(issue ProtocolChallengeIssue) (*ProtocolEvidenceContract, error) {
	if issue.EvidenceContract == nil {
		return nil, nil
	}
	contract := *issue.EvidenceContract
	contract.Kind = strings.TrimSpace(contract.Kind)
	contract.SubjectID = strings.TrimSpace(contract.SubjectID)
	switch issue.ChallengeType {
	case types.ChallengeType_CHALLENGE_TYPE_IDENTITY_ATTESTATION:
		if contract.Kind != protocolEvidenceKindIdentityAttestation {
			return nil, fmt.Errorf("identity attestation evidence contract kind mismatch")
		}
		if contract.SubjectID != strings.TrimSpace(issue.Target) {
			return nil, fmt.Errorf("identity attestation evidence subject mismatch")
		}
		nonceDigest, err := normalizeProtocolDigest("nonce_digest", contract.NonceDigest)
		if err != nil {
			return nil, err
		}
		contract.NonceDigest = nonceDigest
	case types.ChallengeType_CHALLENGE_TYPE_TEE_REPORT:
		if contract.Kind != protocolEvidenceKindTEEReport {
			return nil, fmt.Errorf("tee report evidence contract kind mismatch")
		}
		if contract.SubjectID != strings.TrimSpace(issue.Target) {
			return nil, fmt.Errorf("tee report evidence subject mismatch")
		}
		validated, err := validateTEEReportEvidenceContract(contract)
		if err != nil {
			return nil, err
		}
		contract = validated
	default:
		return nil, fmt.Errorf("evidence_contract is only supported for identity_attestation and tee_report challenges")
	}
	return &contract, nil
}

func normalizeSLOProbeRegistryReference(ref ProtocolRegistryReference) (*ProtocolRegistryReference, error) {
	clean := ProtocolRegistryReference{
		SourceModule: strings.TrimSpace(ref.SourceModule),
		ToolID:       strings.TrimSpace(ref.ToolID),
		TargetKind:   strings.TrimSpace(ref.TargetKind),
		TargetID:     strings.TrimSpace(ref.TargetID),
		Reason:       strings.TrimSpace(ref.Reason),
	}
	if clean.SourceModule == "" {
		clean.SourceModule = "registry"
	}
	if clean.SourceModule != "registry" {
		return nil, fmt.Errorf("registry_reference source_module must be registry")
	}
	if clean.ToolID == "" {
		return nil, fmt.Errorf("registry_reference tool_id is required")
	}
	if clean.TargetID == "" {
		return nil, fmt.Errorf("registry_reference target_id is required")
	}
	if !isSLOProbeChallengeTargetKind(clean.TargetKind) {
		return nil, fmt.Errorf("registry_reference target_kind must be %s or %s", SLOProbeChallengeTargetSubmission, SLOProbeChallengeTargetAggregate)
	}
	if !isCanonicalSLOProbeChallengeReason(clean.Reason) {
		return nil, fmt.Errorf("registry_reference reason is not a canonical SLO probe challenge reason")
	}
	return &clean, nil
}

func normalizeTEEReportEvidenceContract(ctx sdk.Context, issue TEEReportChallengeIssue) (*ProtocolEvidenceContract, error) {
	toolID, err := normalizeProtocolPublicID("tool_id", issue.ToolID)
	if err != nil {
		return nil, err
	}
	reportDigest, err := normalizeProtocolDigest("report_digest", issue.ReportDigest)
	if err != nil {
		return nil, err
	}
	verifierResultDigest, err := normalizeProtocolDigest("verifier_result_digest", issue.VerifierResultDigest)
	if err != nil {
		return nil, err
	}
	measurementDigest, err := normalizeProtocolDigest("measurement_digest", issue.MeasurementDigest)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(issue.ExpectedMeasurementDigest) != "" {
		expected, err := normalizeProtocolDigest("expected_measurement_digest", issue.ExpectedMeasurementDigest)
		if err != nil {
			return nil, err
		}
		if !sameProtocolEvidenceValue(measurementDigest, expected) {
			return nil, fmt.Errorf("tee report measurement digest does not match policy")
		}
	}
	provider, err := normalizeProtocolProvider("provider", issue.Provider)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(issue.ExpectedProvider) != "" {
		expected, err := normalizeProtocolProvider("expected_provider", issue.ExpectedProvider)
		if err != nil {
			return nil, err
		}
		if !sameProtocolEvidenceValue(provider, expected) {
			return nil, fmt.Errorf("tee report provider does not match policy")
		}
	}
	policyDigest, err := normalizeProtocolDigest("policy_digest", issue.PolicyDigest)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(issue.VerifierPolicyDigest) != "" {
		verifierPolicy, err := normalizeProtocolDigest("verifier_policy_digest", issue.VerifierPolicyDigest)
		if err != nil {
			return nil, err
		}
		if !sameProtocolEvidenceValue(verifierPolicy, policyDigest) {
			return nil, fmt.Errorf("tee report verifier policy digest does not match policy")
		}
	}
	receiptID, err := normalizeProtocolPublicID("receipt_id", issue.ReceiptID)
	if err != nil {
		return nil, err
	}
	traceID, err := normalizeProtocolPublicID("trace_id", issue.TraceID)
	if err != nil {
		return nil, err
	}
	if issue.VerifiedAt.IsZero() {
		return nil, fmt.Errorf("verified_at is required")
	}
	verifiedAt := issue.VerifiedAt.UTC()
	now := ctx.BlockTime().UTC()
	if verifiedAt.After(now.Add(time.Minute)) {
		return nil, fmt.Errorf("verified_at is in the future")
	}
	freshness := issue.MaxVerifierResultFreshness
	if freshness <= 0 {
		freshness = defaultTEEVerifierFreshness
	}
	if now.Sub(verifiedAt) > freshness {
		return nil, fmt.Errorf("tee report verifier result is stale")
	}
	if issue.VerifierResultExpiresAt.IsZero() || !issue.VerifierResultExpiresAt.After(now) {
		return nil, fmt.Errorf("tee report verifier result is stale")
	}

	return &ProtocolEvidenceContract{
		Kind:                 protocolEvidenceKindTEEReport,
		SubjectID:            toolID,
		ReportDigest:         reportDigest,
		VerifierResultDigest: verifierResultDigest,
		MeasurementDigest:    measurementDigest,
		Provider:             provider,
		PolicyDigest:         policyDigest,
		ReceiptID:            receiptID,
		TraceID:              traceID,
		VerifiedAtUnix:       verifiedAt.Unix(),
		ResultExpiresAtUnix:  issue.VerifierResultExpiresAt.UTC().Unix(),
	}, nil
}

func validateTEEReportEvidenceContract(contract ProtocolEvidenceContract) (ProtocolEvidenceContract, error) {
	if contract.Kind != protocolEvidenceKindTEEReport {
		return contract, fmt.Errorf("tee report evidence contract kind mismatch")
	}
	var err error
	if contract.SubjectID, err = normalizeProtocolPublicID("subject_id", contract.SubjectID); err != nil {
		return contract, err
	}
	if contract.ReportDigest, err = normalizeProtocolDigest("report_digest", contract.ReportDigest); err != nil {
		return contract, err
	}
	if contract.VerifierResultDigest, err = normalizeProtocolDigest("verifier_result_digest", contract.VerifierResultDigest); err != nil {
		return contract, err
	}
	if contract.MeasurementDigest, err = normalizeProtocolDigest("measurement_digest", contract.MeasurementDigest); err != nil {
		return contract, err
	}
	if contract.Provider, err = normalizeProtocolProvider("provider", contract.Provider); err != nil {
		return contract, err
	}
	if contract.PolicyDigest, err = normalizeProtocolDigest("policy_digest", contract.PolicyDigest); err != nil {
		return contract, err
	}
	if contract.ReceiptID, err = normalizeProtocolPublicID("receipt_id", contract.ReceiptID); err != nil {
		return contract, err
	}
	if contract.TraceID, err = normalizeProtocolPublicID("trace_id", contract.TraceID); err != nil {
		return contract, err
	}
	if contract.VerifiedAtUnix <= 0 {
		return contract, fmt.Errorf("verified_at_unix is required")
	}
	if contract.ResultExpiresAtUnix <= 0 {
		return contract, fmt.Errorf("result_expires_at_unix is required")
	}
	return contract, nil
}

func normalizeProtocolDigest(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return "", fmt.Errorf("%s is required and must be canonical", field)
	}
	algorithm, hexDigest, ok := strings.Cut(trimmed, ":")
	if !ok {
		return "", fmt.Errorf("%s must be blake3:<64 hex> or sha256:<64 hex>", field)
	}
	switch algorithm {
	case "blake3", "sha256":
	default:
		return "", fmt.Errorf("%s must use blake3 or sha256", field)
	}
	if len(hexDigest) != 64 {
		return "", fmt.Errorf("%s must contain a 64-byte hex digest", field)
	}
	decoded, err := hex.DecodeString(hexDigest)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("%s must contain a 64-byte hex digest", field)
	}
	return algorithm + ":" + strings.ToLower(hexDigest), nil
}

func normalizeProtocolPublicID(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return "", fmt.Errorf("%s is required and must be canonical", field)
	}
	if len(trimmed) > types.MaxChallengeIDLen {
		return "", fmt.Errorf("%s length exceeds maximum %d", field, types.MaxChallengeIDLen)
	}
	if protocolEvidenceContainsNonPublicDetail(trimmed) {
		return "", fmt.Errorf("%s contains non-public evidence detail", field)
	}
	return trimmed, nil
}

func normalizeProtocolProvider(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return "", fmt.Errorf("%s is required and must be canonical", field)
	}
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "", fmt.Errorf("%s must be lowercase provider identifier", field)
	}
	if protocolEvidenceContainsNonPublicDetail(trimmed) {
		return "", fmt.Errorf("%s contains non-public evidence detail", field)
	}
	return trimmed, nil
}

func sameProtocolEvidenceValue(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func protocolEvidenceContainsNonPublicDetail(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"://",
		"@",
		"api_key=",
		"apikey=",
		"authorization=",
		"bearer ",
		"cookie=",
		"host=",
		"hostname=",
		"ip=",
		"password=",
		"secret=",
		"token=",
		"topology",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func protocolEvidenceContractDigest(contract *ProtocolEvidenceContract) (string, error) {
	if contract == nil {
		return "", fmt.Errorf("evidence contract is required")
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		return "", fmt.Errorf("encode evidence contract: %w", err)
	}
	sum := blake3.Sum256(raw)
	return "blake3:" + hex.EncodeToString(sum[:]), nil
}

func identityAttestationResponseDigest(response IdentityAttestationChallengeResponse) string {
	payload := struct {
		Kind            string `json:"kind"`
		ChallengeID     string `json:"challenge_id"`
		LumeraID        string `json:"lumera_id"`
		NonceDigest     string `json:"nonce_digest"`
		SignatureDigest string `json:"signature_digest"`
	}{
		Kind:            protocolEvidenceKindIdentityAttestation,
		ChallengeID:     strings.TrimSpace(response.ChallengeID),
		LumeraID:        strings.TrimSpace(response.LumeraID),
		NonceDigest:     protocolStringDigest(strings.TrimSpace(response.Nonce)),
		SignatureDigest: protocolStringDigest(strings.TrimSpace(response.SignatureHex)),
	}
	raw, _ := json.Marshal(payload)
	sum := blake3.Sum256(raw)
	return "blake3:" + hex.EncodeToString(sum[:])
}

func protocolStringDigest(value string) string {
	sum := blake3.Sum256([]byte(value))
	return "blake3:" + hex.EncodeToString(sum[:])
}

func isSLOProbeChallengeTargetKind(kind string) bool {
	switch kind {
	case SLOProbeChallengeTargetSubmission, SLOProbeChallengeTargetAggregate:
		return true
	default:
		return false
	}
}

func isCanonicalSLOProbeChallengeReason(reason string) bool {
	switch reason {
	case "signature_mismatch",
		"bundle_digest_mismatch",
		"window_misaligned",
		"sample_out_of_window",
		"duplicate_or_missing_sample",
		"metric_recalculation_mismatch",
		"aggregate_included_invalid_submission":
		return true
	default:
		return false
	}
}

func (r ProtocolChallengeRecord) SLOProbeChallengeReference() (types.SLOProbeChallengeReference, bool) {
	if r.ChallengeType != types.ChallengeType_CHALLENGE_TYPE_SLO_PROBE || r.RegistryReference == nil {
		return types.SLOProbeChallengeReference{}, false
	}
	return types.SLOProbeChallengeReference{
		ChallengeID:    r.ChallengeID,
		ToolID:         r.RegistryReference.ToolID,
		TargetKind:     r.RegistryReference.TargetKind,
		TargetID:       r.RegistryReference.TargetID,
		Reason:         r.RegistryReference.Reason,
		EvidenceDigest: r.EvidenceDigest,
		Outcome:        string(r.Outcome),
		ResponseDigest: r.ResponseDigest,
	}, true
}

func (k *Keeper) recordSLOProbeChallengeIssued(ctx context.Context, record ProtocolChallengeRecord) error {
	if k.registryKeeper == nil {
		return nil
	}
	ref, ok := record.SLOProbeChallengeReference()
	if !ok {
		return nil
	}
	return k.registryKeeper.RecordSLOProbeChallengeIssued(ctx, ref)
}

func (k *Keeper) recordSLOProbeChallengeOutcome(ctx context.Context, record ProtocolChallengeRecord) error {
	if k.registryKeeper == nil {
		return nil
	}
	ref, ok := record.SLOProbeChallengeReference()
	if !ok {
		return nil
	}
	return k.registryKeeper.RecordSLOProbeChallengeOutcome(ctx, ref)
}

func validateProtocolIssue(ctx sdk.Context, issue ProtocolChallengeIssue) error {
	if strings.TrimSpace(issue.Issuer) == "" {
		return errors.New("issuer is required")
	}
	if strings.TrimSpace(issue.Target) == "" {
		return errors.New("target is required")
	}
	if !types.IsProtocolChallengeType(issue.ChallengeType) {
		return fmt.Errorf("challenge_type must be a protocol challenge type")
	}
	if strings.TrimSpace(issue.EvidenceDigest) == "" {
		return errors.New("evidence_digest is required")
	}
	if issue.DeadlineHeight <= ctx.BlockHeight() {
		return fmt.Errorf("deadline_height must be greater than current block height")
	}
	if issue.DeadlineTime.IsZero() || !issue.DeadlineTime.After(ctx.BlockTime()) {
		return fmt.Errorf("deadline_time must be after current block time")
	}
	return nil
}

func protocolChallengeTitle(issue ProtocolChallengeIssue) string {
	if title := strings.TrimSpace(issue.Title); title != "" {
		return title
	}
	return fmt.Sprintf("%s challenge for %s", types.ChallengeTypeLabel(issue.ChallengeType), strings.TrimSpace(issue.Target))
}

func protocolChallengePastDeadline(ctx sdk.Context, record *ProtocolChallengeRecord) bool {
	if record == nil {
		return false
	}
	return ctx.BlockHeight() > record.DeadlineHeight ||
		!ctx.BlockTime().Before(time.Unix(record.DeadlineTimeUnix, 0).UTC())
}

func (k *Keeper) emitProtocolChallengeEvent(ctx sdk.Context, eventType string, record ProtocolChallengeRecord) {
	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyChallengeID, record.ChallengeID),
		sdk.NewAttribute(types.AttributeKeyChallengeClass, types.ChallengeTypeLabel(record.ChallengeType)),
		sdk.NewAttribute(types.AttributeKeyIssuer, record.Issuer),
		sdk.NewAttribute(types.AttributeKeyTarget, record.Target),
		sdk.NewAttribute(types.AttributeKeyIssueHeight, strconv.FormatInt(record.IssueHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyDeadlineHeight, strconv.FormatInt(record.DeadlineHeight, 10)),
		sdk.NewAttribute(types.AttributeKeyEvidenceDigest, record.EvidenceDigest),
		sdk.NewAttribute(types.AttributeKeyResponseDigest, record.ResponseDigest),
		sdk.NewAttribute(types.AttributeKeyOutcome, string(record.Outcome)),
	}
	if record.RegistryReference != nil {
		attrs = append(attrs,
			sdk.NewAttribute(types.AttributeKeyToolID, record.RegistryReference.ToolID),
			sdk.NewAttribute(types.AttributeKeyReason, record.RegistryReference.Reason),
			sdk.NewAttribute("target_kind", record.RegistryReference.TargetKind),
			sdk.NewAttribute("target_id", record.RegistryReference.TargetID),
		)
	}
	if record.EvidenceContract != nil {
		attrs = append(attrs,
			sdk.NewAttribute("evidence_contract_kind", record.EvidenceContract.Kind),
			sdk.NewAttribute("evidence_subject_id", record.EvidenceContract.SubjectID),
			sdk.NewAttribute("nonce_digest", record.EvidenceContract.NonceDigest),
			sdk.NewAttribute("report_digest", record.EvidenceContract.ReportDigest),
			sdk.NewAttribute("verifier_result_digest", record.EvidenceContract.VerifierResultDigest),
			sdk.NewAttribute("measurement_digest", record.EvidenceContract.MeasurementDigest),
			sdk.NewAttribute("provider", record.EvidenceContract.Provider),
			sdk.NewAttribute("policy_digest", record.EvidenceContract.PolicyDigest),
			sdk.NewAttribute("receipt_id", record.EvidenceContract.ReceiptID),
			sdk.NewAttribute("trace_id", record.EvidenceContract.TraceID),
			sdk.NewAttribute("verified_at_unix", strconv.FormatInt(record.EvidenceContract.VerifiedAtUnix, 10)),
			sdk.NewAttribute("result_expires_at_unix", strconv.FormatInt(record.EvidenceContract.ResultExpiresAtUnix, 10)),
		)
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(eventType, attrs...))
}
