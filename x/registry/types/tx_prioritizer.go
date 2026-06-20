package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
	"lukechampine.com/blake3"
)

const (
	// MetadataKeyTxPrioritizerV1 stores optional TxPrioritizer contract overrides in ToolCard.Metadata.
	MetadataKeyTxPrioritizerV1 = "tx_prioritizer_v1"

	// TxPrioritizerContractSchemaV1 is the canonical JSON schema identifier for prioritizer metadata.
	TxPrioritizerContractSchemaV1 = "lumera.tx_prioritizer.contract.v1"
	// TxPrioritizerReputationScoreVersionV1 identifies the v1 reputation scoring formula.
	TxPrioritizerReputationScoreVersionV1 = "tx_prioritizer.reputation.v1"

	// DefaultTxPrioritizerFeeDenom is the fee denom expected by the prioritizer when a contract omits an override.
	DefaultTxPrioritizerFeeDenom = "ulac"

	// TxPrioritizer cache refresh modes.
	TxPrioritizerRefreshModeBeginBlock = "begin_block"
	TxPrioritizerRefreshModeEndBlock   = "end_block"
	TxPrioritizerRefreshModeManual     = "manual"

	// TxPrioritizer cache consistency modes.
	TxPrioritizerConsistencyLastCommit  = "last_commit"
	TxPrioritizerConsistencyBlockFrozen = "block_frozen"

	// DefaultTxPrioritizerVerifiedBoostBPS is the default additive verified boost applied after reputation scoring.
	DefaultTxPrioritizerVerifiedBoostBPS uint32 = 1000
)

var (
	txPrioritizerSuccessWeight      = decimal.RequireFromString("0.5")
	txPrioritizerDisputeWeight      = decimal.RequireFromString("0.3")
	txPrioritizerAvailabilityWeight = decimal.RequireFromString("0.2")
)

// TxPrioritizerContractV1 describes the registry-side contract used to build block-frozen prioritizer inputs.
type TxPrioritizerContractV1 struct {
	Schema     string                            `json:"$schema"`
	Fee        TxPrioritizerFeeContractV1        `json:"fee"`
	Reputation TxPrioritizerReputationContractV1 `json:"reputation"`
	Stake      TxPrioritizerStakeContractV1      `json:"stake"`
	Cache      TxPrioritizerCachePolicyV1        `json:"cache"`
}

// TxPrioritizerFeeContractV1 describes the fee dimensions exposed from ToolCard pricing to the prioritizer.
type TxPrioritizerFeeContractV1 struct {
	Denom        string `json:"denom"`
	PricingModel string `json:"pricing_model,omitempty"`
	MinimumCost  string `json:"minimum_cost,omitempty"`
	MaximumCost  string `json:"maximum_cost,omitempty"`
}

// TxPrioritizerReputationContractV1 describes how reputation should be interpreted and boosted.
type TxPrioritizerReputationContractV1 struct {
	ScoreVersion     string `json:"score_version"`
	Verified         bool   `json:"verified,omitempty"`
	VerifiedBoostBps uint32 `json:"verified_boost_bps,omitempty"`
}

// TxPrioritizerStakeContractV1 describes which stake inventory should be materialized into prioritizer inputs.
type TxPrioritizerStakeContractV1 struct {
	BondDenom    string `json:"bond_denom"`
	MinimumRatio string `json:"minimum_ratio,omitempty"`
}

// TxPrioritizerCachePolicyV1 defines how prioritizer-resolved inputs are cached across heights.
type TxPrioritizerCachePolicyV1 struct {
	RefreshMode       string `json:"refresh_mode"`
	ConsistencyMode   string `json:"consistency_mode"`
	MaxAgeBlocks      uint32 `json:"max_age_blocks"`
	FreezeWithinBlock bool   `json:"freeze_within_block"`
}

