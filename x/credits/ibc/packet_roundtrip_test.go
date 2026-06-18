//go:build cosmos

package ibc

import (
	"encoding/json"
	"math/rand"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// IBC packet encoding round-trip conformance tests.
// These tests prove that settlement memo encoding is lossless and deterministic.
// IBC-critical: encoding inconsistency would cause packet verification failures
// across chains when relayers or counterparty chains decode the memo.

// TestSettlementMemo_RoundTrip_Lossless proves that Build → Parse produces
// identical data for all field combinations.
func TestSettlementMemo_RoundTrip_Lossless(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		memo SettlementMemo
	}{
		{
			name: "required_fields_only",
			memo: SettlementMemo{
				SettlementID:  "settlement-001",
				RefundAddress: "lumera1refund",
			},
		},
		{
			name: "all_fields_populated",
			memo: SettlementMemo{
				Type:          MemoTypeSettlement,
				SettlementID:  "settlement-002",
				ReceiptHash:   "blake3:abc123def456",
				Router:        "lumera1router",
				Publisher:     "lumera1publisher",
				ToolID:        "tool-xyz",
				ToolpackID:    "toolpack-abc",
				ActionID:      "action-789",
				RefundAddress: "lumera1refund",
			},
		},
		{
			name: "partial_optional_fields",
			memo: SettlementMemo{
				SettlementID:  "settlement-003",
				ReceiptHash:   "blake3:partial",
				ToolID:        "tool-only",
				RefundAddress: "lumera1refund",
			},
		},
		{
			name: "unicode_in_ids",
			memo: SettlementMemo{
				SettlementID:  "settlement-日本語-001",
				ReceiptHash:   "blake3:émojis🎉",
				RefundAddress: "lumera1refund",
			},
		},
		{
			name: "special_characters",
			memo: SettlementMemo{
				SettlementID:  `settlement-with"quotes`,
				ReceiptHash:   "blake3:with\\backslash",
				RefundAddress: "lumera1refund",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Build memo JSON
			memoJSON, err := BuildSettlementMemo(tc.memo)
			require.NoError(t, err, "BuildSettlementMemo failed")

			// Parse it back
			parsed, ok, err := ParseSettlementMemo(memoJSON)
			require.NoError(t, err, "ParseSettlementMemo failed")
			require.True(t, ok, "ParseSettlementMemo returned ok=false")

			// Verify all fields match
			require.Equal(t, MemoTypeSettlement, parsed.Type,
				"Type field mismatch after round-trip")
			require.Equal(t, tc.memo.SettlementID, parsed.SettlementID,
				"SettlementID field lost in round-trip")
			require.Equal(t, tc.memo.ReceiptHash, parsed.ReceiptHash,
				"ReceiptHash field lost in round-trip")
			require.Equal(t, tc.memo.Router, parsed.Router,
				"Router field lost in round-trip")
			require.Equal(t, tc.memo.Publisher, parsed.Publisher,
				"Publisher field lost in round-trip")
			require.Equal(t, tc.memo.ToolID, parsed.ToolID,
				"ToolID field lost in round-trip")
			require.Equal(t, tc.memo.ToolpackID, parsed.ToolpackID,
				"ToolpackID field lost in round-trip")
			require.Equal(t, tc.memo.ActionID, parsed.ActionID,
				"ActionID field lost in round-trip")
			require.Equal(t, tc.memo.RefundAddress, parsed.RefundAddress,
				"RefundAddress field lost in round-trip")
		})
	}
}

// TestSettlementMemo_RoundTrip_Deterministic proves that encoding the same
// memo multiple times produces identical JSON bytes.
func TestSettlementMemo_RoundTrip_Deterministic(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		Type:          MemoTypeSettlement,
		SettlementID:  "settlement-deterministic",
		ReceiptHash:   "blake3:deterministic",
		Router:        "lumera1router",
		Publisher:     "lumera1publisher",
		ToolID:        "tool-001",
		ToolpackID:    "toolpack-001",
		ActionID:      "action-001",
		RefundAddress: "lumera1refund",
	}

	// Build 10 times and verify byte equality
	var firstJSON string
	for i := 0; i < 10; i++ {
		memoJSON, err := BuildSettlementMemo(memo)
		require.NoError(t, err)

		if i == 0 {
			firstJSON = memoJSON
			continue
		}

		require.Equal(t, firstJSON, memoJSON,
			"iteration %d: JSON encoding not deterministic — IBC packet verification would fail", i)
	}
}

