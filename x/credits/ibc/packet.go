//go:build cosmos

package ibc

import (
	"fmt"
	"strings"

	sdkmath "cosmossdk.io/math"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
)

const (
	// ChannelVersion declares the IBC channel version for credits settlement.
	ChannelVersion = "lumera-credits-v1"
	// PortID declares the IBC port ID for credits settlement.
	PortID = "credits"
)

// EscrowTransfer describes an ICS-20 transfer that carries settlement metadata.
type EscrowTransfer struct {
	Denom    string
	Amount   sdkmath.Int
	Sender   string
	Receiver string
	Memo     SettlementMemo
}

// Validate ensures the transfer is safe to serialize.
func (t EscrowTransfer) Validate() error {
	if err := requireNoSurroundingWhitespace("denom", t.Denom); err != nil {
		return err
	}
	if err := requireNoASCIIWhitespaceOrControl("denom", t.Denom); err != nil {
		return err
	}
	if strings.TrimSpace(t.Denom) == "" {
		return fmt.Errorf("denom is required")
	}
	if !t.Amount.IsPositive() {
		return fmt.Errorf("amount must be positive")
	}
	if err := requireNoSurroundingWhitespace("sender", t.Sender); err != nil {
		return err
	}
	if err := requireNoASCIIWhitespaceOrControl("sender", t.Sender); err != nil {
		return err
	}
	if strings.TrimSpace(t.Sender) == "" {
		return fmt.Errorf("sender is required")
	}
	if err := requireNoSurroundingWhitespace("receiver", t.Receiver); err != nil {
		return err
	}
	if err := requireNoASCIIWhitespaceOrControl("receiver", t.Receiver); err != nil {
		return err
	}
	if strings.TrimSpace(t.Receiver) == "" {
		return fmt.Errorf("receiver is required")
	}
	if err := t.Memo.Validate(); err != nil {
		return err
	}
	return nil
}

// BuildEscrowPacket constructs a fungible token packet with Lumera settlement memo.
func BuildEscrowPacket(transfer EscrowTransfer) (transfertypes.FungibleTokenPacketData, error) {
	if err := transfer.Validate(); err != nil {
		return transfertypes.FungibleTokenPacketData{}, err
	}
	memo, err := BuildSettlementMemo(transfer.Memo)
	if err != nil {
		return transfertypes.FungibleTokenPacketData{}, err
	}
	return transfertypes.NewFungibleTokenPacketData(
		transfer.Denom,
		transfer.Amount.String(),
		transfer.Sender,
		transfer.Receiver,
		memo,
	), nil
}

// ExtractSettlementMemo extracts the settlement metadata from packet memo.
// ok=false indicates the memo does not contain Lumera settlement data.
func ExtractSettlementMemo(packet transfertypes.FungibleTokenPacketData) (*SettlementMemo, bool, error) {
	return ParseSettlementMemo(packet.GetMemo())
}
