
package types

import (
	"fmt"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RoyaltyEntry captures a single royalty accumulator record for genesis export.
type RoyaltyEntry struct {
	ToolpackID string      `json:"toolpack_id" yaml:"toolpack_id"`
	Denom      string      `json:"denom" yaml:"denom"`
	Amount     sdkmath.Int `json:"amount" yaml:"amount"`
	Count      uint64      `json:"count" yaml:"count"`
	LastPayout time.Time   `json:"last_payout" yaml:"last_payout"`
}

// GenesisState defines the initial state for the toolpack NFT module.
type GenesisState struct {
	Toolpacks []*ToolpackNFT     `json:"toolpacks" yaml:"toolpacks"`
	Histories []*ToolpackHistory `json:"histories" yaml:"histories"`
	Royalties []RoyaltyEntry     `json:"royalties" yaml:"royalties"`
}

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Toolpacks: []*ToolpackNFT{},
		Histories: []*ToolpackHistory{},
		Royalties: []RoyaltyEntry{},
	}
}

// Validate performs basic validation checks on the genesis data.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}

	seen := make(map[string]struct{}, len(gs.Toolpacks))
	for i, pack := range gs.Toolpacks {
		if err := validateGenesisToolpack(i, pack); err != nil {
			return err
		}
		if _, exists := seen[pack.Id]; exists {
			return fmt.Errorf("duplicate toolpack id %s", pack.Id)
		}
		seen[pack.Id] = struct{}{}
	}

	if err := gs.validateHistories(seen); err != nil {
		return err
	}
	if err := gs.validateRoyalties(seen); err != nil {
		return err
	}

	return nil
}

func validateGenesisToolpack(index int, pack *ToolpackNFT) error {
	if pack == nil {
		return fmt.Errorf("toolpack entry %d is nil", index)
	}
	if err := validateCanonicalID("toolpack", index, pack.Id); err != nil {
		return err
	}
	if pack.Version == 0 {
		return fmt.Errorf("toolpack %s version must be > 0", pack.Id)
	}
	if _, err := sdk.AccAddressFromBech32(pack.Curator); err != nil {
		return fmt.Errorf("toolpack %s has invalid curator: %w", pack.Id, err)
	}
	if err := validateGenesisToolReferences("toolpack "+pack.Id, pack.Tools); err != nil {
		return err
	}
	if strings.TrimSpace(pack.PolicyVersion) != pack.PolicyVersion {
		return fmt.Errorf("toolpack %s policy version must be canonical", pack.Id)
	}
	if pack.RoyaltyBps > MaxRoyaltyBPS {
		return fmt.Errorf("toolpack %s royalty_bps exceeds maximum (%d)", pack.Id, MaxRoyaltyBPS)
	}
	if err := validateOptionalTimestamp("toolpack "+pack.Id, "created_at", pack.CreatedAt); err != nil {
		return err
	}
	if err := validateOptionalTimestamp("toolpack "+pack.Id, "updated_at", pack.UpdatedAt); err != nil {
		return err
	}
	if err := validateOptionalTimestamp("toolpack "+pack.Id, "expires_at", pack.ExpiresAt); err != nil {
		return err
	}
	if err := validateToolpackTimestampOrder(pack.Id, pack.CreatedAt, pack.UpdatedAt, pack.ExpiresAt); err != nil {
		return err
	}
	return nil
}

func (gs *GenesisState) validateHistories(toolpacks map[string]struct{}) error {
	seen := make(map[string]struct{}, len(gs.Histories))
	versions := make(map[string]uint64, len(gs.Toolpacks))
	for _, pack := range gs.Toolpacks {
		versions[pack.Id] = pack.Version
	}

	for i, history := range gs.Histories {
		if history == nil {
			return fmt.Errorf("history entry %d is nil", i)
		}
		if err := validateCanonicalID("history", i, history.Id); err != nil {
			return err
		}
		if _, exists := toolpacks[history.Id]; !exists {
			return fmt.Errorf("history %s references unknown toolpack", history.Id)
		}
		if history.Version == 0 {
			return fmt.Errorf("history %s version must be > 0", history.Id)
		}
		if history.Version > versions[history.Id] {
			return fmt.Errorf("history %s version %d exceeds toolpack version %d", history.Id, history.Version, versions[history.Id])
		}
		key := fmt.Sprintf("%s/%d", history.Id, history.Version)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate history entry %s", key)
		}
		seen[key] = struct{}{}

		if _, err := sdk.AccAddressFromBech32(history.Curator); err != nil {
			return fmt.Errorf("history %s version %d has invalid curator: %w", history.Id, history.Version, err)
		}
		if err := validateGenesisToolReferences(fmt.Sprintf("history %s version %d", history.Id, history.Version), history.Tools); err != nil {
			return err
		}
		if strings.TrimSpace(history.PolicyVersion) != history.PolicyVersion {
			return fmt.Errorf("history %s version %d policy version must be canonical", history.Id, history.Version)
		}
		if history.RoyaltyBps > MaxRoyaltyBPS {
			return fmt.Errorf("history %s version %d royalty_bps exceeds maximum (%d)", history.Id, history.Version, MaxRoyaltyBPS)
		}
		owner := fmt.Sprintf("history %s version %d", history.Id, history.Version)
		if err := validateOptionalTimestamp(owner, "created_at", history.CreatedAt); err != nil {
			return err
		}
		if err := validateOptionalTimestamp(owner, "expires_at", history.ExpiresAt); err != nil {
			return err
		}
		if err := validateHistoryTimestampOrder(history.Id, history.Version, history.CreatedAt, history.ExpiresAt); err != nil {
			return err
		}
	}
	return nil
}

