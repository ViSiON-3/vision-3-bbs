package ftn

import (
	"bufio"
	"strings"
)

// placeholderTokens are template values in the shipped binkd.conf that the
// FTN Setup Wizard replaces with real data.
var placeholderTokens = []string{
	"hub.example.com",
	"/opt/vision3",
	"21:1/123@fsxnet",
	"3:1/456.1@fidonet",
	"\"My BBS\"",
	"\"Somewhere, USA\"",
	"\"SysOp\"",
	"PASSWORD",
}

// isPlaceholderLine returns true if a line contains template placeholder data
// that should be replaced by the wizard.
func isPlaceholderLine(line string) bool {
	trimmed := strings.TrimSpace(line)

	// Skip comments and blank lines — they're not placeholders.
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}

	for _, p := range placeholderTokens {
		if strings.Contains(trimmed, p) {
			return true
		}
	}
	return false
}

// HasPlaceholders reports whether binkd.conf content still contains template
// placeholder lines, meaning the FTN Setup Wizard has not been run. A token
// that equals the real bbsRoot is skipped, so a sysop whose actual root
// matches a placeholder path is not flagged.
func HasPlaceholders(content, bbsRoot string) bool {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, p := range placeholderTokens {
			if p == bbsRoot {
				continue
			}
			if strings.Contains(trimmed, p) {
				return true
			}
		}
	}
	return false
}
