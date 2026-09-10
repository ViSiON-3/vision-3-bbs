package file

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// writeAreas rewrites file_areas.json in the manager's config path.
func writeAreas(t *testing.T, fm *FileManager, areas []FileArea) {
	t.Helper()
	data, err := json.Marshal(areas)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fm.configPath, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReloadReplacesAreasAndRecords(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{
		{ID: 1, Tag: "OLD", Name: "Old", Path: "old"},
	})

	// A record in the area that is about to be removed.
	if err := os.MkdirAll(filepath.Join(fm.basePath, "old"), 0755); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := fm.AddFileRecord(FileRecord{ID: id, AreaID: 1, Filename: "gone.zip", Description: "d"}); err != nil {
		t.Fatal(err)
	}

	writeAreas(t, fm, []FileArea{
		{ID: 2, Tag: "NEW", Name: "New", Path: "new"},
	})
	if err := fm.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if _, ok := fm.GetAreaByTag("OLD"); ok {
		t.Error("removed area OLD still resolvable by tag")
	}
	if _, ok := fm.GetAreaByTag("NEW"); !ok {
		t.Error("added area NEW not resolvable by tag")
	}
	if _, err := fm.GetFilePath(id); err == nil {
		t.Error("record of a removed area still resolvable after reload")
	}
}

// TestReloadKeepsOldOnBadFile: a malformed save must not wipe the running
// definitions.
func TestReloadKeepsOldOnBadFile(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{
		{ID: 1, Tag: "KEEP", Name: "Keep", Path: "keep"},
	})

	if err := os.WriteFile(fm.configPath, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fm.Reload(); err == nil {
		t.Fatal("Reload succeeded on a malformed file")
	}
	if _, ok := fm.GetAreaByTag("KEEP"); !ok {
		t.Error("area KEEP lost after a failed reload; old definitions must survive")
	}
}

// TestReloadRereadsRecordsFromDisk: records edited on disk (another tool, a
// restore) are picked up, which is the point of re-reading metadata rather
// than carrying the in-memory set across.
func TestReloadRereadsRecordsFromDisk(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{
		{ID: 1, Tag: "A", Name: "A", Path: "a"},
	})

	id := uuid.New()
	records := []FileRecord{{ID: id, AreaID: 1, Filename: "ext.zip", Description: "added externally"}}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(fm.basePath, "a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fm.basePath, "a", "metadata.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := fm.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, err := fm.GetFilePath(id); err != nil {
		t.Errorf("externally added record not visible after reload: %v", err)
	}
}

// TestReloadErrorsOnMissingFile pins the review fix on #331: a missing
// file_areas.json is a reload error keeping the old definitions — not a
// silent success that recreates an empty file whose fresh timestamp would
// later wipe every area.
func TestReloadErrorsOnMissingFile(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{
		{ID: 1, Tag: "KEEP", Name: "Keep", Path: "keep"},
	})

	if err := os.Remove(fm.configPath); err != nil {
		t.Fatal(err)
	}
	if err := fm.Reload(); err == nil {
		t.Fatal("Reload succeeded with file_areas.json missing")
	}
	if _, ok := fm.GetAreaByTag("KEEP"); !ok {
		t.Error("definitions lost after a refused reload")
	}
	if _, err := os.Stat(fm.configPath); !os.IsNotExist(err) {
		t.Error("a refused reload recreated file_areas.json")
	}
}
