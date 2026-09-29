package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// These tests run each file-backed menu feature on a board whose data
// directory (ServerConfig.DataDir) is not <RootConfigPath>/../data. The
// feature must read and write its file under DataDir and create nothing at
// the old configs-relative location (#484).
//
// Fixtures are written as raw files under DataDir, not through the feature's
// own load/save helpers, so a helper that resolved the wrong directory could
// not hide the mistake by seeding and reading the same wrong place.

// relocateDataDir points env's DataDir at a fresh directory away from
// <configs>/../data and returns it together with that old location. Call it
// before any fixture is written; users.json stays where newMenuEnv put it,
// already loaded into env.um.
func relocateDataDir(t *testing.T, env *menuEnv) (dataDir, oldDir string) {
	t.Helper()
	oldDir = filepath.Join(env.cfgDir(), "..", "data")
	dataDir = filepath.Join(t.TempDir(), "relocated-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setServerField(env.e, func(c *config.ServerConfig) { c.DataDir = dataDir })
	return dataDir, oldDir
}

// writeDataJSON marshals v into dataDir/rel, creating parent directories.
func writeDataJSON(t *testing.T, dataDir, rel string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	writeDataFile(t, dataDir, rel, string(b))
}

// writeDataFile writes body to dataDir/rel, creating parent directories.
func writeDataFile(t *testing.T, dataDir, rel, body string) {
	t.Helper()
	p := filepath.Join(dataDir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertStoredUnderDataDir fails unless rel exists under dataDir and not
// under oldDir.
func assertStoredUnderDataDir(t *testing.T, dataDir, oldDir, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dataDir, rel)); err != nil {
		t.Errorf("%s not written under DataDir: %v", rel, err)
	}
	if _, err := os.Stat(filepath.Join(oldDir, rel)); !os.IsNotExist(err) {
		t.Errorf("%s created at the old configs/../data location (stat err %v)", rel, err)
	}
}

func TestNewsUsesServerDataDir(t *testing.T) {
	env := newMenuEnv(t)
	dataDir, oldDir := relocateDataDir(t, env)
	writeDataJSON(t, dataDir, "news.json", NewsData{NextID: 2, Items: []NewsItem{
		{ID: 1, Title: "Relocated Item", From: "Sysop", When: time.Now(), Level: 0, Always: true, Body: "relocated body"},
	}})

	r := env.runCmd("LISTNEWS", env.caller, "", "1\r\r\r")
	if !r.has("Relocated Item", "relocated body") {
		t.Errorf("LISTNEWS did not read news.json from DataDir:\n%s", r.text())
	}
	if _, err := os.Stat(filepath.Join(oldDir, "news.json")); !os.IsNotExist(err) {
		t.Errorf("news.json created at the old location (stat err %v)", err)
	}
}

func TestVotingUsesServerDataDir(t *testing.T) {
	env := newMenuEnv(t)
	dataDir, oldDir := relocateDataDir(t, env)

	r := env.runCmd("VOTE", env.sysop, "", "Y\rBest color?\rY\rY\r10\rRed\rBlue\r\rQ\r")
	if !r.has("Topic created!") {
		t.Fatalf("create topic:\n%s", r.text())
	}
	assertStoredUnderDataDir(t, dataDir, oldDir, "voting.json")

	// A second session reads the topic back from DataDir.
	if r := env.runCmd("VOTE", env.caller, "", "Q\r"); !r.has("Best color?") {
		t.Errorf("VOTE did not list the topic stored under DataDir:\n%s", r.text())
	}
}

func TestRumorsUseServerDataDir(t *testing.T) {
	env := newMenuEnv(t)
	dataDir, oldDir := relocateDataDir(t, env)
	writeDataJSON(t, dataDir, "rumors.json", rumorsData{NextID: 2, Rumors: []RumorRecord{
		{ID: 1, Author: "Sysop", RealUser: "Sysop", UserID: 1, Text: "Relocated rumor", PostedAt: time.Now(), MinLevel: 1},
	}})

	if r := env.runCmd("RUMORSLIST", env.caller, "", "\r"); !r.has("Relocated rumor") {
		t.Errorf("RUMORSLIST did not read rumors.json from DataDir:\n%s", r.text())
	}
	if _, err := os.Stat(filepath.Join(oldDir, "rumors.json")); !os.IsNotExist(err) {
		t.Errorf("rumors.json created at the old location (stat err %v)", err)
	}
}

func TestInfoFormsUseServerDataDir(t *testing.T) {
	env := newMenuEnv(t)
	dataDir, oldDir := relocateDataDir(t, env)
	env.caller.Validated = false
	writeDataJSON(t, dataDir, filepath.Join("infoforms", "config.json"), InfoFormConfig{RequiredForms: "1"})
	writeDataFile(t, dataDir, filepath.Join("infoforms", "templates", "form_1.txt"), "Real name: *!")

	r := env.runCmd("INFOFORMREQUIRED", env.caller, "", "Carl Caller\r")
	if !r.has("Real name:") {
		t.Fatalf("INFOFORMREQUIRED did not use the form stored under DataDir:\n%s", r.text())
	}
	assertStoredUnderDataDir(t, dataDir, oldDir, filepath.Join("infoforms", "responses", "2_1.json"))
	if _, err := os.Stat(filepath.Join(oldDir, "infoforms")); !os.IsNotExist(err) {
		t.Errorf("infoforms/ created at the old location (stat err %v)", err)
	}
}

func TestBBSListUsesServerDataDir(t *testing.T) {
	env := newMenuEnv(t)
	dataDir, oldDir := relocateDataDir(t, env)
	input := "Relocated BBS\rbbs.example.org\r23\r\r\rSam\r\rA board\r"

	if r := env.runCmd("BBSLISTADD", env.caller, "", input); !r.has("Your entry has been added!") {
		t.Fatalf("BBSLISTADD:\n%s", r.text())
	}
	assertStoredUnderDataDir(t, dataDir, oldDir, "bbslist.json")
	if r := env.runCmd("BBSLIST", env.caller, "", "q"); !r.has("Relocated BBS") {
		t.Errorf("BBSLIST did not read the entry stored under DataDir:\n%s", r.text())
	}
}

func TestWantListUsesServerDataDir(t *testing.T) {
	env := newMenuEnv(t)
	dataDir, oldDir := relocateDataDir(t, env)

	env.runCmd("WANTLIST", env.caller, "", "DOOM2.ZIP\rneed it\r")
	assertStoredUnderDataDir(t, dataDir, oldDir, "wantlist.json")
	b, err := os.ReadFile(filepath.Join(dataDir, "wantlist.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wl wantListData
	if err := json.Unmarshal(b, &wl); err != nil || len(wl.Entries) != 1 || wl.Entries[0].Filename != "DOOM2.ZIP" {
		t.Errorf("DataDir want list = %+v (err %v), want the DOOM2.ZIP request", wl, err)
	}
}

func TestNUVUsesServerDataDir(t *testing.T) {
	env := newMenuEnv(t)
	dataDir, oldDir := relocateDataDir(t, env)

	if r := env.runCmd("LISTNUV", env.sysop, "", "ACaller\rQ"); !r.has("Added 'Caller' to NUV queue.") {
		t.Fatalf("LISTNUV add:\n%s", r.text())
	}
	assertStoredUnderDataDir(t, dataDir, oldDir, "nuv.json")

	// CHECKNUV at login finds the candidate in DataDir.
	enableNUV(env, 5, 5, true, false)
	if r := env.run(runCheckNUV, env.sysop, "", "N"); !r.has("You have NOT voted on 1 New Users.") {
		t.Errorf("CHECKNUV did not read nuv.json from DataDir:\n%s", r.text())
	}
}
