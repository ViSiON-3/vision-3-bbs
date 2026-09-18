package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// The old recognizer was "digits and semicolons then a letter", which missed
// three CSI forms and counted their bytes as visible columns.
func TestEscapeLenCoversTheCSIGrammar(t *testing.T) {
	for _, tc := range []struct {
		name string
		seq  string
	}{
		{"colour", "\x1b[31m"},
		{"reset", "\x1b[0m"},
		{"private mode (hide cursor)", "\x1b[?25l"},
		{"private mode (show cursor)", "\x1b[?25h"},
		{"colon-form extended colour", "\x1b[38:5:12m"},
		{"intermediate byte (cursor style)", "\x1b[1 q"},
		{"no parameters", "\x1b[m"},
		{"erase in line", "\x1b[K"},
		{"cursor position", "\x1b[12;40H"},
		{"charset designation", "\x1b(B"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := escapeLen(tc.seq, 0); got != len(tc.seq) {
				t.Errorf("escapeLen(%q) = %d, want %d", tc.seq, got, len(tc.seq))
			}
			if got := stripEscapes(tc.seq + "text"); got != "text" {
				t.Errorf("stripEscapes(%q+text) = %q, want %q", tc.seq, got, "text")
			}
			if got := visibleColumns(tc.seq+"text", ansi.OutputModeUTF8); got != 4 {
				t.Errorf("visibleColumns(%q+text) = %d, want 4", tc.seq, got)
			}
		})
	}
}

// Escapes must not be counted when deciding whether a line fits, or a line of
// heavily coloured text wraps far earlier than it needs to.
func TestEscapeLenKeepsPrivateModesOutOfTheWidth(t *testing.T) {
	const width = 20
	line := "\x1b[?25l" + strings.Repeat("a", width) + "\x1b[?25h"

	if got := visibleColumns(line, ansi.OutputModeUTF8); got != width {
		t.Errorf("line measured %d columns, want %d", got, width)
	}
	if got := wrapAnsiString(line, width, ansi.OutputModeUTF8); len(got) != 1 {
		t.Errorf("line fits but wrapped onto %d lines: %q", len(got), got)
	}
}

// hardBreak must never cut an escape in half; the old byte scanner stopped at
// the first letter, so a colon-form sequence could be split mid-parameter.
func TestEscapeLenSurvivesAHardBreak(t *testing.T) {
	const width = 10
	seq := "\x1b[38:5:12m"
	line := strings.Repeat("a", 15) + seq + strings.Repeat("b", 15)

	chunks := breakOversizedLines([]string{line}, width, ansi.OutputModeUTF8)
	if len(chunks) < 2 {
		t.Fatalf("expected the line to break, got %d chunk(s)", len(chunks))
	}
	joined := strings.Join(chunks, "")
	if joined != line {
		t.Errorf("hard break altered the line:\n got  %q\n want %q", joined, line)
	}
	if !strings.Contains(joined, seq) {
		t.Errorf("escape was split across a chunk boundary: %q", chunks)
	}
	for i, c := range chunks {
		if w := visibleColumns(c, ansi.OutputModeUTF8); w > width {
			t.Errorf("chunk %d is %d visible columns, over %d: %q", i, w, width, c)
		}
	}
}
