package cli

import (
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// GetTxCmd returns the registry module's transaction commands.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transaction subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdRegisterTool())
	cmd.AddCommand(CmdCreateBond())
	cmd.AddCommand(CmdWithdrawBond())
	return cmd
}

// CmdRegisterTool registers a ToolCard; the signer becomes the tool's
// owner/publisher of record (used by credits settlement to route publisher pay)
// and escrows the publisher bond. Pass --bond to post more than the minimum.
func CmdRegisterTool() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-tool [tool-id]",
		Short: "Register a tool card and escrow the publisher bond (signer becomes the owner)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			bondStr, err := cmd.Flags().GetString("bond")
			if err != nil {
				return err
			}
			var bond sdk.Coins
			if strings.TrimSpace(bondStr) != "" {
				bond, err = sdk.ParseCoinsNormalized(bondStr)
				if err != nil {
					return fmt.Errorf("invalid --bond: %w", err)
				}
			}
			msg := &types.MsgRegisterTool{
				Owner:    clientCtx.GetFromAddress().String(),
				ToolCard: &types.ToolCard{ToolId: args[0]},
				Bond:     bond,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String("bond", "", "bond to escrow (e.g. 2000000ulume); defaults to the params minimum")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// CmdCreateBond posts (or tops up) a tool's publisher bond.
func CmdCreateBond() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-bond [tool-id] [amount]",
		Short: "Escrow (or top up) a tool's publisher bond",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			amount, err := sdk.ParseCoinsNormalized(args[1])
			if err != nil {
				return fmt.Errorf("invalid amount: %w", err)
			}
			msg := &types.MsgCreateBond{
				Owner:  clientCtx.GetFromAddress().String(),
				ToolId: args[0],
				Amount: amount,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// CmdWithdrawBond reclaims part (or all) of a tool's publisher bond. While a
// tool is registered with a non-zero minimum, only the excess above the minimum
// can be reclaimed.
func CmdWithdrawBond() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw-bond [tool-id] [amount]",
		Short: "Withdraw a tool's publisher bond (only the excess above the minimum while registered)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			amount, err := sdk.ParseCoinsNormalized(args[1])
			if err != nil {
				return fmt.Errorf("invalid amount: %w", err)
			}
			msg := &types.MsgWithdrawBond{
				Owner:  clientCtx.GetFromAddress().String(),
				ToolId: args[0],
				Amount: amount,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
