package keeper

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

func TestWorkflows_Publish_ValidSemver(t *testing.T) {
	ctx, k := setupKeeper(t)
	author := workflowTestAuthorAddress()
	msg := publishMsg(author, "wf-storage", "1.0.0", "1000000")

	require.NoError(t, k.PublishWorkflow(ctx, msg))

	workflow, found, err := k.GetWorkflow(ctx, "wf-storage", "1.0.0")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.WorkflowStatusActive, workflow.Status)
	require.Equal(t, author, workflow.AuthorAddress)

	bond, found, err := k.GetAuthorBond(ctx, author)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "1000000", bond.Bond.Amount.String())
	require.Equal(t, []string{"wf-storage/1.0.0"}, bond.LockedFor)

	require.Error(t, k.PublishWorkflow(ctx, publishMsg(author, "wf-storage-bad", "not-semver", "1000000")))
}

func TestMsgServer_RejectsNilKeeper(t *testing.T) {
	ctx, k := setupKeeper(t)
	author := workflowTestAuthorAddress()

	cases := []struct {
		name string
		call func(*msgServer) error
	}{
		{
			name: "publish workflow",
			call: func(server *msgServer) error {
				return server.PublishWorkflow(ctx, publishMsg(author, "wf-msgserver-publish", "1.0.0", "1000000"))
			},
		},
		{
			name: "upgrade workflow",
			call: func(server *msgServer) error {
				return server.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
					Author:       author,
					WorkflowID:   "wf-msgserver-upgrade",
					FromVersion:  "1.0.0",
					WorkflowCard: workflowCard("wf-msgserver-upgrade", "1.1.0"),
				})
			},
		},
		{
			name: "deactivate workflow",
			call: func(server *msgServer) error {
				return server.DeactivateWorkflow(ctx, &types.MsgDeactivateWorkflow{
					Author:     author,
					WorkflowID: "wf-msgserver-deactivate",
					Version:    "1.0.0",
					Reason:     "test",
				})
			},
		},
		{
			name: "withdraw bond",
			call: func(server *msgServer) error {
				return server.WithdrawBond(ctx, &types.MsgWithdrawBond{
					Author: author,
					Amount: coin("ulac", "1"),
				})
			},
		},
		{
			name: "top up author bond",
			call: func(server *msgServer) error {
				return server.TopUpAuthorBond(ctx, &types.MsgTopUpAuthorBond{
					Author: author,
					Amount: coin("ulac", "1"),
				})
			},
		},
		{
			name: "update params",
			call: func(server *msgServer) error {
				return server.UpdateParams(ctx, &types.MsgUpdateParams{
					Authority: k.Authority(),
					Params:    types.DefaultParams(),
				})
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"/zero server", func(t *testing.T) {
			err := tc.call(&msgServer{})
			require.Error(t, err)
			require.Contains(t, err.Error(), "workflows keeper not initialized")
		})
		t.Run(tc.name+"/nil receiver", func(t *testing.T) {
			var server *msgServer
			err := tc.call(server)
			require.Error(t, err)
			require.Contains(t, err.Error(), "workflows keeper not initialized")
		})
	}
}

