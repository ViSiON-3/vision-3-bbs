package ftn

import (
	"fmt"
	"os"
	"strings"
)

// Syncing the standalone binkd settings (listen port, log level) in place.

// SyncBinkdSettings updates the iport and loglevel lines in binkd.conf to
// match the configured values. The file is only rewritten when a value
// differs; a missing binkd.conf is a no-op (the FTN Setup Wizard creates it).
// Non-positive port/logLevel values leave the corresponding line untouched.
func SyncBinkdSettings(confPath string, port, logLevel int) error {
	existing, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No binkd.conf to sync.
		}
		return fmt.Errorf("reading binkd.conf: %w", err)
	}

	var out strings.Builder
	changed := false

	for _, line := range confLines(string(existing)) {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "iport ") && port > 0 {
			newLine := fmt.Sprintf("iport %d", port)
			if trimmed != newLine {
				out.WriteString(newLine)
				out.WriteByte('\n')
				changed = true
				continue
			}
		}
		if strings.HasPrefix(trimmed, "loglevel ") && logLevel > 0 {
			newLine := fmt.Sprintf("loglevel %d", logLevel)
			if trimmed != newLine {
				out.WriteString(newLine)
				out.WriteByte('\n')
				changed = true
				continue
			}
		}

		out.WriteString(line)
		out.WriteByte('\n')
	}

	if !changed {
		return nil
	}
	return writeFileAtomic(confPath, out.String(), 0600)
}
