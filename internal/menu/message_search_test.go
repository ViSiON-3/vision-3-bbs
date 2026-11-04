package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/msgsearch"
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
	r = env.runCmd("SEARCHMSGS", env.caller, "A", "purple\r")
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
	// 30 results at 17 a page: Enter shows page 2, Enter on the last page leaves
	r := env.runCmd("SEARCHMSGS", env.caller, "", "green\r\r\r")
	if !r.has("Page 1/2", "Page 2/2", "(30)") {
		t.Errorf("paging:\n%s", r.text())
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
	hits, full, stopped := searchMessages(env.e, all, q, accept, func() bool { return true })
	if len(hits) != 0 || full || !stopped {
		t.Errorf("stop at once: %d hits, full %v, stopped %v", len(hits), full, stopped)
	}
	hits, _, stopped = searchMessages(env.e, all, q, accept, func() bool { return false })
	if stopped || len(hits) != 3 {
		t.Errorf("no stop: %d hits (want 3), stopped %v", len(hits), stopped)
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
