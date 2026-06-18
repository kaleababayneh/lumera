package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLogger.Warn, Error, and Debug all contain level-filter early-
// returns. The existing TestNewTestLoggerWithLevel_FiltersDebug test
// covers Debug (at level=warn) but not Warn (at level=error) or
// Error (at level>error).
//
// The level-filter branches are subtle because they're the ONLY
// thing preventing a debug-level test harness from capturing
// high-volume runtime logs and polluting the test output.

// TestTestLogger_Warn_LevelFilter pins that Warn at level=error
// silently drops the message. Without this, a test running at
// level=error would still see Warn messages, breaking the "show
// only errors" contract that noisy-test-harness users rely on.
func TestTestLogger_Warn_LevelFilter(t *testing.T) {
	logger := NewTestLoggerWithLevel("error")
	logger.Warn("should not appear", String("k", "v"))
	require.Equal(t, 0, logger.Count("warn"),
		"Warn at level=error must be dropped — level-filter invariant")
	require.Equal(t, 0, logger.Count(""),
		"no entries must be recorded at all")
}

// TestTestLogger_Error_LevelFilter pins the Error early-return
// branch. Since NewTestLoggerWithLevel only parses canonical levels
// (debug/info/warn/error = 0-3), we set the level field directly
// to a value > LevelError (4) to exercise the guard. This mirrors
// how a future level (e.g. LevelFatal=4) would interact with the
// existing filter — pinning the `>` comparison direction.
func TestTestLogger_Error_LevelFilter(t *testing.T) {
	logger := NewTestLogger()
	// Directly set level above LevelError to exercise the early-
	// return branch. In-package access is deliberate: public API
	// (parseLevel) only emits 0-3, but the guard must work for
	// any hypothetical higher level.
	logger.level = LevelError + 1

	logger.Error("should not appear")
	require.Equal(t, 0, logger.Count("error"),
		"Error at level>LevelError must be dropped — pins the `>` "+
			"comparison direction. A regression to `>=` would skip "+
			"every Error call at level=LevelError, silently losing "+
			"production error logs")
}

// TestTestLogger_Warn_RecordsAtWarnLevel is the polarity anchor —
// without it, a regression that always early-returned (regardless
// of level) would pass the filter tests above but break the normal
// record path for Warn/Error messages.
func TestTestLogger_Warn_RecordsAtWarnLevel(t *testing.T) {
	logger := NewTestLoggerWithLevel("warn")
	logger.Warn("visible warning")
	require.Equal(t, 1, logger.Count("warn"),
		"Warn at level=warn must record — polarity anchor catches "+
			"always-early-return regressions")
}

// TestTestLogger_Error_RecordsAtErrorLevel pins the same polarity
// anchor for Error at canonical level=error (NOT the artificially-
// raised level used above).
func TestTestLogger_Error_RecordsAtErrorLevel(t *testing.T) {
	logger := NewTestLoggerWithLevel("error")
	logger.Error("visible error")
	require.Equal(t, 1, logger.Count("error"))
}
