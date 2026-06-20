
package types

import (
	"errors"
	"fmt"

	"github.com/cosmos/gogoproto/proto"
)

// MaxVoteExtensionBytes caps the serialized size of a ValidatorVote
// accepted by ParseVoteExtension / emitted by MarshalVoteExtension.
// A real ValidatorVote carries a timestamp + a handful of PriceFeed
// entries (asset_pair + price + volume strings, typically ~50-100
// bytes each). Even a validator reporting 100 feeds encodes well
// under 16 KiB. The 64 KiB ceiling is ~1000x realistic, which
// bounds adversarial proto.Unmarshal amplification while leaving
// ample headroom for legitimate growth. A misbehaving proposer
// that injects oversized VoteExtension bytes would otherwise burn
// Unmarshal compute on every validator processing the batch.
const MaxVoteExtensionBytes = 64 * 1024

// MarshalVoteExtension encodes a validator vote for use as a CometBFT vote extension.
//
// The returned bytes are signed by CometBFT as part of the vote itself, so we avoid any
// additional application-level signature scheme here.
func MarshalVoteExtension(vote *ValidatorVote) ([]byte, error) {
	if vote == nil {
		return nil, errors.New("nil vote")
	}
	if vote.Timestamp.IsZero() {
		return nil, errors.New("vote timestamp missing")
	}

	// Use a gogoproto Buffer with deterministic serialization so vote extension
	// bytes are byte-stable across validators (ValidatorVote has no map fields,
	// but we keep the explicit guarantee for forward-compat).
	buf := proto.NewBuffer(nil)
	buf.SetDeterministic(true)
	if err := buf.Marshal(vote); err != nil {
		return nil, err
	}
	raw := buf.Bytes()
	if len(raw) > MaxVoteExtensionBytes {
		return nil, fmt.Errorf("vote extension bytes exceed %d-byte cap (got %d); outbound guard symmetric with ParseVoteExtension",
			MaxVoteExtensionBytes, len(raw))
	}
	return raw, nil
}

// ParseVoteExtension decodes a vote extension payload into a ValidatorVote.
func ParseVoteExtension(voteExtension []byte) (*ValidatorVote, error) {
	if len(voteExtension) == 0 {
		return &ValidatorVote{}, nil
	}
	// Reject oversized input BEFORE proto.Unmarshal. A misbehaving
	// proposer could otherwise inject max-consensus-size VoteExtension
	// bytes whose dense proto burns Unmarshal compute on every
	// validator processing the batch — the same amplification pattern
	// as the registry/ibc UnmarshalPacket cap (lumera_ai-qke3g).
	if len(voteExtension) > MaxVoteExtensionBytes {
		return nil, fmt.Errorf("vote extension bytes exceed %d-byte cap (got %d); rejected to prevent proto.Unmarshal DoS amplification",
			MaxVoteExtensionBytes, len(voteExtension))
	}

	vote := &ValidatorVote{}
	if err := proto.Unmarshal(voteExtension, vote); err != nil {
		return nil, err
	}

	if vote.Timestamp.IsZero() {
		return nil, errors.New("vote timestamp missing")
	}

	return vote, nil
}
