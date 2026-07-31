package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// GoXY takes (column, row) — the one place in this package where the order is
// x,y rather than row,col.
func TestScreenGoXYAndWriteDirect(t *testing.T) {
	tt := testterm.New(40, 10)
	s := NewScreen(tt, ansi.OutputModeUTF8, 40, 10)

	s.GoXY(5, 3)
	s.WriteDirect("Hello")

	if got := tt.Row(3); got != "    Hello" {
		t.Errorf("Row(3) = %q, want %q", got, "    Hello")
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty — the double missed output Screen emitted", got)
	}
}

func TestScreenClearEOL(t *testing.T) {
	tt := testterm.New(40, 10)
	s := NewScreen(tt, ansi.OutputModeUTF8, 40, 10)

	s.GoXY(1, 2)
	s.WriteDirect("abcdefgh")
	s.GoXY(4, 2)
	s.ClearEOL()

	if got := tt.Row(2); got != "abc" {
		t.Errorf("Row(2) = %q, want %q", got, "abc")
	}
}

func TestScreenClearScreen(t *testing.T) {
	tt := testterm.New(40, 10)
	s := NewScreen(tt, ansi.OutputModeUTF8, 40, 10)

	s.GoXY(1, 1)
	s.WriteDirect("junk")
	s.GoXY(1, 5)
	s.WriteDirect("more junk")
	s.ClearScreen()

	if got := tt.Snapshot(); got != "" {
		t.Errorf("Snapshot() = %q, want empty", got)
	}
	if row, col := tt.Cursor(); row != 1 || col != 1 {
		t.Errorf("Cursor() = (%d,%d), want (1,1) — ClearScreen homes the cursor", row, col)
	}
}

// WriteDirectProcessed runs the real pipe-code table, so this covers SGR
// arriving from production code rather than from a hand-written escape.
func TestScreenWriteDirectProcessedAppliesPipeCodeColour(t *testing.T) {
	tt := testterm.New(40, 10)
	s := NewScreen(tt, ansi.OutputModeUTF8, 40, 10)

	s.GoXY(1, 1)
	s.WriteDirectProcessed("|09Bright")

	if got := tt.Row(1); got != "Bright" {
		t.Errorf("Row(1) = %q, want %q — pipe code leaked into the text", got, "Bright")
	}
	if c := tt.Cell(1, 1); c.Fg == -1 {
		t.Errorf("Cell(1,1) = %+v, want a foreground colour from |09", c)
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}

// The same source text must land as the same runes in both output modes: in
// CP437 mode Screen puts single high bytes on the wire, in UTF-8 mode it puts
// multi-byte sequences.
func TestScreenRendersSameRunesInBothOutputModes(t *testing.T) {
	const shade = "░░░"

	utf8Term := testterm.New(40, 10)
	utf8Screen := NewScreen(utf8Term, ansi.OutputModeUTF8, 40, 10)
	utf8Screen.GoXY(1, 1)
	utf8Screen.WriteDirect(shade)

	cp437Term := testterm.New(40, 10, testterm.CP437())
	cp437Screen := NewScreen(cp437Term, ansi.OutputModeCP437, 40, 10)
	cp437Screen.GoXY(1, 1)
	cp437Screen.WriteDirect(shade)

	if got := utf8Term.Row(1); got != shade {
		t.Errorf("UTF-8 mode Row(1) = %q, want %q", got, shade)
	}
	if got := cp437Term.Row(1); got != shade {
		t.Errorf("CP437 mode Row(1) = %q, want %q", got, shade)
	}
	ur, uc := utf8Term.Cursor()
	cr, cc := cp437Term.Cursor()
	if ur != cr || uc != cc {
		t.Errorf("cursor differs between modes: UTF-8 (%d,%d) vs CP437 (%d,%d) — one glyph must be one column in both", ur, uc, cr, cc)
	}
}

// writeEditorTemplate writes a minimal FSEDITOR.ANS containing an @I@
// placeholder and returns the menu-set path to pass to LoadHeaderTemplate.
// A template needs at least one @CODE@ marker for Screen to treat it as the
// current format, which is what makes it track the @I@ position.
func writeEditorTemplate(t *testing.T) string {
	t.Helper()
	menuSet := t.TempDir()
	ansiDir := filepath.Join(menuSet, "ansi")
	if err := os.MkdirAll(ansiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Row 1: subject. Row 2: the insert-mode indicator at column 7.
	template := "Subj: @E@\r\nMode: @I@\r\n"
	if err := os.WriteFile(filepath.Join(ansiDir, "FSEDITOR.ANS"), []byte(template), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	return menuSet
}

// DisplayStatusLine writes the insert-mode indicator through a
// save / jump / colour / write / reset / restore sequence. It exercises more of
// the parser in one call than any other Screen method.
func TestScreenInsertModeIndicator(t *testing.T) {
	menuSet := writeEditorTemplate(t)

	tt := testterm.New(40, 10)
	s := NewScreen(tt, ansi.OutputModeUTF8, 40, 10)
	if err := s.LoadHeaderTemplate(menuSet, "Test subject", "someone", "me", false); err != nil {
		t.Fatalf("LoadHeaderTemplate: %v", err)
	}
	s.DisplayHeader()

	// The @E@ placeholder is substituted with the subject, so this also covers
	// the header render path.
	if got := tt.Row(1); got != "Subj: Test subject" {
		t.Errorf("Row(1) = %q, want %q", got, "Subj: Test subject")
	}

	// Park the cursor somewhere identifiable, then update the indicator.
	s.GoXY(10, 8)
	s.DisplayStatusLine(true, 1, 1)

	if got := tt.Row(2); got != "Mode: Ins" {
		t.Errorf("Row(2) = %q, want %q", got, "Mode: Ins")
	}
	if row, col := tt.Cursor(); row != 8 || col != 10 {
		t.Errorf("Cursor() = (%d,%d), want (8,10) — the indicator write must restore the cursor", row, col)
	}

	s.DisplayStatusLine(false, 1, 1)
	if got := tt.Row(2); got != "Mode: Ovr" {
		t.Errorf("Row(2) = %q, want %q", got, "Mode: Ovr")
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}