func TestMsgServer_RejectsInvalidRequestsBeforeNilKeeper(t *testing.T) {
	server := NewMsgServerImpl(nil)
	author := workflowTestAuthorAddress()

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "publish workflow",
			call: func() error {
				return server.PublishWorkflow(context.Background(), publishMsg(" "+author, "wf-invalid-publish", "1.0.0", "1000000"))
			},
			want: "author",
		},
		{
			name: "upgrade workflow",
			call: func() error {
				return server.UpgradeWorkflow(context.Background(), &types.MsgUpgradeWorkflow{
					Author:       author,
					WorkflowID:   " wf-invalid-upgrade",
					FromVersion:  "1.0.0",
					WorkflowCard: workflowCard("wf-invalid-upgrade", "1.1.0"),
				})
			},
			want: "workflow_id",
		},
		{
			name: "deactivate workflow",
			call: func() error {
				return server.DeactivateWorkflow(context.Background(), &types.MsgDeactivateWorkflow{
					Author:     author,
					WorkflowID: "wf-invalid-deactivate",
					Version:    " 1.0.0",
					Reason:     "test",
				})
			},
			want: "version",
		},
		{
			name: "withdraw bond",
			call: func() error {
				return server.WithdrawBond(context.Background(), &types.MsgWithdrawBond{
					Author: author,
					Amount: coin("ulac", "not-an-int"),
				})
			},
			want: "amount",
		},
		{
			name: "top up author bond",
			call: func() error {
				return server.TopUpAuthorBond(context.Background(), &types.MsgTopUpAuthorBond{
					Author: author,
					Amount: coin("ulac", "not-an-int"),
				})
			},
			want: "amount",
		},
		{
			name: "update params",
			call: func() error {
				return server.UpdateParams(context.Background(), &types.MsgUpdateParams{
					Authority: "not-a-bech32-address",
					Params:    types.DefaultParams(),
				})
			},
			want: "authority",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			require.NotContains(t, err.Error(), "workflows keeper not initialized")
		})
	}
}

func TestWorkflows_Publish_UsesToolCardReader(t *testing.T) {
	ctx, k := setupKeeper(t)
	k.SetWorkflowToolCardReader(workflowToolReaderStub{
		tools: map[string]*registrytypes.ToolCard{
			"tool.alpha": workflowRegistryTool("tool.alpha", "1.0.0"),
		},
	})

	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-resolved-tools", "1.0.0", "1000000")))
}

func TestWorkflows_Publish_RejectsUnresolvedToolCards(t *testing.T) {
	cases := []struct {
		name   string
		reader WorkflowToolCardReader
		want   string
	}{
		{
			name:   "missing tool",
			reader: workflowToolReaderStub{},
			want:   types.WorkflowStaticReasonStepToolNotFound,
		},
		{
			name: "version mismatch",
			reader: workflowToolReaderStub{
				tools: map[string]*registrytypes.ToolCard{
					"tool.alpha": workflowRegistryTool("tool.alpha", "2.0.0"),
				},
			},
			want: types.WorkflowStaticReasonStepToolVersionMismatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, k := setupKeeper(t)
			k.SetWorkflowToolCardReader(tc.reader)

			err := k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-unresolved-tools", "1.0.0", "1000000"))
			assertWorkflowStaticReason(t, err, tc.want)
		})
	}
}

func TestWorkflows_Publish_DAGValidatorReasonCodes(t *testing.T) {
	ctx, k := setupKeeper(t)
	require.NoError(t, k.PublishWorkflow(ctx, publishMsgWithCard(workflowTestAuthorAddress(), workflowCardWithSecondStep("wf-publish-dag-valid", "1.0.0"))))

	cases := []struct {
		name   string
		card   *types.WorkflowCard
		mutate func(*types.WorkflowCard)
		want   string
	}{
		{
			name: "cycle",
			card: workflowCardWithSecondStep("wf-publish-dag-cycle", "1.0.0"),
			mutate: func(card *types.WorkflowCard) {
				card.Dag[0].DependsOn = []string{"step-b"}
			},
			want: types.WorkflowStaticReasonDAGCycle,
		},
		{
			name: "version range",
			card: workflowCardWithSecondStep("wf-publish-dag-range", "1.0.0"),
			mutate: func(card *types.WorkflowCard) {
				card.Dag[0].ToolVersionConstraint = ">=1.0.0 <2.0.0"
			},
			want: types.WorkflowStaticReasonStepVersionInvalid,
		},
		{
			name: "unknown dependency",
			card: workflowCardWithSecondStep("wf-publish-dag-unknown", "1.0.0"),
			mutate: func(card *types.WorkflowCard) {
				card.Dag[1].DependsOn = []string{"missing_step"}
			},
			want: types.WorkflowStaticReasonStepDependencyUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.mutate(tc.card)
			err := k.PublishWorkflow(ctx, publishMsgWithCard(workflowTestAuthorAddress(), tc.card))
			assertWorkflowStaticReason(t, err, tc.want)
		})
	}
}

