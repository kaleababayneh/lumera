//go:build cosmos

package ibc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// This file applies the testing-golden-artifacts skill to the
// FAILURE-PATH event behavior of the credits/ibc fee-split
// middleware: OnAcknowledgementPacket and OnTimeoutPacket. It
// is the FINAL CORNER of the credits/ibc event coverage matrix:
//
//   per-event wire format (events_golden_test.go)
//     ✓ all 7 emitted event types, byte-stable
//   end-to-end Recv stream (tick 54)
//     ✓ 4 scenarios: all-roles, zero-insurance, advisory, minimal
//   multi-packet conformance (tick 47)
//     ✓ determinism across multiple OnRecvPacket calls
//   conservation MRs (tick 55)
//     ✓ 7 metamorphic relations + composite reconciliation
//   audit reconstruction (fee_split_audit_events_test.go)
//     ✓ events are sufficient to reconstruct settlement
//   THIS FILE: Ack/Timeout event stream (failure paths)
//     ← FINAL — pins that the middleware emits ZERO events on
//       Ack/Timeout (it delegates to the underlying app)
//
// Why this golden matters even though the middleware is a
// pass-through on Ack/Timeout:
//
//   1. The "no-event-emission" contract is load-bearing. A
//      regression that started emitting events on Ack would
//      double-count fee-collected metrics in indexer pipelines
//      (every settlement would appear in OnRecv AND OnAck).
//   2. Cross-scenario uniformity: settlement-memo and non-
//      settlement packets must behave IDENTICALLY on Ack/
//      Timeout (the middleware doesn't differentiate; only
//      OnRecv inspects the memo). A regression that started
//      checking the memo on Ack would create an asymmetry.
//   3. Multi-packet sequencing: N consecutive Acks must produce
//      N×0 middleware events. A regression that accumulated
//      state across calls (caching, batching) would trip.
//
// Scenarios pinned:
//
//   1. ack_settlement_memo: OnAck of a settlement-memo packet
//      → 0 middleware events
//   2. ack_non_settlement_memo: OnAck of a packet WITHOUT a
//      settlement memo → 0 middleware events
//   3. timeout_settlement_memo: OnTimeout of a settlement-memo
//      packet → 0 middleware events
//   4. timeout_non_settlement: OnTimeout of a non-settlement
//      packet → 0 middleware events
//   5. multi_ack_sequence: 5 Acks in sequence → 0 middleware
//      events total (no accumulator leak)

// --------------------------------------------------------------
// Failure-path event golden
// --------------------------------------------------------------

// failurePathGolden captures the failure-path event behavior
// of the middleware. The Trail is intentionally either empty
// or contains ONLY events from the underlying app (the stub) —
// the middleware itself emits nothing.
type failurePathGolden struct {
	Scenario              string            `json:"scenario"`
	HandlerCalled         string            `json:"handler_called"` // OnAck | OnTimeout
	UnderlyingAppCalled   bool              `json:"underlying_app_called"`
	MiddlewareEventCount  int               `json:"middleware_event_count"` // MUST be 0
	NumIterations         int               `json:"num_iterations"`
	Trail                 []eventInStream   `json:"trail"` // any middleware events (should be empty)
}

// collectMiddlewareEventsOnly filters out non-fee-split events
// from the ctx event manager. The middleware emits 3 event types
// on OnRecv (fee_collected, fee_split_applied, transfer_routed)
// — anything from those types found here on Ack/Timeout would
// be a contract violation.
func collectMiddlewareEventsOnly(t *testing.T, ctx sdk.Context) []eventInStream {
	t.Helper()
	middlewareTypes := map[string]bool{
		"fee_collected":     true,
		"fee_split_applied": true,
		"transfer_routed":   true,
	}
	var stream []eventInStream
	idx := 0
	for _, e := range ctx.EventManager().Events() {
		if !middlewareTypes[e.Type] {
			continue
		}
		attrs := make([]attributeFormat, len(e.Attributes))
		for i, a := range e.Attributes {
			attrs[i] = attributeFormat{
				Key:   string(a.Key),
				Value: string(a.Value),
			}
		}
		stream = append(stream, eventInStream{
			Index:      idx,
			Type:       e.Type,
			Attributes: attrs,
		})
		idx++
	}
	return stream
}

