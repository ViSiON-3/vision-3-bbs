package file

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// reloadAreasForTest exercises the public Reload, which performs exactly the
// lock sequence this file exists to prove safe: exclusive muAreas.Lock inside
// loadAreas, then muFiles.Lock inside loadAllFileRecords.
func reloadAreasForTest(fm *FileManager) {
	_ = fm.Reload()
}

// lockTestManager builds a manager with two areas and a set of records whose
// files exist on disk, so GetFilePath/Move/Delete run their full paths.
func lockTestManager(t *testing.T, recordCount int) (*FileManager, []uuid.UUID) {
	t.Helper()
	fm := setupTestFileManager(t, []FileArea{
		{ID: 1, Tag: "ONE", Name: "One", Path: "one"},
		{ID: 2, Tag: "TWO", Name: "Two", Path: "two"},
	})
	for _, sub := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(fm.basePath, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}

	ids := make([]uuid.UUID, recordCount)
	for i := range ids {
		ids[i] = uuid.New()
		name := fmt.Sprintf("file%03d.zip", i)
		if err := os.WriteFile(filepath.Join(fm.basePath, "one", name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := fm.AddFileRecord(FileRecord{ID: ids[i], AreaID: 1, Filename: name, Description: "d"}); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}
	return fm, ids
}

// TestNoDeadlockUnderConcurrentAreaWriteLock is the regression test for the
// muAreas/muFiles ABBA inversion this package's doc comment used to warn
// about. The historical cycle needed three parties:
//
//	reader   holds muFiles, waits for muAreas.RLock — queued behind…
//	reload B …a pending muAreas.Lock, which waits for…
//	reload A …a held muAreas.RLock whose owner waits for muFiles
//
// Two goroutines running the reload sequence plus record readers/mutators
// reproduce exactly that shape. On the pre-fix code this deadlocks within a
// few hundred iterations; with the locks never held together there is no
// cycle to form. The writers bound the test (a fixed iteration count ends
// it); the watchdog turns a hang into a failure instead of a stuck CI job.
func TestNoDeadlockUnderConcurrentAreaWriteLock(t *testing.T) {
	fm, ids := lockTestManager(t, 8)

	const reloads = 400
	scenario := func() {
		var wg sync.WaitGroup
		writersDone := make(chan struct{})

		var writerWg sync.WaitGroup
		for w := 0; w < 2; w++ {
			writerWg.Add(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer writerWg.Done()
				for i := 0; i < reloads; i++ {
					reloadAreasForTest(fm)
				}
			}()
		}
		go func() {
			writerWg.Wait()
			close(writersDone)
		}()

		// Readers and mutators: the operations that historically nested
		// muFiles → muAreas.
		for r := 0; r < 4; r++ {
			wg.Add(1)
			go func(r int) {
				defer wg.Done()
				for {
					select {
					case <-writersDone:
						return
					default:
					}
					for _, id := range ids {
						_, _ = fm.GetFilePath(id)
					}
					_ = fm.ListAreas()
					if r == 0 {
						// One mover, ping-ponging a record between areas.
						_ = fm.MoveFileRecord(ids[0], 2)
						_ = fm.MoveFileRecord(ids[0], 1)
					}
				}
			}(r)
		}
		wg.Wait()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		scenario()
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("deadlock suspected: reload loops and record operations did not finish")
	}
}

// TestDeleteRevalidatesUnderWriteLock covers the ID re-search that replaced
// the stale index: a record deleted between Delete's read-phase and its
// write-phase must yield not-found, not corrupt a neighbor.
func TestDeleteRevalidatesUnderWriteLock(t *testing.T) {
	fm, ids := lockTestManager(t, 2)

	if err := fm.DeleteFileRecord(ids[0], true); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if err := fm.DeleteFileRecord(ids[0], true); err == nil {
		t.Fatal("second delete of the same ID succeeded; want not-found")
	}
	// The surviving record must be untouched.
	if _, err := fm.GetFilePath(ids[1]); err != nil {
		t.Fatalf("surviving record lost: %v", err)
	}
}

// TestConcurrentDeleteAndMoveSameRecord hammers Delete and Move on the same
// ID; under -race this checks the copy-then-revalidate structure, and the
// invariant checked afterwards is that the record ends up in exactly one
// state: present in one area or fully gone.
func TestConcurrentDeleteAndMoveSameRecord(t *testing.T) {
	for round := 0; round < 20; round++ {
		fm, ids := lockTestManager(t, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = fm.DeleteFileRecord(ids[0], true)
		}()
		go func() {
			defer wg.Done()
			_ = fm.MoveFileRecord(ids[0], 2)
		}()
		wg.Wait()

		fm.muFiles.RLock()
		count := 0
		for _, records := range fm.fileRecords {
			for i := range records {
				if records[i].ID == ids[0] {
					count++
				}
			}
		}
		fm.muFiles.RUnlock()
		if count > 1 {
			t.Fatalf("round %d: record exists in %d areas after concurrent delete/move", round, count)
		}
	}
}

// TestDeleteUsesCurrentFilenameAfterRename covers the stale-filename hazard
// Copilot flagged on PR #330: the disk delete must target the record's
// filename as it stands under the write lock, not a copy captured earlier.
// Sequentially: rename the record (and its disk file), then delete — the
// renamed file must be removed and no stray remains.
func TestDeleteUsesCurrentFilenameAfterRename(t *testing.T) {
	fm, ids := lockTestManager(t, 1)
	dir := filepath.Join(fm.basePath, "one")

	if err := os.Rename(filepath.Join(dir, "file000.zip"), filepath.Join(dir, "renamed.zip")); err != nil {
		t.Fatal(err)
	}
	if err := fm.UpdateFileRecord(ids[0], func(r *FileRecord) { r.Filename = "renamed.zip" }); err != nil {
		t.Fatalf("UpdateFileRecord: %v", err)
	}

	if err := fm.DeleteFileRecord(ids[0], true); err != nil {
		t.Fatalf("DeleteFileRecord: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "renamed.zip")); !os.IsNotExist(err) {
		t.Error("renamed.zip still on disk; delete used a stale filename")
	}
}

// TestDeleteAfterRenameByAnotherProcessTargetsCurrentFile covers the
// stale-filename hazard from PR #330's review across processes: another
// process renames the record (and its file) after this FileManager cached it,
// and plants a bystander under the old name. The delete must remove the file
// the list on disk NOW names; acting on the cached name would remove the
// bystander.
func TestDeleteAfterRenameByAnotherProcessTargetsCurrentFile(t *testing.T) {
	fm, ids := lockTestManager(t, 1)
	dir := filepath.Join(fm.basePath, "one")

	if err := os.Rename(filepath.Join(dir, "file000.zip"), filepath.Join(dir, "renamed.zip")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file000.zip"), []byte("bystander"), 0644); err != nil {
		t.Fatal(err)
	}
	err := UpdateAreaMetadata(dir, func(records []FileRecord) ([]FileRecord, error) {
		for i := range records {
			if records[i].ID == ids[0] {
				records[i].Filename = "renamed.zip"
			}
		}
		return records, nil
	})
	if err != nil {
		t.Fatalf("UpdateAreaMetadata: %v", err)
	}

	if err := fm.DeleteFileRecord(ids[0], true); err != nil {
		t.Fatalf("DeleteFileRecord: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "renamed.zip")); !os.IsNotExist(err) {
		t.Error("renamed.zip still on disk: the delete did not target the record's current filename")
	}
	if _, err := os.Stat(filepath.Join(dir, "file000.zip")); err != nil {
		t.Error("bystander file000.zip was deleted: the delete used the stale cached filename")
	}
}
