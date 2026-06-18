
package types

// Event type and attribute keys emitted by the Toolpack NFT module.
const (
	EventTypeToolpackMinted      = "toolpack_minted"
	EventTypeToolpackUpdated     = "toolpack_updated"
	EventTypeToolpackDeactivated = "toolpack_deactivated"
	EventTypeRoyaltyPayout       = "toolpack_royalty_paid"

	AttributeKeyToolpackID = "toolpack_id"
	AttributeKeyCurator    = "curator"
	AttributeKeyVersion    = "version"
	AttributeKeyAmount     = "amount"
)
