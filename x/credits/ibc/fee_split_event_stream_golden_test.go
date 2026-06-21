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

// This file applies the testing-golden-artifacts skill to
// the END-TO-END EVENT STREAM emitted during a single packet
// dispatch through the fee-split middleware. It completes the
// x/credits/ibc event coverage matrix started in
// events_golden_test.go:
//
//   PER-EVENT goldens (existing, 7 files):
//     event_fee_collected.golden.json
//     event_fee_split_applied.golden.json
//     event_transfer_routed_{publisher,router,referrer,burn,insurance}
//
//   END-TO-END STREAM goldens (this file, 4 new):
//     stream_all_roles_populated.golden.json      (7 events total)
//     stream_zero_insurance_suppressed.golden.json (6 events)
//     stream_advisory_no_executor.golden.json     (7 events, executed=false)
//     stream_minimal_memo_router_only.golden.json (5 events)
//
// Why stream-level goldens are NECESSARY on top of per-event:
//
// Per-event goldens pin the shape of ONE event in isolation. They
// do not catch:
//
//   (a) Emission ORDER across event types (fee_collected MUST come
//       before fee_split_applied before the transfer_routed burst;
//       indexers that process the stream with a state machine rely
//       on this order).
//
//   (b) Conditional-emission MATRIX: which transfer_routed events
//       are suppressed when split shares round to zero or when the
//       memo lacks the corresponding recipient field. A rewrite
//       that accidentally emits a zero-amount transfer_routed would
//       pass every per-event golden (shape unchanged) but flood
//       downstream indexers with phantom entries.
//
//   (c) Stream-wide attribute consistency: settlement_id MUST match
//       across every event in the stream; denom MUST be the same
//       token in every event. A refactor that re-read memo mid-
//       stream could divergent the settlement_id, silently splitting
//       one logical settlement into multiple indexer records.
//
// The stream-level golden closes all three gaps.

// --------------------------------------------------------------
// Stream-level wire format
// --------------------------------------------------------------

// eventInStream is a single event in the stream, captured in
// emission order. Uses a plain struct (not sdk.Event) so the
// golden file is JSON-stable and doesn't depend on sdk.Event's
// proto-generated wire repr.
type eventInStream struct {
	Index      int               `json:"index"`
	Type       string            `json:"type"`
	Attributes []attributeFormat `json:"attributes"`
}

// streamGolden is the golden-file top-level structure.
type streamGolden struct {
	Scenario      string          `json:"scenario"`
	PacketID      string          `json:"packet_id"`
	EventCount    int             `json:"event_count"`
	OrderedStream []eventInStream `json:"ordered_stream"`
}

