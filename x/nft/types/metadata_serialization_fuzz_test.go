
package types

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Fuzz harness for x/nft metadata serialization edge cases.
//
// Attack surface
// --------------
// ToolpackNFT, ToolpackHistory, and ToolReference cross the wire at
// several points:
//   - MsgMintToolpack / MsgUpdateToolpack — bidder-controlled fields
//   - Genesis import — operator-controlled at chain launch
//   - IBC discovery responses — counterparty-chain consumers
//   - Query gRPC/REST responses — external indexers and dashboards
//   - State storage — proto.Marshal at every keeper write
//
// A panic during proto decode crashes the block executor; a memory
// blow-up from oversized payloads is a consensus DoS; round-trip
// non-stability across Marshal/Unmarshal causes validators to compute
// different state hashes for the same logical NFT.
//
// Targets fuzzed (8 harnesses):
//   1. ToolpackNFT proto decode (random bytes never panic)
//   2. ToolReference proto decode (leaf type)
//   3. ToolpackHistory proto decode (history snapshot type)
//   4. ToolpackNFT round-trip Marshal/Unmarshal byte-stability
//   5. ToolpackNFT oversized repeated Tools[] field
//   6. ToolpackNFT adversarial string fields (NUL, Unicode, huge)
//   7. ToolpackNFT timestamp edge cases (zero, far-future, large)
//   8. MsgRecordRoyaltyPayout coin amount edge cases
//
// Correctness criterion: NEVER PANIC. Decode may error or return
// partially-populated structs; that's fine. Round-trip after a
// successful decode must reach a fixed point (Marshal twice produces
// identical bytes).

// decodeToolpackMustNotPanic wraps proto.Unmarshal in a panic guard.
func decodeToolpackMustNotPanic(t *testing.T, data []byte, msg proto.Message) error {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("proto.Unmarshal panicked on %T: %v (len=%d)", msg, r, len(data))
		}
	}()
	return proto.Unmarshal(data, msg)
}

// FuzzToolpackNFT_ProtoDecode feeds arbitrary bytes to ToolpackNFT's
// Unmarshal. The full metadata struct is the largest attack surface;
// every reach-into-state path through the NFT keeper goes through it.
func FuzzToolpackNFT_ProtoDecode(f *testing.F) {
	seeds := [][]byte{
		{},
		{0x00},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, // varint overflow
		{0x0a, 0x05, 'p', 'a', 'c', 'k', '1'},                       // valid: id="pack1"
		{0x10, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, // version varint overflow
		{0x22, 0xff, 0xff, 0xff, 0x7f},                              // tools field with huge length
		{0x42, 0x00, 0x42, 0x00, 0x42, 0x00},                        // repeated nested timestamps
		{0x32, 0x00},                                                 // policy_version with zero length
		{0x38, 0xff, 0xff, 0xff, 0x7f},                              // royalty_bps overflow
		make([]byte, 1024),                                           // zero-filled large input
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var msg ToolpackNFT
		if err := decodeToolpackMustNotPanic(t, data, &msg); err != nil {
			return
		}

		// Successful decode: verify every Tools entry is non-nil
		// addressable (proto unmarshal should never produce a nil-but-
		// non-empty repeated message).
		for i, ref := range msg.Tools {
			if ref == nil {
				t.Fatalf("ToolpackNFT.Tools[%d] decoded as nil — proto invariant broken", i)
			}
		}
	})
}

// FuzzToolReference_ProtoDecode targets the leaf metadata type.
// Trivially simple struct (2 fields) but must still never panic on
// arbitrary input.
func FuzzToolReference_ProtoDecode(f *testing.F) {
	seeds := [][]byte{
		{},
		{0x0a, 0x04, 't', 'o', 'o', 'l'},
		{0x12, 0x02, 'v', '1'},
		{0x0a, 0xff, 0xff, 0xff, 0x7f}, // huge tool_id length claim
		{0x12, 0xff, 0xff, 0xff, 0x7f}, // huge version length claim
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var msg ToolReference
		_ = decodeToolpackMustNotPanic(t, data, &msg)
	})
}

