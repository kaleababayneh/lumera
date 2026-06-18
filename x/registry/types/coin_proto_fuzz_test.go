//go:build cosmos

package types

import (
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FuzzCoinFromProtoCrashFreedom pins crash-freedom for the private
// coinFromProto helper. It's called from every ValidateBasic that
// accepts a Coin field — MsgRegisterTool, MsgStakeTool, MsgSlashTool,
// MsgTopUpStake — so a panic here propagates through Cosmos SDK's
// txn-validation pipeline.
//
// Before hardening, a denom that was non-empty but failed
// sdk.ValidateDenom's format rules (e.g., "UPPER", "1bad") made
// sdk.NewCoin panic inside coinFromProto. The helper now calls
// sdk.ValidateDenom first and returns a clean validation error.
// This fuzz locks that down.
func FuzzCoinFromProtoCrashFreedom(f *testing.F) {
	seeds := []struct {
		denom  string
		amount string
	}{
		{"ulac", "100"},
		{"", "100"},
		{"ulac", ""},
		{"ulac", "0"},
		{"ulac", "-1"},
		{"UPPER", "100"},
		{"1bad", "100"},
		{"ulac", "nota_number"},
		{"", ""},
	}
	for _, s := range seeds {
		f.Add(s.denom, s.amount)
	}

	f.Fuzz(func(t *testing.T, denom, amount string) {
		c := &v1beta1.Coin{Denom: denom, Amount: amount}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("coinFromProto panicked on user-supplied input (denom=%q, amount=%q): %v",
					denom, amount, r)
			}
		}()

		coin, err := coinFromProto(c, "test")

		// Determinism.
		coin2, err2 := coinFromProto(c, "test")
		if (err == nil) != (err2 == nil) {
			t.Fatalf("non-deterministic error: first=%v second=%v", err, err2)
		}
		if err == nil && !coin.Equal(coin2) {
			t.Fatalf("non-deterministic coin")
		}

		// No-error path must return a coin that passes
		// sdk.ValidateDenom and is non-negative.
		if err == nil {
			if coin.Amount.IsNegative() {
				t.Fatalf("no-error path returned negative amount: %s", coin)
			}
			if dErr := sdk.ValidateDenom(coin.Denom); dErr != nil {
				t.Fatalf("no-error path returned invalid denom %q: %v", coin.Denom, dErr)
			}
		}
	})
}
