//go:build cosmos

package ibc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
)

const (
	// MaxBPS is the total basis points that splits must sum to.
	MaxBPS = uint32(10000)

	// MaxSettlementAmountBits caps the bit-width of accepted settlement
	// amounts. sdkmath.Int parses up to 256 bits, but no real cross-chain
	// transfer comes anywhere near 2^128; bounding here turns adversarial
	// max-precision amounts (which would still pass IsPositive) into an
	// explicit rejection rather than letting them flow into BPS math, event
	// attributes, and indexer pipelines.
	MaxSettlementAmountBits = 128

	// DefaultBurnBPS is the default protocol burn rate on settlement (3%).
	DefaultBurnBPS = uint32(300)

	// DefaultInsuranceBPS disables premium collection until claims are production-ready.
	DefaultInsuranceBPS = uint32(0)

	// DefaultPublisherBPS is the default publisher share of net amount (70%).
	DefaultPublisherBPS = uint32(7000)

	// MaxICS20PacketDataBytes caps the size of remote-chain-submitted
	// packet.Data before json.Unmarshal. Real ICS-20 FungibleTokenPacketData
	// is under 2 KiB even with generous denoms, sender/receiver bech32
	// addresses, and a substantive memo; 64 KiB is a comfortable ceiling
	// that bounds adversarial amplification without constraining any
	// legitimate path. Without this cap, a misbehaving counterparty chain
	// can pipeline max-size packets (consensus.block.max_bytes ~1 MiB)
	// whose dense JSON costs every destination validator disproportionate
	// Unmarshal compute — see lumera_ai-qke3g for the cross-module
	// inventory of the same pattern.
	MaxICS20PacketDataBytes = 64 * 1024

	// DefaultRouterBPS is the default router share of net amount (20%).
	DefaultRouterBPS = uint32(2000)

	// DefaultReferrerBPS is the default referrer share of net amount (10%).
	DefaultReferrerBPS = uint32(1000)

	minIBCIdentifierLength = 2
	maxIBCIdentifierLength = 128
)

// FeeSplitParams configures how settlement fees are split.
type FeeSplitParams struct {
	BurnBPS      uint32 `json:"burn_bps"`
	InsuranceBPS uint32 `json:"insurance_bps"`
	PublisherBPS uint32 `json:"publisher_bps"`
	RouterBPS    uint32 `json:"router_bps"`
	ReferrerBPS  uint32 `json:"referrer_bps"`
}

// DefaultFeeSplitParams returns the default fee split parameters.
func DefaultFeeSplitParams() FeeSplitParams {
	return FeeSplitParams{
		BurnBPS:      DefaultBurnBPS,
		InsuranceBPS: DefaultInsuranceBPS,
		PublisherBPS: DefaultPublisherBPS,
		RouterBPS:    DefaultRouterBPS,
		ReferrerBPS:  DefaultReferrerBPS,
	}
}

// Validate checks that fee split parameters are consistent.
func (p FeeSplitParams) Validate() error {
	if p.BurnBPS > MaxBPS {
		return fmt.Errorf("burn_bps %d exceeds maximum %d", p.BurnBPS, MaxBPS)
	}
	if p.InsuranceBPS > MaxBPS {
		return fmt.Errorf("insurance_bps %d exceeds maximum %d", p.InsuranceBPS, MaxBPS)
	}
	if p.BurnBPS+p.InsuranceBPS > MaxBPS {
		return fmt.Errorf("burn_bps + insurance_bps = %d exceeds %d", p.BurnBPS+p.InsuranceBPS, MaxBPS)
	}
	for _, share := range []struct {
		name  string
		value uint32
	}{
		{name: "publisher_bps", value: p.PublisherBPS},
		{name: "router_bps", value: p.RouterBPS},
		{name: "referrer_bps", value: p.ReferrerBPS},
	} {
		if share.value > MaxBPS {
			return fmt.Errorf("%s %d exceeds maximum %d", share.name, share.value, MaxBPS)
		}
	}
	splitTotal := p.PublisherBPS + p.RouterBPS + p.ReferrerBPS
	if splitTotal != MaxBPS {
		return fmt.Errorf("publisher_bps + router_bps + referrer_bps must equal %d, got %d", MaxBPS, splitTotal)
	}
	return nil
}

