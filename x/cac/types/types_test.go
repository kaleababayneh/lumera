
package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	errs := []error{
		ErrInvalidParams, ErrEntryNotFound, ErrEntryExpired,
		ErrContentTooLarge, ErrInvalidContentHash, ErrInvalidRequestHash,
		ErrInvalidToolID, ErrDuplicateEntry, ErrInvalidTier,
		ErrTierCapacityExceeded, ErrInvalidationFailed, ErrUnauthorized,
		ErrRoyaltyFailed, ErrPromotionFailed, ErrInvalidTTL,
	}
	for _, e := range errs {
		assert.NotNil(t, e)
		assert.NotEmpty(t, e.Error())
	}
}

func TestSentinelErrorCodesUnique(t *testing.T) {
	t.Parallel()
	errs := []error{
		ErrInvalidParams, ErrEntryNotFound, ErrEntryExpired,
		ErrContentTooLarge, ErrInvalidContentHash, ErrInvalidRequestHash,
		ErrInvalidToolID, ErrDuplicateEntry, ErrInvalidTier,
		ErrTierCapacityExceeded, ErrInvalidationFailed, ErrUnauthorized,
		ErrRoyaltyFailed, ErrPromotionFailed, ErrInvalidTTL,
	}
	type coder interface{ ABCICode() uint32 }
	codes := make(map[uint32]string)
	for _, e := range errs {
		c, ok := e.(coder)
		if !ok {
			continue
		}
		code := c.ABCICode()
		if prev, dup := codes[code]; dup {
			t.Errorf("duplicate ABCI code %d: %q and %q", code, prev, e.Error())
		}
		codes[code] = e.Error()
	}
	require.NotEmpty(t, codes, "expected at least one coded error")
}

// ---------------------------------------------------------------------------
// Event types
// ---------------------------------------------------------------------------

func TestEventTypesNonEmpty(t *testing.T) {
	t.Parallel()
	events := []string{
		EventTypeCacheStore, EventTypeCacheHit, EventTypeCacheMiss,
		EventTypeCacheInvalidate, EventTypeCacheEvict,
		EventTypeTierPromotion, EventTypeRoyaltyDistributed,
		EventTypeDecayTick,
	}
	seen := make(map[string]struct{})
	for _, e := range events {
		assert.NotEmpty(t, e)
		if _, ok := seen[e]; ok {
			t.Errorf("duplicate event type: %s", e)
		}
		seen[e] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Attribute keys
// ---------------------------------------------------------------------------

func TestAttributeKeysNonEmpty(t *testing.T) {
	t.Parallel()
	attrs := []string{
		AttributeKeyContentHash, AttributeKeyRequestHash, AttributeKeyToolID,
		AttributeKeyPublisher, AttributeKeyTier, AttributeKeySize,
		AttributeKeyTTL, AttributeKeyOriginToolID, AttributeKeyServingToolID,
		AttributeKeyOriginRoyalty, AttributeKeyServingRoyalty,
		AttributeKeyGovernanceRoyalty,
		AttributeKeyCostSaved, AttributeKeyLatencyMs,
		AttributeKeyInvalidationTarget, AttributeKeyEntriesInvalidated,
		AttributeKeyBytesFreed,
	}
	seen := make(map[string]struct{})
	for _, a := range attrs {
		assert.NotEmpty(t, a)
		if _, ok := seen[a]; ok {
			t.Errorf("duplicate attribute key: %s", a)
		}
		seen[a] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Default parameter constants
// ---------------------------------------------------------------------------

func TestDefaultParameterConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, uint64(7*24*60*60), DefaultTTLSeconds)
	assert.Equal(t, uint64(1*1024*1024), DefaultMaxEntrySizeBytes)
	assert.Equal(t, uint64(16*1024*1024*1024), DefaultL1CapacityBytes)
	assert.Equal(t, uint64(1*1024*1024*1024*1024), DefaultL2CapacityBytes)
	assert.Equal(t, uint64(100*1024*1024*1024*1024), DefaultL3CapacityBytes)
	assert.Equal(t, uint32(5000), DefaultRoyaltyOriginBPS)
	assert.Equal(t, uint32(2000), DefaultRoyaltyStorageBPS)
	assert.Equal(t, uint32(1500), DefaultRoyaltyBandwidthBPS)
	assert.Equal(t, uint32(1000), DefaultRoyaltyVerificationBPS)
	assert.Equal(t, uint32(500), DefaultRoyaltyGovernanceBPS)
	assert.Equal(t, uint32(9500), DefaultRoyaltyDecayBPS)
	assert.Equal(t, uint64(14400), DefaultBlocksPerDay)
	assert.Equal(t, uint64(10), DefaultMinAccessForPromotion)
	assert.True(t, DefaultEnableRoyalties)
}

// ---------------------------------------------------------------------------
// Codec smoke tests
// ---------------------------------------------------------------------------

func TestRegisterInterfaces_NoPanic(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterInterfaces panicked: %v", r)
		}
	}()
	RegisterInterfaces(ModuleCdc.InterfaceRegistry())
}

func TestModuleCdc_NotNil(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, ModuleCdc)
	assert.NotNil(t, Amino)
}