// FuzzToolpackHistory_ProtoDecode targets the snapshot type. Almost
// identical layout to ToolpackNFT but stored at every version bump,
// so high-volume on the wire.
func FuzzToolpackHistory_ProtoDecode(f *testing.F) {
	seeds := [][]byte{
		{},
		{0x0a, 0x05, 'p', 'a', 'c', 'k', '1'},
		{0x10, 0x05}, // version=5
		// Repeated tools field
		{0x1a, 0x06, 0x0a, 0x04, 't', 'o', 'o', 'l'},
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var msg ToolpackHistory
		_ = decodeToolpackMustNotPanic(t, data, &msg)
	})
}

// FuzzToolpackNFT_RoundTripStability proves that Marshal(Unmarshal(x))
// reaches a fixed point. Validators replaying the same block must
// produce byte-identical state — a non-stable round-trip is a
// consensus bug.
func FuzzToolpackNFT_RoundTripStability(f *testing.F) {
	f.Add([]byte{}, "pack-1", "lumera1curator", uint64(1), uint32(100))
	f.Add(marshalToolpack(t_helper(&ToolpackNFT{
		Id: "pack-rt", Version: 1, Curator: "c",
		PolicyVersion: "v1",
	})), "extra", "extra", uint64(0), uint32(0))

	f.Fuzz(func(t *testing.T, data []byte, id, curator string, version uint64, royaltyBps uint32) {
		// Stage 1: decode arbitrary bytes (may fail).
		var msg ToolpackNFT
		if err := decodeToolpackMustNotPanic(t, data, &msg); err != nil {
			return
		}

		// Stage 2: marshal back. Must not panic.
		var raw1 []byte
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("proto.Marshal panicked on decoded ToolpackNFT: %v", r)
				}
			}()
			var err error
			raw1, err = proto.Marshal(&msg)
			if err != nil {
				t.Fatalf("proto.Marshal failed: %v", err)
			}
		}()

		// Stage 3: re-decode the re-encoded bytes. Must succeed.
		var msg2 ToolpackNFT
		if err := proto.Unmarshal(raw1, &msg2); err != nil {
			t.Fatalf("re-decode failed for valid round-trip output: %v", err)
		}

		// Stage 4: marshal the second decode. Must equal raw1
		// (fixed point invariant).
		raw2, err := proto.Marshal(&msg2)
		if err != nil {
			t.Fatalf("second marshal failed: %v", err)
		}
		if !bytes.Equal(raw1, raw2) {
			t.Fatalf("round-trip not byte-stable: first=%d bytes second=%d bytes "+
				"(diff at index %d)", len(raw1), len(raw2), firstDiff(raw1, raw2))
		}

		// Use the fuzz inputs to construct an additional fresh NFT and
		// round-trip that too — covers the "valid construction" path.
		if !utf8.ValidString(id) || !utf8.ValidString(curator) {
			return
		}
		if len(id) > 1024 || len(curator) > 1024 {
			return
		}
		fresh := &ToolpackNFT{
			Id:         id,
			Curator:    curator,
			Version:    version,
			RoyaltyBps: royaltyBps,
		}
		freshRaw, err := proto.Marshal(fresh)
		if err != nil {
			return
		}
		var freshDecoded ToolpackNFT
		if err := proto.Unmarshal(freshRaw, &freshDecoded); err != nil {
			t.Fatalf("fresh NFT round-trip decode failed: %v", err)
		}
		freshRaw2, _ := proto.Marshal(&freshDecoded)
		if !bytes.Equal(freshRaw, freshRaw2) {
			t.Fatalf("fresh NFT round-trip not stable")
		}
	})
}

