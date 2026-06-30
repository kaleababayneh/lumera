package types

import (
	"bytes"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
)

func TestWorkflowCardProtoRoundtrip(t *testing.T) {
	card := &WorkflowCard{
		WorkflowId:   "11111111-1111-4111-8111-111111111111",
		Version:      "1.0.0",
		DisplayName:  "Linear Price Brief",
		Description:  "Fetch a token price, summarize it, and emit a compact briefing.",
		AuthorId:     "workflow-author-linear",
		AuthorPubkey: "ed448:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Categories:   []string{"defi", "briefing"},
		Tags:         []string{"golden", "linear"},
		LicenseLane:  "byo_key",
		Jurisdictions: []string{
			"US",
			"GB",
		},
		Dag: []*Step{
			{
				StepId:                "fetch_price",
				ToolId:                "defi.token_price",
				ToolVersionConstraint: ">=1.0.0 <2.0.0",
				InputBinding:          "$.inputs.asset",
				MaxSubCost:            coin("ulac", "20000"),
				SubSloP95Ms:           300,
				RetryPolicy: &RetryPolicy{
					MaxAttempts:       2,
					InitialBackoffMs:  50,
					MaxBackoffMs:      250,
					BackoffMultiplier: 2,
					RetryableErrorCodes: []string{
						"publisher_unavailable",
					},
				},
				FailureAction: FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
				SideEffect:    SideEffect_SIDE_EFFECT_REVERSIBLE,
			},
			{
				StepId:                "summarize_price",
				ToolId:                "docs.summarize",
				ToolVersionConstraint: "^1.2.0",
				InputBinding:          "$.steps.fetch_price.output",
				MaxSubCost:            coin("ulac", "15000"),
				SubSloP95Ms:           500,
				RetryPolicy: &RetryPolicy{
					MaxAttempts: 1,
				},
				FailureAction: FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
				SideEffect:    SideEffect_SIDE_EFFECT_REVERSIBLE,
				DependsOn: []string{
					"fetch_price",
				},
			},
		},
		InputSchema:  `{"type":"object","required":["asset"],"properties":{"asset":{"type":"string"}}}`,
		OutputSchema: `{"type":"object","required":["brief"],"properties":{"brief":{"type":"string"}}}`,
		Pricing: &WorkflowPricing{
			PricingModel:    "sum_steps_plus_margin",
			AuthorMarginBps: 500,
			MinBond:         coin("ulac", "1000000"),
			InsuranceBps:    300,
		},
		PassportRequirements: &PassportRequirements{
			MinTier:               PassportTier_PASSPORT_TIER_STANDARD,
			MinReputationScore:    600,
			RequireActivePassport: true,
		},
		Governance: &Governance{
			AuthorAddresses: []string{
				"lumera1linearworkflowauthor",
			},
			UpgradePolicy:        UpgradePolicy_UPGRADE_POLICY_SEMVER_COMPATIBLE,
			DisputeWindowSeconds: 86400,
		},
		SafetyInvariants: []*SafetyInvariant{
			{
				InvariantId: "total_cost_bound",
				Expression:  "total_cost <= max_cost",
				Phase:       InvariantPhase_INVARIANT_PHASE_VERIFY,
				Severity:    "error",
				ErrorCode:   "workflow_cost_exceeded",
				HintMessage: "Increase the workflow max_cost or choose cheaper tools.",
			},
		},
		SchemaHash: []byte("workflowcard.schema.json"),
		AuthorSig:  []byte("author-signature-placeholder"),
		Metadata: map[string]string{
			"fixture": "simple_linear",
		},
		CreatedAt: time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 20, 12, 30, 0, 0, time.UTC),
	}

	wire, err := proto.Marshal(card)
	if err != nil {
		t.Fatalf("marshal workflow card: %v", err)
	}

	var decoded WorkflowCard
	if err := proto.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("unmarshal workflow card: %v", err)
	}
	// gogoproto's proto.Equal cannot compare the customtype math.Int amounts on
	// the embedded Coin fields, so assert on the deterministic wire bytes instead.
	reWire, err := proto.Marshal(&decoded)
	if err != nil {
		t.Fatalf("re-marshal workflow card: %v", err)
	}
	if !bytes.Equal(wire, reWire) {
		t.Fatalf("workflow card roundtrip drift:\n got: %v\nwant: %v", &decoded, card)
	}
}

func FuzzWorkflowCardProtoDecoding(f *testing.F) {
	valid := &WorkflowCard{
		WorkflowId:  "11111111-1111-4111-8111-111111111111",
		Version:     "1.0.0",
		DisplayName: "Fuzz Seed",
		AuthorId:    "workflow-author",
		Dag: []*Step{
			{
				StepId:                "fetch_price",
				ToolId:                "defi.token_price",
				ToolVersionConstraint: "*",
				InputBinding:          "$.inputs.asset",
				MaxSubCost:            coin("ulac", "1"),
				SubSloP95Ms:           1,
				RetryPolicy:           &RetryPolicy{MaxAttempts: 1},
				FailureAction:         FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
				SideEffect:            SideEffect_SIDE_EFFECT_REVERSIBLE,
			},
		},
	}
	wire, err := proto.Marshal(valid)
	if err != nil {
		f.Fatalf("marshal seed: %v", err)
	}

	f.Add([]byte{})
	f.Add([]byte{0xff, 0x01, 0x02})
	f.Add(wire)
	f.Fuzz(func(t *testing.T, data []byte) {
		var card WorkflowCard
		_ = proto.Unmarshal(data, &card)
	})
}

func coin(denom, amount string) sdk.Coin {
	amt, ok := sdkmath.NewIntFromString(amount)
	if !ok {
		amt = sdkmath.ZeroInt()
	}
	return sdk.Coin{Denom: denom, Amount: amt}
}
