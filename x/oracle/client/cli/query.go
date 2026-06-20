
// Package cli exposes the oracle module query commands.
package cli

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// GetQueryCmd returns the root query command for the oracle module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Query commands for the Oracle module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQueryParamsCmd())
	cmd.AddCommand(NewQueryPriceFeedCmd())
	cmd.AddCommand(NewQueryAllPriceFeedsCmd())
	cmd.AddCommand(NewQueryAggregatedPriceCmd())

	return cmd
}

// NewQueryParamsCmd returns a command to query oracle module parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query oracle module parameters",
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

// NewQueryPriceFeedCmd returns a command to query a price feed by asset pair.
func NewQueryPriceFeedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "price-feed [asset-pair]",
		Short: "Query a price feed by asset pair (e.g. BTC/USD)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.PriceFeed(context.Background(), &types.QueryPriceFeedRequest{
				AssetPair: args[0],
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

// NewQueryAllPriceFeedsCmd returns a command to list all price feeds.
func NewQueryAllPriceFeedsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "all-price-feeds",
		Short: "List all price feeds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.AllPriceFeeds(context.Background(), &types.QueryAllPriceFeedsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryAggregatedPriceCmd returns a command to query the aggregated price for an asset pair.
func NewQueryAggregatedPriceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregated-price [asset-pair]",
		Short: "Query aggregated price for an asset pair (e.g. BTC/USD)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.AggregatedPrice(context.Background(), &types.QueryAggregatedPriceRequest{
				AssetPair: args[0],
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
