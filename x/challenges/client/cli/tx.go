// Package cli provides Cosmos CLI commands for the challenges module.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

const (
	flagTitle                = "title"
	flagDescription          = "description"
	flagChallengeType        = "type"
	flagPrizePool            = "prize-pool"
	flagEntryFee             = "entry-fee"
	flagLatencyWeightBPS     = "latency-weight-bps"
	flagCostWeightBPS        = "cost-weight-bps"
	flagAccuracyWeightBPS    = "accuracy-weight-bps"
	flagReliabilityWeightBPS = "reliability-weight-bps"
	flagConformanceWeightBPS = "conformance-weight-bps"
	flagWinnerSharesBPS      = "winner-shares-bps"
	flagRequiredCategories   = "required-categories"
	flagMinBadgeTier         = "min-badge-tier"
	flagMaxParticipants      = "max-participants"
	flagStartsAt             = "starts-at"
	flagEndsAt               = "ends-at"
	flagChallengeID          = "challenge-id"
	flagToolID               = "tool-id"
	flagLatencyScore         = "latency-score"
	flagCostScore            = "cost-score"
	flagAccuracyScore        = "accuracy-score"
	flagReliabilityScore     = "reliability-score"
	flagConformanceScore     = "conformance-score"
	flagGoldenTaskHash       = "golden-task-result-hash"
	flagParamsFile           = "params-file"
)

const maxChallengesParamsFileBytes int64 = 1 << 20

// GetTxCmd returns the challenges transaction command tree.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Challenges transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewCreateChallengeCmd(),
		NewJoinChallengeCmd(),
		NewSubmitResultCmd(),
		NewActivateChallengeCmd(),
		NewCancelChallengeCmd(),
		NewUpdateParamsCmd(),
	)

	return cmd
}

