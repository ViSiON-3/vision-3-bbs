package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestRegisterUploadedFiles pins the post-transfer step of an upload: a new
// file is described, moved into the area and recorded; a name already in
// the area's records or already on disk is rejected and removed from the
// staging directory; and the uploader is credited only for accepted files.
func TestRegisterUploadedFiles(t *testing.T) {
	env := newMenuEnv(t)
	targetDir, err := env.e.FileMgr.GetAreaUploadPath(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "ONDISK.TXT"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	incoming := t.TempDir()
	staged := map[string]string{"DUP.TXT": "d", "NEW.TXT": "new bytes", "ONDISK.TXT": "new"}
	for n, body := range staged {
		if err := os.WriteFile(filepath.Join(incoming, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := []newFileInfo{{name: "DUP.TXT", size: 1}, {name: "NEW.TXT", size: 9}, {name: "ONDISK.TXT", size: 3}}

	var ok, dup int
	longDesc := strings.Repeat("x", 70)
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		ok, dup = c.e.registerUploadedFiles(c.s, c.terminal, c.outputMode, 1, files, incoming, targetDir, 1,
			c.currentUser, c.userManager, map[string]bool{"dup.txt": true})
		return nil, "", nil
	}, env.sysop, "", "\r"+longDesc+"\r\r\r")

	if ok != 1 || dup != 2 {
		t.Errorf("registered %d, duplicates %d; want 1 and 2\n%s", ok, dup, r.text())
	}
	if !r.has("'DUP.TXT' already exists in this area.", "'ONDISK.TXT' already exists in this area.", "NEW.TXT") {
		t.Errorf("messages missing:\n%s", r.text())
	}
	recs := env.e.FileMgr.GetFilesForArea(1)
	if len(recs) != 1 || recs[0].Filename != "NEW.TXT" || recs[0].UploadedBy != "Sysop" || recs[0].Size != 9 {
		t.Fatalf("records = %+v, want just NEW.TXT by Sysop", recs)
	}
	if recs[0].Description != strings.Repeat("x", 60) {
		t.Errorf("description = %q, want truncated to 60", recs[0].Description)
	}
	if b, _ := os.ReadFile(filepath.Join(targetDir, "NEW.TXT")); string(b) != "new bytes" {
		t.Errorf("NEW.TXT not moved into the area: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(targetDir, "ONDISK.TXT")); string(b) != "old" {
		t.Errorf("existing ONDISK.TXT clobbered: %q", b)
	}
	if left, _ := os.ReadDir(incoming); len(left) != 0 {
		t.Errorf("staging not emptied: %v", left)
	}
	if saved := env.mustDiskUser(env.sysop.ID); saved.NumUploads != 1 {
		t.Errorf("NumUploads = %d, want 1", saved.NumUploads)
	}
}

// TestRegisterUploadedFilesDefaultsDescription pins that a blank description,
// or a session lost at the prompt, records "No description" rather than
// dropping the upload, and that unsafe names are rejected without counting
// as duplicates.
func TestRegisterUploadedFilesDefaultsDescription(t *testing.T) {
	env := newMenuEnv(t)
	targetDir, err := env.e.FileMgr.GetAreaUploadPath(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	incoming := t.TempDir()
	for _, n := range []string{"BLANK.TXT", "LOST.TXT"} {
		if err := os.WriteFile(filepath.Join(incoming, n), []byte("b"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := []newFileInfo{{name: "../EVIL.TXT"}, {name: "BLANK.TXT", size: 1}, {name: "LOST.TXT", size: 1}}

	var ok, dup int
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		ok, dup = c.e.registerUploadedFiles(c.s, c.terminal, c.outputMode, 1, files, incoming, targetDir, 1,
			c.currentUser, c.userManager, map[string]bool{})
		return nil, "", nil
	}, env.sysop, "", "\r\r")

	if ok != 2 || dup != 0 {
		t.Errorf("registered %d, duplicates %d; want 2 and 0", ok, dup)
	}
	if !r.has("'../EVIL.TXT' rejected: invalid filename.") {
		t.Errorf("unsafe name not rejected:\n%s", r.text())
	}
	for _, rec := range env.e.FileMgr.GetFilesForArea(1) {
		if rec.Description != "No description" {
			t.Errorf("%s description = %q, want No description", rec.Filename, rec.Description)
		}
	}
}
