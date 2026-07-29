package menu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newFileAreaExecutor builds a MenuExecutor whose FileMgr serves the given
// file_areas.json content, for exercising setDefaultArea's file branch.
func newFileAreaExecutor(t *testing.T, areasJSON string) *MenuExecutor {
	t.Helper()
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areasJSON), 0644); err != nil {
		t.Fatalf("write areas: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	return &MenuExecutor{FileMgr: fm}
}

// newAreaSeedState wires a runLoopState with a test session/terminal so
// checkACS can be evaluated against u.
func newAreaSeedState(t *testing.T, e *MenuExecutor, u *user.User) *runLoopState {
	t.Helper()
	ts := newTestSession("")
	return &runLoopState{
		e:                e,
		s:                ts,
		terminal:         newTestTerminal(ts),
		currentUser:      u,
		nodeNumber:       1,
		sessionStartTime: time.Now(),
	}
}

func TestSetDefaultAreaSeedsFirstAccessibleFileArea(t *testing.T) {
	// Area 1 requires level 255; area 2 is open. A level-10 user must land on 2.
	e := newFileAreaExecutor(t, `[
		{"id":1,"tag":"STAFF","name":"Staff","path":"staff","acs_list":"s255"},
		{"id":2,"tag":"PUBLIC","name":"Public","path":"public","acs_list":""}
	]`)
	u := &user.User{Handle: "Tester", AccessLevel: 10}
	st := newAreaSeedState(t, e, u)

	st.setDefaultArea("file")

	if u.CurrentFileAreaID != 2 || u.CurrentFileAreaTag != "PUBLIC" {
		t.Fatalf("seeded area = %d/%q, want 2/PUBLIC (first accessible)", u.CurrentFileAreaID, u.CurrentFileAreaTag)
	}
}

func TestSetDefaultAreaClearsStaleTagWhenNoAccess(t *testing.T) {
	// Every area is out of reach, and the user carries a stale tag with ID 0.
	// Seeding must clear the tag rather than leave it pointing at nothing.
	e := newFileAreaExecutor(t, `[
		{"id":1,"tag":"STAFF","name":"Staff","path":"staff","acs_list":"s255"}
	]`)
	u := &user.User{Handle: "Tester", AccessLevel: 10, CurrentFileAreaTag: "GONE"}
	st := newAreaSeedState(t, e, u)

	st.setDefaultArea("file")

	if u.CurrentFileAreaID != 0 || u.CurrentFileAreaTag != "" {
		t.Fatalf("area after denial = %d/%q, want 0/\"\"", u.CurrentFileAreaID, u.CurrentFileAreaTag)
	}
}

func TestSetDefaultAreaKeepsSavedArea(t *testing.T) {
	// A user with a saved area keeps it, even when another area is accessible.
	e := newFileAreaExecutor(t, `[
		{"id":1,"tag":"PUBLIC","name":"Public","path":"public","acs_list":""}
	]`)
	u := &user.User{Handle: "Tester", AccessLevel: 10, CurrentFileAreaID: 7, CurrentFileAreaTag: "SAVED"}
	st := newAreaSeedState(t, e, u)

	st.setDefaultArea("file")

	if u.CurrentFileAreaID != 7 || u.CurrentFileAreaTag != "SAVED" {
		t.Fatalf("saved area = %d/%q, want 7/SAVED untouched", u.CurrentFileAreaID, u.CurrentFileAreaTag)
	}
}
