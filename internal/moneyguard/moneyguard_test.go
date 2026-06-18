package moneyguard

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestIsSafeExponent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"zero", "0", true},
		{"small_positive", "1", true},
		{"small_negative", "-1", true},
		{"fractional", "0.5", true},
		{"micro", "0.000000000000000001", true}, // -18 exponent
		{"trillion", "1000000000000", true},
		{"boundary_positive", "1e100", true},
		{"boundary_negative", "1e-100", true},
		{"just_over_positive", "1e101", false},
		{"just_over_negative", "1e-101", false},
		{"absurd_positive", "1e11100100", false},
		{"absurd_negative", "1e-11100100", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d, err := decimal.NewFromString(tc.in)
			if err != nil {
				t.Fatalf("seed %q failed to parse: %v", tc.in, err)
			}
			if got := IsSafeExponent(d); got != tc.want {
				t.Fatalf("IsSafeExponent(%q) = %v, want %v (exp=%d)",
					tc.in, got, tc.want, d.Exponent())
			}
		})
	}
}

// TestIsSafeExponent_MaxAbsExponentExactlyAtBoundary pins the
// inclusive semantics: exponent == MaxAbsExponent must be accepted
// so fixed-point currencies near the bound aren't rejected by an
// off-by-one mistake.
func TestIsSafeExponent_MaxAbsExponentExactlyAtBoundary(t *testing.T) {
	d, err := decimal.NewFromString("1e100")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !IsSafeExponent(d) {
		t.Fatalf("exponent=%d (== MaxAbsExponent=%d) should be accepted",
			d.Exponent(), MaxAbsExponent)
	}
}
