//go:build cosmos

package ibc

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// FeeSplitParams validation
// ---------------------------------------------------------------------------

func TestFeeSplitParams_Validate_Default(t *testing.T) {
	p := DefaultFeeSplitParams()
	require.NoError(t, p.Validate())
	assert.Zero(t, p.InsuranceBPS)
}

func TestFeeSplitParams_Validate_BurnExceedsMax(t *testing.T) {
	p := DefaultFeeSplitParams()
	p.BurnBPS = MaxBPS + 1
	assert.ErrorContains(t, p.Validate(), "burn_bps")
}

func TestFeeSplitParams_Validate_InsuranceExceedsMax(t *testing.T) {
	p := DefaultFeeSplitParams()
	p.InsuranceBPS = MaxBPS + 1
	assert.ErrorContains(t, p.Validate(), "insurance_bps")
}

func TestFeeSplitParams_Validate_BurnPlusInsuranceExceedsMax(t *testing.T) {
	p := DefaultFeeSplitParams()
	p.BurnBPS = 6000
	p.InsuranceBPS = 5000
	assert.ErrorContains(t, p.Validate(), "burn_bps + insurance_bps")
}

func TestFeeSplitParams_Validate_SplitNotTenThousand(t *testing.T) {
	p := DefaultFeeSplitParams()
	p.PublisherBPS = 5000
	// 5000 + 2000 + 1000 = 8000 ≠ 10000
	assert.ErrorContains(t, p.Validate(), "must equal")
}

func TestFeeSplitParams_Validate_RouteShareExceedsMax(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*FeeSplitParams)
		want   string
	}{
		{
			name: "publisher",
			mutate: func(p *FeeSplitParams) {
				p.PublisherBPS = MaxBPS + 1
			},
			want: "publisher_bps",
		},
		{
			name: "router",
			mutate: func(p *FeeSplitParams) {
				p.RouterBPS = MaxBPS + 1
			},
			want: "router_bps",
		},
		{
			name: "referrer",
			mutate: func(p *FeeSplitParams) {
				p.ReferrerBPS = MaxBPS + 1
			},
			want: "referrer_bps",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultFeeSplitParams()
			tc.mutate(&p)
			assert.ErrorContains(t, p.Validate(), tc.want)
		})
	}
}

func TestFeeSplitParams_Validate_RouteShareOverflow(t *testing.T) {
	p := FeeSplitParams{
		PublisherBPS: ^uint32(0),
		RouterBPS:    MaxBPS + 1,
		ReferrerBPS:  0,
	}
	assert.ErrorContains(t, p.Validate(), "publisher_bps")
}

func TestFeeSplitParams_Validate_CustomValid(t *testing.T) {
	p := FeeSplitParams{
		BurnBPS:      500,
		InsuranceBPS: 100,
		PublisherBPS: 6000,
		RouterBPS:    2500,
		ReferrerBPS:  1500,
	}
	require.NoError(t, p.Validate())
}

// ---------------------------------------------------------------------------
// ComputeFeeSplit
// ---------------------------------------------------------------------------

func TestComputeFeeSplit_Default(t *testing.T) {
	amount := sdkmath.NewInt(10000)
	split, err := ComputeFeeSplit(amount, "ulac", "settle-1", DefaultFeeSplitParams())
	require.NoError(t, err)

	assert.Equal(t, "settle-1", split.SettlementID)
	assert.Equal(t, "ulac", split.Denom)

	// Burn: 10000 * 300 / 10000 = 300
	assert.Equal(t, sdkmath.NewInt(300), split.BurnAmount)
	// After burn: 9700
	assert.True(t, split.Insurance.IsZero())
	assert.Equal(t, sdkmath.NewInt(9700), split.NetAmount)
	assert.Equal(t, sdkmath.NewInt(1940), split.Router)
	assert.Equal(t, sdkmath.NewInt(970), split.Referrer)
	assert.Equal(t, sdkmath.NewInt(6790), split.Publisher)

	// Verify conservation: burn + insurance + publisher + router + referrer = total
	total := split.BurnAmount.Add(split.Insurance).Add(split.Publisher).Add(split.Router).Add(split.Referrer)
	assert.Equal(t, amount, total)
}

func TestComputeFeeSplit_ZeroAmount(t *testing.T) {
	split, err := ComputeFeeSplit(sdkmath.ZeroInt(), "ulac", "settle-0", DefaultFeeSplitParams())
	require.NoError(t, err)
	assert.True(t, split.BurnAmount.IsZero())
	assert.True(t, split.Insurance.IsZero())
	assert.True(t, split.Publisher.IsZero())
	assert.True(t, split.Router.IsZero())
	assert.True(t, split.Referrer.IsZero())
}

