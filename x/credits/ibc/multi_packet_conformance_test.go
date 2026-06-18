//go:build cosmos

package ibc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file applies the testing-conformance-harnesses skill
// (Pattern 1 — Differential Testing) to multi-packet IBC
// settlement processing. The fee_split middleware processes
// each ICS-20 packet bearing a Lumera settlement memo by
// computing splits, optionally invoking the executor, and
// emitting events. Multi-packet processing across two
// validator instances MUST produce byte-identical:
//
//   1. Acknowledgment sequence (success/error per packet)
//   2. Event sequence (ordered events from each packet)
//   3. Underlying-app call count (delegated packets)
//
// Existing coverage:
//   - fee_split_middleware_test.go: per-packet unit tests
//   - fee_split_audit_events_test.go (tick 7): audit-recon-
//     struction event coverage
//   - events_golden_test.go (tick 23): per-event wire format
//   - packet_roundtrip_test.go: encoding round-trip
//
// What's NOT covered: ENTIRE-SEQUENCE determinism across
// validators when N packets stream through. A refactor
// introducing per-packet hidden state (cache, counter, random
// seed) would surface here as divergence between two
// independent middleware instances.

// packetSpec describes a single packet to feed to the middleware.
type packetSpec struct {
	amount       string
	denom        string
	settlementID string
	publisher    string
	router       string
	isSettlement bool // false → non-settlement (passthrough)
	corrupt      bool // true → malformed memo (error ack expected)
}

// buildPacket creates a channel.Packet from a spec. For
// is-settlement=true, builds a valid settlement packet; for
// false, builds a non-settlement transfer (passthrough).
func buildPacket(t *testing.T, spec packetSpec) channeltypes.Packet {
	t.Helper()
	if !spec.isSettlement {
		// Non-settlement ICS-20 packet — passthrough.
		ftData := map[string]string{
			"denom":    spec.denom,
			"amount":   spec.amount,
			"sender":   "sender",
			"receiver": "receiver",
		}
		bz, err := json.Marshal(ftData)
		require.NoError(t, err)
		return channeltypes.Packet{
			Sequence:           1,
			SourcePort:         "transfer",
			SourceChannel:      "channel-0",
			DestinationPort:    "transfer",
			DestinationChannel: "channel-1",
			Data:               bz,
		}
	}
	if spec.corrupt {
		// Corrupt memo: a non-empty memo string that LOOKS like
		// a settlement memo but fails JSON parsing — triggers
		// the error-ack path.
		ftData := map[string]string{
			"denom":    spec.denom,
			"amount":   spec.amount,
			"sender":   "sender",
			"receiver": "receiver",
			"memo":     `{"type":"lumera_settlement",broken_json`,
		}
		bz, err := json.Marshal(ftData)
		require.NoError(t, err)
		return channeltypes.Packet{
			Sequence:           1,
			SourcePort:         "transfer",
			SourceChannel:      "channel-0",
			DestinationPort:    "transfer",
			DestinationChannel: "channel-1",
			Data:               bz,
		}
	}
	return buildSettlementPacket(t, spec.amount, spec.denom,
		spec.settlementID, spec.publisher, spec.router)
}

// processSequence feeds N packets through a fresh middleware
// and returns the per-packet ack outcomes plus the captured
// event manager.
type sequenceResult struct {
	AckSuccesses []bool       // one per packet
	StubCalls    []bool       // recvPacketCalled state per packet
	Events       []sdk.Event  // all events from a fresh manager
}

func processSequence(t *testing.T, params FeeSplitParams, packets []packetSpec) sequenceResult {
	t.Helper()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, params)
	require.NoError(t, err)

	ctx := testCtx() // fresh ctx with empty event manager
	out := sequenceResult{
		AckSuccesses: make([]bool, 0, len(packets)),
		StubCalls:    make([]bool, 0, len(packets)),
	}

	for i, spec := range packets {
		// Reset stub state per packet to capture per-call info.
		stub.recvPacketCalled = false

		packet := buildPacket(t, spec)
		ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

		out.AckSuccesses = append(out.AckSuccesses, ack.Success())
		out.StubCalls = append(out.StubCalls, stub.recvPacketCalled)
		_ = i
	}

	out.Events = ctx.EventManager().Events()
	return out
}

