package ziplab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oldPack is an art pack packed with PKZIP 1.x's methods (FILE_ID.DIZ and
// OLDPACK.ANS imploded, README shrunk, NOTES.TXT reduced), from the pkzip1
// package's tests.
var oldPack = filepath.Join("..", "pkzip1", "testdata", "oldpack.zip")

// TestOldZipUpload checks that an archive from before PKZIP 2.04 passes the
// upload check, extracts in full, and gives its FILE_ID.DIZ as the
// description, where each used to fail as a corrupt or unreadable entry.
func TestOldZipUpload(t *testing.T) {
	p := NewProcessor(DefaultConfig(), "")
	if err := p.testZipIntegrity(oldPack); err != nil {
		t.Errorf("integrity: %v", err)
	}
	dest := t.TempDir()
	if err := p.extractZip(oldPack, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for name, want := range map[string]string{
		"FILE_ID.DIZ": "OLD PACK",
		"OLDPACK.ANS": "SAUCE00Old Pack Art",
		"README":      "made for tests",
	} {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || !strings.Contains(string(data), want) {
			t.Errorf("%s: %q, %v", name, data, err)
		}
	}
	if fi, err := os.Stat(filepath.Join(dest, "NOTES.TXT")); err != nil || fi.Size() != 2000 {
		t.Errorf("NOTES.TXT: %v, %v", fi, err)
	}
	diz, err := ExtractDIZFromZip(oldPack)
	if err != nil || !strings.Contains(diz, "a test art pack") {
		t.Errorf("DIZ = %q, %v", diz, err)
	}
}

// TestUnreadableZipUpload checks that entries nothing here can unpack (a
// bzip2 member, an encrypted one) still fail the upload check.
func TestUnreadableZipUpload(t *testing.T) {
	err := NewProcessor(DefaultConfig(), "").testZipIntegrity(filepath.Join("..", "pkzip1", "testdata", "unreadable.zip"))
	if err == nil || !strings.Contains(err.Error(), "corrupt entry BZIP2.TXT") {
		t.Errorf("integrity = %v", err)
	}
}
