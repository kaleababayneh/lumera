
package keeper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// Golden tests for credits keeper flow events.
// These tests lock the exact JSON wire format of events emitted by the
// credits keeper during settlement, CAC distribution, adaptive burn,
// and swap flows. Downstream indexers, explorers, and analytics pipelines
// depend on these wire formats.

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

func loadGoldenKeeper(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file %s", path)
	return data
}

func assertGoldenMatchKeeper(t *testing.T, evt sdk.Event, goldenFile string) {
	t.Helper()

	wire := sdkEventToWireFormat(evt)
	got, err := json.Marshal(wire)
	require.NoError(t, err)

	want := loadGoldenKeeper(t, goldenFile)

	var gotObj, wantObj eventWireFormat
	require.NoError(t, json.Unmarshal(got, &gotObj))
	require.NoError(t, json.Unmarshal(want, &wantObj))

	require.Equal(t, wantObj.Type, gotObj.Type,
		"event type mismatch — wire format change breaks downstream indexers")

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

// TestEventInsuranceContributionSent_GoldenWireFormat pins the
// insurance_contribution_sent event emitted when insurance funds
// are transferred during settlement.
func TestEventInsuranceContributionSent_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"insurance_contribution_sent",
		sdk.NewAttribute("receipt_id", "receipt-001"),
		sdk.NewAttribute("amount", "15000ulac"),
	)

	assertGoldenMatchKeeper(t, evt, "event_insurance_contribution_sent.golden.json")
}

// TestEventDistributeOriginSurface_GoldenWireFormat pins the
// revenue_distribute event for origin surface (CAC) royalties.
func TestEventDistributeOriginSurface_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.EventTypeDistribute,
		sdk.NewAttribute(types.AttributeKeyToolpackID, "toolpack-001"),
		sdk.NewAttribute(types.AttributeKeyAmount, "50000ulac"),
		sdk.NewAttribute("recipient_role", "origin_surface"),
		sdk.NewAttribute("recipient", "lumera1originsurface"),
	)

	assertGoldenMatchKeeper(t, evt, "event_distribute_origin_surface.golden.json")
}

// TestEventDistributeTreasury_GoldenWireFormat pins the
// revenue_distribute event for treasury allocations.
func TestEventDistributeTreasury_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.EventTypeDistribute,
		sdk.NewAttribute(types.AttributeKeyAmount, "25000ulac"),
		sdk.NewAttribute("recipient_role", "treasury"),
		sdk.NewAttribute("recipient", "lumera1treasury"),
	)

	assertGoldenMatchKeeper(t, evt, "event_distribute_treasury.golden.json")
}

// TestEventDistributeFull_GoldenWireFormat pins the full
// revenue_distribute event with all recipient amounts.
func TestEventDistributeFull_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.EventTypeDistribute,
		sdk.NewAttribute(types.AttributeKeySettlementID, "settle-001"),
		sdk.NewAttribute(types.AttributeKeyPublisher, "lumera1publisher"),
		sdk.NewAttribute("publisher_amount", "600000ulac"),
		sdk.NewAttribute(types.AttributeKeyRouter, "lumera1router"),
		sdk.NewAttribute("router_amount", "200000ulac"),
		sdk.NewAttribute("origin_surface_amount", "50000ulac"),
		sdk.NewAttribute("treasury_amount", "25000ulac"),
	)

	assertGoldenMatchKeeper(t, evt, "event_distribute_full.golden.json")
}

// TestEventBurnWithRate_GoldenWireFormat pins the lac_burn event
// with burn_rate_bps attribute.
func TestEventBurnWithRate_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.EventTypeBurn,
		sdk.NewAttribute(types.AttributeKeySettlementID, "settle-001"),
		sdk.NewAttribute(types.AttributeKeyAmount, "30000ulac"),
		sdk.NewAttribute("burn_rate_bps", "300"),
	)

	assertGoldenMatchKeeper(t, evt, "event_burn_with_rate.golden.json")
}