func TestComputeFeeSplit_NegativeAmount(t *testing.T) {
	_, err := ComputeFeeSplit(sdkmath.NewInt(-100), "ulac", "settle-neg", DefaultFeeSplitParams())
	assert.ErrorContains(t, err, "non-negative")
}

func TestComputeFeeSplit_InvalidParams(t *testing.T) {
	bad := FeeSplitParams{PublisherBPS: 5000, RouterBPS: 2000, ReferrerBPS: 1000}
	_, err := ComputeFeeSplit(sdkmath.NewInt(1000), "ulac", "s1", bad)
	assert.ErrorContains(t, err, "invalid fee split params")
}

func TestComputeFeeSplit_LargeAmount_Conservation(t *testing.T) {
	// Test with a large amount to verify no rounding loss.
	amount := sdkmath.NewInt(999_999_999_999)
	split, err := ComputeFeeSplit(amount, "ulac", "large", DefaultFeeSplitParams())
	require.NoError(t, err)

	total := split.BurnAmount.Add(split.Insurance).Add(split.Publisher).Add(split.Router).Add(split.Referrer)
	assert.Equal(t, amount, total, "conservation violated for large amount")
}

func TestComputeFeeSplit_SmallAmount_Conservation(t *testing.T) {
	// Small amounts where integer division causes truncation.
	for _, amt := range []int64{1, 2, 3, 7, 11, 97, 100, 333} {
		t.Run(fmt.Sprintf("amount_%d", amt), func(t *testing.T) {
			amount := sdkmath.NewInt(amt)
			split, err := ComputeFeeSplit(amount, "ulac", "small", DefaultFeeSplitParams())
			require.NoError(t, err)

			total := split.BurnAmount.Add(split.Insurance).Add(split.Publisher).Add(split.Router).Add(split.Referrer)
			assert.Equal(t, amount, total, "conservation violated for amount %d", amt)
		})
	}
}

func TestComputeFeeSplit_NoBurn(t *testing.T) {
	params := FeeSplitParams{
		BurnBPS:      0,
		InsuranceBPS: 0,
		PublisherBPS: 7000,
		RouterBPS:    2000,
		ReferrerBPS:  1000,
	}
	amount := sdkmath.NewInt(10000)
	split, err := ComputeFeeSplit(amount, "ulac", "no-burn", params)
	require.NoError(t, err)

	assert.True(t, split.BurnAmount.IsZero())
	assert.True(t, split.Insurance.IsZero())
	assert.Equal(t, amount, split.NetAmount)
	// Publisher: 7000, Router: 2000, Referrer: 1000
	assert.Equal(t, sdkmath.NewInt(7000), split.Publisher)
	assert.Equal(t, sdkmath.NewInt(2000), split.Router)
	assert.Equal(t, sdkmath.NewInt(1000), split.Referrer)
}

func TestComputeFeeSplit_AllToPublisher(t *testing.T) {
	params := FeeSplitParams{
		BurnBPS:      0,
		InsuranceBPS: 0,
		PublisherBPS: MaxBPS,
		RouterBPS:    0,
		ReferrerBPS:  0,
	}
	amount := sdkmath.NewInt(12345)
	split, err := ComputeFeeSplit(amount, "ulac", "all-pub", params)
	require.NoError(t, err)
	assert.Equal(t, amount, split.Publisher)
	assert.True(t, split.Router.IsZero())
	assert.True(t, split.Referrer.IsZero())
}

// ---------------------------------------------------------------------------
// bpsOf
// ---------------------------------------------------------------------------

func TestBpsOf(t *testing.T) {
	assert.Equal(t, sdkmath.NewInt(300), bpsOf(sdkmath.NewInt(10000), 300))
	assert.Equal(t, sdkmath.ZeroInt(), bpsOf(sdkmath.NewInt(10000), 0))
	assert.Equal(t, sdkmath.NewInt(10000), bpsOf(sdkmath.NewInt(10000), 10000))
	// Truncation: 33 * 3333 / 10000 = 10 (truncated from 10.9989)
	assert.Equal(t, sdkmath.NewInt(10), bpsOf(sdkmath.NewInt(33), 3333))
}

// ---------------------------------------------------------------------------
// Stub types for middleware tests
// ---------------------------------------------------------------------------

type stubIBCModule struct {
	recvPacketCalled     bool
	recvPacketAck        ibcexported.Acknowledgement
	recvPacketVersion    string
	timeoutCalled        bool
	timeoutVersion       string
	ackPacketCalled      bool
	ackPacketVersion     string
	chanOpenInitCalled   bool
	setICS4WrapperCalled bool
	lastSetWrapper       porttypes.ICS4Wrapper
}

