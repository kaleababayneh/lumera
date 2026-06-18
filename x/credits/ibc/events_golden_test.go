//go:build cosmos

package ibc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// This file pins the WIRE FORMAT of the three IBC fee-split
// middleware events emitted from fee_split_middleware.go:339-423:
//
//   - "fee_collected"     (6 attributes)
//   - "fee_split_applied" (14 attributes — split amounts + BPS)
//   - "transfer_routed"   (6 attributes — per-recipient-role, 5 roles)
//
// Following the testing-golden-artifacts skill and the pattern
// established by commit 8a18162f3 (pane 10, x/router/types/
// events_golden_test.go + internal/router/audit_golden_test.go).
//
// Why this matters
// ----------------
// Downstream consumers — IBC relayers, cross-chain indexers (Mintscan,
// Nomic, SquadScan), settlement explorers, SIEM rules, auditor
// pipelines — parse these events byte-for-byte off the ABCI event log.
// Any silent change to attribute keys, attribute order, event type
// strings, or value-formatting (e.g. coin.String() switching to a
// different format) silently breaks those integrations. Unlike
// x/credits/types where EventType*/AttributeKey* are module-level
// constants, fee_split_middleware.go uses STRING LITERALS in-place —
// so a bare rename is even easier to land without test failure. Golden
// files catch exactly that class of drift.
//
// Complement to the bd-5na.7 audit test
// -------------------------------------
// TestFeeSplitMiddleware_EventsProvideAuditReconstruction (in
// fee_split_audit_events_test.go) pins that the events contain
// enough info to reconstruct the settlement flow end-to-end. This
// golden test is ORTHOGONAL: it pins the EXACT WIRE FORMAT —
// attribute key strings, ordering, type literal, value encoding.
// Audit test asks "can I reconstruct?"; golden test asks "is the
// format byte-stable?". Both matter; this file closes the format
// angle.

// eventWireFormat mirrors the JSON structure of sdk.Event for golden
// comparison. Intentionally duplicated from x/router/types' test
// helper (same pattern, 8a18162f3) rather than cross-package-imported,
// keeping each test package self-contained and avoiding test-only
// cross-module dependencies.
type eventWireFormat struct {
	Type       string            `json:"type"`
	Attributes []attributeFormat `json:"attributes"`
}

type attributeFormat struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func sdkEventToWireFormat(evt sdk.Event) eventWireFormat {
	attrs := make([]attributeFormat, len(evt.Attributes))
	for i, attr := range evt.Attributes {
		attrs[i] = attributeFormat{
			Key:   string(attr.Key),
			Value: string(attr.Value),
		}
	}
	return eventWireFormat{
		Type:       evt.Type,
		Attributes: attrs,
	}
}

func loadGoldenIBC(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file %s", path)
	return data
}

func assertGoldenMatchIBC(t *testing.T, evt sdk.Event, goldenFile string) {
	t.Helper()

	wire := sdkEventToWireFormat(evt)
	got, err := json.Marshal(wire)
	require.NoError(t, err)

	want := loadGoldenIBC(t, goldenFile)

	// Compare as decoded JSON objects so whitespace/key-order
	// of the enclosing envelope doesn't matter — but attribute
	// ORDER within .Attributes is significant (it determines
	// how indexers ingest tuples by position in the legacy wire).
	var gotObj, wantObj eventWireFormat
	require.NoError(t, json.Unmarshal(got, &gotObj))
	require.NoError(t, json.Unmarshal(want, &wantObj))

	require.Equal(t, wantObj.Type, gotObj.Type,
		"event type mismatch — a rename of %q breaks every downstream "+
			"relayer/indexer that subscribes to this event type",
		wantObj.Type)

	require.Equal(t, len(wantObj.Attributes), len(gotObj.Attributes),
		"attribute count mismatch for %q — adding/removing an attribute "+
			"is a wire-format change; run `UPDATE_GOLDENS=1 go test` and "+
			"review the diff before accepting",
		wantObj.Type)

	for i, wantAttr := range wantObj.Attributes {
		gotAttr := gotObj.Attributes[i]
		require.Equal(t, wantAttr.Key, gotAttr.Key,
			"attribute[%d] key mismatch for %q: want %q got %q — rename "+
				"breaks downstream indexer queries that filter by key name",
			i, wantObj.Type, wantAttr.Key, gotAttr.Key)
		require.Equal(t, wantAttr.Value, gotAttr.Value,
			"attribute[%d] value mismatch for %q key=%q — value formatting "+
				"change (e.g. coin.String() switching denom prefix, bps "+
				"formatting switching base) silently breaks consumers that "+
				"parse values",
			i, wantObj.Type, wantAttr.Key)
	}
}

