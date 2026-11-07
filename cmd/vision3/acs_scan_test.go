package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeACSFixture builds a board under t.TempDir with one questionable ACS
// string in each kind of source, and returns its root.
func writeACSFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("menus/v3/mnu/MAIN.MNU", `{"ACS": "S10 FA"}`)
	write("menus/v3/mnu/MSGMENU.MNU", `{"ACS": ""}`)
	write("menus/v3/cfg/MAIN.CFG", `[{"KEYS": "G", "CMD": "LOGOFF", "ACS": "*"}, {"KEYS": "X", "CMD": "GOTO:ADMIN", "ACS": "S255&"}]`)
	// the overlay's MSGMENU.CFG shadows the stock one, which is never checked
	write("menus/v3/cfg/MSGMENU.CFG", `[{"KEYS": "R", "ACS": "(broken"}]`)
	write("menus.d/v3/cfg/MSGMENU.CFG", `[{"KEYS": "R", "ACS": "!Q"}]`)
	// the SAUCE comment's {{...}} is never rendered, so never checked
	sauce := "SAUCE00" + strings.Repeat(" ", 121)
	write("menus/v3/ansi/MAIN.ANS", "[M] Messages\r\n{{SPONSOR}}x{{/}}{{S50|Fab}}[*] Sysop{{/}}\x1aCOMNT{{S10 FA}}\r\n"+sauce)
	write("menus/v3/ansi/STATS.ANS", "{{!!S10}}not a menu screen{{/}}")
	write("configs/message_areas.json", `[{"id": 1, "tag": "GENERAL", "acs_read": "", "acs_write": "s20"}, {"id": 2, "tag": "SYSOP", "acs_read": "S@250", "acs_write": "SYSOP"}]`)
	write("configs/file_areas.json", `[{"id": 1, "tag": "UTILS", "acs_list": "s10", "acs_upload": "s20&V", "acs_download": "H25"}]`)
	write("configs/conferences.json", `[{"id": 1, "tag": "LOCAL", "acs": "s10)"}]`)
	write("configs/config.json", `{"doorCategories": [{"code": "GAMES", "name": "Games", "acs": "S20&!Sx"}]}`)
	write("configs/ftn.json", `{"networks": {"fsxnet": {"auto_create": {"enabled": true, "acs_read": "s10", "acs_write": "s20@"}}}}`)
	write("configs/doors.json", `[{"code": "LORD", "name": "LORD", "acs": "s20&!Q"}]`)
	return root
}

func TestScanACS(t *testing.T) {
	root := writeACSFixture(t)
	type want struct{ file, record, field, text string }
	wants := []want{
		{"menus/v3/mnu/MAIN.MNU", "menu MAIN", "ACS", `no operator before "FA"`},
		{"menus/v3/cfg/MAIN.CFG", `command 2 ("X")`, "ACS", "needs a term on each side"},
		{"menus.d/v3/cfg/MSGMENU.CFG", `command 1 ("R")`, "ACS", "Q is not an ACS term"},
		{"menus/v3/ansi/MAIN.ANS", "region on line 2", "{{acs}}", "exactly one flag"},
		{"configs/message_areas.json", "area SYSOP", "acs_read", "unexpected character '@'"},
		{"configs/file_areas.json", "area UTILS", "acs_download", "hour from 0 to 23"},
		{"configs/conferences.json", "conference 1 (LOCAL)", "acs", "no matching '('"},
		{"configs/config.json", "door category GAMES", "acs", "S needs a whole number"},
		{"configs/ftn.json", "network fsxnet auto_create", "acs_write", "unexpected character '@'"},
		{"configs/doors.json", "door 1 (LORD)", "acs", "Q is not an ACS term"},
	}
	findings, _ := scanACS(root)
	for _, w := range wants {
		found := false
		for _, f := range findings {
			if f.Source.File == w.file && f.Source.Record == w.record && f.Source.Field == w.field && strings.Contains(f.Detail(), w.text) {
				found = true
			}
		}
		if !found {
			t.Errorf("no finding for %s %s %s containing %q", w.file, w.record, w.field, w.text)
		}
	}
	for _, f := range findings {
		if f.Source.File == "menus/v3/cfg/MSGMENU.CFG" {
			t.Errorf("shadowed stock file checked: %s", f)
		}
		if f.Source.File == "menus/v3/ansi/STATS.ANS" {
			t.Errorf("regions checked in a screen that isn't a menu: %s", f)
		}
		if f.Source.ACS == "S10 FA" && f.Source.Field == "{{acs}}" {
			t.Errorf("region in the SAUCE record checked: %s", f)
		}
		if f.Source.ACS == "" || f.Source.ACS == "*" {
			t.Errorf("finding for an empty or wildcard string: %s", f)
		}
	}
}

