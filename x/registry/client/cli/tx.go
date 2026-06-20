package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
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
	return cmd
}

// CmdRegisterTool registers a ToolCard; the signer becomes the tool's
// owner/publisher of record (used by credits settlement to route publisher pay).
func CmdRegisterTool() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-tool [tool-id]",
		Short: "Register a tool card (signer becomes the tool publisher/owner)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := &types.MsgRegisterTool{
				Owner:    clientCtx.GetFromAddress().String(),
				ToolCard: &types.ToolCard{ToolId: args[0]},
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
