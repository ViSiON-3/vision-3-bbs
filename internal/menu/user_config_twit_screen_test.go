package menu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// setTwitLists turns the board-wide Twit List switch on or off for env.
func setTwitLists(env *menuEnv, on bool) {
	cfg := env.e.GetServerConfig()
	cfg.TwitListEnabled = &on
	env.e.SetServerConfig(cfg)
}

// TestTwitScreenClearsAndShowsEmptyState checks the dedicated screen and its empty list.
func TestTwitScreenClearsAndShowsEmptyState(t *testing.T) {
	env := newMsgEnv(t)
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r")
	if r.err != nil || !strings.Contains(r.raw, ansi.ClearScreen()) || !r.has("Twit Filter List (0/50)", "empty", "[Q]") {
		t.Fatalf("screen: %v\n%s", r.err, r.text())
	}
	if r.next != "" {
		t.Fatalf("next=%q, want return to the calling menu", r.next)
	}
}

// TestTwitScreenRedrawsWithNoticeAfterAction checks that actions repaint the list with their result.
func TestTwitScreenRedrawsWithNoticeAfterAction(t *testing.T) {
	env := newMsgEnv(t)
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "A\rRemote\rX\rQ\r")
	parts := strings.Split(r.raw, ansi.ClearScreen())
	if len(parts) != 4 {
		t.Fatalf("got %d paints, want 3\n%s", len(parts)-1, r.text())
	}
	if !strings.Contains(parts[2], "(1/50)") || !strings.Contains(parts[2], "1. Remote") || !strings.Contains(parts[2], "Added to your Twit List.") {
		t.Fatalf("after add:\n%s", parts[2])
	}
	if !strings.Contains(parts[3], "Invalid") || strings.Contains(parts[3], "Added") {
		t.Fatalf("unknown choice:\n%s", parts[3])
	}
}

// TestTwitScreenPagesLongLists checks that a long list pages and numbers stay global.
func TestTwitScreenPagesLongLists(t *testing.T) {
	env := newMsgEnv(t)
	for i := 1; i <= 20; i++ {
		env.caller.TwitList = append(env.caller.TwitList, user.TwitEntry{Name: fmt.Sprintf("Author%02d", i)})
	}
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "N\rD\r20\rQ\r")
	parts := strings.Split(r.raw, ansi.ClearScreen())
	if len(parts) != 4 {
		t.Fatalf("got %d paints\n%s", len(parts)-1, r.text())
	}
	first, second, third := parts[1], parts[2], parts[3]
	if !strings.Contains(first, "Page 1 of 2") || !strings.Contains(first, "Author01") || strings.Contains(first, "Author20") {
		t.Fatalf("first page:\n%s", first)
	}
	if !strings.Contains(second, "Page 2 of 2") || !strings.Contains(second, "20. Author20") || strings.Contains(second, "Author01") {
		t.Fatalf("second page:\n%s", second)
	}
	if !strings.Contains(third, "removed") || strings.Contains(third, "Author20") {
		t.Fatalf("after delete:\n%s", third)
	}
	if got := env.mustDiskUser(2).TwitList; len(got) != 19 {
		t.Fatalf("saved %d entries, want 19", len(got))
	}
}

// TestTwitReaderBarShowsIgnore checks the reader's bottom bar offers the quick-add.
func TestTwitReaderBarShowsIgnore(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Remote", to: "All", subject: "one"})
	r := env.runCmd("READMSGS", env.caller, "", "Q")
	if !r.has(" Twit ", " Thread ") {
		t.Fatalf("bar:\n%s", r.text())
	}
}

// TestTwitReaderQuickAddCancelKeepsMessage checks that declining changes nothing.
func TestTwitReaderQuickAddCancelKeepsMessage(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Remote", to: "All", subject: "first"}, testMsg{from: "Other", to: "All", subject: "second"})
	r := env.runCmd("READMSGS", env.caller, "", "INQ")
	if len(env.mustDiskUser(2).TwitList) != 0 || strings.Contains(r.text(), "second-body") {
		t.Fatalf("cancel changed list or moved on:\n%s", r.text())
	}
}

