package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/payment_rails/types"
)

// CreateIBCSettlement creates a new IBC settlement record for a deposit or withdrawal.
func (k *Keeper) CreateIBCSettlement(
	ctx context.Context,
	packetType types.SettlementPacketType,
	user string,
	referenceID string,
	amount sdk.Coin,
	creditAmount sdk.Coin,
	requestID string,
	channelID string,
	portID string,
) (*types.IBCSettlementRecord, error) {
	user = strings.TrimSpace(user)
	if strings.TrimSpace(channelID) != channelID {
		return nil, types.ErrInvalidRequest.Wrap("channel_id must be canonical")
	}
	if strings.TrimSpace(portID) != portID {
		return nil, types.ErrInvalidRequest.Wrap("port_id must be canonical")
	}

	switch packetType {
	case types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_DEPOSIT_FINALIZE,
		types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_WITHDRAW_COMPLETE,
		types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_RELEASE:
	default:
		return nil, types.ErrInvalidRequest.Wrap("unsupported settlement packet type")
	}

	if user == "" {
		return nil, types.ErrInvalidRequest.Wrap("user required")
	}
	if err := validateKeeperID("reference_id", referenceID); err != nil {
		return nil, err
	}
	if requestID != "" {
		if err := validateKeeperID("request_id", requestID); err != nil {
			return nil, err
		}
	}
	if channelID == "" {
		return nil, types.ErrInvalidRequest.Wrap("channel_id required")
	}
	if portID == "" {
		return nil, types.ErrInvalidRequest.Wrap("port_id required")
	}

	// Check for existing settlement by request ID (idempotency)
	if requestID != "" {
		existingID, err := k.state.IBCSettlementByRequest.Get(ctx, requestID)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return nil, fmt.Errorf("lookup settlement by request_id %q: %w", requestID, err)
		}
		if err == nil && existingID != "" {
			existing, err := k.GetIBCSettlement(ctx, existingID)
			if err != nil {
				return nil, fmt.Errorf("stale request settlement index request_id=%q settlement_id=%q: %w", requestID, existingID, err)
			}
			if existing != nil {
				if !matchesSettlementRequest(existing, packetType, user, referenceID, amount, creditAmount, channelID, portID) {
					return nil, types.ErrDuplicateRequest.Wrap("request_id already used with different settlement payload")
				}
				// Return existing settlement for idempotency.
				return existing, nil
			}
		}
	}

	// Check for pending settlement on same reference
	existingID, err := k.state.IBCSettlementByRef.Get(ctx, referenceID)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, fmt.Errorf("lookup settlement by reference_id %q: %w", referenceID, err)
	}
	if err == nil && existingID != "" {
		existing, err := k.GetIBCSettlement(ctx, existingID)
		if err != nil {
			return nil, fmt.Errorf("stale reference settlement index reference_id=%q settlement_id=%q: %w", referenceID, existingID, err)
		}
		if existing != nil && existing.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING {
			return nil, types.ErrDuplicateRequest.Wrap("pending settlement exists for reference")
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()

	seq, err := k.state.IBCSettlementSeq.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("allocate settlement id: %w", err)
	}
	settlementID := fmt.Sprintf("stl-%d", seq)

	record := &types.IBCSettlementRecord{
		SettlementId: settlementID,
		PacketType:   packetType,
		User:         user,
		ReferenceId:  referenceID,
		Amount:       types.CoinToProto(amount),
		CreditAmount: types.CoinToProto(creditAmount),
		Status:       types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		ChannelId:    channelID,
		PortId:       portID,
		RequestId:    requestID,
		RetryCount:   0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := k.state.IBCSettlements.Set(ctx, settlementID, record); err != nil {
		return nil, err
	}
	if requestID != "" {
		if err := k.state.IBCSettlementByRequest.Set(ctx, requestID, settlementID); err != nil {
			return nil, err
		}
	}
	if err := k.state.IBCSettlementByRef.Set(ctx, referenceID, settlementID); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_ibc_settlement_created",
			sdk.NewAttribute("settlement_id", settlementID),
			sdk.NewAttribute("packet_type", packetType.String()),
			sdk.NewAttribute("reference_id", referenceID),
			sdk.NewAttribute("channel_id", channelID),
		),
	)

	return record, nil
}

