//go:build cosmos

package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMsgSubmitReceipt_ValidateBasic pins the router-address guard plus
// nil, blank, and canonical receipt-id checks before the keeper indexes
// receipts by their raw ID.
func TestMsgSubmitReceipt_ValidateBasic(t *testing.T) {
	validRouter := validBech32Addr(t, 0x41)
	validReceipt := &UsageReceipt{ReceiptId: "rcpt-1"}

	cases := []struct {
		name      string
		msg       *MsgSubmitReceipt
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "happy_path",
			msg:     &MsgSubmitReceipt{Router: validRouter, Receipt: validReceipt},
			wantErr: false,
		},
		{
			name:      "empty_router",
			msg:       &MsgSubmitReceipt{Router: "", Receipt: validReceipt},
			wantErr:   true,
			errSubstr: "address",
		},
		{
			name:    "invalid_bech32_router",
			msg:     &MsgSubmitReceipt{Router: "not-a-bech32", Receipt: validReceipt},
			wantErr: true,
		},
		{
			name:      "nil_receipt",
			msg:       &MsgSubmitReceipt{Router: validRouter, Receipt: nil},
			wantErr:   true,
			errSubstr: "receipt.receipt_id",
		},
		{
			name:      "empty_receipt_id",
			msg:       &MsgSubmitReceipt{Router: validRouter, Receipt: &UsageReceipt{ReceiptId: ""}},
			wantErr:   true,
			errSubstr: "receipt.receipt_id",
		},
		{
			name:      "whitespace_receipt_id",
			msg:       &MsgSubmitReceipt{Router: validRouter, Receipt: &UsageReceipt{ReceiptId: "   "}},
			wantErr:   true,
			errSubstr: "receipt.receipt_id",
		},
		{
			name:      "padded_receipt_id",
			msg:       &MsgSubmitReceipt{Router: validRouter, Receipt: &UsageReceipt{ReceiptId: " rcpt-1"}},
			wantErr:   true,
			errSubstr: "receipt.receipt_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					require.Contains(t, err.Error(), tc.errSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMsgChallengeReceipt_ValidateBasic mirrors SubmitReceipt for the
// dispute-initiation path, where challenge.receipt_id is also a raw key.
func TestMsgChallengeReceipt_ValidateBasic(t *testing.T) {
	validChallenger := validBech32Addr(t, 0x42)
	validChallenge := &Challenge{ReceiptId: "rcpt-disputed"}

	cases := []struct {
		name      string
		msg       *MsgChallengeReceipt
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "happy_path",
			msg:     &MsgChallengeReceipt{Challenger: validChallenger, Challenge: validChallenge},
			wantErr: false,
		},
		{
			name:      "empty_challenger",
			msg:       &MsgChallengeReceipt{Challenger: "", Challenge: validChallenge},
			wantErr:   true,
			errSubstr: "address",
		},
		{
			name:      "nil_challenge",
			msg:       &MsgChallengeReceipt{Challenger: validChallenger, Challenge: nil},
			wantErr:   true,
			errSubstr: "challenge.receipt_id",
		},
		{
			name:      "empty_receipt_id_in_challenge",
			msg:       &MsgChallengeReceipt{Challenger: validChallenger, Challenge: &Challenge{ReceiptId: ""}},
			wantErr:   true,
			errSubstr: "challenge.receipt_id",
		},
		{
			name:      "whitespace_receipt_id_in_challenge",
			msg:       &MsgChallengeReceipt{Challenger: validChallenger, Challenge: &Challenge{ReceiptId: "\t "}},
			wantErr:   true,
			errSubstr: "challenge.receipt_id",
		},
		{
			name:      "padded_receipt_id_in_challenge",
			msg:       &MsgChallengeReceipt{Challenger: validChallenger, Challenge: &Challenge{ReceiptId: "rcpt-disputed "}},
			wantErr:   true,
			errSubstr: "challenge.receipt_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					require.Contains(t, err.Error(), tc.errSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMsgSettleReceipt_ValidateBasic pins the direct-field settlement
// shape: receipt_id is not nested, but it is still a raw settlement key.
func TestMsgSettleReceipt_ValidateBasic(t *testing.T) {
	validSettler := validBech32Addr(t, 0x43)

	cases := []struct {
		name      string
		msg       *MsgSettleReceipt
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "happy_path",
			msg:     &MsgSettleReceipt{Settler: validSettler, ReceiptId: "rcpt-to-settle"},
			wantErr: false,
		},
		{
			name:      "empty_settler",
			msg:       &MsgSettleReceipt{Settler: "", ReceiptId: "rcpt-to-settle"},
			wantErr:   true,
			errSubstr: "address",
		},
		{
			name:    "invalid_bech32_settler",
			msg:     &MsgSettleReceipt{Settler: "not-bech32", ReceiptId: "rcpt-to-settle"},
			wantErr: true,
		},
		{
			name:      "empty_receipt_id",
			msg:       &MsgSettleReceipt{Settler: validSettler, ReceiptId: ""},
			wantErr:   true,
			errSubstr: "receipt_id",
		},
		{
			name:      "whitespace_receipt_id",
			msg:       &MsgSettleReceipt{Settler: validSettler, ReceiptId: "\n\t"},
			wantErr:   true,
			errSubstr: "receipt_id",
		},
		{
			name:      "padded_receipt_id",
			msg:       &MsgSettleReceipt{Settler: validSettler, ReceiptId: "rcpt-to-settle "},
			wantErr:   true,
			errSubstr: "receipt_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					require.Contains(t, err.Error(), tc.errSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
