// Package cli defines Cobra commands for the router module. The router records
// and aggregates on-chain routing telemetry; the tx surface is intentionally
// minimal (only the tool-owner-signable activation record), while the query
// surface exposes the metrics the off-chain router and explorers consume.
package cli

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// GetQueryCmd returns the root query command for the router module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Query commands for the Router (routing-telemetry) module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewQueryParamsCmd(),
		NewQueryGlobalMetricsCmd(),
		NewQueryToolMetricsCmd(),
		NewQueryActiveToolsCmd(),
		NewQueryToolRankingCmd(),
	)
	return cmd
}

// NewQueryParamsCmd queries the router module parameters.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query the router module parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			qc := types.NewQueryClient(clientCtx)
			resp, err := qc.Params(context.Background(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryGlobalMetricsCmd queries the aggregated global routing metrics.
func NewQueryGlobalMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "global-metrics",
		Short: "Query the aggregated global routing metrics (activations, invocations, cache hits, spend)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			qc := types.NewQueryClient(clientCtx)
			resp, err := qc.GlobalMetrics(context.Background(), &types.QueryGlobalMetricsRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryToolMetricsCmd queries per-tool activation metrics + selection score.
func NewQueryToolMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool-metrics [tool-id]",
		Short: "Query routing metrics and selection score for a tool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			qc := types.NewQueryClient(clientCtx)
			resp, err := qc.ToolMetrics(context.Background(), &types.QueryToolMetricsRequest{ToolId: args[0]})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryActiveToolsCmd lists tools currently active (optionally per session/category).
func NewQueryActiveToolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "active-tools",
		Short: "List currently-active tools (optionally filtered by session or category)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			session, _ := cmd.Flags().GetString("session-id")
			category, _ := cmd.Flags().GetString("category")
			qc := types.NewQueryClient(clientCtx)
			resp, err := qc.ActiveTools(context.Background(), &types.QueryActiveToolsRequest{
				SessionId: session,
				Category:  category,
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}
	cmd.Flags().String("session-id", "", "filter by session id")
	cmd.Flags().String("category", "", "filter by category")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryToolRankingCmd returns the tool ranking by routing performance.
func NewQueryToolRankingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool-ranking",
		Short: "Query the tool ranking by routing performance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			category, _ := cmd.Flags().GetString("category")
			limit, _ := cmd.Flags().GetUint32("limit")
			window, _ := cmd.Flags().GetUint32("window-seconds")
			qc := types.NewQueryClient(clientCtx)
			resp, err := qc.ToolRanking(context.Background(), &types.QueryToolRankingRequest{
				Category:      category,
				Limit:         limit,
				WindowSeconds: window,
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}
	cmd.Flags().String("category", "", "filter by category")
	cmd.Flags().Uint32("limit", 20, "maximum results")
	cmd.Flags().Uint32("window-seconds", 0, "time window in seconds (0 = all time)")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
