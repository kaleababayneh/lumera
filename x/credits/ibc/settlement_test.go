//go:build cosmos

package ibc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestMarkRefunded_Success(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkRefunded(record, now, "timeout")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED, record.Status)
	require.Equal(t, StageIBCRefunded, record.Stage)
	require.NotNil(t, record.CompletedAt)
	require.Equal(t, "timeout", record.FailureReason)
}

func TestMarkRefunded_NilRecord(t *testing.T) {
	now := time.Now().UTC()
	err := MarkRefunded(nil, now, "timeout")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestMarkRefunded_EmptyID(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkRefunded(record, now, "timeout")
	require.Error(t, err)
	require.Contains(t, err.Error(), "id is required")
}

func TestMarkRefunded_WhitespaceID(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "   ",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkRefunded(record, now, "timeout")
	require.Error(t, err)
	require.Contains(t, err.Error(), "id is required")
}

func TestMarkRefunded_RejectsPaddedID(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     " settlement-1 ",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkRefunded(record, now, "timeout")
	require.ErrorContains(t, err, "settlement id must not contain leading or trailing whitespace")
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, record.Status)
	require.Empty(t, record.Stage)
	require.Nil(t, record.CompletedAt)
}

func TestMarkRefunded_RejectsEmbeddedWhitespaceAndControlID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "newline", id: "settlement\n1"},
		{name: "tab", id: "settlement\t1"},
		{name: "nul", id: "settlement\x001"},
		{name: "delete", id: "settlement\x7f1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			record := &types.SettlementRecord{
				Id:     tc.id,
				Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
			}

			err := MarkRefunded(record, now, "timeout")
			require.ErrorContains(t, err, "settlement id must not include whitespace or control characters")
			require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, record.Status)
			require.Empty(t, record.Stage)
			require.Empty(t, record.FailureReason)
			require.Nil(t, record.CompletedAt)
		})
	}
}

func TestMarkRefunded_AlreadyRefunded(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED,
		Stage:  StageIBCRefunded,
	}

	// Should be idempotent - no error, no changes
	err := MarkRefunded(record, now, "new reason")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED, record.Status)
	// FailureReason should not change on idempotent call
}

func TestMarkRefunded_CompletedSettlement(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
	}

	err := MarkRefunded(record, now, "timeout")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot refund completed")
}

func TestMarkRefunded_ZeroTime(t *testing.T) {
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkRefunded(record, time.Time{}, "timeout")
	require.Error(t, err)
	require.Contains(t, err.Error(), "time must be set")
}

func TestMarkRefunded_EmptyReason(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkRefunded(record, now, "")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED, record.Status)
	require.Equal(t, "", record.FailureReason) // Empty reason is allowed
}

func TestMarkRefunded_WhitespaceReason(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkRefunded(record, now, "   ")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED, record.Status)
	// Whitespace-only reason should be treated as empty
	require.Equal(t, "", record.FailureReason)
}

func TestMarkRefunded_FromPendingStatus(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Stage:  StageIBCPending,
	}

	err := MarkRefunded(record, now, "ibc timeout")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED, record.Status)
	require.Equal(t, StageIBCRefunded, record.Stage)
	require.Equal(t, "ibc timeout", record.FailureReason)
}

func TestMarkRefunded_FromFailedStatus(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_FAILED,
	}

	err := MarkRefunded(record, now, "channel closed")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED, record.Status)
	require.Equal(t, StageIBCRefunded, record.Stage)
}

func TestMarkRefunded_CompletedAtTimestamp(t *testing.T) {
	now := time.Date(2026, 1, 26, 12, 0, 0, 0, time.UTC)
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkRefunded(record, now, "test")
	require.NoError(t, err)
	require.NotNil(t, record.CompletedAt)
	require.Equal(t, now.Unix(), record.CompletedAt.AsTime().Unix())
}

func TestStageConstants(t *testing.T) {
	// Verify stage constants are properly defined
	require.Equal(t, "ibc_pending", StageIBCPending)
	require.Equal(t, "ibc_refunded", StageIBCRefunded)
	require.Equal(t, "ibc_completed", StageIBCCompleted)
}

func TestMarkCompleted_Success(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Stage:  StageIBCPending,
	}

	err := MarkCompleted(record, now, "0xreceipt")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, record.Status)
	require.Equal(t, StageIBCCompleted, record.Stage)
	require.NotNil(t, record.CompletedAt)
	require.Equal(t, "0xreceipt", record.ReceiptHash)
	require.Equal(t, "", record.FailureReason)
}

