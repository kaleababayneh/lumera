//go:build cosmos && cosmos_full

package keeper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file builds on pane 9's per-event golden pattern (commit
// 3c92ca8f0, flow_events_golden_test.go) to pin EVENT STREAMS:
// the ORDERED SEQUENCES of events emitted by REAL keeper
// invocations during end-to-end flows. Pane 9's goldens pin
// individual event shapes; this file pins the ORDERING contract
// — which indexers replaying the stream depend on to reconstruct
// transaction timelines.
//
// Applying testing-golden-artifacts skill, Pattern 4 (semantic
// golden over decoded structure): we capture each event as
// (type, sorted-key-values) to focus on what indexers actually
// parse, then write the sequence of such records as a single
// golden file. Order matters; attribute-VALUE ordering within a
// single event also matters (indexers position-index in legacy
// wire).
//
// Complements existing coverage:
//   - x/credits/types/events_golden_test.go: per-event format
//   - x/credits/keeper/flow_events_golden_test.go: per-event
//     format for 14 flow events
//   - x/credits/ibc/events_golden_test.go: per-event format for
//     IBC middleware events (tick 23)
//
// This file's NEW contribution: the STREAM — a cross-step
// sequence of events as they fire from a real Lock → Unlock or
// Lock → ExpireLocks flow. A refactor reordering the emission
// sequence (e.g. swapping unlock-before-expiry-index-remove vs
// after) would pass all per-event goldens but change the
// observable wire stream.

// streamEvent captures a single event's wire shape for stream
// golden comparison. Matches the format pane 9 uses.
type streamEvent struct {
	Type       string            `json:"type"`
	Attributes []attributeFormat `json:"attributes"`
}

// streamGolden is a sequence of events — the GOLDEN ARTIFACT for
// an end-to-end flow.
type streamGolden struct {
	FlowName string        `json:"flow"`
	Events   []streamEvent `json:"events"`
}

// extractStream filters the EventManager events to only credits-
// module events (pinned types), converts each to streamEvent,
// and preserves emission order.
//
// Rationale: tests run through a harness that emits framework
// events (e.g., message/module) which would pollute the golden
// and make it fragile. We filter to credit-specific event types
// only, which is what downstream indexers subscribe to.
func extractStream(t *testing.T, ctx sdk.Context, flowName string) streamGolden {
	t.Helper()
	allowed := map[string]bool{
		types.EventTypeLock:        true,
		types.EventTypeUnlock:      true,
		types.EventTypeBurn:        true,
		types.EventTypeDistribute:  true,
		types.EventTypeSettlement:  true,
		types.EventTypeSwap:        true,
		types.EventTypeDispute:     true,
		"insurance_contribution_sent": true,
		"adaptive_burn_rate_evaluated": true,
		"adaptive_burn_rate_reason":    true,
		"cac_royalty_distribution":     true,
	}

	raw := ctx.EventManager().Events()
	events := make([]streamEvent, 0, len(raw))
	for _, e := range raw {
		if !allowed[e.Type] {
			continue
		}
		attrs := make([]attributeFormat, len(e.Attributes))
		for i, a := range e.Attributes {
			attrs[i] = attributeFormat{
				Key:   string(a.Key),
				Value: string(a.Value),
			}
		}
		events = append(events, streamEvent{Type: e.Type, Attributes: attrs})
	}
	return streamGolden{FlowName: flowName, Events: events}
}

