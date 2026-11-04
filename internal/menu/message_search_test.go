package menu

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/msgsearch"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// addSearchArea adds a local area in conference confID with read ACS acs
// and posts one public message per subject, from Sysop to All, with the
// body "<subject> body".
func addSearchArea(env *menuEnv, tag string, confID int, acs string, subjects ...string) int {
	env.t.Helper()
	id, err := env.e.MessageMgr.AddArea(message.MessageArea{
		Tag: tag, Name: tag + " Area", AreaType: "local", ConferenceID: confID,
		ACSRead: acs, ACSWrite: acs,
	})
	if err != nil {
		env.t.Fatalf("AddArea %s: %v", tag, err)
	}
	for _, s := range subjects {
		if _, err := env.e.MessageMgr.AddMessage(id, "Sysop", "All", s, s+" body", ""); err != nil {
			env.t.Fatal(err)
		}
	}
	return id
}

// searchEnv is a board with two conferences of areas, every message
// mentioning "blue" except where noted:
//
//	LOCAL (1)     GENERAL: "general blue one", "general red two"
//	              ALPHA:   "alpha blue"
//	FELONYNET (2) BETA:    "beta blue"
func searchEnv(t *testing.T) *menuEnv {
	t.Helper()
	withScanNoticePause(t)
	env := newMsgEnv(t)
	for _, s := range []string{"general blue one", "general red two"} {
		if _, err := env.e.MessageMgr.AddMessage(generalAreaID, "Sysop", "All", s, s+" body", ""); err != nil {
			t.Fatal(err)
		}
	}
	addSearchArea(env, "ALPHA", 1, "s10", "alpha blue")
	addSearchArea(env, "BETA", 2, "s10", "beta blue")
	return env
}

func TestSearchMsgsScopes(t *testing.T) {
	env := searchEnv(t)
	tests := []struct {
		args      string
		want, not []string
	}{
		{"", []string{"general blue one", "alpha blue"}, []string{"beta blue"}},
		{"G", []string{"general blue one", "alpha blue"}, []string{"beta blue"}},
		{"c", []string{"general blue one"}, []string{"alpha blue", "beta blue"}},
		{"A", []string{"general blue one", "alpha blue", "beta blue"}, nil},
	}
	for _, tt := range tests {
		r := env.runCmd("SEARCHMSGS", env.caller, tt.args, "blue\rQ\r")
		if r.err != nil {
			t.Fatalf("SEARCHMSGS %q: %v", tt.args, r.err)
		}
		for _, s := range tt.want {
			if !r.has(s) {
				t.Errorf("scope %q: %q not listed", tt.args, s)
			}
		}
		for _, s := range tt.not {
			if r.has(s) {
				t.Errorf("scope %q: %q listed", tt.args, s)
			}
		}
		if r.has("general red two") {
			t.Errorf("scope %q: a message without the word was listed", tt.args)
		}
	}
}

func TestSearchMsgsBooleanQuery(t *testing.T) {
	env := searchEnv(t)
	r := env.runCmd("SEARCHMSGS", env.caller, "A", "(general OR beta) AND NOT red\rQ\r")
	if !r.has("general blue one", "beta blue", "(2)") {
		t.Errorf("expected the two matches:\n%s", r.text())
	}
	if r.has("alpha blue") || r.has("general red two") {
		t.Errorf("listed a message the query excludes:\n%s", r.text())
	}
}