func assertFailurePathGolden(t *testing.T, got failurePathGolden, goldenFile string) {
	t.Helper()
	gotBytes, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)

	path := filepath.Join("testdata", goldenFile)
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, gotBytes, 0o644))
		t.Logf("[GOLDEN] wrote %s (%d bytes)", path, len(gotBytes))
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err,
		"read golden %s — run with UPDATE_GOLDENS=1 and diff-review",
		path)

	var wantParsed failurePathGolden
	require.NoError(t, json.Unmarshal(want, &wantParsed))

	require.Equal(t, wantParsed.Scenario, got.Scenario)
	require.Equal(t, wantParsed.HandlerCalled, got.HandlerCalled)
	require.Equal(t, wantParsed.UnderlyingAppCalled, got.UnderlyingAppCalled,
		"%s: underlying app call divergence — middleware delegation broken",
		got.Scenario)
	require.Equal(t, wantParsed.MiddlewareEventCount, got.MiddlewareEventCount,
		"%s: middleware emitted %d events on %s — MUST be 0; "+
			"regression that started emitting events on the failure "+
			"path would double-count metrics in downstream indexers",
		got.Scenario, got.MiddlewareEventCount, got.HandlerCalled)
	require.Equal(t, wantParsed.NumIterations, got.NumIterations)
	require.Equal(t, len(wantParsed.Trail), len(got.Trail))
	for i, wantEvt := range wantParsed.Trail {
		require.Equal(t, wantEvt, got.Trail[i],
			"trail[%d] diverges", i)
	}
}

// --------------------------------------------------------------
// SCENARIO 1: Ack of a settlement-memo packet → 0 events
// --------------------------------------------------------------

// TestCreditsIBCAckTimeout_Golden_AckSettlementMemo pins that
// OnAcknowledgementPacket delegates fully to the underlying app
// when the packet carries a settlement memo. The middleware
// emits ZERO events of its own — a regression that started
// emitting on Ack would double-count fee_collected metrics
// (settlement would appear in OnRecv events AND in spurious
// OnAck events).
func TestCreditsIBCAckTimeout_Golden_AckSettlementMemo(t *testing.T) {
	t.Parallel()

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	const settlementID = "ack-settle-1"
	packet := buildSettlementPacket(t, "1000000", "ulac", settlementID,
		"lumera1pub-ack", "lumera1router-ack")
	ctx := testCtx()

	// Simulate IBC framework calling OnAcknowledgementPacket
	// after a successful relay round-trip.
	ackBytes := []byte(`{"result":"AQ=="}`)
	require.NoError(t, mw.OnAcknowledgementPacket(ctx, "", packet, ackBytes, sdk.AccAddress{}))

	stream := collectMiddlewareEventsOnly(t, ctx)
	require.Empty(t, stream,
		"middleware MUST emit 0 events on OnAck (got %d)", len(stream))

	got := failurePathGolden{
		Scenario:             "ack_settlement_memo",
		HandlerCalled:        "OnAck",
		UnderlyingAppCalled:  stub.ackPacketCalled,
		MiddlewareEventCount: len(stream),
		NumIterations:        1,
		Trail:                stream,
	}
	assertFailurePathGolden(t, got, "ack_timeout_ack_settlement_memo.golden.json")
}

// --------------------------------------------------------------
// SCENARIO 2: Ack of non-settlement packet → 0 events (parity)
// --------------------------------------------------------------

