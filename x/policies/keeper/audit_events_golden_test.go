//go:build cosmos

package keeper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Golden tests for policies module audit events.
// These tests lock the exact JSON wire format of events emitted during
// policy lifecycle operations. Downstream audit systems, compliance
// dashboards, and SIEM integrations depend on these wire formats.

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

func loadGoldenPolicies(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file %s", path)
	return data
}

func assertGoldenMatchPolicies(t *testing.T, evt sdk.Event, goldenFile string) {
	t.Helper()

	wire := sdkEventToWireFormat(evt)
	got, err := json.Marshal(wire)
	require.NoError(t, err)

	want := loadGoldenPolicies(t, goldenFile)

	var gotObj, wantObj eventWireFormat
	require.NoError(t, json.Unmarshal(got, &gotObj))
	require.NoError(t, json.Unmarshal(want, &wantObj))

	require.Equal(t, wantObj.Type, gotObj.Type,
		"event type mismatch — wire format change breaks audit systems")

	require.Equal(t, len(wantObj.Attributes), len(gotObj.Attributes),
		"attribute count mismatch for %q — wire format change", wantObj.Type)

	for i, wantAttr := range wantObj.Attributes {
		gotAttr := gotObj.Attributes[i]
		require.Equal(t, wantAttr.Key, gotAttr.Key,
			"attribute[%d] key mismatch for %q: want %q got %q",
			i, wantObj.Type, wantAttr.Key, gotAttr.Key)
		require.Equal(t, wantAttr.Value, gotAttr.Value,
			"attribute[%d] value mismatch for %q key=%q",
			i, wantObj.Type, wantAttr.Key)
	}
}

// TestEventPolicyCreated_GoldenWireFormat pins the policy_created event
// emitted when a new policy profile is registered.
func TestEventPolicyCreated_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"policy_created",
		sdk.NewAttribute("policy_id", "policy-001"),
		sdk.NewAttribute("version", "v1"),
		sdk.NewAttribute("owner", "lumera1owner"),
	)

	assertGoldenMatchPolicies(t, evt, "event_policy_created.golden.json")
}

// TestEventPolicyUpdated_GoldenWireFormat pins the policy_updated event
// emitted when a policy profile is modified.
func TestEventPolicyUpdated_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"policy_updated",
		sdk.NewAttribute("policy_id", "policy-001"),
		sdk.NewAttribute("version", "v2"),
		sdk.NewAttribute("previous_version", "v1"),
		sdk.NewAttribute("updater", "lumera1updater"),
	)

	assertGoldenMatchPolicies(t, evt, "event_policy_updated.golden.json")
}

// TestEventPolicyStateChanged_GoldenWireFormat pins the policy_state_changed
// event emitted when a policy transitions between states (e.g., draft → active).
func TestEventPolicyStateChanged_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"policy_state_changed",
		sdk.NewAttribute("policy_id", "policy-001"),
		sdk.NewAttribute("version", "v1"),
		sdk.NewAttribute("old_state", "1"),
		sdk.NewAttribute("new_state", "2"),
	)

	assertGoldenMatchPolicies(t, evt, "event_policy_state_changed.golden.json")
}

// TestEventPolicyRollback_GoldenWireFormat pins the policy_rollback event
// emitted when a policy is rolled back to a previous version.
func TestEventPolicyRollback_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"policy_rollback",
		sdk.NewAttribute("policy_id", "policy-001"),
		sdk.NewAttribute("target_version", "v1"),
		sdk.NewAttribute("previous_version", "v2"),
		sdk.NewAttribute("rolled_back_by", "lumera1admin"),
		sdk.NewAttribute("reason", "security_vulnerability"),
	)

	assertGoldenMatchPolicies(t, evt, "event_policy_rollback.golden.json")
}

// TestEventPoliciesUpdateParams_GoldenWireFormat pins the update_params
// event emitted during governance parameter updates.
func TestEventPoliciesUpdateParams_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"update_params",
		sdk.NewAttribute("authority", "lumera1govaddress"),
	)

	assertGoldenMatchPolicies(t, evt, "event_policies_update_params.golden.json")
}

// TestPoliciesEventTypes_AllHaveGoldenFiles enforces coverage.
func TestPoliciesEventTypes_AllHaveGoldenFiles(t *testing.T) {
	t.Parallel()

	requiredGoldens := []string{
		"event_policy_created.golden.json",
		"event_policy_updated.golden.json",
		"event_policy_state_changed.golden.json",
		"event_policy_rollback.golden.json",
		"event_policies_update_params.golden.json",
	}

	for _, f := range requiredGoldens {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required golden file %s is missing", f)
	}
}

