package types

import (
	"math"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestPassportSummaryValidate_AllowsNil(t *testing.T) {
	var summary *PassportSummary
	require.NoError(t, summary.Validate())
}

func TestPassportSummaryValidate_InvalidCoin(t *testing.T) {
	summary := &PassportSummary{
		TotalSpend: sdk.Coin{Denom: "", Amount: sdkmath.NewInt(10)},
	}
	err := summary.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "total_spend")
}

func TestPassportSummaryValidate_InvalidShares(t *testing.T) {
	summary := &PassportSummary{
		TotalSpend:         sdk.NewCoin("ulume", sdkmath.NewInt(0)),
		ToolDiversityIndex: 1.2,
	}
	require.Error(t, summary.Validate())

	summary.ToolDiversityIndex = 0.5
	summary.VerifiedSpendShare = -0.1
	require.Error(t, summary.Validate())

	summary.VerifiedSpendShare = 0.2
	summary.CollusionRiskScore = math.NaN()
	require.Error(t, summary.Validate())
}