func (s *stubIBCModule) OnChanOpenInit(_ sdk.Context, _ channeltypes.Order, _ []string, _, _ string, _ channeltypes.Counterparty, version string) (string, error) {
	s.chanOpenInitCalled = true
	return version, nil
}

func (s *stubIBCModule) OnChanOpenTry(_ sdk.Context, _ channeltypes.Order, _ []string, _, _ string, _ channeltypes.Counterparty, _ string) (string, error) {
	return "", nil
}

func (s *stubIBCModule) OnChanOpenAck(_ sdk.Context, _, _, _, _ string) error { return nil }

func (s *stubIBCModule) OnChanOpenConfirm(_ sdk.Context, _, _ string) error { return nil }

func (s *stubIBCModule) OnChanCloseInit(_ sdk.Context, _, _ string) error { return nil }

func (s *stubIBCModule) OnChanCloseConfirm(_ sdk.Context, _, _ string) error { return nil }

func (s *stubIBCModule) OnRecvPacket(_ sdk.Context, channelVersion string, _ channeltypes.Packet, _ sdk.AccAddress) ibcexported.Acknowledgement {
	s.recvPacketCalled = true
	s.recvPacketVersion = channelVersion
	if s.recvPacketAck != nil {
		return s.recvPacketAck
	}
	return channeltypes.NewResultAcknowledgement([]byte("ok"))
}

func (s *stubIBCModule) OnAcknowledgementPacket(_ sdk.Context, channelVersion string, _ channeltypes.Packet, _ []byte, _ sdk.AccAddress) error {
	s.ackPacketCalled = true
	s.ackPacketVersion = channelVersion
	return nil
}

func (s *stubIBCModule) OnTimeoutPacket(_ sdk.Context, channelVersion string, _ channeltypes.Packet, _ sdk.AccAddress) error {
	s.timeoutCalled = true
	s.timeoutVersion = channelVersion
	return nil
}

func (s *stubIBCModule) SetICS4Wrapper(wrapper porttypes.ICS4Wrapper) {
	s.setICS4WrapperCalled = true
	s.lastSetWrapper = wrapper
}

type stubICS4Wrapper struct {
	sendPacketSeq uint64
}

type recordingSplitExecutor struct {
	called bool
	memo   SettlementMemo
	split  FeeSplitResult
}

func (e *recordingSplitExecutor) Execute(_ sdk.Context, _ channeltypes.Packet, memo SettlementMemo, split FeeSplitResult) error {
	e.called = true
	e.memo = memo
	e.split = split
	return nil
}

func (s *stubICS4Wrapper) SendPacket(_ sdk.Context, _, _ string, _ clienttypes.Height, _ uint64, _ []byte) (uint64, error) {
	s.sendPacketSeq++
	return s.sendPacketSeq, nil
}

func (s *stubICS4Wrapper) WriteAcknowledgement(_ sdk.Context, _ ibcexported.PacketI, _ ibcexported.Acknowledgement) error {
	return nil
}

func (s *stubICS4Wrapper) GetAppVersion(_ sdk.Context, _, _ string) (string, bool) {
	return ChannelVersion, true
}

// ---------------------------------------------------------------------------
// Middleware construction
// ---------------------------------------------------------------------------

func TestNewFeeSplitMiddleware_Valid(t *testing.T) {
	mw, err := NewFeeSplitMiddleware(&stubIBCModule{}, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)
	assert.NotNil(t, mw.app)
}

func TestNewFeeSplitMiddleware_InvalidParams(t *testing.T) {
	_, err := NewFeeSplitMiddleware(&stubIBCModule{}, &stubICS4Wrapper{}, FeeSplitParams{})
	assert.ErrorContains(t, err, "fee split middleware")
}

// ---------------------------------------------------------------------------
// Helper: build test packet
// ---------------------------------------------------------------------------

func buildSettlementPacket(t *testing.T, amount, denom, settlementID, publisher, router string) channeltypes.Packet {
	t.Helper()
	return buildSettlementPacketWithParties(t, amount, denom, "sender", "receiver", settlementID, publisher, router)
}

func buildSettlementPacketWithParties(t *testing.T, amount, denom, sender, receiver, settlementID, publisher, router string) channeltypes.Packet {
	t.Helper()
	memo, err := BuildSettlementMemo(SettlementMemo{
		Type:          MemoTypeSettlement,
		SettlementID:  settlementID,
		Publisher:     publisher,
		Router:        router,
		RefundAddress: "lumera1refund",
	})
	require.NoError(t, err)

	ftData := transfertypes.NewFungibleTokenPacketData(denom, amount, sender, receiver, memo)
	data, err := json.Marshal(ftData)
	require.NoError(t, err)

	return channeltypes.Packet{
		Sequence:           1,
		SourcePort:         "transfer",
		SourceChannel:      "channel-0",
		DestinationPort:    "transfer",
		DestinationChannel: "channel-1",
		Data:               data,
	}
}

