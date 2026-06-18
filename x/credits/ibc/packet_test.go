//go:build cosmos

package ibc

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"
)

func TestBuildEscrowPacket_Success(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			ReceiptHash:   "blake3:abc",
			RefundAddress: "lumera1refund",
		},
	}

	packet, err := BuildEscrowPacket(transfer)
	require.NoError(t, err)
	require.Equal(t, transfer.Denom, packet.Denom)
	require.Equal(t, transfer.Amount.String(), packet.Amount)
	require.Equal(t, transfer.Sender, packet.Sender)
	require.Equal(t, transfer.Receiver, packet.Receiver)
	require.NotEmpty(t, packet.Memo)

	memo, ok, err := ExtractSettlementMemo(packet)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, transfer.Memo.SettlementID, memo.SettlementID)
}

func TestBuildEscrowPacket_EmptyDenom(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "denom is required")
}

func TestBuildEscrowPacket_WhitespaceDenom(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "   ",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "denom is required")
}

func TestBuildEscrowPacket_RejectsSurroundingWhitespaceFields(t *testing.T) {
	validTransfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}

	tests := []struct {
		name       string
		mutate     func(*EscrowTransfer)
		wantErrMsg string
	}{
		{
			name: "denom",
			mutate: func(transfer *EscrowTransfer) {
				transfer.Denom = " ulac"
			},
			wantErrMsg: "denom must not include surrounding whitespace",
		},
		{
			name: "sender",
			mutate: func(transfer *EscrowTransfer) {
				transfer.Sender = "lumera1sender "
			},
			wantErrMsg: "sender must not include surrounding whitespace",
		},
		{
			name: "receiver",
			mutate: func(transfer *EscrowTransfer) {
				transfer.Receiver = "\tinj1receiver"
			},
			wantErrMsg: "receiver must not include surrounding whitespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transfer := validTransfer
			tc.mutate(&transfer)

			_, err := BuildEscrowPacket(transfer)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErrMsg)
		})
	}
}

func TestBuildEscrowPacket_RejectsEmbeddedWhitespaceAndControlFields(t *testing.T) {
	validTransfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}

	tests := []struct {
		name       string
		mutate     func(*EscrowTransfer)
		wantErrMsg string
	}{
		{
			name: "denom space",
			mutate: func(transfer *EscrowTransfer) {
				transfer.Denom = "u lac"
			},
			wantErrMsg: "denom must not include whitespace or control characters",
		},
		{
			name: "sender newline",
			mutate: func(transfer *EscrowTransfer) {
				transfer.Sender = "lumera1\nsender"
			},
			wantErrMsg: "sender must not include whitespace or control characters",
		},
		{
			name: "receiver nul",
			mutate: func(transfer *EscrowTransfer) {
				transfer.Receiver = "inj1\x00receiver"
			},
			wantErrMsg: "receiver must not include whitespace or control characters",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transfer := validTransfer
			tc.mutate(&transfer)

			_, err := BuildEscrowPacket(transfer)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErrMsg)
		})
	}
}

func TestBuildEscrowPacket_ZeroAmount(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(0),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "amount must be positive")
}

func TestBuildEscrowPacket_NegativeAmount(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(-100),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "amount must be positive")
}

func TestBuildEscrowPacket_EmptySender(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sender is required")
}

func TestBuildEscrowPacket_WhitespaceSender(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "   ",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sender is required")
}

func TestBuildEscrowPacket_EmptyReceiver(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "receiver is required")
}

func TestBuildEscrowPacket_WhitespaceReceiver(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "   ",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "receiver is required")
}

func TestBuildEscrowPacket_InvalidMemo(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "", // Invalid - empty
			RefundAddress: "lumera1refund",
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "settlement_id is required")
}

func TestBuildEscrowPacket_InvalidMemoRefundAddress(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "", // Invalid - empty
		},
	}
	_, err := BuildEscrowPacket(transfer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refund_address is required")
}

func TestEscrowTransfer_Validate_AllFields(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(1000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-1",
			RefundAddress: "lumera1refund",
		},
	}
	err := transfer.Validate()
	require.NoError(t, err)
}

func TestExtractSettlementMemo_NonLumeraMemo(t *testing.T) {
	packet := transfertypes.FungibleTokenPacketData{
		Denom:    "ulac",
		Amount:   "1000",
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo:     `{"other":"data"}`,
	}
	memo, ok, err := ExtractSettlementMemo(packet)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, memo)
}

func TestExtractSettlementMemo_EmptyMemo(t *testing.T) {
	packet := transfertypes.FungibleTokenPacketData{
		Denom:    "ulac",
		Amount:   "1000",
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo:     "",
	}
	memo, ok, err := ExtractSettlementMemo(packet)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, memo)
}

func TestExtractSettlementMemo_ValidLumeraMemo(t *testing.T) {
	packet := transfertypes.FungibleTokenPacketData{
		Denom:    "ulac",
		Amount:   "1000",
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo:     `{"lumera":{"type":"lumera_credits_settlement","settlement_id":"settlement-1","refund_address":"lumera1refund"}}`,
	}
	memo, ok, err := ExtractSettlementMemo(packet)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "settlement-1", memo.SettlementID)
	require.Equal(t, "lumera1refund", memo.RefundAddress)
}

func TestPacketConstants(t *testing.T) {
	require.Equal(t, "lumera-credits-v1", ChannelVersion)
	require.Equal(t, "credits", PortID)
}

func TestBuildEscrowPacket_LargeAmount(t *testing.T) {
	// Test with a large amount (18 decimals is common in crypto)
	largeAmount := sdkmath.NewIntFromBigInt(sdkmath.NewIntWithDecimal(1, 18).BigInt())
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   largeAmount,
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			SettlementID:  "settlement-large",
			RefundAddress: "lumera1refund",
		},
	}

	packet, err := BuildEscrowPacket(transfer)
	require.NoError(t, err)
	require.Equal(t, largeAmount.String(), packet.Amount)
}

func TestBuildEscrowPacket_AllMemoFields(t *testing.T) {
	transfer := EscrowTransfer{
		Denom:    "ulac",
		Amount:   sdkmath.NewInt(5000),
		Sender:   "lumera1sender",
		Receiver: "inj1receiver",
		Memo: SettlementMemo{
			Type:          MemoTypeSettlement,
			SettlementID:  "settlement-full",
			ReceiptHash:   "blake3:fullhash",
			Router:        "lumera1router",
			Publisher:     "lumera1publisher",
			ToolID:        "tool-123",
			ToolpackID:    "toolpack-456",
			ActionID:      "action-789",
			RefundAddress: "lumera1refund",
		},
	}

	packet, err := BuildEscrowPacket(transfer)
	require.NoError(t, err)

	memo, ok, err := ExtractSettlementMemo(packet)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "settlement-full", memo.SettlementID)
	require.Equal(t, "blake3:fullhash", memo.ReceiptHash)
	require.Equal(t, "lumera1router", memo.Router)
	require.Equal(t, "lumera1publisher", memo.Publisher)
	require.Equal(t, "tool-123", memo.ToolID)
	require.Equal(t, "toolpack-456", memo.ToolpackID)
	require.Equal(t, "action-789", memo.ActionID)
}
