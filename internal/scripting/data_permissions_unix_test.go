//go:build !windows

package scripting

import (
	"os"
	"os/exec"
	"path/filepath"
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
