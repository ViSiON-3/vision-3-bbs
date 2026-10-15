package configeditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

func writeAreasFile(t *testing.T, dir string, areas []message.MessageArea) {
	t.Helper()
	lock, err := message.LockAreasFile(filepath.Join(dir, "message_areas.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if _, err := message.WriteAreasFile(filepath.Join(dir, "message_areas.json"), areas); err != nil {
		t.Fatal(err)
	}
}

// TestSaveMsgAreasKeepsExternalEdits is issue #639 from the editor's side: a
// save, even one that changed no area, must not undo area changes written to
// disk after the editor loaded them (by helper or the BBS).
func TestSaveMsgAreasKeepsExternalEdits(t *testing.T) {
	dir := t.TempDir()
	writeAreasFile(t, dir, []message.MessageArea{
		{ID: 1, Position: 1, Tag: "ONE", Name: "One", AreaType: "local"},
		{ID: 2, Position: 2, Tag: "TWO", Name: "Two", AreaType: "local"},
	})
	ac, err := loadAllConfigs(dir)
	if err != nil {
		t.Fatal(err)
	}

	// helper areas set runs while the editor is open.
	writeAreasFile(t, dir, []message.MessageArea{
		{ID: 1, Position: 1, Tag: "ONE", Name: "One", AreaType: "local", RealNameOnly: true},
		{ID: 2, Position: 2, Tag: "TWO", Name: "Two", AreaType: "local", RealNameOnly: true},
	})

	// The sysop renames area 2 in the editor.
	for i := range ac.MsgAreas {
		if ac.MsgAreas[i].ID == 2 {
			ac.MsgAreas[i].Name = "Two Renamed"
		}
	}
	conflicts, err := saveMsgAreas(dir, &ac)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %v", conflicts)
	}

	disk := reloadConfigs(t, dir).MsgAreas
	for _, a := range disk {
		if !a.RealNameOnly {
			t.Errorf("area %s lost the external RealNameOnly", a.Tag)
		}
		if a.ID == 2 && a.Name != "Two Renamed" {
			t.Errorf("area 2 lost the editor's rename: %q", a.Name)
		}
	}
	// The editor's copy now matches disk, so the next save is clean.
	for _, a := range ac.MsgAreas {
		if !a.RealNameOnly {
			t.Errorf("editor copy of %s not updated", a.Tag)
		}
	}
}

// With no area changed in the editor and nothing changed on disk, a save
// leaves message_areas.json alone, so a running BBS is not made to reload it.
func TestSaveMsgAreasUnchangedLeavesFile(t *testing.T) {
	dir := t.TempDir()
	writeAreasFile(t, dir, []message.MessageArea{{ID: 1, Position: 1, Tag: "ONE", AreaType: "local"}})
	path := filepath.Join(dir, "message_areas.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	ac, err := loadAllConfigs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveMsgAreas(dir, &ac); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("message_areas.json was rewritten although nothing changed")
	}
}

// A failed write must leave the editor's areas and merge base untouched, or a
// retry would read the sysop's edits as unchanged and drop them.
func TestSaveMsgAreasFailedWriteKeepsEdits(t *testing.T) {
	dir := t.TempDir()
	writeAreasFile(t, dir, []message.MessageArea{{ID: 1, Position: 1, Tag: "ONE", Name: "One", AreaType: "local"}})
	ac, err := loadAllConfigs(dir)
	if err != nil {
		t.Fatal(err)
	}
	ac.MsgAreas[0].Name = "Edited"

	// Make the write fail: a directory where the temp file would go.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	_, saveErr := saveMsgAreas(dir, &ac)
	_ = os.Chmod(dir, 0o755)
	if saveErr == nil {
		t.Skip("directory permissions not enforced (running as root?)")
	}
	if ac.MsgAreas[0].Name != "Edited" || ac.msgAreasBase[0].Name != "One" {
		t.Fatalf("failed save changed editor state: areas=%q base=%q", ac.MsgAreas[0].Name, ac.msgAreasBase[0].Name)
	}
	if _, err := saveMsgAreas(dir, &ac); err != nil {
		t.Fatal(err)
	}
	if got := reloadConfigs(t, dir).MsgAreas[0].Name; got != "Edited" {
		t.Errorf("retry saved %q, want the edit", got)
	}
}
