package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/incentives/types"
)

// refreshUsageMetrics folds a tool's on-chain conduct — successful Proof-of-
// Service receipts and upheld disputes, read from the registry — into its metric
// snapshot's invocation / receipt-validity / dispute dimensions, so reputation
// responds to real behaviour rather than only externally-reported metrics. The
// off-chain-reported dimensions (uptime, latency, SBOM, SLSA, throughput, ...)
// on an existing snapshot are preserved. This is the trust-graph self-feed: the
// flywheel's usage signal drives the reputation that gates discovery.
func (k Keeper) refreshUsageMetrics(ctx context.Context, toolID string) {
	if k.registryKeeper == nil {
		return
	}
	successful, disputed, err := k.registryKeeper.GetToolUsage(ctx, toolID)
	if err != nil {
		k.Logger(sdk.UnwrapSDKContext(ctx)).Error("get tool usage failed", "tool", toolID, "error", err)
		return
	}
	total := successful + disputed
	if total == 0 {
		return // no on-chain history yet; leave any reported metrics untouched
	}

	snap, found := k.GetMetrics(ctx, toolID)
	if !found || snap == nil {
		snap = &types.MetricSnapshot{ToolId: toolID}
	}
	bps := func(n uint64) uint32 { return uint32(n * 10000 / total) }
	snap.ToolId = toolID
	snap.TotalInvocations = total
	snap.SuccessfulInvocations = successful
	snap.FailedInvocations = disputed
	snap.SuccessRateBps = bps(successful)
	snap.ReceiptValidityBps = bps(successful)
	snap.DisputeRateBps = bps(disputed)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	snap.BlockHeight = sdkCtx.BlockHeight()
	snap.Timestamp = sdkCtx.BlockTime()
	if err := k.RecordMetrics(ctx, snap); err != nil {
		k.Logger(sdkCtx).Error("refresh usage metrics failed", "tool", toolID, "error", err)
	}
}
