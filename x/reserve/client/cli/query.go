
// Package cli provides CLI query commands for the reserve module.
package cli

import (
	"context"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/reserve/types"
)

// GetQueryCmd bundles all reserve query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the reserve module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQueryCommitmentCmd())
	cmd.AddCommand(NewQueryCommitmentsByOwnerCmd())
	cmd.AddCommand(NewQueryCommitmentsByPolicyCmd())
	cmd.AddCommand(NewQueryActiveCommitmentCmd())
	cmd.AddCommand(NewQueryParamsCmd())

	return cmd
}

// NewQueryCommitmentCmd retrieves one reserve commitment by id.
func NewQueryCommitmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commitment [commitment-id]",
		Short: "Query a reserve commitment by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Commitment(context.Background(), &types.QueryCommitmentRequest{
				CommitmentId: strings.TrimSpace(args[0]),
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

// NewQueryCommitmentsByOwnerCmd lists reserve commitments owned by one account.
func NewQueryCommitmentsByOwnerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commitments-by-owner [owner]",
		Short: "List reserve commitments by owner",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.CommitmentsByOwner(context.Background(), &types.QueryCommitmentsByOwnerRequest{
				Owner:      strings.TrimSpace(args[0]),
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, "commitments")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryCommitmentsByPolicyCmd lists reserve commitments for one policy.
func NewQueryCommitmentsByPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commitments-by-policy [policy-id]",
		Short: "List reserve commitments by policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.CommitmentsByPolicy(context.Background(), &types.QueryCommitmentsByPolicyRequest{
				PolicyId:   strings.TrimSpace(args[0]),
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, "commitments")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryActiveCommitmentCmd checks whether a policy has an active reserve.
func NewQueryActiveCommitmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "active-commitment [policy-id]",
		Short: "Check active reserve commitment for a policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			toolID, err := cmd.Flags().GetString(flagToolID)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.ActiveCommitment(context.Background(), &types.QueryActiveCommitmentRequest{
				PolicyId: strings.TrimSpace(args[0]),
				ToolId:   strings.TrimSpace(toolID),
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String(flagToolID, "", "Optional tool identifier; empty checks wildcard reserve semantics")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryParamsCmd retrieves reserve module parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query reserve module parameters",
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
