package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Doctor notes strings.json keys the BBS doesn't read, and not the caller
// colour slot defaults or _-prefixed metadata it keeps.
func TestDoctorNotesUnknownStringsKeys(t *testing.T) {
	dir := t.TempDir()
	run := func(content string) []doctorCheck {
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
	checks := run(`{"pauseString": "x", "Read_Feedback": "Read Feedback?", "execReadmailPlaceholder": "%s", "defColor1": 12, "_note": "mine"}`)
	if !hasDoctorCheck(checks, "configs/strings.json", doctorInfo, "2 key(s) the BBS does not read: Read_Feedback, execReadmailPlaceholder") {
		t.Errorf("checks = %+v", checks)
	}
	if checks := run(`{"pauseString": "x", "defColor7": 3}`); len(checks) != 0 {
		t.Errorf("known keys only: %+v; want nothing said", checks)
	}
	if checks := run(`{`); len(checks) != 0 {
		t.Errorf("broken file: %+v; want it left to the parse check", checks)
	}
}

// A key spelt with other capitals is read (encoding/json ignores case), so
// it is not reported.
func TestDoctorStringsKeyCase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "strings.json"), []byte(`{"PauseString": "custom pause"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var checks []doctorCheck
	checkStringsKeys(dir, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if len(checks) != 0 {
		t.Errorf("checks = %+v; want nothing said", checks)
	}
}