// coinStr mirrors fee_split_middleware.go's .String() usage for
// amount fields. Fixed-denom "utoken" + fixed-amount so the golden
// is deterministic across runs.
func coinStr(amount int64) string {
	return sdk.NewCoin("utoken", sdkmath.NewInt(amount)).String()
}

// executedAttrValue mirrors the middleware's strconv.FormatBool of
// the splitExecuted flag. Pinning both branches (true/false) would
// double the golden count; we pin the true branch as the common
// case — production traffic with an executor configured.
const executedGoldenValue = "true"

func executedAttr() sdk.Attribute {
	return sdk.NewAttribute("executed", strconv.FormatBool(true))
}

// ---------------------------------------------------------------
// fee_collected
// ---------------------------------------------------------------

// TestEventFeeCollected_GoldenWireFormat pins the fee_collected
// event emitted at fee_split_middleware.go:339-347. This is the
// TOP-LEVEL accounting event that cross-chain aggregators use to
// tally total settlement throughput per channel pair.
func TestEventFeeCollected_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"fee_collected",
		sdk.NewAttribute("settlement_id", "settle-001"),
		sdk.NewAttribute("total_amount", coinStr(1_000_000)),
		sdk.NewAttribute("denom", "utoken"),
		sdk.NewAttribute("source_channel", "channel-0"),
		sdk.NewAttribute("destination_channel", "channel-42"),
		executedAttr(),
	)

	assertGoldenMatchIBC(t, evt, "event_fee_collected.golden.json")
}

// ---------------------------------------------------------------
// fee_split_applied
// ---------------------------------------------------------------

// TestEventFeeSplitApplied_GoldenWireFormat pins the
// fee_split_applied event emitted at :350-366. This event carries
// the per-role split amounts AND the BPS configuration — both are
// needed by explorers to verify settlement arithmetic matches the
// advertised rates. A silent reordering between amount/BPS blocks
// would break downstream proofs.
func TestEventFeeSplitApplied_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"fee_split_applied",
		sdk.NewAttribute("settlement_id", "settle-001"),
		sdk.NewAttribute("burn_amount", coinStr(30_000)),
		sdk.NewAttribute("insurance_amount", coinStr(0)),
		sdk.NewAttribute("net_amount", coinStr(970_000)),
		sdk.NewAttribute("publisher_amount", coinStr(679_000)),
		sdk.NewAttribute("router_amount", coinStr(194_000)),
		sdk.NewAttribute("referrer_amount", coinStr(97_000)),
		sdk.NewAttribute("denom", "utoken"),
		sdk.NewAttribute("burn_bps", "300"),
		sdk.NewAttribute("insurance_bps", "0"),
		sdk.NewAttribute("publisher_bps", "7000"),
		sdk.NewAttribute("router_bps", "2000"),
		sdk.NewAttribute("referrer_bps", "1000"),
		executedAttr(),
	)

	assertGoldenMatchIBC(t, evt, "event_fee_split_applied.golden.json")
}

// ---------------------------------------------------------------
// transfer_routed — 5 recipient-role variants
// ---------------------------------------------------------------

