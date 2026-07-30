package menu

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

var centerCursorRe = regexp.MustCompile(`\r\x1b\[(\d+)C`)

// The pause prompt is centered by measuring its visible width. That width must
// be counted in characters: the shipped pauseString contains U+25A0 block
// glyphs, which are 3 bytes each in UTF-8 mode, so a byte count over-measures
// and shifts the prompt left of center.
func TestWriteCenteredPausePromptCentersByVisibleColumns(t *testing.T) {
	const termWidth = 80
	// Mirrors the shipped strings.json pauseString shape: pipe codes plus
	// multi-byte block glyphs around ASCII text.
	prompt := "|15■|07■|08■ |15SlAm eNtEr!|08 ■|07■|15■"

	ts := newTestSession("\r")
	terminal := newTestTerminal(ts)

	if err := writeCenteredPausePrompt(ts, terminal, prompt, ansi.OutputModeUTF8, termWidth, 24); err != nil {
		t.Fatalf("writeCenteredPausePrompt: %v", err)
	}

	m := centerCursorRe.FindStringSubmatch(ts.output())
	if m == nil {
		t.Fatalf("no centering cursor move found in output %q", ts.output())
	}
	gotPad, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("bad cursor column %q: %v", m[1], err)
	}

	// Expected padding from the visible text, pipe codes removed.
	visible := strings.NewReplacer("|15", "", "|07", "", "|08", "").Replace(prompt)
	wantPad := (termWidth - utf8.RuneCountInString(visible)) / 2
	if gotPad != wantPad {
		t.Errorf("left padding = %d, want %d (visible text %q is %d columns, %d bytes)",
			gotPad, wantPad, visible, utf8.RuneCountInString(visible), len(visible))
	}
}
