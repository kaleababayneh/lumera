package types

import (
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// WorkflowStatusActive marks a workflow version as publishable once storage lands.
	WorkflowStatusActive = "active"
	// WorkflowStatusInactive marks a workflow version as disabled.
	WorkflowStatusInactive = "inactive"
)

// WorkflowRecord is the scaffold state record for a WorkflowCard version.
type WorkflowRecord struct {
	WorkflowID    string        `json:"workflow_id"`
	Version       string        `json:"version"`
	Status        string        `json:"status"`
	AuthorAddress string        `json:"author_address"`
	Card          *WorkflowCard `json:"card,omitempty"`
	CreatedHeight int64         `json:"created_height"`
	UpdatedHeight int64         `json:"updated_height"`
}

// AuthorBondRecord tracks workflow-author bond state for future keeper logic.
type AuthorBondRecord struct {
	AuthorAddress string    `json:"author_address"`
	Bond          *sdk.Coin `json:"bond,omitempty"`
	Slashed       *sdk.Coin `json:"slashed,omitempty"`
	LockedFor     []string  `json:"locked_for,omitempty"`
	UpdatedHeight int64     `json:"updated_height"`
}

// GenesisState defines the workflows module's genesis state.
type GenesisState struct {
	Params       *Params              `json:"params"`
	Workflows    []*WorkflowRecord    `json:"workflows,omitempty"`
	AuthorBonds  []*AuthorBondRecord  `json:"author_bonds,omitempty"`
	BundleQuotes []*BundleQuoteRecord `json:"bundle_quotes,omitempty"`
}

type workflowGenesisEntry struct {
	author string
	status string
}

// DefaultGenesis returns the default workflows genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:       DefaultParams(),
		Workflows:    []*WorkflowRecord{},
		AuthorBonds:  []*AuthorBondRecord{},
		BundleQuotes: []*BundleQuoteRecord{},
	}
}

// Validate performs basic genesis-state validation.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if gs.Params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	seenWorkflows := make(map[string]workflowGenesisEntry, len(gs.Workflows))
	for i, workflow := range gs.Workflows {
		if workflow == nil {
			return fmt.Errorf("workflow entry %d cannot be nil", i)
		}
		key, err := WorkflowKey(workflow.WorkflowID, workflow.Version)
		if err != nil {
			return fmt.Errorf("workflow entry %d: %w", i, err)
		}
		if _, ok := seenWorkflows[key]; ok {
			return fmt.Errorf("duplicate workflow version: %s", key)
		}
		author := strings.TrimSpace(workflow.AuthorAddress)
		if author == "" {
			return fmt.Errorf("workflow %s missing author_address", key)
		}
		if author != workflow.AuthorAddress {
			return fmt.Errorf("workflow %s author_address must be canonical: %q", key, workflow.AuthorAddress)
		}
		status := strings.TrimSpace(workflow.Status)
		if status != workflow.Status {
			return fmt.Errorf("workflow %s status must be canonical: %q", key, workflow.Status)
		}
		if status != WorkflowStatusActive && status != WorkflowStatusInactive {
			return fmt.Errorf("workflow %s has invalid status %q", key, workflow.Status)
		}
		seenWorkflows[key] = workflowGenesisEntry{author: author, status: status}
		if workflow.Card != nil {
			cardKey, err := WorkflowKey(workflow.Card.WorkflowId, workflow.Card.Version)
			if err != nil {
				return fmt.Errorf("workflow %s card key: %w", key, err)
			}
			if cardKey != key {
				return fmt.Errorf("workflow %s card identity mismatch: %s", key, cardKey)
			}
			if err := StaticCheckWorkflowCard(workflow.Card); err != nil {
				return fmt.Errorf("workflow %s card static validation: %w", key, err)
			}
			if err := validateWorkflowCardTimestamps(key, workflow.Card); err != nil {
				return err
			}
		}
	}

	seenAuthors := make(map[string]struct{}, len(gs.AuthorBonds))
	lockedWorkflows := make(map[string]string, len(gs.AuthorBonds))
	for i, bond := range gs.AuthorBonds {
		if bond == nil {
			return fmt.Errorf("author bond entry %d cannot be nil", i)
		}
		author := strings.TrimSpace(bond.AuthorAddress)
		if author == "" {
			return fmt.Errorf("author bond entry %d missing author_address", i)
		}
		if author != bond.AuthorAddress {
			return fmt.Errorf("author bond entry %d author_address must be canonical: %q", i, bond.AuthorAddress)
		}
		if _, ok := seenAuthors[author]; ok {
			return fmt.Errorf("duplicate author bond: %s", author)
		}
		seenAuthors[author] = struct{}{}
		if err := validateWorkflowGenesisCoin(fmt.Sprintf("author bond %s", author), bond.Bond, true, true); err != nil {
			return err
		}
		if err := validateWorkflowGenesisCoin(fmt.Sprintf("author bond %s slashed", author), bond.Slashed, false, false); err != nil {
			return err
		}
		seenLocks := make(map[string]struct{}, len(bond.LockedFor))
		for _, lockedFor := range bond.LockedFor {
			key := strings.TrimSpace(lockedFor)
			if key == "" {
				return fmt.Errorf("author bond %s has empty locked_for entry", author)
			}
			if key != lockedFor {
				return fmt.Errorf("author bond %s locked_for entry must be canonical: %q", author, lockedFor)
			}
			if _, ok := seenLocks[key]; ok {
				return fmt.Errorf("author bond %s duplicate locked_for entry: %s", author, key)
			}
			workflow, ok := seenWorkflows[key]
			if !ok {
				return fmt.Errorf("author bond %s locked_for references unknown workflow: %s", author, key)
			}
			if workflow.author != author {
				return fmt.Errorf("author bond %s locked_for references workflow %s owned by %s", author, key, workflow.author)
			}
			if workflow.status != WorkflowStatusActive {
				return fmt.Errorf("author bond %s locked_for references inactive workflow: %s", author, key)
			}
			seenLocks[key] = struct{}{}
			lockedWorkflows[key] = author
		}
	}
	for key, workflow := range seenWorkflows {
		if workflow.status != WorkflowStatusActive {
			continue
		}
		if lockedWorkflows[key] != workflow.author {
			return fmt.Errorf("active workflow %s missing author bond lock for %s", key, workflow.author)
		}
	}
	seenQuotes := make(map[string]struct{}, len(gs.BundleQuotes))
	for i, quote := range gs.BundleQuotes {
		if quote == nil {
			return fmt.Errorf("bundle quote entry %d cannot be nil", i)
		}
		if err := quote.Validate(); err != nil {
			return fmt.Errorf("bundle quote entry %d: %w", i, err)
		}
		if _, ok := seenQuotes[quote.BundleID]; ok {
			return fmt.Errorf("duplicate bundle quote: %s", quote.BundleID)
		}
		seenQuotes[quote.BundleID] = struct{}{}
	}
	return nil
}

