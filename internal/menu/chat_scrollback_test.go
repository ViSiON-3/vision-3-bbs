package menu

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func chatLines(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("line %d", i))
	}
	return out
}

func TestChatScrollback(t *testing.T) {
	b := &chatScrollback{capacity: 10}
	b.add(chatLines(1, 4)...)
	chatcovEqual(t, "view of a short buffer", b.view(3), chatLines(2, 4))
	chatcovEqual(t, "view taller than the buffer", b.view(6), chatLines(1, 4))

	// The oldest lines go once the buffer is full.
	b.add(chatLines(5, 12)...)
	if b.len() != 10 {
		t.Errorf("len = %d, want the capacity 10", b.len())
	}
	chatcovEqual(t, "live view", b.view(3), chatLines(10, 12))

	// Paging moves a page less one line, so a line of context stays.
	if !b.page(-1, 3) || b.live() {
		t.Fatalf("page up did not move off the live view")
	}
	chatcovEqual(t, "one page up", b.view(3), chatLines(8, 10))
	b.page(-1, 3)
	b.page(-1, 3)
	b.page(-1, 3)
	chatcovEqual(t, "paging stops at the oldest line", b.view(3), chatLines(3, 5))
	if b.page(-1, 3) {
		t.Errorf("page up at the top reported a move")
	}

	// New lines while scrolled back do not move the view.
	b.add("line 13")
	chatcovEqual(t, "view after a new line", b.view(3), chatLines(4, 6))

	for b.page(1, 3) {
	}
	if !b.live() {
		t.Errorf("paging down to the end did not return to the live view")
	}
	chatcovEqual(t, "back at the end", b.view(3), chatLines(11, 13))

	b.page(-1, 3)
	b.reset()
	if b.len() != 0 || !b.live() {
		t.Errorf("reset left %d lines, live=%v", b.len(), b.live())
	}
}

// TestChatScrollbackPaging drives the scrollback through the chat screen:
// PgUp redraws the chat rows with older lines and says so on the hint bar,
// the header counts the lines held, and Enter goes back to the live view.
func TestChatScrollbackPaging(t *testing.T) {
	env := newMenuEnv(t)
	var keys strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&keys, "msg %02d\r", i)
	}
	keys.WriteString("\x1b[5~") // PgUp
	keys.WriteString("\x1b[5~")
	keys.WriteString("\x1b[6~") // PgDn
	keys.WriteString("\r")      // a blank line still returns to the live view
	keys.WriteString("/q\r")
	r := chatcovChat(env, env.caller, keys.String())

	// The join notice and 30 messages are 31 lines of the 500 kept.
	if !strings.Contains(chatLastHeader(t, r), "[Buffer·031/500]") {
		t.Errorf("Buffer counter not filled:\n%s", chatLastHeader(t, r))
	}
	// PgUp redraws the 16 chat rows, then the hint bar. With 31 lines a page
	// of 15 already reaches the top: the join notice on row 6 and msg 15 on
	// row 21. The second PgUp has nowhere to go.
	paged, rest, ok := strings.Cut(r.raw, "Scrollback")
	if !ok {
		t.Fatalf("hint bar never showed the scrollback notice:\n%s", chatcovText(r))
	}
	if got := chatRowText(paged, 6); !strings.Contains(got, "*** Joined #lobby") {
		t.Errorf("row 6 after PgUp = %q, want the join notice", got)
	}
	if got := chatRowText(paged, 21); !strings.Contains(got, "msg 15") {
		t.Errorf("row 21 after PgUp = %q, want msg 15", got)
	}
	if strings.Count(r.raw, "Scrollback") != 1 {
		t.Errorf("hint bar redrawn %d times, want once: the second PgUp should not move", strings.Count(r.raw, "Scrollback"))
	}
	// PgDn returns to the live view: msg 30 on row 21 and the usual hints.
	if got := chatRowText(rest, 21); !strings.Contains(got, "msg 30") {
		t.Errorf("row 21 after PgDn = %q, want msg 30", got)
	}
	if !strings.Contains(testAnsiEscape.ReplaceAllString(rest, ""), "/rooms /join /msg") {
		t.Errorf("command hints not restored after PgDn")
	}
}

// chatRowText returns the text last written at the start of row in raw,
// up to the next cursor move, without colour codes.
func chatRowText(raw string, row int) string {
	at := fmt.Sprintf("\x1b[%d;1H", row)
	i := strings.LastIndex(raw, at)
	if i < 0 {
		return ""
	}
	rest := raw[i+len(at):]
	if loc := chatCursorMove.FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	return testAnsiEscape.ReplaceAllString(rest, "")
}

// chatCursorMove matches a cursor position sequence.
var chatCursorMove = regexp.MustCompile(`\x1b\[\d+;\d+H`)
