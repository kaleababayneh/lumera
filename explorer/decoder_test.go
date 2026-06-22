package main

import "testing"

func TestLabelOf(t *testing.T) {
	cases := map[string]string{
		"/lumera.registry.v1.MsgRegisterTool":          "Register Tool",
		"/lumera.registry.v1.MsgSubmitSLOProbeReceipt": "Submit SLO Probe Receipt",
		"/lumera.registry.v1.MsgSetSLATemplate":        "Set SLA Template",
		"/lumera.credits.v1.MsgSwapLUMEtoLAC":          "Swap LUME to LAC",
		"/lumera.credits.v1.MsgSwapLACtoLUME":          "Swap LAC to LUME",
		"/lumera.incentives.v1.MsgRequestEvaluation":   "Request Evaluation",
		"/cosmos.evm.erc20.v1.MsgConvertERC20":         "Convert ERC20",
		"/cosmos.bank.v1beta1.MsgSend":                 "Send",
	}
	for in, want := range cases {
		if got := labelOf(in); got != want {
			t.Errorf("labelOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModuleOf(t *testing.T) {
	cases := map[string]string{
		"/lumera.registry.v1.MsgRegisterTool":       "registry",
		"/lumera.claim.MsgClaim":                    "claim",
		"/cosmos.bank.v1beta1.MsgSend":              "bank",
		"/cosmos.staking.v1beta1.MsgDelegate":       "staking",
		"/cosmos.evm.vm.v1.MsgEthereumTx":           "vm",
		"/cosmos.evm.erc20.v1.MsgConvertERC20":      "vm",
		"/cosmos.evm.feemarket.v1.MsgUpdateParams":  "vm",
		"/ibc.applications.transfer.v1.MsgTransfer": "ibc",
		"/cosmwasm.wasm.v1.MsgExecuteContract":      "wasm",
	}
	for in, want := range cases {
		if got := moduleOf(in); got != want {
			t.Errorf("moduleOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEventModule(t *testing.T) {
	type tc struct {
		typ   string
		attrs []EventAttr
		want  string
	}
	cases := []tc{
		{"slash", []EventAttr{{Key: "tool_id", Value: "code-fix"}}, "registry"},      // registry bond slash
		{"slash", []EventAttr{{Key: "address", Value: "lumeravaloper1x"}}, "cosmos"}, // SDK validator slash
		{"policy_created", nil, "policies"},
		{"policy_state_changed", nil, "policies"},
		{"cac_royalty_distribution", nil, "credits"},
		{"adaptive_burn_rate_evaluated", nil, "credits"},
		{"ethereum_tx", nil, "vm"},
		{"supernode_storage_full", nil, "supernode"},
		{"badge_awarded", nil, "incentives"},
		{"insurance_pool_metrics_updated", nil, "insurance"},
		{"tool_registered", nil, "registry"},
		{"transfer", nil, "cosmos"},
		{"totally_unknown_event", nil, "chain"},
	}
	for _, c := range cases {
		if got := eventModule(c.typ, c.attrs); got != c.want {
			t.Errorf("eventModule(%q) = %q, want %q", c.typ, got, c.want)
		}
	}
}
