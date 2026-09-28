package menu

import (
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// seedRumors writes rd as the env's rumors.json.
func seedRumors(t *testing.T, env *menuEnv, rd *rumorsData) {
	t.Helper()
	if err := saveRumorsData(env.cfgDir(), rd); err != nil {
		t.Fatalf("seed rumors: %v", err)
	}
}

// loadEnvRumors reloads rumors.json the way the handlers do.
func loadEnvRumors(t *testing.T, env *menuEnv) *rumorsData {
	t.Helper()
	rd, err := loadRumorsData(env.cfgDir())
	if err != nil {
		t.Fatalf("reload rumors: %v", err)
	}
	return rd
}

// rumorFixture is a board with a public rumor, a public anonymous rumor (under
// the configured anonymous name) and a
// rumor only level 100+ may see.
func rumorFixture(env *menuEnv, posted time.Time) *rumorsData {
	return &rumorsData{NextID: 4, Rumors: []RumorRecord{
		{ID: 1, Author: "Sysop", RealUser: "Sysop", UserID: 1, Text: "The modem pool is haunted", PostedAt: posted, MinLevel: 1},
		{ID: 2, Author: rumorAnonName(env.e), RealUser: "Caller", UserID: 2, Text: "Someone ate the donuts", PostedAt: posted, MinLevel: 1},
		{ID: 3, Author: "Sysop", RealUser: "Sysop", UserID: 1, Text: "Secret upgrade on Friday", PostedAt: posted, MinLevel: 100},
	}}
}

// A caller lists only the rumors their level allows, and anonymous authors
// stay anonymous; the sysop sees everything, with the real poster unmasked.
func TestRumorsListHonoursLevelAndAnonymity(t *testing.T) {
	env := newMenuEnv(t)
	seedRumors(t, env, rumorFixture(env, time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)))

	r := env.runCmd("RUMORSLIST", env.caller, "", "\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("The modem pool is haunted", "Someone ate the donuts", "03/04/26") {
		t.Errorf("caller list missing visible rumors:\n%s", r.text())
	}
	if r.has("Secret upgrade") {
		t.Errorf("caller saw a level-100 rumor:\n%s", r.text())
	}
	if r.has(rumorAnonName(env.e) + " (Caller)") {
		t.Errorf("caller saw the real name behind an anonymous rumor")
	}

	// The sysop sees every rumor. The author column is cut to 15 runes, which
	// the stock "Anonymous Coward" name already fills, so the real poster is
	// shown on a line of its own under it.
	r = env.runCmd("RUMORSLIST", env.sysop, "", "\r")
	if !r.has("Secret upgrade on Friday", "Anonymous Co...") {
		t.Errorf("sysop list should show every rumor:\n%s", r.text())
	}
	if !r.has("(Caller)") {
		t.Errorf("sysop list should unmask the anonymous poster:\n%s", r.text())
	}
	if n := strings.Count(r.text(), "(Sysop)"); n != 0 {
		t.Errorf("sysop list unmasked %d rumors posted under the poster's own name:\n%s", n, r.text())
	}
	for _, line := range strings.Split(r.text(), "\n") {
		if w := utf8.RuneCountInString(strings.TrimRight(line, "\r")); w >= 80 {
			t.Errorf("list line is %d columns wide, want < 80: %q", w, line)
		}
	}
}

// A rumor posted under an anonymous name the board has since changed is
// still masked: the sysop sees who posted it, a caller does not.
func TestRumorsUnmaskEarlierAnonymousName(t *testing.T) {
	env := newMenuEnv(t)
	seedRumors(t, env, &rumorsData{NextID: 2, Rumors: []RumorRecord{
		{ID: 1, Author: "Mystery Guest", RealUser: "Caller", UserID: 2, Text: "Old anonymous rumor", PostedAt: time.Now(), MinLevel: 1},
	}})

	if r := env.runCmd("RUMORSLIST", env.sysop, "", "\r"); !r.has("Mystery Guest", "(Caller)") {
		t.Errorf("sysop list should unmask the poster:\n%s", r.text())
	}
	if r := env.runCmd("RUMORSSEARCH", env.sysop, "", "old\r"); !r.has("by Mystery Guest (Caller)") {
		t.Errorf("sysop search should unmask the poster: %q", r.text())
	}
	for _, cmd := range []string{"RUMORSLIST", "RUMORSSEARCH"} {
		if r := env.runCmd(cmd, env.caller, "", "old\r"); !r.has("Mystery Guest") || r.has("Caller") {
			t.Errorf("%s as caller: %q", cmd, r.text())
		}
	}
}

// With nothing posted (or nothing the caller may see) the list says so, and a
// caller who has not logged in gets no output at all.
func TestRumorsListEmptyAndLoggedOut(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("RUMORSLIST", env.caller, "", ""); !r.has("There are no rumors!") {
		t.Errorf("empty board: %q", r.text())
	}

	seedRumors(t, env, &rumorsData{NextID: 2, Rumors: []RumorRecord{
		{ID: 1, Author: "Sysop", Text: "hidden", MinLevel: 200},
	}})
	if r := env.runCmd("RUMORSLIST", env.caller, "", ""); !r.has("There are no rumors!") || r.has("hidden") {
		t.Errorf("only-hidden board: %q", r.text())
	}

	if r := env.runCmd("RUMORSLIST", nil, "", ""); r.raw != "" || r.user != nil {
		t.Errorf("logged-out run wrote %q / returned user %v", r.raw, r.user)
	}
}

