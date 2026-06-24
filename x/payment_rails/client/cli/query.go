package cli

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/payment_rails/types"
)

// GetQueryCmd returns the root query command for the payment_rails module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Query commands for the Payment Rails module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQueryParamsCmd())
	cmd.AddCommand(NewQueryDepositCmd())
	cmd.AddCommand(NewQueryDepositsCmd())
	cmd.AddCommand(NewQueryWithdrawCmd())
	cmd.AddCommand(NewQueryWithdrawalsCmd())
	cmd.AddCommand(NewQueryPricingCmd())

	return cmd
}

// NewQueryParamsCmd returns a command to query payment rails parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query payment rails parameters",
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

// NewQueryDepositCmd returns a command to query a specific deposit.
func NewQueryDepositCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit [deposit-id]",
		Short: "Query a deposit by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Deposit(context.Background(), &types.QueryDepositRequest{
				DepositId: args[0],
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

// NewQueryDepositsCmd returns a command to list deposits with optional filters.
func NewQueryDepositsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposits",
		Short: "List deposits, optionally filtered by user or status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			user, _ := cmd.Flags().GetString("user")
			statusStr, _ := cmd.Flags().GetString("status")
			status, err := parseDepositStatus(statusStr)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Deposits(context.Background(), &types.QueryDepositsRequest{
				User:   user,
				Status: status,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String("user", "", "Filter by user address")
	cmd.Flags().String("status", "", "Filter by status: pending|priced|minted|finalized|refunded")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryWithdrawCmd returns a command to query a specific withdrawal.
func NewQueryWithdrawCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw [withdraw-id]",
		Short: "Query a withdrawal by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Withdraw(context.Background(), &types.QueryWithdrawRequest{
				WithdrawId: args[0],
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

// NewQueryWithdrawalsCmd returns a command to list withdrawals with optional filters.
func NewQueryWithdrawalsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdrawals",
		Short: "List withdrawals, optionally filtered by user or status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			user, _ := cmd.Flags().GetString("user")
			statusStr, _ := cmd.Flags().GetString("status")
			status, err := parseWithdrawStatus(statusStr)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Withdrawals(context.Background(), &types.QueryWithdrawalsRequest{
				User:   user,
				Status: status,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String("user", "", "Filter by user address")
	cmd.Flags().String("status", "", "Filter by status: requested|completed|failed")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryPricingCmd returns a command to query pricing for a deposit.
func NewQueryPricingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pricing [deposit-id]",
		Short: "Query pricing information for a deposit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Pricing(context.Background(), &types.QueryPricingRequest{
				DepositId: args[0],
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
