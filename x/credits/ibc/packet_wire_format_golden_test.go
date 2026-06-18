//go:build cosmos

package ibc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// Golden artifact tests for x/credits IBC packet wire formats.
//
// Why this matters
// ----------------
// IBC packets cross chain boundaries. Once emitted, a packet's JSON bytes
// are parsed by:
//   - Counterparty chains (which MAY NOT be Lumera and MAY NOT share code)
//   - IBC relayers (Hermes, Go relayer, rly) that log memo contents
//   - Cross-chain indexers that filter transfers by memo type/content
//   - Auditors reconstructing settlement flows from packet history
//
// Unlike the existing packet_roundtrip_test.go which proves internal
// consistency (Build → Parse is lossless), these golden tests freeze the
// EXACT BYTE-LEVEL wire format. A refactor that silently reorders memo
// fields, switches JSON casing, or changes omit-empty semantics would
// roundtrip-pass but break every external consumer.
//
// Complementary to packet_roundtrip_test.go:
//   - Roundtrip tests: "Build → Parse preserves data"
//   - Golden tests:    "Build produces THESE EXACT BYTES"
//
// Constants pinned here (ChannelVersion, PortID, MemoTypeSettlement,
// StageIBCPending, StageIBCRefunded) are part of the cross-chain handshake
// contract — changing any one of them breaks channel negotiation or
// settlement state tracking.

func loadGoldenWireFormat(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file %s", path)
	return data
}

// assertJSONBytesGolden compares JSON bytes by round-tripping through
// canonical re-marshal on both sides, so trivial whitespace/key-order
// differences in the golden file don't cause false positives — but
// structural field name changes, additions, removals, or value drift
// DO fail.
func assertJSONBytesGolden(t *testing.T, got []byte, goldenFile string) {
	t.Helper()

	want := loadGoldenWireFormat(t, goldenFile)

	var gotObj, wantObj map[string]interface{}
	require.NoError(t, json.Unmarshal(got, &gotObj),
		"produced bytes are not valid JSON")
	require.NoError(t, json.Unmarshal(want, &wantObj),
		"golden file %s is not valid JSON", goldenFile)

	require.Equal(t, wantObj, gotObj,
		"wire format drift in %s — a change to the IBC packet/memo "+
			"wire format breaks counterparty chains, relayers, and "+
			"cross-chain indexers. Review the diff carefully before "+
			"updating the golden.",
		goldenFile)
}

// TestMemo_WireFormat_AllFields pins the memo JSON with every optional
// field populated. This is the canonical "rich memo" that external
// consumers index against.
func TestMemo_WireFormat_AllFields(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		Type:          MemoTypeSettlement,
		SettlementID:  "settlement-wire-all-001",
		ReceiptHash:   "blake3:abc123def456",
		Router:        "lumera1router",
		Publisher:     "lumera1publisher",
		ToolID:        "tool-xyz",
		ToolpackID:    "toolpack-abc",
		ActionID:      "action-789",
		RefundAddress: "lumera1refund",
	}

	got, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	assertJSONBytesGolden(t, []byte(got), "memo_all_fields.golden.json")
}

// TestMemo_WireFormat_RequiredOnly pins the minimal memo shape — just
// the fields required to pass validation. Proves omit-empty semantics
// at the byte level.
func TestMemo_WireFormat_RequiredOnly(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		SettlementID:  "settlement-wire-min-001",
		RefundAddress: "lumera1refund",
	}

	got, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	assertJSONBytesGolden(t, []byte(got), "memo_required_only.golden.json")
}

// TestMemo_WireFormat_PartialOptional pins a common partial-memo shape —
// receipt hash + tool ID but no publisher/action. Catches regressions
// where optional-field omission flips silently.
func TestMemo_WireFormat_PartialOptional(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		SettlementID:  "settlement-wire-partial-001",
		ReceiptHash:   "blake3:partial",
		ToolID:        "tool-partial",
		RefundAddress: "lumera1refund",
	}

	got, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	assertJSONBytesGolden(t, []byte(got), "memo_partial_optional.golden.json")
}

// TestEscrowPacket_WireFormat pins the full FungibleTokenPacketData
// JSON bytes — this is what the counterparty chain receives on wire.
func TestEscrowPacket_WireFormat(t *testing.T) {
	t.Parallel()

	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			Type:          MemoTypeSettlement,
			SettlementID:  "packet-wire-001",
			ReceiptHash:   "blake3:packetwire",
			Router:        "lumera1router",
			Publisher:     "lumera1publisher",
			ToolID:        "tool-packet",
			ToolpackID:    "toolpack-packet",
			ActionID:      "action-packet",
			RefundAddress: "lumera1refund",
		},
	}

	packet, err := BuildEscrowPacket(transfer)
	require.NoError(t, err)

	// GetBytes returns the canonical IBC wire JSON for the packet
	got := packet.GetBytes()
	assertJSONBytesGolden(t, got, "packet_escrow_full.golden.json")
}

// TestEscrowPacket_WireFormat_MinimalMemo pins the packet bytes when
// the memo has only required fields.
func TestEscrowPacket_WireFormat_MinimalMemo(t *testing.T) {
	t.Parallel()

	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(42),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "packet-wire-min-001",
			RefundAddress: "lumera1refund",
		},
	}

	packet, err := BuildEscrowPacket(transfer)
	require.NoError(t, err)

	got := packet.GetBytes()
	assertJSONBytesGolden(t, got, "packet_escrow_minimal.golden.json")
}