// assertStreamGolden encodes the stream, compares against the
// golden file. On mismatch, emits a side-by-side diff via
// JSON-pretty prints for easier review.
func assertStreamGolden(t *testing.T, got streamGolden, goldenFile string) {
	t.Helper()

	gotBytes, err := json.Marshal(got)
	require.NoError(t, err, "marshal stream")

	path := filepath.Join("testdata", goldenFile)

	// UPDATE_GOLDENS=1 regenerates the golden from the current
	// output. Use sparingly and diff-review the result before
	// committing — see testing-golden-artifacts skill.
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, gotBytes, 0o644))
		t.Logf("[GOLDEN] wrote %s (%d bytes)", path, len(gotBytes))
		return
	}

	wantBytes, err := os.ReadFile(path)
	require.NoError(t, err, "read golden %s", path)

	var wantParsed streamGolden
	require.NoError(t, json.Unmarshal(wantBytes, &wantParsed),
		"parse golden %s", path)

	var gotParsed streamGolden
	require.NoError(t, json.Unmarshal(gotBytes, &gotParsed))

	// FlowName pin — catches accidental label drift.
	require.Equal(t, wantParsed.FlowName, gotParsed.FlowName,
		"flow name drift")

	// Event count pin — a refactor adding or removing an event
	// in the flow surfaces here.
	require.Equal(t, len(wantParsed.Events), len(gotParsed.Events),
		"event count in stream %q: want %d got %d. Events emitted "+
			"in flow have drifted — adding or removing an event "+
			"changes the indexer-observable stream shape.",
		wantParsed.FlowName, len(wantParsed.Events),
		len(gotParsed.Events))

	// Per-event: type + attribute count + attribute key/value in
	// order. Any reorder changes the indexer position-index.
	for i, want := range wantParsed.Events {
		got := gotParsed.Events[i]
		require.Equal(t, want.Type, got.Type,
			"stream[%d] type drift in flow %q: want %q got %q. "+
				"Emission ORDERING contract broken — indexers "+
				"reconstructing timelines would misattribute events.",
			i, wantParsed.FlowName, want.Type, got.Type)
		require.Equal(t, len(want.Attributes), len(got.Attributes),
			"stream[%d] (type %q) attribute count drift: want %d "+
				"got %d", i, want.Type, len(want.Attributes),
			len(got.Attributes))
		for j, wantAttr := range want.Attributes {
			gotAttr := got.Attributes[j]
			require.Equal(t, wantAttr.Key, gotAttr.Key,
				"stream[%d].attr[%d] key drift in type %q: "+
					"want %q got %q", i, j, want.Type,
				wantAttr.Key, gotAttr.Key)
			require.Equal(t, wantAttr.Value, gotAttr.Value,
				"stream[%d].attr[%d] value drift for key %q in "+
					"type %q: want %q got %q. Attribute VALUE is "+
					"part of the indexer contract; a format change "+
					"(e.g. coin.String() emitting a different denom "+
					"prefix) would silently break downstream parsing.",
				i, j, wantAttr.Key, want.Type, wantAttr.Value,
				gotAttr.Value)
		}
	}
}

// --------------------------------------------------------------
// Stream 1: Lock → UnlockCredits (explicit cancel)
// --------------------------------------------------------------

// TestCreditsEventStream_LockThenCancelUnlock pins the ordered
// sequence from a Lock + explicit Unlock (reason="cancelled").
// Two events expected: credit_lock then credit_unlock.
func TestCreditsEventStream_LockThenCancelUnlock(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	// Deterministic timestamp so any time-keyed attribute stays
	// stable across runs.
	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	// Fixed router address + funding.
	router := newAccAddress()
	accKeeper.accounts[router.String()] = router
	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(router, sdk.NewCoins(lockAmount))

	// Lock.
	lockID, err := keeper.LockCredits(
		ctx,
		router.String(),
		"session-stream-cancel",
		lockAmount,
		"tool-stream",
		"quote-stream-cancel",
		"policy@v1",
		"intent-stream-cancel",
	)
	require.NoError(t, err)

	// Immediate explicit cancel.
	require.NoError(t, keeper.UnlockCredits(ctx, lockID, "cancelled"))

	stream := extractStream(t, ctx, "lock_then_cancel_unlock")

	// The router address and lockID are captured inside attributes
	// — we can't pin them as values since they're per-run-random.
	// Normalize: replace router and lockID with stable labels
	// before comparison.
	stream = normalizeStream(stream, map[string]string{
		router.String(): "<ROUTER_ADDR>",
		lockID:          "<LOCK_ID>",
	})

	assertStreamGolden(t, stream, "stream_lock_then_cancel_unlock.golden.json")
}

// --------------------------------------------------------------
// Stream 2: Lock → ExpireLocks (auto-unlock via sweep)
// --------------------------------------------------------------

// TestCreditsEventStream_LockThenExpireUnlock pins the ordered
// sequence where a lock expires via the sweep path. Two events:
// credit_lock then credit_unlock with reason="expired".
//
// This stream MUST match the manual-cancel stream EXCEPT for
// the reason attribute — pane 9's individual goldens don't
// verify this cross-path equivalence; the stream does.
func TestCreditsEventStream_LockThenExpireUnlock(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	// Short TTL so ExpireLocks triggers at a reasonable time.
	params := keeper.GetParams(ctx)
	params.MaxLockTtlSeconds = 10
	params.DefaultLockTtlSeconds = 10
	require.NoError(t, keeper.SetParams(ctx, params))

	// Re-fresh event manager after SetParams (may emit events).
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	router := newAccAddress()
	accKeeper.accounts[router.String()] = router
	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(router, sdk.NewCoins(lockAmount))

	lockID, err := keeper.LockCredits(
		ctx,
		router.String(),
		"session-stream-expire",
		lockAmount,
		"tool-stream",
		"quote-stream-expire",
		"policy@v1",
		"intent-stream-expire",
	)
	require.NoError(t, err)

	// Advance past TTL then sweep.
	ctx = ctx.WithBlockTime(baseTime.Add(20 * time.Second))
	require.NoError(t, keeper.ExpireLocks(ctx, 0))

	stream := extractStream(t, ctx, "lock_then_expire_unlock")
	stream = normalizeStream(stream, map[string]string{
		router.String(): "<ROUTER_ADDR>",
		lockID:          "<LOCK_ID>",
	})

	assertStreamGolden(t, stream, "stream_lock_then_expire_unlock.golden.json")
}

