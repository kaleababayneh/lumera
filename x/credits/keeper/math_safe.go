
package keeper

import (
	"sort"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	types "github.com/LumeraProtocol/lumera/x/credits/types"
)

const splitTotalBPS = uint32(10000)

// Principal names every revenue recipient class in the credits split table.
type Principal string

const (
	PrincipalPublisher      Principal = "publisher"
	PrincipalRouter         Principal = "router"
	PrincipalOriginSurface  Principal = "origin_surface"
	PrincipalTreasury       Principal = "treasury"
	PrincipalReferrer       Principal = "referrer"
	PrincipalWorkflowAuthor Principal = "workflow_author"
)

// SplitRoute carries a complete basis-point split table for one settlement route.
type SplitRoute struct {
	PublisherBPS      uint32
	RouterBPS         uint32
	OriginSurfaceBPS  uint32
	TreasuryBPS       uint32
	ReferrerBPS       uint32
	WorkflowAuthorBPS uint32
}

type principalBPS struct {
	principal Principal
	bps       uint32
}

// SafeMulDiv computes (amount * rate) / scale using sdkmath.Int with basic validation.
// It returns an error when the inputs would produce undefined behaviour (e.g. negative
// rates or non-positive scale values).
func SafeMulDiv(amount sdkmath.Int, rate int64, scale int64) (result sdkmath.Int, err error) {
	if rate < 0 {
		return sdkmath.Int{}, types.ErrInvalidParams.Wrap("rate must be non-negative")
	}
	if scale <= 0 {
		return sdkmath.Int{}, types.ErrInvalidParams.Wrap("scale must be positive")
	}
	if rate > scale {
		return sdkmath.Int{}, types.ErrInvalidParams.Wrap("rate cannot exceed scale")
	}

	defer func() {
		if r := recover(); r != nil {
			result = sdkmath.Int{}
			err = types.ErrInvalidParams.Wrapf("percentage multiplication overflow: %v", r)
		}
	}()

	product := amount.MulRaw(rate)
	return product.QuoRaw(scale), nil
}

// SafePercentage converts a value expressed in basis points (0-10000) into an
// sdkmath.Int derived from the supplied amount.
func SafePercentage(amount sdkmath.Int, basisPoints uint32) (sdkmath.Int, error) {
	if basisPoints > splitTotalBPS {
		return sdkmath.Int{}, types.ErrInvalidParams.Wrapf(
			"basis points %d exceeds maximum %d", basisPoints, splitTotalBPS,
		)
	}
	return SafeMulDiv(amount, int64(basisPoints), int64(splitTotalBPS))
}

// SafeSubtract subtracts subtrahend from minuend, returning an error when the
// operation would underflow (i.e. produce a negative value).
func SafeSubtract(minuend, subtrahend sdkmath.Int) (sdkmath.Int, error) {
	if minuend.LT(subtrahend) {
		return sdkmath.Int{}, types.ErrInsufficientFunds.Wrapf(
			"subtraction would result in negative value: %s - %s",
			minuend.String(), subtrahend.String(),
		)
	}
	return minuend.Sub(subtrahend), nil
}

// SafeAddCoins adds two sdk.Coins collections and converts any overflow panic into
// a module error so callers can handle it gracefully.
func SafeAddCoins(a, b sdk.Coins) (result sdk.Coins, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = sdk.Coins{}
			err = types.ErrInvalidParams.Wrapf("coin addition overflow: %v", r)
		}
	}()
	result = a.Add(b...)
	return result, nil
}

// CalculateBurnAmount splits a total into burn + remaining portions using the
// supplied burnRateBPS (basis points). It returns the computed burn amount and the
// remaining amount after burn.
func CalculateBurnAmount(total sdkmath.Int, burnRateBPS uint32) (burn sdkmath.Int, remaining sdkmath.Int, err error) {
	burn, err = SafePercentage(total, burnRateBPS)
	if err != nil {
		return sdkmath.Int{}, sdkmath.Int{}, err
	}

	remaining, err = SafeSubtract(total, burn)
	if err != nil {
		return sdkmath.Int{}, sdkmath.Int{}, err
	}

	return burn, remaining, nil
}

// CalculateSplit divides an amount into publisher/router/origin-surface/treasury/referrer shares
// using basis-point splits. The shares must sum to 10000 basis points.
func CalculateSplit(
	amount sdkmath.Int,
	publisherBPS, routerBPS, originSurfaceBPS, treasuryBPS, referrerBPS uint32,
) (publisher, router, originSurface, treasury, referrer sdkmath.Int, err error) {
	parts, err := computePrincipalSplits(amount, []principalBPS{
		{PrincipalPublisher, publisherBPS},
		{PrincipalRouter, routerBPS},
		{PrincipalOriginSurface, originSurfaceBPS},
		{PrincipalTreasury, treasuryBPS},
		{PrincipalReferrer, referrerBPS},
	})
	if err != nil {
		return sdkmath.Int{}, sdkmath.Int{}, sdkmath.Int{}, sdkmath.Int{}, sdkmath.Int{}, err
	}

	return parts[PrincipalPublisher],
		parts[PrincipalRouter],
		parts[PrincipalOriginSurface],
		parts[PrincipalTreasury],
		parts[PrincipalReferrer],
		nil
}

