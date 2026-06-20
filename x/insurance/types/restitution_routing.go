package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Restitution routing planner for bead lumera_ai-tvanr "Slash
// restitution routing not implemented per spec".
//
// ComputeRestitutionSplit (restitution.go) divides a slashed amount
// into the four spec-defined shares (users / insurance / treasury /
// burn) per specs/governance/slashing-rules.md §"Restitution
// Routing". This file layers the ROUTING step on top:
//
//   ComputeRestitutionSplit  →  RestitutionSplit{Users, Insurance, Treasury, Burn}
//   BuildRoutingInstructions →  []Instruction{destination_kind, amount, destination_ref}
//
// A keeper-level executor (follow-up scope) takes the planned
// instructions and issues the actual bank operations:
//
//   - Users leg → module→account sends to the user-protection pool
//     address specified in destinations.
//   - Insurance leg → module→module send to the insurance-reserve
//     module account.
//   - Treasury leg → module→module send to the governance-treasury
//     module account.
//   - Burn leg → bank.BurnCoins, immutable.
//
// Splitting the planner from the executor keeps the split math
// provably correct at the type layer (already covered by the
// restitution.go fuzz + metamorphic tests) while letting the bank
// wiring evolve independently — a follow-up that adds BurnCoins to
// the expected BankKeeper interface only needs to plumb the plan
// through, not re-derive any math.

// RestitutionDestinationKind enumerates the four legs of restitution
// routing. The String form is stable and used in events and logs so
// operators can correlate a burn-leg log line to the same event's
// users-leg log line.
type RestitutionDestinationKind int32

const (
	// RestitutionDestinationUnspecified is the zero value and is
	// never valid for a planned instruction.
	RestitutionDestinationUnspecified RestitutionDestinationKind = 0
	// RestitutionDestinationUsers routes slashed value to the
	// user-protection pool for instant insurance credit.
	RestitutionDestinationUsers RestitutionDestinationKind = 1
	// RestitutionDestinationInsurance replenishes the insurance
	// reserve module account.
	RestitutionDestinationInsurance RestitutionDestinationKind = 2
	// RestitutionDestinationTreasury routes to the governance
	// treasury module account.
	RestitutionDestinationTreasury RestitutionDestinationKind = 3
	// RestitutionDestinationBurn marks the burn leg. The executor
	// calls bank.BurnCoins rather than performing a send.
	RestitutionDestinationBurn RestitutionDestinationKind = 4
)

// String returns the canonical lowercase form used in events and logs.
func (k RestitutionDestinationKind) String() string {
	switch k {
	case RestitutionDestinationUnspecified:
		return "unspecified"
	case RestitutionDestinationUsers:
		return "users"
	case RestitutionDestinationInsurance:
		return "insurance"
	case RestitutionDestinationTreasury:
		return "treasury"
	case RestitutionDestinationBurn:
		return "burn"
	default:
		return fmt.Sprintf("restitution_dest_unknown_%d", int32(k))
	}
}

// IsValid reports whether the kind is one of the four defined legs.
func (k RestitutionDestinationKind) IsValid() bool {
	switch k {
	case RestitutionDestinationUsers,
		RestitutionDestinationInsurance,
		RestitutionDestinationTreasury,
		RestitutionDestinationBurn:
		return true
	default:
		return false
	}
}

// RestitutionDestinations carries the concrete routing targets for a
// slashing event. Each non-burn leg needs a destination reference
// the keeper's bank wiring can resolve to a module account or bech32
// address; the burn leg has no destination (it's a BurnCoins call).
//
// UsersPoolAddr is a bech32 account address (the user-protection
// pool is NOT a module account — it's a governance-controlled wallet
// per the spec, so governance can audit routes).
//
// InsuranceModule and TreasuryModule are module-account names that
// the keeper resolves via AccountKeeper.GetModuleAddress.
type RestitutionDestinations struct {
	UsersPoolAddr    string
	InsuranceModule  string
	TreasuryModule   string
}

