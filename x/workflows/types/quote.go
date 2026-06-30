package types

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"cosmossdk.io/math"
	"github.com/cloudflare/circl/sign/ed448"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gowebpki/jcs"
	"lukechampine.com/blake3"
)

const (
	// DefaultBundleQuoteTTL is the router-side default validity window for workflow bundle quotes.
	DefaultBundleQuoteTTL = 2 * time.Minute
	// MaxPassportReputationScore is the WorkflowCard schema reputation ceiling.
	MaxPassportReputationScore = 1000
)

// QuoteWorkflowRequest contains the deterministic inputs needed to produce a BundleQuote.
type QuoteWorkflowRequest struct {
	WorkflowID            string
	Version               string
	Inputs                json.RawMessage
	CallerPassportTier    string
	CallerPassportActive  bool
	CallerReputationScore uint32
	CallerPassportBadges  []string
	Nonce                 string
	Validity              time.Duration
	RouterPrivateKey      ed448.PrivateKey
}

// QuoteCoin is the JSON-stable coin representation used in signed bundle quotes.
type QuoteCoin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// BundleStepQuote pins a WorkflowCard DAG step to a concrete tool version and bound.
type BundleStepQuote struct {
	StepID      string    `json:"step_id"`
	ToolID      string    `json:"tool_id"`
	ToolVersion string    `json:"tool_version"`
	SubMaxCost  QuoteCoin `json:"sub_max_cost"`
	SubSloP95Ms uint32    `json:"sub_slo_p95_ms"`
}

// BundleQuote is the signed quote returned by quote_workflow.
type BundleQuote struct {
	BundleID              string            `json:"bundle_id"`
	WorkflowID            string            `json:"workflow_id"`
	Version               string            `json:"version"`
	InputsHash            string            `json:"inputs_hash"`
	CallerPassportTier    string            `json:"caller_passport_tier"`
	CallerPassportActive  bool              `json:"caller_passport_active"`
	CallerReputationScore uint32            `json:"caller_reputation_score"`
	CallerPassportBadges  []string          `json:"caller_passport_badges"`
	Nonce                 string            `json:"nonce"`
	StepQuotes            []BundleStepQuote `json:"step_quotes"`
	TotalMaxCost          QuoteCoin         `json:"total_max_cost"`
	TotalSloP95Ms         uint32            `json:"total_slo_p95_ms"`
	AnchoredHeight        int64             `json:"anchored_height"`
	ExpiresAt             string            `json:"expires_at"`
	RouterPubkey          string            `json:"router_pubkey"`
	Signed                string            `json:"signed,omitempty"`
}

// BundleQuoteRecord stores quote replay state in the workflows module.
type BundleQuoteRecord struct {
	BundleID      string                    `json:"bundle_id"`
	Quote         *BundleQuote              `json:"quote,omitempty"`
	Consumed      bool                      `json:"consumed,omitempty"`
	ExpiresAt     string                    `json:"expires_at"`
	UpdatedHeight int64                     `json:"updated_height"`
	Invocation    *WorkflowInvocationRecord `json:"invocation,omitempty"`
}

type bundleQuoteSignaturePayload struct {
	BundleID              string            `json:"bundle_id"`
	WorkflowID            string            `json:"workflow_id"`
	Version               string            `json:"version"`
	InputsHash            string            `json:"inputs_hash"`
	CallerPassportTier    string            `json:"caller_passport_tier"`
	CallerPassportActive  bool              `json:"caller_passport_active"`
	CallerReputationScore uint32            `json:"caller_reputation_score"`
	CallerPassportBadges  []string          `json:"caller_passport_badges"`
	Nonce                 string            `json:"nonce"`
	StepQuotes            []BundleStepQuote `json:"step_quotes"`
	TotalMaxCost          QuoteCoin         `json:"total_max_cost"`
	TotalSloP95Ms         uint32            `json:"total_slo_p95_ms"`
	AnchoredHeight        int64             `json:"anchored_height"`
	ExpiresAt             string            `json:"expires_at"`
	RouterPubkey          string            `json:"router_pubkey"`
}

type bundleQuoteIDPayload struct {
	WorkflowID            string            `json:"workflow_id"`
	Version               string            `json:"version"`
	InputsHash            string            `json:"inputs_hash"`
	CallerPassportTier    string            `json:"caller_passport_tier"`
	CallerPassportActive  bool              `json:"caller_passport_active"`
	CallerReputationScore uint32            `json:"caller_reputation_score"`
	CallerPassportBadges  []string          `json:"caller_passport_badges"`
	Nonce                 string            `json:"nonce"`
	StepQuotes            []BundleStepQuote `json:"step_quotes"`
	TotalMaxCost          QuoteCoin         `json:"total_max_cost"`
	TotalSloP95Ms         uint32            `json:"total_slo_p95_ms"`
	AnchoredHeight        int64             `json:"anchored_height"`
	ExpiresAt             string            `json:"expires_at"`
	RouterPubkey          string            `json:"router_pubkey"`
}

