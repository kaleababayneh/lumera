package cli

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

// GetQueryCmd returns the workflows query root command.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Query commands for the Workflows module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(NewQueryParamsCmd(), NewQueryWorkflowCmd(), NewQueryAuthorBondCmd())
	return cmd
}

// NewQueryParamsCmd queries the workflows module parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query the workflows module parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			resp, err := types.NewQueryClient(clientCtx).Params(context.Background(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryWorkflowCmd queries a published workflow by id + version.
func NewQueryWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow [workflow-id] [version]",
		Short: "Query a published workflow by id and version",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			resp, err := types.NewQueryClient(clientCtx).Workflow(context.Background(), &types.QueryWorkflowRequest{
				WorkflowId: args[0],
				Version:    args[1],
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

// NewQueryAuthorBondCmd queries an author's escrowed/slashed bond.
func NewQueryAuthorBondCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "author-bond [author]",
		Short: "Query an author's escrowed/slashed workflow bond",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			resp, err := types.NewQueryClient(clientCtx).AuthorBond(context.Background(), &types.QueryAuthorBondRequest{Author: args[0]})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