// TestSettlementMemo_MultipleRoundTrips proves that multiple round-trips
// don't mutate the data (no accumulating drift).
func TestSettlementMemo_MultipleRoundTrips(t *testing.T) {
	t.Parallel()

	original := SettlementMemo{
		SettlementID:  "settlement-multi",
		ReceiptHash:   "blake3:multi",
		Router:        "lumera1router",
		Publisher:     "lumera1publisher",
		ToolID:        "tool-multi",
		RefundAddress: "lumera1refund",
	}

	current := original
	for i := 0; i < 5; i++ {
		// Build
		memoJSON, err := BuildSettlementMemo(current)
		require.NoError(t, err, "round-trip %d: build failed", i)

		// Parse
		parsed, ok, err := ParseSettlementMemo(memoJSON)
		require.NoError(t, err, "round-trip %d: parse failed", i)
		require.True(t, ok)

		// Verify against original (not just previous iteration)
		require.Equal(t, original.SettlementID, parsed.SettlementID,
			"round-trip %d: SettlementID drifted from original", i)
		require.Equal(t, original.ReceiptHash, parsed.ReceiptHash,
			"round-trip %d: ReceiptHash drifted from original", i)
		require.Equal(t, original.Router, parsed.Router,
			"round-trip %d: Router drifted from original", i)
		require.Equal(t, original.Publisher, parsed.Publisher,
			"round-trip %d: Publisher drifted from original", i)
		require.Equal(t, original.ToolID, parsed.ToolID,
			"round-trip %d: ToolID drifted from original", i)
		require.Equal(t, original.RefundAddress, parsed.RefundAddress,
			"round-trip %d: RefundAddress drifted from original", i)

		current = *parsed
	}
}

// TestEscrowTransfer_FullRoundTrip proves the full packet encoding flow:
// EscrowTransfer → FungibleTokenPacketData → ExtractSettlementMemo
func TestEscrowTransfer_FullRoundTrip(t *testing.T) {
	t.Parallel()

	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "full-roundtrip-001",
			ReceiptHash:   "blake3:fullroundtrip",
			Router:        "lumera1router",
			Publisher:     "lumera1publisher",
			ToolID:        "tool-full",
			ToolpackID:    "toolpack-full",
			ActionID:      "action-full",
			RefundAddress: "lumera1refund",
		},
	}

	// Build packet
	packet, err := BuildEscrowPacket(transfer)
	require.NoError(t, err)

	// Verify packet fields
	require.Equal(t, transfer.Denom, packet.Denom)
	require.Equal(t, transfer.Amount.String(), packet.Amount)
	require.Equal(t, transfer.Sender, packet.Sender)
	require.Equal(t, transfer.Receiver, packet.Receiver)

	// Extract and verify memo
	extracted, ok, err := ExtractSettlementMemo(packet)
	require.NoError(t, err)
	require.True(t, ok)

	require.Equal(t, transfer.Memo.SettlementID, extracted.SettlementID)
	require.Equal(t, transfer.Memo.ReceiptHash, extracted.ReceiptHash)
	require.Equal(t, transfer.Memo.Router, extracted.Router)
	require.Equal(t, transfer.Memo.Publisher, extracted.Publisher)
	require.Equal(t, transfer.Memo.ToolID, extracted.ToolID)
	require.Equal(t, transfer.Memo.ToolpackID, extracted.ToolpackID)
	require.Equal(t, transfer.Memo.ActionID, extracted.ActionID)
	require.Equal(t, transfer.Memo.RefundAddress, extracted.RefundAddress)
}

// TestMemoEnvelope_JSONFieldOrder proves JSON field ordering is stable.
func TestMemoEnvelope_JSONFieldOrder(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		SettlementID:  "order-test",
		RefundAddress: "lumera1refund",
	}

	json1, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	json2, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	// Byte-level equality proves field order stability
	require.Equal(t, json1, json2,
		"JSON field order not stable — would cause memo hash mismatches")
}

// TestMemoEnvelope_OmitEmptyBehavior proves empty optional fields are
// omitted from JSON (not serialized as empty strings).
func TestMemoEnvelope_OmitEmptyBehavior(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		SettlementID:  "omit-test",
		RefundAddress: "lumera1refund",
		// All other fields empty
	}

	memoJSON, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	// Parse raw JSON to check structure
	var raw map[string]interface{}
	err = json.Unmarshal([]byte(memoJSON), &raw)
	require.NoError(t, err)

	lumera, ok := raw["lumera"].(map[string]interface{})
	require.True(t, ok)

	// Required fields present
	require.Contains(t, lumera, "settlement_id")
	require.Contains(t, lumera, "refund_address")
	require.Contains(t, lumera, "type")

	// Optional empty fields should be omitted
	_, hasReceiptHash := lumera["receipt_hash"]
	_, hasRouter := lumera["router"]
	_, hasPublisher := lumera["publisher"]
	_, hasToolID := lumera["tool_id"]
	_, hasToolpackID := lumera["toolpack_id"]
	_, hasActionID := lumera["action_id"]

	require.False(t, hasReceiptHash, "empty receipt_hash should be omitted")
	require.False(t, hasRouter, "empty router should be omitted")
	require.False(t, hasPublisher, "empty publisher should be omitted")
	require.False(t, hasToolID, "empty tool_id should be omitted")
	require.False(t, hasToolpackID, "empty toolpack_id should be omitted")
	require.False(t, hasActionID, "empty action_id should be omitted")
}

