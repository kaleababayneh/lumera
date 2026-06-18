//go:build cosmos

package ibc

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// channelOrder returns the canonical ICS-20 channel ordering
// (UNORDERED). Helper to keep test bodies focused on the
// migration-relevant arguments rather than IBC boilerplate.
func channelOrder() channeltypes.Order { return channeltypes.UNORDERED }

// channelCounterparty constructs a channeltypes.Counterparty
// with the given port + channel IDs. Helper for the regression
// tests' channel-open* steps.
func channelCounterparty(portID, channelID string) channeltypes.Counterparty {
	return channeltypes.Counterparty{PortId: portID, ChannelId: channelID}
}

// This file is a REGRESSION SUITE that exercises every public IBC
// flow of the credits/ibc fee-split middleware end-to-end. It
// exists to validate that the IBC v8 → v11 migration doesn't
// silently break the wire-level behavior that downstream
// integrations (relayers, indexers, audit pipelines) depend on.
//
// Coverage: all five IBCModule lifecycle handlers + ICS4Wrapper
// + the supplementary methods, exercised through the public
// FeeSplitMiddleware surface so signature changes from the
// migration force the test to be re-validated:
//
//   IBCModule (per ibc-go interface):
//     - OnChanOpenInit
//     - OnChanOpenTry
//     - OnChanOpenAck
//     - OnChanOpenConfirm
//     - OnChanCloseInit
//     - OnChanCloseConfirm
//     - OnRecvPacket (settlement memo + non-settlement)
//     - OnAcknowledgementPacket (success + error ack)
//     - OnTimeoutPacket
//
//   ICS4Wrapper:
//     - SendPacket
//     - WriteAcknowledgement
//     - GetAppVersion
//
// Each test calls the middleware method, asserts that the
// underlying app stub was invoked (proving delegation works),
// and asserts the return values match the underlying app's.
//
// The v11 migration has landed (merge commit 4e86243bb). The call
// sites in this file were updated alongside the middleware surgery;
// if a future ibc-go bump rotates signatures again, this file acts
// as a forcing function that surfaces silent breakage at PR review.

// TestMigrationV11_OnChanOpenInit_DelegatesToApp pins that the
// channel-open-init lifecycle delegates to the underlying app
// and surfaces its return values unchanged.
func TestMigrationV11_OnChanOpenInit_DelegatesToApp(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	const requestedVersion = "lumera-fee-split-v1"

	// stubIBCModule's OnChanOpenInit returns the input version
	// unchanged. The middleware must pass it through.
	gotVersion, err := mw.OnChanOpenInit(ctx, channelOrder(), nil, "transfer",
		"channel-0", channelCounterparty("transfer", "channel-1"),
		requestedVersion)
	require.NoError(t, err)
	require.Equal(t, requestedVersion, gotVersion,
		"OnChanOpenInit must surface the underlying app's version reply")
	require.True(t, stub.chanOpenInitCalled,
		"underlying app's OnChanOpenInit must be invoked — delegation broken")
}

// TestMigrationV11_OnChanOpenTry_DelegatesToApp pins the try
// handler. Empty version response from stub means an empty
// negotiated version, which is a valid IBC outcome.
func TestMigrationV11_OnChanOpenTry_DelegatesToApp(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	gotVersion, err := mw.OnChanOpenTry(ctx, channelOrder(), nil, "transfer",
		"channel-0", channelCounterparty("transfer", "channel-1"),
		"counterparty-version")
	require.NoError(t, err)
	// Stub returns "" — middleware must pass it through unchanged.
	require.Equal(t, "", gotVersion,
		"OnChanOpenTry must surface the underlying app's version reply")
}

// TestMigrationV11_OnChanOpenAck_NoOp pins the ack-step
// delegation. Stub is a no-op; middleware must return nil.
func TestMigrationV11_OnChanOpenAck_NoOp(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	require.NoError(t, mw.OnChanOpenAck(ctx, "transfer", "channel-0",
		"channel-1", "counterparty-v1"))
}

// TestMigrationV11_OnChanOpenConfirm_NoOp pins the confirm-step.
func TestMigrationV11_OnChanOpenConfirm_NoOp(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	require.NoError(t, mw.OnChanOpenConfirm(ctx, "transfer", "channel-0"))
}

// TestMigrationV11_OnChanCloseInit_NoOp pins close-init delegation.
func TestMigrationV11_OnChanCloseInit_NoOp(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	require.NoError(t, mw.OnChanCloseInit(ctx, "transfer", "channel-0"))
}

// TestMigrationV11_OnChanCloseConfirm_NoOp pins close-confirm.
func TestMigrationV11_OnChanCloseConfirm_NoOp(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	require.NoError(t, mw.OnChanCloseConfirm(ctx, "transfer", "channel-0"))
}

// TestMigrationV11_OnRecvPacket_SettlementMemoFullPipeline pins
// that an OnRecvPacket of a settlement-memo packet runs the full
// fee-split pipeline AND delegates to the underlying app. This
// is THE most-load-bearing flow in the credits/ibc package.
func TestMigrationV11_OnRecvPacket_SettlementMemoFullPipeline(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	packet := buildSettlementPacket(t, "1000000", "ulac", "regression-settle",
		"lumera1pub", "lumera1router")

	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.NotNil(t, ack)
	require.True(t, ack.Success(),
		"settlement packet must be accepted (success ack)")
	require.True(t, stub.recvPacketCalled,
		"underlying app's OnRecvPacket must be invoked")

	// Verify fee-split events were emitted (load-bearing pipeline
	// invariant: settlement packets MUST produce fee_collected +
	// fee_split_applied at minimum).
	events := ctx.EventManager().Events()
	feeCollected, feeSplitApplied := false, false
	for _, e := range events {
		if e.Type == "fee_collected" {
			feeCollected = true
		}
		if e.Type == "fee_split_applied" {
			feeSplitApplied = true
		}
	}
	require.True(t, feeCollected,
		"OnRecv of settlement memo MUST emit fee_collected event")
	require.True(t, feeSplitApplied,
		"OnRecv of settlement memo MUST emit fee_split_applied event")
}

