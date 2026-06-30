package types

import (
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NOTE: TestGenesisState_Validate_RejectsInvalidWorkflowCardTimestamps was
// removed after the gogoproto migration: WorkflowCard.created_at/updated_at are
// now value time.Time (stdtime), which has no out-of-range/invalid state to
// reject, so the former cases are unreachable.

func TestGenesisState_Validate_AllowsNilWorkflowCardTimestamps(t *testing.T) {
	genesis := genesisWithWorkflowCard(validGenesisWorkflowCard())

	if err := genesis.Validate(); err != nil {
		t.Fatalf("nil workflow card timestamps should validate: %v", err)
	}
}

func TestGenesisState_Validate_RejectsNonTerminalNonReversibleWorkflowCard(t *testing.T) {
	card := validGenesisWorkflowCard()
	card.Dag[0].SideEffect = SideEffect_SIDE_EFFECT_NON_REVERSIBLE
	card.Dag = append(card.Dag, staticStep("step-b", "step-a"))
	genesis := genesisWithWorkflowCard(card)

	err := genesis.Validate()
	if err == nil {
		t.Fatal("expected non-terminal non_reversible workflow card to fail genesis validation")
	}
	if !strings.Contains(err.Error(), WorkflowStaticReasonStepSideEffectNonTerminal) {
		t.Fatalf("expected static non-terminal side_effect reason, got %v", err)
	}
}

func TestGenesisState_Validate_RejectsUncanonicalWorkflowStatus(t *testing.T) {
	genesis := genesisWithWorkflowCard(validGenesisWorkflowCard())
	genesis.Workflows[0].Status = " active "

	err := genesis.Validate()
	if err == nil {
		t.Fatal("expected padded workflow status to be rejected")
	}
	if !strings.Contains(err.Error(), "status must be canonical") {
		t.Fatalf("expected canonical status error, got %v", err)
	}
}

func TestGenesisState_Validate_RejectsInvalidWorkflowAuthorBondLocks(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*GenesisState)
		want   string
	}{
		{
			name: "missing active workflow lock",
			mutate: func(genesis *GenesisState) {
				genesis.AuthorBonds = nil
			},
			want: "missing author bond lock",
		},
		{
			name: "lock owned by different author",
			mutate: func(genesis *GenesisState) {
				genesis.AuthorBonds[0].AuthorAddress = "lumera1other"
			},
			want: "owned by lumera1author",
		},
		{
			name: "inactive workflow remains locked",
			mutate: func(genesis *GenesisState) {
				genesis.Workflows[0].Status = WorkflowStatusInactive
			},
			want: "references inactive workflow",
		},
		{
			name: "padded workflow author",
			mutate: func(genesis *GenesisState) {
				genesis.Workflows[0].AuthorAddress = " lumera1author "
			},
			want: "author_address must be canonical",
		},
		{
			name: "padded author bond",
			mutate: func(genesis *GenesisState) {
				genesis.AuthorBonds[0].AuthorAddress = " lumera1author "
			},
			want: "author_address must be canonical",
		},
		{
			name: "padded workflow id",
			mutate: func(genesis *GenesisState) {
				genesis.Workflows[0].WorkflowID = " wf-genesis "
			},
			want: "workflow_id must be canonical",
		},
		{
			name: "slash workflow id",
			mutate: func(genesis *GenesisState) {
				genesis.Workflows[0].WorkflowID = "wf/genesis"
			},
			want: "workflow_id cannot contain /",
		},
		{
			name: "padded workflow version",
			mutate: func(genesis *GenesisState) {
				genesis.Workflows[0].Version = " 1.0.0 "
			},
			want: "version must be canonical",
		},
		{
			name: "slash workflow version",
			mutate: func(genesis *GenesisState) {
				genesis.Workflows[0].Version = "1.0/0"
			},
			want: "version cannot contain /",
		},
		{
			name: "padded workflow card id",
			mutate: func(genesis *GenesisState) {
				genesis.Workflows[0].Card.WorkflowId = " wf-genesis "
			},
			want: "workflow_id must be canonical",
		},
		{
			name: "padded locked workflow key",
			mutate: func(genesis *GenesisState) {
				genesis.AuthorBonds[0].LockedFor = []string{" wf-genesis/1.0.0 "}
			},
			want: "locked_for entry must be canonical",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			genesis := genesisWithWorkflowCard(validGenesisWorkflowCard())
			tc.mutate(genesis)

			err := genesis.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to mention %q, got %v", tc.want, err)
			}
		})
	}
}

