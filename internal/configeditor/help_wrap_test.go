package configeditor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestWrapHelpTwoLines covers #274: field help longer than the box was cut
// mid-word. It must wrap onto a second line, on word boundaries, losing nothing.
func TestWrapHelpTwoLines(t *testing.T) {
	const width = 71 // boxW(70)+1

	long := "Network identifier (e.g. fsxnet, fidonet) — also the binkd domain; stored lowercase"
	l1, l2 := wrapHelpTwoLines(long, width)
	if l2 == "" {
		t.Fatal("expected the over-long help to wrap onto a second line")
	}
	if lipgloss.Width(l1) > width || lipgloss.Width(l2) > width {
		t.Errorf("wrapped lines exceed width: %d / %d > %d", lipgloss.Width(l1), lipgloss.Width(l2), width)
	}
	if strings.HasSuffix(l1, " ") || strings.HasPrefix(l2, " ") {
		t.Errorf("break landed inside whitespace: %q | %q", l1, l2)
	}
	if got := strings.Join(strings.Fields(l1+" "+l2), " "); got != long {
		t.Errorf("text not preserved:\n got %q\nwant %q", got, long)
	}

	if a, b := wrapHelpTwoLines("Enable built-in echomail tosser", width); b != "" || a != "Enable built-in echomail tosser" {
		t.Errorf("short help should not wrap: %q | %q", a, b)
	}
}

// TestWrapHelpWideGlyphStaysInWidth covers the display-width truncation both
// reviewers flagged: a single word wider than the line, in full-width glyphs,
// must be cut by display cells (not rune count) so it never overruns the box.
func TestWrapHelpWideGlyphStaysInWidth(t *testing.T) {
	const width = 20
	wide := strings.Repeat("漢", 40) // 80 display cells, one unbreakable "word"
	l1, l2 := wrapHelpTwoLines(wide, width)
	if lipgloss.Width(l1) > width {
		t.Errorf("line1 width %d exceeds %d: %q", lipgloss.Width(l1), width, l1)
	}
	if l2 != "" {
		t.Errorf("a single word should not spill to a second line: %q", l2)
	}
	if !strings.HasSuffix(l1, "…") {
		t.Errorf("truncated help should be marked with an ellipsis: %q", l1)
	}
}

// TestTruncateWithEllipsis measures in display cells and reserves the ellipsis.
func TestTruncateWithEllipsis(t *testing.T) {
	if got := truncateWithEllipsis("hello", 10); got != "hello" {
		t.Errorf("short string should be unchanged, got %q", got)
	}
	got := truncateWithEllipsis(strings.Repeat("漢", 10), 9) // 20 cells -> 9
	if lipgloss.Width(got) > 9 {
		t.Errorf("width %d exceeds 9: %q", lipgloss.Width(got), got)
	}
}

// TestFieldHelpAreaKeepsGap covers the user request: the blank separator row
// between the field help and the footer must survive whether the help fits on
// one line or wraps to two. Renders the record editor for a short-help and a
// long-help field and checks the second-to-last help-region row is blank.
func TestFieldHelpAreaKeepsGap(t *testing.T) {
	strip := func(s string) string {
		var b strings.Builder
		esc := false
		for _, r := range s {
			if r == 0x1b {
				esc = true
				continue
			}
			if esc {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
					esc = false
				}
				continue
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	// The separator/gap row is a backdrop-fill line (shading glyphs), not
	// spaces, so "blank" here means "carries no help/footer text" — no letters.
	hasLetter := func(s string) bool {
		for _, r := range strip(s) {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				return true
			}
		}
		return false
	}

	build := func(recType, label string) []string {
		m := configuredModel()
		m.width, m.height, m.mode = 78, 40, modeRecordEdit
		m.recordType, m.recordEditIdx = recType, 0
		m.recordFields = m.buildRecordFields()
		for i, f := range m.recordFields {
			if f.Label == label {
				m.editField = i
			}
		}
		return strings.Split(m.viewRecordEdit(), "\n")
	}

	for _, tc := range []struct{ name, recType, field string }{
		{"short help", "ftn", "Tosser Enabled"},
		{"wrapped help", "msgarea", "Area Type"}, // 84-cell help, wraps to two lines
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := build(tc.recType, tc.field)
			// last line is the footer (help bar); the line above it must be blank.
			footer := len(lines) - 1
			if !hasLetter(lines[footer]) {
				t.Fatalf("last line is not the footer: %q", strip(lines[footer]))
			}
			if hasLetter(lines[footer-1]) {
				t.Errorf("no blank separator above the footer:\n above=%q\n footer=%q",
					strip(lines[footer-1]), strip(lines[footer]))
			}
		})
	}
}
