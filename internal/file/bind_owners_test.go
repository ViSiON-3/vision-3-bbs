package file

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Records from before uploader IDs are bound once to the account holding
// the handle; records already bound, or bound to nobody, are left alone.
func TestBindUploaders(t *testing.T) {
	dir := t.TempDir()
	dataDir, configDir := filepath.Join(dir, "data"), filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})
	if err := os.WriteFile(filepath.Join(configDir, "file_areas.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	fm, err := NewFileManager(dataDir, configDir)
	if err != nil {
		t.Fatal(err)
	}
	recs := map[string]FileRecord{
		"OLD.ZIP":  {UploadedBy: "Alice"},
		"GONE.ZIP": {UploadedBy: "Ghost"},
		"NEW.ZIP":  {UploadedBy: "Alice", UploadedByID: 3},
		"TIC.ZIP":  {UploadedBy: "Bob", UploadedByID: NoUploaderID},
	}
	for name, r := range recs {
		r.ID, r.AreaID, r.Filename = uuid.New(), 1, name
		recs[name] = r
		if err := fm.AddFileRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	ids := map[string]int{"alice": 5, "bob": 6}
	bound, unowned, err := fm.BindUploaders(func(h string) int { return ids[strings.ToLower(h)] })
	if err != nil || bound != 1 || unowned != 1 {
		t.Fatalf("BindUploaders = %d, %d, %v", bound, unowned, err)
	}
	for name, want := range map[string]int{"OLD.ZIP": 5, "GONE.ZIP": NoUploaderID, "NEW.ZIP": 3, "TIC.ZIP": NoUploaderID} {
		got, err := fm.GetFileRecordByID(recs[name].ID)
		if err != nil || got.UploadedByID != want {
			t.Errorf("%s UploadedByID = %+v, want %d", name, got, want)
		}
	}
	if !(FileRecord{UploadedByID: 5}).UploadedByAccount(5) || (FileRecord{UploadedByID: NoUploaderID}).UploadedByAccount(-1) {
		t.Error("UploadedByAccount")
	}
}

// The area-guarded operations act only while the record is in the area the
// caller expects, and change nothing otherwise.
func TestAreaGuardedUpdateAndMove(t *testing.T) {
	dir := t.TempDir()
	dataDir, configDir := filepath.Join(dir, "data"), filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]FileArea{{ID: 1, Tag: "A", Name: "A", Path: "a"}, {ID: 2, Tag: "B", Name: "B", Path: "b"}, {ID: 3, Tag: "C", Name: "C", Path: "c"}})
	if err := os.WriteFile(filepath.Join(configDir, "file_areas.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	fm, err := NewFileManager(dataDir, configDir)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := fm.AddFileRecord(FileRecord{ID: id, AreaID: 1, Filename: "X.ZIP", Description: "old"}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(dataDir, "files", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := fm.GetFilePath(id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fm.UpdateFileRecordInArea(id, 2, func(r *FileRecord) { r.Description = "wrong" }); !errors.Is(err, ErrRecordMoved) {
		t.Errorf("update in the wrong area: %v", err)
	}
	if err := fm.MoveFileRecordFromArea(id, 2, 3); !errors.Is(err, ErrRecordMoved) {
		t.Errorf("move from the wrong area: %v", err)
	}
	if err := fm.UpdateFileRecordInArea(id, 1, func(r *FileRecord) { r.Description = "new" }); err != nil {
		t.Fatal(err)
	}
	if err := fm.MoveFileRecordFromArea(id, 1, 2); err != nil {
		t.Fatal(err)
	}
	got, _ := fm.GetFileRecordByID(id)
	if got.AreaID != 2 || got.Description != "new" {
		t.Errorf("record = %+v", got)
	}
}

// RenameFileInArea renames on disk and in the record together, only while
// the file is in the expected area, and never over another file.
func TestRenameFileInArea(t *testing.T) {
	dir := t.TempDir()
	dataDir, configDir := filepath.Join(dir, "data"), filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]FileArea{{ID: 1, Tag: "A", Name: "A", Path: "a"}, {ID: 2, Tag: "B", Name: "B", Path: "b"}})
	if err := os.WriteFile(filepath.Join(configDir, "file_areas.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	fm, err := NewFileManager(dataDir, configDir)
	if err != nil {
		t.Fatal(err)
	}
	add := func(area int, name string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if err := fm.AddFileRecord(FileRecord{ID: id, AreaID: area, Filename: name}); err != nil {
			t.Fatal(err)
		}
		p, _ := fm.GetFilePath(id)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := add(1, "OLD.ZIP")
	add(1, "TAKEN.ZIP")
	areaA, _ := fm.areaDir(1)
	if err := os.WriteFile(filepath.Join(areaA, "STRAY.ZIP"), []byte("stray"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		area int
		name string
		want error
	}{
		{2, "NEW.ZIP", ErrRecordMoved},          // not in area 2
		{1, "TAKEN.ZIP", ErrRenameTargetExists}, // another record's name
		{1, "taken.zip", ErrRenameTargetExists}, // ignoring case
		{1, "STRAY.ZIP", ErrRenameTargetExists}, // a file on disk with no record
		{1, "stray.zip", ErrRenameTargetExists}, // ...in any case
		{1, "../escape.zip", nil},               // refused as a path (checked below)
	} {
		err := fm.RenameFileInArea(id, tc.area, tc.name)
		if tc.want == nil {
			if err == nil {
				t.Errorf("rename to %q succeeded", tc.name)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("rename to %q in area %d: %v, want %v", tc.name, tc.area, err, tc.want)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(areaA, "STRAY.ZIP")); string(b) != "stray" {
		t.Error("the stray file was overwritten")
	}

	if err := fm.RenameFileInArea(id, 1, "NEW.ZIP"); err != nil {
		t.Fatal(err)
	}
	got, _ := fm.GetFileRecordByID(id)
	if got.Filename != "NEW.ZIP" {
		t.Errorf("record name = %q", got.Filename)
	}
	if _, err := os.Stat(filepath.Join(areaA, "NEW.ZIP")); err != nil {
		t.Errorf("file not renamed on disk: %v", err)
	}
	// A case-only change is allowed.
	if err := fm.RenameFileInArea(id, 1, "new.zip"); err != nil {
		t.Errorf("case-only rename: %v", err)
	}

	// Moved to area 2, it can't be renamed as area 1's file, and nothing in
	// area 2 changes.
	if err := fm.MoveFileRecord(id, 2); err != nil {
		t.Fatal(err)
	}
	if err := fm.RenameFileInArea(id, 1, "SNEAKY.ZIP"); !errors.Is(err, ErrRecordMoved) {
		t.Errorf("rename after a move: %v", err)
	}
	areaB, _ := fm.areaDir(2)
	if _, err := os.Stat(filepath.Join(areaB, "SNEAKY.ZIP")); !os.IsNotExist(err) {
		t.Error("the moved file was renamed in its new area")
	}
}
