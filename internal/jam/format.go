package jam

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// Version is kept for compatibility; use internal/version.Number for new code.
var Version = version.Number

// softwareName is the product name stamped into tearlines and PID/TID kludges.
const softwareName = "ViSiON/3"

// AddTearline appends the software tearline to the message text.
// Format: "--- ViSiON/3 v0.8.0/macOS"
//
// Per FTS-0004 the tearline identifies the software that produced the message
// and is assigned by that software, not by the sysop. The sysop-configurable
// line is the origin line; see AddOriginLine.
func AddTearline(text string) string {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + fmt.Sprintf("--- %s\n", DefaultTearlineText())
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
	return fmt.Sprintf("%s %s/%s", softwareName, version.Display(), version.Platform())
}

// FormatPID returns the PID kludge value.
func FormatPID() string {
	return DefaultTearlineText()
}

// FormatTID returns the TID kludge value.
func FormatTID() string {
	return FormatPID()
}