// FeeSplitResult captures the outcome of a fee split computation.
type FeeSplitResult struct {
	SettlementID string      `json:"settlement_id"`
	TotalAmount  sdkmath.Int `json:"total_amount"`
	BurnAmount   sdkmath.Int `json:"burn_amount"`
	Insurance    sdkmath.Int `json:"insurance"`
	NetAmount    sdkmath.Int `json:"net_amount"`
	Publisher    sdkmath.Int `json:"publisher"`
	Router       sdkmath.Int `json:"router"`
	Referrer     sdkmath.Int `json:"referrer"`
	Denom        string      `json:"denom"`
}

// ComputeFeeSplit calculates the fee distribution for a settlement amount.
// Order of operations: burn → insurance → net split (publisher/router/referrer).
func ComputeFeeSplit(amount sdkmath.Int, denom, settlementID string, params FeeSplitParams) (FeeSplitResult, error) {
	if err := params.Validate(); err != nil {
		return FeeSplitResult{}, fmt.Errorf("invalid fee split params: %w", err)
	}
	if amount.IsNegative() {
		return FeeSplitResult{}, fmt.Errorf("amount must be non-negative")
	}
	if amount.IsZero() {
		return FeeSplitResult{
			SettlementID: settlementID,
			TotalAmount:  amount,
			BurnAmount:   sdkmath.ZeroInt(),
			Insurance:    sdkmath.ZeroInt(),
			NetAmount:    sdkmath.ZeroInt(),
			Publisher:    sdkmath.ZeroInt(),
			Router:       sdkmath.ZeroInt(),
			Referrer:     sdkmath.ZeroInt(),
			Denom:        denom,
		}, nil
	}

	// Step 1: Burn.
	burnAmount := bpsOf(amount, params.BurnBPS)
	afterBurn := amount.Sub(burnAmount)

	// Step 2: Insurance.
	insuranceAmount := bpsOf(afterBurn, params.InsuranceBPS)
	netAmount := afterBurn.Sub(insuranceAmount)

	// Step 3: Split net among publisher/router/referrer.
	publisherAmount := bpsOf(netAmount, params.PublisherBPS)
	routerAmount := bpsOf(netAmount, params.RouterBPS)
	referrerAmount := bpsOf(netAmount, params.ReferrerBPS)

	// Assign rounding dust to the largest share holder (publisher).
	distributed := publisherAmount.Add(routerAmount).Add(referrerAmount)
	dust := netAmount.Sub(distributed)
	publisherAmount = publisherAmount.Add(dust)

	return FeeSplitResult{
		SettlementID: settlementID,
		TotalAmount:  amount,
		BurnAmount:   burnAmount,
		Insurance:    insuranceAmount,
		NetAmount:    netAmount,
		Publisher:    publisherAmount,
		Router:       routerAmount,
		Referrer:     referrerAmount,
		Denom:        denom,
	}, nil
}

// bpsOf computes amount * bps / 10000 using integer math.
func bpsOf(amount sdkmath.Int, bps uint32) sdkmath.Int {
	if bps == 0 {
		return sdkmath.ZeroInt()
	}
	return amount.MulRaw(int64(bps)).QuoRaw(int64(MaxBPS))
}

func validateIBCIdentifier(fieldName, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace and must match ICS-24 identifier grammar", fieldName)
	}
	if len(value) < minIBCIdentifierLength || len(value) > maxIBCIdentifierLength {
		return fmt.Errorf("%s must be %d-%d characters per ICS-24 host requirements", fieldName, minIBCIdentifierLength, maxIBCIdentifierLength)
	}
	for i := 0; i < len(value); i++ {
		if !isIBCIdentifierChar(value[i]) {
			return fmt.Errorf("%s must match ICS-24 identifier grammar (alphanumeric plus . _ + - # [ ] < >)", fieldName)
		}
	}
	return nil
}

func isIBCIdentifierChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '.' ||
		ch == '_' ||
		ch == '+' ||
		ch == '-' ||
		ch == '#' ||
		ch == '[' ||
		ch == ']' ||
		ch == '<' ||
		ch == '>'
}

func validateICS20TransferPort(fieldName, value string) error {
	if value != transfertypes.PortID {
		return fmt.Errorf("%s must be %q for ICS-20 transfer packets", fieldName, transfertypes.PortID)
	}
	return nil
}

func validateIBCSequence(fieldName string, value uint64) error {
	if value == 0 {
		return fmt.Errorf("%s must be non-zero for IBC packets", fieldName)
	}
	return nil
}

// SplitExecutor executes the per-leg transfers for a fee split result.
// Implementations are expected to move funds according to split.Publisher,
// split.Router, split.Referrer, split.BurnAmount, and split.Insurance —
// typically via the bank keeper against module accounts. Returning an error
// causes the middleware to reject the settlement packet with an error
// acknowledgement so the counterparty can retry rather than getting charged
// without splits executing.
//
// The middleware invokes Execute after verifying amount, denom, and memo
// but before emitting audit events, so event attributes can reflect the
// actual executed state rather than an advisory intent.
type SplitExecutor interface {
	Execute(ctx sdk.Context, packet channeltypes.Packet, memo SettlementMemo, split FeeSplitResult) error
}

// FeeSplitMiddleware is an IBC middleware that intercepts ICS-20 settlement
// packets and applies the Lumera fee split protocol. Non-settlement packets
// pass through unmodified.
//
// When Executor is nil, OnRecvPacket behaves in advisory mode: it computes
// the split, emits events, and delegates to the underlying ICS-20 app,
// which still moves the FULL amount to the memo's Publisher (not the split
// legs). Operators that cannot tolerate the advisory gap can set
// RequireSplitExecutor, which fails the packet closed rather than emitting
// misleading events. When Executor is set, the middleware invokes it
// before emitting events; a successful Execute flips the `executed`
// attribute on every emitted event to true. This keeps lumera_ai-s871p's
// audit-vs-reality gap pluggable: the chain binary can wire a bank-keeper
// executor at wiring time without touching middleware internals.
type FeeSplitMiddleware struct {
	app         porttypes.IBCModule
	ics4Wrapper porttypes.ICS4Wrapper
	params      FeeSplitParams

	// Executor, when set, performs the actual per-leg token transfers.
	// Nil means advisory mode (events only, underlying app moves full
	// amount).
	Executor SplitExecutor

	// RequireSplitExecutor, when true, fails settlement packets closed
	// whenever Executor is nil. Set this in production to refuse
	// settlements until the real token transfer wiring lands; leave false
	// for pre-production chains that tolerate advisory-only events.
	RequireSplitExecutor bool
}

// NewFeeSplitMiddleware wraps an IBC module with fee split logic.
func NewFeeSplitMiddleware(app porttypes.IBCModule, ics4Wrapper porttypes.ICS4Wrapper, params FeeSplitParams) (FeeSplitMiddleware, error) {
	if err := params.Validate(); err != nil {
		return FeeSplitMiddleware{}, fmt.Errorf("fee split middleware: %w", err)
	}
	return FeeSplitMiddleware{
		app:         app,
		ics4Wrapper: ics4Wrapper,
		params:      params,
	}, nil
}

// WithRequireSplitExecutor returns a copy of m that fails settlement packets
// closed until a real split executor is wired in. Provided as a functional
// knob so operators can opt-in without widening the constructor signature.
func (m FeeSplitMiddleware) WithRequireSplitExecutor(require bool) FeeSplitMiddleware {
	m.RequireSplitExecutor = require
	return m
}

