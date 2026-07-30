package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runFileNewscan renders FILE_ID.DIZ descriptions that internal/ziplab/diz.go
// has already converted to UTF-8. desc[:maxDesc-3] cut on bytes, so a
// multi-byte description is sliced mid-rune: the dangling lead byte is not
// valid UTF-8, and terminalio's UTF-8 writer maps it to an unrelated CP437
// glyph rather than the truncated text, silently dropping runes from the
// description. maxDesc=40 (termWidth 80 - 40) here, so the byte cut at
// maxDesc-3=37 bytes lands after only 12 whole "日" runes (36 bytes) plus one
// stray lead byte, instead of the 37 runes a rune-correct cut would keep.
func TestRunFileNewscanNonASCIIDescriptionNotSplitMidRune(t *testing.T) {
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	areasJSON := `[{"id":1,"tag":"UTILS","name":"Utilities","path":"utils","acs_list":""}]`
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areasJSON), 0644); err != nil {
		t.Fatalf("write areas: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}

	// 50 CJK runes (150 bytes): well past maxDesc (40, rune-correct), and far
	// enough past it in bytes (150) that a byte-based cut lands inside a rune.
	desc := strings.Repeat("日", 50)
	uploadTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := fm.AddFileRecord(file.FileRecord{
		ID: uuid.New(), AreaID: 1, Filename: "test.zip", Description: desc,
		Size: 1024, UploadedAt: uploadTime, UploadedBy: "sysop",
	}); err != nil {
		t.Fatalf("AddFileRecord: %v", err)
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
	u.LastLogin = uploadTime.Add(-time.Hour)

	e := &MenuExecutor{FileMgr: fm}
	ts := newTestSession("")
	terminal := newTestTerminal(ts)

	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, userManager: um, currentUser: u,
		nodeNumber: 1, sessionStartTime: uploadTime,
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}

	if _, _, err := runFileNewscan(c, ""); err != nil {
		t.Fatalf("runFileNewscan: %v", err)
	}

	out := ts.output()
	// A rune-correct cut keeps 37 whole "日" runes before the "..." ellipsis.
	if got, want := strings.Count(out, "日"), 37; got != want {
		t.Errorf("rendered description has %d intact '日' runes, want %d (output %q)", got, want, out)
	}
}

// runFileNewscanConfig's picker lays out area names with an inline padRight
// closure and an areaName[:37]-style truncation, both measuring len() in
// bytes. Area names are sysop-editable UTF-8 JSON and can be auto-created
// from hub-supplied names by V3Net area sync, so both are reachable with
// real data.
func TestRunFileNewscanConfigAreaNameRuneCorrect(t *testing.T) {
	// 20 CJK runes = 60 bytes: rune count is well under the 40-column limit,
	// but the byte length is over it, so a byte-length truncation check wrongly
	// truncates a name that should render in full, splitting mid-rune.
	longName := strings.Repeat("日", 20)
	// 20 "é" runes = 40 bytes exactly: byte length reaches the 40-column pad
	// width even though the rune (visible) length is only 20, so a byte-length
	// pad check skips padding a name that is visibly 20 columns short.
	padName := strings.Repeat("é", 20)

	dataDir, cfgDir := t.TempDir(), t.TempDir()
	areasJSON := `[
		{"id":1,"tag":"AREA1","name":"` + longName + `","path":"a1","acs_list":""},
		{"id":2,"tag":"AREA2","name":"` + padName + `","path":"a2","acs_list":""}
	]`
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areasJSON), 0644); err != nil {
		t.Fatalf("write areas: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
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

	e := &MenuExecutor{FileMgr: fm}
	ts := newTestSession("q")
	terminal := newTestTerminal(ts)

	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, userManager: um, currentUser: u,
		nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}

	if _, _, err := runFileNewscanConfig(c, ""); err != nil {
		t.Fatalf("runFileNewscanConfig: %v", err)
	}

	stripped := testAnsiEscape.ReplaceAllString(ts.output(), "")

	if got, want := strings.Count(stripped, "日"), 20; got != want {
		t.Errorf("rendered long area name has %d intact '日' runes, want %d (all 20 fit under the 40-column limit): %q",
			got, want, stripped)
	}

	// padRight must pad padName (20 visible columns) out to 40 columns before
	// the " [" status bracket.
	wantPadded := padName + strings.Repeat(" ", 20) + " ["
	if !strings.Contains(stripped, wantPadded) {
		t.Errorf("rendered short area name row missing %d columns of padding before the bracket; want substring %q in %q",
			20, wantPadded, stripped)
	}
}
