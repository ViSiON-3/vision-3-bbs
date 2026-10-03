package menu

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"golang.org/x/term"
)

// K shows a sysop the current message's control information; a caller's K
// does nothing.
func TestMessageReaderKludgeView(t *testing.T) {
	// A fresh environment per run: reading advances the user's last-read
	// pointer, after which READMSGS has nothing new to open.
	run := func(sysop bool, input string) runResult {
		env := newMsgEnv(t)
		env.generalMsgs(1)
		u := env.caller
		if sysop {
			u = env.sysop
		}
		return env.runCmd("READMSGS", u, "", input)
	}

	if r := run(true, "KqQ"); !r.has("Message #1 control information", "Written:", "Attributes:") {
		t.Errorf("K did not show the control information; output:\n%s", r.text())
	}
	if r := run(false, "KQ"); r.has("control information") {
		t.Error("caller's K showed the control information")
	}
	if r := run(true, "?Q"); !r.has("ludges / Control Info") {
		t.Error("sysop's help does not list K")
	}
	if r := run(false, "?Q"); r.has("ludges / Control Info") {
		t.Error("caller's help lists K")
	}
}

func TestWrapKludgeLines(t *testing.T) {
	seen := "SEEN-BY: " + strings.Repeat("1234/5678 ", 20)
	rows := wrapKludgeLines([]string{"MSGID: 1:2/3 abc", seen}, 40, ansi.OutputModeUTF8)
	if rows[0] != "MSGID: 1:2/3 abc" {
		t.Errorf("short line changed: %q", rows[0])
	}
	if len(rows) < 4 {
		t.Fatalf("long line not wrapped: %q", rows)
	}
	for _, r := range rows {
		if len([]rune(r)) > 40 {
			t.Errorf("row wider than 40: %q", r)
		}
	}
	for _, r := range rows[2:] {
		if !strings.HasPrefix(r, "    ") {
			t.Errorf("continuation not indented: %q", r)
		}
	}
	// Nothing lost: the wrapped SEEN-BY rejoins to the original words.
	var words []string
	for _, r := range rows[1:] {
		words = append(words, strings.Fields(r)...)
	}
	if strings.Join(words, " ") != strings.Join(strings.Fields(seen), " ") {
		t.Error("wrapping lost or changed text")
	}
}

// Width is measured as the terminal renders it: a CJK character is two
// columns in UTF-8 mode, and a CP437 line is cut by bytes, never re-encoded.
func TestWrapKludgeLines_EncodingAware(t *testing.T) {
	wide := "PID: " + strings.Repeat("\u6f22", 20) // 25 runes, 45 columns
	for _, r := range wrapKludgeLines([]string{wide}, 39, ansi.OutputModeUTF8) {
		if w := columnWidth(r, true, ansi.OutputModeUTF8); w > 39 {
			t.Errorf("UTF-8 row is %d columns, over 39: %q", w, r)
		}
	}

	cp437 := "NOTE: " + strings.Repeat("Caf\x82 ", 12) // not valid UTF-8
	rows := wrapKludgeLines([]string{cp437}, 30, ansi.OutputModeUTF8)
	if len(rows) < 2 {
		t.Fatalf("long CP437 line not wrapped: %q", rows)
	}
	var joined []string
	for _, r := range rows {
		if len(r) > 30 {
			t.Errorf("CP437 row is %d bytes, over 30: %q", len(r), r)
		}
		if utf8.ValidString(r) && strings.ContainsRune(r, utf8.RuneError) {
			t.Errorf("CP437 bytes were decoded: %q", r)
		}
		joined = append(joined, strings.Fields(r)...)
	}
	if strings.Join(joined, " ") != strings.Join(strings.Fields(cp437), " ") {
		t.Errorf("wrapping changed the CP437 bytes: %q", rows)
	}
}

// On a narrow terminal the title stays on one row, so a full page never
// reaches the footer.
func TestShowKludgeView_TitleFitsNarrowTerminal(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "TID: line")
	}
	screen := testterm.New(40, 24)
	terminal := term.NewTerminal(testterm.NewSession(screen, ""), "")
	ih := &scriptedKeys{keys: []int{'q'}}
	if err := showKludgeView(ih, terminal, ansi.OutputModeCP437, 1234567, lines, 40, 24); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(screen.Row(1), " "); !strings.HasPrefix(got, "Message #1234567") || len(got) > 39 {
		t.Errorf("title row = %q; want the title within 39 columns", got)
	}
	if !strings.HasPrefix(screen.Row(2), "---") {
		t.Errorf("row 2 = %q; want the rule, so the title took one row", screen.Row(2))
	}
}

type scriptedKeys struct{ keys []int }

func (s *scriptedKeys) ReadKey() (int, error) {
	k := s.keys[0]
	s.keys = s.keys[1:]
	return k, nil
}