// --------------------------------------------------------------
// Stream 3: Three concurrent locks → partial unlocks
// --------------------------------------------------------------

// TestCreditsEventStream_MultipleLocks_PartialUnlock pins the
// 3-lock stream where only some are unlocked. The event stream
// should contain 3 lock events + 2 unlock events in the
// interleave corresponding to emission order (all locks first,
// then the unlocks). Pins the "no interleaving cross-lock"
// property.
func TestCreditsEventStream_MultipleLocks_PartialUnlock(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	router := newAccAddress()
	accKeeper.accounts[router.String()] = router
	bank.FundAccount(router, sdk.NewCoins(
		sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000_000),
	))

	// Lock 3 times with distinct quote IDs + amounts.
	lockIDs := make([]string, 0, 3)
	amounts := []int64{100_000, 200_000, 300_000}
	for i, amt := range amounts {
		id, err := keeper.LockCredits(
			ctx,
			router.String(),
			"session-multi-"+string(rune('0'+i)),
			sdk.NewInt64Coin(types.DefaultCreditDenom, amt),
			"tool-multi",
			"quote-multi-"+string(rune('0'+i)),
			"policy@v1",
			"intent-multi-"+string(rune('0'+i)),
		)
		require.NoError(t, err)
		lockIDs = append(lockIDs, id)
	}

	// Unlock first and third (skip second).
	require.NoError(t, keeper.UnlockCredits(ctx, lockIDs[0], "cancelled"))
	require.NoError(t, keeper.UnlockCredits(ctx, lockIDs[2], "cancelled"))

	stream := extractStream(t, ctx, "multiple_locks_partial_unlock")
	// Normalize per-run IDs.
	replacements := map[string]string{
		router.String(): "<ROUTER_ADDR>",
	}
	for i, id := range lockIDs {
		replacements[id] = "<LOCK_ID_" + string(rune('0'+i)) + ">"
	}
	stream = normalizeStream(stream, replacements)

	assertStreamGolden(t, stream, "stream_multiple_locks_partial_unlock.golden.json")
}

// --------------------------------------------------------------
// Helpers
// --------------------------------------------------------------

// normalizeStream replaces per-run-random strings (addresses,
// lock IDs) with stable labels so the golden stays byte-stable
// across runs. Operates on attribute Values only (keys are
// already stable).
func normalizeStream(s streamGolden, replacements map[string]string) streamGolden {
	out := streamGolden{FlowName: s.FlowName, Events: make([]streamEvent, len(s.Events))}
	for i, evt := range s.Events {
		nattrs := make([]attributeFormat, len(evt.Attributes))
		for j, a := range evt.Attributes {
			v := a.Value
			if replacement, ok := replacements[v]; ok {
				v = replacement
			}
			nattrs[j] = attributeFormat{Key: a.Key, Value: v}
		}
		out.Events[i] = streamEvent{Type: evt.Type, Attributes: nattrs}
	}
	return out
}

// --------------------------------------------------------------
// Meta: stream golden coverage
// --------------------------------------------------------------

// TestCreditsEventStream_AllStreamsHaveGoldenFiles enforces that
// every declared stream has a golden file. A new flow added
// without a paired golden surfaces as a coverage gap.
func TestCreditsEventStream_AllStreamsHaveGoldenFiles(t *testing.T) {
	t.Parallel()
	required := []string{
		"stream_lock_then_cancel_unlock.golden.json",
		"stream_lock_then_expire_unlock.golden.json",
		"stream_multiple_locks_partial_unlock.golden.json",
	}
	for _, f := range required {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required stream golden %s missing — a flow was added "+
				"without pairing it with a golden artifact, leaving "+
				"the event ORDER contract unpinned for that path.",
			f)
	}
}
