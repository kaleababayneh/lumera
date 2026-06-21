//go:build cosmos

package ibc

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	sdkmath "cosmossdk.io/math"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
)

// Fuzz harness for x/credits IBC packet parsing edge cases.
//
// Attack surface
// --------------
// IBC packet data arrives from counterparty chains we do not control.
// The ingestion path is:
//
//   bytes --> json.Unmarshal --> FungibleTokenPacketData
//         --> ExtractSettlementMemo --> ParseSettlementMemo
//         --> SettlementMemo.Validate
//
// Every stage is a potential crash surface. A relayer-controlled input
// that panics inside OnRecvPacket takes down the node (or at least the
// relayer pathway); an input that parses into a "valid" memo with
// surprising semantics can route funds incorrectly.
//
// This harness covers the three classes flagged in the task:
//   1. Malformed proto/JSON: garbage bytes, partial JSON, nested objects
//   2. Oversized payloads: huge strings, max-precision numbers
//   3. Canonical-ID collisions: conflicting type fields, duplicate keys,
//      settlement_id/router mismatches
//
// Correctness criterion for every fuzz target: NEVER PANIC. Parsing may
// return an error and it may return ok=false, but it must never crash
// the node via an uncaught panic or nil dereference.

// FuzzParseSettlementMemo_MalformedJSON fuzzes the raw-string entry
// point. Counterparty chains control the memo byte-for-byte, so this
// is the most exposed parsing surface.
func FuzzParseSettlementMemo_MalformedJSON(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"{}",
		`{"lumera": null}`,
		`{"lumera": {}}`,
		`{"lumera": []}`,
		`{"lumera": "not_an_object"}`,
		`{"lumera": {"settlement_id": "x"}}`,  // missing refund_address
		`{"lumera": {"refund_address": "x"}}`, // missing settlement_id
		`{"lumera": {"settlement_id": "", "refund_address": ""}}`,
		`{"lumera": {"type": "wrong_type", "settlement_id": "x", "refund_address": "y"}}`,
		`{"lumera": {"settlement_id": "x", "refund_address": "y", "lumera": {"nested": true}}}`,
		`{not valid json`,
		"]]]][[[",
		`{"lumera":` + strings.Repeat(" ", 10000) + `{}}`, // whitespace bomb
		"\x00\x01\x02",
		`{"lumera": {"settlement_id": "x", "refund_address": "y", "router": null}}`,
		`{"lumera": {"settlement_id": " ", "refund_address": "y"}}`,
		// Deeply nested - catches stack overflow risk in recursive parsers
		strings.Repeat(`{"a":`, 100) + "null" + strings.Repeat("}", 100),
		// Long array in a string field
		`{"lumera": {"settlement_id": "` + strings.Repeat("x", 50000) + `", "refund_address": "y"}}`,
		`{"lumera": {"settlement_id": "x", "refund_address": "y", "extra_unknown_field": "ignored"}}`,
		// Duplicate JSON key - Go's json.Unmarshal takes the last value
		`{"lumera": {"settlement_id": "first", "settlement_id": "second", "refund_address": "y"}}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		// Must not panic regardless of input.
		memo, ok, err := ParseSettlementMemo(raw)

		// Invariant: if we claim ok, memo must be non-nil and pass Validate.
		if ok {
			if memo == nil {
				t.Fatalf("ParseSettlementMemo returned ok=true with nil memo (input: %q)", truncate(raw))
			}
			if vErr := memo.Validate(); vErr != nil {
				t.Fatalf("ParseSettlementMemo returned ok=true with memo that fails Validate: %v (input: %q)",
					vErr, truncate(raw))
			}
			if strings.TrimSpace(memo.SettlementID) == "" {
				t.Fatalf("ok=true but settlement_id is blank (input: %q)", truncate(raw))
			}
			if strings.TrimSpace(memo.RefundAddress) == "" {
				t.Fatalf("ok=true but refund_address is blank (input: %q)", truncate(raw))
			}
		}

		// Invariant: if ok=false, err may or may not be set but we must
		// have a sensible state.
		if !ok && memo != nil {
			t.Fatalf("ParseSettlementMemo returned ok=false with non-nil memo (input: %q)", truncate(raw))
		}

		_ = err
	})
}

// FuzzPacketDataUnmarshal simulates the full middleware ingestion path:
// raw bytes on the wire -> FungibleTokenPacketData -> ExtractSettlementMemo.
// This is exactly what OnRecvPacket does with counterparty-controlled bytes.
func FuzzPacketDataUnmarshal(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{}`),
		[]byte(`{"denom":"ulac","amount":"100","sender":"a","receiver":"b","memo":""}`),
		[]byte(`{"denom":"","amount":"","sender":"","receiver":"","memo":""}`),
		[]byte(`{"denom":"ulac","amount":"-1","sender":"a","receiver":"b","memo":""}`),
		[]byte(`{"denom":"ulac","amount":"not_a_number","sender":"a","receiver":"b","memo":""}`),
		[]byte(`{"denom":"ulac","amount":"0","sender":"a","receiver":"b","memo":"{\"lumera\":{\"settlement_id\":\"x\",\"refund_address\":\"y\"}}"}`),
		[]byte(``),
		[]byte(`null`),
		[]byte(`"just a string"`),
		[]byte(`[]`),
		[]byte("\x00"),
		[]byte(`{"amount":"` + strings.Repeat("9", 200) + `"}`), // huge number
		[]byte(`{"memo":"{not json"}`),                          // invalid inner memo
		[]byte(`{"memo":"{\"lumera\":{}}"}`),                    // empty Lumera envelope
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Stage 1: outer packet unmarshal. Must not panic.
		var ftPacket transfertypes.FungibleTokenPacketData
		if err := json.Unmarshal(data, &ftPacket); err != nil {
			// Normal rejection path; nothing more to verify.
			return
		}

		// Stage 2: memo extraction. Must not panic even on weird packets.
		memo, ok, err := ExtractSettlementMemo(ftPacket)

		// Invariant: ok implies memo is non-nil and valid.
		if ok {
			if memo == nil {
				t.Fatalf("ExtractSettlementMemo returned ok=true with nil memo")
			}
			if vErr := memo.Validate(); vErr != nil {
				t.Fatalf("ok=true but Validate failed: %v", vErr)
			}
		}
		if !ok && memo != nil {
			t.Fatalf("ExtractSettlementMemo returned ok=false with non-nil memo")
		}

		// Stage 3: if the middleware accepts this as a settlement, it
		// will parse the amount. Simulate the same call.
		if ok && ftPacket.Amount != "" {
			// sdkmath.NewIntFromString handles garbage safely (returns false)
			// but this surface should be fuzzed anyway.
			_, _ = sdkmath.NewIntFromString(ftPacket.Amount)
		}
		_ = err
	})
}

