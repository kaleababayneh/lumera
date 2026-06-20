
// Package cli provides CLI query commands for the policies module.
package cli

import (
	"context"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

const (
	flagOwner = "owner"
	flagState = "state"
)

// GetQueryCmd bundles all policies query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the policies module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQueryPolicyCmd())
	cmd.AddCommand(NewQueryPoliciesCmd())
	cmd.AddCommand(NewQueryParamsCmd())

	return cmd
}

// NewQueryPolicyCmd queries a single policy by id and optional version.
func NewQueryPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy [policy-id] [version]",
		Short: "Query a policy by id and optional version",
		Long: `Query a policy by its identifier. If version is omitted, returns the latest version.

Examples:
  lumeraai query policies policy my-policy-id
  lumeraai query policies policy my-policy-id 1.0.0`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			policyID := strings.TrimSpace(args[0])
			version := ""
			if len(args) > 1 {
				version = strings.TrimSpace(args[1])
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Policy(context.Background(), &types.QueryPolicyRequest{
				PolicyId: policyID,
				Version:  version,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryPoliciesCmd queries a paginated list of policies with optional filters.
func NewQueryPoliciesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policies",
		Short: "List policies with optional filters",
		Long: `List all policies with optional filtering by owner and state.

Examples:
  lumeraai query policies policies
  lumeraai query policies policies --owner lumera1abc...
  lumeraai query policies policies --state POLICY_STATE_ACTIVE
  lumeraai query policies policies --owner lumera1abc... --state POLICY_STATE_DRAFT`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			owner, err := cmd.Flags().GetString(flagOwner)
			if err != nil {
				return err
			}

			stateStr, err := cmd.Flags().GetString(flagState)
			if err != nil {
				return err
			}

			var state types.PolicyState
			if stateStr != "" {
				stateInt, ok := types.PolicyState_value[stateStr]
				if !ok {
					// Try with prefix
					stateInt, ok = types.PolicyState_value["POLICY_STATE_"+strings.ToUpper(stateStr)]
				}
				if ok {
					state = types.PolicyState(stateInt)
				}
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Policies(context.Background(), &types.QueryPoliciesRequest{
				Owner:      strings.TrimSpace(owner),
				State:      state,
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String(flagOwner, "", "Filter by policy owner address")
	cmd.Flags().String(flagState, "", "Filter by policy state (DRAFT, ACTIVE, DEPRECATED, ARCHIVED)")
	flags.AddPaginationFlagsToCmd(cmd, "policies")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryParamsCmd queries module parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query policies module parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Params(context.Background(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
