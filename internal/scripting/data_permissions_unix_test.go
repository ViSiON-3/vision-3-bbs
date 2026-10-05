//go:build !windows

package scripting

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDataStoreUmask runs in a separate process so the mask cannot affect
// other tests or Go runtime filesystem operations in this process.
func TestDataStoreUmask(t *testing.T) {
	if os.Getenv("VISION_DATA_UMASK_CHILD") != "1" {
		cmd := exec.Command("sh", "-c", `umask 077; exec "$1" -test.run '^TestDataStoreUmask$'`, "sh", os.Args[0])
		cmd.Env = append(os.Environ(), "VISION_DATA_UMASK_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("umask child: %v\n%s", err, output)
		}
		return
	}
	h := newHarness(t, harnessOpts{})
	h.mustRun(`v3.data.set("kept", 1)`)
	info, err := os.Stat(filepath.Join(h.dataDir, "test.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	h.mustRun(`v3.data.set("next", 2); v3.data.delete("next")`)
}

// TestDataStoreSymlink rejects a mutation that would replace the link itself.
func TestDataStoreSymlink(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`v3.data.set("kept", 1)`)
	path := filepath.Join(h.dataDir, "test.json")
	target := filepath.Join(h.dataDir, "target.json")
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`v3.data.set("next", 2)`, `v3.data.delete("kept")`} {
		if got := h.evalErr(expr); !strings.Contains(got, "symbolic link") {
			t.Errorf("%s: %s", expr, got)
		}
		if _, err := os.Readlink(path); err != nil {
			t.Fatalf("link changed: %v", err)
		}
		after, err := os.ReadFile(target)
		if err != nil || string(after) != string(before) {
			t.Fatalf("target changed: %q, %v", after, err)
		}
	}
	// Once its target disappears, all reads must report the dangling link.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`v3.data.get("kept")`, `v3.data.keys()`, `v3.data.getAll()`, `v3.data.set("next", 2)`, `v3.data.delete("kept")`} {
		if got := h.evalErr(expr); !strings.Contains(got, "read script data") {
			t.Errorf("%s: %s", expr, got)
		}
	}
}
