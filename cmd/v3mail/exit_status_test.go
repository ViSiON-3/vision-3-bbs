package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBaseFailureExitsNonZero pins #496: a command given a base it cannot
// open, followed by a good one, reports the bad one on stderr, still carries
// on and processes the good one, and exits 1, so scheduled maintenance can't
// mistake a failure for success.
func TestBaseFailureExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	good := seedBase(t, filepath.Join(dir, "good"), seedMsg{subject: "hello"})
	blocker := filepath.Join(dir, "afile")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(blocker, "base") // a path under a plain file never opens

	for _, tc := range []struct {
		name string
		args []string
		// goodOut is what the command prints for the good base, which it
		// only reaches by carrying on past the bad one listed first.
		goodOut string
	}{
		{"stats", []string{"stats", bad, good}, "=== " + good + " ==="},
		{"pack", []string{"pack", bad, good}, "good: no deleted messages, skipping"},
		{"purge", []string{"purge", "--days", "30", bad, good}, "good: deleted 0 messages"},
		{"lastread", []string{"lastread", bad, good}, "good: no lastread records"},
		{"link", []string{"link", bad, good}, "good: 1 messages, all links current"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runV3mail(t, dir, tc.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1\nstdout: %s\nstderr: %s", code, out, errOut)
			}
			wantContains(t, "stderr", errOut, "Error opening")
			wantContains(t, "stdout", out, tc.goodOut)
		})
	}
}
