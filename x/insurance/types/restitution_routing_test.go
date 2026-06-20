package types

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func TestRestitutionDestinationKind_String(t *testing.T) {
	cases := []struct {
		kind RestitutionDestinationKind
		want string
	}{
		{RestitutionDestinationUnspecified, "unspecified"},
		{RestitutionDestinationUsers, "users"},
		{RestitutionDestinationInsurance, "insurance"},
		{RestitutionDestinationTreasury, "treasury"},
		{RestitutionDestinationBurn, "burn"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("kind(%d).String() = %q; want %q", int32(tc.kind), got, tc.want)
		}
	}
}

func TestRestitutionDestinationKind_String_UnknownIsDeterministic(t *testing.T) {
	if got := RestitutionDestinationKind(99).String(); got != "restitution_dest_unknown_99" {
		t.Errorf("unknown kind string = %q; want %q", got, "restitution_dest_unknown_99")
	}
}

func TestRestitutionDestinationKind_IsValid(t *testing.T) {
	cases := []struct {
		kind RestitutionDestinationKind
		want bool
	}{
		{RestitutionDestinationUnspecified, false},
		{RestitutionDestinationUsers, true},
		{RestitutionDestinationInsurance, true},
		{RestitutionDestinationTreasury, true},
		{RestitutionDestinationBurn, true},
		{RestitutionDestinationKind(99), false},
		{RestitutionDestinationKind(-1), false},
	}
	for _, tc := range cases {
		if got := tc.kind.IsValid(); got != tc.want {
			t.Errorf("kind(%d).IsValid() = %v; want %v", int32(tc.kind), got, tc.want)
		}
	}
}

func validDestinations() RestitutionDestinations {
	return RestitutionDestinations{
		UsersPoolAddr:   "lumera1usersPool0000000000000000000000000",
		InsuranceModule: "insurance_reserve",
		TreasuryModule:  "treasury",
	}
}