// Validate enforces that all three destinations the planner emits
// instructions for are non-empty. Burn has no destination reference
// so it's not checked here.
func (d RestitutionDestinations) Validate() error {
	if strings.TrimSpace(d.UsersPoolAddr) == "" {
		return errors.New("restitution destinations: users_pool_addr is required")
	}
	if strings.TrimSpace(d.InsuranceModule) == "" {
		return errors.New("restitution destinations: insurance_module is required")
	}
	if strings.TrimSpace(d.TreasuryModule) == "" {
		return errors.New("restitution destinations: treasury_module is required")
	}
	return nil
}

// RestitutionRoutingInstruction describes a single bank operation
// the executor must perform. Amount is in the same unit as the
// ComputeRestitutionSplit input (typically micro-LAC integer Decimals
// produced by the slashing pipeline).
type RestitutionRoutingInstruction struct {
	// Kind identifies which leg this instruction represents.
	Kind RestitutionDestinationKind
	// Amount is the Decimal amount to send or burn. Always
	// non-negative. Callers that need integer precision should
	// ensure upstream amounts are integer-valued Decimals; the
	// planner preserves bit-width (no rounding beyond what
	// ComputeRestitutionSplit already did).
	Amount decimal.Decimal
	// DestRef is the bech32 address for Users, the module-account
	// name for Insurance and Treasury, or the empty string for Burn.
	DestRef string
}

// BuildRestitutionRoutingInstructions turns a computed split plus
// concrete destinations into the ordered list of bank instructions
// the executor will issue. Instructions are returned in a stable
// order (Users, Insurance, Treasury, Burn) so event-stream consumers
// can rely on the per-slashing ordering.
//
// Zero-amount legs are omitted from the returned slice — a burn of
// zero is a no-op, and emitting a zero-amount send triggers bank
// errors on some cosmos-sdk versions. This keeps the instruction
// list lean and avoids spurious bank calls.
//
// Returns an error if ComputeRestitutionSplit rejects the amount, if
// the split's Total() does not equal the input exactly (paranoid
// round-trip check — the split math is covered by its own fuzz but
// a regression there would be catastrophic here, so the planner
// re-verifies), or if any destination leg is mis-configured.
func BuildRestitutionRoutingInstructions(
	amount decimal.Decimal,
	destinations RestitutionDestinations,
) ([]RestitutionRoutingInstruction, error) {
	if err := destinations.Validate(); err != nil {
		return nil, err
	}
	split, err := ComputeRestitutionSplit(amount)
	if err != nil {
		return nil, err
	}
	// Round-trip paranoia check: the split math is the authoritative
	// value-preservation guarantee; if a future refactor broke it,
	// the planner would silently route less value than was slashed.
	// This is a cheap cross-check to make that impossible.
	if !split.Total().Equal(amount) {
		return nil, fmt.Errorf(
			"restitution routing: split total %s != input %s (value-preservation broken)",
			split.Total().String(), amount.String(),
		)
	}

	instructions := make([]RestitutionRoutingInstruction, 0, 4)
	if split.Users.IsPositive() {
		instructions = append(instructions, RestitutionRoutingInstruction{
			Kind:    RestitutionDestinationUsers,
			Amount:  split.Users,
			DestRef: strings.TrimSpace(destinations.UsersPoolAddr),
		})
	}
	if split.Insurance.IsPositive() {
		instructions = append(instructions, RestitutionRoutingInstruction{
			Kind:    RestitutionDestinationInsurance,
			Amount:  split.Insurance,
			DestRef: strings.TrimSpace(destinations.InsuranceModule),
		})
	}
	if split.Treasury.IsPositive() {
		instructions = append(instructions, RestitutionRoutingInstruction{
			Kind:    RestitutionDestinationTreasury,
			Amount:  split.Treasury,
			DestRef: strings.TrimSpace(destinations.TreasuryModule),
		})
	}
	if split.Burn.IsPositive() {
		instructions = append(instructions, RestitutionRoutingInstruction{
			Kind:   RestitutionDestinationBurn,
			Amount: split.Burn,
			// DestRef deliberately empty for burn — the executor
			// dispatches on Kind, not on address.
		})
	}
	return instructions, nil
}