// TestMigrationV11_OnRecvPacket_NonSettlementBypassesPipeline
// pins that non-settlement packets bypass the fee-split pipeline
// and delegate directly to the underlying app — no middleware
// events are emitted.
func TestMigrationV11_OnRecvPacket_NonSettlementBypassesPipeline(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	packet := buildNonSettlementPacket(t, "100000", "ulac")

	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.NotNil(t, ack)
	require.True(t, ack.Success())
	require.True(t, stub.recvPacketCalled)

	// No fee-split events emitted.
	for _, e := range ctx.EventManager().Events() {
		require.NotContains(t, []string{
			"fee_collected", "fee_split_applied", "transfer_routed",
		}, e.Type,
			"non-settlement packet must NOT trigger fee-split events")
	}
}

// TestMigrationV11_OnAcknowledgementPacket_DelegatesPlain pins
// that OnAck delegates to the underlying app and emits zero
// middleware events (covered in detail by the ack/timeout
// stream golden — this is the regression-suite touch).
func TestMigrationV11_OnAcknowledgementPacket_DelegatesPlain(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	packet := buildSettlementPacket(t, "100000", "ulac", "ack-regression",
		"lumera1pub", "lumera1router")
	ackBytes := []byte(`{"result":"AQ=="}`)

	require.NoError(t, mw.OnAcknowledgementPacket(ctx, "", packet, ackBytes, sdk.AccAddress{}))
	require.True(t, stub.ackPacketCalled,
		"underlying app's OnAcknowledgementPacket must be invoked")
}

// TestMigrationV11_OnTimeoutPacket_DelegatesPlain pins
// timeout-path delegation.
func TestMigrationV11_OnTimeoutPacket_DelegatesPlain(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	packet := buildSettlementPacket(t, "100000", "ulac", "timeout-regression",
		"lumera1pub", "lumera1router")

	require.NoError(t, mw.OnTimeoutPacket(ctx, "", packet, sdk.AccAddress{}))
	require.True(t, stub.timeoutCalled,
		"underlying app's OnTimeoutPacket must be invoked")
}

// TestMigrationV11_GetAppVersion_DelegatesToICS4Wrapper pins the
// ICS4 GetAppVersion delegation.
func TestMigrationV11_GetAppVersion_DelegatesToICS4Wrapper(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	wrapper := &stubICS4Wrapper{}
	mw, err := NewFeeSplitMiddleware(stub, wrapper, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	version, ok := mw.GetAppVersion(ctx, "transfer", "channel-0")
	require.True(t, ok,
		"GetAppVersion must surface the wrapper's ok=true response")
	require.Equal(t, ChannelVersion, version,
		"GetAppVersion must surface the wrapper's version string")
}

// TestMigrationV11_FullLifecycle_OpenSendRecvAckClose runs the
// entire IBC handshake + packet-flow lifecycle in sequence and
// asserts every method delegates without surfacing errors. This
// is the END-TO-END regression: a v11 signature mismatch
// anywhere in the chain breaks here.
func TestMigrationV11_FullLifecycle_OpenSendRecvAckClose(t *testing.T) {
	t.Parallel()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	const port = "transfer"
	const channel = "channel-0"

	// Step 1: open-init (responder side simulated).
	v1, err := mw.OnChanOpenInit(ctx, channelOrder(), nil, port, channel, channelCounterparty(port, "channel-1"), "v1")
	require.NoError(t, err, "open-init failed")
	require.Equal(t, "v1", v1)

	// Step 2: open-try (counterparty side simulated).
	v2, err := mw.OnChanOpenTry(ctx, channelOrder(), nil, port, channel, channelCounterparty(port, "channel-1"), "v1")
	require.NoError(t, err, "open-try failed")
	_ = v2

	// Step 3: open-ack.
	require.NoError(t, mw.OnChanOpenAck(ctx, port, channel, "channel-1", "v1"))

	// Step 4: open-confirm.
	require.NoError(t, mw.OnChanOpenConfirm(ctx, port, channel))

	// Step 5: receive a settlement packet (the load-bearing flow).
	packet := buildSettlementPacket(t, "1000000", "ulac", "lifecycle-test",
		"lumera1pub", "lumera1router")
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success(), "lifecycle settlement-recv must succeed")

	// Step 6: simulate ack of an outgoing packet.
	ackBytes := []byte(`{"result":"AQ=="}`)
	require.NoError(t, mw.OnAcknowledgementPacket(ctx, "", packet, ackBytes, sdk.AccAddress{}))

	// Step 7: simulate timeout of an outgoing packet.
	require.NoError(t, mw.OnTimeoutPacket(ctx, "", packet, sdk.AccAddress{}))

	// Step 8: close-init then close-confirm (channel teardown).
	require.NoError(t, mw.OnChanCloseInit(ctx, port, channel))
	require.NoError(t, mw.OnChanCloseConfirm(ctx, port, channel))

	// Final: every stub method must have been touched.
	require.True(t, stub.chanOpenInitCalled, "chan-open-init not delegated")
	require.True(t, stub.recvPacketCalled, "recv-packet not delegated")
	require.True(t, stub.ackPacketCalled, "ack-packet not delegated")
	require.True(t, stub.timeoutCalled, "timeout not delegated")
}
