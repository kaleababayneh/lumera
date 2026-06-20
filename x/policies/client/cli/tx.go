
// Package cli provides CLI transaction commands for the policies module.
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
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

const (
	flagPolicyID      = "policy-id"
	flagVersion       = "version"
	flagUpdateReason  = "reason"
	flagPolicyFile    = "policy-file"
	flagName          = "name"
	flagDescription   = "description"
	flagSchemaVersion = "schema-version"

	// UpdateParams flags
	flagMinPolicyDeposit           = "min-policy-deposit"
	flagMaxPolicyVersionHistory    = "max-policy-version-history"
	flagDefaultMigrationWindowSecs = "default-migration-window-seconds"

	maxPolicyFileBytes int64 = 4 << 20
)

// mustMarkFlagRequired marks a cobra flag required, panicking with the flag
// name on failure. MarkFlagRequired only errors when the flag is not
// registered — a programmer error at command-construction time — so a panic
// is the correct halt, but the flag name is essential for diagnosing the typo.
func mustMarkFlagRequired(cmd *cobra.Command, flag string) {
	if err := cmd.MarkFlagRequired(flag); err != nil {
		panic(fmt.Errorf("policies cli: mark flag %q required: %w", flag, err))
	}
}

// GetTxCmd bundles all policies tx commands under the module name.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Policies transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewCreatePolicyCmd())
	cmd.AddCommand(NewUpdatePolicyCmd())
	cmd.AddCommand(NewActivatePolicyCmd())
	cmd.AddCommand(NewDeprecatePolicyCmd())
	cmd.AddCommand(NewArchivePolicyCmd())
	cmd.AddCommand(NewUpdateParamsCmd())

	return cmd
}

// NewCreatePolicyCmd creates a new policy.
func NewCreatePolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-policy",
		Short: "Create a new policy",
		Long: `Create a new policy profile. You can provide the policy definition via a JSON file
or specify basic fields via flags.

Examples:
  # Create from JSON file
  lumeraai tx policies create-policy --policy-file policy.json --from mykey

  # Create with inline fields
  lumeraai tx policies create-policy --policy-id my-policy --version 1.0.0 --name "My Policy" --from mykey`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			var policy *types.PolicyProfile

			// Check if policy file is provided
			policyFile, err := cmd.Flags().GetString(flagPolicyFile)
			if err != nil {
				return err
			}

			if policyFile != "" {
				policy, err = readPolicyProfileFile(policyFile)
				if err != nil {
					return err
				}
			} else {
				// Build policy from flags
				policyID, err := cmd.Flags().GetString(flagPolicyID)
				if err != nil {
					return err
				}
				if policyID == "" {
					return fmt.Errorf("--%s or --%s is required", flagPolicyID, flagPolicyFile)
				}

				version, err := cmd.Flags().GetString(flagVersion)
				if err != nil {
					return err
				}
				if version == "" {
					version = "1.0.0"
				}

				name, err := cmd.Flags().GetString(flagName)
				if err != nil {
					return err
				}

				description, err := cmd.Flags().GetString(flagDescription)
				if err != nil {
					return err
				}

				schemaVersion, err := cmd.Flags().GetString(flagSchemaVersion)
				if err != nil {
					return err
				}
				if schemaVersion == "" {
					schemaVersion = "1.0"
				}

				policy = &types.PolicyProfile{
					PolicyId:      strings.TrimSpace(policyID),
					Version:       strings.TrimSpace(version),
					SchemaVersion: strings.TrimSpace(schemaVersion),
					Metadata: &types.PolicyMetadata{
						Name:        strings.TrimSpace(name),
						Description: strings.TrimSpace(description),
					},
				}
			}

			msg := &types.MsgCreatePolicy{
				Creator: clientCtx.GetFromAddress().String(),
				Policy:  policy,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagPolicyFile, "", "Path to JSON file containing policy definition")
	cmd.Flags().String(flagPolicyID, "", "Unique policy identifier")
	cmd.Flags().String(flagVersion, "1.0.0", "Policy version (semver)")
	cmd.Flags().String(flagName, "", "Policy display name")
	cmd.Flags().String(flagDescription, "", "Policy description")
	cmd.Flags().String(flagSchemaVersion, "1.0", "Schema version")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func readPolicyProfileFile(path string) (*types.PolicyProfile, error) {
	data, err := readPolicyFile(path)
	if err != nil {
		return nil, err
	}

	policy := &types.PolicyProfile{}
	if err := json.Unmarshal(data, policy); err != nil {
		return nil, fmt.Errorf("failed to parse policy JSON: %w", err)
	}
	return policy, nil
}

func readPolicyFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy file: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect policy file: %w", err)
	}
	if info.Mode().IsRegular() && info.Size() > maxPolicyFileBytes {
		return nil, fmt.Errorf("policy file exceeds %d-byte limit", maxPolicyFileBytes)
	}

	data, err := io.ReadAll(io.LimitReader(file, maxPolicyFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read policy file: %w", err)
	}
	if int64(len(data)) > maxPolicyFileBytes {
		return nil, fmt.Errorf("policy file exceeds %d-byte limit", maxPolicyFileBytes)
	}
	return data, nil
}