// TestTwitReaderQuickAddAtLastMessage checks the add on the last visible message ends cleanly.
func TestTwitReaderQuickAddAtLastMessage(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Other", to: "All", subject: "first"}, testMsg{from: "Remote", to: "All", subject: "last"})
	r := env.runCmd("READMSGS", env.caller, "", "NIY")
	if r.err != nil || !r.has("Added to your Twit List.", "End of messages.") {
		t.Fatalf("end: %v\n%s", r.err, r.text())
	}
	if r.next == "LOGOFF" {
		t.Fatal("reader logged off instead of leaving")
	}
}

// TestTwitDisabledGatesEverythingAndKeepsLists checks the board-wide switch.
func TestTwitDisabledGatesEverythingAndKeepsLists(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.TwitList = []user.TwitEntry{{Name: "Remote"}}
	if err := env.um.UpdateUser(env.caller); err != nil {
		t.Fatal(err)
	}
	env.postMsgs(generalAreaID, testMsg{from: "Remote", to: "All", subject: "twit-post"}, testMsg{from: "Other", to: "All", subject: "other-post"})
	setTwitLists(env, false)

	r := env.runCmd("CFG_TWITLIST", env.caller, "", "C\rY\rQ\r")
	if !r.has("turned off") || strings.Contains(r.raw, ansi.ClearScreen()) {
		t.Fatalf("editor while off:\n%s", r.text())
	}
	r = env.runCmd("READMSGS", env.caller, "", "IYQ")
	if !r.has("twit-post-body") || r.has(" Twit ") || r.has("Add Remote") {
		t.Fatalf("reader while off:\n%s", r.text())
	}
	if env.e.TwitMatcher(env.um, env.caller) != nil {
		t.Fatal("matcher built while off")
	}
	if got := env.mustDiskUser(2).TwitList; len(got) != 1 || got[0].Name != "Remote" {
		t.Fatalf("saved list changed while off: %+v", got)
	}

	setTwitLists(env, true)
	r = env.runCmd("READMSGS", env.mustDiskUser(2), "", "Q")
	if r.has("twit-post-body") || !r.has("other-post-body") {
		t.Fatalf("re-enabled list not applied:\n%s", r.text())
	}
}

// TestTwitKonfigShowsOffWhenDisabled checks the configuration form while the switch is off.
func TestTwitKonfigShowsOffWhenDisabled(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigTestExecutor(t)
	off := false
	cfg := e.GetServerConfig()
	cfg.TwitListEnabled = &off
	e.SetServerConfig(cfg)
	st := &konfigState{c: &cmdCtx{e: e, userManager: um, currentUser: u}}
	if err := editTwitList(st); err != nil || st.redraw || !strings.Contains(st.status, "turned off") {
		t.Fatalf("status=%q redraw=%v err=%v", st.status, st.redraw, err)
	}
}

// TestReaderBarSuffixFitsWidth checks both bars keep their label within 79
// columns at 80, and that a bar too wide drops it rather than wrapping.
func TestReaderBarSuffixFitsWidth(t *testing.T) {
	caller := append(append([]MsgLightbarOption(nil), msgReaderOptions...), msgReaderIgnoreOption)
	sysop := append(append([]MsgLightbarOption(nil), caller...), msgReaderDeleteOption)
	for _, suffix := range []string{" |11(Reading)", " |11(NewScan)"} {
		for name, opts := range map[string][]MsgLightbarOption{"caller": caller, "sysop": sysop} {
			if got := readerBarSuffix(opts, suffix, 80); got != suffix {
				t.Errorf("%s bar dropped %q at 80 columns", name, suffix)
			}
		}
		if got := readerBarSuffix(sysop, suffix, 79); got != "" {
			t.Errorf("sysop bar kept %q at 79 columns", suffix)
		}
	}
}

// TestTwitScreenClipsLongNames keeps each entry on one row.
func TestTwitScreenClipsLongNames(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.TwitList = []user.TwitEntry{{Name: strings.Repeat("x", 150)}}
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r")
	if !r.has("  1. "+strings.Repeat("x", 74)) || r.has(strings.Repeat("x", 75)) {
		t.Fatalf("long name not clipped:\n%s", r.text())
	}
}
