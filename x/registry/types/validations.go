//go:build cosmos

package types

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/LumeraProtocol/lumera/internal/logging"
	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/Masterminds/semver/v3"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	metadataLicenseReference          = "license_reference"
	metadataLicenseExpiresAt          = "license_expires_at"
	metadataLicenseGovernanceStatus   = "license_governance_status"
	metadataLicenseGovernanceProposal = "license_governance_proposal"
)

// Validation limits for registry metadata fields.
const (
	MaxDescriptionLength   = 10240 // 10KB
	MaxMetadataKeys        = 64
	MaxMetadataKeyLength   = 128
	MaxMetadataValueLength = 2048 // 2KB
	MaxTags                = 32
	MaxTagLength           = 64
	MaxToolIDLength        = 64

	// MaxSLAIdentifierLength caps sla_id / dispute_terms_id on
	// MsgSetSLATemplate and MsgSetDisputeTerms. These govern
	// registry lookups and get embedded as map keys; 64 matches
	// MaxToolIDLength parity and is ~8x realistic SLA identifier
	// shape ("sla-v1", "dispute-sla-nlp", etc.).
	MaxSLAIdentifierLength = 64

	// MaxSLATemplatePayloadLength caps the structured JSON payload
	// on MsgSetSLATemplate / MsgSetDisputeTerms. Real SLA templates
	// (latency targets, availability, dispute procedures) measure
	// in KB; 64 KiB is ~30x realistic shape. Without a cap, a
	// governance proposal could embed a megabyte-scale payload
	// that persists in state and gets scanned on every SLA lookup
	// (paired with MaxSLAIdentifierLength above; same bug class as
	// the ValidateBasic parity gaps closed across x/insurance,
	// x/nft, x/payment_rails, x/vaults this session).
	MaxSLATemplatePayloadLength = 64 * 1024
)

func requireTimestamp(ts *timestamppb.Timestamp, name string) (time.Time, error) {
	if ts == nil {
		return time.Time{}, fmt.Errorf("%s cannot be nil", name)
	}
	if err := ts.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: %w", name, err)
	}
	return ts.AsTime(), nil
}

func validateHTTPURL(raw string, fieldName string, allowHTTP bool) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	if trimmed != raw || containsWhitespaceOrControl(trimmed) {
		return fmt.Errorf("invalid %s: must not include whitespace or control characters", fieldName)
	}
	if urlAuthorityContainsUserinfo(trimmed) {
		return fmt.Errorf("invalid %s: must not include userinfo", fieldName)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid %s: %s", fieldName, registryURLParseErrorReason(err))
	}
	if parsed.User != nil {
		return fmt.Errorf("invalid %s: must not include userinfo", fieldName)
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return fmt.Errorf("invalid %s: missing host", fieldName)
	}
	if allowHTTP {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("invalid %s: scheme must be http or https", fieldName)
		}
		return nil
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("invalid %s: scheme must be https", fieldName)
	}
	return nil
}

