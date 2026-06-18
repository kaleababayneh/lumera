
package types

import (
	"math"
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/require"
)

func TestPassportSummaryValidate_AllowsNil(t *testing.T) {
	var summary *PassportSummary
	require.NoError(t, summary.Validate())
}

func TestPassportSummaryValidate_InvalidCoin(t *testing.T) {
	summary := &PassportSummary{
		TotalSpend: &v1beta1.Coin{Denom: "", Amount: "10"},
	}
	err := summary.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "total_spend")
}

func TestPassportSummaryValidate_InvalidShares(t *testing.T) {
	summary := &PassportSummary{
		TotalSpend:         &v1beta1.Coin{Denom: "ulume", Amount: "0"},
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