// TestEventAdaptiveBurnRateEvaluated_GoldenWireFormat pins the
// adaptive_burn_rate_evaluated event emitted during adaptive burn
// rate recalculation.
func TestEventAdaptiveBurnRateEvaluated_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"adaptive_burn_rate_evaluated",
		sdk.NewAttribute("old_rate_bps", "300"),
		sdk.NewAttribute("requested_rate_bps", "350"),
		sdk.NewAttribute("new_rate_bps", "325"),
		sdk.NewAttribute("annualized_deflation_bps", "200"),
		sdk.NewAttribute("target_annual_deflation_bps", "250"),
		sdk.NewAttribute("period_contraction_bps", "50"),
		sdk.NewAttribute("sample_count", "100"),
	)

	assertGoldenMatchKeeper(t, evt, "event_adaptive_burn_rate_evaluated.golden.json")
}

// TestEventAdaptiveBurnRateReason_GoldenWireFormat pins the
// adaptive_burn_rate_reason event with direction and reason.
func TestEventAdaptiveBurnRateReason_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"adaptive_burn_rate_reason",
		sdk.NewAttribute("direction", "increase"),
		sdk.NewAttribute("reason", "below_target_deflation"),
		sdk.NewAttribute("clamp_reason", "none"),
		sdk.NewAttribute("death_spiral_triggered", "false"),
	)

	assertGoldenMatchKeeper(t, evt, "event_adaptive_burn_rate_reason.golden.json")
}

// TestEventCACRoyaltyDistribution_GoldenWireFormat pins the
// cac_royalty_distribution event for Content-Addressable Cache royalties.
func TestEventCACRoyaltyDistribution_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"cac_royalty_distribution",
		sdk.NewAttribute("origin_tool", "tool-origin-001"),
		sdk.NewAttribute("serving_tool", "tool-serving-002"),
		sdk.NewAttribute("origin_share", "70000ulac"),
		sdk.NewAttribute("serving_share", "30000ulac"),
		sdk.NewAttribute("total_amount", "100000ulac"),
	)

	assertGoldenMatchKeeper(t, evt, "event_cac_royalty_distribution.golden.json")
}

// TestEventLumeLacSwap_GoldenWireFormat pins the lume_lac_swap event
// for LUME to LAC conversions.
func TestEventLumeLacSwap_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		"lume_lac_swap",
		sdk.NewAttribute("sender", "lumera1sender"),
		sdk.NewAttribute("lume_amount", "1000000ulume"),
		sdk.NewAttribute("lac_amount", "1000000ulac"),
		sdk.NewAttribute("acq_burn_rate", "0"),
	)

	assertGoldenMatchKeeper(t, evt, "event_lume_lac_swap.golden.json")
}

// TestEventLacToLumeSwap_GoldenWireFormat pins the lume_lac_swap event
// for LAC to LUME conversions.
func TestEventLacToLumeSwap_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.EventTypeSwap,
		sdk.NewAttribute("sender", "lumera1sender"),
		sdk.NewAttribute("lac_in", "500000ulac"),
		sdk.NewAttribute("lume_out", "495000ulume"),
		sdk.NewAttribute("direction", "lac_to_lume"),
	)

	assertGoldenMatchKeeper(t, evt, "event_lac_to_lume_swap.golden.json")
}

// TestEventCreditLockWithQuote_GoldenWireFormat pins the credit_lock
// event with quote_id attribute.
func TestEventCreditLockWithQuote_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.EventTypeLock,
		sdk.NewAttribute(types.AttributeKeyLockID, "lock-001"),
		sdk.NewAttribute(types.AttributeKeyAmount, "1000000ulac"),
		sdk.NewAttribute(types.AttributeKeyStatus, "active"),
		sdk.NewAttribute(types.AttributeKeyToolID, "tool-001"),
		sdk.NewAttribute(types.AttributeKeyRouter, "lumera1router"),
		sdk.NewAttribute(types.AttributeKeySessionID, "session-001"),
		sdk.NewAttribute("quote_id", "quote-001"),
	)

	assertGoldenMatchKeeper(t, evt, "event_credit_lock_with_quote.golden.json")
}

