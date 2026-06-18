//go:build cosmos

package types

import (
	"strings"
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ---------------------------------------------------------------------------
// Keys & constants
// ---------------------------------------------------------------------------

func TestModuleConstants(t *testing.T) {
	t.Parallel()
	if ModuleName != "nft" {
		t.Errorf("ModuleName = %q, want %q", ModuleName, "nft")
	}
	if StoreKey != ModuleName {
		t.Errorf("StoreKey = %q, want %q", StoreKey, ModuleName)
	}
	if RouterKey != ModuleName {
		t.Errorf("RouterKey = %q, want %q", RouterKey, ModuleName)
	}
}

func TestKeyPrefixesUnique(t *testing.T) {
	t.Parallel()
	prefixes := [][]byte{
		ToolpackKeyPrefix, ToolpackHistoryPrefix,
		ToolpackCuratorIndex, ToolpackHistoryIndex,
		RoyaltyAccumulatorPrefix,
	}
	seen := make(map[byte]struct{})
	for _, p := range prefixes {
		if len(p) != 1 {
			t.Fatalf("prefix %v should be length 1", p)
		}
		if _, ok := seen[p[0]]; ok {
			t.Errorf("duplicate prefix byte 0x%02x", p[0])
		}
		seen[p[0]] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	errs := []error{
		ErrInvalidCurator, ErrInvalidToolpackID, ErrDuplicateToolpack,
		ErrToolpackNotFound, ErrInactiveToolpack, ErrInvalidRoyalty,
		ErrInvalidTools, ErrUnauthorized,
	}
	for _, e := range errs {
		if e == nil {
			t.Error("sentinel error should not be nil")
		}
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
			t.Errorf("duplicate error code %d: %q and %q", code, prev, e.Error())
		}
		codes[code] = e.Error()
	}
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

func TestEventTypesNonEmpty(t *testing.T) {
	t.Parallel()
	types := []string{
		EventTypeToolpackMinted, EventTypeToolpackUpdated,
		EventTypeToolpackDeactivated, EventTypeRoyaltyPayout,
	}
	seen := make(map[string]struct{})
	for _, et := range types {
		if et == "" {
			t.Error("event type should not be empty")
		}
		if _, ok := seen[et]; ok {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = struct{}{}
	}
}

func TestAttributeKeysNonEmpty(t *testing.T) {
	t.Parallel()
	attrs := []string{
		AttributeKeyToolpackID, AttributeKeyCurator,
		AttributeKeyVersion, AttributeKeyAmount,
	}
	seen := make(map[string]struct{})
	for _, a := range attrs {
		if a == "" {
			t.Error("attribute key should not be empty")
		}
		if _, ok := seen[a]; ok {
			t.Errorf("duplicate attribute key: %s", a)
		}
		seen[a] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Genesis
// ---------------------------------------------------------------------------

const genesisCurator = "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"

func validGenesisToolpack(id string) *ToolpackNFT {
	return &ToolpackNFT{
		Id:            id,
		Version:       1,
		Curator:       genesisCurator,
		Tools:         []*ToolReference{{ToolId: "tool-a", Version: "v1"}},
		PolicyVersion: "policy-v1",
		RoyaltyBps:    100,
		Active:        true,
	}
}

func validGenesisHistory(id string, version uint64) *ToolpackHistory {
	return &ToolpackHistory{
		Id:            id,
		Version:       version,
		Curator:       genesisCurator,
		Tools:         []*ToolReference{{ToolId: "tool-a", Version: "v1"}},
		PolicyVersion: "policy-v1",
		RoyaltyBps:    100,
	}
}

func validGenesisRoyalty(toolpackID string) RoyaltyEntry {
	return RoyaltyEntry{
		ToolpackID: toolpackID,
		Denom:      "ulac",
		Amount:     sdkmath.NewInt(42),
		Count:      1,
		LastPayout: time.Date(2026, time.January, 21, 12, 0, 0, 0, time.UTC),
	}
}

func TestDefaultGenesis(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	if gs == nil {
		t.Fatal("DefaultGenesis() returned nil")
	}
	if gs.Toolpacks == nil {
		t.Fatal("Toolpacks should not be nil")
	}
	if len(gs.Toolpacks) != 0 {
		t.Errorf("len(Toolpacks) = %d, want 0", len(gs.Toolpacks))
	}
	if gs.Histories == nil {
		t.Fatal("Histories should not be nil")
	}
	if len(gs.Histories) != 0 {
		t.Errorf("len(Histories) = %d, want 0", len(gs.Histories))
	}
}

func TestGenesisState_Validate_Default(t *testing.T) {
	t.Parallel()
	if err := DefaultGenesis().Validate(); err != nil {
		t.Fatalf("default genesis should be valid: %v", err)
	}
}

func TestGenesisState_Validate_NilState(t *testing.T) {
	t.Parallel()
	var gs *GenesisState
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil genesis state")
	}
}

func TestGenesisState_Validate_NilToolpackEntry(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Toolpacks: []*ToolpackNFT{nil},
		Histories: []*ToolpackHistory{},
	}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for nil toolpack entry")
	}
}

func TestGenesisState_Validate_EmptyToolpackID(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Toolpacks: []*ToolpackNFT{{Id: ""}},
		Histories: []*ToolpackHistory{},
	}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for empty toolpack id")
	}
}

