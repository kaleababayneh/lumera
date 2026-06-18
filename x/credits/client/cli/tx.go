//go:build cosmos

// Package cli provides CLI transaction commands for the credits module.
package cli

import (
	"fmt"
	"strings"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

const (
	flagAmount        = "amount"
	flagSessionID     = "session-id"
	flagToolID        = "tool-id"
	flagLockID        = "lock-id"
	flagActualCost    = "actual-cost"
	flagReceiptID     = "receipt-id"
	flagTTLSeconds    = "ttl-seconds"
	flagQuoteID       = "quote-id"
	flagPolicyVersion = "policy-version"
	flagIntentHash    = "intent-hash"
	flagToolpackID    = "toolpack-id"

	flagReason = "reason"

	flagPublisher      = "publisher"
	flagReferrer       = "referrer"
	flagCacheHit       = "cache-hit"
	flagOriginToolID   = "origin-tool-id"
	flagSwapMinLacOut  = "min-lac-out"
	flagSwapMinLumeOut = "min-lume-out"

	flagMaxLockTTLSeconds                = "max-lock-ttl-seconds"
	flagDisputeWindowHours               = "dispute-window-hours"
	flagBurnRateSpendBps                 = "burn-rate-spend-bps"
	flagBurnRateAcqBps                   = "burn-rate-acq-bps"
	flagTargetAnnualDeflationBps         = "target-annual-deflation-bps"
	flagMinBurnRateSpendBps              = "min-burn-rate-spend-bps"
	flagMaxBurnRateSpendBps              = "max-burn-rate-spend-bps"
	flagBurnRateAdjustmentEpoch          = "burn-rate-adjustment-epoch"
	flagDeathSpiralSupplyContractionBps  = "death-spiral-supply-contraction-bps"
	flagDeathSpiralBurnRateCapBps        = "death-spiral-burn-rate-cap-bps"
	flagOverdraftMaxCreditLineToBondBps  = "overdraft-max-credit-line-to-bond-bps"
	flagOverdraftLiquidationThresholdBps = "overdraft-liquidation-threshold-bps"
	flagDisableOverdraft                 = "disable-overdraft"
	flagDisableBurnRateAdjustment        = "disable-burn-rate-adjustment"
	flagResetDisputeWindow               = "reset-dispute-window"
	flagSwapRate                         = "swap-rate"
)

// mustMarkFlagRequired marks a cobra flag required, panicking with the flag
// name on failure. Cobra only errors here when the flag is not registered —
// a programmer error at command-construction time — so a panic is the correct
// halt, but the flag name is essential for diagnosing the typo.
func mustMarkFlagRequired(cmd *cobra.Command, flag string) {
	if err := cmd.MarkFlagRequired(flag); err != nil {
		panic(fmt.Errorf("credits cli: mark flag %q required: %w", flag, err))
	}
}

// GetTxCmd bundles all credits tx commands under the module name.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Credits transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(NewSwapLUMEtoLACCmd())
	cmd.AddCommand(NewSwapLACtoLUMECmd())
	cmd.AddCommand(NewLockCreditsCmd())
	cmd.AddCommand(NewUnlockCreditsCmd())
	cmd.AddCommand(NewSettleCreditsCmd())
	cmd.AddCommand(NewUpdateParamsCmd())

	return cmd
}

