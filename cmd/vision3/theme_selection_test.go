package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
)

// themeBoard is a BBS root with the given themes under menus/ and a
// config.json holding configJSON ("" for none).
func themeBoard(t *testing.T, configJSON string, themes ...string) string {
	t.Helper()
	root := t.TempDir()
	dirs := []string{"configs"}
	for _, name := range themes {
		dirs = append(dirs, filepath.Join("menus", name))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if configJSON != "" {
		writeDoctorFixture(t, filepath.Join(root, "configs", "config.json"), configJSON)
	}
	return root
}

func preflightResultNamed(results []preflightResult, name string) (preflightResult, bool) {
	for _, r := range results {
		if r.name == name {
			return r, true
		}
	}
	return preflightResult{}, false
}

// Preflight checks the menu set of the selected theme: v3 when theme.name
// is omitted, and no other theme's directory stands in for one that is
// missing or misnamed.
func TestPreflightChecksTheSelectedTheme(t *testing.T) {
	root := themeBoard(t, "", "v3", "amber")
	if r, ok := preflightResultNamed(collectPreflightResults(root), "menus/v3/"); !ok || !r.ok {
		t.Errorf("no theme.name: menus/v3/ = %+v (found %v)", r, ok)
	}

	root = themeBoard(t, `{"theme": {"name": "amber"}}`, "v3", "amber")
	results := collectPreflightResults(root)
	if r, ok := preflightResultNamed(results, "menus/amber/"); !ok || !r.ok {
		t.Errorf("amber: menus/amber/ = %+v (found %v)", r, ok)
	}
	if _, ok := preflightResultNamed(results, "menus/v3/"); ok {
		t.Error("amber selected, but menus/v3/ was checked")
	}

	for name, js := range map[string]string{
		"missing": `{"theme": {"name": "neon"}}`,
		"invalid": `{"theme": {"name": "../v3"}}`,
	} {
		root = themeBoard(t, js, "v3")
		r, ok := preflightResultNamed(collectPreflightResults(root), themeCheckName)
		if !ok || r.ok || !r.critical || r.message == "" {
			t.Errorf("%s theme: %+v (found %v), want a critical failure with a message", name, r, ok)
		}
	}

	// The default theme missing is the setup problem it always was.
	root = themeBoard(t, "")
	if r, ok := preflightResultNamed(collectPreflightResults(root), menusCheckName); !ok || r.ok || !r.critical {
		t.Errorf("no menus at all: %+v (found %v)", r, ok)
	}
}

// Doctor reports the selected theme, and fails on a selection the server
// would refuse to start with.
func TestDoctorReportsTheTheme(t *testing.T) {
	report := inspectBoard(themeBoard(t, `{"theme": {"name": "amber"}}`, "v3", "amber"))
	if !hasDoctorCheck(report.Checks, "theme", doctorOK, "amber") {
		t.Errorf("amber not reported as the theme: %+v", report.Checks)
	}

	report = inspectBoard(themeBoard(t, "", "v3"))
	if !hasDoctorCheck(report.Checks, "theme", doctorOK, "v3") {
		t.Errorf("default theme not reported: %+v", report.Checks)
	}

	report = inspectBoard(themeBoard(t, `{"theme": {"name": "neon"}}`, "v3"))
	if !hasDoctorCheck(report.Checks, "theme", doctorFail, `theme "neon" not found`) {
		t.Errorf("missing theme not reported as a failure: %+v", report.Checks)
	}
	fails := 0
	for _, c := range report.Checks {
		if c.Status == doctorFail && strings.Contains(c.Message, "neon") {
			fails++
		}
	}
	if fails != 1 {
		t.Errorf("the missing theme is reported %d times, want once", fails)
	}
}

// A running server keeps the theme it started with: a reload of a
// config.json whose theme.name changed applies everything else, logs that a
// restart is needed, and leaves the running selection in place.
func TestReloadKeepsTheRunningTheme(t *testing.T) {
	root := themeBoard(t, `{"boardName": "Before", "theme": {"name": "amber"}}`, "v3", "amber")
	configDir := filepath.Join(root, "configs")
	serverCfg, err := config.LoadServerConfig(configDir)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.RWMutex
	e := &menu.MenuExecutor{}
	cw, err := NewConfigWatcher(configDir, filepath.Join(root, "menus", "amber"), e, nil, &serverCfg, &mu, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Stopped before the reloads are driven by hand, so the watcher's own
	// polling can't run one at the same time.
	cw.Stop()

	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(prev)

	writeDoctorFixture(t, filepath.Join(configDir, "config.json"), `{"boardName": "After", "theme": {"name": "v3"}}`)
	cw.reloadServerConfig()

	if serverCfg.BoardName != "After" {
		t.Errorf("BoardName = %q: the rest of the reload didn't apply", serverCfg.BoardName)
	}
	if got := serverCfg.ThemeName(); got != "amber" {
		t.Errorf("running theme = %q after reload, want amber", got)
	}
	if got := e.GetServerConfig().ThemeName(); got != "amber" {
		t.Errorf("executor's theme = %q after reload, want amber", got)
	}
	if cw.menuSetPath != filepath.Join(root, "menus", "amber") {
		t.Errorf("watcher moved to %s", cw.menuSetPath)
	}
	if out := logged.String(); !strings.Contains(out, "restart the BBS to switch theme") || !strings.Contains(out, "configured=v3") {
		t.Errorf("no restart warning logged:\n%s", out)
	}

	// The same name again, or an unchanged one, warns about nothing.
	logged.Reset()
	writeDoctorFixture(t, filepath.Join(configDir, "config.json"), `{"boardName": "Again", "theme": {"name": "amber"}}`)
	cw.reloadServerConfig()
	if strings.Contains(logged.String(), "restart the BBS") {
		t.Errorf("warned with the theme unchanged:\n%s", logged.String())
	}
}
