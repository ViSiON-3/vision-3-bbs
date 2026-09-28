package menu

import (
	"fmt"
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

// runSearchFiles clamps a result's filename to 12 characters with
// fname[:12], counting bytes. Two sibling renderers (formatFileListLine in
// executor_files_list_render.go, and file_lightbar_render.go) do the
// identical 12-char clamp rune-correctly with []rune, because uploaded
// filenames are not ASCII-normalized: registerUploadedFiles only rejects
// path traversal and duplicates (internal/menu/executor_files_upload.go),
// it does not strip non-ASCII bytes, so a ZMODEM upload with a UTF-8
// filename reaches this unmodified.
func TestRunSearchFilesFilenameClampedByRunes(t *testing.T) {
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	areasJSON := `[{"id":1,"tag":"UTILS","name":"Utilities","path":"utils","acs_list":""}]`
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areasJSON), 0644); err != nil {
		t.Fatalf("write areas: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}

	// "AB" (2 ASCII bytes) + 18 CJK runes (54 bytes): a byte cut at position 12
	// lands 1 byte into the 4th CJK rune, splitting it. The search query
	// matches via the description, not the filename, so the filename's
	// content doesn't need to relate to the query term.
	fname := "AB" + strings.Repeat("日", 18) + ".zip"
	if err := fm.AddFileRecord(file.FileRecord{
		ID: uuid.New(), AreaID: 1, Filename: fname, Description: "search target info",
		Size: 1024, UploadedAt: time.Now(), UploadedBy: "sysop",
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

	e := &MenuExecutor{FileMgr: fm}
	ts := newTestSession("info\r")
	terminal := newTestTerminal(ts)

	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, currentUser: u,
		nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}

	if _, _, err := runSearchFiles(c, ""); err != nil {
		t.Fatalf("runSearchFiles: %v", err)
	}

	out := ts.output()
	// A rune-correct clamp to 12 runes keeps "AB" plus the first 10 "日".
	if got, want := strings.Count(out, "日"), 10; got != want {
		t.Errorf("rendered filename has %d intact '日' runes, want %d (output %q)", got, want, out)
	}
}

// TestSearchFilesMatchesAcrossReadableAreas pins SEARCH_FILES: a query
// matching names or descriptions lists hits from every area the user can
// read, hides hits in areas they cannot, and counts them in the summary.
func TestSearchFilesMatchesAcrossReadableAreas(t *testing.T) {
	env := newMenuEnv(t)
	for _, rec := range []file.FileRecord{
		{ID: uuid.New(), AreaID: 1, Filename: "DOOMWAD.ZIP", Description: "levels"},
		{ID: uuid.New(), AreaID: 1, Filename: "OTHER.ZIP", Description: "a doom clone"},
		{ID: uuid.New(), AreaID: 1, Filename: "UNRELATED.ZIP", Description: "nothing"},
		{ID: uuid.New(), AreaID: 2, Filename: "DOOMQ.ZIP", Description: "queued"},
	} {
		if err := env.e.FileMgr.AddFileRecord(rec); err != nil {
			t.Fatal(err)
		}
	}

	r := env.runCmd("SEARCH_FILES", env.sysop, "", "doom\r\r")
	if !r.has("DOOMWAD.ZIP", "OTHER.ZIP", "DOOMQ.ZIP", "UPLOADS") || r.has("UNRELATED.ZIP") {
		t.Errorf("sysop results:\n%s", r.text())
	}
	r = env.runCmd("SEARCH_FILES", env.caller, "", "doom\r\r")
	if !r.has("DOOMWAD.ZIP", "OTHER.ZIP") || r.has("DOOMQ.ZIP") {
		t.Errorf("caller should not see the Upload Queue:\n%s", r.text())
	}
	if !strings.Contains(r.text(), stripPipes(fmt.Sprintf(env.e.Strings().SearchResultsSummary, 2))) {
		t.Errorf("summary should count 2:\n%s", r.text())
	}
}

// TestSearchFilesRejectsShortAndEmptyQueries pins SEARCH_FILES' guards: a
// blank query returns quietly, fewer than three characters is refused, no
// match says so, a disconnect logs off, and no user is a no-op.
func TestSearchFilesRejectsShortAndEmptyQueries(t *testing.T) {
	env := newMenuEnv(t)
	addDownloadRecords(t, env, "ABC.ZIP")

	if r := env.runCmd("SEARCH_FILES", env.caller, "", "\r"); r.user != env.caller || r.has("ABC.ZIP") {
		t.Errorf("blank query:\n%s", r.text())
	}
	if r := env.runCmd("SEARCH_FILES", env.caller, "", "ab\r"); !strings.Contains(r.text(), stripPipes(env.e.Strings().SearchFilesMinChars)) {
		t.Errorf("short query:\n%s", r.text())
	}
	if r := env.runCmd("SEARCH_FILES", env.caller, "", "zzzz\r"); !strings.Contains(r.text(), stripPipes(env.e.Strings().SearchNoResults)) {
		t.Errorf("no results:\n%s", r.text())
	}
	if r := env.runCmd("SEARCH_FILES", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q", r.next)
	}
	if r := env.runCmd("SEARCH_FILES", nil, "", "abc\r"); r.raw != "" {
		t.Errorf("no user printed:\n%s", r.text())
	}
}

// TestSearchFilesPagesLongResults pins that a long result list pauses per
// screen and a disconnect at the pause stops the listing.
func TestSearchFilesPagesLongResults(t *testing.T) {
	env := newMenuEnv(t)
	names := make([]string, 30)
	for i := range names {
		names[i] = fmt.Sprintf("HIT%02d.ZIP", i)
	}
	addDownloadRecords(t, env, names...)

	r := env.runCmd("SEARCH_FILES", env.sysop, "", "hit\r")
	if !r.has("HIT00.ZIP") || r.has("HIT29.ZIP") {
		t.Errorf("should stop at the first pause:\n%s", r.text())
	}
	r = env.runCmd("SEARCH_FILES", env.sysop, "", "hit\r\r\r\r")
	if !r.has("HIT29.ZIP") {
		t.Errorf("continuing should reach the end:\n%s", r.text())
	}
}