func testCtx() sdk.Context {
	return sdk.Context{}.
		WithBlockTime(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)).
		WithEventManager(sdk.NewEventManager())
}

// ---------------------------------------------------------------------------
// Middleware: OnRecvPacket – happy path
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_OnRecvPacket_Settlement(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "10000", "ulac", "settle-42", "pub-addr", "router-addr")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	// Underlying app should be called.
	assert.True(t, stub.recvPacketCalled)
	// Ack should be successful.
	assert.True(t, ack.Success(), "expected successful ack")

	// Verify events were emitted.
	events := ctx.EventManager().Events()
	eventTypes := make(map[string]int)
	for _, e := range events {
		eventTypes[e.Type]++
	}

	assert.Equal(t, 1, eventTypes["fee_collected"], "expected fee_collected event")
	assert.Equal(t, 1, eventTypes["fee_split_applied"], "expected fee_split_applied event")
	assert.GreaterOrEqual(t, eventTypes["transfer_routed"], 3, "expected at least 3 transfer_routed events")

	// Verify fee_split_applied attributes.
	for _, e := range events {
		if e.Type == "fee_split_applied" {
			attrs := eventAttrs(e)
			assert.Equal(t, "settle-42", attrs["settlement_id"])
			assert.Equal(t, "300", attrs["burn_amount"]) // 10000 * 3% = 300
			assert.Equal(t, "0", attrs["insurance_amount"])
			assert.Equal(t, "9700", attrs["net_amount"])
			assert.Equal(t, "ulac", attrs["denom"])
		}
	}
}

// ---------------------------------------------------------------------------
// Middleware: OnRecvPacket – non-settlement passthrough
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_OnRecvPacket_NonSettlement(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// ICS-20 packet without settlement memo.
	ftData := transfertypes.NewFungibleTokenPacketData("ulac", "1000", "sender", "receiver", "")
	data, _ := json.Marshal(ftData)
	packet := channeltypes.Packet{Data: data}

	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.True(t, stub.recvPacketCalled)
	assert.True(t, ack.Success())
	// No fee events for non-settlement.
	assert.Empty(t, ctx.EventManager().Events())
}

// ---------------------------------------------------------------------------
// Middleware: OnRecvPacket – non-ICS-20 passthrough
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_OnRecvPacket_NonICS20(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Non-JSON data.
	packet := channeltypes.Packet{Data: []byte("not-json")}
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.True(t, stub.recvPacketCalled)
	assert.True(t, ack.Success())
}

// ---------------------------------------------------------------------------
// Middleware: OnRecvPacket – invalid amount
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_OnRecvPacket_InvalidAmount(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "-500", "ulac", "settle-bad", "pub", "router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.False(t, ack.Success(), "expected error ack for negative amount")
	assert.False(t, stub.recvPacketCalled, "underlying app should not be called on error")
}

// Regression for lumera_ai-qke3g: FeeSplitMiddleware.OnRecvPacket must
// reject packet.Data larger than MaxICS20PacketDataBytes BEFORE handing
// the bytes to json.Unmarshal. A misbehaving counterparty chain could
// otherwise pipeline max-size packets (consensus block limit ~1 MiB)
// whose dense JSON burns disproportionate Unmarshal compute on every
// destination validator.
func TestFeeSplitMiddleware_OnRecvPacket_RejectsOversizedPacketData(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Build a packet strictly larger than the cap. Content doesn't matter
	// — the guard must trip on length before Unmarshal runs.
	oversized := make([]byte, MaxICS20PacketDataBytes+1)
	for i := range oversized {
		oversized[i] = ' ' // valid JSON whitespace — would otherwise parse
	}
	oversized[0] = '{'
	oversized[len(oversized)-1] = '}'
	packet := channeltypes.Packet{Data: oversized}
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.False(t, ack.Success(), "oversized packet must return error ack")
	assert.False(t, stub.recvPacketCalled,
		"underlying app must NOT be invoked on oversized packet — the rejection "+
			"must short-circuit before Unmarshal runs")
}

