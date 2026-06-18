
package types

import (
	"cosmossdk.io/collections"
	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
)

// ReceiptCollections defines the collections-backed schema for receipts and the settlement queue.
//
// The module stores the canonical receipt payload in PendingReceipts, while queue-specific metadata
// lives in QueuedReceipts. Secondary indexes provide efficient lookups by status, tool, router, and
// ready time ordering (ReadyIndex).
type ReceiptCollections struct {
	// PendingReceipts stores canonical receipt payloads keyed by receipt ID.
	PendingReceipts collections.Map[string, *ReceiptPending]

	// QueuedReceipts stores queue metadata keyed by receipt ID.
	QueuedReceipts collections.Map[string, *QueuedReceipt]

	// QueuedByStatus indexes queued receipts by status.
	QueuedByStatus collections.KeySet[collections.Pair[string, string]]

	// QueuedByTool indexes queued receipts by tool ID.
	QueuedByTool collections.KeySet[collections.Pair[string, string]]

	// QueuedByRouter indexes queued receipts by router bech32 address.
	QueuedByRouter collections.KeySet[collections.Pair[string, string]]

	// QueuedByUser indexes queued receipts by user bech32 address.
	QueuedByUser collections.KeySet[collections.Pair[string, string]]

	// ReadyIndex indexes queued receipts by ready-at unix timestamp for ordered processing.
	ReadyIndex collections.KeySet[collections.Pair[int64, string]]

	// SettledDateIndex indexes settled/failed receipts by processed-at unix timestamp.
	SettledDateIndex collections.KeySet[collections.Pair[int64, string]]

	// QueueSequence provides a monotonically increasing sequence number for queued receipts.
	QueueSequence collections.Sequence
}

// NewReceiptCollections builds the collections schema for receipt storage and queue management.
func NewReceiptCollections(sb *collections.SchemaBuilder) ReceiptCollections {
	return ReceiptCollections{
		PendingReceipts: collections.NewMap(
			sb,
			collections.NewPrefix(PendingReceiptPrefix),
			"pending_receipts",
			collections.StringKey,
			sdkcodec.CollValueV2[ReceiptPending](),
		),
		QueuedReceipts: collections.NewMap(
			sb,
			collections.NewPrefix(QueuedReceiptPrefix),
			"queued_receipts",
			collections.StringKey,
			sdkcodec.CollValueV2[QueuedReceipt](),
		),
		QueuedByStatus: collections.NewKeySet(
			sb,
			collections.NewPrefix(QueuedReceiptStatusIndexPrefix),
			"queued_receipts_by_status",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		QueuedByTool: collections.NewKeySet(
			sb,
			collections.NewPrefix(QueuedReceiptToolIndexPrefix),
			"queued_receipts_by_tool",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		QueuedByRouter: collections.NewKeySet(
			sb,
			collections.NewPrefix(QueuedReceiptRouterIndexPrefix),
			"queued_receipts_by_router",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		QueuedByUser: collections.NewKeySet(
			sb,
			collections.NewPrefix(QueuedReceiptUserIndexPrefix),
			"queued_receipts_by_user",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		ReadyIndex: collections.NewKeySet(
			sb,
			collections.NewPrefix(ReadyIndexPrefix),
			"queued_receipts_by_ready_at",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),
		SettledDateIndex: collections.NewKeySet(
			sb,
			collections.NewPrefix(SettledDateIndexPrefix),
			"queued_receipts_by_processed_at",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),
		QueueSequence: collections.NewSequence(
			sb,
			collections.NewPrefix(QueueSequenceKey),
			"queue_sequence",
		),
	}
}

