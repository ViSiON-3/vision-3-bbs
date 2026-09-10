// Package logging owns all of ViSiON/3's logging concerns: a configurable
// rolling file writer, level parsing, and a one-call Init that installs a
// structured slog logger as the process default. Binaries call Init once at
// startup; every other package logs through stdlib slog with no import coupling
// to this package.
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// securityCategory is attached to Security records so security-relevant events
// can be filtered in the JSON logs (replacing the old "SECURITY:" prefix).
const securityCategory = "security"

// osExit is indirected for testing Fatal without terminating the test process.
var osExit = os.Exit

// ParseLevel maps a case-insensitive level name to a slog.Level. Recognized
// names are DEBUG, INFO, WARN (or WARNING), and ERROR. Unknown input returns
// slog.LevelInfo and a non-nil error so callers can fall back safely.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "INFO":
		return slog.LevelInfo, nil
	case "WARN", "WARNING":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", s)
	}
}

// Fatal logs msg at Error level on the default logger, then exits the process
// with status 1. It replaces the old "FATAL:"/log.Fatalf pattern, which has no
// native slog level.
func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	osExit(1)
}

// Security logs msg at Warn level with a category=security attribute. It
// replaces the old "SECURITY:" prefix, which has no native slog level.
func Security(msg string, args ...any) {
	slog.Warn(msg, append([]any{slog.String("category", securityCategory)}, args...)...)
}

// SetLevel changes the minimum level of the logger Init installed, so a
// config.json logging.level edit takes effect without a restart. The level
// string is validated the same way Init validates it; an unknown name returns
// an error and leaves the current level unchanged. Calling SetLevel before
// Init has run is an error rather than a silent no-op.
func SetLevel(s string) error {
	level, err := ParseLevel(s)
	if err != nil {
		return err
	}
	if activeLevel == nil {
		return fmt.Errorf("logging not initialized")
	}
	activeLevel.Set(level)
	return nil
}

// Level reports the current minimum level of the logger Init installed. It
// exists for tests and diagnostics; before Init it reports slog.LevelInfo.
func Level() slog.Level {
	if activeLevel == nil {
		return slog.LevelInfo
	}
	return activeLevel.Level()
}