// Exact-at-cap packet must be accepted — the cap is a threshold, not a
// floor, and legitimate-but-large packets should not be spuriously
// rejected.
func TestFeeSplitMiddleware_OnRecvPacket_AtCapPacketAccepted(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Fill to exactly MaxICS20PacketDataBytes with non-JSON bytes. The
	// middleware falls through to the underlying app (non-ICS20 path),
	// exercising the fact that the cap check passes.
	atCap := make([]byte, MaxICS20PacketDataBytes)
	for i := range atCap {
		atCap[i] = 'x'
	}
	packet := channeltypes.Packet{Data: atCap}
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.True(t, stub.recvPacketCalled,
		"at-cap packet must pass the size guard and fall through to the underlying app")
	assert.True(t, ack.Success())
}

// Regression for lumera_ai-9l1if: zero-amount settlement transfers must be
// rejected, not silently turned into no-op fee splits with bogus audit events.
func TestFeeSplitMiddleware_OnRecvPacket_ZeroAmountRejected(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "0", "ulac", "settle-zero", "pub", "router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.False(t, ack.Success(), "expected error ack for zero amount")
	assert.False(t, stub.recvPacketCalled, "underlying app must not be called on rejected packet")
}

// Regression for lumera_ai-9l1if: amounts beyond MaxSettlementAmountBits (128
// bits, far past any plausible real transfer) must be rejected. A counterparty
// relayer controls this string and can otherwise smuggle pathologically large
// values into BPS math, event attributes, and downstream indexer state.
func TestFeeSplitMiddleware_OnRecvPacket_AmountTooLargeRejected(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// 2^130 — well above the 128-bit cap, well below sdkmath.Int's 256-bit
	// parse limit, so it would parse and pass IsPositive without the bounds
	// check that this test guards.
	huge := sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 130))
	require.True(t, huge.IsPositive())
	require.Greater(t, huge.BigInt().BitLen(), MaxSettlementAmountBits,
		"test setup must produce an amount above the cap")

	packet := buildSettlementPacket(t, huge.String(), "ulac", "settle-huge", "pub", "router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.False(t, ack.Success(), "expected error ack for over-cap amount")
	assert.False(t, stub.recvPacketCalled, "underlying app must not be called on rejected packet")
}

// An amount exactly at the 128-bit boundary is still pathological for normal
// use but is the inclusive cap; document the boundary by accepting 2^128 - 1.
func TestFeeSplitMiddleware_OnRecvPacket_AmountAtCapAccepted(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// 2^128 - 1: exactly MaxSettlementAmountBits bits.
	max128 := sdkmath.NewIntFromBigInt(
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)),
	)
	require.Equal(t, MaxSettlementAmountBits, max128.BigInt().BitLen())

	packet := buildSettlementPacket(t, max128.String(), "ulac", "settle-cap", "pub", "router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.True(t, ack.Success(), "amount exactly at cap should pass; got ack=%v", ack)
}

// ---------------------------------------------------------------------------
// Middleware: OnRecvPacket – missing denom
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_OnRecvPacket_MissingDenom(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "1000", "", "settle-nodenom", "pub", "router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.False(t, ack.Success(), "expected error ack for missing denom")
}

func TestFeeSplitMiddleware_OnRecvPacket_RejectsNonCanonicalDenom(t *testing.T) {
	cases := []struct {
		name  string
		denom string
	}{
		{name: "leading_space", denom: " ulac"},
		{name: "trailing_space", denom: "ulac "},
		{name: "embedded_space", denom: "u lac"},
		{name: "embedded_tab", denom: "u\tlac"},
		{name: "embedded_control", denom: "u\x7flac"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubIBCModule{}
			mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
			require.NoError(t, err)

			packet := buildSettlementPacket(t, "1000", tc.denom, "settle-"+tc.name, "pub", "router")
			ctx := testCtx()
			ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

			assert.False(t, ack.Success(), "expected error ack for non-canonical denom %q", tc.denom)
			assert.False(t, stub.recvPacketCalled, "invalid denom must not reach the transfer app")
			assert.Empty(t, ctx.EventManager().Events(), "invalid denom must not emit fee-split audit events")
		})
	}
}

func TestFeeSplitMiddleware_OnRecvPacket_RejectsNonCanonicalSenderReceiver(t *testing.T) {
	cases := []struct {
		name     string
		sender   string
		receiver string
	}{
		{name: "missing_sender", sender: "", receiver: "receiver"},
		{name: "blank_sender", sender: " \t", receiver: "receiver"},
		{name: "padded_sender", sender: "sender ", receiver: "receiver"},
		{name: "embedded_sender_control", sender: "send\ner", receiver: "receiver"},
		{name: "missing_receiver", sender: "sender", receiver: ""},
		{name: "blank_receiver", sender: "sender", receiver: "\t"},
		{name: "padded_receiver", sender: "sender", receiver: " receiver"},
		{name: "embedded_receiver_control", sender: "sender", receiver: "receiv\ter"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubIBCModule{}
			mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
			require.NoError(t, err)

			packet := buildSettlementPacketWithParties(
				t,
				"1000",
				"ulac",
				tc.sender,
				tc.receiver,
				"settle-"+tc.name,
				"pub",
				"router",
			)
			ctx := testCtx()
			ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

			assert.False(t, ack.Success(), "expected error ack for sender=%q receiver=%q", tc.sender, tc.receiver)
			assert.False(t, stub.recvPacketCalled, "invalid sender/receiver must not reach the transfer app")
			assert.Empty(t, ctx.EventManager().Events(), "invalid sender/receiver must not emit fee-split audit events")
		})
	}
}

