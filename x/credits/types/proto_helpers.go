
// Package types holds shared types and helpers for the credits module.
package types

import (
	"fmt"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// --- Coin conversion helpers ---

// CoinToProto converts an sdk.Coin into a protobuf coin.
func CoinToProto(c sdk.Coin) *basev1beta1.Coin {
	return &basev1beta1.Coin{
		Denom:  c.Denom,
		Amount: c.Amount.String(),
	}
}

// CoinsToProto converts sdk.Coins into protobuf coins.
func CoinsToProto(coins sdk.Coins) []*basev1beta1.Coin {
	if len(coins) == 0 {
		return nil
	}
	out := make([]*basev1beta1.Coin, 0, len(coins))
	for _, coin := range coins {
		out = append(out, CoinToProto(coin))
	}
	return out
}

// CoinFromProtoSafe converts a protobuf coin into an sdk.Coin, returning an
// error for malformed amounts instead of panicking. Use this for user-supplied
// input in message handlers.
func CoinFromProtoSafe(p *basev1beta1.Coin) (sdk.Coin, error) {
	if p == nil {
		return sdk.Coin{Denom: "", Amount: math.ZeroInt()}, nil
	}
	amount, ok := math.NewIntFromString(p.Amount)
	if !ok {
		return sdk.Coin{}, fmt.Errorf("invalid coin amount: %q", p.Amount)
	}
	// sdk.NewCoin panics on empty/invalid denom or negative amount; convert
	// those to errors here so the function matches its "safe for user input"
	// contract. Every caller in msg_server relies on the returned error —
	// a panic would crash the handler through baseapp's recover instead of
	// surfacing as a clean validation error to the user.
	if err := sdk.ValidateDenom(p.Denom); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid coin denom: %w", err)
	}
	if amount.IsNegative() {
		return sdk.Coin{}, fmt.Errorf("negative coin amount: %s", amount.String())
	}
	return sdk.NewCoin(p.Denom, amount), nil
}

// CoinFromProto converts a protobuf coin into an sdk.Coin.
// It panics on malformed amounts — use CoinFromProtoSafe for user-supplied input.
// Returns a zero-valued coin (with initialized Amount) when input is nil.
func CoinFromProto(p *basev1beta1.Coin) sdk.Coin {
	c, err := CoinFromProtoSafe(p)
	if err != nil {
		panic(fmt.Errorf("credits: malformed proto coin from trusted state: %w", err))
	}
	return c
}

// CoinsFromProto converts protobuf coins into sdk.Coins.
func CoinsFromProto(input []*basev1beta1.Coin) sdk.Coins {
	if len(input) == 0 {
		return sdk.Coins{}
	}
	out := make(sdk.Coins, 0, len(input))
	for _, coin := range input {
		out = append(out, CoinFromProto(coin))
	}
	return out
}

// --- Message method helpers ---

// AmountCoin returns the locked coin for the lock record.
func (m *Lock) AmountCoin() sdk.Coin {
	return CoinFromProto(m.Amount)
}

// SetAmountCoin stores the provided coin in the lock record.
func (m *Lock) SetAmountCoin(coin sdk.Coin) {
	if m == nil {
		return
	}
	m.Amount = CoinToProto(coin)
}

// CreatedAtTime converts CreatedAt to time.Time.
func (m *Lock) CreatedAtTime() time.Time {
	if m == nil || m.CreatedAt == nil {
		return time.Time{}
	}
	return m.CreatedAt.AsTime()
}

// SetCreatedAtTime updates CreatedAt from a time value.
func (m *Lock) SetCreatedAtTime(t time.Time) {
	if m == nil {
		return
	}
	m.CreatedAt = timestamppb.New(t)
}

// ExpiresAtTime converts ExpiresAt to time.Time.
func (m *Lock) ExpiresAtTime() time.Time {
	if m == nil || m.ExpiresAt == nil {
		return time.Time{}
	}
	return m.ExpiresAt.AsTime()
}

// SetExpiresAtTime updates ExpiresAt from a time value.
func (m *Lock) SetExpiresAtTime(t time.Time) {
	if m == nil {
		return
	}
	m.ExpiresAt = timestamppb.New(t)
}

// AmountBurnedCoin returns the burned amount coin.
func (m *Settlement) AmountBurnedCoin() sdk.Coin {
	return CoinFromProto(m.AmountBurned)
}

// SetAmountBurnedCoin stores the burned amount coin.
func (m *Settlement) SetAmountBurnedCoin(coin sdk.Coin) {
	if m == nil {
		return
	}
	m.AmountBurned = CoinToProto(coin)
}

// RefundAmountCoin returns the refund coin.
func (m *Settlement) RefundAmountCoin() sdk.Coin {
	return CoinFromProto(m.RefundAmount)
}

// SetRefundAmountCoin stores the refund coin.
func (m *Settlement) SetRefundAmountCoin(coin sdk.Coin) {
	if m == nil {
		return
	}
	m.RefundAmount = CoinToProto(coin)
}

// SettledAtTime converts SettledAt to time.Time.
func (m *Settlement) SettledAtTime() time.Time {
	if m == nil || m.SettledAt == nil {
		return time.Time{}
	}
	return m.SettledAt.AsTime()
}

// SetSettledAtTime updates SettledAt from a time value.
func (m *Settlement) SetSettledAtTime(t time.Time) {
	if m == nil {
		return
	}
	m.SettledAt = timestamppb.New(t)
}