func matchesSettlementRequest(
	existing *types.IBCSettlementRecord,
	packetType types.SettlementPacketType,
	user string,
	referenceID string,
	amount sdk.Coin,
	creditAmount sdk.Coin,
	channelID string,
	portID string,
) bool {
	if existing == nil {
		return false
	}
	if existing.PacketType != packetType {
		return false
	}
	if existing.User != user || existing.ReferenceId != referenceID {
		return false
	}
	if existing.ChannelId != channelID || existing.PortId != portID {
		return false
	}

	existingAmount := types.CoinFromProto(existing.Amount)
	if !existingAmount.IsEqual(amount) {
		return false
	}
	existingCreditAmount := types.CoinFromProto(existing.CreditAmount)
	return existingCreditAmount.IsEqual(creditAmount)
}

// GetIBCSettlement retrieves an IBC settlement by ID.
func (k *Keeper) GetIBCSettlement(ctx context.Context, settlementID string) (*types.IBCSettlementRecord, error) {
	record, err := k.state.IBCSettlements.Get(ctx, settlementID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrSettlementNotFound
		}
		return nil, err
	}
	if record == nil {
		return nil, types.ErrSettlementNotFound
	}
	return record, nil
}

// GetIBCSettlementByReference retrieves an IBC settlement by reference ID.
func (k *Keeper) GetIBCSettlementByReference(ctx context.Context, referenceID string) (*types.IBCSettlementRecord, error) {
	settlementID, err := k.state.IBCSettlementByRef.Get(ctx, referenceID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrSettlementNotFound
		}
		return nil, err
	}
	return k.GetIBCSettlement(ctx, settlementID)
}

// UpdateIBCSettlementStatus updates the status of an IBC settlement.
func (k *Keeper) UpdateIBCSettlementStatus(
	ctx context.Context,
	settlementID string,
	status types.SettlementStatus,
	errorMessage string,
) error {
	record, err := k.GetIBCSettlement(ctx, settlementID)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()

	record.Status = status
	record.ErrorMessage = errorMessage
	record.UpdatedAt = now

	if status == types.SettlementStatus_SETTLEMENT_STATUS_ACKNOWLEDGED ||
		status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
		record.AckAt = now
	}

	if err := k.state.IBCSettlements.Set(ctx, settlementID, record); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_ibc_settlement_updated",
			sdk.NewAttribute("settlement_id", settlementID),
			sdk.NewAttribute("status", status.String()),
		),
	)

	return nil
}

// SetIBCSettlementSequence sets the packet sequence for a submitted settlement.
func (k *Keeper) SetIBCSettlementSequence(ctx context.Context, settlementID string, sequence uint64) error {
	record, err := k.GetIBCSettlement(ctx, settlementID)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	record.Sequence = sequence
	record.Status = types.SettlementStatus_SETTLEMENT_STATUS_SUBMITTED
	record.UpdatedAt = sdkCtx.BlockTime()

	return k.state.IBCSettlements.Set(ctx, settlementID, record)
}

// IncrementIBCSettlementRetry increments the retry count for a failed settlement.
func (k *Keeper) IncrementIBCSettlementRetry(ctx context.Context, settlementID string) error {
	record, err := k.GetIBCSettlement(ctx, settlementID)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	record.RetryCount++
	record.Status = types.SettlementStatus_SETTLEMENT_STATUS_PENDING
	record.UpdatedAt = sdkCtx.BlockTime()

	return k.state.IBCSettlements.Set(ctx, settlementID, record)
}