// eventsToWire converts the captured events to a stable JSON
// representation for byte-comparison.
func eventsToWire(events []sdk.Event) []byte {
	type attr struct {
		K string `json:"k"`
		V string `json:"v"`
	}
	type ev struct {
		T    string `json:"t"`
		Attr []attr `json:"a"`
	}
	out := make([]ev, len(events))
	for i, e := range events {
		out[i].T = e.Type
		out[i].Attr = make([]attr, len(e.Attributes))
		for j, a := range e.Attributes {
			out[i].Attr[j] = attr{K: string(a.Key), V: string(a.Value)}
		}
	}
	bz, _ := json.Marshal(out)
	return bz
}

// --------------------------------------------------------------
// CORE CONFORMANCE: two middlewares, same packet sequence
// --------------------------------------------------------------

// TestFeeSplitConformance_TwoMiddlewaresProduceIdenticalSequence
// is the canonical multi-packet conformance test. Two
// middleware instances with identical params and identical
// packet sequence MUST produce byte-identical acks + events.
func TestFeeSplitConformance_TwoMiddlewaresProduceIdenticalSequence(t *testing.T) {
	t.Parallel()

	packets := []packetSpec{
		{amount: "10000", denom: "ulac", settlementID: "settle-1",
			publisher: "lumera1pub1", router: "lumera1router1",
			isSettlement: true},
		{amount: "25000", denom: "ulac", settlementID: "settle-2",
			publisher: "lumera1pub2", router: "lumera1router2",
			isSettlement: true},
		{amount: "100000", denom: "uatom", settlementID: "settle-3",
			publisher: "lumera1pub3", router: "lumera1router3",
			isSettlement: true},
		// Non-settlement packet (passthrough).
		{amount: "500", denom: "ulac", isSettlement: false},
		// Settlement again — pin that ordering matters.
		{amount: "75000", denom: "ulac", settlementID: "settle-5",
			publisher: "lumera1pub5", router: "lumera1router5",
			isSettlement: true},
	}

	resultA := processSequence(t, DefaultFeeSplitParams(), packets)
	resultB := processSequence(t, DefaultFeeSplitParams(), packets)

	// Per-packet ack and stub-call match.
	require.Equal(t, resultA.AckSuccesses, resultB.AckSuccesses,
		"ack-success sequence diverges across middlewares")
	require.Equal(t, resultA.StubCalls, resultB.StubCalls,
		"stub-call sequence diverges across middlewares")

	// Event-stream byte equality.
	bzA := eventsToWire(resultA.Events)
	bzB := eventsToWire(resultB.Events)
	assert.True(t, bytes.Equal(bzA, bzB),
		"event stream byte diverges across middlewares.\n  "+
			"a: %s\n  b: %s", string(bzA), string(bzB))

	// Event count sanity: 5 packets, 4 settlements expected to
	// emit fee_collected + fee_split_applied + transfer_routed
	// per recipient. Non-settlement passes through with no
	// fee_split events.
	require.Equal(t, len(resultA.Events), len(resultB.Events),
		"event count matches between A and B")
}

// --------------------------------------------------------------
// SHAPE INVARIANTS over the multi-packet stream
// --------------------------------------------------------------

// TestFeeSplitConformance_AllSettlementsAreAcknowledgedSuccess
// pins that every well-formed settlement packet produces a
// SUCCESS ack. Pre-existing unit tests cover this for one
// packet; this pins it across N packets in a sequence.
func TestFeeSplitConformance_AllSettlementsInBatchAreSuccessfullyAcked(t *testing.T) {
	t.Parallel()

	packets := []packetSpec{}
	for i := 0; i < 10; i++ {
		packets = append(packets, packetSpec{
			amount:       fmt.Sprintf("%d", 1000*(i+1)),
			denom:        "ulac",
			settlementID: fmt.Sprintf("settle-%d", i),
			publisher:    fmt.Sprintf("lumera1pub%d", i),
			router:       fmt.Sprintf("lumera1router%d", i),
			isSettlement: true,
		})
	}

	result := processSequence(t, DefaultFeeSplitParams(), packets)

	for i, ok := range result.AckSuccesses {
		assert.True(t, ok,
			"packet %d (settlement) MUST be acked success — got "+
				"failure. No per-packet errors should arise from "+
				"otherwise-valid sequence position.", i)
	}
}

