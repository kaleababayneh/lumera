
package types

import (
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// keys.go - empty string edge cases not covered in types_test.go
// ---------------------------------------------------------------------------

func TestGetClaimKey_EmptyID(t *testing.T) {
	key := GetClaimKey("")
	// Empty string produces just the prefix
	assert.Equal(t, ClaimsKeyPrefix, key)
}

func TestGetContributionKey_EmptyID(t *testing.T) {
	key := GetContributionKey("")
	assert.Equal(t, ContributionsKeyPrefix, key)
}

func TestGetPublisherRiskKey_EmptyID(t *testing.T) {
	key := GetPublisherRiskKey("")
	assert.Equal(t, PublisherRiskKeyPrefix, key)
}

func TestGetPayoutKey_EmptyID(t *testing.T) {
	key := GetPayoutKey("")
	assert.Equal(t, PayoutsKeyPrefix, key)
}

func TestGetClaimByReceiptIndexKey_EmptyBoth(t *testing.T) {
	key := GetClaimByReceiptIndexKey("", "")
	assert.Equal(t, ClaimsByReceiptIndexPrefix, key)
}

func TestGetClaimByStatusIndexKey_EmptyBoth(t *testing.T) {
	key := GetClaimByStatusIndexKey("", "")
	assert.Equal(t, ClaimsByStatusIndexPrefix, key)
}

func TestGetReceiptOwnersKeyPrefix_Value(t *testing.T) {
	prefix := GetReceiptOwnersKeyPrefix()
	assert.Equal(t, ReceiptOwnersKeyPrefix, prefix)
	assert.Len(t, prefix, 1)
}

// ---------------------------------------------------------------------------
// keys.go - verify all index prefixes are single-byte and unique
// ---------------------------------------------------------------------------

func TestAllKeyPrefixesSingleByteAndUnique(t *testing.T) {
	prefixes := map[string][]byte{
		"PoolKey":                     PoolKey,
		"BondPrefix":                  BondPrefix,
		"ClaimsKeyPrefix":             ClaimsKeyPrefix,
		"ContributionsKeyPrefix":      ContributionsKeyPrefix,
		"PublisherRiskKeyPrefix":      PublisherRiskKeyPrefix,
		"PayoutsKeyPrefix":            PayoutsKeyPrefix,
		"ParamsKey":                   ParamsKey,
		"MetricsKey":                  MetricsKey,
		"ClaimsByReceiptIndexPrefix":  ClaimsByReceiptIndexPrefix,
		"ClaimsByStatusIndexPrefix":   ClaimsByStatusIndexPrefix,
		"ClaimSequenceKey":            ClaimSequenceKey,
		"ContributionSequenceKey":     ContributionSequenceKey,
		"PayoutSequenceKey":           PayoutSequenceKey,
		"ClaimReceiptIndexPrefix":     ClaimReceiptIndexPrefix,
		"ClaimClaimantIndexPrefix":    ClaimClaimantIndexPrefix,
		"ClaimPublisherIndexPrefix":   ClaimPublisherIndexPrefix,
		"ClaimStatusIndexPrefix":      ClaimStatusIndexPrefix,
		"ContribReceiptIndexPrefix":   ContribReceiptIndexPrefix,
		"ContribPublisherIndexPrefix": ContribPublisherIndexPrefix,
		"ContribToolIndexPrefix":      ContribToolIndexPrefix,
		"PayoutClaimIndexPrefix":      PayoutClaimIndexPrefix,
		"PayoutRecipientIndexPrefix":  PayoutRecipientIndexPrefix,
		"PayoutStatusIndexPrefix":     PayoutStatusIndexPrefix,
		"PoolBalanceKey":              PoolBalanceKey,
		"PoolMetricsKeyVal":           PoolMetricsKeyVal,
		"ClaimCreatedIndexPrefix":     ClaimCreatedIndexPrefix,
		"ReceiptOwnersKeyPrefix":      ReceiptOwnersKeyPrefix,
	}

	seen := make(map[byte]string)
	for name, prefix := range prefixes {
		require.Len(t, prefix, 1, "%s should be single byte", name)
		if existing, exists := seen[prefix[0]]; exists {
			t.Errorf("duplicate prefix byte 0x%02x: %s and %s", prefix[0], existing, name)
		}
		seen[prefix[0]] = name
	}
}

// ---------------------------------------------------------------------------
// msgs.go - whitespace edge cases for ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgProcessContribution_ValidateBasic_OnlySpacesAuthority(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   "   ",
		ReceiptId:   "receipt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authority is required")
}

