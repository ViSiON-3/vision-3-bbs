package menu

import (
	"regexp"
	"strconv"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

var promptCursorBackRe = regexp.MustCompile(`\x1b\[(\d+)D`)

// promptYesNoLightbar computes optionsWidth = len(noOptionText) + spacing +
// len(yesOptionText), counting bytes, and uses it to move the cursor
// backward (CursorBackward) before erasing and redrawing the options. The
// shipped Yes/No labels are ASCII ("Yes"/"No"), so this is unreachable
// today, but a localized label with multi-byte characters would move the
// cursor too far and the following "\x1b[K" would erase into the prompt
// text itself. This test uses custom labels to exercise that path
// pre-emptively.
func TestPromptYesNoLightbarCursorBackIsRuneWidth(t *testing.T) {
	yesLabel := "はい" // 2 runes, 6 bytes
	noLabel := "いいえ" // 3 runes, 9 bytes

	e := &MenuExecutor{
		LoadedStrings: config.StringsConfig{YesPromptText: yesLabel, NoPromptText: noLabel},
	}
	ts := newTestSession("\x1b[C\r") // toggle selection, then confirm
	terminal := newTestTerminal(ts)

	if _, err := e.PromptYesNo(ts, terminal, "Continue?", ansi.OutputModeUTF8, 1, 80, 24, false); err != nil {
		t.Fatalf("PromptYesNo: %v", err)
	}

	out := ts.output()
	matches := promptCursorBackRe.FindAllStringSubmatch(out, -1)
	if len(matches) == 0 {
		t.Fatalf("no cursor-backward sequence found in output %q", out)
	}

	// optionsWidth = " いいえ " + 2 spacing + " はい " (rune counts: 5 + 2 + 4 = 11).
	wantWidth := utf8.RuneCountInString(" "+noLabel+" ") + 2 + utf8.RuneCountInString(" "+yesLabel+" ")
	for _, m := range matches {
		got, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("parse cursor-back count %q: %v", m[1], err)
		}
		if got != wantWidth {
			t.Errorf("cursor-backward count = %d, want %d (visible width of the options area); output %q", got, wantWidth, out)
		}
	}
}
