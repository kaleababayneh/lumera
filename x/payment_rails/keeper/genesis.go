package keeper

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/payment_rails/types"
)

// InitGenesis initializes module state from genesis.
func (k Keeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if err := gs.Validate(); err != nil {
		return fmt.Errorf("invalid genesis: %w", err)
	}
	if err := k.state.Params.Set(ctx, gs.Params); err != nil {
		return err
	}

	var maxDepositSeq uint64
	for _, dep := range gs.Deposits {
		if dep == nil {
			continue
		}
		if err := k.state.Deposits.Set(ctx, dep.DepositId, dep); err != nil {
			return err
		}
		if err := k.state.DepositsByUser.Set(ctx, collections.Join(dep.User, dep.DepositId)); err != nil {
			return fmt.Errorf("genesis: index deposit by user: %w", err)
		}
		if seq, err := parseDepositSeq(dep.DepositId); err == nil && seq > maxDepositSeq {
			maxDepositSeq = seq
		}
		if dep.RequestId != "" {
			if err := k.state.DepositByRequest.Set(ctx, dep.RequestId, dep.DepositId); err != nil {
				return fmt.Errorf("genesis: index deposit by request: %w", err)
			}
		}
	}
	for _, pricing := range gs.Pricings {
		if pricing == nil {
			continue
		}
		if err := k.state.Pricings.Set(ctx, pricing.DepositId, pricing); err != nil {
			return err
		}
	}
	for _, mint := range gs.Mints {
		if mint == nil {
			continue
		}
		if err := k.state.Mints.Set(ctx, mint.DepositId, mint); err != nil {
			return err
		}
	}

	var maxWithdrawSeq uint64
	for _, wd := range gs.Withdrawals {
		if wd == nil {
			continue
		}
		if err := k.state.Withdrawals.Set(ctx, wd.WithdrawId, wd); err != nil {
			return err
		}
		if err := k.state.WithdrawalsByUser.Set(ctx, collections.Join(wd.User, wd.WithdrawId)); err != nil {
			return fmt.Errorf("genesis: index withdraw by user: %w", err)
		}
		if seq, err := parseWithdrawSeq(wd.WithdrawId); err == nil && seq > maxWithdrawSeq {
			maxWithdrawSeq = seq
		}
		if wd.RequestId != "" {
			if err := k.state.WithdrawByRequest.Set(ctx, wd.RequestId, wd.WithdrawId); err != nil {
				return fmt.Errorf("genesis: index withdraw by request: %w", err)
			}
		}
	}

	if maxDepositSeq > 0 {
		if err := k.state.DepositSeq.Set(ctx, maxDepositSeq+1); err != nil {
			return fmt.Errorf("genesis: set deposit sequence: %w", err)
		}
	}
	if maxWithdrawSeq > 0 {
		if err := k.state.WithdrawSeq.Set(ctx, maxWithdrawSeq+1); err != nil {
			return fmt.Errorf("genesis: set withdraw sequence: %w", err)
		}
	}

	// Import IBC settlements and rebuild indexes
	var maxSettlementSeq uint64
	for _, stl := range gs.IbcSettlements {
		if stl == nil {
			continue
		}
		if err := k.state.IBCSettlements.Set(ctx, stl.SettlementId, stl); err != nil {
			return fmt.Errorf("genesis: import ibc settlement %s: %w", stl.SettlementId, err)
		}
		if stl.ReferenceId != "" {
			if err := k.state.IBCSettlementByRef.Set(ctx, stl.ReferenceId, stl.SettlementId); err != nil {
				return fmt.Errorf("genesis: index ibc settlement by ref: %w", err)
			}
		}
		if stl.RequestId != "" {
			if err := k.state.IBCSettlementByRequest.Set(ctx, stl.RequestId, stl.SettlementId); err != nil {
				return fmt.Errorf("genesis: index ibc settlement by request: %w", err)
			}
		}
		if seq, err := parseSettlementSeq(stl.SettlementId); err == nil && seq > maxSettlementSeq {
			maxSettlementSeq = seq
		}
	}
	if maxSettlementSeq > 0 {
		if err := k.state.IBCSettlementSeq.Set(ctx, maxSettlementSeq+1); err != nil {
			return fmt.Errorf("genesis: set ibc settlement sequence: %w", err)
		}
	}

	// Import rate limit windows
	for _, entry := range gs.UserHourly {
		if entry == nil || entry.WindowStart.IsZero() || entry.Window == nil {
			continue
		}
		key := collections.Join(entry.User, entry.WindowStart)
		if err := k.state.UserHourly.Set(ctx, key, entry.Window); err != nil {
			return fmt.Errorf("genesis: import user hourly: %w", err)
		}
	}
	for _, entry := range gs.UserDaily {
		if entry == nil || entry.WindowStart.IsZero() || entry.Window == nil {
			continue
		}
		key := collections.Join(entry.User, entry.WindowStart)
		if err := k.state.UserDaily.Set(ctx, key, entry.Window); err != nil {
			return fmt.Errorf("genesis: import user daily: %w", err)
		}
	}

	return nil
}