// collectFeeSplitStream extracts fee-split middleware events from
// the ctx's event manager in emission order. Non-fee-split events
// (from the underlying transfer app) are filtered out so the
// golden captures ONLY what the middleware emits.
func collectFeeSplitStream(t *testing.T, ctx sdk.Context) []eventInStream {
	t.Helper()
	feeSplitEventTypes := map[string]bool{
		"fee_collected":     true,
		"fee_split_applied": true,
		"transfer_routed":   true,
	}
	var stream []eventInStream
	idx := 0
	for _, e := range ctx.EventManager().Events() {
		if !feeSplitEventTypes[e.Type] {
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

// assertStreamGolden compares captured stream against golden file.
// UPDATE_GOLDENS=1 regenerates per testing-golden-artifacts skill.
func assertStreamGolden(t *testing.T, got streamGolden, goldenFile string) {
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
		"read golden %s — run with UPDATE_GOLDENS=1 and diff-review "+
			"before committing", path)

	var wantParsed streamGolden
	require.NoError(t, json.Unmarshal(want, &wantParsed))

	require.Equal(t, wantParsed.Scenario, got.Scenario,
		"scenario name diverges")
	require.Equal(t, wantParsed.PacketID, got.PacketID,
		"packet id diverges")
	require.Equal(t, wantParsed.EventCount, got.EventCount,
		"event count diverges (want %d got %d) — a conditional "+
			"emission branch changed: either a transfer_routed was "+
			"suppressed that shouldn't be, or emitted that shouldn't",
		wantParsed.EventCount, got.EventCount)
	require.Equal(t, len(wantParsed.OrderedStream), len(got.OrderedStream),
		"ordered stream length diverges")

	for i, wantEvt := range wantParsed.OrderedStream {
		gotEvt := got.OrderedStream[i]
		require.Equal(t, wantEvt.Index, gotEvt.Index,
			"stream[%d] index", i)
		require.Equal(t, wantEvt.Type, gotEvt.Type,
			"stream[%d] type: want %q got %q — event order changed "+
				"mid-stream; downstream state machines depending on "+
				"fee_collected→fee_split_applied→transfer_routed order "+
				"will desync",
			i, wantEvt.Type, gotEvt.Type)
		require.Equal(t, len(wantEvt.Attributes), len(gotEvt.Attributes),
			"stream[%d] attribute count drift", i)
		for j, wantAttr := range wantEvt.Attributes {
			gotAttr := gotEvt.Attributes[j]
			require.Equal(t, wantAttr.Key, gotAttr.Key,
				"stream[%d] (%s) attr[%d] key drift",
				i, wantEvt.Type, j)
			require.Equal(t, wantAttr.Value, gotAttr.Value,
				"stream[%d] (%s) attr[%d] key=%q value drift",
				i, wantEvt.Type, j, wantAttr.Key)
		}
	}
}

// --------------------------------------------------------------
// Stream-wide invariants — checked on every scenario.
// --------------------------------------------------------------

// assertStreamWideInvariants pins cross-event invariants that
// apply to every scenario. Complements per-event goldens by
// catching divergences that manifest only when you look at the
// full stream.
func assertStreamWideInvariants(t *testing.T, stream []eventInStream, expectSettlementID string) {
	t.Helper()

	require.NotEmpty(t, stream,
		"fee-split middleware must emit at least fee_collected + "+
			"fee_split_applied (2 events) on any valid packet")

	// Invariant: the first two events are fee_collected then
	// fee_split_applied, in that order. Indexers rely on this
	// prefix.
	require.Equal(t, "fee_collected", stream[0].Type,
		"fee_collected MUST be the FIRST event in the fee-split "+
			"stream — state-machine indexers read it as the "+
			"stream-start sentinel")
	require.GreaterOrEqual(t, len(stream), 2,
		"stream must have at least fee_collected + fee_split_applied")
	require.Equal(t, "fee_split_applied", stream[1].Type,
		"fee_split_applied MUST be the SECOND event — indexers "+
			"read it immediately after fee_collected to extract "+
			"split parameters")

	// Invariant: every subsequent event is transfer_routed.
	// Mixing event types mid-stream breaks indexer parsers.
	for i := 2; i < len(stream); i++ {
		require.Equal(t, "transfer_routed", stream[i].Type,
			"stream[%d] expected transfer_routed, got %q — "+
				"mid-stream event-type mixing breaks the "+
				"'transfer_routed suffix' contract",
			i, stream[i].Type)
	}

	// Invariant: settlement_id is consistent across every event.
	// A rewrite that re-read memo.SettlementID would trip this.
	for i, e := range stream {
		found := false
		for _, a := range e.Attributes {
			if a.Key == "settlement_id" {
				require.Equal(t, expectSettlementID, a.Value,
					"stream[%d] (%s) settlement_id drifted mid-stream: "+
						"want %q got %q — same packet must produce one "+
						"settlement_id throughout its event stream",
					i, e.Type, expectSettlementID, a.Value)
				found = true
				break
			}
		}
		require.True(t, found,
			"stream[%d] (%s) missing settlement_id — EVERY fee-split "+
				"event must carry settlement_id for cross-event "+
				"correlation",
			i, e.Type)
	}

	// Invariant: executed attribute is uniform across the stream.
	// If any event reports executed=true and any other executed=
	// false, downstream accounting cannot know whether the split
	// took effect.
	var firstExecuted string
	for i, e := range stream {
		for _, a := range e.Attributes {
			if a.Key == "executed" {
				if i == 0 {
					firstExecuted = a.Value
				} else {
					require.Equal(t, firstExecuted, a.Value,
						"stream[%d] (%s) executed drift: first=%q now=%q "+
							"— the executed flag must be uniform across "+
							"the whole stream (all-or-nothing contract)",
						i, e.Type, firstExecuted, a.Value)
				}
				break
			}
		}
	}

	// Invariant: denom is uniform across every event in the stream.
	// A single packet transfers ONE denom; no cross-event denom
	// drift is legitimate.
	var firstDenom string
	for i, e := range stream {
		for _, a := range e.Attributes {
			if a.Key == "denom" {
				if firstDenom == "" {
					firstDenom = a.Value
				} else {
					require.Equal(t, firstDenom, a.Value,
						"stream[%d] (%s) denom drift: first=%q now=%q",
						i, e.Type, firstDenom, a.Value)
				}
				break
			}
		}
	}
}

// --------------------------------------------------------------
// SCENARIO 1: ALL ROLES POPULATED — default params + full memo
// --------------------------------------------------------------

// TestFeeSplitEventStream_Golden_AllRolesPopulated pins the
// complete stream when every recipient role is represented:
//   - publisher + router set in memo
//   - BurnBPS > 0 → burn transfer_routed
//   - InsuranceBPS > 0 → insurance transfer_routed
//   - ReferrerBPS > 0 → referrer transfer_routed
//
// Expected: fee_collected + fee_split_applied + 5 transfer_routed = 7 events.
func TestFeeSplitEventStream_Golden_AllRolesPopulated(t *testing.T) {
	params := FeeSplitParams{
		BurnBPS:      300,  // 3%
		InsuranceBPS: 200,  // 2%
		PublisherBPS: 7000, // 70% of net
		RouterBPS:    2000, // 20% of net
		ReferrerBPS:  1000, // 10% of net
	}
	require.NoError(t, params.Validate())

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, params)
	require.NoError(t, err)
	// Install executor so splitExecuted=true flag flows through.
	mw.Executor = noopFeeSplitExecutor{}

	const settlementID = "stream-all-roles"
	packet := buildSettlementPacket(t, "1000000", "ulac", settlementID,
		"lumera1pub-all-roles", "lumera1router-all-roles")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success(), "packet should be accepted")

	stream := collectFeeSplitStream(t, ctx)
	assertStreamWideInvariants(t, stream, settlementID)

	got := streamGolden{
		Scenario:      "all_roles_populated",
		PacketID:      settlementID,
		EventCount:    len(stream),
		OrderedStream: stream,
	}
	assertStreamGolden(t, got, "stream_all_roles_populated.golden.json")
}

