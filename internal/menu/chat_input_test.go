package menu

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestChatWrapInput(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		width      int
		want       []string
	}{
		{"empty", "", 10, []string{""}},
		{"fits", "hello", 10, []string{"hello"}},
		{"exactly fills the line", "hello worl", 10, []string{"hello worl"}},
		{"a word that does not fit moves whole", "hello world", 10, []string{"hello ", "world"}},
		{"several wraps", "aaa bbb ccc ddd eee", 7, []string{"aaa bbb", "ccc ddd", "eee"}},
		{"a space past the edge starts the next line", "hello worl ", 10, []string{"hello worl", ""}},
		{"spaces inside a line are kept", "a  b", 10, []string{"a  b"}},
		{"a word longer than a line starts its own line", "hi abcdefghijkl", 5, []string{"hi ", "abcde", "fghij", "kl"}},
		{"a long word alone is cut at the edge only", "abcdefghij", 5, []string{"abcde", "fghij"}},
		{"text after a long word wraps normally", "abcdefg hi", 5, []string{"abcde", "fg hi"}},
		{"width below one still makes progress", "abc", 0, []string{"a", "b", "c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, line := range chatWrapInput([]rune(tc.text), tc.width) {
				got = append(got, string(line))
			}
			chatcovEqual(t, "lines", got, tc.want)
		})
	}
}

// chatRegionSets matches every DECSTBM sequence that sets a scroll region.
var chatRegionSets = regexp.MustCompile(`\x1b\[\d+;\d+r`)

// chatReadScreen runs chatReadLine on a 24-row screen of the given width and
// returns the line, everything drawn, and the screen afterwards.
func chatReadScreen(t *testing.T, input string, width int, prompt string) (string, string, *chatScreen) {
	t.Helper()
	ts := newTestSession(input)
	t.Cleanup(func() { resetSessionIH(ts) })
	var mu sync.Mutex
	var out strings.Builder
	scr := &chatScreen{height: 24, width: width, chatTop: 6, inputRows: 1, write: func(b []byte) { out.Write(b) }}
	scr.drawStatus = func() { out.WriteString("<status>") }
	line, err := chatReadLine(ts, &mu, scr, prompt)
	if err != nil {
		t.Fatalf("chatReadLine: %v", err)
	}
	return line, out.String(), scr
}

// TestChatReadLineWraps covers #657: typing past the end of the input row
// wraps onto a new row instead of dropping keys, and the whole text is sent.
func TestChatReadLineWraps(t *testing.T) {
	// "> " leaves 17 columns per row at width 20.
	long := "the quick brown fox jumps over the lazy dog"
	line, out, scr := chatReadScreen(t, long+"\r", 20, "> ")
	if line != long {
		t.Errorf("line = %q, want the whole text %q", line, long)
	}
	// Three rows at most: the input area grew upward by two rows, so the
	// chat region shrank to end at row 19, and the status bars moved up.
	for _, want := range []string{"\x1b[6;20r", "\x1b[6;19r"} {
		if !strings.Contains(out, want) {
			t.Errorf("scroll region %q never set", want)
		}
	}
	// The rows as drawn when the last key was typed: words kept whole, and
	// continuation rows indented under the text.
	for _, want := range []string{
		"\x1b[22;1H\x1b[2K> the quick brown",
		"\x1b[23;1H\x1b[2K  fox jumps over",
		"\x1b[24;1H\x1b[2K  the lazy dog\x1b[24;15H",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("row %q not drawn:\n%q", want, out)
		}
	}
	// After Enter the input area is back to one row and the region to 21.
	if scr.inputRows != 1 {
		t.Errorf("inputRows after Enter = %d, want 1", scr.inputRows)
	}
	if regions := chatRegionSets.FindAllString(out, -1); len(regions) == 0 || regions[len(regions)-1] != "\x1b[6;21r" {
		t.Errorf("region not restored to rows 6-21 after Enter: %q", regions)
	}
	if !strings.HasSuffix(out, "\x1b[24;1H\x1b[2K> \x1b[24;3H") {
		t.Errorf("input row not cleared back to the prompt after Enter: %q", out[len(out)-40:])
	}
	if strings.Count(out, "<status>") < 3 {
		t.Errorf("status bars not redrawn on every resize: %d redraws", strings.Count(out, "<status>"))
	}
}

