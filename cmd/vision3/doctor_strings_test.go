package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

func stringsChecks(t *testing.T, dir, content string) []doctorCheck {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "strings.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var checks []doctorCheck
	checkStringsKeys(dir, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	return checks
}

// Doctor notes strings.json entries that hold shipped text, keys the BBS
// doesn't read, and customised strings whose default changed; it says
// nothing about a file of the sysop's own strings.
func TestDoctorNotesStringsFile(t *testing.T) {
	dir := t.TempDir()
	shipped, err := configtemplates.StringDefaults()
	if err != nil {
		t.Fatal(err)
	}
	pause, _ := json.Marshal(shipped["pauseString"])
	checks := stringsChecks(t, dir, `{"pauseString": `+string(pause)+`, "anonymousName": "Nobody", "Read_Feedback": "Read Feedback?", "defColor1": 12, "_note": "mine"}`)
	if !hasDoctorCheck(checks, "configs/strings.json", doctorInfo, "1 string(s) hold text they shipped with, so they keep it when a release rewords them; 1 key(s) the BBS does not read: Read_Feedback") {
		t.Errorf("checks = %+v", checks)
	}

	checks = stringsChecks(t, dir, `{"anonymousName": "Nobody", "_defaults": {"anonymousName": "0000000000000000"}}`)
	if !hasDoctorCheck(checks, "configs/strings.json", doctorInfo, "the shipped default changed under 1 string(s) you customised: anonymousName") || len(checks) != 1 {
		t.Errorf("changed default: %+v", checks)
	}

	if checks := stringsChecks(t, dir, `{"anonymousName": "Nobody", "defColor7": 3}`); len(checks) != 0 {
		t.Errorf("own strings only: %+v; want nothing said", checks)
	}
	if checks := stringsChecks(t, dir, `{`); len(checks) != 0 {
		t.Errorf("broken file: %+v; want it left to the parse check", checks)
	}
}

// doctor --fix trims strings.json to the sysop's own strings, after a
// backup; --dry-run changes nothing; the board prints the same afterwards.
func TestDoctorFixTrimsStrings(t *testing.T) {
	root := t.TempDir()
	configs := filepath.Join(root, "configs")
	if err := os.MkdirAll(configs, 0o755); err != nil {
		t.Fatal(err)
	}
	old := config.StringSuperseded["chatMsgUsage"][0]
	oldJSON, _ := json.Marshal(old)
	content := `{"chatMsgUsage": ` + string(oldJSON) + `, "anonymousName": "Nobody <x&y>", "Read_Feedback": "Read Feedback?"}`
	path := filepath.Join(configs, "strings.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := config.LoadStrings(configs)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 16, 15, 4, 5, 0, time.UTC)

	fixes := fixStrings(root, true, now)
	if len(fixes) != 1 || fixes[0].Status != "would-fix" || !strings.Contains(fixes[0].Action, "keep 1 of your own") {
		t.Fatalf("dry run = %+v", fixes)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Fatalf("dry run changed the file: %s", data)
	}

	fixes = fixStrings(root, false, now)
	if len(fixes) != 1 || fixes[0].Status != "fixed" || fixes[0].Message != "backup: data/backups/strings.json.20261116-150405" {
		t.Fatalf("fix = %+v", fixes)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "data", "backups", "strings.json.20261116-150405")); string(data) != content {
		t.Errorf("backup = %q", data)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 2 || doc["anonymousName"] == nil || doc[config.StringBasesKey] == nil {
		t.Errorf("after the fix: %s; want anonymousName and its fingerprint", data)
	}
	if !strings.Contains(string(data), "<x&y>") {
		t.Errorf("value HTML-escaped: %s", data)
	}
	after, err := config.LoadStrings(configs)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Error("the board prints differently after the fix")
	}
	if fixes := fixStrings(root, false, now); len(fixes) != 0 {
		t.Errorf("a second fix = %+v; want nothing to do", fixes)
	}
}

// A key spelt with other capitals is read (encoding/json ignores case), so
// it is neither reported nor trimmed as unknown.
func TestDoctorStringsKeyCase(t *testing.T) {
	dir := t.TempDir()
	if checks := stringsChecks(t, dir, `{"PauseString": "custom pause"}`); len(checks) != 0 {
		t.Errorf("checks = %+v; want nothing said", checks)
	}
}
