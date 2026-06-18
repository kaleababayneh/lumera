
package types

import (
	"testing"
)

// ---------------------------------------------------------------------------
// DefaultGasCostProfile
// ---------------------------------------------------------------------------

func TestDefaultGasCostProfile(t *testing.T) {
	t.Parallel()
	p := DefaultGasCostProfile()

	if p.LockGas != DefaultLockGas {
		t.Errorf("LockGas = %d, want %d", p.LockGas, DefaultLockGas)
	}
	if p.SettleGas != DefaultSettleGas {
		t.Errorf("SettleGas = %d, want %d", p.SettleGas, DefaultSettleGas)
	}
	if p.InvocationGas != DefaultInvocationGas {
		t.Errorf("InvocationGas = %d, want %d", p.InvocationGas, DefaultInvocationGas)
	}
	if p.CacheDiscountBPS != DefaultCacheDiscountBPS {
		t.Errorf("CacheDiscountBPS = %d, want %d", p.CacheDiscountBPS, DefaultCacheDiscountBPS)
	}
}

func TestDefaultGasConstants(t *testing.T) {
	t.Parallel()
	if DefaultLockGas != 20_000 {
		t.Errorf("DefaultLockGas = %d, want 20000", DefaultLockGas)
	}
	if DefaultSettleGas != 35_000 {
		t.Errorf("DefaultSettleGas = %d, want 35000", DefaultSettleGas)
	}
	if DefaultInvocationGas != 15_000 {
		t.Errorf("DefaultInvocationGas = %d, want 15000", DefaultInvocationGas)
	}
	if DefaultCacheDiscountBPS != 0 {
		t.Errorf("DefaultCacheDiscountBPS = %d, want 0", DefaultCacheDiscountBPS)
	}
	if MaxGasDiscountBPS != 10_000 {
		t.Errorf("MaxGasDiscountBPS = %d, want 10000", MaxGasDiscountBPS)
	}
}

// ---------------------------------------------------------------------------
// NormalizeGasProfile
// ---------------------------------------------------------------------------

func TestNormalizeGasProfile_Nil(t *testing.T) {
	t.Parallel()
	p := NormalizeGasProfile(nil)
	def := DefaultGasCostProfile()
	if p != def {
		t.Errorf("nil profile should return defaults, got %+v", p)
	}
}

func TestNormalizeGasProfile_AllZeros(t *testing.T) {
	t.Parallel()
	// Zero values in proto should fall through to defaults
	proto := &GasProfile{
		LockGas:             0,
		SettleGas:           0,
		InvocationGas:       0,
		CacheHitDiscountBps: 0,
	}
	p := NormalizeGasProfile(proto)
	def := DefaultGasCostProfile()
	if p != def {
		t.Errorf("all-zero profile should return defaults, got %+v", p)
	}
}

func TestNormalizeGasProfile_CustomValues(t *testing.T) {
	t.Parallel()
	proto := &GasProfile{
		LockGas:             50_000,
		SettleGas:           100_000,
		InvocationGas:       25_000,
		CacheHitDiscountBps: 3000,
	}
	p := NormalizeGasProfile(proto)
	if p.LockGas != 50_000 {
		t.Errorf("LockGas = %d, want 50000", p.LockGas)
	}
	if p.SettleGas != 100_000 {
		t.Errorf("SettleGas = %d, want 100000", p.SettleGas)
	}
	if p.InvocationGas != 25_000 {
		t.Errorf("InvocationGas = %d, want 25000", p.InvocationGas)
	}
	if p.CacheDiscountBPS != 3000 {
		t.Errorf("CacheDiscountBPS = %d, want 3000", p.CacheDiscountBPS)
	}
}

