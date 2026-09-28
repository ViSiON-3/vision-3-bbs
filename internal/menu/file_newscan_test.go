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

// addNewscanRecord adds a metadata-only record uploaded at when.
func addNewscanRecord(t *testing.T, env *menuEnv, areaID int, name string, when time.Time) {
	t.Helper()
	if err := env.e.FileMgr.AddFileRecord(file.FileRecord{
		ID: uuid.New(), AreaID: areaID, Filename: name, Description: name + " desc\nsecond line",
		Size: 3000, UploadedAt: when, UploadedBy: "Sysop",
	}); err != nil {
		t.Fatalf("AddFileRecord: %v", err)
	}
}

// TestFileNewscanListsFilesSinceCutoff pins FILE_NEWSCAN: only files newer
// than the cutoff are shown, grouped under their area with a count; the
// summary counts them; and with no new files the no-new notice appears.
func TestFileNewscanListsFilesSinceCutoff(t *testing.T) {
	env := newMenuEnv(t)
	cut := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	addNewscanRecord(t, env, 1, "OLD.ZIP", cut.Add(-time.Hour))
	addNewscanRecord(t, env, 1, "FRESH.ZIP", cut.Add(time.Hour))
	addNewscanRecord(t, env, 2, "QUEUED.ZIP", cut.Add(time.Hour))
	env.sysop.FileNewscanSince = &cut

	r := env.runCmd("FILE_NEWSCAN", env.sysop, "", "\r")
	if !r.has("FRESH.ZIP", "QUEUED.ZIP", "FRESH.ZIP desc", "03/01/2026") {
		t.Errorf("new files missing:\n%s", r.text())
	}
	if r.has("OLD.ZIP") || r.has("second line") {
		t.Errorf("old file or second description line shown:\n%s", r.text())
	}

	// The caller cannot list the Upload Queue, so its file is not scanned.
	env.caller.FileNewscanSince = &cut
	r = env.runCmd("FILE_NEWSCAN", env.caller, "", "\r")
	if !r.has("FRESH.ZIP") || r.has("QUEUED.ZIP") {
		t.Errorf("caller scan:\n%s", r.text())
	}

	later := cut.Add(48 * time.Hour)
	env.caller.FileNewscanSince = &later
	r = env.runCmd("FILE_NEWSCAN", env.caller, "", "\r")
	if !r.has(stripPipes(env.e.Strings().FileNewscanNoNew)) || r.has("FRESH.ZIP") {
		t.Errorf("no-new notice missing:\n%s", r.text())
	}

	zero := time.Time{}
	env.caller.FileNewscanSince = &zero
	if r := env.runCmd("FILE_NEWSCAN", env.caller, "", "\r"); !r.has("all files", "OLD.ZIP") {
		t.Errorf("zero cutoff should scan all files:\n%s", r.text())
	}
	if r := env.runCmd("FILE_NEWSCAN", nil, "", ""); r.raw != "" {
		t.Errorf("no user should print nothing:\n%s", r.text())
	}
}

// TestFileNewscanHonoursTagsAndCurrent pins area selection: tagged areas
// limit the scan, and the CURRENT argument scans only the current area.
func TestFileNewscanHonoursTagsAndCurrent(t *testing.T) {
	env := newMenuEnv(t)
	zero := time.Time{}
	env.sysop.FileNewscanSince = &zero
	addNewscanRecord(t, env, 1, "GEN.ZIP", time.Now())
	addNewscanRecord(t, env, 2, "UPQ.ZIP", time.Now())

	env.sysop.TaggedFileAreaTags = []string{"uploads"}
	r := env.runCmd("FILE_NEWSCAN", env.sysop, "", "\r")
	if !r.has("UPQ.ZIP") || r.has("GEN.ZIP") {
		t.Errorf("tagged scan:\n%s", r.text())
	}

	env.sysop.CurrentFileAreaID = 1
	r = env.runCmd("FILE_NEWSCAN", env.sysop, "current", "\r")
	if !r.has("GEN.ZIP") || r.has("UPQ.ZIP") {
		t.Errorf("CURRENT scan:\n%s", r.text())
	}
}