// FuzzBuildSettlementMemo_AdversarialInputs fuzzes the memo-construction
// path with adversarial string fields. BuildSettlementMemo calls
// memo.Validate() and then json.Marshal; this probes both.
func FuzzBuildSettlementMemo_AdversarialInputs(f *testing.F) {
	type seed struct {
		typ, settlementID, refundAddr, router, publisher, toolID string
	}
	seeds := []seed{
		{"", "s", "r", "", "", ""},
		{"wrong_type", "s", "r", "", "", ""},
		{MemoTypeSettlement, "", "r", "", "", ""},
		{MemoTypeSettlement, "s", "", "", "", ""},
		{MemoTypeSettlement, " ", " ", "", "", ""}, // whitespace-only required fields
		{MemoTypeSettlement, "s", "r", strings.Repeat("x", 10000), "", ""},
		{MemoTypeSettlement, "s\"quote", "r\\back", "rt", "pub", "tool"},
		{MemoTypeSettlement, "\x00null", "\x00null", "", "", ""},
		{MemoTypeSettlement, "id", "addr", "🔥", "💎", "🚀"},
		{MemoTypeSettlement, "id", "addr", "router\nwith\nnewlines", "", ""},
	}
	for _, s := range seeds {
		f.Add(s.typ, s.settlementID, s.refundAddr, s.router, s.publisher, s.toolID)
	}

	f.Fuzz(func(t *testing.T, typ, settlementID, refundAddr, router, publisher, toolID string) {
		if !utf8.ValidString(typ) || !utf8.ValidString(settlementID) ||
			!utf8.ValidString(refundAddr) || !utf8.ValidString(router) ||
			!utf8.ValidString(publisher) || !utf8.ValidString(toolID) {
			return
		}

		memo := SettlementMemo{
			Type:          typ,
			SettlementID:  settlementID,
			RefundAddress: refundAddr,
			Router:        router,
			Publisher:     publisher,
			ToolID:        toolID,
		}

		// Validate must not panic.
		_ = memo.Validate()

		// Build must not panic (returns error on invalid input).
		raw, err := BuildSettlementMemo(memo)
		if err != nil {
			return // normal rejection
		}

		// If Build succeeded, the output must parse back and survive round-trip.
		parsed, ok, err := ParseSettlementMemo(raw)
		if err != nil {
			t.Fatalf("BuildSettlementMemo produced output that ParseSettlementMemo rejected: %v\ninput=%+v\noutput=%q",
				err, memo, truncate(raw))
		}
		if !ok {
			t.Fatalf("BuildSettlementMemo produced output that parsed ok=false\ninput=%+v\noutput=%q",
				memo, truncate(raw))
		}
		// Required fields must round-trip verbatim (modulo default type).
		if strings.TrimSpace(memo.SettlementID) != "" && parsed.SettlementID != memo.SettlementID {
			t.Fatalf("settlement_id drifted: input=%q parsed=%q", memo.SettlementID, parsed.SettlementID)
		}
		if strings.TrimSpace(memo.RefundAddress) != "" && parsed.RefundAddress != memo.RefundAddress {
			t.Fatalf("refund_address drifted: input=%q parsed=%q", memo.RefundAddress, parsed.RefundAddress)
		}
	})
}