func validateEvidenceURI(raw string, fieldName string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	if trimmed != raw || containsWhitespaceOrControl(trimmed) {
		return fmt.Errorf("invalid %s: must not include whitespace or control characters", fieldName)
	}
	if urlAuthorityContainsUserinfo(trimmed) {
		return fmt.Errorf("invalid %s: must not include userinfo", fieldName)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid %s: %s", fieldName, registryURLParseErrorReason(err))
	}
	if parsed.User != nil {
		return fmt.Errorf("invalid %s: must not include userinfo", fieldName)
	}
	if parsed.Scheme == "" {
		return fmt.Errorf("invalid %s: missing scheme", fieldName)
	}
	switch parsed.Scheme {
	case "http", "https", "ipfs", "ipns":
		// allowed
	default:
		return fmt.Errorf("invalid %s: unsupported scheme %q", fieldName, parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return fmt.Errorf("invalid %s: missing host", fieldName)
	}
	return nil
}

func urlAuthorityContainsUserinfo(raw string) bool {
	authorityStart := strings.Index(raw, "://")
	switch {
	case authorityStart >= 0:
		authorityStart += len("://")
	case strings.HasPrefix(raw, "//"):
		authorityStart = len("//")
	default:
		return false
	}
	authority := raw[authorityStart:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	return strings.Contains(authority, "@")
}

func registryURLParseErrorReason(err error) string {
	if urlErr, ok := err.(*url.Error); ok && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return "malformed URL"
}

func containsWhitespaceOrControl(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func parseMetadataTimestamp(value string, fieldName string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("%s cannot be empty", fieldName)
	}
	if isDigits(trimmed) {
		seconds, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid %s unix seconds: %w", fieldName, err)
		}
		if seconds <= 0 {
			return time.Time{}, fmt.Errorf("invalid %s unix seconds: must be > 0", fieldName)
		}
		return time.Unix(seconds, 0).UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err == nil {
		return parsed, nil
	}
	parsed, errNano := time.Parse(time.RFC3339Nano, trimmed)
	if errNano == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid %s (expected unix seconds or RFC3339): %w", fieldName, err)
}

func isDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validateEnum(value string, allowed map[string]struct{}, fieldName string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	if _, ok := allowed[trimmed]; !ok {
		return fmt.Errorf("invalid %s: %s", fieldName, trimmed)
	}
	return nil
}

func validateOriginIDSchema(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	normalized := strings.ToLower(trimmed)
	if len(normalized) > 64 {
		return fmt.Errorf("origin_id too long (max 64 chars)")
	}
	parts := strings.Split(normalized, ":")
	if len(parts) != 2 {
		return fmt.Errorf("origin_id must be <namespace>:<surface>")
	}
	for idx, part := range parts {
		label := "origin_id namespace"
		if idx == 1 {
			label = "origin_id surface"
		}
		if part == "" {
			return fmt.Errorf("%s cannot be empty", label)
		}
		if len(part) > 32 {
			return fmt.Errorf("%s too long (max 32 chars)", label)
		}
		for i := 0; i < len(part); i++ {
			ch := part[i]
			isAlpha := ch >= 'a' && ch <= 'z'
			isNum := ch >= '0' && ch <= '9'
			isPunct := ch == '-' || ch == '_'
			if !isAlpha && !isNum && !isPunct {
				return fmt.Errorf("%s contains invalid character %q", label, ch)
			}
			if i == 0 && isPunct {
				return fmt.Errorf("%s must start with [a-z0-9]", label)
			}
		}
	}
	return nil
}

func validateToolID(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("tool ID cannot be empty")
	}
	if len(trimmed) > MaxToolIDLength {
		return fmt.Errorf("tool ID exceeds maximum length of %d", MaxToolIDLength)
	}
	for _, ch := range trimmed {
		isAlpha := ch >= 'a' && ch <= 'z'
		isNum := ch >= '0' && ch <= '9'
		isPunct := ch == '-' || ch == '.' || ch == '_'
		if !isAlpha && !isNum && !isPunct {
			return fmt.Errorf("tool ID contains invalid character %q (must be [a-z0-9-._])", ch)
		}
	}
	return nil
}

// Additional validation methods for generated types

// Validate performs comprehensive validation on UsageReceipt
func (r *UsageReceipt) Validate() error {
	if r == nil {
		return fmt.Errorf("receipt cannot be nil")
	}
	if r.ReceiptId == "" {
		return fmt.Errorf("receipt ID cannot be empty")
	}
	if err := validateToolID(r.ToolId); err != nil {
		return err
	}
	if r.RequestId == "" {
		return fmt.Errorf("request ID cannot be empty")
	}
	if len(r.RequestHash) == 0 {
		return fmt.Errorf("request hash cannot be empty")
	}
	unitsUsed, err := decimal.NewFromString(r.UnitsUsed)
	if err != nil {
		return fmt.Errorf("invalid units used: %w", err)
	}
	// Consensus-halt guard: UsageReceipt.Validate runs before
	// verifyReceiptSignature (registry_keeper.go:714/719), which computes
	// `units.Mul(price)` and `actual.Equal(expectedAmount)` — both force
	// shopspring to expand a symbolic big.Int. A MsgSubmitReceipt with
	// "1e11100100" in any of these four fields would halt block production
	// across all validators. Reject at field-validation time so the
	// vulnerable arithmetic downstream never sees an absurd exponent.
	if !moneyguard.IsSafeExponent(unitsUsed) {
		return fmt.Errorf("units used magnitude out of range")
	}
	if unitsUsed.IsNegative() {
		return fmt.Errorf("units used cannot be negative")
	}
	if r.Unit == "" {
		return fmt.Errorf("unit cannot be empty")
	}
	if strings.TrimSpace(r.Unit) != r.Unit {
		return fmt.Errorf("unit cannot contain leading or trailing whitespace")
	}
	allowedUnits := map[string]struct{}{
		"req":   {},
		"page":  {},
		"sec":   {},
		"token": {},
		"byte":  {},
	}
	if _, ok := allowedUnits[r.Unit]; !ok {
		return fmt.Errorf("invalid unit: %s", r.Unit)
	}
	price, err := decimal.NewFromString(r.PricePerUnit)
	if err != nil {
		return fmt.Errorf("invalid price per unit: %w", err)
	}
	if !moneyguard.IsSafeExponent(price) {
		return fmt.Errorf("price per unit magnitude out of range")
	}
	if price.IsNegative() {
		return fmt.Errorf("price per unit cannot be negative")
	}
	quoted, err := decimal.NewFromString(r.QuotedAmount)
	if err != nil {
		return fmt.Errorf("invalid quoted amount: %w", err)
	}
	if !moneyguard.IsSafeExponent(quoted) {
		return fmt.Errorf("quoted amount magnitude out of range")
	}
	if quoted.IsNegative() {
		return fmt.Errorf("quoted amount cannot be negative")
	}
	actual, err := decimal.NewFromString(r.ActualAmount)
	if err != nil {
		return fmt.Errorf("invalid actual amount: %w", err)
	}
	if !moneyguard.IsSafeExponent(actual) {
		return fmt.Errorf("actual amount magnitude out of range")
	}
	if actual.IsNegative() {
		return fmt.Errorf("actual amount cannot be negative")
	}
	if r.RouterAddress == "" {
		return fmt.Errorf("router address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(r.RouterAddress); err != nil {
		return fmt.Errorf("invalid router address: %w", err)
	}
	if r.UserAddress == "" {
		return fmt.Errorf("user address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(r.UserAddress); err != nil {
		return fmt.Errorf("invalid user address: %w", err)
	}
	timestamp, err := requireTimestamp(r.Timestamp, "timestamp")
	if err != nil {
		return err
	}
	expiresAt, err := requireTimestamp(r.ExpiresAt, "expires_at")
	if err != nil {
		return err
	}
	if expiresAt.Before(timestamp) {
		return fmt.Errorf("expires at cannot be before timestamp")
	}
	// Validate status if provided
	if r.Status != "" {
		validStatuses := map[string]bool{
			ReceiptStatusPending:  true,
			ReceiptStatusSettled:  true,
			ReceiptStatusDisputed: true,
			ReceiptStatusExpired:  true,
		}
		if !validStatuses[r.Status] {
			return fmt.Errorf("invalid receipt status: %s", r.Status)
		}
	}
	if trustClass := strings.TrimSpace(r.TrustClass); trustClass != "" {
		allowedTrustClasses := map[string]struct{}{
			TrustClassPublisherSignedBonded: {},
			TrustClassRouterAttested:        {},
		}
		if err := validateEnum(trustClass, allowedTrustClasses, "trust class"); err != nil {
			return err
		}
	}
	if err := validateOriginIDSchema(r.OriginId); err != nil {
		return err
	}
	return nil
}

// Validate performs validation on Challenge
func (c *Challenge) Validate() error {
	if c == nil {
		return fmt.Errorf("challenge cannot be nil")
	}
	if c.ChallengeId == "" {
		return fmt.Errorf("challenge ID cannot be empty")
	}
	if c.ReceiptId == "" {
		return fmt.Errorf("receipt ID cannot be empty")
	}
	if c.ChallengerAddress == "" {
		return fmt.Errorf("challenger address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(c.ChallengerAddress); err != nil {
		return fmt.Errorf("invalid challenger address: %w", err)
	}
	if len(c.ChallengerStake) == 0 {
		return fmt.Errorf("challenger stake cannot be empty")
	}
	if c.Reason == "" {
		return fmt.Errorf("reason cannot be empty")
	}
	challengedAt, err := requireTimestamp(c.ChallengedAt, "challenged_at")
	if err != nil {
		return err
	}
	deadlineAt, err := requireTimestamp(c.DeadlineAt, "deadline_at")
	if err != nil {
		return err
	}
	if deadlineAt.Before(challengedAt) {
		return fmt.Errorf("deadline cannot be before challenge time")
	}
	return nil
}

// Validate performs validation on BondRecord
func (b *BondRecord) Validate() error {
	if b == nil {
		return fmt.Errorf("bond record cannot be nil")
	}
	if err := validateToolID(b.ToolId); err != nil {
		return err
	}
	if b.Owner == "" {
		return fmt.Errorf("owner cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(b.Owner); err != nil {
		return fmt.Errorf("invalid owner address: %w", err)
	}
	// Allow zero bonded amount for withdrawn or fully slashed bonds
	if b.Status != BondStatusWithdrawn && b.Status != BondStatusSlashed {
		if len(b.BondedAmount) == 0 {
			return fmt.Errorf("bonded amount cannot be empty for active bonds")
		}
	}
	if b.InsurancePremiumMultiplier != "" {
		multiplier, err := decimal.NewFromString(b.InsurancePremiumMultiplier)
		if err != nil {
			return fmt.Errorf("invalid insurance premium multiplier: %w", err)
		}
		// Consensus-halt guard: the multiplier flows into insurance-
		// premium Mul arithmetic on the settlement path; a symbolic
		// exponent would expand big.Int and hang every validator.
		if !moneyguard.IsSafeExponent(multiplier) {
			return fmt.Errorf("insurance premium multiplier magnitude out of range")
		}
		if multiplier.IsNegative() {
			return fmt.Errorf("insurance premium multiplier cannot be negative")
		}
	}
	if _, err := requireTimestamp(b.BondedAt, "bonded_at"); err != nil {
		return err
	}
	if _, err := requireTimestamp(b.LastUpdatedAt, "last_updated_at"); err != nil {
		return err
	}
	// Validate status
	if b.Status != "" {
		validStatuses := map[string]bool{
			BondStatusActive:      true,
			BondStatusWithdrawing: true,
			BondStatusWithdrawn:   true,
			BondStatusSlashed:     true,
		}
		if !validStatuses[b.Status] {
			return fmt.Errorf("invalid bond status: %s", b.Status)
		}
	}
	return nil
}

// Validate performs validation on ToolCard
func (tc *ToolCard) Validate() error {
	return tc.ValidateAtTime(time.Now())
}

// ValidateAtTime validates the tool card using the given reference time for
// time-dependent checks (e.g. license expiry). Consensus-critical callers
// must pass ctx.BlockTime() to ensure determinism.
func (tc *ToolCard) ValidateAtTime(now time.Time) error {
	if tc == nil {
		return fmt.Errorf("tool card cannot be nil")
	}
	if err := validateToolID(tc.ToolId); err != nil {
		return err
	}
	if strings.TrimSpace(tc.Owner) == "" {
		return fmt.Errorf("owner cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(tc.Owner); err != nil {
		return fmt.Errorf("invalid owner address: %w", err)
	}
	if strings.TrimSpace(tc.Version) == "" {
		return fmt.Errorf("version cannot be empty")
	}
	if _, err := semver.NewVersion(tc.Version); err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}
	if len(tc.Categories) == 0 {
		return fmt.Errorf("at least one category is required")
	}
	allowedLicenseLanes := map[string]struct{}{
		LicenseLaneBYOKey:         {},
		LicenseLaneLicensedResale: {},
		LicenseLaneCommunity:      {},
		LicenseLaneProxied:        {},
	}
	if err := validateEnum(tc.LicenseLane, allowedLicenseLanes, "license lane"); err != nil {
		return err
	}
	lane := strings.TrimSpace(tc.LicenseLane)
	trustClass := strings.TrimSpace(tc.TrustClass)
	if trustClass != "" {
		allowedTrustClasses := map[string]struct{}{
			TrustClassPublisherSignedBonded: {},
			TrustClassRouterAttested:        {},
		}
		if err := validateEnum(trustClass, allowedTrustClasses, "trust class"); err != nil {
			return err
		}
	}
	if lane == LicenseLaneProxied {
		if trustClass != TrustClassRouterAttested {
			return fmt.Errorf("trust class must be %s when license lane is %s", TrustClassRouterAttested, LicenseLaneProxied)
		}
	} else if trustClass == TrustClassRouterAttested {
		return fmt.Errorf("trust class %s is only valid when license lane is %s", TrustClassRouterAttested, LicenseLaneProxied)
	}
	if len(tc.Jurisdictions) == 0 {
		return fmt.Errorf("at least one jurisdiction is required")
	}
	if len(tc.SchemaHash) == 0 {
		return fmt.Errorf("schema hash cannot be empty")
	}
	if len(tc.SbomHash) == 0 {
		return fmt.Errorf("sbom hash cannot be empty")
	}
	if len(tc.AttestationRoot) == 0 {
		return fmt.Errorf("attestation root cannot be empty")
	}
	if strings.TrimSpace(tc.InputSchema) == "" {
		return fmt.Errorf("input schema cannot be empty")
	}
	if strings.TrimSpace(tc.OutputSchema) == "" {
		return fmt.Errorf("output schema cannot be empty")
	}
	if tc.Pricing == nil {
		return fmt.Errorf("pricing cannot be nil")
	}
	if err := tc.Pricing.Validate(); err != nil {
		return fmt.Errorf("invalid pricing: %w", err)
	}
	if tc.Slo == nil {
		return fmt.Errorf("SLO cannot be nil")
	}
	if err := tc.Slo.Validate(); err != nil {
		return fmt.Errorf("invalid SLO: %w", err)
	}
	if tc.Sandbox == nil {
		return fmt.Errorf("sandbox profile cannot be nil")
	}
	if err := tc.Sandbox.Validate(); err != nil {
		return fmt.Errorf("invalid sandbox profile: %w", err)
	}
	if tc.Cache != nil {
		if err := tc.Cache.Validate(); err != nil {
			return fmt.Errorf("invalid cache policy: %w", err)
		}
	}
	if len(tc.Endpoints) == 0 {
		return fmt.Errorf("at least one endpoint is required")
	}
	for i, endpoint := range tc.Endpoints {
		if endpoint == nil {
			return fmt.Errorf("endpoint %d cannot be nil", i)
		}
		if err := endpoint.Validate(); err != nil {
			return fmt.Errorf("invalid endpoint %d: %w", i, err)
		}
	}
	if len(tc.McpProtocols) == 0 {
		return fmt.Errorf("at least one mcp protocol is required")
	}
	allowedMCPProtocols := map[string]struct{}{"https": {}}
	for i, proto := range tc.McpProtocols {
		if err := validateEnum(proto, allowedMCPProtocols, fmt.Sprintf("mcp protocol %d", i)); err != nil {
			return err
		}
	}

	if len(tc.Description) > MaxDescriptionLength {
		return fmt.Errorf("description exceeds maximum length of %d", MaxDescriptionLength)
	}

	if len(tc.Tags) > MaxTags {
		return fmt.Errorf("too many tags (max %d)", MaxTags)
	}
	for i, tag := range tc.Tags {
		if len(tag) > MaxTagLength {
			return fmt.Errorf("tag %d exceeds maximum length of %d", i, MaxTagLength)
		}
	}

	if len(tc.Metadata) > MaxMetadataKeys {
		return fmt.Errorf("too many metadata keys (max %d)", MaxMetadataKeys)
	}
	for k, v := range tc.Metadata {
		if len(k) > MaxMetadataKeyLength {
			return fmt.Errorf("metadata key %q exceeds maximum length of %d", redactRegistryMetadataDiagnostic(k), MaxMetadataKeyLength)
		}
		if len(v) > MaxMetadataValueLength {
			return fmt.Errorf("metadata value for key %q exceeds maximum length of %d", redactRegistryMetadataDiagnostic(k), MaxMetadataValueLength)
		}
	}
	if raw := strings.TrimSpace(tc.Metadata[MetadataKeyToolWasmContractV01]); raw != "" {
		contract, err := ParseToolWasmContractV01(raw)
		if err != nil {
			return fmt.Errorf("metadata %s: %w", MetadataKeyToolWasmContractV01, err)
		}
		if err := contract.Validate(); err != nil {
			return fmt.Errorf("metadata %s: %w", MetadataKeyToolWasmContractV01, err)
		}
	}

	if lane == LicenseLaneLicensedResale {
		if len(tc.Metadata) == 0 {
			return fmt.Errorf("metadata required when license lane is %s", LicenseLaneLicensedResale)
		}

		licenseRef := strings.TrimSpace(tc.Metadata[metadataLicenseReference])
		if licenseRef == "" {
			return fmt.Errorf("metadata %s required when license lane is %s", metadataLicenseReference, LicenseLaneLicensedResale)
		}
		if err := validateEvidenceURI(licenseRef, fmt.Sprintf("metadata.%s", metadataLicenseReference)); err != nil {
			return err
		}

		expiresAt := strings.TrimSpace(tc.Metadata[metadataLicenseExpiresAt])
		if expiresAt == "" {
			return fmt.Errorf("metadata %s required when license lane is %s", metadataLicenseExpiresAt, LicenseLaneLicensedResale)
		}
		ts, err := parseMetadataTimestamp(expiresAt, fmt.Sprintf("metadata.%s", metadataLicenseExpiresAt))
		if err != nil {
			return err
		}
		if ts.Before(now) {
			return fmt.Errorf("metadata %s: license has expired; cannot register tool with expired license", metadataLicenseExpiresAt)
		}

		status := strings.ToLower(strings.TrimSpace(tc.Metadata[metadataLicenseGovernanceStatus]))
		if status == "" {
			return fmt.Errorf("metadata %s required when license lane is %s", metadataLicenseGovernanceStatus, LicenseLaneLicensedResale)
		}
		switch status {
		case "pending", "approved":
			// allowed
		case "revoked":
			return fmt.Errorf("metadata %s: cannot register tool with revoked license", metadataLicenseGovernanceStatus)
		default:
			return fmt.Errorf("invalid metadata %s: must be pending, approved, or revoked", metadataLicenseGovernanceStatus)
		}

		proposal := strings.TrimSpace(tc.Metadata[metadataLicenseGovernanceProposal])
		if proposal == "" {
			return fmt.Errorf("metadata %s required when license lane is %s", metadataLicenseGovernanceProposal, LicenseLaneLicensedResale)
		}
		if err := validateEvidenceURI(proposal, fmt.Sprintf("metadata.%s", metadataLicenseGovernanceProposal)); err != nil {
			return err
		}
	}
	return nil
}

func redactRegistryMetadataDiagnostic(value string) string {
	return logging.RedactPII(strings.TrimSpace(value))
}

// Validate performs validation on SandboxProfile.
func (s *SandboxProfile) Validate() error {
	if s == nil {
		return fmt.Errorf("sandbox profile cannot be nil")
	}
	allowedProfiles := map[string]struct{}{
		"scrape":            {},
		"docs-search":       {},
		"defi-analytics":    {},
		"onchain":           {},
		"community":         {},
		"enclave":           {},
		"enclave-sovereign": {},
	}
	if err := validateEnum(s.Profile, allowedProfiles, "sandbox profile"); err != nil {
		return err
	}
	if s.MaxExecutionSec == 0 {
		return fmt.Errorf("max_execution_sec must be greater than zero")
	}
	if len(s.EgressAllowlist) > 0 {
		for i, raw := range s.EgressAllowlist {
			if err := validateHTTPURL(raw, fmt.Sprintf("egress_allowlist[%d]", i), true); err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(s.PiiHandling) != "" {
		allowedPII := map[string]struct{}{"none": {}, "hashed": {}, "redacted": {}}
		if err := validateEnum(s.PiiHandling, allowedPII, "pii handling"); err != nil {
			return err
		}
	}
	return nil
}

// Validate performs validation on CachePolicy.
func (c *CachePolicy) Validate() error {
	if c == nil {
		return nil
	}
	if c.Enabled && c.TtlSeconds == 0 {
		return fmt.Errorf("cache ttl must be positive when caching is enabled")
	}
	if c.RoyaltyShareBps > BPSDenominator {
		return fmt.Errorf("cache royalty share exceeds %d bps", BPSDenominator)
	}
	return nil
}

// Validate performs validation on Endpoint.
func (e *Endpoint) Validate() error {
	if e == nil {
		return fmt.Errorf("endpoint cannot be nil")
	}
	if err := validateEnum(e.Protocol, map[string]struct{}{"https": {}}, "endpoint protocol"); err != nil {
		return err
	}
	if err := validateHTTPURL(e.Url, "endpoint url", false); err != nil {
		return err
	}
	return nil
}

// Validate performs validation on Pricing
func (p *Pricing) Validate() error {
	if p == nil {
		return fmt.Errorf("pricing cannot be nil")
	}
	allowedModels := map[string]struct{}{
		"per_call":  {},
		"per_unit":  {},
		"per_byte":  {},
		"per_token": {},
	}
	if err := validateEnum(p.Model, allowedModels, "pricing model"); err != nil {
		return err
	}
	allowedUnits := map[string]struct{}{
		"req":   {},
		"page":  {},
		"sec":   {},
		"token": {},
		"byte":  {},
	}
	if err := validateEnum(p.Unit, allowedUnits, "pricing unit"); err != nil {
		return err
	}
	pricePerUnit, err := decimal.NewFromString(p.PricePerUnit)
	if err != nil {
		return fmt.Errorf("invalid price per unit: %w", err)
	}
	// Reject absurd shopspring exponents at the consensus boundary. Pricing
	// fields are validator-supplied via MsgRegisterTool / MsgUpdateTool, and
	// the maxCost.LessThan(minCost) comparison below forces shopspring to
	// expand the big.Int to match exponents. A "1e11100100" pricing value
	// would hang every validator's Validate() call and halt block production.
	if !moneyguard.IsSafeExponent(pricePerUnit) {
		return fmt.Errorf("price per unit magnitude out of range")
	}
	if pricePerUnit.IsNegative() {
		return fmt.Errorf("price per unit cannot be negative")
	}
	minCost := decimal.Zero
	hasMinCost := strings.TrimSpace(p.MinimumCost) != ""
	if hasMinCost {
		var err error
		minCost, err = decimal.NewFromString(p.MinimumCost)
		if err != nil {
			return fmt.Errorf("invalid minimum cost: %w", err)
		}
		if !moneyguard.IsSafeExponent(minCost) {
			return fmt.Errorf("minimum cost magnitude out of range")
		}
	}
	if minCost.IsNegative() {
		return fmt.Errorf("minimum cost cannot be negative")
	}
	maxCost := decimal.Zero
	hasMaxCost := strings.TrimSpace(p.MaximumCost) != ""
	if hasMaxCost {
		var err error
		maxCost, err = decimal.NewFromString(p.MaximumCost)
		if err != nil {
			return fmt.Errorf("invalid maximum cost: %w", err)
		}
		if !moneyguard.IsSafeExponent(maxCost) {
			return fmt.Errorf("maximum cost magnitude out of range")
		}
	}
	if maxCost.IsNegative() {
		return fmt.Errorf("maximum cost cannot be negative")
	}
	if hasMinCost && hasMaxCost && maxCost.LessThan(minCost) {
		return fmt.Errorf("maximum cost cannot be less than minimum cost")
	}
	if strings.TrimSpace(p.QuoteEndpoint) != "" {
		if err := validateHTTPURL(p.QuoteEndpoint, "quote endpoint", false); err != nil {
			return err
		}
	}
	return nil
}

// Validate performs validation on SLO
func (s *SLO) Validate() error {
	if s == nil {
		return fmt.Errorf("SLO cannot be nil")
	}
	if s.P95LatencyMs == 0 {
		return fmt.Errorf("P95 latency must be specified")
	}
	avail, err := decimal.NewFromString(s.Availability)
	if err != nil {
		return fmt.Errorf("invalid availability: %w", err)
	}
	// Reject absurd shopspring exponents before the comparisons below force
	// shopspring to expand the big.Int. SLO is set via MsgRegisterTool /
	// MsgUpdateTool on-chain; a "1e11100100" availability hangs every
	// validator's Validate() call — a consensus-halt DoS.
	if !moneyguard.IsSafeExponent(avail) {
		return fmt.Errorf("availability magnitude out of range")
	}
	if avail.LessThanOrEqual(decimal.Zero) || avail.GreaterThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("availability must be between 0 and 1")
	}
	if s.ErrorRateBps > 10000 {
		return fmt.Errorf("error rate cannot exceed 10000 bps")
	}
	if s.TimeoutMs == 0 {
		return fmt.Errorf("timeout must be specified")
	}
	return nil
}

// Validate performs validation on PendingSlash
func (p *PendingSlash) Validate() error {
	if p == nil {
		return fmt.Errorf("pending slash cannot be nil")
	}
	if p.SlashId == "" {
		return fmt.Errorf("slash ID cannot be empty")
	}
	if len(p.Amount) == 0 {
		return fmt.Errorf("amount cannot be empty")
	}
	if p.Reason == "" {
		return fmt.Errorf("reason cannot be empty")
	}
	proposedAt, err := requireTimestamp(p.ProposedAt, "proposed_at")
	if err != nil {
		return err
	}
	executeAt, err := requireTimestamp(p.ExecuteAt, "execute_at")
	if err != nil {
		return err
	}
	if executeAt.Before(proposedAt) {
		return fmt.Errorf("execute at cannot be before proposed at")
	}
	return nil
}

// Validate performs validation on SettlementRecord
func (s *SettlementRecord) Validate() error {
	if s == nil {
		return fmt.Errorf("settlement cannot be nil")
	}
	if s.ReceiptId == "" {
		return fmt.Errorf("receipt ID cannot be empty")
	}
	if err := validateToolID(s.ToolId); err != nil {
		return err
	}
	// Consensus-halt guards: SettlementRecord.Validate is invoked on
	// the settlement finalization path; the parsed values flow into
	// downstream Sub/Mul arithmetic (refund calc, burn distribution,
	// audit reconciliation). Reject symbolic exponents at parse time.
	if s.LockedQuote != "" {
		locked, err := decimal.NewFromString(s.LockedQuote)
		if err != nil {
			return fmt.Errorf("invalid locked quote: %w", err)
		}
		if !moneyguard.IsSafeExponent(locked) {
			return fmt.Errorf("locked quote magnitude out of range")
		}
		if locked.IsNegative() {
			return fmt.Errorf("locked quote cannot be negative")
		}
	}
	if s.ActualSettled != "" {
		settled, err := decimal.NewFromString(s.ActualSettled)
		if err != nil {
			return fmt.Errorf("invalid actual settled: %w", err)
		}
		if !moneyguard.IsSafeExponent(settled) {
			return fmt.Errorf("actual settled magnitude out of range")
		}
		if settled.IsNegative() {
			return fmt.Errorf("actual settled cannot be negative")
		}
	}
	if s.BurnAmount != "" {
		burn, err := decimal.NewFromString(s.BurnAmount)
		if err != nil {
			return fmt.Errorf("invalid burn amount: %w", err)
		}
		if !moneyguard.IsSafeExponent(burn) {
			return fmt.Errorf("burn amount magnitude out of range")
		}
		if burn.IsNegative() {
			return fmt.Errorf("burn amount cannot be negative")
		}
	}
	if s.PublisherAddress == "" {
		return fmt.Errorf("publisher address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(s.PublisherAddress); err != nil {
		return fmt.Errorf("invalid publisher address: %w", err)
	}
	if s.RouterAddress == "" {
		return fmt.Errorf("router address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(s.RouterAddress); err != nil {
		return fmt.Errorf("invalid router address: %w", err)
	}
	if s.UserAddress == "" {
		return fmt.Errorf("user address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(s.UserAddress); err != nil {
		return fmt.Errorf("invalid user address: %w", err)
	}
	if _, err := requireTimestamp(s.SettledAt, "settled_at"); err != nil {
		return err
	}
	return nil
}

// Validate performs validation on LaneRegistryEntry.
func (l *LaneRegistryEntry) Validate() error {
	if l == nil {
		return fmt.Errorf("lane registry entry cannot be nil")
	}
	if strings.TrimSpace(l.LaneId) == "" {
		return fmt.Errorf("lane_id cannot be empty")
	}
	if strings.TrimSpace(l.OperatorId) == "" {
		return fmt.Errorf("operator_id cannot be empty")
	}
	allowedLaneTypes := map[string]struct{}{
		LaneTypePublic:                   {},
		LaneTypeConfidentialTEE:          {},
		LaneTypeConfidentialPublisherTEE: {},
	}
	if err := validateEnum(l.LaneType, allowedLaneTypes, "lane_type"); err != nil {
		return err
	}
	if l.Metering == nil {
		return fmt.Errorf("metering config cannot be nil")
	}
	if err := l.Metering.Validate(); err != nil {
		return err
	}
	if l.Compliance == nil {
		return fmt.Errorf("compliance config cannot be nil")
	}
	if err := l.Compliance.Validate(); err != nil {
		return err
	}

	// Attestation is required for confidential lanes.
	if l.LaneType != LaneTypePublic {
		if l.Attestation == nil {
			return fmt.Errorf("attestation policy required for confidential lanes")
		}
		if err := l.Attestation.Validate(); err != nil {
			return err
		}
	}
	if l.Attestation != nil && l.LaneType == LaneTypePublic {
		if err := l.Attestation.Validate(); err != nil {
			return err
		}
	}
	for i, hash := range l.AllowedCapsuleHashes {
		if len(hash) != 32 {
			return fmt.Errorf("allowed_capsule_hashes[%d] must be 32 bytes", i)
		}
	}
	return nil
}

// Validate performs validation on LaneAttestationPolicy.
func (p *LaneAttestationPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("attestation policy cannot be nil")
	}
	allowedTEE := map[string]struct{}{
		LaneTEETypeSGX:       {},
		LaneTEETypeSEVSNP:    {},
		LaneTEETypeNitro:     {},
		LaneTEETypeSapphire:  {},
		LaneTEETypeTrustZone: {},
	}
	if err := validateEnum(p.TeeType, allowedTEE, "tee_type"); err != nil {
		return err
	}
	if len(p.PolicyHash) == 0 {
		return fmt.Errorf("policy_hash cannot be empty")
	}
	if len(p.PolicyHash) != 32 {
		return fmt.Errorf("policy_hash must be 32 bytes")
	}
	for i, measurement := range p.AllowedMeasurements {
		if len(measurement) != 32 {
			return fmt.Errorf("allowed_measurements[%d] must be 32 bytes", i)
		}
	}
	for i, signer := range p.SignerKeys {
		if len(signer) != 32 {
			return fmt.Errorf("signer_keys[%d] must be 32 bytes", i)
		}
	}
	return nil
}

// Validate performs validation on LaneMeteringConfig.
func (m *LaneMeteringConfig) Validate() error {
	if m == nil {
		return fmt.Errorf("metering config cannot be nil")
	}
	allowedMeterTypes := map[string]struct{}{
		LaneMeterTypeTEESigned:       {},
		LaneMeterTypeWatcherVerified: {},
	}
	if err := validateEnum(m.MeterType, allowedMeterTypes, "meter_type"); err != nil {
		return err
	}
	if strings.TrimSpace(m.PricingModelId) == "" {
		return fmt.Errorf("pricing_model_id cannot be empty")
	}
	return nil
}

// Validate performs validation on LaneComplianceConfig.
func (c *LaneComplianceConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("compliance config cannot be nil")
	}
	if len(c.EgressAllowlist) == 0 {
		return fmt.Errorf("egress_allowlist cannot be empty")
	}
	for i, raw := range c.EgressAllowlist {
		if err := validateHTTPURL(raw, fmt.Sprintf("egress_allowlist[%d]", i), true); err != nil {
			return err
		}
	}
	allowedLogPolicies := map[string]struct{}{
		LaneLogPolicyNone:      {},
		LaneLogPolicyHashOnly:  {},
		LaneLogPolicyEncrypted: {},
	}
	if err := validateEnum(c.LogPolicy, allowedLogPolicies, "log_policy"); err != nil {
		return err
	}
	allowedPII := map[string]struct{}{
		LanePIIPolicyDeny:               {},
		LanePIIPolicyAllowWithRedaction: {},
	}
	if err := validateEnum(c.PiiPolicy, allowedPII, "pii_policy"); err != nil {
		return err
	}
	return nil
}

// Validate performs validation on ToolCapsule.
func (c *ToolCapsule) Validate() error {
	if c == nil {
		return fmt.Errorf("tool capsule cannot be nil")
	}
	if err := validateToolID(c.ToolId); err != nil {
		return err
	}
	allowedCapsuleTypes := map[string]struct{}{
		CapsuleTypeWASM: {},
		CapsuleTypeOCI:  {},
	}
	if err := validateEnum(c.CapsuleType, allowedCapsuleTypes, "capsule_type"); err != nil {
		return err
	}
	if len(c.CapsuleHash) != 32 {
		return fmt.Errorf("capsule_hash must be 32 bytes")
	}
	if strings.TrimSpace(c.Entrypoint) == "" {
		return fmt.Errorf("entrypoint cannot be empty")
	}
	allowedRuntimes := map[string]struct{}{
		CapsuleRuntimeWasmtime:   {},
		CapsuleRuntimeContainerd: {},
	}
	if err := validateEnum(c.Runtime, allowedRuntimes, "runtime"); err != nil {
		return err
	}
	if len(c.EgressAllowlist) == 0 {
		return fmt.Errorf("egress_allowlist cannot be empty")
	}
	for i, raw := range c.EgressAllowlist {
		if err := validateHTTPURL(raw, fmt.Sprintf("egress_allowlist[%d]", i), true); err != nil {
			return err
		}
	}
	if c.ResourceLimits == nil {
		return fmt.Errorf("resource_limits cannot be nil")
	}
	if err := c.ResourceLimits.Validate(); err != nil {
		return err
	}
	if len(c.PublisherSig) == 0 {
		return fmt.Errorf("publisher_sig cannot be empty")
	}
	return nil
}

// Validate performs validation on CapsuleResourceLimits.
func (r *CapsuleResourceLimits) Validate() error {
	if r == nil {
		return fmt.Errorf("resource_limits cannot be nil")
	}
	if r.CpuMs == 0 {
		return fmt.Errorf("resource_limits.cpu_ms must be > 0")
	}
	if r.MemMb == 0 {
		return fmt.Errorf("resource_limits.mem_mb must be > 0")
	}
	if r.NetKb == 0 {
		return fmt.Errorf("resource_limits.net_kb must be > 0")
	}
	return nil
}

// Validate performs validation on WatcherRecord.
func (w *WatcherRecord) Validate() error {
	if w == nil {
		return fmt.Errorf("watcher record cannot be nil")
	}
	if strings.TrimSpace(w.WatcherAddress) == "" {
		return fmt.Errorf("watcher address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(w.WatcherAddress); err != nil {
		return fmt.Errorf("invalid watcher address: %w", err)
	}
	if w.Status != "" {
		valid := map[string]bool{
			WatcherStatusActive:  true,
			WatcherStatusJailed:  true,
			WatcherStatusSlashed: true,
		}
		if !valid[w.Status] {
			return fmt.Errorf("invalid watcher status: %s", w.Status)
		}
	}
	if w.Status != WatcherStatusSlashed && len(w.BondedAmount) == 0 {
		return fmt.Errorf("bonded amount cannot be empty for active watchers")
	}
	if _, err := requireTimestamp(w.RegisteredAt, "registered_at"); err != nil {
		return err
	}
	if _, err := requireTimestamp(w.LastUpdatedAt, "last_updated_at"); err != nil {
		return err
	}
	return nil
}

// Validate performs validation on SLOProbeReceipt.
func (r *SLOProbeReceipt) Validate() error {
	if r == nil {
		return fmt.Errorf("slo probe receipt cannot be nil")
	}
	if strings.TrimSpace(r.ReceiptId) == "" {
		return fmt.Errorf("receipt_id cannot be empty")
	}
	if strings.TrimSpace(r.ToolId) != r.ToolId {
		return fmt.Errorf("tool_id cannot contain leading or trailing whitespace")
	}
	if err := validateToolID(r.ToolId); err != nil {
		return err
	}
	if strings.TrimSpace(r.WatcherAddress) == "" {
		return fmt.Errorf("watcher_address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(r.WatcherAddress); err != nil {
		return fmt.Errorf("invalid watcher address: %w", err)
	}
	windowStart, err := requireTimestamp(r.WindowStart, "window_start")
	if err != nil {
		return err
	}
	windowEnd, err := requireTimestamp(r.WindowEnd, "window_end")
	if err != nil {
		return err
	}
	if windowEnd.Before(windowStart) {
		return fmt.Errorf("window_end cannot be before window_start")
	}
	if r.ProbeCount == 0 {
		return fmt.Errorf("probe_count must be > 0")
	}
	if r.SuccessCount+r.FailureCount > r.ProbeCount {
		return fmt.Errorf("success_count + failure_count cannot exceed probe_count")
	}
	if r.P95LatencyMs == 0 {
		return fmt.Errorf("p95_latency_ms must be > 0")
	}
	if r.ErrorRateBps > 10000 {
		return fmt.Errorf("error_rate_bps cannot exceed 10000")
	}
	if r.AvailabilityBps > 10000 {
		return fmt.Errorf("availability_bps cannot exceed 10000")
	}
	if _, err := requireTimestamp(r.SubmittedAt, "submitted_at"); err != nil {
		return err
	}
	return nil
}

// Validate performs validation on SLOProbeAggregate.
func (a *SLOProbeAggregate) Validate() error {
	if a == nil {
		return fmt.Errorf("slo probe aggregate cannot be nil")
	}
	if strings.TrimSpace(a.ToolId) != a.ToolId {
		return fmt.Errorf("tool_id cannot contain leading or trailing whitespace")
	}
	if err := validateToolID(a.ToolId); err != nil {
		return err
	}
	start, err := requireTimestamp(a.WindowStart, "window_start")
	if err != nil {
		return err
	}
	end, err := requireTimestamp(a.WindowEnd, "window_end")
	if err != nil {
		return err
	}
	if end.Before(start) {
		return fmt.Errorf("window_end cannot be before window_start")
	}
	if a.WatcherCount == 0 {
		return fmt.Errorf("watcher_count must be > 0")
	}
	if a.Version == 0 {
		return fmt.Errorf("version must be > 0")
	}
	if !isValidSLOProbeAggregateStatus(a.Status) {
		return fmt.Errorf("status must be provisional, final, challenged, or superseded")
	}
	if a.MedianAvailabilityBps > 10000 {
		return fmt.Errorf("median_availability_bps cannot exceed 10000")
	}
	if a.MedianErrorRateBps > 10000 {
		return fmt.Errorf("median_error_rate_bps cannot exceed 10000")
	}
	if _, err := requireTimestamp(a.AggregatedAt, "aggregated_at"); err != nil {
		return err
	}
	if a.Status == SLOProbeAggregateStatusSuperseded {
		if a.SupersededByVersion <= a.Version {
			return fmt.Errorf("superseded_by_version must exceed version")
		}
		if strings.TrimSpace(a.SupersededByChallengeId) == "" {
			return fmt.Errorf("superseded_by_challenge_id required when status is superseded")
		}
		if _, err := requireTimestamp(a.SupersededAt, "superseded_at"); err != nil {
			return err
		}
		return nil
	}
	if a.SupersededByVersion != 0 {
		return fmt.Errorf("superseded_by_version requires superseded status")
	}
	if strings.TrimSpace(a.SupersededByChallengeId) != "" {
		return fmt.Errorf("superseded_by_challenge_id requires superseded status")
	}
	if a.SupersededAt != nil {
		return fmt.Errorf("superseded_at requires superseded status")
	}
	return nil
}

func isValidSLOProbeAggregateStatus(status string) bool {
	switch status {
	case SLOProbeAggregateStatusProvisional,
		SLOProbeAggregateStatusFinal,
		SLOProbeAggregateStatusChallenged,
		SLOProbeAggregateStatusSuperseded:
		return true
	default:
		return false
	}
}
