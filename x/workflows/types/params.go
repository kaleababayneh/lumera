package types

import (
	"fmt"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// DefaultMinAuthorBondAmount is the default minimum workflow-author bond in ulac.
	DefaultMinAuthorBondAmount = "1000000"
	// DefaultWastedWorkBPS funds insurance from author bonds when a bundle reverts after partial work.
	DefaultWastedWorkBPS = uint32(1000)
	// DefaultMaxWorkflowVersions caps retained versions per workflow id.
	DefaultMaxWorkflowVersions = uint32(32)
	// DefaultDisputeWindowSeconds mirrors registry's scaffold-level dispute window default.
	DefaultDisputeWindowSeconds = uint32(86_400)
)

// Params captures governance-tunable workflows module settings.
type Params struct {
	MinAuthorBondAmount  string `json:"min_author_bond_amount"`
	BondDenom            string `json:"bond_denom"`
	WastedWorkBPS        uint32 `json:"wasted_work_bps"`
	MaxWorkflowVersions  uint32 `json:"max_workflow_versions"`
	DisputeWindowSeconds uint32 `json:"dispute_window_seconds"`
}

// DefaultParams returns the canonical default parameter set for x/workflows.
func DefaultParams() *Params {
	return &Params{
		MinAuthorBondAmount:  DefaultMinAuthorBondAmount,
		BondDenom:            "ulac",
		WastedWorkBPS:        DefaultWastedWorkBPS,
		MaxWorkflowVersions:  DefaultMaxWorkflowVersions,
		DisputeWindowSeconds: DefaultDisputeWindowSeconds,
	}
}

// Validate performs stateless sanity checks on governance parameters.
func (p *Params) Validate() error {
	if p == nil {
		return ErrInvalidParams.Wrap("params cannot be nil")
	}
	minAuthorBondAmount := strings.TrimSpace(p.MinAuthorBondAmount)
	if minAuthorBondAmount != p.MinAuthorBondAmount {
		return ErrInvalidParams.Wrap("min_author_bond_amount must not have leading or trailing whitespace")
	}
	amount, ok := math.NewIntFromString(minAuthorBondAmount)
	if !ok {
		return ErrInvalidParams.Wrap("min_author_bond_amount must be an integer")
	}
	if amount.String() != minAuthorBondAmount {
		return ErrInvalidParams.Wrap("min_author_bond_amount must be canonical")
	}
	if amount.IsNegative() {
		return ErrInvalidParams.Wrap("min_author_bond_amount cannot be negative")
	}
	if amount.IsZero() {
		return ErrInvalidParams.Wrap("min_author_bond_amount must be positive")
	}
	bondDenom := strings.TrimSpace(p.BondDenom)
	if bondDenom == "" {
		return ErrInvalidParams.Wrap("bond_denom cannot be empty")
	}
	if bondDenom != p.BondDenom {
		return ErrInvalidParams.Wrap("bond_denom must not have leading or trailing whitespace")
	}
	if err := sdk.ValidateDenom(bondDenom); err != nil {
		return ErrInvalidParams.Wrapf("bond_denom is invalid: %v", err)
	}
	if p.WastedWorkBPS > BPSDenominator {
		return ErrInvalidParams.Wrapf("wasted_work_bps must be <= %d", BPSDenominator)
	}
	if p.MaxWorkflowVersions == 0 {
		return ErrInvalidParams.Wrap("max_workflow_versions must be positive")
	}
	if p.DisputeWindowSeconds == 0 {
		return ErrInvalidParams.Wrap("dispute_window_seconds must be positive")
	}
	return nil
}

// MinAuthorBond renders the configured minimum bond as a Cosmos coin string.
func (p *Params) MinAuthorBond() string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%s%s", strings.TrimSpace(p.MinAuthorBondAmount), strings.TrimSpace(p.BondDenom))
}
