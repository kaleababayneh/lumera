package types

import (
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func validOwnerAddr() string {
	return sdk.AccAddress([]byte("reserve-test-addr-01")).String()
}

func validCommitment() ReserveCommitment {
	now := time.Unix(1_700_000_000, 0).UTC()
	return ReserveCommitment{
		ID:              "commit-1",
		Owner:           validOwnerAddr(),
		PolicyID:        "policy-1",
		ToolID:          "tool-1",
		Tier:            "silver",
		TotalAmount:     sdk.NewInt64Coin(DefaultCreditDenom, 500_000),
		RemainingAmount: sdk.NewInt64Coin(DefaultCreditDenom, 300_000),
		DiscountBps:     500,
		StartTime:       now,
		ExpireTime:      now.Add(30 * 24 * time.Hour),
		RolloverAllowed: true,
	}
}

// ---------------------------------------------------------------------------
// ReserveCommitment.ValidateBasic
// ---------------------------------------------------------------------------

func TestCommitmentValidateBasic(t *testing.T) {
	require.NoError(t, validCommitment().ValidateBasic())
}

func TestCommitmentValidateBasicEmptyToolID(t *testing.T) {
	c := validCommitment()
	c.ToolID = "" // wildcard — allowed
	require.NoError(t, c.ValidateBasic())
}

func TestCommitmentValidateBasicErrors(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	tests := []struct {
		name   string
		modify func(c *ReserveCommitment)
		errMsg string
	}{
		{
			name:   "empty id",
			modify: func(c *ReserveCommitment) { c.ID = "" },
			errMsg: "commitment id required",
		},
		{
			name:   "padded id",
			modify: func(c *ReserveCommitment) { c.ID = " commit-1 " },
			errMsg: "commitment id cannot contain leading or trailing whitespace",
		},
		{
			name:   "oversized id",
			modify: func(c *ReserveCommitment) { c.ID = strings.Repeat("c", 257) },
			errMsg: "commitment id exceeds 256-byte cap",
		},
		{
			name:   "invalid owner",
			modify: func(c *ReserveCommitment) { c.Owner = "not-bech32" },
			errMsg: "invalid owner address",
		},
		{
			name:   "empty policy id",
			modify: func(c *ReserveCommitment) { c.PolicyID = "" },
			errMsg: "policy id required",
		},
		{
			name:   "padded policy id",
			modify: func(c *ReserveCommitment) { c.PolicyID = " policy-1 " },
			errMsg: "policy id cannot contain leading or trailing whitespace",
		},
		{
			name:   "oversized policy id",
			modify: func(c *ReserveCommitment) { c.PolicyID = strings.Repeat("p", 257) },
			errMsg: "policy id exceeds 256-byte cap",
		},
		{
			name:   "padded tool id",
			modify: func(c *ReserveCommitment) { c.ToolID = " tool-1 " },
			errMsg: "tool id cannot contain leading or trailing whitespace",
		},
		{
			name:   "oversized tool id",
			modify: func(c *ReserveCommitment) { c.ToolID = strings.Repeat("t", 257) },
			errMsg: "tool id exceeds 256-byte cap",
		},
		{
			name:   "whitespace tool id",
			modify: func(c *ReserveCommitment) { c.ToolID = "   " },
			errMsg: "tool id cannot contain only whitespace",
		},
		{
			name:   "empty tier",
			modify: func(c *ReserveCommitment) { c.Tier = "" },
			errMsg: "tier required",
		},
		{
			name:   "padded tier",
			modify: func(c *ReserveCommitment) { c.Tier = " silver " },
			errMsg: "tier cannot contain leading or trailing whitespace",
		},
		{
			name:   "oversized tier",
			modify: func(c *ReserveCommitment) { c.Tier = strings.Repeat("s", 257) },
			errMsg: "tier exceeds 256-byte cap",
		},
		{
			name: "zero total amount",
			modify: func(c *ReserveCommitment) {
				c.TotalAmount = sdk.NewInt64Coin(DefaultCreditDenom, 0)
			},
			errMsg: "total amount invalid",
		},
		{
			name: "negative remaining amount",
			modify: func(c *ReserveCommitment) {
				c.RemainingAmount = sdk.Coin{Denom: DefaultCreditDenom, Amount: sdkmath.NewInt(-1)}
			},
			errMsg: "remaining amount out of bounds",
		},
		{
			name: "nil remaining amount",
			modify: func(c *ReserveCommitment) {
				c.RemainingAmount = sdk.Coin{Denom: DefaultCreditDenom}
			},
			errMsg: "remaining amount invalid",
		},
		{
			name: "invalid remaining denom",
			modify: func(c *ReserveCommitment) {
				c.RemainingAmount = sdk.Coin{Denom: "bad denom", Amount: sdkmath.NewInt(10)}
			},
			errMsg: "remaining amount denom",
		},
		{
			name: "remaining denom mismatch",
			modify: func(c *ReserveCommitment) {
				c.RemainingAmount = sdk.NewInt64Coin("uatom", 100)
			},
			errMsg: "remaining amount denom",
		},
		{
			name: "remaining exceeds total",
			modify: func(c *ReserveCommitment) {
				c.TotalAmount = sdk.NewInt64Coin(DefaultCreditDenom, 100)
				c.RemainingAmount = sdk.NewInt64Coin(DefaultCreditDenom, 200)
			},
			errMsg: "remaining amount out of bounds",
		},
		{
			name:   "discount exceeds 100%",
			modify: func(c *ReserveCommitment) { c.DiscountBps = 10_001 },
			errMsg: "discount exceeds 100%",
		},
		{
			name:   "zero start time",
			modify: func(c *ReserveCommitment) { c.StartTime = time.Time{} },
			errMsg: "start time required",
		},
		{
			name: "expiry equal to start",
			modify: func(c *ReserveCommitment) {
				c.StartTime = now
				c.ExpireTime = now
			},
			errMsg: "expiry must be after start",
		},
		{
			name: "expiry before start",
			modify: func(c *ReserveCommitment) {
				c.StartTime = now
				c.ExpireTime = now.Add(-time.Hour)
			},
			errMsg: "expiry must be after start",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validCommitment()
			tc.modify(&c)
			err := c.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

// ---------------------------------------------------------------------------
// RemainingRatio
// ---------------------------------------------------------------------------

func TestRemainingRatio(t *testing.T) {
	c := validCommitment()
	c.TotalAmount = sdk.NewInt64Coin(DefaultCreditDenom, 1000)
	c.RemainingAmount = sdk.NewInt64Coin(DefaultCreditDenom, 500)
	require.Equal(t, uint32(5_000), c.RemainingRatio()) // 50%
}

func TestRemainingRatioFull(t *testing.T) {
	c := validCommitment()
	c.TotalAmount = sdk.NewInt64Coin(DefaultCreditDenom, 1000)
	c.RemainingAmount = sdk.NewInt64Coin(DefaultCreditDenom, 1000)
	require.Equal(t, uint32(10_000), c.RemainingRatio()) // 100%
}

func TestRemainingRatioZero(t *testing.T) {
	c := validCommitment()
	c.TotalAmount = sdk.NewInt64Coin(DefaultCreditDenom, 1000)
	c.RemainingAmount = sdk.NewInt64Coin(DefaultCreditDenom, 0)
	require.Equal(t, uint32(0), c.RemainingRatio())
}

func TestRemainingRatioZeroTotal(t *testing.T) {
	c := validCommitment()
	c.TotalAmount = sdk.NewInt64Coin(DefaultCreditDenom, 0)
	c.RemainingAmount = sdk.NewInt64Coin(DefaultCreditDenom, 0)
	require.Equal(t, uint32(0), c.RemainingRatio())
}

// TestRemainingRatio_ClampsRemainingExceedsTotal pins the defensive
// clamp at 10_000 for the pathological case where remaining > total
// (a corrupt state that shouldn't occur in production but might
// surface from a migration bug or bad genesis import). The helper
// returns 10_000 (100%) rather than a value that could overflow
// the uint32 cast or confuse downstream UI showing "105%".
// Regression guard against a refactor that dropped the clamp.
func TestRemainingRatio_ClampsRemainingExceedsTotal(t *testing.T) {
	c := validCommitment()
	c.TotalAmount = sdk.NewInt64Coin(DefaultCreditDenom, 1_000)
	// Corrupt state: remaining 1.5x total.
	c.RemainingAmount = sdk.NewInt64Coin(DefaultCreditDenom, 1_500)
	require.Equal(t, uint32(10_000), c.RemainingRatio(),
		"remaining>total must clamp to 10_000 (100%), not leak over-unity ratio")
}

// TestRemainingRatio_ClampsFarExceedsTotal exercises an extreme
// over-unity case (remaining 1_000_000 vs total 1_000) to confirm
// the clamp holds regardless of ratio magnitude. This also guards
// against an off-by-one where the clamp was `bps >= 10_000` —
// valid boundary (exactly 100%) would then be silently reduced to
// 9_999, which the existing TestRemainingRatioFull test already
// catches, but the asymmetric catastrophic case deserves its own
// assertion.
func TestRemainingRatio_ClampsFarExceedsTotal(t *testing.T) {
	c := validCommitment()
	c.TotalAmount = sdk.NewInt64Coin(DefaultCreditDenom, 1_000)
	c.RemainingAmount = sdk.NewInt64Coin(DefaultCreditDenom, 1_000_000)
	require.Equal(t, uint32(10_000), c.RemainingRatio())
}

// ---------------------------------------------------------------------------
// ApplyDiscount
// ---------------------------------------------------------------------------

func TestApplyDiscountZero(t *testing.T) {
	coin := sdk.NewInt64Coin(DefaultCreditDenom, 10_000)
	result := ApplyDiscount(coin, 0)
	require.Equal(t, coin, result)
}

func TestApplyDiscount250Bps(t *testing.T) {
	coin := sdk.NewInt64Coin(DefaultCreditDenom, 10_000)
	result := ApplyDiscount(coin, 250) // 2.5%
	// 10000 * (10000-250) / 10000 = 10000 * 9750 / 10000 = 9750
	require.Equal(t, sdkmath.NewInt(9750), result.Amount)
	require.Equal(t, DefaultCreditDenom, result.Denom)
}

func TestApplyDiscount10000Bps(t *testing.T) {
	coin := sdk.NewInt64Coin(DefaultCreditDenom, 10_000)
	result := ApplyDiscount(coin, 10_000) // 100%
	require.True(t, result.Amount.IsZero())
}

func TestApplyDiscount5000Bps(t *testing.T) {
	coin := sdk.NewInt64Coin(DefaultCreditDenom, 10_000)
	result := ApplyDiscount(coin, 5_000) // 50%
	require.Equal(t, sdkmath.NewInt(5_000), result.Amount)
}

// TestApplyDiscount_BoundAndCurrencyMetamorphic asserts three
// structural invariants across a sweep of input amounts and discounts:
//
//  1. Denom is preserved verbatim regardless of discountBps.
//  2. Result amount is in [0, input.Amount] — a discount can never
//     inflate the charge.
//  3. discountBps values above 10_000 are clamped to 10_000 (zero
//     output), not taken literally (which would produce negative
//     coins via the (10000 - disc) term).
//
// Existing unit tests cover three specific discount values plus zero;
// this pins the bound/clamp contract across a wide domain so any
// refactor that changed the sign of the (10000 - disc) factor or
// dropped the cap would fail here.
func TestApplyDiscount_BoundAndCurrencyMetamorphic(t *testing.T) {
	denoms := []string{DefaultCreditDenom, "uatom", "inj"}
	amounts := []int64{0, 1, 100, 10_000, 999_999}
	discounts := []uint32{0, 1, 500, 2500, 5000, 7500, 9999, 10_000, 15_000, 999_999}
	for _, denom := range denoms {
		for _, amt := range amounts {
			for _, disc := range discounts {
				coin := sdk.NewInt64Coin(denom, amt)
				got := ApplyDiscount(coin, disc)
				if got.Denom != denom {
					t.Fatalf("Denom drift: amt=%d disc=%d denom=%q got %q",
						amt, disc, denom, got.Denom)
				}
				if got.Amount.IsNegative() {
					t.Fatalf("negative amount: amt=%d disc=%d got=%s",
						amt, disc, got.Amount)
				}
				if got.Amount.GT(coin.Amount) {
					t.Fatalf("discount inflated amount: amt=%d disc=%d got=%s",
						amt, disc, got.Amount)
				}
			}
		}
	}
}

// TestApplyDiscount_MonotonicityMetamorphic asserts that for fixed
// input amount, increasing discountBps never produces a larger result
// amount. Truncating integer math could theoretically introduce a
// non-monotone step if the divisor or rounding direction changed;
// lock it in.
func TestApplyDiscount_MonotonicityMetamorphic(t *testing.T) {
	coin := sdk.NewInt64Coin(DefaultCreditDenom, 1_000_000)
	var prev = coin.Amount
	for disc := uint32(0); disc <= 10_000; disc += 50 {
		got := ApplyDiscount(coin, disc).Amount
		if got.GT(prev) {
			t.Fatalf("amount rose with discount: disc=%d prev=%s got=%s", disc, prev, got)
		}
		prev = got
	}
	// At 100% discount the result must be exactly zero.
	if got := ApplyDiscount(coin, 10_000).Amount; !got.IsZero() {
		t.Fatalf("100%% discount must zero out amount, got %s", got)
	}
}

// TestRemainingRatio_BoundAndMonotonicityMetamorphic asserts that
// RemainingRatio stays in [0, 10_000] and is non-decreasing in
// RemainingAmount for a fixed TotalAmount. Consumers compare this
// ratio against tier thresholds to decide renewal vs expiry; any
// regression that produced out-of-range or non-monotone values would
// silently change eligibility.
func TestRemainingRatio_BoundAndMonotonicityMetamorphic(t *testing.T) {
	denom := DefaultCreditDenom
	total := sdk.NewInt64Coin(denom, 1_000)
	// Zero-total edge: always 0.
	empty := ReserveCommitment{
		TotalAmount:     sdk.NewInt64Coin(denom, 0),
		RemainingAmount: sdk.NewInt64Coin(denom, 0),
	}
	if got := empty.RemainingRatio(); got != 0 {
		t.Fatalf("zero-total must yield 0 ratio, got %d", got)
	}

	var prev uint32
	for remaining := int64(0); remaining <= 1_000; remaining += 10 {
		c := ReserveCommitment{
			TotalAmount:     total,
			RemainingAmount: sdk.NewInt64Coin(denom, remaining),
		}
		got := c.RemainingRatio()
		if got > 10_000 {
			t.Fatalf("ratio exceeds 10_000: remaining=%d got=%d", remaining, got)
		}
		if got < prev {
			t.Fatalf("ratio dropped as remaining grew: remaining=%d prev=%d got=%d",
				remaining, prev, got)
		}
		prev = got
	}
	// Remaining > Total (unusual, but possible during partial-fill race): clamped to 10_000.
	over := ReserveCommitment{
		TotalAmount:     total,
		RemainingAmount: sdk.NewInt64Coin(denom, 2_000),
	}
	if got := over.RemainingRatio(); got != 10_000 {
		t.Fatalf("over-total must clamp to 10_000, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// TierConfig.ValidateBasic
// ---------------------------------------------------------------------------

func TestTierConfigValidateBasic(t *testing.T) {
	tier := TierConfig{
		Name:                "bronze",
		MinCommitmentAmount: sdkmath.NewInt(100_000),
		DiscountBps:         250,
		DefaultDurationSec:  30 * 24 * 60 * 60,
		MaxActivePerPolicy:  2,
		RolloverAllowed:     true,
	}
	require.NoError(t, tier.ValidateBasic())
}

func TestTierConfigValidateBasicErrors(t *testing.T) {
	tests := []struct {
		name   string
		modify func(tc *TierConfig)
		errMsg string
	}{
		{
			name:   "empty name",
			modify: func(tc *TierConfig) { tc.Name = "" },
			errMsg: "tier name required",
		},
		{
			name:   "whitespace name",
			modify: func(tc *TierConfig) { tc.Name = "   " },
			errMsg: "tier name required",
		},
		{
			name:   "padded name",
			modify: func(tc *TierConfig) { tc.Name = " bronze " },
			errMsg: "tier name cannot contain leading or trailing whitespace",
		},
		{
			name:   "zero min commitment",
			modify: func(tc *TierConfig) { tc.MinCommitmentAmount = sdkmath.ZeroInt() },
			errMsg: "positive minimum commitment",
		},
		{
			name:   "negative min commitment",
			modify: func(tc *TierConfig) { tc.MinCommitmentAmount = sdkmath.NewInt(-1) },
			errMsg: "positive minimum commitment",
		},
		{
			name:   "discount exceeds 100%",
			modify: func(tc *TierConfig) { tc.DiscountBps = 10_001 },
			errMsg: "discount exceeds 100%",
		},
		{
			name:   "zero duration",
			modify: func(tc *TierConfig) { tc.DefaultDurationSec = 0 },
			errMsg: "non-zero duration",
		},
		{
			name:   "duration exceeds time.Duration limit",
			modify: func(tc *TierConfig) { tc.DefaultDurationSec = maxTierDefaultDurationSec + 1 },
			errMsg: "default duration exceeds maximum safe duration seconds",
		},
		{
			name:   "zero max active",
			modify: func(tc *TierConfig) { tc.MaxActivePerPolicy = 0 },
			errMsg: "max active per policy > 0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tier := TierConfig{
				Name:                "test",
				MinCommitmentAmount: sdkmath.NewInt(100_000),
				DiscountBps:         250,
				DefaultDurationSec:  86400,
				MaxActivePerPolicy:  2,
			}
			tc.modify(&tier)
			err := tier.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

// TestTierConfigValidateBasic_MaxDiscountAccepted pins the strict-GT
// boundary on DiscountBps: exactly 10_000 (100% discount, i.e.
// "full waiver") must be accepted. Regression guard against a
// refactor flipping the check to `>= 10_000` which would silently
// reject governance-approved 100% discount tiers (typically rare
// but legal for promo/onboarding tiers).
func TestTierConfigValidateBasic_MaxDiscountAccepted(t *testing.T) {
	tier := TierConfig{
		Name:                "promo",
		MinCommitmentAmount: sdkmath.NewInt(100_000),
		DiscountBps:         10_000, // exactly 100%
		DefaultDurationSec:  86400,
		MaxActivePerPolicy:  1,
	}
	require.NoError(t, tier.ValidateBasic(),
		"DiscountBps == 10_000 (full waiver) must be accepted — boundary is strict GT")
}

// ---------------------------------------------------------------------------
// Params.ValidateBasic
// ---------------------------------------------------------------------------

func TestParamsValidateBasic(t *testing.T) {
	p := DefaultParams()
	require.NoError(t, p.ValidateBasic())
}

func TestParamsValidateBasicNil(t *testing.T) {
	var p *Params
	require.Error(t, p.ValidateBasic())
}

func TestParamsValidateBasicInvalidDenom(t *testing.T) {
	p := DefaultParams()
	p.CreditDenom = "" // invalid
	require.Error(t, p.ValidateBasic())
}

func TestParamsValidateBasicEmptyTiers(t *testing.T) {
	p := DefaultParams()
	p.Tiers = nil
	err := p.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one tier")
}

func TestParamsValidateBasicDuplicateTier(t *testing.T) {
	p := DefaultParams()
	p.Tiers = append(p.Tiers, p.Tiers[0]) // duplicate bronze
	err := p.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate tier name")
}

func TestParamsValidateBasicInvalidTier(t *testing.T) {
	p := DefaultParams()
	p.Tiers[0].Name = "" // invalid
	require.Error(t, p.ValidateBasic())
}

// ---------------------------------------------------------------------------
// FindTier
// ---------------------------------------------------------------------------

func TestFindTier(t *testing.T) {
	p := DefaultParams()
	tier, found := p.FindTier("silver")
	require.True(t, found)
	require.Equal(t, "silver", tier.Name)
	require.Equal(t, uint32(500), tier.DiscountBps)
}

func TestFindTierNotFound(t *testing.T) {
	p := DefaultParams()
	_, found := p.FindTier("platinum")
	require.False(t, found)
}

func TestFindTierNilParams(t *testing.T) {
	var p *Params
	_, found := p.FindTier("bronze")
	require.False(t, found)
}

// ---------------------------------------------------------------------------
// DefaultParams sanity
// ---------------------------------------------------------------------------

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	require.Equal(t, DefaultCreditDenom, p.CreditDenom)
	require.Len(t, p.Tiers, 3)
	require.Equal(t, "bronze", p.Tiers[0].Name)
	require.Equal(t, "silver", p.Tiers[1].Name)
	require.Equal(t, "gold", p.Tiers[2].Name)
	require.NoError(t, p.ValidateBasic())
}

// ---------------------------------------------------------------------------
// Module constants
// ---------------------------------------------------------------------------

func TestModuleConstants(t *testing.T) {
	require.Equal(t, "reserve", ModuleName)
	require.Equal(t, "reserve", StoreKey)
	require.Equal(t, "mem_reserve", MemStoreKey)
	require.Equal(t, "ulac", DefaultCreditDenom)
}

// ---------------------------------------------------------------------------
// Key prefix uniqueness
// ---------------------------------------------------------------------------

func TestKeyPrefixesUnique(t *testing.T) {
	prefixes := [][]byte{
		ParamsKeyPrefix, CommitmentKeyPrefix,
		CommitmentByPolicyKeyPrefix, CommitmentSeqKeyPrefix,
		CommitmentExpiryKeyPrefix,
	}
	seen := make(map[byte]struct{})
	for _, p := range prefixes {
		require.Len(t, p, 1, "prefix should be length 1")
		_, exists := seen[p[0]]
		require.False(t, exists, "duplicate prefix byte 0x%02x", p[0])
		seen[p[0]] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	errs := []error{
		ErrTierNotFound, ErrInvalidCommitment, ErrCommitmentNotFound,
		ErrCommitmentExpired, ErrInsufficientCapacity, ErrCreditDenomMismatch,
	}
	for _, e := range errs {
		require.NotNil(t, e)
		require.NotEmpty(t, e.Error())
	}

	type coder interface{ ABCICode() uint32 }
	codes := make(map[uint32]string)
	for _, e := range errs {
		c, ok := e.(coder)
		if !ok {
			continue
		}
		code := c.ABCICode()
		prev, dup := codes[code]
		require.False(t, dup, "duplicate error code %d: %q and %q", code, prev, e.Error())
		codes[code] = e.Error()
	}
}

// ---------------------------------------------------------------------------
// Discount boundary
// ---------------------------------------------------------------------------

func TestApplyDiscountOverflow(t *testing.T) {
	coin := sdk.NewInt64Coin(DefaultCreditDenom, 1)
	result := ApplyDiscount(coin, 9_999)    // 99.99%
	require.True(t, result.Amount.IsZero()) // rounds to zero
}

func TestTierConfigDiscountAtMax(t *testing.T) {
	tier := TierConfig{
		Name:                "max",
		MinCommitmentAmount: sdkmath.NewInt(100),
		DiscountBps:         10_000,
		DefaultDurationSec:  86400,
		MaxActivePerPolicy:  1,
	}
	require.NoError(t, tier.ValidateBasic())
}

// TestParams_ValidateBasic_RejectsOversizedTiersSlice pins the
// cap on the Tiers slice. Governance-gated but defense-in-depth
// prevents a malformed gov proposal from exploding module state.
func TestParams_ValidateBasic_RejectsOversizedTiersSlice(t *testing.T) {
	tiers := make([]TierConfig, MaxTiers+1)
	for i := range tiers {
		tiers[i] = TierConfig{
			Name:                fmt.Sprintf("tier-%d", i),
			MinCommitmentAmount: sdkmath.NewInt(100),
			DiscountBps:         100,
			DefaultDurationSec:  86400,
			MaxActivePerPolicy:  1,
		}
	}
	p := &Params{
		CreditDenom: DefaultCreditDenom,
		Tiers:       tiers,
	}
	err := p.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "tiers")
	require.Contains(t, err.Error(), "cap")
}

// TestTierConfig_ValidateBasic_RejectsOversizedName pins the
// per-entry length cap on TierConfig.Name.
func TestTierConfig_ValidateBasic_RejectsOversizedName(t *testing.T) {
	huge := make([]byte, MaxTierNameLen+1)
	for i := range huge {
		huge[i] = 'n'
	}
	tier := TierConfig{
		Name:                string(huge),
		MinCommitmentAmount: sdkmath.NewInt(100),
		DiscountBps:         100,
		DefaultDurationSec:  86400,
		MaxActivePerPolicy:  1,
	}
	err := tier.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "tier name")
	require.Contains(t, err.Error(), "cap")
}
