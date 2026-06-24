// Package types holds shared types and helpers for the payment_rails module.
package types

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// After the gogoproto migration the record coin fields are value sdk.Coin and
// the timestamp fields are value time.Time (gogoproto stdtime). The accessor
// helpers below are retained so the keeper call sites are unchanged; they are
// now thin value views over the fields rather than proto<->sdk conversions.

// --- Coin conversion helpers ---

// CoinToProto is an identity helper retained at call sites post-migration.
func CoinToProto(c sdk.Coin) sdk.Coin { return c }

// CoinFromProtoSafe validates a stored/user-supplied coin, returning an error
// for malformed input instead of panicking. Use this for user-supplied input
// in message handlers.
func CoinFromProtoSafe(p sdk.Coin) (sdk.Coin, error) {
	if p.Denom == "" && p.Amount.IsNil() {
		return sdk.Coin{}, nil
	}
	if err := sdk.ValidateDenom(p.Denom); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid coin denom: %w", err)
	}
	if p.Amount.IsNil() {
		return sdk.Coin{}, fmt.Errorf("coin amount is nil")
	}
	if p.Amount.IsNegative() {
		return sdk.Coin{}, fmt.Errorf("negative coin amount: %s", p.Amount.String())
	}
	return p, nil
}

// CoinFromProto returns the coin as-is. It panics on malformed amounts — use
// CoinFromProtoSafe for user-supplied input.
func CoinFromProto(p sdk.Coin) sdk.Coin {
	c, err := CoinFromProtoSafe(p)
	if err != nil {
		panic(err)
	}
	return c
}

// --- Timestamp conversion helpers ---

// ProtoTimestampOrZero returns the stored time value (already time.Time).
func ProtoTimestampOrZero(ts time.Time) time.Time { return ts }

// TimeToProto returns the time value (stored as gogoproto stdtime time.Time).
func TimeToProto(ts time.Time) time.Time { return ts }

// --- DepositRecord proto helpers ---

// AmountCoin returns the deposit amount as sdk.Coin.
func (m *DepositRecord) AmountCoin() sdk.Coin { return m.Amount }

// SetAmountCoin stores the provided coin in the deposit record.
func (m *DepositRecord) SetAmountCoin(coin sdk.Coin) {
	if m == nil {
		return
	}
	m.Amount = coin
}

// CreatedAtTime returns CreatedAt.
func (m *DepositRecord) CreatedAtTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.CreatedAt
}

// SetCreatedAtTime updates CreatedAt from a time value.
func (m *DepositRecord) SetCreatedAtTime(t time.Time) {
	if m == nil {
		return
	}
	m.CreatedAt = t
}

// UpdatedAtTime returns UpdatedAt.
func (m *DepositRecord) UpdatedAtTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.UpdatedAt
}

// SetUpdatedAtTime updates UpdatedAt from a time value.
func (m *DepositRecord) SetUpdatedAtTime(t time.Time) {
	if m == nil {
		return
	}
	m.UpdatedAt = t
}

// --- PricingRecord proto helpers ---

// TimestampTime returns Timestamp.
func (m *PricingRecord) TimestampTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.Timestamp
}

// SetTimestampTime updates Timestamp from a time value.
func (m *PricingRecord) SetTimestampTime(t time.Time) {
	if m == nil {
		return
	}
	m.Timestamp = t
}

// --- MintRecord proto helpers ---

// LacMintedCoin returns the LAC minted as sdk.Coin.
func (m *MintRecord) LacMintedCoin() sdk.Coin { return m.LacMinted }

// SetLacMintedCoin stores the provided coin.
func (m *MintRecord) SetLacMintedCoin(coin sdk.Coin) {
	if m == nil {
		return
	}
	m.LacMinted = coin
}

// FeeLacCoin returns the fee LAC as sdk.Coin.
func (m *MintRecord) FeeLacCoin() sdk.Coin { return m.FeeLac }

// SetFeeLacCoin stores the provided coin.
func (m *MintRecord) SetFeeLacCoin(coin sdk.Coin) {
	if m == nil {
		return
	}
	m.FeeLac = coin
}

// TimestampTime returns Timestamp.
func (m *MintRecord) TimestampTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.Timestamp
}

// SetTimestampTime updates Timestamp from a time value.
func (m *MintRecord) SetTimestampTime(t time.Time) {
	if m == nil {
		return
	}
	m.Timestamp = t
}

// --- WithdrawRecord proto helpers ---

// LacBurnedCoin returns the LAC burned as sdk.Coin.
func (m *WithdrawRecord) LacBurnedCoin() sdk.Coin { return m.LacBurned }

// SetLacBurnedCoin stores the provided coin.
func (m *WithdrawRecord) SetLacBurnedCoin(coin sdk.Coin) {
	if m == nil {
		return
	}
	m.LacBurned = coin
}

// AssetReleasedCoin returns the asset released as sdk.Coin.
func (m *WithdrawRecord) AssetReleasedCoin() sdk.Coin { return m.AssetReleased }

// SetAssetReleasedCoin stores the provided coin.
func (m *WithdrawRecord) SetAssetReleasedCoin(coin sdk.Coin) {
	if m == nil {
		return
	}
	m.AssetReleased = coin
}

// RequestedAtTime returns RequestedAt.
func (m *WithdrawRecord) RequestedAtTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.RequestedAt
}

// SetRequestedAtTime updates RequestedAt from a time value.
func (m *WithdrawRecord) SetRequestedAtTime(t time.Time) {
	if m == nil {
		return
	}
	m.RequestedAt = t
}

// CompletedAtTime returns CompletedAt.
func (m *WithdrawRecord) CompletedAtTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.CompletedAt
}

// SetCompletedAtTime updates CompletedAt from a time value.
func (m *WithdrawRecord) SetCompletedAtTime(t time.Time) {
	if m == nil {
		return
	}
	m.CompletedAt = t
}