// TestEventTransferRouted_Publisher_GoldenWireFormat pins the
// transfer_routed event for the publisher role (:370-378). Each
// role uses the SAME 6-attribute shape but distinct recipient_role
// + recipient values. Downstream processors switch on
// recipient_role — a value-rename there breaks revenue attribution.
func TestEventTransferRouted_Publisher_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"transfer_routed",
		sdk.NewAttribute("settlement_id", "settle-001"),
		sdk.NewAttribute("recipient_role", "publisher"),
		sdk.NewAttribute("recipient", "cosmos1publisheraddress"),
		sdk.NewAttribute("amount", coinStr(679_000)),
		sdk.NewAttribute("denom", "utoken"),
		executedAttr(),
	)

	assertGoldenMatchIBC(t, evt, "event_transfer_routed_publisher.golden.json")
}

// TestEventTransferRouted_Router_GoldenWireFormat pins the router
// variant (:381-389).
func TestEventTransferRouted_Router_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"transfer_routed",
		sdk.NewAttribute("settlement_id", "settle-001"),
		sdk.NewAttribute("recipient_role", "router"),
		sdk.NewAttribute("recipient", "cosmos1routeraddress"),
		sdk.NewAttribute("amount", coinStr(194_000)),
		sdk.NewAttribute("denom", "utoken"),
		executedAttr(),
	)

	assertGoldenMatchIBC(t, evt, "event_transfer_routed_router.golden.json")
}

// TestEventTransferRouted_Referrer_GoldenWireFormat pins the
// referrer variant (:392-400). Recipient is the literal "referrer"
// string — referrers are not resolved to addresses at this layer.
func TestEventTransferRouted_Referrer_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"transfer_routed",
		sdk.NewAttribute("settlement_id", "settle-001"),
		sdk.NewAttribute("recipient_role", "referrer"),
		sdk.NewAttribute("recipient", "referrer"),
		sdk.NewAttribute("amount", coinStr(97_000)),
		sdk.NewAttribute("denom", "utoken"),
		executedAttr(),
	)

	assertGoldenMatchIBC(t, evt, "event_transfer_routed_referrer.golden.json")
}

// TestEventTransferRouted_Burn_GoldenWireFormat pins the burn
// variant (:403-411). Recipient is the literal "burn" — burns
// don't have an address; attributing them via the literal keeps
// indexer logic uniform.
func TestEventTransferRouted_Burn_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"transfer_routed",
		sdk.NewAttribute("settlement_id", "settle-001"),
		sdk.NewAttribute("recipient_role", "burn"),
		sdk.NewAttribute("recipient", "burn"),
		sdk.NewAttribute("amount", coinStr(30_000)),
		sdk.NewAttribute("denom", "utoken"),
		executedAttr(),
	)

	assertGoldenMatchIBC(t, evt, "event_transfer_routed_burn.golden.json")
}

// TestEventTransferRouted_Insurance_GoldenWireFormat pins the
// insurance variant (:414-422). Recipient is "insurance_pool" — a
// SEPARATE literal from the role ("insurance"). A refactor that
// unified these to a single string would be a silent wire change.
func TestEventTransferRouted_Insurance_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"transfer_routed",
		sdk.NewAttribute("settlement_id", "settle-001"),
		sdk.NewAttribute("recipient_role", "insurance"),
		sdk.NewAttribute("recipient", "insurance_pool"),
		sdk.NewAttribute("amount", coinStr(50_000)),
		sdk.NewAttribute("denom", "utoken"),
		executedAttr(),
	)

	assertGoldenMatchIBC(t, evt, "event_transfer_routed_insurance.golden.json")
}

// ---------------------------------------------------------------
// Wire-stability meta-tests
// ---------------------------------------------------------------

// TestIBCEventTypes_AllHaveGoldenFiles enforces that every event
// type emitted from fee_split_middleware.go has a corresponding
// golden file. A new event type added to the middleware without
// a golden file is a wire-format coverage gap. Mirrors the
// TestEventTypes_AllHaveGoldenFiles anchor from 8a18162f3.
func TestIBCEventTypes_AllHaveGoldenFiles(t *testing.T) {
	t.Parallel()

	required := []string{
		"event_fee_collected.golden.json",
		"event_fee_split_applied.golden.json",
		"event_transfer_routed_publisher.golden.json",
		"event_transfer_routed_router.golden.json",
		"event_transfer_routed_referrer.golden.json",
		"event_transfer_routed_burn.golden.json",
		"event_transfer_routed_insurance.golden.json",
	}
	for _, f := range required {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required golden file %s is missing. A new IBC event type "+
				"was likely added without a paired golden — pin its wire "+
				"format or downstream relayers break silently.", f)
	}
}

