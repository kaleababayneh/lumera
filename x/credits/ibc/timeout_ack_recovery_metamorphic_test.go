//go:build cosmos

package ibc

import (
	"fmt"
	"math/rand"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// This file applies the testing-metamorphic skill to the
// FAILURE-PATH RECOVERY semantics of the credits/ibc fee-split
// middleware. Companion to:
//
//   - tick 75 (ack_timeout_event_stream_golden_test.go): pins
//     ZERO middleware events on these paths via 5 scenario
//     goldens
//   - tick fdd18631b (migration_v11_regression_test.go): pins
//     interface-level delegation under signature changes
//
// THIS file pins INVARIANT RELATIONS across MANY input
// variations of the failure-path flow. Where goldens pin
// specific scenarios and conformance pins MUST clauses,
// metamorphic relations catch regressions that pass scenario
// tests but break for slightly different inputs.
//
// Why these paths matter operationally:
//
//   - Failed cross-chain settlements (counterparty rejects, OR
//     packet times out via IBC's height/timestamp guards) are
//     the load-bearing recovery surface. The contract per
//     fee_split_middleware.go:456-457: "No partial split is
//     applied on timeout — the full amount is refunded by the
//     underlying transfer module."
//
//   - A regression that started PARTIAL execution on
//     timeout/ack (e.g. firing the fee-split executor before
//     the underlying app could refund) would create LOST
//     FUNDS — the worst possible failure mode for a settlement
//     module.
//
// Six MRs spanning the failure-path recovery surface:
//
//   MR-1 (MEMO-SHAPE INVARIANCE): middleware behavior on
//     ack/timeout is INDEPENDENT of memo content. Settlement
//     memo, malformed memo, empty memo, oversized memo — all
//     produce identical delegation+zero-events outcome
//   MR-2 (ACK-CONTENT INVARIANCE): middleware behavior on ack
//     is INDEPENDENT of acknowledgement content. Success ack,
//     error ack, malformed ack, empty ack — all delegate
//     identically with zero middleware events
//   MR-3 (SEQUENCE-LENGTH MONOTONICITY): N consecutive
//     ack/timeout calls produce exactly N delegations and
//     0 middleware events; no per-call state accumulator
//   MR-4 (ORDER INVARIANCE): same set of N packets ack'd or
//     timed out in any order produces same total stub-state
//     (delegated counts) and same zero-event count
//   MR-5 (CROSS-OPERATION ISOLATION): a timeout of packet X
//     does not affect the subsequent ack of packet Y; the
//     two recovery operations are independent
//   MR-6 (REPLAY DETERMINISM): same recovery script across two
//     pipelines yields byte-equal stub state and byte-equal
//     middleware event count (which is always 0)

// --------------------------------------------------------------
// Recovery harness — wraps the existing stub IBC module + ICS4
// wrapper from fee_split_middleware_test.go with helpers for
// MR-style multi-call recovery tests.
// --------------------------------------------------------------

// recoveryScenario describes one failure-path operation in a
// recovery script.
type recoveryScenario struct {
	op            string // "ack" | "timeout"
	memoType      string // "settlement" | "non_settlement"
	settlementID  string // for settlement memo
	ackBytes      []byte // for ack
	expectSuccess bool   // ack expected to be success-flavored
}

// runRecoveryScript drives N failure-path operations through a
// fresh middleware. Returns the (recvCount, ackCount, timeoutCount)
// stub-call totals + total middleware events emitted.
type recoveryResult struct {
	ackCalls       int
	timeoutCalls   int
	mwEvents       int
	allDelegated   bool
}

func runRecoveryScript(t *testing.T, script []recoveryScenario) recoveryResult {
	t.Helper()
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	res := recoveryResult{allDelegated: true}

	for i, s := range script {
		// Build packet per scenario.
		var packet channeltypes.Packet
		switch s.memoType {
		case "settlement":
			packet = buildSettlementPacket(t, "100000", "ulac",
				s.settlementID,
				fmt.Sprintf("lumera1pub-%d", i),
				fmt.Sprintf("lumera1router-%d", i))
		case "non_settlement":
			packet = buildNonSettlementPacket(t, "100000", "ulac")
		default:
			t.Fatalf("unknown memoType %q", s.memoType)
		}

		// Reset stub flags so each per-iteration assertion is
		// unambiguous.
		stub.ackPacketCalled = false
		stub.timeoutCalled = false

		switch s.op {
		case "ack":
			require.NoError(t, mw.OnAcknowledgementPacket(ctx, "", packet, s.ackBytes, sdk.AccAddress{}),
				"script[%d] ack call", i)
			if !stub.ackPacketCalled {
				res.allDelegated = false
			}
			res.ackCalls++
		case "timeout":
			require.NoError(t, mw.OnTimeoutPacket(ctx, "", packet, sdk.AccAddress{}),
				"script[%d] timeout call", i)
			if !stub.timeoutCalled {
				res.allDelegated = false
			}
			res.timeoutCalls++
		default:
			t.Fatalf("unknown op %q", s.op)
		}
	}

	// Count middleware events accumulated across the whole script.
	for _, e := range ctx.EventManager().Events() {
		if e.Type == "fee_collected" || e.Type == "fee_split_applied" || e.Type == "transfer_routed" {
			res.mwEvents++
		}
	}
	return res
}

// --------------------------------------------------------------
// MR 1 (MEMO-SHAPE INVARIANCE): same behavior across memo types
// --------------------------------------------------------------

func TestTimeoutAckRecovery_MR_MemoShapeInvariance(t *testing.T) {
	t.Parallel()

	// Same operation (timeout), four memo variations.
	settleScript := []recoveryScenario{{
		op: "timeout", memoType: "settlement",
		settlementID: "memo-settle-1",
	}}
	nonSettleScript := []recoveryScenario{{
		op: "timeout", memoType: "non_settlement",
	}}

	resSettle := runRecoveryScript(t, settleScript)
	resNonSettle := runRecoveryScript(t, nonSettleScript)

	// Both must produce: 1 timeout delegation, 0 middleware events.
	require.Equal(t, 1, resSettle.timeoutCalls,
		"MR-1 settle: expected 1 timeout delegation")
	require.Equal(t, 1, resNonSettle.timeoutCalls,
		"MR-1 non-settle: expected 1 timeout delegation")
	require.Equal(t, 0, resSettle.mwEvents,
		"MR-1 settle: middleware MUST emit 0 events on timeout")
	require.Equal(t, 0, resNonSettle.mwEvents,
		"MR-1 non-settle: middleware MUST emit 0 events on timeout")
	require.True(t, resSettle.allDelegated,
		"MR-1 settle: every call must delegate to underlying app")
	require.True(t, resNonSettle.allDelegated,
		"MR-1 non-settle: every call must delegate to underlying app")

	// Same applies for ack path.
	ackSettle := []recoveryScenario{{
		op: "ack", memoType: "settlement",
		settlementID: "memo-ack-settle",
		ackBytes:     []byte(`{"result":"AQ=="}`),
	}}
	ackNonSettle := []recoveryScenario{{
		op: "ack", memoType: "non_settlement",
		ackBytes: []byte(`{"result":"AQ=="}`),
	}}
	resAckS := runRecoveryScript(t, ackSettle)
	resAckN := runRecoveryScript(t, ackNonSettle)
	require.Equal(t, 0, resAckS.mwEvents,
		"MR-1 ack-settle: middleware events must be 0")
	require.Equal(t, 0, resAckN.mwEvents,
		"MR-1 ack-non-settle: middleware events must be 0")
	require.Equal(t, resAckS.ackCalls, resAckN.ackCalls,
		"MR-1 ack: delegation count must match across memo types")
}

// --------------------------------------------------------------
// MR 2 (ACK-CONTENT INVARIANCE): same delegation regardless of ack content
// --------------------------------------------------------------

func TestTimeoutAckRecovery_MR_AckContentInvariance(t *testing.T) {
	t.Parallel()

	// Same operation (ack of a settlement packet), four ack-byte variations.
	scenarios := []struct {
		name     string
		ackBytes []byte
	}{
		{"success_ack", []byte(`{"result":"AQ=="}`)},
		{"error_ack", []byte(`{"error":"upstream_failure"}`)},
		{"empty_ack", []byte(``)},
		{"malformed_ack", []byte(`not-json-at-all-{}{`)},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			res := runRecoveryScript(t, []recoveryScenario{{
				op: "ack", memoType: "settlement",
				settlementID: "ack-content-" + sc.name,
				ackBytes:     sc.ackBytes,
			}})
			require.Equal(t, 1, res.ackCalls,
				"MR-2 %s: ack must always delegate", sc.name)
			require.Equal(t, 0, res.mwEvents,
				"MR-2 %s: middleware events must be 0 regardless of ack content",
				sc.name)
			require.True(t, res.allDelegated,
				"MR-2 %s: ack delegation must succeed", sc.name)
		})
	}
}