func TestGenesisState_Validate_DuplicateToolpackID(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Toolpacks: []*ToolpackNFT{
			validGenesisToolpack("tp-1"),
			validGenesisToolpack("tp-1"),
		},
		Histories: []*ToolpackHistory{},
	}
	if err := gs.Validate(); err == nil {
		t.Error("expected error for duplicate toolpack id")
	}
}

func TestGenesisState_Validate_ValidToolpacks(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Toolpacks: []*ToolpackNFT{
			validGenesisToolpack("tp-1"),
			validGenesisToolpack("tp-2"),
		},
		Histories: []*ToolpackHistory{},
	}
	if err := gs.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenesisState_Validate_EmptyToolpacksValid(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Toolpacks: []*ToolpackNFT{},
		Histories: []*ToolpackHistory{},
	}
	if err := gs.Validate(); err != nil {
		t.Fatalf("empty toolpacks should be valid: %v", err)
	}
}

func TestDefaultGenesis_HasRoyaltiesSlice(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	if gs.Royalties == nil {
		t.Error("Royalties should not be nil")
	}
	if len(gs.Royalties) != 0 {
		t.Errorf("len(Royalties) = %d, want 0", len(gs.Royalties))
	}
}

func TestGenesisState_Validate_RejectsInvalidToolpackFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*ToolpackNFT)
	}{
		{
			name: "uncanonical id",
			mutate: func(pack *ToolpackNFT) {
				pack.Id = " tp-1 "
			},
		},
		{
			name: "zero version",
			mutate: func(pack *ToolpackNFT) {
				pack.Version = 0
			},
		},
		{
			name: "invalid curator",
			mutate: func(pack *ToolpackNFT) {
				pack.Curator = "not-bech32"
			},
		},
		{
			name: "empty tools",
			mutate: func(pack *ToolpackNFT) {
				pack.Tools = nil
			},
		},
		{
			name: "duplicate tools",
			mutate: func(pack *ToolpackNFT) {
				pack.Tools = []*ToolReference{
					{ToolId: "tool-a", Version: "v1"},
					{ToolId: "tool-a", Version: "v2"},
				}
			},
		},
		{
			name: "oversized royalty",
			mutate: func(pack *ToolpackNFT) {
				pack.RoyaltyBps = MaxRoyaltyBPS + 1
			},
		},
		{
			name: "invalid created_at",
			mutate: func(pack *ToolpackNFT) {
				pack.CreatedAt = &timestamppb.Timestamp{Nanos: 1_000_000_000}
			},
		},
		{
			name: "invalid updated_at",
			mutate: func(pack *ToolpackNFT) {
				pack.UpdatedAt = &timestamppb.Timestamp{Nanos: -1}
			},
		},
		{
			name: "invalid expires_at",
			mutate: func(pack *ToolpackNFT) {
				pack.ExpiresAt = &timestamppb.Timestamp{Seconds: 253402300800}
			},
		},
		{
			name: "updated_at before created_at",
			mutate: func(pack *ToolpackNFT) {
				createdAt := time.Unix(1_700_000_100, 0).UTC()
				pack.CreatedAt = timestamppb.New(createdAt)
				pack.UpdatedAt = timestamppb.New(createdAt.Add(-time.Second))
			},
		},
		{
			name: "expires_at before created_at",
			mutate: func(pack *ToolpackNFT) {
				createdAt := time.Unix(1_700_000_100, 0).UTC()
				pack.CreatedAt = timestamppb.New(createdAt)
				pack.ExpiresAt = timestamppb.New(createdAt.Add(-time.Second))
			},
		},
		{
			name: "expires_at equals created_at",
			mutate: func(pack *ToolpackNFT) {
				createdAt := time.Unix(1_700_000_100, 0).UTC()
				pack.CreatedAt = timestamppb.New(createdAt)
				pack.ExpiresAt = timestamppb.New(createdAt)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pack := validGenesisToolpack("tp-1")
			tc.mutate(pack)
			gs := &GenesisState{Toolpacks: []*ToolpackNFT{pack}}
			if err := gs.Validate(); err == nil {
				t.Fatalf("expected invalid genesis for %s", tc.name)
			}
		})
	}
}

