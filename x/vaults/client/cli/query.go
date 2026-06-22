// Package cli provides CLI query helpers for the vaults module.
package cli

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/vaults/types"
)

// GetQueryCmd bundles all vault query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the vaults module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQueryVaultCmd())
	cmd.AddCommand(NewQueryVaultsCmd())
	return cmd
}

// NewQueryVaultCmd retrieves a specific vault by id.
func NewQueryVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault [id]",
		Short: "Query a vault by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			id := args[0]
			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Vault(context.Background(), &types.QueryVaultRequest{Id: id})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryVaultsCmd lists vaults optionally filtered by owner address.
func NewQueryVaultsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vaults",
		Short: "List vaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			owner, err := cmd.Flags().GetString(flagOwner)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Vaults(context.Background(), &types.QueryVaultsRequest{Owner: owner})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String(flagOwner, "", "Filter vaults by owner address")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

const flagOwner = "owner"
