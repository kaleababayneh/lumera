
// Package cli defines Cobra commands for the passport module.
package cli

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/passport/types"
)

// GetQueryCmd returns the root query command for the passport module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Query commands for the Passport module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQueryPassportCmd())
	cmd.AddCommand(NewQueryPassportByAgentCmd())
	cmd.AddCommand(NewQueryPassportsCmd())
	cmd.AddCommand(NewQueryParamsCmd())

	return cmd
}

// NewQueryPassportCmd returns a command to query a passport by ID.
func NewQueryPassportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "passport [passport-id]",
		Short: "Query a passport by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Passport(context.Background(), &types.QueryPassportRequest{
				PassportId: args[0],
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryPassportByAgentCmd returns a command to query a passport by agent public key.
func NewQueryPassportByAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "by-agent [agent-pubkey]",
		Short: "Query a passport by agent public key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.PassportByAgent(context.Background(), &types.QueryPassportByAgentRequest{
				AgentPubkey: args[0],
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryPassportsCmd returns a command to list passports with optional status filter.
func NewQueryPassportsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List passports, optionally filtered by status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			statusStr, _ := cmd.Flags().GetString("status")
			status, err := parsePassportStatus(statusStr)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Passports(context.Background(), &types.QueryPassportsRequest{
				StatusFilter: status,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String("status", "", "Filter by status: active|suspended|revoked")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryParamsCmd returns a command to query passport module parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query passport module parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Params(context.Background(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// parsePassportStatus converts a CLI string to a PassportStatus enum value.
func parsePassportStatus(s string) (types.PassportStatus, error) {
	if s == "" {
		return types.PassportStatus_PASSPORT_STATUS_UNSPECIFIED, nil
	}
	switch s {
	case "active":
		return types.PassportStatus_PASSPORT_STATUS_ACTIVE, nil
	case "suspended":
		return types.PassportStatus_PASSPORT_STATUS_SUSPENDED, nil
	case "revoked":
		return types.PassportStatus_PASSPORT_STATUS_REVOKED, nil
	default:
		return 0, fmt.Errorf("unknown passport status %q; expected active|suspended|revoked", s)
	}
}