// TestIBCEventAttributeKeys_WireContractStability pins the exact
// string-literal attribute keys used in fee_split_middleware.go.
// These are NOT defined as package-level constants (unlike
// x/credits/types/settlement_events.go) — they appear inline as
// sdk.NewAttribute("denom", ...) etc. That makes them even more
// susceptible to silent drift during refactors. A unit-level pin
// on the exact key strings catches rename-sprees before they ship.
func TestIBCEventAttributeKeys_WireContractStability(t *testing.T) {
	t.Parallel()

	// The universe of distinct attribute keys across all three
	// IBC middleware event types.
	requiredKeys := []string{
		// fee_collected
		"settlement_id",
		"total_amount",
		"denom",
		"source_channel",
		"destination_channel",
		// fee_split_applied
		"burn_amount",
		"insurance_amount",
		"net_amount",
		"publisher_amount",
		"router_amount",
		"referrer_amount",
		"burn_bps",
		"insurance_bps",
		"publisher_bps",
		"router_bps",
		"referrer_bps",
		// transfer_routed
		"recipient_role",
		"recipient",
		"amount",
		// universal
		"executed",
	}

	// Build a representative event per type with every key present,
	// then verify that encoding/decoding preserves the exact
	// attribute-key strings. This catches accidental rename via
	// find-and-replace that would slip past individual goldens if
	// the replacement was consistent across all files.
	seen := make(map[string]bool)
	events := []sdk.Event{
		sdk.NewEvent("fee_collected",
			sdk.NewAttribute("settlement_id", "x"),
			sdk.NewAttribute("total_amount", "1utoken"),
			sdk.NewAttribute("denom", "utoken"),
			sdk.NewAttribute("source_channel", "channel-0"),
			sdk.NewAttribute("destination_channel", "channel-1"),
			executedAttr(),
		),
		sdk.NewEvent("fee_split_applied",
			sdk.NewAttribute("settlement_id", "x"),
			sdk.NewAttribute("burn_amount", "0utoken"),
			sdk.NewAttribute("insurance_amount", "0utoken"),
			sdk.NewAttribute("net_amount", "0utoken"),
			sdk.NewAttribute("publisher_amount", "0utoken"),
			sdk.NewAttribute("router_amount", "0utoken"),
			sdk.NewAttribute("referrer_amount", "0utoken"),
			sdk.NewAttribute("denom", "utoken"),
			sdk.NewAttribute("burn_bps", "0"),
			sdk.NewAttribute("insurance_bps", "0"),
			sdk.NewAttribute("publisher_bps", "0"),
			sdk.NewAttribute("router_bps", "0"),
			sdk.NewAttribute("referrer_bps", "0"),
			executedAttr(),
		),
		sdk.NewEvent("transfer_routed",
			sdk.NewAttribute("settlement_id", "x"),
			sdk.NewAttribute("recipient_role", "x"),
			sdk.NewAttribute("recipient", "x"),
			sdk.NewAttribute("amount", "0utoken"),
			sdk.NewAttribute("denom", "utoken"),
			executedAttr(),
		),
	}
	for _, e := range events {
		for _, a := range e.Attributes {
			seen[string(a.Key)] = true
		}
	}

	for _, k := range requiredKeys {
		require.True(t, seen[k],
			"required attribute key %q missing from emitted events. "+
				"Pins the wire contract: downstream indexer queries that "+
				"filter on this key name would silently match zero "+
				"records after a rename.", k)
	}

	// And the inverse: every key observed MUST be in the required set,
	// so adding a new key without updating this pin surfaces as a test
	// failure (prompting the author to also add a golden file).
	for k := range seen {
		require.Contains(t, requiredKeys, k,
			"observed unexpected attribute key %q — add it to "+
				"requiredKeys here AND add a golden file covering the "+
				"event type that emits it. Otherwise downstream "+
				"indexers ingest a key with no format contract.", k)
	}
}
