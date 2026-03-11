package file

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSearchFiles(t *testing.T) {
	areas := []FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
		{ID: 2, Tag: "GAMES", Name: "Games", Path: "games"},
	}
	fm := setupTestFileManager(t, areas)

	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "COOLUTIL.ZIP", Description: "A cool utility"})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 2, Filename: "TETRIS.ZIP", Description: "Cool tetris game"})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "BORING.ZIP", Description: "Nothing special"})

	// Search matches filename and description
	results := fm.SearchFiles("cool")
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'cool', got %d", len(results))
	}

	// Search matches description only
	results = fm.SearchFiles("tetris")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'tetris', got %d", len(results))
	}

	// No match
	results = fm.SearchFiles("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results for 'nonexistent', got %d", len(results))
	}

	// Case insensitive
	results = fm.SearchFiles("BORING")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'BORING', got %d", len(results))
	}
}

func TestGetFilesNewerThan(t *testing.T) {
	areas := []FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
	}
	fm := setupTestFileManager(t, areas)

	now := time.Now()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "OLD.ZIP", UploadedAt: old})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "NEW.ZIP", UploadedAt: recent})

	cutoff := now.Add(-24 * time.Hour)
	results := fm.GetFilesNewerThan(1, cutoff)
	if len(results) != 1 {
		t.Fatalf("expected 1 file newer than cutoff, got %d", len(results))
	}
	if results[0].Filename != "NEW.ZIP" {
		t.Errorf("expected NEW.ZIP, got %s", results[0].Filename)
	}

	// All files newer than epoch
	results = fm.GetFilesNewerThan(1, time.Time{})
	if len(results) != 2 {
		t.Errorf("expected 2 files newer than epoch, got %d", len(results))
	}

	// No files newer than now
	results = fm.GetFilesNewerThan(1, now)
	if len(results) != 0 {
		t.Errorf("expected 0 files newer than now, got %d", len(results))
	}

	// Non-existent area
	results = fm.GetFilesNewerThan(999, cutoff)
	if len(results) != 0 {
		t.Errorf("expected 0 files for non-existent area, got %d", len(results))
	}
}

func TestSearchFiles_ResultsHaveCorrectAreaID(t *testing.T) {
	areas := []FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
		{ID: 2, Tag: "GAMES", Name: "Games", Path: "games"},
	}
	fm := setupTestFileManager(t, areas)

	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "TOOL.ZIP", Description: "A tool"})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 2, Filename: "TOOL2.ZIP", Description: "Another tool"})

	results := fm.SearchFiles("tool")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	areaIDs := map[int]bool{}
	for _, r := range results {
		areaIDs[r.AreaID] = true
	}
	if !areaIDs[1] || !areaIDs[2] {
		t.Errorf("expected results from areas 1 and 2, got %v", areaIDs)
	}
}

func TestSearchFiles_EmptyQuery(t *testing.T) {
	areas := []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}}
	fm := setupTestFileManager(t, areas)
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "FILE.ZIP"})

	results := fm.SearchFiles("")
	if len(results) != 1 {
		t.Errorf("empty query should match all files, got %d", len(results))
	}
}

func TestGetUnreviewedFiles_MarkReviewed(t *testing.T) {
	areas := []FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"}}
	fm := setupTestFileManager(t, areas)

	id := uuid.New()
	fm.AddFileRecord(FileRecord{ID: id, AreaID: 1, Filename: "NEW.ZIP"})

	results := fm.GetUnreviewedFiles(1)
	if len(results) != 1 {
		t.Fatalf("expected 1 unreviewed, got %d", len(results))
	}

	fm.UpdateFileRecord(id, func(r *FileRecord) { r.Reviewed = true })

	results = fm.GetUnreviewedFiles(1)
	if len(results) != 0 {
		t.Errorf("expected 0 unreviewed after marking, got %d", len(results))
	}
}

func TestGetUnreviewedFiles(t *testing.T) {
	areas := []FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
	}
	fm := setupTestFileManager(t, areas)

	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "REVIEWED.ZIP", Reviewed: true})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "UNREVIEWED.ZIP", Reviewed: false})
	fm.AddFileRecord(FileRecord{ID: uuid.New(), AreaID: 1, Filename: "ALSO_UNREVIEWED.ZIP"})

	results := fm.GetUnreviewedFiles(1)
	if len(results) != 2 {
		t.Fatalf("expected 2 unreviewed files, got %d", len(results))
	}

	// Non-existent area
	results = fm.GetUnreviewedFiles(999)
	if len(results) != 0 {
		t.Errorf("expected 0 for non-existent area, got %d", len(results))
	}
}
