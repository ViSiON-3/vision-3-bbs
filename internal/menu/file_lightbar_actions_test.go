package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
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

// TestFileLightbarViewsSelectedFile pins "v": a text file is paged out and
// an archive is listed through ZipLab, each returning to the list after.
func TestFileLightbarViewsSelectedFile(t *testing.T) {
	env := lightbarListEnv(t)
	addFileWithContent(t, env, 1, "A.TXT", []byte("text body here\n"))
	addFileWithContent(t, env, 1, "B.ZIP", zipBytes(t, map[string]string{"PACKED.NFO": "x"}))

	r := env.runCmd("LISTFILES", env.caller, "", "v\rq")
	if !r.has("Viewing: A.TXT", "text body here", "End of File") {
		t.Errorf("text view missing:\n%s", r.text())
	}
	r = env.run(runListFiles, env.caller, "", "\x1b[BvQ\rq")
	if !r.has("PACKED.NFO") {
		t.Errorf("archive listing missing:\n%s", r.text())
	}
	if r.next != "" || r.err != nil {
		t.Errorf("quit after view: next=%q err=%v", r.next, r.err)
	}
}

// TestFileLightbarSpaceMarkPersists pins that Space marks the selected file
// and saves the mark, and a second Space unmarks and saves again.
func TestFileLightbarSpaceMarkPersists(t *testing.T) {
	env := lightbarListEnv(t)
	ids := addDownloadRecords(t, env, "ONE.ZIP", "TWO.ZIP")

	env.runCmd("LISTFILES", env.caller, "", "\x1b[B q")
	if saved := env.mustDiskUser(env.caller.ID); len(saved.TaggedFileIDs) != 1 || saved.TaggedFileIDs[0] != ids[1] {
		t.Errorf("saved tags = %v, want [%v]", saved.TaggedFileIDs, ids[1])
	}
	env.runCmd("LISTFILES", env.caller, "", "\x1b[B q")
	if saved := env.mustDiskUser(env.caller.ID); len(saved.TaggedFileIDs) != 0 {
		t.Errorf("saved tags = %v, want none after unmarking", saved.TaggedFileIDs)
	}
}

// TestFileLightbarDownloadFlow pins "d": nothing marked explains how to mark;
// No at the confirm keeps marks; no protocols keeps marks too; confirming a
// batch whose files are missing on disk fails them all and clears the marks.
func TestFileLightbarDownloadFlow(t *testing.T) {
	env := lightbarListEnv(t)
	ids := addDownloadRecords(t, env, "GHOST.ZIP")

	if r := env.runCmd("LISTFILES", env.caller, "", "dq"); !r.has("No files marked for download.") {
		t.Errorf("nothing marked:\n%s", r.text())
	}

	env.caller.TaggedFileIDs = ids
	r := env.runCmd("LISTFILES", env.caller, "", "dNq")
	if !r.has("Download 1 marked file(s)?") || len(env.caller.TaggedFileIDs) != 1 {
		t.Errorf("decline: tags=%v\n%s", env.caller.TaggedFileIDs, r.text())
	}

	if r := env.runCmd("LISTFILES", env.caller, "", "d"); r.next != "LOGOFF" {
		t.Errorf("disconnect at confirm: next = %q, want LOGOFF", r.next)
	}

	r = env.runCmd("LISTFILES", env.caller, "", "dYq")
	if !r.has("Could not find any of the marked files", "Success: 0, Failed: 1.") {
		t.Errorf("missing-file outcome:\n%s", r.text())
	}
	if saved := env.mustDiskUser(env.caller.ID); len(saved.TaggedFileIDs) != 0 {
		t.Errorf("saved tags = %v, want cleared", saved.TaggedFileIDs)
	}
}

