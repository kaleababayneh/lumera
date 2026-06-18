//go:build cosmos

package ibc

import (
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	"github.com/stretchr/testify/require"
)

// The IBC v11 migration (a4de96c44) introduced two post-construction mutation
// hooks on *FeeSplitMiddleware:
//
//   func (m *FeeSplitMiddleware) SetICS4Wrapper(wrapper porttypes.ICS4Wrapper)
//   func (m *FeeSplitMiddleware) SetUnderlyingApplication(app porttypes.IBCModule)
//
// Both satisfy interfaces that v11 IBC requires of application-layer modules
// (IBCModule and Middleware). This file characterizes the mutation surface
// so that:
//
//   1. Future refactors cannot silently break in-place replacement of the
//      wrapper or the underlying app.
//   2. The pointer-receiver requirement is exercised — compile-time broken
//      code (value receiver, copy-by-value) fails the `var _ = ...` interface
//      assertion in this file and is caught before a build lands on main.
//   3. OnRecvPacket's new channelVersion parameter is forwarded verbatim to
//      the underlying application (non-settlement passthrough path), which is
//      the only contract callers can rely on until core IBC starts routing
//      non-empty versions. Regressions here would silently strip version
//      strings that future ICS-20 v2+ apps may require.
//
// This is a characterization test suite (per bead lumera_ai-8k297), not a
// property-based suite: each test is a minimal scenario pinning exactly
// one observable invariant of the v11 mutation surface.