// --------------------------------------------------------------
// SCENARIO 2: ZERO INSURANCE SUPPRESSED — InsuranceBPS=0
// --------------------------------------------------------------

// TestFeeSplitEventStream_Golden_ZeroInsuranceSuppressed pins
// that InsuranceBPS=0 suppresses the insurance transfer_routed
// event — critical for relayers that otherwise would index a
// zero-amount "phantom" insurance transfer. The fee_split_applied
// event still carries insurance_bps=0 + insurance_amount=0, so
// consumers can still verify the configuration.
// Expected: fee_collected + fee_split_applied + 4 transfer_routed = 6 events.
func TestFeeSplitEventStream_Golden_ZeroInsuranceSuppressed(t *testing.T) {
	params := FeeSplitParams{
		BurnBPS:      500, // 5%
		InsuranceBPS: 0,   // DISABLED
		PublisherBPS: 7000,
		RouterBPS:    2000,
		ReferrerBPS:  1000,
	}
	require.NoError(t, params.Validate())

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, params)
	require.NoError(t, err)
	mw.Executor = noopFeeSplitExecutor{}

	const settlementID = "stream-zero-insurance"
	packet := buildSettlementPacket(t, "1000000", "ulac", settlementID,
		"lumera1pub-zero-ins", "lumera1router-zero-ins")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	stream := collectFeeSplitStream(t, ctx)
	assertStreamWideInvariants(t, stream, settlementID)

	// Additional scenario-specific invariant: NO transfer_routed
	// event should carry recipient_role="insurance".
	for i, e := range stream {
		if e.Type != "transfer_routed" {
			continue
		}
		for _, a := range e.Attributes {
			if a.Key == "recipient_role" && a.Value == "insurance" {
				t.Fatalf("stream[%d] emitted insurance transfer_routed "+
					"despite InsuranceBPS=0 — phantom event would "+
					"pollute indexer records", i)
			}
		}
	}

	got := streamGolden{
		Scenario:      "zero_insurance_suppressed",
		PacketID:      settlementID,
		EventCount:    len(stream),
		OrderedStream: stream,
	}
	assertStreamGolden(t, got, "stream_zero_insurance_suppressed.golden.json")
}

// --------------------------------------------------------------
// SCENARIO 3: ADVISORY NO EXECUTOR — executed=false flag
// --------------------------------------------------------------

