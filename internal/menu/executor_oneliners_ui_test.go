package menu

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// onelinerEnv builds a menuEnv and returns it with the path of its oneliners
// file, which lives under the env's DataDir.
func onelinerEnv(t *testing.T) (*menuEnv, string) {
	t.Helper()
	env := newMenuEnv(t)
	return env, onelinerFilePath(env.dataDir())
}

// An empty wall invites the first post, and declining leaves no file behind.
func TestOnelinerEmptyWallDecline(t *testing.T) {
	env, path := onelinerEnv(t)
	r := env.runCmd("ONELINER", env.caller, "", "N")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("No one-liners yet. Be the first!", "Add a One Liner?") {
		t.Errorf("empty wall:\n%s", r.text())
	}
	if r.has("What do you say") {
		t.Errorf("declining still asked for text")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("oneliners.json written after declining (stat err %v)", err)
	}
}

// Only the newest ten one-liners are shown, and anonymous ones show the
// configured anonymous name instead of the poster.
func TestOnelinerShowsNewestTenAndHidesAnonymousPoster(t *testing.T) {
	env, path := onelinerEnv(t)
	var recs []onelinerRecord
	for i := 1; i <= 12; i++ {
		recs = append(recs, onelinerRecord{Text: fmt.Sprintf("line number %02d", i), PostedByHandle: "Poster"})
	}
	recs[11].Anonymous = true
	recs[11].PostedByHandle = "SecretPoster"
	if err := saveOnelinerRecords(path, recs); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("ONELINER", env.caller, "", "N")
	if r.has("line number 01") || r.has("line number 02") {
		t.Errorf("older than the newest ten shown:\n%s", r.text())
	}
	if !r.has("line number 03", "line number 12", "Anonymous Coward") {
		t.Errorf("newest ten / anonymous name missing:\n%s", r.text())
	}
	if r.has("SecretPoster") {
		t.Errorf("anonymous poster's handle shown")
	}
}

// A caller's post is appended under their handle; they are below the
// anonymous level so are never offered anonymity.
func TestOnelinerAddAsCaller(t *testing.T) {
	env, path := onelinerEnv(t)
	if err := saveOnelinerRecords(path, []onelinerRecord{{Text: "first!", PostedByHandle: "Sysop"}}); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("ONELINER", env.caller, "", "Y  |12hello wall  \r")
	if r.err != nil || !r.has("Oneliner added!") {
		t.Fatalf("err=%v output:\n%s", r.err, r.text())
	}
	if r.has("nonymous?") {
		t.Errorf("level-10 caller offered anonymity")
	}

	recs, err := loadOnelinerRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("records = %+v, want 2", recs)
	}
	got := recs[1]
	if got.Text != "|12hello wall" || got.Anonymous || got.PostedByHandle != "Caller" || got.PostedAt == "" {
		t.Errorf("new record = %+v", got)
	}
}

// A sysop may post anonymously; the record keeps the real handle but is
// flagged anonymous.
func TestOnelinerAddAnonymousAsSysop(t *testing.T) {
	env, path := onelinerEnv(t)
	r := env.runCmd("ONELINER", env.sysop, "", "YYwho said that\r")
	if !r.has("nonymous", "Oneliner added!") {
		t.Errorf("anonymous add:\n%s", r.text())
	}
	recs, err := loadOnelinerRecords(path)
	if err != nil || len(recs) != 1 {
		t.Fatalf("records = %+v err = %v", recs, err)
	}
	if !recs[0].Anonymous || recs[0].PostedByHandle != "Sysop" || recs[0].Text != "who said that" {
		t.Errorf("record = %+v", recs[0])
	}
}

// Background and extended colour codes are refused and nothing is saved.
func TestOnelinerRejectsDisallowedColour(t *testing.T) {
	env, path := onelinerEnv(t)
	r := env.runCmd("ONELINER", env.caller, "", "Y|20bad colour\r")
	if !r.has("Only standard foreground colors") || r.has("Oneliner added!") {
		t.Errorf("colour check:\n%s", r.text())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("oneliners.json written for a rejected post (stat err %v)", err)
	}
}

// A blank post is reported and not saved.
func TestOnelinerRejectsBlank(t *testing.T) {
	env, path := onelinerEnv(t)
	r := env.runCmd("ONELINER", env.caller, "", "Y   \r")
	if !r.has("Empty oneliner not added.") {
		t.Errorf("blank post:\n%s", r.text())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("oneliners.json written for a blank post (stat err %v)", err)
	}
}

// A disconnect at the add prompt logs the caller off, and one while typing
// saves nothing.
func TestOnelinerDisconnect(t *testing.T) {
	env, path := onelinerEnv(t)
	if r := env.runCmd("ONELINER", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect at prompt: next = %q, want LOGOFF", r.next)
	}
	r := env.runCmd("ONELINER", env.caller, "", "Yhalf typed")
	if r.next != "LOGOFF" {
		t.Errorf("disconnect while typing: next = %q, want LOGOFF", r.next)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("partial post saved (stat err %v)", err)
	}
	if strings.Contains(r.text(), "Oneliner added!") {
		t.Errorf("partial post reported as added")
	}
}