// TestFileNewscanPausesLongResults pins that a long scan pauses each screen
// and a disconnect at the pause ends the scan early.
func TestFileNewscanPausesLongResults(t *testing.T) {
	env := newMenuEnv(t)
	zero := time.Time{}
	env.sysop.FileNewscanSince = &zero
	for i := 0; i < 30; i++ {
		addNewscanRecord(t, env, 1, "F"+string(rune('A'+i%26))+string(rune('A'+i/26))+".ZIP", time.Now())
	}
	r := env.runCmd("FILE_NEWSCAN", env.sysop, "", "")
	if r.err != nil || !r.has("FAA.ZIP") || r.has("FDB.ZIP") {
		t.Errorf("scan should stop at the first pause (err %v):\n%s", r.err, r.text())
	}
	r = env.runCmd("FILE_NEWSCAN", env.sysop, "", "\r\r\r")
	if !r.has("FDB.ZIP") {
		t.Errorf("continuing should reach the last file:\n%s", r.text())
	}
}

// TestFileNewscanConfigTogglesAndSaves pins FILENEWSCANCONFIG: Space/Enter
// toggle the highlighted area, arrows move between areas, and Q saves the
// tagged set to the user record.
func TestFileNewscanConfigTogglesAndSaves(t *testing.T) {
	env := newMenuEnv(t)

	r := env.runCmd("FILENEWSCANCONFIG", env.sysop, "", " \x1b[B\r\x1b[A\x1b[Aq")
	if !r.has("General Files", "Upload Queue") {
		t.Errorf("areas not listed:\n%s", r.text())
	}
	saved := env.mustDiskUser(env.sysop.ID)
	if len(saved.TaggedFileAreaTags) != 2 {
		t.Errorf("tags = %v, want both areas", saved.TaggedFileAreaTags)
	}

	env.runCmd("FILENEWSCANCONFIG", env.sysop, "", " q")
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileAreaTags) != 1 || saved.TaggedFileAreaTags[0] != "UPLOADS" {
		t.Errorf("after untagging General: %v, want [UPLOADS]", saved.TaggedFileAreaTags)
	}
}

// TestFileNewscanConfigAllNoneAndExit pins N (tag none), A (tag all), the
// paging keys, Esc as save-and-exit, and that a disconnect saves nothing.
func TestFileNewscanConfigAllNoneAndExit(t *testing.T) {
	env := newMenuEnv(t)

	env.runCmd("FILENEWSCANCONFIG", env.sysop, "", "a\x1b[6~\x1b[5~\x1b")
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileAreaTags) != 2 {
		t.Errorf("A: tags = %v, want both", saved.TaggedFileAreaTags)
	}
	r := env.runCmd("FILENEWSCANCONFIG", env.sysop, "", "Aq")
	if !r.has("2") {
		t.Errorf("saved notice missing count:\n%s", r.text())
	}
	env.runCmd("FILENEWSCANCONFIG", env.sysop, "", "nQ")
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileAreaTags) != 0 {
		t.Errorf("N: tags = %v, want none", saved.TaggedFileAreaTags)
	}

	if r := env.runCmd("FILENEWSCANCONFIG", env.sysop, "", "a"); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q, want LOGOFF", r.next)
	}
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileAreaTags) != 0 {
		t.Errorf("disconnect saved tags %v", saved.TaggedFileAreaTags)
	}
	if r := env.runCmd("FILENEWSCANCONFIG", nil, "", "q"); r.user != nil {
		t.Errorf("no user: got %v", r.user)
	}
}

// TestFileNewscanConfigNoAccessibleAreas pins that a user who can list no
// area is told so and nothing is saved.
func TestFileNewscanConfigNoAccessibleAreas(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(&user.User{ID: 3, Handle: "Newbie", AccessLevel: 1, Validated: true})
	u, _ := env.um.GetUserByID(3)

	r := env.runCmd("FILENEWSCANCONFIG", u, "", "q")
	if !r.has(stripPipes(env.e.Strings().ScanNoAccessibleAreas)) || r.has("General Files") {
		t.Errorf("no-access notice missing:\n%s", r.text())
	}
}
