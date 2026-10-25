package menu

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/mattn/go-runewidth"
)

// setTwitLists turns the board-wide Twit List switch on or off for env.
func setTwitLists(env *menuEnv, on bool) {
	cfg := env.e.GetServerConfig()
	cfg.TwitListEnabled = &on
	env.e.SetServerConfig(cfg)
}

// twitPaints splits a run's output at each screen clear, with colour removed.
func twitPaints(r runResult) []string {
	parts := strings.Split(r.raw, ansi.ClearScreen())
	for i := range parts {
		parts[i] = stripEscapes(parts[i])
	}
	return parts
}

// TestTwitScreenClearsAndShowsEmptyState checks the dedicated screen and its empty list.
func TestTwitScreenClearsAndShowsEmptyState(t *testing.T) {
	env := newMsgEnv(t)
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r")
	paint := strings.Join(twitPaints(r)[1:], "")
	if r.err != nil || !strings.Contains(r.raw, ansi.ClearScreen()) || r.has("#   Author") {
		t.Fatalf("screen: %v\n%s", r.err, r.text())
	}
	for _, want := range []string{"Twit List / Personal Ignore List", "(0 of 50 used)", "empty", "Q) Quit", "Selection:"} {
		if !strings.Contains(paint, want) {
			t.Errorf("missing %q", want)
		}
	}
	if r.next != "" {
		t.Fatalf("next=%q, want return to the calling menu", r.next)
	}
}

// TestTwitScreenRedrawsWithNoticeAfterAction checks that actions repaint the list with their result.
func TestTwitScreenRedrawsWithNoticeAfterAction(t *testing.T) {
	env := newMsgEnv(t)
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "A\rRemote\rX\rQ\r")
	parts := twitPaints(r)
	if len(parts) != 4 {
		t.Fatalf("got %d paints, want 3\n%s", len(parts)-1, r.text())
	}
	if !strings.Contains(parts[2], "(1 of 50 used)") || !strings.Contains(parts[2], " 1  Remote") || !strings.Contains(parts[2], "Added to your Twit List.") {
		t.Fatalf("after add:\n%s", parts[2])
	}
	if !strings.Contains(parts[3], "Invalid") || strings.Contains(parts[3], "Added") {
		t.Fatalf("unknown choice:\n%s", parts[3])
	}
}

// TestTwitScreenPagesLongLists checks a full list of 50 takes two pages of
// three columns at 80x24, and that numbers stay global across pages.
func TestTwitScreenPagesLongLists(t *testing.T) {
	env := newMsgEnv(t)
	for i := 1; i <= 50; i++ {
		env.caller.TwitList = append(env.caller.TwitList, user.TwitEntry{Name: fmt.Sprintf("Author%02d", i)})
	}
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "]\rD\r50\rQ\r")
	parts := twitPaints(r)
	if len(parts) != 4 {
		t.Fatalf("got %d paints\n%s", len(parts)-1, r.text())
	}
	first, second, third := parts[1], parts[2], parts[3]
	if !strings.Contains(first, "Page 1 of 2") || !strings.Contains(first, "Author30") || strings.Contains(first, "Author31") {
		t.Fatalf("first page:\n%s", first)
	}
	if !strings.Contains(second, "Page 2 of 2") || !strings.Contains(second, "50  Author50") || strings.Contains(second, "Author30") {
		t.Fatalf("second page:\n%s", second)
	}
	if !strings.Contains(third, "removed") || strings.Contains(third, "Author50") {
		t.Fatalf("after delete:\n%s", third)
	}
	if got := env.mustDiskUser(2).TwitList; len(got) != 49 {
		t.Fatalf("saved %d entries, want 49", len(got))
	}
}

