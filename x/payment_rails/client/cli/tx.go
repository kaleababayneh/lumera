// Package cli provides Cosmos SDK CLI commands for the payment_rails module.
package cli

import (
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/payment_rails/types"
)

const (
	flagTxHash        = "tx-hash"
	flagRequestID     = "request-id"
	flagConfirmations = "confirmations"
	flagQuotedPrice   = "quoted-price"
	flagDenom         = "denom"
)

// GetTxCmd returns the root tx command for the payment_rails module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Payment Rails transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewCreateDepositCmd())
	cmd.AddCommand(NewRequestWithdrawCmd())

	return cmd
}

// NewCreateDepositCmd builds a create-deposit transaction command.
func NewCreateDepositCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-deposit [amount]",
		Short: "Create a deposit to mint LAC tokens",
		Long: `Submit a deposit of the specified amount (e.g., 1000ulumera) to mint LAC tokens.
The amount is specified in standard Cosmos coin format (e.g., "1000ulumera").`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			coin, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return fmt.Errorf("invalid amount: %w", err)
			}

			txHash, err := cmd.Flags().GetString(flagTxHash)
			if err != nil {
				return err
			}
			requestID, err := cmd.Flags().GetString(flagRequestID)
			if err != nil {
				return err
			}
			confirmations, err := cmd.Flags().GetUint64(flagConfirmations)
			if err != nil {
				return err
			}
			quotedPrice, err := cmd.Flags().GetString(flagQuotedPrice)
			if err != nil {
				return err
			}

			msg := &types.MsgCreateDeposit{
				User:          clientCtx.GetFromAddress().String(),
				Amount:        types.CoinToProto(coin),
				TxHash:        txHash,
				RequestId:     requestID,
				Confirmations: confirmations,
				QuotedPrice:   quotedPrice,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagTxHash, "", "On-chain transaction hash for the deposit")
	cmd.Flags().String(flagRequestID, "", "Unique request identifier for idempotency")
	cmd.Flags().Uint64(flagConfirmations, 0, "Number of block confirmations")
	cmd.Flags().String(flagQuotedPrice, "", "Quoted exchange price at deposit time")

	_ = cmd.MarkFlagRequired(flagTxHash)
	_ = cmd.MarkFlagRequired(flagRequestID)
	_ = cmd.MarkFlagRequired(flagConfirmations)
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewRequestWithdrawCmd builds a request-withdraw transaction command.
func NewRequestWithdrawCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request-withdraw [lac-amount]",
		Short: "Request withdrawal of LAC tokens for native assets",
		Long: `Submit a withdrawal request to burn LAC tokens and receive native assets.
The lac-amount is specified in standard Cosmos coin format (e.g., "500ulac").`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			coin, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return fmt.Errorf("invalid LAC amount: %w", err)
			}

			denom, err := cmd.Flags().GetString(flagDenom)
			if err != nil {
				return err
			}
			requestID, err := cmd.Flags().GetString(flagRequestID)
			if err != nil {
				return err
			}
			quotedPrice, err := cmd.Flags().GetString(flagQuotedPrice)
			if err != nil {
				return err
			}

			msg := &types.MsgRequestWithdraw{
				User:        clientCtx.GetFromAddress().String(),
				LacAmount:   types.CoinToProto(coin),
				Denom:       denom,
				RequestId:   requestID,
				QuotedPrice: quotedPrice,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagDenom, "", "Target denomination for withdrawal (required)")
	cmd.Flags().String(flagRequestID, "", "Unique request identifier for idempotency")
	cmd.Flags().String(flagQuotedPrice, "", "Quoted exchange price at withdrawal time")

	_ = cmd.MarkFlagRequired(flagDenom)
	_ = cmd.MarkFlagRequired(flagRequestID)
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func parseDepositStatus(s string) (types.DepositStatus, error) {
	switch strings.ToLower(s) {
	case "", "unspecified":
		return types.DepositStatus_DEPOSIT_STATUS_UNSPECIFIED, nil
	case "pending":
		return types.DepositStatus_DEPOSIT_STATUS_PENDING, nil
	case "priced":
		return types.DepositStatus_DEPOSIT_STATUS_PRICED, nil
	case "minted":
		return types.DepositStatus_DEPOSIT_STATUS_MINTED, nil
	case "finalized":
		return types.DepositStatus_DEPOSIT_STATUS_FINALIZED, nil
	case "refunded":
		return types.DepositStatus_DEPOSIT_STATUS_REFUNDED, nil
	default:
		return types.DepositStatus_DEPOSIT_STATUS_UNSPECIFIED,
			fmt.Errorf("unknown deposit status %q; expected pending|priced|minted|finalized|refunded", s)
	}
}

func parseWithdrawStatus(s string) (types.WithdrawStatus, error) {
	switch strings.ToLower(s) {
	case "", "unspecified":
		return types.WithdrawStatus_WITHDRAW_STATUS_UNSPECIFIED, nil
	case "requested":
		return types.WithdrawStatus_WITHDRAW_STATUS_REQUESTED, nil
	case "completed":
		return types.WithdrawStatus_WITHDRAW_STATUS_COMPLETED, nil
	case "failed":
		return types.WithdrawStatus_WITHDRAW_STATUS_FAILED, nil
	default:
		return types.WithdrawStatus_WITHDRAW_STATUS_UNSPECIFIED,
			fmt.Errorf("unknown withdraw status %q; expected requested|completed|failed", s)
	}
}
