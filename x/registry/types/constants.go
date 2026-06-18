
// Package types provides core constants and helpers for the registry module.
//
//revive:disable:var-naming // Cosmos module conventions use the `types` package name.
package types

import (
	"fmt"
	"strings"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/shopspring/decimal"
)

// BPSDenominator represents 100% in basis points (100% = 10000 bps)
// Used throughout the module for percentage calculations
const BPSDenominator = 10000

// Origin routing split bounds.
const (
	// OriginRoutingMinDirectBps is the governance floor for the direct payout leg.
	OriginRoutingMinDirectBps uint32 = 5000

	// OriginRoutingMaxBuybackBps caps the buyback leg to prevent excessive market impact.
	OriginRoutingMaxBuybackBps uint32 = 3000
)

// BondDenom is the default bond denomination
const BondDenom = "ulume"

// DecimalPrecision defines the number of decimal places used for string conversions.
const DecimalPrecision = 18

// Metadata keys stored in ToolCard.Metadata
const (
	MetadataKeySBOMUpdatedAt             = "sbom_updated_at"
	MetadataKeyAttestationUpdatedAt      = "attestation_updated_at"
	MetadataKeyToolContractV01           = "tool_contract_v0_1"
	MetadataKeyToolWasmContractV01       = "tool_wasm_contract_v0_1"
	MetadataKeyVerifiedBadgeMigratedAt   = "verified_badge_migrated_at"
	MetadataKeyVerifiedBadgeGraceExpires = "verified_badge_grace_expires_at"
	MetadataKeyVerifiedBadgeGraceReason  = "verified_badge_grace_reason"
	MetadataKeyVerifiedBadgeGraceTier    = "verified_badge_grace_tier"
)

// Default decimal values
var (
	DecimalZero = decimal.Zero
	DecimalOne  = decimal.NewFromInt(1)

	// Default multiplier for insurance premium
	DefaultInsuranceMultiplier = "1"

	// Tolerance for floating point comparisons as string
	DecimalToleranceString = "0.0000001"

	// Tolerance for floating point comparisons as decimal
	DecimalTolerance = decimal.RequireFromString(DecimalToleranceString)

	// Default empty decimal string
	EmptyDecimalString = "0"

	// InsuranceTargetBand is the tolerance band for insurance utilization adjustments (±5%)
	InsuranceTargetBand = decimal.RequireFromString("0.05")
)

// Receipt status values
const (
	ReceiptStatusPending  = "pending"
	ReceiptStatusSettled  = "settled"
	ReceiptStatusDisputed = "disputed"
	ReceiptStatusExpired  = "expired"
)

// Bond status values
const (
	BondStatusActive      = "active"
	BondStatusWithdrawing = "withdrawing"
	BondStatusWithdrawn   = "withdrawn"
	BondStatusSlashed     = "slashed"
)

// Watcher status values
const (
	WatcherStatusActive  = "active"
	WatcherStatusJailed  = "jailed"
	WatcherStatusSlashed = "slashed"
)

// SLO probe aggregate status values
const (
	SLOProbeAggregateStatusProvisional = "provisional"
	SLOProbeAggregateStatusFinal       = "final"
	SLOProbeAggregateStatusChallenged  = "challenged"
	SLOProbeAggregateStatusSuperseded  = "superseded"
)

// Challenge status values
const (
	ChallengeStatusPending   = "pending"
	ChallengeStatusAccepted  = "accepted"
	ChallengeStatusRejected  = "rejected"
	ChallengeStatusExpired   = "expired"
	ChallengeStatusUpheld    = "upheld"
	ChallengeStatusPartial   = "partial"
	ChallengeStatusWithdrawn = "withdrawn"
)

// License lane values
const (
	LicenseLaneBYOKey         = "byo_key"
	LicenseLaneLicensedResale = "licensed_resale"
	LicenseLaneCommunity      = "community"
	LicenseLaneProxied        = "proxied"
)

// Tool and receipt trust class values.
const (
	TrustClassPublisherSignedBonded = "publisher_signed_bonded"
	TrustClassRouterAttested        = "router_attested"
)