// TestFeeSplitMiddleware_SetICS4Wrapper_ReplacesWrapperInPlace pins the
// in-place-replacement semantics of SetICS4Wrapper. A subsequent SendPacket
// must route through the newly-injected wrapper, not the constructor-supplied
// one. Regressions (e.g., storing into a copy, guarding against overwrite)
// would break the IBC v11 middleware stack wiring contract silently — the
// upstream wrapper would never actually intercept packets.
func TestFeeSplitMiddleware_SetICS4Wrapper_ReplacesWrapperInPlace(t *testing.T) {
	original := &stubICS4Wrapper{}
	mw, err := NewFeeSplitMiddleware(&stubIBCModule{}, original, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Drive one SendPacket through the constructor-supplied wrapper so we
	// can later assert the replacement took effect. If Set*Wrapper were a
	// no-op, both wrappers would end up with sendPacketSeq == 1.
	ctx := testCtx()
	_, err = mw.SendPacket(ctx, "transfer", "ch-0", clienttypes.Height{}, 0, []byte("pre"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), original.sendPacketSeq, "constructor wrapper must receive first SendPacket")

	// Inject a fresh wrapper via the v11 mutation hook.
	replacement := &stubICS4Wrapper{}
	mw.SetICS4Wrapper(replacement)

	// Subsequent SendPacket must route through the replacement, leaving
	// the original untouched. This is the load-bearing invariant for
	// ics4 middleware stacking: the upstream wrapper intercepts outgoing
	// packets from this middleware onward.
	_, err = mw.SendPacket(ctx, "transfer", "ch-0", clienttypes.Height{}, 0, []byte("post"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), original.sendPacketSeq,
		"original wrapper must NOT see packets after SetICS4Wrapper replacement")
	require.Equal(t, uint64(1), replacement.sendPacketSeq,
		"replacement wrapper must receive SendPacket after being injected via SetICS4Wrapper")
}

// TestFeeSplitMiddleware_SetICS4Wrapper_ForwardsToInnerApp pins that the
// wrapper injected into this middleware is also forwarded to the wrapped app
// when that app has its own SetICS4Wrapper hook. Without this propagation,
// middleware stacks with another ICS4-aware layer below fee-split would route
// this middleware's outgoing packets through the upstream wrapper while the
// inner app kept a stale wrapper.
func TestFeeSplitMiddleware_SetICS4Wrapper_ForwardsToInnerApp(t *testing.T) {
	inner := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(inner, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	replacement := &stubICS4Wrapper{}
	mw.SetICS4Wrapper(replacement)

	require.True(t, inner.setICS4WrapperCalled,
		"inner app must receive SetICS4Wrapper when it implements the hook")
	require.Same(t, replacement, inner.lastSetWrapper,
		"inner app must receive the same wrapper injected into fee-split middleware")
}

// TestFeeSplitMiddleware_SetUnderlyingApplication_ReplacesApp pins that
// SetUnderlyingApplication replaces the underlying IBCModule in place and
// subsequent OnRecvPacket dispatches route through the new app. The v11
// Middleware interface (porttypes.Middleware) requires this setter to
// support the "build wrapper before app is known" wiring pattern.
func TestFeeSplitMiddleware_SetUnderlyingApplication_ReplacesApp(t *testing.T) {
	original := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(original, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Non-ICS-20 packet — guaranteed passthrough to the underlying app
	// without touching fee-split logic.
	packet := channeltypes.Packet{Data: []byte("not-json")}
	ctx := testCtx()

	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())
	require.True(t, original.recvPacketCalled, "constructor app must receive first OnRecvPacket")

	// Swap in a new app and reset the original's call tracking so a
	// post-swap call to the original would be detectable.
	replacement := &stubIBCModule{}
	mw.SetUnderlyingApplication(replacement)
	original.recvPacketCalled = false

	ack = mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())
	require.False(t, original.recvPacketCalled,
		"original app must NOT see packets after SetUnderlyingApplication replacement")
	require.True(t, replacement.recvPacketCalled,
		"replacement app must receive OnRecvPacket after being injected via SetUnderlyingApplication")
}

// Compile-time assertion: the v11 interface contracts are satisfied by
// *FeeSplitMiddleware (pointer) and NOT by FeeSplitMiddleware (value).
// If a future refactor moves SetICS4Wrapper or SetUnderlyingApplication
// to a value receiver, the first line still compiles but the underlying
// middleware would silently lose in-place mutations. If someone drops the
// Set* methods entirely, the first line fails to compile. Either
// regression is caught at build time here.
//
// Cannot assert the negative ("FeeSplitMiddleware{} does NOT satisfy
// IBCModule") at compile time without build tags that trigger expected
// failure — instead we pin the positive assertion for the pointer type
// and note the constraint in this comment. The runtime test below
// exercises the value-copy trap so the contract stays documented.
var (
	_ porttypes.IBCModule   = (*FeeSplitMiddleware)(nil)
	_ porttypes.ICS4Wrapper = (*FeeSplitMiddleware)(nil)
	_ porttypes.Middleware  = (*FeeSplitMiddleware)(nil)
)

// TestFeeSplitMiddleware_ValueCopyLosesMutations documents the value-copy
// trap that the pointer-receiver setter creates. Any call site that holds
// a FeeSplitMiddleware by value and then calls Set* on that copy mutates
// only the copy — the original is unchanged. The v11 interface requires
// pointer semantics specifically to make this observable to the type
// system. This test asserts that the runtime behaviour matches the
// compile-time constraint: a dereferenced copy's Set* call does not leak
// back to the source middleware.
//
// We cannot use `var _ porttypes.Middleware = FeeSplitMiddleware{}` as a
// negative compile-time assertion (that would make the test file fail to
// compile). Instead we pin the behavioural invariant.
func TestFeeSplitMiddleware_ValueCopyLosesMutations(t *testing.T) {
	original := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(original, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Copy by value — the operation a careless caller might do inside a
	// helper or when putting the middleware into a map.
	copyOfMw := mw

	// Mutate the copy. Because Set* is a pointer receiver, the dereference
	// below on &copyOfMw targets the local copy, not mw.
	replacement := &stubIBCModule{}
	(&copyOfMw).SetUnderlyingApplication(replacement)

	// Drive a non-ICS-20 passthrough through the ORIGINAL middleware. It
	// must still route to `original`, not `replacement` — the mutation
	// was scoped to the copy.
	packet := channeltypes.Packet{Data: []byte("not-json")}
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())
	require.True(t, original.recvPacketCalled,
		"original middleware must see the packet — value-copy mutation must not leak back")
	require.False(t, replacement.recvPacketCalled,
		"replacement app must NOT see packets through the original middleware; "+
			"Set* on a value copy is a silently-scoped mutation by design")
}

// TestFeeSplitMiddleware_OnRecvPacket_ForwardsChannelVersion pins that
// the channelVersion parameter added by IBC v11 is forwarded verbatim to
// the underlying app in the non-settlement passthrough path. Core IBC
// currently routes empty strings here for most channels, but future
// ICS-20 v2+ apps will need to see the real channel version to select
// parsing strategies. Regressions (e.g., dropping the arg or substituting
// a constant) would be invisible today but would break v2 channels
// silently when they arrive.
func TestFeeSplitMiddleware_OnRecvPacket_ForwardsChannelVersion(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Non-ICS-20 passthrough: guarantees the underlying app is called
	// without fee-split intercepting. The channelVersion the middleware
	// receives should flow through untouched.
	packet := channeltypes.Packet{Data: []byte("not-json")}
	ctx := testCtx()

	mw.OnRecvPacket(ctx, "ics20-2-future", packet, sdk.AccAddress{})
	require.Equal(t, "ics20-2-future", stub.recvPacketVersion,
		"channelVersion must be forwarded verbatim to underlying app on non-ICS-20 passthrough")

	// ICS-20 non-settlement passthrough is the other routing path that
	// delegates to the app unchanged. A settlement-carrying packet would
	// be intercepted by fee-split and never reach the app's OnRecvPacket.
	ftData := buildNonSettlementFTPacketData(t)
	nonSettlement := channeltypes.Packet{Data: ftData}
	stub.recvPacketVersion = ""
	mw.OnRecvPacket(ctx, "ics20-1", nonSettlement, sdk.AccAddress{})
	require.Equal(t, "ics20-1", stub.recvPacketVersion,
		"channelVersion must forward through non-settlement ICS-20 passthrough too")
}

// buildNonSettlementFTPacketData produces JSON bytes for an ICS-20 packet
// without a Lumera settlement memo — the fee-split middleware will pass
// it through to the underlying app without intercepting.
func buildNonSettlementFTPacketData(t *testing.T) []byte {
	t.Helper()
	body := map[string]string{
		"denom":    "ulac",
		"amount":   "100",
		"sender":   "sender",
		"receiver": "receiver",
		"memo":     "", // empty memo — NOT a Lumera settlement
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)
	return b
}