func TestCheckACSStringsSeverities(t *testing.T) {
	root := writeACSFixture(t)
	type check struct {
		status  doctorSeverity
		message string
	}
	var checks []check
	checkACSStrings(root, func(name string, status doctorSeverity, message, _ string) {
		if !strings.HasPrefix(name, "ACS: ") {
			t.Errorf("check name %q is outside the ACS section", name)
		}
		if doctorSection(name) != "ACCESS CONTROL" {
			t.Errorf("check %q is in section %q", name, doctorSection(name))
		}
		checks = append(checks, check{status, message})
	})
	// a string with any problem denies every caller, so every finding is a
	// failure, in a door's acs or anywhere else
	for text, want := range map[string]doctorSeverity{
		`"S255&"`:   doctorFail,
		`"S10 FA"`:  doctorFail,
		`"(broken"`: "",
		`"!Q"`:      doctorFail,
		`"H25"`:     doctorFail,
		`"s20@"`:    doctorFail,
		`"s20&!Q"`:  doctorFail,
	} {
		var got doctorSeverity
		for _, c := range checks {
			if strings.Contains(c.message, text) && got != doctorFail {
				got = c.status
			}
		}
		if got != want {
			t.Errorf("%s: status %q, want %q", text, got, want)
		}
	}
}

