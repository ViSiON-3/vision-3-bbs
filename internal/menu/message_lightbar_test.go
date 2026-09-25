package menu

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"golang.org/x/term"
)

func runTestMsgLightbar(t *testing.T, input string, passKeys []int) (byte, int) {
	t.Helper()
	ih := editor.NewInputHandler(strings.NewReader(input))
	terminal := term.NewTerminal(&bytes.Buffer{}, "")
	sel, pass, err := runMsgLightbar(ih, terminal, msgReaderOptions, ansi.OutputModeUTF8,
		15, 9, "", 0, true, 1, passKeys)
	if err != nil {
		t.Fatalf("runMsgLightbar(%q): %v", input, err)
	}
	return sel, pass
}

// TestMsgLightbarPassesScrollKeys guards #412: once Left/Right had moved into
// the reader's lightbar, Up and Down were swallowed there and the message could
// no longer scroll. Keys the caller lists must come straight back to it.
func TestMsgLightbarPassesScrollKeys(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int
	}{
		{"\x1b[C\x1b[B", editor.KeyArrowDown},
		{"\x1b[D\x1b[A", editor.KeyArrowUp},
		{"\x1b[C\x1b[6~", editor.KeyPageDown},
		{"\x1b[C\x05", editor.KeyCtrlE},
	} {
		sel, pass := runTestMsgLightbar(t, tc.input, []int{
			editor.KeyArrowUp, editor.KeyArrowDown, editor.KeyPageDown, editor.KeyCtrlE,
		})
		if sel != 0 || pass != tc.want {
			t.Errorf("input %q: got (sel %q, pass %#x), want (0, %#x)", tc.input, sel, pass, tc.want)
		}
	}
}

func TestMsgLightbarSelects(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  byte
	}{
		{"\x1b[C\r", 'R'},       // Right then Enter selects the second option
		{"\x1b[D\r", 'Q'},       // Left wraps to the last option
		{"\x1b[B\x1b[C\r", 'R'}, // Up/Down are ignored when not passed through
		{"66\r", 'S'},           // numpad right twice: Next -> Reply -> Prev
		{"j", 'J'},              // direct hotkey, case-insensitive
	} {
		sel, pass := runTestMsgLightbar(t, tc.input, nil)
		if sel != tc.want || pass != 0 {
			t.Errorf("input %q: got (sel %q, pass %#x), want (%q, 0)", tc.input, sel, pass, tc.want)
		}
	}
}

// TestMsgLightbarReportsIdleTimeout checks that an idle session's timeout
// comes back as ErrIdleTimeout, so callers log the user off instead of
// treating it as an ordinary read error and carrying on.
func TestMsgLightbarReportsIdleTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	ih := editor.NewInputHandler(pr)
	ih.SetSessionIdleTimeout(20 * time.Millisecond)
	terminal := term.NewTerminal(&bytes.Buffer{}, "")
	_, _, err := runMsgLightbar(ih, terminal, msgReaderOptions, ansi.OutputModeUTF8,
		15, 9, "", 0, true, 1, nil)
	if !errors.Is(err, editor.ErrIdleTimeout) {
		t.Fatalf("err = %v, want ErrIdleTimeout", err)
	}
}
