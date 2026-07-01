package cli

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/passport/types"
)

// GetTxCmd returns the root tx command for the passport module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Passport agent-identity transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewRegisterPassportCmd())
	cmd.AddCommand(NewReactivatePassportCmd())
	cmd.AddCommand(NewTopUpStakeCmd())
	cmd.AddCommand(NewUnregisterPassportCmd())

	return cmd
}

// NewRegisterPassportCmd builds a register-passport transaction command.
func NewRegisterPassportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register [agent-pubkey] [stake-amount]",
		Short: "Register a new agent passport with a stake",
		Long:  "Register a new agent passport. stake-amount is a Cosmos coin string, e.g. 1000ulumera.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			coin, err := sdk.ParseCoinNormalized(args[1])
			if err != nil {
				return err
			}

			msg := &types.MsgRegisterPassport{
				Creator:     clientCtx.GetFromAddress().String(),
				AgentPubkey: args[0],
				Stake:       coin,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewTopUpStakeCmd builds a top-up-stake transaction command.
func NewTopUpStakeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top-up [passport-id] [stake-amount]",
		Short: "Add stake to an existing passport",
		Long:  "Add stake to an existing passport. stake-amount is a Cosmos coin string, e.g. 1000ulumera.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			coin, err := sdk.ParseCoinNormalized(args[1])
			if err != nil {
				return err
			}

			msg := types.NewMsgTopUpStake(clientCtx.GetFromAddress().String(), args[0], coin)
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUnregisterPassportCmd builds an unregister-passport transaction command.
func NewUnregisterPassportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unregister [passport-id]",
		Short: "Unregister a passport and withdraw remaining stake",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgUnregisterPassport(clientCtx.GetFromAddress().String(), args[0])
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewReactivatePassportCmd builds a reactivate-passport transaction command.
func NewReactivatePassportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reactivate [passport-id]",
		Short: "Reactivate a suspended passport",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgReactivatePassport{
				Owner:      clientCtx.GetFromAddress().String(),
				PassportId: args[0],
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