func TestCheckACSStringsClean(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "configs", "conferences.json"), []byte(`[{"id": 1, "acs": "s10"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []doctorSeverity
	checkACSStrings(root, func(_ string, status doctorSeverity, _, _ string) { got = append(got, status) })
	if len(got) != 1 || got[0] != doctorOK {
		t.Errorf("clean board: %v, want one ok", got)
	}
}

// TestScanACSDoors checks that each door's acs in doors.json is scanned,
// trimmed as LoadDoors trims it, and that any problem in one is a failure:
// the BBS denies such a door to every caller.
func TestScanACSDoors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "configs", "doors.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	doors := `[{"code": "NOTQ", "acs": "!Q"}, {"code": "BROKEN", "acs": "s20&"}, {"code": "GOOD", "acs": " s20&fG "},
		{"code": "BLANK", "acs": "   "}, {"code": "NONE"}, {"code": "HOUR", "acs": "H25"}]`
	if err := os.WriteFile(path, []byte(doors), 0o644); err != nil {
		t.Fatal(err)
	}

	findings, checked := scanACS(root)
	if checked != 4 {
		t.Errorf("checked %d door strings, want 4 (blank and missing are no rule)", checked)
	}
	got := map[string]acsFinding{}
	for _, f := range findings {
		if f.Source.File != "configs/doors.json" || f.Source.Field != "acs" {
			t.Errorf("unexpected finding %s", f)
		}
		got[f.Source.Record] = f
	}
	for _, rec := range []string{"door 1 (NOTQ)", "door 2 (BROKEN)", "door 6 (HOUR)"} {
		if _, ok := got[rec]; !ok {
			t.Errorf("no finding for %s", rec)
		}
	}
	if len(got) != 3 {
		t.Errorf("findings %v, want NOTQ, BROKEN and HOUR only", got)
	}

	var statuses []doctorSeverity
	checkACSStrings(root, func(_ string, status doctorSeverity, _, _ string) { statuses = append(statuses, status) })
	for _, st := range statuses {
		if st != doctorFail {
			t.Errorf("door acs check status %q, want fail", st)
		}
	}

	resetACSLogged(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	logACSFindings(root)
	if n := strings.Count(buf.String(), "level=ERROR"); n != 3 || strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("want three door findings logged as errors:\n%s", buf.String())
	}
}

// TestLogACSFindingsOnce checks that the BBS logs each problem once across
// startup and reloads, and logs a new one when a reload brings it.
func TestLogACSFindingsOnce(t *testing.T) {
	root := writeACSFixture(t)
	resetACSLogged(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logACSFindings(root)
	first := strings.Count(buf.String(), "ACS string problem")
	if first == 0 {
		t.Fatal("nothing logged at startup")
	}
	if strings.Count(buf.String(), "level=ERROR") != first || strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("want every problem logged as an error:\n%s", buf.String())
	}
	logACSFindings(root)
	if n := strings.Count(buf.String(), "ACS string problem"); n != first {
		t.Errorf("rescan logged %d more lines, want none", n-first)
	}
	if err := os.WriteFile(filepath.Join(root, "configs", "conferences.json"), []byte(`[{"id": 1, "tag": "LOCAL", "acs": "s10&&"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	logACSFindings(root)
	if n := strings.Count(buf.String(), "ACS string problem"); n != first+1 {
		t.Errorf("after an edit: %d new lines, want 1", n-first)
	}
}

// TestScanACSLoaderSpelling checks that the scan reads a menu's files by the
// name the loader opens, NAME.MNU, NAME.CFG and NAME.ANS: a differently cased
// file is scanned only where the filesystem folds case, as the loader would
// open it there too.
func TestScanACSLoaderSpelling(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("menus/v3/mnu/MAIN.MNU", `{"ACS": ""}`)
	write("menus/v3/cfg/old.cfg", `[{"KEYS": "X", "ACS": "S255&"}]`)
	write("menus/v3/mnu/Stats.Mnu", `{"ACS": "S10 FA"}`)
	write("menus/v3/ansi/main.ans", "{{!!S10}}x{{/}}")
	_, err := os.Stat(filepath.Join(root, "menus", "v3", "cfg", "OLD.CFG"))
	foldsCase := err == nil

	findings, _ := scanACS(root)
	got := map[string]bool{}
	for _, f := range findings {
		got[f.Source.ACS] = true
	}
	for _, acs := range []string{"S255&", "S10 FA", "!!S10"} {
		if got[acs] != foldsCase {
			t.Errorf("%q found = %v, want %v (filesystem folds case: %v)", acs, got[acs], foldsCase, foldsCase)
		}
	}
}

// TestScanACSCount checks that doctor's clean count comes from the same pass
// as its findings: every non-empty string, wildcards included.
func TestScanACSCount(t *testing.T) {
	root := writeACSFixture(t)
	findings, checked := scanACS(root)
	want := 0
	for _, s := range collectACSSources(root) {
		if s.ACS != "" {
			want++
		}
	}
	if checked != want || len(findings) == 0 {
		t.Errorf("scanACS checked %d strings (findings %d), want %d", checked, len(findings), want)
	}
}

// TestLogACSFindingsReturns checks that a problem fixed and later brought
// back (a restore from backup) is logged again.
func TestLogACSFindingsReturns(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "configs", "conferences.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	set := func(acs string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`[{"id": 1, "tag": "LOCAL", "acs": "`+acs+`"}]`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resetACSLogged(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for i, step := range []struct {
		acs  string
		want int
	}{{"s10&&", 1}, {"s10&&", 1}, {"s10", 1}, {"s10&&", 2}} {
		set(step.acs)
		logACSFindings(root)
		if n := strings.Count(buf.String(), "ACS string problem"); n != step.want {
			t.Errorf("step %d (%q): %d lines logged, want %d", i, step.acs, n, step.want)
		}
	}
}

// resetACSLogged starts a test with no findings logged, as at startup; the
// set is keyed by board-relative path, so tests on different boards share it.
func resetACSLogged(t *testing.T) {
	t.Helper()
	acsLogged.Lock()
	acsLogged.seen = map[string]bool{}
	acsLogged.Unlock()
}
