
package types

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func validInsuranceAddress(seed string) string {
	return sdk.AccAddress([]byte(seed)).String()
}

// ---------- DefaultParams ----------

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	require.NotNil(t, p)
	assert.Equal(t, uint32(300), p.InsurancePoolBps)
	assert.Equal(t, "0.2", p.TargetUtilization)
	assert.Equal(t, "1000", p.MinPoolBalance)
	assert.Equal(t, "0.1", p.MaxClaimPercent)
	assert.Equal(t, int64(86400), p.ClaimWindowSeconds)
	assert.Equal(t, "100", p.DisputeStakeLac)
	assert.Equal(t, uint32(25), p.PremiumAdjustmentBps)
	assert.Equal(t, "10", p.AutoApproveThreshold)
	assert.Equal(t, uint32(30), p.SlashDecayDays)
	assert.Equal(t, uint32(100), p.MaxClaimsPerBlock)
	assert.Equal(t, uint32(50), p.MaxPayoutsPerBlock)
	assert.True(t, p.Enabled)
}

func TestDefaultParams_ValidateBasic(t *testing.T) {
	require.NoError(t, DefaultParams().ValidateBasic())
}

// ---------- Params.ValidateBasic ----------

func TestParams_ValidateBasic_NilParams(t *testing.T) {
	var p *Params
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_InsurancePoolBpsAboveMax(t *testing.T) {
	p := DefaultParams()
	p.InsurancePoolBps = 10_001
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_InsurancePoolBpsAtMax(t *testing.T) {
	p := DefaultParams()
	p.InsurancePoolBps = 10_000
	require.NoError(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_InsurancePoolBpsZero(t *testing.T) {
	p := DefaultParams()
	p.InsurancePoolBps = 0
	require.NoError(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_InvalidTargetUtilization(t *testing.T) {
	p := DefaultParams()
	p.TargetUtilization = "not_a_number"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_NegativeTargetUtilization(t *testing.T) {
	p := DefaultParams()
	p.TargetUtilization = "-0.1"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_TargetUtilizationAboveOne(t *testing.T) {
	p := DefaultParams()
	p.TargetUtilization = "1.1"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_TargetUtilizationExactlyOne(t *testing.T) {
	p := DefaultParams()
	p.TargetUtilization = "1.0"
	require.NoError(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_InvalidMinPoolBalance(t *testing.T) {
	p := DefaultParams()
	p.MinPoolBalance = "abc"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_NegativeMinPoolBalance(t *testing.T) {
	p := DefaultParams()
	p.MinPoolBalance = "-100"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_InvalidMaxClaimPercent(t *testing.T) {
	p := DefaultParams()
	p.MaxClaimPercent = "xyz"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_NegativeMaxClaimPercent(t *testing.T) {
	p := DefaultParams()
	p.MaxClaimPercent = "-0.05"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_MaxClaimPercentAboveOne(t *testing.T) {
	p := DefaultParams()
	p.MaxClaimPercent = "1.1"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_ZeroClaimWindow(t *testing.T) {
	p := DefaultParams()
	p.ClaimWindowSeconds = 0
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_NegativeClaimWindow(t *testing.T) {
	p := DefaultParams()
	p.ClaimWindowSeconds = -1
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_InvalidDisputeStake(t *testing.T) {
	p := DefaultParams()
	p.DisputeStakeLac = "not_valid"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_NegativeDisputeStake(t *testing.T) {
	p := DefaultParams()
	p.DisputeStakeLac = "-10"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_PremiumBpsAboveMax(t *testing.T) {
	p := DefaultParams()
	p.PremiumAdjustmentBps = 1001
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_PremiumBpsAtMax(t *testing.T) {
	p := DefaultParams()
	p.PremiumAdjustmentBps = 1000
	require.NoError(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_InvalidAutoApprove(t *testing.T) {
	p := DefaultParams()
	p.AutoApproveThreshold = "bad"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_NegativeAutoApprove(t *testing.T) {
	p := DefaultParams()
	p.AutoApproveThreshold = "-5"
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_ZeroSlashDecayDays(t *testing.T) {
	p := DefaultParams()
	p.SlashDecayDays = 0
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_ZeroMaxClaimsPerBlock(t *testing.T) {
	p := DefaultParams()
	p.MaxClaimsPerBlock = 0
	require.Error(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_ZeroMaxPayoutsPerBlock(t *testing.T) {
	p := DefaultParams()
	p.MaxPayoutsPerBlock = 0
	require.Error(t, p.ValidateBasic())
}

// TestParams_ValidateBasic_RejectsAbsurdExponent is the consensus-halt
// regression for the shopspring-decimal DoS vector in x/insurance. Params
// are set via MsgUpdateParams (governance) which calls
// msgServer.UpdateParams -> msg.Params.ValidateBasic on every validator.
// TargetUtilization and MaxClaimPercent each run a `.GreaterThan(decimal.NewFromInt(1))`
// comparison that would force shopspring to expand a symbolic big.Int to
// match exponents — minutes of CPU per validator, chain halts. The
// moneyguard.IsSafeExponent gates at each parse site short-circuit the
// attack before Cmp is reached.
func TestParams_ValidateBasic_RejectsAbsurdExponent(t *testing.T) {
	for _, field := range []string{
		"TargetUtilization",
		"MinPoolBalance",
		"MaxClaimPercent",
		"DisputeStakeLac",
		"AutoApproveThreshold",
	} {
		t.Run(field, func(t *testing.T) {
			p := DefaultParams()
			switch field {
			case "TargetUtilization":
				p.TargetUtilization = "1e11100100"
			case "MinPoolBalance":
				p.MinPoolBalance = "1e11100100"
			case "MaxClaimPercent":
				p.MaxClaimPercent = "1e11100100"
			case "DisputeStakeLac":
				p.DisputeStakeLac = "1e11100100"
			case "AutoApproveThreshold":
				p.AutoApproveThreshold = "1e11100100"
			}
			err := p.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), "magnitude out of range")
		})
	}

	// Sanity: legitimate small-exponent value parses.
	t.Run("LegitimateScientificNotationAccepted", func(t *testing.T) {
		p := DefaultParams()
		p.MinPoolBalance = "1e3" // 1000 LAC, well within bounds
		require.NoError(t, p.ValidateBasic())
	})
}

// ---------- DefaultPoolState ----------

func TestDefaultPoolState(t *testing.T) {
	ps := DefaultPoolState()
	require.NotNil(t, ps)
	assert.Equal(t, "0", ps.TotalFunds)
	assert.Equal(t, "0", ps.AvailableFunds)
	assert.Equal(t, "0", ps.ReservedFunds)
	assert.Equal(t, "0", ps.TotalContributions)
	assert.Equal(t, "0", ps.TotalPayouts)
	assert.Equal(t, "0.2", ps.TargetUtilization)
	assert.Equal(t, "0", ps.CurrentUtilization)
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, ps.Status)
}

// ---------- DefaultPoolMetrics ----------

func TestDefaultPoolMetrics(t *testing.T) {
	m := DefaultPoolMetrics()
	require.NotNil(t, m)
	assert.Equal(t, "0", m.TotalContributions_24H)
	assert.Equal(t, "0", m.TotalPayouts_24H)
	assert.Equal(t, uint32(0), m.PendingClaims)
	assert.Equal(t, "0", m.AverageClaimAmount)
	assert.Equal(t, "0", m.ClaimApprovalRate)
	assert.Equal(t, "100", m.PoolHealthScore)
	assert.Equal(t, "0", m.RiskExposure)
	assert.Equal(t, "1.0", m.CoverageRatio)
	assert.Equal(t, "0", m.UtilizationEwma)
	assert.Equal(t, "0", m.DisputeRateEwma)
	assert.Equal(t, uint64(0), m.Samples)
}

// ---------- DefaultGenesis ----------

func TestDefaultGenesis(t *testing.T) {
	gs := DefaultGenesis()
	require.NotNil(t, gs)
	require.NotNil(t, gs.Params)
	require.NotNil(t, gs.Pool)
	require.NotNil(t, gs.Claims)
	require.NotNil(t, gs.Contributions)
	require.NotNil(t, gs.PublisherRisks)
	require.NotNil(t, gs.Payouts)
	require.NotNil(t, gs.Metrics)
	assert.Equal(t, uint64(1), gs.ClaimSequence)
	assert.Equal(t, uint64(1), gs.PayoutSequence)
}

func TestDefaultGenesis_Validate(t *testing.T) {
	require.NoError(t, DefaultGenesis().Validate())
}

// ---------- GenesisState.Validate ----------

func TestGenesis_Validate_NilState(t *testing.T) {
	var gs *GenesisState
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NilParams(t *testing.T) {
	gs := DefaultGenesis()
	gs.Params = nil
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_InvalidParams(t *testing.T) {
	gs := DefaultGenesis()
	gs.Params.InsurancePoolBps = 99999
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NegativePoolTotalFunds(t *testing.T) {
	gs := DefaultGenesis()
	gs.Pool.TotalFunds = "-100"
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_InvalidPoolTotalFunds(t *testing.T) {
	gs := DefaultGenesis()
	gs.Pool.TotalFunds = "not_a_number"
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NegativeAvailableFunds(t *testing.T) {
	gs := DefaultGenesis()
	gs.Pool.AvailableFunds = "-1"
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_InvalidAvailableFunds(t *testing.T) {
	gs := DefaultGenesis()
	gs.Pool.AvailableFunds = "abc"
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NegativeReservedFunds(t *testing.T) {
	gs := DefaultGenesis()
	gs.Pool.ReservedFunds = "-50"
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_InvalidReservedFunds(t *testing.T) {
	gs := DefaultGenesis()
	gs.Pool.ReservedFunds = "xyz"
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_PoolFundsMustBalance(t *testing.T) {
	for _, tc := range []struct {
		name      string
		total     string
		available string
		reserved  string
	}{
		{
			name:      "reserved plus available exceeds total",
			total:     "100",
			available: "80",
			reserved:  "30",
		},
		{
			name:      "reserved plus available below total",
			total:     "100",
			available: "60",
			reserved:  "30",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs := DefaultGenesis()
			gs.Pool.TotalFunds = tc.total
			gs.Pool.AvailableFunds = tc.available
			gs.Pool.ReservedFunds = tc.reserved

			require.ErrorContains(t, gs.Validate(), "pool total funds must equal available funds plus reserved funds")
		})
	}
}

func TestGenesis_Validate_NilPool(t *testing.T) {
	gs := DefaultGenesis()
	gs.Pool = nil
	require.NoError(t, gs.Validate())
}

func TestGenesis_Validate_NilClaim(t *testing.T) {
	gs := DefaultGenesis()
	gs.Claims = []*Claim{nil}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NilImportedEntries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*GenesisState)
		field  string
	}{
		{
			name: "nil contribution",
			mutate: func(gs *GenesisState) {
				gs.Contributions = []*Contribution{nil}
			},
			field: "contribution entry at index 0",
		},
		{
			name: "nil publisher risk",
			mutate: func(gs *GenesisState) {
				gs.PublisherRisks = []*PublisherRisk{nil}
			},
			field: "publisher risk entry at index 0",
		},
		{
			name: "nil payout",
			mutate: func(gs *GenesisState) {
				gs.Payouts = []*Payout{nil}
			},
			field: "payout entry at index 0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := DefaultGenesis()
			tc.mutate(gs)
			err := gs.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.field)
		})
	}
}

func TestGenesis_Validate_DuplicateClaimIDs(t *testing.T) {
	gs := DefaultGenesis()
	gs.Claims = []*Claim{
		{Id: "claim-1", Status: ClaimStatus_CLAIM_STATUS_PENDING},
		{Id: "claim-1", Status: ClaimStatus_CLAIM_STATUS_PENDING},
	}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_RejectsInvalidEnumStatuses(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*GenesisState)
		wantErr string
	}{
		{
			name: "pool status unspecified",
			mutate: func(gs *GenesisState) {
				gs.Pool.Status = PoolStatus_POOL_STATUS_UNSPECIFIED
			},
			wantErr: "pool status must be specified and known",
		},
		{
			name: "pool status unknown",
			mutate: func(gs *GenesisState) {
				gs.Pool.Status = PoolStatus(99)
			},
			wantErr: "pool status must be specified and known",
		},
		{
			name: "claim status unspecified",
			mutate: func(gs *GenesisState) {
				gs.Claims = []*Claim{{Id: "claim-unspecified", Status: ClaimStatus_CLAIM_STATUS_UNSPECIFIED}}
			},
			wantErr: "claim claim-unspecified status must be specified and known",
		},
		{
			name: "claim status unknown",
			mutate: func(gs *GenesisState) {
				gs.Claims = []*Claim{{Id: "claim-unknown", Status: ClaimStatus(99)}}
			},
			wantErr: "claim claim-unknown status must be specified and known",
		},
		{
			name: "payout status unspecified",
			mutate: func(gs *GenesisState) {
				gs.Payouts = []*Payout{{Id: "payout-unspecified", Status: PayoutStatus_PAYOUT_STATUS_UNSPECIFIED}}
			},
			wantErr: "payout payout-unspecified status must be specified and known",
		},
		{
			name: "payout status unknown",
			mutate: func(gs *GenesisState) {
				gs.Payouts = []*Payout{{Id: "payout-unknown", Status: PayoutStatus(99)}}
			},
			wantErr: "payout payout-unknown status must be specified and known",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := DefaultGenesis()
			tc.mutate(gs)

			require.ErrorContains(t, gs.Validate(), tc.wantErr)
		})
	}
}

func TestGenesis_Validate_AllowsValidImportedStatuses(t *testing.T) {
	for _, status := range []PoolStatus{
		PoolStatus_POOL_STATUS_HEALTHY,
		PoolStatus_POOL_STATUS_UNDERFUNDED,
		PoolStatus_POOL_STATUS_CRITICAL,
		PoolStatus_POOL_STATUS_OVERFUNDED,
	} {
		t.Run(status.String(), func(t *testing.T) {
			gs := DefaultGenesis()
			gs.Pool.Status = status
			require.NoError(t, gs.Validate())
		})
	}

	for _, status := range []ClaimStatus{
		ClaimStatus_CLAIM_STATUS_PENDING,
		ClaimStatus_CLAIM_STATUS_APPROVED,
		ClaimStatus_CLAIM_STATUS_REJECTED,
		ClaimStatus_CLAIM_STATUS_PAID,
		ClaimStatus_CLAIM_STATUS_EXPIRED,
		ClaimStatus_CLAIM_STATUS_DISPUTED,
	} {
		t.Run(status.String(), func(t *testing.T) {
			gs := DefaultGenesis()
			gs.Claims = []*Claim{{Id: "claim-valid", Status: status}}
			require.NoError(t, gs.Validate())
		})
	}

	for _, status := range []PayoutStatus{
		PayoutStatus_PAYOUT_STATUS_PENDING,
		PayoutStatus_PAYOUT_STATUS_COMPLETED,
		PayoutStatus_PAYOUT_STATUS_FAILED,
	} {
		t.Run(status.String(), func(t *testing.T) {
			gs := DefaultGenesis()
			gs.Payouts = []*Payout{{Id: "payout-valid", Status: status}}
			require.NoError(t, gs.Validate())
		})
	}
}

func TestGenesis_Validate_NegativeClaimAmount(t *testing.T) {
	gs := DefaultGenesis()
	gs.Claims = []*Claim{
		{
			Id:            "claim-1",
			Status:        ClaimStatus_CLAIM_STATUS_PENDING,
			ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "-100"},
		},
	}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_InvalidClaimAmount(t *testing.T) {
	gs := DefaultGenesis()
	gs.Claims = []*Claim{
		{
			Id:            "claim-2",
			Status:        ClaimStatus_CLAIM_STATUS_PENDING,
			ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "not_number"},
		},
	}
	require.Error(t, gs.Validate())
}

// TestGenesis_Validate_RejectsAbsurdExponent is the consensus-halt
// regression for shopspring-exponent values planted in genesis state.
// PoolState fields and Claim amounts are later fed to Div/Mul/Cmp in the
// insurance keeper; a crafted genesis file ("1e11100100" in TotalFunds
// or in a seed Claim) would brick the chain on the first block that
// touches the pool. Genesis runs before any msg_server can gate, so the
// gate has to live in GenesisState.Validate itself.
func TestGenesis_Validate_RejectsAbsurdExponent(t *testing.T) {
	t.Run("pool_total_funds", func(t *testing.T) {
		gs := DefaultGenesis()
		gs.Pool.TotalFunds = "1e11100100"
		err := gs.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "pool total funds magnitude")
	})
	t.Run("pool_available_funds", func(t *testing.T) {
		gs := DefaultGenesis()
		gs.Pool.AvailableFunds = "1e11100100"
		err := gs.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "pool available funds magnitude")
	})
	t.Run("pool_reserved_funds", func(t *testing.T) {
		gs := DefaultGenesis()
		gs.Pool.ReservedFunds = "1e11100100"
		err := gs.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "pool reserved funds magnitude")
	})
	t.Run("genesis_claim_amount", func(t *testing.T) {
		gs := DefaultGenesis()
		gs.Claims = []*Claim{
			{
				Id:            "claim-absurd",
				Status:        ClaimStatus_CLAIM_STATUS_PENDING,
				ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "1e11100100"},
			},
		}
		err := gs.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "claim-absurd amount magnitude")
	})
}

func TestGenesis_Validate_RejectsInvalidTimestamps(t *testing.T) {
	invalid := func() *timestamppb.Timestamp {
		return &timestamppb.Timestamp{Nanos: 1_000_000_000}
	}

	cases := []struct {
		name   string
		mutate func(*GenesisState)
		field  string
	}{
		{
			name: "pool last updated",
			mutate: func(gs *GenesisState) {
				gs.Pool.LastUpdated = invalid()
			},
			field: "last_updated",
		},
		{
			name: "metrics last adjudicated",
			mutate: func(gs *GenesisState) {
				gs.Metrics.LastAdjudicatedAt = invalid()
			},
			field: "last_adjudicated_at",
		},
		{
			name: "metrics last premium adjust",
			mutate: func(gs *GenesisState) {
				gs.Metrics.LastPremiumAdjustAt = invalid()
			},
			field: "last_premium_adjust_at",
		},
		{
			name: "contribution timestamp",
			mutate: func(gs *GenesisState) {
				gs.Contributions = []*Contribution{{Id: "contrib-bad", Timestamp: invalid()}}
			},
			field: "timestamp",
		},
		{
			name: "claim created_at",
			mutate: func(gs *GenesisState) {
				gs.Claims = []*Claim{{Id: "claim-created", Status: ClaimStatus_CLAIM_STATUS_PENDING, CreatedAt: invalid()}}
			},
			field: "created_at",
		},
		{
			name: "claim updated_at",
			mutate: func(gs *GenesisState) {
				gs.Claims = []*Claim{{Id: "claim-updated", Status: ClaimStatus_CLAIM_STATUS_PENDING, UpdatedAt: invalid()}}
			},
			field: "updated_at",
		},
		{
			name: "claim resolved_at",
			mutate: func(gs *GenesisState) {
				gs.Claims = []*Claim{{Id: "claim-resolved", Status: ClaimStatus_CLAIM_STATUS_PENDING, ResolvedAt: invalid()}}
			},
			field: "resolved_at",
		},
		{
			name: "publisher risk last slash",
			mutate: func(gs *GenesisState) {
				gs.PublisherRisks = []*PublisherRisk{{PublisherId: "pub-1", ToolId: "tool-1", LastSlashTime: invalid()}}
			},
			field: "last_slash_time",
		},
		{
			name: "publisher risk last evaluated",
			mutate: func(gs *GenesisState) {
				gs.PublisherRisks = []*PublisherRisk{{PublisherId: "pub-1", ToolId: "tool-1", LastEvaluated: invalid()}}
			},
			field: "last_evaluated",
		},
		{
			name: "payout paid_at",
			mutate: func(gs *GenesisState) {
				gs.Payouts = []*Payout{{Id: "payout-bad", Status: PayoutStatus_PAYOUT_STATUS_PENDING, PaidAt: invalid()}}
			},
			field: "paid_at",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := DefaultGenesis()
			tc.mutate(gs)
			err := gs.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.field)
			require.Contains(t, err.Error(), "out-of-range nanos")
		})
	}
}

func TestGenesis_Validate_RejectsRuntimeImpossibleClaimTimestampOrder(t *testing.T) {
	createdAt := timestamppb.New(time.Unix(1_700_000_100, 0).UTC())
	beforeCreated := timestamppb.New(time.Unix(1_700_000_000, 0).UTC())

	tests := []struct {
		name    string
		claim   *Claim
		wantErr string
	}{
		{
			name: "updated before created",
			claim: &Claim{
				Id:        "claim-updated-before-created",
				Status:    ClaimStatus_CLAIM_STATUS_PENDING,
				CreatedAt: createdAt,
				UpdatedAt: beforeCreated,
			},
			wantErr: "claim claim-updated-before-created updated_at cannot be before created_at",
		},
		{
			name: "resolved before created",
			claim: &Claim{
				Id:         "claim-resolved-before-created",
				Status:     ClaimStatus_CLAIM_STATUS_PENDING,
				CreatedAt:  createdAt,
				UpdatedAt:  createdAt,
				ResolvedAt: beforeCreated,
			},
			wantErr: "claim claim-resolved-before-created resolved_at cannot be before created_at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := DefaultGenesis()
			gs.Claims = []*Claim{tt.claim}

			require.ErrorContains(t, gs.Validate(), tt.wantErr)
		})
	}
}

func TestGenesis_Validate_ZeroClaimSequence(t *testing.T) {
	gs := DefaultGenesis()
	gs.ClaimSequence = 0
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_ZeroPayoutSequence(t *testing.T) {
	gs := DefaultGenesis()
	gs.PayoutSequence = 0
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_ValidClaimsWithAmount(t *testing.T) {
	gs := DefaultGenesis()
	gs.Claims = []*Claim{
		{
			Id:            "claim-1",
			Status:        ClaimStatus_CLAIM_STATUS_PENDING,
			ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
		},
		{
			Id:            "claim-2",
			Status:        ClaimStatus_CLAIM_STATUS_APPROVED,
			ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "1000"},
		},
	}
	require.NoError(t, gs.Validate())
}

// ---------- Keys ----------

func TestModuleConstants(t *testing.T) {
	assert.Equal(t, "insurance", ModuleName)
	assert.Equal(t, "insurance", StoreKey)
	assert.Equal(t, "insurance", RouterKey)
	assert.Equal(t, "insurance", QuerierRoute)
	assert.Equal(t, "mem_insurance", MemStoreKey)
}

func TestKeyPrefixesUnique(t *testing.T) {
	prefixes := [][]byte{
		PoolKey, ClaimsKeyPrefix, ContributionsKeyPrefix,
		PublisherRiskKeyPrefix, PayoutsKeyPrefix, ParamsKey,
		MetricsKey, ClaimsByReceiptIndexPrefix, ClaimsByStatusIndexPrefix,
		ClaimSequenceKey, ContributionSequenceKey, PayoutSequenceKey,
	}
	seen := make(map[byte]bool)
	for _, p := range prefixes {
		require.Len(t, p, 1)
		assert.False(t, seen[p[0]], "duplicate prefix byte: 0x%02x", p[0])
		seen[p[0]] = true
	}
}

func TestGetClaimKey(t *testing.T) {
	key := GetClaimKey("abc")
	assert.Equal(t, append(ClaimsKeyPrefix, []byte("abc")...), key)
}

func TestGetContributionKey(t *testing.T) {
	key := GetContributionKey("c1")
	assert.Equal(t, append(ContributionsKeyPrefix, []byte("c1")...), key)
}

func TestGetPublisherRiskKey(t *testing.T) {
	key := GetPublisherRiskKey("pub1")
	assert.Equal(t, append(PublisherRiskKeyPrefix, []byte("pub1")...), key)
}

func TestGetPayoutKey(t *testing.T) {
	key := GetPayoutKey("p1")
	assert.Equal(t, append(PayoutsKeyPrefix, []byte("p1")...), key)
}

func TestGetClaimByReceiptIndexKey(t *testing.T) {
	key := GetClaimByReceiptIndexKey("r1", "c1")
	expected := append(append(ClaimsByReceiptIndexPrefix, []byte("r1")...), []byte("c1")...)
	assert.Equal(t, expected, key)
}

func TestGetClaimByStatusIndexKey(t *testing.T) {
	key := GetClaimByStatusIndexKey("pending", "c1")
	expected := append(append(ClaimsByStatusIndexPrefix, []byte("pending")...), []byte("c1")...)
	assert.Equal(t, expected, key)
}

func TestKeyHelperFunctions(t *testing.T) {
	assert.Equal(t, ParamsKey, ParamsKeyPrefix())
	assert.Equal(t, PoolBalanceKey, PoolBalanceKeyPrefix())
	assert.Equal(t, PoolMetricsKeyVal, PoolMetricsKeyPrefix())
	assert.Equal(t, ClaimsKeyPrefix, GetClaimsKeyPrefix())
	assert.Equal(t, ContributionsKeyPrefix, GetContributionsKeyPrefix())
	assert.Equal(t, PublisherRiskKeyPrefix, PublisherRisksKeyPrefix())
	assert.Equal(t, PayoutsKeyPrefix, GetPayoutsKeyPrefix())
	assert.Equal(t, ClaimSequenceKey, ClaimCounterKey())
	assert.Equal(t, ContributionSequenceKey, ContribCounterKey())
	assert.Equal(t, PayoutSequenceKey, PayoutCounterKey())
}

// ---------- Events ----------

func TestEventTypes(t *testing.T) {
	events := []string{
		EventTypeClaimFiled, EventTypeClaimProcessed, EventTypeClaimPaid,
		EventTypeContribution, EventTypeFundsReserved, EventTypeFundsReleased,
		EventTypePublisherRiskUpdated, EventTypePoolMetricsUpdated, EventTypeParamsUpdated,
	}
	seen := make(map[string]bool)
	for _, e := range events {
		assert.NotEmpty(t, e)
		assert.False(t, seen[e], "duplicate event type: %s", e)
		seen[e] = true
	}
}

func TestAttributeKeys(t *testing.T) {
	attrs := []string{
		AttributeKeyClaimID, AttributeKeyReceiptID, AttributeKeyClaimant,
		AttributeKeyPublisher, AttributeKeyToolID, AttributeKeyAmount,
		AttributeKeyStatus, AttributeKeyResolution, AttributeKeyPayoutID,
		AttributeKeyRiskScore, AttributeKeyPremiumTier, AttributeKeyPoolBalance,
		AttributeKeyUtilization, AttributeKeyAuthority, AttributeKeyContributionID,
	}
	seen := make(map[string]bool)
	for _, a := range attrs {
		assert.NotEmpty(t, a)
		assert.False(t, seen[a], "duplicate attribute key: %s", a)
		seen[a] = true
	}
}

// ---------- Errors ----------

func TestSentinelErrors(t *testing.T) {
	errs := []error{
		ErrInsufficientFunds, ErrClaimNotFound, ErrClaimAlreadyResolved,
		ErrInvalidClaimRequest, ErrInvalidAmount, ErrClaimWindowExpired,
		ErrDuplicateClaim, ErrPoolUnavailable, ErrExceedsMaxClaim,
		ErrInvalidEvidence, ErrInvalidContribution, ErrInvalidPayout,
		ErrClaimAlreadyPaid, ErrClaimNotApproved, ErrInvalidPublisher,
		ErrInvalidReceipt, ErrRateLimitExceeded, ErrInvalidParameters,
		ErrUnauthorized, ErrInternalError,
		ErrModuleAccountNotFound, ErrClaimAlreadyProcessed,
		ErrInvalidClaimResolution, ErrRateLimitCheckFailed,
		ErrClaimRateLimitExceeded, ErrContributionRateLimitExceeded,
		ErrGlobalClaimRateLimitExceeded,
	}
	for _, e := range errs {
		assert.NotNil(t, e)
		assert.NotEmpty(t, e.Error())
	}
}

// ---------- EvaluatePoolHealth ----------

func TestEvaluatePoolHealth_ZeroTargetZeroCurrent(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "0",
		CurrentUtilization: "0",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_ZeroTargetNonzeroCurrent(t *testing.T) {
	pool := &PoolState{
		TargetUtilization:  "0",
		CurrentUtilization: "0.5",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_Overfunded(t *testing.T) {
	// ratio < 0.5 => overfunded
	pool := &PoolState{
		TargetUtilization:  "0.8",
		CurrentUtilization: "0.3",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_OVERFUNDED, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_Healthy(t *testing.T) {
	// 0.5 <= ratio < 0.8 => healthy
	pool := &PoolState{
		TargetUtilization:  "0.5",
		CurrentUtilization: "0.3",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_HEALTHY, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_Underfunded(t *testing.T) {
	// 0.8 <= ratio < 1.2 => underfunded
	pool := &PoolState{
		TargetUtilization:  "0.5",
		CurrentUtilization: "0.5",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_UNDERFUNDED, EvaluatePoolHealth(pool))
}

func TestEvaluatePoolHealth_Critical(t *testing.T) {
	// ratio >= 1.2 => critical
	pool := &PoolState{
		TargetUtilization:  "0.5",
		CurrentUtilization: "0.8",
	}
	assert.Equal(t, PoolStatus_POOL_STATUS_CRITICAL, EvaluatePoolHealth(pool))
}

// ---------- CalculatePremium ----------

func TestCalculatePremium(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "1.5"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 300} // 3%

	premium := CalculatePremium(risk, baseAmount, params)
	// base * BPS / 10000 = 1000 * 300 / 10000 = 30
	// 30 * 1.5 = 45
	assert.True(t, premium.Equal(decimal.NewFromInt(45)))
}

func TestCalculatePremium_ZeroMultiplier(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "0"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 300}

	premium := CalculatePremium(risk, baseAmount, params)
	assert.True(t, premium.IsZero())
}

func TestCalculatePremium_ZeroBPS(t *testing.T) {
	risk := &PublisherRisk{PremiumMultiplier: "2.0"}
	baseAmount := decimal.NewFromInt(1000)
	params := &Parameters{InsurancePoolBPS: 0}

	premium := CalculatePremium(risk, baseAmount, params)
	assert.True(t, premium.IsZero())
}

// ---------- ReceiptSettlementStatus ----------

func TestSettlementStatusConstants(t *testing.T) {
	assert.Equal(t, ReceiptSettlementStatus("pending"), SettlementStatusPending)
	assert.Equal(t, ReceiptSettlementStatus("settled"), SettlementStatusSettled)
	assert.Equal(t, ReceiptSettlementStatus("challenged"), SettlementStatusChallenged)
}

// ---------- MsgUpdateParams.ValidateBasic ----------

func TestMsgUpdateParams_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgUpdateParams{
		Authority: validInsuranceAddress("insurance-valid-0001"),
		Params:    DefaultParams(),
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgUpdateParams_ValidateBasic_EmptyAuthority(t *testing.T) {
	msg := &MsgUpdateParams{
		Authority: "",
		Params:    DefaultParams(),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUpdateParams_ValidateBasic_InvalidAuthority(t *testing.T) {
	msg := &MsgUpdateParams{
		Authority: "not_a_valid_address",
		Params:    DefaultParams(),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUpdateParams_ValidateBasic_NilParams(t *testing.T) {
	msg := &MsgUpdateParams{
		Authority: validInsuranceAddress("insurance-valid-0001"),
		Params:    nil,
	}
	require.Error(t, msg.ValidateBasic())
}

// ---------- MsgProcessContribution.ValidateBasic ----------

func TestMsgProcessContribution_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-valid-0002"),
		ReceiptId:   "rcpt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgProcessContribution_ValidateBasic_EmptyAuthority(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   "",
		ReceiptId:   "rcpt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessContribution_ValidateBasic_InvalidAuthority(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   "governance",
		ReceiptId:   "rcpt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority address")
}

func TestMsgProcessContribution_ValidateBasic_EmptyReceiptID(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-valid-0002"),
		ReceiptId:   "",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessContribution_ValidateBasic_EmptyToolID(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-valid-0002"),
		ReceiptId:   "rcpt-1",
		ToolId:      "",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessContribution_ValidateBasic_EmptyPublisherID(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-valid-0002"),
		ReceiptId:   "rcpt-1",
		ToolId:      "tool-1",
		PublisherId: "",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessContribution_ValidateBasic_NilAmount(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-valid-0002"),
		ReceiptId:   "rcpt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      nil,
	}
	require.Error(t, msg.ValidateBasic())
}

// ---------- MsgFileClaim.ValidateBasic ----------

func TestMsgFileClaim_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-valid-0003"),
		ReceiptId:     "rcpt-1",
		ToolId:        "tool-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
		Reason:        "service failure",
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgFileClaim_ValidateBasic_EmptyClaimant(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      "",
		ReceiptId:     "rcpt-1",
		ToolId:        "tool-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
		Reason:        "service failure",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgFileClaim_ValidateBasic_InvalidClaimant(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      "claimant",
		ReceiptId:     "rcpt-1",
		ToolId:        "tool-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
		Reason:        "service failure",
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid claimant address")
}

func TestMsgFileClaim_ValidateBasic_EmptyReceiptID(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-valid-0003"),
		ReceiptId:     "",
		ToolId:        "tool-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
		Reason:        "service failure",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgFileClaim_ValidateBasic_EmptyToolID(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-valid-0003"),
		ReceiptId:     "rcpt-1",
		ToolId:        "",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
		Reason:        "service failure",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgFileClaim_ValidateBasic_NilClaimedAmount(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-valid-0003"),
		ReceiptId:     "rcpt-1",
		ToolId:        "tool-1",
		ClaimedAmount: nil,
		Reason:        "service failure",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgFileClaim_ValidateBasic_EmptyReason(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-valid-0003"),
		ReceiptId:     "rcpt-1",
		ToolId:        "tool-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
		Reason:        "",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgFileClaim_ValidateBasic_RejectsPaddedReason(t *testing.T) {
	for _, reason := range []string{
		" service failure",
		"service failure ",
		"\tservice failure\n",
	} {
		t.Run(reason, func(t *testing.T) {
			msg := &MsgFileClaim{
				Claimant:      validInsuranceAddress("insurance-valid-0003"),
				ReceiptId:     "rcpt-1",
				ToolId:        "tool-1",
				ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
				Reason:        reason,
			}
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), "reason must be canonical")
		})
	}
}

// TestMsgFileClaim_ValidateBasic_RejectsAbsurdAmountExponent is the
// consensus-halt regression for the shopspring-exponent DoS on the claim
// filing path. v1beta1.Coin.Amount is an unconstrained proto string, so a
// claimant can submit "1e11100100". Once stored, the claim-processing
// path at x/insurance/keeper/keeper.go:355 does
//
//	claimedAmt.LessThanOrEqual(autoApproveThreshold)
//
// which expands shopspring's symbolic big.Int to match exponents —
// millions of digits per validator, chain halts. ValidateBasic gates
// statelessly so adversarial amounts never reach the handler.
func TestMsgFileClaim_ValidateBasic_RejectsAbsurdAmountExponent(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-valid-0003"),
		ReceiptId:     "rcpt-1",
		ToolId:        "tool-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "1e11100100"},
		Reason:        "fraud",
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "claimed_amount")
	require.Contains(t, err.Error(), "magnitude out of range")
}

// TestMsgProcessContribution_ValidateBasic_RejectsAbsurdAmountExponent is
// the parallel regression for the contribution-processing path; the
// stored contribution amount feeds into pool-funds arithmetic
// (insurance/keeper/keeper.go Div/Add/LessThan on TotalFunds etc.).
func TestMsgProcessContribution_ValidateBasic_RejectsAbsurdAmountExponent(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-valid-0002"),
		ReceiptId:   "rcpt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "1e11100100"},
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "amount")
	require.Contains(t, err.Error(), "magnitude out of range")
}

// TestMsgProcessPayout_ValidateBasic_RejectsAbsurdAmountExponent completes
// the trio for the user-reachable insurance msgs that carry a
// v1beta1.Coin amount.
func TestMsgProcessPayout_ValidateBasic_RejectsAbsurdAmountExponent(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: validInsuranceAddress("insurance-valid-0004"),
		ClaimId:   "claim-1",
		Recipient: validInsuranceAddress("insurance-valid-0005"),
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "1e11100100"},
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "amount")
	require.Contains(t, err.Error(), "magnitude out of range")
}

func TestInsuranceAmountMessages_ValidateBasic_RejectInvalidCoins(t *testing.T) {
	validCoin := func() *basev1beta1.Coin {
		return &basev1beta1.Coin{Denom: "ulac", Amount: "100"}
	}
	amountCases := map[string]func(*basev1beta1.Coin){
		"empty_denom": func(coin *basev1beta1.Coin) {
			coin.Denom = ""
		},
		"padded_denom": func(coin *basev1beta1.Coin) {
			coin.Denom = " ulac"
		},
		"invalid_denom": func(coin *basev1beta1.Coin) {
			coin.Denom = "1bad"
		},
		"empty_amount": func(coin *basev1beta1.Coin) {
			coin.Amount = ""
		},
		"padded_amount": func(coin *basev1beta1.Coin) {
			coin.Amount = "100 "
		},
		"invalid_amount": func(coin *basev1beta1.Coin) {
			coin.Amount = "1.5"
		},
		"zero_amount": func(coin *basev1beta1.Coin) {
			coin.Amount = "0"
		},
		"negative_amount": func(coin *basev1beta1.Coin) {
			coin.Amount = "-1"
		},
	}
	messageCases := map[string]func(*basev1beta1.Coin) interface{ ValidateBasic() error }{
		"process_contribution": func(coin *basev1beta1.Coin) interface{ ValidateBasic() error } {
			return &MsgProcessContribution{
				Authority:   validInsuranceAddress("insurance-valid-0002"),
				ReceiptId:   "rcpt-1",
				ToolId:      "tool-1",
				PublisherId: "pub-1",
				Amount:      coin,
			}
		},
		"file_claim": func(coin *basev1beta1.Coin) interface{ ValidateBasic() error } {
			return &MsgFileClaim{
				Claimant:      validInsuranceAddress("insurance-valid-0003"),
				ReceiptId:     "rcpt-1",
				ToolId:        "tool-1",
				ClaimedAmount: coin,
				Reason:        "service failure",
			}
		},
		"process_claim_approved_amount": func(coin *basev1beta1.Coin) interface{ ValidateBasic() error } {
			return &MsgProcessClaim{
				Authority:      validInsuranceAddress("insurance-valid-0006"),
				ClaimId:        "claim-1",
				Resolution:     "approve",
				ApprovedAmount: coin,
			}
		},
		"process_payout": func(coin *basev1beta1.Coin) interface{ ValidateBasic() error } {
			return &MsgProcessPayout{
				Authority: validInsuranceAddress("insurance-valid-0004"),
				ClaimId:   "claim-1",
				Recipient: validInsuranceAddress("insurance-valid-0005"),
				Amount:    coin,
			}
		},
	}

	for messageName, buildMessage := range messageCases {
		for amountName, mutate := range amountCases {
			t.Run(messageName+"/"+amountName, func(t *testing.T) {
				coin := validCoin()
				mutate(coin)
				err := buildMessage(coin).ValidateBasic()
				require.Error(t, err)
			})
		}
	}
}

func TestInsuranceIdentifierMessages_ValidateBasic_RejectPaddedIdentifiers(t *testing.T) {
	tests := map[string]struct {
		msg  interface{ ValidateBasic() error }
		want string
	}{
		"process_contribution_receipt_id": {
			msg: &MsgProcessContribution{
				Authority:   validInsuranceAddress("insurance-valid-0002"),
				ReceiptId:   " rcpt-1",
				ToolId:      "tool-1",
				PublisherId: "pub-1",
				Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
			},
			want: "receipt_id",
		},
		"process_contribution_tool_id": {
			msg: &MsgProcessContribution{
				Authority:   validInsuranceAddress("insurance-valid-0002"),
				ReceiptId:   "rcpt-1",
				ToolId:      "tool-1 ",
				PublisherId: "pub-1",
				Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
			},
			want: "tool_id",
		},
		"process_contribution_publisher_id": {
			msg: &MsgProcessContribution{
				Authority:   validInsuranceAddress("insurance-valid-0002"),
				ReceiptId:   "rcpt-1",
				ToolId:      "tool-1",
				PublisherId: "\tpub-1",
				Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
			},
			want: "publisher_id",
		},
		"file_claim_receipt_id": {
			msg: &MsgFileClaim{
				Claimant:      validInsuranceAddress("insurance-valid-0003"),
				ReceiptId:     "\nrcpt-1",
				ToolId:        "tool-1",
				ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
				Reason:        "service failure",
			},
			want: "receipt_id",
		},
		"file_claim_tool_id": {
			msg: &MsgFileClaim{
				Claimant:      validInsuranceAddress("insurance-valid-0003"),
				ReceiptId:     "rcpt-1",
				ToolId:        "tool-1\t",
				ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
				Reason:        "service failure",
			},
			want: "tool_id",
		},
		"file_claim_optional_publisher_id": {
			msg: &MsgFileClaim{
				Claimant:      validInsuranceAddress("insurance-valid-0003"),
				ReceiptId:     "rcpt-1",
				ToolId:        "tool-1",
				PublisherId:   " pub-1",
				ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
				Reason:        "service failure",
			},
			want: "publisher_id",
		},
		"process_claim_claim_id": {
			msg: &MsgProcessClaim{
				Authority:  validInsuranceAddress("insurance-valid-0006"),
				ClaimId:    "claim-1 ",
				Resolution: "approve",
			},
			want: "claim_id",
		},
		"process_payout_claim_id": {
			msg: &MsgProcessPayout{
				Authority: validInsuranceAddress("insurance-valid-0004"),
				ClaimId:   "\tclaim-1",
				Recipient: validInsuranceAddress("insurance-valid-0005"),
				Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
			},
			want: "claim_id",
		},
		"update_publisher_risk_publisher_id": {
			msg: &MsgUpdatePublisherRisk{
				Authority:    validInsuranceAddress("insurance-valid-0007"),
				PublisherId:  "pub-1\n",
				ToolId:       "tool-1",
				RiskScoreBps: 500,
			},
			want: "publisher_id",
		},
		"update_publisher_risk_tool_id": {
			msg: &MsgUpdatePublisherRisk{
				Authority:    validInsuranceAddress("insurance-valid-0007"),
				PublisherId:  "pub-1",
				ToolId:       " tool-1",
				RiskScoreBps: 500,
			},
			want: "tool_id",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			require.Contains(t, err.Error(), "canonical")
		})
	}
}

// ---------- MsgProcessClaim.ValidateBasic ----------

func TestMsgProcessClaim_ValidateBasic_Approve(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-valid-0006"),
		ClaimId:    "claim-1",
		Resolution: "approve",
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgProcessClaim_ValidateBasic_Reject(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-valid-0006"),
		ClaimId:    "claim-1",
		Resolution: "reject",
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgProcessClaim_ValidateBasic_Partial(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-valid-0006"),
		ClaimId:    "claim-1",
		Resolution: "partial",
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgProcessClaim_ValidateBasic_EmptyAuthority(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  "",
		ClaimId:    "claim-1",
		Resolution: "approve",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessClaim_ValidateBasic_InvalidAuthority(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  "governance",
		ClaimId:    "claim-1",
		Resolution: "approve",
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority address")
}

func TestMsgProcessClaim_ValidateBasic_EmptyClaimID(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-valid-0006"),
		ClaimId:    "",
		Resolution: "approve",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessClaim_ValidateBasic_InvalidResolution(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-valid-0006"),
		ClaimId:    "claim-1",
		Resolution: "unknown",
	}
	require.Error(t, msg.ValidateBasic())
}

// ---------- MsgProcessPayout.ValidateBasic ----------

func TestMsgProcessPayout_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: validInsuranceAddress("insurance-valid-0004"),
		ClaimId:   "claim-1",
		Recipient: validInsuranceAddress("insurance-valid-0005"),
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgProcessPayout_ValidateBasic_EmptyAuthority(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: "",
		ClaimId:   "claim-1",
		Recipient: "cosmos1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh",
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessPayout_ValidateBasic_InvalidAuthority(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: "governance",
		ClaimId:   "claim-1",
		Recipient: validInsuranceAddress("insurance-valid-0005"),
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority address")
}

func TestMsgProcessPayout_ValidateBasic_EmptyClaimID(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: validInsuranceAddress("insurance-valid-0004"),
		ClaimId:   "",
		Recipient: validInsuranceAddress("insurance-valid-0005"),
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessPayout_ValidateBasic_EmptyRecipient(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: validInsuranceAddress("insurance-valid-0004"),
		ClaimId:   "claim-1",
		Recipient: "",
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgProcessPayout_ValidateBasic_InvalidRecipientAddress(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: validInsuranceAddress("insurance-valid-0004"),
		ClaimId:   "claim-1",
		Recipient: "not-a-bech32-address",
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid recipient address")
}

func TestMsgProcessPayout_ValidateBasic_NilAmount(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: validInsuranceAddress("insurance-valid-0004"),
		ClaimId:   "claim-1",
		Recipient: validInsuranceAddress("insurance-valid-0005"),
		Amount:    nil,
	}
	require.Error(t, msg.ValidateBasic())
}

// ---------- MsgUpdatePublisherRisk.ValidateBasic ----------

func TestMsgUpdatePublisherRisk_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgUpdatePublisherRisk{
		Authority:    validInsuranceAddress("insurance-valid-0007"),
		PublisherId:  "pub-1",
		ToolId:       "tool-1",
		RiskScoreBps: 500,
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgUpdatePublisherRisk_ValidateBasic_EmptyAuthority(t *testing.T) {
	msg := &MsgUpdatePublisherRisk{
		Authority:    "",
		PublisherId:  "pub-1",
		ToolId:       "tool-1",
		RiskScoreBps: 500,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUpdatePublisherRisk_ValidateBasic_InvalidAuthority(t *testing.T) {
	msg := &MsgUpdatePublisherRisk{
		Authority:    "governance",
		PublisherId:  "pub-1",
		ToolId:       "tool-1",
		RiskScoreBps: 500,
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority address")
}

func TestMsgUpdatePublisherRisk_ValidateBasic_EmptyPublisherID(t *testing.T) {
	msg := &MsgUpdatePublisherRisk{
		Authority:    validInsuranceAddress("insurance-valid-0007"),
		PublisherId:  "",
		ToolId:       "tool-1",
		RiskScoreBps: 500,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUpdatePublisherRisk_ValidateBasic_EmptyToolID(t *testing.T) {
	msg := &MsgUpdatePublisherRisk{
		Authority:    validInsuranceAddress("insurance-valid-0007"),
		PublisherId:  "pub-1",
		ToolId:       "",
		RiskScoreBps: 500,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUpdatePublisherRisk_ValidateBasic_RiskBpsAboveMax(t *testing.T) {
	msg := &MsgUpdatePublisherRisk{
		Authority:    validInsuranceAddress("insurance-valid-0007"),
		PublisherId:  "pub-1",
		ToolId:       "tool-1",
		RiskScoreBps: 10_001,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUpdatePublisherRisk_ValidateBasic_RiskBpsAtMax(t *testing.T) {
	msg := &MsgUpdatePublisherRisk{
		Authority:    validInsuranceAddress("insurance-valid-0007"),
		PublisherId:  "pub-1",
		ToolId:       "tool-1",
		RiskScoreBps: 10_000,
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgUpdatePublisherRisk_ValidateBasic_ZeroRiskBps(t *testing.T) {
	msg := &MsgUpdatePublisherRisk{
		Authority:    validInsuranceAddress("insurance-valid-0007"),
		PublisherId:  "pub-1",
		ToolId:       "tool-1",
		RiskScoreBps: 0,
	}
	require.NoError(t, msg.ValidateBasic())
}

// ---------- Error code uniqueness ----------

func TestErrorCodeUniqueness(t *testing.T) {
	type errInfo struct {
		code uint32
		name string
	}
	entries := []errInfo{
		{1100, "ErrInsufficientFunds"},
		{1101, "ErrClaimNotFound"},
		{1102, "ErrClaimAlreadyResolved"},
		{1103, "ErrInvalidClaimRequest"},
		{1104, "ErrInvalidAmount"},
		{1105, "ErrClaimWindowExpired"},
		{1106, "ErrDuplicateClaim"},
		{1107, "ErrPoolUnavailable"},
		{1108, "ErrExceedsMaxClaim"},
		{1109, "ErrInvalidEvidence"},
		{1110, "ErrInvalidContribution"},
		{1111, "ErrInvalidPayout"},
		{1112, "ErrClaimAlreadyPaid"},
		{1113, "ErrClaimNotApproved"},
		{1114, "ErrInvalidPublisher"},
		{1115, "ErrInvalidReceipt"},
		{1116, "ErrRateLimitExceeded"},
		{1117, "ErrInvalidParameters"},
		{1118, "ErrUnauthorized"},
		{1120, "ErrInternalError"},
		{1121, "ErrModuleAccountNotFound"},
		{1122, "ErrClaimAlreadyProcessed"},
		{1123, "ErrInvalidClaimResolution"},
		{1124, "ErrRateLimitCheckFailed"},
		{1125, "ErrClaimRateLimitExceeded"},
		{1126, "ErrContributionRateLimitExceeded"},
		{1127, "ErrGlobalClaimRateLimitExceeded"},
	}
	seen := make(map[uint32]string)
	for _, e := range entries {
		if prev, ok := seen[e.code]; ok {
			t.Errorf("duplicate error code %d: %s and %s", e.code, prev, e.name)
		}
		seen[e.code] = e.name
	}
	assert.Len(t, seen, 28)
}

// ---------- Codec ----------

func TestRegisterLegacyAminoCodec(t *testing.T) {
	amino := codec.NewLegacyAmino()
	require.NotPanics(t, func() { RegisterLegacyAminoCodec(amino) })
}

func TestRegisterInterfaces(t *testing.T) {
	registry := cdctypes.NewInterfaceRegistry()
	require.NotPanics(t, func() { RegisterInterfaces(registry) })
}

func TestModuleCdc_NotNil(t *testing.T) {
	require.NotNil(t, ModuleCdc)
}