func TestWorkflows_Upgrade_VersionMonotonic(t *testing.T) {
	ctx, k := setupKeeper(t)
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-upgrade", "1.0.0", "1000000")))

	err := k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-upgrade",
		FromVersion:  "1.0.0",
		WorkflowCard: workflowCard("wf-upgrade", "1.1.0"),
	})
	require.NoError(t, err)

	versions, err := k.ListWorkflowVersions(ctx, "wf-upgrade")
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, "1.0.0", versions[0].Version)
	require.Equal(t, "1.1.0", versions[1].Version)

	err = k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-upgrade",
		FromVersion:  "1.1.0",
		WorkflowCard: workflowCard("wf-upgrade", "1.0.1"),
	})
	require.Error(t, err)
}

func TestWorkflows_Upgrade_RejectsRaisedMinBondWithoutTopUp(t *testing.T) {
	ctx, k := setupKeeper(t)
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-upgrade-bond", "1.0.0", "1000000")))

	upgradeCard := workflowCard("wf-upgrade-bond", "1.1.0")
	upgradeCard.Pricing.MinBond = coin("ulac", "2000000")

	err := k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-upgrade-bond",
		FromVersion:  "1.0.0",
		WorkflowCard: upgradeCard,
	})
	require.ErrorContains(t, err, "author bond must be >= 2000000ulac")

	_, found, err := k.GetWorkflow(ctx, "wf-upgrade-bond", "1.1.0")
	require.NoError(t, err)
	require.False(t, found)
}

func TestWorkflows_TopUpAuthorBond_AllowsRaisedMinBondUpgrade(t *testing.T) {
	ctx, k := setupKeeper(t)
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-topup-upgrade", "1.0.0", "1000000")))

	upgradeCard := workflowCard("wf-topup-upgrade", "1.1.0")
	upgradeCard.Pricing.MinBond = coin("ulac", "2000000")

	require.NoError(t, k.TopUpAuthorBond(ctx, &types.MsgTopUpAuthorBond{
		Author: workflowTestAuthorAddress(),
		Amount: coin("ulac", "1000000"),
	}))

	bond, found, err := k.GetAuthorBond(ctx, workflowTestAuthorAddress())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "2000000", bond.Bond.Amount.String())
	require.Equal(t, types.EventTypeAuthorBondToppedUp, ctx.EventManager().Events()[len(ctx.EventManager().Events())-1].Type)

	require.NoError(t, k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-topup-upgrade",
		FromVersion:  "1.0.0",
		WorkflowCard: upgradeCard,
	}))

	_, found, err = k.GetWorkflow(ctx, "wf-topup-upgrade", "1.1.0")
	require.NoError(t, err)
	require.True(t, found)

	bond, found, err = k.GetAuthorBond(ctx, workflowTestAuthorAddress())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []string{"wf-topup-upgrade/1.0.0", "wf-topup-upgrade/1.1.0"}, bond.LockedFor)
}

func TestWorkflows_TopUpAuthorBond_CreatesUnlockedBondAndRejectsDenomMismatch(t *testing.T) {
	ctx, k := setupKeeper(t)

	require.NoError(t, k.TopUpAuthorBond(ctx, &types.MsgTopUpAuthorBond{
		Author: workflowTestPrefundAuthorAddress(),
		Amount: coin("ulac", "123"),
	}))
	bond, found, err := k.GetAuthorBond(ctx, workflowTestPrefundAuthorAddress())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "123", bond.Bond.Amount.String())
	require.Equal(t, "ulac", bond.Bond.Denom)
	require.Empty(t, bond.LockedFor)

	err = k.TopUpAuthorBond(ctx, &types.MsgTopUpAuthorBond{
		Author: workflowTestPrefundAuthorAddress(),
		Amount: coin("uatom", "1"),
	})
	require.ErrorContains(t, err, "author bond denom ulac does not match top-up denom uatom")
}

