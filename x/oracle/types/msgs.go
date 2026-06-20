
package types

import (
	"bytes"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxInjectedVotesPerMsg caps the outer votes slice on
// MsgInjectOracleVotes. Each element is itself size-capped by
// ParseVoteExtension at 64 KiB; the outer bound enforces a
// ceiling on total attacker-injected Unmarshal work per message.
// Legitimate values equal the CometBFT validator-set size
// (typically 50-200, max ~200 with default ConsensusParams). 2048
// is ~10x the default cap and leaves room for future large
// validator sets while bounding adversarial injection.
const MaxInjectedVotesPerMsg = 2048

// ValidateBasic performs stateless validation on MsgInjectOracleVotes.
func (m *MsgInjectOracleVotes) ValidateBasic() error {
	if strings.TrimSpace(m.GetAuthority()) == "" {
		return fmt.Errorf("authority is required")
	}
	if _, err := sdk.AccAddressFromBech32(m.GetAuthority()); err != nil {
		return fmt.Errorf("invalid authority address: %w", err)
	}
	if m.GetHeight() <= 0 {
		return fmt.Errorf("height must be positive")
	}
	if len(m.GetVotes()) > MaxInjectedVotesPerMsg {
		return fmt.Errorf("votes exceeds %d-entry cap (got %d); each vote is further capped by ParseVoteExtension",
			MaxInjectedVotesPerMsg, len(m.GetVotes()))
	}
	var lastValidatorAddr []byte
	for idx, vote := range m.GetVotes() {
		if vote == nil {
			return fmt.Errorf("vote[%d] is nil", idx)
		}
		validatorAddr := vote.GetValidatorAddress()
		if len(validatorAddr) == 0 {
			return fmt.Errorf("vote[%d] validator address is empty", idx)
		}
		if idx > 0 && bytes.Compare(validatorAddr, lastValidatorAddr) <= 0 {
			return fmt.Errorf("votes must be sorted by validator address (index %d)", idx)
		}
		if len(vote.GetVoteExtension()) > MaxVoteExtensionBytes {
			return fmt.Errorf("vote[%d] extension exceeds %d-byte cap (got %d); rejected before ParseVoteExtension",
				idx, MaxVoteExtensionBytes, len(vote.GetVoteExtension()))
		}
		lastValidatorAddr = validatorAddr
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (m *MsgInjectOracleVotes) GetSigners() []sdk.AccAddress {
	authority, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{authority}
}
