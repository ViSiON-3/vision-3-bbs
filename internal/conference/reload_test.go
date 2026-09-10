package conference

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfs(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, conferencesFile), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReloadReplacesDefinitions(t *testing.T) {
	dir := t.TempDir()
	writeConfs(t, dir, `[{"id":1,"tag":"GEN","name":"General"}]`)

	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cm.GetByID(1); !ok {
		t.Fatal("conference 1 missing after initial load")
	}

	writeConfs(t, dir, `[{"id":2,"tag":"TECH","name":"Tech"},{"id":3,"tag":"CHAT","name":"Chat"}]`)
	if err := cm.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if _, ok := cm.GetByID(1); ok {
		t.Error("conference 1 still present after being removed from the file")
	}
	if _, ok := cm.GetByTag("TECH"); !ok {
		t.Error("conference TECH missing after reload")
	}
	if got := len(cm.ListConferences()); got != 2 {
		t.Errorf("ListConferences() = %d entries, want 2", got)
	}
}

// TestReloadKeepsOldOnBadFile: a malformed save must not wipe the running
// definitions — the reload fails and the previous set stays.
func TestReloadKeepsOldOnBadFile(t *testing.T) {
	dir := t.TempDir()
	writeConfs(t, dir, `[{"id":1,"tag":"GEN","name":"General"}]`)

	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	writeConfs(t, dir, `{definitely not json`)
	if err := cm.Reload(); err == nil {
		t.Fatal("Reload succeeded on a malformed file")
	}

	if _, ok := cm.GetByID(1); !ok {
		t.Error("conference 1 lost after a failed reload; old definitions must survive")
	}
}

// TestReloadEmptyFileClearsDefinitions: emptying the file means "no
// conferences", and must not leave the previous contents in memory.
func TestReloadEmptyFileClearsDefinitions(t *testing.T) {
	dir := t.TempDir()
	writeConfs(t, dir, `[{"id":1,"tag":"GEN","name":"General"}]`)

	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	writeConfs(t, dir, ``)
	if err := cm.Reload(); err != nil {
		t.Fatalf("Reload of an empty file: %v", err)
	}
	if got := len(cm.ListConferences()); got != 0 {
		t.Errorf("ListConferences() = %d entries after emptying the file, want 0", got)
	}
}

// TestReloadRaceWithReaders proves the map swap is safe against concurrent
// lookups under -race.
func TestReloadRaceWithReaders(t *testing.T) {
	dir := t.TempDir()
	writeConfs(t, dir, `[{"id":1,"tag":"GEN","name":"General"}]`)

	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			if err := cm.Reload(); err != nil {
				t.Errorf("Reload: %v", err)
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		default:
		}
		_, _ = cm.GetByID(1)
		_, _ = cm.GetByTag("GEN")
		_ = cm.ListConferences()
	}
}

// TestReloadMissingFileClearsDefinitions: deleting conferences.json converges
// to the boot behavior for a missing file — no conferences — rather than
// erroring and keeping stale definitions.
func TestReloadMissingFileClearsDefinitions(t *testing.T) {
	dir := t.TempDir()
	writeConfs(t, dir, `[{"id":1,"tag":"GEN","name":"General"}]`)

	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(dir, conferencesFile)); err != nil {
		t.Fatal(err)
	}
	if err := cm.Reload(); err != nil {
		t.Fatalf("Reload with the file missing: %v", err)
	}
	if got := len(cm.ListConferences()); got != 0 {
		t.Errorf("ListConferences() = %d entries after deleting the file, want 0", got)
	}
}