// Lane registry values
const (
	LaneTypePublic                   = "public"
	LaneTypeConfidentialTEE          = "confidential_tee"
	LaneTypeConfidentialPublisherTEE = "confidential_publisher_tee"
)

// TEE type identifiers for confidential lane execution environments.
const (
	LaneTEETypeSGX       = "sgx"
	LaneTEETypeSEVSNP    = "sev_snp"
	LaneTEETypeNitro     = "nitro"
	LaneTEETypeSapphire  = "sapphire"
	LaneTEETypeTrustZone = "trustedzone"
)

// Lane metering strategies for execution verification.
const (
	LaneMeterTypeTEESigned       = "tee_signed"
	LaneMeterTypeWatcherVerified = "watcher_verified"
)

// Lane log retention policies for audit trails.
const (
	LaneLogPolicyNone      = "none"
	LaneLogPolicyHashOnly  = "hash_only"
	LaneLogPolicyEncrypted = "encrypted"
)

// PII handling policies for lane data classification.
const (
	LanePIIPolicyDeny               = "deny"
	LanePIIPolicyAllowWithRedaction = "allow_with_redaction"
)

// Tool capsule values.
const (
	CapsuleTypeWASM = "wasm"
	CapsuleTypeOCI  = "oci"
)

// Capsule runtime identifiers for tool execution environments.
const (
	CapsuleRuntimeWasmtime   = "wasmtime"
	CapsuleRuntimeContainerd = "containerd"
)

// Slash severity levels
const (
	SlashSeverityMinor    = "minor"
	SlashSeverityMajor    = "major"
	SlashSeverityCritical = "critical"
)

// Default availability values (in basis points)
const (
	DefaultAvailabilityBps = 9900 // 99% availability
	DefaultErrorRateBps    = 100  // 1% error rate
	DefaultDisputeRateBps  = 10   // 0.1% dispute rate
)

// Default watcher enforcement thresholds (basis points).
const (
	DefaultWatcherDeviationBps = 2000 // 20% deviation allowed from median
	DefaultWatcherSlashBps     = 1000 // 10% slash of watcher stake on bad probe
)

// SafeDecimalFromString converts a string to decimal, returning zero if empty or invalid
// Callers should log the error if the input is expected to be valid.
//
// DoS guard: absurd shopspring exponents (outside moneyguard's ±100 bound)
// are also degraded to zero. This helper is the central read path for
// persisted decimal fields across settlement.go, invariants.go, receipt.go,
// receipt_queue.go, bond.go, and quality_rebate.go — every caller feeds the
// result into Add / Sub / Mul / Div chains that would otherwise expand
// shopspring's big.Int to multi-million digits on "1e11100100" and halt
// block production on every validator. See sibling guards: 5c237b056
// (x/router/types decimalFromString), bf5be3a18 (x/insurance/types
// CalculatePremium/EvaluatePoolHealth), cbbaba3cb, 09cffa7b4.
func SafeDecimalFromString(s string) decimal.Decimal {
	if s == "" {
		return DecimalZero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		// Return zero for invalid input - callers can use ValidateDecimalString
		// if they need error details for logging
		return DecimalZero
	}
	if !moneyguard.IsSafeExponent(d) {
		return DecimalZero
	}
	return d
}

// SafeDecimalFromStringWithError converts a string to decimal and returns any error
// This variant allows callers to handle/log errors as needed.
// Same exponent-bomb guard as SafeDecimalFromString — values whose
// shopspring exponent is outside moneyguard's ±100 bound are rejected as
// errors so downstream arithmetic cannot hang.
func SafeDecimalFromStringWithError(s string) (decimal.Decimal, error) {
	if s == "" {
		return DecimalZero, nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return DecimalZero, err
	}
	if !moneyguard.IsSafeExponent(d) {
		return DecimalZero, fmt.Errorf("decimal %q exponent out of safe range", s)
	}
	return d, nil
}