func TestGenesisState_Validate_ValidHistoriesAndRoyalties(t *testing.T) {
	t.Parallel()

	pack := validGenesisToolpack("tp-1")
	pack.Version = 2
	gs := &GenesisState{
		Toolpacks: []*ToolpackNFT{pack},
		Histories: []*ToolpackHistory{
			validGenesisHistory("tp-1", 1),
			validGenesisHistory("tp-1", 2),
		},
		Royalties: []RoyaltyEntry{
			validGenesisRoyalty("tp-1"),
		},
	}
	if err := gs.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenesisState_Validate_RejectsInvalidHistories(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		histories []*ToolpackHistory
	}{
		{
			name:      "nil history",
			histories: []*ToolpackHistory{nil},
		},
		{
			name:      "unknown toolpack",
			histories: []*ToolpackHistory{validGenesisHistory("missing", 1)},
		},
		{
			name: "zero version",
			histories: []*ToolpackHistory{
				validGenesisHistory("tp-1", 0),
			},
		},
		{
			name: "version exceeds current",
			histories: []*ToolpackHistory{
				validGenesisHistory("tp-1", 2),
			},
		},
		{
			name: "duplicate version",
			histories: []*ToolpackHistory{
				validGenesisHistory("tp-1", 1),
				validGenesisHistory("tp-1", 1),
			},
		},
		{
			name: "invalid tools",
			histories: []*ToolpackHistory{
				{
					Id:            "tp-1",
					Version:       1,
					Curator:       genesisCurator,
					Tools:         []*ToolReference{{ToolId: " "}},
					PolicyVersion: "policy-v1",
				},
			},
		},
		{
			name: "invalid created_at",
			histories: []*ToolpackHistory{
				func() *ToolpackHistory {
					history := validGenesisHistory("tp-1", 1)
					history.CreatedAt = &timestamppb.Timestamp{Nanos: 1_000_000_000}
					return history
				}(),
			},
		},
		{
			name: "invalid expires_at",
			histories: []*ToolpackHistory{
				func() *ToolpackHistory {
					history := validGenesisHistory("tp-1", 1)
					history.ExpiresAt = &timestamppb.Timestamp{Seconds: 253402300800}
					return history
				}(),
			},
		},
		{
			name: "expires_at before created_at",
			histories: []*ToolpackHistory{
				func() *ToolpackHistory {
					createdAt := time.Unix(1_700_000_100, 0).UTC()
					history := validGenesisHistory("tp-1", 1)
					history.CreatedAt = timestamppb.New(createdAt)
					history.ExpiresAt = timestamppb.New(createdAt.Add(-time.Second))
					return history
				}(),
			},
		},
		{
			name: "expires_at equals created_at",
			histories: []*ToolpackHistory{
				func() *ToolpackHistory {
					createdAt := time.Unix(1_700_000_100, 0).UTC()
					history := validGenesisHistory("tp-1", 1)
					history.CreatedAt = timestamppb.New(createdAt)
					history.ExpiresAt = timestamppb.New(createdAt)
					return history
				}(),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gs := &GenesisState{
				Toolpacks: []*ToolpackNFT{validGenesisToolpack("tp-1")},
				Histories: tc.histories,
			}
			if err := gs.Validate(); err == nil {
				t.Fatalf("expected invalid genesis for %s", tc.name)
			}
		})
	}
}

