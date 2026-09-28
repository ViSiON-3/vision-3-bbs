package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// TestShowFileInfoDisplaysRecord pins SHOWFILEINFO: a filename typed in any
// case in the current area shows its size, date, uploader, downloads, area
// and description.
func TestShowFileInfoDisplaysRecord(t *testing.T) {
	env := newMenuEnv(t)
	for _, rec := range []file.FileRecord{
		{ID: uuid.New(), AreaID: 1, Filename: "BIG.ZIP", Size: 5000, DownloadCount: 7, UploadedBy: "Uploader",
			Description: "A big one", UploadedAt: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)},
		{ID: uuid.New(), AreaID: 1, Filename: "TINY.TXT", Size: 12, UploadedBy: "Sysop"},
	} {
		if err := env.e.FileMgr.AddFileRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	env.caller.CurrentFileAreaID = 1

	r := env.runCmd("SHOWFILEINFO", env.caller, "", "big.zip\r\r")
	if !r.has("BIG.ZIP", "5 KB", "02/03/2026", "Uploader", "Downloads   : 7", "General Files", "A big one") {
		t.Errorf("metadata missing:\n%s", r.text())
	}
	if r := env.runCmd("SHOWFILEINFO", env.caller, "", "TINY.TXT\r\r"); !r.has("12 bytes") {
		t.Errorf("small size not in bytes:\n%s", r.text())
	}
}

// TestShowFileInfoEdgeCases pins SHOWFILEINFO's exits: no user, no area,
// blank input, unknown file and a disconnect.
func TestShowFileInfoEdgeCases(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("SHOWFILEINFO", nil, "", "X\r"); r.raw != "" {
		t.Errorf("no user printed:\n%s", r.text())
	}
	if r := env.runCmd("SHOWFILEINFO", env.caller, "", "X\r"); !strings.Contains(r.text(), stripPipes(env.e.Strings().FileNoAreaSelected)) {
		t.Errorf("no area:\n%s", r.text())
	}
	env.caller.CurrentFileAreaID = 1
	if r := env.runCmd("SHOWFILEINFO", env.caller, "", "\r"); r.has("Filename    :") {
		t.Errorf("blank input showed info:\n%s", r.text())
	}
	if r := env.runCmd("SHOWFILEINFO", env.caller, "", "MISSING.ZIP\r"); !r.has("MISSING.ZIP") || r.has("Filename    :") {
		t.Errorf("unknown file:\n%s", r.text())
	}
	if r := env.runCmd("SHOWFILEINFO", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q", r.next)
	}
}
