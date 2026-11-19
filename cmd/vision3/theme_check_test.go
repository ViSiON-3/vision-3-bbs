package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeThemeFiles writes files, by path relative to dir, making their
// directories.
func writeThemeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func themeChecks(f func(add func(string, doctorSeverity, string, string))) []doctorCheck {
	var checks []doctorCheck
	f(func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	return checks
}

// The theme check reports what a theme needs and lacks: a MAIN menu, a
// screen for each menu with commands and no script, the theme scripts and
// script doors its menus name, and files that don't load.
func TestCheckThemeReportsMissingScreensScriptsAndBadFiles(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	writeThemeFiles(t, configDir, map[string]string{
		"doors.json": `[{"name":"Lister","type":"v3_script","script":"list.js","code":"LISTER"},
		                {"name":"Native","type":"native","command":"/bin/true","code":"NATIVE"}]`,
	})
	dir := filepath.Join(root, "menus", "glass")
	writeThemeFiles(t, dir, map[string]string{
		"theme.json":       `{"menuScript": "missing-default.js"`,
		"strings.json":     `{"pauseString": `,
		"mnu/FILES.MNU":    `{}`,
		"cfg/FILES.CFG":    `[{"KEYS":"Q","CMD":"GOTO:FILES"}]`,
		"mnu/MSGS.MNU":     `{"SCRIPT": "gone.js blue"}`,
		"mnu/USERS.MNU":    `{"SCRIPT": "../escape.js"}`,
		"mnu/DOORS.MNU":    `{"SCRIPT": "NODOOR"}`,
		"mnu/NATIVE.MNU":   `{"SCRIPT": "NATIVE"}`,
		"mnu/LIST.MNU":     `{"SCRIPT": "LISTER"}`,
		"mnu/GOOD.MNU":     `{"SCRIPT": "menu.js"}`,
		"mnu/PLAIN.MNU":    `{"SCRIPT": "NONE"}`,
		"cfg/PLAIN.CFG":    `[{"KEYS":"Q","CMD":"GOTO:FILES"}]`,
		"ansi/PLAIN.ANS":   "plain",
		"mnu/BROKEN.MNU":   `{"TITLE": `,
		"scripts/menu.js":  "",
		"data/picture.png": "",
	})
	checks := themeChecks(func(add func(string, doctorSeverity, string, string)) {
		checkTheme(dir, configDir, add)
	})

	want := []struct {
		status doctorSeverity
		text   string
	}{
		{doctorFail, "no mnu/MAIN.MNU"},
		{doctorWarn, "FILES has commands but no screen ansi/FILES.ANS"},
		{doctorWarn, `mnu/MSGS.MNU: SCRIPT names theme script "gone.js"`},
		{doctorWarn, `mnu/USERS.MNU: SCRIPT "../escape.js" is not a file name`},
		{doctorWarn, `mnu/DOORS.MNU: SCRIPT names door "NODOOR", which configs/doors.json does not have`},
		{doctorWarn, `mnu/NATIVE.MNU: SCRIPT names door "NATIVE", which is not a v3_script door`},
		{doctorFail, "theme.json"},
		{doctorFail, "strings.json"},
		{doctorFail, "BROKEN.MNU"},
	}
	for _, w := range want {
		found := false
		for _, c := range checks {
			if c.Status == w.status && strings.Contains(c.Name+"  "+c.Message, w.text) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no %s check mentioning %q in:\n%s", w.status, w.text, dumpChecks(checks))
		}
	}
	for _, c := range checks {
		text := c.Name + "  " + c.Message
		for _, ok := range []string{"GOOD.MNU", "LIST.MNU", "PLAIN"} {
			if c.Status != doctorOK && c.Status != doctorInfo && strings.Contains(text, ok) {
				t.Errorf("%s is reported: %s %s", ok, c.Status, text)
			}
		}
	}
}

// theme.json's menuScript is a script reference too, and a menu with
// commands that the default script draws needs no screen of its own.
func TestCheckThemeMenuScriptDefault(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "menus", "glass")
	writeThemeFiles(t, dir, map[string]string{
		"theme.json":    `{"menuScript": "absent.js"}`,
		"mnu/MAIN.MNU":  `{}`,
		"cfg/MAIN.CFG":  `[]`,
		"ansi/MAIN.ANS": "main",
		"mnu/FILES.MNU": `{}`,
		"cfg/FILES.CFG": `[]`,
	})
	checks := themeChecks(func(add func(string, doctorSeverity, string, string)) {
		checkTheme(dir, filepath.Join(root, "configs"), add)
	})
	if !hasCheckText(checks, doctorWarn, `theme.json: menuScript names theme script "absent.js"`) {
		t.Errorf("menuScript not checked:\n%s", dumpChecks(checks))
	}
	if hasCheckText(checks, doctorWarn, "no screen ansi/FILES.ANS") {
		t.Errorf("a menu the default script draws is reported as missing its screen:\n%s", dumpChecks(checks))
	}
}