// NewQuoteCoin validates a quote coin and returns its canonical representation.
func NewQuoteCoin(denom string, amount string) (QuoteCoin, error) {
	rawDenom := denom
	denom = strings.TrimSpace(denom)
	if denom == "" {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrap("quote coin denom is required")
	}
	if denom != rawDenom {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrapf("quote coin denom must be canonical: %q", rawDenom)
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrapf("quote coin denom is invalid: %v", err)
	}
	rawAmount := amount
	amount = strings.TrimSpace(amount)
	if amount != rawAmount {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrapf("quote coin amount must be canonical: %q", rawAmount)
	}
	if amount != "0" && strings.HasPrefix(amount, "0") {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrapf("quote coin amount must be canonical: %q", rawAmount)
	}
	parsed, ok := math.NewIntFromString(amount)
	if !ok {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrap("quote coin amount must be an integer")
	}
	if parsed.IsNegative() {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrap("quote coin amount cannot be negative")
	}
	if parsed.String() != amount {
		return QuoteCoin{}, ErrInvalidWorkflow.Wrapf("quote coin amount must be canonical: %q", rawAmount)
	}
	return QuoteCoin{Denom: denom, Amount: parsed.String()}, nil
}

// CanonicalQuoteInputs returns the JCS-canonical byte representation of request inputs.
func CanonicalQuoteInputs(inputs json.RawMessage) ([]byte, error) {
	raw := strings.TrimSpace(string(inputs))
	if raw == "" {
		raw = "{}"
	}
	if !json.Valid([]byte(raw)) {
		return nil, ErrInvalidWorkflow.Wrap("quote inputs must be valid JSON")
	}
	canonical, err := jcs.Transform([]byte(raw))
	if err != nil {
		return nil, ErrInvalidWorkflow.Wrapf("canonicalize quote inputs: %v", err)
	}
	return canonical, nil
}

// QuoteInputsHash returns the BLAKE3 digest of the canonical request inputs.
func QuoteInputsHash(inputs json.RawMessage) (string, error) {
	canonical, err := CanonicalQuoteInputs(inputs)
	if err != nil {
		return "", err
	}
	sum := blake3.Sum256(canonical)
	return "blake3:" + hex.EncodeToString(sum[:]), nil
}

// NormalizePassportBadges returns deterministic badge slugs for quote evidence and signing.
func NormalizePassportBadges(raw []string) []string {
	if len(raw) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, badge := range raw {
		badge = strings.ToLower(strings.TrimSpace(badge))
		if badge == "" {
			continue
		}
		if _, ok := seen[badge]; ok {
			continue
		}
		seen[badge] = struct{}{}
		out = append(out, badge)
	}
	sort.Strings(out)
	return out
}

// RouterPubkeyFromPrivateKey renders the Ed448 public key corresponding to priv.
func RouterPubkeyFromPrivateKey(priv ed448.PrivateKey) (string, error) {
	if len(priv) != ed448.PrivateKeySize {
		return "", ErrInvalidWorkflow.Wrapf("router private key must be %d bytes", ed448.PrivateKeySize)
	}
	pub, ok := priv.Public().(ed448.PublicKey)
	if !ok || len(pub) != ed448.PublicKeySize {
		return "", ErrInvalidWorkflow.Wrap("derive router Ed448 public key")
	}
	return "ed448:" + hex.EncodeToString(pub), nil
}

// ComputeBundleQuoteID derives a UUID-shaped, deterministic bundle id from unsigned quote contents.
func ComputeBundleQuoteID(q *BundleQuote) (string, error) {
	if q == nil {
		return "", ErrInvalidWorkflow.Wrap("bundle quote cannot be nil")
	}
	if err := q.validateBasic(false, false); err != nil {
		return "", err
	}
	payload := bundleQuoteIDPayload{
		WorkflowID:            strings.TrimSpace(q.WorkflowID),
		Version:               strings.TrimSpace(q.Version),
		InputsHash:            strings.TrimSpace(q.InputsHash),
		CallerPassportTier:    strings.TrimSpace(q.CallerPassportTier),
		CallerPassportActive:  q.CallerPassportActive,
		CallerReputationScore: q.CallerReputationScore,
		CallerPassportBadges:  NormalizePassportBadges(q.CallerPassportBadges),
		Nonce:                 strings.TrimSpace(q.Nonce),
		StepQuotes:            normalizedStepQuotes(q.StepQuotes),
		TotalMaxCost:          q.TotalMaxCost,
		TotalSloP95Ms:         q.TotalSloP95Ms,
		AnchoredHeight:        q.AnchoredHeight,
		ExpiresAt:             strings.TrimSpace(q.ExpiresAt),
		RouterPubkey:          strings.TrimSpace(q.RouterPubkey),
	}
	canonical, err := canonicalJSON(payload)
	if err != nil {
		return "", err
	}
	sum := blake3.Sum256(canonical)
	id := sum[:16]
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16]), nil
}

