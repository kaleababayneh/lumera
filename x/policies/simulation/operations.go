
package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/LumeraProtocol/lumera/x/policies/keeper"
	"github.com/LumeraProtocol/lumera/x/policies/types"
)

//#nosec G101 -- these are simulation operation weight keys, not credentials
const (
	opWeightCreatePolicy    = "op_weight_policies_create"
	opWeightActivatePolicy  = "op_weight_policies_activate"
	opWeightDeprecatePolicy = "op_weight_policies_deprecate"

	defaultWeightCreate    = 25
	defaultWeightActivate  = 20
	defaultWeightDeprecate = 10
)

// WeightedOperations wires policies simulation scenarios into the cosmos simulator.
func WeightedOperations(
	appParams simtypes.AppParams,
	cdc codec.JSONCodec,
	k keeper.Keeper,
) simulation.WeightedOperations {
	_ = cdc

	var (
		weightCreate    int
		weightActivate  int
		weightDeprecate int
	)

	appParams.GetOrGenerate(opWeightCreatePolicy, &weightCreate, nil,
		func(_ *rand.Rand) { weightCreate = defaultWeightCreate })
	appParams.GetOrGenerate(opWeightActivatePolicy, &weightActivate, nil,
		func(_ *rand.Rand) { weightActivate = defaultWeightActivate })
	appParams.GetOrGenerate(opWeightDeprecatePolicy, &weightDeprecate, nil,
		func(_ *rand.Rand) { weightDeprecate = defaultWeightDeprecate })

	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightCreate, simulateCreatePolicy(&k)),
		simulation.NewWeightedOperation(weightActivate, simulateActivatePolicy(&k)),
		simulation.NewWeightedOperation(weightDeprecate, simulateDeprecatePolicy(&k)),
	}
}

// simulateCreatePolicy creates a new policy with randomized parameters.
func simulateCreatePolicy(k *keeper.Keeper) simtypes.Operation {
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) < 1 {
			return simtypes.NoOpMsg(types.ModuleName, "create_policy", "need at least 1 account"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)
		creator, _ := simtypes.RandomAcc(r, accs)

		policyID := fmt.Sprintf("sim-policy-%d-%d", ctx.BlockHeight(), r.Int63())

		tags := []string{"defi", "ai", "analytics", "oracle", "nft"}
		categories := []string{"compute", "storage", "inference", "validation"}

		msg := &types.MsgCreatePolicy{
			Creator: creator.Address.String(),
			Policy: &types.PolicyProfile{
				PolicyId:      policyID,
				Version:       fmt.Sprintf("v1.%d.0", r.Intn(10)),
				SchemaVersion: "1.0",
				Metadata: &types.PolicyMetadata{
					Name:        fmt.Sprintf("Sim Policy %d", r.Intn(1000)),
					Owner:       creator.Address.String(),
					Description: "Simulation-generated policy for testing",
					Tags:        []string{tags[r.Intn(len(tags))]},
				},
				Lifecycle: &types.PolicyLifecycle{
					State:     types.PolicyState_POLICY_STATE_DRAFT,
					CreatedBy: creator.Address.String(),
				},
				ToolFilters: &types.ToolFilters{
					AllowedCategories: []string{categories[r.Intn(len(categories))]},
				},
			},
		}

		resp, err := msgServer.CreatePolicy(ctx, msg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "create_policy", fmt.Sprintf("failed: %v", err)), nil, nil
		}

		comment := fmt.Sprintf("created policy=%s version=%s", resp.PolicyId, resp.Version)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "create_policy", comment, true, nil), nil, nil
	}
}

// simulateActivatePolicy creates a policy and immediately activates it.
func simulateActivatePolicy(k *keeper.Keeper) simtypes.Operation {
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) < 1 {
			return simtypes.NoOpMsg(types.ModuleName, "activate_policy", "need at least 1 account"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)
		authority := k.GetAuthority()
		creator, _ := simtypes.RandomAcc(r, accs)

		policyID := fmt.Sprintf("sim-activate-%d-%d", ctx.BlockHeight(), r.Int63())

		// Create the policy first
		createMsg := &types.MsgCreatePolicy{
			Creator: creator.Address.String(),
			Policy: &types.PolicyProfile{
				PolicyId:      policyID,
				Version:       "v1.0.0",
				SchemaVersion: "1.0",
				Metadata: &types.PolicyMetadata{
					Name:        fmt.Sprintf("Activatable Policy %d", r.Intn(1000)),
					Owner:       creator.Address.String(),
					Description: "Policy created for activation simulation",
				},
				Lifecycle: &types.PolicyLifecycle{
					State:     types.PolicyState_POLICY_STATE_DRAFT,
					CreatedBy: creator.Address.String(),
				},
			},
		}

		if _, err := msgServer.CreatePolicy(ctx, createMsg); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "activate_policy", fmt.Sprintf("create failed: %v", err)), nil, nil
		}

		// Activate it
		activateMsg := &types.MsgActivatePolicy{
			Authority: authority,
			PolicyId:  policyID,
		}

		_, err := msgServer.ActivatePolicy(ctx, activateMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "activate_policy", fmt.Sprintf("activate failed: %v", err)), nil, nil
		}

		comment := fmt.Sprintf("activated policy=%s", policyID)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "activate_policy", comment, true, nil), nil, nil
	}
}

// simulateDeprecatePolicy creates, activates, and then deprecates a policy.
func simulateDeprecatePolicy(k *keeper.Keeper) simtypes.Operation {
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) < 1 {
			return simtypes.NoOpMsg(types.ModuleName, "deprecate_policy", "need at least 1 account"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)
		authority := k.GetAuthority()
		creator, _ := simtypes.RandomAcc(r, accs)

		policyID := fmt.Sprintf("sim-deprecate-%d-%d", ctx.BlockHeight(), r.Int63())

		// Create the policy
		createMsg := &types.MsgCreatePolicy{
			Creator: creator.Address.String(),
			Policy: &types.PolicyProfile{
				PolicyId:      policyID,
				Version:       "v1.0.0",
				SchemaVersion: "1.0",
				Metadata: &types.PolicyMetadata{
					Name:        fmt.Sprintf("Deprecatable Policy %d", r.Intn(1000)),
					Owner:       creator.Address.String(),
					Description: "Policy created for deprecation simulation",
				},
				Lifecycle: &types.PolicyLifecycle{
					State:     types.PolicyState_POLICY_STATE_DRAFT,
					CreatedBy: creator.Address.String(),
				},
			},
		}

		if _, err := msgServer.CreatePolicy(ctx, createMsg); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "deprecate_policy", fmt.Sprintf("create failed: %v", err)), nil, nil
		}

		// Activate it first
		if _, err := msgServer.ActivatePolicy(ctx, &types.MsgActivatePolicy{
			Authority: authority,
			PolicyId:  policyID,
		}); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "deprecate_policy", fmt.Sprintf("activate failed: %v", err)), nil, nil
		}

		// Then deprecate
		deprecateMsg := &types.MsgDeprecatePolicy{
			Authority:              authority,
			PolicyId:               policyID,
			MigrationWindowSeconds: uint32(3600 + r.Intn(86400)),
		}

		_, err := msgServer.DeprecatePolicy(ctx, deprecateMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "deprecate_policy", fmt.Sprintf("deprecate failed: %v", err)), nil, nil
		}

		comment := fmt.Sprintf("deprecated policy=%s migration_window=%ds", policyID, deprecateMsg.MigrationWindowSeconds)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "deprecate_policy", comment, true, nil), nil, nil
	}
}