// TestFeeSplitEventStream_Golden_AdvisoryNoExecutor pins the
// stream when NO executor is wired (split is advisory — events
// describe what WOULD happen, but no actual transfer executes).
// The executed=false flag MUST be uniform across every event, so
// downstream accounting correctly treats the stream as
// informational. RequireSplitExecutor=false allows the packet
// through; true would close it — that's a different scenario.
// Expected: fee_collected + fee_split_applied + 5 transfer_routed = 7 events,
//
//	ALL with executed=false.
func TestFeeSplitEventStream_Golden_AdvisoryNoExecutor(t *testing.T) {
	params := FeeSplitParams{
		BurnBPS:      300,
		InsuranceBPS: 200,
		PublisherBPS: 7000,
		RouterBPS:    2000,
		ReferrerBPS:  1000,
	}
	require.NoError(t, params.Validate())

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, params)
	require.NoError(t, err)
	// NO executor → advisory mode. RequireSplitExecutor default = false.
	require.Nil(t, mw.Executor,
		"fresh middleware must start with no executor for this "+
			"scenario to exercise the advisory path")

	const settlementID = "stream-advisory"
	packet := buildSettlementPacket(t, "1000000", "ulac", settlementID,
		"lumera1pub-advisory", "lumera1router-advisory")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success(),
		"advisory packet (no executor, RequireSplitExecutor=false) "+
			"must be accepted")

	stream := collectFeeSplitStream(t, ctx)
	assertStreamWideInvariants(t, stream, settlementID)

	// Scenario-specific: every event MUST have executed=false.
	for i, e := range stream {
		found := false
		for _, a := range e.Attributes {
			if a.Key == "executed" {
				require.Equal(t, "false", a.Value,
					"stream[%d] (%s) executed=%q but advisory mode "+
						"must emit executed=false uniformly",
					i, e.Type, a.Value)
				found = true
				break
			}
		}
		require.True(t, found,
			"stream[%d] (%s) missing executed attribute", i, e.Type)
	}

	got := streamGolden{
		Scenario:      "advisory_no_executor",
		PacketID:      settlementID,
		EventCount:    len(stream),
		OrderedStream: stream,
	}
	assertStreamGolden(t, got, "stream_advisory_no_executor.golden.json")
}

// --------------------------------------------------------------
// SCENARIO 4: MINIMAL MEMO — router only (no publisher set)
// --------------------------------------------------------------

// TestFeeSplitEventStream_Golden_MinimalMemoRouterOnly pins the
// stream when the memo carries only Router (publisher empty).
// The fee_split_middleware logic at :369 skips the publisher
// transfer_routed emission when memo.Publisher == "" (regardless
// of PublisherBPS split). Consumers that expected a publisher
// transfer_routed for every fee_split_applied would be wrong —
// this pins the actual behavior.
// Expected: fee_collected + fee_split_applied + 4 transfer_routed = 6 events
//
//	(publisher SKIPPED; router + referrer + burn + insurance present).
func TestFeeSplitEventStream_Golden_MinimalMemoRouterOnly(t *testing.T) {
	params := FeeSplitParams{
		BurnBPS:      300,
		InsuranceBPS: 200,
		PublisherBPS: 7000,
		RouterBPS:    2000,
		ReferrerBPS:  1000,
	}
	require.NoError(t, params.Validate())

	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, params)
	require.NoError(t, err)
	mw.Executor = noopFeeSplitExecutor{}

	const settlementID = "stream-minimal-router"
	// Empty publisher → :369 condition memo.Publisher != ""
	// suppresses the publisher transfer_routed.
	packet := buildSettlementPacket(t, "1000000", "ulac", settlementID,
		"", "lumera1router-minimal")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	stream := collectFeeSplitStream(t, ctx)
	assertStreamWideInvariants(t, stream, settlementID)

	// Scenario-specific: NO transfer_routed with recipient_role=publisher.
	for i, e := range stream {
		if e.Type != "transfer_routed" {
			continue
		}
		for _, a := range e.Attributes {
			if a.Key == "recipient_role" && a.Value == "publisher" {
				t.Fatalf("stream[%d] emitted publisher transfer_routed "+
					"despite empty memo.Publisher — suppression "+
					"contract at :369 is broken", i)
			}
		}
	}

	got := streamGolden{
		Scenario:      "minimal_memo_router_only",
		PacketID:      settlementID,
		EventCount:    len(stream),
		OrderedStream: stream,
	}
	assertStreamGolden(t, got, "stream_minimal_memo_router_only.golden.json")
}

// --------------------------------------------------------------
// Coverage matrix: every stream scenario must pair with a golden.
// --------------------------------------------------------------

// TestFeeSplitEventStream_CoverageMatrix enforces the
// stream-level scenario→golden pairing. Pairs with
// TestIBCEventTypes_AllHaveGoldenFiles which enforces the
// per-event coverage. Both together close the coverage matrix
// for x/credits/ibc event emission.
func TestFeeSplitEventStream_CoverageMatrix(t *testing.T) {
	t.Parallel()
	required := []string{
		"stream_all_roles_populated.golden.json",
		"stream_zero_insurance_suppressed.golden.json",
		"stream_advisory_no_executor.golden.json",
		"stream_minimal_memo_router_only.golden.json",
	}
	for _, f := range required {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required stream golden %s missing — a new scenario "+
				"was added without pairing with a golden artifact, "+
				"or UPDATE_GOLDENS was not run after introducing it",
			f)
	}
}

// --------------------------------------------------------------
// Helper: no-op fee-split executor so splitExecuted=true flag
// is set without requiring a real bank keeper.
// --------------------------------------------------------------

type noopFeeSplitExecutor struct{}

func (noopFeeSplitExecutor) Execute(_ sdk.Context, _ channeltypes.Packet, _ SettlementMemo, _ FeeSplitResult) error {
	return nil
}
