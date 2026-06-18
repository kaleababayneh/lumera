//go:build cosmos

package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func validTxPrioritizerContract() *TxPrioritizerContractV1 {
	contract := DefaultTxPrioritizerContractV1()
	contract.Reputation.Verified = true
	return contract
}

func TestTxPrioritizerContractV1Validate_Default(t *testing.T) {
	t.Parallel()
	require.NoError(t, DefaultTxPrioritizerContractV1().Validate())
}

func TestTxPrioritizerContractV1Validate_FieldErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*TxPrioritizerContractV1)
		errMsg string
	}{
		{
			name:   "empty schema",
			mutate: func(c *TxPrioritizerContractV1) { c.Schema = "" },
			errMsg: "schema is required",
		},
		{
			name:   "wrong schema",
			mutate: func(c *TxPrioritizerContractV1) { c.Schema = "lumera.tx_prioritizer.contract.v9" },
			errMsg: "unsupported tx prioritizer schema",
		},
		{
			name:   "empty fee denom",
			mutate: func(c *TxPrioritizerContractV1) { c.Fee.Denom = "" },
			errMsg: "denom is required",
		},
		{
			name:   "invalid score version",
			mutate: func(c *TxPrioritizerContractV1) { c.Reputation.ScoreVersion = "unknown" },
			errMsg: "unsupported score_version",
		},
		{
			name:   "verified boost too large",
			mutate: func(c *TxPrioritizerContractV1) { c.Reputation.VerifiedBoostBps = BPSDenominator + 1 },
			errMsg: "verified_boost_bps",
		},
		{
			name:   "invalid cache refresh mode",
			mutate: func(c *TxPrioritizerContractV1) { c.Cache.RefreshMode = "hourly" },
			errMsg: "unsupported refresh_mode",
		},
		{
			name:   "block frozen without freeze flag",
			mutate: func(c *TxPrioritizerContractV1) { c.Cache.FreezeWithinBlock = false },
			errMsg: "freeze_within_block must be true",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			contract := validTxPrioritizerContract()
			tc.mutate(contract)
			err := contract.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

func TestParseTxPrioritizerContractV1RoundTrip(t *testing.T) {
	t.Parallel()

	original := validTxPrioritizerContract()
	raw, err := original.Marshal()
	require.NoError(t, err)

	parsed, err := ParseTxPrioritizerContractV1(raw)
	require.NoError(t, err)
	require.Equal(t, original.Schema, parsed.Schema)
	require.Equal(t, original.Fee.Denom, parsed.Fee.Denom)
	require.Equal(t, original.Reputation.Verified, parsed.Reputation.Verified)
	require.Equal(t, original.Cache.ConsistencyMode, parsed.Cache.ConsistencyMode)
	require.NoError(t, parsed.Validate())
}

func TestExtractTxPrioritizerContractV1_DefaultsWhenMissing(t *testing.T) {
	t.Parallel()

	tool := validToolCard(t)
	tool.Metadata = nil

	contract, err := ExtractTxPrioritizerContractV1(tool)
	require.NoError(t, err)
	require.Equal(t, TxPrioritizerContractSchemaV1, contract.Schema)
	require.Equal(t, DefaultTxPrioritizerFeeDenom, contract.Fee.Denom)
	require.Equal(t, TxPrioritizerConsistencyBlockFrozen, contract.Cache.ConsistencyMode)
}

func TestBuildTxPrioritizerResolvedInputsV1(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 10, 20, 0, 0, 0, time.UTC)
	tool := validToolCard(t)
	tool.Metadata = map[string]string{}

	contract := validTxPrioritizerContract()
	rawContract, err := contract.Marshal()
	require.NoError(t, err)
	tool.Metadata[MetadataKeyTxPrioritizerV1] = rawContract

	bond := &BondRecord{
		ToolId:                     tool.ToolId,
		Owner:                      tool.Owner,
		BondedAmount:               []*v1beta1.Coin{{Denom: BondDenom, Amount: "10000"}},
		MinimumRequired:            []*v1beta1.Coin{{Denom: BondDenom, Amount: "5000"}},
		LockedAmount:               []*v1beta1.Coin{{Denom: BondDenom, Amount: "2500"}},
		InsurancePremiumMultiplier: "1.25",
		Status:                     BondStatusActive,
		LastUpdatedAt:              timestamppb.New(now),
		DisputeCount:               2,
		SuccessfulCalls:            80,
		FailedCalls:                20,
	}

	aggregate := &SLOProbeAggregate{
		ToolId:                tool.ToolId,
		MedianAvailabilityBps: 9900,
		MedianErrorRateBps:    100,
		AggregatedAt:          timestamppb.New(now.Add(2 * time.Minute)),
		Version:               1,
		Status:                SLOProbeAggregateStatusFinal,
	}

	resolved, err := BuildTxPrioritizerResolvedInputsV1(tool, bond, aggregate, 128)
	require.NoError(t, err)
	require.Equal(t, tool.ToolId, resolved.ToolID)
	require.Equal(t, tool.Owner, resolved.Publisher)
	require.Equal(t, tool.Version, resolved.ToolVersion)
	require.Equal(t, DefaultTxPrioritizerFeeDenom, resolved.Fee.Denom)
	require.Equal(t, "0.8", resolved.Reputation.SuccessRate)
	require.Equal(t, "0.02", resolved.Reputation.DisputeRate)
	require.Equal(t, "0.99", resolved.Reputation.Availability)
	require.Equal(t, "0.01", resolved.Reputation.ErrorRate)
	require.Equal(t, "0.892", resolved.Reputation.Score)
	require.True(t, resolved.Reputation.Verified)
	require.Equal(t, "10000", resolved.Stake.BondedAmount)
	require.Equal(t, "5000", resolved.Stake.MinimumRequired)
	require.Equal(t, "2500", resolved.Stake.LockedAmount)
	require.Equal(t, "2", resolved.Stake.EffectiveRatio)
	require.Equal(t, "1.25", resolved.Stake.InsurancePremiumMultiplier)
	require.Equal(t, int64(128), resolved.Cache.SourceHeight)
	require.Equal(t, int64(129), resolved.Cache.ExpiresAfterHeight)
	require.Equal(t, TxPrioritizerConsistencyBlockFrozen, resolved.Cache.ConsistencyMode)
	require.True(t, strings.HasPrefix(resolved.Cache.DeterministicID, "blake3:"))
	require.NoError(t, resolved.Validate())
}

func TestTxPrioritizerReputationSnapshotV1MarshalJSON_OmitsZeroSourceTime(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(TxPrioritizerReputationSnapshotV1{
		Score:        "0.892",
		ScoreVersion: TxPrioritizerReputationScoreVersionV1,
		SuccessRate:  "0.8",
		DisputeRate:  "0.02",
		Availability: "0.99",
		ErrorRate:    "0.01",
		SampleSize:   100,
		Verified:     true,
		SourceHeight: 128,
	})
	require.NoError(t, err)
	require.NotContains(t, string(payload), "source_time")
	require.NotContains(t, string(payload), "0001-01-01T00:00:00Z")
}

func TestTxPrioritizerStakeSnapshotV1MarshalJSON_NormalizesSourceTimeUTC(t *testing.T) {
	t.Parallel()

	sourceTime := time.Date(2026, time.April, 11, 9, 15, 0, 0, time.FixedZone("UTC-5", -5*60*60))
	payload, err := json.Marshal(TxPrioritizerStakeSnapshotV1{
		BondDenom:                  BondDenom,
		BondedAmount:               "10000",
		MinimumRequired:            "5000",
		LockedAmount:               "2500",
		EffectiveRatio:             "2",
		InsurancePremiumMultiplier: "1.25",
		Status:                     BondStatusActive,
		SourceHeight:               128,
		SourceTime:                 sourceTime,
	})
	require.NoError(t, err)
	require.Contains(t, string(payload), `"source_time":"2026-04-11T14:15:00Z"`)
	require.NotContains(t, string(payload), "0001-01-01T00:00:00Z")
}