func TestGenesisState_Validate_RejectsInvalidRoyalties(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		royalties []RoyaltyEntry
	}{
		{
			name:      "unknown toolpack",
			royalties: []RoyaltyEntry{validGenesisRoyalty("missing")},
		},
		{
			name: "invalid denom",
			royalties: []RoyaltyEntry{
				func() RoyaltyEntry {
					royalty := validGenesisRoyalty("tp-1")
					royalty.Denom = "bad denom"
					return royalty
				}(),
			},
		},
		{
			name: "zero amount",
			royalties: []RoyaltyEntry{
				func() RoyaltyEntry {
					royalty := validGenesisRoyalty("tp-1")
					royalty.Amount = sdkmath.ZeroInt()
					return royalty
				}(),
			},
		},
		{
			name: "zero count",
			royalties: []RoyaltyEntry{
				func() RoyaltyEntry {
					royalty := validGenesisRoyalty("tp-1")
					royalty.Count = 0
					return royalty
				}(),
			},
		},
		{
			name: "zero last payout",
			royalties: []RoyaltyEntry{
				func() RoyaltyEntry {
					royalty := validGenesisRoyalty("tp-1")
					royalty.LastPayout = time.Time{}
					return royalty
				}(),
			},
		},
		{
			name: "duplicate denom",
			royalties: []RoyaltyEntry{
				validGenesisRoyalty("tp-1"),
				func() RoyaltyEntry {
					royalty := validGenesisRoyalty("tp-1")
					royalty.Amount = sdkmath.NewInt(2)
					return royalty
				}(),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gs := &GenesisState{
				Toolpacks: []*ToolpackNFT{validGenesisToolpack("tp-1")},
				Royalties: tc.royalties,
			}
			if err := gs.Validate(); err == nil {
				t.Fatalf("expected invalid genesis for %s", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MsgMintToolpack.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgMintToolpack_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: 500,
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("expected no error: %v", err)
	}
}

func TestMsgMintToolpack_ValidateBasic_EmptyCurator(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: 500,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty curator")
	}
}

func TestMsgMintToolpack_ValidateBasic_WhitespaceCurator(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "   ",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: 500,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for whitespace-only curator")
	}
}

func TestMsgMintToolpack_ValidateBasic_InvalidCuratorAddress(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "not-a-bech32-address",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: 500,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for invalid curator address")
	}
}

func TestMsgMintToolpack_ValidateBasic_EmptyID(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: 500,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty id")
	}
}

func TestMsgMintToolpack_ValidateBasic_PaddedID(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         " tp-1 ",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: 500,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatal("expected error for padded id")
	}
	if !strings.Contains(err.Error(), "id must be canonical") {
		t.Fatalf("expected canonical id error, got %q", err.Error())
	}
}

func TestMsgMintToolpack_ValidateBasic_PaddedPolicyVersion(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:       "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:            "tp-1",
		Tools:         []*ToolReference{{ToolId: "tool-a"}},
		PolicyVersion: " policy-v1 ",
		RoyaltyBps:    500,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatal("expected error for padded policy_version")
	}
	if !strings.Contains(err.Error(), "policy_version must be canonical") {
		t.Fatalf("expected canonical policy_version error, got %q", err.Error())
	}
}

func TestMsgMintToolpack_ValidateBasic_EmptyTools(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{},
		RoyaltyBps: 500,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty tools")
	}
}

func TestMsgMintToolpack_ValidateBasic_NilTools(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      nil,
		RoyaltyBps: 500,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for nil tools")
	}
}

