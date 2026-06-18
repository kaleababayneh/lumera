
package keeper

import (
	"bytes"
	"testing"
	"time"
	"unicode/utf8"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

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
		lockID, router, sessionID, toolID                    string
		quoteID, policyVersion, intentHash, toolpackID       string
		lastError                                            string
		amountStr                                            string
		createdAtSec, expiresAtSec                           int64
		status                                               int32
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
			Amount:        &v1beta1.Coin{Denom: "ulac", Amount: amountStr},
			CreatedAt:     timestamppb.New(time.Unix(createdAtSec, 0).UTC()),
			ExpiresAt:     timestamppb.New(time.Unix(expiresAtSec, 0).UTC()),
			Status:        types.LockStatus(status),
			LastError:     lastError,
			ToolpackId:    toolpackID,
		}

		bz, err := proto.Marshal(lock)
		require.NoError(t, err, "proto.Marshal failed")

		var decoded types.Lock
		require.NoError(t, proto.Unmarshal(bz, &decoded),
			"proto.Unmarshal failed")

		if !proto.Equal(lock, &decoded) {
			t.Fatalf("round-trip proto.Equal failed.\n"+
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
		lock := &types.Lock{
			LockId: lockID,
			Router: router,
			Amount: &v1beta1.Coin{Denom: "ulac", Amount: amountStr},
			Status: types.LockStatus_LOCK_STATUS_ACTIVE,
		}

		// Use DETERMINISTIC marshal opts — proto.Marshal's
		// default is NOT deterministic (map iteration order) but
		// Lock has no map fields, so plain Marshal should be
		// stable. Pin this.
		opts := proto.MarshalOptions{Deterministic: true}

		bz1, err := opts.Marshal(lock)
		require.NoError(t, err)

		// 5 repetitions MUST produce byte-identical output.
		for i := 0; i < 5; i++ {
			bzN, err := opts.Marshal(lock)
			require.NoError(t, err)
			if !bytes.Equal(bz1, bzN) {
				t.Fatalf("marshal iteration %d diverged from first. "+
					"len1=%d lenN=%d. Non-determinism in Lock marshal "+
					"would fragment validator state-root computations.",
					i, len(bz1), len(bzN))
			}
		}

		// Also pin that NON-deterministic option produces the
		// same output since there are no map fields.
		defaultOpts := proto.MarshalOptions{}
		bzDefault, err := defaultOpts.Marshal(lock)
		require.NoError(t, err)
		// Note: deterministic and default may produce different
		// outputs in general, but for a schema with no map
		// fields they should match. Pin this as a sanity check.
		// If this assertion ever fails, it's a signal that the
		// schema acquired a map field that needs deterministic
		// encoding in storage paths.
		if !bytes.Equal(bz1, bzDefault) {
			t.Fatalf("deterministic vs default marshal diverged — "+
				"schema may have acquired a map field requiring "+
				"deterministic marshaling in storage paths.\n"+
				"  deterministic: %x\n"+
				"  default:       %x", bz1, bzDefault)
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
		lockID, router    string
		amountStr         string
		createdSec        int64
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

		ctx, keeper, _, _, _ := setupCreditsKeeper(t)

		lock := &types.Lock{
			LockId: lockID,
			Router: router,
			Amount: &v1beta1.Coin{Denom: "ulac", Amount: amountStr},
			CreatedAt: timestamppb.New(time.Unix(createdSec, 0).UTC()),
			ExpiresAt: timestamppb.New(time.Unix(createdSec+3600, 0).UTC()),
			Status: types.LockStatus_LOCK_STATUS_ACTIVE,
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

		if !proto.Equal(lock, retrieved) {
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
	// Seeds: various combinations of nil vs non-nil pointer fields.
	f.Add(true, true, true)    // all nil
	f.Add(false, true, true)   // Amount non-nil, rest nil
	f.Add(true, false, true)   // CreatedAt non-nil, rest nil
	f.Add(true, true, false)   // ExpiresAt non-nil, rest nil
	f.Add(false, false, false) // none nil

	f.Fuzz(func(t *testing.T, amountNil, createdNil, expiresNil bool) {
		lock := &types.Lock{
			LockId: "nil-test",
			Router: "cosmos1rnil",
			Status: types.LockStatus_LOCK_STATUS_ACTIVE,
		}
		if !amountNil {
			lock.Amount = &v1beta1.Coin{Denom: "ulac", Amount: "100"}
		}
		if !createdNil {
			lock.CreatedAt = timestamppb.New(time.Unix(1_700_000_000, 0).UTC())
		}
		if !expiresNil {
			lock.ExpiresAt = timestamppb.New(time.Unix(1_700_003_600, 0).UTC())
		}

		bz, err := proto.Marshal(lock)
		require.NoError(t, err)

		var decoded types.Lock
		require.NoError(t, proto.Unmarshal(bz, &decoded))

		// Pin nil-ness per field.
		if amountNil && decoded.Amount != nil {
			t.Fatalf("Amount was nil, became %v after round-trip — "+
				"codec auto-populated a zero-value default, "+
				"changing observable state", decoded.Amount)
		}
		if !amountNil && decoded.Amount == nil {
			t.Fatalf("Amount was %v, became nil after round-trip — "+
				"data loss", lock.Amount)
		}
		if createdNil && decoded.CreatedAt != nil {
			t.Fatalf("CreatedAt was nil, became %v after round-trip",
				decoded.CreatedAt)
		}
		if !createdNil && decoded.CreatedAt == nil {
			t.Fatalf("CreatedAt was %v, became nil after round-trip",
				lock.CreatedAt)
		}
		if expiresNil && decoded.ExpiresAt != nil {
			t.Fatalf("ExpiresAt was nil, became %v after round-trip",
				decoded.ExpiresAt)
		}
		if !expiresNil && decoded.ExpiresAt == nil {
			t.Fatalf("ExpiresAt was %v, became nil after round-trip",
				lock.ExpiresAt)
		}

		// Overall equality too.
		require.True(t, proto.Equal(lock, &decoded),
			"overall proto.Equal failed")
	})
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
			Amount:    &v1beta1.Coin{Denom: "ulac", Amount: "100"},
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