func TestWorkflows_Upgrade_RejectsUnresolvedToolCardVersion(t *testing.T) {
	ctx, k := setupKeeper(t)
	k.SetWorkflowToolCardReader(workflowToolReaderStub{
		tools: map[string]*registrytypes.ToolCard{
			"tool.alpha": workflowRegistryTool("tool.alpha", "1.0.0"),
		},
	})
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-upgrade-tools", "1.0.0", "1000000")))

	upgrade := &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-upgrade-tools",
		FromVersion:  "1.0.0",
		WorkflowCard: workflowCard("wf-upgrade-tools", "1.1.0"),
	}
	upgrade.WorkflowCard.Dag[0].ToolVersionConstraint = "2.0.0"

	err := k.UpgradeWorkflow(ctx, upgrade)
	assertWorkflowStaticReason(t, err, types.WorkflowStaticReasonStepToolVersionMismatch)
}

func TestWorkflows_InFlightQuotesAcrossVersionBump(t *testing.T) {
	ctx, k := setupKeeper(t)
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-quotes", "1.0.0", "1000000")))
	require.NoError(t, k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-quotes",
		FromVersion:  "1.0.0",
		WorkflowCard: workflowCard("wf-quotes", "2.0.0"),
	}))

	oldVersion, found, err := k.GetWorkflow(ctx, "wf-quotes", "1.0.0")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.WorkflowStatusActive, oldVersion.Status)

	newVersion, found, err := k.GetWorkflow(ctx, "wf-quotes", "2.0.0")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.WorkflowStatusActive, newVersion.Status)
}

func TestWorkflows_BondSlash_OnDispute(t *testing.T) {
	ctx, k := setupKeeper(t)
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-dispute", "1.0.0", "1000000")))

	require.NoError(t, k.SlashWorkflowAuthorBond(ctx, "wf-dispute", "1.0.0", coinPtr("ulac", "250000"), "dispute"))

	bond, found, err := k.GetAuthorBond(ctx, workflowTestAuthorAddress())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "750000", bond.Bond.Amount.String())
	require.Equal(t, "250000", bond.Slashed.Amount.String())

	events := ctx.EventManager().Events()
	require.Equal(t, types.EventTypeAuthorBondSlashed, events[len(events)-1].Type)
}

func TestPropWorkflows_BondArithmetic(t *testing.T) {
	ctx, k := setupKeeper(t)
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-arithmetic", "1.0.0", "1000000")))

	slashed := int64(0)
	for i, amount := range []int64{1, 7, 49, 343, 2401, 16807} {
		version := "1.0.0"
		require.NoError(t, k.SlashWorkflowAuthorBond(ctx, "wf-arithmetic", version, coinPtr("ulac", intString(amount)), "prop"))
		slashed += amount
		bond, found, err := k.GetAuthorBond(ctx, workflowTestAuthorAddress())
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, intString(1_000_000-slashed), bond.Bond.Amount.String(), "iteration %d", i)
		require.Equal(t, intString(slashed), bond.Slashed.Amount.String(), "iteration %d", i)
	}
}

func TestWorkflows_DeterministicStateHashAcrossKeepers(t *testing.T) {
	ctxA, keeperA := setupKeeper(t)
	ctxB, keeperB := setupKeeper(t)

	applyLifecycleStream(t, ctxA, keeperA)
	applyLifecycleStream(t, ctxB, keeperB)

	hashA := exportedGenesisJSON(t, ctxA, keeperA)
	hashB := exportedGenesisJSON(t, ctxB, keeperB)
	require.Equal(t, hashA, hashB)
}

func TestWorkflows_DeactivateUnlocksAndWithdrawsBond(t *testing.T) {
	ctx, k := setupKeeper(t)
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-withdraw", "1.0.0", "1000000")))
	require.Error(t, k.WithdrawBond(ctx, &types.MsgWithdrawBond{Author: workflowTestAuthorAddress(), Amount: coin("ulac", "1")}))

	require.NoError(t, k.DeactivateWorkflow(ctx, &types.MsgDeactivateWorkflow{
		Author:     workflowTestAuthorAddress(),
		WorkflowID: "wf-withdraw",
		Version:    "1.0.0",
		Reason:     "author_request",
	}))
	require.NoError(t, k.WithdrawBond(ctx, &types.MsgWithdrawBond{Author: workflowTestAuthorAddress(), Amount: coin("ulac", "1000000")}))

	_, found, err := k.GetAuthorBond(ctx, workflowTestAuthorAddress())
	require.NoError(t, err)
	require.False(t, found)
}

