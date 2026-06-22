package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/LumeraProtocol/lumera/x/vaults/types"
)

const (
	flagPolicyID = "policy-id"
	flagToolID   = "tool-id"
	flagTier     = "tier"
	flagAmount   = "amount"
	flagEndTime  = "end-time"
	flagRollover = "rollover"
)

// mustMarkFlagRequired marks a cobra flag required, panicking with the flag
// name on failure. MarkFlagRequired only errors when the flag is not
// registered — a programmer error at command-construction time — so a panic
// is the correct halt, but the flag name is essential for diagnosing the typo.
func mustMarkFlagRequired(cmd *cobra.Command, flag string) {
	if err := cmd.MarkFlagRequired(flag); err != nil {
		panic(fmt.Errorf("vaults cli: mark flag %q required: %w", flag, err))
	}
}

// GetTxCmd bundles all vault transaction commands under a single root command.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Vault transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewCreateVaultCmd())
	return cmd
}

// NewCreateVaultCmd constructs a command to create a new prepaid vault commitment.
func NewCreateVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a prepaid vault commitment",
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
			rollover, err := cmd.Flags().GetBool(flagRollover)
			if err != nil {
				return err
			}

			if strings.TrimSpace(policyID) == "" {
				return fmt.Errorf("policy id is required")
			}
			if strings.TrimSpace(tier) == "" {
				return fmt.Errorf("tier is required")
			}

			coin, err := sdk.ParseCoinNormalized(amountStr)
			if err != nil {
				return err
			}

			endTime, err := parseEndTime(cmd.Flags())
			if err != nil {
				return err
			}

			msg := types.MsgCreateVault{
				Owner:           clientCtx.GetFromAddress().String(),
				PolicyId:        policyID,
				ToolId:          toolID,
				Tier:            tier,
				PrepaidAmount:   coin,
				RolloverAllowed: rollover,
			}
			if endTime != nil {
				msg.CommitmentEndTime = *endTime
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	cmd.Flags().String(flagPolicyID, "", "Policy identifier that purchased the reserve commitment")
	cmd.Flags().String(flagToolID, "", "Tool identifier the commitment is scoped to (optional)")
	cmd.Flags().String(flagTier, "", "Reserve tier to purchase")
	cmd.Flags().String(flagAmount, "", "Prepaid amount (e.g. 1000000ulac)")
	cmd.Flags().String(flagEndTime, "", "Commitment end time in RFC3339 (optional)")
	cmd.Flags().Bool(flagRollover, false, "Allow automatic rollover on expiry")

	mustMarkFlagRequired(cmd, flagPolicyID)
	mustMarkFlagRequired(cmd, flagTier)
	mustMarkFlagRequired(cmd, flagAmount)

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func parseEndTime(fs *pflag.FlagSet) (*time.Time, error) {
	value, err := fs.GetString(flagEndTime)
	if err != nil {
		return nil, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("invalid end-time: %w", err)
	}
	return &t, nil
}
