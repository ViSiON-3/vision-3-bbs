package scripting

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDataStoreMacACLRejectsUpdates checks a writable store with an explicit ACL.
func TestDataStoreMacACLRejectsUpdates(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`v3.data.set("kept", 1)`)
	path := filepath.Join(h.dataDir, "test.json")
	if output, err := exec.Command("/bin/chmod", "+a", "everyone deny delete", path).CombinedOutput(); err != nil {
		t.Fatalf("set ACL: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("/bin/chmod", "-N", path).Run() })
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`v3.data.set("next", 2)`, `v3.data.delete("kept")`} {
		if got := h.evalErr(expr); !strings.Contains(got, "macOS ACL") {
			t.Errorf("%s: %s", expr, got)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatalf("store changed: %q, %v", after, err)
		}
	}
}

// TestDataStoreMacSymlinkDoesNotBypassACL checks a writable link to a readable
// target that restricts another local user. Neither may change on mutation.
func TestDataStoreMacSymlinkDoesNotBypassACL(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`v3.data.set("kept", 1)`)
	path := filepath.Join(h.dataDir, "test.json")
	target := filepath.Join(h.dataDir, "target.json")
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/bin/chmod", "+a", "nobody deny read", target).CombinedOutput(); err != nil {
		t.Fatalf("set target ACL: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("/bin/chmod", "-N", target).Run() })
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	aclBefore, err := exec.Command("/bin/ls", "-ldeq", target).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`v3.data.set("next", 2)`, `v3.data.delete("kept")`} {
		if got := h.evalErr(expr); !strings.Contains(got, "symbolic link") {
			t.Errorf("%s: %s", expr, got)
		}
		link, err := os.Readlink(path)
		if err != nil || link != target {
			t.Fatalf("link changed: %q, %v", link, err)
		}
		after, err := os.ReadFile(target)
		if err != nil || string(after) != string(before) {
			t.Fatalf("target changed: %q, %v", after, err)
		}
		aclAfter, err := exec.Command("/bin/ls", "-ldeq", target).Output()
		if err != nil || string(aclAfter) != string(aclBefore) {
			t.Fatalf("target ACL changed: %v", err)
		}
	}
}