// ComputeSplits divides amount across a complete route split table, including
// the workflow_author principal used by Agent Contracts.
func ComputeSplits(amount sdkmath.Int, route SplitRoute) (map[Principal]sdkmath.Int, error) {
	return computePrincipalSplits(amount, []principalBPS{
		{PrincipalPublisher, route.PublisherBPS},
		{PrincipalRouter, route.RouterBPS},
		{PrincipalOriginSurface, route.OriginSurfaceBPS},
		{PrincipalTreasury, route.TreasuryBPS},
		{PrincipalReferrer, route.ReferrerBPS},
		{PrincipalWorkflowAuthor, route.WorkflowAuthorBPS},
	})
}

func computePrincipalSplits(amount sdkmath.Int, entries []principalBPS) (map[Principal]sdkmath.Int, error) {
	totalBPS := uint64(0)
	for _, entry := range entries {
		if entry.bps > splitTotalBPS {
			return nil, types.ErrInvalidParams.Wrapf(
				"%s bps %d exceeds maximum %d", entry.principal, entry.bps, splitTotalBPS,
			)
		}
		totalBPS += uint64(entry.bps)
	}
	if totalBPS != uint64(splitTotalBPS) {
		return nil, types.ErrInvalidParams.Wrapf(
			"splits must sum to %d bps, got %d", splitTotalBPS, totalBPS,
		)
	}

	parts := make(map[Principal]sdkmath.Int, len(entries))
	total := sdkmath.ZeroInt()
	for _, entry := range entries {
		part, err := SafePercentage(amount, entry.bps)
		if err != nil {
			return nil, err
		}
		parts[entry.principal] = part
		total = total.Add(part)
	}

	if total.Equal(amount) {
		return parts, nil
	}

	diff := amount.Sub(total)
	adjustments := make([]principalBPS, len(entries))
	copy(adjustments, entries)
	sort.SliceStable(adjustments, func(i, j int) bool {
		return adjustments[i].bps > adjustments[j].bps
	})

	for _, candidate := range adjustments {
		newValue := parts[candidate.principal].Add(diff)
		if newValue.IsNegative() {
			continue
		}
		parts[candidate.principal] = newValue
		return parts, nil
	}

	return nil, types.ErrInvalidParams.Wrapf("cannot apply rounding adjustment %s safely", diff.String())
}

// ValidateRates validates configured burn/insurance rates and revenue splits.
func ValidateRates(
	burnRate, insuranceRate uint32,
	publisherShare, routerShare, originSurfaceShare, treasuryShare, referrerShare uint32,
) error {
	// Check individual rate bounds
	if burnRate > splitTotalBPS {
		return types.ErrInvalidParams.Wrapf("burn rate %d exceeds %d bps", burnRate, splitTotalBPS)
	}
	if insuranceRate > splitTotalBPS {
		return types.ErrInvalidParams.Wrapf("insurance rate %d exceeds %d bps", insuranceRate, splitTotalBPS)
	}

	// Check combined burn and insurance don't exceed 100%
	// This prevents negative net amounts after deductions
	combinedDeductions := burnRate + insuranceRate
	if combinedDeductions > splitTotalBPS {
		return types.ErrInvalidParams.Wrapf(
			"combined burn and insurance rates exceed 100%%: %d + %d = %d bps (max %d)",
			burnRate, insuranceRate, combinedDeductions, splitTotalBPS,
		)
	}

	// Check that revenue shares sum to 100%
	totalShares := publisherShare + routerShare + originSurfaceShare + treasuryShare + referrerShare
	if totalShares != splitTotalBPS {
		return types.ErrInvalidParams.Wrapf(
			"shares must sum to %d bps, got %d (pub=%d, router=%d, origin_surface=%d, treasury=%d, ref=%d)",
			splitTotalBPS, totalShares, publisherShare, routerShare, originSurfaceShare, treasuryShare, referrerShare,
		)
	}

	return nil
}

// SafeIncrementCounter increments a uint64 counter, ensuring overflow is reported
// as a module error instead of wrapping.
func SafeIncrementCounter(counter uint64) (uint64, error) {
	if counter == ^uint64(0) {
		return 0, types.ErrInvalidParams.Wrap("counter overflow")
	}
	return counter + 1, nil
}
