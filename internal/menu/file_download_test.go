package menu

import (
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

// TestBatchDownloadNoProtocolsKeepsBatch pins that with no protocol usable
// on the connection the download is abandoned and the batch left intact.
func TestBatchDownloadNoProtocolsKeepsBatch(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.TaggedFileIDs = addDownloadRecords(t, env, "GAME.ZIP")
	env.e.SetProtocols(nil)

	r := env.runCmd("BATCHDOWNLOAD", env.sysop, "", "\r")
	if r.err != nil || len(env.sysop.TaggedFileIDs) != 1 {
		t.Errorf("err=%v tags=%v, want the batch kept", r.err, env.sysop.TaggedFileIDs)
	}
}

// TestBatchDownloadDropsUnresolvableBatch pins that a batch whose files have
// all vanished is cleared and saved rather than offered for transfer.
func TestBatchDownloadDropsUnresolvableBatch(t *testing.T) {
	t.Parallel() // the notice holds for a fixed two seconds
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
	t.Parallel() // the notice holds for a fixed second
	env := newMenuEnv(t)
	env.sysop.TaggedFileIDs = addDownloadRecords(t, env, "A.ZIP", "B.ZIP")
	if err := env.um.UpdateUser(env.sysop); err != nil {
		t.Fatal(err)
	}

	env.runCmd("CLEAR_BATCH", env.sysop, "", "")
	if saved := env.mustDiskUser(env.sysop.ID); len(saved.TaggedFileIDs) != 0 {
		t.Errorf("saved tags = %v, want cleared", saved.TaggedFileIDs)
	}
}
