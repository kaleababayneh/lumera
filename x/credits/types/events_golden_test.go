
package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Golden tests for credits module event wire formats.
// These tests lock the exact JSON structure that downstream indexers,
// explorers, and analytics pipelines depend on. Any field rename or
// structural change breaks those integrations.

// eventWireFormat mirrors sdk.Event JSON structure for golden comparison.
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

func loadGolden(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file %s", path)
	return data
}

func assertGoldenMatch(t *testing.T, evt sdk.Event, goldenFile string) {
	t.Helper()

	wire := sdkEventToWireFormat(evt)
	got, err := json.Marshal(wire)
	require.NoError(t, err)

	want := loadGolden(t, goldenFile)

	var gotObj, wantObj eventWireFormat
	require.NoError(t, json.Unmarshal(got, &gotObj))
	require.NoError(t, json.Unmarshal(want, &wantObj))

	require.Equal(t, wantObj.Type, gotObj.Type,
		"event type mismatch — wire format change breaks downstream indexers")

	require.Equal(t, len(wantObj.Attributes), len(gotObj.Attributes),
		"attribute count mismatch — wire format change")

	for i, wantAttr := range wantObj.Attributes {
		gotAttr := gotObj.Attributes[i]
		require.Equal(t, wantAttr.Key, gotAttr.Key,
			"attribute[%d] key mismatch — wire format change", i)
		require.Equal(t, wantAttr.Value, gotAttr.Value,
			"attribute[%d] value mismatch for key %q", i, wantAttr.Key)
	}
}

// TestEventSettlement_GoldenWireFormat pins the settlement event wire format.
func TestEventSettlement_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		EventTypeSettlement,
		sdk.NewAttribute(AttributeKeySettlementID, "settle-001"),
		sdk.NewAttribute(AttributeKeyToolID, "tp.test-tool"),
		sdk.NewAttribute(AttributeKeyPublisher, "lumera1publisher"),
		sdk.NewAttribute(AttributeKeyUser, "lumera1user"),
		sdk.NewAttribute(AttributeKeyAmount, "1000000ulac"),
		sdk.NewAttribute(AttributeKeyBurnAmount, "30000ulac"),
		sdk.NewAttribute(AttributeKeyStatus, "completed"),
	)

	assertGoldenMatch(t, evt, "event_settlement.golden.json")
}

// TestEventBurn_GoldenWireFormat pins the LAC burn event wire format.
func TestEventBurn_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		EventTypeBurn,
		sdk.NewAttribute(AttributeKeySettlementID, "settle-001"),
		sdk.NewAttribute(AttributeKeyBurnAmount, "30000ulac"),
		sdk.NewAttribute(AttributeKeyReason, "settlement burn"),
	)

	assertGoldenMatch(t, evt, "event_lac_burn.golden.json")
}

// TestEventCreditLock_GoldenWireFormat pins the credit lock event wire format.
func TestEventCreditLock_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		EventTypeLock,
		sdk.NewAttribute(AttributeKeyLockID, "lock-001"),
		sdk.NewAttribute(AttributeKeyRouter, "lumera1router"),
		sdk.NewAttribute(AttributeKeySessionID, "sess-001"),
		sdk.NewAttribute(AttributeKeyToolID, "tp.test-tool"),
		sdk.NewAttribute(AttributeKeyAmount, "1000000ulac"),
		sdk.NewAttribute(AttributeKeyExpiresAt, "2023-11-14T23:13:20Z"),
	)

	assertGoldenMatch(t, evt, "event_credit_lock.golden.json")
}

// TestEventCreditUnlock_GoldenWireFormat pins the credit unlock event wire format.
func TestEventCreditUnlock_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		EventTypeUnlock,
		sdk.NewAttribute(AttributeKeyLockID, "lock-001"),
		sdk.NewAttribute(AttributeKeyAmount, "950000ulac"),
		sdk.NewAttribute(AttributeKeyReason, "settlement refund"),
	)

	assertGoldenMatch(t, evt, "event_credit_unlock.golden.json")
}

// TestEventDispute_GoldenWireFormat pins the settlement dispute event wire format.
func TestEventDispute_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		EventTypeDispute,
		sdk.NewAttribute(AttributeKeyDisputeID, "dispute-001"),
		sdk.NewAttribute(AttributeKeySettlementID, "settle-001"),
		sdk.NewAttribute(AttributeKeyUser, "lumera1challenger"),
		sdk.NewAttribute(AttributeKeyReason, "invalid receipt"),
		sdk.NewAttribute(AttributeKeyStatus, "pending"),
	)

	assertGoldenMatch(t, evt, "event_settlement_dispute.golden.json")
}

// TestEventDistribute_GoldenWireFormat pins the revenue distribution event wire format.
func TestEventDistribute_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		EventTypeDistribute,
		sdk.NewAttribute(AttributeKeySettlementID, "settle-001"),
		sdk.NewAttribute(AttributeKeyPublisher, "lumera1publisher"),
		sdk.NewAttribute(AttributeKeyAmount, "900000ulac"),
	)

	assertGoldenMatch(t, evt, "event_revenue_distribute.golden.json")
}

