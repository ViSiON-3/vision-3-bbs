package menu

import (
	"errors"
	"fmt"
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
// scripted input — letting navigation/paging be pinned by comparing output.
var fixedUploadTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// runFileLightbarN drives runListFilesLightbar through the harness with an
// in-memory area of numFiles files (file00.txt, file01.txt, …) and a real user
// manager (the tag path persists via UpdateUser). The mid template renders each
// file's mark/number/name/size so output reflects selection and tagging.
func runFileLightbarN(t *testing.T, input string, numFiles int) (*user.User, string, error, string) {
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
	for i := 0; i < numFiles; i++ {
		if err := fm.AddFileRecord(file.FileRecord{
			ID: uuid.New(), AreaID: 1, Filename: fmt.Sprintf("file%02d.txt", i),
			Size: int64(1024 * (i + 1)), UploadedAt: fixedUploadTime, UploadedBy: "sysop",
		}); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}
	area, ok := fm.GetAreaByID(1)
	if !ok {
		t.Fatal("area 1 not found after load")
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
	ts := newTestSession(input)
	terminal := newTestTerminal(ts)

	res, action, runErr := runListFilesLightbar(
		e, ts, terminal, um, u, 1, fixedUploadTime,
		1, "UTILS", area,
		[]byte{}, "^MARK^NUM ^NAME ^SIZE", []byte{}, // top / mid / bot templates
		10, numFiles, (numFiles+9)/10, // filesPerPage, totalFiles, totalPages
		nil, nil, // cmd/hi bar options -> defaults
		ansi.OutputModeUTF8)
	resetSessionIH(ts)
	return res, action, runErr, ts.output()
}

func runFileLightbar(t *testing.T, input string) (*user.User, string, error, string) {
	t.Helper()
	return runFileLightbarN(t, input, 2)
}

func TestFileLightbar_QuitClean(t *testing.T) {
	res, action, err, out := runFileLightbar(t, "q")
	if res != nil || action != "" || err != nil {
		t.Fatalf("quit = (%v, %q, %v), want (nil, \"\", nil)", res, action, err)
	}
	if !strings.Contains(out, "file00.txt") || !strings.Contains(out, "file01.txt") {
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

func TestFileLightbar_ArrowDownMovesSelection(t *testing.T) {
	_, _, _, noNav := runFileLightbar(t, "q")
	_, _, _, down := runFileLightbar(t, "\x1b[Bq")
	if down == noNav {
		t.Error("arrow-down produced identical output — selection did not move")
	}
}

func TestFileLightbar_ArrowUpAtTopClamps(t *testing.T) {
	_, _, _, noNav := runFileLightbar(t, "q")
	_, _, _, up := runFileLightbar(t, "\x1b[Aq")
	if up != noNav {
		t.Error("arrow-up at the top changed the render — should clamp at the first file")
	}
}

// TestFileLightbar_SpaceTogglesTag pins the tag path: Space marks the selected
// file (persisting via UpdateUser), which renders as a '*' in the ^MARK column.
func TestFileLightbar_SpaceTogglesTag(t *testing.T) {
	_, _, _, untagged := runFileLightbar(t, "q")
	_, _, _, tagged := runFileLightbar(t, " q")
	if strings.Contains(untagged, "*") {
		t.Error("no file should be marked before tagging")
	}
	if !strings.Contains(tagged, "*") {
		t.Error("Space should mark the selected file with '*'")
	}
}

// TestFileLightbar_CmdBarNavChangesRender pins that arrow-right moves the command
// bar selection (re-rendering the bar differently).
func TestFileLightbar_CmdBarNavChangesRender(t *testing.T) {
	_, _, _, noNav := runFileLightbar(t, "q")
	_, _, _, right := runFileLightbar(t, "\x1b[Cq")
	if right == noNav {
		t.Error("arrow-right produced identical output — command-bar selection did not move")
	}
}

// TestFileLightbar_PageDownChangesView pins paging: with more files than fit on
// one screen, PageDown shows a different slice of the list.
func TestFileLightbar_PageDownChangesView(t *testing.T) {
	_, _, _, page1 := runFileLightbarN(t, "q", 30)
	_, _, _, paged := runFileLightbarN(t, "\x1b[6~q", 30)
	if paged == page1 {
		t.Error("PageDown produced identical output with 30 files — paging did not advance")
	}
}

// TestFileLightbar_EndJumpsToLastFile pins End: with more files than fit on a
// screen, End scrolls to the last page (a different rendered slice than the top).
func TestFileLightbar_EndJumpsToLastFile(t *testing.T) {
	_, _, _, top := runFileLightbarN(t, "q", 30)
	_, _, _, end := runFileLightbarN(t, "\x1b[Fq", 30)
	if end == top {
		t.Error("End produced identical output with 30 files — did not jump to the last page")
	}
	// The last file should be visible after End but not on the first screen.
	if strings.Contains(top, "file29.txt") {
		t.Skip("all files fit on one screen; End-paging not exercised")
	}
	if !strings.Contains(end, "file29.txt") {
		t.Error("End should scroll the last file (file29.txt) into view")
	}
}