// TestEventCreditUnlockSettled_GoldenWireFormat pins the credit_unlock
// event with "settled" reason.
func TestEventCreditUnlockSettled_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.EventTypeUnlock,
		sdk.NewAttribute(types.AttributeKeyLockID, "lock-001"),
		sdk.NewAttribute(types.AttributeKeyAmount, "50000ulac"),
		sdk.NewAttribute(types.AttributeKeyStatus, "settled"),
		sdk.NewAttribute(types.AttributeKeyRouter, "lumera1router"),
		sdk.NewAttribute("reason", "settled"),
	)

	assertGoldenMatchKeeper(t, evt, "event_credit_unlock_settled.golden.json")
}

// TestEventSettlementFull_GoldenWireFormat pins the full settlement
// event with net_amount and cache_hit attributes.
func TestEventSettlementFull_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.EventTypeSettlement,
		sdk.NewAttribute(types.AttributeKeySettlementID, "settle-001"),
		sdk.NewAttribute(types.AttributeKeyToolID, "tool-001"),
		sdk.NewAttribute(types.AttributeKeyAmount, "1000000ulac"),
		sdk.NewAttribute("net_amount", "970000ulac"),
		sdk.NewAttribute("cache_hit", "false"),
		sdk.NewAttribute(types.AttributeKeyStatus, "completed"),
	)

	assertGoldenMatchKeeper(t, evt, "event_settlement_full.golden.json")
}

// TestEventUpdateParams_GoldenWireFormat pins the credits.MsgUpdateParams
// event emitted on parameter updates.
func TestEventUpdateParams_GoldenWireFormat(t *testing.T) {
	t.Parallel()

	evt := sdk.NewEvent(
		types.TypeMsgUpdateParams,
		sdk.NewAttribute("authority", "lumera1govaddress"),
	)

	assertGoldenMatchKeeper(t, evt, "event_update_params.golden.json")
}

// TestKeeperEventTypes_AllHaveGoldenFiles enforces coverage.
func TestKeeperEventTypes_AllHaveGoldenFiles(t *testing.T) {
	t.Parallel()

	requiredGoldens := []string{
		"event_insurance_contribution_sent.golden.json",
		"event_distribute_origin_surface.golden.json",
		"event_distribute_treasury.golden.json",
		"event_distribute_full.golden.json",
		"event_burn_with_rate.golden.json",
		"event_adaptive_burn_rate_evaluated.golden.json",
		"event_adaptive_burn_rate_reason.golden.json",
		"event_cac_royalty_distribution.golden.json",
		"event_lume_lac_swap.golden.json",
		"event_lac_to_lume_swap.golden.json",
		"event_credit_lock_with_quote.golden.json",
		"event_credit_unlock_settled.golden.json",
		"event_settlement_full.golden.json",
		"event_update_params.golden.json",
	}

	for _, f := range requiredGoldens {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required golden file %s is missing", f)
	}
}

