// Package cli provides Cosmos CLI commands for the challenges module.
package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

const (
	flagStatus = "status"
	flagLimit  = "limit"
)

// GetQueryCmd returns the challenges query command tree.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the challenges module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewQueryParamsCmd(),
		NewQueryChallengeCmd(),
		NewQueryChallengesCmd(),
		NewQueryParticipantsCmd(),
		NewQueryLeaderboardCmd(),
		NewQueryToolChallengesCmd(),
	)

	return cmd
}

// NewQueryParamsCmd queries challenges module params.
func NewQueryParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query challenges module parameters",
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

// NewQueryChallengeCmd queries a single challenge.
func NewQueryChallengeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "challenge [challenge-id]",
		Short: "Query a challenge by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Challenge(context.Background(), &types.QueryChallengeRequest{
				ChallengeId: strings.TrimSpace(args[0]),
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

// NewQueryChallengesCmd queries a filtered challenge list.
func NewQueryChallengesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "challenges",
		Short: "List challenges with optional status/type filters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			statusRaw, err := cmd.Flags().GetString(flagStatus)
			if err != nil {
				return err
			}
			status, err := parseChallengeStatus(statusRaw)
			if err != nil {
				return err
			}

			typeRaw, err := cmd.Flags().GetString(flagChallengeType)
			if err != nil {
				return err
			}
			challengeType, err := parseChallengeType(typeRaw)
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Challenges(context.Background(), &types.QueryChallengesRequest{
				Status:        status,
				ChallengeType: challengeType,
				Pagination:    pageReq,
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().String(flagStatus, "", "Status filter (draft|active|scoring|completed|cancelled)")
	cmd.Flags().String(flagChallengeType, "", "Type filter ("+challengeTypeHelp()+")")
	flags.AddPaginationFlagsToCmd(cmd, "challenges")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryParticipantsCmd queries participants for one challenge.
func NewQueryParticipantsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "participants [challenge-id]",
		Short: "List challenge participants",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Participants(context.Background(), &types.QueryParticipantsRequest{
				ChallengeId: strings.TrimSpace(args[0]),
				Pagination:  pageReq,
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, "participants")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryLeaderboardCmd queries rankings for a challenge.
func NewQueryLeaderboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "leaderboard [challenge-id]",
		Short: "Query leaderboard rankings for a challenge",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			limit, err := cmd.Flags().GetUint32(flagLimit)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.Leaderboard(context.Background(), &types.QueryLeaderboardRequest{
				ChallengeId: strings.TrimSpace(args[0]),
				Limit:       limit,
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}

	cmd.Flags().Uint32(flagLimit, 0, "Maximum number of rankings to return (0 = no explicit limit)")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryToolChallengesCmd queries all challenge participations for a tool.
func NewQueryToolChallengesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool-challenges [tool-id]",
		Short: "Query a tool's challenge history and rankings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			toolID := strings.TrimSpace(args[0])
			if toolID == "" {
				return fmt.Errorf("tool-id is required")
			}

			queryClient := types.NewQueryClient(clientCtx)
			resp, err := queryClient.ToolChallenges(context.Background(), &types.QueryToolChallengesRequest{
				ToolId:     toolID,
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(resp)
		},
	}

	flags.AddPaginationFlagsToCmd(cmd, "tool challenges")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