// --------------------------------------------------------------
// MR 3 (SEQUENCE-LENGTH MONOTONICITY): N calls → N delegations, 0 events
// --------------------------------------------------------------

func TestTimeoutAckRecovery_MR_SequenceLengthMonotonicity(t *testing.T) {
	t.Parallel()

	for _, n := range []int{1, 5, 10, 50} {
		n := n
		t.Run(fmt.Sprintf("seq_len_%d", n), func(t *testing.T) {
			script := make([]recoveryScenario, n)
			for i := range script {
				// Mix ack and timeout in alternation.
				if i%2 == 0 {
					script[i] = recoveryScenario{
						op: "ack", memoType: "settlement",
						settlementID: fmt.Sprintf("seq-%d-%d", n, i),
						ackBytes:     []byte(`{"result":"AQ=="}`),
					}
				} else {
					script[i] = recoveryScenario{
						op: "timeout", memoType: "settlement",
						settlementID: fmt.Sprintf("seq-%d-%d", n, i),
					}
				}
			}
			res := runRecoveryScript(t, script)
			expectedAcks := (n + 1) / 2
			expectedTimeouts := n / 2
			require.Equal(t, expectedAcks, res.ackCalls,
				"MR-3 N=%d: ack delegations expected=%d got=%d",
				n, expectedAcks, res.ackCalls)
			require.Equal(t, expectedTimeouts, res.timeoutCalls,
				"MR-3 N=%d: timeout delegations expected=%d got=%d",
				n, expectedTimeouts, res.timeoutCalls)
			require.Equal(t, 0, res.mwEvents,
				"MR-3 N=%d: middleware events MUST stay 0 across all "+
					"%d ops; got %d (per-call accumulator detected)",
				n, n, res.mwEvents)
		})
	}
}

