package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/internal/logging"
	"github.com/LumeraProtocol/lumera/x/cac/types"
)

const (
	flagToolID        = "tool-id"
	flagRequestHash   = "request-hash"
	flagTTLSeconds    = "ttl"
	flagDeterministic = "deterministic"
	flagRoyaltyElig   = "royalty-eligible"
	flagTargetType    = "target-type"
	flagReason        = "reason"
	flagCascade       = "cascade"
	flagTargetTier    = "target-tier"
)

// GetTxCmd returns the root tx command for the cac module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "CAC cache transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewCacheStoreCmd())
	cmd.AddCommand(NewCacheInvalidateCmd())
	cmd.AddCommand(NewPromoteTierCmd())

	return cmd
}

// NewCacheStoreCmd builds a cache-store transaction command.
func NewCacheStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache-store [content-file]",
		Short: "Store content in the content-addressed cache",
		Long: `Store content in the CAC module. The content is read from the specified
file path. The request-hash and tool-id flags are required.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			content, err := readCacheStoreContentFile(args[0])
			if err != nil {
				return err
			}

			toolID, err := cmd.Flags().GetString(flagToolID)
			if err != nil {
				return err
			}
			requestHash, err := cmd.Flags().GetString(flagRequestHash)
			if err != nil {
				return err
			}
			ttl, err := cmd.Flags().GetUint64(flagTTLSeconds)
			if err != nil {
				return err
			}
			deterministic, err := cmd.Flags().GetBool(flagDeterministic)
			if err != nil {
				return err
			}
			royaltyEligible, err := cmd.Flags().GetBool(flagRoyaltyElig)
			if err != nil {
				return err
			}

			msg := &types.MsgCacheStore{
				Publisher:       clientCtx.GetFromAddress().String(),
				ToolId:          toolID,
				RequestHash:     requestHash,
				Content:         content,
				TtlSeconds:      ttl,
				IsDeterministic: deterministic,
				RoyaltyEligible: royaltyEligible,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagToolID, "", "Tool identifier producing the content (required)")
	cmd.Flags().String(flagRequestHash, "", "Blake3 hash of the original request (hex, required)")
	cmd.Flags().Uint64(flagTTLSeconds, 0, "Time-to-live in seconds (0 = use default)")
	cmd.Flags().Bool(flagDeterministic, false, "Tool produces deterministic output")
	cmd.Flags().Bool(flagRoyaltyElig, false, "Cache hits should trigger royalty payments")

	_ = cmd.MarkFlagRequired(flagToolID)
	_ = cmd.MarkFlagRequired(flagRequestHash)
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewCacheInvalidateCmd builds a cache-invalidate transaction command.
func NewCacheInvalidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache-invalidate [target-value]",
		Short: "Invalidate cache entries",
		Long: `Invalidate cache entries by target type. The target-value argument is
interpreted based on the --target-type flag:

  content-hash  - a specific content hash
  tool-id       - all entries for a tool
  request-hash  - entries matching a request hash`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			targetTypeStr, err := cmd.Flags().GetString(flagTargetType)
			if err != nil {
				return err
			}
			targetType, err := parseInvalidationTargetType(targetTypeStr)
			if err != nil {
				return err
			}

			reason, err := cmd.Flags().GetString(flagReason)
			if err != nil {
				return err
			}
			cascade, err := cmd.Flags().GetBool(flagCascade)
			if err != nil {
				return err
			}

			msg := &types.MsgCacheInvalidate{
				Requester:   clientCtx.GetFromAddress().String(),
				TargetType:  targetType,
				TargetValue: args[0],
				Reason:      reason,
				Cascade:     cascade,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagTargetType, "content-hash", "Invalidation target type: content-hash|tool-id|request-hash")
	cmd.Flags().String(flagReason, "", "Reason for invalidation")
	cmd.Flags().Bool(flagCascade, false, "Cascade invalidation to dependent entries")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewPromoteTierCmd builds a promote-tier transaction command.
func NewPromoteTierCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promote-tier [content-hash]",
		Short: "Promote a cache entry to a higher tier",
		Long: `Promote a cache entry to a higher-performance cache tier.

Available tiers: l1-memory, l2-ssd, l3-hdd, l4-cold`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			tierStr, err := cmd.Flags().GetString(flagTargetTier)
			if err != nil {
				return err
			}
			tier, err := parseCacheTier(tierStr)
			if err != nil {
				return err
			}

			msg := &types.MsgPromoteTier{
				Authority:   clientCtx.GetFromAddress().String(),
				ContentHash: args[0],
				TargetTier:  tier,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagTargetTier, "l1-memory", "Target cache tier: l1-memory|l2-ssd|l3-hdd|l4-cold")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func readCacheStoreContentFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read content file: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect content file: %w", err)
	}
	if info.Mode().IsRegular() && info.Size() > types.MaxCachedContentBytes {
		return nil, fmt.Errorf("content file exceeds %d-byte limit", types.MaxCachedContentBytes)
	}

	content, err := io.ReadAll(io.LimitReader(file, types.MaxCachedContentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read content file: %w", err)
	}
	if len(content) > types.MaxCachedContentBytes {
		return nil, fmt.Errorf("content file exceeds %d-byte limit", types.MaxCachedContentBytes)
	}
	return content, nil
}

func parseInvalidationTargetType(s string) (types.InvalidationTargetType, error) {
	switch strings.ToLower(s) {
	case "content-hash":
		return types.InvalidationTargetType_INVALIDATION_TARGET_TYPE_CONTENT_HASH, nil
	case "tool-id":
		return types.InvalidationTargetType_INVALIDATION_TARGET_TYPE_TOOL_ID, nil
	case "request-hash":
		return types.InvalidationTargetType_INVALIDATION_TARGET_TYPE_REQUEST_HASH, nil
	default:
		return types.InvalidationTargetType_INVALIDATION_TARGET_TYPE_UNSPECIFIED,
			fmt.Errorf("unknown or unsupported invalidation target type %q; expected content-hash|tool-id|request-hash", redactCACCLIDiagnostic(s))
	}
}

func parseCacheTier(s string) (types.CacheTier, error) {
	switch strings.ToLower(s) {
	case "l1-memory", "l1":
		return types.CacheTier_CACHE_TIER_L1_MEMORY, nil
	case "l2-ssd", "l2":
		return types.CacheTier_CACHE_TIER_L2_SSD, nil
	case "l3-hdd", "l3":
		return types.CacheTier_CACHE_TIER_L3_HDD, nil
	case "l4-cold", "l4":
		return types.CacheTier_CACHE_TIER_L4_COLD, nil
	default:
		return types.CacheTier_CACHE_TIER_UNSPECIFIED,
			fmt.Errorf("unknown cache tier %q; expected l1-memory|l2-ssd|l3-hdd|l4-cold", redactCACCLIDiagnostic(s))
	}
}

func redactCACCLIDiagnostic(value string) string {
	return logging.RedactPII(strings.TrimSpace(value))
}