func TestMsgProcessContribution_ValidateBasic_TabsInReceiptId(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-boost-0001"),
		ReceiptId:   "\t\t",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "receipt_id is required")
}

func TestMsgProcessContribution_ValidateBasic_NewlinesInToolId(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-boost-0001"),
		ReceiptId:   "receipt-1",
		ToolId:      "\n\n",
		PublisherId: "pub-1",
		Amount:      sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool_id is required")
}

func TestMsgProcessContribution_ValidateBasic_MixedWhitespacePublisherId(t *testing.T) {
	msg := &MsgProcessContribution{
		Authority:   validInsuranceAddress("insurance-boost-0001"),
		ReceiptId:   "receipt-1",
		ToolId:      "tool-1",
		PublisherId: " \t\n ",
		Amount:      sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "publisher_id is required")
}

func TestMsgFileClaim_ValidateBasic_WhitespaceClaimant(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      "  \t\n  ",
		ReceiptId:     "receipt-1",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "test",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claimant is required")
}

func TestMsgFileClaim_ValidateBasic_WhitespaceReceiptId(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-boost-0002"),
		ReceiptId:     "\t",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "test",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "receipt_id is required")
}

func TestMsgFileClaim_ValidateBasic_WhitespaceToolId(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-boost-0002"),
		ReceiptId:     "receipt-1",
		ToolId:        "   ",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "test",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool_id is required")
}

func TestMsgFileClaim_ValidateBasic_WhitespaceReason(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-boost-0002"),
		ReceiptId:     "receipt-1",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "   ",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reason is required")
}

// TestMsgFileClaim_ValidateBasic_RejectsOversizedEvidenceSlice pins
// the per-message cap on the length of Evidence. Without it, a
// claimant could attach 100k+ evidence entries, each with 4 × MB-
// scale strings, bloating every stored claim and slowing payout-
// processing state walks. Even with the bond/stake gate on claims,
// a single bad actor could submit cheaply-priced huge claims.
func TestMsgFileClaim_ValidateBasic_RejectsOversizedEvidenceSlice(t *testing.T) {
	ev := make([]*Evidence, MaxEvidenceEntries+1)
	for i := range ev {
		ev[i] = &Evidence{Type: "log", Hash: "blake3:x"}
	}
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-boost-0002"),
		ReceiptId:     "receipt-1",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "test",
		Evidence:      ev,
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evidence")
	assert.Contains(t, err.Error(), "cap")
}

// TestMsgFileClaim_ValidateBasic_RejectsOversizedEvidenceField pins
// the per-field length cap. A single evidence entry with a huge
// description would otherwise be a storage-bomb.
func TestMsgFileClaim_ValidateBasic_RejectsOversizedEvidenceField(t *testing.T) {
	huge := make([]byte, MaxEvidenceFieldLen+1)
	for i := range huge {
		huge[i] = 'a'
	}
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-boost-0002"),
		ReceiptId:     "receipt-1",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "test",
		Evidence:      []*Evidence{{Type: "log", Description: string(huge)}},
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evidence[0].description")
	assert.Contains(t, err.Error(), "cap")
}

// TestMsgFileClaim_ValidateBasic_AcceptsAtCapEvidence pins the cap
// as a threshold, not a floor. A legitimate full-cap claim passes.
func TestMsgFileClaim_ValidateBasic_AcceptsAtCapEvidence(t *testing.T) {
	ev := make([]*Evidence, MaxEvidenceEntries)
	for i := range ev {
		ev[i] = &Evidence{Type: "log", Hash: "blake3:x", Description: "ok"}
	}
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-boost-0002"),
		ReceiptId:     "receipt-1",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "test",
		Evidence:      ev,
	}
	require.NoError(t, msg.ValidateBasic())
}

// TestMsgFileClaim_ValidateBasic_RejectsOversizedReason pins the
// length cap on the free-form `reason` string. Unlike the ID fields
// (slug-shaped, ~50 bytes), reason is human text — a claimant could
// attach megabytes of descriptive content before this guard.
func TestMsgFileClaim_ValidateBasic_RejectsOversizedReason(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      validInsuranceAddress("insurance-boost-0002"),
		ReceiptId:     "receipt-1",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        strings.Repeat("a", MaxClaimReasonLen+1),
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "reason")
	require.Contains(t, err.Error(), "cap")
}