// TxPrioritizerResolvedInputsV1 is the materialized, block-frozen input set that future ABCI prioritizer logic consumes.
type TxPrioritizerResolvedInputsV1 struct {
	ToolID      string                            `json:"tool_id"`
	Publisher   string                            `json:"publisher"`
	ToolVersion string                            `json:"tool_version"`
	PolicyTag   string                            `json:"policy_tag,omitempty"`
	Fee         TxPrioritizerResolvedFeeInputsV1  `json:"fee"`
	Reputation  TxPrioritizerReputationSnapshotV1 `json:"reputation"`
	Stake       TxPrioritizerStakeSnapshotV1      `json:"stake"`
	Cache       TxPrioritizerCacheBindingV1       `json:"cache"`
}

// TxPrioritizerResolvedFeeInputsV1 captures the ToolCard fee settings that intersect with tx-local fee metadata.
type TxPrioritizerResolvedFeeInputsV1 struct {
	Denom         string `json:"denom"`
	PricingModel  string `json:"pricing_model,omitempty"`
	MinimumCost   string `json:"minimum_cost,omitempty"`
	MaximumCost   string `json:"maximum_cost,omitempty"`
	QuoteEndpoint string `json:"quote_endpoint,omitempty"`
}

// TxPrioritizerReputationSnapshotV1 captures the block-frozen reputation inputs used by the prioritizer.
type TxPrioritizerReputationSnapshotV1 struct {
	Score        string    `json:"score"`
	ScoreVersion string    `json:"score_version"`
	SuccessRate  string    `json:"success_rate"`
	DisputeRate  string    `json:"dispute_rate"`
	Availability string    `json:"availability"`
	ErrorRate    string    `json:"error_rate"`
	SampleSize   uint64    `json:"sample_size"`
	Verified     bool      `json:"verified"`
	SourceHeight int64     `json:"source_height"`
	SourceTime   time.Time `json:"source_time,omitempty"`
}

// TxPrioritizerStakeSnapshotV1 captures the block-frozen bond state used by the prioritizer.
type TxPrioritizerStakeSnapshotV1 struct {
	BondDenom                  string    `json:"bond_denom"`
	BondedAmount               string    `json:"bonded_amount"`
	MinimumRequired            string    `json:"minimum_required"`
	LockedAmount               string    `json:"locked_amount"`
	EffectiveRatio             string    `json:"effective_ratio"`
	InsurancePremiumMultiplier string    `json:"insurance_premium_multiplier"`
	Status                     string    `json:"status,omitempty"`
	SourceHeight               int64     `json:"source_height"`
	SourceTime                 time.Time `json:"source_time,omitempty"`
}

