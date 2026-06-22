// Package testutil provides test utilities for Lumera Cosmos SDK integration tests.
package testutil

import (
	"sync/atomic"
	"testing"
	"time"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/app"
	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
)

// SetupTestApp creates a new Lumera app instance for testing with an in-memory
// database. It returns a fresh context and the fully-initialized app instance.
//
// The app is built via app.Setup which runs InitChain (genesis, module
// accounts, default params for every module) so the returned context is backed
// by an initialized multistore.
func SetupTestApp(t *testing.T) (sdk.Context, *app.App) {
	t.Helper()

	lumeraApp := app.Setup(t)

	// Create a deterministic context with a fixed block time.
	header := cmtproto.Header{
		Height:  1,
		Time:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		ChainID: "testing",
	}
	ctx := lumeraApp.NewContextLegacy(false, header)

	return ctx, lumeraApp
}

// TestAccount wraps an account for testing purposes.
type TestAccount struct {
	addr sdk.AccAddress
}

// GetAddress returns the account address.
func (ta TestAccount) GetAddress() sdk.AccAddress {
	return ta.addr
}

// CreateTestAccount creates a new test account with a unique address. The caller
// must pass the context returned by SetupTestApp so the account keeper can
// access the properly-initialized multistore.
func CreateTestAccount(t *testing.T, lumeraApp *app.App, ctxOpt ...sdk.Context) TestAccount {
	t.Helper()

	addr := sdk.AccAddress(generateRandomBytes(20))

	var ctx sdk.Context
	if len(ctxOpt) > 0 {
		ctx = ctxOpt[0]
	} else {
		ctx = lumeraApp.NewContextLegacy(false, cmtproto.Header{
			Height:  1,
			Time:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			ChainID: "testing",
		})
	}

	acc := lumeraApp.AuthKeeper.NewAccountWithAddress(ctx, addr)
	lumeraApp.AuthKeeper.SetAccount(ctx, acc)

	return TestAccount{addr: addr}
}

// FundAccount mints and sends coins to the specified account.
//
// When funding ulac (credit tokens), it also mints the equivalent ulume in the
// credits module to satisfy the 1:1 LUME backing invariant required by BurnLAC.
func FundAccount(ctx sdk.Context, lumeraApp *app.App, addr sdk.AccAddress, coins sdk.Coins) error {
	if err := lumeraApp.BankKeeper.MintCoins(ctx, creditstypes.ModuleName, coins); err != nil {
		return err
	}

	// Mint backing ulume for any ulac in the request to maintain the 1:1
	// invariant. BurnLAC and BurnCreditsFromAccount burn both ulac and the
	// corresponding ulume.
	var lumeBacking sdk.Coins
	for _, coin := range coins {
		if coin.Denom == creditstypes.DefaultCreditDenom {
			lumeBacking = lumeBacking.Add(sdk.NewCoin(creditstypes.DefaultLumeDenom, coin.Amount))
		}
	}
	if !lumeBacking.IsZero() {
		if err := lumeraApp.BankKeeper.MintCoins(ctx, creditstypes.ModuleName, lumeBacking); err != nil {
			return err
		}
		// lumeBacking stays in the module as escrow — not sent to the target.
	}

	return lumeraApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, creditstypes.ModuleName, addr, coins)
}

// addressCounter backs generateRandomBytes. It is accessed via sync/atomic so
// concurrent tests do not race on the increment.
var addressCounter atomic.Uint64

// generateRandomBytes generates deterministic bytes for testing.
func generateRandomBytes(n int) []byte {
	seed := addressCounter.Add(1)
	result := make([]byte, n)
	for i := 0; i < n; i++ {
		result[i] = byte((seed + uint64(i)) % 256)
	}
	return result
}
