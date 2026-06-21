// Package types defines public constants used for credits module settlement events.
//
//revive:disable:var-naming // Cosmos modules keep auxiliary protobuf types under package types.
package types

const (
	// EventTypeSettlement denotes settlement completion events.
	EventTypeSettlement = "settlement"
	// EventTypeBurn denotes LAC burn events.
	EventTypeBurn = "lac_burn"
	// EventTypeDistribute denotes revenue distribution events.
	EventTypeDistribute = "revenue_distribute"
	// EventTypeLock denotes creation of a credit lock.
	EventTypeLock = "credit_lock"
	// EventTypeUnlock denotes release of a credit lock.
	EventTypeUnlock = "credit_unlock"
	// EventTypeDispute denotes dispute lifecycle events.
	EventTypeDispute = "settlement_dispute"
	// EventTypeSwap denotes LUME↔LAC swap activity.
	EventTypeSwap = "lume_lac_swap"
)

const (
	// AttributeKeySettlementID captures the settlement identifier attribute.
	AttributeKeySettlementID = "settlement_id"
	// AttributeKeyToolID captures the tool identifier attribute.
	AttributeKeyToolID = "tool_id"
	// AttributeKeyPublisher captures the publisher address attribute.
	AttributeKeyPublisher = "publisher"
	// AttributeKeyUser captures the user address attribute.
	AttributeKeyUser = "user"
	// AttributeKeyAmount captures the primary amount attribute.
	AttributeKeyAmount = "amount"
	// AttributeKeyBurnAmount captures the burned amount attribute.
	AttributeKeyBurnAmount = "burn_amount"
	// AttributeKeyStatus captures the settlement status attribute.
	AttributeKeyStatus = "status"
	// AttributeKeyLockID captures the lock identifier attribute.
	AttributeKeyLockID = "lock_id"
	// AttributeKeyDisputeID captures the dispute identifier attribute.
	AttributeKeyDisputeID = "dispute_id"
	// AttributeKeySwapRate captures the swap rate attribute on swap events.
	AttributeKeySwapRate = "swap_rate"
	// AttributeKeyRouter captures the router address attribute.
	AttributeKeyRouter = "router"
	// AttributeKeySessionID captures the router session attribute.
	AttributeKeySessionID = "session_id"
	// AttributeKeyReason captures human-readable reasons (e.g., unlock failure).
	AttributeKeyReason = "reason"
	// AttributeKeyExpiresAt captures lock expiration timestamps.
	AttributeKeyExpiresAt = "expires_at"
	// AttributeKeyToolpackID captures the curated Toolpack identifier.
	AttributeKeyToolpackID = "toolpack_id"
)