// TestMsgFileClaim_ValidateBasic_RejectsOversizedClaimant pins the
// ID cap on claimant (bech32 address, realistic ~43 bytes).
func TestMsgFileClaim_ValidateBasic_RejectsOversizedClaimant(t *testing.T) {
	msg := &MsgFileClaim{
		Claimant:      strings.Repeat("a", MaxInsuranceIDLen+1),
		ReceiptId:     "receipt-1",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "test",
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "claimant")
	require.Contains(t, err.Error(), "cap")
}

// TestMsgProcessContribution_ValidateBasic_RejectsOversizedIDs pins
// caps on receipt_id, tool_id, and publisher_id. Authority-gated but
// defense-in-depth prevents a compromised/buggy authority from
// injecting unbounded ID strings into contribution records.
func TestMsgProcessContribution_ValidateBasic_RejectsOversizedIDs(t *testing.T) {
	base := func() *MsgProcessContribution {
		return &MsgProcessContribution{
			Authority:   validInsuranceAddress("insurance-boost-0001"),
			ReceiptId:   "receipt-1",
			ToolId:      "tool-1",
			PublisherId: "pub-1",
			Amount:      sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		}
	}
	for _, tc := range []struct {
		name  string
		mut   func(*MsgProcessContribution)
		field string
	}{
		{
			name:  "oversized receipt_id",
			mut:   func(m *MsgProcessContribution) { m.ReceiptId = strings.Repeat("a", MaxInsuranceIDLen+1) },
			field: "receipt_id",
		},
		{
			name:  "oversized tool_id",
			mut:   func(m *MsgProcessContribution) { m.ToolId = strings.Repeat("a", MaxInsuranceIDLen+1) },
			field: "tool_id",
		},
		{
			name:  "oversized publisher_id",
			mut:   func(m *MsgProcessContribution) { m.PublisherId = strings.Repeat("a", MaxInsuranceIDLen+1) },
			field: "publisher_id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := base()
			tc.mut(msg)
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.field)
			require.Contains(t, err.Error(), "cap")
		})
	}
}

// TestMsgProcessClaim_ValidateBasic_RejectsOversizedClaimID pins
// the ID cap on claim_id.
func TestMsgProcessClaim_ValidateBasic_RejectsOversizedClaimID(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-boost-0003"),
		ClaimId:    strings.Repeat("a", MaxInsuranceIDLen+1),
		Resolution: "approve",
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "claim_id")
	require.Contains(t, err.Error(), "cap")
}

// TestMsgProcessPayout_ValidateBasic_RejectsOversizedIDs pins the
// caps on claim_id and recipient.
func TestMsgProcessPayout_ValidateBasic_RejectsOversizedIDs(t *testing.T) {
	base := func() *MsgProcessPayout {
		return &MsgProcessPayout{
			Authority: validInsuranceAddress("insurance-boost-0004"),
			ClaimId:   "claim-1",
			Recipient: validInsuranceAddress("insurance-boost-0005"),
			Amount:    sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		}
	}
	for _, tc := range []struct {
		name  string
		mut   func(*MsgProcessPayout)
		field string
	}{
		{
			name:  "oversized claim_id",
			mut:   func(m *MsgProcessPayout) { m.ClaimId = strings.Repeat("a", MaxInsuranceIDLen+1) },
			field: "claim_id",
		},
		{
			name:  "oversized recipient",
			mut:   func(m *MsgProcessPayout) { m.Recipient = strings.Repeat("a", MaxInsuranceIDLen+1) },
			field: "recipient",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := base()
			tc.mut(msg)
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.field)
			require.Contains(t, err.Error(), "cap")
		})
	}
}

// TestMsgUpdatePublisherRisk_ValidateBasic_RejectsOversizedIDs pins
// the ID caps on publisher_id and tool_id.
func TestMsgUpdatePublisherRisk_ValidateBasic_RejectsOversizedIDs(t *testing.T) {
	base := func() *MsgUpdatePublisherRisk {
		return &MsgUpdatePublisherRisk{
			Authority:    validInsuranceAddress("insurance-boost-0006"),
			PublisherId:  "pub-1",
			ToolId:       "tool-1",
			RiskScoreBps: 500,
		}
	}
	for _, tc := range []struct {
		name  string
		mut   func(*MsgUpdatePublisherRisk)
		field string
	}{
		{
			name:  "oversized publisher_id",
			mut:   func(m *MsgUpdatePublisherRisk) { m.PublisherId = strings.Repeat("a", MaxInsuranceIDLen+1) },
			field: "publisher_id",
		},
		{
			name:  "oversized tool_id",
			mut:   func(m *MsgUpdatePublisherRisk) { m.ToolId = strings.Repeat("a", MaxInsuranceIDLen+1) },
			field: "tool_id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := base()
			tc.mut(msg)
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.field)
			require.Contains(t, err.Error(), "cap")
		})
	}
}

