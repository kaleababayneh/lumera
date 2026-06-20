
package types

import (
	"errors"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
)

// findAttr returns the value of the first attribute whose key matches,
// or ("", false) if absent. Attribute order is significant in the
// builders so tests also assert positional shape separately; this
// helper is for value-only lookups.
func findAttr(attrs []sdk.Attribute, key string) (string, bool) {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value, true
		}
	}
	return "", false
}

func TestBuildRestitutionPlannedAttrs_Shape(t *testing.T) {
	amount := decimal.NewFromInt(1000)
	instructions := []RestitutionRoutingInstruction{
		{Kind: RestitutionDestinationUsers, Amount: decimal.NewFromInt(600), DestRef: "cosmos1users"},
		{Kind: RestitutionDestinationInsurance, Amount: decimal.NewFromInt(250), DestRef: "insurance"},
		{Kind: RestitutionDestinationTreasury, Amount: decimal.NewFromInt(100), DestRef: "treasury"},
		{Kind: RestitutionDestinationBurn, Amount: decimal.NewFromInt(50), DestRef: ""},
	}
	attrs := BuildRestitutionPlannedAttrs(amount, instructions)

	if len(attrs) != 2 {
		t.Fatalf("planned attrs len=%d; want 2", len(attrs))
	}
	if attrs[0].Key != AttributeKeyRestitutionTotal {
		t.Errorf("attrs[0].Key=%q; want %q", attrs[0].Key, AttributeKeyRestitutionTotal)
	}
	if attrs[0].Value != "1000" {
		t.Errorf("attrs[0].Value=%q; want %q", attrs[0].Value, "1000")
	}
	if attrs[1].Key != "restitution_leg_count" {
		t.Errorf("attrs[1].Key=%q; want %q", attrs[1].Key, "restitution_leg_count")
	}
	if attrs[1].Value != "4" {
		t.Errorf("attrs[1].Value=%q; want %q", attrs[1].Value, "4")
	}
}

func TestBuildRestitutionPlannedAttrs_EmptyInstructions(t *testing.T) {
	attrs := BuildRestitutionPlannedAttrs(decimal.Zero, nil)
	if len(attrs) != 2 {
		t.Fatalf("planned attrs len=%d; want 2", len(attrs))
	}
	if v, _ := findAttr(attrs, AttributeKeyRestitutionTotal); v != "0" {
		t.Errorf("total attr=%q; want %q", v, "0")
	}
	if v, _ := findAttr(attrs, "restitution_leg_count"); v != "0" {
		t.Errorf("leg_count attr=%q; want %q", v, "0")
	}
}

func TestBuildRestitutionPlannedAttrs_DecimalPreservation(t *testing.T) {
	// Non-integer decimals should round-trip through Decimal.String()
	// without scientific notation or rounding. Receipt consumers
	// parse these attribute strings back into Decimals, so any
	// formatter drift (e.g., switching to StringFixed) would silently
	// change parsed values.
	amount := decimal.RequireFromString("12345.6789")
	attrs := BuildRestitutionPlannedAttrs(amount, nil)
	if v, _ := findAttr(attrs, AttributeKeyRestitutionTotal); v != "12345.6789" {
		t.Errorf("total attr=%q; want %q", v, "12345.6789")
	}
}