func txPrioritizerOptionalTimeUTC(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// MarshalJSON omits zero source_time values so persisted snapshots do not leak the Go zero timestamp.
func (r TxPrioritizerReputationSnapshotV1) MarshalJSON() ([]byte, error) {
	type reputationSnapshotJSON struct {
		Score        string     `json:"score"`
		ScoreVersion string     `json:"score_version"`
		SuccessRate  string     `json:"success_rate"`
		DisputeRate  string     `json:"dispute_rate"`
		Availability string     `json:"availability"`
		ErrorRate    string     `json:"error_rate"`
		SampleSize   uint64     `json:"sample_size"`
		Verified     bool       `json:"verified"`
		SourceHeight int64      `json:"source_height"`
		SourceTime   *time.Time `json:"source_time,omitempty"`
	}

	return json.Marshal(reputationSnapshotJSON{
		Score:        r.Score,
		ScoreVersion: r.ScoreVersion,
		SuccessRate:  r.SuccessRate,
		DisputeRate:  r.DisputeRate,
		Availability: r.Availability,
		ErrorRate:    r.ErrorRate,
		SampleSize:   r.SampleSize,
		Verified:     r.Verified,
		SourceHeight: r.SourceHeight,
		SourceTime:   txPrioritizerOptionalTimeUTC(r.SourceTime),
	})
}

// MarshalJSON omits zero source_time values so persisted snapshots do not leak the Go zero timestamp.
func (s TxPrioritizerStakeSnapshotV1) MarshalJSON() ([]byte, error) {
	type stakeSnapshotJSON struct {
		BondDenom                  string     `json:"bond_denom"`
		BondedAmount               string     `json:"bonded_amount"`
		MinimumRequired            string     `json:"minimum_required"`
		LockedAmount               string     `json:"locked_amount"`
		EffectiveRatio             string     `json:"effective_ratio"`
		InsurancePremiumMultiplier string     `json:"insurance_premium_multiplier"`
		Status                     string     `json:"status,omitempty"`
		SourceHeight               int64      `json:"source_height"`
		SourceTime                 *time.Time `json:"source_time,omitempty"`
	}

	return json.Marshal(stakeSnapshotJSON{
		BondDenom:                  s.BondDenom,
		BondedAmount:               s.BondedAmount,
		MinimumRequired:            s.MinimumRequired,
		LockedAmount:               s.LockedAmount,
		EffectiveRatio:             s.EffectiveRatio,
		InsurancePremiumMultiplier: s.InsurancePremiumMultiplier,
		Status:                     s.Status,
		SourceHeight:               s.SourceHeight,
		SourceTime:                 txPrioritizerOptionalTimeUTC(s.SourceTime),
	})
}

// TxPrioritizerCacheBindingV1 binds a resolved input set to the exact cache window it was built from.
type TxPrioritizerCacheBindingV1 struct {
	SourceHeight       int64  `json:"source_height"`
	RefreshedAtHeight  int64  `json:"refreshed_at_height"`
	ExpiresAfterHeight int64  `json:"expires_after_height"`
	DeterministicID    string `json:"deterministic_id"`
	RefreshMode        string `json:"refresh_mode"`
	ConsistencyMode    string `json:"consistency_mode"`
}

// DefaultTxPrioritizerContractV1 returns the canonical prioritizer contract when a ToolCard omits overrides.
func DefaultTxPrioritizerContractV1() *TxPrioritizerContractV1 {
	return &TxPrioritizerContractV1{
		Schema: TxPrioritizerContractSchemaV1,
		Fee: TxPrioritizerFeeContractV1{
			Denom: DefaultTxPrioritizerFeeDenom,
		},
		Reputation: TxPrioritizerReputationContractV1{
			ScoreVersion:     TxPrioritizerReputationScoreVersionV1,
			VerifiedBoostBps: DefaultTxPrioritizerVerifiedBoostBPS,
		},
		Stake: TxPrioritizerStakeContractV1{
			BondDenom:    BondDenom,
			MinimumRatio: "1",
		},
		Cache: TxPrioritizerCachePolicyV1{
			RefreshMode:       TxPrioritizerRefreshModeEndBlock,
			ConsistencyMode:   TxPrioritizerConsistencyBlockFrozen,
			MaxAgeBlocks:      1,
			FreezeWithinBlock: true,
		},
	}
}

// ParseTxPrioritizerContractV1 parses a JSON-encoded prioritizer contract.
func ParseTxPrioritizerContractV1(raw string) (*TxPrioritizerContractV1, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("tx prioritizer metadata is empty")
	}

	var contract TxPrioritizerContractV1
	if err := json.Unmarshal([]byte(raw), &contract); err != nil {
		return nil, fmt.Errorf("invalid tx prioritizer metadata: %w", err)
	}
	return &contract, nil
}