// TestFileLightbarDownloadProtocolStep pins the lightbar's protocol step for
// files that are on disk: with no protocols it fails them, and Q at the
// protocol menu cancels; neither runs a transfer or credits a download.
func TestFileLightbarDownloadProtocolStep(t *testing.T) {
	env := lightbarListEnv(t)
	id := addFileWithContent(t, env, 1, "REAL.ZIP", []byte("zip-ish"))

	env.caller.TaggedFileIDs = []uuid.UUID{id}
	env.e.SetProtocols(nil)
	r := env.runCmd("LISTFILES", env.caller, "", "dYq")
	if !r.has("No transfer protocols configured", "Success: 0, Failed: 1.") {
		t.Errorf("no protocols:\n%s", r.text())
	}

	env.caller.TaggedFileIDs = []uuid.UUID{id}
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", SendCmd: "/nonexistent/sz", Default: true}})
	r = env.runCmd("LISTFILES", env.caller, "", "dYQ\rq")
	if !r.has("Transfer Protocols:", "Download cancelled.", "Success: 0, Failed: 0.") {
		t.Errorf("protocol cancel:\n%s", r.text())
	}
	if saved := env.mustDiskUser(env.caller.ID); saved.NumDownloads != 0 {
		t.Errorf("downloads = %d, want 0", saved.NumDownloads)
	}

	env.caller.TaggedFileIDs = []uuid.UUID{id}
	if r := env.runCmd("LISTFILES", env.caller, "", "dY"); r.next != "LOGOFF" {
		t.Errorf("disconnect at protocol menu: next = %q, want LOGOFF", r.next)
	}
}

// TestFileLightbarUploadCancels pins "u": the sysop reaches the upload start
// prompt and Q there returns to the list with no transfer and no new file.
func TestFileLightbarUploadCancels(t *testing.T) {
	env := lightbarListEnv(t)
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", RecvCmd: "/nonexistent/rz", Default: true}})

	r := env.runCmd("LISTFILES", env.sysop, "", "u\rQ\rq")
	if !r.has("Uploading to: General Files", "Press ENTER to begin") {
		t.Errorf("upload prompt missing:\n%s", r.text())
	}
	if n, _ := env.e.FileMgr.GetFileCountForArea(1); n != 0 {
		t.Errorf("area has %d files after cancel", n)
	}
}

// TestFileLightbarZipLabExtractUsesTransferFlow pins extracting an archive
// member from the lightbar's ZipLab view: it goes through the download ACS
// and protocol menu like any download, and once the transfer program exits
// the viewer and the list keep reading keys instead of logging the caller
// off (the transfer used to run beside the live InputHandler).
func TestFileLightbarZipLabExtractUsesTransferFlow(t *testing.T) {
	env := lightbarListEnv(t)
	addFileWithContent(t, env, 1, "B.ZIP", zipBytes(t, map[string]string{"FILE_ID.DIZ": "x"}))

	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", SendCmd: "/nonexistent/sz", Default: true}})
	// The sysop passes the area's download ACS.
	r := env.runCmd("LISTFILES", env.sysop, "", "v1\rQ\rQ\rq")
	if !r.has("FILE_ID.DIZ", "Transfer Protocols:", "Download cancelled.") {
		t.Errorf("protocol cancel:\n%s", r.text())
	}
	if r.next != "" || r.err != nil {
		t.Errorf("after cancel: next=%q err=%v", r.next, r.err)
	}

	// The transfer program's stdin pump swallows whatever scripted input
	// follows, so this run ends on EOF; it pins that the transfer ran and
	// the viewer was back at its prompt afterwards.
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", SendCmd: "/bin/true", Default: true}})
	r = env.runCmd("LISTFILES", env.sysop, "", "v1\rZ\r")
	_, after, ok := strings.Cut(r.text(), "[1/1] FILE_ID.DIZ: OK")
	if !ok || !strings.Contains(after, "ZipLab [#/Q]:") {
		t.Errorf("transfer outcome:\n%s", r.text())
	}

	// The caller does not, so nothing is offered for transfer.
	r = env.runCmd("LISTFILES", env.caller, "", "v1\rQ\rq")
	if !r.has(stripPipes(env.e.Strings().YouCantDownloadHere)) || r.has("Transfer Protocols:") {
		t.Errorf("download ACS not enforced:\n%s", r.text())
	}
}