// BuildSettlementPacket creates an IBC settlement packet from a settlement record.
func (k *Keeper) BuildSettlementPacket(
	ctx context.Context,
	record *types.IBCSettlementRecord,
	oraclePrice string,
	timeoutNanos uint64,
) (*types.IBCSettlementPacket, error) {
	if record == nil {
		return nil, types.ErrInvalidRequest.Wrap("settlement record required")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	packet := &types.IBCSettlementPacket{
		SettlementId:     record.SettlementId,
		PacketType:       record.PacketType,
		SourceChain:      sdkCtx.ChainID(),
		User:             record.User,
		ReferenceId:      record.ReferenceId,
		Amount:           record.Amount,
		CreditAmount:     record.CreditAmount,
		RequestId:        record.RequestId,
		OraclePrice:      oraclePrice,
		TimeoutTimestamp: timeoutNanos,
		CreatedAt:        sdkCtx.BlockTime(),
		Metadata:         make(map[string]string),
	}

	return packet, nil
}

// HandleSettlementAck processes an IBC settlement acknowledgement.
func (k *Keeper) HandleSettlementAck(ctx context.Context, ack *types.IBCSettlementAck) error {
	if ack == nil {
		return types.ErrInvalidRequest.Wrap("acknowledgement required")
	}
	if strings.TrimSpace(ack.SettlementId) == "" {
		return types.ErrInvalidRequest.Wrap("settlement_id required in ack")
	}

	record, err := k.GetIBCSettlement(ctx, ack.SettlementId)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	var finalizeErr error

	if ack.Success {
		record.ErrorMessage = ""

		// Finalize the underlying deposit/withdrawal based on packet type
		switch record.PacketType {
		case types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_DEPOSIT_FINALIZE:
			if err := k.finalizeDepositFromIBC(ctx, record.ReferenceId); err != nil {
				finalizeErr = fmt.Errorf("finalize deposit %s from IBC ack: %w", record.ReferenceId, err)
			}
		case types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_WITHDRAW_COMPLETE:
			if _, err := k.FinalizeWithdraw(ctx, k.authority, record.ReferenceId); err != nil {
				finalizeErr = fmt.Errorf("finalize withdraw %s from IBC ack: %w", record.ReferenceId, err)
			}
		case types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_RELEASE:
			// RELEASE packets are pure control signals for counterparty recovery.
			// No local settlement finalization is required.
		default:
			finalizeErr = fmt.Errorf("unsupported packet type for ack handling: %s", record.PacketType.String())
		}

		if finalizeErr != nil {
			k.logger.Error("failed to apply IBC settlement acknowledgement", "settlement_id", record.SettlementId, "error", finalizeErr)
			record.Status = types.SettlementStatus_SETTLEMENT_STATUS_FAILED
			record.AckAt = time.Time{}
			record.ErrorMessage = finalizeErr.Error()
		} else {
			record.Status = types.SettlementStatus_SETTLEMENT_STATUS_ACKNOWLEDGED
			record.AckAt = now
		}
	} else {
		record.Status = types.SettlementStatus_SETTLEMENT_STATUS_FAILED
		record.ErrorMessage = ack.Error
	}

	record.UpdatedAt = now
	if err := k.state.IBCSettlements.Set(ctx, ack.SettlementId, record); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_ibc_settlement_ack",
			sdk.NewAttribute("settlement_id", ack.SettlementId),
			sdk.NewAttribute("success", fmt.Sprintf("%t", ack.Success)),
			sdk.NewAttribute("status", record.Status.String()),
		),
	)

	if finalizeErr != nil {
		k.logger.Error("HandleSettlementAck completed with internal finalization error", "error", finalizeErr)
		return finalizeErr
	}

	return nil
}