// Marshal returns the JSON-encoded prioritizer contract.
func (c *TxPrioritizerContractV1) Marshal() (string, error) {
	if c == nil {
		return "", fmt.Errorf("tx prioritizer contract cannot be nil")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Validate ensures the contract is semantically valid.
func (c *TxPrioritizerContractV1) Validate() error {
	if c == nil {
		return fmt.Errorf("tx prioritizer contract cannot be nil")
	}
	if strings.TrimSpace(c.Schema) == "" {
		return fmt.Errorf("tx prioritizer schema is required")
	}
	if c.Schema != TxPrioritizerContractSchemaV1 {
		return fmt.Errorf("unsupported tx prioritizer schema: %s", c.Schema)
	}
	if err := c.Fee.Validate(); err != nil {
		return fmt.Errorf("fee: %w", err)
	}
	if err := c.Reputation.Validate(); err != nil {
		return fmt.Errorf("reputation: %w", err)
	}
	if err := c.Stake.Validate(); err != nil {
		return fmt.Errorf("stake: %w", err)
	}
	if err := c.Cache.Validate(); err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	return nil
}

// Validate ensures the fee contract is semantically valid.
func (c TxPrioritizerFeeContractV1) Validate() error {
	if err := validateRequiredCanonicalText("denom", c.Denom); err != nil {
		return err
	}
	minimum, err := validateOptionalNonNegativeDecimal(c.MinimumCost, "minimum_cost")
	if err != nil {
		return err
	}
	maximum, err := validateOptionalNonNegativeDecimal(c.MaximumCost, "maximum_cost")
	if err != nil {
		return err
	}
	if !minimum.IsZero() && !maximum.IsZero() && maximum.LessThan(minimum) {
		return fmt.Errorf("maximum_cost must be >= minimum_cost")
	}
	return nil
}

// Validate ensures the reputation contract is semantically valid.
func (c TxPrioritizerReputationContractV1) Validate() error {
	if strings.TrimSpace(c.ScoreVersion) == "" {
		return fmt.Errorf("score_version is required")
	}
	if c.ScoreVersion != TxPrioritizerReputationScoreVersionV1 {
		return fmt.Errorf("unsupported score_version: %s", c.ScoreVersion)
	}
	if c.VerifiedBoostBps > BPSDenominator {
		return fmt.Errorf("verified_boost_bps must be <= %d", BPSDenominator)
	}
	return nil
}

// Validate ensures the stake contract is semantically valid.
func (c TxPrioritizerStakeContractV1) Validate() error {
	if err := validateRequiredCanonicalText("bond_denom", c.BondDenom); err != nil {
		return err
	}
	if _, err := validateOptionalNonNegativeDecimal(c.MinimumRatio, "minimum_ratio"); err != nil {
		return err
	}
	return nil
}

// Validate ensures the cache policy is semantically valid.
func (c TxPrioritizerCachePolicyV1) Validate() error {
	refreshMode := strings.TrimSpace(c.RefreshMode)
	if refreshMode != c.RefreshMode {
		return fmt.Errorf("refresh_mode must be canonical: %q", c.RefreshMode)
	}
	switch c.RefreshMode {
	case TxPrioritizerRefreshModeBeginBlock, TxPrioritizerRefreshModeEndBlock, TxPrioritizerRefreshModeManual:
	default:
		return fmt.Errorf("unsupported refresh_mode: %s", c.RefreshMode)
	}

	consistencyMode := strings.TrimSpace(c.ConsistencyMode)
	if consistencyMode != c.ConsistencyMode {
		return fmt.Errorf("consistency_mode must be canonical: %q", c.ConsistencyMode)
	}
	switch c.ConsistencyMode {
	case TxPrioritizerConsistencyLastCommit, TxPrioritizerConsistencyBlockFrozen:
	default:
		return fmt.Errorf("unsupported consistency_mode: %s", c.ConsistencyMode)
	}

	if c.MaxAgeBlocks == 0 {
		return fmt.Errorf("max_age_blocks must be > 0")
	}
	if c.ConsistencyMode == TxPrioritizerConsistencyBlockFrozen && !c.FreezeWithinBlock {
		return fmt.Errorf("freeze_within_block must be true when consistency_mode is %s", TxPrioritizerConsistencyBlockFrozen)
	}
	return nil
}

// ExtractTxPrioritizerContractV1 returns the parsed contract override from ToolCard metadata or the canonical defaults.
func ExtractTxPrioritizerContractV1(tool *ToolCard) (*TxPrioritizerContractV1, error) {
	if tool == nil {
		return nil, fmt.Errorf("tool card cannot be nil")
	}

	raw := strings.TrimSpace(tool.GetMetadata()[MetadataKeyTxPrioritizerV1])
	if raw == "" {
		return DefaultTxPrioritizerContractV1(), nil
	}

	contract, err := ParseTxPrioritizerContractV1(raw)
	if err != nil {
		return nil, err
	}
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	return contract, nil
}

// BuildTxPrioritizerResolvedInputsV1 materializes block-frozen prioritizer inputs from ToolCard, BondRecord, and SLOProbeAggregate state.
func BuildTxPrioritizerResolvedInputsV1(
	tool *ToolCard,
	bond *BondRecord,
	aggregate *SLOProbeAggregate,
	sourceHeight int64,
) (*TxPrioritizerResolvedInputsV1, error) {
	if tool == nil {
		return nil, fmt.Errorf("tool card cannot be nil")
	}
	if sourceHeight < 0 {
		return nil, fmt.Errorf("source_height cannot be negative")
	}

	contract, err := ExtractTxPrioritizerContractV1(tool)
	if err != nil {
		return nil, err
	}

	fee := buildTxPrioritizerResolvedFeeInputsV1(tool, contract)
	reputation := buildTxPrioritizerReputationSnapshotV1(bond, aggregate, contract, sourceHeight)
	stake, err := buildTxPrioritizerStakeSnapshotV1(bond, contract, sourceHeight)
	if err != nil {
		return nil, err
	}

	resolved := &TxPrioritizerResolvedInputsV1{
		ToolID:      strings.TrimSpace(tool.GetToolId()),
		Publisher:   strings.TrimSpace(tool.GetOwner()),
		ToolVersion: strings.TrimSpace(tool.GetVersion()),
		PolicyTag:   strings.TrimSpace(tool.GetPolicyTag()),
		Fee:         fee,
		Reputation:  reputation,
		Stake:       stake,
	}
	resolved.Cache = buildTxPrioritizerCacheBindingV1(resolved, bond, aggregate, contract, sourceHeight)

	if err := resolved.Validate(); err != nil {
		return nil, err
	}
	return resolved, nil
}

// Validate ensures the resolved snapshot contains the required canonical fields.
func (r *TxPrioritizerResolvedInputsV1) Validate() error {
	if r == nil {
		return fmt.Errorf("resolved inputs cannot be nil")
	}
	if err := validateRequiredCanonicalText("tool_id", r.ToolID); err != nil {
		return err
	}
	if err := validateRequiredCanonicalText("publisher", r.Publisher); err != nil {
		return err
	}
	if err := validateRequiredCanonicalText("tool_version", r.ToolVersion); err != nil {
		return err
	}
	if err := validateRequiredCanonicalText("fee.denom", r.Fee.Denom); err != nil {
		return err
	}
	if err := r.Reputation.Validate(); err != nil {
		return fmt.Errorf("reputation: %w", err)
	}
	if err := r.Stake.Validate(); err != nil {
		return fmt.Errorf("stake: %w", err)
	}
	if err := r.Cache.Validate(); err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	return nil
}

func validateRequiredCanonicalText(field string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must be canonical: %q", field, value)
	}
	return nil
}

// Validate ensures the reputation snapshot is well-formed.
func (r TxPrioritizerReputationSnapshotV1) Validate() error {
	if err := validateRequiredCanonicalText("score_version", r.ScoreVersion); err != nil {
		return err
	}
	if _, err := SafeDecimalFromStringStrict(zeroIfEmpty(r.Score), "score"); err != nil {
		return err
	}
	if _, err := validateClosedUnitInterval(r.SuccessRate, "success_rate"); err != nil {
		return err
	}
	if _, err := validateClosedUnitInterval(r.DisputeRate, "dispute_rate"); err != nil {
		return err
	}
	if _, err := validateClosedUnitInterval(r.Availability, "availability"); err != nil {
		return err
	}
	if _, err := validateClosedUnitInterval(r.ErrorRate, "error_rate"); err != nil {
		return err
	}
	if r.SourceHeight < 0 {
		return fmt.Errorf("source_height cannot be negative")
	}
	return nil
}

// Validate ensures the stake snapshot is well-formed.
func (s TxPrioritizerStakeSnapshotV1) Validate() error {
	if err := validateRequiredCanonicalText("bond_denom", s.BondDenom); err != nil {
		return err
	}
	if _, err := SafeDecimalFromStringStrict(zeroIfEmpty(s.BondedAmount), "bonded_amount"); err != nil {
		return err
	}
	if _, err := SafeDecimalFromStringStrict(zeroIfEmpty(s.MinimumRequired), "minimum_required"); err != nil {
		return err
	}
	if _, err := SafeDecimalFromStringStrict(zeroIfEmpty(s.LockedAmount), "locked_amount"); err != nil {
		return err
	}
	if _, err := SafeDecimalFromStringStrict(zeroIfEmpty(s.EffectiveRatio), "effective_ratio"); err != nil {
		return err
	}
	if zeroIfEmpty(s.InsurancePremiumMultiplier) != "" {
		if _, err := SafeDecimalFromStringStrict(zeroIfEmpty(s.InsurancePremiumMultiplier), "insurance_premium_multiplier"); err != nil {
			return err
		}
	}
	if s.SourceHeight < 0 {
		return fmt.Errorf("source_height cannot be negative")
	}
	return nil
}

// Validate ensures the cache binding is well-formed.
func (c TxPrioritizerCacheBindingV1) Validate() error {
	if c.SourceHeight < 0 {
		return fmt.Errorf("source_height cannot be negative")
	}
	if c.RefreshedAtHeight < 0 {
		return fmt.Errorf("refreshed_at_height cannot be negative")
	}
	if c.ExpiresAfterHeight < c.RefreshedAtHeight {
		return fmt.Errorf("expires_after_height must be >= refreshed_at_height")
	}
	if err := validateRequiredCanonicalText("deterministic_id", c.DeterministicID); err != nil {
		return err
	}
	policy := TxPrioritizerCachePolicyV1{
		RefreshMode:       c.RefreshMode,
		ConsistencyMode:   c.ConsistencyMode,
		MaxAgeBlocks:      uint32(maxInt64(1, c.ExpiresAfterHeight-c.RefreshedAtHeight)),
		FreezeWithinBlock: c.ConsistencyMode == TxPrioritizerConsistencyBlockFrozen,
	}
	return policy.Validate()
}

func buildTxPrioritizerResolvedFeeInputsV1(tool *ToolCard, contract *TxPrioritizerContractV1) TxPrioritizerResolvedFeeInputsV1 {
	pricing := tool.GetPricing()
	fee := TxPrioritizerResolvedFeeInputsV1{
		Denom: strings.TrimSpace(contract.Fee.Denom),
	}
	if pricing == nil {
		return fee
	}

	fee.PricingModel = firstNonEmpty(strings.TrimSpace(contract.Fee.PricingModel), strings.TrimSpace(pricing.GetModel()))
	fee.MinimumCost = firstNonEmpty(strings.TrimSpace(contract.Fee.MinimumCost), strings.TrimSpace(pricing.GetMinimumCost()))
	fee.MaximumCost = firstNonEmpty(strings.TrimSpace(contract.Fee.MaximumCost), strings.TrimSpace(pricing.GetMaximumCost()))
	fee.QuoteEndpoint = strings.TrimSpace(pricing.GetQuoteEndpoint())
	return fee
}

func buildTxPrioritizerReputationSnapshotV1(
	bond *BondRecord,
	aggregate *SLOProbeAggregate,
	contract *TxPrioritizerContractV1,
	sourceHeight int64,
) TxPrioritizerReputationSnapshotV1 {
	successRate := DecimalZero
	disputeRate := DecimalZero
	availability := DecimalZero
	errorRate := DecimalZero
	sampleSize := uint64(0)

	var sourceTime time.Time
	if bond != nil {
		totalCalls := bond.GetSuccessfulCalls() + bond.GetFailedCalls()
		sampleSize = totalCalls
		if totalCalls > 0 {
			successRate = decimal.NewFromInt(int64(bond.GetSuccessfulCalls())).Div(decimal.NewFromInt(int64(totalCalls)))
			disputeRate = decimal.NewFromInt(int64(bond.GetDisputeCount())).Div(decimal.NewFromInt(int64(totalCalls)))
		}
		if ts := bond.GetLastUpdatedAt(); !ts.IsZero() {
			sourceTime = ts.UTC()
		} else if ts := bond.GetBondedAt(); !ts.IsZero() {
			sourceTime = ts.UTC()
		}
	}
	if aggregate != nil {
		availability = decimal.NewFromInt(int64(aggregate.GetMedianAvailabilityBps())).Div(decimal.NewFromInt(BPSDenominator))
		errorRate = decimal.NewFromInt(int64(aggregate.GetMedianErrorRateBps())).Div(decimal.NewFromInt(BPSDenominator))
		if ts := aggregate.GetAggregatedAt(); !ts.IsZero() {
			sourceTime = ts.UTC()
		}
	}

	score := DecimalZero
	if sampleSize > 0 {
		score = successRate.Mul(txPrioritizerSuccessWeight).
			Add(DecimalOne.Sub(clampUnitInterval(disputeRate)).Mul(txPrioritizerDisputeWeight))
	}
	if !availability.IsZero() {
		score = score.Add(clampUnitInterval(availability).Mul(txPrioritizerAvailabilityWeight))
	}
	score = clampUnitInterval(score)

	return TxPrioritizerReputationSnapshotV1{
		Score:        DecimalToString(score),
		ScoreVersion: contract.Reputation.ScoreVersion,
		SuccessRate:  DecimalToString(clampUnitInterval(successRate)),
		DisputeRate:  DecimalToString(clampUnitInterval(disputeRate)),
		Availability: DecimalToString(clampUnitInterval(availability)),
		ErrorRate:    DecimalToString(clampUnitInterval(errorRate)),
		SampleSize:   sampleSize,
		Verified:     contract.Reputation.Verified,
		SourceHeight: sourceHeight,
		SourceTime:   sourceTime,
	}
}

func buildTxPrioritizerStakeSnapshotV1(
	bond *BondRecord,
	contract *TxPrioritizerContractV1,
	sourceHeight int64,
) (TxPrioritizerStakeSnapshotV1, error) {
	snapshot := TxPrioritizerStakeSnapshotV1{
		BondDenom:                  strings.TrimSpace(contract.Stake.BondDenom),
		BondedAmount:               "0",
		MinimumRequired:            "0",
		LockedAmount:               "0",
		EffectiveRatio:             "0",
		InsurancePremiumMultiplier: DefaultInsuranceMultiplier,
		SourceHeight:               sourceHeight,
	}
	if bond == nil {
		return snapshot, nil
	}

	bondedAmount, err := sumCoinAmountsByDenom(bond.GetBondedAmount(), snapshot.BondDenom)
	if err != nil {
		return TxPrioritizerStakeSnapshotV1{}, fmt.Errorf("bonded_amount: %w", err)
	}
	minimumRequired, err := sumCoinAmountsByDenom(bond.GetMinimumRequired(), snapshot.BondDenom)
	if err != nil {
		return TxPrioritizerStakeSnapshotV1{}, fmt.Errorf("minimum_required: %w", err)
	}
	lockedAmount, err := sumCoinAmountsByDenom(bond.GetLockedAmount(), snapshot.BondDenom)
	if err != nil {
		return TxPrioritizerStakeSnapshotV1{}, fmt.Errorf("locked_amount: %w", err)
	}

	effectiveRatio := DecimalZero
	if minimumRequired.GreaterThan(DecimalZero) {
		effectiveRatio = bondedAmount.Div(minimumRequired)
	} else if bondedAmount.GreaterThan(DecimalZero) {
		effectiveRatio = DecimalOne
	}

	snapshot.BondedAmount = DecimalToString(bondedAmount)
	snapshot.MinimumRequired = DecimalToString(minimumRequired)
	snapshot.LockedAmount = DecimalToString(lockedAmount)
	snapshot.EffectiveRatio = DecimalToString(effectiveRatio)
	snapshot.InsurancePremiumMultiplier = firstNonEmpty(strings.TrimSpace(bond.GetInsurancePremiumMultiplier()), DefaultInsuranceMultiplier)
	snapshot.Status = strings.TrimSpace(bond.GetStatus())
	if ts := bond.GetLastUpdatedAt(); !ts.IsZero() {
		snapshot.SourceTime = ts.UTC()
	} else if ts := bond.GetBondedAt(); !ts.IsZero() {
		snapshot.SourceTime = ts.UTC()
	}

	return snapshot, nil
}

func buildTxPrioritizerCacheBindingV1(
	resolved *TxPrioritizerResolvedInputsV1,
	bond *BondRecord,
	aggregate *SLOProbeAggregate,
	contract *TxPrioritizerContractV1,
	sourceHeight int64,
) TxPrioritizerCacheBindingV1 {
	expiresAfter := sourceHeight + int64(contract.Cache.MaxAgeBlocks)
	if contract.Cache.MaxAgeBlocks == 0 {
		expiresAfter = sourceHeight
	}

	seed := struct {
		ToolID        string `json:"tool_id"`
		ToolVersion   string `json:"tool_version"`
		Publisher     string `json:"publisher"`
		PolicyTag     string `json:"policy_tag,omitempty"`
		Reputation    string `json:"reputation_score"`
		StakeRatio    string `json:"stake_ratio"`
		FeeDenom      string `json:"fee_denom"`
		RefreshMode   string `json:"refresh_mode"`
		Consistency   string `json:"consistency_mode"`
		SourceHeight  int64  `json:"source_height"`
		BondUpdatedAt string `json:"bond_updated_at,omitempty"`
		SLOAggregated string `json:"slo_aggregated_at,omitempty"`
	}{
		ToolID:       resolved.ToolID,
		ToolVersion:  resolved.ToolVersion,
		Publisher:    resolved.Publisher,
		PolicyTag:    resolved.PolicyTag,
		Reputation:   resolved.Reputation.Score,
		StakeRatio:   resolved.Stake.EffectiveRatio,
		FeeDenom:     resolved.Fee.Denom,
		RefreshMode:  contract.Cache.RefreshMode,
		Consistency:  contract.Cache.ConsistencyMode,
		SourceHeight: sourceHeight,
	}
	if bond != nil {
		if ts := bond.GetLastUpdatedAt(); !ts.IsZero() {
			seed.BondUpdatedAt = ts.UTC().Format(time.RFC3339Nano)
		} else if ts := bond.GetBondedAt(); !ts.IsZero() {
			seed.BondUpdatedAt = ts.UTC().Format(time.RFC3339Nano)
		}
	}
	if aggregate != nil && !aggregate.GetAggregatedAt().IsZero() {
		seed.SLOAggregated = aggregate.GetAggregatedAt().UTC().Format(time.RFC3339Nano)
	}

	raw, _ := json.Marshal(seed)
	sum := blake3.Sum256(raw)

	return TxPrioritizerCacheBindingV1{
		SourceHeight:       sourceHeight,
		RefreshedAtHeight:  sourceHeight,
		ExpiresAfterHeight: expiresAfter,
		DeterministicID:    "blake3:" + hex.EncodeToString(sum[:]),
		RefreshMode:        contract.Cache.RefreshMode,
		ConsistencyMode:    contract.Cache.ConsistencyMode,
	}
}

func sumCoinAmountsByDenom(coins sdk.Coins, denom string) (decimal.Decimal, error) {
	total := DecimalZero
	for _, coin := range coins {
		if strings.TrimSpace(coin.Denom) != denom {
			continue
		}
		amount, err := SafeDecimalFromStringStrict(coin.Amount.String(), "coin.amount")
		if err != nil {
			return decimal.Decimal{}, err
		}
		total = total.Add(amount)
	}
	return total, nil
}

func validateOptionalNonNegativeDecimal(value string, field string) (decimal.Decimal, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DecimalZero, nil
	}
	return SafeDecimalFromStringStrict(trimmed, field)
}

func validateClosedUnitInterval(value string, field string) (decimal.Decimal, error) {
	dec, err := SafeDecimalFromStringStrict(zeroIfEmpty(value), field)
	if err != nil {
		return decimal.Decimal{}, err
	}
	if dec.LessThan(DecimalZero) || dec.GreaterThan(DecimalOne) {
		return decimal.Decimal{}, fmt.Errorf("%s must be between 0 and 1", field)
	}
	return dec, nil
}

func clampUnitInterval(value decimal.Decimal) decimal.Decimal {
	if value.LessThan(DecimalZero) {
		return DecimalZero
	}
	if value.GreaterThan(DecimalOne) {
		return DecimalOne
	}
	return value
}

func zeroIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "0"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