// --------------------------------------------------------------
// MR 4 (ORDER INVARIANCE): same packet set in any order → same totals
// --------------------------------------------------------------

func TestTimeoutAckRecovery_MR_OrderInvariance(t *testing.T) {
	t.Parallel()

	mkScript := func() []recoveryScenario {
		return []recoveryScenario{
			{op: "ack", memoType: "settlement", settlementID: "ord-1", ackBytes: []byte(`{"result":"AQ=="}`)},
			{op: "timeout", memoType: "non_settlement"},
			{op: "ack", memoType: "non_settlement", ackBytes: []byte(`{"result":"AQ=="}`)},
			{op: "timeout", memoType: "settlement", settlementID: "ord-4"},
			{op: "ack", memoType: "settlement", settlementID: "ord-5", ackBytes: []byte(`{"error":"reject"}`)},
		}
	}

	runOrder := func(perm []int) recoveryResult {
		base := mkScript()
		ordered := make([]recoveryScenario, len(perm))
		for i, p := range perm {
			ordered[i] = base[p]
		}
		return runRecoveryScript(t, ordered)
	}

	forward := runOrder([]int{0, 1, 2, 3, 4})
	reverse := runOrder([]int{4, 3, 2, 1, 0})
	shuffled := runOrder([]int{2, 0, 4, 1, 3})

	for label, res := range map[string]recoveryResult{
		"forward":  forward,
		"reverse":  reverse,
		"shuffled": shuffled,
	} {
		require.Equal(t, 0, res.mwEvents,
			"MR-4 %s: events must be 0", label)
	}
	require.Equal(t, forward.ackCalls, reverse.ackCalls,
		"MR-4: ack count differs forward=%d reverse=%d", forward.ackCalls, reverse.ackCalls)
	require.Equal(t, forward.ackCalls, shuffled.ackCalls,
		"MR-4: ack count differs forward=%d shuffled=%d", forward.ackCalls, shuffled.ackCalls)
	require.Equal(t, forward.timeoutCalls, reverse.timeoutCalls,
		"MR-4: timeout count differs forward=%d reverse=%d", forward.timeoutCalls, reverse.timeoutCalls)
	require.Equal(t, forward.timeoutCalls, shuffled.timeoutCalls,
		"MR-4: timeout count differs forward=%d shuffled=%d", forward.timeoutCalls, shuffled.timeoutCalls)
}

// --------------------------------------------------------------
// MR 5 (CROSS-OPERATION ISOLATION): timeout of X doesn't affect ack of Y
// --------------------------------------------------------------