// TestFeeSplitConformance_NonSettlementPacketsAlwaysPassThrough
// pins that a sequence of NON-settlement packets all pass
// through to the underlying app (stub.recvPacketCalled =
// true) and never emit fee_split events. A refactor
// accidentally treating non-settlement packets as settlements
// would emit phantom events.
func TestFeeSplitConformance_NonSettlementPacketsAlwaysDelegate(t *testing.T) {
	t.Parallel()

	packets := []packetSpec{}
	for i := 0; i < 5; i++ {
		packets = append(packets, packetSpec{
			amount:       fmt.Sprintf("%d", 100*(i+1)),
			denom:        "ulac",
			isSettlement: false,
		})
	}

	result := processSequence(t, DefaultFeeSplitParams(), packets)

	for i, called := range result.StubCalls {
		assert.True(t, called,
			"non-settlement packet %d must delegate to underlying app", i)
	}

	// No fee_split events should be present.
	feeSplitEventCount := 0
	for _, e := range result.Events {
		if e.Type == "fee_collected" || e.Type == "fee_split_applied" || e.Type == "transfer_routed" {
			feeSplitEventCount++
		}
	}
	assert.Equal(t, 0, feeSplitEventCount,
		"non-settlement packet sequence MUST emit ZERO fee-split "+
			"events. Found %d. Pins that the memo-detection guard "+
			"correctly distinguishes settlement vs ordinary packets.",
		feeSplitEventCount)
}

// TestFeeSplitConformance_MixedSettlementNonSettlementOrder pins
// that interleaving settlement and non-settlement packets
// preserves the order: settlements emit events at their
// position in the stream; non-settlements don't.
func TestFeeSplitConformance_MixedSequenceEventCountMatchesSettlementCount(t *testing.T) {
	t.Parallel()

	// 3 settlements + 2 non-settlements, interleaved.
	packets := []packetSpec{
		{amount: "1000", denom: "ulac", settlementID: "s1",
			publisher: "lumera1pub1", router: "lumera1router1",
			isSettlement: true},
		{amount: "100", denom: "ulac", isSettlement: false},
		{amount: "2000", denom: "ulac", settlementID: "s2",
			publisher: "lumera1pub2", router: "lumera1router2",
			isSettlement: true},
		{amount: "200", denom: "ulac", isSettlement: false},
		{amount: "3000", denom: "ulac", settlementID: "s3",
			publisher: "lumera1pub3", router: "lumera1router3",
			isSettlement: true},
	}

	result := processSequence(t, DefaultFeeSplitParams(), packets)

	// Settlement packets emit at least: fee_collected +
	// fee_split_applied + N transfer_routed events. Pin that
	// each settlement's events appear after the prior packet's.
	settlementIDsInOrder := []string{}
	for _, e := range result.Events {
		if e.Type == "fee_collected" {
			for _, a := range e.Attributes {
				if string(a.Key) == "settlement_id" {
					settlementIDsInOrder = append(settlementIDsInOrder,
						string(a.Value))
				}
			}
		}
	}

	require.Equal(t, []string{"s1", "s2", "s3"}, settlementIDsInOrder,
		"MR ordering: fee_collected events appear in settlement-"+
			"arrival order (s1, s2, s3) regardless of interleaved "+
			"non-settlement packets between them. A refactor "+
			"buffering events for batch emission could disrupt "+
			"this ordering.")
}

// --------------------------------------------------------------
// LARGE-SCALE STREAMING
// --------------------------------------------------------------