// The comparison lists the menus, screens and commands the shipped theme
// has that the checked theme lacks; extras of the checked theme are not
// listed.
func TestThemeGaps(t *testing.T) {
	root := t.TempDir()
	stock := filepath.Join(root, "menus", "v3")
	writeThemeFiles(t, stock, map[string]string{
		"mnu/MAIN.MNU":       `{}`,
		"mnu/NEWS.MNU":       `{}`,
		"cfg/MAIN.CFG":       `[{"KEYS":"J","CMD":"RUN:LISTNEWS"},{"KEYS":"G","CMD":"logoff"},{"KEYS":"N","CMD":"GOTO:NEWS"}]`,
		"cfg/NEWS.CFG":       `[{"KEYS":"R","CMD":"run:readnews 1"}]`,
		"ansi/MAIN.ANS":      "",
		"ansi/NEWSHDR.ANS":   "",
		"templates/NEWS.TOP": "",
		"templates/NEWS.MID": "",
		"bar/MAIN.BAR":       "",
	})
	theme := filepath.Join(root, "menus", "glass")
	writeThemeFiles(t, theme, map[string]string{
		"mnu/MAIN.MNU":       `{}`,
		"mnu/EXTRA.MNU":      `{}`,
		"cfg/MAIN.CFG":       `[{"KEYS":"Q","CMD":"LOGOFF"},{"KEYS":"X","CMD":"RUN:EXTRA"}]`,
		"ansi/MAIN.ANS":      "",
		"ansi/EXTRA.ANS":     "",
		"templates/NEWS.TOP": "",
	})
	gaps, err := themeGaps(stock, theme)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(gaps.Menus, ","), "NEWS"; got != want {
		t.Errorf("menus = %q, want %q", got, want)
	}
	if got, want := strings.Join(gaps.Screens, ","), "ansi/NEWSHDR.ANS,bar/MAIN.BAR,templates/NEWS.MID"; got != want {
		t.Errorf("screens = %q, want %q", got, want)
	}
	if got, want := strings.Join(gaps.Commands, ","), "GOTO:NEWS,RUN:LISTNEWS,RUN:READNEWS 1"; got != want {
		t.Errorf("commands = %q, want %q", got, want)
	}
}

// doctor --theme checks one theme: a name under menus/ or a directory,
// and reports what the shipped theme has that it lacks.
func TestRunThemeDoctor(t *testing.T) {
	root := t.TempDir()
	writeThemeFiles(t, filepath.Join(root, "menus", "v3"), map[string]string{
		"theme.json":    `{}`,
		"mnu/MAIN.MNU":  `{}`,
		"cfg/MAIN.CFG":  `[{"KEYS":"J","CMD":"RUN:LISTNEWS"}]`,
		"ansi/MAIN.ANS": "",
		"ansi/BYE.ANS":  "",
	})
	glass := map[string]string{
		"theme.json":    `{}`,
		"mnu/MAIN.MNU":  `{}`,
		"cfg/MAIN.CFG":  `[]`,
		"ansi/MAIN.ANS": "",
	}
	writeThemeFiles(t, filepath.Join(root, "menus", "glass"), glass)
	elsewhere := filepath.Join(t.TempDir(), "incoming")
	writeThemeFiles(t, elsewhere, glass)

	for _, arg := range []string{"glass", filepath.Join(root, "menus", "glass"), elsewhere} {
		var out, errOut bytes.Buffer
		code := runThemeDoctor(root, arg, false, &out, &errOut)
		if code != 0 {
			t.Errorf("%s: exit %d, stderr %q\n%s", arg, code, errOut.String(), out.String())
		}
		for _, want := range []string{"THEME", "ansi/BYE.ANS", "RUN:LISTNEWS", "lacks"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: output lacks %q:\n%s", arg, want, out.String())
			}
		}
	}

	var out, errOut bytes.Buffer
	if code := runThemeDoctor(root, "nosuch", false, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "nosuch") {
		t.Errorf("unknown theme: exit %d, stderr %q", code, errOut.String())
	}

	// A theme with a failure exits 2, and --json reports the checks.
	writeThemeFiles(t, filepath.Join(root, "menus", "broken"), map[string]string{"theme.json": "{"})
	out.Reset()
	if code := runThemeDoctor(root, "broken", true, &out, &errOut); code != 2 {
		t.Errorf("broken theme: exit %d", code)
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.Summary.Failures == 0 {
		t.Errorf("no failures in %+v", report)
	}
}

