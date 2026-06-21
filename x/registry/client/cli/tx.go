package cli

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
	"lukechampine.com/blake3"

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
	cmd.AddCommand(CmdSubmitReceipt())
	cmd.AddCommand(CmdChallengeReceipt())
	cmd.AddCommand(CmdResolveDispute())
	return cmd
}

// CmdChallengeReceipt opens a dispute against a Proof-of-Service receipt,
// escrowing the challenger's stake and locking an equal slice of the bond.
func CmdChallengeReceipt() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "challenge-receipt [receipt-id] [stake]",
		Short: "Dispute a receipt within its window (escrows a stake, locks the publisher bond)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			stake, err := sdk.ParseCoinsNormalized(args[1])
			if err != nil {
				return fmt.Errorf("invalid stake: %w", err)
			}
			reason, _ := cmd.Flags().GetString("reason")
			msg := &types.MsgChallengeReceipt{
				Challenger: clientCtx.GetFromAddress().String(),
				Challenge: &types.Challenge{
					ReceiptId:         args[0],
					ChallengerAddress: clientCtx.GetFromAddress().String(),
					ChallengerStake:   stake,
					Reason:            reason,
				},
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String("reason", "disputed output", "human-readable dispute reason")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// CmdResolveDispute UPHOLDS a disputed receipt's challenge (slash the bond).
// The signer must be an active SuperNode adjudicator.
func CmdResolveDispute() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve-dispute [receipt-id]",
		Short: "Uphold a receipt dispute — slash the publisher bond (signer must be an active supernode)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := &types.MsgSettleReceipt{
				Settler:   clientCtx.GetFromAddress().String(),
				ReceiptId: args[0],
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// CmdSubmitReceipt anchors a SuperNode-attested Proof-of-Service receipt. The
// signer must be an active SuperNode account. The receipt_id is the
// content-addressed digest pos1<hex(BLAKE3(BLAKE3(input)‖model‖BLAKE3(output)))>,
// computed here client-side from --input/--model/--output.
func CmdSubmitReceipt() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit-receipt [tool-id]",
		Short: "Anchor a Proof-of-Service inference receipt (signer must be an active supernode)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			input, _ := cmd.Flags().GetString("input")
			model, _ := cmd.Flags().GetString("model")
			output, _ := cmd.Flags().GetString("result")
			sessionID, _ := cmd.Flags().GetString("session-id")
			lockID, _ := cmd.Flags().GetString("lock-id")
			if strings.TrimSpace(model) == "" {
				return fmt.Errorf("--model is required")
			}

			requestHash := blake3.Sum256([]byte(input))
			outputHash := blake3.Sum256([]byte(output))
			traceInput := append(append(append([]byte{}, requestHash[:]...), []byte(model)...), outputHash[:]...)
			trace := blake3.Sum256(traceInput)
			receiptID := "pos1" + hex.EncodeToString(trace[:])

			msg := &types.MsgSubmitReceipt{
				Router: clientCtx.GetFromAddress().String(),
				Receipt: &types.UsageReceipt{
					ReceiptId:   receiptID,
					ToolId:      args[0],
					RequestHash: requestHash[:],
					TraceHash:   trace[:],
					SessionId:   sessionID,
					LockId:      lockID,
				},
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String("input", "", "canonical inference input (hashed client-side)")
	cmd.Flags().String("model", "", "model identifier (required)")
	cmd.Flags().String("result", "", "canonical inference output (hashed client-side)")
	cmd.Flags().String("session-id", "", "optional session id to bind the receipt to")
	cmd.Flags().String("lock-id", "", "optional credits lock id this receipt settles")
	flags.AddTxFlagsToCmd(cmd)
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
