package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/incentives/types"
)

// InitGenesis restores the incentives module state: params, tier configs,
// badges, the latest metric snapshots, and the badge-event audit trail.
func (k Keeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) {
	if gs == nil {
		gs = types.DefaultGenesis()
	}
	params := gs.Params
	if params == nil {
		params = types.DefaultParams()
	}
	if err := k.SetParams(ctx, params); err != nil {
		panic(err)
	}
	for _, cfg := range gs.TierConfigs {
		if cfg == nil {
			continue
		}
		if err := k.SetTierConfig(ctx, cfg); err != nil {
			panic(err)
		}
	}
	for _, badge := range gs.Badges {
		if badge == nil {
			continue
		}
		if err := k.SetBadge(ctx, badge); err != nil {
			panic(err)
		}
	}
	for _, snapshot := range gs.MetricSnapshots {
		if snapshot == nil {
			continue
		}
		if err := k.RecordMetrics(ctx, snapshot); err != nil {
			panic(err)
		}
	}
	for _, event := range gs.BadgeEvents {
		if event == nil {
			continue
		}
		if err := k.RestoreBadgeEvent(ctx, event); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis dumps the incentives module state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs := types.DefaultGenesis()
	gs.Params = k.GetParams(ctx)
	gs.TierConfigs = k.GetAllTierConfigs(ctx)
	gs.MetricSnapshots = k.GetAllMetricSnapshots(ctx)

	badges := make([]*types.Badge, 0)
	_ = k.IterateBadges(ctx, func(b *types.Badge) bool {
		if b != nil {
			badges = append(badges, b)
		}
		return false
	})
	gs.Badges = badges

	events := make([]*types.BadgeEvent, 0)
	_ = k.IterateBadgeEvents(ctx, func(e *types.BadgeEvent) bool {
		if e != nil {
			events = append(events, e)
		}
		return false
	})
	gs.BadgeEvents = events
	return gs
}