func TestNormalizeGasProfile_PartialOverride(t *testing.T) {
	t.Parallel()
	// Only set LockGas, rest should default
	proto := &GasProfile{
		LockGas: 99_000,
	}
	p := NormalizeGasProfile(proto)
	if p.LockGas != 99_000 {
		t.Errorf("LockGas = %d, want 99000", p.LockGas)
	}
	if p.SettleGas != DefaultSettleGas {
		t.Errorf("SettleGas = %d, want %d (default)", p.SettleGas, DefaultSettleGas)
	}
	if p.InvocationGas != DefaultInvocationGas {
		t.Errorf("InvocationGas = %d, want %d (default)", p.InvocationGas, DefaultInvocationGas)
	}
	if p.CacheDiscountBPS != DefaultCacheDiscountBPS {
		t.Errorf("CacheDiscountBPS = %d, want %d (default)", p.CacheDiscountBPS, DefaultCacheDiscountBPS)
	}
}

// ---------------------------------------------------------------------------
// CacheAdjusted
// ---------------------------------------------------------------------------

func TestCacheAdjusted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		profile  GasCostProfile
		base     uint64
		cacheHit bool
		expected uint64
	}{
		{
			"no_cache_hit",
			GasCostProfile{CacheDiscountBPS: 5000},
			10_000, false, 10_000,
		},
		{
			"cache_hit_zero_discount",
			GasCostProfile{CacheDiscountBPS: 0},
			10_000, true, 10_000,
		},
		{
			"cache_hit_50pct_discount",
			GasCostProfile{CacheDiscountBPS: 5000},
			10_000, true, 5_000,
		},
		{
			"cache_hit_100pct_discount",
			GasCostProfile{CacheDiscountBPS: 10_000},
			10_000, true, 0,
		},
		{
			"cache_hit_25pct_discount",
			GasCostProfile{CacheDiscountBPS: 2500},
			20_000, true, 15_000,
		},
		{
			"cache_hit_small_base",
			GasCostProfile{CacheDiscountBPS: 5000},
			1, true, 1, // 1 * 5000 / 10000 = 0 discount
		},
		{
			"base_zero",
			GasCostProfile{CacheDiscountBPS: 5000},
			0, true, 0,
		},
		{
			"discount_exceeds_base",
			// This can happen if CacheDiscountBPS > 10000
			GasCostProfile{CacheDiscountBPS: 20_000},
			10_000, true, 0, // discount = 20000, >= base, returns 0
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.profile.CacheAdjusted(tc.base, tc.cacheHit)
			if got != tc.expected {
				t.Errorf("CacheAdjusted(%d, %v) = %d, want %d",
					tc.base, tc.cacheHit, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NormalizeToolGasProfile
// ---------------------------------------------------------------------------

func TestNormalizeToolGasProfile_NilTool(t *testing.T) {
	t.Parallel()
	err := NormalizeToolGasProfile(nil)
	if err == nil {
		t.Error("expected error for nil tool card")
	}
}

func TestNormalizeToolGasProfile_NilGasProfile(t *testing.T) {
	t.Parallel()
	tool := &ToolCard{GasProfile: nil}
	err := NormalizeToolGasProfile(tool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.GasProfile == nil {
		t.Fatal("GasProfile should be set after normalization")
	}
	if tool.GasProfile.LockGas != DefaultLockGas {
		t.Errorf("LockGas = %d, want %d", tool.GasProfile.LockGas, DefaultLockGas)
	}
	if tool.GasProfile.SettleGas != DefaultSettleGas {
		t.Errorf("SettleGas = %d, want %d", tool.GasProfile.SettleGas, DefaultSettleGas)
	}
}

func TestNormalizeToolGasProfile_CustomValues(t *testing.T) {
	t.Parallel()
	tool := &ToolCard{
		GasProfile: &GasProfile{
			LockGas:             50_000,
			SettleGas:           70_000,
			InvocationGas:       30_000,
			CacheHitDiscountBps: 5000,
		},
	}
	err := NormalizeToolGasProfile(tool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.GasProfile.LockGas != 50_000 {
		t.Errorf("LockGas = %d, want 50000", tool.GasProfile.LockGas)
	}
	if tool.GasProfile.CacheHitDiscountBps != 5000 {
		t.Errorf("CacheHitDiscountBps = %d, want 5000", tool.GasProfile.CacheHitDiscountBps)
	}
}

func TestNormalizeToolGasProfile_ExceedsMaxDiscount(t *testing.T) {
	t.Parallel()
	tool := &ToolCard{
		GasProfile: &GasProfile{
			CacheHitDiscountBps: MaxGasDiscountBPS + 1,
		},
	}
	err := NormalizeToolGasProfile(tool)
	if err == nil {
		t.Error("expected error when cache discount exceeds max")
	}
}

func TestNormalizeToolGasProfile_ExactMaxDiscount(t *testing.T) {
	t.Parallel()
	tool := &ToolCard{
		GasProfile: &GasProfile{
			CacheHitDiscountBps: MaxGasDiscountBPS,
		},
	}
	err := NormalizeToolGasProfile(tool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.GasProfile.CacheHitDiscountBps != MaxGasDiscountBPS {
		t.Errorf("CacheHitDiscountBps = %d, want %d", tool.GasProfile.CacheHitDiscountBps, MaxGasDiscountBPS)
	}
}

// TestCacheAdjusted_BoundMetamorphic asserts the core pricing
// invariant: CacheAdjusted(base, true) is never strictly greater
// than base, regardless of the discount setting. This is the whole
// point of a "discount" — it can reduce or zero the charge, never
// inflate it. A sign flip in the (base - discount) subtraction would
// surface here immediately. Also pins the cacheHit=false invariant:
// returns base unchanged for any discount value.
func TestCacheAdjusted_BoundMetamorphic(t *testing.T) {
	t.Parallel()
	bases := []uint64{0, 1, 100, 10_000, 1_000_000, ^uint64(0)}
	// Sweep discounts including boundary values and a handful above
	// MaxGasDiscountBPS to exercise the "discount >= base" short-circuit.
	discounts := []uint64{0, 1, 100, 2500, 5000, 7500, 9999, 10_000, 15_000, 999_999}
	for _, base := range bases {
		for _, disc := range discounts {
			p := GasCostProfile{CacheDiscountBPS: disc}
			// cacheHit=false: discount must be ignored entirely.
			if got := p.CacheAdjusted(base, false); got != base {
				t.Fatalf("cacheHit=false base=%d disc=%d: got %d, want %d",
					base, disc, got, base)
			}
			// cacheHit=true: output must never exceed base.
			got := p.CacheAdjusted(base, true)
			if got > base {
				t.Fatalf("cacheHit=true base=%d disc=%d: got %d exceeds base %d — discount cannot inflate",
					base, disc, got, base)
			}
		}
	}
}

// TestCacheAdjusted_MonotonicityInDiscountMetamorphic asserts that
// for a fixed base and cacheHit=true, increasing CacheDiscountBPS
// never increases the adjusted value. Truncating integer math could
// theoretically introduce a non-monotone step if the rounding ever
// flipped direction; lock against that.
func TestCacheAdjusted_MonotonicityInDiscountMetamorphic(t *testing.T) {
	t.Parallel()
	base := uint64(1_000_000)
	var prev = base // start at no-discount baseline
	for disc := uint64(0); disc <= MaxGasDiscountBPS; disc += 100 {
		p := GasCostProfile{CacheDiscountBPS: disc}
		got := p.CacheAdjusted(base, true)
		if got > prev {
			t.Fatalf("adjusted rose with discount: base=%d disc=%d prev=%d got=%d",
				base, disc, prev, got)
		}
		prev = got
	}
	// End of sweep: at 100% discount, adjusted value is 0.
	p := GasCostProfile{CacheDiscountBPS: MaxGasDiscountBPS}
	if got := p.CacheAdjusted(base, true); got != 0 {
		t.Fatalf("100%% discount must yield 0, got %d", got)
	}
}