func TestMsgMintToolpack_ValidateBasic_RoyaltyBpsAboveMax(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: MaxRoyaltyBPS + 1,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Errorf("expected error for royalty_bps > %d", MaxRoyaltyBPS)
	}
}

func TestMsgMintToolpack_ValidateBasic_RoyaltyBpsAtMax(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: MaxRoyaltyBPS,
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("royalty_bps at %d should be valid: %v", MaxRoyaltyBPS, err)
	}
}

func TestMsgMintToolpack_ValidateBasic_ZeroRoyaltyBps(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}},
		RoyaltyBps: 0,
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("zero royalty_bps should be valid: %v", err)
	}
}

func TestMsgMintToolpack_ValidateBasic_MultipleTools(t *testing.T) {
	t.Parallel()
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}, {ToolId: "tool-b"}, {ToolId: "tool-c"}},
		RoyaltyBps: 100,
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("multiple tools should be valid: %v", err)
	}
}

// TestMsgMintToolpack_ValidateBasic_RejectsOversizedToolsSlice pins
// the per-message cap on the Tools slice. Without it, a curator
// could embed tens of thousands of ToolReference entries, bloating
// the stored ToolpackNFT and every read/iteration downstream.
func TestMsgMintToolpack_ValidateBasic_RejectsOversizedToolsSlice(t *testing.T) {
	t.Parallel()
	tools := make([]*ToolReference, MaxToolpackTools+1)
	for i := range tools {
		tools[i] = &ToolReference{ToolId: "tool-a"}
	}
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-big",
		Tools:      tools,
		RoyaltyBps: 100,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatalf("expected error for oversized tools slice")
	}
	if !strings.Contains(err.Error(), "tools") || !strings.Contains(err.Error(), "cap") {
		t.Errorf("expected cap error, got: %v", err)
	}
}

// TestMsgMintToolpack_ValidateBasic_RejectsOversizedToolID pins
// the per-entry tool_id length cap.
func TestMsgMintToolpack_ValidateBasic_RejectsOversizedToolID(t *testing.T) {
	t.Parallel()
	huge := make([]byte, MaxToolIDLen+1)
	for i := range huge {
		huge[i] = 'a'
	}
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: string(huge)}},
		RoyaltyBps: 100,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatalf("expected error for oversized tool_id")
	}
	if !strings.Contains(err.Error(), "tools[0].tool_id") {
		t.Errorf("expected tool_id cap error, got: %v", err)
	}
}

// TestMsgMintToolpack_ValidateBasic_RejectsOversizedToolVersion pins
// the per-entry version length cap.
func TestMsgMintToolpack_ValidateBasic_RejectsOversizedToolVersion(t *testing.T) {
	t.Parallel()
	huge := make([]byte, MaxToolVersionLen+1)
	for i := range huge {
		huge[i] = 'v'
	}
	msg := &MsgMintToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a", Version: string(huge)}},
		RoyaltyBps: 100,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatalf("expected error for oversized tool version")
	}
	if !strings.Contains(err.Error(), "tools[0].version") {
		t.Errorf("expected version cap error, got: %v", err)
	}
}

func TestMsgMintToolpack_ValidateBasic_RejectsMalformedToolReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tools []*ToolReference
		want  string
	}{
		{
			name:  "empty tool id",
			tools: []*ToolReference{{ToolId: "", Version: "1"}},
			want:  "tools[0].tool_id is required",
		},
		{
			name:  "padded tool id",
			tools: []*ToolReference{{ToolId: " tool-a ", Version: "1"}},
			want:  "tools[0].tool_id must be canonical",
		},
		{
			name:  "padded version",
			tools: []*ToolReference{{ToolId: "tool-a", Version: " 1 "}},
			want:  "tools[0].version must be canonical",
		},
		{
			name: "duplicate tool id",
			tools: []*ToolReference{
				{ToolId: "tool-a", Version: "1"},
				{ToolId: "tool-a", Version: "2"},
			},
			want: "duplicate tool_id tool-a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MsgMintToolpack{
				Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
				Id:         "tp-1",
				Tools:      tc.tools,
				RoyaltyBps: 100,
			}
			err := msg.ValidateBasic()
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

// TestMsgUpdateToolpack_ValidateBasic_RejectsOversizedToolsSlice
// ensures update can't be used to sidestep the mint-time cap.
func TestMsgUpdateToolpack_ValidateBasic_RejectsOversizedToolsSlice(t *testing.T) {
	t.Parallel()
	tools := make([]*ToolReference, MaxToolpackTools+1)
	for i := range tools {
		tools[i] = &ToolReference{ToolId: "tool-a"}
	}
	msg := &MsgUpdateToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      tools,
		RoyaltyBps: 100,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatalf("expected error for oversized update tools slice")
	}
	if !strings.Contains(err.Error(), "tools") || !strings.Contains(err.Error(), "cap") {
		t.Errorf("expected cap error, got: %v", err)
	}
}

