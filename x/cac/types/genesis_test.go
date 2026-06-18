
package types

import (
	"math"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ---------- GenesisState ----------

func TestDefaultGenesisState(t *testing.T) {
	gs := DefaultGenesisState()
	require.NotNil(t, gs)
	require.NotNil(t, gs.Params)
	assert.Equal(t, DefaultTTLSeconds, gs.Params.DefaultTtlSeconds)
	assert.Equal(t, DefaultMaxEntrySizeBytes, gs.Params.MaxEntrySizeBytes)
	assert.NotNil(t, gs.Entries)
	assert.Len(t, gs.Entries, 0)
	assert.NotNil(t, gs.ToolStats)
	assert.Len(t, gs.ToolStats, 0)
	assert.NotNil(t, gs.EntryHeights)
	assert.Empty(t, gs.EntryHeights)
}

func TestNewGenesisState(t *testing.T) {
	params := DefaultCacheParams()
	gs := NewGenesisState(params)
	require.NotNil(t, gs)
	assert.Equal(t, params, gs.Params)
	assert.NotNil(t, gs.Entries)
	assert.Len(t, gs.Entries, 0)
	assert.NotNil(t, gs.ToolStats)
	assert.NotNil(t, gs.EntryHeights)
}

func TestGenesisState_Validate_Default(t *testing.T) {
	gs := DefaultGenesisState()
	require.NoError(t, gs.Validate())
}

func TestGenesisState_Validate_NilParams(t *testing.T) {
	gs := &GenesisState{Params: nil}
	require.NoError(t, gs.Validate())
}

func TestGenesisState_Validate_InvalidParams(t *testing.T) {
	gs := &GenesisState{
		Params: &CacheParams{
			DefaultTtlSeconds: 0, // invalid: zero
			MaxEntrySizeBytes: DefaultMaxEntrySizeBytes,
		},
	}
	require.Error(t, gs.Validate())
}

func TestGenesisState_Validate_EntriesAndToolStats(t *testing.T) {
	validEntry := func(contentHash string) *CacheEntry {
		return &CacheEntry{
			ContentHash:      contentHash,
			ToolId:           "tool-alpha",
			RequestHash:      "request-alpha",
			Content:          []byte("payload"),
			ContentSizeBytes: 7,
			Tier:             CacheTier_CACHE_TIER_L2_SSD,
		}
	}

	tests := []struct {
		name    string
		mutate  func(*GenesisState)
		wantErr string
	}{
		{
			name: "valid entry and stats",
			mutate: func(gs *GenesisState) {
				gs.Entries = []*CacheEntry{validEntry("blake3:alpha")}
				gs.EntryHeights = map[string]int64{"blake3:alpha": 12}
				gs.ToolStats = []*ToolCacheStats{{
					ToolId:     "tool-alpha",
					HitCount:   1,
					MissCount:  2,
					OriginHits: map[string]uint64{"origin-tool": 1},
				}}
			},
		},
		{
			name: "valid ordered entry timestamps",
			mutate: func(gs *GenesisState) {
				now := time.Unix(1_700_000_000, 0).UTC()
				entry := validEntry("blake3:alpha")
				entry.CreatedAt = timestamppb.New(now)
				entry.ExpiresAt = timestamppb.New(now.Add(time.Hour))
				entry.LastAccessAt = timestamppb.New(now.Add(time.Minute))
				gs.Entries = []*CacheEntry{entry}
				gs.EntryHeights = map[string]int64{"blake3:alpha": 12}
			},
		},
		{
			name: "nil entry",
			mutate: func(gs *GenesisState) {
				gs.Entries = []*CacheEntry{nil}
			},
			wantErr: "entries[0] cannot be nil",
		},
		{
			name: "missing content hash",
			mutate: func(gs *GenesisState) {
				entry := validEntry(" ")
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "content_hash is required",
		},
		{
			name: "duplicate content hash",
			mutate: func(gs *GenesisState) {
				gs.Entries = []*CacheEntry{
					validEntry("blake3:duplicate"),
					validEntry("blake3:duplicate"),
				}
			},
			wantErr: "duplicate entries content_hash",
		},
		{
			name: "missing tool id",
			mutate: func(gs *GenesisState) {
				entry := validEntry("blake3:alpha")
				entry.ToolId = ""
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "tool_id is required",
		},
		{
			name: "missing request hash",
			mutate: func(gs *GenesisState) {
				entry := validEntry("blake3:alpha")
				entry.RequestHash = ""
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "request_hash is required",
		},
		{
			name: "empty content",
			mutate: func(gs *GenesisState) {
				entry := validEntry("blake3:alpha")
				entry.Content = nil
				entry.ContentSizeBytes = 0
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "content cannot be empty",
		},
		{
			name: "content size mismatch",
			mutate: func(gs *GenesisState) {
				entry := validEntry("blake3:alpha")
				entry.ContentSizeBytes = 99
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "does not match content length",
		},
		{
			name: "invalid created timestamp",
			mutate: func(gs *GenesisState) {
				entry := validEntry("blake3:alpha")
				entry.CreatedAt = &timestamppb.Timestamp{Seconds: 253402300800}
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "entries[0].created_at is invalid",
		},
		{
			name: "invalid expiry timestamp",
			mutate: func(gs *GenesisState) {
				entry := validEntry("blake3:alpha")
				entry.ExpiresAt = &timestamppb.Timestamp{Nanos: 1_000_000_000}
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "entries[0].expires_at is invalid",
		},
		{
			name: "invalid last access timestamp",
			mutate: func(gs *GenesisState) {
				entry := validEntry("blake3:alpha")
				entry.LastAccessAt = &timestamppb.Timestamp{Seconds: -62135596801}
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "entries[0].last_access_at is invalid",
		},
		{
			name: "expiry equal to created at",
			mutate: func(gs *GenesisState) {
				now := time.Unix(1_700_000_000, 0).UTC()
				entry := validEntry("blake3:alpha")
				entry.CreatedAt = timestamppb.New(now)
				entry.ExpiresAt = timestamppb.New(now)
				entry.LastAccessAt = timestamppb.New(now)
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "expires_at must be after created_at",
		},
		{
			name: "expiry before created at",
			mutate: func(gs *GenesisState) {
				now := time.Unix(1_700_000_000, 0).UTC()
				entry := validEntry("blake3:alpha")
				entry.CreatedAt = timestamppb.New(now)
				entry.ExpiresAt = timestamppb.New(now.Add(-time.Second))
				entry.LastAccessAt = timestamppb.New(now)
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "expires_at must be after created_at",
		},
		{
			name: "last access before created at",
			mutate: func(gs *GenesisState) {
				now := time.Unix(1_700_000_000, 0).UTC()
				entry := validEntry("blake3:alpha")
				entry.CreatedAt = timestamppb.New(now)
				entry.ExpiresAt = timestamppb.New(now.Add(time.Hour))
				entry.LastAccessAt = timestamppb.New(now.Add(-time.Second))
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "last_access_at cannot be before created_at",
		},
		{
			name: "last access after expiry",
			mutate: func(gs *GenesisState) {
				now := time.Unix(1_700_000_000, 0).UTC()
				entry := validEntry("blake3:alpha")
				entry.CreatedAt = timestamppb.New(now)
				entry.ExpiresAt = timestamppb.New(now.Add(time.Hour))
				entry.LastAccessAt = timestamppb.New(now.Add(2 * time.Hour))
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "last_access_at cannot be after expires_at",
		},
		{
			name: "unspecified tier",
			mutate: func(gs *GenesisState) {
				entry := validEntry("blake3:alpha")
				entry.Tier = CacheTier_CACHE_TIER_UNSPECIFIED
				gs.Entries = []*CacheEntry{entry}
			},
			wantErr: "tier is required",
		},
		{
			name: "entry missing height",
			mutate: func(gs *GenesisState) {
				gs.Entries = []*CacheEntry{validEntry("blake3:alpha")}
			},
			wantErr: "entries[0].content_hash blake3:alpha missing entry_heights entry",
		},
		{
			name: "entry height references unknown entry",
			mutate: func(gs *GenesisState) {
				gs.EntryHeights = map[string]int64{"blake3:missing": 7}
			},
			wantErr: "entry_heights references unknown content_hash",
		},
		{
			name: "negative entry height",
			mutate: func(gs *GenesisState) {
				gs.Entries = []*CacheEntry{validEntry("blake3:alpha")}
				gs.EntryHeights = map[string]int64{"blake3:alpha": -1}
			},
			wantErr: "entry_heights[blake3:alpha] cannot be negative",
		},
		{
			name: "negative last decay tick",
			mutate: func(gs *GenesisState) {
				gs.LastDecayTick = -1
			},
			wantErr: "last_decay_tick cannot be negative",
		},
		{
			name: "nil tool stats",
			mutate: func(gs *GenesisState) {
				gs.ToolStats = []*ToolCacheStats{nil}
			},
			wantErr: "tool_stats[0] cannot be nil",
		},
		{
			name: "duplicate tool stats",
			mutate: func(gs *GenesisState) {
				gs.ToolStats = []*ToolCacheStats{
					{ToolId: "tool-alpha"},
					{ToolId: "tool-alpha"},
				}
			},
			wantErr: "duplicate tool_stats tool_id",
		},
		{
			name: "blank origin hit tool id",
			mutate: func(gs *GenesisState) {
				gs.ToolStats = []*ToolCacheStats{{
					ToolId:     "tool-alpha",
					OriginHits: map[string]uint64{" ": 1},
				}}
			},
			wantErr: "origin_hits contains blank tool_id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := DefaultGenesisState()
			tc.mutate(gs)
			err := gs.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// ---------- CacheParams ----------

func TestDefaultCacheParams(t *testing.T) {
	p := DefaultCacheParams()
	require.NotNil(t, p)
	assert.Equal(t, DefaultTTLSeconds, p.DefaultTtlSeconds)
	assert.Equal(t, DefaultMaxEntrySizeBytes, p.MaxEntrySizeBytes)
	assert.Equal(t, DefaultL1CapacityBytes, p.L1CapacityBytes)
	assert.Equal(t, DefaultL2CapacityBytes, p.L2CapacityBytes)
	assert.Equal(t, DefaultL3CapacityBytes, p.L3CapacityBytes)
	assert.Equal(t, DefaultRoyaltyOriginBPS, p.RoyaltyOriginBps)
	assert.Equal(t, DefaultRoyaltyStorageBPS, p.RoyaltyStorageBps)
	assert.Equal(t, DefaultRoyaltyBandwidthBPS, p.RoyaltyBandwidthBps)
	assert.Equal(t, DefaultRoyaltyVerificationBPS, p.RoyaltyVerificationBps)
	assert.Equal(t, DefaultRoyaltyGovernanceBPS, p.RoyaltyGovernanceBps)
	assert.Equal(t, DefaultRoyaltyDecayBPS, p.RoyaltyDecayBps)
	assert.Equal(t, DefaultBlocksPerDay, p.BlocksPerDay)
	assert.Equal(t, DefaultMinAccessForPromotion, p.MinAccessForPromotion)
	assert.Equal(t, DefaultEnableRoyalties, p.EnableRoyalties)
}

func TestCacheParams_Validate_Valid(t *testing.T) {
	p := DefaultCacheParams()
	require.NoError(t, p.Validate())
}

func TestCacheParams_Validate_ZeroTTL(t *testing.T) {
	p := DefaultCacheParams()
	p.DefaultTtlSeconds = 0
	err := p.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default_ttl_seconds")
}

func TestCacheParams_Validate_DefaultTTLDurationBoundary(t *testing.T) {
	p := DefaultCacheParams()
	p.DefaultTtlSeconds = MaxTTLSeconds
	require.NoError(t, p.Validate())

	p.DefaultTtlSeconds = MaxTTLSeconds + 1
	err := p.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default_ttl_seconds exceeds maximum safe duration seconds")
}

func TestCacheParams_Validate_ZeroMaxEntrySize(t *testing.T) {
	p := DefaultCacheParams()
	p.MaxEntrySizeBytes = 0
	err := p.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_entry_size_bytes")
}

func TestCacheParams_Validate_ZeroTierCapacities(t *testing.T) {
	cases := []struct {
		name          string
		mutate        func(*CacheParams)
		wantSubstring string
	}{
		{
			name:          "zero L1 capacity",
			mutate:        func(p *CacheParams) { p.L1CapacityBytes = 0 },
			wantSubstring: "l1_capacity_bytes",
		},
		{
			name:          "zero L2 capacity",
			mutate:        func(p *CacheParams) { p.L2CapacityBytes = 0 },
			wantSubstring: "l2_capacity_bytes",
		},
		{
			name:          "zero L3 capacity",
			mutate:        func(p *CacheParams) { p.L3CapacityBytes = 0 },
			wantSubstring: "l3_capacity_bytes",
		},
		{
			name:          "zero L4 capacity",
			mutate:        func(p *CacheParams) { p.L4CapacityBytes = 0 },
			wantSubstring: "l4_capacity_bytes",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultCacheParams()
			tc.mutate(p)
			err := p.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSubstring)
		})
	}
}

func TestCacheParams_Validate_RoyaltyBPSTooHigh(t *testing.T) {
	cases := []struct {
		name          string
		mutate        func(*CacheParams)
		wantSubstring string
	}{
		{
			name:          "origin",
			mutate:        func(p *CacheParams) { p.RoyaltyOriginBps = 10001 },
			wantSubstring: "royalty_origin_bps",
		},
		{
			name:          "storage",
			mutate:        func(p *CacheParams) { p.RoyaltyStorageBps = 10001 },
			wantSubstring: "royalty_storage_bps",
		},
		{
			name:          "bandwidth",
			mutate:        func(p *CacheParams) { p.RoyaltyBandwidthBps = 10001 },
			wantSubstring: "royalty_bandwidth_bps",
		},
		{
			name:          "verification",
			mutate:        func(p *CacheParams) { p.RoyaltyVerificationBps = 10001 },
			wantSubstring: "royalty_verification_bps",
		},
		{
			name:          "governance",
			mutate:        func(p *CacheParams) { p.RoyaltyGovernanceBps = 10001 },
			wantSubstring: "royalty_governance_bps",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultCacheParams()
			tc.mutate(p)
			err := p.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSubstring)
		})
	}
}

func TestCacheParams_Validate_RoyaltyBPSSumDoesNotWrapUint32(t *testing.T) {
	p := DefaultCacheParams()
	p.RoyaltyOriginBps = 0
	p.RoyaltyStorageBps = math.MaxUint32
	p.RoyaltyBandwidthBps = 1
	p.RoyaltyVerificationBps = 10000
	p.RoyaltyGovernanceBps = 0

	err := p.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "royalty_storage_bps")
}

func TestCacheParams_Validate_RoyaltyBPSAtMax(t *testing.T) {
	p := DefaultCacheParams()
	p.RoyaltyOriginBps = 10000
	p.RoyaltyStorageBps = 0
	p.RoyaltyBandwidthBps = 0
	p.RoyaltyVerificationBps = 0
	p.RoyaltyGovernanceBps = 0
	require.NoError(t, p.Validate())
}

func TestCacheParams_Validate_RoyaltyBPSZero(t *testing.T) {
	p := DefaultCacheParams()
	p.RoyaltyOriginBps = 0
	require.Error(t, p.Validate())
}

func TestCacheParams_Validate_ZeroBlocksPerDay(t *testing.T) {
	p := DefaultCacheParams()
	p.BlocksPerDay = 0
	err := p.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocks_per_day")
}

func TestCacheParams_Validate_ZeroMinAccessForPromotion(t *testing.T) {
	p := DefaultCacheParams()
	p.MinAccessForPromotion = 0
	err := p.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min_access_for_promotion")
}

// ---------- MsgCacheStore ----------

func testAddr(t *testing.T) string {
	t.Helper()
	priv := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(priv.PubKey().Address())
	return addr.String()
}

func TestMsgCacheStore_Route(t *testing.T) {
	msg := &MsgCacheStore{}
	assert.Equal(t, RouterKey, msg.Route())
}

func TestMsgCacheStore_Type(t *testing.T) {
	msg := &MsgCacheStore{}
	assert.Equal(t, TypeMsgCacheStore, msg.Type())
}

func TestMsgCacheStore_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgCacheStore{
		Publisher:   testAddr(t),
		ToolId:      "tool-123",
		RequestHash: "abc123hash",
		Content:     []byte("test content"),
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgCacheStore_ValidateBasic_EmptyPublisher(t *testing.T) {
	msg := &MsgCacheStore{
		Publisher:   "",
		ToolId:      "tool-123",
		RequestHash: "abc123hash",
		Content:     []byte("test content"),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgCacheStore_ValidateBasic_RoyaltyRequiresDeterminism(t *testing.T) {
	base := func() *MsgCacheStore {
		return &MsgCacheStore{
			Publisher:   testAddr(t),
			ToolId:      "tool-123",
			RequestHash: "abc123hash",
			Content:     []byte("test content"),
		}
	}

	// Royalty-eligible + non-deterministic is rejected: royalties would pay
	// out for serving content that the tool need not reproduce.
	bad := base()
	bad.RoyaltyEligible = true
	bad.IsDeterministic = false
	err := bad.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "royalty_eligible requires is_deterministic")

	// Royalty-eligible + deterministic is allowed.
	good := base()
	good.RoyaltyEligible = true
	good.IsDeterministic = true
	require.NoError(t, good.ValidateBasic())

	// Non-royalty entries may be non-deterministic.
	nonRoyalty := base()
	nonRoyalty.RoyaltyEligible = false
	nonRoyalty.IsDeterministic = false
	require.NoError(t, nonRoyalty.ValidateBasic())
}

func TestMsgCacheStore_ValidateBasic_InvalidPublisher(t *testing.T) {
	msg := &MsgCacheStore{
		Publisher:   "not-a-valid-address",
		ToolId:      "tool-123",
		RequestHash: "abc123hash",
		Content:     []byte("test content"),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgCacheStore_ValidateBasic_EmptyToolID(t *testing.T) {
	msg := &MsgCacheStore{
		Publisher:   testAddr(t),
		ToolId:      "",
		RequestHash: "abc123hash",
		Content:     []byte("test content"),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgCacheStore_ValidateBasic_EmptyRequestHash(t *testing.T) {
	msg := &MsgCacheStore{
		Publisher:   testAddr(t),
		ToolId:      "tool-123",
		RequestHash: "",
		Content:     []byte("test content"),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgCacheStore_ValidateBasic_EmptyContent(t *testing.T) {
	msg := &MsgCacheStore{
		Publisher:   testAddr(t),
		ToolId:      "tool-123",
		RequestHash: "abc123hash",
		Content:     nil,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgCacheStore_ValidateBasic_PaddedIdentifiers(t *testing.T) {
	for name, msg := range map[string]*MsgCacheStore{
		"tool_id": {
			Publisher:   testAddr(t),
			ToolId:      " tool-123 ",
			RequestHash: "abc123hash",
			Content:     []byte("test content"),
		},
		"request_hash": {
			Publisher:   testAddr(t),
			ToolId:      "tool-123",
			RequestHash: "\tabc123hash",
			Content:     []byte("test content"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), "must not contain leading or trailing whitespace")
		})
	}
}

func TestMsgCacheStore_ValidateBasic_TTLDurationBoundary(t *testing.T) {
	msg := &MsgCacheStore{
		Publisher:   testAddr(t),
		ToolId:      "tool-123",
		RequestHash: "abc123hash",
		Content:     []byte("test content"),
		TtlSeconds:  MaxTTLSeconds,
	}
	require.NoError(t, msg.ValidateBasic())

	msg.TtlSeconds = MaxTTLSeconds + 1
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ttl_seconds exceeds maximum safe duration seconds")
}

func TestMsgCacheStore_GetSigners(t *testing.T) {
	addr := testAddr(t)
	msg := &MsgCacheStore{Publisher: addr}
	signers := msg.GetSigners()
	require.Len(t, signers, 1)
	assert.Equal(t, addr, signers[0].String())
}

// ---------- MsgCacheInvalidate ----------

func TestMsgCacheInvalidate_Route(t *testing.T) {
	msg := &MsgCacheInvalidate{}
	assert.Equal(t, RouterKey, msg.Route())
}

func TestMsgCacheInvalidate_Type(t *testing.T) {
	msg := &MsgCacheInvalidate{}
	assert.Equal(t, TypeMsgCacheInvalidate, msg.Type())
}

func TestMsgCacheInvalidate_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgCacheInvalidate{
		Requester:   testAddr(t),
		TargetType:  InvalidationTargetType_INVALIDATION_TARGET_TYPE_CONTENT_HASH,
		TargetValue: "some-content-hash",
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgCacheInvalidate_ValidateBasic_EmptyRequester(t *testing.T) {
	msg := &MsgCacheInvalidate{
		Requester:   "",
		TargetType:  InvalidationTargetType_INVALIDATION_TARGET_TYPE_CONTENT_HASH,
		TargetValue: "some-content-hash",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgCacheInvalidate_ValidateBasic_UnspecifiedTargetType(t *testing.T) {
	msg := &MsgCacheInvalidate{
		Requester:   testAddr(t),
		TargetType:  InvalidationTargetType_INVALIDATION_TARGET_TYPE_UNSPECIFIED,
		TargetValue: "some-hash",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgCacheInvalidate_ValidateBasic_EmptyTargetValue(t *testing.T) {
	msg := &MsgCacheInvalidate{
		Requester:   testAddr(t),
		TargetType:  InvalidationTargetType_INVALIDATION_TARGET_TYPE_TOOL_ID,
		TargetValue: "",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgCacheInvalidate_ValidateBasic_PaddedTargetValue(t *testing.T) {
	msg := &MsgCacheInvalidate{
		Requester:   testAddr(t),
		TargetType:  InvalidationTargetType_INVALIDATION_TARGET_TYPE_CONTENT_HASH,
		TargetValue: " some-content-hash ",
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not contain leading or trailing whitespace")
}

func TestMsgCacheInvalidate_ValidateBasic_SupportedTargetTypes(t *testing.T) {
	validTypes := []InvalidationTargetType{
		InvalidationTargetType_INVALIDATION_TARGET_TYPE_CONTENT_HASH,
		InvalidationTargetType_INVALIDATION_TARGET_TYPE_TOOL_ID,
		InvalidationTargetType_INVALIDATION_TARGET_TYPE_REQUEST_HASH,
	}
	for _, tt := range validTypes {
		msg := &MsgCacheInvalidate{
			Requester:   testAddr(t),
			TargetType:  tt,
			TargetValue: "some-value",
		}
		require.NoError(t, msg.ValidateBasic(), "target type %v should be valid", tt)
	}
}

func TestMsgCacheInvalidate_ValidateBasic_UnsupportedTargetTypes(t *testing.T) {
	unsupportedTypes := []InvalidationTargetType{
		InvalidationTargetType_INVALIDATION_TARGET_TYPE_PATTERN,
		InvalidationTargetType_INVALIDATION_TARGET_TYPE_EXPIRED,
	}
	for _, tt := range unsupportedTypes {
		msg := &MsgCacheInvalidate{
			Requester:   testAddr(t),
			TargetType:  tt,
			TargetValue: "some-value",
		}
		err := msg.ValidateBasic()
		require.Error(t, err, "target type %v should be rejected until CacheInvalidate implements it", tt)
		require.Contains(t, err.Error(), "unsupported target_type")
	}
}

func TestMsgCacheInvalidate_GetSigners(t *testing.T) {
	addr := testAddr(t)
	msg := &MsgCacheInvalidate{Requester: addr}
	signers := msg.GetSigners()
	require.Len(t, signers, 1)
	assert.Equal(t, addr, signers[0].String())
}

// ---------- MsgRecordCacheHit ----------

func TestMsgRecordCacheHit_Route(t *testing.T) {
	msg := &MsgRecordCacheHit{}
	assert.Equal(t, RouterKey, msg.Route())
}

func TestMsgRecordCacheHit_Type(t *testing.T) {
	msg := &MsgRecordCacheHit{}
	assert.Equal(t, TypeMsgRecordCacheHit, msg.Type())
}

func TestMsgRecordCacheHit_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgRecordCacheHit{
		Router:           testAddr(t),
		ContentHash:      "hash-abc",
		OriginToolId:     "tool-origin",
		ServingToolId:    "tool-serving",
		RequesterAddress: testAddr(t),
		Tier:             CacheTier_CACHE_TIER_L2_SSD,
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgRecordCacheHit_ValidateBasic_EmptyRouter(t *testing.T) {
	msg := &MsgRecordCacheHit{
		Router:           "",
		ContentHash:      "hash-abc",
		OriginToolId:     "tool-origin",
		ServingToolId:    "tool-serving",
		RequesterAddress: testAddr(t),
		Tier:             CacheTier_CACHE_TIER_L2_SSD,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgRecordCacheHit_ValidateBasic_EmptyContentHash(t *testing.T) {
	msg := &MsgRecordCacheHit{
		Router:           testAddr(t),
		ContentHash:      "",
		OriginToolId:     "tool-origin",
		ServingToolId:    "tool-serving",
		RequesterAddress: testAddr(t),
		Tier:             CacheTier_CACHE_TIER_L2_SSD,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgRecordCacheHit_ValidateBasic_EmptyOriginToolID(t *testing.T) {
	msg := &MsgRecordCacheHit{
		Router:           testAddr(t),
		ContentHash:      "hash-abc",
		OriginToolId:     "",
		ServingToolId:    "tool-serving",
		RequesterAddress: testAddr(t),
		Tier:             CacheTier_CACHE_TIER_L2_SSD,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgRecordCacheHit_ValidateBasic_EmptyServingToolID(t *testing.T) {
	msg := &MsgRecordCacheHit{
		Router:           testAddr(t),
		ContentHash:      "hash-abc",
		OriginToolId:     "tool-origin",
		ServingToolId:    "",
		RequesterAddress: testAddr(t),
		Tier:             CacheTier_CACHE_TIER_L2_SSD,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgRecordCacheHit_ValidateBasic_EmptyRequester(t *testing.T) {
	msg := &MsgRecordCacheHit{
		Router:           testAddr(t),
		ContentHash:      "hash-abc",
		OriginToolId:     "tool-origin",
		ServingToolId:    "tool-serving",
		RequesterAddress: "",
		Tier:             CacheTier_CACHE_TIER_L2_SSD,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgRecordCacheHit_ValidateBasic_InvalidTier(t *testing.T) {
	tests := []struct {
		name    string
		tier    CacheTier
		wantErr string
	}{
		{
			name:    "unspecified",
			tier:    CacheTier_CACHE_TIER_UNSPECIFIED,
			wantErr: "tier is required",
		},
		{
			name:    "unknown",
			tier:    CacheTier(99),
			wantErr: "tier is invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MsgRecordCacheHit{
				Router:           testAddr(t),
				ContentHash:      "hash-abc",
				OriginToolId:     "tool-origin",
				ServingToolId:    "tool-serving",
				RequesterAddress: testAddr(t),
				Tier:             tc.tier,
			}
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestMsgRecordCacheHit_ValidateBasic_PaddedIdentifiers(t *testing.T) {
	for name, msg := range map[string]*MsgRecordCacheHit{
		"content_hash": {
			Router:           testAddr(t),
			ContentHash:      " hash-abc ",
			OriginToolId:     "tool-origin",
			ServingToolId:    "tool-serving",
			RequesterAddress: testAddr(t),
			Tier:             CacheTier_CACHE_TIER_L2_SSD,
		},
		"origin_tool_id": {
			Router:           testAddr(t),
			ContentHash:      "hash-abc",
			OriginToolId:     "\ttool-origin",
			ServingToolId:    "tool-serving",
			RequesterAddress: testAddr(t),
			Tier:             CacheTier_CACHE_TIER_L2_SSD,
		},
		"serving_tool_id": {
			Router:           testAddr(t),
			ContentHash:      "hash-abc",
			OriginToolId:     "tool-origin",
			ServingToolId:    "tool-serving\n",
			RequesterAddress: testAddr(t),
			Tier:             CacheTier_CACHE_TIER_L2_SSD,
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), "must not contain leading or trailing whitespace")
		})
	}
}

func TestMsgRecordCacheHit_GetSigners(t *testing.T) {
	addr := testAddr(t)
	msg := &MsgRecordCacheHit{Router: addr}
	signers := msg.GetSigners()
	require.Len(t, signers, 1)
	assert.Equal(t, addr, signers[0].String())
}

// ---------- MsgPromoteTier ----------

func TestMsgPromoteTier_Route(t *testing.T) {
	msg := &MsgPromoteTier{}
	assert.Equal(t, RouterKey, msg.Route())
}

func TestMsgPromoteTier_Type(t *testing.T) {
	msg := &MsgPromoteTier{}
	assert.Equal(t, TypeMsgPromoteTier, msg.Type())
}

func TestMsgPromoteTier_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgPromoteTier{
		Authority:   testAddr(t),
		ContentHash: "hash-abc",
		TargetTier:  CacheTier_CACHE_TIER_L1_MEMORY,
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgPromoteTier_ValidateBasic_EmptyAuthority(t *testing.T) {
	msg := &MsgPromoteTier{
		Authority:   "",
		ContentHash: "hash-abc",
		TargetTier:  CacheTier_CACHE_TIER_L1_MEMORY,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgPromoteTier_ValidateBasic_EmptyContentHash(t *testing.T) {
	msg := &MsgPromoteTier{
		Authority:   testAddr(t),
		ContentHash: "",
		TargetTier:  CacheTier_CACHE_TIER_L1_MEMORY,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgPromoteTier_ValidateBasic_PaddedContentHash(t *testing.T) {
	msg := &MsgPromoteTier{
		Authority:   testAddr(t),
		ContentHash: " hash-abc ",
		TargetTier:  CacheTier_CACHE_TIER_L1_MEMORY,
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not contain leading or trailing whitespace")
}

func TestMsgPromoteTier_ValidateBasic_UnspecifiedTier(t *testing.T) {
	msg := &MsgPromoteTier{
		Authority:   testAddr(t),
		ContentHash: "hash-abc",
		TargetTier:  CacheTier_CACHE_TIER_UNSPECIFIED,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgPromoteTier_ValidateBasic_UnknownTier(t *testing.T) {
	msg := &MsgPromoteTier{
		Authority:   testAddr(t),
		ContentHash: "hash-abc",
		TargetTier:  CacheTier(-1),
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "target_tier is invalid")
}

func TestMsgPromoteTier_GetSigners(t *testing.T) {
	addr := testAddr(t)
	msg := &MsgPromoteTier{Authority: addr}
	signers := msg.GetSigners()
	require.Len(t, signers, 1)
	assert.Equal(t, addr, signers[0].String())
}

// ---------- MsgUpdateParams ----------

func TestMsgUpdateParams_Route(t *testing.T) {
	msg := &MsgUpdateParams{}
	assert.Equal(t, RouterKey, msg.Route())
}

func TestMsgUpdateParams_Type(t *testing.T) {
	msg := &MsgUpdateParams{}
	assert.Equal(t, TypeMsgUpdateParams, msg.Type())
}

func TestMsgUpdateParams_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgUpdateParams{
		Authority: testAddr(t),
		Params:    DefaultCacheParams(),
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgUpdateParams_ValidateBasic_EmptyAuthority(t *testing.T) {
	msg := &MsgUpdateParams{
		Authority: "",
		Params:    DefaultCacheParams(),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUpdateParams_ValidateBasic_NilParams(t *testing.T) {
	msg := &MsgUpdateParams{
		Authority: testAddr(t),
		Params:    nil,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUpdateParams_GetSigners(t *testing.T) {
	addr := testAddr(t)
	msg := &MsgUpdateParams{Authority: addr}
	signers := msg.GetSigners()
	require.Len(t, signers, 1)
	assert.Equal(t, addr, signers[0].String())
}

// ---------- Keys/Constants ----------

func TestModuleConstants(t *testing.T) {
	assert.Equal(t, "cac", ModuleName)
	assert.Equal(t, ModuleName, StoreKey)
	assert.Equal(t, ModuleName, RouterKey)
	assert.Equal(t, ModuleName, QuerierRoute)
	assert.Equal(t, ModuleName, ModuleAccountName)
}

func TestPrefixBytesUnique(t *testing.T) {
	prefixes := []uint8{
		ParamsPrefixByte,
		CacheEntriesPrefixByte,
		RequestIndexPrefixByte,
		ToolIndexPrefixByte,
		CacheHitsPrefixByte,
		CacheStatsPrefixByte,
		InvalidationsPrefixByte,
		TierCapacityPrefixByte,
		EntrySeqKeyPrefixByte,
		HitSeqKeyPrefixByte,
		ContentRequestIndexPrefixByte,
		ExpiryIndexPrefixByte,
		ToolHitStatsPrefixByte,
		ToolMissStatsPrefixByte,
		OriginHitStatsPrefixByte,
		EntryHeightPrefixByte,
		LastDecayTickPrefixByte,
	}

	seen := make(map[uint8]bool)
	for _, p := range prefixes {
		assert.False(t, seen[p], "duplicate prefix byte: 0x%02x", p)
		seen[p] = true
	}
}

func TestPrefixSlicesMatchBytes(t *testing.T) {
	assert.Equal(t, []byte{ParamsPrefixByte}, ParamsPrefix)
	assert.Equal(t, []byte{CacheEntriesPrefixByte}, CacheEntriesPrefix)
	assert.Equal(t, []byte{RequestIndexPrefixByte}, RequestIndexPrefix)
	assert.Equal(t, []byte{ToolIndexPrefixByte}, ToolIndexPrefix)
	assert.Equal(t, []byte{CacheHitsPrefixByte}, CacheHitsPrefix)
	assert.Equal(t, []byte{CacheStatsPrefixByte}, CacheStatsPrefix)
	assert.Equal(t, []byte{InvalidationsPrefixByte}, InvalidationsPrefix)
	assert.Equal(t, []byte{TierCapacityPrefixByte}, TierCapacityPrefix)
	assert.Equal(t, []byte{EntrySeqKeyPrefixByte}, EntrySeqKeyPrefix)
	assert.Equal(t, []byte{HitSeqKeyPrefixByte}, HitSeqKeyPrefix)
	assert.Equal(t, []byte{ContentRequestIndexPrefixByte}, ContentRequestIndexPrefix)
	assert.Equal(t, []byte{ExpiryIndexPrefixByte}, ExpiryIndexPrefix)
	assert.Equal(t, []byte{ToolHitStatsPrefixByte}, ToolHitStatsPrefix)
	assert.Equal(t, []byte{ToolMissStatsPrefixByte}, ToolMissStatsPrefix)
	assert.Equal(t, []byte{OriginHitStatsPrefixByte}, OriginHitStatsPrefix)
	assert.Equal(t, []byte{EntryHeightPrefixByte}, EntryHeightPrefix)
	assert.Equal(t, []byte{LastDecayTickPrefixByte}, LastDecayTickPrefix)
}