func FuzzWorkflows_StateTransitions(f *testing.F) {
	f.Add(uint8(0), uint8(0), int64(1_000_000))
	f.Add(uint8(1), uint8(2), int64(2_000_000))
	f.Add(uint8(2), uint8(4), int64(1_500_000))
	f.Fuzz(func(t *testing.T, first uint8, second uint8, bondAmount int64) {
		if bondAmount < 0 {
			bondAmount = -bondAmount
		}
		bondAmount = 1_000_000 + bondAmount%3_000_000
		ctx, k := setupKeeper(t)
		ops := []uint8{first % 4, second % 4}
		published := false
		upgraded := false
		for _, op := range ops {
			switch op {
			case 0:
				err := k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-fuzz", "1.0.0", intString(bondAmount)))
				if err == nil {
					published = true
				}
			case 1:
				if published {
					err := k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
						Author:       workflowTestAuthorAddress(),
						WorkflowID:   "wf-fuzz",
						FromVersion:  "1.0.0",
						WorkflowCard: workflowCard("wf-fuzz", "1.1.0"),
					})
					if err == nil {
						upgraded = true
					}
				}
			case 2:
				if published {
					_ = k.DeactivateWorkflow(ctx, &types.MsgDeactivateWorkflow{Author: workflowTestAuthorAddress(), WorkflowID: "wf-fuzz", Version: "1.0.0"})
				}
			case 3:
				if upgraded {
					_ = k.SlashWorkflowAuthorBond(ctx, "wf-fuzz", "1.1.0", coinPtr("ulac", "1"), "fuzz")
				}
			}
		}
		gs, err := k.ExportGenesis(ctx)
		require.NoError(t, err)
		require.NoError(t, gs.Validate())
	})
}

func applyLifecycleStream(t *testing.T, ctx sdk.Context, k *Keeper) {
	t.Helper()
	require.NoError(t, k.PublishWorkflow(ctx, publishMsg(workflowTestAuthorAddress(), "wf-deterministic", "1.0.0", "1000000")))
	require.NoError(t, k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-deterministic",
		FromVersion:  "1.0.0",
		WorkflowCard: workflowCard("wf-deterministic", "1.1.0"),
	}))
	require.NoError(t, k.SlashWorkflowAuthorBond(ctx, "wf-deterministic", "1.1.0", coinPtr("ulac", "42"), "deterministic"))
}

func exportedGenesisJSON(t *testing.T, ctx sdk.Context, k *Keeper) string {
	t.Helper()
	gs, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	raw, err := json.Marshal(gs)
	require.NoError(t, err)
	return string(raw)
}

func publishMsg(author, workflowID, version, amount string) *types.MsgPublishWorkflow {
	return &types.MsgPublishWorkflow{
		Author:       author,
		WorkflowCard: workflowCard(workflowID, version),
		Bond:         coin("ulac", amount),
	}
}

func publishMsgWithCard(author string, card *types.WorkflowCard) *types.MsgPublishWorkflow {
	return &types.MsgPublishWorkflow{
		Author:       author,
		WorkflowCard: card,
		Bond:         coin("ulac", "1000000"),
	}
}

func workflowCard(workflowID, version string) *types.WorkflowCard {
	return &types.WorkflowCard{
		WorkflowId:   workflowID,
		Version:      version,
		DisplayName:  "Workflow " + workflowID,
		AuthorId:     "author-1",
		AuthorPubkey: workflowAuthorPubkey(),
		Categories:   []string{"agent-contracts"},
		LicenseLane:  "byo_key",
		Dag: []*types.Step{
			{
				StepId:                "step-a",
				ToolId:                "tool.alpha",
				ToolVersionConstraint: "1.0.0",
				InputBinding:          "$.inputs",
				MaxSubCost:            coin("ulac", "1"),
				SubSloP95Ms:           1000,
				RetryPolicy:           &types.RetryPolicy{MaxAttempts: 1},
				FailureAction:         types.FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
				SideEffect:            types.SideEffect_SIDE_EFFECT_REVERSIBLE,
			},
		},
		InputSchema:  `{"type":"object"}`,
		OutputSchema: `{"type":"object"}`,
		Pricing: &types.WorkflowPricing{
			PricingModel: "sum_steps_plus_margin",
			MinBond:      coin("ulac", "1000000"),
		},
		PassportRequirements: &types.PassportRequirements{
			MinTier: types.PassportTier_PASSPORT_TIER_BASIC,
		},
		Governance:       workflowGovernance(),
		SafetyInvariants: []*types.SafetyInvariant{workflowTestSafetyInvariant()},
	}
}

