package menu

import (
	"strings"
	"testing"
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
	rows := wrapKludgeLines([]string{"MSGID: 1:2/3 abc", seen}, 40)
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