// FuzzToolpackNFT_OversizedRepeatedFields probes the Tools[] repeated
// field. A many-element Tools array is an attack vector — a malicious
// MsgMintToolpack with thousands of tools could blow up state storage.
func FuzzToolpackNFT_OversizedRepeatedFields(f *testing.F) {
	f.Add(uint16(10), uint16(10))
	f.Add(uint16(100), uint16(50))
	f.Add(uint16(1000), uint16(5))
	f.Add(uint16(0), uint16(0))

	f.Fuzz(func(t *testing.T, toolCount, idLen uint16) {
		// Cap to keep fuzz iterations fast.
		if toolCount > 2000 {
			toolCount = 2000
		}
		if idLen > 256 {
			idLen = 256
		}

		tools := make([]*ToolReference, toolCount)
		for i := range tools {
			tools[i] = &ToolReference{
				ToolId:  strings.Repeat("a", int(idLen)),
				Version: "1",
			}
		}

		nft := &ToolpackNFT{
			Id:         "pack-oversized",
			Curator:    "lumera1c",
			Version:    1,
			Tools:      tools,
			RoyaltyBps: 100,
			Active:     true,
		}

		var raw []byte
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Marshal panicked on Tools[%d] each-id-len=%d: %v",
						toolCount, idLen, r)
				}
			}()
			var err error
			raw, err = proto.Marshal(nft)
			if err != nil {
				t.Fatalf("Marshal failed on Tools[%d]: %v", toolCount, err)
			}
		}()

		// Decode must succeed.
		var decoded ToolpackNFT
		if err := decodeToolpackMustNotPanic(t, raw, &decoded); err != nil {
			t.Fatalf("Unmarshal failed for Tools[%d]: %v", toolCount, err)
		}

		// All tool entries must round-trip — no silent truncation.
		if len(decoded.Tools) != int(toolCount) {
			t.Fatalf("Tools slice silently truncated: input=%d, decoded=%d",
				toolCount, len(decoded.Tools))
		}
	})
}

// FuzzToolpackNFT_AdversarialStrings probes the string fields with
// adversarial content: NULs, Unicode, embedded whitespace, very long.
// proto3 spec requires valid UTF-8; we filter non-UTF-8 to focus on
// the in-spec adversarial space.
func FuzzToolpackNFT_AdversarialStrings(f *testing.F) {
	f.Add("pack-1", "lumera1curator", "v1.0")
	f.Add("", "", "")
	f.Add("\x00null", "\nlinefeed", "\rcr")
	f.Add("🔥pack🔥", "lumera1🚀", "v1.0🎉")
	f.Add(strings.Repeat("a", 10000), strings.Repeat("b", 5000), strings.Repeat("c", 1000))

	f.Fuzz(func(t *testing.T, id, curator, policyVersion string) {
		if !utf8.ValidString(id) || !utf8.ValidString(curator) || !utf8.ValidString(policyVersion) {
			return
		}

		nft := &ToolpackNFT{
			Id:            id,
			Curator:       curator,
			Version:       1,
			PolicyVersion: policyVersion,
			RoyaltyBps:    100,
			Active:        true,
		}

		// Marshal must not panic.
		var raw []byte
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Marshal panicked on adversarial strings: %v", r)
				}
			}()
			var err error
			raw, err = proto.Marshal(nft)
			if err != nil {
				return
			}
		}()
		if raw == nil {
			return
		}

		// Round-trip preserves all 3 string fields verbatim.
		var decoded ToolpackNFT
		if err := proto.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if decoded.Id != id {
			t.Fatalf("Id drifted: input=%q decoded=%q", id, decoded.Id)
		}
		if decoded.Curator != curator {
			t.Fatalf("Curator drifted: input=%q decoded=%q", curator, decoded.Curator)
		}
		if decoded.PolicyVersion != policyVersion {
			t.Fatalf("PolicyVersion drifted: input=%q decoded=%q",
				policyVersion, decoded.PolicyVersion)
		}
	})
}