// SafeDecimalFromStringStrict converts a string to decimal for critical financial calculations
// Returns an error if the string is empty or invalid - does NOT silently return zero
// Use this for amounts that MUST be valid (e.g., payment amounts, settlement values).
//
// As a chain-wide DoS guard, this also rejects values whose shopspring
// exponent falls outside moneyguard's ±100 bound. shopspring stores
// scientific exponents symbolically (NewFromString is cheap), but every
// downstream Mul / Div / Cmp / String call forces the big.Int to expand
// to match exponents — millions of decimal digits for "1e11100100".
// Because this helper is the central "strict parse" used across settlement,
// dispute, SLA, verified-badge, reputation-IBC, and tx-prioritizer paths
// (all consensus-critical), gating here protects every caller in one shot.
func SafeDecimalFromStringStrict(s string, fieldName string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Decimal{}, fmt.Errorf("%s cannot be empty", fieldName)
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("invalid %s value '%s': %w", fieldName, s, err)
	}
	if !moneyguard.IsSafeExponent(d) {
		return decimal.Decimal{}, fmt.Errorf("%s magnitude out of range: %s", fieldName, s)
	}
	if d.IsNegative() {
		return decimal.Decimal{}, fmt.Errorf("%s cannot be negative: %s", fieldName, s)
	}
	return d, nil
}

// DecimalToString converts a decimal to string with appropriate precision
// It removes trailing zeros but maintains up to DecimalPrecision digits
func DecimalToString(d decimal.Decimal) string {
	// Handle zero case explicitly
	if d.IsZero() {
		return "0"
	}

	// Round to our maximum precision to avoid floating point artifacts
	rounded := d.Round(DecimalPrecision)

	// Use StringFixed with our precision, then remove trailing zeros
	// This ensures we never exceed DecimalPrecision digits after the decimal
	str := rounded.StringFixed(DecimalPrecision)

	// Optimize: single pass to remove trailing zeros and decimal point
	// Only process if there's a decimal point
	if idx := strings.IndexByte(str, '.'); idx >= 0 {
		// Find the last non-zero digit after decimal
		end := len(str) - 1
		for end > idx && str[end] == '0' {
			end--
		}
		// If we're at the decimal point, exclude it too
		if end == idx {
			str = str[:idx]
		} else {
			str = str[:end+1]
		}
	}

	return str
}

// ValidateDecimalString validates a decimal string and returns the parsed value.
// Same exponent-bomb guard as SafeDecimalFromString — values whose
// shopspring exponent is outside moneyguard's ±100 bound are rejected
// as field-level errors so downstream Add/Sub/Mul/Div/Cmp chains cannot
// hang block production on a symbolic-exponent input.
func ValidateDecimalString(s string, fieldName string) (decimal.Decimal, error) {
	if s == "" {
		return DecimalZero, nil // Empty is valid, means zero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return DecimalZero, fmt.Errorf("invalid %s: %w", fieldName, err)
	}
	if !moneyguard.IsSafeExponent(d) {
		return DecimalZero, fmt.Errorf("invalid %s: magnitude out of safe range", fieldName)
	}
	return d, nil
}

// SafeBPSCalculation calculates basis points safely with overflow protection
// Returns (numerator * BPSDenominator) / denominator as uint32
// For percentage calculations: numerator should be the part, denominator the whole
// Example: 50 out of 200 = SafeBPSCalculation(50, 200) = 2500 bps = 25%
func SafeBPSCalculation(numerator, denominator int64) uint32 {
	// Validate inputs
	if denominator <= 0 {
		return 0
	}
	if numerator < 0 {
		return 0
	}
	if numerator == 0 {
		return 0
	}
	if numerator > denominator {
		// Over 100%, cap at 100%
		return uint32(BPSDenominator)
	}

	// Use decimal arithmetic for precision without overflow or float64 issues
	// This avoids both integer overflow and floating point precision loss
	num := decimal.NewFromInt(numerator)
	denom := decimal.NewFromInt(denominator)
	bpsDenom := decimal.NewFromInt(BPSDenominator)

	// Calculate: (numerator / denominator) * BPSDenominator
	ratio := num.Div(denom)
	result := ratio.Mul(bpsDenom)

	// Round to nearest integer to avoid precision loss
	// For example, 1/3 * 10000 = 3333.333... should round to 3333
	rounded := result.Round(0)

	// Convert to int64 for bounds checking
	resultInt := rounded.IntPart()
	if resultInt > int64(BPSDenominator) {
		return uint32(BPSDenominator)
	}
	if resultInt < 0 {
		return 0
	}

	return uint32(resultInt)
}
