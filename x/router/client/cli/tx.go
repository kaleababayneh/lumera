package cli

import (
	"strconv"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// GetTxCmd returns the root tx command for the router module. Only the
// tool-owner-signable activation record is exposed on the CLI; the authority-
// gated invocation/cache-hit/aggregation records are driven by the off-chain
// router over gRPC.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Transaction commands for the Router module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(NewRecordActivationCmd())
	return cmd
}

// NewRecordActivationCmd records a tool activation or deactivation in a session.
// The signer (--from) is recorded as the authority; the keeper accepts either
// the module authority or the tool's registered owner.
func NewRecordActivationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record-activation [tool-id] [activated true|false]",
		Short: "Record a tool activation/deactivation in a routing session (tool owner or module authority)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			activated, err := strconv.ParseBool(args[1])
			if err != nil {
				return err
			}
			session, _ := cmd.Flags().GetString("session-id")
			reason, _ := cmd.Flags().GetString("reason")

			msg := &types.MsgRecordActivation{
				Authority: clientCtx.GetFromAddress().String(),
				ToolId:    args[0],
				SessionId: session,
				Activated: activated,
				Reason:    reason,
				Timestamp: time.Now().UTC(),
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String("session-id", "", "routing session id")
	cmd.Flags().String("reason", "", "reason for the activation/deactivation")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
