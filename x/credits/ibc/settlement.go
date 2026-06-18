//go:build cosmos

package ibc

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

const (
	// StageIBCPending marks settlements awaiting IBC acknowledgement.
	StageIBCPending = "ibc_pending"
	// StageIBCRefunded marks settlements refunded after IBC timeout.
	StageIBCRefunded = "ibc_refunded"
	// StageIBCCompleted marks settlements completed after a successful IBC ack.
	StageIBCCompleted = "ibc_completed"
)

// MarkRefunded updates a settlement record to reflect an IBC timeout refund.
func MarkRefunded(record *types.SettlementRecord, now time.Time, reason string) error {
	if record == nil {
		return fmt.Errorf("settlement record cannot be nil")
	}
	if err := validateSettlementRecordID(record.Id); err != nil {
		return err
	}
	if record.Status == types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED {
		return nil
	}
	if record.Status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
		return fmt.Errorf("cannot refund completed settlement %s", record.Id)
	}
	if now.IsZero() {
		return fmt.Errorf("refund time must be set")
	}

	record.Status = types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED
	reason = strings.TrimSpace(reason)
	if reason != "" {
		record.FailureReason = reason
	}
	record.CompletedAt = timestamppb.New(now)
	record.Stage = StageIBCRefunded
	return nil
}

// MarkCompleted updates a settlement record to reflect a successful IBC ack.
// It is idempotent on already-completed records and refuses to overwrite a
// refunded settlement, preserving the success/refund terminal-state invariant
// enforced across the settlement lifecycle.
func MarkCompleted(record *types.SettlementRecord, now time.Time, receiptHash string) error {
	if record == nil {
		return fmt.Errorf("settlement record cannot be nil")
	}
	if err := validateSettlementRecordID(record.Id); err != nil {
		return err
	}
	if record.Status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
		return nil
	}
	if record.Status == types.SettlementStatus_SETTLEMENT_STATUS_REFUNDED {
		return fmt.Errorf("cannot complete refunded settlement %s", record.Id)
	}
	if now.IsZero() {
		return fmt.Errorf("completion time must be set")
	}
	canonicalReceiptHash, hasReceiptHash, err := canonicalSettlementReceiptHash(receiptHash)
	if err != nil {
		return err
	}

	record.Status = types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED
	if hasReceiptHash {
		record.ReceiptHash = canonicalReceiptHash
	}
	record.CompletedAt = timestamppb.New(now)
	record.Stage = StageIBCCompleted
	record.FailureReason = ""
	return nil
}

func validateSettlementRecordID(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("settlement id is required")
	}
	if trimmed != id {
		return fmt.Errorf("settlement id must not contain leading or trailing whitespace")
	}
	if err := requireNoASCIIWhitespaceOrControl("settlement id", id); err != nil {
		return err
	}
	return nil
}

func canonicalSettlementReceiptHash(receiptHash string) (string, bool, error) {
	trimmed := strings.TrimSpace(receiptHash)
	if trimmed == "" {
		return "", false, nil
	}
	if trimmed != receiptHash {
		return "", false, fmt.Errorf("receipt hash must not contain leading or trailing whitespace")
	}
	if err := requireNoASCIIWhitespaceOrControl("receipt hash", receiptHash); err != nil {
		return "", false, err
	}
	return receiptHash, true, nil
}