func TestBuildRestitutionRoutedAttrs_Shape(t *testing.T) {
	instruction := RestitutionRoutingInstruction{
		Kind:    RestitutionDestinationUsers,
		Amount:  decimal.NewFromInt(600),
		DestRef: "cosmos1userpool",
	}
	attrs := BuildRestitutionRoutedAttrs(instruction)

	if len(attrs) != 3 {
		t.Fatalf("routed attrs len=%d; want 3", len(attrs))
	}
	// Position-0 = dest_kind
	if attrs[0].Key != AttributeKeyRestitutionDestKind || attrs[0].Value != "users" {
		t.Errorf("attrs[0]=(%q,%q); want (%q,%q)",
			attrs[0].Key, attrs[0].Value, AttributeKeyRestitutionDestKind, "users")
	}
	// Position-1 = dest_ref
	if attrs[1].Key != AttributeKeyRestitutionDestRef || attrs[1].Value != "cosmos1userpool" {
		t.Errorf("attrs[1]=(%q,%q); want (%q,%q)",
			attrs[1].Key, attrs[1].Value, AttributeKeyRestitutionDestRef, "cosmos1userpool")
	}
	// Position-2 = leg_amount
	if attrs[2].Key != AttributeKeyRestitutionLegAmount || attrs[2].Value != "600" {
		t.Errorf("attrs[2]=(%q,%q); want (%q,%q)",
			attrs[2].Key, attrs[2].Value, AttributeKeyRestitutionLegAmount, "600")
	}
}

func TestBuildRestitutionRoutedAttrs_BurnLegEmitsEmptyDestRef(t *testing.T) {
	// Regression pin: burn leg has DestRef="" by convention; the
	// builder MUST still emit a dest_ref attribute with empty value,
	// not omit it. Consumers keyed by attribute position (or by the
	// invariant "all routed events carry dest_ref") would misalign
	// otherwise.
	instruction := RestitutionRoutingInstruction{
		Kind:    RestitutionDestinationBurn,
		Amount:  decimal.NewFromInt(50),
		DestRef: "",
	}
	attrs := BuildRestitutionRoutedAttrs(instruction)

	if len(attrs) != 3 {
		t.Fatalf("routed attrs len=%d; want 3", len(attrs))
	}
	v, ok := findAttr(attrs, AttributeKeyRestitutionDestRef)
	if !ok {
		t.Fatalf("dest_ref attribute MUST be present even for burn leg")
	}
	if v != "" {
		t.Errorf("burn dest_ref=%q; want empty string", v)
	}
}

func TestBuildRestitutionRoutedAttrs_AllKindsStringified(t *testing.T) {
	cases := []struct {
		kind     RestitutionDestinationKind
		wantKind string
	}{
		{RestitutionDestinationUsers, "users"},
		{RestitutionDestinationInsurance, "insurance"},
		{RestitutionDestinationTreasury, "treasury"},
		{RestitutionDestinationBurn, "burn"},
	}
	for _, tc := range cases {
		t.Run(tc.wantKind, func(t *testing.T) {
			instruction := RestitutionRoutingInstruction{
				Kind:    tc.kind,
				Amount:  decimal.NewFromInt(1),
				DestRef: "ref-" + tc.wantKind,
			}
			attrs := BuildRestitutionRoutedAttrs(instruction)
			if v, _ := findAttr(attrs, AttributeKeyRestitutionDestKind); v != tc.wantKind {
				t.Errorf("dest_kind=%q; want %q", v, tc.wantKind)
			}
		})
	}
}

func TestBuildRestitutionFailedAttrs_Shape(t *testing.T) {
	instruction := RestitutionRoutingInstruction{
		Kind:    RestitutionDestinationTreasury,
		Amount:  decimal.NewFromInt(100),
		DestRef: "treasury",
	}
	cause := errors.New("insufficient funds in module account")
	attrs := BuildRestitutionFailedAttrs(instruction, cause)

	if len(attrs) != 4 {
		t.Fatalf("failed attrs len=%d; want 4", len(attrs))
	}
	if attrs[0].Key != AttributeKeyRestitutionDestKind || attrs[0].Value != "treasury" {
		t.Errorf("attrs[0]=(%q,%q); want (dest_kind,treasury)", attrs[0].Key, attrs[0].Value)
	}
	if attrs[1].Key != AttributeKeyRestitutionDestRef || attrs[1].Value != "treasury" {
		t.Errorf("attrs[1]=(%q,%q); want (dest_ref,treasury)", attrs[1].Key, attrs[1].Value)
	}
	if attrs[2].Key != AttributeKeyRestitutionLegAmount || attrs[2].Value != "100" {
		t.Errorf("attrs[2]=(%q,%q); want (leg_amount,100)", attrs[2].Key, attrs[2].Value)
	}
	if attrs[3].Key != AttributeKeyRestitutionError || attrs[3].Value != "insufficient funds in module account" {
		t.Errorf("attrs[3]=(%q,%q); want (error,insufficient funds in module account)",
			attrs[3].Key, attrs[3].Value)
	}
}

