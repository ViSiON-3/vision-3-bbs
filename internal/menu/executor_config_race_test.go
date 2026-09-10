package menu

import (
	"strconv"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
)

// TestConfigReloadRaceWithReaders is the regression test for issue #321.
//
// The hot-reload setters took a mutex, but the several hundred read sites did
// not, so every reload raced against every active session. Run under -race,
// this reloads each config in a loop while readers hammer the accessors; it
// fails on the pre-fix code and passes on the snapshot-based one.
func TestConfigReloadRaceWithReaders(t *testing.T) {
	e := &MenuExecutor{}
	e.SetStrings(config.StringsConfig{PauseString: "initial"})
	e.SetTheme(config.ThemeConfig{})
	e.SetServerConfig(config.ServerConfig{BoardName: "initial"})
	e.SetDoorRegistry(map[string]config.DoorConfig{"A": {}})
	e.SetLoginSequence([]config.LoginItem{{}})
	e.SetProtocols([]transfer.ProtocolConfig{{Name: "initial"}})

	// The writer bounds the test: it runs a fixed number of reloads and then
	// signals the readers to stop. Letting the main goroutine end the test
	// instead would close the window in microseconds, often before the writer
	// was even scheduled, and the race detector would report nothing.
	const reloads = 5000

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := 0; i < reloads; i++ {
			n := strconv.Itoa(i)
			e.SetStrings(config.StringsConfig{PauseString: n})
			e.SetTheme(config.ThemeConfig{YesNoHighlightColor: i % 256})
			e.SetServerConfig(config.ServerConfig{BoardName: n, CoSysOpLevel: i % 255})
			e.SetDoorRegistry(map[string]config.DoorConfig{n: {}})
			e.SetLoginSequence([]config.LoginItem{{}, {}})
			e.SetProtocols([]transfer.ProtocolConfig{{Name: n}})
		}
	}()

	// Readers use the shapes real call sites use, until the writer is finished.
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				_ = e.Strings().PauseString
				_ = e.Theme().YesNoHighlightColor
				_ = e.GetServerConfig().CoSysOpLevel
				_ = len(e.DoorRegistry())
				_, _ = e.GetDoorConfig("A")
				_ = len(e.GetLoginSequence())
				for range e.Protocols() {
				}
			}
		}()
	}

	wg.Wait()
}

// TestAccessorsSafeOnZeroExecutor covers an executor built without
// NewExecutor — the accessors must return usable zero values rather than
// dereferencing a nil snapshot. Several tests construct executors this way.
func TestAccessorsSafeOnZeroExecutor(t *testing.T) {
	e := &MenuExecutor{}

	if got := e.Strings().PauseString; got != "" {
		t.Errorf("Strings().PauseString = %q, want empty", got)
	}
	if got := e.Theme().YesNoHighlightColor; got != 0 {
		t.Errorf("Theme().YesNoHighlightColor = %d, want 0", got)
	}
	if got := e.GetServerConfig().BoardName; got != "" {
		t.Errorf("GetServerConfig().BoardName = %q, want empty", got)
	}
	if got := e.DoorRegistry(); got != nil {
		t.Errorf("DoorRegistry() = %v, want nil", got)
	}
	if _, ok := e.GetDoorConfig("nope"); ok {
		t.Error("GetDoorConfig found a door in an empty registry")
	}
	if got := e.GetLoginSequence(); got != nil {
		t.Errorf("GetLoginSequence() = %v, want nil", got)
	}
	if got := e.Protocols(); got != nil {
		t.Errorf("Protocols() = %v, want nil", got)
	}
}

// TestGetServerConfigReturnsACopy guards the one accessor that deliberately
// returns a value: callers adjust the result locally, and a pointer would let
// that leak into the snapshot every other session is reading.
func TestGetServerConfigReturnsACopy(t *testing.T) {
	e := &MenuExecutor{}
	e.SetServerConfig(config.ServerConfig{BoardName: "original", CoSysOpLevel: 250})

	cfg := e.GetServerConfig()
	cfg.BoardName = "mutated"
	cfg.CoSysOpLevel = 1

	if got := e.GetServerConfig().BoardName; got != "original" {
		t.Errorf("BoardName = %q after mutating a returned copy, want %q", got, "original")
	}
	if got := e.GetServerConfig().CoSysOpLevel; got != 250 {
		t.Errorf("CoSysOpLevel = %d after mutating a returned copy, want 250", got)
	}
}

// TestSetStringsSnapshotIsIsolated confirms a stored config is decoupled from
// the caller's variable, so a caller reusing its local cannot mutate what
// readers already loaded.
func TestSetStringsSnapshotIsIsolated(t *testing.T) {
	e := &MenuExecutor{}
	cfg := config.StringsConfig{PauseString: "first"}
	e.SetStrings(cfg)

	cfg.PauseString = "second" // caller reuses its local
	if got := e.Strings().PauseString; got != "first" {
		t.Errorf("PauseString = %q after the caller mutated its local, want %q", got, "first")
	}
}

// TestCollectionSnapshotsAreIsolated enforces what used to be only a doc
// comment (flagged in review of #327): callers of the map/slice setters and
// accessors must not be able to reach the shared snapshot. The setter clones
// its argument; the collection accessors clone their result.
func TestCollectionSnapshotsAreIsolated(t *testing.T) {
	e := &MenuExecutor{}

	doors := map[string]config.DoorConfig{"A": {}}
	e.SetDoorRegistry(doors)
	doors["B"] = config.DoorConfig{} // caller mutates its own map after storing
	if _, ok := e.GetDoorConfig("B"); ok {
		t.Error("mutating the setter's argument reached the stored door registry")
	}
	reg := e.DoorRegistry()
	reg["C"] = config.DoorConfig{} // caller mutates the returned map
	if _, ok := e.GetDoorConfig("C"); ok {
		t.Error("mutating an accessor's returned map reached the stored door registry")
	}

	seq := []config.LoginItem{{}}
	e.SetLoginSequence(seq)
	seq[0] = config.LoginItem{Command: "mutated"}
	if got := e.GetLoginSequence(); len(got) != 1 || got[0].Command == "mutated" {
		t.Error("mutating the setter's argument reached the stored login sequence")
	}
	out := e.GetLoginSequence()
	out[0].Command = "mutated-out"
	if got := e.GetLoginSequence(); got[0].Command == "mutated-out" {
		t.Error("mutating an accessor's returned slice reached the stored login sequence")
	}

	protos := []transfer.ProtocolConfig{{Name: "Z"}}
	e.SetProtocols(protos)
	protos[0].Name = "mutated"
	if got := e.Protocols(); len(got) != 1 || got[0].Name != "Z" {
		t.Errorf("mutating the setter's argument reached the stored protocols: %+v", got)
	}
	pout := e.Protocols()
	pout[0].Name = "mutated-out"
	if got := e.Protocols(); got[0].Name != "Z" {
		t.Error("mutating an accessor's returned slice reached the stored protocols")
	}
}