// A corrupt rumors.json is reported rather than treated as an empty board.
func TestRumorsHandlersReportCorruptFile(t *testing.T) {
	env := newMenuEnv(t)
	if err := os.WriteFile(rumorsFilePath(env.cfgDir()), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"RUMORSLIST", "RUMORSSEARCH", "RUMORSNEWSCAN", "RUMORSADD", "RUMORSDELETE"} {
		if r := env.runCmd(cmd, env.caller, "", ""); !r.has("Error loading rumors.") {
			t.Errorf("%s on a corrupt file: %q", cmd, r.text())
		}
	}
}

// Search matches text and author case-insensitively, skips rumors above the
// caller's level, and says so when nothing matches.
func TestRumorsSearch(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("RUMORSSEARCH", env.caller, "", "x\r"); !r.has("No rumors exist!") {
		t.Errorf("empty board: %q", r.text())
	}

	seedRumors(t, env, rumorFixture(env, time.Now()))

	r := env.runCmd("RUMORSSEARCH", env.caller, "", "DONUTS\r")
	if !r.has("Someone ate the donuts", "by "+rumorAnonName(env.e)) || r.has("modem pool") {
		t.Errorf("text search: %q", r.text())
	}

	// The sysop sees who is behind an anonymous rumor.
	r = env.runCmd("RUMORSSEARCH", env.sysop, "", "donuts\r")
	if !r.has("by " + rumorAnonName(env.e) + " (Caller)") {
		t.Errorf("sysop search should unmask the poster: %q", r.text())
	}

	// By author: both Sysop rumors match, but the caller may see only one.
	r = env.runCmd("RUMORSSEARCH", env.caller, "", "sysop\r")
	if !r.has("The modem pool is haunted") || r.has("Secret upgrade") {
		t.Errorf("author search as caller: %q", r.text())
	}
	r = env.runCmd("RUMORSSEARCH", env.sysop, "", "sysop\r")
	if !r.has("The modem pool is haunted", "Secret upgrade on Friday") {
		t.Errorf("author search as sysop: %q", r.text())
	}

	r = env.runCmd("RUMORSSEARCH", env.caller, "", "secret\r")
	if !r.has("No matching rumors found.") || r.has("Secret upgrade") {
		t.Errorf("search hitting only a hidden rumor: %q", r.text())
	}

	// A blank search just returns.
	r = env.runCmd("RUMORSSEARCH", env.caller, "", "   \r")
	if r.has("No matching") || r.has(" by ") {
		t.Errorf("blank search produced results: %q", r.text())
	}
}

// Newscan lists only rumors posted after the caller's previous login, and
// treats a caller with no previous login as having everything new.
func TestRumorsNewscanUsesPreviousLogin(t *testing.T) {
	env := newMenuEnv(t)
	last := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rd := rumorFixture(env, last.Add(-time.Hour))
	rd.Rumors = append(rd.Rumors, RumorRecord{ID: 4, Author: "Sysop", Text: "Fresh gossip", PostedAt: last.Add(time.Hour), MinLevel: 1})
	rd.NextID = 5
	seedRumors(t, env, rd)

	env.caller.PreviousLogin = last
	r := env.runCmd("RUMORSNEWSCAN", env.caller, "", "\r")
	if !r.has("Rumors Newscan", "Fresh gossip") || r.has("modem pool") {
		t.Errorf("newscan since last login: %q", r.text())
	}

	env.caller.PreviousLogin = last.Add(2 * time.Hour)
	r = env.runCmd("RUMORSNEWSCAN", env.caller, "", "\r")
	if !r.has("No new rumors since your last login.") {
		t.Errorf("nothing new: %q", r.text())
	}

	env.caller.PreviousLogin = time.Time{}
	r = env.runCmd("RUMORSNEWSCAN", env.caller, "", "\r")
	if !r.has("Fresh gossip", "The modem pool is haunted") || r.has("Secret upgrade") {
		t.Errorf("first-time caller newscan: %q", r.text())
	}
}

// The login-sequence random rumor shows one rumor the caller may see,
// bracketed and centred, and stays silent when there is nothing to show.
func TestRandomRumor(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("RANDOMRUMOR", env.caller, "", ""); r.raw != "" {
		t.Errorf("no rumors should print nothing, got %q", r.raw)
	}

	seedRumors(t, env, &rumorsData{NextID: 3, Rumors: []RumorRecord{
		{ID: 1, Text: "only one visible", MinLevel: 1},
		{ID: 2, Text: "not for you", MinLevel: 200},
	}})
	for i := 0; i < 5; i++ {
		r := env.runCmd("RANDOMRUMOR", env.caller, "", "")
		txt := r.text()
		if !strings.Contains(txt, "[ only one visible ]") {
			t.Fatalf("random rumor: %q", txt)
		}
		// 80 cols, 20 visible chars -> 30 columns of padding.
		if !strings.Contains(txt, strings.Repeat(" ", 30)+"[ only") {
			t.Errorf("rumor not centred: %q", txt)
		}
	}

	if r := env.runCmd("RANDOMRUMOR", nil, "", ""); r.raw != "" {
		t.Errorf("logged-out run wrote %q", r.raw)
	}
}
