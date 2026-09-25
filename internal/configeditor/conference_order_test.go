package configeditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
)

// Mirrors a real config: older conferences saved with position 0 and a newer
// one with position 1. The BBS lists the position-1 conference first, then the
// rest by ID; the editor must load them in that same order.
func TestLoadAllConfigsOrdersConferencesLikeBBS(t *testing.T) {
	dir := t.TempDir()
	confs := `[
  {"id": 1, "position": 0, "tag": "LOCAL"},
  {"id": 2, "position": 0, "tag": "FSXNET"},
  {"id": 3, "position": 1, "tag": "DOVENET"}
]`
	if err := os.WriteFile(filepath.Join(dir, "conferences.json"), []byte(confs), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ac, err := loadAllConfigs(dir)
	if err != nil {
		t.Fatalf("loadAllConfigs: %v", err)
	}

	mgr, err := conference.NewConferenceManager(dir)
	if err != nil {
		t.Fatalf("NewConferenceManager: %v", err)
	}
	bbs := mgr.ListConferences()

	if len(ac.Conferences) != len(bbs) {
		t.Fatalf("editor has %d conferences, BBS has %d", len(ac.Conferences), len(bbs))
	}
	for i := range bbs {
		got, want := ac.Conferences[i], bbs[i]
		if got.ID != want.ID || got.Position != want.Position {
			t.Errorf("row %d: editor = id %d pos %d, BBS = id %d pos %d",
				i, got.ID, got.Position, want.ID, want.Position)
		}
	}
}

// Adding a conference while others still have position 0 must place it after
// them, not ahead of them.
func TestNextConferenceIDAndPositionAfterUnset(t *testing.T) {
	confs := []conference.Conference{
		{ID: 1, Position: 0},
		{ID: 2, Position: 0},
	}
	id, pos := nextConferenceIDAndPosition(confs)
	if id != 3 {
		t.Errorf("id = %d, want 3", id)
	}
	if pos != 3 {
		t.Errorf("pos = %d, want 3", pos)
	}
	if confs[0].Position != 1 || confs[1].Position != 2 {
		t.Errorf("existing positions = %d, %d; want 1, 2", confs[0].Position, confs[1].Position)
	}
}
