// Package cli provides Cosmos CLI commands for the challenges module.
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

func parseChallengeType(raw string) (types.ChallengeType, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "any") || strings.EqualFold(trimmed, "unspecified") {
		return types.ChallengeType_CHALLENGE_TYPE_UNSPECIFIED, nil
	}

	normalized := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(trimmed, "-", "_"), " ", "_"))
	if !strings.HasPrefix(normalized, "CHALLENGE_TYPE_") {
		normalized = "CHALLENGE_TYPE_" + normalized
	}

	value, ok := types.ChallengeType_value[normalized]
	if !ok {
		return types.ChallengeType_CHALLENGE_TYPE_UNSPECIFIED, fmt.Errorf("unknown challenge type: %q", raw)
	}
	return types.ChallengeType(value), nil
}

func challengeTypeHelp() string {
	return "performance|quality|conformance|composite|identity_attestation|slo_probe|tee_report|receipt_replay"
}

func challengeTypeHasExplicitScoringFlags(cmd *cobra.Command) bool {
	for _, flagName := range []string{
		flagLatencyWeightBPS,
		flagCostWeightBPS,
		flagAccuracyWeightBPS,
		flagReliabilityWeightBPS,
		flagConformanceWeightBPS,
	} {
		if cmd.Flags().Changed(flagName) {
			return true
		}
	}
	return false
}

func parseChallengeStatus(raw string) (types.ChallengeStatus, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "any") || strings.EqualFold(trimmed, "unspecified") {
		return types.ChallengeStatus_CHALLENGE_STATUS_UNSPECIFIED, nil
	}

	normalized := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(trimmed, "-", "_"), " ", "_"))
	if !strings.HasPrefix(normalized, "CHALLENGE_STATUS_") {
		normalized = "CHALLENGE_STATUS_" + normalized
	}

	value, ok := types.ChallengeStatus_value[normalized]
	if !ok {
		return types.ChallengeStatus_CHALLENGE_STATUS_UNSPECIFIED, fmt.Errorf("unknown challenge status: %q", raw)
	}
	return types.ChallengeStatus(value), nil
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseRFC3339Timestamp(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected RFC3339 timestamp, got %q: %w", raw, err)
	}
	return parsed.UTC(), nil
}
