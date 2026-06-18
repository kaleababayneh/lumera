
package types

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// GenesisState defines the CAC module's genesis state.
type GenesisState struct {
	// Params contains the module parameters.
	Params *CacheParams `json:"params"`
	// Entries contains the initial cache entries.
	Entries []*CacheEntry `json:"entries,omitempty"`
	// ToolStats contains per-tool hit/miss counters exported for explorer continuity.
	ToolStats []*ToolCacheStats `json:"tool_stats,omitempty"`
	// EntryHeights preserves content_hash -> creation height for decay math.
	EntryHeights map[string]int64 `json:"entry_heights,omitempty"`
	// LastDecayTick records the last successful upkeep height.
	LastDecayTick int64 `json:"last_decay_tick,omitempty"`
}

// NewGenesisState creates a new genesis state with default values.
func NewGenesisState(params *CacheParams) *GenesisState {
	return &GenesisState{
		Params:       params,
		Entries:      []*CacheEntry{},
		ToolStats:    []*ToolCacheStats{},
		EntryHeights: map[string]int64{},
	}
}

// DefaultGenesisState returns the default genesis state.
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(DefaultCacheParams())
}

// DefaultCacheParams returns the default CAC module parameters.
func DefaultCacheParams() *CacheParams {
	return &CacheParams{
		DefaultTtlSeconds:      DefaultTTLSeconds,
		MaxEntrySizeBytes:      DefaultMaxEntrySizeBytes,
		L1CapacityBytes:        DefaultL1CapacityBytes,
		L2CapacityBytes:        DefaultL2CapacityBytes,
		L3CapacityBytes:        DefaultL3CapacityBytes,
		L4CapacityBytes:        DefaultL4CapacityBytes,
		RoyaltyOriginBps:       DefaultRoyaltyOriginBPS,
		RoyaltyStorageBps:      DefaultRoyaltyStorageBPS,
		RoyaltyBandwidthBps:    DefaultRoyaltyBandwidthBPS,
		RoyaltyVerificationBps: DefaultRoyaltyVerificationBPS,
		RoyaltyGovernanceBps:   DefaultRoyaltyGovernanceBPS,
		RoyaltyDecayBps:        DefaultRoyaltyDecayBPS,
		BlocksPerDay:           DefaultBlocksPerDay,
		MinAccessForPromotion:  DefaultMinAccessForPromotion,
		EnableRoyalties:        DefaultEnableRoyalties,
	}
}

// Validate validates the genesis state.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return nil
	}
	if gs.Params != nil {
		if err := gs.Params.Validate(); err != nil {
			return err
		}
	}
	if err := validateGenesisEntries(gs); err != nil {
		return err
	}
	if err := validateGenesisToolStats(gs.ToolStats); err != nil {
		return err
	}
	if gs.LastDecayTick < 0 {
		return fmt.Errorf("last_decay_tick cannot be negative")
	}
	return nil
}

