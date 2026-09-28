package scripting

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/dop251/goja"
)

// scriptHarness runs V3 scripts against a real Engine wired to fake BBS state.
//
// Layout under a per-test temp dir:
//
//	<root>/menus/            anchors findBBSRoot so art lookups stay in <root>
//	<root>/scripts/          engine WorkingDir; scripts are written here
//	<root>/scripts/data/     v3.fs sandbox and v3.data store
//
// The session is an interruptibleSession: scripted input is replayed, then
// Read blocks until cleanup closes the interrupt (as a real connection does).
// Everything the script writes is captured and returned by output().
type scriptHarness struct {
	t          *testing.T
	root       string
	scriptsDir string
	dataDir    string
	sess       *interruptibleSession
	eng        *Engine
	sc         *SessionContext
	hangup     sync.Once
}

// harnessOpts tweaks the engine a harness builds. Zero value is a
// console-only engine with no providers and the default run time.
type harnessOpts struct {
	input      string
	providers  *Providers
	maxRunTime time.Duration
	ctx        context.Context
	args       []string
	session    func(*SessionContext)
	// workingDir overrides the engine working dir (relative to root).
	workingDir string
}

// newHarness builds an engine for one test and registers its cleanup.
func newHarness(t *testing.T, o harnessOpts) *scriptHarness {
	t.Helper()
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	// scripts/data ships in the repo (.gitkeep); mirror that layout.
	for _, d := range []string{filepath.Join(root, "menus"), filepath.Join(scriptsDir, "data")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	wd := scriptsDir
	if o.workingDir != "" {
		wd = filepath.Join(root, o.workingDir)
		if err := os.MkdirAll(wd, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sc := &SessionContext{
		OutputMode:       ansi.OutputModeCP437,
		UserID:           7,
		UserHandle:       "Tester",
		UserRealName:     "Test User",
		AccessLevel:      50,
		TimeLimit:        60,
		TimesCalled:      12,
		Location:         "Testville",
		ScreenWidth:      80,
		ScreenHeight:     24,
		NodeNumber:       3,
		SessionStartTime: time.Now(),
		BoardName:        "Test BBS",
		SysOpName:        "Sysop",
		BBSVersion:       "3.0-test",
	}
	if o.session != nil {
		o.session(sc)
	}
	sess := newInterruptibleSession(o.input)
	sc.Session = sess
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	eng := NewEngine(ctx, sc, ScriptConfig{
		Script:     "test.js",
		WorkingDir: wd,
		Args:       o.args,
		MaxRunTime: o.maxRunTime,
	}, o.providers)

	h := &scriptHarness{
		t:          t,
		root:       root,
		scriptsDir: scriptsDir,
		dataDir:    filepath.Join(scriptsDir, "data"),
		sess:       sess,
		eng:        eng,
		sc:         sc,
	}
	t.Cleanup(func() {
		h.disconnect()
		eng.Close()
	})
	return h
}

// disconnect simulates the caller dropping: blocked session reads return EOF.
// Safe to call more than once.
func (h *scriptHarness) disconnect() {
	h.hangup.Do(h.sess.closeInterrupt)
}

// run writes src to test.js in the working dir and executes it via Engine.Run.
func (h *scriptHarness) run(src string) error {
	h.t.Helper()
	path := filepath.Join(h.eng.cfg.WorkingDir, "test.js")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return h.eng.Run("test.js")
}

// mustRun is run that fails the test on any script error.
func (h *scriptHarness) mustRun(src string) {
	h.t.Helper()
	if err := h.run(src); err != nil {
		h.t.Fatalf("script failed: %v\nsource:\n%s", err, src)
	}
}

// eval evaluates a JS expression in the engine's VM and returns its value.
func (h *scriptHarness) eval(expr string) goja.Value {
	h.t.Helper()
	v, err := h.eng.vm.RunString(expr)
	if err != nil {
		h.t.Fatalf("eval %q: %v", expr, err)
	}
	return v
}

// evalErr evaluates expr and returns the JS exception message; it fails the
// test if evaluation succeeds.
func (h *scriptHarness) evalErr(expr string) string {
	h.t.Helper()
	_, err := h.eng.vm.RunString(expr)
	if err == nil {
		h.t.Fatalf("eval %q: expected a JS exception, got none", expr)
	}
	var ex *goja.Exception
	if errors.As(err, &ex) {
		return ex.Error()
	}
	return err.Error()
}

// output returns everything written to the session so far.
func (h *scriptHarness) output() string {
	h.sess.mu.Lock()
	defer h.sess.mu.Unlock()
	return string(h.sess.out)
}

// resetOutput discards captured output.
func (h *scriptHarness) resetOutput() {
	h.sess.mu.Lock()
	h.sess.out = nil
	h.sess.mu.Unlock()
}

// writeFile writes content to root-relative path, creating parents.
func (h *scriptHarness) writeFile(rel, content string) string {
	h.t.Helper()
	p := filepath.Join(h.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

// pipe returns the ANSI expansion of a pipe-coded string, for expectations.
func pipe(s string) string { return string(ansi.ReplacePipeCodes([]byte(s))) }
