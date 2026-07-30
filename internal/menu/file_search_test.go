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