func validateGenesisEntries(gs *GenesisState) error {
	seenEntries := make(map[string]int, len(gs.Entries))
	for i, entry := range gs.Entries {
		if entry == nil {
			return fmt.Errorf("entries[%d] cannot be nil", i)
		}
		rawContentHash := entry.GetContentHash()
		contentHash := strings.TrimSpace(rawContentHash)
		if contentHash == "" {
			return fmt.Errorf("entries[%d].content_hash is required", i)
		}
		if contentHash != rawContentHash {
			return fmt.Errorf("entries[%d].content_hash cannot contain leading or trailing whitespace", i)
		}
		if len(contentHash) > MaxCacheIDLen {
			return fmt.Errorf("entries[%d].content_hash length %d exceeds maximum %d", i, len(contentHash), MaxCacheIDLen)
		}
		if _, ok := seenEntries[contentHash]; ok {
			return fmt.Errorf("duplicate entries content_hash: %s", contentHash)
		}
		seenEntries[contentHash] = i

		rawToolID := entry.GetToolId()
		toolID := strings.TrimSpace(rawToolID)
		if toolID == "" {
			return fmt.Errorf("entries[%d].tool_id is required", i)
		}
		if toolID != rawToolID {
			return fmt.Errorf("entries[%d].tool_id cannot contain leading or trailing whitespace", i)
		}
		if len(toolID) > MaxCacheIDLen {
			return fmt.Errorf("entries[%d].tool_id length %d exceeds maximum %d", i, len(toolID), MaxCacheIDLen)
		}

		rawRequestHash := entry.GetRequestHash()
		requestHash := strings.TrimSpace(rawRequestHash)
		if requestHash == "" {
			return fmt.Errorf("entries[%d].request_hash is required", i)
		}
		if requestHash != rawRequestHash {
			return fmt.Errorf("entries[%d].request_hash cannot contain leading or trailing whitespace", i)
		}
		if len(requestHash) > MaxCacheIDLen {
			return fmt.Errorf("entries[%d].request_hash length %d exceeds maximum %d", i, len(requestHash), MaxCacheIDLen)
		}

		if len(entry.GetContent()) == 0 {
			return fmt.Errorf("entries[%d].content cannot be empty", i)
		}
		if entry.GetContentSizeBytes() != uint64(len(entry.GetContent())) {
			return fmt.Errorf(
				"entries[%d].content_size_bytes %d does not match content length %d",
				i,
				entry.GetContentSizeBytes(),
				len(entry.GetContent()),
			)
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("entries[%d].created_at", i), entry.GetCreatedAt()); err != nil {
			return err
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("entries[%d].expires_at", i), entry.GetExpiresAt()); err != nil {
			return err
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("entries[%d].last_access_at", i), entry.GetLastAccessAt()); err != nil {
			return err
		}
		if err := validateGenesisEntryTimestampOrder(i, entry); err != nil {
			return err
		}

		tier := entry.GetTier()
		if tier == CacheTier_CACHE_TIER_UNSPECIFIED {
			return fmt.Errorf("entries[%d].tier is required", i)
		}
		if _, ok := CacheTier_name[int32(tier)]; !ok {
			return fmt.Errorf("entries[%d].tier is invalid: %d", i, tier)
		}
	}

	for contentHash, index := range seenEntries {
		if _, ok := gs.EntryHeights[contentHash]; !ok {
			return fmt.Errorf("entries[%d].content_hash %s missing entry_heights entry", index, contentHash)
		}
	}

	for contentHash := range gs.EntryHeights {
		trimmedContentHash := strings.TrimSpace(contentHash)
		if trimmedContentHash == "" {
			return fmt.Errorf("entry_heights contains blank content_hash")
		}
		if trimmedContentHash != contentHash {
			return fmt.Errorf("entry_heights content_hash cannot contain leading or trailing whitespace: %s", contentHash)
		}
		if _, ok := seenEntries[contentHash]; !ok {
			return fmt.Errorf("entry_heights references unknown content_hash: %s", contentHash)
		}
		if gs.EntryHeights[contentHash] < 0 {
			return fmt.Errorf("entry_heights[%s] cannot be negative", contentHash)
		}
	}
	return nil
}

func validateGenesisEntryTimestampOrder(index int, entry *CacheEntry) error {
	if entry.GetCreatedAt() != nil && entry.GetExpiresAt() != nil && !entry.GetExpiresAt().AsTime().After(entry.GetCreatedAt().AsTime()) {
		return fmt.Errorf("entries[%d].expires_at must be after created_at", index)
	}
	if entry.GetCreatedAt() != nil && entry.GetLastAccessAt() != nil && entry.GetLastAccessAt().AsTime().Before(entry.GetCreatedAt().AsTime()) {
		return fmt.Errorf("entries[%d].last_access_at cannot be before created_at", index)
	}
	if entry.GetExpiresAt() != nil && entry.GetLastAccessAt() != nil && entry.GetLastAccessAt().AsTime().After(entry.GetExpiresAt().AsTime()) {
		return fmt.Errorf("entries[%d].last_access_at cannot be after expires_at", index)
	}
	return nil
}

func validateGenesisTimestamp(field string, ts *timestamppb.Timestamp) error {
	if ts == nil {
		return nil
	}
	if err := ts.CheckValid(); err != nil {
		return fmt.Errorf("%s is invalid: %w", field, err)
	}
	return nil
}

