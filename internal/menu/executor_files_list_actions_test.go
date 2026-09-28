package menu

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestToggleFileTag(t *testing.T) {
	id := uuid.New()
	u := &user.User{}
	st := &fileListState{
		currentUser:  u,
		filesPerPage: 10,
		currentPage:  1,
		filesOnPage:  []file.FileRecord{{ID: id, Filename: "TEST.ZIP"}},
	}
	st.toggleFileTag("1")
	if len(u.TaggedFileIDs) != 1 || u.TaggedFileIDs[0] != id {
		t.Fatalf("tag: got %v, want [%s]", u.TaggedFileIDs, id)
	}
	st.toggleFileTag("1")
	if len(u.TaggedFileIDs) != 0 {
		t.Fatalf("untag: got %v, want empty", u.TaggedFileIDs)
	}
}

// classicListEnv returns an env whose caller lists General Files in the
// classic (line-prompt) file lister.
func classicListEnv(t *testing.T) *menuEnv {
	t.Helper()
	env := newMenuEnv(t)
	for _, u := range []*user.User{env.caller, env.sysop} {
		u.FileListingMode = "classic"
		u.CurrentFileAreaID = 1
		u.CurrentFileAreaTag = "GENERAL"
	}
	return env
}

// TestListFilesClassicPagesThroughArea pins the classic lister's paging: the
// first page shows the first files only, N moves to the next page, N on the
// last page says so, P returns and P on the first page says so.
func TestListFilesClassicPagesThroughArea(t *testing.T) {
	env := classicListEnv(t)
	names := make([]string, 40)
	for i := range names {
		names[i] = fmt.Sprintf("F%02d.ZIP", i)
	}
	addDownloadRecords(t, env, names...)

	r := env.runCmd("LISTFILES", env.caller, "", "Q\r")
	if !r.has("F00.ZIP") || r.has("F39.ZIP") {
		t.Fatalf("first page should hold F00 but not F39:\n%s", r.text())
	}

	r = env.runCmd("LISTFILES", env.caller, "", "N\rN\rN\rN\rP\rP\rP\rP\rQ\r")
	if !r.has("F39.ZIP", "Already on last page.", "Already on first page.") {
		t.Errorf("paging messages or last page missing:\n%s", r.text())
	}
	if r.next != "" || r.err != nil {
		t.Errorf("Q: next=%q err=%v", r.next, r.err)
	}

	if r := env.runCmd("LISTFILES", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q, want LOGOFF", r.next)
	}
}

// TestListFilesExtendedShowsAllColumns pins that LISTFILES_EXTENDED ignores
// the user's column choices: a user showing only names still sees sizes.
func TestListFilesExtendedShowsAllColumns(t *testing.T) {
	env := classicListEnv(t)
	addFileWithContent(t, env, 1, "SIZED.ZIP", make([]byte, 2048))
	env.caller.FileListColumns.Name = true

	plain := env.runCmd("LISTFILES", env.caller, "", "Q\r")
	ext := env.runCmd("LISTFILES_EXTENDED", env.caller, "", "Q\r")
	if !plain.has("SIZED.ZIP") || !ext.has("SIZED.ZIP") {
		t.Fatalf("file missing from a listing:\n%s\n---\n%s", plain.text(), ext.text())
	}
	if plain.has("desc of SIZED.ZIP") {
		t.Errorf("name-only listing shows the description:\n%s", plain.text())
	}
	if !ext.has("desc of SIZED.ZIP") {
		t.Errorf("extended listing hides the description:\n%s", ext.text())
	}
}

// TestListFilesRefusesWithoutArea pins LISTFILES' early exits: no user, no
// area selected, and an area the caller cannot read all return to the menu
// without listing.
func TestListFilesRefusesWithoutArea(t *testing.T) {
	env := newMenuEnv(t)
	addDownloadRecords(t, env, "HIDDEN.ZIP")

	if r := env.runCmd("LISTFILES", nil, "", "Q\r"); !r.has("must be logged in") {
		t.Errorf("no user:\n%s", r.text())
	}
	if r := env.runCmd("LISTFILES", env.caller, "", "Q\r"); !r.has("No file area selected") {
		t.Errorf("no area:\n%s", r.text())
	}
	env.caller.CurrentFileAreaID = 2 // Upload Queue: s250 to list
	if r := env.runCmd("LISTFILES", env.caller, "", "Q\r"); r.has("HIDDEN.ZIP") || r.err != nil {
		t.Errorf("unreadable area listed or errored (%v):\n%s", r.err, r.text())
	}
	env.caller.CurrentFileAreaID = 77
	if r := env.runCmd("LISTFILES", env.caller, "", "Q\r"); r.err != nil || r.has("HIDDEN.ZIP") {
		t.Errorf("stale area: err=%v", r.err)
	}
}

// TestListFilesClassicDownloadNothingMarked pins that D with no marked files
// explains how to mark one and never offers a protocol.
func TestListFilesClassicDownloadNothingMarked(t *testing.T) {
	env := classicListEnv(t)
	addDownloadRecords(t, env, "A.ZIP")

	r := env.runCmd("LISTFILES", env.caller, "", "D\rQ\r")
	if !r.has("No files marked for download.") || r.has("Transfer Protocols:") {
		t.Errorf("unexpected D output:\n%s", r.text())
	}
}

