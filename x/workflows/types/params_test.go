package types

import (
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const validWorkflowAuthority = "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"

func TestParamsValidate(t *testing.T) {
	valid := DefaultParams()
	if err := valid.Validate(); err != nil {
		t.Fatalf("default params should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Params)
	}{
		{"negative min bond", func(p *Params) { p.MinAuthorBondAmount = "-1" }},
		{"zero min bond", func(p *Params) { p.MinAuthorBondAmount = "0" }},
		{"bad min bond", func(p *Params) { p.MinAuthorBondAmount = "not-an-int" }},
		{"padded min bond", func(p *Params) { p.MinAuthorBondAmount = " 1000000 " }},
		{"leading zero min bond", func(p *Params) { p.MinAuthorBondAmount = "01000000" }},
		{"empty denom", func(p *Params) { p.BondDenom = "" }},
		{"padded denom", func(p *Params) { p.BondDenom = " ulac " }},
		{"invalid denom", func(p *Params) { p.BondDenom = "1bad" }},
		{"wasted work overflow", func(p *Params) { p.WastedWorkBPS = BPSDenominator + 1 }},
		{"zero versions", func(p *Params) { p.MaxWorkflowVersions = 0 }},
		{"zero dispute window", func(p *Params) { p.DisputeWindowSeconds = 0 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := *DefaultParams()
			tc.mutate(&params)
			if err := params.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestMsgValidateBasic(t *testing.T) {
	validCard := validMsgWorkflowCard()
	msgs := []interface{ ValidateBasic() error }{
		&MsgPublishWorkflow{Author: validWorkflowAuthority, WorkflowCard: validCard},
		&MsgUpgradeWorkflow{Author: validWorkflowAuthority, WorkflowID: "wf-1", FromVersion: "0.9.0", WorkflowCard: validCard},
		&MsgDeactivateWorkflow{Author: validWorkflowAuthority, WorkflowID: "wf-1", Version: "1.0.0"},
		&MsgTopUpAuthorBond{Author: validWorkflowAuthority, Amount: sdk.NewCoin("ulac", sdkmath.NewInt(1))},
		&MsgUpdateParams{Authority: validWorkflowAuthority, Params: DefaultParams()},
	}

	for _, msg := range msgs {
		if err := msg.ValidateBasic(); err != nil {
			t.Fatalf("%T should validate: %v", msg, err)
		}
	}
}

func TestMsgUpdateParamsValidateBasicRejectsMalformedAuthority(t *testing.T) {
	msg := &MsgUpdateParams{
		Authority: "lumera1authority",
		Params:    DefaultParams(),
	}

	err := msg.ValidateBasic()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid authority address") {
		t.Fatalf("expected invalid authority address error, got %q", err.Error())
	}
}

func TestMsgValidateBasicRejectsNonCanonicalWorkflowIdentity(t *testing.T) {
	withCardMutation := func(mutate func(*WorkflowCard)) *WorkflowCard {
		card := validMsgWorkflowCard()
		mutate(card)
		return card
	}

	cases := []struct {
		name string
		msg  interface{ ValidateBasic() error }
		want string
	}{
		{
			name: "publish padded author",
			msg: &MsgPublishWorkflow{
				Author:       " " + validWorkflowAuthority + " ",
				WorkflowCard: validMsgWorkflowCard(),
			},
			want: "author must be canonical",
		},
		{
			name: "publish padded workflow id",
			msg: &MsgPublishWorkflow{
				Author:       validWorkflowAuthority,
				WorkflowCard: withCardMutation(func(card *WorkflowCard) { card.WorkflowId = " wf-1 " }),
			},
			want: "workflow_id must be canonical",
		},
		{
			name: "publish slash workflow id",
			msg: &MsgPublishWorkflow{
				Author:       validWorkflowAuthority,
				WorkflowCard: withCardMutation(func(card *WorkflowCard) { card.WorkflowId = "wf/1" }),
			},
			want: "workflow_id cannot contain /",
		},
		{
			name: "publish padded version",
			msg: &MsgPublishWorkflow{
				Author:       validWorkflowAuthority,
				WorkflowCard: withCardMutation(func(card *WorkflowCard) { card.Version = " 1.0.0 " }),
			},
			want: "version must be canonical",
		},
		{
			name: "upgrade padded request workflow id",
			msg: &MsgUpgradeWorkflow{
				Author:       validWorkflowAuthority,
				WorkflowID:   " wf-1 ",
				FromVersion:  "0.9.0",
				WorkflowCard: validMsgWorkflowCard(),
			},
			want: "workflow_id must be canonical",
		},
		{
			name: "upgrade padded from version",
			msg: &MsgUpgradeWorkflow{
				Author:       validWorkflowAuthority,
				WorkflowID:   "wf-1",
				FromVersion:  " 0.9.0 ",
				WorkflowCard: validMsgWorkflowCard(),
			},
			want: "version must be canonical",
		},
		{
			name: "deactivate padded version",
			msg: &MsgDeactivateWorkflow{
				Author:     validWorkflowAuthority,
				WorkflowID: "wf-1",
				Version:    " 1.0.0 ",
			},
			want: "version must be canonical",
		},
		{
			name: "deactivate slash version",
			msg: &MsgDeactivateWorkflow{
				Author:     validWorkflowAuthority,
				WorkflowID: "wf-1",
				Version:    "1.0/0",
			},
			want: "version cannot contain /",
		},
		{
			name: "withdraw padded author",
			msg: &MsgWithdrawBond{
				Author: validWorkflowAuthority + " ",
				Amount: sdk.NewCoin("ulac", sdkmath.NewInt(1)),
			},
			want: "author must be canonical",
		},
		{
			name: "top up padded author",
			msg: &MsgTopUpAuthorBond{
				Author: " " + validWorkflowAuthority,
				Amount: sdk.NewCoin("ulac", sdkmath.NewInt(1)),
			},
			want: "author must be canonical",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestMsgValidateBasicRejectsMalformedCoinFields(t *testing.T) {
	cases := []struct {
		name string
		msg  interface{ ValidateBasic() error }
		want string
	}{
		{
			name: "publish bond invalid denom",
			msg: &MsgPublishWorkflow{
				Author:       validWorkflowAuthority,
				WorkflowCard: validMsgWorkflowCard(),
				Bond:         sdk.Coin{Denom: "1bad", Amount: sdkmath.NewInt(1)},
			},
			want: "bond denom is invalid",
		},
		{
			name: "publish bond padded denom",
			msg: &MsgPublishWorkflow{
				Author:       validWorkflowAuthority,
				WorkflowCard: validMsgWorkflowCard(),
				Bond:         sdk.Coin{Denom: " ulac ", Amount: sdkmath.NewInt(1)},
			},
			want: "bond denom must be canonical",
		},
		{
			name: "publish bond zero amount",
			msg: &MsgPublishWorkflow{
				Author:       validWorkflowAuthority,
				WorkflowCard: validMsgWorkflowCard(),
				Bond:         sdk.Coin{Denom: "ulac", Amount: sdkmath.ZeroInt()},
			},
			want: "bond amount must be positive",
		},
		{
			name: "withdraw amount negative",
			msg: &MsgWithdrawBond{
				Author: validWorkflowAuthority,
				Amount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(-1)},
			},
			want: "amount amount cannot be negative",
		},
		{
			name: "withdraw amount zero",
			msg: &MsgWithdrawBond{
				Author: validWorkflowAuthority,
				Amount: sdk.Coin{Denom: "ulac", Amount: sdkmath.ZeroInt()},
			},
			want: "amount amount must be positive",
		},
		{
			name: "top up amount negative",
			msg: &MsgTopUpAuthorBond{
				Author: validWorkflowAuthority,
				Amount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(-1)},
			},
			want: "amount amount cannot be negative",
		},
		{
			name: "top up amount zero",
			msg: &MsgTopUpAuthorBond{
				Author: validWorkflowAuthority,
				Amount: sdk.Coin{Denom: "ulac", Amount: sdkmath.ZeroInt()},
			},
			want: "amount amount must be positive",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func validMsgWorkflowCard() *WorkflowCard {
	return &WorkflowCard{
		WorkflowId:   "wf-1",
		Version:      "1.0.0",
		DisplayName:  "Msg workflow fixture",
		AuthorId:     "author-1",
		AuthorPubkey: validStaticWorkflowAuthorPubkey(),
		Categories:   []string{"agent-contracts"},
		LicenseLane:  "byo_key",
		Dag: []*Step{
			{
				StepId:                "step-a",
				ToolId:                "tool.alpha",
				ToolVersionConstraint: "1.0.0",
				InputBinding:          "$.inputs",
				MaxSubCost:            sdk.NewCoin("ulac", sdkmath.NewInt(1)),
				SubSloP95Ms:           1000,
				RetryPolicy:           &RetryPolicy{MaxAttempts: 1},
				FailureAction:         FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
				SideEffect:            SideEffect_SIDE_EFFECT_REVERSIBLE,
			},
		},
		InputSchema:  `{"type":"object"}`,
		OutputSchema: `{"type":"object"}`,
		PassportRequirements: &PassportRequirements{
			MinTier: PassportTier_PASSPORT_TIER_BASIC,
		},
		Pricing: &WorkflowPricing{
			PricingModel: "sum_steps_plus_margin",
			MinBond:      sdk.NewCoin("ulac", sdkmath.NewInt(1000000)),
		},
		Governance:       validMsgWorkflowGovernance(),
		SafetyInvariants: []*SafetyInvariant{validStaticWorkflowSafetyInvariant()},
	}
}

func validMsgWorkflowGovernance() *Governance {
	return &Governance{
		AuthorAddresses: []string{validWorkflowAuthority},
		UpgradePolicy:   UpgradePolicy_UPGRADE_POLICY_SEMVER_COMPATIBLE,
	}
}
