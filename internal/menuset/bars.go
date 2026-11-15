package menuset

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// BarInheritMarker is the line that makes a BAR file an inherit marker
// (#1079): a file holding it overrides nothing, and its screen takes the
// shared lightbar colours from the theme and the board. Put in the overlay
// (menus.d/<theme>/bar/NAME.BAR) it shadows a shipped BAR of the same name,
// which deleting an overlay file cannot do: the shipped one would show
// through.
const BarInheritMarker = "INHERIT"

// BarInheritFile is the content of an inherit marker file.
const BarInheritFile = "; This screen inherits the shared lightbar colours (Theme in ./config).\n" +
	"; Delete this file to use the BAR it shadows again, if the theme ships one.\n" +
	BarInheritMarker + "\n"

// ScreenBar is a BAR file that overrides one screen's highlight colours.
type ScreenBar struct {
	Name   string // file stem: bar/<Name>.BAR
	Screen string // the screen it colours, as the config editor names it
}

// ScreenBars lists the per-screen highlight overrides, layer 4 of the
// colour precedence. Each colours the selected row of one lightbar screen
// in place of the lightbarSelected role.
var ScreenBars = []ScreenBar{
	{"FILELISTHI", "File List"},
	{"FILEAREAHI", "File Areas"},
	{"MSGAREAHI", "Message Areas"},
	{"MSGCONFHI", "Conferences"},
	{"DOORMENUHI", "Door Menu"},
}

// BarLines returns the lines of a BAR file that are records or the
// inherit marker: trimmed, without blank lines and ';' comments.
func BarLines(data []byte) []string {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// BarInherits reports whether a BAR file's content is an inherit marker.
func BarInherits(data []byte) bool {
	for _, line := range BarLines(data) {
		if strings.EqualFold(line, BarInheritMarker) {
			return true
		}
	}
	return false
}

// BarHighlight returns the highlight and regular colours of a BAR file's
// first record, which is all a *HI.BAR is read for. ok is false for an
// inherit marker and for a file with no usable record.
func BarHighlight(data []byte) (highlight, regular int, ok bool) {
	if BarInherits(data) {
		return 0, 0, false
	}
	for _, line := range BarLines(data) {
		parts := strings.SplitN(line, ",", 7)
		if len(parts) != 7 {
			continue
		}
		hi, hiErr := strconv.Atoi(strings.TrimSpace(parts[2]))
		lo, loErr := strconv.Atoi(strings.TrimSpace(parts[3]))
		if hiErr != nil || loErr != nil {
			continue
		}
		return hi, lo, true
	}
	return 0, 0, false
}
