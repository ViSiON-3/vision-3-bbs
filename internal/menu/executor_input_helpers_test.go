package menu

import (
	"fmt"
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

// --- styledInput: rune-correct column math for extended characters ---

// TestStyledInput_CP437ByteRendersOneColumn types a single CP437 extended
// byte (0x82, 'é') and checks that the cursor reposition after it treats the
// typed character as ONE column, not two bytes. A byte-based measurement
// (len(input) where input holds the 2-byte UTF-8 encoding of 'é') would emit
// "\x1b[3D" instead of the correct "\x1b[4D", mis-drawing the box.
func TestStyledInput_CP437ByteRendersOneColumn(t *testing.T) {
	const maxLen = 5
	ts := newTestSession("\x82\r")
	SetSessionOutputMode(ts, ansi.OutputModeCP437)
	terminal := newTestTerminal(ts)

	result, err := styledInput(terminal, ts, ansi.OutputModeCP437, maxLen, "")
	if err != nil {
		t.Fatalf("styledInput: %v", err)
	}
	if result != "é" {
		t.Errorf("result = %q, want %q", result, "é")
	}

	out := ts.output()
	wantMove := fmt.Sprintf("\x1b[%dD", maxLen-1) // maxLen(5) - cursorCols(1)
	if !strings.Contains(out, wantMove) {
		t.Errorf("output = %q, want cursor reposition %q (column-based, not byte-based)", out, wantMove)
	}
	if !strings.Contains(out, "\x82") {
		t.Errorf("output = %q, want raw byte 0x82 echoed back via the rendered box", out)
	}
}

// TestStyledInput_StrayLeadByteDoesNotEatNextChar mirrors the reader-side
// regression: styledInput's read loop has the identical utf8Pending
// structure, and the same bug applied here -- an ASCII keystroke between a
// stray lead byte and a legitimate multi-byte character must not leave the
// stale pending buffer around to swallow it.
func TestStyledInput_StrayLeadByteDoesNotEatNextChar(t *testing.T) {
	const maxLen = 5
	ts := newTestSession("\xc9A\xc3\xa9\r")
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	terminal := newTestTerminal(ts)

	result, err := styledInput(terminal, ts, ansi.OutputModeUTF8, maxLen, "")
	if err != nil {
		t.Fatalf("styledInput: %v", err)
	}
	if !utf8.ValidString(result) {
		t.Fatalf("result is not valid UTF-8: %q", result)
	}
	if result != "Aé" {
		t.Errorf("result = %q, want %q (stray lead byte must not swallow the next legitimate character)", result, "Aé")
	}
}

// TestStyledInput_DefaultValueClampCountsRunesNotBytes is the maxLen-as-bytes
// hazard called out in the design brief: "éééé" is 4 runes but 8 bytes. A
// byte-based clamp (input[:maxLen]) on maxLen=3 would slice mid-rune and
// corrupt the UTF-8. It must clamp to 3 RUNES instead.
func TestStyledInput_DefaultValueClampCountsRunesNotBytes(t *testing.T) {
	const maxLen = 3
	ts := newTestSession("\r") // accept the (clamped) default unmodified
	terminal := newTestTerminal(ts)

	result, err := styledInput(terminal, ts, ansi.OutputModeUTF8, maxLen, "éééé")
	if err != nil {
		t.Fatalf("styledInput: %v", err)
	}
	if !utf8.ValidString(result) {
		t.Fatalf("result is not valid UTF-8: %q", result)
	}
	if result != "ééé" {
		t.Errorf("result = %q, want %q (clamp must count runes, not bytes)", result, "ééé")
	}
}

// TestStyledInput_DefaultValueWithinMaxLenIsUnchanged guards the common case
// where a multi-byte default value already fits within maxLen columns: it
// must survive the clamp untouched.
func TestStyledInput_DefaultValueWithinMaxLenIsUnchanged(t *testing.T) {
	const maxLen = 3
	ts := newTestSession("\r")
	terminal := newTestTerminal(ts)

	result, err := styledInput(terminal, ts, ansi.OutputModeUTF8, maxLen, "éé")
	if err != nil {
		t.Fatalf("styledInput: %v", err)
	}
	if result != "éé" {
		t.Errorf("result = %q, want unmodified default %q", result, "éé")
	}
}