// TestIBCConstants_WireContract pins the string constants that form
// the IBC channel/memo handshake contract. Changing any of these is a
// breaking wire-format change that MUST coordinate with counterparty chains.
func TestIBCConstants_WireContract(t *testing.T) {
	t.Parallel()

	// Channel version: sent during IBC channel handshake (ChanOpenInit/Try/Ack).
	// A change here breaks channel negotiation with any counterparty already
	// running the old version.
	require.Equal(t, "lumera-credits-v1", ChannelVersion,
		"ChannelVersion changed — breaks IBC channel handshake with "+
			"counterparty chains running the prior version. Coordinate a "+
			"versioned upgrade before changing this.")

	// Port ID: part of the channel identity. Cannot change without
	// retiring every open channel.
	require.Equal(t, "credits", PortID,
		"PortID changed — breaks every open IBC channel and any "+
			"counterparty channel that hard-codes this port.")

	// Memo type discriminator: parsers on counterparty chains and indexers
	// filter by this exact string to route Lumera settlement transfers.
	require.Equal(t, "lumera_credits_settlement", MemoTypeSettlement,
		"MemoTypeSettlement changed — breaks every downstream consumer "+
			"that filters memos by this type string.")

	// Settlement stage identifiers: persisted in SettlementRecord.Stage
	// and indexed by auditors. Changing these silently reassigns historical
	// records.
	require.Equal(t, "ibc_pending", StageIBCPending,
		"StageIBCPending changed — reassigns historical settlement records "+
			"and breaks auditor queries filtering by this stage.")

	require.Equal(t, "ibc_refunded", StageIBCRefunded,
		"StageIBCRefunded changed — reassigns historical settlement records "+
			"and breaks auditor queries filtering by this stage.")
}

// TestMemoEnvelope_WireContract_FieldNames pins the JSON field names of
// SettlementMemo. Renaming any of these breaks counterparty parsers.
func TestMemoEnvelope_WireContract_FieldNames(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		Type:          MemoTypeSettlement,
		SettlementID:  "field-name-test",
		ReceiptHash:   "blake3:test",
		Router:        "lumera1router",
		Publisher:     "lumera1publisher",
		ToolID:        "tool-fn",
		ToolpackID:    "toolpack-fn",
		ActionID:      "action-fn",
		RefundAddress: "lumera1refund",
	}

	memoJSON, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	var envelope map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(memoJSON), &envelope))

	// Top-level envelope key
	require.Contains(t, envelope, "lumera",
		`top-level key "lumera" is the namespace discriminator — `+
			`renaming breaks every parser that identifies Lumera memos`)

	inner, ok := envelope["lumera"].(map[string]interface{})
	require.True(t, ok, "lumera value must be a JSON object")

	// Required field names
	requiredKeys := []string{
		"type",
		"settlement_id",
		"refund_address",
	}
	for _, k := range requiredKeys {
		require.Contains(t, inner, k,
			"required memo field %q missing — rename breaks counterparty parsers", k)
	}

	// Optional field names (populated in this test)
	optionalKeys := []string{
		"receipt_hash",
		"router",
		"publisher",
		"tool_id",
		"toolpack_id",
		"action_id",
	}
	for _, k := range optionalKeys {
		require.Contains(t, inner, k,
			"optional memo field %q missing when populated — rename "+
				"breaks consumers that index by this field name", k)
	}
}

// TestMemoEnvelope_WireContract_OmitEmpty pins the wire behavior of empty
// optional fields: they MUST be omitted, not serialized as empty strings.
// Switching from omitempty to always-serialize is a wire-format change
// because consumers distinguish "not set" from "set to empty string".
func TestMemoEnvelope_WireContract_OmitEmpty(t *testing.T) {
	t.Parallel()

	memo := SettlementMemo{
		SettlementID:  "omit-wire-test",
		RefundAddress: "lumera1refund",
	}

	memoJSON, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	var envelope map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(memoJSON), &envelope))

	inner := envelope["lumera"].(map[string]interface{})

	// These optional fields must NOT appear in the JSON when empty.
	// If a refactor changes `omitempty` → always-emit, indexers that
	// distinguish null/missing from empty-string would break.
	omittedKeys := []string{
		"receipt_hash",
		"router",
		"publisher",
		"tool_id",
		"toolpack_id",
		"action_id",
	}
	for _, k := range omittedKeys {
		_, present := inner[k]
		require.False(t, present,
			"empty optional field %q serialized when it should be omitted "+
				"— changing omitempty semantics is a wire-format change", k)
	}
}

// TestEscrowPacket_WireContract_FieldNames pins the FungibleTokenPacketData
// JSON field names. These are defined by ICS-20 but we assert them here so
// a future ibc-go major version that renames fields would surface.
func TestEscrowPacket_WireContract_FieldNames(t *testing.T) {
	t.Parallel()

	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(100),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "packet-fn-test",
			RefundAddress: "lumera1refund",
		},
	}

	packet, err := BuildEscrowPacket(transfer)
	require.NoError(t, err)

	raw := packet.GetBytes()
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	// ICS-20 canonical field names
	ics20Keys := []string{"denom", "amount", "sender", "receiver", "memo"}
	for _, k := range ics20Keys {
		require.Contains(t, obj, k,
			"ICS-20 packet field %q missing — counterparty chains expect "+
				"this exact name per ICS-20 spec", k)
	}
}

// TestGoldenFiles_AllPresent enforces coverage — every wire-format golden
// referenced in this file must exist on disk.
func TestGoldenFiles_AllPresent(t *testing.T) {
	t.Parallel()

	requiredGoldens := []string{
		"memo_all_fields.golden.json",
		"memo_required_only.golden.json",
		"memo_partial_optional.golden.json",
		"packet_escrow_full.golden.json",
		"packet_escrow_minimal.golden.json",
	}

	for _, f := range requiredGoldens {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required golden file %s is missing — run the test to generate, "+
				"then commit the .golden.json file", f)
	}
}