// NewCreateChallengeCmd creates a grand challenge.
func NewCreateChallengeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-challenge",
		Short: "Create a new challenge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			title, err := cmd.Flags().GetString(flagTitle)
			if err != nil {
				return err
			}
			description, err := cmd.Flags().GetString(flagDescription)
			if err != nil {
				return err
			}
			challengeTypeRaw, err := cmd.Flags().GetString(flagChallengeType)
			if err != nil {
				return err
			}
			challengeType, err := parseChallengeType(challengeTypeRaw)
			if err != nil {
				return err
			}
			if challengeType == types.ChallengeType_CHALLENGE_TYPE_UNSPECIFIED {
				return fmt.Errorf("--%s is required", flagChallengeType)
			}

			prizePool, err := readCoinFlag(cmd, flagPrizePool)
			if err != nil {
				return err
			}
			entryFee, err := readCoinFlag(cmd, flagEntryFee)
			if err != nil {
				return err
			}

			scoringWeights, err := readChallengeScoringWeights(cmd, challengeType)
			if err != nil {
				return err
			}
			prizeDistribution, err := readChallengePrizeDistribution(cmd, challengeType)
			if err != nil {
				return err
			}

			requiredCategoriesRaw, err := cmd.Flags().GetString(flagRequiredCategories)
			if err != nil {
				return err
			}
			requiredCategories := parseCSV(requiredCategoriesRaw)

			minBadgeTier, err := cmd.Flags().GetUint32(flagMinBadgeTier)
			if err != nil {
				return err
			}
			maxParticipants, err := cmd.Flags().GetUint32(flagMaxParticipants)
			if err != nil {
				return err
			}

			startsAtRaw, err := cmd.Flags().GetString(flagStartsAt)
			if err != nil {
				return err
			}
			startsAt, err := parseRFC3339Timestamp(startsAtRaw)
			if err != nil {
				return fmt.Errorf("invalid --%s: %w", flagStartsAt, err)
			}
			endsAtRaw, err := cmd.Flags().GetString(flagEndsAt)
			if err != nil {
				return err
			}
			endsAt, err := parseRFC3339Timestamp(endsAtRaw)
			if err != nil {
				return fmt.Errorf("invalid --%s: %w", flagEndsAt, err)
			}
			if startsAt.IsZero() || endsAt.IsZero() {
				return fmt.Errorf("--%s and --%s are required", flagStartsAt, flagEndsAt)
			}

			msg := &types.MsgCreateChallenge{
				Creator:       clientCtx.GetFromAddress().String(),
				Title:         strings.TrimSpace(title),
				Description:   strings.TrimSpace(description),
				ChallengeType: challengeType,
				PrizePool:          prizePool,
				EntryFee:           entryFee,
				ScoringWeights:     scoringWeights,
				PrizeDistribution:  prizeDistribution,
				RequiredCategories: requiredCategories,
				MinBadgeTier:       minBadgeTier,
				MaxParticipants:    maxParticipants,
				StartsAt:           startsAt,
				EndsAt:             endsAt,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagTitle, "", "Challenge title")
	cmd.Flags().String(flagDescription, "", "Challenge description")
	cmd.Flags().String(flagChallengeType, "", "Challenge type ("+challengeTypeHelp()+")")
	cmd.Flags().String(flagPrizePool, "", "Escrowed prize pool coin (e.g. 1000000ulac)")
	cmd.Flags().String(flagEntryFee, "", "Entry fee coin (e.g. 1000ulac)")
	cmd.Flags().Uint32(flagLatencyWeightBPS, 2000, "Latency weight in basis points")
	cmd.Flags().Uint32(flagCostWeightBPS, 2000, "Cost weight in basis points")
	cmd.Flags().Uint32(flagAccuracyWeightBPS, 2000, "Accuracy weight in basis points")
	cmd.Flags().Uint32(flagReliabilityWeightBPS, 2000, "Reliability weight in basis points")
	cmd.Flags().Uint32(flagConformanceWeightBPS, 2000, "Conformance weight in basis points")
	cmd.Flags().String(flagWinnerSharesBPS, "5000,3000,2000", "Comma-separated winner share basis points")
	cmd.Flags().String(flagRequiredCategories, "", "Comma-separated required tool categories")
	cmd.Flags().Uint32(flagMinBadgeTier, 0, "Minimum badge tier required")
	cmd.Flags().Uint32(flagMaxParticipants, 0, "Maximum participant count (0 means no hard limit)")
	cmd.Flags().String(flagStartsAt, "", "Challenge start timestamp (RFC3339)")
	cmd.Flags().String(flagEndsAt, "", "Challenge end timestamp (RFC3339)")

	_ = cmd.MarkFlagRequired(flagTitle)
	_ = cmd.MarkFlagRequired(flagChallengeType)
	_ = cmd.MarkFlagRequired(flagPrizePool)
	_ = cmd.MarkFlagRequired(flagEntryFee)
	_ = cmd.MarkFlagRequired(flagStartsAt)
	_ = cmd.MarkFlagRequired(flagEndsAt)

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewJoinChallengeCmd joins a challenge as a tool participant.
func NewJoinChallengeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "join-challenge",
		Short: "Join a challenge with a tool id",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			challengeID, err := cmd.Flags().GetString(flagChallengeID)
			if err != nil {
				return err
			}
			toolID, err := cmd.Flags().GetString(flagToolID)
			if err != nil {
				return err
			}

			msg := &types.MsgJoinChallenge{
				Publisher:   clientCtx.GetFromAddress().String(),
				ChallengeId: strings.TrimSpace(challengeID),
				ToolId:      strings.TrimSpace(toolID),
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagChallengeID, "", "Challenge identifier")
	cmd.Flags().String(flagToolID, "", "Tool identifier")
	_ = cmd.MarkFlagRequired(flagChallengeID)
	_ = cmd.MarkFlagRequired(flagToolID)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSubmitResultCmd submits scoring metrics for a tool.
func NewSubmitResultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit-result",
		Short: "Submit challenge scores for a tool",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			challengeID, err := cmd.Flags().GetString(flagChallengeID)
			if err != nil {
				return err
			}
			toolID, err := cmd.Flags().GetString(flagToolID)
			if err != nil {
				return err
			}

			latencyScore, err := cmd.Flags().GetUint32(flagLatencyScore)
			if err != nil {
				return err
			}
			costScore, err := cmd.Flags().GetUint32(flagCostScore)
			if err != nil {
				return err
			}
			accuracyScore, err := cmd.Flags().GetUint32(flagAccuracyScore)
			if err != nil {
				return err
			}
			reliabilityScore, err := cmd.Flags().GetUint32(flagReliabilityScore)
			if err != nil {
				return err
			}
			conformanceScore, err := cmd.Flags().GetUint32(flagConformanceScore)
			if err != nil {
				return err
			}
			goldenHash, err := cmd.Flags().GetString(flagGoldenTaskHash)
			if err != nil {
				return err
			}

			msg := &types.MsgSubmitResult{
				Submitter:            clientCtx.GetFromAddress().String(),
				ChallengeId:          strings.TrimSpace(challengeID),
				ToolId:               strings.TrimSpace(toolID),
				LatencyScore:         latencyScore,
				CostScore:            costScore,
				AccuracyScore:        accuracyScore,
				ReliabilityScore:     reliabilityScore,
				ConformanceScore:     conformanceScore,
				GoldenTaskResultHash: strings.TrimSpace(goldenHash),
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagChallengeID, "", "Challenge identifier")
	cmd.Flags().String(flagToolID, "", "Tool identifier")
	cmd.Flags().Uint32(flagLatencyScore, 0, "Latency score (0-10000)")
	cmd.Flags().Uint32(flagCostScore, 0, "Cost score (0-10000)")
	cmd.Flags().Uint32(flagAccuracyScore, 0, "Accuracy score (0-10000)")
	cmd.Flags().Uint32(flagReliabilityScore, 0, "Reliability score (0-10000)")
	cmd.Flags().Uint32(flagConformanceScore, 0, "Conformance score (0-10000)")
	cmd.Flags().String(flagGoldenTaskHash, "", "Golden-task result hash")

	_ = cmd.MarkFlagRequired(flagChallengeID)
	_ = cmd.MarkFlagRequired(flagToolID)
	_ = cmd.MarkFlagRequired(flagGoldenTaskHash)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewActivateChallengeCmd activates a draft challenge.
func NewActivateChallengeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activate-challenge",
		Short: "Activate a challenge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			challengeID, err := cmd.Flags().GetString(flagChallengeID)
			if err != nil {
				return err
			}

			msg := &types.MsgActivateChallenge{
				Creator:     clientCtx.GetFromAddress().String(),
				ChallengeId: strings.TrimSpace(challengeID),
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagChallengeID, "", "Challenge identifier")
	_ = cmd.MarkFlagRequired(flagChallengeID)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewCancelChallengeCmd cancels a challenge.
func NewCancelChallengeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel-challenge",
		Short: "Cancel a challenge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			challengeID, err := cmd.Flags().GetString(flagChallengeID)
			if err != nil {
				return err
			}

			msg := &types.MsgCancelChallenge{
				Creator:     clientCtx.GetFromAddress().String(),
				ChallengeId: strings.TrimSpace(challengeID),
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagChallengeID, "", "Challenge identifier")
	_ = cmd.MarkFlagRequired(flagChallengeID)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUpdateParamsCmd updates module params (governance authority only).
func NewUpdateParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params",
		Short: "Update challenges module params from JSON file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			paramsPath, err := cmd.Flags().GetString(flagParamsFile)
			if err != nil {
				return err
			}
			paramsPath = strings.TrimSpace(paramsPath)
			if paramsPath == "" {
				return fmt.Errorf("--%s is required", flagParamsFile)
			}

			raw, err := readChallengesParamsFile(paramsPath)
			if err != nil {
				return err
			}

			var params types.Params
			if err := json.Unmarshal(raw, &params); err != nil {
				return fmt.Errorf("decode params file: %w", err)
			}

			msg := &types.MsgUpdateParams{
				Authority: clientCtx.GetFromAddress().String(),
				Params:    &params,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagParamsFile, "", "Path to JSON file containing lumera.challenges.v1.Params")
	_ = cmd.MarkFlagRequired(flagParamsFile)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func readChallengesParamsFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read params file: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect params file: %w", err)
	}
	if info.Mode().IsRegular() && info.Size() > maxChallengesParamsFileBytes {
		return nil, fmt.Errorf("params file exceeds %d-byte limit", maxChallengesParamsFileBytes)
	}

	raw, err := io.ReadAll(io.LimitReader(file, maxChallengesParamsFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read params file: %w", err)
	}
	if int64(len(raw)) > maxChallengesParamsFileBytes {
		return nil, fmt.Errorf("params file exceeds %d-byte limit", maxChallengesParamsFileBytes)
	}
	return raw, nil
}

func readCoinFlag(cmd *cobra.Command, name string) (sdk.Coin, error) {
	raw, err := cmd.Flags().GetString(name)
	if err != nil {
		return sdk.Coin{}, err
	}
	coin, err := sdk.ParseCoinNormalized(strings.TrimSpace(raw))
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid --%s: %w", name, err)
	}
	return coin, nil
}

func readScoringWeights(cmd *cobra.Command) (*types.ScoringWeight, error) {
	latency, err := cmd.Flags().GetUint32(flagLatencyWeightBPS)
	if err != nil {
		return nil, err
	}
	cost, err := cmd.Flags().GetUint32(flagCostWeightBPS)
	if err != nil {
		return nil, err
	}
	accuracy, err := cmd.Flags().GetUint32(flagAccuracyWeightBPS)
	if err != nil {
		return nil, err
	}
	reliability, err := cmd.Flags().GetUint32(flagReliabilityWeightBPS)
	if err != nil {
		return nil, err
	}
	conformance, err := cmd.Flags().GetUint32(flagConformanceWeightBPS)
	if err != nil {
		return nil, err
	}

	total := uint64(latency) + uint64(cost) + uint64(accuracy) + uint64(reliability) + uint64(conformance)
	if total != 10_000 {
		return nil, fmt.Errorf("scoring weights must sum to 10000 bps, got %d", total)
	}

	return &types.ScoringWeight{
		LatencyWeightBps:     latency,
		CostWeightBps:        cost,
		AccuracyWeightBps:    accuracy,
		ReliabilityWeightBps: reliability,
		ConformanceWeightBps: conformance,
	}, nil
}

func readChallengeScoringWeights(cmd *cobra.Command, challengeType types.ChallengeType) (*types.ScoringWeight, error) {
	scoringWeights, err := readScoringWeights(cmd)
	if err != nil {
		return nil, err
	}
	if types.IsProtocolChallengeType(challengeType) && !challengeTypeHasExplicitScoringFlags(cmd) {
		return nil, nil
	}
	return scoringWeights, nil
}

func readChallengePrizeDistribution(cmd *cobra.Command, challengeType types.ChallengeType) (*types.PrizeDistribution, error) {
	sharesRaw, err := cmd.Flags().GetString(flagWinnerSharesBPS)
	if err != nil {
		return nil, err
	}
	winnerShares, err := parseUint32CSV(sharesRaw)
	if err != nil {
		return nil, err
	}
	if len(winnerShares) == 0 {
		return nil, fmt.Errorf("--%s must include at least one entry", flagWinnerSharesBPS)
	}
	if types.IsProtocolChallengeType(challengeType) && !cmd.Flags().Changed(flagWinnerSharesBPS) {
		return nil, nil
	}
	return &types.PrizeDistribution{WinnerSharesBps: winnerShares}, nil
}

func parseUint32CSV(raw string) ([]uint32, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	values := make([]uint32, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q: %w", value, err)
		}
		values = append(values, uint32(parsed))
	}
	return values, nil
}