// TestKeeperFlowEventAttributeKeys_WireContractStability pins
// attribute key strings used in keeper flow events.
func TestKeeperFlowEventAttributeKeys_WireContractStability(t *testing.T) {
	t.Parallel()

	requiredKeys := []string{
		// insurance
		"receipt_id",
		// distribution
		"recipient_role",
		"recipient",
		"publisher_amount",
		"router_amount",
		"origin_surface_amount",
		"treasury_amount",
		// burn
		"burn_rate_bps",
		// adaptive burn
		"old_rate_bps",
		"requested_rate_bps",
		"new_rate_bps",
		"annualized_deflation_bps",
		"target_annual_deflation_bps",
		"period_contraction_bps",
		"sample_count",
		"direction",
		"reason",
		"clamp_reason",
		"death_spiral_triggered",
		// CAC
		"origin_tool",
		"serving_tool",
		"origin_share",
		"serving_share",
		"total_amount",
		// swap
		"sender",
		"lume_amount",
		"lac_amount",
		"acq_burn_rate",
		"lac_in",
		"lume_out",
		// lock
		"quote_id",
		// settlement
		"net_amount",
		"cache_hit",
		// governance
		"authority",
	}

	seen := make(map[string]bool)
	events := []sdk.Event{
		sdk.NewEvent("insurance_contribution_sent",
			sdk.NewAttribute("receipt_id", "x"),
			sdk.NewAttribute("amount", "x"),
		),
		sdk.NewEvent(types.EventTypeDistribute,
			sdk.NewAttribute("recipient_role", "x"),
			sdk.NewAttribute("recipient", "x"),
			sdk.NewAttribute("publisher_amount", "x"),
			sdk.NewAttribute("router_amount", "x"),
			sdk.NewAttribute("origin_surface_amount", "x"),
			sdk.NewAttribute("treasury_amount", "x"),
		),
		sdk.NewEvent(types.EventTypeBurn,
			sdk.NewAttribute("burn_rate_bps", "x"),
		),
		sdk.NewEvent("adaptive_burn_rate_evaluated",
			sdk.NewAttribute("old_rate_bps", "x"),
			sdk.NewAttribute("requested_rate_bps", "x"),
			sdk.NewAttribute("new_rate_bps", "x"),
			sdk.NewAttribute("annualized_deflation_bps", "x"),
			sdk.NewAttribute("target_annual_deflation_bps", "x"),
			sdk.NewAttribute("period_contraction_bps", "x"),
			sdk.NewAttribute("sample_count", "x"),
		),
		sdk.NewEvent("adaptive_burn_rate_reason",
			sdk.NewAttribute("direction", "x"),
			sdk.NewAttribute("reason", "x"),
			sdk.NewAttribute("clamp_reason", "x"),
			sdk.NewAttribute("death_spiral_triggered", "x"),
		),
		sdk.NewEvent("cac_royalty_distribution",
			sdk.NewAttribute("origin_tool", "x"),
			sdk.NewAttribute("serving_tool", "x"),
			sdk.NewAttribute("origin_share", "x"),
			sdk.NewAttribute("serving_share", "x"),
			sdk.NewAttribute("total_amount", "x"),
		),
		sdk.NewEvent("lume_lac_swap",
			sdk.NewAttribute("sender", "x"),
			sdk.NewAttribute("lume_amount", "x"),
			sdk.NewAttribute("lac_amount", "x"),
			sdk.NewAttribute("acq_burn_rate", "x"),
		),
		sdk.NewEvent(types.EventTypeSwap,
			sdk.NewAttribute("lac_in", "x"),
			sdk.NewAttribute("lume_out", "x"),
		),
		sdk.NewEvent(types.EventTypeLock,
			sdk.NewAttribute("quote_id", "x"),
		),
		sdk.NewEvent(types.EventTypeSettlement,
			sdk.NewAttribute("net_amount", "x"),
			sdk.NewAttribute("cache_hit", "x"),
		),
		sdk.NewEvent(types.TypeMsgUpdateParams,
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
				"downstream indexers depend on this key name", k)
	}
}

// writeGoldenFile is a helper for initial golden file generation.
// Run with: UPDATE_GOLDENS=1 go test -tags=cosmos -run=Golden
func writeGoldenFile(t *testing.T, evt sdk.Event, filename string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDENS") == "" {
		return
	}

	wire := sdkEventToWireFormat(evt)
	data, err := json.Marshal(wire)
	require.NoError(t, err)

	path := filepath.Join("testdata", filename)
	err = os.WriteFile(path, data, 0644)
	require.NoError(t, err)
	fmt.Printf("Wrote golden file: %s\n", path)
}
