package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Every menu handler is reachable through the RUN: registry, and a menu that
// runs before login hands the handler a nil user. These four dereferenced the
// user before checking it.
func TestMenuHandlersToleratesNilUser(t *testing.T) {
	handlers := map[string]RunnableFunc{
		"NEWSCAN":  runNewscan,
		"LISTNUV":  runNUVList,
		"SCANNUV":  runNUVScan,
		"WANTLIST": runWantList,
	}
	for name, fn := range handlers {
		t.Run(name, func(t *testing.T) {
			e := &MenuExecutor{RootConfigPath: t.TempDir()}
			e.SetServerConfig(config.ServerConfig{UseNUV: true, CoSysOpLevel: 250, SysOpLevel: 255})
			ts := newTestSession("")
			c := &cmdCtx{e: e, s: ts, terminal: newTestTerminal(ts), nodeNumber: 1,
				sessionStartTime: time.Now(), outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
			u, next, err := fn(c, "")
			if u != nil || next != "" || err != nil {
				t.Fatalf("%s with nil user returned (%v, %q, %v), want (nil, \"\", nil)", name, u, next, err)
			}
		})
	}
}

// isSysOpOrAbove follows sysOpLevel from config.json rather than a literal 255,
// so a board that lowers the level gets sysop treatment everywhere.
func TestIsSysOpOrAboveUsesConfiguredLevel(t *testing.T) {
	e := &MenuExecutor{}
	e.SetServerConfig(config.ServerConfig{SysOpLevel: 200, CoSysOpLevel: 150})

	if e.isSysOpOrAbove(nil) {
		t.Error("nil user must not be sysop")
	}
	if !e.isSysOpOrAbove(&user.User{AccessLevel: 200}) {
		t.Error("level 200 should be sysop when sysOpLevel is 200")
	}
	if e.isSysOpOrAbove(&user.User{AccessLevel: 199}) {
		t.Error("level 199 should not be sysop when sysOpLevel is 200")
	}
	if !e.isCoSysOpOrAbove(&user.User{AccessLevel: 150}) || e.isCoSysOpOrAbove(&user.User{AccessLevel: 149}) {
		t.Error("isCoSysOpOrAbove should follow coSysOpLevel")
	}
}

func newFileNewscanFixture(t *testing.T, areasJSON string) (*file.FileManager, *user.UserMgr, *user.User, time.Time) {
	t.Helper()
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areasJSON), 0644); err != nil {
		t.Fatalf("write areas: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	uploadTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, rec := range []struct {
		area int
		name string
	}{{1, "in-utils.zip"}, {2, "in-games.zip"}} {
		if err := fm.AddFileRecord(file.FileRecord{
			ID: uuid.New(), AreaID: rec.area, Filename: rec.name, Description: "desc",
			Size: 10, UploadedAt: uploadTime, UploadedBy: "sysop",
		}); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
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
	return fm, um, u, uploadTime
}

func runFileNewscanOutput(t *testing.T, fm *file.FileManager, um *user.UserMgr, u *user.User, at time.Time, args string) string {
	t.Helper()
	e := &MenuExecutor{FileMgr: fm}
	ts := newTestSession("")
	c := &cmdCtx{
		e: e, s: ts, terminal: newTestTerminal(ts), userManager: um, currentUser: u,
		nodeNumber: 1, sessionStartTime: at,
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}
	if _, _, err := runFileNewscan(c, args); err != nil {
		t.Fatalf("runFileNewscan: %v", err)
	}
	return ts.output()
}

const twoFileAreasJSON = `[{"id":1,"tag":"UTILS","name":"Utilities","path":"utils","acs_list":""},
{"id":2,"tag":"GAMES","name":"Games","path":"games","acs_list":""}]`

// FILENEWSCANCONFIG stores the user's tagged file areas; the scan must honour
// them, fall back to every area when nothing is tagged, and ignore them when
// the menu asks for the current area only.
func TestFileNewscanHonoursTaggedAreas(t *testing.T) {
	fm, um, u, at := newFileNewscanFixture(t, twoFileAreasJSON)

	t.Run("nothing tagged scans every area", func(t *testing.T) {
		u.TaggedFileAreaTags = nil
		out := runFileNewscanOutput(t, fm, um, u, at, "")
		if !strings.Contains(out, "in-utils.zip") || !strings.Contains(out, "in-games.zip") {
			t.Errorf("expected both areas in output, got %q", out)
		}
	})

	t.Run("tagged areas restrict the scan", func(t *testing.T) {
		u.TaggedFileAreaTags = []string{"games"}
		out := runFileNewscanOutput(t, fm, um, u, at, "")
		if strings.Contains(out, "in-utils.zip") {
			t.Errorf("untagged UTILS area should be skipped, got %q", out)
		}
		if !strings.Contains(out, "in-games.zip") {
			t.Errorf("tagged GAMES area should be scanned, got %q", out)
		}
	})

	t.Run("CURRENT ignores tags", func(t *testing.T) {
		u.TaggedFileAreaTags = []string{"GAMES"}
		u.CurrentFileAreaID = 1
		out := runFileNewscanOutput(t, fm, um, u, at, "CURRENT")
		if !strings.Contains(out, "in-utils.zip") || strings.Contains(out, "in-games.zip") {
			t.Errorf("CURRENT should scan only area 1, got %q", out)
		}
	})
}
