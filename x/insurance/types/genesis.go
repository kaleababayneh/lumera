
package types

import (
	"fmt"
	"math"
	"time"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/shopspring/decimal"
)

// MaxClaimWindowSeconds is the largest claim review window that fits in time.Duration.
const MaxClaimWindowSeconds = int64(math.MaxInt64 / int64(time.Second))

// ClaimWindowDuration validates claim-window seconds before converting to time.Duration.
func ClaimWindowDuration(seconds int64) (time.Duration, error) {
	if seconds <= 0 {
		return 0, fmt.Errorf("claim window seconds must be positive")
	}
	if seconds > MaxClaimWindowSeconds {
		return 0, fmt.Errorf("claim window seconds exceeds maximum safe duration seconds (%d)", MaxClaimWindowSeconds)
	}
	return time.Duration(seconds) * time.Second, nil
}

// GenesisState defines the insurance module's genesis state
// It intentionally mirrors the proto definitions in lumera/insurance/v1/
// to keep JSON and on-chain representations aligned.
type GenesisState struct {
	Params         *Params          `json:"params"`
	Pool           *PoolState       `json:"pool,omitempty"`
	Claims         []*Claim         `json:"claims"`
	Contributions  []*Contribution  `json:"contributions"`
	PublisherRisks []*PublisherRisk `json:"publisher_risks"`
	Payouts        []*Payout        `json:"payouts"`
	Metrics        *PoolMetrics     `json:"metrics,omitempty"`
	ClaimSequence  uint64           `json:"claim_sequence"`
	PayoutSequence uint64           `json:"payout_sequence"`
}

// DefaultGenesis returns the canonical genesis configuration for the module.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		Pool:           DefaultPoolState(),
		Claims:         []*Claim{},
		Contributions:  []*Contribution{},
		PublisherRisks: []*PublisherRisk{},
		Payouts:        []*Payout{},
		Metrics:        DefaultPoolMetrics(),
		ClaimSequence:  1,
		PayoutSequence: 1,
	}
}

// DefaultParams returns default insurance parameters that satisfy ValidateBasic.
func DefaultParams() *Params {
	return &Params{
		InsurancePoolBps:     300,    // 3%
		TargetUtilization:    "0.2",  // 20% target utilisation
		MinPoolBalance:       "1000", // 1000 LAC minimum
		MaxClaimPercent:      "0.1",  // 10% of pool max per claim
		ClaimWindowSeconds:   86400,  // 24 hours
		DisputeStakeLac:      "100",  // 100 LAC to dispute
		PremiumAdjustmentBps: 25,     // 0.25% adjustment per step
		AutoApproveThreshold: "10",   // Auto-approve claims under 10 LAC
		SlashDecayDays:       30,     // 30 days decay
		MaxClaimsPerBlock:    100,    // Process up to 100 claims per block
		MaxPayoutsPerBlock:   50,     // Process up to 50 payouts per block
		Enabled:              true,
	}
}

// DefaultPoolState returns a default insurance pool state.
func DefaultPoolState() *PoolState {
	return &PoolState{
		TotalFunds:         "0",
		AvailableFunds:     "0",
		ReservedFunds:      "0",
		TotalContributions: "0",
		TotalPayouts:       "0",
		TargetUtilization:  "0.2",
		CurrentUtilization: "0",
		Status:             PoolStatus_POOL_STATUS_HEALTHY,
	}
}

