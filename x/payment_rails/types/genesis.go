package types

import (
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		Deposits:       []*DepositRecord{},
		Pricings:       []*PricingRecord{},
		Mints:          []*MintRecord{},
		Withdrawals:    []*WithdrawRecord{},
		IbcSettlements: []*IBCSettlementRecord{},
		UserHourly:     []*UserRateLimitEntry{},
		UserDaily:      []*UserRateLimitEntry{},
	}
}

// Validate performs basic validation of genesis state.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if gs.Params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := gs.Params.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	seenDeposits := map[string]struct{}{}
	seenDepositRequests := map[string]string{}
	for i, dep := range gs.Deposits {
		if dep == nil {
			return fmt.Errorf("deposit[%d] cannot be nil", i)
		}
		if err := validateGenesisID(fmt.Sprintf("deposit[%d] id", i), dep.DepositId); err != nil {
			return err
		}
		if _, ok := seenDeposits[dep.DepositId]; ok {
			return fmt.Errorf("duplicate deposit id %s", dep.DepositId)
		}
		seenDeposits[dep.DepositId] = struct{}{}
		if err := validateGenesisDeposit(i, dep); err != nil {
			return err
		}
		if existing, ok := seenDepositRequests[dep.RequestId]; ok {
			return fmt.Errorf("duplicate deposit request_id %s for deposits %s and %s", dep.RequestId, existing, dep.DepositId)
		}
		seenDepositRequests[dep.RequestId] = dep.DepositId
	}
	seenPricings := map[string]struct{}{}
	for i, pricing := range gs.Pricings {
		if pricing == nil {
			return fmt.Errorf("pricing[%d] cannot be nil", i)
		}
		if pricing.DepositId == "" {
			return fmt.Errorf("pricing[%d] deposit_id required", i)
		}
		if _, ok := seenPricings[pricing.DepositId]; ok {
			return fmt.Errorf("duplicate pricing deposit id %s", pricing.DepositId)
		}
		seenPricings[pricing.DepositId] = struct{}{}
		if err := validateOptionalGenesisTimestamp(fmt.Sprintf("pricing[%d] timestamp", i), pricing.Timestamp); err != nil {
			return err
		}
	}
	for i, pricing := range gs.Pricings {
		if _, ok := seenDeposits[pricing.DepositId]; !ok {
			return fmt.Errorf("pricing[%d] references unknown deposit %s", i, pricing.DepositId)
		}
	}
	seenMints := map[string]struct{}{}
	for i, mint := range gs.Mints {
		if mint == nil {
			return fmt.Errorf("mint[%d] cannot be nil", i)
		}
		if mint.DepositId == "" {
			return fmt.Errorf("mint[%d] deposit_id required", i)
		}
		if _, ok := seenMints[mint.DepositId]; ok {
			return fmt.Errorf("duplicate mint deposit id %s", mint.DepositId)
		}
		seenMints[mint.DepositId] = struct{}{}
		if err := validateOptionalGenesisTimestamp(fmt.Sprintf("mint[%d] timestamp", i), mint.Timestamp); err != nil {
			return err
		}
	}
	for i, mint := range gs.Mints {
		if _, ok := seenDeposits[mint.DepositId]; !ok {
			return fmt.Errorf("mint[%d] references unknown deposit %s", i, mint.DepositId)
		}
		if err := validatePositiveGenesisCoin(fmt.Sprintf("mint[%d] lac_minted", i), mint.LacMinted); err != nil {
			return err
		}
	}
	seenWithdrawals := map[string]struct{}{}
	seenWithdrawRequests := map[string]string{}
	for i, wd := range gs.Withdrawals {
		if wd == nil {
			return fmt.Errorf("withdraw[%d] cannot be nil", i)
		}
		if err := validateGenesisID(fmt.Sprintf("withdraw[%d] id", i), wd.WithdrawId); err != nil {
			return err
		}
		if _, ok := seenWithdrawals[wd.WithdrawId]; ok {
			return fmt.Errorf("duplicate withdraw id %s", wd.WithdrawId)
		}
		seenWithdrawals[wd.WithdrawId] = struct{}{}
		if err := validateGenesisWithdraw(i, gs.Params, wd); err != nil {
			return err
		}
		if existing, ok := seenWithdrawRequests[wd.RequestId]; ok {
			return fmt.Errorf("duplicate withdraw request_id %s for withdrawals %s and %s", wd.RequestId, existing, wd.WithdrawId)
		}
		seenWithdrawRequests[wd.RequestId] = wd.WithdrawId
	}
	seenSettlements := map[string]struct{}{}
	seenSettlementRequests := map[string]string{}
	for i, stl := range gs.IbcSettlements {
		if stl == nil {
			return fmt.Errorf("ibc_settlement[%d] cannot be nil", i)
		}
		if err := validateGenesisID(fmt.Sprintf("ibc_settlement[%d] id", i), stl.SettlementId); err != nil {
			return err
		}
		if _, ok := seenSettlements[stl.SettlementId]; ok {
			return fmt.Errorf("duplicate ibc settlement id %s", stl.SettlementId)
		}
		seenSettlements[stl.SettlementId] = struct{}{}
		if err := validateOptionalGenesisTimestamp(fmt.Sprintf("ibc_settlement[%d] created_at", i), stl.CreatedAt); err != nil {
			return err
		}
		if err := validateOptionalGenesisTimestamp(fmt.Sprintf("ibc_settlement[%d] updated_at", i), stl.UpdatedAt); err != nil {
			return err
		}
		if err := validateOptionalGenesisTimestamp(fmt.Sprintf("ibc_settlement[%d] ack_at", i), stl.AckAt); err != nil {
			return err
		}
		if err := validateGenesisTimestampNotBefore(fmt.Sprintf("ibc_settlement[%d]", i), "updated_at", stl.UpdatedAt, "created_at", stl.CreatedAt); err != nil {
			return err
		}
		if err := validateGenesisTimestampNotBefore(fmt.Sprintf("ibc_settlement[%d]", i), "ack_at", stl.AckAt, "created_at", stl.CreatedAt); err != nil {
			return err
		}
		if stl.RequestId != "" {
			if err := validateGenesisID(fmt.Sprintf("ibc_settlement[%d] request_id", i), stl.RequestId); err != nil {
				return err
			}
			if existing, ok := seenSettlementRequests[stl.RequestId]; ok {
				return fmt.Errorf("duplicate ibc settlement request_id %s for settlements %s and %s", stl.RequestId, existing, stl.SettlementId)
			}
			seenSettlementRequests[stl.RequestId] = stl.SettlementId
		}
	}
	for i, entry := range gs.UserHourly {
		if entry == nil {
			return fmt.Errorf("user_hourly[%d] cannot be nil", i)
		}
		if entry.User == "" {
			return fmt.Errorf("user_hourly[%d] user required", i)
		}
		if entry.WindowStart.IsZero() {
			return fmt.Errorf("user_hourly[%d] window_start required", i)
		}
		if entry.Window == nil {
			return fmt.Errorf("user_hourly[%d] window required", i)
		}
		if err := validateGenesisUser(fmt.Sprintf("user_hourly[%d] user", i), entry.User); err != nil {
			return err
		}
	}
	if err := validateUniqueRateLimitWindows("user_hourly", gs.UserHourly); err != nil {
		return err
	}
	for i, entry := range gs.UserDaily {
		if entry == nil {
			return fmt.Errorf("user_daily[%d] cannot be nil", i)
		}
		if entry.User == "" {
			return fmt.Errorf("user_daily[%d] user required", i)
		}
		if entry.WindowStart.IsZero() {
			return fmt.Errorf("user_daily[%d] window_start required", i)
		}
		if entry.Window == nil {
			return fmt.Errorf("user_daily[%d] window required", i)
		}
		if err := validateGenesisUser(fmt.Sprintf("user_daily[%d] user", i), entry.User); err != nil {
			return err
		}
	}
	if err := validateUniqueRateLimitWindows("user_daily", gs.UserDaily); err != nil {
		return err
	}
	return nil
}

