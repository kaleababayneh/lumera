//go:build cosmos

package keeper

import (
	"context"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// recordingBankKeeper stubs the bank keeper and lets us force specific sends
// to fail, so we can exercise the partial-failure burn path in
// distributeVoteRewards.
type recordingBankKeeper struct {
	minted           sdk.Coins
	burned           sdk.Coins
	failSendFor      sdk.AccAddress
	successfulSendTo []string
}

func (b *recordingBankKeeper) MintCoins(_ context.Context, _ string, amt sdk.Coins) error {
	b.minted = b.minted.Add(amt...)
	return nil
}

func (b *recordingBankKeeper) BurnCoins(_ context.Context, _ string, amt sdk.Coins) error {
	b.burned = b.burned.Add(amt...)
	return nil
}

func (b *recordingBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, _ string, recipient sdk.AccAddress, _ sdk.Coins) error {
	if b.failSendFor != nil && recipient.Equals(b.failSendFor) {
		return fmt.Errorf("simulated send failure for %s", recipient.String())
	}
	b.successfulSendTo = append(b.successfulSendTo, recipient.String())
	return nil
}

// Regression for lumera_ai-1xwqx: when a SendCoinsFromModuleToAccount call
// fails partway through reward distribution, the undistributed portion must
// be burned so minted-but-not-paid coins don't inflate supply and compound
// into later distributions.
func TestDistributeVoteRewards_BurnsUndistributedOnPartialFailure(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	// Two validators, both with reward addresses set.
	val1 := "lumera1validatorone"
	val2 := "lumera1validatortwo"
	addr1 := sdk.AccAddress([]byte("oracle_rewarder_01__"))
	addr2 := sdk.AccAddress([]byte("oracle_rewarder_02__"))
	require.NoError(t, k.setRewardAddress(ctx, val1, addr1.String()))
	require.NoError(t, k.setRewardAddress(ctx, val2, addr2.String()))

	// Fail the send for validator 2 so exactly one reward is "stranded".
	bank := &recordingBankKeeper{failSendFor: addr2}
	k.SetBankKeeper(bank)

	require.NoError(t, k.distributeVoteRewards(ctx, []string{val1, val2}))

	// One successful send, one leftover burn.
	require.Len(t, bank.successfulSendTo, 1, "expected exactly one successful send")

	require.Equal(t, 1, len(bank.burned), "burn coins should contain one denom")
	reward := sdkmath.NewInt(types.DefaultRewardAmount)
	require.True(t, bank.burned.AmountOf(types.DefaultRewardDenom).Equal(reward),
		"expected burn of one reward unit, got %s", bank.burned)

	// Supply accounting: minted = paid + burned.
	mintedAmt := bank.minted.AmountOf(types.DefaultRewardDenom)
	burnedAmt := bank.burned.AmountOf(types.DefaultRewardDenom)
	paidAmt := mintedAmt.Sub(burnedAmt)
	require.True(t, paidAmt.Equal(reward),
		"minted(%s) - burned(%s) must equal one reward; got paid=%s", mintedAmt, burnedAmt, paidAmt)
}

// When every send succeeds, nothing should be burned.
func TestDistributeVoteRewards_NoBurnWhenAllSendsSucceed(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	val := "lumera1validatoronly"
	addr := sdk.AccAddress([]byte("oracle_rewarder_only"))
	require.NoError(t, k.setRewardAddress(ctx, val, addr.String()))

	bank := &recordingBankKeeper{}
	k.SetBankKeeper(bank)

	require.NoError(t, k.distributeVoteRewards(ctx, []string{val}))

	require.Len(t, bank.successfulSendTo, 1)
	require.True(t, bank.burned.IsZero(), "nothing should be burned on full success, got %s", bank.burned)
}