// TestCreditsIBCAckTimeout_Golden_AckNonSettlement pins parity:
// the middleware behaves identically on Ack regardless of memo.
// A regression that started inspecting the memo on Ack would
// create an asymmetry (settlement-memo Acks emit, non-settlement
// don't, or vice versa) — both cases must be byte-equal here.
func TestCreditsIBCAckTimeout_Golden_AckNonSettlement(t *testing.T) {
	t.Parallel()

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Build a packet WITHOUT a Lumera settlement memo (raw
	// transfer packet with empty memo).
	packet := buildNonSettlementPacket(t, "500000", "ulac")
	ctx := testCtx()

	ackBytes := []byte(`{"result":"AQ=="}`)
	require.NoError(t, mw.OnAcknowledgementPacket(ctx, "", packet, ackBytes, sdk.AccAddress{}))

	stream := collectMiddlewareEventsOnly(t, ctx)
	require.Empty(t, stream,
		"middleware MUST emit 0 events on Ack regardless of memo")

	got := failurePathGolden{
		Scenario:             "ack_non_settlement",
		HandlerCalled:        "OnAck",
		UnderlyingAppCalled:  stub.ackPacketCalled,
		MiddlewareEventCount: len(stream),
		NumIterations:        1,
		Trail:                stream,
	}
	assertFailurePathGolden(t, got, "ack_timeout_ack_non_settlement.golden.json")
}

// --------------------------------------------------------------
// SCENARIO 3: Timeout of settlement packet → 0 events
// --------------------------------------------------------------

// TestCreditsIBCAckTimeout_Golden_TimeoutSettlementMemo pins
// that OnTimeoutPacket delegates fully even on settlement
// memos. The comment at fee_split_middleware.go:456-457 is
// explicit: "No partial split is applied on timeout — the full
// amount is refunded by the underlying transfer module." This
// golden enforces that contract: zero middleware events.
func TestCreditsIBCAckTimeout_Golden_TimeoutSettlementMemo(t *testing.T) {
	t.Parallel()

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	const settlementID = "timeout-settle-1"
	packet := buildSettlementPacket(t, "750000", "ulac", settlementID,
		"lumera1pub-timeout", "lumera1router-timeout")
	ctx := testCtx()

	require.NoError(t, mw.OnTimeoutPacket(ctx, "", packet, sdk.AccAddress{}))

	stream := collectMiddlewareEventsOnly(t, ctx)
	require.Empty(t, stream,
		"middleware MUST emit 0 events on Timeout (settlement memo)")

	got := failurePathGolden{
		Scenario:             "timeout_settlement_memo",
		HandlerCalled:        "OnTimeout",
		UnderlyingAppCalled:  stub.timeoutCalled,
		MiddlewareEventCount: len(stream),
		NumIterations:        1,
		Trail:                stream,
	}
	assertFailurePathGolden(t, got, "ack_timeout_timeout_settlement_memo.golden.json")
}

// --------------------------------------------------------------
// SCENARIO 4: Timeout of non-settlement packet → 0 events
// --------------------------------------------------------------

func TestCreditsIBCAckTimeout_Golden_TimeoutNonSettlement(t *testing.T) {
	t.Parallel()

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildNonSettlementPacket(t, "200000", "ulac")
	ctx := testCtx()

	require.NoError(t, mw.OnTimeoutPacket(ctx, "", packet, sdk.AccAddress{}))

	stream := collectMiddlewareEventsOnly(t, ctx)
	require.Empty(t, stream,
		"middleware MUST emit 0 events on Timeout (non-settlement)")

	got := failurePathGolden{
		Scenario:             "timeout_non_settlement",
		HandlerCalled:        "OnTimeout",
		UnderlyingAppCalled:  stub.timeoutCalled,
		MiddlewareEventCount: len(stream),
		NumIterations:        1,
		Trail:                stream,
	}
	assertFailurePathGolden(t, got, "ack_timeout_timeout_non_settlement.golden.json")
}

// --------------------------------------------------------------
// SCENARIO 5: Multi-Ack sequence — 5 in a row produce 0 events
// --------------------------------------------------------------

// TestCreditsIBCAckTimeout_Golden_MultiAckSequence pins that
// even N consecutive OnAck calls produce zero middleware events.
// A regression accumulating state (caching memo data, batching
// for delayed emission) would leak events here that wouldn't
// appear with a single call.
func TestCreditsIBCAckTimeout_Golden_MultiAckSequence(t *testing.T) {
	t.Parallel()

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	ackBytes := []byte(`{"result":"AQ=="}`)
	const numAcks = 5
	for i := 0; i < numAcks; i++ {
		// Mix settlement and non-settlement memos to also pin
		// that the per-call no-leak guarantee holds across
		// memo variants.
		var packet channeltypes.Packet
		if i%2 == 0 {
			packet = buildSettlementPacket(t, "100000", "ulac",
				"multi-ack-settle", "lumera1pub-multi", "lumera1router-multi")
		} else {
			packet = buildNonSettlementPacket(t, "100000", "ulac")
		}
		require.NoError(t, mw.OnAcknowledgementPacket(ctx, "", packet, ackBytes, sdk.AccAddress{}))
	}

	stream := collectMiddlewareEventsOnly(t, ctx)
	require.Empty(t, stream,
		"middleware MUST emit 0 events across %d OnAck calls; got %d "+
			"— accumulator leak detected",
		numAcks, len(stream))

	got := failurePathGolden{
		Scenario:             "multi_ack_sequence",
		HandlerCalled:        "OnAck",
		UnderlyingAppCalled:  stub.ackPacketCalled,
		MiddlewareEventCount: len(stream),
		NumIterations:        numAcks,
		Trail:                stream,
	}
	assertFailurePathGolden(t, got, "ack_timeout_multi_ack_sequence.golden.json")
}

// --------------------------------------------------------------
// Coverage matrix + cross-file invariants
// --------------------------------------------------------------

// TestCreditsIBCAckTimeout_CoverageMatrix asserts every scenario
// pairs with a golden file. Completes the credits/ibc event
// coverage matrix: per-event + Recv-stream + Ack/Timeout-stream.
func TestCreditsIBCAckTimeout_CoverageMatrix(t *testing.T) {
	t.Parallel()
	required := []string{
		"ack_timeout_ack_settlement_memo.golden.json",
		"ack_timeout_ack_non_settlement.golden.json",
		"ack_timeout_timeout_settlement_memo.golden.json",
		"ack_timeout_timeout_non_settlement.golden.json",
		"ack_timeout_multi_ack_sequence.golden.json",
	}
	for _, f := range required {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required failure-path golden %s missing", f)
	}
}

// TestCreditsIBCAckTimeout_Invariant_ZeroEventsAcrossAllScenarios
// asserts that EVERY captured failure-path golden has
// MiddlewareEventCount=0. This is the load-bearing contract:
// middleware emits NOTHING on Ack/Timeout. A regression
// breaking this anywhere would trip here even if a specific
// scenario's per-test assertion was somehow weakened.
func TestCreditsIBCAckTimeout_Invariant_ZeroEventsAcrossAllScenarios(t *testing.T) {
	t.Parallel()
	for _, f := range []string{
		"ack_timeout_ack_settlement_memo.golden.json",
		"ack_timeout_ack_non_settlement.golden.json",
		"ack_timeout_timeout_settlement_memo.golden.json",
		"ack_timeout_timeout_non_settlement.golden.json",
		"ack_timeout_multi_ack_sequence.golden.json",
	} {
		bz, err := os.ReadFile(filepath.Join("testdata", f))
		require.NoError(t, err, "read %s", f)
		var g failurePathGolden
		require.NoError(t, json.Unmarshal(bz, &g))

		require.Equal(t, 0, g.MiddlewareEventCount,
			"%s: MiddlewareEventCount=%d, MUST be 0 — middleware "+
				"started emitting events on a failure-path handler, "+
				"breaking the pass-through contract",
			f, g.MiddlewareEventCount)
		require.Empty(t, g.Trail,
			"%s: Trail not empty — failure path leaked events",
			f)
		require.True(t, g.UnderlyingAppCalled,
			"%s: UnderlyingAppCalled=false — middleware failed to "+
				"delegate; underlying transfer module never saw "+
				"the packet (refund path broken)",
			f)
	}
}

// --------------------------------------------------------------
// Test helper — non-settlement packet (no Lumera memo)
// --------------------------------------------------------------

// buildNonSettlementPacket returns a transfer packet with an
// empty memo. Used to test the parity invariant: middleware
// behavior on Ack/Timeout is identical regardless of whether
// the packet carries settlement metadata.
func buildNonSettlementPacket(t *testing.T, amount, denom string) channeltypes.Packet {
	t.Helper()
	// Reuse the testCtx helper from middleware_test, but build
	// a packet with empty memo (no settlement envelope).
	return channeltypes.Packet{
		Sequence:           1,
		SourcePort:         "transfer",
		SourceChannel:      "channel-0",
		DestinationPort:    "transfer",
		DestinationChannel: "channel-1",
		Data:               buildPlainTransferData(t, amount, denom),
	}
}

// buildPlainTransferData builds an FT transfer packet data with
// an empty memo (used for non-settlement scenarios).
func buildPlainTransferData(t *testing.T, amount, denom string) []byte {
	t.Helper()
	// Build a minimal FungibleTokenPacketData JSON manually so
	// we don't have to import transfertypes for this test.
	data := map[string]string{
		"denom":    denom,
		"amount":   amount,
		"sender":   "sender",
		"receiver": "receiver",
		"memo":     "",
	}
	bz, err := json.Marshal(data)
	require.NoError(t, err)
	return bz
}