func validateGenesisDeposit(i int, dep *DepositRecord) error {
	if err := validateGenesisUser(fmt.Sprintf("deposit[%d] user", i), dep.User); err != nil {
		return err
	}
	if strings.TrimSpace(dep.Denom) == "" {
		return fmt.Errorf("deposit[%d] denom required", i)
	}
	if err := validateGenesisID(fmt.Sprintf("deposit[%d] request_id", i), dep.RequestId); err != nil {
		return err
	}
	if err := validateGenesisID(fmt.Sprintf("deposit[%d] tx_hash", i), dep.TxHash); err != nil {
		return err
	}
	if _, ok := DepositStatus_name[int32(dep.Status)]; !ok || dep.Status == DepositStatus_DEPOSIT_STATUS_UNSPECIFIED {
		return fmt.Errorf("deposit[%d] invalid status %s", i, dep.Status.String())
	}
	coin, err := genesisCoin(fmt.Sprintf("deposit[%d] amount", i), dep.Amount)
	if err != nil {
		return err
	}
	if !coin.Amount.IsPositive() {
		return fmt.Errorf("deposit[%d] amount must be positive", i)
	}
	if coin.Denom != dep.Denom {
		return fmt.Errorf("deposit[%d] denom %s does not match amount denom %s", i, dep.Denom, coin.Denom)
	}
	if err := validateOptionalGenesisTimestamp(fmt.Sprintf("deposit[%d] created_at", i), dep.CreatedAt); err != nil {
		return err
	}
	if err := validateOptionalGenesisTimestamp(fmt.Sprintf("deposit[%d] updated_at", i), dep.UpdatedAt); err != nil {
		return err
	}
	if err := validateGenesisTimestampNotBefore(fmt.Sprintf("deposit[%d]", i), "updated_at", dep.UpdatedAt, "created_at", dep.CreatedAt); err != nil {
		return err
	}
	return nil
}

