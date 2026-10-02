package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateFilenameRejectsTraversal(t *testing.T) {
	bad := []string{"", ".", "..", "../evil", "sub/dir.zip", `sub\dir.zip`, "/etc/passwd", "a\x00b"}
	for _, name := range bad {
		if _, err := validateFilename(name); err == nil {
			t.Errorf("validateFilename(%q) was accepted", name)
		}
	}
}

// filepath.Base leaves ".." intact, so a record holding it produces
// filepath.Join(base, areaPath, "..") -- the area's parent directory.
func TestValidateFilenameRejectsDotDotSpecifically(t *testing.T) {
	if _, err := validateFilename(".."); err == nil {
		t.Fatal(`validateFilename("..") was accepted; it resolves to the area's parent directory`)
	}
}

// The old check rejected any name containing "..", which also refused ordinary
// files. Base() already guarantees there are no separators, so an embedded ".."
// cannot traverse anywhere.
func TestValidateFilenameAcceptsOrdinaryNames(t *testing.T) {
	good := []string{"README.TXT", "patch..v2.zip", "file.name.with.dots.lha", "..hidden-ish.txt", "a.zip"}
	for _, name := range good {
		got, err := validateFilename(name)
		if err != nil {
			t.Errorf("validateFilename(%q) rejected a legitimate name: %v", name, err)
			continue
		}
		if got != name {
			t.Errorf("validateFilename(%q) = %q, want it returned unchanged", name, got)
		}
	}
}

func TestAddFileRecordRejectsPathInFilename(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})

	err := fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "../../etc/passwd"})
	if err == nil {
		t.Fatal("AddFileRecord accepted a filename containing a path")
	}
	if len(fm.GetFilesForArea(1)) != 0 {
		t.Error("the rejected record was stored anyway")
	}
}

// A record whose filename escapes the area must not be able to delete a file
// outside it. Metadata-only deletion stays available so a corrupt record can
// still be removed through the UI.
func TestDeleteFileRecordRefusesDiskDeleteForUnsafeFilename(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})

	id := uuid.New()
	seedRecords(t, fm, 1, FileRecord{ID: id, AreaID: 1, Filename: ".."})

	err := fm.DeleteFileRecord(id, true)
	if err == nil {
		t.Fatal("DeleteFileRecord agreed to delete from disk using an unsafe filename")
	}
	// Without the name check this still errors, but only because os.Remove
	// happens to fail on the directory it resolved to -- which it would not do
	// if that directory were empty. Assert the refusal came from validation.
	if !strings.Contains(err.Error(), "invalid filename") {
		t.Errorf("delete failed for the wrong reason: %v", err)
	}
	if len(fm.GetFilesForArea(1)) != 1 {
		t.Error("the record was removed even though the delete failed")
	}
	if err := fm.DeleteFileRecord(id, false); err != nil {
		t.Errorf("metadata-only deletion of a corrupt record should still work: %v", err)
	}
}

func TestMoveFileRecordRejectsUnsafeFilename(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
		{ID: 2, Tag: "TEXTS", Name: "Texts", Path: "texts"},
	})

	id := uuid.New()
	seedRecords(t, fm, 1, FileRecord{ID: id, AreaID: 1, Filename: ".."})

	err := fm.MoveFileRecord(id, 2)
	if err == nil {
		t.Fatal("MoveFileRecord accepted a record whose filename escapes its area")
	}
	// As above: the rename would fail anyway here, so check it was refused
	// before any filesystem call rather than by one.
	if !strings.Contains(err.Error(), "invalid filename") {
		t.Errorf("move failed for the wrong reason: %v", err)
	}
}

// GetFilePath must not reject legitimate names that merely contain "..".
func TestGetFilePathAllowsDotsInsideFilename(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})

	id := uuid.New()
	seedRecords(t, fm, 1, FileRecord{ID: id, AreaID: 1, Filename: "patch..v2.zip"})

	got, err := fm.GetFilePath(id)
	if err != nil {
		t.Fatalf("GetFilePath rejected a legitimate filename: %v", err)
	}
	if filepath.Base(got) != "patch..v2.zip" {
		t.Errorf("GetFilePath returned %q, want it to end in patch..v2.zip", got)
	}
}

func TestGetFilePathRejectsTraversalRecord(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})

	id := uuid.New()
	seedRecords(t, fm, 1, FileRecord{ID: id, AreaID: 1, Filename: ".."})

	if _, err := fm.GetFilePath(id); err == nil {
		t.Fatal(`GetFilePath accepted a record with filename ".."`)
	}
}

// A non-positive page would make startIndex negative and panic the slice; the
// guard that prevents it sits at the top of the function, far from the
// arithmetic it protects. This pins it there.
func TestGetFilesForAreaPaginatedHandlesNonPositiveArgs(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}})
	for i := 0; i < 3; i++ {
		seedRecords(t, fm, 1, FileRecord{ID: uuid.New(), AreaID: 1, Filename: "a.zip"})
	}

	cases := []struct{ page, pageSize int }{{0, 10}, {-1, 10}, {1, 0}, {1, -5}, {-100, -100}}
	for _, c := range cases {
		got, err := fm.GetFilesForAreaPaginated(1, c.page, c.pageSize)
		if err == nil && len(got) != 0 {
			t.Errorf("page=%d pageSize=%d returned %d records with no error", c.page, c.pageSize, len(got))
		}
	}
}

// End to end: when the target-area save fails, the moved record must be gone
// from the target and back in the source.
func TestMoveFileRecordRollsBackOnTargetSaveFailure(t *testing.T) {
	fm := setupTestFileManager(t, []FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
		{ID: 2, Tag: "TEXTS", Name: "Texts", Path: "texts"},
	})

	movedID := uuid.New()
	seedRecords(t, fm, 1, FileRecord{ID: movedID, AreaID: 1, Filename: "moved.zip"})

	srcDir, err := fm.GetAreaUploadPath(1)
	if err != nil {
		t.Fatalf("GetAreaUploadPath(1): %v", err)
	}
	dstDir, err := fm.GetAreaUploadPath(2)
	if err != nil {
		t.Fatalf("GetAreaUploadPath(2): %v", err)
	}
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "moved.zip"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	// A directory where the metadata file belongs makes the target-area save fail.
	if err := os.MkdirAll(filepath.Join(dstDir, "metadata.json"), 0o755); err != nil {
		t.Fatalf("mkdir metadata.json: %v", err)
	}

	if err := fm.MoveFileRecord(movedID, 2); err == nil {
		t.Fatal("expected the move to fail on the target-area save")
	}

	for _, r := range fm.GetFilesForArea(2) {
		if r.ID == movedID {
			t.Error("the rolled-back record is still in the target area")
		}
	}
	back := false
	for _, r := range fm.GetFilesForArea(1) {
		if r.ID == movedID {
			back = true
		}
	}
	if !back {
		t.Error("the record was not restored to the source area")
	}
	if _, err := os.Stat(filepath.Join(srcDir, "moved.zip")); err != nil {
		t.Errorf("the file was not renamed back into the source area: %v", err)
	}
}

// seedRecords stores records in an area's metadata.json, the way another
// process would, bypassing the validation AddFileRecord applies. Records
// injected into the cache alone are not enough: mutators work on the list on
// disk.
func seedRecords(t *testing.T, fm *FileManager, areaID int, records ...FileRecord) {
	t.Helper()
	err := fm.mutateAreas([]int{areaID}, func(lists map[int][]FileRecord) (func(), error) {
		lists[areaID] = append(lists[areaID], records...)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("seed records: %v", err)
	}
}
