//go:build cosmos

package types

import (
	"bytes"
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// validBech32Addr returns a syntactically valid bech32 address for
// test inputs where the address itself isn't under test (so we can
// isolate the branch being exercised).
func validBech32Addr(t *testing.T, seedByte byte) string {
	t.Helper()
	return sdk.AccAddress(bytes.Repeat([]byte{seedByte}, 20)).String()
}

// TestMsgRegisterTool_ValidateBasic pins the four-branch guard
// chain in MsgRegisterTool.ValidateBasic at msgs.go:103-114. This
// is the entry-point validator for the MsgRegisterTool handler —
// anything that passes here reaches the keeper's SetToolCard path.
// A silent-accept on any of these branches would let malformed
// state-writing msgs through to the keeper. Initial bond sufficiency
// is param-dependent, so omitted bonds are keeper-validated instead
// of rejected by this stateless entry-point check.
//
// Branches pinned (in guard order):
//  1. Owner address  → sanitizeAddress at msgs.go:35-40.
//     Empty, whitespace, and invalid-bech32 Owner all rejected.
//  2. Tool card nil  → fails at msgs.go:107.
//  3. Tool ID        → empty, whitespace-only, or padded tool_id rejected.
//  4. Bond coins     → coinsFromProto at msgs.go:76-89.
//     Omitted bond accepted; malformed non-empty coins rejected;
//     valid coins accepted.
//
// Happy path: all four branches pass → ValidateBasic returns nil.
func TestMsgRegisterTool_ValidateBasic(t *testing.T) {
	validOwner := validBech32Addr(t, 0x11)
	validBond := []*v1beta1.Coin{{Denom: "ulac", Amount: "1000000"}}
	validCard := &ToolCard{ToolId: "tool-1", Owner: validOwner, Version: "1.0.0"}

	cases := []struct {
		name      string
		msg       *MsgRegisterTool
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "happy_path",
			msg:     &MsgRegisterTool{Owner: validOwner, ToolCard: validCard, Bond: validBond},
			wantErr: false,
		},
		{
			name:      "empty_owner",
			msg:       &MsgRegisterTool{Owner: "", ToolCard: validCard, Bond: validBond},
			wantErr:   true,
			errSubstr: "address",
		},
		{
			name:      "whitespace_owner",
			msg:       &MsgRegisterTool{Owner: "   ", ToolCard: validCard, Bond: validBond},
			wantErr:   true,
			errSubstr: "address",
		},
		{
			name:    "invalid_bech32_owner",
			msg:     &MsgRegisterTool{Owner: "not-a-valid-bech32", ToolCard: validCard, Bond: validBond},
			wantErr: true, // sdk.AccAddressFromBech32 error — exact text is lib-owned.
		},
		{
			name:      "nil_tool_card",
			msg:       &MsgRegisterTool{Owner: validOwner, ToolCard: nil, Bond: validBond},
			wantErr:   true,
			errSubstr: "tool_card.tool_id",
		},
		{
			name:      "empty_tool_id_in_card",
			msg:       &MsgRegisterTool{Owner: validOwner, ToolCard: &ToolCard{ToolId: "", Owner: validOwner}, Bond: validBond},
			wantErr:   true,
			errSubstr: "tool_card.tool_id",
		},
		{
			name:      "whitespace_tool_id_in_card",
			msg:       &MsgRegisterTool{Owner: validOwner, ToolCard: &ToolCard{ToolId: "   ", Owner: validOwner}, Bond: validBond},
			wantErr:   true,
			errSubstr: "tool_card.tool_id",
		},
		{
			name:      "padded_tool_id_in_card",
			msg:       &MsgRegisterTool{Owner: validOwner, ToolCard: &ToolCard{ToolId: " tool-1 ", Owner: validOwner}, Bond: validBond},
			wantErr:   true,
			errSubstr: "tool_card.tool_id",
		},
		{
			name:    "omitted_bond",
			msg:     &MsgRegisterTool{Owner: validOwner, ToolCard: validCard},
			wantErr: false,
		},
		{
			name:      "malformed_bond",
			msg:       &MsgRegisterTool{Owner: validOwner, ToolCard: validCard, Bond: []*v1beta1.Coin{{Denom: "", Amount: "1"}}},
			wantErr:   true,
			errSubstr: "bond",
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

// TestMsgCreateBond_ValidateBasic pins the three-branch guard chain
// at msgs.go:359-370. Same shape as MsgWithdrawBond (msgs.go:384-395)
// — pinning CreateBond's branches also defends the sibling's attr
// contract by extension, since both go through the same helpers.
//
// Branches pinned:
//  1. Owner  → sanitizeAddress.
//  2. Tool ID → empty/whitespace/padded rejected.
//  3. Amount → coinsFromProto, empty list rejected.
func TestMsgCreateBond_ValidateBasic(t *testing.T) {
	validOwner := validBech32Addr(t, 0x22)
	validAmount := []*v1beta1.Coin{{Denom: "ulac", Amount: "5000000"}}

	cases := []struct {
		name      string
		msg       *MsgCreateBond
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "happy_path",
			msg:     &MsgCreateBond{Owner: validOwner, ToolId: "tool-bond", Amount: validAmount},
			wantErr: false,
		},
		{
			name:      "empty_owner",
			msg:       &MsgCreateBond{Owner: "", ToolId: "tool-bond", Amount: validAmount},
			wantErr:   true,
			errSubstr: "address",
		},
		{
			name:    "invalid_bech32_owner",
			msg:     &MsgCreateBond{Owner: "garbage-not-bech32", ToolId: "tool-bond", Amount: validAmount},
			wantErr: true,
		},
		{
			name:      "empty_tool_id",
			msg:       &MsgCreateBond{Owner: validOwner, ToolId: "", Amount: validAmount},
			wantErr:   true,
			errSubstr: "tool_id",
		},
		{
			name:      "whitespace_tool_id",
			msg:       &MsgCreateBond{Owner: validOwner, ToolId: "\t\n", Amount: validAmount},
			wantErr:   true,
			errSubstr: "tool_id",
		},
		{
			name:      "padded_tool_id",
			msg:       &MsgCreateBond{Owner: validOwner, ToolId: " tool-bond ", Amount: validAmount},
			wantErr:   true,
			errSubstr: "tool_id",
		},
		{
			name:      "empty_amount",
			msg:       &MsgCreateBond{Owner: validOwner, ToolId: "tool-bond", Amount: []*v1beta1.Coin{}},
			wantErr:   true,
			errSubstr: "amount",
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

// TestMsgUnregisterWatcher_ValidateBasic pins the minimal
// single-branch validator at msgs.go:293-298: only the Watcher
// address is checked. This is the simplest of the 16 untested
// MsgXxx.ValidateBasic methods in x/registry/types/msgs.go and
// sets the baseline for the sanitizeAddress contract — every
// other MsgXxx in this file shares this same address-check prefix.
func TestMsgUnregisterWatcher_ValidateBasic(t *testing.T) {
	validWatcher := validBech32Addr(t, 0x33)

	cases := []struct {
		name    string
		watcher string
		wantErr bool
	}{
		{"happy_path", validWatcher, false},
		{"empty", "", true},
		{"whitespace_only", "   ", true},
		{"invalid_bech32", "not-a-valid-address", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MsgUnregisterWatcher{Watcher: tc.watcher}
			err := msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