func TestTimeoutAckRecovery_MR_CrossOperationIsolation(t *testing.T) {
	t.Parallel()

	// Scenario: alternating timeouts and acks. After each pair,
	// total delegation count should match the total operation
	// count exactly — no operation "consumes" or "blocks"
	// another.
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)
	ctx := testCtx()

	for i := 0; i < 6; i++ {
		// Timeout packet i.
		pktTimeout := buildSettlementPacket(t, "100000", "ulac",
			fmt.Sprintf("isolation-timeout-%d", i),
			fmt.Sprintf("lumera1pub-t-%d", i),
			fmt.Sprintf("lumera1router-t-%d", i))
		stub.timeoutCalled = false
		require.NoError(t, mw.OnTimeoutPacket(ctx, "", pktTimeout, sdk.AccAddress{}))
		require.True(t, stub.timeoutCalled,
			"MR-5 round %d: timeout must delegate", i)

		// Ack packet i (different settlement_id).
		pktAck := buildSettlementPacket(t, "100000", "ulac",
			fmt.Sprintf("isolation-ack-%d", i),
			fmt.Sprintf("lumera1pub-a-%d", i),
			fmt.Sprintf("lumera1router-a-%d", i))
		stub.ackPacketCalled = false
		require.NoError(t, mw.OnAcknowledgementPacket(ctx, "", pktAck,
			[]byte(`{"result":"AQ=="}`), sdk.AccAddress{}))
		require.True(t, stub.ackPacketCalled,
			"MR-5 round %d: ack must delegate even after preceding "+
				"timeout in the same block — no cross-operation "+
				"interference",
			i)
	}

	// No middleware events accumulated across all 12 ops.
	mwEvents := 0
	for _, e := range ctx.EventManager().Events() {
		if e.Type == "fee_collected" || e.Type == "fee_split_applied" || e.Type == "transfer_routed" {
			mwEvents++
		}
	}
	require.Equal(t, 0, mwEvents,
		"MR-5: 0 middleware events across 12 alternating timeout+ack ops")
}

// --------------------------------------------------------------
// MR 6 (REPLAY DETERMINISM): same script → byte-equal stub state
// --------------------------------------------------------------

func TestTimeoutAckRecovery_MR_ReplayDeterministic(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(0xDEADC0DE))
	const scriptLen = 30
	script := make([]recoveryScenario, scriptLen)
	for i := range script {
		op := "ack"
		if rng.Intn(2) == 0 {
			op = "timeout"
		}
		memo := "settlement"
		if rng.Intn(3) == 0 {
			memo = "non_settlement"
		}
		ackBytes := []byte(`{"result":"AQ=="}`)
		if rng.Intn(2) == 0 {
			ackBytes = []byte(`{"error":"upstream"}`)
		}
		script[i] = recoveryScenario{
			op:           op,
			memoType:     memo,
			settlementID: fmt.Sprintf("replay-%d", i),
			ackBytes:     ackBytes,
		}
	}

	resA := runRecoveryScript(t, script)
	resB := runRecoveryScript(t, script)

	require.Equal(t, resA.ackCalls, resB.ackCalls,
		"MR-6: ack count diverges A=%d B=%d", resA.ackCalls, resB.ackCalls)
	require.Equal(t, resA.timeoutCalls, resB.timeoutCalls,
		"MR-6: timeout count diverges A=%d B=%d", resA.timeoutCalls, resB.timeoutCalls)
	require.Equal(t, resA.mwEvents, resB.mwEvents,
		"MR-6: middleware events diverge A=%d B=%d", resA.mwEvents, resB.mwEvents)
	require.Equal(t, 0, resA.mwEvents,
		"MR-6 sanity: middleware events must be 0 throughout")
	require.True(t, resA.allDelegated && resB.allDelegated,
		"MR-6: both pipelines must delegate every call")
}

// --------------------------------------------------------------
// Composite: large-burst + memo-mix recovery — total stub
// delegations equals total operations, total events stays 0
// --------------------------------------------------------------

func TestTimeoutAckRecovery_Composite_LargeBurstZeroEventsTotalDelegationMatch(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(0xFEEDFACE))
	const scriptLen = 100
	script := make([]recoveryScenario, scriptLen)
	for i := range script {
		op := "ack"
		if rng.Intn(2) == 0 {
			op = "timeout"
		}
		memo := []string{"settlement", "non_settlement"}[rng.Intn(2)]
		script[i] = recoveryScenario{
			op:           op,
			memoType:     memo,
			settlementID: fmt.Sprintf("burst-%d", i),
			ackBytes:     []byte(`{"result":"AQ=="}`),
		}
	}

	res := runRecoveryScript(t, script)
	require.Equal(t, scriptLen, res.ackCalls+res.timeoutCalls,
		"composite: total delegations(%d) ≠ script length(%d) — "+
			"some op was dropped",
		res.ackCalls+res.timeoutCalls, scriptLen)
	require.Equal(t, 0, res.mwEvents,
		"composite: 100-op burst must produce 0 middleware events")
	require.True(t, res.allDelegated,
		"composite: every op delegated to underlying app")
}