// WithExecutor returns a copy of m with the provided SplitExecutor. Passing
// nil reverts to advisory-only mode.
func (m FeeSplitMiddleware) WithExecutor(executor SplitExecutor) FeeSplitMiddleware {
	m.Executor = executor
	return m
}

// OnChanOpenInit delegates to the underlying app.
// IBC v11 migration: removed *capabilitytypes.Capability parameter
// (capabilities are no longer part of the IBCModule interface).
func (m FeeSplitMiddleware) OnChanOpenInit(ctx sdk.Context, order channeltypes.Order, connectionHops []string, portID, channelID string, counterparty channeltypes.Counterparty, version string) (string, error) {
	return m.app.OnChanOpenInit(ctx, order, connectionHops, portID, channelID, counterparty, version)
}

// OnChanOpenTry delegates to the underlying app.
// IBC v11 migration: removed *capabilitytypes.Capability parameter.
func (m FeeSplitMiddleware) OnChanOpenTry(ctx sdk.Context, order channeltypes.Order, connectionHops []string, portID, channelID string, counterparty channeltypes.Counterparty, counterpartyVersion string) (string, error) {
	return m.app.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, counterparty, counterpartyVersion)
}

// OnChanOpenAck delegates to the underlying app.
func (m FeeSplitMiddleware) OnChanOpenAck(ctx sdk.Context, portID, channelID, counterpartyChannelID, counterpartyVersion string) error {
	return m.app.OnChanOpenAck(ctx, portID, channelID, counterpartyChannelID, counterpartyVersion)
}

// OnChanOpenConfirm delegates to the underlying app.
func (m FeeSplitMiddleware) OnChanOpenConfirm(ctx sdk.Context, portID, channelID string) error {
	return m.app.OnChanOpenConfirm(ctx, portID, channelID)
}

// OnChanCloseInit delegates to the underlying app.
func (m FeeSplitMiddleware) OnChanCloseInit(ctx sdk.Context, portID, channelID string) error {
	return m.app.OnChanCloseInit(ctx, portID, channelID)
}

// OnChanCloseConfirm delegates to the underlying app.
func (m FeeSplitMiddleware) OnChanCloseConfirm(ctx sdk.Context, portID, channelID string) error {
	return m.app.OnChanCloseConfirm(ctx, portID, channelID)
}

