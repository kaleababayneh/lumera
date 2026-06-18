
package types

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func validLaneEntry() *LaneRegistryEntry {
	return &LaneRegistryEntry{
		LaneId:     "lane-1",
		LaneType:   LaneTypeConfidentialTEE,
		OperatorId: "operator-1",
		Attestation: &LaneAttestationPolicy{
			TeeType:             LaneTEETypeNitro,
			PolicyHash:          bytes.Repeat([]byte{0x01}, 32),
			AllowedMeasurements: [][]byte{bytes.Repeat([]byte{0x02}, 32)},
			MinSvn:              1,
			SignerKeys:          [][]byte{bytes.Repeat([]byte{0x03}, 32)},
		},
		Metering: &LaneMeteringConfig{
			MeterType:      LaneMeterTypeTEESigned,
			PricingModelId: "pricing-1",
		},
		Compliance: &LaneComplianceConfig{
			EgressAllowlist: []string{"https://api.example.com"},
			LogPolicy:       LaneLogPolicyHashOnly,
			PiiPolicy:       LanePIIPolicyDeny,
		},
	}
}

func TestLaneRegistryEntryValidate(t *testing.T) {
	entry := validLaneEntry()
	require.NoError(t, entry.Validate())

	entry.LaneId = ""
	require.Error(t, entry.Validate())
	entry.LaneId = "lane-1"

	entry.LaneType = "unknown"
	require.Error(t, entry.Validate())
	entry.LaneType = LaneTypeConfidentialTEE

	entry.Metering.MeterType = "invalid"
	require.Error(t, entry.Validate())
	entry.Metering.MeterType = LaneMeterTypeTEESigned

	entry.Compliance.LogPolicy = "bad"
	require.Error(t, entry.Validate())
	entry.Compliance.LogPolicy = LaneLogPolicyHashOnly

	entry.Compliance.PiiPolicy = "bad"
	require.Error(t, entry.Validate())
	entry.Compliance.PiiPolicy = LanePIIPolicyDeny

	entry.Compliance.EgressAllowlist = []string{"ftp://example.com"}
	require.Error(t, entry.Validate())
	entry.Compliance.EgressAllowlist = []string{"https://api.example.com"}

	entry.Attestation.PolicyHash = []byte{0x01}
	require.Error(t, entry.Validate())
	entry.Attestation.PolicyHash = bytes.Repeat([]byte{0x01}, 32)

	entry.AllowedCapsuleHashes = [][]byte{[]byte{0x01}}
	require.Error(t, entry.Validate())
	entry.AllowedCapsuleHashes = [][]byte{bytes.Repeat([]byte{0x02}, 32)}
}

func TestLaneRegistryEntryPublicAllowsNoAttestation(t *testing.T) {
	entry := validLaneEntry()
	entry.LaneType = LaneTypePublic
	entry.Attestation = nil
	require.NoError(t, entry.Validate())
}