// Areas and conferences the caller may not read, other people's private
// mail and deleted messages never appear, and aren't counted.
func TestSearchMsgsHidesWhatTheCallerCantRead(t *testing.T) {
	env := searchEnv(t)
	addSearchArea(env, "LOCKED", 1, "s200", "locked blue")
	conf := `[
 {"id": 1, "position": 1, "tag": "LOCAL", "name": "Local", "acs": "s10"},
 {"id": 2, "position": 2, "tag": "FELONYNET", "name": "FelonyNet", "acs": "s10"},
 {"id": 3, "position": 3, "tag": "STAFF", "name": "Staff", "acs": "s200"}
]`
	if err := os.WriteFile(filepath.Join(env.cfgDir(), "conferences.json"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := env.e.ConferenceMgr.Reload(); err != nil {
		t.Fatal(err)
	}
	addSearchArea(env, "STAFFTALK", 3, "s10", "staff blue")
	env.postMsgs(privmailAreaID,
		testMsg{from: "Sysop", to: "Sysop", subject: "sysop private blue", private: true},
		testMsg{from: "Sysop", to: "Caller", subject: "caller private blue", private: true})
	gone, err := env.e.MessageMgr.AddMessage(generalAreaID, "Sysop", "All", "deleted blue", "deleted blue body", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.e.MessageMgr.DeleteMessage(generalAreaID, gone); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("SEARCHMSGS", env.caller, "A", "blue\rQ\r")
	for _, hidden := range []string{"locked blue", "staff blue", "sysop private blue", "deleted blue"} {
		if r.has(hidden) {
			t.Errorf("%q was listed to the caller", hidden)
		}
	}
	// general blue one, alpha blue, beta blue, caller private blue
	if !r.has("caller private blue", "(4)") {
		t.Errorf("want the caller's own private mail and a count of 4:\n%s", r.text())
	}

	// the sysop sees the locked area, the staff conference and their mail
	r = env.runCmd("SEARCHMSGS", env.sysop, "A", "blue\rQ\r")
	if !r.has("locked blue", "staff blue", "sysop private blue") {
		t.Errorf("the sysop's search is missing areas:\n%s", r.text())
	}
	if r.has("deleted blue") {
		t.Error("a deleted message was listed to the sysop")
	}
}

// Searching finds messages already read, and searching, listing and
// reading a result leave the last-read pointers and the caller's current
// area as they were.
func TestSearchMsgsLeavesPointersAndArea(t *testing.T) {
	env := searchEnv(t)
	env.markRead(generalAreaID, env.caller.Handle, 2) // all of GENERAL read
	alpha, _ := env.e.MessageMgr.GetAreaByTag("ALPHA")
	env.markRead(alpha.ID, env.caller.Handle, 0)

	// open result 2 (alpha blue), Q out of the reader, Q out of the list
	r := env.runCmd("SEARCHMSGS", env.caller, "", "blue\r2\rQQ\r")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !r.has("alpha blue body") {
		t.Fatalf("the result wasn't opened:\n%s", r.text())
	}
	if !r.has("general blue one") {
		t.Errorf("a message already read wasn't found:\n%s", r.text())
	}
	if lr := env.diskLastRead(alpha.ID, env.caller.Handle); lr != 0 {
		t.Errorf("ALPHA last-read = %d after reading a result, want 0", lr)
	}
	if lr := env.diskLastRead(generalAreaID, env.caller.Handle); lr != 2 {
		t.Errorf("GENERAL last-read = %d, want 2", lr)
	}
	u := env.mustDiskUser(env.caller.ID)
	if u.CurrentMessageAreaID != generalAreaID || u.CurrentMsgConferenceID != 1 {
		t.Errorf("current area is %d in conference %d, want GENERAL in 1", u.CurrentMessageAreaID, u.CurrentMsgConferenceID)
	}
}

// In the reader, N moves only between matching messages of the area.
func TestSearchMsgsReaderStaysOnMatches(t *testing.T) {
	env := searchEnv(t)
	for _, s := range []string{"general red three", "general blue four"} {
		if _, err := env.e.MessageMgr.AddMessage(generalAreaID, "Sysop", "All", s, s+" body", ""); err != nil {
			t.Fatal(err)
		}
	}
	// result 1 is "general blue one"; N goes to "general blue four"
	r := env.runCmd("SEARCHMSGS", env.caller, "C", "blue\r1\rNQQ\r")
	if !r.has("general blue one body", "general blue four body") {
		t.Errorf("N didn't move to the next match:\n%s", r.text())
	}
	if r.has("general red two body") || r.has("general red three body") {
		t.Errorf("the reader showed a message that doesn't match:\n%s", r.text())
	}
}

func TestSearchMsgsInvalidAndEmpty(t *testing.T) {
	env := searchEnv(t)
	r := env.runCmd("SEARCHMSGS", env.caller, "A", "(blue\r")
	if !r.has("Can't search for that: unmatched ( at 1") {
		t.Errorf("no parse error shown:\n%s", r.text())
	}
	r = env.runCmd("SEARCHMSGS", env.caller, "A", "purple\rQ")
	if !r.has("No messages match your search") {
		t.Errorf("no no-match notice:\n%s", r.text())
	}
	r = env.runCmd("SEARCHMSGS", env.caller, "A", "\r")
	if r.has("Messages matching") || r.has("No messages match") {
		t.Errorf("an empty search searched:\n%s", r.text())
	}
	r = env.runCmd("SEARCHMSGS", nil, "A", "")
	if !strings.Contains(r.text(), "logged in") {
		t.Errorf("logged-out caller wasn't refused:\n%s", r.text())
	}
}

func TestSearchMsgsPages(t *testing.T) {
	env := searchEnv(t)
	var subjects []string
	for i := 0; i < 30; i++ {
		subjects = append(subjects, "many green "+strings.Repeat("x", i%3)+string(rune('a'+i%26)))
	}
	addSearchArea(env, "MANY", 1, "s10", subjects...)
	// 30 results at 17 a page: Enter shows page 2, and Enter on the last
	// page stays there, as N does, until Q
	r := env.runCmd("SEARCHMSGS", env.caller, "", "green\r\r\r\rQ\r")
	if !r.has("Page 1/2", "Page 2/2", "(30)") {
		t.Errorf("paging:\n%s", r.text())
	}
	if n := strings.Count(r.text(), "Page 2/2"); n != 3 {
		t.Errorf("Page 2/2 shown %d times, want 3 (Enter on the last page stays):\n%s", n, r.text())
	}
	if r.next == "LOGOFF" {
		t.Error("the list ended before Q")
	}

	// one page: Enter shows it again
	r = env.runCmd("SEARCHMSGS", env.caller, "C", "blue\r\rQ\r")
	if n := strings.Count(r.text(), "Page 1/1"); n != 2 || r.next == "LOGOFF" {
		t.Errorf("one page: Page 1/1 shown %d times (want 2), next %q", n, r.next)
	}
}

func TestParseMsgSearchScope(t *testing.T) {
	for in, want := range map[string]msgSearchScope{
		"": msgSearchConference, "G": msgSearchConference, "g": msgSearchConference,
		"C": msgSearchArea, " c ": msgSearchArea, "A": msgSearchAll, "x": msgSearchConference,
	} {
		if got := parseMsgSearchScope(in); got != want {
			t.Errorf("parseMsgSearchScope(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSearchMessagesStops(t *testing.T) {
	env := searchEnv(t)
	q := mustQuery(t, "blue")
	all := env.e.MessageMgr.ListAreas()
	accept := func(*message.DisplayMessage) bool { return true }
	hits, full, stopped := searchMessages(env.e, all, q, accept, accept, func() bool { return true })
	if len(hits) != 0 || full || !stopped {
		t.Errorf("stop at once: %d hits, full %v, stopped %v", len(hits), full, stopped)
	}
	hits, _, stopped = searchMessages(env.e, all, q, accept, accept, func() bool { return false })
	if stopped || len(hits) != 3 {
		t.Errorf("no stop: %d hits (want 3), stopped %v", len(hits), stopped)
	}
}

// The line under the results header says why the list is short: the cap
// asks the caller to narrow the search, a stopped search says it was
// stopped, and a search that ran to the end says nothing.
func TestMsgSearchResultsNotices(t *testing.T) {
	env := searchEnv(t)
	q := mustQuery(t, "blue")
	accept := func(*message.DisplayMessage) bool { return true }
	hits, _, _ := searchMessages(env.e, env.e.MessageMgr.ListAreas(), q, accept, accept, func() bool { return false })
	if len(hits) != 3 {
		t.Fatalf("%d hits, want 3", len(hits))
	}
	const capped, stoppedEarly = "Only the first 3 matches are shown", "Search stopped; matches found so far: 3."
	for _, tc := range []struct {
		name          string
		full, stopped bool
		want, notWant []string
	}{
		{"capped", true, false, []string{capped}, []string{stoppedEarly}},
		{"stopped", false, true, []string{stoppedEarly}, []string{capped}},
		{"complete", false, false, nil, []string{capped, stoppedEarly}},
	} {
		r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
			return runMsgSearchResults(c, q, hits, tc.full, tc.stopped)
		}, env.caller, "", "Q")
		if !r.has("Messages matching") {
			t.Errorf("%s: no results header:\n%s", tc.name, r.text())
		}
		for _, w := range tc.want {
			if !r.has(w) {
				t.Errorf("%s: %q not shown:\n%s", tc.name, w, r.text())
			}
		}
		for _, w := range tc.notWant {
			if r.has(w) {
				t.Errorf("%s: %q shown:\n%s", tc.name, w, r.text())
			}
		}
	}
}

func mustQuery(t *testing.T, s string) *msgsearch.Query {
	t.Helper()
	q, err := msgsearch.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return q
}

// A caller who hangs up during a search stops it: it doesn't go on reading
// every base for nobody.
func TestSearchMsgsStopsWhenTheCallerHangsUp(t *testing.T) {
	env := searchEnv(t)
	r := env.runCmd("SEARCHMSGS", env.caller, "A", "blue\r") // then the connection closes
	if !r.has("Search stopped") || r.has("Messages matching") {
		t.Errorf("the search ran on after the caller left:\n%s", r.text())
	}
}

func TestMsgSearchStopKey(t *testing.T) {
	newIH := func(t *testing.T, input string, closeAfter bool) *editor.InputHandler {
		t.Helper()
		pr, pw := io.Pipe()
		ih := editor.NewInputHandler(pr)
		ih.SetEscTimeout(50 * time.Millisecond)
		t.Cleanup(func() { _ = pw.Close(); ih.Close() })
		go func() {
			_, _ = pw.Write([]byte(input))
			if closeAfter {
				_ = pw.Close()
			}
		}()
		if !ih.WaitForInput(time.Second) {
			t.Fatal("no input arrived")
		}
		return ih
	}

	t.Run("closed connection", func(t *testing.T) {
		ih := newIH(t, "", true)
		if !msgSearchStopKey(ih) {
			t.Error("a closed connection didn't stop the search")
		}
	})
	t.Run("Esc", func(t *testing.T) {
		if !msgSearchStopKey(newIH(t, "\x1b", false)) {
			t.Error("Esc didn't stop the search")
		}
	})
	t.Run("Ctrl-C", func(t *testing.T) {
		if !msgSearchStopKey(newIH(t, "\x03", false)) {
			t.Error("Ctrl-C didn't stop the search")
		}
	})
	// type-ahead, including a key that starts with Esc, is left whole
	for name, tc := range map[string]struct {
		input string
		key   int
	}{
		"digit": {"7", '7'},
		"PgDn":  {"\x1b[6~", editor.KeyPageDown},
		"Up":    {"\x1b[A", editor.KeyArrowUp},
	} {
		t.Run(name, func(t *testing.T) {
			ih := newIH(t, tc.input, false)
			if msgSearchStopKey(ih) {
				t.Fatal("stopped the search")
			}
			if key, err := ih.ReadKey(); err != nil || key != tc.key {
				t.Errorf("next key = %#x, %v; want %#x", key, err, tc.key)
			}
		})
	}
}

// A colour code in a subject or sender prints as typed and doesn't move
// the columns after it or recolour them.
func TestMsgSearchRowEscapesPipes(t *testing.T) {
	row := func(subject, from string) string {
		t.Helper()
		ts := newTestSession("")
		writeMsgSearchRow(newTestTerminal(ts), ansi.OutputModeUTF8, 1, msgSearchHit{
			confTag: "LOCAL", areaTag: "GENERAL", msgNum: 7, subject: subject, from: from,
			dateTime: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC),
		})
		return ts.output()
	}
	plain := testAnsiEscape.ReplaceAllString(row("plain subject", "Sysop"), "")
	raw := row("|12URGENT|07 meeting", "|14Sy|Bsop")
	coloured := testAnsiEscape.ReplaceAllString(raw, "")
	if !strings.Contains(coloured, "|12URGENT|07 meeting") || !strings.Contains(coloured, "|14Sy|Bsop") {
		t.Errorf("the codes weren't printed as typed: %q", coloured)
	}
	if strings.Contains(raw, string(ansi.ReplacePipeCodes([]byte("|12")))) || strings.Contains(raw, string(ansi.ReplacePipeCodes([]byte("|14")))) {
		t.Errorf("a code from the message coloured the row: %q", raw)
	}
	if a, b := strings.Index(plain, "11/04/26"), strings.Index(coloured, "11/04/26"); a < 0 || a != b {
		t.Errorf("the date moved: column %d, want %d\n%q\n%q", b, a, plain, coloured)
	}
}

// Opening a result shows the whole thread in the pane, not only the replies
// that match the search.
func TestSearchMsgsThreadPaneIsNotFiltered(t *testing.T) {
	withScanNoticePause(t)
	env := newMsgEnv(t)
	env.seedSampleThread()
	// only #5 says "Font sticks"
	r := env.runCmd("SEARCHMSGS", env.caller, "C", "\"Font sticks\"\r1\rQQ\r")
	if !r.has("Reading message #5 ") {
		t.Fatalf("the result wasn't opened:\n%s", r.text())
	}
	for _, reply := range []string{"#1 Phosphor", "#3 Kestrel_", "#7 Mercury "} {
		if !r.has(reply) {
			t.Errorf("the thread pane left out %s:\n%s", reply, r.text())
		}
	}
}