func validateGenesisWithdraw(i int, params *Params, wd *WithdrawRecord) error {
	if err := validateGenesisUser(fmt.Sprintf("withdraw[%d] user", i), wd.User); err != nil {
		return err
	}
	if strings.TrimSpace(wd.Denom) == "" {
		return fmt.Errorf("withdraw[%d] denom required", i)
	}
	if err := validateGenesisID(fmt.Sprintf("withdraw[%d] request_id", i), wd.RequestId); err != nil {
		return err
	}
	if _, ok := WithdrawStatus_name[int32(wd.Status)]; !ok || wd.Status == WithdrawStatus_WITHDRAW_STATUS_UNSPECIFIED {
		return fmt.Errorf("withdraw[%d] invalid status %s", i, wd.Status.String())
	}
	lacBurned, err := genesisCoin(fmt.Sprintf("withdraw[%d] lac_burned", i), wd.LacBurned)
	if err != nil {
		return err
	}
	if !lacBurned.Amount.IsPositive() {
		return fmt.Errorf("withdraw[%d] lac_burned must be positive", i)
	}
	if lacBurned.Denom != params.CreditDenom {
		return fmt.Errorf("withdraw[%d] lac_burned denom %s does not match credit denom %s", i, lacBurned.Denom, params.CreditDenom)
	}
	assetReleased, err := genesisCoin(fmt.Sprintf("withdraw[%d] asset_released", i), wd.AssetReleased)
	if err != nil {
		return err
	}
	if !assetReleased.Amount.IsPositive() {
		return fmt.Errorf("withdraw[%d] asset_released must be positive", i)
	}
	if assetReleased.Denom != wd.Denom {
		return fmt.Errorf("withdraw[%d] denom %s does not match asset_released denom %s", i, wd.Denom, assetReleased.Denom)
	}
	if err := validateOptionalGenesisTimestamp(fmt.Sprintf("withdraw[%d] requested_at", i), wd.RequestedAt); err != nil {
		return err
	}
	if err := validateOptionalGenesisTimestamp(fmt.Sprintf("withdraw[%d] completed_at", i), wd.CompletedAt); err != nil {
		return err
	}
	if err := validateGenesisTimestampNotBefore(fmt.Sprintf("withdraw[%d]", i), "completed_at", wd.CompletedAt, "requested_at", wd.RequestedAt); err != nil {
		return err
	}
	return nil
}

func validateGenesisID(name string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s required", name)
	}
	if trimmed != value {
		return fmt.Errorf("%s must be canonical", name)
	}
	if len(value) > MaxIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", name, MaxIDLen, len(value))
	}
	return nil
}

func validateGenesisUser(name string, user string) error {
	if strings.TrimSpace(user) == "" {
		return fmt.Errorf("%s required", name)
	}
	if _, err := sdk.AccAddressFromBech32(user); err != nil {
		return fmt.Errorf("%s invalid address: %w", name, err)
	}
	return nil
}

func validateOptionalGenesisTimestamp(_ string, ts time.Time) error {
	_ = ts
	return nil
}

func validateGenesisTimestampNotBefore(recordName, laterName string, later time.Time, earlierName string, earlier time.Time) error {
	if later.IsZero() || earlier.IsZero() {
		return nil
	}
	if later.Before(earlier) {
		return fmt.Errorf("%s %s cannot be before %s", recordName, laterName, earlierName)
	}
	return nil
}

func validatePositiveGenesisCoin(name string, coin sdk.Coin) error {
	parsed, err := genesisCoin(name, coin)
	if err != nil {
		return err
	}
	if !parsed.Amount.IsPositive() {
		return fmt.Errorf("%s must be positive", name)
	}
	return nil
}

func genesisCoin(name string, coin sdk.Coin) (sdk.Coin, error) {
	if coin.Denom == "" && coin.Amount.IsNil() {
		return sdk.Coin{}, fmt.Errorf("%s required", name)
	}
	parsed, err := CoinFromProtoSafe(coin)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("%s invalid: %w", name, err)
	}
	return parsed, nil
}

func validateUniqueRateLimitWindows(name string, entries []*UserRateLimitEntry) error {
	seen := map[string]struct{}{}
	for _, entry := range entries {
		windowStart := entry.WindowStart
		key := fmt.Sprintf("%s/%d/%d", entry.User, windowStart.Unix(), int64(windowStart.Nanosecond()))
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate %s window for user %s at %s", name, entry.User, windowStart.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
		}
		seen[key] = struct{}{}
	}
	return nil
}
