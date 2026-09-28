package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestRunOneliners_UsesServerDataDir drives ONELINER end to end with a DataDir
// that is not ./data. The wall must be read from and saved to DataDir, and
// nothing may be created relative to the working directory (#450).
func TestRunOneliners_UsesServerDataDir(t *testing.T) {
	root := t.TempDir()
	menuSet := filepath.Join(root, "menus")
	dataDir := filepath.Join(root, "boarddata")
	for _, d := range []string{filepath.Join(menuSet, "templates"), dataDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	for name, body := range map[string]string{
		"ONELINER.TOP": "TOP\r\n",
		"ONELINER.MID": "^NU: ^OL\r\n",
		"ONELINER.BOT": "BOT\r\n",
	} {
		if err := os.WriteFile(filepath.Join(menuSet, "templates", name), []byte(body), 0644); err != nil {
			t.Fatalf("write template: %v", err)
		}
	}
	onelinerPath := filepath.Join(dataDir, "oneliners.json")
	seed := `[{"text":"seeded in datadir","posted_by_username":"Sysop","posted_by_handle":"Sysop"}]`
	if err := os.WriteFile(onelinerPath, []byte(seed), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Run from an empty working directory so a cwd-relative path would show.
	cwd := t.TempDir()
	t.Chdir(cwd)

	e := &MenuExecutor{MenuSetPath: menuSet}
	e.SetStrings(config.StringsConfig{
		AskOneLiner:       "Add one? @",
		EnterOneLiner:     "Say: ",
		ExecOnelinerAdded: "Added.",
	})
	e.SetServerConfig(config.ServerConfig{DataDir: dataDir, AnonymousLevel: 255})

	// Y answers the add prompt; the new line follows, ended by CR.
	ts := newTestSession("yposted via test\r")
	t.Cleanup(func() { resetSessionIH(ts) })
	c := &cmdCtx{
		e:                e,
		s:                ts,
		terminal:         newTestTerminal(ts),
		currentUser:      &user.User{Handle: "Tester", AccessLevel: 10},
		nodeNumber:       1,
		sessionStartTime: time.Now(),
		outputMode:       ansi.OutputModeCP437,
		termWidth:        80,
		termHeight:       24,
	}

	if _, _, err := runOneliners(c, ""); err != nil {
		t.Fatalf("runOneliners: %v", err)
	}

	if out := ts.output(); !strings.Contains(out, "seeded in datadir") {
		t.Errorf("wall did not show the entry stored under DataDir:\n%q", out)
	}

	records, err := loadOnelinerRecords(onelinerPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(records) != 2 || records[1].Text != "posted via test" {
		t.Errorf("DataDir oneliners = %+v, want seeded entry plus %q", records, "posted via test")
	}

	entries, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatalf("read cwd: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("working directory gained %d entries (first %q); oneliners must live under DataDir", len(entries), entries[0].Name())
	}
}

func TestOnelinerFilePath(t *testing.T) {
	if got, want := onelinerFilePath("/srv/bbs/data"), filepath.Join("/srv/bbs/data", "oneliners.json"); got != want {
		t.Errorf("onelinerFilePath = %q, want %q", got, want)
	}
	if got, want := onelinerFilePath(""), filepath.Join("data", "oneliners.json"); got != want {
		t.Errorf("onelinerFilePath(\"\") = %q, want %q", got, want)
	}
}