// TestSettlementMemo_ParseTolerance proves parsing tolerates whitespace
// and default type inference.
func TestSettlementMemo_ParseTolerance(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		rawJSON  string
		wantID   string
		wantType string
	}{
		{
			name:     "with_leading_whitespace",
			rawJSON:  `   {"lumera":{"settlement_id":"ws-test","refund_address":"lumera1ref"}}`,
			wantID:   "ws-test",
			wantType: MemoTypeSettlement,
		},
		{
			name:     "with_trailing_whitespace",
			rawJSON:  `{"lumera":{"settlement_id":"ws-test2","refund_address":"lumera1ref"}}   `,
			wantID:   "ws-test2",
			wantType: MemoTypeSettlement,
		},
		{
			name:     "type_inferred_when_missing",
			rawJSON:  `{"lumera":{"settlement_id":"infer-type","refund_address":"lumera1ref"}}`,
			wantID:   "infer-type",
			wantType: MemoTypeSettlement,
		},
		{
			name:     "explicit_type",
			rawJSON:  `{"lumera":{"type":"lumera_credits_settlement","settlement_id":"explicit","refund_address":"lumera1ref"}}`,
			wantID:   "explicit",
			wantType: MemoTypeSettlement,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed, ok, err := ParseSettlementMemo(tc.rawJSON)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, tc.wantID, parsed.SettlementID)
			require.Equal(t, tc.wantType, parsed.Type)
		})
	}
}

// TestEscrowTransfer_AmountPrecision proves large amounts survive round-trip.
func TestEscrowTransfer_AmountPrecision(t *testing.T) {
	t.Parallel()

	testAmounts := []string{
		"1",
		"1000000",
		"1000000000000000000",                    // 10^18 (common in crypto)
		"115792089237316195423570985008687907853", // large but valid
	}

	for _, amtStr := range testAmounts {
		amtStr := amtStr
		t.Run("amount_"+amtStr, func(t *testing.T) {
			t.Parallel()

			amt, ok := sdkmath.NewIntFromString(amtStr)
			require.True(t, ok)

			transfer := EscrowTransfer{
				Denom:    "ulac",
				Amount:   amt,
				Sender:   "lumera1sender",
				Receiver: "inj1receiver",
				Memo: SettlementMemo{
					SettlementID:  "precision-test",
					RefundAddress: "lumera1refund",
				},
			}

			packet, err := BuildEscrowPacket(transfer)
			require.NoError(t, err)
			require.Equal(t, amtStr, packet.Amount,
				"amount precision lost in packet encoding")
		})
	}
}

// TestSettlementMemo_RandomizedRoundTrip uses randomized field values
// to find edge cases.
func TestSettlementMemo_RandomizedRoundTrip(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(12345))
	charset := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"

	randomString := func(length int) string {
		b := make([]byte, length)
		for i := range b {
			b[i] = charset[rng.Intn(len(charset))]
		}
		return string(b)
	}

	for i := 0; i < 50; i++ {
		memo := SettlementMemo{
			SettlementID:  "rand-" + randomString(10+rng.Intn(20)),
			ReceiptHash:   "blake3:" + randomString(32),
			Router:        "lumera1" + randomString(8),
			Publisher:     "lumera1" + randomString(8),
			ToolID:        "tool-" + randomString(5),
			ToolpackID:    "toolpack-" + randomString(5),
			ActionID:      "action-" + randomString(5),
			RefundAddress: "lumera1" + randomString(8),
		}

		memoJSON, err := BuildSettlementMemo(memo)
		require.NoError(t, err, "iteration %d: build failed", i)

		parsed, ok, err := ParseSettlementMemo(memoJSON)
		require.NoError(t, err, "iteration %d: parse failed", i)
		require.True(t, ok)

		require.Equal(t, memo.SettlementID, parsed.SettlementID,
			"iteration %d: SettlementID mismatch", i)
		require.Equal(t, memo.ReceiptHash, parsed.ReceiptHash,
			"iteration %d: ReceiptHash mismatch", i)
		require.Equal(t, memo.RefundAddress, parsed.RefundAddress,
			"iteration %d: RefundAddress mismatch", i)
	}
}

// TestBuildSettlementMemo_TypeDefaulting proves Type defaults to
// MemoTypeSettlement when empty.
func TestBuildSettlementMemo_TypeDefaulting(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		Type:          "", // empty
		SettlementID:  "type-default-test",
		RefundAddress: "lumera1refund",
	}

	memoJSON, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	parsed, ok, err := ParseSettlementMemo(memoJSON)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, MemoTypeSettlement, parsed.Type,
		"Type should default to MemoTypeSettlement")
}