// CanonicalBytes returns the JCS-canonical bytes signed by the router.
func (q *BundleQuote) CanonicalBytes() ([]byte, error) {
	if q == nil {
		return nil, ErrInvalidWorkflow.Wrap("bundle quote cannot be nil")
	}
	payload := bundleQuoteSignaturePayload{
		BundleID:              strings.TrimSpace(q.BundleID),
		WorkflowID:            strings.TrimSpace(q.WorkflowID),
		Version:               strings.TrimSpace(q.Version),
		InputsHash:            strings.TrimSpace(q.InputsHash),
		CallerPassportTier:    strings.TrimSpace(q.CallerPassportTier),
		CallerPassportActive:  q.CallerPassportActive,
		CallerReputationScore: q.CallerReputationScore,
		CallerPassportBadges:  NormalizePassportBadges(q.CallerPassportBadges),
		Nonce:                 strings.TrimSpace(q.Nonce),
		StepQuotes:            normalizedStepQuotes(q.StepQuotes),
		TotalMaxCost:          q.TotalMaxCost,
		TotalSloP95Ms:         q.TotalSloP95Ms,
		AnchoredHeight:        q.AnchoredHeight,
		ExpiresAt:             strings.TrimSpace(q.ExpiresAt),
		RouterPubkey:          strings.TrimSpace(q.RouterPubkey),
	}
	return canonicalJSON(payload)
}

// SignBundleQuote signs q in place with an Ed448 router key.
func SignBundleQuote(q *BundleQuote, priv ed448.PrivateKey) error {
	if q == nil {
		return ErrInvalidWorkflow.Wrap("bundle quote cannot be nil")
	}
	pubkey, err := RouterPubkeyFromPrivateKey(priv)
	if err != nil {
		return err
	}
	q.RouterPubkey = pubkey
	q.Signed = ""
	if err := q.validateBasic(true, false); err != nil {
		return err
	}
	canonical, err := q.CanonicalBytes()
	if err != nil {
		return err
	}
	q.Signed = "ed448:" + hex.EncodeToString(ed448.Sign(priv, canonical, ""))
	return nil
}

// VerifyBundleQuoteSignature verifies q's Ed448 signature against expectedRouterPubkey.
func VerifyBundleQuoteSignature(q *BundleQuote, expectedRouterPubkey string) error {
	if q == nil {
		return ErrInvalidWorkflow.Wrap("bundle quote cannot be nil")
	}
	if err := q.ValidateBasic(); err != nil {
		return err
	}
	if err := validateBundleQuoteID(q); err != nil {
		return err
	}
	if err := ensureEd448PublicKeyFieldMatches("bundle quote router pubkey", expectedRouterPubkey, q.RouterPubkey); err != nil {
		return err
	}
	pubkeyText := strings.TrimSpace(expectedRouterPubkey)
	if pubkeyText == "" {
		pubkeyText = q.RouterPubkey
	}
	pubkey, err := decodeEd448PublicKey(pubkeyText)
	if err != nil {
		return ErrInvalidWorkflow.Wrapf("invalid bundle quote router pubkey: %v", err)
	}
	signature, err := decodeEd448Signature(q.Signed)
	if err != nil {
		return ErrInvalidWorkflow.Wrapf("invalid bundle quote signature: %v", err)
	}
	canonical, err := q.CanonicalBytes()
	if err != nil {
		return err
	}
	if !ed448.Verify(ed448.PublicKey(pubkey), canonical, signature, "") {
		return ErrInvalidWorkflow.Wrap("bundle quote signature does not match")
	}
	return nil
}

// ExpiresAtTime parses the quote expiry timestamp.
func (q *BundleQuote) ExpiresAtTime() (time.Time, error) {
	if q == nil {
		return time.Time{}, ErrInvalidWorkflow.Wrap("bundle quote cannot be nil")
	}
	expires, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(q.ExpiresAt))
	if err != nil {
		return time.Time{}, ErrInvalidWorkflow.Wrapf("invalid bundle quote expires_at: %v", err)
	}
	return expires.UTC(), nil
}

