package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/nft/types"
)

// GetTxCmd returns the nft module's transaction commands.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transaction subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(CmdMintToolpack())
	return cmd
}

// CmdMintToolpack mints a Toolpack NFT owned by the signing curator.
func CmdMintToolpack() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mint-toolpack [id] [policy-version]",
		Short: "Mint a toolpack NFT (signer becomes the curator)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := &types.MsgMintToolpack{
				Curator:       clientCtx.GetFromAddress().String(),
				Id:            args[0],
				PolicyVersion: args[1],
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
