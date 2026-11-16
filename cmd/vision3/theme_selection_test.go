package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// Board colour overrides (config.json theme.colors) reload with
// config.json, over whatever theme.json holds, and a theme.json reload
// keeps them. A config.json with a colour that isn't one is not applied:
// the running configuration and colours stay.
func TestBoardColoursHotReload(t *testing.T) {
	root := themeBoard(t, `{"boardName": "Before"}`, "v3")
	configDir := filepath.Join(root, "configs")
	menuSet := filepath.Join(root, "menus", "v3")
	themeJSON := filepath.Join(menuSet, "theme.json")
	writeDoctorFixture(t, themeJSON, `{"yesNoHighlightColor": 31, "yesNoRegularColor": 13, "lightbarHint": 8}`)

	serverCfg, err := config.LoadServerConfig(configDir)
	if err != nil {
		t.Fatal(err)
	}
	theme, err := config.LoadThemeConfig(menuSet)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.RWMutex
	e := &menu.MenuExecutor{}
	e.SetTheme(theme)
	e.SetServerConfig(serverCfg)
	cw, err := NewConfigWatcher(configDir, menuSet, e, nil, &serverCfg, &mu, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Stopped before the reloads are driven by hand, so the watcher's own
	// polling can't run one at the same time.
	cw.Stop()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	defer slog.SetDefault(prev)

	colour := func(role config.ColorRole) int { return e.Theme().Color(role) }
	if colour(config.RoleLightbarSelected) != 31 || colour(config.RoleLightbarHint) != 8 {
		t.Fatalf("before any override: selected %d, hint %d", colour(config.RoleLightbarSelected), colour(config.RoleLightbarHint))
	}

	writeDoctorFixture(t, filepath.Join(configDir, "config.json"), `{"boardName": "Overridden", "theme": {"colors": {"lightbarSelected": 78}}}`)
	cw.reloadServerConfig()
	if colour(config.RoleLightbarSelected) != 78 || colour(config.RoleLightbarHint) != 8 {
		t.Errorf("after the override reloaded: selected %d, hint %d; want 78 and the theme's 8", colour(config.RoleLightbarSelected), colour(config.RoleLightbarHint))
	}

	// An invalid edit: nothing of it is applied.
	writeDoctorFixture(t, filepath.Join(configDir, "config.json"), `{"boardName": "Broken", "theme": {"colors": {"lightbarSelected": 300, "lightbarHint": 3}}}`)
	cw.reloadServerConfig()
	if serverCfg.BoardName != "Overridden" || e.GetServerConfig().BoardName != "Overridden" {
		t.Errorf("an invalid config.json replaced the running one: %q", serverCfg.BoardName)
	}
	if colour(config.RoleLightbarSelected) != 78 || colour(config.RoleLightbarHint) != 8 {
		t.Errorf("after an invalid edit: selected %d, hint %d; want them unchanged", colour(config.RoleLightbarSelected), colour(config.RoleLightbarHint))
	}

	// The theme changes under the override: the override holds, the rest follows.
	writeDoctorFixture(t, themeJSON, `{"yesNoHighlightColor": 47, "yesNoRegularColor": 13, "lightbarHint": 3}`)
	cw.reloadTheme()
	if colour(config.RoleLightbarSelected) != 78 || colour(config.RoleLightbarHint) != 3 || colour(config.RoleYesNoHighlight) != 47 {
		t.Errorf("after theme.json reloaded: selected %d, hint %d, yes/no %d", colour(config.RoleLightbarSelected), colour(config.RoleLightbarHint), colour(config.RoleYesNoHighlight))
	}

	// The override removed (Use theme default): the theme's colour is back.
	writeDoctorFixture(t, filepath.Join(configDir, "config.json"), `{"boardName": "Plain"}`)
	cw.reloadServerConfig()
	if colour(config.RoleLightbarSelected) != 47 {
		t.Errorf("after the override was removed: selected %d, want the theme's 47", colour(config.RoleLightbarSelected))
	}
}

// Doctor notes an overlay theme.json that only repeats the shipped theme,
// as the config editor used to write, names the keys of one that partly
// does, and says nothing about one that holds only values of the sysop's
// own.
func TestDoctorNotesRedundantThemeOverlay(t *testing.T) {
	shipped := `{"yesNoHighlightColor": 31, "yesNoRegularColor": 13, "lightbarSelected": 31, "chatSysopColor": 11, "sysopChatScreen": "V3CHAT1"}`
	note := "menus.d/v3/theme.json"
	check := func(overlay string) []doctorCheck {
		t.Helper()
		root := themeBoard(t, "", "v3")
		writeDoctorFixture(t, filepath.Join(root, "menus", "v3", "theme.json"), shipped)
		if overlay != "" {
			if err := os.MkdirAll(filepath.Join(root, "menus.d", "v3"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeDoctorFixture(t, filepath.Join(root, "menus.d", "v3", "theme.json"), overlay)
		}
		var checks []doctorCheck
		checkThemeOverlay(filepath.Join(root, "menus", "v3"), func(name string, status doctorSeverity, message, fix string) {
			checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
		})
		return checks
	}

	// A full copy from before the roles: every key it has matches.
	copyOf := "{\n    \"yesNoHighlightColor\": 31,\n    \"yesNoRegularColor\": 13,\n    \"chatSysopColor\": 11,\n    \"sysopChatScreen\": \"V3CHAT1\"\n}\n"
	if checks := check(copyOf); !hasDoctorCheck(checks, note, doctorInfo, "only repeats the shipped theme's values") {
		t.Errorf("a redundant overlay was not noted: %+v", checks)
	}
	if checks := check(`{"yesNoHighlightColor": 79, "yesNoRegularColor": 13}`); !hasDoctorCheck(checks, note, doctorInfo, "1 of 2 keys only repeat the shipped theme's values, so they pin them if a later theme changes one: yesNoRegularColor") {
		t.Errorf("a partly redundant overlay was not noted: %+v", checks)
	}
	for name, overlay := range map[string]string{
		"no overlay":         "",
		"a changed colour":   `{"yesNoHighlightColor": 79}`,
		"a key of its own":   `{"lightbarHint": 3}`,
		"an empty overlay":   `{}`,
		"an unreadable file": `{`,
	} {
		if checks := check(overlay); len(checks) != 0 {
			t.Errorf("%s: %+v, want nothing said", name, checks)
		}
	}
}

// doctor --fix backs up the selected theme's overlay theme.json and takes
// out the keys that repeat the shipped theme; --dry-run only says so.
func TestDoctorFixTrimsThemeOverlay(t *testing.T) {
	root := themeBoard(t, "", "v3")
	writeDoctorFixture(t, filepath.Join(root, "menus", "v3", "theme.json"), `{"yesNoHighlightColor": 31, "chatUserColor": 10}`)
	overlayDir := filepath.Join(root, "menus.d", "v3")
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	overlay := `{"yesNoHighlightColor": 31, "chatUserColor": 12}`
	overlayFile := filepath.Join(overlayDir, "theme.json")
	writeDoctorFixture(t, overlayFile, overlay)
	now := time.Date(2026, 11, 16, 15, 4, 5, 0, time.UTC)

	fixes := fixThemeOverlay(root, true, now)
	if len(fixes) != 1 || fixes[0].Status != "would-fix" || !strings.Contains(fixes[0].Action, "yesNoHighlightColor") {
		t.Fatalf("dry run = %+v", fixes)
	}
	if data, _ := os.ReadFile(overlayFile); string(data) != overlay {
		t.Fatalf("dry run changed the overlay: %s", data)
	}

	fixes = fixThemeOverlay(root, false, now)
	if len(fixes) != 1 || fixes[0].Status != "fixed" {
		t.Fatalf("fix = %+v", fixes)
	}
	if fixes[0].Message != "backup: data/backups/menus.d-v3-theme.json.20261116-150405" {
		t.Errorf("fix message = %q", fixes[0].Message)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "data", "backups", "menus.d-v3-theme.json.20261116-150405")); string(data) != overlay {
		t.Errorf("backup = %q; want the original overlay", data)
	}
	theme, err := config.LoadThemeConfig(filepath.Join(root, "menus", "v3"))
	if err != nil || theme.ChatUserColor != 12 || theme.YesNoHighlightColor != 31 {
		t.Errorf("after the fix: %+v, %v", theme, err)
	}
	if fixes := fixThemeOverlay(root, false, now); len(fixes) != 0 {
		t.Errorf("a second fix = %+v; want nothing to do", fixes)
	}

	// An overlay that is all repetition goes entirely.
	writeDoctorFixture(t, overlayFile, `{"chatUserColor": 10}`)
	if fixes := fixThemeOverlay(root, false, now.Add(time.Second)); len(fixes) != 1 || !strings.HasPrefix(fixes[0].Action, "remove (") {
		t.Fatalf("fix = %+v", fixes)
	}
	if _, err := os.Stat(overlayFile); !os.IsNotExist(err) {
		t.Errorf("overlay still there: %v", err)
	}
}

// Doctor notes a retired stock *HI.BAR left in the shipped set, which pins
// its screen's highlight, and nothing else: not a BAR with colours of the
// sysop's own, and not one the overlay already overrides or inherits past.
func TestDoctorNotesRetiredScreenBars(t *testing.T) {
	stock := "; File listing highlight bar colors\n0,0,31,7,*,*,FileHighlight\n"
	root := themeBoard(t, "", "v3")
	bar := func(layer, name, content string) {
		t.Helper()
		dir := filepath.Join(root, layer, "v3", "bar")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeDoctorFixture(t, filepath.Join(dir, name+".BAR"), content)
	}
	bar("menus", "FILELISTHI", stock)                   // left over: noted
	bar("menus", "MSGAREAHI", "0,0,79,7,*,*,Mine\n")    // changed in place: the sysop's
	bar("menus", "MSGCONFHI", stock)                    // left over, but
	bar("menus.d", "MSGCONFHI", "; inherit\nINHERIT\n") // already inheriting
	bar("menus", "FILEAREAHI", stock)                   // never a stock file

	var checks []doctorCheck
	checkRetiredScreenBars(filepath.Join(root, "menus", "v3"), func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if len(checks) != 1 || !hasDoctorCheck(checks, "menus/v3/bar/FILELISTHI.BAR", doctorInfo, "no longer ships") {
		t.Errorf("checks = %+v, want one note, for FILELISTHI", checks)
	}

	// Another theme never shipped these files: a BAR there with the same
	// colours is that theme's own, and is left alone.
	amber := filepath.Join(root, "menus", "amber", "bar")
	if err := os.MkdirAll(amber, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoctorFixture(t, filepath.Join(amber, "FILELISTHI.BAR"), stock)
	checks = nil
	checkRetiredScreenBars(filepath.Join(root, "menus", "amber"), func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if len(checks) != 0 {
		t.Errorf("another theme's BAR was called retired stock: %+v", checks)
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