// OnRecvPacket intercepts ICS-20 packets with Lumera settlement memos and
// applies the fee split. Non-settlement packets pass through to the underlying app.
// IBC v11 migration: added channelVersion string as 2nd parameter
// (after ctx). Stored to forward to the underlying app delegations.
func (m FeeSplitMiddleware) OnRecvPacket(ctx sdk.Context, channelVersion string, packet channeltypes.Packet, relayer sdk.AccAddress) ibcexported.Acknowledgement {
	// Reject oversized packet data BEFORE parsing. A misbehaving
	// counterparty chain can otherwise stream max-size packets whose
	// dense JSON burns disproportionate Unmarshal compute on every
	// destination validator.
	if len(packet.Data) > MaxICS20PacketDataBytes {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: packet data exceeds %d-byte cap (got %d); rejected to prevent IBC Unmarshal DoS amplification",
				MaxICS20PacketDataBytes, len(packet.Data)),
		)
	}
	// Try to parse as ICS-20 fungible token packet.
	var ftPacket transfertypes.FungibleTokenPacketData
	if err := json.Unmarshal(packet.Data, &ftPacket); err != nil {
		// Not an ICS-20 packet; pass through.
		return m.app.OnRecvPacket(ctx, channelVersion, packet, relayer)
	}

	// Check for Lumera settlement memo.
	memo, isSettlement, err := ExtractSettlementMemo(ftPacket)
	if err != nil {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: invalid settlement memo: %w", err),
		)
	}
	if !isSettlement {
		// Not a settlement transfer; pass through.
		return m.app.OnRecvPacket(ctx, channelVersion, packet, relayer)
	}

	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "source_port", value: packet.SourcePort},
		{name: "destination_port", value: packet.DestinationPort},
	} {
		if err := validateICS20TransferPort(field.name, field.value); err != nil {
			return channeltypes.NewErrorAcknowledgement(
				fmt.Errorf("fee_split: invalid %s: %w", field.name, err),
			)
		}
	}
	if err := validateIBCSequence("sequence", packet.Sequence); err != nil {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: invalid packet sequence: %w", err),
		)
	}

	// Parse amount. A counterparty relayer controls this string, so we apply
	// strict bounds: must parse, must be strictly positive (zero settlement
	// transfers waste cycles and emit misleading audit events), and must fit
	// in MaxSettlementAmountBits to defend downstream BPS math against
	// pathologically large operands that bloat event logs and indexer state.
	amount, ok := sdkmath.NewIntFromString(ftPacket.Amount)
	if !ok || !amount.IsPositive() {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: invalid amount %q (must be positive integer)", ftPacket.Amount),
		)
	}
	if amount.BigInt().BitLen() > MaxSettlementAmountBits {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: amount %q exceeds %d-bit cap", ftPacket.Amount, MaxSettlementAmountBits),
		)
	}
	if err := requireNoSurroundingWhitespace("denom", ftPacket.Denom); err != nil {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: invalid denom: %w", err),
		)
	}
	if err := requireNoASCIIWhitespaceOrControl("denom", ftPacket.Denom); err != nil {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: invalid denom: %w", err),
		)
	}
	if strings.TrimSpace(ftPacket.Denom) == "" {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: denom is required"),
		)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "sender", value: ftPacket.Sender},
		{name: "receiver", value: ftPacket.Receiver},
	} {
		if err := requireNoSurroundingWhitespace(field.name, field.value); err != nil {
			return channeltypes.NewErrorAcknowledgement(
				fmt.Errorf("fee_split: invalid %s: %w", field.name, err),
			)
		}
		if err := requireNoASCIIWhitespaceOrControl(field.name, field.value); err != nil {
			return channeltypes.NewErrorAcknowledgement(
				fmt.Errorf("fee_split: invalid %s: %w", field.name, err),
			)
		}
		if strings.TrimSpace(field.value) == "" {
			return channeltypes.NewErrorAcknowledgement(
				fmt.Errorf("fee_split: %s is required", field.name),
			)
		}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "source_channel", value: packet.SourceChannel},
		{name: "destination_channel", value: packet.DestinationChannel},
	} {
		if err := validateIBCIdentifier(field.name, field.value); err != nil {
			return channeltypes.NewErrorAcknowledgement(
				fmt.Errorf("fee_split: invalid %s: %w", field.name, err),
			)
		}
	}

	// Compute fee split.
	split, err := ComputeFeeSplit(amount, ftPacket.Denom, memo.SettlementID, m.params)
	if err != nil {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: compute split: %w", err),
		)
	}

	// If an executor is wired in, run the actual per-leg transfers before
	// emitting events so audit attributes reflect reality. Without an
	// executor, the underlying ICS-20 app still moves the FULL amount to
	// the memo's designated recipient — operators who cannot tolerate that
	// gap can set RequireSplitExecutor to fail the packet closed instead
	// of emitting misleading events.
	splitExecuted := false
	if m.Executor != nil {
		if err := m.Executor.Execute(ctx, packet, *memo, split); err != nil {
			return channeltypes.NewErrorAcknowledgement(
				fmt.Errorf("fee_split: executor failed for packet %s: %w", memo.SettlementID, err),
			)
		}
		splitExecuted = true
	} else if m.RequireSplitExecutor {
		return channeltypes.NewErrorAcknowledgement(
			fmt.Errorf("fee_split: split executor not configured; refusing packet %s (set RequireSplitExecutor=false to accept advisory splits)", memo.SettlementID),
		)
	}
	executedAttr := sdk.NewAttribute("executed", strconv.FormatBool(splitExecuted))

	// Emit fee_collected event.
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"fee_collected",
		sdk.NewAttribute("settlement_id", memo.SettlementID),
		sdk.NewAttribute("total_amount", amount.String()),
		sdk.NewAttribute("denom", ftPacket.Denom),
		sdk.NewAttribute("source_channel", packet.SourceChannel),
		sdk.NewAttribute("destination_channel", packet.DestinationChannel),
		executedAttr,
	))

	// Emit fee_split_applied event.
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"fee_split_applied",
		sdk.NewAttribute("settlement_id", memo.SettlementID),
		sdk.NewAttribute("burn_amount", split.BurnAmount.String()),
		sdk.NewAttribute("insurance_amount", split.Insurance.String()),
		sdk.NewAttribute("net_amount", split.NetAmount.String()),
		sdk.NewAttribute("publisher_amount", split.Publisher.String()),
		sdk.NewAttribute("router_amount", split.Router.String()),
		sdk.NewAttribute("referrer_amount", split.Referrer.String()),
		sdk.NewAttribute("denom", ftPacket.Denom),
		sdk.NewAttribute("burn_bps", fmt.Sprintf("%d", m.params.BurnBPS)),
		sdk.NewAttribute("insurance_bps", fmt.Sprintf("%d", m.params.InsuranceBPS)),
		sdk.NewAttribute("publisher_bps", fmt.Sprintf("%d", m.params.PublisherBPS)),
		sdk.NewAttribute("router_bps", fmt.Sprintf("%d", m.params.RouterBPS)),
		sdk.NewAttribute("referrer_bps", fmt.Sprintf("%d", m.params.ReferrerBPS)),
		executedAttr,
	))

	// Emit transfer_routed events for each recipient.
	if memo.Publisher != "" && split.Publisher.IsPositive() {
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"transfer_routed",
			sdk.NewAttribute("settlement_id", memo.SettlementID),
			sdk.NewAttribute("recipient_role", "publisher"),
			sdk.NewAttribute("recipient", memo.Publisher),
			sdk.NewAttribute("amount", split.Publisher.String()),
			sdk.NewAttribute("denom", ftPacket.Denom),
			executedAttr,
		))
	}
	if memo.Router != "" && split.Router.IsPositive() {
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"transfer_routed",
			sdk.NewAttribute("settlement_id", memo.SettlementID),
			sdk.NewAttribute("recipient_role", "router"),
			sdk.NewAttribute("recipient", memo.Router),
			sdk.NewAttribute("amount", split.Router.String()),
			sdk.NewAttribute("denom", ftPacket.Denom),
			executedAttr,
		))
	}
	if split.Referrer.IsPositive() {
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"transfer_routed",
			sdk.NewAttribute("settlement_id", memo.SettlementID),
			sdk.NewAttribute("recipient_role", "referrer"),
			sdk.NewAttribute("recipient", "referrer"),
			sdk.NewAttribute("amount", split.Referrer.String()),
			sdk.NewAttribute("denom", ftPacket.Denom),
			executedAttr,
		))
	}
	if split.BurnAmount.IsPositive() {
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"transfer_routed",
			sdk.NewAttribute("settlement_id", memo.SettlementID),
			sdk.NewAttribute("recipient_role", "burn"),
			sdk.NewAttribute("recipient", "burn"),
			sdk.NewAttribute("amount", split.BurnAmount.String()),
			sdk.NewAttribute("denom", ftPacket.Denom),
			executedAttr,
		))
	}
	if split.Insurance.IsPositive() {
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"transfer_routed",
			sdk.NewAttribute("settlement_id", memo.SettlementID),
			sdk.NewAttribute("recipient_role", "insurance"),
			sdk.NewAttribute("recipient", "insurance_pool"),
			sdk.NewAttribute("amount", split.Insurance.String()),
			sdk.NewAttribute("denom", ftPacket.Denom),
			executedAttr,
		))
	}

	if splitExecuted {
		return channeltypes.NewResultAcknowledgement([]byte("fee_split_executed"))
	}

	// Delegate to underlying app for actual token transfer and state updates
	// only in advisory mode, where no split executor moved the funds.
	return m.app.OnRecvPacket(ctx, channelVersion, packet, relayer)
}

