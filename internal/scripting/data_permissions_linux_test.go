package scripting

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestDataStoreCustomizedACLRejectsUpdates leaves explicit POSIX ACLs intact.
func TestDataStoreCustomizedACLRejectsUpdates(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`v3.data.set("kept", 1)`)
	path := filepath.Join(h.dataDir, "test.json")
	acl := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(acl, 2)
	entries := []struct {
		tag, perm uint16
		id        uint32
	}{{1, 6, ^uint32(0)}, {2, 0, uint32(os.Getuid())}, {4, 4, ^uint32(0)}, {16, 4, ^uint32(0)}, {32, 4, ^uint32(0)}}
	for i, entry := range entries {
		b := acl[4+i*8:]
		binary.LittleEndian.PutUint16(b, entry.tag)
		binary.LittleEndian.PutUint16(b[2:], entry.perm)
		binary.LittleEndian.PutUint32(b[4:], entry.id)
	}
	if err := unix.Setxattr(path, "system.posix_acl_access", acl, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) && os.Getenv("VISION_REQUIRE_DATA_ACL") != "1" {
			t.Skip("filesystem does not support POSIX ACLs")
		}
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`v3.data.set("next", 2)`, `v3.data.delete("kept")`} {
		if got := h.evalErr(expr); !strings.Contains(got, "file-specific POSIX ACL") {
			t.Errorf("%s: %s", expr, got)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("store changed: %q, %v", after, err)
		}
		current := make([]byte, len(acl))
		n, err := unix.Getxattr(path, "system.posix_acl_access", current)
		if err != nil || !bytes.Equal(current[:n], acl) {
			t.Fatalf("ACL changed: %v", err)
		}
	}
	entriesOnDisk, err := os.ReadDir(h.dataDir)
	if err != nil || len(entriesOnDisk) != 1 {
		t.Fatalf("temporary files remain: %v, %v", entriesOnDisk, err)
	}
}