// DefaultPoolMetrics returns default monitoring metrics.
func DefaultPoolMetrics() *PoolMetrics {
	return &PoolMetrics{
		TotalContributions_24H: "0",
		TotalPayouts_24H:       "0",
		PendingClaims:          0,
		AverageClaimAmount:     "0",
		ClaimApprovalRate:      "0",
		PoolHealthScore:        "100",
		RiskExposure:           "0",
		CoverageRatio:          "1.0",
		UtilizationEwma:        "0",
		DisputeRateEwma:        "0",
		Samples:                0,
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if gs.Params == nil {
		return fmt.Errorf("params cannot be nil")
	}

	if err := gs.Params.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	if gs.Pool != nil {
		totalFunds, err := decimal.NewFromString(gs.Pool.TotalFunds)
		if err != nil {
			return fmt.Errorf("invalid pool total funds: %w", err)
		}
		// Consensus-halt guard: PoolState fields are later read by the keeper
		// (e.g. keeper.go:491 totalFunds.Div(...)) where shopspring expands a
		// symbolic big.Int on Cmp/Div/Mul/String. A community genesis file or
		// ExportGenesis round-trip that plants "1e11100100" here would brick
		// every validator on the first block that touches the pool. Reject
		// synchronously at genesis validation.
		if !moneyguard.IsSafeExponent(totalFunds) {
			return fmt.Errorf("pool total funds magnitude out of range")
		}
		if totalFunds.IsNegative() {
			return fmt.Errorf("pool total funds cannot be negative")
		}

		availableFunds, err := decimal.NewFromString(gs.Pool.AvailableFunds)
		if err != nil {
			return fmt.Errorf("invalid pool available funds: %w", err)
		}
		if !moneyguard.IsSafeExponent(availableFunds) {
			return fmt.Errorf("pool available funds magnitude out of range")
		}
		if availableFunds.IsNegative() {
			return fmt.Errorf("pool available funds cannot be negative")
		}

		reservedFunds, err := decimal.NewFromString(gs.Pool.ReservedFunds)
		if err != nil {
			return fmt.Errorf("invalid pool reserved funds: %w", err)
		}
		if !moneyguard.IsSafeExponent(reservedFunds) {
			return fmt.Errorf("pool reserved funds magnitude out of range")
		}
		if reservedFunds.IsNegative() {
			return fmt.Errorf("pool reserved funds cannot be negative")
		}
		if !availableFunds.Add(reservedFunds).Equal(totalFunds) {
			return fmt.Errorf("pool total funds must equal available funds plus reserved funds")
		}
		if err := validatePoolStatus(gs.Pool.Status); err != nil {
			return err
		}
		if err := validateOptionalTimestamp("pool", "last_updated", gs.Pool.LastUpdated); err != nil {
			return err
		}
	}

	if gs.Metrics != nil {
		if err := validateOptionalTimestamp("metrics", "last_adjudicated_at", gs.Metrics.LastAdjudicatedAt); err != nil {
			return err
		}
		if err := validateOptionalTimestamp("metrics", "last_premium_adjust_at", gs.Metrics.LastPremiumAdjustAt); err != nil {
			return err
		}
	}

	claimIDs := make(map[string]struct{})
	for i, claim := range gs.Claims {
		if claim == nil {
			return fmt.Errorf("claim entry at index %d cannot be nil", i)
		}
		if _, exists := claimIDs[claim.Id]; exists {
			return fmt.Errorf("duplicate claim ID: %s", claim.Id)
		}
		claimIDs[claim.Id] = struct{}{}
		if err := validateClaimStatus(claim.Id, claim.Status); err != nil {
			return err
		}
		if !claim.ClaimedAmount.Amount.IsNil() {
			// ClaimedAmount is a value sdk.Coin (gogoproto nullable=false);
			// its Amount is a cosmossdk.io/math.Int, so a symbolic-exponent
			// string can never reach here (the wire decoder rejects it). We
			// only need to reject a negative magnitude.
			if claim.ClaimedAmount.Amount.IsNegative() {
				return fmt.Errorf("claim %s has negative amount", claim.Id)
			}
		}
		if err := validateOptionalTimestamp("claim "+claim.Id, "created_at", claim.CreatedAt); err != nil {
			return err
		}
		if err := validateOptionalTimestamp("claim "+claim.Id, "updated_at", claim.UpdatedAt); err != nil {
			return err
		}
		if err := validateOptionalTimestamp("claim "+claim.Id, "resolved_at", claim.ResolvedAt); err != nil {
			return err
		}
		if err := validateTimestampNotBefore("claim "+claim.Id, "updated_at", claim.UpdatedAt, "created_at", claim.CreatedAt); err != nil {
			return err
		}
		if err := validateTimestampNotBefore("claim "+claim.Id, "resolved_at", claim.ResolvedAt, "created_at", claim.CreatedAt); err != nil {
			return err
		}
	}

	for i, contribution := range gs.Contributions {
		if contribution == nil {
			return fmt.Errorf("contribution entry at index %d cannot be nil", i)
		}
		if err := validateOptionalTimestamp(fmt.Sprintf("contribution entry %d", i), "timestamp", contribution.Timestamp); err != nil {
			return err
		}
	}

	for i, risk := range gs.PublisherRisks {
		if risk == nil {
			return fmt.Errorf("publisher risk entry at index %d cannot be nil", i)
		}
		owner := fmt.Sprintf("publisher risk entry %d", i)
		if risk.PublisherId != "" || risk.ToolId != "" {
			owner = fmt.Sprintf("publisher risk %s/%s", risk.PublisherId, risk.ToolId)
		}
		if err := validateOptionalTimestamp(owner, "last_slash_time", risk.LastSlashTime); err != nil {
			return err
		}
		if err := validateOptionalTimestamp(owner, "last_evaluated", risk.LastEvaluated); err != nil {
			return err
		}
	}

	for i, payout := range gs.Payouts {
		if payout == nil {
			return fmt.Errorf("payout entry at index %d cannot be nil", i)
		}
		owner := fmt.Sprintf("payout entry %d", i)
		if payout.Id != "" {
			owner = "payout " + payout.Id
		}
		if err := validatePayoutStatus(owner, payout.Status); err != nil {
			return err
		}
		if err := validateOptionalTimestamp(owner, "paid_at", payout.PaidAt); err != nil {
			return err
		}
	}

	if gs.ClaimSequence == 0 {
		return fmt.Errorf("claim sequence must be greater than 0")
	}
	if gs.PayoutSequence == 0 {
		return fmt.Errorf("payout sequence must be greater than 0")
	}

	return nil
}

func validatePoolStatus(status PoolStatus) error {
	switch status {
	case PoolStatus_POOL_STATUS_HEALTHY,
		PoolStatus_POOL_STATUS_UNDERFUNDED,
		PoolStatus_POOL_STATUS_CRITICAL,
		PoolStatus_POOL_STATUS_OVERFUNDED:
		return nil
	default:
		return fmt.Errorf("pool status must be specified and known: %d", status)
	}
}

func validateClaimStatus(claimID string, status ClaimStatus) error {
	switch status {
	case ClaimStatus_CLAIM_STATUS_PENDING,
		ClaimStatus_CLAIM_STATUS_APPROVED,
		ClaimStatus_CLAIM_STATUS_REJECTED,
		ClaimStatus_CLAIM_STATUS_PAID,
		ClaimStatus_CLAIM_STATUS_EXPIRED,
		ClaimStatus_CLAIM_STATUS_DISPUTED:
		return nil
	default:
		return fmt.Errorf("claim %s status must be specified and known: %d", claimID, status)
	}
}

func validatePayoutStatus(owner string, status PayoutStatus) error {
	switch status {
	case PayoutStatus_PAYOUT_STATUS_PENDING,
		PayoutStatus_PAYOUT_STATUS_COMPLETED,
		PayoutStatus_PAYOUT_STATUS_FAILED:
		return nil
	default:
		return fmt.Errorf("%s status must be specified and known: %d", owner, status)
	}
}

func validateOptionalTimestamp(owner, field string, ts *time.Time) error {
	// A *time.Time is either nil (unset, valid) or a well-formed time.Time
	// value; gogoproto's stdtime decoding cannot produce an invalid time.
	_ = owner
	_ = field
	_ = ts
	return nil
}

func validateTimestampNotBefore(owner, laterField string, later *time.Time, earlierField string, earlier *time.Time) error {
	if later == nil || earlier == nil {
		return nil
	}
	if later.Before(*earlier) {
		return fmt.Errorf("%s %s cannot be before %s", owner, laterField, earlierField)
	}
	return nil
}

// ValidateBasic validates insurance parameters. It mirrors the economic
// constraints described in specs/ECONOMICS.md so the keeper can enforce them via
// governance and message handlers.
func (p *Params) ValidateBasic() error {
	if p == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if p.InsurancePoolBps > 10_000 {
		return fmt.Errorf("insurance pool BPS must be between 0 and 10000")
	}

	targetUtil, err := decimal.NewFromString(p.TargetUtilization)
	if err != nil {
		return fmt.Errorf("invalid target utilization: %w", err)
	}
	// Reject absurd shopspring exponents at the consensus boundary.
	// ValidateBasic runs on every validator via msgServer.UpdateParams
	// (governance) and keeper.SetParams; a symbolic exponent on the
	// GreaterThan comparison below would expand the big.Int and halt
	// block production.
	if !moneyguard.IsSafeExponent(targetUtil) {
		return fmt.Errorf("target utilization magnitude out of range")
	}
	if targetUtil.IsNegative() || targetUtil.GreaterThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("target utilization must be between 0 and 1")
	}

	minBalance, err := decimal.NewFromString(p.MinPoolBalance)
	if err != nil {
		return fmt.Errorf("invalid min pool balance: %w", err)
	}
	if !moneyguard.IsSafeExponent(minBalance) {
		return fmt.Errorf("min pool balance magnitude out of range")
	}
	if minBalance.IsNegative() {
		return fmt.Errorf("min pool balance cannot be negative")
	}

	maxClaim, err := decimal.NewFromString(p.MaxClaimPercent)
	if err != nil {
		return fmt.Errorf("invalid max claim percent: %w", err)
	}
	// Same consensus-halt guard — GreaterThan below would expand the big.Int.
	if !moneyguard.IsSafeExponent(maxClaim) {
		return fmt.Errorf("max claim percent magnitude out of range")
	}
	if maxClaim.IsNegative() || maxClaim.GreaterThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("max claim percent must be between 0 and 1")
	}

	if _, err := ClaimWindowDuration(p.ClaimWindowSeconds); err != nil {
		return err
	}

	disputeStake, err := decimal.NewFromString(p.DisputeStakeLac)
	if err != nil {
		return fmt.Errorf("invalid dispute stake: %w", err)
	}
	if !moneyguard.IsSafeExponent(disputeStake) {
		return fmt.Errorf("dispute stake magnitude out of range")
	}
	if disputeStake.IsNegative() {
		return fmt.Errorf("dispute stake cannot be negative")
	}

	if p.PremiumAdjustmentBps > 1_000 {
		return fmt.Errorf("premium adjustment BPS must be between 0 and 1000")
	}

	autoApprove, err := decimal.NewFromString(p.AutoApproveThreshold)
	if err != nil {
		return fmt.Errorf("invalid auto approve threshold: %w", err)
	}
	if !moneyguard.IsSafeExponent(autoApprove) {
		return fmt.Errorf("auto approve threshold magnitude out of range")
	}
	if autoApprove.IsNegative() {
		return fmt.Errorf("auto approve threshold cannot be negative")
	}

	if p.SlashDecayDays == 0 {
		return fmt.Errorf("slash decay days must be greater than 0")
	}

	if p.MaxClaimsPerBlock == 0 {
		return fmt.Errorf("max claims per block must be positive")
	}
	if p.MaxPayoutsPerBlock == 0 {
		return fmt.Errorf("max payouts per block must be positive")
	}

	return nil
}
