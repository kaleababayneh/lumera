package keeper

import (
	"bytes"
	"testing"
	"time"
	"unicode/utf8"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// validNonNegativeAmount reports whether amountStr parses to a non-negative
// sdk.Int. After the gogoproto migration Lock.Amount is a value sdk.Coin whose
// Amount is a math.Int, so (unlike the old protobuf-go raw-string Coin) the
// fuzzer cannot feed arbitrary/empty/negative amount strings — those inputs
// can't form a valid coin and are skipped rather than panicking in protoCoin.
func validNonNegativeAmount(amountStr string) bool {
	v, ok := sdkmath.NewIntFromString(amountStr)
	return ok && !v.IsNegative()
}

// allValidUTF8 returns true iff every input string is valid
// UTF-8. Protobuf string fields REQUIRE valid UTF-8 per the
// proto3 spec; proto.Marshal fails otherwise. Since we're
// testing round-trip of VALID Lock values (not proto's
// rejection of invalid inputs), skip non-UTF-8 combinations.
func allValidUTF8(ss ...string) bool {
	for _, s := range ss {
		if !utf8.ValidString(s) {
			return false
		}
	}
	return true
}

// This file applies the testing-fuzzing skill (Archetype 2 —
// Round-Trip Conformance) to x/credits Lock SERIALIZATION.
// The invariant: a Lock written to state survives a reboot
// EXACTLY as written — every field byte-identical post-read.
//
// This is consensus-critical:
//   - Validators restart/state-sync and must re-derive
//     identical state from the same underlying bytes
//   - Archival nodes replay from genesis; Lock records must
//     round-trip losslessly across many years of blocks
//   - State-export/import (for upgrades) must produce a
//     byte-identical Lock on both sides of the migration
//
// Serialization path: Locks collection uses
// codec.CollValueV2[types.Lock](), which proto.Marshals the
// Lock to bytes for storage and proto.Unmarshals on read.
//
// Five fuzz targets:
//
//   1. FuzzLock_ProtoRoundTripPreservesEquality —
//      Marshal → Unmarshal → proto.Equal with original
//   2. FuzzLock_ProtoMarshalIsDeterministic — same Lock
//      marshals to byte-identical output every time
//   3. FuzzLock_KeeperSaveLoadRoundTrip — through the REAL
//      keeper Save+Get path
//   4. FuzzLock_NilTimestampFieldsSurviveRoundTrip — the
//      nil-pointer fields (Amount, CreatedAt, ExpiresAt)
//      survive as nil post-round-trip
//   5. FuzzLock_LargeLastErrorStringSurvives — the
//      LastError field can hold arbitrary text; pins no
//      size-truncation or encoding loss

// --------------------------------------------------------------
// FUZZ 1: proto round-trip preserves proto.Equal
// --------------------------------------------------------------

// FuzzLock_ProtoRoundTripPreservesEquality is the canonical
// round-trip fuzz: random-but-valid Lock → Marshal → Unmarshal
// → proto.Equal returns true. Exercises every field with
// fuzz-generated values.
func FuzzLock_ProtoRoundTripPreservesEquality(f *testing.F) {
	// Seeds: representative Lock shapes.
	seeds := []struct {
		lockID, router, sessionID, toolID              string
		quoteID, policyVersion, intentHash, toolpackID string
		lastError                                      string
		amountStr                                      string
		createdAtSec, expiresAtSec                     int64
		status                                         int32
	}{
		// Typical.
		{"lock-1", "cosmos1router", "session-1", "tool-1",
			"quote-1", "v1", "intent-abc", "",
			"", "100", 1_700_000_000, 1_700_003_600, 1},
		// With toolpack + error.
		{"lock-2", "cosmos1r2", "s2", "t2", "q2", "v1", "h2",
			"pack-1", "network timeout", "50000", 1_700_000_000,
			1_700_003_600, 2},
		// Empty strings.
		{"", "", "", "", "", "", "", "", "", "0", 0, 0, 0},
		// Large text.
		{"lock-big", "cosmos1xxxx", "sess-looooooong",
			"tool-with-long-name", "quote-x", "v2.1.0",
			"abc123def456", "pack-big",
			"very long error message with special chars: < > & \" '",
			"999999999999999999", 1_893_456_000, 1_924_992_000, 3},
	}
	for _, s := range seeds {
		f.Add(s.lockID, s.router, s.sessionID, s.toolID,
			s.quoteID, s.policyVersion, s.intentHash, s.toolpackID,
			s.lastError, s.amountStr,
			s.createdAtSec, s.expiresAtSec, s.status)
	}

	f.Fuzz(func(t *testing.T,
		lockID, router, sessionID, toolID,
		quoteID, policyVersion, intentHash, toolpackID,
		lastError, amountStr string,
		createdAtSec, expiresAtSec int64,
		status int32,
	) {
		// Skip inputs that would fail basic validation to avoid
		// noise from unparseable amounts etc.
		if createdAtSec < 0 || expiresAtSec < 0 {
			return
		}
		if createdAtSec > 4_000_000_000 || expiresAtSec > 4_000_000_000 {
			// Stay in reasonable time range (below ~2096) so
			// timestamppb construction is valid.
			return
		}
		// Cap status to the valid enum range [0, 4].
		if status < 0 || status > 4 {
			return
		}
		// Skip invalid UTF-8 — proto3 string fields require it.
		if !allValidUTF8(lockID, router, sessionID, toolID,
			quoteID, policyVersion, intentHash, toolpackID,
			lastError, amountStr) {
			return
		}
		if !validNonNegativeAmount(amountStr) {
			return
		}

		// Construct the Lock. Skip nil Amount / Timestamp fields
		// here — those are tested in a dedicated target.
		lock := &types.Lock{
			LockId:        lockID,
			Router:        router,
			SessionId:     sessionID,
			ToolId:        toolID,
			QuoteId:       quoteID,
			PolicyVersion: policyVersion,
			IntentHash:    intentHash,
			Amount:        protoCoin("ulac", amountStr),
			CreatedAt:     time.Unix(createdAtSec, 0).UTC(),
			ExpiresAt:     time.Unix(expiresAtSec, 0).UTC(),
			Status:        types.LockStatus(status),
			LastError:     lastError,
			ToolpackId:    toolpackID,
		}

		bz, err := proto.Marshal(lock)
		require.NoError(t, err, "proto.Marshal failed")

		var decoded types.Lock
		require.NoError(t, proto.Unmarshal(bz, &decoded),
			"proto.Unmarshal failed")

		// Equality via re-marshaled bytes. gogoproto's proto.Equal is
		// unreliable on messages with customtype fields (Lock.Amount is a
		// math.Int, CreatedAt/ExpiresAt are time.Time) — it returns false
		// even for byte-identical round-trips — so compare the canonical
		// wire encoding instead, which is the property a reboot actually
		// relies on.
		bz2, err := proto.Marshal(&decoded)
		require.NoError(t, err, "proto.Marshal of decoded failed")
		if !bytes.Equal(bz, bz2) {
			t.Fatalf("round-trip wire bytes differ.\n"+
				"  original: %+v\n"+
				"  decoded:  %+v",
				lock, &decoded)
		}
	})
}

// --------------------------------------------------------------
// FUZZ 2: marshal determinism
// --------------------------------------------------------------

// FuzzLock_ProtoMarshalIsDeterministic pins that repeated
// proto.Marshal of the SAME Lock produces byte-identical output.
// This is CONSENSUS-CRITICAL — validators with different
// concurrent compactions produce identical bytes iff marshal
// is deterministic.
func FuzzLock_ProtoMarshalIsDeterministic(f *testing.F) {
	seeds := []struct {
		lockID, router string
		amountStr      string
	}{
		{"lock-a", "cosmos1a", "100"},
		{"lock-b", "cosmos1b", "0"},
		{"", "", ""},
		{"lock-c", "cosmos1c", "999999999999999999999999999"},
	}
	for _, s := range seeds {
		f.Add(s.lockID, s.router, s.amountStr)
	}

	f.Fuzz(func(t *testing.T, lockID, router, amountStr string) {
		if !allValidUTF8(lockID, router, amountStr) {
			return
		}
		if !validNonNegativeAmount(amountStr) {
			return
		}
		lock := &types.Lock{
			LockId: lockID,
			Router: router,
			Amount: protoCoin("ulac", amountStr),
			Status: types.LockStatus_LOCK_STATUS_ACTIVE,
		}

		// gogoproto's proto.Marshal is deterministic for a schema
		// with no map fields (Lock has none). Pin that repeated
		// marshals of the SAME Lock produce byte-identical output —
		// CONSENSUS-CRITICAL, since validators must compute identical
		// state-root bytes.
		bz1, err := proto.Marshal(lock)
		require.NoError(t, err)

		// 5 repetitions MUST produce byte-identical output.
		for i := 0; i < 5; i++ {
			bzN, err := proto.Marshal(lock)
			require.NoError(t, err)
			if !bytes.Equal(bz1, bzN) {
				t.Fatalf("marshal iteration %d diverged from first. "+
					"len1=%d lenN=%d. Non-determinism in Lock marshal "+
					"would fragment validator state-root computations.",
					i, len(bz1), len(bzN))
			}
		}
	})
}

// --------------------------------------------------------------
// FUZZ 3: keeper-level Save→Get round-trip
// --------------------------------------------------------------

// FuzzLock_KeeperSaveLoadRoundTrip exercises the REAL keeper
// path: SaveLock(ctx, L) → GetLock(ctx, L.LockId) returns a
// Lock proto.Equal to the input. This is the actual reboot-
// survival test: state.Locks.Set uses codec.CollValueV2 which
// wraps proto.Marshal/Unmarshal.
func FuzzLock_KeeperSaveLoadRoundTrip(f *testing.F) {
	seeds := []struct {
		lockID, router string
		amountStr      string
		createdSec     int64
	}{
		{"lock-kr-1", "cosmos1kr1", "100000", 1_700_000_000},
		{"lock-kr-2", "cosmos1kr2", "5000000", 1_893_456_000},
		{"lock-kr-3", "cosmos1kr3", "0", 1_700_000_000},
	}
	for _, s := range seeds {
		f.Add(s.lockID, s.router, s.amountStr, s.createdSec)
	}

	f.Fuzz(func(t *testing.T, lockID, router, amountStr string,
		createdSec int64) {
		// Skip pathological inputs.
		if lockID == "" {
			return // SaveLock requires non-empty ID for map key
		}
		if createdSec < 0 || createdSec > 4_000_000_000 {
			return
		}
		if !allValidUTF8(lockID, router, amountStr) {
			return
		}
		if !validNonNegativeAmount(amountStr) {
			return
		}

		ctx, keeper, _, _, _ := setupCreditsKeeper(t)

		lock := &types.Lock{
			LockId:    lockID,
			Router:    router,
			Amount:    protoCoin("ulac", amountStr),
			CreatedAt: time.Unix(createdSec, 0).UTC(),
			ExpiresAt: time.Unix(createdSec+3600, 0).UTC(),
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		}

		if err := keeper.SaveLock(ctx, lock); err != nil {
			// Some inputs fail SaveLock (e.g. empty ID). That's
			// expected; we just require no panic.
			return
		}

		retrieved, found := keeper.GetLock(ctx, lockID)
		require.True(t, found,
			"just-saved lock must be retrievable by same ID")
		require.NotNil(t, retrieved)

		// Compare canonical wire bytes rather than gogoproto's proto.Equal,
		// which is unreliable on customtype fields (math.Int / time.Time).
		bzSaved, err := proto.Marshal(lock)
		require.NoError(t, err)
		bzGot, err := proto.Marshal(retrieved)
		require.NoError(t, err)
		if !bytes.Equal(bzSaved, bzGot) {
			t.Fatalf("keeper round-trip not byte-equal.\n"+
				"  saved:     %+v\n"+
				"  retrieved: %+v",
				lock, retrieved)
		}
	})
}

// --------------------------------------------------------------
// FUZZ 4: nil timestamp/coin fields survive
// --------------------------------------------------------------

// FuzzLock_NilTimestampFieldsSurviveRoundTrip pins that when
// optional pointer fields (Amount, CreatedAt, ExpiresAt) are
// nil, the round-trip preserves them as nil. A proto-codec
// regression that replaced nil with a zero-valued default
// would change the observable state (e.g. CreatedAt epoch 0
// vs nil means very different things to callers).
func FuzzLock_NilTimestampFieldsSurviveRoundTrip(f *testing.F) {
	f.Skip("not ported: this fuzz pinned nil-pointer survival for Lock.Amount / " +
		"CreatedAt / ExpiresAt. After the gogoproto migration these fields are " +
		"non-nullable value types (sdk.Coin, time.Time) rather than pointers, so " +
		"\"nil vs zero-value default\" is no longer an observable distinction and " +
		"the premise of the test does not apply. Zero-value round-trip fidelity is " +
		"already covered by FuzzLock_KeeperSaveLoadRoundTrip.")
}

// --------------------------------------------------------------
// FUZZ 5: LastError arbitrary text
// --------------------------------------------------------------

// FuzzLock_LargeLastErrorStringSurvives pins that the LastError
// field survives arbitrary text content (including unicode,
// newlines, nulls) through the serialization round-trip. A
// refactor adding a max-length truncation or an encoding
// normalization would surface here.
func FuzzLock_LargeLastErrorStringSurvives(f *testing.F) {
	seeds := []string{
		"",
		"simple",
		"with\nnewlines\nand\ttabs",
		"unicode: \u2764 \u00e9 \u4e2d\u6587",
		"null\x00bytes\x00",
		"very long: " + string(make([]byte, 1000)),
		`"quoted" and 'apostrophes' and <angle brackets>`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, lastError string) {
		// Cap input size to avoid ballooning fuzz test time.
		if len(lastError) > 10_000 {
			return
		}
		if !utf8.ValidString(lastError) {
			return
		}

		lock := &types.Lock{
			LockId:    "err-test",
			Router:    "cosmos1rerr",
			Amount:    protoCoin("ulac", "100"),
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
			LastError: lastError,
		}

		bz, err := proto.Marshal(lock)
		require.NoError(t, err)

		var decoded types.Lock
		require.NoError(t, proto.Unmarshal(bz, &decoded))

		if decoded.LastError != lastError {
			t.Fatalf("LastError round-trip lost data.\n"+
				"  input  (%d bytes): %q\n"+
				"  output (%d bytes): %q",
				len(lastError), lastError,
				len(decoded.LastError), decoded.LastError)
		}
	})
}
