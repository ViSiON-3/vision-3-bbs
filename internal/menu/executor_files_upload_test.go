package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
)

// TestUploadFileRefusals pins UPLOADFILE's gates before any transfer: no
// user, no area selected, and an area the caller may not upload to.
func TestUploadFileRefusals(t *testing.T) {
	env := newMenuEnv(t)
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", RecvCmd: "/nonexistent/rz", Default: true}})

	if r := env.runCmd("UPLOADFILE", nil, "", ""); !r.has("You must be logged in to upload files.") {
		t.Errorf("no user:\n%s", r.text())
	}
	if r := env.runCmd("UPLOADFILE", env.caller, "", ""); !r.has("No file area selected.") || r.user != env.caller {
		t.Errorf("no area:\n%s", r.text())
	}
	env.caller.CurrentFileAreaID = 1 // General Files: upload s250
	r := env.runCmd("UPLOADFILE", env.caller, "", "\r")
	if !r.has("You do not have permission to upload to this area.") || r.has("Transfer Protocols:") {
		t.Errorf("caller upload not refused:\n%s", r.text())
	}
	env.caller.CurrentFileAreaID = 42
	if r := env.runCmd("UPLOADFILE", env.caller, "", "\r"); r.has("Transfer Protocols:") || r.user == nil {
		t.Errorf("missing area should end quietly:\n%s", r.text())
	}
}

// TestUploadFileRegistersReceivedFile pins a successful UPLOADFILE journey:
// the receive command writes into staging, the file and fallback metadata
// move into the area, and the upload credit persists.
func TestUploadFileRegistersReceivedFile(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.CurrentFileAreaID = 2
	env.caller.CurrentFileAreaTag = "UPLOADS"

	receiver := filepath.Join(t.TempDir(), "receive.sh")
	if err := os.WriteFile(receiver, []byte("#!/bin/sh\nprintf 'received payload' > \"$1/UPLOADED.TXT\"\n"), 0o755); err != nil {
		t.Fatalf("write receive command: %v", err)
	}
	env.e.SetProtocols([]transfer.ProtocolConfig{{
		Key: "T", Name: "Testmodem", RecvCmd: "/bin/sh", RecvArgs: []string{receiver, "{targetDir}"}, Default: true,
	}})

	r := env.runCmd("UPLOADFILE", env.caller, "", "\r\r\r\r")
	if r.err != nil || r.user == nil || r.user.ID != env.caller.ID {
		t.Fatalf("result = (user %v, err %v), want reloaded caller and no error", r.user, r.err)
	}
	if !r.has("Starting Testmodem receive", "UPLOADED.TXT", "Upload complete.", "Added: 1") {
		t.Errorf("successful upload output missing:\n%s", r.text())
	}
	areaPath, err := env.e.FileMgr.GetAreaUploadPath(2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(areaPath, "UPLOADED.TXT"))
	if err != nil || string(got) != "received payload" {
		t.Errorf("installed file = %q, err %v", got, err)
	}
	records := env.e.FileMgr.GetFilesForArea(2)
	if len(records) != 1 || records[0].Filename != "UPLOADED.TXT" || records[0].Description != "No description" || records[0].Size != int64(len("received payload")) || records[0].UploadedBy != env.caller.Handle {
		t.Errorf("registered records = %+v", records)
	}
	if saved := env.mustDiskUser(env.caller.ID); saved.NumUploads != 1 {
		t.Errorf("saved NumUploads = %d, want 1", saved.NumUploads)
	}
	if matches, _ := filepath.Glob(filepath.Join(areaPath, ".incoming-*")); len(matches) != 0 {
		t.Errorf("staging directories left behind: %s", strings.Join(matches, ", "))
	}
}

// TestUploadFileCancelPaths pins that the caller uploading to the Upload
// Queue can back out at every step before a transfer starts — no protocols,
// Q at the protocol menu, Q at the start prompt — leaving no staging
// directory or record behind, and that a disconnect logs off.
func TestUploadFileCancelPaths(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.CurrentFileAreaID = 2
	env.caller.CurrentFileAreaTag = "UPLOADS"

	env.e.SetProtocols(nil)
	if r := env.runCmd("UPLOADFILE", env.caller, "", "\r"); !r.has("No transfer protocols configured") {
		t.Errorf("no protocols:\n%s", r.text())
	}

	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "Z", Name: "Zmodem", RecvCmd: "/nonexistent/rz", Default: true}})
	r := env.runCmd("UPLOADFILE", env.caller, "", "Q\r")
	if !r.has("Uploading to: Upload Queue", "Transfer Protocols:") || r.has("Start the Zmodem send") {
		t.Errorf("Q at protocol menu:\n%s", r.text())
	}
	r = env.runCmd("UPLOADFILE", env.caller, "", "\rq\r")
	if !r.has("Start the Zmodem send") || r.has("Starting Zmodem receive") {
		t.Errorf("Q at start prompt:\n%s", r.text())
	}
	if r.user == nil || r.user.ID != env.caller.ID {
		t.Errorf("user = %v, want the reloaded caller", r.user)
	}
	for _, in := range []string{"", "\r"} {
		if r := env.runCmd("UPLOADFILE", env.caller, "", in); r.next != "LOGOFF" {
			t.Errorf("disconnect after %q: next = %q, want LOGOFF", in, r.next)
		}
	}

	dir, err := env.e.FileMgr.GetAreaUploadPath(2)
	if err != nil {
		t.Fatal(err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".incoming-*")); len(matches) != 0 {
		t.Errorf("staging left behind: %v", matches)
	}
	if n, _ := env.e.FileMgr.GetFileCountForArea(2); n != 0 {
		t.Errorf("%d records after cancelled uploads", n)
	}
}
