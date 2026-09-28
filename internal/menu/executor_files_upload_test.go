package menu

import (
	"path/filepath"
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
