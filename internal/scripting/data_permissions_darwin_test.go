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
