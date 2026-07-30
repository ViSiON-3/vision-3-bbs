package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runNewscanConfig's message-area picker lays out area names with an inline
// padRight closure and an areaName[:37]-style truncation, both measuring
// len() in bytes — the same class of bug fixed in runFileNewscanConfig
// (internal/menu/file_newscan.go). Area names are sysop-editable UTF-8 JSON
// and can be auto-created from hub-supplied names by V3Net area sync, so
// both are reachable with real data.
func TestRunNewscanConfigAreaNameRuneCorrect(t *testing.T) {
	// 20 CJK runes = 60 bytes: rune count is well under the 40-column limit,
	// but the byte length is over it, so a byte-length truncation check wrongly
	// truncates a name that should render in full, splitting mid-rune.
	longName := strings.Repeat("日", 20)
	// 20 "é" runes = 40 bytes exactly: byte length reaches the 40-column pad
	// width even though the rune (visible) length is only 20, so a byte-length
	// pad check skips padding a name that is visibly 20 columns short.
	padName := strings.Repeat("é", 20)

	dataDir, cfgDir := t.TempDir(), t.TempDir()
	mm, err := message.NewMessageManager(dataDir, cfgDir, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	if _, err := mm.AddArea(message.MessageArea{Tag: "AREA1", Name: longName, AreaType: "local"}); err != nil {
		t.Fatalf("AddArea longName: %v", err)
	}
	if _, err := mm.AddArea(message.MessageArea{Tag: "AREA2", Name: padName, AreaType: "local"}); err != nil {
		t.Fatalf("AddArea padName: %v", err)
	}

	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u.AccessLevel = 255

	e := &MenuExecutor{MessageMgr: mm}
	ts := newTestSession("q")
	terminal := newTestTerminal(ts)

	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, userManager: um, currentUser: u,
		nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}

	if _, _, err := runNewscanConfig(c, ""); err != nil {
		t.Fatalf("runNewscanConfig: %v", err)
	}

	stripped := testAnsiEscape.ReplaceAllString(ts.output(), "")

	if got, want := strings.Count(stripped, "日"), 20; got != want {
		t.Errorf("rendered long area name has %d intact '日' runes, want %d (all 20 fit under the 40-column limit): %q",
			got, want, stripped)
	}

	wantPadded := padName + strings.Repeat(" ", 20) + " ["
	if !strings.Contains(stripped, wantPadded) {
		t.Errorf("rendered short area name row missing %d columns of padding before the bracket; want substring %q in %q",
			20, wantPadded, stripped)
	}
}
