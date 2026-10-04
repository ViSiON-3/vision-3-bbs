package menu

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
)

// addDownloadRecords adds metadata-only records named names to General Files
// and returns their IDs in order.
func addDownloadRecords(t *testing.T, env *menuEnv, names ...string) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, len(names))
	for i, n := range names {
		ids[i] = uuid.New()
		if err := env.e.FileMgr.AddFileRecord(file.FileRecord{ID: ids[i], AreaID: 1, Filename: n, UploadedBy: "Sysop"}); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}
	return ids
}

func addPhysicalDownloadRecord(t *testing.T, env *menuEnv, name string) uuid.UUID {
	t.Helper()
	ids := addDownloadRecords(t, env, name)
	areaPath, err := env.e.FileMgr.GetAreaUploadPath(1)
	if err != nil {
		t.Fatalf("GetAreaUploadPath: %v", err)
	}
	if err := os.MkdirAll(areaPath, 0o755); err != nil {
		t.Fatalf("create file area: %v", err)
	}
	if err := os.WriteFile(filepath.Join(areaPath, name), []byte("test download contents"), 0o644); err != nil {
		t.Fatalf("write download fixture: %v", err)
	}
	return ids[0]
}

func setTestDownloadProtocol(t *testing.T, env *menuEnv, exitStatus int) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "send.sh")
	if err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", exitStatus)), 0o755); err != nil {
		t.Fatalf("write transfer command: %v", err)
	}
	env.e.SetProtocols([]transfer.ProtocolConfig{{
		Key: "T", Name: "Testmodem", SendCmd: "/bin/sh", SendArgs: []string{script}, Default: true,
	}})
}

// TestDownloadFileTagsNamedFile pins DOWNLOADFILE's add step: a filename
// typed in any case is matched in the current area, tagged, and saved; X
// then leaves the batch menu without transferring.
func TestDownloadFileTagsNamedFile(t *testing.T) {
	env := newMenuEnv(t)
	ids := addDownloadRecords(t, env, "GAME.ZIP")
	env.sysop.CurrentFileAreaID = 1

	r := env.runCmd("DOWNLOADFILE", env.sysop, "", "game.zip\rX\r")
	if r.err != nil || r.user == nil {
		t.Fatalf("result = (%v, %v)", r.user, r.err)
	}
	saved := env.mustDiskUser(env.sysop.ID)
	if len(saved.TaggedFileIDs) != 1 || saved.TaggedFileIDs[0] != ids[0] {
		t.Errorf("saved tags = %v, want [%v]", saved.TaggedFileIDs, ids[0])
	}
	if !r.has("GAME.ZIP") {
		t.Errorf("missing added-to-batch notice:\n%s", r.text())
	}
}

// TestDownloadFileBlankCancels pins that an empty filename at DOWNLOADFILE's
// first prompt returns without tagging anything or entering the batch menu.
func TestDownloadFileBlankCancels(t *testing.T) {
	env := newMenuEnv(t)
	addDownloadRecords(t, env, "GAME.ZIP")
	env.sysop.CurrentFileAreaID = 1

	r := env.runCmd("DOWNLOADFILE", env.sysop, "", "\r")
	if r.err != nil || len(env.sysop.TaggedFileIDs) != 0 {
		t.Errorf("err=%v tags=%v, want nothing tagged", r.err, env.sysop.TaggedFileIDs)
	}

	r = env.runCmd("DOWNLOADFILE", env.sysop, "", "")
	if r.next != "LOGOFF" {
		t.Errorf("disconnect at the prompt: next = %q, want LOGOFF", r.next)
	}
}

// TestDownloadFileAddMoreFromBatchMenu pins the batch menu's A command: a
// second file is added to the same batch; unknown commands just reprompt.
func TestDownloadFileAddMoreFromBatchMenu(t *testing.T) {
	env := newMenuEnv(t)
	ids := addDownloadRecords(t, env, "ONE.ZIP", "TWO.ZIP")
	env.sysop.CurrentFileAreaID = 1

	env.runCmd("DOWNLOADFILE", env.sysop, "", "ONE.ZIP\rA\rtwo.zip\r?\rA\r\rX\r")
	saved := env.mustDiskUser(env.sysop.ID)
	if len(saved.TaggedFileIDs) != 2 || saved.TaggedFileIDs[0] != ids[0] || saved.TaggedFileIDs[1] != ids[1] {
		t.Errorf("saved tags = %v, want %v", saved.TaggedFileIDs, ids)
	}
}

// TestBatchDownloadCancelAtProtocolKeepsBatch pins BATCHDOWNLOAD up to the
// protocol menu without running a transfer: an unknown protocol key is
// refused, Q cancels back to the batch menu, and the batch is kept.
func TestBatchDownloadCancelAtProtocolKeepsBatch(t *testing.T) {
	env := newMenuEnv(t)
	ids := addDownloadRecords(t, env, "GAME.ZIP")
	env.sysop.TaggedFileIDs = ids
	// A protocol that is never chosen: the test only reaches the menu.
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", SendCmd: "/nonexistent/sz", Default: true}})

	r := env.runCmd("BATCHDOWNLOAD", env.sysop, "", "C\rQQ\rQ\rX\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Transfer Protocols:", "Zmodem", `Unknown protocol "QQ"`) {
		t.Errorf("protocol menu not shown as expected:\n%s", r.text())
	}
	if len(env.sysop.TaggedFileIDs) != 1 || env.sysop.NumDownloads != 0 {
		t.Errorf("cancel changed state: tags=%v downloads=%d", env.sysop.TaggedFileIDs, env.sysop.NumDownloads)
	}
}

