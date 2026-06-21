package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// GetQueryCmd returns the registry module's query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s query subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdGetTool())
	cmd.AddCommand(CmdGetBond())
	cmd.AddCommand(CmdGetReceipt())
	return cmd
}

// CmdGetReceipt queries a Proof-of-Service usage receipt by id.
func CmdGetReceipt() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-receipt [receipt-id]",
		Short: "Query a Proof-of-Service inference receipt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			qc := types.NewQueryClient(clientCtx)
			res, err := qc.GetReceipt(cmd.Context(), &types.QueryGetReceiptRequest{ReceiptId: args[0]})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// CmdGetBond queries the bond record escrowed for a tool by its publisher.
func CmdGetBond() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-bond [tool-id]",
		Short: "Query the publisher bond escrowed for a tool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			qc := types.NewQueryClient(clientCtx)
			res, err := qc.GetBond(cmd.Context(), &types.QueryGetBondRequest{ToolId: args[0]})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// CmdGetTool queries a registered tool card (including its owner/publisher).
func CmdGetTool() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-tool [tool-id]",
		Short: "Query a registered tool card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			qc := types.NewQueryClient(clientCtx)
			res, err := qc.GetTool(cmd.Context(), &types.QueryGetToolRequest{ToolId: args[0]})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