// TestListFilesClassicDownloadDeclined pins that marking a file and then
// answering No to the download prompt keeps the mark and saves nothing.
func TestListFilesClassicDownloadDeclined(t *testing.T) {
	env := classicListEnv(t)
	ids := addDownloadRecords(t, env, "A.ZIP")

	r := env.runCmd("LISTFILES", env.caller, "", "1\rD\rNQ\r")
	if !r.has("Download 1 marked file(s)?", "Download cancelled.") {
		t.Errorf("decline not shown:\n%s", r.text())
	}
	if len(env.caller.TaggedFileIDs) != 1 || env.caller.TaggedFileIDs[0] != ids[0] {
		t.Errorf("tags = %v, want [%v]", env.caller.TaggedFileIDs, ids[0])
	}
}

// TestListFilesClassicDownloadProtocolOutcomes pins the protocol step: with
// no protocols the download is abandoned, Q at the menu cancels, and in
// both cases the marks are kept for another try.
func TestListFilesClassicDownloadProtocolOutcomes(t *testing.T) {
	env := classicListEnv(t)
	addDownloadRecords(t, env, "A.ZIP")

	env.e.SetProtocols(nil)
	r := env.runCmd("LISTFILES", env.caller, "", "1\rD\rYQ\r")
	if !r.has("No transfer protocols configured") {
		t.Errorf("no protocols:\n%s", r.text())
	}

	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", SendCmd: "/nonexistent/sz", Default: true}})
	r = env.runCmd("LISTFILES", env.caller, "", "D\rYQ\rQ\r")
	if !r.has("Transfer Protocols:", "Download cancelled.") {
		t.Errorf("protocol cancel:\n%s", r.text())
	}
	if len(env.caller.TaggedFileIDs) != 1 {
		t.Errorf("tags = %v, want the mark kept", env.caller.TaggedFileIDs)
	}
}

// TestListFilesClassicDownloadMissingFiles pins that confirming a download
// whose marked files are not on disk reports them all as failed, never runs
// a transfer, and clears and saves the marks.
func TestListFilesClassicDownloadMissingFiles(t *testing.T) {
	env := classicListEnv(t)
	addDownloadRecords(t, env, "A.ZIP", "B.ZIP") // metadata only, no bytes on disk
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", SendCmd: "/nonexistent/sz", Default: true}})

	r := env.runCmd("LISTFILES", env.caller, "", "1\r2\rD\rY\rQ\r")
	if !r.has("Could not find any of the marked files", "Success: 0, Failed: 2.") {
		t.Errorf("missing-file outcome not reported:\n%s", r.text())
	}
	saved := env.mustDiskUser(env.caller.ID)
	if len(saved.TaggedFileIDs) != 0 || saved.NumDownloads != 0 {
		t.Errorf("saved tags=%v downloads=%d, want cleared and 0", saved.TaggedFileIDs, saved.NumDownloads)
	}
}

// TestListFilesClassicViewCommand pins V: an out-of-range or non-numeric
// number is refused, a text file is shown, and an archive is listed.
func TestListFilesClassicViewCommand(t *testing.T) {
	env := classicListEnv(t)
	addFileWithContent(t, env, 1, "A.TXT", []byte("inside a\n"))
	addFileWithContent(t, env, 1, "B.ZIP", zipBytes(t, map[string]string{"MEMBER.TXT": "m"}))

	r := env.runCmd("LISTFILES", env.caller, "", "V\rzz\rV\r9\rV\r\rV\r1\r\rQ\r")
	if !r.has("Invalid file number.", "File number not on current page.", "inside a") {
		t.Errorf("V outcomes missing:\n%s", r.text())
	}

	r = env.run(runListFiles, env.caller, "", "V\r2\rQ\rQ\r")
	if !r.has("MEMBER.TXT") {
		t.Errorf("archive listing missing:\n%s", r.text())
	}
	if r := env.runCmd("LISTFILES", env.caller, "", "V\r"); r.next != "LOGOFF" {
		t.Errorf("disconnect at V prompt: next = %q, want LOGOFF", r.next)
	}
}

// TestListFilesClassicUploadCommand pins U: the caller without upload access
// to General Files is refused; the sysop reaches the protocol menu and can
// back out at the start prompt without any transfer or new record.
func TestListFilesClassicUploadCommand(t *testing.T) {
	env := classicListEnv(t)
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", RecvCmd: "/nonexistent/rz", Default: true}})

	r := env.runCmd("LISTFILES", env.caller, "", "U\rQ\r")
	if !r.has("You do not have permission to upload to this area.") {
		t.Errorf("caller upload not refused:\n%s", r.text())
	}

	r = env.runCmd("LISTFILES", env.sysop, "", "U\r\rQ\rQ\r")
	if !r.has("Uploading to: General Files", "Start the Zmodem send") {
		t.Errorf("sysop upload prompt missing:\n%s", r.text())
	}
	if n, _ := env.e.FileMgr.GetFileCountForArea(1); n != 0 {
		t.Errorf("area has %d files after a cancelled upload", n)
	}

	r = env.runCmd("LISTFILES", env.caller, "", "A\rXYZ\r99\rQ\r")
	if !r.has("Use menu options to change area.") {
		t.Errorf("A not answered:\n%s", r.text())
	}
}
