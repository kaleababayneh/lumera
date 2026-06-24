// Package cli provides Cosmos SDK query and transaction commands for the CAC module.
package cli

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/cac/types"
)

// GetQueryCmd returns the root query command for the cac module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Query commands for the CAC module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewQueryGetCacheEntryCmd())
	cmd.AddCommand(NewQueryLookupByRequestCmd())
	cmd.AddCommand(NewQueryCacheStatsCmd())
	cmd.AddCommand(NewQueryListToolEntriesCmd())
	cmd.AddCommand(NewQueryParamsCmd())

	return cmd
}

// NewQueryGetCacheEntryCmd returns a command to query a cache entry by content hash.
func NewQueryGetCacheEntryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entry [content-hash]",
		Short: "Query a cache entry by content hash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.GetCacheEntry(context.Background(), &types.QueryGetCacheEntryRequest{
				ContentHash: args[0],
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

// NewQueryLookupByRequestCmd returns a command to look up cache entries by request hash.
func NewQueryLookupByRequestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lookup [request-hash]",
		Short: "Look up cache entries by request hash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			toolID, _ := cmd.Flags().GetString(flagToolID)

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.LookupByRequest(context.Background(), &types.QueryLookupByRequestRequest{
				RequestHash: args[0],
				ToolId:      toolID,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String(flagToolID, "", "Optional filter by tool ID")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryCacheStatsCmd returns a command to query cache performance statistics.
func NewQueryCacheStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Query cache performance statistics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.GetCacheStats(context.Background(), &types.QueryGetCacheStatsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryListToolEntriesCmd returns a command to list cache entries for a specific tool.
func NewQueryListToolEntriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool-entries [tool-id]",
		Short: "List cache entries for a tool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			limit, _ := cmd.Flags().GetUint64("limit")
			offset, _ := cmd.Flags().GetUint64("offset")

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.ListToolEntries(context.Background(), &types.QueryListToolEntriesRequest{
				ToolId: args[0],
				Limit:  limit,
				Offset: offset,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().Uint64("limit", 100, "Maximum results to return (max 1000)")
	cmd.Flags().Uint64("offset", 0, "Pagination offset")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryParamsCmd returns a command to query CAC module parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query CAC module parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.GetParams(context.Background(), &types.QueryGetParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
