package ftn

import "strings"

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
// that should be replaced by the wizard. A token equal to bbsRoot is not
// treated as a placeholder: a real BBS root that happens to match a
// placeholder path (e.g. an actual /opt/vision3 install) is legitimate.
func isPlaceholderLine(line, bbsRoot string) bool {
	trimmed := strings.TrimSpace(line)

	// Skip comments and blank lines — they're not placeholders.
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}

	for _, p := range placeholderTokens {
		if p == bbsRoot {
			continue
		}
		if strings.Contains(trimmed, p) {
			return true
		}
	}
	return false
}

// HasPlaceholders reports whether binkd.conf content still contains template
// placeholder lines, meaning the FTN Setup Wizard has not been run. bbsRoot
// is the real BBS root, exempted as in isPlaceholderLine.
func HasPlaceholders(content, bbsRoot string) bool {
	for _, line := range strings.Split(content, "\n") {
		if isPlaceholderLine(line, bbsRoot) {
			return true
		}
	}
	return false
}
