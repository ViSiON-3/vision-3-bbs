package menu

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runFileLightbar drives runListFilesLightbar through the harness with a small
// in-memory file area (two files) and scripted keystrokes. It pins the control
// flow (quit / EOF / navigation) so the function can be refactored safely.
func runFileLightbar(t *testing.T, input string) (*user.User, string, error, string) {
	t.Helper()
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	areasJSON := `[{"id":1,"tag":"UTILS","name":"Utilities","path":"utils","acs_list":""}]`
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areasJSON), 0644); err != nil {
		t.Fatalf("write areas: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	for _, f := range []struct {
		name string
		size int64
	}{{"readme.txt", 1024}, {"tool.zip", 2048}} {
		if err := fm.AddFileRecord(file.FileRecord{
			ID: uuid.New(), AreaID: 1, Filename: f.name, Description: "desc " + f.name,
			Size: f.size, UploadedAt: time.Now(), UploadedBy: "sysop",
		}); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}
	area, ok := fm.GetAreaByID(1)
	if !ok {
		t.Fatal("area 1 not found after load")
	}

	e := &MenuExecutor{FileMgr: fm}
	u := &user.User{ID: 1, Handle: "Tester", AccessLevel: 255, Validated: true}
	ts := newTestSession(input)
	terminal := newTestTerminal(ts)

	res, action, runErr := runListFilesLightbar(
		e, ts, terminal, nil, u, 1, time.Now(),
		1, "UTILS", area,
		[]byte{}, "", []byte{}, // top / mid / bot templates
		10, 2, 1, // filesPerPage, totalFiles, totalPages
		nil, nil, // cmd/hi bar options -> defaults
		ansi.OutputModeUTF8)
	resetSessionIH(ts)
	return res, action, runErr, ts.output()
}

func TestFileLightbar_QuitClean(t *testing.T) {
	res, action, err, out := runFileLightbar(t, "q")
	if res != nil || action != "" || err != nil {
		t.Fatalf("quit = (%v, %q, %v), want (nil, \"\", nil)", res, action, err)
	}
	if len(out) == 0 {
		t.Error("expected the browser to render something")
	}
}

func TestFileLightbar_EOFLogsOff(t *testing.T) {
	res, action, err, _ := runFileLightbar(t, "")
	if res != nil || action != "LOGOFF" || !errors.Is(err, io.EOF) {
		t.Fatalf("EOF = (%v, %q, %v), want (nil, \"LOGOFF\", io.EOF)", res, action, err)
	}
}

func TestFileLightbar_NavigateDownThenQuit(t *testing.T) {
	// Arrow-down then quit: exercises the move/re-render path before exit.
	// (Navigation is via arrow keys here; 'j' is unmapped and 'k' is kill-file.)
	res, action, err, out := runFileLightbar(t, "\x1b[Bq")
	if res != nil || action != "" || err != nil {
		t.Fatalf("nav+quit = (%v, %q, %v), want (nil, \"\", nil)", res, action, err)
	}
	if len(out) == 0 {
		t.Error("expected rendered output after navigation")
	}
}

func TestFileLightbar_NavigateUpThenQuit(t *testing.T) {
	// Arrow-up at the top clamps, then quit.
	res, action, err, _ := runFileLightbar(t, "\x1b[Aq")
	if res != nil || action != "" || err != nil {
		t.Fatalf("up+quit = (%v, %q, %v), want (nil, \"\", nil)", res, action, err)
	}
}

func TestFileLightbar_EscQuits(t *testing.T) {
	res, action, err, _ := runFileLightbar(t, "\x1b")
	if res != nil || action != "" || err != nil {
		t.Fatalf("esc = (%v, %q, %v), want (nil, \"\", nil)", res, action, err)
	}
}
