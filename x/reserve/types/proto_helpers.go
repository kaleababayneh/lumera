//go:build cosmos

// Package types defines data structures for the reserve module.
package types

import (
	"fmt"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CoinToProto converts an sdk.Coin into the module's protobuf coin shape.
func CoinToProto(coin sdk.Coin) *basev1beta1.Coin {
	return &basev1beta1.Coin{
		Denom:  coin.Denom,
		Amount: coin.Amount.String(),
	}
}

// CoinFromProtoSafe converts a protobuf coin from user input without panics.
func CoinFromProtoSafe(coin *basev1beta1.Coin) (sdk.Coin, error) {
	if coin == nil {
		return sdk.Coin{Denom: "", Amount: sdkmath.ZeroInt()}, nil
	}
	amount, ok := sdkmath.NewIntFromString(coin.Amount)
	if !ok {
		return sdk.Coin{}, fmt.Errorf("invalid coin amount: %q", coin.Amount)
	}
	if err := sdk.ValidateDenom(coin.Denom); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid coin denom: %w", err)
	}
	if amount.IsNegative() {
		return sdk.Coin{}, fmt.Errorf("negative coin amount: %s", amount.String())
	}
	return sdk.NewCoin(coin.Denom, amount), nil
}
