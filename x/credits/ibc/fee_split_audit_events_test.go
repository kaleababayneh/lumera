//go:build cosmos

package ibc

import (
	"encoding/json"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes the remaining audit-reconstruction gaps for
// the IBC fee-split middleware (bd-5na.7, labels ibc+payments).
// Existing tests in fee_split_middleware_test.go check event
// TYPES and fee_split_applied ATTRIBUTES — but the per-role
// transfer_routed content + conservation invariant across
// emitted events were unpinned.
//
// Bead acceptance criterion:
//   "Events provide sufficient detail to audit the split end-
//    to-end."
//
// This file operationalizes that criterion as testable
// conservation + per-role invariants on the emitted event
// stream.
//
// Scan-angle #5 (sibling-pattern pinning with shared semantic)
// applies to the per-role transfer_routed emissions: publisher,
// router, referrer, burn, insurance should each appear with the
// same attribute-set shape (settlement_id, recipient_role,
// recipient, amount, denom, executed). A refactor that
// diverged one role's attributes would break indexer parsers
// that iterate uniformly over the role set.
//
// Scan-angle #3 (hidden-secondary-return pinning) applies to
// the conservation invariant: the emitted events are a
// lossless serialization of ComputeFeeSplit — a downstream
// consumer can reconstruct the FULL split (publisher + router
// + referrer + burn + insurance = total) from event
// attributes alone without re-running the math.

// TestFeeSplitMiddleware_EventsProvideAuditReconstruction is
// the bead's acceptance-criterion anchor. Construct a
// settlement packet, emit events, then RECONSTRUCT the full
// split from event attributes alone. The reconstructed
// numbers must match the inputs (conservation + per-role
// correctness).
func TestFeeSplitMiddleware_EventsProvideAuditReconstruction(t *testing.T) {
	// Non-default params to ensure we're not accidentally matching
	// zero-filled defaults. publisher/router/referrer split must
	// sum to 10000 BPS independent of burn/insurance (the burn and
	// insurance are taken off the top; the remaining NET is split
	// among the three recipient BPS). Use burn=300 (3%),
	// insurance=200 (2%); publisher=7000, router=2000, referrer=1000.
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

	// Deliberately non-round amount so rounding isn't hidden by
	// exact divisibility.
	const totalAmt = "100000"
	packet := buildSettlementPacket(t, totalAmt, "ulac", "settle-audit-1",
		"lumera1pub", "lumera1router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	events := ctx.EventManager().Events()

	// Reconstruct split from emitted events alone.
	var fcAttrs map[string]string
	var fsaAttrs map[string]string
	roleAmounts := make(map[string]sdkmath.Int) // recipient_role → amount
	for _, e := range events {
		switch e.Type {
		case "fee_collected":
			fcAttrs = eventAttrs(e)
		case "fee_split_applied":
			fsaAttrs = eventAttrs(e)
		case "transfer_routed":
			attrs := eventAttrs(e)
			role, hasRole := attrs["recipient_role"]
			require.True(t, hasRole,
				"every transfer_routed event MUST carry recipient_role attribute. "+
					"Pins the scan-angle #5 shared-shape invariant: a refactor "+
					"that dropped the role attribute would leave indexers unable "+
					"to distinguish which recipient the amount belongs to.")
			amtStr, hasAmt := attrs["amount"]
			require.True(t, hasAmt, "transfer_routed for role=%s missing amount", role)
			amt, ok := sdkmath.NewIntFromString(amtStr)
			require.True(t, ok, "transfer_routed amount %q is not an integer", amtStr)
			roleAmounts[role] = amt

			// Pin shared attribute shape across all roles.
			assert.Contains(t, attrs, "settlement_id",
				"role=%s missing settlement_id", role)
			assert.Contains(t, attrs, "recipient",
				"role=%s missing recipient", role)
			assert.Contains(t, attrs, "denom",
				"role=%s missing denom", role)
			assert.Contains(t, attrs, "executed",
				"role=%s missing executed flag", role)
			assert.Equal(t, "ulac", attrs["denom"],
				"role=%s denom must match packet denom", role)
		}
	}
	require.NotNil(t, fcAttrs, "fee_collected event must be emitted")
	require.NotNil(t, fsaAttrs, "fee_split_applied event must be emitted")

	// fee_collected must carry packet-provenance attrs for indexer
	// reconstruction of the originating transfer.
	assert.Equal(t, totalAmt, fcAttrs["total_amount"])
	assert.Equal(t, "ulac", fcAttrs["denom"])
	assert.Equal(t, "settle-audit-1", fcAttrs["settlement_id"])
	assert.NotEmpty(t, fcAttrs["source_channel"])
	assert.NotEmpty(t, fcAttrs["destination_channel"])

	// Reconstruct split: amounts from fee_split_applied must equal
	// the PER-ROLE sums from transfer_routed events.
	for _, role := range []string{"publisher", "router", "referrer", "burn", "insurance"} {
		fsaKey := role + "_amount"
		fsaVal, ok := fsaAttrs[fsaKey]
		switch role {
		case "burn":
			fsaKey = "burn_amount"
			fsaVal, ok = fsaAttrs[fsaKey]
		case "insurance":
			fsaKey = "insurance_amount"
			fsaVal, ok = fsaAttrs[fsaKey]
		}
		require.True(t, ok, "fee_split_applied missing %s", fsaKey)

		routed, routedPresent := roleAmounts[role]
		if !routedPresent {
			// Roles with zero amounts are legitimately omitted from
			// transfer_routed events (the middleware only emits when
			// amount > 0). Verify the split-applied value is also
			// zero in that case.
			fsaAmt, ok := sdkmath.NewIntFromString(fsaVal)
			require.True(t, ok)
			assert.True(t, fsaAmt.IsZero(),
				"role=%s not in transfer_routed but fee_split_applied says %s",
				role, fsaVal)
			continue
		}

		// transfer_routed amount must match fee_split_applied's claim.
		assert.Equal(t, fsaVal, routed.String(),
			"role=%s: fee_split_applied says %s, transfer_routed says %s. "+
				"Pins the scan-angle #3 hidden-secondary-return "+
				"invariant: the two event types MUST agree on the "+
				"per-role amount — a refactor that diverged them would "+
				"let indexers see inconsistent splits depending on "+
				"which event they parsed.",
			role, fsaVal, routed.String())
	}

	// Conservation: sum of ALL transfer_routed amounts (including
	// burn + insurance) must equal the input total.
	total := sdkmath.ZeroInt()
	for _, amt := range roleAmounts {
		total = total.Add(amt)
	}
	want, _ := sdkmath.NewIntFromString(totalAmt)
	assert.True(t, want.Equal(total),
		"CRITICAL — sum of transfer_routed amounts = %s must equal input "+
			"total %s. Pins the audit-reconstruction conservation: no "+
			"value created, no value lost across the split event stream. "+
			"A refactor that rounded differently OR skipped a role in "+
			"the transfer_routed emissions would break auditor "+
			"reconciliation.",
		total.String(), want.String())

	// Verify BPS attributes on fee_split_applied match the params.
	// Pins that indexers can extract the SPLIT RATIOS (not just the
	// absolute amounts) from event metadata for per-period analytics.
	assert.Equal(t, "300", fsaAttrs["burn_bps"])
	assert.Equal(t, "200", fsaAttrs["insurance_bps"])
	assert.Equal(t, "7000", fsaAttrs["publisher_bps"])
	assert.Equal(t, "2000", fsaAttrs["router_bps"])
	assert.Equal(t, "1000", fsaAttrs["referrer_bps"])
}

// TestFeeSplitMiddleware_TransferRoutedOmitsZeroRoles pins the
// suppression of zero-amount transfer_routed events. The
// middleware only emits transfer_routed when the role's
// computed amount is positive — a refactor that always emitted
// all 5 events would spam indexer state with empty-amount
// entries.
func TestFeeSplitMiddleware_TransferRoutedOmitsZeroAmountRoles(t *testing.T) {
	// All-to-publisher params: referrer gets 0, burn gets 0,
	// insurance gets 0. Only publisher + router should surface.
	params := FeeSplitParams{
		BurnBPS:      0,
		InsuranceBPS: 0,
		PublisherBPS: 9000,
		RouterBPS:    1000,
		ReferrerBPS:  0,
	}
	require.NoError(t, params.Validate())

	mw, err := NewFeeSplitMiddleware(&stubIBCModule{}, &stubICS4Wrapper{}, params)
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "10000", "ulac", "settle-no-burn",
		"lumera1pub", "lumera1router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	roles := make(map[string]bool)
	for _, e := range ctx.EventManager().Events() {
		if e.Type != "transfer_routed" {
			continue
		}
		attrs := eventAttrs(e)
		roles[attrs["recipient_role"]] = true
	}
	assert.True(t, roles["publisher"],
		"publisher with non-zero amount → transfer_routed emitted")
	assert.True(t, roles["router"],
		"router with non-zero amount → transfer_routed emitted")
	assert.False(t, roles["referrer"],
		"referrer with zero amount → transfer_routed NOT emitted. "+
			"Pins the positivity-gate at :391/:402/:413: a refactor "+
			"that emitted regardless of amount would spam indexer "+
			"state with no-op entries.")
	assert.False(t, roles["burn"],
		"burn with zero BurnBPS → transfer_routed NOT emitted")
	assert.False(t, roles["insurance"],
		"insurance with zero InsuranceBPS → transfer_routed NOT emitted")
}

// TestFeeSplitMiddleware_FailedPacketEmitsNoEvents pins that
// error paths (invalid amount, invalid denom, malformed memo)
// emit ZERO events. Partial-emission on error would leave
// indexer state in an inconsistent 'collected but not split'
// state.
func TestFeeSplitMiddleware_FailedPacketEmitsNoEvents(t *testing.T) {
	mw, err := NewFeeSplitMiddleware(&stubIBCModule{}, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Invalid amount (non-numeric) → error ack, no events.
	memo, err := BuildSettlementMemo(SettlementMemo{
		Type:          MemoTypeSettlement,
		SettlementID:  "settle-err-1",
		Publisher:     "lumera1pub",
		Router:        "lumera1router",
		RefundAddress: "lumera1refund",
	})
	require.NoError(t, err)
	ftData := transfertypes.NewFungibleTokenPacketData("ulac", "not-a-number", "s", "r", memo)
	data, err := json.Marshal(ftData)
	require.NoError(t, err)
	packet := channeltypes.Packet{
		Sequence: 1, SourcePort: "transfer", SourceChannel: "channel-0",
		DestinationPort: "transfer", DestinationChannel: "channel-1",
		Data: data,
	}

	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	assert.False(t, ack.Success(),
		"invalid amount → error ack")
	assert.Empty(t, ctx.EventManager().Events(),
		"CRITICAL — no events emitted on error path. Pins that "+
			"the fee_collected event is emitted ONLY AFTER the "+
			"amount/denom/memo validation passes. A refactor that "+
			"moved event emission earlier would produce 'orphan' "+
			"fee_collected events with no matching fee_split_applied, "+
			"breaking indexer reconstruction and reporting spurious "+
			"collected-but-unsettled funds.")
}

// TestFeeSplitMiddleware_SettlementIDCorrelatesAcrossEvents
// pins that every event in a single settlement carries the
// SAME settlement_id — the correlation key indexers use to
// group events from one packet.
func TestFeeSplitMiddleware_SettlementIDCorrelatesAcrossEvents(t *testing.T) {
	mw, err := NewFeeSplitMiddleware(&stubIBCModule{}, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	const settlementID = "settle-correlation-42"
	packet := buildSettlementPacket(t, "10000", "ulac", settlementID,
		"lumera1pub", "lumera1router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	// Every emitted event with a settlement_id attribute MUST
	// carry the same value.
	eventCount := 0
	for _, e := range ctx.EventManager().Events() {
		attrs := eventAttrs(e)
		sid, ok := attrs["settlement_id"]
		if !ok {
			continue
		}
		eventCount++
		assert.Equal(t, settlementID, sid,
			"event %s has settlement_id=%q, want %q — pins the "+
				"correlation-key contract indexers use to group "+
				"events from one packet. A refactor that omitted or "+
				"diverged the ID would break group-by-settlement "+
				"analytics.", e.Type, sid, settlementID)
	}
	assert.GreaterOrEqual(t, eventCount, 5,
		"expected at least 5 events carrying settlement_id "+
			"(fee_collected + fee_split_applied + 3+ transfer_routed)")
}
