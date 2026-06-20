
// Package types holds shared types and helpers for the credits module.
package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Coin/Coins conversion helpers.
//
// After the gogoproto migration, the module's protobuf Coin fields are generated
// as native sdk.Coin / sdk.Coins (no separate wire type), so these helpers are
// retained as thin identity/validation shims. Keeping them lets the keeper and
// message handlers stay unchanged across the migration; they can be inlined
// later. The "*Proto" names are historical.

// CoinToProto returns the coin unchanged (Coin fields are now native sdk.Coin).
func CoinToProto(c sdk.Coin) sdk.Coin { return c }

// CoinsToProto returns the coins unchanged (Coins fields are now native sdk.Coins).
func CoinsToProto(coins sdk.Coins) sdk.Coins { return coins }

// CoinFromProtoSafe validates a coin sourced from user input, returning an error
// for malformed amounts/denoms instead of panicking. A zero-value coin (nil
// Amount) is treated as an explicit zero so handlers can reject it on their own
// positivity checks, matching the pre-migration contract.
func CoinFromProtoSafe(c sdk.Coin) (sdk.Coin, error) {
	if c.Amount.IsNil() {
		return sdk.Coin{Denom: "", Amount: math.ZeroInt()}, nil
	}
	if err := sdk.ValidateDenom(c.Denom); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid coin denom: %w", err)
	}
	if c.Amount.IsNegative() {
		return sdk.Coin{}, fmt.Errorf("negative coin amount: %s", c.Amount.String())
	}
	return c, nil
}

// CoinFromProto validates a coin sourced from trusted state and panics on a
// malformed value — use CoinFromProtoSafe for user-supplied input.
func CoinFromProto(c sdk.Coin) sdk.Coin {
	out, err := CoinFromProtoSafe(c)
	if err != nil {
		panic(fmt.Errorf("credits: malformed coin from trusted state: %w", err))
	}
	return out
}

// CoinsFromProto normalizes a coins slice, returning a non-nil empty slice for
// nil input (Coins fields are now native sdk.Coins).
func CoinsFromProto(coins sdk.Coins) sdk.Coins {
	if coins == nil {
		return sdk.Coins{}
	}
	return coins
}
