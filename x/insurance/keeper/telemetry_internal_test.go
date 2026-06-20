
package keeper

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestTelemetryDecimalFloat32RejectsUnsafeExponent(t *testing.T) {
	t.Parallel()

	if _, ok := telemetryDecimalFloat32("1e101"); ok {
		t.Fatal("unsafe telemetry decimal exponent should be skipped")
	}
}

func TestTelemetryDecimalFloat32AcceptsSafeDecimal(t *testing.T) {
	t.Parallel()

	got, ok := telemetryDecimalFloat32("12.5")
	if !ok {
		t.Fatal("safe telemetry decimal should parse")
	}
	if got != 12.5 {
		t.Fatalf("telemetryDecimalFloat32 = %v, want 12.5", got)
	}
}

func TestParsePoolMetricDecimalRejectsUnsafeExponent(t *testing.T) {
	t.Parallel()

	_, err := parsePoolMetricDecimal("TotalFunds", "1e101")
	if err == nil {
		t.Fatal("unsafe pool metric exponent should be rejected")
	}
	if got := err.Error(); got != `invalid TotalFunds "1e101": magnitude out of range` {
		t.Fatalf("parsePoolMetricDecimal error = %q", got)
	}
}

func TestParsePoolMetricDecimalPreservesParseErrors(t *testing.T) {
	t.Parallel()

	_, err := parsePoolMetricDecimal("ReservedFunds", "not-a-decimal")
	if err == nil {
		t.Fatal("malformed pool metric decimal should fail")
	}
	if got := err.Error(); got != `invalid ReservedFunds "not-a-decimal": can't convert not-a-decimal to decimal: exponent is not numeric` {
		t.Fatalf("parsePoolMetricDecimal error = %q", got)
	}
}

func TestPoolMetricDecimalOrZeroSkipsUnsafeExponent(t *testing.T) {
	t.Parallel()

	got := poolMetricDecimalOrZero("AvailableFunds", "1e101")
	if !got.IsZero() {
		t.Fatalf("poolMetricDecimalOrZero = %s, want zero", got)
	}
}

func TestAddStringAmountsSkipsUnsafeExponents(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		current string
		delta   string
		want    string
	}{
		"unsafe current": {
			current: "1e101",
			delta:   "7",
			want:    "7",
		},
		"unsafe delta": {
			current: "5",
			delta:   "1e101",
			want:    "5",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := addStringAmounts(tc.current, tc.delta); got != tc.want {
				t.Fatalf("addStringAmounts(%q, %q) = %q, want %q", tc.current, tc.delta, got, tc.want)
			}
		})
	}
}

func TestInsuranceParamDecimalRejectsUnsafeExponent(t *testing.T) {
	t.Parallel()

	if _, ok := insuranceParamDecimal("1e101"); ok {
		t.Fatal("unsafe insurance parameter exponent should be rejected")
	}
}

func TestInsuranceParamDecimalOrDefaultUsesFallbackForUnsafeExponent(t *testing.T) {
	t.Parallel()

	fallback := decimal.NewFromInt(10)
	got := insuranceParamDecimalOrDefault("1e101", fallback)
	if !got.Equal(fallback) {
		t.Fatalf("insuranceParamDecimalOrDefault = %s, want %s", got, fallback)
	}
}