// ValidateBasic validates the quote shape without checking keeper replay state.
func (q *BundleQuote) ValidateBasic() error {
	return q.validateBasic(true, true)
}

func (q *BundleQuote) validateBasic(requireBundleID bool, requireSignature bool) error {
	if q == nil {
		return ErrInvalidWorkflow.Wrap("bundle quote cannot be nil")
	}
	requiredFields := map[string]string{
		"workflow_id":          q.WorkflowID,
		"version":              q.Version,
		"inputs_hash":          q.InputsHash,
		"caller_passport_tier": q.CallerPassportTier,
		"nonce":                q.Nonce,
		"expires_at":           q.ExpiresAt,
		"router_pubkey":        q.RouterPubkey,
	}
	if requireBundleID {
		requiredFields["bundle_id"] = q.BundleID
	}
	if requireSignature {
		requiredFields["signed"] = q.Signed
	}
	for name, value := range requiredFields {
		if strings.TrimSpace(value) == "" {
			return ErrInvalidWorkflow.Wrapf("bundle quote %s is required", name)
		}
	}
	canonicalFields := map[string]string{
		"workflow_id":          q.WorkflowID,
		"version":              q.Version,
		"inputs_hash":          q.InputsHash,
		"caller_passport_tier": q.CallerPassportTier,
		"nonce":                q.Nonce,
		"expires_at":           q.ExpiresAt,
		"router_pubkey":        q.RouterPubkey,
	}
	if requireBundleID {
		canonicalFields["bundle_id"] = q.BundleID
	}
	if requireSignature {
		canonicalFields["signed"] = q.Signed
	}
	for name, value := range canonicalFields {
		if strings.TrimSpace(value) != value {
			return ErrInvalidWorkflow.Wrapf("bundle quote %s must be canonical: %q", name, value)
		}
	}
	if _, err := q.ExpiresAtTime(); err != nil {
		return err
	}
	if _, err := NewQuoteCoin(q.TotalMaxCost.Denom, q.TotalMaxCost.Amount); err != nil {
		return err
	}
	if q.CallerReputationScore > MaxPassportReputationScore {
		return ErrInvalidWorkflow.Wrapf("bundle quote caller_reputation_score exceeds %d", MaxPassportReputationScore)
	}
	if !passportBadgesAreCanonical(q.CallerPassportBadges) {
		return ErrInvalidWorkflow.Wrap("bundle quote caller_passport_badges must be lowercase, sorted, and unique")
	}
	if len(q.StepQuotes) == 0 {
		return ErrInvalidWorkflow.Wrap("bundle quote step_quotes cannot be empty")
	}
	seenStepIDs := make(map[string]struct{}, len(q.StepQuotes))
	for i, step := range q.StepQuotes {
		stepID := strings.TrimSpace(step.StepID)
		if stepID == "" {
			return ErrInvalidWorkflow.Wrapf("bundle quote step %d missing step_id", i)
		}
		if stepID != step.StepID {
			return ErrInvalidWorkflow.Wrapf("bundle quote step_id must be canonical: %q", step.StepID)
		}
		if _, ok := seenStepIDs[stepID]; ok {
			return ErrInvalidWorkflow.Wrapf("duplicate bundle quote step_id: %s", stepID)
		}
		seenStepIDs[stepID] = struct{}{}
		toolID := strings.TrimSpace(step.ToolID)
		if toolID == "" {
			return ErrInvalidWorkflow.Wrapf("bundle quote step %s missing tool_id", stepID)
		}
		if toolID != step.ToolID {
			return ErrInvalidWorkflow.Wrapf("bundle quote tool_id must be canonical: %q", step.ToolID)
		}
		toolVersion := strings.TrimSpace(step.ToolVersion)
		if toolVersion == "" {
			return ErrInvalidWorkflow.Wrapf("bundle quote step %s missing tool_version", stepID)
		}
		if toolVersion != step.ToolVersion {
			return ErrInvalidWorkflow.Wrapf("bundle quote tool_version must be canonical: %q", step.ToolVersion)
		}
		if _, err := NewQuoteCoin(step.SubMaxCost.Denom, step.SubMaxCost.Amount); err != nil {
			return err
		}
	}
	return nil
}

