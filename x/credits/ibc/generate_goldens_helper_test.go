//go:build cosmos && generate_goldens

package ibc

import (
	"os"
	"path/filepath"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// TestGenerateGoldens is gated by the generate_goldens build tag.
// Run with: go test -tags='cosmos generate_goldens' -run TestGenerateGoldens ./x/credits/ibc/
func TestGenerateGoldens(t *testing.T) {
	generate := func(filename string, data []byte) {
		path := filepath.Join("testdata", filename)
		require.NoError(t, os.WriteFile(path, data, 0644))
		t.Logf("wrote %s (%d bytes)", path, len(data))
	}

	// Full packet
	{
		transfer := EscrowTransfer{
			Denom:    "ulac",
			Amount:   sdkmath.NewInt(1000000),
			Sender:   "lumera1sender",
			Receiver: "inj1receiver",
			Memo: SettlementMemo{
				Type:          MemoTypeSettlement,
				SettlementID:  "packet-wire-001",
				ReceiptHash:   "blake3:packetwire",
				Router:        "lumera1router",
				Publisher:     "lumera1publisher",
				ToolID:        "tool-packet",
				ToolpackID:    "toolpack-packet",
				ActionID:      "action-packet",
				RefundAddress: "lumera1refund",
			},
		}
		packet, err := BuildEscrowPacket(transfer)
		require.NoError(t, err)
		generate("packet_escrow_full.golden.json", packet.GetBytes())
	}

	// Minimal packet
	{
		transfer := EscrowTransfer{
			Denom:    "ulac",
			Amount:   sdkmath.NewInt(42),
			Sender:   "lumera1sender",
			Receiver: "inj1receiver",
			Memo: SettlementMemo{
				SettlementID:  "packet-wire-min-001",
				RefundAddress: "lumera1refund",
			},
		}
		packet, err := BuildEscrowPacket(transfer)
		require.NoError(t, err)
		generate("packet_escrow_minimal.golden.json", packet.GetBytes())
	}
}