// The shipped themes pass the check: v3 with nothing missing, vision3 with
// no failure or warning, whatever it lacks of v3 listed.
func TestCheckShippedThemes(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	// Each theme with the configs of a board that runs it.
	for name, configDir := range map[string]string{
		"v3":      filepath.Join(root, "templates", "configs"),
		"vision3": filepath.Join(root, "templates", "configs"),
	} {
		checks := themeChecks(func(add func(string, doctorSeverity, string, string)) {
			checkTheme(filepath.Join(root, "menus", name), configDir, add)
		})
		for _, c := range checks {
			if c.Status == doctorFail || c.Status == doctorWarn {
				t.Errorf("%s: %s %s %s", name, c.Status, c.Name, c.Message)
			}
		}
	}
	gaps, err := themeGaps(filepath.Join(root, "menus", "v3"), filepath.Join(root, "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps.Menus)+len(gaps.Screens)+len(gaps.Commands) != 0 {
		t.Errorf("v3 lacks something of itself: %+v", gaps)
	}
}

// The board doctor's theme line notes, for a board on a theme other than
// the shipped one, how much of the shipped theme it lacks.
func TestInspectBoardNotesThemeGaps(t *testing.T) {
	root := t.TempDir()
	writeThemeFiles(t, root, map[string]string{
		"configs/config.json":       `{"theme": {"name": "glass"}}`,
		"menus/v3/theme.json":       `{}`,
		"menus/v3/mnu/MAIN.MNU":     `{}`,
		"menus/v3/ansi/MAIN.ANS":    "",
		"menus/v3/ansi/BYE.ANS":     "",
		"menus/glass/theme.json":    `{}`,
		"menus/glass/mnu/MAIN.MNU":  `{}`,
		"menus/glass/ansi/MAIN.ANS": "",
	})
	report := inspectBoard(root)
	found := false
	for _, c := range report.Checks {
		if c.Name == "theme" && c.Status == doctorInfo && strings.Contains(c.Message, "glass lacks 1 screen of the shipped theme v3") &&
			strings.Contains(c.Fix, "./vision3 doctor --theme glass") {
			found = true
		}
	}
	if !found {
		t.Errorf("no gaps note:\n%s", dumpChecks(report.Checks))
	}
}

func hasCheckText(checks []doctorCheck, status doctorSeverity, text string) bool {
	for _, c := range checks {
		if c.Status == status && strings.Contains(c.Name+"  "+c.Message, text) {
			return true
		}
	}
	return false
}

func dumpChecks(checks []doctorCheck) string {
	var b strings.Builder
	for _, c := range checks {
		b.WriteString(string(c.Status) + "  " + c.Name + "  " + c.Message + "\n")
	}
	return b.String()
}

// doctor --theme shows the name and description callers see, with the
// board's theme.labels applied, and warns about a description longer than
// the 60 columns Konfig shows or a name longer than its 20.
func TestCheckThemeLabel(t *testing.T) {
	root := t.TempDir()
	writeThemeFiles(t, root, map[string]string{
		"configs/config.json":      `{"theme": {"labels": {"v3": {"name": "V3-Stock", "description": "Stock ViSiON/3 menus, as shipped"}}}}`,
		"menus/v3/theme.json":      `{}`,
		"menus/v3/mnu/MAIN.MNU":    `{}`,
		"menus/glass/theme.json":   `{"name": "A name far longer than twenty", "description": "` + strings.Repeat("x", 61) + `"}`,
		"menus/glass/mnu/MAIN.MNU": `{}`,
		"menus/plain/mnu/MAIN.MNU": `{}`,
	})
	configDir := filepath.Join(root, "configs")
	run := func(name string) []doctorCheck {
		return themeChecks(func(add func(string, doctorSeverity, string, string)) {
			checkThemeContents(filepath.Join(root, "menus", name), configDir, add)
		})
	}
	if c := run("v3"); !hasCheckText(c, doctorOK, `shown as "V3-Stock": Stock ViSiON/3 menus, as shipped (configs/config.json theme.labels)`) {
		t.Errorf("v3:\n%s", dumpChecks(c))
	}
	if c := run("plain"); !hasCheckText(c, doctorInfo, `states no name or description; shown as "plain"`) {
		t.Errorf("plain:\n%s", dumpChecks(c))
	}
	// A directory name is shown when the theme states none, so its width
	// is checked too.
	writeThemeFiles(t, root, map[string]string{"menus/abcdefghijklmnopqrstu/mnu/MAIN.MNU": `{}`})
	if c := run("abcdefghijklmnopqrstu"); !hasCheckText(c, doctorInfo, "states no name") || !hasCheckText(c, doctorWarn, "name is 21 columns") {
		t.Errorf("long directory name:\n%s", dumpChecks(c))
	}
	// Sources are tracked, not inferred from the text: a board label equal
	// to the theme's own is the board's, a theme named after its directory
	// states a name, and fields from different places are each named.
	writeThemeFiles(t, root, map[string]string{
		"configs/config.json":      `{"theme": {"labels": {"same": {"name": "Same"}, "mixed": {"description": "Board words"}}}}`,
		"menus/same/theme.json":    `{"name": "Same"}`,
		"menus/same/mnu/MAIN.MNU":  `{}`,
		"menus/self/theme.json":    `{"name": "self"}`,
		"menus/self/mnu/MAIN.MNU":  `{}`,
		"menus/mixed/theme.json":   `{"name": "Mixed"}`,
		"menus/mixed/mnu/MAIN.MNU": `{}`,
		"menus/desc/mnu/MAIN.MNU":  `{}`,
		"menus/desc/theme.json":    `{"description": "Only words"}`,
	})
	for theme, want := range map[string]string{
		"same":  `shown as "Same" (name: configs/config.json theme.labels)`,
		"self":  `shown as "self" (name: theme.json)`,
		"mixed": `shown as "Mixed": Board words (name: theme.json; description: configs/config.json theme.labels)`,
		"desc":  `shown as "desc": Only words (name: directory name; description: theme.json)`,
	} {
		if c := run(theme); !hasCheckText(c, doctorOK, want) {
			t.Errorf("%s: want %q in:\n%s", theme, want, dumpChecks(c))
		}
	}

	// A config.json whose theme section doesn't decode is reported, not
	// passed over.
	writeThemeFiles(t, root, map[string]string{"configs/config.json": `{"theme": {"labels": {"glass": 42}}}`})
	if c := run("glass"); !hasCheckText(c, doctorWarn, "configs/config.json theme.labels not applied") {
		t.Errorf("bad config.json:\n%s", dumpChecks(c))
	}
	writeThemeFiles(t, root, map[string]string{"configs/config.json": `{}`})
	c := run("glass")
	if !hasCheckText(c, doctorWarn, "description is 61 columns") || !hasCheckText(c, doctorWarn, "name is 29 columns") {
		t.Errorf("glass:\n%s", dumpChecks(c))
	}
}

// The board doctor warns about a theme.labels entry for a directory that
// isn't under menus/: a misspelt name labels nothing.
func TestInspectBoardThemeLabelsForMissingTheme(t *testing.T) {
	root := t.TempDir()
	writeThemeFiles(t, root, map[string]string{
		"configs/config.json":   `{"theme": {"labels": {"v3": {"name": "V3-Stock"}, "vison3": {"name": "Typo"}}}}`,
		"menus/v3/theme.json":   `{}`,
		"menus/v3/mnu/MAIN.MNU": `{}`,
	})
	report := inspectBoard(root)
	if !hasCheckText(report.Checks, doctorWarn, `theme.labels names "vison3", which is not a theme under menus/`) {
		t.Errorf("no warning:\n%s", dumpChecks(report.Checks))
	}
	if hasCheckText(report.Checks, doctorWarn, `theme.labels names "v3"`) {
		t.Errorf("warned about an installed theme:\n%s", dumpChecks(report.Checks))
	}
}
