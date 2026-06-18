//go:build cosmos

package types

import (
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/require"
)

// Closes the MsgXxx.ValidateBasic coverage-gap sweep in
// x/registry/types/msgs.go. Ticks 12-14 covered 10 methods across
// three thematic groups (general, receipt, governance). This tick
// pins the remaining 6: Update/Delist tool, RegisterWatcher,
// SubmitSLOProbeReceipt, WithdrawBond, SetToolCapsule.
//
// Pairs of structurally-identical methods (Update/Delist, and
// Create/Withdraw Bond) are tested INDEPENDENTLY even though their
// code is byte-for-byte identical. A regression that silently
// removed a guard from only ONE of the pair must surface — that's
// the only failure mode a shared-helper test wouldn't catch, since
// the methods embed the helper call in their own body.

// TestMsgUpdateTool_ValidateBasic pins the 2-branch validator at
// msgs.go:128-136. Structurally identical to MsgDelistTool below
// (byte-for-byte, only the receiver type differs).
func TestMsgUpdateTool_ValidateBasic(t *testing.T) {
	owner := validBech32Addr(t, 0x61)
	cases := []struct {
		name    string
		msg     *MsgUpdateTool
		wantErr bool
	}{
		{"happy_path", &MsgUpdateTool{Owner: owner, ToolId: "tool-1"}, false},
		{"empty_owner", &MsgUpdateTool{Owner: "", ToolId: "tool-1"}, true},
		{"invalid_bech32_owner", &MsgUpdateTool{Owner: "not-bech32", ToolId: "tool-1"}, true},
		{"empty_tool_id", &MsgUpdateTool{Owner: owner, ToolId: ""}, true},
		{"whitespace_tool_id", &MsgUpdateTool{Owner: owner, ToolId: " "}, true},
		{"padded_tool_id", &MsgUpdateTool{Owner: owner, ToolId: " tool-1 "}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantErr {
				require.Error(t, tc.msg.ValidateBasic())
			} else {
				require.NoError(t, tc.msg.ValidateBasic())
			}
		})
	}
}

// TestMsgDelistTool_ValidateBasic pins the byte-identical twin at
// msgs.go:150-158. Tested independently from MsgUpdateTool so a
// regression that silently dropped a guard from only one of the
// pair surfaces without relying on code-dedup assumptions.
func TestMsgDelistTool_ValidateBasic(t *testing.T) {
	owner := validBech32Addr(t, 0x62)
	cases := []struct {
		name    string
		msg     *MsgDelistTool
		wantErr bool
	}{
		{"happy_path", &MsgDelistTool{Owner: owner, ToolId: "tool-1"}, false},
		{"empty_owner", &MsgDelistTool{Owner: "", ToolId: "tool-1"}, true},
		{"empty_tool_id", &MsgDelistTool{Owner: owner, ToolId: ""}, true},
		{"whitespace_tool_id", &MsgDelistTool{Owner: owner, ToolId: "\t"}, true},
		{"padded_tool_id", &MsgDelistTool{Owner: owner, ToolId: "\ttool-1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantErr {
				require.Error(t, tc.msg.ValidateBasic())
			} else {
				require.NoError(t, tc.msg.ValidateBasic())
			}
		})
	}
}

