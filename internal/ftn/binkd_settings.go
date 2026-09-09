package ftn

import (
	"fmt"
	"os"
	"strings"
)

// Syncing the standalone binkd settings (listen port, log level, outbound
// directory) in place.

// SyncBinkdSettings updates the iport, loglevel and domain lines in
// binkd.conf to match the configured values. The file is only rewritten when a
// value differs; a missing binkd.conf is a no-op (the FTN Setup Wizard creates
// it). Non-positive port/logLevel values, and an empty outboundPath, leave the
// corresponding lines untouched.
//
// outboundPath is repointed on every domain line because binkd sends only what
// it finds in its own outbound: if it disagrees with ftn.json's
// binkd_outbound_path the tosser packs bundles into a directory binkd never
// reads, and echomail queues up silently. Syncing here means an install that
// has already drifted is repaired on the next binkd launch.
func SyncBinkdSettings(confPath string, port, logLevel int, outboundPath string) error {
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
		// "domain <name> <outbound-path> <zone>": rewrite the path field
		// only, leaving the sysop's network names and zones alone.
		if strings.HasPrefix(trimmed, "domain ") && outboundPath != "" {
			if fields := strings.Fields(trimmed); len(fields) == 4 && fields[2] != outboundPath {
				fields[2] = outboundPath
				out.WriteString(strings.Join(fields, " "))
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