// TestFeeSplitConformance_LargeSettlementStreamConverges pins
// determinism over a 50-packet stream of varied settlements.
// Two middleware instances with different memo addresses,
// amounts, and denoms still produce byte-identical event
// streams. The toughest conformance probe: many opportunities
// for non-determinism over a long stream.
func TestFeeSplitConformance_LargeSettlementStreamByteIdentical(t *testing.T) {
	t.Parallel()

	// 50 varied settlement packets.
	packets := make([]packetSpec, 50)
	for i := 0; i < 50; i++ {
		denom := "ulac"
		if i%3 == 0 {
			denom = "uatom"
		} else if i%5 == 0 {
			denom = "uosmo"
		}
		packets[i] = packetSpec{
			amount:       fmt.Sprintf("%d", 1000+i*100),
			denom:        denom,
			settlementID: fmt.Sprintf("large-stream-%d", i),
			publisher:    fmt.Sprintf("lumera1pub%d", i),
			router:       fmt.Sprintf("lumera1router%d", i),
			isSettlement: true,
		}
	}

	resultA := processSequence(t, DefaultFeeSplitParams(), packets)
	resultB := processSequence(t, DefaultFeeSplitParams(), packets)

	require.Equal(t, resultA.AckSuccesses, resultB.AckSuccesses)
	bzA := eventsToWire(resultA.Events)
	bzB := eventsToWire(resultB.Events)
	require.True(t, bytes.Equal(bzA, bzB),
		"50-packet stream produces byte-identical event sequence "+
			"across middlewares. lenA=%d lenB=%d",
		len(bzA), len(bzB))
}

// --------------------------------------------------------------
// PARAMS DETERMINISM
// --------------------------------------------------------------

// TestFeeSplitConformance_DifferentParamsProduceDifferentStreams
// pins the dual: with DIFFERENT FeeSplitParams (different BPS
// allocations), the SAME packet sequence produces DIFFERENT
// event streams. A refactor that ignored params would produce
// identical streams regardless — a serious bug.
func TestFeeSplitConformance_DifferentParamsProduceDifferentStreams(t *testing.T) {
	t.Parallel()

	packets := []packetSpec{
		{amount: "100000", denom: "ulac", settlementID: "diff-test",
			publisher: "lumera1pub", router: "lumera1router",
			isSettlement: true},
	}

	defaultResult := processSequence(t, DefaultFeeSplitParams(), packets)

	// Use a different valid configuration.
	custom := FeeSplitParams{
		BurnBPS: 500, InsuranceBPS: 100,
		PublisherBPS: 5000, RouterBPS: 3000, ReferrerBPS: 2000,
	}
	require.NoError(t, custom.Validate())
	customResult := processSequence(t, custom, packets)

	bzDefault := eventsToWire(defaultResult.Events)
	bzCustom := eventsToWire(customResult.Events)

	require.False(t, bytes.Equal(bzDefault, bzCustom),
		"different FeeSplitParams MUST produce different event "+
			"streams (different BPS → different per-recipient "+
			"amounts). A refactor ignoring params would produce "+
			"identical streams — a serious bug. defaultLen=%d "+
			"customLen=%d", len(bzDefault), len(bzCustom))
}

// --------------------------------------------------------------
// REPLAY DETERMINISM
// --------------------------------------------------------------

// TestFeeSplitConformance_RepeatedReplayProducesIdenticalOutput
// pins SINGLE-instance determinism: feeding the same packet
// sequence to the same middleware (FRESH each replay) produces
// byte-identical output across replays. Catches non-
// determinism within a single instance (e.g. random nonce in
// event keys).
func TestFeeSplitConformance_RepeatedReplaysProduceIdenticalOutput(t *testing.T) {
	t.Parallel()

	packets := []packetSpec{
		{amount: "10000", denom: "ulac", settlementID: "replay-1",
			publisher: "lumera1pub", router: "lumera1router",
			isSettlement: true},
		{amount: "20000", denom: "ulac", settlementID: "replay-2",
			publisher: "lumera1pub", router: "lumera1router",
			isSettlement: true},
	}

	first := processSequence(t, DefaultFeeSplitParams(), packets)
	bzFirst := eventsToWire(first.Events)

	for i := 0; i < 3; i++ {
		again := processSequence(t, DefaultFeeSplitParams(), packets)
		bzAgain := eventsToWire(again.Events)
		require.True(t, bytes.Equal(bzFirst, bzAgain),
			"replay %d diverges from first run. Same input MUST "+
				"produce same output across replays.", i)
	}
}