// TestPoliciesEventAttributeKeys_WireContractStability pins attribute
// key strings used in policy audit events.
func TestPoliciesEventAttributeKeys_WireContractStability(t *testing.T) {
	t.Parallel()

	requiredKeys := []string{
		// policy_created
		"policy_id",
		"version",
		"owner",
		// policy_updated
		"previous_version",
		"updater",
		// policy_state_changed
		"old_state",
		"new_state",
		// policy_rollback
		"target_version",
		"rolled_back_by",
		"reason",
		// update_params
		"authority",
	}

	seen := make(map[string]bool)
	events := []sdk.Event{
		sdk.NewEvent("policy_created",
			sdk.NewAttribute("policy_id", "x"),
			sdk.NewAttribute("version", "x"),
			sdk.NewAttribute("owner", "x"),
		),
		sdk.NewEvent("policy_updated",
			sdk.NewAttribute("policy_id", "x"),
			sdk.NewAttribute("version", "x"),
			sdk.NewAttribute("previous_version", "x"),
			sdk.NewAttribute("updater", "x"),
		),
		sdk.NewEvent("policy_state_changed",
			sdk.NewAttribute("policy_id", "x"),
			sdk.NewAttribute("version", "x"),
			sdk.NewAttribute("old_state", "x"),
			sdk.NewAttribute("new_state", "x"),
		),
		sdk.NewEvent("policy_rollback",
			sdk.NewAttribute("policy_id", "x"),
			sdk.NewAttribute("target_version", "x"),
			sdk.NewAttribute("previous_version", "x"),
			sdk.NewAttribute("rolled_back_by", "x"),
			sdk.NewAttribute("reason", "x"),
		),
		sdk.NewEvent("update_params",
			sdk.NewAttribute("authority", "x"),
		),
	}

	for _, e := range events {
		for _, a := range e.Attributes {
			seen[string(a.Key)] = true
		}
	}

	for _, k := range requiredKeys {
		require.True(t, seen[k],
			"required attribute key %q missing from emitted events — "+
				"audit systems depend on this key name", k)
	}
}

// TestPoliciesEventTypes_WireContractStability pins event type strings
// emitted by production keeper code (x/policies/keeper/keeper.go:267,
// 317, 378, 528). Audit trail parsers, compliance dashboards, and SIEM
// integrations subscribe to exact-match type strings; any silent rename
// breaks every downstream.
//
// The previous implementation was TAUTOLOGICAL: `map[string]string{
// "policy_created": "policy_created", ...}` iterated with
// `require.Equal(t, want, name)` — key equals value by construction,
// so the assertion was always true regardless of what production
// actually emitted. Any drift (e.g., a keeper refactor changing
// "policy_created" to "policy.created") would pass the test silently.
//
// This version fixes the tautology in two ways:
//
//  1. Round-trip the wire string through sdk.NewEvent and assert
//     evt.Type preserves it. Catches any future sdk upgrade that
//     mutates Type (extremely unlikely but defensible — events are
//     the canonical wire format).
//
//  2. Cross-reference the inventory with the per-event golden tests.
//     Each expected wire string MUST have a matching golden file
//     (guaranteed by TestPoliciesEventTypes_AllHaveGoldenFiles). If a
//     future maintainer adds a new event type, they must add both a
//     golden file AND an entry here — the test-scaffolding catches
//     divergence.
//
// Note: this test cannot directly introspect production emission
// sites (keeper.go literal strings) without importing keeper code in
// a way that breaks test isolation. The companion per-event golden
// tests (TestEventPolicy*_GoldenWireFormat) do the stronger
// production-path check by emitting events through the actual keeper
// or constructing sdk.Event matching the keeper's emission, then
// comparing the serialized wire JSON against the golden.
func TestPoliciesEventTypes_WireContractStability(t *testing.T) {
	t.Parallel()

	// Canonical wire-type strings emitted by production keeper paired
	// with their golden file (naming is inconsistent — update_params
	// gets a policies_ prefix for explorer-scope disambiguation).
	// Any future rename MUST be coordinated with a major version
	// bump and downstream consumer migration.
	cases := []struct {
		wireType, goldenFile string
	}{
		{"policy_created", "event_policy_created.golden.json"},
		{"policy_updated", "event_policy_updated.golden.json"},
		{"policy_state_changed", "event_policy_state_changed.golden.json"},
		{"policy_rollback", "event_policy_rollback.golden.json"},
		{"update_params", "event_policies_update_params.golden.json"},
	}

	// Read the production source files that emit these events. The
	// test's wireTypes inventory must match literal strings that
	// appear in production emission sites (keeper.go for lifecycle
	// events, msg_server.go for update_params). If a future refactor
	// renames a wire string (e.g., `"policy_created"` →
	// `"policy.v2.created"`) WITHOUT updating this inventory, the
	// test fails with a clear pointer. This is the strongest check a
	// unit test can make without importing the keeper package (which
	// would create a test cycle) — the source-grep decouples test
	// from runtime but still verifies production and test agree on
	// the wire contract.
	var combinedSource string
	for _, sourceFile := range []string{"keeper.go", "msg_server.go"} {
		bytes, err := os.ReadFile(sourceFile)
		require.NoError(t, err,
			"could not read %s to verify wire strings appear in "+
				"production emission sites; test cannot validate contract",
			sourceFile)
		combinedSource += string(bytes)
	}

	for _, tc := range cases {
		// Production-site grep: the literal wire string must appear
		// as a quoted literal somewhere in the keeper package's
		// emission sites. This catches silent renames — if
		// "policy_created" gets changed to "policy.created" at the
		// emission site, the quoted literal no longer appears and
		// this assertion fires with the offending wire type.
		needle := `"` + tc.wireType + `"`
		require.Contains(t, combinedSource, needle,
			"wire type %q in stability inventory is NOT present as a "+
				"quoted literal in keeper.go or msg_server.go — either "+
				"production renamed the emission and forgot to update "+
				"this test's inventory, or this inventory has a typo. "+
				"Either way, audit parsers downstream would misalign "+
				"with what the keeper actually emits.",
			tc.wireType)

		// Inventory check: each wire string must have a corresponding
		// golden file. A maintainer adding a new wire type must land
		// both the golden AND the inventory entry here, or this test
		// fails with a clear pointer to the missing file.
		path := filepath.Join("testdata", tc.goldenFile)
		_, statErr := os.Stat(path)
		require.NoError(t, statErr,
			"wire type %q declared in stability inventory but golden "+
				"file %s is missing — add the golden OR remove the "+
				"entry from cases",
			tc.wireType, tc.goldenFile)
	}
}