func TestChatReadLineBackspaceAcrossWrap(t *testing.T) {
	// "hello world" wraps "world" onto row 24; four backspaces and the
	// whole of "world" leave "hello " on one row again.
	line, out, scr := chatReadScreen(t, "hello world\x08\x08\x08\x08\x08\x08X\r", 10, "> ")
	if line != "helloX" {
		t.Errorf("line = %q, want %q", line, "helloX")
	}
	if !strings.Contains(out, "\x1b[24;1H\x1b[2K  world\x1b[24;8H") {
		t.Errorf("wrapped word not drawn on its own row: %q", out)
	}
	if !strings.Contains(out, "\x1b[24;1H\x1b[2K> helloX\x1b[24;9H") {
		t.Errorf("backspace did not bring the text back onto one row: %q", out)
	}
	if scr.inputRows != 1 {
		t.Errorf("inputRows = %d, want 1", scr.inputRows)
	}
}

func TestChatReadLineLimits(t *testing.T) {
	// The whole message is capped, not the row.
	line, _, _ := chatReadScreen(t, strings.Repeat("ab ", 300)+"\r", 80, "> ")
	if n := len([]rune(line)); n != chatInputMaxLen {
		t.Errorf("line length = %d, want %d", n, chatInputMaxLen)
	}
	// The input area stops growing at chatInputMaxRows and shows the last rows.
	_, out, _ := chatReadScreen(t, strings.Repeat("word ", 40)+"end\r", 20, "> ")
	if strings.Contains(out, "\x1b[6;16r") {
		t.Errorf("input area grew past %d rows", chatInputMaxRows)
	}
	if !strings.Contains(out, "\x1b[6;17r") {
		t.Errorf("input area never reached %d rows", chatInputMaxRows)
	}
	if !strings.Contains(out, "\x1b[24;1H\x1b[2K  word end\x1b[24;11H") {
		t.Errorf("last row not shown at the bottom: %q", out[len(out)-200:])
	}
}

// TestChatLongMessageSentWhole drives #657 through the chat screen: a message
// three input rows long is posted as one message, the scroll region and the
// status bars move up while it is typed, and both are back in place after.
func TestChatLongMessageSentWhole(t *testing.T) {
	env := newMenuEnv(t)
	msg := strings.TrimSpace(strings.Repeat("several words of text ", 7)) // 153 characters
	r := chatcovChat(env, env.caller, msg+"\r/q\r")

	chatcovEqual(t, "stored lobby history", chatcovLocalHistory(env, "lobby"), []string{"Caller: " + msg})
	// "<Caller> " leaves 70 columns a row on an 80-column screen: three rows.
	chatcovEqual(t, "scroll regions set", chatRegionSets.FindAllString(r.raw, -1),
		[]string{"\x1b[6;21r", "\x1b[6;20r", "\x1b[6;19r", "\x1b[6;21r"})
	// The status bars were redrawn above the three input rows.
	for _, row := range []int{20, 21} {
		if !strings.Contains(r.raw, fmt.Sprintf("\x1b[%d;2H", row)) {
			t.Errorf("status bar not redrawn on row %d", row)
		}
	}
	// The sent message is shown once in the chat area, wrapped as received
	// messages are.
	if !chatcovHas(r, "<Caller> several words of text") {
		t.Errorf("own message not shown:\n%s", chatcovText(r))
	}
}

// The text typed into a wrapped input area is not mistaken for chat output
// by the tests' screen scraper: every input row is stripped, not just the last.
func TestChatcovTextStripsWrappedInput(t *testing.T) {
	r := runResult{raw: "\x1b[22;1H\x1b[2K> one\x1b[23;1H\x1b[2K  two\x1b[24;1H\x1b[2K  three\x1b[24;9Hshown"}
	if got := chatcovText(r); got != "shown" {
		t.Errorf("chatcovText = %q, want only the text outside the input area", got)
	}
}