// TestTwitScreenColumnsRunDown checks entries fill each column top to bottom,
// three to a row, and that the heading covers only the columns in use.
func TestTwitScreenColumnsRunDown(t *testing.T) {
	env := newMsgEnv(t)
	for i := 1; i <= 30; i++ {
		env.caller.TwitList = append(env.caller.TwitList, user.TwitEntry{Name: fmt.Sprintf("Author%02d", i)})
	}
	paint := twitPaints(env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r"))[1]
	if !regexp.MustCompile(` 1  Author01 +11  Author11 +21  Author21 *\r*\n`).MatchString(paint) || strings.Contains(paint, "Page") {
		t.Fatalf("not three columns down on one page:\n%s", paint)
	}
	if n := strings.Count(paint, "#  Author"); n != 3 {
		t.Fatalf("heading over %d columns, want 3", n)
	}
	env.caller.TwitList = env.caller.TwitList[:2]
	paint = twitPaints(env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r"))[1]
	if n := strings.Count(paint, "#  Author"); n != 2 {
		t.Fatalf("two entries headed over %d columns, want 2:\n%s", n, paint)
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
	for _, suffix := range []string{" |11Reading", " |11NewScan"} {
		for name, opts := range map[string][]MsgLightbarOption{"caller": caller, "sysop": sysop} {
			if got := readerBarSuffix(opts, suffix, 80); got != suffix {
				t.Errorf("%s bar dropped %q at 80 columns", name, suffix)
			}
		}
		if got := readerBarSuffix(sysop, suffix, 78); got != "" {
			t.Errorf("sysop bar kept %q at 78 columns", suffix)
		}
	}
}

// TestTwitScreenClipsLongNames keeps each entry on one row.
func TestTwitScreenClipsLongNames(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.TwitList = []user.TwitEntry{{Name: strings.Repeat("x", 150)}}
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r")
	if !r.has(" 1  "+strings.Repeat("x", 21)) || r.has(strings.Repeat("x", 22)) {
		t.Fatalf("long name not clipped:\n%s", r.text())
	}
}

// TestTwitReaderRefusesBeforeConfirming checks self and staff refusals come
// without the confirmation prompt, and a co-sysop is protected.
func TestTwitReaderRefusesBeforeConfirming(t *testing.T) {
	for _, tc := range []struct{ from, level, want string }{
		{"Caller", "", "can't ignore yourself"},
		{"Sysop", "", "can't be ignored"},
		{"Cosy", "250", "can't be ignored"},
	} {
		t.Run(tc.from, func(t *testing.T) {
			env := newMsgEnv(t)
			if tc.level != "" {
				u, err := env.um.AddUser("password", tc.from, "Co Sysop", "Loc")
				if err != nil {
					t.Fatal(err)
				}
				u.AccessLevel = 250
				if err := env.um.UpdateUser(u); err != nil {
					t.Fatal(err)
				}
			}
			env.postMsgs(generalAreaID, testMsg{from: tc.from, to: "All", subject: "post"})
			r := env.runCmd("READMSGS", env.caller, "", "IQ")
			if !r.has(tc.want) || r.has("to your Twit List? [Y/N]") || len(env.mustDiskUser(2).TwitList) != 0 {
				t.Fatalf("%v\n%s", r.err, r.text())
			}
		})
	}
}

// TestTwitScreenFallsBackWithoutMenuFiles keeps the screen working on a menu
// set that has no TWITLIST.MNU or templates, such as an upgraded board's.
func TestTwitScreenFallsBackWithoutMenuFiles(t *testing.T) {
	env := newMsgEnv(t)
	env.e.MenuSetPath = t.TempDir() // an empty menu set, never the shipped one
	env.caller.TwitList = []user.TwitEntry{{Name: "Remote"}}
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r")
	paint := strings.Join(twitPaints(r), "")
	if r.err != nil || !strings.Contains(paint, "Personal Ignore List") || !strings.Contains(paint, " 1  Remote") || !strings.Contains(paint, "Selection:") {
		t.Fatalf("fallback: %v\n%s", r.err, paint)
	}
}

// TestTwitDefaultsMatchShippedFiles keeps the built-in fallback identical to
// the shipped TWITLIST.MNU and templates.
func TestTwitDefaultsMatchShippedFiles(t *testing.T) {
	e := &MenuExecutor{MenuSetPath: filepath.Join("..", "..", "menus", "v3")}
	for part, want := range twitDefaultArt {
		b, err := os.ReadFile(filepath.Join(e.MenuSetPath, "templates", "TWITLIST."+part))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimRight(string(b), "\r\n"); got != want {
			t.Errorf("TWITLIST.%s:\n got  %q\n want %q", part, got, want)
		}
	}
	rec, err := LoadMenu("TWITLIST", e.Menus())
	if err != nil {
		t.Fatal(err)
	}
	if *rec != twitDefaultMenu {
		t.Errorf("TWITLIST.MNU = %+v, built-in %+v", *rec, twitDefaultMenu)
	}
}

// TestTwitScreenClipsDoubleWidthNames cuts names by screen columns, so wide
// characters cannot push a cell past its width.
func TestTwitScreenClipsDoubleWidthNames(t *testing.T) {
	env := newMsgEnv(t)
	env.outputMode = ansi.OutputModeUTF8
	env.caller.TwitList = []user.TwitEntry{{Name: strings.Repeat("漢", 30)}, {Name: "Second"}, {Name: "Third"}}
	paint := twitPaints(env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r"))[1]
	for _, line := range strings.Split(paint, "\n") {
		if strings.Contains(line, "漢") {
			cells := strings.TrimRight(line, "\r ")
			if w := runewidth.StringWidth(cells); w > 79 || !strings.Contains(cells, "2  Second") {
				t.Fatalf("row is %d columns or lost its neighbour: %q", w, cells)
			}
			return
		}
	}
	t.Fatalf("no wide name shown:\n%s", paint)
}
