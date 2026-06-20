
package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
)

// Event-attribute builders for slash-restitution routing
// (lumera_ai-tvanr). The planner in restitution_routing.go produces a
// []RestitutionRoutingInstruction; these builders turn each routing
// step into a []sdk.Attribute with a stable shape so the keeper
// can emit events consistently and unit tests can pin the event
// contract independently of keeper wiring.
//
// Attribute-key authority lives in events.go (AttributeKeyRestitution*
// constants). Keeping the attribute shape centralized here means a
// later refactor of event field names touches one file; without a
// centralized builder, the keeper would re-spell attribute keys in
// multiple emission sites and drift is inevitable.

// BuildRestitutionPlannedAttrs returns the attribute list for a
// single EventTypeRestitutionPlanned event, emitted once per slashing
// before any bank operation runs.
//
// Shape:
//   restitution_total     = <total amount>
//   restitution_leg_count = <number of non-zero legs>
//
// The per-leg amounts are NOT encoded here — the planner's leg list
// is summarized by EventTypeRestitutionRouted (one per non-zero leg)
// after each bank op completes. Encoding each leg amount on the
// planned event would duplicate data that the routed events already
// carry and bloat tx logs without adding information.
//
// Callers that need per-leg planned amounts on the planned event
// (e.g., a dry-run mode that plans without executing) can append
// extra attributes; this builder returns the canonical core and
// never panics.
func BuildRestitutionPlannedAttrs(total decimal.Decimal, instructions []RestitutionRoutingInstruction) []sdk.Attribute {
	return []sdk.Attribute{
		sdk.NewAttribute(AttributeKeyRestitutionTotal, total.String()),
		// leg_count doubles as a cross-check: auditors reconciling
		// planned→routed events can verify they received exactly
		// leg_count routed events for this slashing.
		sdk.NewAttribute("restitution_leg_count", decimal.NewFromInt(int64(len(instructions))).String()),
	}
}

// BuildRestitutionRoutedAttrs returns the attribute list for one
// EventTypeRestitutionRouted event, emitted after a single leg's
// bank operation completes successfully.
//
// Shape (three attributes, fixed order):
//   restitution_dest_kind  = "users" | "insurance" | "treasury" | "burn"
//   restitution_dest_ref   = bech32 address, module name, or "" for burn
//   restitution_leg_amount = <decimal string>
//
// DestRef is emitted as an explicit empty-string attribute for the
// burn leg rather than being omitted — downstream consumers keyed by
// attribute position should always see a dest_ref attribute, even if
// empty, so they don't misalign against other event types.
func BuildRestitutionRoutedAttrs(instruction RestitutionRoutingInstruction) []sdk.Attribute {
	return []sdk.Attribute{
		sdk.NewAttribute(AttributeKeyRestitutionDestKind, instruction.Kind.String()),
		sdk.NewAttribute(AttributeKeyRestitutionDestRef, instruction.DestRef),
		sdk.NewAttribute(AttributeKeyRestitutionLegAmount, instruction.Amount.String()),
	}
}

// BuildRestitutionFailedAttrs returns the attribute list for an
// EventTypeRestitutionFailed event. Emitted when a leg's bank
// operation errors mid-routing; operators treat this as a
// PagerDuty-grade signal per events.go.
//
// Shape (four attributes, fixed order):
//   restitution_dest_kind  = "users" | "insurance" | "treasury" | "burn"
//   restitution_dest_ref   = bech32 address, module name, or "" for burn
//   restitution_leg_amount = <decimal string>
//   restitution_error      = <err.Error()>, or "" if cause is nil
//
// A nil cause is tolerated (emits empty string) so the keeper can
// build the attribute list defensively without a nil-check branch at
// each call site.
func BuildRestitutionFailedAttrs(instruction RestitutionRoutingInstruction, cause error) []sdk.Attribute {
	errMsg := ""
	if cause != nil {
		errMsg = cause.Error()
	}
	return []sdk.Attribute{
		sdk.NewAttribute(AttributeKeyRestitutionDestKind, instruction.Kind.String()),
		sdk.NewAttribute(AttributeKeyRestitutionDestRef, instruction.DestRef),
		sdk.NewAttribute(AttributeKeyRestitutionLegAmount, instruction.Amount.String()),
		sdk.NewAttribute(AttributeKeyRestitutionError, errMsg),
	}
}
