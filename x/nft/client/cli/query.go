package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/nft/types"
)

// GetQueryCmd returns the nft module's query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s query subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdToolpack())
	return cmd
}

// CmdToolpack queries a minted toolpack NFT (including its curator).
func CmdToolpack() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "toolpack [id]",
		Short: "Query a toolpack NFT",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			qc := types.NewQueryClient(clientCtx)
			res, err := qc.Toolpack(cmd.Context(), &types.QueryToolpackRequest{Id: args[0]})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
