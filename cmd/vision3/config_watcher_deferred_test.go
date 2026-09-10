package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
)

// newDeferredTestWatcher builds a watcher with one fake deferred target and a
// real session registry, driving poll() by hand.
func newDeferredTestWatcher(t *testing.T) (*ConfigWatcher, string, *recorder, *session.SessionRegistry) {
	t.Helper()
	dir := t.TempDir()
	rec := newRecorder()
	reg := session.NewSessionRegistry()

	cw := &ConfigWatcher{
		rootConfigPath:    dir,
		menuExecutor:      &menu.MenuExecutor{SessionRegistry: reg},
		interval:          defaultPollInterval,
		sentinelPath:      config.ReloadSentinelPath(dir),
		forceSentinelPath: config.ReloadForceSentinelPath(dir),
		mtimes:            make(map[string]time.Time),
		pending:           make(map[string]bool),
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
	}
	path := filepath.Join(dir, "structural.json")
	if err := os.WriteFile(path, []byte(`[]`), 0644); err != nil {
		t.Fatal(err)
	}
	cw.deferredTargets = []deferredTarget{{
		name:     "structural.json",
		path:     path,
		validate: func() error { return nil },
		apply:    func() error { rec.hit("structural.json")(); return nil },
	}}
	cw.seed()
	return cw, dir, rec, reg
}

// TestDeferredWaitsForIdle: a structural change with callers online queues
// and does not apply; it applies within one poll of the board going idle.
func TestDeferredWaitsForIdle(t *testing.T) {
	cw, dir, rec, reg := newDeferredTestWatcher(t)
	reg.Register(&session.BbsSession{ID: 1, NodeID: 1})

	touch(t, filepath.Join(dir, "structural.json"), time.Second)
	cw.poll()

	if got := rec.count("structural.json"); got != 0 {
		t.Fatalf("applied %d times with a caller online, want 0", got)
	}
	if got := cw.PendingReloads(); len(got) != 1 || got[0] != "structural.json" {
		t.Fatalf("PendingReloads() = %v, want [structural.json]", got)
	}

	// Still online, further polls change nothing.
	cw.poll()
	if got := rec.count("structural.json"); got != 0 {
		t.Fatalf("applied %d times while still online, want 0", got)
	}

	reg.Unregister(1)
	cw.poll()
	if got := rec.count("structural.json"); got != 1 {
		t.Errorf("applied %d times after going idle, want 1", got)
	}
	if got := cw.PendingReloads(); got != nil {
		t.Errorf("PendingReloads() = %v after apply, want nil", got)
	}
}

// TestDeferredAppliesImmediatelyWhenIdle: with nobody online the deferred
// lane degrades to the immediate one.
func TestDeferredAppliesImmediatelyWhenIdle(t *testing.T) {
	cw, dir, rec, _ := newDeferredTestWatcher(t)

	touch(t, filepath.Join(dir, "structural.json"), time.Second)
	cw.poll()

	if got := rec.count("structural.json"); got != 1 {
		t.Errorf("applied %d times on an idle board, want 1", got)
	}
}

// TestDeferredValidationFailureDoesNotQueue: a bad save is reported at
// signal time and never queued; fixing the file queues it afresh.
func TestDeferredValidationFailureDoesNotQueue(t *testing.T) {
	cw, dir, rec, reg := newDeferredTestWatcher(t)
	reg.Register(&session.BbsSession{ID: 1, NodeID: 1})

	valid := true
	cw.deferredTargets[0].validate = func() error {
		if valid {
			return nil
		}
		return os.ErrInvalid
	}

	valid = false
	touch(t, filepath.Join(dir, "structural.json"), time.Second)
	cw.poll()
	if got := cw.PendingReloads(); got != nil {
		t.Fatalf("invalid change queued: %v", got)
	}

	valid = true
	touch(t, filepath.Join(dir, "structural.json"), 2*time.Second)
	cw.poll()
	if got := cw.PendingReloads(); len(got) != 1 {
		t.Fatalf("fixed change not queued: %v", got)
	}

	reg.Unregister(1)
	cw.poll()
	if got := rec.count("structural.json"); got != 1 {
		t.Errorf("applied %d times, want 1", got)
	}
}

// TestForceSentinelBypassesIdleGate: touching reload.force applies deferred
// targets with callers still online.
func TestForceSentinelBypassesIdleGate(t *testing.T) {
	cw, dir, rec, reg := newDeferredTestWatcher(t)
	reg.Register(&session.BbsSession{ID: 1, NodeID: 1})

	touch(t, filepath.Join(dir, "structural.json"), time.Second)
	cw.poll()
	if got := rec.count("structural.json"); got != 0 {
		t.Fatalf("applied %d times before force, want 0", got)
	}

	touch(t, config.ReloadForceSentinelPath(dir), 2*time.Second)
	cw.poll()
	if got := rec.count("structural.json"); got != 1 {
		t.Errorf("applied %d times after force, want 1", got)
	}
	if got := cw.PendingReloads(); got != nil {
		t.Errorf("PendingReloads() = %v after force, want nil", got)
	}
}

