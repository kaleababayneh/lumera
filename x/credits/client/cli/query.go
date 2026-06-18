//go:build cosmos

// Package cli provides CLI query commands for the credits module.
package cli

import (
	"context"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// GetQueryCmd bundles all credits query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the credits module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQueryLockCmd())
	cmd.AddCommand(NewQueryLocksCmd())
	cmd.AddCommand(NewQueryHoldCmd())
	cmd.AddCommand(NewQueryHoldsCmd())
	cmd.AddCommand(NewQueryParamsCmd())

	return cmd
}

// NewQueryHoldCmd queries a single hold by id.
func NewQueryHoldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hold [hold-id]",
		Short: "Query a canonical hold view by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			holdID := strings.TrimSpace(args[0])
			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Hold(context.Background(), &types.QueryHoldRequest{HoldId: holdID})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryLockCmd queries a single lock by id.
func NewQueryLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock [lock-id]",
		Short: "Query a credit lock by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			lockID := strings.TrimSpace(args[0])
			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Lock(context.Background(), &types.QueryLockRequest{LockId: lockID})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryLocksCmd queries a paginated list of locks, optionally filtered by router address.
func NewQueryLocksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "locks [router]",
		Short: "List credit locks",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			router := ""
			if len(args) > 0 {
				router = strings.TrimSpace(args[0])
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Locks(context.Background(), &types.QueryLocksRequest{
				Router:     router,
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, "locks")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryHoldsCmd queries canonical hold views with optional filtering.
func NewQueryHoldsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "holds",
		Short: "List canonical hold views",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			router, err := cmd.Flags().GetString("router")
			if err != nil {
				return err
			}
			sessionID, err := cmd.Flags().GetString("session-id")
			if err != nil {
				return err
			}
			activeOnly, err := cmd.Flags().GetBool("active-only")
			if err != nil {
				return err
			}
			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Holds(context.Background(), &types.QueryHoldsRequest{
				Router:     strings.TrimSpace(router),
				SessionId:  strings.TrimSpace(sessionID),
				ActiveOnly: activeOnly,
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String("router", "", "Filter by router address")
	cmd.Flags().String("session-id", "", "Filter by session identifier")
	cmd.Flags().Bool("active-only", false, "Return only active holds")
	flags.AddPaginationFlagsToCmd(cmd, "holds")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryParamsCmd queries module parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query credits module parameters",
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