// HandleSettlementTimeout processes an IBC settlement timeout.
func (k *Keeper) HandleSettlementTimeout(ctx context.Context, settlementID string) error {
	record, err := k.GetIBCSettlement(ctx, settlementID)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()

	record.Status = types.SettlementStatus_SETTLEMENT_STATUS_TIMEOUT
	record.ErrorMessage = "IBC packet timeout"
	record.UpdatedAt = now

	if err := k.state.IBCSettlements.Set(ctx, settlementID, record); err != nil {
		return err
	}

	// Emit event for timeout
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_ibc_settlement_timeout",
			sdk.NewAttribute("settlement_id", settlementID),
			sdk.NewAttribute("reference_id", record.ReferenceId),
		),
	)

	return nil
}

// finalizeDepositFromIBC marks a deposit as finalized after successful IBC settlement.
func (k *Keeper) finalizeDepositFromIBC(ctx context.Context, depositID string) error {
	deposit, err := k.GetDeposit(ctx, depositID)
	if err != nil {
		return err
	}

	if deposit.Status == types.DepositStatus_DEPOSIT_STATUS_FINALIZED {
		return nil // Already finalized
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	deposit.Status = types.DepositStatus_DEPOSIT_STATUS_FINALIZED
	deposit.UpdatedAt = sdkCtx.BlockTime()

	if err := k.state.Deposits.Set(ctx, depositID, deposit); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_deposit_finalized",
			sdk.NewAttribute("deposit_id", depositID),
			sdk.NewAttribute("user", deposit.User),
		),
	)

	return nil
}

// MaxIBCSettlementPacketBytes caps the size of remote-chain-controlled
// packetData passed to OnRecvSettlementPacket before json.Unmarshal. A
// settlement packet carries a handful of identifiers + amounts + a
// request_id — real payloads are well under 8 KiB. 256 KiB is a
// generous ceiling that bounds adversarial Unmarshal amplification
// without risking legitimate settlement rejection
// (lumera_ai-qke3g follow-up; sibling of the ibc_action and registry
// IBC caps).
const MaxIBCSettlementPacketBytes = 256 * 1024

// OnRecvSettlementPacket handles incoming IBC settlement packets from counterparty chain.
func (k *Keeper) OnRecvSettlementPacket(ctx context.Context, packetData []byte) (*types.IBCSettlementAck, error) {
	// Reject oversized packet bytes BEFORE Unmarshal. A misbehaving
	// counterparty chain could otherwise pipeline max-transport-size
	// packets whose dense JSON costs every destination validator
	// disproportionate Unmarshal compute.
	if len(packetData) > MaxIBCSettlementPacketBytes {
		return &types.IBCSettlementAck{
			Success:   false,
			Error:     fmt.Sprintf("packet data %d bytes exceeds %d-byte cap", len(packetData), MaxIBCSettlementPacketBytes),
			ErrorCode: "PACKET_TOO_LARGE",
		}, nil
	}
	var packet types.IBCSettlementPacket
	if err := json.Unmarshal(packetData, &packet); err != nil {
		return &types.IBCSettlementAck{
			Success:   false,
			Error:     "failed to unmarshal settlement packet",
			ErrorCode: "UNMARSHAL_ERROR",
		}, nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()

	// Check for idempotency
	if packet.RequestId != "" {
		existingID, err := k.state.IBCSettlementByRequest.Get(ctx, packet.RequestId)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return settlementStateReadAck(packet.SettlementId, now, "request_id index read failed"), nil
		}
		if err == nil && existingID != "" {
			existing, err := k.GetIBCSettlement(ctx, existingID)
			if err != nil {
				return settlementStateReadAck(packet.SettlementId, now, "request_id index points to missing settlement"), nil
			}
			if existing != nil && existing.Status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
				return &types.IBCSettlementAck{
					Success:      true,
					SettlementId: existing.SettlementId,
					Status:       existing.Status,
					AckAt:        now,
				}, nil
			}
		}
	}

	var processErr error

	switch packet.PacketType {
	case types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_DEPOSIT_FINALIZE:
		processErr = k.processIncomingDepositFinalize(ctx, &packet)
	case types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_WITHDRAW_COMPLETE:
		processErr = k.processIncomingWithdrawComplete(ctx, &packet)
	case types.SettlementPacketType_SETTLEMENT_PACKET_TYPE_RELEASE:
		processErr = k.processIncomingRelease(ctx, &packet)
	default:
		return &types.IBCSettlementAck{
			Success:   false,
			Error:     "unknown packet type",
			ErrorCode: "UNKNOWN_PACKET_TYPE",
		}, nil
	}

	if processErr != nil {
		return &types.IBCSettlementAck{
			Success:      false,
			SettlementId: packet.SettlementId,
			Error:        processErr.Error(),
			ErrorCode:    "PROCESSING_ERROR",
			AckAt:        now,
		}, nil
	}

	return &types.IBCSettlementAck{
		Success:      true,
		SettlementId: packet.SettlementId,
		Status:       types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
		AckAt:        now,
	}, nil
}