func validateGenesisToolStats(stats []*ToolCacheStats) error {
	seenStats := make(map[string]struct{}, len(stats))
	for i, stat := range stats {
		if stat == nil {
			return fmt.Errorf("tool_stats[%d] cannot be nil", i)
		}
		rawToolID := stat.GetToolId()
		toolID := strings.TrimSpace(rawToolID)
		if toolID == "" {
			return fmt.Errorf("tool_stats[%d].tool_id is required", i)
		}
		if toolID != rawToolID {
			return fmt.Errorf("tool_stats[%d].tool_id cannot contain leading or trailing whitespace", i)
		}
		if len(toolID) > MaxCacheIDLen {
			return fmt.Errorf("tool_stats[%d].tool_id length %d exceeds maximum %d", i, len(toolID), MaxCacheIDLen)
		}
		if _, ok := seenStats[toolID]; ok {
			return fmt.Errorf("duplicate tool_stats tool_id: %s", toolID)
		}
		seenStats[toolID] = struct{}{}

		for originToolID := range stat.GetOriginHits() {
			trimmedOriginToolID := strings.TrimSpace(originToolID)
			if trimmedOriginToolID == "" {
				return fmt.Errorf("tool_stats[%d].origin_hits contains blank tool_id", i)
			}
			if trimmedOriginToolID != originToolID {
				return fmt.Errorf("tool_stats[%d].origin_hits tool_id cannot contain leading or trailing whitespace", i)
			}
			if len(originToolID) > MaxCacheIDLen {
				return fmt.Errorf("tool_stats[%d].origin_hits tool_id length %d exceeds maximum %d", i, len(originToolID), MaxCacheIDLen)
			}
		}
	}
	return nil
}

// Validate validates the cache parameters.
func (p *CacheParams) Validate() error {
	if p.DefaultTtlSeconds == 0 {
		return ErrInvalidParams.Wrap("default_ttl_seconds must be positive")
	}
	if p.DefaultTtlSeconds > MaxTTLSeconds {
		return ErrInvalidParams.Wrapf("default_ttl_seconds exceeds maximum safe duration seconds (%d)", MaxTTLSeconds)
	}
	if p.MaxEntrySizeBytes == 0 {
		return ErrInvalidParams.Wrap("max_entry_size_bytes must be positive")
	}
	if p.L1CapacityBytes == 0 {
		return ErrInvalidParams.Wrap("l1_capacity_bytes must be positive")
	}
	if p.L2CapacityBytes == 0 {
		return ErrInvalidParams.Wrap("l2_capacity_bytes must be positive")
	}
	if p.L3CapacityBytes == 0 {
		return ErrInvalidParams.Wrap("l3_capacity_bytes must be positive")
	}
	if p.L4CapacityBytes == 0 {
		return ErrInvalidParams.Wrap("l4_capacity_bytes must be positive")
	}
	royaltyShares := []struct {
		name string
		bps  uint32
	}{
		{name: "royalty_origin_bps", bps: p.RoyaltyOriginBps},
		{name: "royalty_storage_bps", bps: p.RoyaltyStorageBps},
		{name: "royalty_bandwidth_bps", bps: p.RoyaltyBandwidthBps},
		{name: "royalty_verification_bps", bps: p.RoyaltyVerificationBps},
		{name: "royalty_governance_bps", bps: p.RoyaltyGovernanceBps},
	}
	var totalRoyaltyBps uint64
	for _, share := range royaltyShares {
		if share.bps > 10000 {
			return ErrInvalidParams.Wrapf("%s cannot exceed 10000", share.name)
		}
		totalRoyaltyBps += uint64(share.bps)
	}
	if totalRoyaltyBps != 10000 {
		return ErrInvalidParams.Wrapf("royalty share bps must sum to 10000, got %d", totalRoyaltyBps)
	}
	if p.RoyaltyDecayBps == 0 || p.RoyaltyDecayBps > 10000 {
		return ErrInvalidParams.Wrap("royalty_decay_bps must be between 1 and 10000")
	}
	if p.BlocksPerDay == 0 {
		return ErrInvalidParams.Wrap("blocks_per_day must be positive")
	}
	if p.MinAccessForPromotion == 0 {
		return ErrInvalidParams.Wrap("min_access_for_promotion must be positive")
	}
	return nil
}