func TestMsgUpdateToolpack_ValidateBasic_RejectsMalformedToolReferences(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		Tools:      []*ToolReference{{ToolId: "tool-a"}, {ToolId: "tool-a"}},
		RoyaltyBps: 100,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "duplicate tool_id tool-a") {
		t.Fatalf("expected duplicate tool_id error, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// MsgUpdateToolpack.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgUpdateToolpack_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		RoyaltyBps: 300,
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("expected no error: %v", err)
	}
}

func TestMsgUpdateToolpack_ValidateBasic_EmptyCurator(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:    "",
		Id:         "tp-1",
		RoyaltyBps: 300,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty curator")
	}
}

func TestMsgUpdateToolpack_ValidateBasic_InvalidCuratorAddress(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:    "not-a-bech32-address",
		Id:         "tp-1",
		RoyaltyBps: 300,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for invalid curator address")
	}
}

func TestMsgUpdateToolpack_ValidateBasic_EmptyID(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "",
		RoyaltyBps: 300,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty id")
	}
}

func TestMsgUpdateToolpack_ValidateBasic_PaddedID(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "\ttp-1",
		RoyaltyBps: 300,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatal("expected error for padded id")
	}
	if !strings.Contains(err.Error(), "id must be canonical") {
		t.Fatalf("expected canonical id error, got %q", err.Error())
	}
}

func TestMsgUpdateToolpack_ValidateBasic_PaddedPolicyVersion(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:       "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:            "tp-1",
		PolicyVersion: "\tpolicy-v2",
		RoyaltyBps:    300,
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatal("expected error for padded policy_version")
	}
	if !strings.Contains(err.Error(), "policy_version must be canonical") {
		t.Fatalf("expected canonical policy_version error, got %q", err.Error())
	}
}

func TestMsgUpdateToolpack_ValidateBasic_RoyaltyBpsAboveMax(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		RoyaltyBps: MaxRoyaltyBPS + 1,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Errorf("expected error for royalty_bps > %d", MaxRoyaltyBPS)
	}
}

func TestMsgUpdateToolpack_ValidateBasic_RoyaltyBpsAtMax(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateToolpack{
		Curator:    "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:         "tp-1",
		RoyaltyBps: MaxRoyaltyBPS,
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("royalty_bps at %d should be valid: %v", MaxRoyaltyBPS, err)
	}
}

// ---------------------------------------------------------------------------
// MsgDeactivateToolpack.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgDeactivateToolpack_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgDeactivateToolpack{
		Curator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:      "tp-1",
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("expected no error: %v", err)
	}
}

func TestMsgDeactivateToolpack_ValidateBasic_EmptyCurator(t *testing.T) {
	t.Parallel()
	msg := &MsgDeactivateToolpack{
		Curator: "",
		Id:      "tp-1",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty curator")
	}
}

func TestMsgDeactivateToolpack_ValidateBasic_InvalidCuratorAddress(t *testing.T) {
	t.Parallel()
	msg := &MsgDeactivateToolpack{
		Curator: "not-a-bech32-address",
		Id:      "tp-1",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for invalid curator address")
	}
}

func TestMsgDeactivateToolpack_ValidateBasic_EmptyID(t *testing.T) {
	t.Parallel()
	msg := &MsgDeactivateToolpack{
		Curator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:      "",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty id")
	}
}

