package file

import (
	"encoding/json"
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