// TestEventSwap_GoldenWireFormat pins the LUME-LAC swap event wire format.
func TestEventSwap_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		EventTypeSwap,
		sdk.NewAttribute(AttributeKeyUser, "lumera1user"),
		sdk.NewAttribute(AttributeKeyAmount, "1000000ulume"),
		sdk.NewAttribute(AttributeKeySwapRate, "1.0"),
	)

	assertGoldenMatch(t, evt, "event_lume_lac_swap.golden.json")
}

// TestEventTypes_AllHaveGoldenFiles verifies coverage.
func TestEventTypes_AllHaveGoldenFiles(t *testing.T) {
	t.Parallel()

	eventTypeToGolden := map[string]string{
		EventTypeSettlement: "event_settlement.golden.json",
		EventTypeBurn:       "event_lac_burn.golden.json",
		EventTypeLock:       "event_credit_lock.golden.json",
		EventTypeUnlock:     "event_credit_unlock.golden.json",
		EventTypeDispute:    "event_settlement_dispute.golden.json",
		EventTypeDistribute: "event_revenue_distribute.golden.json",
		EventTypeSwap:       "event_lume_lac_swap.golden.json",
	}

	for eventType, goldenFile := range eventTypeToGolden {
		path := filepath.Join("testdata", goldenFile)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"event type %q missing golden file %s", eventType, goldenFile)
	}
}

// TestAttributeKeys_WireContractStability pins attribute key wire values.
func TestAttributeKeys_WireContractStability(t *testing.T) {
	t.Parallel()

	wantKeys := map[string]string{
		"AttributeKeySettlementID": "settlement_id",
		"AttributeKeyToolID":       "tool_id",
		"AttributeKeyPublisher":    "publisher",
		"AttributeKeyUser":         "user",
		"AttributeKeyAmount":       "amount",
		"AttributeKeyBurnAmount":   "burn_amount",
		"AttributeKeyStatus":       "status",
		"AttributeKeyLockID":       "lock_id",
		"AttributeKeyDisputeID":    "dispute_id",
		"AttributeKeySwapRate":     "swap_rate",
		"AttributeKeyRouter":       "router",
		"AttributeKeySessionID":    "session_id",
		"AttributeKeyReason":       "reason",
		"AttributeKeyExpiresAt":    "expires_at",
		"AttributeKeyToolpackID":   "toolpack_id",
	}

	actualKeys := map[string]string{
		"AttributeKeySettlementID": AttributeKeySettlementID,
		"AttributeKeyToolID":       AttributeKeyToolID,
		"AttributeKeyPublisher":    AttributeKeyPublisher,
		"AttributeKeyUser":         AttributeKeyUser,
		"AttributeKeyAmount":       AttributeKeyAmount,
		"AttributeKeyBurnAmount":   AttributeKeyBurnAmount,
		"AttributeKeyStatus":       AttributeKeyStatus,
		"AttributeKeyLockID":       AttributeKeyLockID,
		"AttributeKeyDisputeID":    AttributeKeyDisputeID,
		"AttributeKeySwapRate":     AttributeKeySwapRate,
		"AttributeKeyRouter":       AttributeKeyRouter,
		"AttributeKeySessionID":    AttributeKeySessionID,
		"AttributeKeyReason":       AttributeKeyReason,
		"AttributeKeyExpiresAt":    AttributeKeyExpiresAt,
		"AttributeKeyToolpackID":   AttributeKeyToolpackID,
	}

	for constName, want := range wantKeys {
		got := actualKeys[constName]
		require.Equal(t, want, got,
			"%s wire value changed from %q to %q — breaks downstream parsers",
			constName, want, got)
	}
}

// TestEventTypes_WireContractStability pins event type wire values.
func TestEventTypes_WireContractStability(t *testing.T) {
	t.Parallel()

	wantTypes := map[string]string{
		"EventTypeSettlement": "settlement",
		"EventTypeBurn":       "lac_burn",
		"EventTypeDistribute": "revenue_distribute",
		"EventTypeLock":       "credit_lock",
		"EventTypeUnlock":     "credit_unlock",
		"EventTypeDispute":    "settlement_dispute",
		"EventTypeSwap":       "lume_lac_swap",
	}

	actualTypes := map[string]string{
		"EventTypeSettlement": EventTypeSettlement,
		"EventTypeBurn":       EventTypeBurn,
		"EventTypeDistribute": EventTypeDistribute,
		"EventTypeLock":       EventTypeLock,
		"EventTypeUnlock":     EventTypeUnlock,
		"EventTypeDispute":    EventTypeDispute,
		"EventTypeSwap":       EventTypeSwap,
	}

	for constName, want := range wantTypes {
		got := actualTypes[constName]
		require.Equal(t, want, got,
			"%s wire value changed from %q to %q — breaks downstream parsers",
			constName, want, got)
	}
}