// TestMsgRegisterWatcher_ValidateBasic pins the 2-branch validator
// at msgs.go:271-279: watcher addr + stake coins. Complements
// MsgUnregisterWatcher (tick 12) which only validates the address
// — RegisterWatcher adds the stake-coins requirement. A silent
// "stake not required" regression would let watchers register
// without bonding any collateral.
func TestMsgRegisterWatcher_ValidateBasic(t *testing.T) {
	watcher := validBech32Addr(t, 0x63)
	validStake := []*v1beta1.Coin{{Denom: "ulac", Amount: "1000000"}}
	cases := []struct {
		name      string
		msg       *MsgRegisterWatcher
		wantErr   bool
		errSubstr string
	}{
		{"happy_path", &MsgRegisterWatcher{Watcher: watcher, Stake: validStake}, false, ""},
		{"empty_watcher", &MsgRegisterWatcher{Watcher: "", Stake: validStake}, true, "address"},
		{"invalid_bech32_watcher", &MsgRegisterWatcher{Watcher: "not-bech32", Stake: validStake}, true, ""},
		{"empty_stake", &MsgRegisterWatcher{Watcher: watcher, Stake: []*v1beta1.Coin{}}, true, "stake"},
		{"nil_stake", &MsgRegisterWatcher{Watcher: watcher, Stake: nil}, true, "stake"},
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

// TestMsgSubmitSLOProbeReceipt_ValidateBasic pins the SLO probe
// submission validator at msgs.go:312-334. The msg-level validator
// must reject malformed probe payloads before they reach the handler:
// watcher addr, nil receipt, receipt_id, watcher-address parity, and
// the nested SLOProbeReceipt.Validate invariants.
func TestMsgSubmitSLOProbeReceipt_ValidateBasic(t *testing.T) {
	watcher := validBech32Addr(t, 0x64)
	validReceipt := func() *SLOProbeReceipt {
		t.Helper()
		receipt := validSLOProbeReceipt(t)
		receipt.ReceiptId = "probe-1"
		receipt.WatcherAddress = watcher
		return receipt
	}
	blankWatcherReceipt := validReceipt()
	blankWatcherReceipt.WatcherAddress = ""
	mismatchedWatcherReceipt := validReceipt()
	mismatchedWatcherReceipt.WatcherAddress = validBech32Addr(t, 0x65)
	zeroProbeCountReceipt := validReceipt()
	zeroProbeCountReceipt.ProbeCount = 0
	countsExceedProbeCountReceipt := validReceipt()
	countsExceedProbeCountReceipt.SuccessCount = countsExceedProbeCountReceipt.ProbeCount + 1

	cases := []struct {
		name      string
		msg       *MsgSubmitSLOProbeReceipt
		wantErr   bool
		errSubstr string
	}{
		{
			"happy_path_matching_receipt_watcher",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: validReceipt()},
			false, "",
		},
		{
			"happy_path_fills_blank_receipt_watcher",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: blankWatcherReceipt},
			false, "",
		},
		{
			"empty_watcher",
			&MsgSubmitSLOProbeReceipt{Watcher: "", Receipt: validReceipt()},
			true, "address",
		},
		{
			"nil_receipt",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: nil},
			true, "receipt is required", // distinct from "receipt.receipt_id"
		},
		{
			"empty_receipt_id",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: func() *SLOProbeReceipt {
				receipt := validReceipt()
				receipt.ReceiptId = ""
				return receipt
			}()},
			true, "receipt.receipt_id",
		},
		{
			"whitespace_receipt_id",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: func() *SLOProbeReceipt {
				receipt := validReceipt()
				receipt.ReceiptId = "\n"
				return receipt
			}()},
			true, "receipt.receipt_id",
		},
		{
			"padded_receipt_id",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: func() *SLOProbeReceipt {
				receipt := validReceipt()
				receipt.ReceiptId = " probe-1"
				return receipt
			}()},
			true, "receipt.receipt_id",
		},
		{
			"padded_tool_id",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: func() *SLOProbeReceipt {
				receipt := validReceipt()
				receipt.ToolId = " defi.token-price"
				return receipt
			}()},
			true, "tool_id",
		},
		{
			"mismatched_receipt_watcher",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: mismatchedWatcherReceipt},
			true, "receipt.watcher_address",
		},
		{
			"zero_probe_count",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: zeroProbeCountReceipt},
			true, "probe_count",
		},
		{
			"counts_exceed_probe_count",
			&MsgSubmitSLOProbeReceipt{Watcher: watcher, Receipt: countsExceedProbeCountReceipt},
			true, "success_count + failure_count",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMsgWithdrawBond_ValidateBasic pins the byte-identical twin of
// MsgCreateBond (tick 12) at msgs.go:384-395. Same rationale as
// the Update/Delist pair: independent test so a regression in one
// but not the other surfaces.
func TestMsgWithdrawBond_ValidateBasic(t *testing.T) {
	owner := validBech32Addr(t, 0x65)
	validAmount := []*v1beta1.Coin{{Denom: "ulac", Amount: "500000"}}
	cases := []struct {
		name    string
		msg     *MsgWithdrawBond
		wantErr bool
	}{
		{"happy_path", &MsgWithdrawBond{Owner: owner, ToolId: "tool-1", Amount: validAmount}, false},
		{"empty_owner", &MsgWithdrawBond{Owner: "", ToolId: "tool-1", Amount: validAmount}, true},
		{"empty_tool_id", &MsgWithdrawBond{Owner: owner, ToolId: "", Amount: validAmount}, true},
		{"whitespace_tool_id", &MsgWithdrawBond{Owner: owner, ToolId: " \n", Amount: validAmount}, true},
		{"padded_tool_id", &MsgWithdrawBond{Owner: owner, ToolId: "tool-1 ", Amount: validAmount}, true},
		{"empty_amount", &MsgWithdrawBond{Owner: owner, ToolId: "tool-1", Amount: []*v1beta1.Coin{}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantErr {
				require.Error(t, tc.msg.ValidateBasic())
			} else {
				require.NoError(t, tc.msg.ValidateBasic())
			}
		})
	}
}

// TestMsgUpdateParams_ValidateBasic pins the minimal 2-branch
// governance-params validator at msgs.go:337-345: authority via
// sanitizeAddress + params non-nil. This is the consensus-
// critical governance entry point for parameter updates — a
// silent-accept here would let malformed MsgUpdateParams reach
// the handler where it would fail at the params.Set step, but the
// caller would see a less-specific error and governance proposal
// post-mortems would lack the "ValidateBasic rejected" audit
// signal. Pinning both branches here defends the signal.
func TestMsgUpdateParams_ValidateBasic(t *testing.T) {
	validAuth := validBech32Addr(t, 0x67)
	validParams := &RegistryParams{BurnRateSpendBps: 300}

	cases := []struct {
		name      string
		msg       *MsgUpdateParams
		wantErr   bool
		errSubstr string
	}{
		{"happy_path", &MsgUpdateParams{Authority: validAuth, Params: validParams}, false, ""},
		{"empty_authority", &MsgUpdateParams{Authority: "", Params: validParams}, true, "address"},
		{"invalid_bech32_authority", &MsgUpdateParams{Authority: "not-bech32", Params: validParams}, true, ""},
		{"nil_params", &MsgUpdateParams{Authority: validAuth, Params: nil}, true, "params is required"},
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

// TestMsgSetToolCapsule_ValidateBasic pins the 3-branch validator
// at msgs.go:471-482 — same EXPLICIT-nil-then-field shape as
// MsgSubmitSLOProbeReceipt above (the two share this pattern across
// otherwise-unrelated msg families). Distinguishes from the
// short-circuit form of MsgSubmitReceipt, documenting the intentional
// structural split that a refactor might accidentally unify.
//
// Complements the keeper-side SetToolCapsule tests landed in
// commit 261c6bd86, which covered the stateful guards (tool must
// exist, owner must match); this test covers the STATELESS
// validator that runs before them in the msg pipeline.
func TestMsgSetToolCapsule_ValidateBasic(t *testing.T) {
	owner := validBech32Addr(t, 0x66)
	validCapsule := &ToolCapsule{ToolId: "tool-1"}
	cases := []struct {
		name      string
		msg       *MsgSetToolCapsule
		wantErr   bool
		errSubstr string
	}{
		{"happy_path", &MsgSetToolCapsule{Owner: owner, Capsule: validCapsule}, false, ""},
		{"empty_owner", &MsgSetToolCapsule{Owner: "", Capsule: validCapsule}, true, "address"},
		{"nil_capsule", &MsgSetToolCapsule{Owner: owner, Capsule: nil}, true, "capsule is required"},
		{"empty_tool_id_in_capsule", &MsgSetToolCapsule{Owner: owner, Capsule: &ToolCapsule{ToolId: ""}}, true, "capsule.tool_id"},
		{"whitespace_tool_id_in_capsule", &MsgSetToolCapsule{Owner: owner, Capsule: &ToolCapsule{ToolId: "\t "}}, true, "capsule.tool_id"},
		{"padded_tool_id_in_capsule", &MsgSetToolCapsule{Owner: owner, Capsule: &ToolCapsule{ToolId: " tool-1"}}, true, "capsule.tool_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
