package ftn

import (
	"os"
	"strings"
	"testing"
)

func readConf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSyncBinkdConfUpdatesNodeHostAndPassword(t *testing.T) {
	// Hub change scenario: the link's hostname/port and password now live in
	// ftn.json; sync must rewrite the matching node line in place.
	path := writeConf(t, "iport 24554\nnode 21:4/999@fsxnet oldhub.example.org:24554 oldpwd\n")
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "newpwd", HostPort: "pointhub.example.org:24556"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	got := readConf(t, path)
	if !strings.Contains(got, "node 21:4/999@fsxnet pointhub.example.org:24556 newpwd") {
		t.Errorf("node line not rewritten:\n%s", got)
	}
	if strings.Contains(got, "oldhub") {
		t.Errorf("old host survived:\n%s", got)
	}
}

func TestSyncBinkdConfAppendsMissingNode(t *testing.T) {
	// A link configured in the TUI with a hostname but no node line yet
	// (e.g. address changed, or link added without the wizard) must get a
	// node line appended.
	path := writeConf(t, "iport 24554\n")
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "s3cret", HostPort: "pointhub.example.org:24556"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	if !strings.Contains(readConf(t, path), "node 21:4/999@fsxnet pointhub.example.org:24556 s3cret") {
		t.Errorf("missing node line not appended:\n%s", readConf(t, path))
	}
}

func TestSyncBinkdConfPasswordOnlyLinkDoesNotCreateNode(t *testing.T) {
	// A link with no hostname configured keeps the legacy behavior: update
	// an existing line's password, never invent a node line (no host known).
	path := writeConf(t, "iport 24554\n")
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "s3cret"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	if strings.Contains(readConf(t, path), "node ") {
		t.Errorf("node line invented without a hostname:\n%s", readConf(t, path))
	}
}

func TestSyncBinkdConfNoChangeLeavesFileUntouched(t *testing.T) {
	content := "iport 24554\nnode 21:4/999@fsxnet pointhub.example.org:24556 s3cret\n"
	path := writeConf(t, content)
	info1, _ := os.Stat(path)
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "s3cret", HostPort: "pointhub.example.org:24556"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	if readConf(t, path) != content {
		t.Errorf("file content changed on no-op sync")
	}
	info2, _ := os.Stat(path)
	if info1.ModTime() != info2.ModTime() {
		t.Errorf("file rewritten on no-op sync")
	}
}
