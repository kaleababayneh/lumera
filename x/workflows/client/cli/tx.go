// Package cli defines Cobra commands for the workflows module.
package cli

import (
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

const flagBond = "bond"

// GetTxCmd returns the workflows tx root command.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Transaction commands for the Workflows (Composable Intelligence) module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		NewPublishWorkflowCmd(),
		NewUpgradeWorkflowCmd(),
		NewDeactivateWorkflowCmd(),
		NewTopUpBondCmd(),
		NewWithdrawBondCmd(),
	)
	return cmd
}

// readWorkflowCard parses a WorkflowCard from a proto-JSON file.
func readWorkflowCard(clientCtx client.Context, path string) (*types.WorkflowCard, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	card := &types.WorkflowCard{}
	if err := clientCtx.Codec.UnmarshalJSON(raw, card); err != nil {
		return nil, err
	}
	return card, nil
}

// NewPublishWorkflowCmd publishes a workflow card and escrows the author bond.
func NewPublishWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish-workflow [card-json-file]",
		Short: "Publish a workflow card (JSON) and escrow the author bond",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			card, err := readWorkflowCard(clientCtx, args[0])
			if err != nil {
				return err
			}
			bondStr, _ := cmd.Flags().GetString(flagBond)
			bond, err := sdk.ParseCoinNormalized(bondStr)
			if err != nil {
				return err
			}
			msg := &types.MsgPublishWorkflow{
				Author:       clientCtx.GetFromAddress().String(),
				WorkflowCard: card,
				Bond:         bond,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagBond, "", "Author bond coin to escrow, e.g. 1000000ulac (required)")
	_ = cmd.MarkFlagRequired(flagBond)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUpgradeWorkflowCmd publishes a new version of an existing workflow.
func NewUpgradeWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade-workflow [workflow-id] [from-version] [card-json-file]",
		Short: "Publish a new version of an existing workflow",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			card, err := readWorkflowCard(clientCtx, args[2])
			if err != nil {
				return err
			}
			msg := &types.MsgUpgradeWorkflow{
				Author:       clientCtx.GetFromAddress().String(),
				WorkflowID:   args[0],
				FromVersion:  args[1],
				WorkflowCard: card,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewDeactivateWorkflowCmd deactivates a workflow version.
func NewDeactivateWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deactivate-workflow [workflow-id] [version]",
		Short: "Deactivate a workflow version",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			reason, _ := cmd.Flags().GetString("reason")
			msg := &types.MsgDeactivateWorkflow{
				Author:     clientCtx.GetFromAddress().String(),
				WorkflowID: args[0],
				Version:    args[1],
				Reason:     reason,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String("reason", "", "reason for deactivation")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewTopUpBondCmd increases the author's escrowed bond.
func NewTopUpBondCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top-up-bond [amount]",
		Short: "Increase the author's escrowed workflow bond",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			amount, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}
			msg := &types.MsgTopUpAuthorBond{Author: clientCtx.GetFromAddress().String(), Amount: amount}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewWithdrawBondCmd reclaims the excess author bond above the minimum.
func NewWithdrawBondCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw-bond [amount]",
		Short: "Reclaim excess author bond above the minimum",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			amount, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}
			msg := &types.MsgWithdrawBond{Author: clientCtx.GetFromAddress().String(), Amount: amount}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