func TestRestitutionDestinations_Validate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RestitutionDestinations)
		want   string
	}{
		{"empty_users_pool", func(d *RestitutionDestinations) { d.UsersPoolAddr = "" }, "users_pool_addr"},
		{"whitespace_users_pool", func(d *RestitutionDestinations) { d.UsersPoolAddr = "   " }, "users_pool_addr"},
		{"empty_insurance", func(d *RestitutionDestinations) { d.InsuranceModule = "" }, "insurance_module"},
		{"empty_treasury", func(d *RestitutionDestinations) { d.TreasuryModule = "" }, "treasury_module"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := validDestinations()
			tc.mutate(&d)
			err := d.Validate()
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error should mention %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestRestitutionDestinations_Validate_Happy(t *testing.T) {
	if err := validDestinations().Validate(); err != nil {
		t.Fatalf("valid destinations rejected: %v", err)
	}
}

// TestBuildRestitutionRoutingInstructions_FullSplit pins the complete
// four-leg instruction list for a large amount that produces positive
// shares in every leg. Validates instruction order (Users, Insurance,
// Treasury, Burn) and the kind↔amount↔destref mapping for each leg.
func TestBuildRestitutionRoutingInstructions_FullSplit(t *testing.T) {
	// 10_000 ulac → exact split: users=6000, insurance=2500, treasury=1000, burn=500.
	amount := decimal.NewFromInt(10_000)
	dests := validDestinations()

	instructions, err := BuildRestitutionRoutingInstructions(amount, dests)
	if err != nil {
		t.Fatalf("BuildRestitutionRoutingInstructions: %v", err)
	}

	if len(instructions) != 4 {
		t.Fatalf("expected 4 instructions, got %d: %+v", len(instructions), instructions)
	}

	want := []RestitutionRoutingInstruction{
		{Kind: RestitutionDestinationUsers, Amount: decimal.NewFromInt(6000), DestRef: dests.UsersPoolAddr},
		{Kind: RestitutionDestinationInsurance, Amount: decimal.NewFromInt(2500), DestRef: dests.InsuranceModule},
		{Kind: RestitutionDestinationTreasury, Amount: decimal.NewFromInt(1000), DestRef: dests.TreasuryModule},
		{Kind: RestitutionDestinationBurn, Amount: decimal.NewFromInt(500), DestRef: ""},
	}
	for i, w := range want {
		got := instructions[i]
		if got.Kind != w.Kind {
			t.Errorf("instructions[%d].Kind = %s; want %s", i, got.Kind, w.Kind)
		}
		if !got.Amount.Equal(w.Amount) {
			t.Errorf("instructions[%d].Amount = %s; want %s", i, got.Amount, w.Amount)
		}
		if got.DestRef != w.DestRef {
			t.Errorf("instructions[%d].DestRef = %q; want %q", i, got.DestRef, w.DestRef)
		}
	}
}

// TestBuildRestitutionRoutingInstructions_OmitsZeroLegs pins that a
// zero-amount leg is dropped from the instruction list. The bank
// BurnCoins and SendCoins APIs reject zero-amount inputs on some
// cosmos-sdk versions, and emitting a zero-value event would add
// log noise without value.
func TestBuildRestitutionRoutingInstructions_OmitsZeroLegs(t *testing.T) {
	// 7 ulac → users=6, insurance=1, treasury=0 (floor(0.7)),
	// burn=0 (floor(0.35)). Expect only 2 instructions.
	amount := decimal.NewFromInt(7)
	instructions, err := BuildRestitutionRoutingInstructions(amount, validDestinations())
	if err != nil {
		t.Fatalf("BuildRestitutionRoutingInstructions: %v", err)
	}
	if len(instructions) != 2 {
		t.Fatalf("expected 2 instructions (users + insurance), got %d: %+v",
			len(instructions), instructions)
	}
	if instructions[0].Kind != RestitutionDestinationUsers {
		t.Errorf("first instruction kind = %s; want users", instructions[0].Kind)
	}
	if instructions[1].Kind != RestitutionDestinationInsurance {
		t.Errorf("second instruction kind = %s; want insurance", instructions[1].Kind)
	}
}

// TestBuildRestitutionRoutingInstructions_Zero pins that a zero
// input produces no instructions at all — no send, no burn, no
// event. This matches the slashing pipeline's expectation that
// slashing zero amount is a no-op.
func TestBuildRestitutionRoutingInstructions_Zero(t *testing.T) {
	instructions, err := BuildRestitutionRoutingInstructions(decimal.Zero, validDestinations())
	if err != nil {
		t.Fatalf("Zero amount unexpectedly errored: %v", err)
	}
	if len(instructions) != 0 {
		t.Fatalf("expected 0 instructions for zero input, got %d", len(instructions))
	}
}

// TestBuildRestitutionRoutingInstructions_NegativeRejected pins
// that a negative amount flows the ComputeRestitutionSplit error
// through unchanged. Negative slashing is meaningless and would
// otherwise produce a "Bank.Send(negative)" panic downstream.
func TestBuildRestitutionRoutingInstructions_NegativeRejected(t *testing.T) {
	_, err := BuildRestitutionRoutingInstructions(decimal.NewFromInt(-1), validDestinations())
	if err == nil {
		t.Fatal("negative amount should be rejected")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Fatalf("error should mention negative, got: %v", err)
	}
}

// TestBuildRestitutionRoutingInstructions_InvalidDestinationsRejected
// pins that destination validation runs BEFORE the split math. This
// means a mis-configured keeper surfaces an error immediately
// instead of computing a split that can't be routed anywhere.
func TestBuildRestitutionRoutingInstructions_InvalidDestinationsRejected(t *testing.T) {
	dests := validDestinations()
	dests.UsersPoolAddr = ""
	_, err := BuildRestitutionRoutingInstructions(decimal.NewFromInt(100), dests)
	if err == nil {
		t.Fatal("missing destination should be rejected")
	}
	if !strings.Contains(err.Error(), "users_pool_addr") {
		t.Fatalf("error should mention users_pool_addr: %v", err)
	}
}

// TestBuildRestitutionRoutingInstructions_SumEqualsInput pins the
// value-preservation property at the planner layer: summing every
// instruction's Amount must exactly equal the input amount. This
// complements the per-split Total() check ComputeRestitutionSplit
// already enforces, but catches a regression in the planner itself
// that dropped or double-counted a leg.
func TestBuildRestitutionRoutingInstructions_SumEqualsInput(t *testing.T) {
	amounts := []int64{1, 7, 100, 9_999, 10_000, 100_003, 1_000_000}
	for _, n := range amounts {
		amount := decimal.NewFromInt(n)
		instructions, err := BuildRestitutionRoutingInstructions(amount, validDestinations())
		if err != nil {
			t.Fatalf("amount=%d: %v", n, err)
		}
		sum := decimal.Zero
		for _, ins := range instructions {
			sum = sum.Add(ins.Amount)
		}
		if !sum.Equal(amount) {
			t.Errorf("amount=%d: sum of instructions = %s; want %s",
				n, sum.String(), amount.String())
		}
	}
}

// TestBuildRestitutionRoutingInstructions_BurnHasEmptyDestRef pins
// the convention that the burn leg carries no destination ref. The
// executor dispatches on Kind for burn (bank.BurnCoins) rather than
// on DestRef, so leaving DestRef empty makes the Users/Insurance/
// Treasury→DestRef-is-address invariant unambiguous.
func TestBuildRestitutionRoutingInstructions_BurnHasEmptyDestRef(t *testing.T) {
	// Use 10_000 so every leg including burn is positive.
	instructions, err := BuildRestitutionRoutingInstructions(decimal.NewFromInt(10_000), validDestinations())
	if err != nil {
		t.Fatalf("BuildRestitutionRoutingInstructions: %v", err)
	}
	var burnInstr *RestitutionRoutingInstruction
	for i := range instructions {
		if instructions[i].Kind == RestitutionDestinationBurn {
			burnInstr = &instructions[i]
			break
		}
	}
	if burnInstr == nil {
		t.Fatal("burn instruction missing")
	}
	if burnInstr.DestRef != "" {
		t.Errorf("burn instruction DestRef = %q; want empty", burnInstr.DestRef)
	}
}

// FuzzBuildRestitutionRoutingInstructions_Invariants pins the
// planner's structural contract across arbitrary non-negative
// int64 amounts:
//
//  1. Crash-freedom.
//  2. Sum-preservation: sum of instruction amounts equals input.
//  3. No instruction has a negative or zero amount.
//  4. Burn amount never exceeds 5% of the input (the spec-mandated
//     immutable cap). This reads the split through the planner's
//     output, so a regression that widened the burn leg (or failed
//     to omit a zero burn) would trip here even if the underlying
//     ComputeRestitutionSplit invariants still held.
//  5. Burn instruction DestRef is always empty.
//  6. Non-burn instruction DestRef is always non-empty.
func FuzzBuildRestitutionRoutingInstructions_Invariants(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(1))
	f.Add(int64(7))
	f.Add(int64(100))
	f.Add(int64(10_000))
	f.Add(int64(1_000_003))

	f.Fuzz(func(t *testing.T, n int64) {
		if n < 0 {
			t.Skip()
		}
		amount := decimal.NewFromInt(n)

		instructions, err := BuildRestitutionRoutingInstructions(amount, validDestinations())
		if err != nil {
			t.Fatalf("unexpected error on n=%d: %v", n, err)
		}

		// Sum-preservation.
		sum := decimal.Zero
		for _, ins := range instructions {
			sum = sum.Add(ins.Amount)
		}
		if !sum.Equal(amount) {
			t.Fatalf("n=%d: sum=%s != amount=%s", n, sum.String(), amount.String())
		}

		// Per-instruction invariants.
		fivePct := decimal.NewFromFloat(0.05)
		for _, ins := range instructions {
			if !ins.Amount.IsPositive() {
				t.Fatalf("n=%d: non-positive instruction amount %s (kind=%s)",
					n, ins.Amount.String(), ins.Kind)
			}
			switch ins.Kind {
			case RestitutionDestinationBurn:
				if ins.DestRef != "" {
					t.Fatalf("n=%d: burn DestRef not empty: %q", n, ins.DestRef)
				}
				if n > 0 && ins.Amount.Div(amount).GreaterThan(fivePct) {
					t.Fatalf("n=%d: burn %s exceeds 5%% cap", n, ins.Amount.String())
				}
			case RestitutionDestinationUsers,
				RestitutionDestinationInsurance,
				RestitutionDestinationTreasury:
				if ins.DestRef == "" {
					t.Fatalf("n=%d: non-burn leg %s has empty DestRef", n, ins.Kind)
				}
			default:
				t.Fatalf("n=%d: unexpected instruction kind %s", n, ins.Kind)
			}
		}
	})
}