// Validate validates the persisted quote replay record shape.
func (r *BundleQuoteRecord) Validate() error {
	if r == nil {
		return ErrInvalidWorkflow.Wrap("bundle quote record cannot be nil")
	}
	if strings.TrimSpace(r.BundleID) == "" {
		return ErrInvalidWorkflow.Wrap("bundle quote record missing bundle_id")
	}
	if strings.TrimSpace(r.ExpiresAt) == "" {
		return ErrInvalidWorkflow.Wrap("bundle quote record missing expires_at")
	}
	if strings.TrimSpace(r.ExpiresAt) != r.ExpiresAt {
		return ErrInvalidWorkflow.Wrapf("bundle quote record expires_at must be canonical: %q", r.ExpiresAt)
	}
	if r.Quote == nil {
		return ErrInvalidWorkflow.Wrap("bundle quote record missing quote")
	}
	if err := r.Quote.ValidateBasic(); err != nil {
		return err
	}
	if r.BundleID != r.Quote.BundleID {
		return ErrInvalidWorkflow.Wrapf("bundle quote record id mismatch: %s != %s", r.BundleID, r.Quote.BundleID)
	}
	if err := validateBundleQuoteID(r.Quote); err != nil {
		return err
	}
	if r.ExpiresAt != r.Quote.ExpiresAt {
		return ErrInvalidWorkflow.Wrap("bundle quote record expires_at mismatch")
	}
	if r.Invocation != nil {
		if err := r.Invocation.Validate(); err != nil {
			return err
		}
		if strings.TrimSpace(r.Invocation.BundleID) != strings.TrimSpace(r.BundleID) {
			return ErrInvalidWorkflow.Wrap("bundle quote record invocation id mismatch")
		}
	}
	return nil
}

func validateBundleQuoteID(q *BundleQuote) error {
	expected, err := ComputeBundleQuoteID(q)
	if err != nil {
		return err
	}
	if q.BundleID != expected {
		return ErrInvalidWorkflow.Wrapf("bundle quote bundle_id does not match canonical quote contents: got %s want %s", q.BundleID, expected)
	}
	return nil
}

func canonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidWorkflow.Wrapf("marshal canonical JSON: %v", err)
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return nil, ErrInvalidWorkflow.Wrapf("canonicalize JSON: %v", err)
	}
	return canonical, nil
}

func normalizedStepQuotes(steps []BundleStepQuote) []BundleStepQuote {
	if len(steps) == 0 {
		return []BundleStepQuote{}
	}
	out := make([]BundleStepQuote, len(steps))
	copy(out, steps)
	return out
}

func passportBadgesAreCanonical(raw []string) bool {
	normalized := NormalizePassportBadges(raw)
	if len(raw) != len(normalized) {
		return false
	}
	for i, badge := range raw {
		if strings.TrimSpace(badge) != normalized[i] {
			return false
		}
	}
	return true
}

func decodeEd448PublicKey(encoded string) ([]byte, error) {
	trimmed := stripEd448AlgoAnd0xPrefix(strings.TrimSpace(encoded))
	if decoded, err := hex.DecodeString(trimmed); err == nil && len(decoded) == ed448.PublicKeySize {
		return decoded, nil
	}
	return nil, fmt.Errorf("could not decode public key: expected %d bytes for Ed448", ed448.PublicKeySize)
}

func decodeEd448Signature(encoded string) ([]byte, error) {
	trimmed := stripEd448AlgoAnd0xPrefix(strings.TrimSpace(encoded))
	if decoded, err := hex.DecodeString(trimmed); err == nil && len(decoded) == ed448.SignatureSize {
		return decoded, nil
	}
	return nil, fmt.Errorf("could not decode signature: expected %d bytes for Ed448", ed448.SignatureSize)
}

func ensureEd448PublicKeyFieldMatches(field string, expected string, actual string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	expectedKey, err := decodeEd448PublicKey(expected)
	if err != nil {
		return ErrInvalidWorkflow.Wrapf("invalid expected %s: %v", field, err)
	}
	actualKey, err := decodeEd448PublicKey(actual)
	if err != nil {
		return ErrInvalidWorkflow.Wrapf("invalid %s: %v", field, err)
	}
	if !bytes.Equal(expectedKey, actualKey) {
		return ErrInvalidWorkflow.Wrapf("%s does not match expected public key", field)
	}
	return nil
}

func stripEd448AlgoAnd0xPrefix(s string) string {
	for {
		switch {
		case len(s) >= len("ed448:") && strings.EqualFold(s[:len("ed448:")], "ed448:"):
			s = s[len("ed448:"):]
		case len(s) >= len("0x") && strings.EqualFold(s[:len("0x")], "0x"):
			s = s[len("0x"):]
		default:
			return s
		}
	}
}