func TestFeeSplitMiddleware_OnRecvPacket_RejectsNonCanonicalChannels(t *testing.T) {
	cases := []struct {
		name               string
		sourceChannel      string
		destinationChannel string
	}{
		{name: "missing_source_channel", sourceChannel: "", destinationChannel: "channel-1"},
		{name: "short_source_channel", sourceChannel: "c", destinationChannel: "channel-1"},
		{name: "padded_source_channel", sourceChannel: " channel-0", destinationChannel: "channel-1"},
		{name: "invalid_source_channel_char", sourceChannel: "channel/0", destinationChannel: "channel-1"},
		{name: "long_source_channel", sourceChannel: strings.Repeat("a", maxIBCIdentifierLength+1), destinationChannel: "channel-1"},
		{name: "missing_destination_channel", sourceChannel: "channel-0", destinationChannel: ""},
		{name: "short_destination_channel", sourceChannel: "channel-0", destinationChannel: "c"},
		{name: "padded_destination_channel", sourceChannel: "channel-0", destinationChannel: "channel-1 "},
		{name: "invalid_destination_channel_control", sourceChannel: "channel-0", destinationChannel: "channel-\n1"},
		{name: "long_destination_channel", sourceChannel: "channel-0", destinationChannel: strings.Repeat("z", maxIBCIdentifierLength+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubIBCModule{}
			mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
			require.NoError(t, err)

			packet := buildSettlementPacket(t, "1000", "ulac", "settle-"+tc.name, "pub", "router")
			packet.SourceChannel = tc.sourceChannel
			packet.DestinationChannel = tc.destinationChannel
			ctx := testCtx()
			ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

			assert.False(t, ack.Success(), "expected error ack for source=%q destination=%q", tc.sourceChannel, tc.destinationChannel)
			assert.False(t, stub.recvPacketCalled, "invalid channels must not reach the transfer app")
			assert.Empty(t, ctx.EventManager().Events(), "invalid channels must not emit fee-split audit events")
		})
	}
}

func TestFeeSplitMiddleware_OnRecvPacket_RejectsNonTransferPorts(t *testing.T) {
	cases := []struct {
		name            string
		sourcePort      string
		destinationPort string
	}{
		{name: "missing_source_port", sourcePort: "", destinationPort: transfertypes.PortID},
		{name: "credits_source_port", sourcePort: PortID, destinationPort: transfertypes.PortID},
		{name: "padded_source_port", sourcePort: " transfer", destinationPort: transfertypes.PortID},
		{name: "missing_destination_port", sourcePort: transfertypes.PortID, destinationPort: ""},
		{name: "credits_destination_port", sourcePort: transfertypes.PortID, destinationPort: PortID},
		{name: "padded_destination_port", sourcePort: transfertypes.PortID, destinationPort: "transfer "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubIBCModule{}
			mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
			require.NoError(t, err)

			packet := buildSettlementPacket(t, "1000", "ulac", "settle-"+tc.name, "pub", "router")
			packet.SourcePort = tc.sourcePort
			packet.DestinationPort = tc.destinationPort
			ctx := testCtx()
			ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

			assert.False(t, ack.Success(), "expected error ack for source_port=%q destination_port=%q", tc.sourcePort, tc.destinationPort)
			assert.False(t, stub.recvPacketCalled, "invalid ports must not reach the transfer app")
			assert.Empty(t, ctx.EventManager().Events(), "invalid ports must not emit fee-split audit events")
		})
	}
}

func TestFeeSplitMiddleware_OnRecvPacket_RejectsZeroSequence(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "1000", "ulac", "settle-zero-sequence", "pub", "router")
	packet.Sequence = 0
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.False(t, ack.Success(), "expected error ack for zero packet sequence")
	assert.False(t, stub.recvPacketCalled, "invalid sequence must not reach the transfer app")
	assert.Empty(t, ctx.EventManager().Events(), "invalid sequence must not emit fee-split audit events")
}