func workflowGovernance() *types.Governance {
	return &types.Governance{
		AuthorAddresses: []string{workflowTestGovernanceAuthorAddress()},
		UpgradePolicy:   types.UpgradePolicy_UPGRADE_POLICY_SEMVER_COMPATIBLE,
	}
}

func workflowAuthorPubkey() string {
	return "ed448:" + strings.Repeat("a", 114)
}

func workflowTestSafetyInvariant() *types.SafetyInvariant {
	return &types.SafetyInvariant{
		InvariantId: "total_cost_bound",
		Expression:  "total_cost <= max_cost",
		Phase:       types.InvariantPhase_INVARIANT_PHASE_LOCK,
		Severity:    "error",
		ErrorCode:   "workflow_cost_exceeded",
		HintMessage: "Keep the locked workflow cost within the signed quote budget.",
	}
}

func workflowCardWithSecondStep(workflowID, version string) *types.WorkflowCard {
	card := workflowCard(workflowID, version)
	card.Dag = append(card.Dag, &types.Step{
		StepId:                "step-b",
		ToolId:                "tool.beta",
		ToolVersionConstraint: "1.0.0",
		InputBinding:          "$.inputs",
		MaxSubCost:            coin("ulac", "1"),
		SubSloP95Ms:           1000,
		RetryPolicy:           &types.RetryPolicy{MaxAttempts: 1},
		FailureAction:         types.FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
		SideEffect:            types.SideEffect_SIDE_EFFECT_REVERSIBLE,
		DependsOn:             []string{"step-a"},
	})
	return card
}

type workflowToolReaderStub struct {
	tools map[string]*registrytypes.ToolCard
}

func (s workflowToolReaderStub) GetToolCard(_ sdk.Context, toolID string) (*registrytypes.ToolCard, bool) {
	tool, found := s.tools[toolID]
	return tool, found
}

func workflowRegistryTool(toolID, version string) *registrytypes.ToolCard {
	return &registrytypes.ToolCard{
		ToolId:       toolID,
		Version:      version,
		InputSchema:  `{"type":"object"}`,
		OutputSchema: `{"type":"object"}`,
	}
}

func assertWorkflowStaticReason(t *testing.T, err error, reason string) {
	t.Helper()
	require.Error(t, err)
	var staticErr *types.WorkflowCardStaticValidationError
	require.ErrorAs(t, err, &staticErr)
	for _, finding := range staticErr.Findings {
		if finding.ReasonCode == reason {
			return
		}
	}
	t.Fatalf("missing workflow static reason %q in %+v", reason, staticErr.Findings)
}

// coin builds a value sdk.Coin from a denom and decimal amount string. The denom
// is stored verbatim (so canonicalization tests can pass padded denoms). A
// non-integer amount falls back to zero, which downstream validation rejects.
func coin(denom, amount string) sdk.Coin {
	amt, ok := sdkmath.NewIntFromString(amount)
	if !ok {
		amt = sdkmath.ZeroInt()
	}
	return sdk.Coin{Denom: denom, Amount: amt}
}

// coinPtr is the pointer form of coin, used where a *sdk.Coin is required (e.g.
// SlashWorkflowAuthorBond).
func coinPtr(denom, amount string) *sdk.Coin {
	c := coin(denom, amount)
	return &c
}

func intString(value int64) string {
	if value == 0 {
		return "0"
	}
	out := make([]byte, 0, 20)
	for value > 0 {
		out = append(out, byte('0'+value%10))
		value /= 10
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}
