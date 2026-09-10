package menu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newTestFileMgr builds a real FileManager from a temp file_areas.json.
func newTestFileMgr(t *testing.T, areas string) *file.FileManager {
	t.Helper()
	dataDir := t.TempDir()
	cfgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatalf("write file_areas.json: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	return fm
}

func TestFileAreasInConference(t *testing.T) {
	// Areas across two conferences, out of ID order in the file, one gated.
	fm := newTestFileMgr(t, `[
	  {"id": 30, "tag": "B30", "name": "Beta", "conference_id": 1, "acs_list": "*"},
	  {"id": 10, "tag": "A10", "name": "Alpha", "conference_id": 1, "acs_list": "*"},
	  {"id": 20, "tag": "S20", "name": "Secret", "conference_id": 1, "acs_list": "S255"},
	  {"id": 40, "tag": "C40", "name": "Gamma", "conference_id": 2, "acs_list": "*"}
	]`)
	e := &MenuExecutor{FileMgr: fm}
	u := &user.User{ID: 2, Handle: "u", AccessLevel: 10}

	got := getAccessibleFileAreasInConference(e, nil, nil, u, 1, time.Time{})
	// Conference 1, ACS-visible only (S20 gated out for a level-10 user), sorted by ID.
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 30 {
		t.Fatalf("conf 1 accessible = %+v, want IDs [10 30]", got)
	}

	first := findFirstAccessibleFileAreaInConference(e, nil, nil, u, 1, time.Time{})
	if first == nil || first.ID != 10 || first.Tag != "A10" {
		t.Fatalf("first accessible = %+v, want ID 10 tag A10", first)
	}

	// Conference 2 has one area; a conference with none returns nil.
	if first := findFirstAccessibleFileAreaInConference(e, nil, nil, u, 2, time.Time{}); first == nil || first.ID != 40 {
		t.Fatalf("conf 2 first = %+v, want ID 40", first)
	}
	if got := getAccessibleFileAreasInConference(e, nil, nil, u, 99, time.Time{}); got != nil {
		t.Fatalf("empty conference returned %+v, want nil", got)
	}
	if first := findFirstAccessibleFileAreaInConference(e, nil, nil, u, 99, time.Time{}); first != nil {
		t.Fatalf("empty conference first = %+v, want nil", first)
	}
}