// TestBatchDownloadSuccessfulTransferClearsQueueAndCountsDownload exercises
// the full BATCHDOWNLOAD success path with a local transfer command. It checks
// the caller-visible completion message and persisted batch, user, and file
// download counters.
func TestBatchDownloadSuccessfulTransferClearsQueueAndCountsDownload(t *testing.T) {
	env := newMenuEnv(t)
	id := addPhysicalDownloadRecord(t, env, "GAME.ZIP")
	env.sysop.TaggedFileIDs = []uuid.UUID{id}
	setTestDownloadProtocol(t, env, 0)

	r := env.runCmd("BATCHDOWNLOAD", env.sysop, "", "C\r\r")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (user %v, err %v), want caller and no error", r.user, r.err)
	}
	if !r.has("Initiating Testmodem transfer", "GAME.ZIP: OK", "Download complete: 1 succeeded, 0 failed.") {
		t.Errorf("successful transfer messages missing:\n%s", r.text())
	}
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileIDs) != 0 || saved.NumDownloads != 1 {
		t.Errorf("saved user tags=%v downloads=%d, want empty batch and 1 download", saved.TaggedFileIDs, saved.NumDownloads)
	}
	record, err := env.e.FileMgr.GetFileRecordByID(id)
	if err != nil {
		t.Fatalf("GetFileRecordByID: %v", err)
	}
	if record.DownloadCount != 1 {
		t.Errorf("file download count = %d, want 1", record.DownloadCount)
	}
}

func TestBatchDownloadFailedTransferShowsFailureWithoutCountingDownload(t *testing.T) {
	env := newMenuEnv(t)
	id := addPhysicalDownloadRecord(t, env, "GAME.ZIP")
	env.sysop.TaggedFileIDs = []uuid.UUID{id}
	setTestDownloadProtocol(t, env, 7)

	r := env.runCmd("BATCHDOWNLOAD", env.sysop, "", "C\r\r")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (user %v, err %v), want caller and no error", r.user, r.err)
	}
	if !r.has("GAME.ZIP: FAILED", "Download complete: 0 succeeded, 1 failed.") {
		t.Errorf("failed transfer messages missing:\n%s", r.text())
	}
	if saved := env.mustDiskUser(env.sysop.ID); saved.NumDownloads != 0 {
		t.Errorf("saved download count = %d, want 0 after failed transfer", saved.NumDownloads)
	}
	record, err := env.e.FileMgr.GetFileRecordByID(id)
	if err != nil {
		t.Fatalf("GetFileRecordByID: %v", err)
	}
	if record.DownloadCount != 0 {
		t.Errorf("file download count = %d, want 0 after failed transfer", record.DownloadCount)
	}
}

// TestBatchDownloadNoProtocolsKeepsBatch pins that with no protocol usable
// on the connection the download is abandoned, the caller is told why, and
// the batch is left intact.
func TestBatchDownloadNoProtocolsKeepsBatch(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.TaggedFileIDs = addDownloadRecords(t, env, "GAME.ZIP")
	env.e.SetProtocols(nil)

	r := env.runCmd("BATCHDOWNLOAD", env.sysop, "", "\r")
	if r.err != nil || len(env.sysop.TaggedFileIDs) != 1 {
		t.Errorf("err=%v tags=%v, want the batch kept", r.err, env.sysop.TaggedFileIDs)
	}
	if !r.has("No transfer protocols configured") {
		t.Errorf("no-protocols message not shown:\n%s", r.text())
	}
}

// TestBatchDownloadDropsUnresolvableBatch pins that a batch whose files have
// all vanished is cleared and saved rather than offered for transfer.
func TestBatchDownloadDropsUnresolvableBatch(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.TaggedFileIDs = []uuid.UUID{uuid.New()}

	r := env.runCmd("BATCHDOWNLOAD", env.sysop, "", "C\r")
	if r.has("Transfer Protocols:") {
		t.Errorf("protocol menu offered for a batch with no files:\n%s", r.text())
	}
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileIDs) != 0 {
		t.Errorf("saved tags = %v, want cleared", saved.TaggedFileIDs)
	}
}

// TestClearBatchEmptiesQueue pins CLEAR_BATCH: the tagged list is emptied and
// the empty list is saved.
func TestClearBatchEmptiesQueue(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.TaggedFileIDs = addDownloadRecords(t, env, "A.ZIP", "B.ZIP")
	if err := env.um.UpdateUser(env.sysop); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("CLEAR_BATCH", env.sysop, "", "")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (user %v, err %v), want the caller and no error", r.user, r.err)
	}
	if !r.has("Cleared 2 file(s) from the batch queue.") {
		t.Errorf("success message missing:\n%s", r.text())
	}
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileIDs) != 0 {
		t.Errorf("saved tags = %v, want cleared", saved.TaggedFileIDs)
	}
}

func TestClearBatchEmptyQueueReportsEmpty(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("CLEAR_BATCH", env.sysop, "", "")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (user %v, err %v), want the caller and no error", r.user, r.err)
	}
	if !r.has("Your batch queue is empty.") {
		t.Errorf("empty-queue message missing:\n%s", r.text())
	}
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileIDs) != 0 {
		t.Errorf("empty batch changed saved tags: %v", saved.TaggedFileIDs)
	}
}