func TestMsgProcessClaim_ValidateBasic_WhitespaceAuthority(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  "  ",
		ClaimId:    "claim-1",
		Resolution: "approve",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authority is required")
}

func TestMsgProcessClaim_ValidateBasic_WhitespaceClaimId(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-boost-0003"),
		ClaimId:    "\t\n",
		Resolution: "approve",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claim_id is required")
}

func TestMsgProcessClaim_ValidateBasic_EmptyResolution(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-boost-0003"),
		ClaimId:    "claim-1",
		Resolution: "",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "resolution must be approve, reject, or partial")
}

func TestMsgProcessClaim_ValidateBasic_UppercaseResolution(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-boost-0003"),
		ClaimId:    "claim-1",
		Resolution: "APPROVE",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "resolution must be approve, reject, or partial")
}

func TestMsgProcessClaim_ValidateBasic_MixedCaseResolution(t *testing.T) {
	msg := &MsgProcessClaim{
		Authority:  validInsuranceAddress("insurance-boost-0003"),
		ClaimId:    "claim-1",
		Resolution: "Reject",
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
}

func TestMsgProcessPayout_ValidateBasic_WhitespaceAuthority(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: "   ",
		ClaimId:   "claim-1",
		Recipient: validInsuranceAddress("insurance-boost-0005"),
		Amount:    sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authority is required")
}

func TestMsgProcessPayout_ValidateBasic_WhitespaceClaimId(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: validInsuranceAddress("insurance-boost-0004"),
		ClaimId:   "\n\t",
		Recipient: validInsuranceAddress("insurance-boost-0005"),
		Amount:    sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claim_id is required")
}

func TestMsgProcessPayout_ValidateBasic_WhitespaceRecipient(t *testing.T) {
	msg := &MsgProcessPayout{
		Authority: validInsuranceAddress("insurance-boost-0004"),
		ClaimId:   "claim-1",
		Recipient: "  ",
		Amount:    sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	}
	err := msg.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "recipient is required")
}