func TestGenesisState_Validate_RejectsInvalidAuthorBondCoinAmounts(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AuthorBondRecord)
		want   string
	}{
		{
			name: "missing bond",
			mutate: func(bond *AuthorBondRecord) {
				bond.Bond = nil
			},
			want: "missing coin",
		},
		{
			name: "bond nil amount",
			mutate: func(bond *AuthorBondRecord) {
				bond.Bond = &sdk.Coin{Denom: "ulac"}
			},
			want: "missing amount",
		},
		{
			name: "bond negative amount",
			mutate: func(bond *AuthorBondRecord) {
				bond.Bond.Amount = sdkmath.NewInt(-1)
			},
			want: "amount cannot be negative",
		},
		{
			name: "bond zero amount",
			mutate: func(bond *AuthorBondRecord) {
				bond.Bond.Amount = sdkmath.ZeroInt()
			},
			want: "amount must be positive",
		},
		{
			name: "slashed nil amount",
			mutate: func(bond *AuthorBondRecord) {
				bond.Slashed = &sdk.Coin{Denom: "ulac"}
			},
			want: "missing amount",
		},
		{
			name: "slashed negative amount",
			mutate: func(bond *AuthorBondRecord) {
				bond.Slashed = &sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(-1)}
			},
			want: "amount cannot be negative",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bond := validGenesisAuthorBond()
			tc.mutate(bond)
			genesis := &GenesisState{
				Params:      DefaultParams(),
				AuthorBonds: []*AuthorBondRecord{bond},
			}

			err := genesis.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to mention %q, got %v", tc.want, err)
			}
		})
	}
}

func TestGenesisState_Validate_RejectsNonCanonicalAuthorBondCoins(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AuthorBondRecord)
		want   string
	}{
		{
			name: "bond padded denom",
			mutate: func(bond *AuthorBondRecord) {
				bond.Bond.Denom = " ulac "
			},
			want: "denom must be canonical",
		},
		{
			name: "bond invalid denom",
			mutate: func(bond *AuthorBondRecord) {
				bond.Bond.Denom = "1bad"
			},
			want: "denom is invalid",
		},
		{
			name: "slashed padded denom",
			mutate: func(bond *AuthorBondRecord) {
				bond.Slashed = &sdk.Coin{Denom: " ulac ", Amount: sdkmath.NewInt(1)}
			},
			want: "denom must be canonical",
		},
		{
			name: "slashed invalid denom",
			mutate: func(bond *AuthorBondRecord) {
				bond.Slashed = &sdk.Coin{Denom: "1bad", Amount: sdkmath.NewInt(1)}
			},
			want: "denom is invalid",
		},
		// NOTE: amount canonicalization cases (padded / leading-zero strings)
		// were removed: after the gogoproto migration bond amounts are
		// math.Int, which has no non-canonical string representation.
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bond := validGenesisAuthorBond()
			tc.mutate(bond)
			genesis := &GenesisState{
				Params:      DefaultParams(),
				AuthorBonds: []*AuthorBondRecord{bond},
			}

			err := genesis.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to mention %q, got %v", tc.want, err)
			}
		})
	}
}

func genesisWithWorkflowCard(card *WorkflowCard) *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
		Workflows: []*WorkflowRecord{
			{
				WorkflowID:    card.WorkflowId,
				Version:       card.Version,
				Status:        WorkflowStatusActive,
				AuthorAddress: "lumera1author",
				Card:          card,
			},
		},
		AuthorBonds: []*AuthorBondRecord{
			{
				AuthorAddress: "lumera1author",
				Bond:          &sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(2500000)},
				LockedFor:     []string{card.WorkflowId + "/" + card.Version},
			},
		},
	}
}

func validGenesisAuthorBond() *AuthorBondRecord {
	return &AuthorBondRecord{
		AuthorAddress: "lumera1author",
		Bond:          &sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(2500000)},
	}
}

func validGenesisWorkflowCard() *WorkflowCard {
	return &WorkflowCard{
		WorkflowId:   "wf-genesis",
		Version:      "1.0.0",
		DisplayName:  "Genesis workflow fixture",
		AuthorId:     "author-1",
		AuthorPubkey: validStaticWorkflowAuthorPubkey(),
		Categories:   []string{"agent-contracts"},
		LicenseLane:  "byo_key",
		InputSchema:  `{"type":"object","properties":{"asset":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		Dag: []*Step{
			{
				StepId:                "step-a",
				ToolId:                "tool.step-a",
				ToolVersionConstraint: "1.0.0",
				InputBinding:          "$.inputs.asset",
				MaxSubCost:            sdk.NewCoin("ulac", sdkmath.NewInt(1)),
				SubSloP95Ms:           1000,
				RetryPolicy:           &RetryPolicy{MaxAttempts: 1},
				FailureAction:         FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
				SideEffect:            SideEffect_SIDE_EFFECT_REVERSIBLE,
			},
		},
		PassportRequirements: &PassportRequirements{
			MinTier: PassportTier_PASSPORT_TIER_BASIC,
		},
		Pricing: &WorkflowPricing{
			PricingModel: "sum_steps_plus_margin",
			MinBond:      sdk.NewCoin("ulac", sdkmath.NewInt(1000000)),
		},
		Governance:       validGenesisWorkflowGovernance(),
		SafetyInvariants: []*SafetyInvariant{validStaticWorkflowSafetyInvariant()},
	}
}

func validGenesisWorkflowGovernance() *Governance {
	return &Governance{
		AuthorAddresses: []string{"lumera1workflowauthor"},
		UpgradePolicy:   UpgradePolicy_UPGRADE_POLICY_SEMVER_COMPATIBLE,
	}
}
