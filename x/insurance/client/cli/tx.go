
// Package cli exposes insurance module transaction commands.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

const maxInsuranceParamsFileBytes int64 = 1 << 20

// GetTxCmd returns the transaction commands for the insurance module
func GetTxCmd() *cobra.Command {
	insuranceTxCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	insuranceTxCmd.AddCommand(
		GetCmdFileClaim(),
		GetCmdProcessContribution(),
		newUpdateParamsCmd(),
	)

	return insuranceTxCmd
}

// GetCmdFileClaim returns the CLI command for filing an insurance claim
func GetCmdFileClaim() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "file-claim [receipt-id] [claimed-amount] [reason]",
		Short: "File an insurance claim for a failed tool invocation",
		Long: `File an insurance claim for a failed tool invocation.

Example:
$ lumeraaid tx insurance file-claim receipt123 100000ulac "Tool execution failed" --from user --evidence-hash abc123 --policy-snapshot policy-v1`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			receiptID := args[0]
			// Parse claimed amount (single coin as per proto)
			claimedCoin, err := sdk.ParseCoinNormalized(args[1])
			if err != nil {
				return fmt.Errorf("invalid claimed amount: %w", err)
			}
			reason := args[2]

			// Parse optional flags
			toolID, _ := cmd.Flags().GetString("tool-id")
			publisherID, _ := cmd.Flags().GetString("publisher-id")
			evidenceHash, _ := cmd.Flags().GetString("evidence-hash")
			evidenceList, _ := cmd.Flags().GetStringSlice("evidence")

			// Build evidence list
			evidence := []*types.Evidence{}
			for _, ev := range evidenceList {
				parts := strings.SplitN(ev, ":", 3)
				if len(parts) >= 2 {
					evType := parts[0]
					evHash := evidenceHash
					evURI := ""
					evDesc := parts[1]
					if len(parts) == 3 {
						evURI = parts[2]
					}
					evidence = append(evidence, &types.Evidence{
						Type:        evType,
						Hash:        evHash,
						Uri:         evURI,
						Description: evDesc,
					})
				}
			}

			msg := &types.MsgFileClaim{
				Claimant:      clientCtx.GetFromAddress().String(),
				ReceiptId:     receiptID,
				ToolId:        toolID,
				PublisherId:   publisherID,
				ClaimedAmount: claimedCoin,
				Reason:        reason,
				Evidence:      evidence,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("tool-id", "", "Tool ID for the claim")
	cmd.Flags().String("publisher-id", "", "Publisher ID for the claim")
	cmd.Flags().String("evidence-hash", "", "Hash of supporting evidence")
	cmd.Flags().StringSlice("evidence", []string{}, "Evidence items in format type:content")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// GetCmdProcessContribution returns the CLI command for processing insurance contributions (governance only)
func GetCmdProcessContribution() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "process-contribution [receipt-id] [amount] [tool-id] [publisher-id]",
		Short: "Process an insurance contribution from a settlement (governance only)",
		Long: `Process an insurance contribution from a tool invocation settlement.
This command can only be executed by the governance authority.

Example:
$ lumeraaid tx insurance process-contribution receipt123 10000ulac tool456 publisher789 --from governance`,
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			receiptID := args[0]
			amountCoin, err := parseSingleInsuranceCLIAmount(args[1])
			if err != nil {
				return fmt.Errorf("invalid amount: %w", err)
			}
			toolID := args[2]
			publisherID := args[3]

			msg := &types.MsgProcessContribution{
				Authority:   clientCtx.GetFromAddress().String(),
				ReceiptId:   receiptID,
				Amount:      amountCoin,
				ToolId:      toolID,
				PublisherId: publisherID,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func parseSingleInsuranceCLIAmount(raw string) (sdk.Coin, error) {
	coins, err := sdk.ParseCoinsNormalized(raw)
	if err != nil {
		return sdk.Coin{}, err
	}
	if len(coins) != 1 {
		return sdk.Coin{}, fmt.Errorf("amount must contain exactly one coin")
	}
	return coins[0], nil
}

func newUpdateParamsCmd() *cobra.Command {
	const flagParamsFile = "params-file"

	cmd := &cobra.Command{
		Use:   "update-params",
		Short: "Update insurance module parameters (governance authority only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			paramsFile, err := cmd.Flags().GetString(flagParamsFile)
			if err != nil {
				return err
			}
			paramsFile = strings.TrimSpace(paramsFile)
			if paramsFile == "" {
				return fmt.Errorf("--%s is required", flagParamsFile)
			}

			raw, err := readInsuranceParamsFile(paramsFile)
			if err != nil {
				return err
			}

			var params types.Params
			if err := json.Unmarshal(raw, &params); err != nil {
				return fmt.Errorf("decode params file: %w", err)
			}

			msg := &types.MsgUpdateParams{
				Authority: clientCtx.GetFromAddress().String(),
				Params:    params,
			}
			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagParamsFile, "", "Path to insurance params JSON (Params)")
	_ = cmd.MarkFlagRequired(flagParamsFile)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func readInsuranceParamsFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read params file: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect params file: %w", err)
	}
	if info.Mode().IsRegular() && info.Size() > maxInsuranceParamsFileBytes {
		return nil, fmt.Errorf("params file exceeds %d-byte limit", maxInsuranceParamsFileBytes)
	}

	raw, err := io.ReadAll(io.LimitReader(file, maxInsuranceParamsFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read params file: %w", err)
	}
	if int64(len(raw)) > maxInsuranceParamsFileBytes {
		return nil, fmt.Errorf("params file exceeds %d-byte limit", maxInsuranceParamsFileBytes)
	}
	return raw, nil
}
