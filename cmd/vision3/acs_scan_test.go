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
	write("menus/v3/ansi/MAIN.ANS", "[M] Messages\r\n{{SPONSOR}}x{{/}}{{S50|Fab}}[*] Sysop{{/}}")
	write("menus/v3/ansi/STATS.ANS", "{{!!S10}}not a menu screen{{/}}")
	write("configs/message_areas.json", `[{"id": 1, "tag": "GENERAL", "acs_read": "", "acs_write": "s20"}, {"id": 2, "tag": "SYSOP", "acs_read": "S<250", "acs_write": "SYSOP"}]`)
	write("configs/file_areas.json", `[{"id": 1, "tag": "UTILS", "acs_list": "s10", "acs_upload": "s20&V", "acs_download": "H25"}]`)
	write("configs/conferences.json", `[{"id": 1, "tag": "LOCAL", "acs": "s10)"}]`)
	write("configs/config.json", `{"doorCategories": [{"code": "GAMES", "name": "Games", "acs": "S20&!Sx"}]}`)
	write("configs/ftn.json", `{"networks": {"fsxnet": {"auto_create": {"enabled": true, "acs_read": "s10", "acs_write": "s20@"}}}}`)
	return root
}

func TestScanACS(t *testing.T) {
	root := writeACSFixture(t)
	type want struct{ file, record, field, text string }
	wants := []want{
		{"menus/v3/mnu/MAIN.MNU", "menu MAIN", "ACS", `no operator before "FA"`},
		{"menus/v3/cfg/MAIN.CFG", `command 2 ("X")`, "ACS", "needs a term on each side"},
		{"menus.d/v3/cfg/MSGMENU.CFG", `command 1 ("R")`, "ACS", "the '!' before it"},
		{"menus/v3/ansi/MAIN.ANS", "region on line 2", "{{acs}}", "exactly one flag"},
		{"configs/message_areas.json", "area SYSOP", "acs_read", "unexpected character '<'"},
		{"configs/file_areas.json", "area UTILS", "acs_download", "hour from 0 to 23"},
		{"configs/conferences.json", "conference 1 (LOCAL)", "acs", "no matching '('"},
		{"configs/config.json", "door category GAMES", "acs", "the '!' before it"},
		{"configs/ftn.json", "network fsxnet auto_create", "acs_write", "unexpected character '@'"},
	}
	findings := scanACS(root)
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
	// a string that fails as a whole is a failure; one only a stricter policy
	// would change is a warning
	for text, want := range map[string]doctorSeverity{
		`"S255&"`:   doctorFail,
		`"S10 FA"`:  doctorFail,
		`"(broken"`: "",
		`"!Q"`:      doctorWarn,
		`"H25"`:     doctorWarn,
		`"s20@"`:    doctorWarn,
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

// TestLogACSFindingsOnce checks that the BBS logs each problem once across
// startup and reloads, and logs a new one when a reload brings it.
func TestLogACSFindingsOnce(t *testing.T) {
	root := writeACSFixture(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logACSFindings(root)
	first := strings.Count(buf.String(), "ACS string problem")
	if first == 0 {
		t.Fatal("nothing logged at startup")
	}
	if !strings.Contains(buf.String(), "level=ERROR") || !strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("want errors and warnings at their own levels:\n%s", buf.String())
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