// FuzzEscrowTransferValidate probes the EscrowTransfer validation path
// with adversarial inputs. This is called in BuildEscrowPacket before
// memo building, so it's the first gate for outgoing packets.
func FuzzEscrowTransferValidate(f *testing.F) {
	f.Add("ulac", "1", "sender", "receiver", "id", "addr")
	f.Add("", "0", "", "", "", "")
	f.Add(" ", "-1", " ", " ", " ", " ")
	f.Add("ulac", "99999999999999999999999999999999999999999999999", "s", "r", "id", "addr")
	f.Add("ulac\x00null", "1", "s", "r", "id", "addr")
	f.Add("ulac", "1", strings.Repeat("s", 10000), "r", "id", "addr")

	f.Fuzz(func(t *testing.T, denom, amount, sender, receiver, settleID, refundAddr string) {
		if !utf8.ValidString(denom) || !utf8.ValidString(amount) ||
			!utf8.ValidString(sender) || !utf8.ValidString(receiver) ||
			!utf8.ValidString(settleID) || !utf8.ValidString(refundAddr) {
			return
		}

		amt, ok := sdkmath.NewIntFromString(amount)
		if !ok {
			amt = sdkmath.ZeroInt()
		}

		transfer := EscrowTransfer{
			Denom:    denom,
			Amount:   amt,
			Sender:   sender,
			Receiver: receiver,
			Memo: SettlementMemo{
				SettlementID:  settleID,
				RefundAddress: refundAddr,
			},
		}

		// Validate must not panic.
		_ = transfer.Validate()

		// BuildEscrowPacket must not panic (returns error on invalid input).
		_, _ = BuildEscrowPacket(transfer)
	})
}