func validateWorkflowCardTimestamps(key string, card *WorkflowCard) error {
	if card == nil {
		return nil
	}
	if err := validateWorkflowGenesisTimestamp(fmt.Sprintf("workflow %s card.created_at", key), card.GetCreatedAt()); err != nil {
		return err
	}
	if err := validateWorkflowGenesisTimestamp(fmt.Sprintf("workflow %s card.updated_at", key), card.GetUpdatedAt()); err != nil {
		return err
	}
	return nil
}

func validateWorkflowGenesisTimestamp(field string, ts time.Time) error {
	// A value time.Time (gogoproto stdtime) is always valid; the zero value is
	// treated as "unset" and accepted.
	_ = field
	return nil
}

func validateWorkflowGenesisCoin(field string, coin *sdk.Coin, required bool, positive bool) error {
	if coin == nil {
		if required {
			return fmt.Errorf("%s missing coin", field)
		}
		return nil
	}
	denom := strings.TrimSpace(coin.Denom)
	if denom == "" {
		return fmt.Errorf("%s missing denom", field)
	}
	if denom != coin.Denom {
		return fmt.Errorf("%s denom must be canonical: %q", field, coin.Denom)
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return fmt.Errorf("%s denom is invalid: %w", field, err)
	}
	if coin.Amount.IsNil() {
		return fmt.Errorf("%s missing amount", field)
	}
	if coin.Amount.IsNegative() {
		return fmt.Errorf("%s amount cannot be negative", field)
	}
	if positive && coin.Amount.IsZero() {
		return fmt.Errorf("%s amount must be positive", field)
	}
	return nil
}

// WorkflowKey returns the canonical collections key for a workflow version.
func WorkflowKey(workflowID, version string) (string, error) {
	workflowID, err := canonicalWorkflowKeyPart("workflow_id", workflowID)
	if err != nil {
		return "", err
	}
	version, err = canonicalWorkflowKeyPart("version", version)
	if err != nil {
		return "", err
	}
	return workflowID + "/" + version, nil
}

func canonicalWorkflowKeyPart(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ErrInvalidWorkflow.Wrapf("%s cannot be empty", field)
	}
	if trimmed != value {
		return "", ErrInvalidWorkflow.Wrapf("%s must be canonical: %q", field, value)
	}
	if strings.Contains(value, "/") {
		return "", ErrInvalidWorkflow.Wrapf("%s cannot contain /: %q", field, value)
	}
	return value, nil
}
