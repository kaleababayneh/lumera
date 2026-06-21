package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/incentives/types"
)

// GetTxCmd returns the incentives module's transaction commands.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transaction subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdRequestEvaluation())
	return cmd
}

// CmdRequestEvaluation requests an immediate badge evaluation for a tool. The
// signer must be the tool's publisher; scoring uses the tool's recorded metrics.
func CmdRequestEvaluation() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request-evaluation [tool-id]",
		Short: "Request a reputation-badge evaluation for a tool (signer must be the publisher)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := &types.MsgRequestEvaluation{
				Publisher: clientCtx.GetFromAddress().String(),
				ToolId:    args[0],
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