// ExportGenesis exports module state to genesis.
func (k Keeper) ExportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	params, err := k.state.Params.Get(ctx)
	if err != nil || params == nil {
		params = types.DefaultParams()
	}

	gen := &types.GenesisState{
		Params:         params,
		Deposits:       []*types.DepositRecord{},
		Pricings:       []*types.PricingRecord{},
		Mints:          []*types.MintRecord{},
		Withdrawals:    []*types.WithdrawRecord{},
		IbcSettlements: []*types.IBCSettlementRecord{},
		UserHourly:     []*types.UserRateLimitEntry{},
		UserDaily:      []*types.UserRateLimitEntry{},
	}

	err = k.state.Deposits.Walk(ctx, nil, func(_ string, dep *types.DepositRecord) (bool, error) {
		if dep != nil {
			gen.Deposits = append(gen.Deposits, dep)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	err = k.state.Pricings.Walk(ctx, nil, func(_ string, pricing *types.PricingRecord) (bool, error) {
		if pricing != nil {
			gen.Pricings = append(gen.Pricings, pricing)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	err = k.state.Mints.Walk(ctx, nil, func(_ string, mint *types.MintRecord) (bool, error) {
		if mint != nil {
			gen.Mints = append(gen.Mints, mint)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	err = k.state.Withdrawals.Walk(ctx, nil, func(_ string, wd *types.WithdrawRecord) (bool, error) {
		if wd != nil {
			gen.Withdrawals = append(gen.Withdrawals, wd)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	err = k.state.IBCSettlements.Walk(ctx, nil, func(_ string, stl *types.IBCSettlementRecord) (bool, error) {
		if stl != nil {
			gen.IbcSettlements = append(gen.IbcSettlements, stl)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	err = k.state.UserHourly.Walk(ctx, nil, func(key collections.Pair[string, time.Time], w *types.TopupWindow) (bool, error) {
		if w != nil {
			gen.UserHourly = append(gen.UserHourly, &types.UserRateLimitEntry{
				User:        key.K1(),
				WindowStart: key.K2(),
				Window:      w,
			})
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	err = k.state.UserDaily.Walk(ctx, nil, func(key collections.Pair[string, time.Time], w *types.TopupWindow) (bool, error) {
		if w != nil {
			gen.UserDaily = append(gen.UserDaily, &types.UserRateLimitEntry{
				User:        key.K1(),
				WindowStart: key.K2(),
				Window:      w,
			})
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return gen, nil
}

func parseDepositSeq(id string) (uint64, error) {
	if !strings.HasPrefix(id, "dep-") {
		return 0, fmt.Errorf("invalid deposit id")
	}
	trimmed := strings.TrimPrefix(id, "dep-")
	return strconv.ParseUint(trimmed, 10, 64)
}

func parseWithdrawSeq(id string) (uint64, error) {
	if !strings.HasPrefix(id, "wd-") {
		return 0, fmt.Errorf("invalid withdraw id")
	}
	trimmed := strings.TrimPrefix(id, "wd-")
	return strconv.ParseUint(trimmed, 10, 64)
}

func parseSettlementSeq(id string) (uint64, error) {
	if !strings.HasPrefix(id, "stl-") {
		return 0, fmt.Errorf("invalid settlement id")
	}
	trimmed := strings.TrimPrefix(id, "stl-")
	return strconv.ParseUint(trimmed, 10, 64)
}
