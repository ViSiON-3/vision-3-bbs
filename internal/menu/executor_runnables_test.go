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

// TestRunShowStats drives runShowStats through the harness_test.go seam,
// verifying that user-derived placeholders in YOURSTAT.ANS get substituted
// into the rendered output.
func TestRunShowStats(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ansi"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "Handle: |UH Level: |UL Uploads: |UK\r\n"
	if err := os.WriteFile(filepath.Join(root, "ansi", "YOURSTAT.ANS"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	e := &MenuExecutor{MenuSetPath: root}
	e.SetStrings(config.StringsConfig{
		PauseString: "|07Press [ENTER]",
	})
	currentUser := &user.User{
		Handle:      "TestUser",
		AccessLevel: 50,
		NumUploads:  3,
		TimeLimit:   0, // unlimited
	}

	// The pause prompt at the end of runShowStats blocks on a keypress; feed a
	// CR (KeyEnter) so it completes without hanging.
	ts := newTestSession("\r")
	terminal := newTestTerminal(ts)
	c := &cmdCtx{
		e:                e,
		s:                ts,
		terminal:         terminal,
		currentUser:      currentUser,
		nodeNumber:       1,
		sessionStartTime: time.Now(),
		outputMode:       ansi.OutputModeAuto,
		termWidth:        80,
		termHeight:       24,
	}

	resultUser, nextCmd, err := runShowStats(c, "")
	if err != nil {
		t.Fatalf("runShowStats returned error: %v", err)
	}
	if nextCmd != "" {
		t.Errorf("expected empty nextCmd, got %q", nextCmd)
	}
	if resultUser != nil {
		t.Errorf("expected nil user returned, got %+v", resultUser)
	}

	out := ts.output()
	if !strings.Contains(out, "TestUser") {
		t.Errorf("output missing substituted handle: %q", out)
	}
	if !strings.Contains(out, "50") {
		t.Errorf("output missing substituted access level: %q", out)
	}
	if strings.Contains(out, "|UH") || strings.Contains(out, "|UL") {
		t.Errorf("placeholder codes not substituted: %q", out)
	}
}

// TestShowStats_HarnessUTF8 pins SHOWSTATS on the shipped YOURSTAT.ANS in
// UTF-8 mode: the handle and level are substituted, a time-limited user sees
// minutes remaining rather than "Unlimited", and no |XX placeholder survives.
func TestShowStats_HarnessUTF8(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("SHOWSTATS", env.caller, "", "\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Caller", "10") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.has("Unlimited") {
		t.Errorf("60-minute user shown as unlimited:\n%s", r.text())
	}
	for _, code := range []string{"|UH", "|UL", "|TL"} {
		if strings.Contains(r.raw, code) {
			t.Errorf("placeholder %s not substituted", code)
		}
	}
	if r := env.runCmd("SHOWSTATS", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect at pause: next = %q, want LOGOFF", r.next)
	}
}

// TestShowStats_RequiresLogin pins the error shown to a session with no user.
func TestShowStats_RequiresLogin(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("SHOWSTATS", nil, "", "\r")
	if r.err != nil || !r.has("You must be logged in to view stats.") {
		t.Errorf("err = %v output:\n%s", r.err, r.text())
	}
}