func TestMsgDeactivateToolpack_ValidateBasic_PaddedID(t *testing.T) {
	t.Parallel()
	msg := &MsgDeactivateToolpack{
		Curator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Id:      "tp-1\n",
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatal("expected error for padded id")
	}
	if !strings.Contains(err.Error(), "id must be canonical") {
		t.Fatalf("expected canonical id error, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// MsgRecordRoyaltyPayout.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgRecordRoyaltyPayout_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgRecordRoyaltyPayout{
		Authority:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		ToolpackId: "tp-1",
		Amount:     &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("expected no error: %v", err)
	}
}

func TestMsgRecordRoyaltyPayout_ValidateBasic_EmptyAuthority(t *testing.T) {
	t.Parallel()
	msg := &MsgRecordRoyaltyPayout{
		Authority:  "",
		ToolpackId: "tp-1",
		Amount:     &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty authority")
	}
}

func TestMsgRecordRoyaltyPayout_ValidateBasic_InvalidAuthorityAddress(t *testing.T) {
	t.Parallel()
	msg := &MsgRecordRoyaltyPayout{
		Authority:  "not-a-bech32-address",
		ToolpackId: "tp-1",
		Amount:     &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for invalid authority address")
	}
}

func TestMsgRecordRoyaltyPayout_ValidateBasic_EmptyToolpackID(t *testing.T) {
	t.Parallel()
	msg := &MsgRecordRoyaltyPayout{
		Authority:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		ToolpackId: "",
		Amount:     &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty toolpack_id")
	}
}

func TestMsgRecordRoyaltyPayout_ValidateBasic_PaddedToolpackID(t *testing.T) {
	t.Parallel()
	msg := &MsgRecordRoyaltyPayout{
		Authority:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		ToolpackId: " tp-1",
		Amount:     &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
	}
	err := msg.ValidateBasic()
	if err == nil {
		t.Fatal("expected error for padded toolpack_id")
	}
	if !strings.Contains(err.Error(), "toolpack_id must be canonical") {
		t.Fatalf("expected canonical toolpack_id error, got %q", err.Error())
	}
}

func TestMsgRecordRoyaltyPayout_ValidateBasic_NilAmount(t *testing.T) {
	t.Parallel()
	msg := &MsgRecordRoyaltyPayout{
		Authority:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		ToolpackId: "tp-1",
		Amount:     nil,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for nil amount")
	}
}

func TestMsgRecordRoyaltyPayout_ValidateBasic_InvalidAmount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		amount *basev1beta1.Coin
	}{
		{name: "empty denom", amount: &basev1beta1.Coin{Denom: "", Amount: "500"}},
		{name: "invalid denom", amount: &basev1beta1.Coin{Denom: "bad denom", Amount: "500"}},
		{name: "invalid amount", amount: &basev1beta1.Coin{Denom: "ulac", Amount: "five"}},
		{name: "zero amount", amount: &basev1beta1.Coin{Denom: "ulac", Amount: "0"}},
		{name: "negative amount", amount: &basev1beta1.Coin{Denom: "ulac", Amount: "-1"}},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := &MsgRecordRoyaltyPayout{
				Authority:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
				ToolpackId: "tp-1",
				Amount:     tc.amount,
			}
			if err := msg.ValidateBasic(); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Codec
// ---------------------------------------------------------------------------

func TestRegisterLegacyAminoCodec(t *testing.T) {
	t.Parallel()
	amino := codec.NewLegacyAmino()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterLegacyAminoCodec panicked: %v", r)
		}
	}()
	RegisterLegacyAminoCodec(amino)
}

func TestRegisterInterfaces(t *testing.T) {
	t.Parallel()
	registry := cdctypes.NewInterfaceRegistry()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterInterfaces panicked: %v", r)
		}
	}()
	RegisterInterfaces(registry)
}

func TestModuleCdc_NotNil(t *testing.T) {
	t.Parallel()
	if ModuleCdc == nil {
		t.Error("ModuleCdc should not be nil")
	}
}

func TestAmino_NotNil(t *testing.T) {
	t.Parallel()
	if Amino == nil {
		t.Error("Amino should not be nil")
	}
}