func TestBuildRestitutionFailedAttrs_NilCauseEmitsEmptyError(t *testing.T) {
	// A nil cause is accepted; the error attribute is present with
	// empty value. This lets keeper call sites defensively build the
	// attribute list without a nil-check branch at each site.
	instruction := RestitutionRoutingInstruction{
		Kind:    RestitutionDestinationUsers,
		Amount:  decimal.NewFromInt(1),
		DestRef: "cosmos1x",
	}
	attrs := BuildRestitutionFailedAttrs(instruction, nil)

	if len(attrs) != 4 {
		t.Fatalf("failed attrs len=%d; want 4", len(attrs))
	}
	if v, ok := findAttr(attrs, AttributeKeyRestitutionError); !ok || v != "" {
		t.Errorf("error attr: ok=%v value=%q; want ok=true value=\"\"", ok, v)
	}
}

func TestBuildRestitutionFailedAttrs_WrappedErrorUnwrapsCleanly(t *testing.T) {
	// Regression pin: the keeper will pass wrapped errors via
	// fmt.Errorf("bank op: %w", inner). The attribute value should
	// reflect the full wrapped message, preserving the inner text
	// that operators grep for in event logs.
	inner := errors.New("denom invalid")
	wrapped := fmt.Errorf("restitution users leg: %w", inner)
	instruction := RestitutionRoutingInstruction{
		Kind:    RestitutionDestinationUsers,
		Amount:  decimal.NewFromInt(600),
		DestRef: "cosmos1users",
	}
	attrs := BuildRestitutionFailedAttrs(instruction, wrapped)
	v, _ := findAttr(attrs, AttributeKeyRestitutionError)
	want := "restitution users leg: denom invalid"
	if v != want {
		t.Errorf("error attr=%q; want %q", v, want)
	}
}

// FuzzBuildRestitutionAttrs_Shapes applies the testing-fuzzing workflow to
// the narrow event-attribute boundary. These builders feed operator logs and
// indexers, so the oracle is structural rather than semantic: fixed attribute
// count, fixed key order, deterministic kind stringification, exact decimal
// string preservation, explicit burn dest_ref, and nil/error message handling.
func FuzzBuildRestitutionAttrs_Shapes(f *testing.F) {
	f.Add(int64(0), int64(0), "users-pool", "cause")
	f.Add(int64(10_000), int64(4), "", "")
	f.Add(int64(-7), int64(99), "  spaced-ref  ", "bank op: denom invalid")

	f.Fuzz(func(t *testing.T, rawAmount int64, rawKind int64, destRef string, causeMsg string) {
		if len(destRef) > 256 || len(causeMsg) > 256 {
			t.Skip()
		}

		amount := decimal.NewFromInt(rawAmount)
		kind := restitutionKindFromFuzz(rawKind)
		instruction := RestitutionRoutingInstruction{
			Kind:    kind,
			Amount:  amount,
			DestRef: destRef,
		}

		plannedCount := int(rawKind % 8)
		if plannedCount < 0 {
			plannedCount = -plannedCount
		}
		instructions := make([]RestitutionRoutingInstruction, plannedCount)
		plannedAttrs := BuildRestitutionPlannedAttrs(amount, instructions)
		if len(plannedAttrs) != 2 {
			t.Fatalf("planned attr len=%d; want 2", len(plannedAttrs))
		}
		if plannedAttrs[0].Key != AttributeKeyRestitutionTotal {
			t.Fatalf("planned[0].Key=%q; want %q", plannedAttrs[0].Key, AttributeKeyRestitutionTotal)
		}
		if plannedAttrs[0].Value != amount.String() {
			t.Fatalf("planned total=%q; want %q", plannedAttrs[0].Value, amount.String())
		}
		if plannedAttrs[1].Key != "restitution_leg_count" {
			t.Fatalf("planned[1].Key=%q; want restitution_leg_count", plannedAttrs[1].Key)
		}
		if plannedAttrs[1].Value != decimal.NewFromInt(int64(plannedCount)).String() {
			t.Fatalf("planned leg_count=%q; want %d", plannedAttrs[1].Value, plannedCount)
		}

		routedAttrs := BuildRestitutionRoutedAttrs(instruction)
		assertRestitutionInstructionAttrs(t, routedAttrs, instruction, false, "")

		var cause error
		if causeMsg != "" {
			cause = errors.New(causeMsg)
		}
		failedAttrs := BuildRestitutionFailedAttrs(instruction, cause)
		wantErr := ""
		if cause != nil {
			wantErr = cause.Error()
		}
		assertRestitutionInstructionAttrs(t, failedAttrs, instruction, true, wantErr)
	})
}