// ---------------------------------------------------------------------------
// Middleware: OnRecvPacket – invalid memo (bad settlement type)
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_OnRecvPacket_InvalidMemo(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	// Memo with lumera key but wrong type.
	memo := `{"lumera":{"type":"bad_type","settlement_id":"s1","refund_address":"addr"}}`
	ftData := transfertypes.NewFungibleTokenPacketData("ulac", "1000", "sender", "receiver", memo)
	data, _ := json.Marshal(ftData)
	packet := channeltypes.Packet{Data: data}

	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	assert.False(t, ack.Success(), "expected error ack for invalid memo type")
}

// ---------------------------------------------------------------------------
// Middleware: OnTimeoutPacket – no partial split
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_OnTimeoutPacket(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "10000", "ulac", "settle-timeout", "pub", "router")
	ctx := testCtx()
	err = mw.OnTimeoutPacket(ctx, "", packet, sdk.AccAddress{})

	require.NoError(t, err)
	assert.True(t, stub.timeoutCalled, "timeout should delegate to underlying app")
	// No fee events should be emitted on timeout.
	assert.Empty(t, ctx.EventManager().Events(), "no events on timeout")
}

// ---------------------------------------------------------------------------
// Middleware: OnAcknowledgementPacket – delegates
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_OnAcknowledgementPacket(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "10000", "ulac", "settle-ack", "pub", "router")
	ctx := testCtx()
	err = mw.OnAcknowledgementPacket(ctx, "", packet, []byte(`{"result":"ok"}`), sdk.AccAddress{})

	require.NoError(t, err)
	assert.True(t, stub.ackPacketCalled)
}

// ---------------------------------------------------------------------------
// Middleware: Channel lifecycle delegates
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_ChannelLifecycle(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	_, err = mw.OnChanOpenInit(ctx, channeltypes.UNORDERED, nil, "transfer", "ch-0", channeltypes.Counterparty{}, "ics20-1")
	require.NoError(t, err)
	assert.True(t, stub.chanOpenInitCalled)
}

// ---------------------------------------------------------------------------
// Middleware: ICS4Wrapper delegates
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_ICS4Wrapper(t *testing.T) {
	wrapper := &stubICS4Wrapper{}
	mw, err := NewFeeSplitMiddleware(&stubIBCModule{}, wrapper, DefaultFeeSplitParams())
	require.NoError(t, err)

	ctx := testCtx()
	seq, err := mw.SendPacket(ctx, "transfer", "ch-0", clienttypes.Height{}, 0, []byte("data"))
	require.NoError(t, err)
	assert.Equal(t, uint64(1), seq)

	ver, ok := mw.GetAppVersion(ctx, "transfer", "ch-0")
	assert.True(t, ok)
	assert.Equal(t, ChannelVersion, ver)
}

// ---------------------------------------------------------------------------
// Middleware: Event audit – complete event trail
// ---------------------------------------------------------------------------

