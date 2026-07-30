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

	e := &MenuExecutor{
		MenuSetPath: root,
		LoadedStrings: config.StringsConfig{
			PauseString: "|07Press [ENTER]",
		},
	}
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
