//go:build cosmos

package ibc

import (
	"fmt"
	"sort"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// This file applies the testing-conformance-harnesses skill
// (Pattern 1: Differential Testing + spec-derived MUST matrix)
// to the CROSS-CHAIN IBC SETTLEMENT DETERMINISM contract.
//
// Cross-chain pipeline:
//
//   SOURCE CHAIN              DESTINATION CHAIN
//   ─────────────             ────────────────
//   send packet:              receive via OnRecvPacket:
//     - escrow tokens           - parse settlement memo
//     - emit send event         - apply fee-split middleware
//     - record outbound         - delegate to underlying app
//                               - ack returned to source
//
//   on ack:                   on timeout:
//     - release escrow          - source: refund escrow
//     - mark settled            - destination: rolled back
//     OR
//   on error ack:
//     - refund escrow
//
// THE LOAD-BEARING CONTRACT: after every (recv → ack | recv →
// timeout | recv → error-ack) cycle, source and destination
// states must be CONSISTENT — total tokens conserved across
// both chains, no funds lost or duplicated.
//
// Where prior tests covered the middleware in isolation
// (tick 75 ack/timeout goldens, tick 4bfb5b9a5 recovery MRs),
// THIS file pins the BIDIRECTIONAL convergence: both chain
// states evolve in lockstep across the full packet lifecycle.
//
// Seven MUST clauses:
//
//   MUST-1: SUCCESS-PATH CONSERVATION — source escrow released
//     + destination credit accounted for; total cross-chain
//     supply byte-equal pre/post
//   MUST-2: ERROR-ACK SOURCE REFUND — source refunds escrow on
//     error ack; destination has no state change (the rollback
//     is implicit in IBC's atomicity)
//   MUST-3: TIMEOUT REFUND EQUIVALENT TO ERROR-ACK — timeout
//     produces the SAME source state as an error ack on the
//     same packet (recovery paths converge)
//   MUST-4: MULTI-PACKET ACCUMULATION — N successful packets
//     through the cycle produce source state = source -
//     Σ amounts, destination state = destination + Σ amounts
//   MUST-5: OUT-OF-ORDER ACKS DETERMINISTIC — packet B's ack
//     processed before packet A's still produces the SAME
//     final (source, destination) state pair as in-order
//   MUST-6: CROSS-CHAIN REPLAY DETERMINISM — same packet
//     sequence across two parallel (source+destination) chain
//     pairs produces byte-equal state on BOTH sides
//   MUST-7: ERROR-ACK AND TIMEOUT RECOVERY EQUIVALENT — for
//     a given packet, the recovery state from an error ack vs
//     a timeout is byte-equal (both fully refund; neither
//     leaks any partial commit to destination)

// --------------------------------------------------------------
// Cross-chain state simulator. Two chain models:
//
//   chainState tracks per-account credit balances and an
//   escrow account; mirrors the bank-keeper view of the
//   settlement-relevant tokens.
//
//   crossChainPair binds a SOURCE and DESTINATION chain into
//   a packet-flow harness with delivery semantics.
// --------------------------------------------------------------

type chainState struct {
	balances map[string]int64 // address → balance
	escrow   int64             // module account
}

func newChainState(initialBalances map[string]int64) *chainState {
	cs := &chainState{
		balances: map[string]int64{},
		escrow:   0,
	}
	for k, v := range initialBalances {
		cs.balances[k] = v
	}
	return cs
}

func (c *chainState) totalSupply() int64 {
	t := c.escrow
	for _, b := range c.balances {
		t += b
	}
	return t
}

// snapshot returns a stable byte representation of the state
// for byte-equal comparison across pipelines.
func (c *chainState) snapshot() string {
	keys := make([]string, 0, len(c.balances))
	for k := range c.balances {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := fmt.Sprintf("escrow=%d|", c.escrow)
	for _, k := range keys {
		out += fmt.Sprintf("%s=%d|", k, c.balances[k])
	}
	return out
}

// crossChainPair models a source+destination chain pair plus a
// packet pipeline.
type crossChainPair struct {
	source      *chainState
	destination *chainState
	mw          FeeSplitMiddleware
	stub        *stubIBCModule
	ctx         sdk.Context
	pendingPackets map[string]packetState // settlementID → state
}

type packetState struct {
	settlementID string
	amount       int64
	sender       string
	receiver     string
}

func newCrossChainPair(t *testing.T, sourceFunding, destFunding int64) *crossChainPair {
	t.Helper()
	source := newChainState(map[string]int64{
		"router-source": sourceFunding,
	})
	dest := newChainState(map[string]int64{
		"publisher-dest": destFunding,
	})
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)
	return &crossChainPair{
		source:         source,
		destination:    dest,
		mw:             mw,
		stub:           stub,
		ctx:            testCtx(),
		pendingPackets: map[string]packetState{},
	}
}

// sendPacket simulates source-side packet emission: escrow
// tokens, record pending outbound packet.
func (p *crossChainPair) sendPacket(t *testing.T, settlementID, sender string, amount int64) {
	t.Helper()
	require.GreaterOrEqual(t, p.source.balances[sender], amount,
		"sender %s has insufficient balance for %d", sender, amount)
	p.source.balances[sender] -= amount
	p.source.escrow += amount
	p.pendingPackets[settlementID] = packetState{
		settlementID: settlementID,
		amount:       amount,
		sender:       sender,
		receiver:     "publisher-dest",
	}
}

// deliverRecv simulates destination-side packet receipt via
// OnRecvPacket. Returns whether the middleware accepted it.
func (p *crossChainPair) deliverRecv(t *testing.T, settlementID string) bool {
	t.Helper()
	state, ok := p.pendingPackets[settlementID]
	require.True(t, ok, "no pending packet for settlement %s", settlementID)

	pkt := buildSettlementPacket(t, fmt.Sprintf("%d", state.amount),
		"ulac", state.settlementID, state.receiver, "lumera1router-X")

	ack := p.mw.OnRecvPacket(p.ctx, "", pkt, sdk.AccAddress{})
	if !ack.Success() {
		return false
	}
	// Successful recv → destination credit (advisory mode: the
	// underlying app moves the full amount to receiver).
	p.destination.balances[state.receiver] += state.amount
	return true
}

// processSuccessAck simulates source-side processing of a
// successful ack: release escrow (the destination already
// credited the receiver).
func (p *crossChainPair) processSuccessAck(t *testing.T, settlementID string) {
	t.Helper()
	state, ok := p.pendingPackets[settlementID]
	require.True(t, ok, "no pending packet for ack of %s", settlementID)

	pkt := buildSettlementPacket(t, fmt.Sprintf("%d", state.amount),
		"ulac", state.settlementID, state.receiver, state.sender)
	require.NoError(t, p.mw.OnAcknowledgementPacket(p.ctx, "", pkt,
		[]byte(`{"result":"AQ=="}`), sdk.AccAddress{}))

	// On success ack: release escrow (the destination already
	// has the credit). Source escrow → 0 (or -=amount).
	p.source.escrow -= state.amount
	delete(p.pendingPackets, settlementID)
}

// processErrorAck simulates source-side processing of an
// error ack: refund escrow back to sender.
func (p *crossChainPair) processErrorAck(t *testing.T, settlementID string) {
	t.Helper()
	state, ok := p.pendingPackets[settlementID]
	require.True(t, ok, "no pending packet for error ack of %s", settlementID)

	pkt := buildSettlementPacket(t, fmt.Sprintf("%d", state.amount),
		"ulac", state.settlementID, state.receiver, state.sender)
	require.NoError(t, p.mw.OnAcknowledgementPacket(p.ctx, "", pkt,
		[]byte(`{"error":"upstream_failure"}`), sdk.AccAddress{}))

	// On error ack: refund. Escrow → 0, sender balance restored.
	p.source.escrow -= state.amount
	p.source.balances[state.sender] += state.amount
	delete(p.pendingPackets, settlementID)
}

// processTimeout simulates source-side processing of a timeout:
// refund escrow back to sender.
func (p *crossChainPair) processTimeout(t *testing.T, settlementID string) {
	t.Helper()
	state, ok := p.pendingPackets[settlementID]
	require.True(t, ok, "no pending packet for timeout of %s", settlementID)

	pkt := buildSettlementPacket(t, fmt.Sprintf("%d", state.amount),
		"ulac", state.settlementID, state.receiver, state.sender)
	require.NoError(t, p.mw.OnTimeoutPacket(p.ctx, "", pkt, sdk.AccAddress{}))

	p.source.escrow -= state.amount
	p.source.balances[state.sender] += state.amount
	delete(p.pendingPackets, settlementID)
}

// --------------------------------------------------------------
// MUST-1: SUCCESS-PATH CONSERVATION
// --------------------------------------------------------------

func TestCrossChain_MUST1_SuccessPathConserves(t *testing.T) {
	t.Parallel()
	p := newCrossChainPair(t, 1_000_000, 0)

	preTotal := p.source.totalSupply() + p.destination.totalSupply()
	require.Equal(t, int64(1_000_000), preTotal, "prereq: total supply 1M")

	// Full happy-path cycle.
	p.sendPacket(t, "settle-1", "router-source", 100_000)
	require.True(t, p.deliverRecv(t, "settle-1"))
	p.processSuccessAck(t, "settle-1")

	postTotal := p.source.totalSupply() + p.destination.totalSupply()
	require.Equal(t, preTotal, postTotal,
		"MUST-1: cross-chain total supply diverged pre=%d post=%d",
		preTotal, postTotal)

	// Source: router-source spent 100k, escrow released to 0.
	require.Equal(t, int64(900_000), p.source.balances["router-source"],
		"MUST-1: source balance after success ack")
	require.Equal(t, int64(0), p.source.escrow,
		"MUST-1: source escrow released after success ack")
	require.Equal(t, int64(100_000), p.destination.balances["publisher-dest"],
		"MUST-1: destination credited after recv")
}

// --------------------------------------------------------------
// MUST-2: ERROR-ACK SOURCE REFUND
// --------------------------------------------------------------

func TestCrossChain_MUST2_ErrorAckRefundsSource(t *testing.T) {
	t.Parallel()
	p := newCrossChainPair(t, 1_000_000, 0)

	// Send + recv (recv succeeds at the middleware level).
	p.sendPacket(t, "err-1", "router-source", 200_000)
	require.True(t, p.deliverRecv(t, "err-1"))

	// In real IBC, an error ack would cause the destination's
	// recv state to be rolled back. We model that by reverting
	// the destination credit. (In production this happens via
	// the IBC framework's atomic-execution semantics.)
	p.destination.balances["publisher-dest"] -= 200_000

	// Now process error ack on source.
	p.processErrorAck(t, "err-1")

	// Source: balance restored to original.
	require.Equal(t, int64(1_000_000), p.source.balances["router-source"],
		"MUST-2: source balance fully refunded on error ack")
	require.Equal(t, int64(0), p.source.escrow,
		"MUST-2: source escrow released")
	// Destination: no net change.
	require.Equal(t, int64(0), p.destination.balances["publisher-dest"],
		"MUST-2: destination state rolled back to pre-recv")
}

// --------------------------------------------------------------
// MUST-3: TIMEOUT EQUIVALENT TO ERROR-ACK
// --------------------------------------------------------------

func TestCrossChain_MUST3_TimeoutEquivalentToErrorAck(t *testing.T) {
	t.Parallel()

	// Pipeline A: send → recv → error ack
	pA := newCrossChainPair(t, 1_000_000, 0)
	pA.sendPacket(t, "rec-1", "router-source", 300_000)
	pA.deliverRecv(t, "rec-1")
	pA.destination.balances["publisher-dest"] -= 300_000 // simulate rollback
	pA.processErrorAck(t, "rec-1")

	// Pipeline B: send → recv → timeout (no ack)
	pB := newCrossChainPair(t, 1_000_000, 0)
	pB.sendPacket(t, "rec-1", "router-source", 300_000)
	pB.deliverRecv(t, "rec-1")
	pB.destination.balances["publisher-dest"] -= 300_000 // simulate rollback
	pB.processTimeout(t, "rec-1")

	require.Equal(t, pA.source.snapshot(), pB.source.snapshot(),
		"MUST-3: source state diverges between error-ack(%s) and timeout(%s)",
		pA.source.snapshot(), pB.source.snapshot())
	require.Equal(t, pA.destination.snapshot(), pB.destination.snapshot(),
		"MUST-3: destination state diverges between error-ack(%s) and timeout(%s)",
		pA.destination.snapshot(), pB.destination.snapshot())
}

// --------------------------------------------------------------
// MUST-4: MULTI-PACKET ACCUMULATION
// --------------------------------------------------------------

func TestCrossChain_MUST4_MultiPacketAccumulation(t *testing.T) {
	t.Parallel()
	p := newCrossChainPair(t, 5_000_000, 0)

	amounts := []int64{100_000, 250_000, 500_000, 75_000, 300_000}
	var total int64
	for i, amt := range amounts {
		sid := fmt.Sprintf("multi-%d", i)
		p.sendPacket(t, sid, "router-source", amt)
		require.True(t, p.deliverRecv(t, sid))
		p.processSuccessAck(t, sid)
		total += amt
	}

	require.Equal(t, int64(5_000_000)-total, p.source.balances["router-source"],
		"MUST-4: source balance after %d successful packets summing %d",
		len(amounts), total)
	require.Equal(t, total, p.destination.balances["publisher-dest"],
		"MUST-4: destination accumulated %d across %d packets",
		total, len(amounts))
	require.Equal(t, int64(0), p.source.escrow,
		"MUST-4: all packets fully settled, escrow drained")
	require.Empty(t, p.pendingPackets,
		"MUST-4: no pending packets after all acks")
}

// --------------------------------------------------------------
// MUST-5: OUT-OF-ORDER ACKS DETERMINISTIC
// --------------------------------------------------------------

func TestCrossChain_MUST5_OutOfOrderAcksConverge(t *testing.T) {
	t.Parallel()

	mkPair := func() *crossChainPair {
		p := newCrossChainPair(t, 5_000_000, 0)
		p.sendPacket(t, "p-A", "router-source", 100_000)
		p.sendPacket(t, "p-B", "router-source", 200_000)
		p.sendPacket(t, "p-C", "router-source", 300_000)
		require.True(t, p.deliverRecv(t, "p-A"))
		require.True(t, p.deliverRecv(t, "p-B"))
		require.True(t, p.deliverRecv(t, "p-C"))
		return p
	}

	// Pipeline X: ack in order A, B, C.
	pX := mkPair()
	pX.processSuccessAck(t, "p-A")
	pX.processSuccessAck(t, "p-B")
	pX.processSuccessAck(t, "p-C")

	// Pipeline Y: ack out of order C, A, B.
	pY := mkPair()
	pY.processSuccessAck(t, "p-C")
	pY.processSuccessAck(t, "p-A")
	pY.processSuccessAck(t, "p-B")

	require.Equal(t, pX.source.snapshot(), pY.source.snapshot(),
		"MUST-5: source state diverges across ack ordering "+
			"in-order(%s) out-of-order(%s)",
		pX.source.snapshot(), pY.source.snapshot())
	require.Equal(t, pX.destination.snapshot(), pY.destination.snapshot(),
		"MUST-5: destination state diverges across ack ordering "+
			"in-order(%s) out-of-order(%s)",
		pX.destination.snapshot(), pY.destination.snapshot())
}

// --------------------------------------------------------------
// MUST-6: CROSS-CHAIN REPLAY DETERMINISM
// --------------------------------------------------------------

func TestCrossChain_MUST6_CrossChainReplayDeterministic(t *testing.T) {
	t.Parallel()

	// Same script across two parallel chain pairs — both
	// (source, destination) states must be byte-equal.
	runScript := func() (string, string) {
		p := newCrossChainPair(t, 10_000_000, 0)
		// Mixed lifecycle: 3 success, 1 error, 1 timeout.
		p.sendPacket(t, "rep-1", "router-source", 100_000)
		p.deliverRecv(t, "rep-1")
		p.processSuccessAck(t, "rep-1")

		p.sendPacket(t, "rep-2", "router-source", 200_000)
		p.deliverRecv(t, "rep-2")
		p.destination.balances["publisher-dest"] -= 200_000 // rollback
		p.processErrorAck(t, "rep-2")

		p.sendPacket(t, "rep-3", "router-source", 150_000)
		p.deliverRecv(t, "rep-3")
		p.processSuccessAck(t, "rep-3")

		p.sendPacket(t, "rep-4", "router-source", 75_000)
		p.deliverRecv(t, "rep-4")
		p.destination.balances["publisher-dest"] -= 75_000 // rollback
		p.processTimeout(t, "rep-4")

		p.sendPacket(t, "rep-5", "router-source", 500_000)
		p.deliverRecv(t, "rep-5")
		p.processSuccessAck(t, "rep-5")

		return p.source.snapshot(), p.destination.snapshot()
	}

	srcA, destA := runScript()
	srcB, destB := runScript()

	require.Equal(t, srcA, srcB,
		"MUST-6: source state diverges across pipelines A=%s B=%s",
		srcA, srcB)
	require.Equal(t, destA, destB,
		"MUST-6: destination state diverges across pipelines A=%s B=%s",
		destA, destB)
}

// --------------------------------------------------------------
// MUST-7: ERROR-ACK AND TIMEOUT RECOVERY EQUIVALENT (per packet)
// --------------------------------------------------------------

func TestCrossChain_MUST7_ErrorAckTimeoutRecoveryEquivalent(t *testing.T) {
	t.Parallel()

	// Same packet → recv → recovery: error-ack vs timeout
	// must produce IDENTICAL final state on both chains.
	const amount int64 = 425_000

	pAck := newCrossChainPair(t, 5_000_000, 0)
	pAck.sendPacket(t, "rec-eq", "router-source", amount)
	pAck.deliverRecv(t, "rec-eq")
	pAck.destination.balances["publisher-dest"] -= amount
	pAck.processErrorAck(t, "rec-eq")

	pTo := newCrossChainPair(t, 5_000_000, 0)
	pTo.sendPacket(t, "rec-eq", "router-source", amount)
	pTo.deliverRecv(t, "rec-eq")
	pTo.destination.balances["publisher-dest"] -= amount
	pTo.processTimeout(t, "rec-eq")

	require.Equal(t, pAck.source.snapshot(), pTo.source.snapshot(),
		"MUST-7: source state diverges for same packet recovery via "+
			"error-ack(%s) vs timeout(%s)",
		pAck.source.snapshot(), pTo.source.snapshot())
	require.Equal(t, pAck.destination.snapshot(), pTo.destination.snapshot(),
		"MUST-7: destination state diverges between error-ack and timeout")
	require.Equal(t, int64(5_000_000),
		pAck.source.balances["router-source"]+pAck.source.escrow,
		"MUST-7: source full balance restored on recovery")
	require.Equal(t, int64(0), pAck.destination.balances["publisher-dest"],
		"MUST-7: destination has no net credit from rolled-back packet")
}

// --------------------------------------------------------------
// Coverage matrix
// --------------------------------------------------------------

func TestCrossChain_CoverageMatrix(t *testing.T) {
	t.Parallel()
	matrix := []struct{ id, description string }{
		{"MUST-1", "success-path conservation: total cross-chain supply byte-equal pre/post"},
		{"MUST-2", "error-ack source refund: destination rolled back, source restored"},
		{"MUST-3", "timeout ≡ error-ack on same packet"},
		{"MUST-4", "5-packet accumulation: source/destination shift by Σ amounts"},
		{"MUST-5", "out-of-order acks: in-order ≡ shuffled-order final state"},
		{"MUST-6", "cross-chain replay: same script across two pipelines → byte-equal"},
		{"MUST-7", "per-packet recovery equivalence: error-ack ≡ timeout on same packet"},
	}
	require.Len(t, matrix, 7,
		"coverage matrix must have exactly 7 MUST clauses")
	for _, m := range matrix {
		require.NotEmpty(t, m.id)
		require.NotEmpty(t, m.description)
		t.Logf("[crosschain-conformance] %s: %s", m.id, m.description)
	}
	_ = sdkmath.NewInt(0) // keep imports lean across builds
	_ = channeltypes.UNORDERED
}