func TestMarkCompleted_NilRecord(t *testing.T) {
	err := MarkCompleted(nil, time.Now().UTC(), "0xreceipt")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestMarkCompleted_EmptyID(t *testing.T) {
	record := &types.SettlementRecord{
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}
	err := MarkCompleted(record, time.Now().UTC(), "0xreceipt")
	require.Error(t, err)
	require.Contains(t, err.Error(), "id is required")
}

func TestMarkCompleted_WhitespaceID(t *testing.T) {
	record := &types.SettlementRecord{
		Id:     "   ",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}
	err := MarkCompleted(record, time.Now().UTC(), "0xreceipt")
	require.Error(t, err)
	require.Contains(t, err.Error(), "id is required")
}

func TestMarkCompleted_RejectsPaddedID(t *testing.T) {
	record := &types.SettlementRecord{
		Id:     " settlement-1 ",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}

	err := MarkCompleted(record, time.Now().UTC(), "0xreceipt")
	require.ErrorContains(t, err, "settlement id must not contain leading or trailing whitespace")
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, record.Status)
	require.Empty(t, record.Stage)
	require.Nil(t, record.CompletedAt)
	require.Empty(t, record.ReceiptHash)
}

func TestMarkCompleted_RejectsEmbeddedWhitespaceAndControlID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "space", id: "settlement 1"},
		{name: "newline", id: "settlement\n1"},
		{name: "nul", id: "settlement\x001"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := &types.SettlementRecord{
				Id:          tc.id,
				Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
				Stage:       StageIBCPending,
				ReceiptHash: "0xpreexisting",
			}

			err := MarkCompleted(record, time.Now().UTC(), "0xreceipt")
			require.ErrorContains(t, err, "settlement id must not include whitespace or control characters")
			require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, record.Status)
			require.Equal(t, StageIBCPending, record.Stage)
			require.Equal(t, "0xpreexisting", record.ReceiptHash)
			require.Nil(t, record.CompletedAt)
		})
	}
}

func TestMarkCompleted_Idempotent(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:          "settlement-1",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
		Stage:       StageIBCCompleted,
		ReceiptHash: "0xoriginal",
	}

	err := MarkCompleted(record, now, "0xreplacement")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, record.Status)
	require.Equal(t, "0xoriginal", record.ReceiptHash, "idempotent call must not overwrite receipt hash")
}

func TestMarkCompleted_RefundedRejected(t *testing.T) {
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED,
		Stage:  StageIBCRefunded,
	}
	err := MarkCompleted(record, time.Now().UTC(), "0xreceipt")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot complete refunded")
}

func TestMarkCompleted_ZeroTime(t *testing.T) {
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}
	err := MarkCompleted(record, time.Time{}, "0xreceipt")
	require.Error(t, err)
	require.Contains(t, err.Error(), "time must be set")
}

func TestMarkCompleted_EmptyReceiptHashPreservesExisting(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:          "settlement-1",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		ReceiptHash: "0xpreexisting",
	}
	err := MarkCompleted(record, now, "   ")
	require.NoError(t, err)
	require.Equal(t, "0xpreexisting", record.ReceiptHash, "whitespace hash must not overwrite pre-existing value")
}

func TestMarkCompleted_RejectsPaddedReceiptHash(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:          "settlement-1",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Stage:       StageIBCPending,
		ReceiptHash: "0xpreexisting",
	}

	err := MarkCompleted(record, now, " 0xreceipt ")
	require.ErrorContains(t, err, "receipt hash must not contain leading or trailing whitespace")
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, record.Status)
	require.Equal(t, StageIBCPending, record.Stage)
	require.Equal(t, "0xpreexisting", record.ReceiptHash)
	require.Nil(t, record.CompletedAt)
}

func TestMarkCompleted_RejectsEmbeddedWhitespaceAndControlReceiptHash(t *testing.T) {
	tests := []struct {
		name        string
		receiptHash string
	}{
		{name: "space", receiptHash: "blake3:abc def"},
		{name: "newline", receiptHash: "blake3:abc\ndef"},
		{name: "nul", receiptHash: "blake3:abc\x00def"},
		{name: "delete", receiptHash: "blake3:abc\x7fdef"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			record := &types.SettlementRecord{
				Id:          "settlement-1",
				Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
				Stage:       StageIBCPending,
				ReceiptHash: "0xpreexisting",
			}

			err := MarkCompleted(record, now, tc.receiptHash)
			require.ErrorContains(t, err, "receipt hash must not include whitespace or control characters")
			require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, record.Status)
			require.Equal(t, StageIBCPending, record.Stage)
			require.Equal(t, "0xpreexisting", record.ReceiptHash)
			require.Nil(t, record.CompletedAt)
		})
	}
}

func TestMarkCompleted_ClearsFailureReason(t *testing.T) {
	now := time.Now().UTC()
	record := &types.SettlementRecord{
		Id:            "settlement-1",
		Status:        types.SettlementStatus_SETTLEMENT_STATUS_FAILED,
		FailureReason: "transient ibc retry",
	}
	err := MarkCompleted(record, now, "0xreceipt")
	require.NoError(t, err)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, record.Status)
	require.Equal(t, "", record.FailureReason, "completed settlements must not carry a failure reason")
}

func TestMarkCompleted_CompletedAtTimestamp(t *testing.T) {
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	record := &types.SettlementRecord{
		Id:     "settlement-1",
		Status: types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
	}
	err := MarkCompleted(record, now, "0xreceipt")
	require.NoError(t, err)
	require.NotNil(t, record.CompletedAt)
	require.Equal(t, now.Unix(), record.CompletedAt.AsTime().Unix())
}
