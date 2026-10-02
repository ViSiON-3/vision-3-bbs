package file

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// addExternally appends a record to an area's list the way v3mail or helper
// does, from outside this FileManager.
func addExternally(t *testing.T, fm *FileManager, areaPath string, rec FileRecord) {
	t.Helper()
	err := UpdateAreaMetadata(filepath.Join(fm.basePath, areaPath), func(records []FileRecord) ([]FileRecord, error) {
		return append(records, rec), nil
	})
	if err != nil {
		t.Fatalf("UpdateAreaMetadata: %v", err)
	}
}

// A file another process adds is listed without a restart.
func TestRecordAddedByAnotherProcessIsListed(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})

	id := uuid.New()
	addExternally(t, fm, "utils", FileRecord{ID: id, AreaID: 1, Filename: "TIC.ZIP"})

	files := fm.GetFilesForArea(1)
	if len(files) != 1 || files[0].ID != id {
		t.Fatalf("GetFilesForArea = %+v, want the externally added record", files)
	}
	if n, _ := fm.GetFileCountForArea(1); n != 1 {
		t.Errorf("GetFileCountForArea = %d, want 1", n)
	}
	if _, err := fm.GetFileRecordByID(id); err != nil {
		t.Errorf("GetFileRecordByID: %v", err)
	}
}

// The BBS saving a change must not erase a record another process added
// after the BBS cached the list — the bug this read-modify-write exists for.
func TestLocalChangeKeepsRecordAddedByAnotherProcess(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})

	local := uuid.New()
	if err := fm.AddFileRecord(FileRecord{ID: local, AreaID: 1, Filename: "LOCAL.ZIP"}); err != nil {
		t.Fatal(err)
	}
	external := uuid.New()
	addExternally(t, fm, "utils", FileRecord{ID: external, AreaID: 1, Filename: "TIC.ZIP"})

	if err := fm.IncrementDownloadCount(local); err != nil {
		t.Fatalf("IncrementDownloadCount: %v", err)
	}

	onDisk, err := ReadAreaMetadata(filepath.Join(fm.basePath, "utils"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[uuid.UUID]FileRecord{}
	for _, r := range onDisk {
		found[r.ID] = r
	}
	if _, ok := found[external]; !ok {
		t.Error("the externally added record was erased by the local save")
	}
	if found[local].DownloadCount != 1 {
		t.Errorf("download count = %d, want 1", found[local].DownloadCount)
	}
}

// A record another process removed stays removed: an update for it fails
// rather than writing the cached copy back.
func TestUpdateOfRecordRemovedByAnotherProcessFails(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})

	id := uuid.New()
	if err := fm.AddFileRecord(FileRecord{ID: id, AreaID: 1, Filename: "GONE.ZIP"}); err != nil {
		t.Fatal(err)
	}
	err := UpdateAreaMetadata(filepath.Join(fm.basePath, "utils"), func([]FileRecord) ([]FileRecord, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := fm.UpdateFileDescription(id, "new"); err == nil {
		t.Fatal("updating a record another process removed succeeded")
	}
	if n := len(fm.GetFilesForArea(1)); n != 0 {
		t.Errorf("%d records listed, want 0", n)
	}
}

func TestIsMetadataFile(t *testing.T) {
	for _, name := range []string{"metadata.json", "metadata.json.lock", "metadata.json.tmp-12345"} {
		if !IsMetadataFile(name) {
			t.Errorf("IsMetadataFile(%q) = false", name)
		}
	}
	for _, name := range []string{"METADATA.ZIP", "file.zip", "metadata.txt"} {
		if IsMetadataFile(name) {
			t.Errorf("IsMetadataFile(%q) = true", name)
		}
	}
}
