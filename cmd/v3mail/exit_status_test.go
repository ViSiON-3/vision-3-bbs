package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBaseFailureExitsNonZero pins #496: a command given one good base and
// one it cannot open still processes the good one, reports the bad one on
// stderr, and exits 1, so scheduled maintenance can't mistake a failure for
// success.
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
	}{
		{"stats", []string{"stats", good, bad}},
		{"pack", []string{"pack", good, bad}},
		{"purge", []string{"purge", "--days", "30", good, bad}},
		{"lastread", []string{"lastread", good, bad}},
		{"link", []string{"link", good, bad}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runV3mail(t, dir, tc.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1\nstdout: %s\nstderr: %s", code, out, errOut)
			}
			wantContains(t, "stderr", errOut, "Error opening")
			if _, err := os.Stat(good + ".jhr"); err != nil {
				t.Errorf("good base damaged: %v", err)
			}
		})
	}
}