func (gs *GenesisState) validateRoyalties(toolpacks map[string]struct{}) error {
	seen := make(map[string]struct{}, len(gs.Royalties))
	for i, royalty := range gs.Royalties {
		if err := validateCanonicalID("royalty toolpack", i, royalty.ToolpackID); err != nil {
			return err
		}
		if _, exists := toolpacks[royalty.ToolpackID]; !exists {
			return fmt.Errorf("royalty entry %d references unknown toolpack %s", i, royalty.ToolpackID)
		}
		if err := sdk.ValidateDenom(royalty.Denom); err != nil {
			return fmt.Errorf("royalty entry %d has invalid denom: %w", i, err)
		}
		if royalty.Amount.IsNil() || !royalty.Amount.IsPositive() {
			return fmt.Errorf("royalty entry %d amount must be positive", i)
		}
		if royalty.Count == 0 {
			return fmt.Errorf("royalty entry %d count must be > 0", i)
		}
		if royalty.LastPayout.IsZero() {
			return fmt.Errorf("royalty entry %d last_payout is required", i)
		}
		key := royalty.ToolpackID + "/" + royalty.Denom
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate royalty entry %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateOptionalTimestamp(owner, field string, ts *time.Time) error {
	if ts == nil {
		return nil
	}
	if ts.IsZero() {
		return fmt.Errorf("%s %s is invalid: zero timestamp", owner, field)
	}
	return nil
}

func validateToolpackTimestampOrder(id string, createdAt, updatedAt, expiresAt *time.Time) error {
	if createdAt != nil && updatedAt != nil && updatedAt.Before(*createdAt) {
		return fmt.Errorf("toolpack %s updated_at cannot be before created_at", id)
	}
	if createdAt != nil && expiresAt != nil && !expiresAt.After(*createdAt) {
		return fmt.Errorf("toolpack %s expires_at must be after created_at", id)
	}
	return nil
}

func validateHistoryTimestampOrder(id string, version uint64, createdAt, expiresAt *time.Time) error {
	if createdAt != nil && expiresAt != nil && !expiresAt.After(*createdAt) {
		return fmt.Errorf("history %s version %d expires_at must be after created_at", id, version)
	}
	return nil
}

func validateCanonicalID(kind string, index int, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("%s entry %d missing id", kind, index)
	}
	if trimmed != id {
		return fmt.Errorf("%s entry %d id must be canonical", kind, index)
	}
	return nil
}

func validateGenesisToolReferences(owner string, tools []*ToolReference) error {
	if len(tools) == 0 {
		return fmt.Errorf("%s must contain at least one tool", owner)
	}
	if err := validateToolReferences(tools); err != nil {
		return fmt.Errorf("%s has invalid tools: %w", owner, err)
	}

	seen := make(map[string]struct{}, len(tools))
	for i, tool := range tools {
		toolID := strings.TrimSpace(tool.GetToolId())
		if toolID == "" {
			return fmt.Errorf("%s tools[%d].tool_id is required", owner, i)
		}
		if toolID != tool.GetToolId() {
			return fmt.Errorf("%s tools[%d].tool_id must be canonical", owner, i)
		}
		if version := tool.GetVersion(); strings.TrimSpace(version) != version {
			return fmt.Errorf("%s tools[%d].version must be canonical", owner, i)
		}
		if _, exists := seen[toolID]; exists {
			return fmt.Errorf("%s has duplicate tool_id %s", owner, toolID)
		}
		seen[toolID] = struct{}{}
	}
	return nil
}
