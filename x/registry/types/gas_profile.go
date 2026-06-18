
package types

import "fmt"

// Gas profile defaults used when a ToolCard omits explicit tuning.
const (
	DefaultLockGas          uint64 = 20_000
	DefaultSettleGas        uint64 = 35_000
	DefaultInvocationGas    uint64 = 15_000
	DefaultCacheDiscountBPS uint64 = 0

	// MaxGasDiscountBPS caps cache discounts at 100%.
	MaxGasDiscountBPS uint64 = 10_000
)

// GasCostProfile represents the sanitized gas settings for a tool.
type GasCostProfile struct {
	LockGas          uint64
	SettleGas        uint64
	InvocationGas    uint64
	CacheDiscountBPS uint64
}

// DefaultGasCostProfile returns the baseline profile when no overrides are set.
func DefaultGasCostProfile() GasCostProfile {
	return GasCostProfile{
		LockGas:          DefaultLockGas,
		SettleGas:        DefaultSettleGas,
		InvocationGas:    DefaultInvocationGas,
		CacheDiscountBPS: DefaultCacheDiscountBPS,
	}
}

// NormalizeGasProfile converts a (possibly nil) proto GasProfile into a usable GasCostProfile.
func NormalizeGasProfile(profile *GasProfile) GasCostProfile {
	defaults := DefaultGasCostProfile()
	if profile == nil {
		return defaults
	}

	if val := profile.GetLockGas(); val > 0 {
		defaults.LockGas = val
	}
	if val := profile.GetSettleGas(); val > 0 {
		defaults.SettleGas = val
	}
	if val := profile.GetInvocationGas(); val > 0 {
		defaults.InvocationGas = val
	}
	if val := profile.GetCacheHitDiscountBps(); val > 0 {
		defaults.CacheDiscountBPS = val
	}

	return defaults
}

// CacheAdjusted returns the gas after applying the cache-hit discount.
func (p GasCostProfile) CacheAdjusted(base uint64, cacheHit bool) uint64 {
	if !cacheHit || p.CacheDiscountBPS == 0 {
		return base
	}

	discount := (base * p.CacheDiscountBPS) / 10_000
	if discount >= base {
		return 0
	}
	return base - discount
}

// NormalizeToolGasProfile copies the sanitized gas profile back onto the card,
// applying defaults and validating discount bounds.
func NormalizeToolGasProfile(tool *ToolCard) error {
	if tool == nil {
		return fmt.Errorf("tool card cannot be nil")
	}

	normalized := NormalizeGasProfile(tool.GasProfile)
	if normalized.CacheDiscountBPS > MaxGasDiscountBPS {
		return fmt.Errorf("cache discount bps cannot exceed %d", MaxGasDiscountBPS)
	}

	tool.GasProfile = &GasProfile{
		LockGas:             normalized.LockGas,
		SettleGas:           normalized.SettleGas,
		InvocationGas:       normalized.InvocationGas,
		CacheHitDiscountBps: normalized.CacheDiscountBPS,
	}

	return nil
}
