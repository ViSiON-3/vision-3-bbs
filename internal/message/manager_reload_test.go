package message

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMsgAreas(t *testing.T, configDir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReloadReplacesAreas(t *testing.T) {
	dataDir, configDir := t.TempDir(), t.TempDir()
	writeMsgAreas(t, configDir, `[{"id":1,"tag":"OLD","name":"Old"}]`)

	mm, err := NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mm.GetAreaByTag("OLD"); !ok {
		t.Fatal("area OLD missing after initial load")
	}

	writeMsgAreas(t, configDir, `[{"id":2,"tag":"NEW","name":"New","echo_tag":"NEW_ECHO","network":"testnet"}]`)
	if err := mm.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if _, ok := mm.GetAreaByTag("OLD"); ok {
		t.Error("removed area OLD still resolvable")
	}
	if _, ok := mm.GetAreaByTag("NEW"); !ok {
		t.Error("added area NEW not resolvable")
	}
	if _, ok := mm.GetAreaByEchoTag("NEW_ECHO"); !ok {
		t.Error("added area's echo tag not resolvable")
	}
}

func TestReloadKeepsOldOnBadFile(t *testing.T) {
	dataDir, configDir := t.TempDir(), t.TempDir()
	writeMsgAreas(t, configDir, `[{"id":1,"tag":"KEEP","name":"Keep"}]`)

	mm, err := NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}

	writeMsgAreas(t, configDir, `{not json`)
	if err := mm.Reload(); err == nil {
		t.Fatal("Reload succeeded on a malformed file")
	}
	if _, ok := mm.GetAreaByTag("KEEP"); !ok {
		t.Error("area KEEP lost after a failed reload")
	}
}

func TestReloadEmptyFileClearsAreas(t *testing.T) {
	dataDir, configDir := t.TempDir(), t.TempDir()
	writeMsgAreas(t, configDir, `[{"id":1,"tag":"GONE","name":"Gone"}]`)

	mm, err := NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}

	writeMsgAreas(t, configDir, ``)
	if err := mm.Reload(); err != nil {
		t.Fatalf("Reload of an empty file: %v", err)
	}
	if got := len(mm.ListAreas()); got != 0 {
		t.Errorf("ListAreas() = %d after emptying the file, want 0", got)
	}
}

// TestReloadDropsCachedIndexes: an area's BasePath can change across a
// reload, and a stale thread/MSGID index would answer for the wrong base.
func TestReloadDropsCachedIndexes(t *testing.T) {
	dataDir, configDir := t.TempDir(), t.TempDir()
	writeMsgAreas(t, configDir, `[{"id":1,"tag":"A","name":"A"}]`)

	mm, err := NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Seed fake cache entries directly; building real ones needs a JAM base
	// and the property under test is only that Reload drops whatever is there.
	mm.mu.Lock()
	mm.threadIndex[1] = &threadIndex{}
	mm.msgidIndex[1] = &msgidIndex{}
	mm.mu.Unlock()

	if err := mm.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	mm.mu.RLock()
	ti, mi := len(mm.threadIndex), len(mm.msgidIndex)
	mm.mu.RUnlock()
	if ti != 0 || mi != 0 {
		t.Errorf("cached indexes survived reload: thread=%d msgid=%d, want 0/0", ti, mi)
	}
}

// TestReloadRaceWithReaders proves the map swap is safe against concurrent
// lookups under -race.
func TestReloadRaceWithReaders(t *testing.T) {
	dataDir, configDir := t.TempDir(), t.TempDir()
	writeMsgAreas(t, configDir, `[{"id":1,"tag":"GEN","name":"General"}]`)

	mm, err := NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			if err := mm.Reload(); err != nil {
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
		_, _ = mm.GetAreaByTag("GEN")
		_, _ = mm.GetAreaByID(1)
		_ = mm.ListAreas()
	}
}