func TestFeeSplitMiddleware_EventAudit(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "100000", "ulac", "settle-audit", "lumera1pub", "lumera1router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	events := ctx.EventManager().Events()

	// Collect all transfer_routed roles.
	roles := map[string]string{}
	for _, e := range events {
		if e.Type == "transfer_routed" {
			attrs := eventAttrs(e)
			roles[attrs["recipient_role"]] = attrs["amount"]
		}
	}

	assert.Contains(t, roles, "publisher")
	assert.Contains(t, roles, "router")
	assert.Contains(t, roles, "referrer")
	assert.Contains(t, roles, "burn")
	assert.NotContains(t, roles, "insurance")

	// Verify the fee_split_applied event has BPS parameters.
	for _, e := range events {
		if e.Type == "fee_split_applied" {
			attrs := eventAttrs(e)
			assert.Equal(t, "300", attrs["burn_bps"])
			assert.Equal(t, "0", attrs["insurance_bps"])
			assert.Equal(t, "7000", attrs["publisher_bps"])
			assert.Equal(t, "2000", attrs["router_bps"])
			assert.Equal(t, "1000", attrs["referrer_bps"])
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func eventAttrs(e sdk.Event) map[string]string {
	m := make(map[string]string)
	for _, attr := range e.Attributes {
		m[attr.Key] = attr.Value
	}
	return m
}

// Compile-time interface checks for stubs.
var (
	_ porttypes.IBCModule   = (*stubIBCModule)(nil)
	_ porttypes.ICS4Wrapper = (*stubICS4Wrapper)(nil)
)

// ---------------------------------------------------------------------------
// s871p regression: fail-closed and honest events
// ---------------------------------------------------------------------------

// TestFeeSplitMiddleware_EventsMarkExecutedFalse documents that every split
// event carries executed=false until a real split executor is wired in.
// Downstream consumers (indexers, settlement reconciliation) must be able
// to distinguish advisory events from actual transfers without parsing the
// middleware source.
func TestFeeSplitMiddleware_EventsMarkExecutedFalse(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	packet := buildSettlementPacket(t, "10000", "ulac", "settle-s871p-1", "pub-addr", "router-addr")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})
	require.True(t, ack.Success())

	events := ctx.EventManager().Events()
	require.NotEmpty(t, events)

	splitEvents := []string{"fee_collected", "fee_split_applied", "transfer_routed"}
	checked := 0
	for _, e := range events {
		matched := false
		for _, want := range splitEvents {
			if e.Type == want {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		attrs := eventAttrs(e)
		executed, ok := attrs["executed"]
		if !ok {
			t.Errorf("%s event missing executed attribute", e.Type)
			continue
		}
		if executed != "false" {
			t.Errorf("%s event executed = %q, want \"false\" (no real split executor yet)", e.Type, executed)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no split events were checked; helper is broken")
	}
}

// TestFeeSplitMiddleware_RequireSplitExecutorFailsClosed is the fail-closed
// regression for s871p. When an operator cannot tolerate the gap between
// advertised split events and the full-amount ICS-20 transfer the underlying
// app actually performs, flipping RequireSplitExecutor must reject the
// packet before any side effects occur: no events emitted, underlying app
// not invoked.
func TestFeeSplitMiddleware_RequireSplitExecutorFailsClosed(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)
	mw = mw.WithRequireSplitExecutor(true)

	packet := buildSettlementPacket(t, "10000", "ulac", "settle-s871p-2", "pub", "router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	if ack.Success() {
		t.Fatal("expected error ack when split executor required but unavailable")
	}
	if stub.recvPacketCalled {
		t.Error("underlying app must not run when RequireSplitExecutor blocks the packet")
	}
	if evs := ctx.EventManager().Events(); len(evs) > 0 {
		t.Errorf("no split events should be emitted in fail-closed mode; got %d", len(evs))
	}
}

// TestFeeSplitMiddleware_ExecutorSkipsUnderlyingFullTransfer pins that once a
// real split executor handles the settlement, the middleware must not also
// delegate the same packet to the underlying ICS-20 app. Delegating after
// Execute would let one packet perform the per-leg split and then process the
// full transfer again.
func TestFeeSplitMiddleware_ExecutorSkipsUnderlyingFullTransfer(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)

	executor := &recordingSplitExecutor{}
	mw = mw.WithExecutor(executor)

	packet := buildSettlementPacket(t, "10000", "ulac", "settle-executed-once", "pub", "router")
	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	require.True(t, ack.Success(), "executor-owned split should return a success ack")
	require.True(t, executor.called, "split executor must handle the settlement")
	require.False(t, stub.recvPacketCalled, "underlying app must not process the full transfer after executor split")
	require.Equal(t, "settle-executed-once", executor.memo.SettlementID)
	require.True(t, executor.split.TotalAmount.Equal(sdkmath.NewInt(10000)))

	for _, event := range ctx.EventManager().Events() {
		if event.Type != "fee_collected" && event.Type != "fee_split_applied" && event.Type != "transfer_routed" {
			continue
		}
		attrs := eventAttrs(event)
		require.Equal(t, "true", attrs["executed"], "executed split events must be marked as real transfers")
	}
}

// TestFeeSplitMiddleware_RequireSplitExecutorStillAllowsNonSettlement confirms
// the fail-closed toggle only affects settlement packets. Plain ICS-20
// transfers without a Lumera memo must continue to pass through so opting
// in does not break unrelated transfer flows.
func TestFeeSplitMiddleware_RequireSplitExecutorStillAllowsNonSettlement(t *testing.T) {
	stub := &stubIBCModule{}
	mw, err := NewFeeSplitMiddleware(stub, &stubICS4Wrapper{}, DefaultFeeSplitParams())
	require.NoError(t, err)
	mw = mw.WithRequireSplitExecutor(true)

	ftData := transfertypes.NewFungibleTokenPacketData("ulac", "1000", "sender", "receiver", "")
	data, _ := json.Marshal(ftData)
	packet := channeltypes.Packet{Data: data}

	ctx := testCtx()
	ack := mw.OnRecvPacket(ctx, "", packet, sdk.AccAddress{})

	if !ack.Success() {
		t.Fatal("non-settlement packet should pass through even in fail-closed mode")
	}
	if !stub.recvPacketCalled {
		t.Error("underlying app should have been invoked for non-settlement packet")
	}
}