func TestMsgUpdatePublisherRisk_ValidateBasic_WhitespaceFields(t *testing.T) {
	tests := []struct {
		name      string
		authority string
		pubID     string
		toolID    string
		wantErr   string
	}{
		{"whitespace authority", "   ", "pub-1", "tool-1", "authority is required"},
		{"whitespace publisher", validInsuranceAddress("insurance-boost-0006"), "\t", "tool-1", "publisher_id is required"},
		{"whitespace tool", validInsuranceAddress("insurance-boost-0006"), "pub-1", "\n", "tool_id is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MsgUpdatePublisherRisk{
				Authority:    tc.authority,
				PublisherId:  tc.pubID,
				ToolId:       tc.toolID,
				RiskScoreBps: 500,
			}
			err := msg.ValidateBasic()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestMsgUpdatePublisherRisk_ValidateBasic_BoundaryScores(t *testing.T) {
	tests := []struct {
		name    string
		score   uint32
		wantErr bool
	}{
		{"zero score valid", 0, false},
		{"max score valid", 10_000, false},
		{"over max invalid", 10_001, true},
		{"way over max", 50_000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MsgUpdatePublisherRisk{
				Authority:    validInsuranceAddress("insurance-boost-0006"),
				PublisherId:  "pub-1",
				ToolId:       "tool-1",
				RiskScoreBps: tc.score,
			}
			err := msg.ValidateBasic()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// genesis.go - additional validation edge cases
// ---------------------------------------------------------------------------

func TestGenesisState_Validate_NilPoolIsValid(t *testing.T) {
	gs := DefaultGenesis()
	gs.Pool = nil
	err := gs.Validate()
	assert.NoError(t, err)
}

func TestGenesisState_Validate_ClaimWithNilAmountIsValid(t *testing.T) {
	gs := DefaultGenesis()
	gs.Claims = []*Claim{
		{Id: "claim-1", Status: ClaimStatus_CLAIM_STATUS_PENDING, ClaimedAmount: sdk.Coin{}},
	}
	err := gs.Validate()
	assert.NoError(t, err)
}

func TestGenesisState_Validate_ClaimEmptyAmountStringParsesAsZero(t *testing.T) {
	gs := DefaultGenesis()
	gs.Claims = []*Claim{
		{
			Id:     "claim-1",
			Status: ClaimStatus_CLAIM_STATUS_PENDING,
			// Post-gogoproto ClaimedAmount is a value sdk.Coin; an unset amount is
			// a nil math.Int (IsNil), which genesis.go skips as valid — the gogo
			// analogue of the old empty-amount-string-parses-as-zero case.
			ClaimedAmount: sdk.Coin{Denom: "ulac"},
		},
	}
	err := gs.Validate()
	assert.NoError(t, err)
}

func TestGenesisState_Validate_LargeSequenceNumbers(t *testing.T) {
	gs := DefaultGenesis()
	gs.ClaimSequence = ^uint64(0)
	gs.PayoutSequence = ^uint64(0)
	err := gs.Validate()
	assert.NoError(t, err)
}

func TestParams_ValidateBasic_PremiumAdjustmentBpsBoundary(t *testing.T) {
	tests := []struct {
		name    string
		bps     uint32
		wantErr bool
	}{
		{"zero valid", 0, false},
		{"max valid", 1_000, false},
		{"over max", 1_001, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultParams()
			p.PremiumAdjustmentBps = tc.bps
			err := p.ValidateBasic()
			if tc.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "premium adjustment BPS")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParams_ValidateBasic_SlashDecayDaysZeroValue(t *testing.T) {
	p := DefaultParams()
	p.SlashDecayDays = 0
	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "slash decay days must be greater than 0")
}

func TestParams_ValidateBasic_MaxClaimsPerBlockZeroValue(t *testing.T) {
	p := DefaultParams()
	p.MaxClaimsPerBlock = 0
	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max claims per block must be positive")
}

func TestParams_ValidateBasic_MaxPayoutsPerBlockZeroValue(t *testing.T) {
	p := DefaultParams()
	p.MaxPayoutsPerBlock = 0
	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max payouts per block must be positive")
}

func TestParams_ValidateBasic_NegativeClaimWindowSeconds(t *testing.T) {
	p := DefaultParams()
	p.ClaimWindowSeconds = -100
	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claim window seconds must be positive")
}

func TestParams_ValidateBasic_ClaimWindowSecondsExceedsDurationLimit(t *testing.T) {
	p := DefaultParams()
	p.ClaimWindowSeconds = MaxClaimWindowSeconds + 1

	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claim window seconds exceeds maximum safe duration")
}

func TestClaimWindowDurationRejectsUnsafeValues(t *testing.T) {
	duration, err := ClaimWindowDuration(MaxClaimWindowSeconds)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(MaxClaimWindowSeconds)*time.Second, duration)

	_, err = ClaimWindowDuration(0)
	require.ErrorContains(t, err, "claim window seconds must be positive")

	_, err = ClaimWindowDuration(MaxClaimWindowSeconds + 1)
	require.ErrorContains(t, err, "claim window seconds exceeds maximum safe duration")
}

func TestParams_ValidateBasic_InvalidDisputeStakeLac(t *testing.T) {
	p := DefaultParams()
	p.DisputeStakeLac = "not-a-number"
	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid dispute stake")
}

func TestParams_ValidateBasic_NegativeDisputeStakeLac(t *testing.T) {
	p := DefaultParams()
	p.DisputeStakeLac = "-50"
	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dispute stake cannot be negative")
}

func TestParams_ValidateBasic_InvalidAutoApproveThreshold(t *testing.T) {
	p := DefaultParams()
	p.AutoApproveThreshold = "invalid"
	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid auto approve threshold")
}

func TestParams_ValidateBasic_NegativeAutoApproveThreshold(t *testing.T) {
	p := DefaultParams()
	p.AutoApproveThreshold = "-10"
	err := p.ValidateBasic()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "auto approve threshold cannot be negative")
}

// ---------------------------------------------------------------------------
// codec.go coverage
// ---------------------------------------------------------------------------

func TestRegisterLegacyAminoCodec_NoPanic(t *testing.T) {
	cdc := codec.NewLegacyAmino()
	require.NotPanics(t, func() {
		RegisterLegacyAminoCodec(cdc)
	})
}

func TestRegisterInterfaces_NoPanic(t *testing.T) {
	registry := cdctypes.NewInterfaceRegistry()
	require.NotPanics(t, func() {
		RegisterInterfaces(registry)
	})
}

func TestModuleCdc_Initialized(t *testing.T) {
	require.NotNil(t, ModuleCdc)
}

func TestRegisterInterfaces_Idempotent(t *testing.T) {
	registry := cdctypes.NewInterfaceRegistry()
	RegisterInterfaces(registry)
	RegisterInterfaces(registry)
	RegisterInterfaces(registry)
	// Should not panic with multiple calls
}
