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

// TestDeleteAfterConcurrentRenameTargetsCurrentFile deterministically forces
// the interleave behind the stale-filename hazard from PR #330's review: a
// rename lands between DeleteFileRecord's read phase and its write phase.
//
// The interleave is forced through the locks themselves, in two stages:
//
//  1. The test holds muFiles exclusively while starting the delete, parking
//     it at its phase-1 RLock; releasing and immediately re-acquiring
//     muFiles then acts as a barrier — the write lock is only granted after
//     phase 1's RUnlock, proving the read phase completed.
//  2. The test also holds muAreas exclusively throughout, so the delete
//     parks again at its area lookup while the rename and a bystander file
//     under the old name land.
//
// On release, a correct delete removes the file the record NOW names; the
// pre-fix code removed the bystander. (A sleep-based version of this test
// could not prove the delete had passed phase 1 before the rename — a late
// goroutine spawn would let even the stale implementation pass.)
func TestDeleteAfterConcurrentRenameTargetsCurrentFile(t *testing.T) {
	fm, ids := lockTestManager(t, 1)
	dir := filepath.Join(fm.basePath, "one")

	fm.muAreas.Lock() // parks the delete between phase 2 and its write phase
	fm.muFiles.Lock() // parks the delete at the start of phase 1

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- fm.DeleteFileRecord(ids[0], true)
	}()

	// Wait for the goroutine to be running, plus a beat for it to reach the
	// phase-1 RLock; the barrier below is what actually proves phase 1 ran.
	<-started
	time.Sleep(10 * time.Millisecond)

	// Barrier: this Lock is granted only once phase 1's RLock has been
	// released, so everything after this line happens-after the read phase.
	fm.muFiles.Unlock()
	fm.muFiles.Lock()

	// Rename the record (directly — UpdateFileRecord's save path takes
	// muAreas.RLock, which the test holds exclusively and would deadlock on;
	// an instructive demonstration of why this package never nests these
	// locks), rename the disk file, and plant a bystander under the old name.
	for i := range fm.fileRecords[1] {
		if fm.fileRecords[1][i].ID == ids[0] {
			fm.fileRecords[1][i].Filename = "renamed.zip"
		}
	}
	if err := os.Rename(filepath.Join(dir, "file000.zip"), filepath.Join(dir, "renamed.zip")); err != nil {
		fm.muFiles.Unlock()
		fm.muAreas.Unlock()
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file000.zip"), []byte("bystander"), 0644); err != nil {
		fm.muFiles.Unlock()
		fm.muAreas.Unlock()
		t.Fatal(err)
	}
	fm.muFiles.Unlock()

	fm.muAreas.Unlock() // release the delete into its write phase
	if err := <-done; err != nil {
		t.Fatalf("DeleteFileRecord: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "renamed.zip")); !os.IsNotExist(err) {
		t.Error("renamed.zip still on disk: the delete did not target the record's current filename")
	}
	if _, err := os.Stat(filepath.Join(dir, "file000.zip")); err != nil {
		t.Error("bystander file000.zip was deleted: the delete used the stale pre-rename filename")
	}
}
