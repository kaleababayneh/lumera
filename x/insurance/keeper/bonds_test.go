//go:build cosmos && cosmos_full && todo_bonds

package keeper_test

import (
	"bytes"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
)

func bondTestPublisher(seed byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20)).String()
}

func defaultSLOTargets() keeper.SLOTargets {
	return keeper.SLOTargets{
		MaxLatencyMs:       250,
		MinAvailabilityBps: 9990, // 99.9%
		MaxErrorRateBps:    100,  // 1%
		MaxCostVarBps:      1000, // 10%
	}
}

func TestBond_PostAndGet(t *testing.T) {
	fixture := setupKeeperTest(t)

	bond, err := fixture.keeper.PostBond(
		fixture.ctx,
		"tool-bond-1",
		bondTestPublisher(1),
		sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1_500_000_000))),
		defaultSLOTargets(),
	)
	require.NoError(t, err)
	require.NotNil(t, bond)
	require.Equal(t, "active", bond.Status)

	got, err := fixture.keeper.GetBond(fixture.ctx, bond.BondID)
	require.NoError(t, err)
	require.Equal(t, bond.BondID, got.BondID)
	require.Equal(t, "tool-bond-1", got.ToolID)
	require.Equal(t, bondTestPublisher(1), got.PublisherID)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1_500_000_000))), got.Amount)
}

func TestBond_PostRejectsBelowMinimum(t *testing.T) {
	fixture := setupKeeperTest(t)

	bond, err := fixture.keeper.PostBond(
		fixture.ctx,
		"tool-bond-min",
		bondTestPublisher(2),
		sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(999_999_999))),
		defaultSLOTargets(),
	)
	require.Error(t, err)
	require.Nil(t, bond)
	require.Contains(t, err.Error(), "bond amount below minimum")
}

func TestBond_SlashLifecycleAndCap(t *testing.T) {
	fixture := setupKeeperTest(t)

	bond, err := fixture.keeper.PostBond(
		fixture.ctx,
		"tool-bond-slash",
		bondTestPublisher(3),
		sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(2_000_000_000))),
		defaultSLOTargets(),
	)
	require.NoError(t, err)

	err = fixture.keeper.SlashBond(
		fixture.ctx,
		bond.BondID,
		"latency",
		sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(500_000_000))),
		"claim-1",
	)
	require.NoError(t, err)

	afterFirst, err := fixture.keeper.GetBond(fixture.ctx, bond.BondID)
	require.NoError(t, err)
	require.Equal(t, "active", afterFirst.Status)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1_500_000_000))), afterFirst.Amount)
	require.Len(t, afterFirst.SlashRecord, 1)

	// Slash more than the remaining amount; keeper should cap to remainder and deplete the bond.
	err = fixture.keeper.SlashBond(
		fixture.ctx,
		bond.BondID,
		"cost_overrun",
		sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(9_000_000_000))),
		"claim-2",
	)
	require.NoError(t, err)

	afterSecond, err := fixture.keeper.GetBond(fixture.ctx, bond.BondID)
	require.NoError(t, err)
	require.Equal(t, "slashed", afterSecond.Status)
	require.True(t, afterSecond.Amount.IsZero())
	require.Len(t, afterSecond.SlashRecord, 2)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1_500_000_000))), afterSecond.SlashRecord[1].Amount)
}

func TestBond_WithdrawOwnershipAndStatus(t *testing.T) {
	fixture := setupKeeperTest(t)

	publisher := bondTestPublisher(4)
	bond, err := fixture.keeper.PostBond(
		fixture.ctx,
		"tool-bond-withdraw",
		publisher,
		sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1_200_000_000))),
		defaultSLOTargets(),
	)
	require.NoError(t, err)

	err = fixture.keeper.WithdrawBond(fixture.ctx, bond.BondID, bondTestPublisher(5))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	err = fixture.keeper.WithdrawBond(fixture.ctx, bond.BondID, publisher)
	require.NoError(t, err)

	updated, err := fixture.keeper.GetBond(fixture.ctx, bond.BondID)
	require.NoError(t, err)
	require.Equal(t, "withdrawn", updated.Status)
}

func TestBond_CalculateSlashAmountCapsAtHalf(t *testing.T) {
	fixture := setupKeeperTest(t)

	bond := &keeper.SLABond{
		Amount: sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1_000_000_000))),
	}

	highSeverity := fixture.keeper.CalculateSlashAmount(fixture.ctx, bond, "cost_overrun", decimal.NewFromInt(10))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(500_000_000))), highSeverity)

	defaultSeverity := fixture.keeper.CalculateSlashAmount(fixture.ctx, bond, "unknown", decimal.NewFromInt(1))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(50_000_000))), defaultSeverity)
}

// Regression for lumera_ai-25fio: tiny bonds where amount * slashRate
// truncates below 1 must still be slashed (rounded up to at least 1 unit),
// otherwise micro-bonds escape any penalty whatsoever.
func TestBond_CalculateSlashAmountMicroBondNotZero(t *testing.T) {
	fixture := setupKeeperTest(t)

	// 10 ulac at 5% = 0.5 ulac; old code truncated to 0 and slashed nothing.
	bond := &keeper.SLABond{
		Amount: sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(10))),
	}
	got := fixture.keeper.CalculateSlashAmount(fixture.ctx, bond, "latency", decimal.NewFromInt(1))
	require.Equal(t,
		sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1))),
		got,
		"micro-bond must still receive a 1-unit slash, not zero",
	)
}

// CalculateSlashAmount must never return more than the bond itself, even when
// severity * baseRate would compute a slash exceeding the per-coin amount.
func TestBond_CalculateSlashAmountCapsAtBondSize(t *testing.T) {
	fixture := setupKeeperTest(t)

	bond := &keeper.SLABond{
		Amount: sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(3))),
	}
	// cost_overrun 15% * severity 10 = 150% pre-cap, then maxRate caps to 50%.
	// 50% of 3 = 1.5, ceil -> 2, well under bond size -> 2 returned.
	got := fixture.keeper.CalculateSlashAmount(fixture.ctx, bond, "cost_overrun", decimal.NewFromInt(10))
	require.Equal(t,
		sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(2))),
		got,
	)
}

// A zero-severity slash must produce no coins (no negative or zero entries
// added to the coin set).
func TestBond_CalculateSlashAmountZeroSeverityReturnsEmpty(t *testing.T) {
	fixture := setupKeeperTest(t)

	bond := &keeper.SLABond{
		Amount: sdk.NewCoins(sdk.NewCoin("lac", sdkmath.NewInt(1_000_000_000))),
	}
	got := fixture.keeper.CalculateSlashAmount(fixture.ctx, bond, "latency", decimal.Zero)
	require.True(t, got.IsZero(), "zero severity must produce an empty slash, got %s", got.String())
}
