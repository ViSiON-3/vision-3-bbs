package jam

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// Version is kept for compatibility; use internal/version.Number for new code.
var Version = version.Number

// softwareName is the product name stamped into tearlines and PID/TID kludges.
const softwareName = "ViSiON/3"

// maxTearlineLength is the FTS-0004 limit on the text following "--- ".
const maxTearlineLength = 35

// AddTearline stamps the software tearline onto the message text.
// Format: "--- ViSiON/3 v0.8.0/macOS"
//
// Per FTS-0004 the tearline identifies the software that produced the message
// and is assigned by that software, not by the sysop. The sysop-configurable
// line is the origin line; see AddOriginLine.
//
// The tearline belongs at the end of the message, directly above the origin
// line, so any tearline already sitting in that trailing block is replaced
// rather than duplicated. Only the trailer is rewritten: a "--- " line inside
// the body — a quoted separator, a list item, an e-mail-style signature cut —
// is message content and is left exactly where the author put it.
func AddTearline(text string) string {
	tearline := "--- " + DefaultTearlineText()

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	start := trailerStart(lines)

	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:start]...)
	out = append(out, tearline)
	for _, line := range lines[start:] {
		// Tearlines in the trailer are superseded by the one just added;
		// origin lines and any other trailer content survive.
		if !isTearlineLine(line) {
			out = append(out, line)
		}
	}

	return strings.Join(out, "\n") + "\n"
}

// trailerStart returns the index at which the message's trailing tearline and
// origin block begins, or len(lines) when the message has no such block. Only
// an unbroken run of tearline and origin lines at the very end counts, so body
// text is never mistaken for a trailer.
func trailerStart(lines []string) int {
	start := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if !isTearlineLine(lines[i]) && !isOriginLine(lines[i]) {
			break
		}
		start = i
	}
	return start
}

// isTearlineLine reports whether a line is a tearline: "---" alone, or "---"
// followed by a software identifier.
func isTearlineLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "---" || strings.HasPrefix(trimmed, "--- ")
}

// isOriginLine reports whether a line is an FTN origin line.
func isOriginLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "* Origin:")
}

// AddOriginLine appends an origin line to the message text.
// Format: " * Origin: BBS Name (1:103/705)"
func AddOriginLine(text, systemName, address string) string {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + fmt.Sprintf(" * Origin: %s (%s)\n", systemName, address)
}

// DefaultTearlineText returns the tearline body carrying the running
// version and platform, e.g. "ViSiON/3 v0.8.0/macOS".
func DefaultTearlineText() string {
	platform := version.Platform()
	versionText := version.Display()
	// FTS-0004 caps the text after "--- " at 35 characters, and version.Number
	// takes whatever a -ldflags build stamps into it. The separators are the
	// space and the slash in the format string below.
	maxVersionLength := maxTearlineLength - len(softwareName) - len(" /") - len(platform)
	if maxVersionLength < 0 {
		maxVersionLength = 0
	}
	// Trim whole runes: a build could stamp a non-ASCII version, and cutting
	// one in half would put invalid UTF-8 on the wire.
	for len(versionText) > maxVersionLength {
		_, size := utf8.DecodeLastRuneInString(versionText)
		versionText = versionText[:len(versionText)-size]
	}
	return fmt.Sprintf("%s %s/%s", softwareName, versionText, platform)
}

// FormatPID returns the PID kludge value.
func FormatPID() string {
	return DefaultTearlineText()
}

// FormatTID returns the TID kludge value.
func FormatTID() string {
	return FormatPID()
}