// FuzzToolpackNFT_TimestampEdgeCases probes the 3 timestamp fields
// (CreatedAt, UpdatedAt, ExpiresAt) with extreme values: zero,
// negative seconds, far-future, and large nanos.
func FuzzToolpackNFT_TimestampEdgeCases(f *testing.F) {
	f.Add(int64(0), int64(0), int64(0))
	f.Add(int64(1_700_000_000), int64(1_700_000_001), int64(1_800_000_000))
	f.Add(int64(-1), int64(0), int64(1))
	f.Add(int64(253_402_300_799), int64(0), int64(0)) // year 9999

	f.Fuzz(func(t *testing.T, createdSec, updatedSec, expiresSec int64) {
		// Skip values outside protobuf timestamp range.
		const minSec = -62_135_596_800
		const maxSec = 253_402_300_799
		clamp := func(v int64) int64 {
			if v < minSec {
				return minSec
			}
			if v > maxSec {
				return maxSec
			}
			return v
		}
		createdSec = clamp(createdSec)
		updatedSec = clamp(updatedSec)
		expiresSec = clamp(expiresSec)

		nft := &ToolpackNFT{
			Id:        "pack-ts",
			Curator:   "lumera1c",
			Version:   1,
			CreatedAt: timestamppb.New(time.Unix(createdSec, 0).UTC()),
			UpdatedAt: timestamppb.New(time.Unix(updatedSec, 0).UTC()),
			ExpiresAt: timestamppb.New(time.Unix(expiresSec, 0).UTC()),
		}

		var raw []byte
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Marshal panicked on timestamps "+
						"(c=%d u=%d e=%d): %v",
						createdSec, updatedSec, expiresSec, r)
				}
			}()
			var err error
			raw, err = proto.Marshal(nft)
			if err != nil {
				return
			}
		}()
		if raw == nil {
			return
		}

		var decoded ToolpackNFT
		if err := proto.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		// Each timestamp must round-trip seconds exactly.
		if decoded.CreatedAt == nil || decoded.CreatedAt.Seconds != createdSec {
			t.Fatalf("CreatedAt drifted: in=%d out=%v", createdSec, decoded.CreatedAt)
		}
		if decoded.UpdatedAt == nil || decoded.UpdatedAt.Seconds != updatedSec {
			t.Fatalf("UpdatedAt drifted: in=%d out=%v", updatedSec, decoded.UpdatedAt)
		}
		if decoded.ExpiresAt == nil || decoded.ExpiresAt.Seconds != expiresSec {
			t.Fatalf("ExpiresAt drifted: in=%d out=%v", expiresSec, decoded.ExpiresAt)
		}
	})
}

// FuzzMsgRecordRoyaltyPayout_AmountEdgeCases probes the nested
// v1beta1.Coin field on the royalty-payout message — denom + amount
// strings get parsed through the keeper at runtime.
func FuzzMsgRecordRoyaltyPayout_AmountEdgeCases(f *testing.F) {
	f.Add("ulumera", "100")
	f.Add("", "")
	f.Add("ulumera", "-1")
	f.Add("ulumera", "0")
	f.Add("ULUMERA", "100") // case attack
	f.Add("u/lumera", "100")
	f.Add("ulumera\x00null", "100")
	f.Add("ulumera", "99999999999999999999999999999999999999999999999")
	f.Add(strings.Repeat("a", 200), "1")

	f.Fuzz(func(t *testing.T, denom, amount string) {
		if !utf8.ValidString(denom) || !utf8.ValidString(amount) {
			return
		}
		if len(denom) > 1000 || len(amount) > 1000 {
			return
		}

		msg := &MsgRecordRoyaltyPayout{
			Authority:  "lumera1auth",
			ToolpackId: "pack-royalty",
			Amount: &v1beta1.Coin{
				Denom:  denom,
				Amount: amount,
			},
		}

		// Marshal must not panic on any input combination.
		var raw []byte
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Marshal panicked on coin{denom=%q,amount=%q}: %v",
						denom, amount, r)
				}
			}()
			var err error
			raw, err = proto.Marshal(msg)
			if err != nil {
				return
			}
		}()
		if raw == nil {
			return
		}

		// Round-trip preserves coin fields.
		var decoded MsgRecordRoyaltyPayout
		if err := proto.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if decoded.Amount == nil {
			t.Fatalf("Amount round-tripped to nil — coin field corruption")
		}
		if decoded.Amount.Denom != denom {
			t.Fatalf("Denom drifted: input=%q decoded=%q", denom, decoded.Amount.Denom)
		}
		if decoded.Amount.Amount != amount {
			t.Fatalf("Amount drifted: input=%q decoded=%q", amount, decoded.Amount.Amount)
		}

		// ValidateBasic must not panic on the decoded message either.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ValidateBasic panicked on decoded msg: %v", r)
				}
			}()
			_ = decoded.ValidateBasic()
		}()
	})
}

// firstDiff returns the index of the first differing byte between a
// and b, or len(a) if they are equal up to that length.
func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// marshalToolpack is a tiny helper for fuzz seeds — silently returns
// nil bytes on marshal failure (acceptable for seed corpus where we
// just want a baseline).
func marshalToolpack(msg proto.Message) []byte {
	if msg == nil {
		return nil
	}
	raw, err := proto.Marshal(msg)
	if err != nil {
		return nil
	}
	return raw
}

// t_helper passes through a value while marking the call site as
// "test fixture construction" — keeps the fuzz seed lines readable.
func t_helper(v *ToolpackNFT) *ToolpackNFT { return v }
