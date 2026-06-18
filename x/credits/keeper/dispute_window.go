
package keeper

import (
	"context"
	"time"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

type registryDisputeWindowProvider interface {
	GetDisputeWindowSeconds(ctx context.Context) uint32
}

func creditsDisputeWindow(params *types.Params) time.Duration {
	return types.DisputeWindowDuration(params)
}

func (k Keeper) SettlementDisputeWindow(ctx context.Context) time.Duration {
	if provider, ok := k.registryKeeper.(registryDisputeWindowProvider); ok {
		if disputeWindowSeconds := provider.GetDisputeWindowSeconds(ctx); disputeWindowSeconds > 0 {
			return time.Duration(disputeWindowSeconds) * time.Second
		}
	}
	return creditsDisputeWindow(k.GetParams(ctx))
}