// OnAcknowledgementPacket delegates to the underlying app.
// IBC v11 migration: added channelVersion string as 2nd parameter.
func (m FeeSplitMiddleware) OnAcknowledgementPacket(ctx sdk.Context, channelVersion string, packet channeltypes.Packet, acknowledgement []byte, relayer sdk.AccAddress) error {
	return m.app.OnAcknowledgementPacket(ctx, channelVersion, packet, acknowledgement, relayer)
}

// OnTimeoutPacket delegates to the underlying app. No partial split is applied
// on timeout — the full amount is refunded by the underlying transfer module.
// IBC v11 migration: added channelVersion string as 2nd parameter.
func (m FeeSplitMiddleware) OnTimeoutPacket(ctx sdk.Context, channelVersion string, packet channeltypes.Packet, relayer sdk.AccAddress) error {
	return m.app.OnTimeoutPacket(ctx, channelVersion, packet, relayer)
}

// SendPacket delegates to the underlying ICS4 wrapper.
// IBC v11 migration: removed *capabilitytypes.Capability parameter.
func (m FeeSplitMiddleware) SendPacket(ctx sdk.Context, sourcePort, sourceChannel string, timeoutHeight clienttypes.Height, timeoutTimestamp uint64, data []byte) (uint64, error) {
	return m.ics4Wrapper.SendPacket(ctx, sourcePort, sourceChannel, timeoutHeight, timeoutTimestamp, data)
}

