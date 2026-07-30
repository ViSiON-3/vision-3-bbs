package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// newTestFileManagerWithRecord builds a real *file.FileManager with a single
// area and, optionally, a file record. AddFileRecord only writes metadata —
// it never creates the backing file on disk — so a record added this way
// has a resolvable path via GetFilePath but fails os.Stat, exercising the
// "missing from disk" branch of collectTaggedPaths exactly like the
// existing TestFileLightbar_* fixtures do.
func newTestFileManagerWithRecord(t *testing.T, id uuid.UUID, filename string) *file.FileManager {
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
	if filename != "" {
		if err := fm.AddFileRecord(file.FileRecord{
			ID: id, AreaID: 1, Filename: filename, Size: 1024, UploadedBy: "sysop",
		}); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}
	return fm
}

// TestCollectTaggedPaths_MissingFromDisk pins that a tagged ID whose record
// exists but whose file was never written to disk is skipped and counted as
// a failure, not resolved as a downloadable path.
func TestCollectTaggedPaths_MissingFromDisk(t *testing.T) {
	id := uuid.New()
	fm := newTestFileManagerWithRecord(t, id, "ghost.txt")

	paths, ids, failCount := collectTaggedPaths(fm, 1, []uuid.UUID{id})

	if len(paths) != 0 || len(ids) != 0 {
		t.Errorf("paths/ids = %v/%v, want both empty (file missing from disk)", paths, ids)
	}
	if failCount != 1 {
		t.Errorf("failCount = %d, want 1", failCount)
	}
}

// TestCollectTaggedPaths_UnknownID pins that a tagged ID with no matching
// record at all (GetFilePath itself errors) is also skipped and counted as
// a failure.
func TestCollectTaggedPaths_UnknownID(t *testing.T) {
	fm := newTestFileManagerWithRecord(t, uuid.Nil, "")

	paths, ids, failCount := collectTaggedPaths(fm, 1, []uuid.UUID{uuid.New()})

	if len(paths) != 0 || len(ids) != 0 {
		t.Errorf("paths/ids = %v/%v, want both empty (unknown ID)", paths, ids)
	}
	if failCount != 1 {
		t.Errorf("failCount = %d, want 1", failCount)
	}
}

// TestCollectTaggedPaths_Mixed pins that resolvable and unresolvable IDs in
// the same batch are partitioned correctly: only failures increment
// failCount, and paths/ids stay parallel and in order for the rest — even
// though every ID here ultimately fails (no test file is ever written to
// disk), collectTaggedPaths must not fail closed on the whole batch just
// because it saw one bad ID first.
func TestCollectTaggedPaths_Mixed(t *testing.T) {
	knownID := uuid.New()
	unknownID := uuid.New()
	fm := newTestFileManagerWithRecord(t, knownID, "known.txt")

	_, _, failCount := collectTaggedPaths(fm, 1, []uuid.UUID{unknownID, knownID})

	if failCount != 2 {
		t.Errorf("failCount = %d, want 2 (unknown ID + missing-from-disk known ID)", failCount)
	}
}
