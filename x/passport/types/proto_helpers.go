
package types

import (
	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CoinFromProto converts a protobuf v1beta1.Coin to an sdk.Coin.
// Returns sdk.Coin{} (the zero value) rather than panicking when the
// proto coin is nil, has an invalid Denom, or has a negative Amount.
// Params.Validate is the primary gate that keeps malformed Params
// out, but this helper is also reachable from callers like
// MinStakeCoin after a storage round-trip, so defending here as well
// keeps a single bad write from crashing the whole passport keeper.
func CoinFromProto(c *v1beta1.Coin) sdk.Coin {
	if c == nil {
		return sdk.Coin{}
	}
	if sdk.ValidateDenom(c.Denom) != nil {
		return sdk.Coin{}
	}
	amount, ok := sdkmath.NewIntFromString(c.Amount)
	if !ok {
		return sdk.NewCoin(c.Denom, sdkmath.ZeroInt())
	}
	if amount.IsNegative() {
		return sdk.Coin{}
	}
	return sdk.NewCoin(c.Denom, amount)
}

// CoinToProto converts an sdk.Coin to a protobuf v1beta1.Coin.
func CoinToProto(c sdk.Coin) *v1beta1.Coin {
	return &v1beta1.Coin{
		Denom:  c.Denom,
		Amount: c.Amount.String(),
	}
}

// CoinsFromProto converts a slice of protobuf v1beta1.Coins to sdk.Coins.
// Skips entries that CoinFromProto rejects (nil input, invalid denom,
// or negative amount) — those return sdk.Coin{} with a nil Amount,
// which would panic inside sdk.Coins.Add via a nil big.Int deref.
// Zero-amount coins are also skipped because sdk.Coins.Add rejects
// non-positive additions.
func CoinsFromProto(coins []*v1beta1.Coin) sdk.Coins {
	result := sdk.NewCoins()
	for _, c := range coins {
		if c == nil {
			continue
		}
		converted := CoinFromProto(c)
		// CoinFromProto returns sdk.Coin{} (nil Amount) on invalid
		// denom / negative amount. Skip rather than let Add panic.
		if converted.Denom == "" || converted.Amount.IsNil() {
			continue
		}
		if !converted.Amount.IsPositive() {
			continue
		}
		result = result.Add(converted)
	}
	return result
}

// CoinsToProto converts sdk.Coins to a slice of protobuf v1beta1.Coins.
func CoinsToProto(coins sdk.Coins) []*v1beta1.Coin {
	result := make([]*v1beta1.Coin, 0, len(coins))
	for _, c := range coins {
		result = append(result, CoinToProto(c))
	}
	return result
}
