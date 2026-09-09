package configeditor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestWrapHelpTwoLines covers #274: field help longer than the box was cut
// mid-word at the right edge. It must now wrap onto a second line, on word
// boundaries, and preserve the whole text.
func TestWrapHelpTwoLines(t *testing.T) {
	const width = 71 // boxW(70)+1, the field-help area

	// The Network Name help from the screenshot — 83 cells, was cut at "stor".
	long := "Network identifier (e.g. fsxnet, fidonet) — also the binkd domain; stored lowercase"
	l1, l2 := wrapHelpTwoLines(long, width)
	if l2 == "" {
		t.Fatal("expected the over-long help to wrap onto a second line")
	}
	if lipgloss.Width(l1) > width || lipgloss.Width(l2) > width {
		t.Errorf("wrapped lines exceed width: %d / %d > %d", lipgloss.Width(l1), lipgloss.Width(l2), width)
	}
	// No word is split across the break, and nothing is lost.
	if l1[len(l1)-1] == ' ' || strings.HasPrefix(l2, " ") {
		t.Errorf("break landed inside whitespace: %q | %q", l1, l2)
	}
	if got := strings.Join(strings.Fields(l1+" "+l2), " "); got != long {
		t.Errorf("text not preserved across wrap:\n got %q\nwant %q", got, long)
	}

	// Short help stays on one line.
	if a, b := wrapHelpTwoLines("Enable built-in echomail tosser", width); b != "" || a != "Enable built-in echomail tosser" {
		t.Errorf("short help should not wrap: %q | %q", a, b)
	}
}