// TestSaveSentinelDoesNotBypassIdleGate: reload.now is touched automatically
// by v3config on every save; it must queue structural changes, never apply
// them over live callers.
func TestSaveSentinelDoesNotBypassIdleGate(t *testing.T) {
	cw, dir, rec, reg := newDeferredTestWatcher(t)
	reg.Register(&session.BbsSession{ID: 1, NodeID: 1})

	touch(t, filepath.Join(dir, "structural.json"), time.Second)
	if err := config.TouchReloadSentinel(dir); err != nil {
		t.Fatal(err)
	}
	cw.poll()

	if got := rec.count("structural.json"); got != 0 {
		t.Fatalf("save sentinel applied a structural change over a live caller (%d times)", got)
	}
	if got := cw.PendingReloads(); len(got) != 1 {
		t.Fatalf("save sentinel did not queue the structural change: %v", got)
	}
}

// TestFileAreasReloadEndToEnd drives the real file_areas.json target through
// the watcher: queued while online, applied at idle, new area visible.
func TestFileAreasReloadEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "configs")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "file_areas.json"),
		[]byte(`[{"id":1,"tag":"ONE","name":"One","path":"one"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	fileMgr, err := file.NewFileManager(dataDir, configDir)
	if err != nil {
		t.Fatal(err)
	}
	reg := session.NewSessionRegistry()
	e := &menu.MenuExecutor{SessionRegistry: reg, FileMgr: fileMgr}

	cw := &ConfigWatcher{
		rootConfigPath:    configDir,
		menuExecutor:      e,
		interval:          defaultPollInterval,
		sentinelPath:      config.ReloadSentinelPath(configDir),
		forceSentinelPath: config.ReloadForceSentinelPath(configDir),
		mtimes:            make(map[string]time.Time),
		pending:           make(map[string]bool),
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
	}
	cw.deferredTargets = []deferredTarget{{
		name:     "file_areas.json",
		path:     filepath.Join(configDir, "file_areas.json"),
		validate: cw.validateFileAreas,
		apply:    cw.applyFileAreas,
	}}
	cw.seed()

	reg.Register(&session.BbsSession{ID: 1, NodeID: 1})
	if err := os.WriteFile(filepath.Join(configDir, "file_areas.json"),
		[]byte(`[{"id":1,"tag":"ONE","name":"One","path":"one"},{"id":2,"tag":"TWO","name":"Two","path":"two"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(time.Second)
	if err := os.Chtimes(filepath.Join(configDir, "file_areas.json"), ts, ts); err != nil {
		t.Fatal(err)
	}

	cw.poll()
	if _, ok := fileMgr.GetAreaByTag("TWO"); ok {
		t.Fatal("new area visible while a caller was online")
	}

	reg.Unregister(1)
	cw.poll()
	if _, ok := fileMgr.GetAreaByTag("TWO"); !ok {
		t.Error("new area not visible after the board went idle")
	}
}

// TestMessageAreasReloadEndToEnd drives the real message_areas.json target
// through the watcher: queued while online, applied at idle.
func TestMessageAreasReloadEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "configs")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"),
		[]byte(`[{"id":1,"tag":"GEN","name":"General"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	msgMgr, err := message.NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := session.NewSessionRegistry()
	e := &menu.MenuExecutor{SessionRegistry: reg, MessageMgr: msgMgr}

	cw := &ConfigWatcher{
		rootConfigPath:    configDir,
		menuExecutor:      e,
		interval:          defaultPollInterval,
		sentinelPath:      config.ReloadSentinelPath(configDir),
		forceSentinelPath: config.ReloadForceSentinelPath(configDir),
		mtimes:            make(map[string]time.Time),
		pending:           make(map[string]bool),
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
	}
	cw.deferredTargets = []deferredTarget{{
		name:     "message_areas.json",
		path:     filepath.Join(configDir, "message_areas.json"),
		validate: cw.validateMessageAreas,
		apply:    cw.applyMessageAreas,
	}}
	cw.seed()

	reg.Register(&session.BbsSession{ID: 1, NodeID: 1})
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"),
		[]byte(`[{"id":1,"tag":"GEN","name":"General"},{"id":2,"tag":"TECH","name":"Tech"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(time.Second)
	if err := os.Chtimes(filepath.Join(configDir, "message_areas.json"), ts, ts); err != nil {
		t.Fatal(err)
	}

	cw.poll()
	if _, ok := msgMgr.GetAreaByTag("TECH"); ok {
		t.Fatal("new message area visible while a caller was online")
	}

	reg.Unregister(1)
	cw.poll()
	if _, ok := msgMgr.GetAreaByTag("TECH"); !ok {
		t.Error("new message area not visible after the board went idle")
	}
}