// NewLockCreditsCmd locks credits for a tool invocation.
func NewLockCreditsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock-credits",
		Short: "Lock LAC credits for an invocation quote",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			amountStr, err := cmd.Flags().GetString(flagAmount)
			if err != nil {
				return err
			}
			coin, err := sdk.ParseCoinNormalized(strings.TrimSpace(amountStr))
			if err != nil {
				return fmt.Errorf("invalid --%s: %w", flagAmount, err)
			}

			sessionID, err := cmd.Flags().GetString(flagSessionID)
			if err != nil {
				return err
			}
			toolID, err := cmd.Flags().GetString(flagToolID)
			if err != nil {
				return err
			}
			ttl, err := cmd.Flags().GetUint64(flagTTLSeconds)
			if err != nil {
				return err
			}
			quoteID, err := cmd.Flags().GetString(flagQuoteID)
			if err != nil {
				return err
			}
			policyVersion, err := cmd.Flags().GetString(flagPolicyVersion)
			if err != nil {
				return err
			}
			intentHash, err := cmd.Flags().GetString(flagIntentHash)
			if err != nil {
				return err
			}
			toolpackID, err := cmd.Flags().GetString(flagToolpackID)
			if err != nil {
				return err
			}

			msg := &types.MsgLockCredits{
				Router:        clientCtx.GetFromAddress().String(),
				SessionId:     strings.TrimSpace(sessionID),
				ToolId:        strings.TrimSpace(toolID),
				Amount:        &basev1beta1.Coin{Denom: coin.Denom, Amount: coin.Amount.String()},
				QuoteId:       strings.TrimSpace(quoteID),
				PolicyVersion: strings.TrimSpace(policyVersion),
				IntentHash:    strings.TrimSpace(intentHash),
				TtlSeconds:    ttl,
				ToolpackId:    strings.TrimSpace(toolpackID),
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagAmount, "", "Amount to lock (e.g. 1000ulac)")
	cmd.Flags().String(flagSessionID, "", "Session identifier (UUID)")
	cmd.Flags().String(flagToolID, "", "Tool identifier being invoked")
	cmd.Flags().Uint64(flagTTLSeconds, 0, "Override lock TTL in seconds (0 uses module default)")
	cmd.Flags().String(flagQuoteID, "", "Quote identifier to associate with the lock")
	cmd.Flags().String(flagPolicyVersion, "", "Policy version snapshot (e.g. policy-v1)")
	cmd.Flags().String(flagIntentHash, "", "Optional intent hash (content-addressed)")
	cmd.Flags().String(flagToolpackID, "", "Optional toolpack id when invocation is via a Toolpack NFT")

	mustMarkFlagRequired(cmd, flagAmount)
	mustMarkFlagRequired(cmd, flagSessionID)
	mustMarkFlagRequired(cmd, flagToolID)
	mustMarkFlagRequired(cmd, flagQuoteID)
	mustMarkFlagRequired(cmd, flagPolicyVersion)
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewUnlockCreditsCmd unlocks a previously created lock.
func NewUnlockCreditsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock-credits",
		Short: "Unlock a previously locked credit reservation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			lockID, err := cmd.Flags().GetString(flagLockID)
			if err != nil {
				return err
			}

			reason, err := cmd.Flags().GetString(flagReason)
			if err != nil {
				return err
			}

			msg := &types.MsgUnlockCredits{
				Router: clientCtx.GetFromAddress().String(),
				LockId: strings.TrimSpace(lockID),
				Reason: strings.TrimSpace(reason),
			}
			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagLockID, "", "Lock identifier to unlock")
	cmd.Flags().String(flagReason, "", "Optional unlock reason (for observability/debugging)")
	mustMarkFlagRequired(cmd, flagLockID)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSettleCreditsCmd settles a lock with an actual cost and distributes funds.
func NewSettleCreditsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settle-credits",
		Short: "Settle a lock after invocation completes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			lockID, err := cmd.Flags().GetString(flagLockID)
			if err != nil {
				return err
			}
			actualCostStr, err := cmd.Flags().GetString(flagActualCost)
			if err != nil {
				return err
			}
			receiptID, err := cmd.Flags().GetString(flagReceiptID)
			if err != nil {
				return err
			}
			toolID, err := cmd.Flags().GetString(flagToolID)
			if err != nil {
				return err
			}

			actualCost, err := sdk.ParseCoinNormalized(strings.TrimSpace(actualCostStr))
			if err != nil {
				return fmt.Errorf("invalid --%s: %w", flagActualCost, err)
			}

			publisher, err := cmd.Flags().GetString(flagPublisher)
			if err != nil {
				return err
			}
			publisher = strings.TrimSpace(publisher)

			referrer, err := cmd.Flags().GetString(flagReferrer)
			if err != nil {
				return err
			}
			cacheHit, err := cmd.Flags().GetBool(flagCacheHit)
			if err != nil {
				return err
			}
			originToolID, err := cmd.Flags().GetString(flagOriginToolID)
			if err != nil {
				return err
			}
			originToolID = strings.TrimSpace(originToolID)
			if cacheHit && originToolID == "" {
				return fmt.Errorf("--%s is required when --%s is set", flagOriginToolID, flagCacheHit)
			}
			toolpackID, err := cmd.Flags().GetString(flagToolpackID)
			if err != nil {
				return err
			}

			msg := &types.MsgSettleCredits{
				Router:       clientCtx.GetFromAddress().String(),
				LockId:       strings.TrimSpace(lockID),
				ActualCost:   &basev1beta1.Coin{Denom: actualCost.Denom, Amount: actualCost.Amount.String()},
				ReceiptId:    strings.TrimSpace(receiptID),
				ToolId:       strings.TrimSpace(toolID),
				Publisher:    publisher,
				Referrer:     strings.TrimSpace(referrer),
				CacheHit:     cacheHit,
				OriginToolId: originToolID,
				ToolpackId:   strings.TrimSpace(toolpackID),
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagLockID, "", "Lock identifier to settle")
	cmd.Flags().String(flagActualCost, "", "Actual cost to charge against the lock (e.g. 12ulac)")
	cmd.Flags().String(flagReceiptID, "", "Receipt identifier for settlement auditability")
	cmd.Flags().String(flagToolID, "", "Tool identifier being settled")
	cmd.Flags().String(flagPublisher, "", "Publisher address receiving settlement")
	cmd.Flags().String(flagReferrer, "", "Optional referrer address")
	cmd.Flags().Bool(flagCacheHit, false, "Whether invocation result was served from CAC")
	cmd.Flags().String(flagOriginToolID, "", "Origin tool id for CAC royalties (required when --cache-hit)")
	cmd.Flags().String(flagToolpackID, "", "Optional toolpack id when invocation is via a Toolpack NFT")

	mustMarkFlagRequired(cmd, flagLockID)
	mustMarkFlagRequired(cmd, flagActualCost)
	mustMarkFlagRequired(cmd, flagReceiptID)
	mustMarkFlagRequired(cmd, flagToolID)
	mustMarkFlagRequired(cmd, flagPublisher)
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewSwapLUMEtoLACCmd swaps LUME tokens into LAC credits.
func NewSwapLUMEtoLACCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swap-lume-to-lac",
		Short: "Swap LUME tokens into LAC credits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			amountStr, err := cmd.Flags().GetString(flagAmount)
			if err != nil {
				return err
			}
			coin, err := sdk.ParseCoinNormalized(strings.TrimSpace(amountStr))
			if err != nil {
				return fmt.Errorf("invalid --%s: %w", flagAmount, err)
			}
			if coin.Denom != "ulume" {
				return fmt.Errorf("expected denom ulume, got %s", coin.Denom)
			}

			minOut, err := cmd.Flags().GetString(flagSwapMinLacOut)
			if err != nil {
				return err
			}

			msg := &types.MsgSwapLUMEtoLAC{
				Sender:     clientCtx.GetFromAddress().String(),
				LumeAmount: &basev1beta1.Coin{Denom: coin.Denom, Amount: coin.Amount.String()},
				MinLacOut:  strings.TrimSpace(minOut),
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagAmount, "", "Amount to swap (e.g. 10ulume)")
	cmd.Flags().String(flagSwapMinLacOut, "", "Minimum LAC to receive (slippage protection, raw amount)")
	mustMarkFlagRequired(cmd, flagAmount)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSwapLACtoLUMECmd swaps LAC credits back into LUME tokens.
func NewSwapLACtoLUMECmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swap-lac-to-lume",
		Short: "Swap LAC credits into LUME tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			amountStr, err := cmd.Flags().GetString(flagAmount)
			if err != nil {
				return err
			}
			coin, err := sdk.ParseCoinNormalized(strings.TrimSpace(amountStr))
			if err != nil {
				return fmt.Errorf("invalid --%s: %w", flagAmount, err)
			}

			minOut, err := cmd.Flags().GetString(flagSwapMinLumeOut)
			if err != nil {
				return err
			}

			msg := &types.MsgSwapLACtoLUME{
				Sender:     clientCtx.GetFromAddress().String(),
				LacAmount:  &basev1beta1.Coin{Denom: coin.Denom, Amount: coin.Amount.String()},
				MinLumeOut: strings.TrimSpace(minOut),
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagAmount, "", "Amount to swap (e.g. 10ulac)")
	cmd.Flags().String(flagSwapMinLumeOut, "", "Minimum LUME to receive (slippage protection, raw amount)")
	mustMarkFlagRequired(cmd, flagAmount)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUpdateParamsCmd builds a governance parameter update message for the credits module.
func NewUpdateParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params",
		Short: "Update credits module parameters (governance authority only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgUpdateParams{
				Authority: clientCtx.GetFromAddress().String(),
			}

			changed := false
			if cmd.Flags().Changed(flagMaxLockTTLSeconds) {
				val, err := cmd.Flags().GetUint32(flagMaxLockTTLSeconds)
				if err != nil {
					return err
				}
				msg.MaxLockTtlSeconds = val
				changed = true
			}
			if cmd.Flags().Changed(flagDisputeWindowHours) {
				val, err := cmd.Flags().GetUint32(flagDisputeWindowHours)
				if err != nil {
					return err
				}
				msg.DisputeWindowHours = val
				changed = true
			}
			if cmd.Flags().Changed(flagBurnRateSpendBps) {
				val, err := cmd.Flags().GetUint32(flagBurnRateSpendBps)
				if err != nil {
					return err
				}
				msg.BurnRateSpendBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagBurnRateAcqBps) {
				val, err := cmd.Flags().GetUint32(flagBurnRateAcqBps)
				if err != nil {
					return err
				}
				msg.BurnRateAcqBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagTargetAnnualDeflationBps) {
				val, err := cmd.Flags().GetUint32(flagTargetAnnualDeflationBps)
				if err != nil {
					return err
				}
				msg.TargetAnnualDeflationBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagMinBurnRateSpendBps) {
				val, err := cmd.Flags().GetUint32(flagMinBurnRateSpendBps)
				if err != nil {
					return err
				}
				msg.MinBurnRateSpendBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagMaxBurnRateSpendBps) {
				val, err := cmd.Flags().GetUint32(flagMaxBurnRateSpendBps)
				if err != nil {
					return err
				}
				msg.MaxBurnRateSpendBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagBurnRateAdjustmentEpoch) {
				val, err := cmd.Flags().GetUint32(flagBurnRateAdjustmentEpoch)
				if err != nil {
					return err
				}
				msg.BurnRateAdjustmentEpoch = val
				changed = true
			}
			if cmd.Flags().Changed(flagDeathSpiralSupplyContractionBps) {
				val, err := cmd.Flags().GetUint32(flagDeathSpiralSupplyContractionBps)
				if err != nil {
					return err
				}
				msg.DeathSpiralSupplyContractionBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagDeathSpiralBurnRateCapBps) {
				val, err := cmd.Flags().GetUint32(flagDeathSpiralBurnRateCapBps)
				if err != nil {
					return err
				}
				msg.DeathSpiralBurnRateCapBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagOverdraftMaxCreditLineToBondBps) {
				val, err := cmd.Flags().GetUint32(flagOverdraftMaxCreditLineToBondBps)
				if err != nil {
					return err
				}
				msg.OverdraftMaxCreditLineToBondBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagOverdraftLiquidationThresholdBps) {
				val, err := cmd.Flags().GetUint32(flagOverdraftLiquidationThresholdBps)
				if err != nil {
					return err
				}
				msg.OverdraftLiquidationThresholdBps = val
				changed = true
			}
			if cmd.Flags().Changed(flagDisableOverdraft) {
				val, err := cmd.Flags().GetBool(flagDisableOverdraft)
				if err != nil {
					return err
				}
				msg.DisableOverdraft = val
				if val {
					changed = true
				}
			}
			if cmd.Flags().Changed(flagDisableBurnRateAdjustment) {
				val, err := cmd.Flags().GetBool(flagDisableBurnRateAdjustment)
				if err != nil {
					return err
				}
				msg.DisableBurnRateAdjustment = val
				if val {
					changed = true
				}
			}
			if cmd.Flags().Changed(flagResetDisputeWindow) {
				val, err := cmd.Flags().GetBool(flagResetDisputeWindow)
				if err != nil {
					return err
				}
				msg.ResetDisputeWindow = val
				if val {
					changed = true
				}
			}
			if cmd.Flags().Changed(flagSwapRate) {
				val, err := cmd.Flags().GetString(flagSwapRate)
				if err != nil {
					return err
				}
				msg.SwapRate = strings.TrimSpace(val)
				changed = true
			}

			if !changed {
				return fmt.Errorf("at least one parameter flag must be set")
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().Uint32(flagMaxLockTTLSeconds, 0, "Upper bound (seconds) for credit locks")
	cmd.Flags().Uint32(flagDisputeWindowHours, 0, "Min hours before automatic settlement")
	cmd.Flags().Uint32(flagBurnRateSpendBps, 0, "Burn rate (basis points) applied on settlement spend")
	cmd.Flags().Uint32(flagBurnRateAcqBps, 0, "Burn rate (basis points) applied on LUME→LAC swaps")
	cmd.Flags().Uint32(flagTargetAnnualDeflationBps, 0, "Target annualized burn-driven deflation (basis points)")
	cmd.Flags().Uint32(flagMinBurnRateSpendBps, 0, "Minimum adaptive settlement burn rate (basis points)")
	cmd.Flags().Uint32(flagMaxBurnRateSpendBps, 0, "Maximum adaptive settlement burn rate (basis points)")
	cmd.Flags().Uint32(flagBurnRateAdjustmentEpoch, 0, "Evaluate adaptive burn every N blocks")
	cmd.Flags().Uint32(flagDeathSpiralSupplyContractionBps, 0, "30-day contraction threshold that activates death-spiral protection")
	cmd.Flags().Uint32(flagDeathSpiralBurnRateCapBps, 0, "Maximum settlement burn rate while death-spiral protection is active")
	cmd.Flags().Uint32(flagOverdraftMaxCreditLineToBondBps, 0, "Maximum overdraft credit line as basis points of bonded value")
	cmd.Flags().Uint32(flagOverdraftLiquidationThresholdBps, 0, "Overdraft utilization threshold that requires liquidation handling")
	cmd.Flags().Bool(flagDisableOverdraft, false, "Disable overdraft by zeroing both overdraft parameters")
	cmd.Flags().Bool(flagDisableBurnRateAdjustment, false, "Disable the adaptive burn controller by zeroing burn-rate-adjustment-epoch")
	cmd.Flags().Bool(flagResetDisputeWindow, false, "Reset dispute-window-hours to zero so the registry's canonical window applies")
	cmd.Flags().String(flagSwapRate, "", "Governance-published swap reference rate (decimal)")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