// processIncomingDepositFinalize handles an incoming deposit finalization from counterparty.
func (k *Keeper) processIncomingDepositFinalize(ctx context.Context, packet *types.IBCSettlementPacket) error {
	// For incoming deposit finalization, we just record it and emit an event.
	// The actual credit minting was done on the source chain.
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_ibc_deposit_finalize_received",
			sdk.NewAttribute("settlement_id", packet.SettlementId),
			sdk.NewAttribute("reference_id", packet.ReferenceId),
			sdk.NewAttribute("user", packet.User),
			sdk.NewAttribute("source_chain", packet.SourceChain),
		),
	)

	return nil
}

func settlementStateReadAck(settlementID string, now time.Time, message string) *types.IBCSettlementAck {
	return &types.IBCSettlementAck{
		Success:      false,
		SettlementId: settlementID,
		Error:        message,
		ErrorCode:    "STATE_READ_ERROR",
		AckAt:        now,
	}
}

// processIncomingWithdrawComplete handles an incoming withdrawal completion from counterparty.
func (k *Keeper) processIncomingWithdrawComplete(ctx context.Context, packet *types.IBCSettlementPacket) error {
	// For incoming withdrawal completion, the counterparty chain has released assets.
	// We record the completion.
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_ibc_withdraw_complete_received",
			sdk.NewAttribute("settlement_id", packet.SettlementId),
			sdk.NewAttribute("reference_id", packet.ReferenceId),
			sdk.NewAttribute("user", packet.User),
			sdk.NewAttribute("source_chain", packet.SourceChain),
		),
	)

	return nil
}

// processIncomingRelease handles an incoming credit release request (failure recovery).
func (k *Keeper) processIncomingRelease(ctx context.Context, packet *types.IBCSettlementPacket) error {
	// Release request indicates the counterparty failed and wants to release locked credits.
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payment_rails_ibc_release_received",
			sdk.NewAttribute("settlement_id", packet.SettlementId),
			sdk.NewAttribute("reference_id", packet.ReferenceId),
			sdk.NewAttribute("user", packet.User),
			sdk.NewAttribute("source_chain", packet.SourceChain),
		),
	)

	return nil
}

// ListPendingIBCSettlements returns all pending IBC settlements that may need retry.
func (k *Keeper) ListPendingIBCSettlements(ctx context.Context, limit int) ([]*types.IBCSettlementRecord, error) {
	var pending []*types.IBCSettlementRecord

	iter, err := k.state.IBCSettlements.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	for ; iter.Valid(); iter.Next() {
		record, err := iter.Value()
		if err != nil {
			return nil, fmt.Errorf("failed to read settlement record: %w", err)
		}
		if record.Status == types.SettlementStatus_SETTLEMENT_STATUS_PENDING ||
			record.Status == types.SettlementStatus_SETTLEMENT_STATUS_TIMEOUT {
			pending = append(pending, record)
			if limit > 0 && len(pending) >= limit {
				break
			}
		}
	}

	return pending, nil
}

// GetDefaultIBCTimeout returns the default IBC packet timeout duration.
func (k *Keeper) GetDefaultIBCTimeout() time.Duration {
	return time.Hour // 1 hour default
}
