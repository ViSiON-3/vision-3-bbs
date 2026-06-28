package menu

import (
	"errors"
	"io"
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

// fixedUploadTime keeps the fixture deterministic so two runs differ only by the
// scripted input — letting navigation be pinned by comparing rendered output.
var fixedUploadTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// runFileLightbar drives runListFilesLightbar through the harness with a small
// in-memory file area (two files) and scripted keystrokes. The mid template
// renders each file's number/name/size so the output reflects the file list.
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
			Size: f.size, UploadedAt: fixedUploadTime, UploadedBy: "sysop",
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
		e, ts, terminal, nil, u, 1, fixedUploadTime,
		1, "UTILS", area,
		[]byte{}, "^NUM ^NAME ^SIZE", []byte{}, // top / mid / bot templates
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
	// The list rendered both files.
	if !strings.Contains(out, "readme.txt") || !strings.Contains(out, "tool.zip") {
		t.Errorf("expected both filenames in rendered output")
	}
}

func TestFileLightbar_EOFLogsOff(t *testing.T) {
	res, action, err, _ := runFileLightbar(t, "")
	if res != nil || action != "LOGOFF" || !errors.Is(err, io.EOF) {
		t.Fatalf("EOF = (%v, %q, %v), want (nil, \"LOGOFF\", io.EOF)", res, action, err)
	}
}

func TestFileLightbar_EscQuits(t *testing.T) {
	res, action, err, _ := runFileLightbar(t, "\x1b")
	if res != nil || action != "" || err != nil {
		t.Fatalf("esc = (%v, %q, %v), want (nil, \"\", nil)", res, action, err)
	}
}

// TestFileLightbar_ArrowDownMovesSelection pins that arrow-down actually changes
// the render (the highlight moves to the next file) — not just that the function
// quits. The fixture is deterministic, so any difference is the navigation.
func TestFileLightbar_ArrowDownMovesSelection(t *testing.T) {
	_, _, _, noNav := runFileLightbar(t, "q")
	_, _, _, down := runFileLightbar(t, "\x1b[Bq")
	if down == noNav {
		t.Error("arrow-down produced identical output — selection did not move")
	}
}

// TestFileLightbar_ArrowUpAtTopClamps pins that arrow-up at the first file is a
// no-op: its render must match the no-navigation render exactly.
func TestFileLightbar_ArrowUpAtTopClamps(t *testing.T) {
	_, _, _, noNav := runFileLightbar(t, "q")
	_, _, _, up := runFileLightbar(t, "\x1b[Aq")
	if up != noNav {
		t.Error("arrow-up at the top changed the render — should clamp at the first file")
	}
}
