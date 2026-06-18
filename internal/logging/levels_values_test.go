package logging

import (
	"testing"
)

// logging.go ships 4 Level iota values + the parseLevel string→Level
// mapping consumed by LOG_LEVEL env-var / config-file parsing:
//
//   - LevelDebug = 0 (verbose)
//   - LevelInfo  = 1 (default / fallback)
//   - LevelWarn  = 2
//   - LevelError = 3 (strictest)
//
// Filter semantics throughout the codebase use `l.level > LevelX`
// to suppress below-threshold output — a reorder (e.g.
// Error=0, Debug=3) would silently invert the guard, flooding
// production with debug spam. parseLevel maps "debug"/"warn"/
// "error" (case-insensitive, whitespace-trimmed) to the 3 non-
// default levels and falls through to LevelInfo on anything else.
//
// Existing logging_test.go exercises parseLevel + filter behaviour
// but never pins the iota integer values nor the string-mapping
// vocabulary as exact contracts.

// TestLogLevel_ExactIotaValues pins the 4 iota integer values.
// A reorder would invert > filter semantics in every
// `l.level > LevelX` call site.
func TestLogLevel_ExactIotaValues(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		got  Level
		want int
	}{
		"LevelDebug": {LevelDebug, 0},
		"LevelInfo":  {LevelInfo, 1},
		"LevelWarn":  {LevelWarn, 2},
		"LevelError": {LevelError, 3},
	}
	for name, tc := range cases {
		if int(tc.got) != tc.want {
			t.Errorf("%s = %d, want %d — log-filter `l.level > LevelX` ordering contract",
				name, int(tc.got), tc.want)
		}
	}
}

// TestLogLevel_IotaContiguous pins that values start at 0 and are
// consecutive. A `LevelDebug = iota + 1` regression would silently
// shift all 4 values by +1, breaking every on-disk Level-integer
// log record (metadata stores the level as int, not string).
func TestLogLevel_IotaContiguous(t *testing.T) {
	t.Parallel()

	levels := []Level{LevelDebug, LevelInfo, LevelWarn, LevelError}
	for i, l := range levels {
		if int(l) != i {
			t.Errorf("level at index %d = %d, want %d — iota contiguity",
				i, int(l), i)
		}
	}
}

// TestLogLevel_DebugIsLowestErrorIsHighest pins the ordering
// invariant: Debug < Info < Warn < Error. Filter semantics rely on
// this: `l.level > LevelDebug` means "suppress debug", which
// requires Debug to be the smallest value. Inversion would make
// debug-spam the default and silence errors.
func TestLogLevel_DebugIsLowestErrorIsHighest(t *testing.T) {
	t.Parallel()

	if LevelDebug >= LevelInfo {
		t.Errorf("LevelDebug=%d must be < LevelInfo=%d — filter ordering",
			LevelDebug, LevelInfo)
	}
	if LevelInfo >= LevelWarn {
		t.Errorf("LevelInfo=%d must be < LevelWarn=%d — filter ordering",
			LevelInfo, LevelWarn)
	}
	if LevelWarn >= LevelError {
		t.Errorf("LevelWarn=%d must be < LevelError=%d — filter ordering",
			LevelWarn, LevelError)
	}
}

// TestParseLevel_ExactMapping pins the string→Level mapping
// consumed by LOG_LEVEL env-var / config-file parsing. Operators
// set `LOG_LEVEL=debug` in YAML and expect the verbose floor.
func TestParseLevel_ExactMapping(t *testing.T) {
	t.Parallel()

	cases := map[string]Level{
		"debug": LevelDebug,
		"info":  LevelInfo, // explicit match + default fallback path
		"warn":  LevelWarn,
		"error": LevelError,
	}
	for input, want := range cases {
		if got := parseLevel(input); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v — LOG_LEVEL parse contract",
				input, got, want)
		}
	}
}

// TestParseLevel_CaseAndWhitespaceInsensitive pins the
// normalization: case-insensitive + whitespace-trimmed so
// "DEBUG" / " debug " / "DeBuG" all produce LevelDebug.
// Operators often paste copy-paste config with leading whitespace.
func TestParseLevel_CaseAndWhitespaceInsensitive(t *testing.T) {
	t.Parallel()

	cases := []string{"DEBUG", "Debug", "  debug  ", "\tdebug\n"}
	for _, input := range cases {
		if got := parseLevel(input); got != LevelDebug {
			t.Errorf("parseLevel(%q) = %v, want LevelDebug — case/whitespace normalization",
				input, got)
		}
	}
}

// TestParseLevel_UnknownFallsBackToInfo pins the documented
// fallback: unrecognized input (including empty string and
// malformed values) returns LevelInfo. A regression that returned
// LevelDebug as the fallback would flood production with debug
// spam on every typo'd config.
func TestParseLevel_UnknownFallsBackToInfo(t *testing.T) {
	t.Parallel()

	cases := []string{"", "verbose", "trace", "critical", "typo", "  "}
	for _, input := range cases {
		if got := parseLevel(input); got != LevelInfo {
			t.Errorf("parseLevel(%q) = %v, want LevelInfo — unknown-input fallback",
				input, got)
		}
	}
}