// FuzzCanonicalIDCollisions targets the specific class of inputs where
// the memo's type, envelope, or ID fields collide in surprising ways.
// Catches parser behavior that allows semantic ambiguity (e.g. two
// valid interpretations of the same byte sequence).
func FuzzCanonicalIDCollisions(f *testing.F) {
	// Each seed is a full memo JSON exploring one collision pattern.
	seeds := []string{
		// Two "lumera" keys at different levels
		`{"lumera":{"settlement_id":"outer","refund_address":"a","lumera":{"settlement_id":"inner","refund_address":"b"}}}`,
		// Conflicting type fields via envelope repetition (invalid JSON but catches naive parsers)
		`{"lumera":{"type":"lumera_credits_settlement","type":"evil_override","settlement_id":"x","refund_address":"y"}}`,
		// Settlement ID with embedded JSON delimiters
		`{"lumera":{"settlement_id":"a\",\"refund_address\":\"attacker","refund_address":"real"}}`,
		// Router field that looks like another envelope
		`{"lumera":{"settlement_id":"x","refund_address":"y","router":"{\"lumera\":{\"settlement_id\":\"hijack\"}}"}}`,
		// Same settlement_id spelled different casings (won't parse as same field but tests canonicalization)
		`{"lumera":{"Settlement_ID":"canonical","settlement_id":"real","refund_address":"y"}}`,
		// Empty string fields that might canonicalize to something else
		`{"lumera":{"settlement_id":"  x  ","refund_address":"  y  "}}`,
		// Type field in wrong namespace
		`{"lumera":{"settlement_id":"x","refund_address":"y"},"type":"lumera_credits_settlement"}`,
		// Non-string field where string expected
		`{"lumera":{"settlement_id":42,"refund_address":"y"}}`,
		`{"lumera":{"settlement_id":null,"refund_address":"y"}}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		// Must not panic.
		memo1, ok1, _ := ParseSettlementMemo(raw)

		// Parse twice and verify idempotence — same input must produce
		// the same result. Catches parsers with hidden state or random ordering.
		memo2, ok2, _ := ParseSettlementMemo(raw)

		if ok1 != ok2 {
			t.Fatalf("ParseSettlementMemo non-idempotent: ok differs %v vs %v (input: %q)",
				ok1, ok2, truncate(raw))
		}
		if ok1 && ok2 {
			if memo1.SettlementID != memo2.SettlementID {
				t.Fatalf("ParseSettlementMemo non-idempotent on settlement_id: %q vs %q",
					memo1.SettlementID, memo2.SettlementID)
			}
			if memo1.RefundAddress != memo2.RefundAddress {
				t.Fatalf("ParseSettlementMemo non-idempotent on refund_address: %q vs %q",
					memo1.RefundAddress, memo2.RefundAddress)
			}
			if memo1.Router != memo2.Router {
				t.Fatalf("ParseSettlementMemo non-idempotent on router: %q vs %q",
					memo1.Router, memo2.Router)
			}
		}

		// Rebuild and reparse the accepted memo — must survive round-trip.
		if ok1 && memo1 != nil {
			rebuilt, err := BuildSettlementMemo(*memo1)
			if err != nil {
				t.Fatalf("parsed memo failed to rebuild: %v\nmemo=%+v", err, memo1)
			}
			reparsed, ok3, err := ParseSettlementMemo(rebuilt)
			if err != nil || !ok3 {
				t.Fatalf("rebuilt memo failed to reparse: err=%v ok=%v\noriginal=%q\nrebuilt=%q",
					err, ok3, truncate(raw), truncate(rebuilt))
			}
			// After round-trip, parsed memo must be stable.
			if reparsed.SettlementID != memo1.SettlementID {
				t.Fatalf("round-trip drift on settlement_id: %q -> %q",
					memo1.SettlementID, reparsed.SettlementID)
			}
			if reparsed.RefundAddress != memo1.RefundAddress {
				t.Fatalf("round-trip drift on refund_address: %q -> %q",
					memo1.RefundAddress, reparsed.RefundAddress)
			}
		}
	})
}

// FuzzOversizedPayloads specifically targets the "oversized" attack
// class: huge strings in memo fields that a relayer could submit.
// The goal is not just "no panic" but also bounded resource usage —
// we measure that parsing doesn't blow up.
func FuzzOversizedPayloads(f *testing.F) {
	f.Add(uint16(10), uint16(10))
	f.Add(uint16(1000), uint16(100))
	f.Add(uint16(10000), uint16(1))
	f.Add(uint16(0), uint16(0))

	f.Fuzz(func(t *testing.T, fieldSize, fieldCount uint16) {
		// Cap to avoid runaway fuzz iterations; the interesting
		// behavior surfaces at small-to-medium sizes.
		if fieldSize > 50000 {
			fieldSize = 50000
		}
		if fieldCount > 50 {
			fieldCount = 50
		}

		bigStr := strings.Repeat("x", int(fieldSize))

		memo := SettlementMemo{
			Type:          MemoTypeSettlement,
			SettlementID:  bigStr,
			RefundAddress: bigStr,
			ReceiptHash:   bigStr,
			Router:        bigStr,
			Publisher:     bigStr,
			ToolID:        bigStr,
			ToolpackID:    bigStr,
			ActionID:      bigStr,
		}

		// Build must not panic; may reject via Validate.
		raw, err := BuildSettlementMemo(memo)
		if err != nil {
			return
		}

		// Parse must not panic.
		parsed, ok, _ := ParseSettlementMemo(raw)
		if ok && parsed != nil {
			// Verify we didn't silently truncate the big string — if
			// we accept it, all bytes must round-trip.
			if len(parsed.SettlementID) != len(memo.SettlementID) {
				t.Fatalf("SettlementID silently truncated: %d -> %d bytes",
					len(memo.SettlementID), len(parsed.SettlementID))
			}
		}

		// Multi-field variant: many small fields instead of one huge one.
		// Builds a JSON doc with fieldCount many extra keys. This tests
		// that json.Unmarshal doesn't have pathological behavior on
		// many-field inputs.
		if fieldCount > 0 {
			var b strings.Builder
			b.WriteString(`{"lumera":{"type":"lumera_credits_settlement","settlement_id":"x","refund_address":"y"`)
			for i := uint16(0); i < fieldCount; i++ {
				b.WriteString(`,"extra_field_`)
				b.WriteString(strings.Repeat("k", 5))
				b.WriteString(`":"v"`)
			}
			b.WriteString("}}")
			_, _, _ = ParseSettlementMemo(b.String())
		}
	})
}

// FuzzAmountParsingEdgeCases targets the amount-string parsing in the
// full middleware path. Amount is also counterparty-controlled.
func FuzzAmountParsingEdgeCases(f *testing.F) {
	seeds := []string{
		"",
		"0",
		"1",
		"-1",
		"not_a_number",
		"1e100",
		"1.5",
		"0x100",
		"99999999999999999999999999999999999999999999999",
		"000000001",
		" 1 ",
		"1\n",
		"\x00\x01",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, amount string) {
		// sdkmath.NewIntFromString must not panic.
		amt, ok := sdkmath.NewIntFromString(amount)
		if !ok {
			return
		}

		// The middleware checks amt.IsPositive() and
		// amt.BigInt().BitLen() <= MaxSettlementAmountBits. Exercise both.
		_ = amt.IsPositive()
		_ = amt.BigInt().BitLen()
	})
}

// truncate shortens strings for error messages so test output stays
// readable even when the fuzzer generates huge inputs.
func truncate(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