func restitutionKindFromFuzz(raw int64) RestitutionDestinationKind {
	switch raw % 6 {
	case 0:
		return RestitutionDestinationUnspecified
	case 1:
		return RestitutionDestinationUsers
	case 2:
		return RestitutionDestinationInsurance
	case 3:
		return RestitutionDestinationTreasury
	case 4:
		return RestitutionDestinationBurn
	default:
		return RestitutionDestinationKind(raw)
	}
}

func assertRestitutionInstructionAttrs(
	t *testing.T,
	attrs []sdk.Attribute,
	instruction RestitutionRoutingInstruction,
	withError bool,
	wantErr string,
) {
	t.Helper()
	wantLen := 3
	if withError {
		wantLen = 4
	}
	if len(attrs) != wantLen {
		t.Fatalf("attrs len=%d; want %d", len(attrs), wantLen)
	}
	if attrs[0].Key != AttributeKeyRestitutionDestKind {
		t.Fatalf("attrs[0].Key=%q; want %q", attrs[0].Key, AttributeKeyRestitutionDestKind)
	}
	if attrs[0].Value != instruction.Kind.String() {
		t.Fatalf("dest kind=%q; want %q", attrs[0].Value, instruction.Kind.String())
	}
	if attrs[1].Key != AttributeKeyRestitutionDestRef {
		t.Fatalf("attrs[1].Key=%q; want %q", attrs[1].Key, AttributeKeyRestitutionDestRef)
	}
	if attrs[1].Value != instruction.DestRef {
		t.Fatalf("dest ref=%q; want %q", attrs[1].Value, instruction.DestRef)
	}
	if instruction.Kind == RestitutionDestinationBurn && attrs[1].Value != "" {
		t.Fatalf("burn dest_ref=%q; want explicit empty string", attrs[1].Value)
	}
	if attrs[2].Key != AttributeKeyRestitutionLegAmount {
		t.Fatalf("attrs[2].Key=%q; want %q", attrs[2].Key, AttributeKeyRestitutionLegAmount)
	}
	if attrs[2].Value != instruction.Amount.String() {
		t.Fatalf("leg amount=%q; want %q", attrs[2].Value, instruction.Amount.String())
	}
	if !withError {
		return
	}
	if attrs[3].Key != AttributeKeyRestitutionError {
		t.Fatalf("attrs[3].Key=%q; want %q", attrs[3].Key, AttributeKeyRestitutionError)
	}
	if attrs[3].Value != wantErr {
		t.Fatalf("error attr=%q; want %q", attrs[3].Value, wantErr)
	}
}
