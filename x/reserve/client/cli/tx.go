
// Package cli provides CLI transaction commands for the reserve module.
package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogojsonpb "github.com/cosmos/gogoproto/jsonpb"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/internal/logging"
	"github.com/LumeraProtocol/lumera/x/reserve/types"
)

const (
	flagPolicyID        = "policy-id"
	flagToolID          = "tool-id"
	flagTier            = "tier"
	flagAmount          = "amount"
	flagDurationSeconds = "duration-seconds"
	flagParamsFile      = "params-file"
)

const maxReserveParamsFileBytes int64 = 1 << 20

// mustMarkFlagRequired marks a cobra flag required, panicking with the flag
// name on failure. Cobra only errors when the flag was not registered, which
// is a command-construction bug.
func mustMarkFlagRequired(cmd *cobra.Command, flag string) {
	if err := cmd.MarkFlagRequired(flag); err != nil {
		panic(fmt.Errorf("reserve cli: mark flag %q required: %w", flag, err))
	}
}

// GetTxCmd bundles all reserve tx commands under the module name.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Reserve transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewCreateCommitmentCmd())
	cmd.AddCommand(NewReleaseExpiredCmd())
	cmd.AddCommand(NewUpdateParamsCmd())

	return cmd
}

// NewCreateCommitmentCmd provisions a reserve commitment.
func NewCreateCommitmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-commitment",
		Short: "Create a reserve commitment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			policyID, err := cmd.Flags().GetString(flagPolicyID)
			if err != nil {
				return err
			}
			toolID, err := cmd.Flags().GetString(flagToolID)
			if err != nil {
				return err
			}
			tier, err := cmd.Flags().GetString(flagTier)
			if err != nil {
				return err
			}
			amountStr, err := cmd.Flags().GetString(flagAmount)
			if err != nil {
				return err
			}
			durationSeconds, err := cmd.Flags().GetUint64(flagDurationSeconds)
			if err != nil {
				return err
			}
			amount, err := parseReserveCoinFlag(amountStr)
			if err != nil {
				return err
			}

			msg := &types.MsgCreateCommitment{
				Owner:           clientCtx.GetFromAddress().String(),
				PolicyId:        strings.TrimSpace(policyID),
				ToolId:          strings.TrimSpace(toolID),
				Tier:            strings.TrimSpace(tier),
				Amount:          amount,
				DurationSeconds: durationSeconds,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagPolicyID, "", "Policy identifier that owns the reserve commitment")
	cmd.Flags().String(flagToolID, "", "Tool identifier scoped to this commitment; empty means wildcard")
	cmd.Flags().String(flagTier, "", "Reserve tier name")
	cmd.Flags().String(flagAmount, "", "Commitment amount (e.g. 1000000ulac)")
	cmd.Flags().Uint64(flagDurationSeconds, 0, "Commitment duration in seconds (0 uses tier default)")
	mustMarkFlagRequired(cmd, flagPolicyID)
	mustMarkFlagRequired(cmd, flagTier)
	mustMarkFlagRequired(cmd, flagAmount)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewReleaseExpiredCmd sweeps expired reserve commitments.
func NewReleaseExpiredCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release-expired",
		Short: "Release expired reserve commitments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgReleaseExpired{
				Authority: clientCtx.GetFromAddress().String(),
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUpdateParamsCmd updates reserve module parameters from a JSON file.
func NewUpdateParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params",
		Short: "Update reserve module parameters",
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
			params, err := parseReserveParamsFile(paramsFile)
			if err != nil {
				return err
			}

			msg := &types.MsgUpdateParams{
				Authority: clientCtx.GetFromAddress().String(),
				Params:    *params,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagParamsFile, "", "Path to reserve params JSON using lumera.reserve.v1.ReserveParams shape")
	mustMarkFlagRequired(cmd, flagParamsFile)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func parseReserveCoinFlag(value string) (sdk.Coin, error) {
	value = strings.TrimSpace(value)
	coin, err := sdk.ParseCoinNormalized(value)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid --%s %q: %s",
			flagAmount,
			redactReserveCLIDiagnostic(value),
			redactReserveCLIDiagnostic(err.Error()))
	}
	return coin, nil
}

func parseReserveParamsFile(path string) (*types.ReserveParams, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("--%s is required", flagParamsFile)
	}
	data, err := readReserveParamsFile(path)
	if err != nil {
		return nil, err
	}
	return parseReserveParamsJSON(data)
}

func readReserveParamsFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read reserve params file %q: %s",
			redactReserveCLIDiagnostic(path),
			redactReserveCLIDiagnostic(err.Error()))
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect reserve params file %q: %s",
			redactReserveCLIDiagnostic(path),
			redactReserveCLIDiagnostic(err.Error()))
	}
	if info.Mode().IsRegular() && info.Size() > maxReserveParamsFileBytes {
		return nil, fmt.Errorf("reserve params file %q exceeds %d-byte limit",
			redactReserveCLIDiagnostic(path),
			maxReserveParamsFileBytes)
	}

	data, err := io.ReadAll(io.LimitReader(file, maxReserveParamsFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read reserve params file %q: %s",
			redactReserveCLIDiagnostic(path),
			redactReserveCLIDiagnostic(err.Error()))
	}
	if int64(len(data)) > maxReserveParamsFileBytes {
		return nil, fmt.Errorf("reserve params file %q exceeds %d-byte limit",
			redactReserveCLIDiagnostic(path),
			maxReserveParamsFileBytes)
	}
	return data, nil
}

func parseReserveParamsJSON(data []byte) (*types.ReserveParams, error) {
	params := &types.ReserveParams{}
	if err := gogojsonpb.Unmarshal(bytes.NewReader(data), params); err != nil {
		return nil, fmt.Errorf("failed to parse reserve params JSON: %s",
			redactReserveCLIDiagnostic(err.Error()))
	}
	if strings.TrimSpace(params.GetCreditDenom()) == "" {
		return nil, fmt.Errorf("reserve params credit_denom is required")
	}
	if len(params.GetTiers()) == 0 {
		return nil, fmt.Errorf("reserve params tiers are required")
	}
	return params, nil
}

func redactReserveCLIDiagnostic(value string) string {
	return logging.RedactPII(strings.TrimSpace(value))
}