// WriteAcknowledgement delegates to the underlying ICS4 wrapper.
// IBC v11 migration: removed *capabilitytypes.Capability parameter.
func (m FeeSplitMiddleware) WriteAcknowledgement(ctx sdk.Context, packet ibcexported.PacketI, ack ibcexported.Acknowledgement) error {
	return m.ics4Wrapper.WriteAcknowledgement(ctx, packet, ack)
}

// SetICS4Wrapper satisfies the IBC v11 IBCModule interface. Allows
// upstream middleware to inject itself as the ICS4 wrapper. Stores
// the new wrapper in place of any prior one; consensus-critical
// callers using SendPacket/WriteAcknowledgement now route through
// the upstream wrapper if set.
func (m *FeeSplitMiddleware) SetICS4Wrapper(wrapper porttypes.ICS4Wrapper) {
	m.ics4Wrapper = wrapper
	if setter, ok := m.app.(interface {
		SetICS4Wrapper(porttypes.ICS4Wrapper)
	}); ok {
		setter.SetICS4Wrapper(wrapper)
	}
}

// SetUnderlyingApplication satisfies the IBC v11 Middleware interface.
// Allows the IBC stack to set the underlying base application (the
// transfer module) after construction.
func (m *FeeSplitMiddleware) SetUnderlyingApplication(app porttypes.IBCModule) {
	m.app = app
}

// GetAppVersion delegates to the underlying ICS4 wrapper.
func (m FeeSplitMiddleware) GetAppVersion(ctx sdk.Context, portID, channelID string) (string, bool) {
	return m.ics4Wrapper.GetAppVersion(ctx, portID, channelID)
}

// Compile-time interface checks.
// IBC v11 migration: SetICS4Wrapper / SetUnderlyingApplication require
// pointer receiver, so the interface check binds to *FeeSplitMiddleware.
var (
	_ porttypes.IBCModule   = (*FeeSplitMiddleware)(nil)
	_ porttypes.ICS4Wrapper = (*FeeSplitMiddleware)(nil)
	_ porttypes.Middleware  = (*FeeSplitMiddleware)(nil)
)