// NewUpdatePolicyCmd updates an existing policy, creating a new version.
func NewUpdatePolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-policy",
		Short: "Update an existing policy (creates new version)",
		Long: `Update an existing policy, creating a new version. Only the policy owner can update.

Examples:
  lumeraai tx policies update-policy --policy-id my-policy --policy-file updated-policy.json --reason "Bug fix" --from mykey`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			policyID, err := cmd.Flags().GetString(flagPolicyID)
			if err != nil {
				return err
			}
			if policyID == "" {
				return fmt.Errorf("--%s is required", flagPolicyID)
			}

			policyFile, err := cmd.Flags().GetString(flagPolicyFile)
			if err != nil {
				return err
			}
			if policyFile == "" {
				return fmt.Errorf("--%s is required for update", flagPolicyFile)
			}

			policy, err := readPolicyProfileFile(policyFile)
			if err != nil {
				return err
			}

			// Ensure policy ID matches
			if policy.PolicyId == "" {
				policy.PolicyId = policyID
			} else if policy.PolicyId != policyID {
				return fmt.Errorf("policy_id in file (%s) does not match --%s (%s)", policy.PolicyId, flagPolicyID, policyID)
			}

			reason, err := cmd.Flags().GetString(flagUpdateReason)
			if err != nil {
				return err
			}

			msg := &types.MsgUpdatePolicy{
				Updater:      clientCtx.GetFromAddress().String(),
				PolicyId:     strings.TrimSpace(policyID),
				Policy:       policy,
				UpdateReason: strings.TrimSpace(reason),
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagPolicyID, "", "Policy identifier to update")
	cmd.Flags().String(flagPolicyFile, "", "Path to JSON file containing updated policy definition")
	cmd.Flags().String(flagUpdateReason, "", "Reason for the update")
	mustMarkFlagRequired(cmd, flagPolicyID)
	mustMarkFlagRequired(cmd, flagPolicyFile)
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewActivatePolicyCmd activates a policy (governance authority only).
func NewActivatePolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activate-policy [policy-id] [version]",
		Short: "Activate a policy (governance authority only)",
		Long: `Activate a policy to make it enforceable. Only the governance authority can activate policies.

Examples:
  lumeraai tx policies activate-policy my-policy 1.0.0 --from authority`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			policyID := strings.TrimSpace(args[0])
			version := strings.TrimSpace(args[1])

			msg := &types.MsgActivatePolicy{
				Authority: clientCtx.GetFromAddress().String(),
				PolicyId:  policyID,
				Version:   version,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewDeprecatePolicyCmd deprecates a policy (governance authority only).
func NewDeprecatePolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deprecate-policy [policy-id] [version]",
		Short: "Deprecate a policy (governance authority only)",
		Long: `Deprecate an active policy. Only the governance authority can deprecate policies.
Deprecated policies remain functional during a migration window.

Examples:
  lumeraai tx policies deprecate-policy my-policy 1.0.0 --from authority`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			policyID := strings.TrimSpace(args[0])
			version := strings.TrimSpace(args[1])

			msg := &types.MsgDeprecatePolicy{
				Authority: clientCtx.GetFromAddress().String(),
				PolicyId:  policyID,
				Version:   version,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewArchivePolicyCmd archives a policy (governance authority only).
func NewArchivePolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive-policy [policy-id] [version]",
		Short: "Archive a deprecated policy (governance authority only)",
		Long: `Archive a deprecated policy. Only the governance authority can archive policies.
Archived policies can no longer be used but remain in history.

Examples:
  lumeraai tx policies archive-policy my-policy 1.0.0 --from authority`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			policyID := strings.TrimSpace(args[0])
			version := strings.TrimSpace(args[1])

			msg := &types.MsgArchivePolicy{
				Authority: clientCtx.GetFromAddress().String(),
				PolicyId:  policyID,
				Version:   version,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUpdateParamsCmd updates module parameters (governance authority only).
func NewUpdateParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params",
		Short: "Update policies module parameters (governance authority only)",
		Long: `Update policies module parameters. Only the governance authority can update parameters.

Examples:
  lumeraai tx policies update-params --min-policy-deposit 200000000 --from authority`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			params := types.DefaultParams()
			changed := false

			if cmd.Flags().Changed(flagMinPolicyDeposit) {
				val, err := cmd.Flags().GetString(flagMinPolicyDeposit)
				if err != nil {
					return err
				}
				params.MinPolicyDeposit = strings.TrimSpace(val)
				changed = true
			}

			if cmd.Flags().Changed(flagMaxPolicyVersionHistory) {
				val, err := cmd.Flags().GetUint32(flagMaxPolicyVersionHistory)
				if err != nil {
					return err
				}
				params.MaxPolicyVersionHistory = val
				changed = true
			}

			if cmd.Flags().Changed(flagDefaultMigrationWindowSecs) {
				val, err := cmd.Flags().GetUint32(flagDefaultMigrationWindowSecs)
				if err != nil {
					return err
				}
				params.DefaultMigrationWindowSeconds = val
				changed = true
			}

			if !changed {
				return fmt.Errorf("at least one parameter flag must be set")
			}

			msg := &types.MsgUpdateParams{
				Authority: clientCtx.GetFromAddress().String(),
				Params:    params,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagMinPolicyDeposit, "", "Minimum deposit required for policy creation (e.g. 100000000)")
	cmd.Flags().Uint32(flagMaxPolicyVersionHistory, 0, "Maximum number of policy versions to retain")
	cmd.Flags().Uint32(flagDefaultMigrationWindowSecs, 0, "Default migration window in seconds for deprecated policies")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