// TotalCostCoins converts the total cost to sdk.Coins.
func (m *SettlementRecord) TotalCostCoins() sdk.Coins {
	return CoinsFromProto(m.GetTotalCost())
}

// BurnAmountCoins converts the burn amount to sdk.Coins.
func (m *SettlementRecord) BurnAmountCoins() sdk.Coins {
	return CoinsFromProto(m.GetBurnAmount())
}

// NetAmountCoins converts the net amount to sdk.Coins.
func (m *SettlementRecord) NetAmountCoins() sdk.Coins {
	return CoinsFromProto(m.GetNetAmount())
}

// SetTotalCostCoins stores the total cost coins.
func (m *SettlementRecord) SetTotalCostCoins(coins sdk.Coins) {
	if m == nil {
		return
	}
	m.TotalCost = CoinsToProto(coins)
}

// SetBurnAmountCoins stores the burned coins.
func (m *SettlementRecord) SetBurnAmountCoins(coins sdk.Coins) {
	if m == nil {
		return
	}
	m.BurnAmount = CoinsToProto(coins)
}

// SetNetAmountCoins stores the net amount coins.
func (m *SettlementRecord) SetNetAmountCoins(coins sdk.Coins) {
	if m == nil {
		return
	}
	m.NetAmount = CoinsToProto(coins)
}

// TimestampTime converts Timestamp to time.Time.
func (m *SettlementRecord) TimestampTime() time.Time {
	if m == nil || m.Timestamp == nil {
		return time.Time{}
	}
	return m.Timestamp.AsTime()
}

// SetTimestampTime updates Timestamp from a time value.
func (m *SettlementRecord) SetTimestampTime(t time.Time) {
	if m == nil {
		return
	}
	m.Timestamp = timestamppb.New(t)
}

// CompletedAtTime returns a pointer to the completed time value.
func (m *SettlementRecord) CompletedAtTime() *time.Time {
	if m == nil || m.CompletedAt == nil {
		return nil
	}
	t := m.CompletedAt.AsTime()
	return &t
}

// SetCompletedAtTime stores the completed time pointer.
func (m *SettlementRecord) SetCompletedAtTime(t *time.Time) {
	if m == nil {
		return
	}
	if t == nil {
		m.CompletedAt = nil
		return
	}
	m.CompletedAt = timestamppb.New(*t)
}

// CreatedAtTime converts CreatedAt to time.Time.
func (m *DisputeRecord) CreatedAtTime() time.Time {
	if m == nil || m.CreatedAt == nil {
		return time.Time{}
	}
	return m.CreatedAt.AsTime()
}

// SetCreatedAtTime stores the provided creation time.
func (m *DisputeRecord) SetCreatedAtTime(t time.Time) {
	if m == nil {
		return
	}
	m.CreatedAt = timestamppb.New(t)
}

// ResolvedAtTime returns the resolution time pointer.
func (m *DisputeRecord) ResolvedAtTime() *time.Time {
	if m == nil || m.ResolvedAt == nil {
		return nil
	}
	t := m.ResolvedAt.AsTime()
	return &t
}

// SetResolvedAtTime stores the provided resolution time pointer.
func (m *DisputeRecord) SetResolvedAtTime(t *time.Time) {
	if m == nil {
		return
	}
	if t == nil {
		m.ResolvedAt = nil
		return
	}
	m.ResolvedAt = timestamppb.New(*t)
}

// --- CACRoyaltyRecord helpers ---

// TotalAmountCoins converts TotalAmount to sdk.Coins.
func (m *CACRoyaltyRecord) TotalAmountCoins() sdk.Coins {
	return CoinsFromProto(m.GetTotalAmount())
}

// OriginShareCoins converts OriginShare to sdk.Coins.
func (m *CACRoyaltyRecord) OriginShareCoins() sdk.Coins {
	return CoinsFromProto(m.GetOriginShare())
}

// ServingShareCoins converts ServingShare to sdk.Coins.
func (m *CACRoyaltyRecord) ServingShareCoins() sdk.Coins {
	return CoinsFromProto(m.GetServingShare())
}

// TimestampTime converts Timestamp to time.Time.
func (m *CACRoyaltyRecord) TimestampTime() time.Time {
	if m == nil || m.Timestamp == nil {
		return time.Time{}
	}
	return m.Timestamp.AsTime()
}

// --- CACRoyaltyStats helpers ---

// TotalRoyaltiesEarnedCoins converts TotalRoyaltiesEarned to sdk.Coins.
func (m *CACRoyaltyStats) TotalRoyaltiesEarnedCoins() sdk.Coins {
	return CoinsFromProto(m.GetTotalRoyaltiesEarned())
}

// TotalRoyaltiesPaidCoins converts TotalRoyaltiesPaid to sdk.Coins.
func (m *CACRoyaltyStats) TotalRoyaltiesPaidCoins() sdk.Coins {
	return CoinsFromProto(m.GetTotalRoyaltiesPaid())
}

// LastUpdatedTime converts LastUpdated to time.Time.
func (m *CACRoyaltyStats) LastUpdatedTime() time.Time {
	if m == nil || m.LastUpdated == nil {
		return time.Time{}
	}
	return m.LastUpdated.AsTime()
}

// ProtoTimestampOrZero safely converts a protobuf timestamp to time.Time.
func ProtoTimestampOrZero(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// TimeToProto wraps a time.Time into a protobuf timestamp.
func TimeToProto(ts time.Time) *timestamppb.Timestamp {
	if ts.IsZero() {
		return nil
	}
	return timestamppb.New(ts)
}
