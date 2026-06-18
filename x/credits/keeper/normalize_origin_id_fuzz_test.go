//go:build cosmos && cosmos_full

package keeper

import (
	"strings"
	"testing"
)

// FuzzNormalizeOriginID pins the contract of the origin_id
// canonicalizer. origin_id is "<namespace>:<surface>" and gets
// written into credit-ledger state as a map key; drift here
// silently partitions credits across case/whitespace variants
// and breaks cross-request accounting. Same bug class as the
// normalizeHash / normalizeHashString / normalizeSignerKey
// sweep.
//
// Invariants across arbitrary input:
//
//  1. No panic on any input.
//  2. Error-path return contract: err != nil → output is "".
//     A refactor that partially populated the return on error
//     would silently leak partial state through the err check.
//  3. Empty or whitespace-only input → ("", nil) — documented
//     "no origin" behavior.
//  4. On success: non-empty output contains exactly one ":".
//  5. On success: output has no uppercase ASCII letter (ToLower
//     applied).
//  6. On success: output has no leading/trailing whitespace.
//  7. On success: output length is in [3, 64] (minimum "x:y" for
//     both parts non-empty; maximum from the length check).
//  8. Idempotent: normalize(normalize(valid)) == normalize(valid).
func FuzzNormalizeOriginID(f *testing.F) {
	seeds := []string{
		"",
		"   ",
		"user:dashboard",
		"USER:DASHBOARD",
		"  user:DASHBOARD  ",
		"user",         // no colon
		":surface",     // empty namespace
		"user:",        // empty surface
		"a:b:c",        // multi-colon
		strings.Repeat("x", 70) + ":" + "y", // too long
		"\x00:\x00",
		"user::surface",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		// Invariant 1: no panic.
		got, err := normalizeOriginID(raw)

		// Invariant 2: err != nil → output is "".
		if err != nil {
			if got != "" {
				t.Fatalf("error path returned non-empty output: got=%q err=%v (raw=%q)",
					got, err, raw)
			}
			return
		}

		// Invariant 3: whitespace-only / empty input → "".
		if strings.TrimSpace(raw) == "" {
			if got != "" {
				t.Fatalf("whitespace-only input %q produced non-empty output %q",
					raw, got)
			}
			return
		}

		// Invariants 4-7: well-formed output on success.
		if got == "" {
			// No-op: empty input handled above. Any other non-error
			// empty-output path would be surprising.
			t.Fatalf("non-empty input %q produced empty output without error",
				raw)
		}
		if strings.Count(got, ":") != 1 {
			t.Fatalf("output must contain exactly one ':', got %q (raw=%q)",
				got, raw)
		}
		for i := 0; i < len(got); i++ {
			b := got[i]
			if b >= 'A' && b <= 'Z' {
				t.Fatalf("uppercase ASCII %q in output: %q (raw=%q)",
					b, got, raw)
			}
		}
		if trimmed := strings.TrimSpace(got); trimmed != got {
			t.Fatalf("output has leading/trailing whitespace: %q (raw=%q)",
				got, raw)
		}
		if len(got) < 3 || len(got) > 64 {
			t.Fatalf("output length %d outside [3, 64]: %q (raw=%q)",
				len(got), got, raw)
		}

		// Invariant 8: idempotence.
		twice, err2 := normalizeOriginID(got)
		if err2 != nil {
			t.Fatalf("second pass errored on own valid output: first=%q err=%v",
				got, err2)
		}
		if twice != got {
			t.Fatalf("not idempotent: first=%q second=%q (raw=%q)",
				got, twice, raw)
		}
	})
}
