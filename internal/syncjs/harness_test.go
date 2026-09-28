package syncjs

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

// doorHarness runs Synchronet-style JS against a real Engine with fake
// session state.
//
// Layout under a per-test temp dir:
//
//	<root>/game/    WorkingDir (scripts are written here as test.js)
//	<root>/exec/    ExecDir (Synchronet standard libs)
//	<root>/data/    DataDir
//	<root>/node/    NodeDir
//	<root>/lib/     LibraryPaths[0]
//
// The session is an interruptibleSession: scripted input is replayed, then
// Read blocks until cleanup (or disconnect) closes the interrupt, like a live
// connection. Everything written is captured and returned by output().
type doorHarness struct {
	t      *testing.T
	root   string
	game   string
	sess   *interruptibleSession
	eng    *Engine
	sc     *SessionContext
	hangup sync.Once
	closed sync.Once
}

// doorOpts tweaks the engine a harness builds; the zero value is fine.
type doorOpts struct {
	input string
	// inputChunks follow input, each delivered by its own session Read,
	// as bytes split across network reads arrive.
	inputChunks []string
	args        []string
	ctx         context.Context
	session     func(*SessionContext)
}

// newDoor builds an engine for one test and registers its cleanup.
func newDoor(t *testing.T, o doorOpts) *doorHarness {
	t.Helper()
	root := t.TempDir()
	dirs := map[string]string{}
	for _, d := range []string{"game", "exec", "data", "node", "lib"} {
		p := filepath.Join(root, d)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		dirs[d] = p
	}
	sc := &SessionContext{
		OutputMode:       ansi.OutputModeCP437,
		UserID:           5,
		UserHandle:       "Tester",
		UserRealName:     "Test User",
		AccessLevel:      60,
		TimeLimit:        30,
		TimesCalled:      9,
		Location:         "Testville",
		ScreenWidth:      80,
		ScreenHeight:     24,
		NodeNumber:       2,
		SessionStartTime: time.Now(),
		BoardName:        "My Test Board",
		SysOpName:        "Sysop",
	}
	if o.session != nil {
		o.session(sc)
	}
	sess := newInterruptibleSession(o.input)
	for _, c := range o.inputChunks {
		sess.chunks = append(sess.chunks, []byte(c))
	}
	sc.Session = sess
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	eng := NewEngine(ctx, sc, SyncJSDoorConfig{
		Script:       "test.js",
		WorkingDir:   dirs["game"],
		ExecDir:      dirs["exec"],
		DataDir:      dirs["data"],
		NodeDir:      dirs["node"],
		LibraryPaths: []string{dirs["lib"]},
		Args:         o.args,
	})
	h := &doorHarness{t: t, root: root, game: dirs["game"], sess: sess, eng: eng, sc: sc}
	t.Cleanup(h.close)
	return h
}

// disconnect makes blocked session reads return EOF. Idempotent.
func (h *doorHarness) disconnect() { h.hangup.Do(h.sess.closeInterrupt) }

// close runs the door handler's shutdown sequence (interrupt, then Close),
// which fires js.on_exit handlers. Idempotent.
func (h *doorHarness) close() {
	h.closed.Do(func() {
		h.disconnect()
		h.eng.Close()
	})
}

// run writes src to game/test.js and executes it via Engine.Run.
func (h *doorHarness) run(src string) error {
	h.t.Helper()
	h.write("game/test.js", src)
	return h.eng.Run("test.js")
}

// mustRun is run that fails the test on any script error.
func (h *doorHarness) mustRun(src string) {
	h.t.Helper()
	if err := h.run(src); err != nil {
		h.t.Fatalf("script failed: %v\nsource:\n%s", err, src)
	}
}

// eval evaluates a JS expression in the engine's VM.
func (h *doorHarness) eval(expr string) goja.Value {
	h.t.Helper()
	v, err := h.eng.vm.RunString(expr)
	if err != nil {
		h.t.Fatalf("eval %q: %v", expr, err)
	}
	return v
}

// evalErr evaluates expr, requires it to throw, and returns the message.
func (h *doorHarness) evalErr(expr string) string {
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
func (h *doorHarness) output() string {
	h.sess.mu.Lock()
	defer h.sess.mu.Unlock()
	return string(h.sess.out)
}

// resetOutput discards captured output.
func (h *doorHarness) resetOutput() {
	h.sess.mu.Lock()
	h.sess.out = nil
	h.sess.mu.Unlock()
}

// write creates a root-relative file (and parents) and returns its path.
func (h *doorHarness) write(rel, content string) string {
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

// path returns root-relative rel as an absolute, JS-literal-safe path.
func (h *doorHarness) path(rel string) string {
	return filepath.ToSlash(filepath.Join(h.root, rel))
}

// evalCancelled evaluates expr on an engine whose context is already done.
// Cancellation interrupts the VM asynchronously, so the pending interrupt is
// cleared and the evaluation retried until it runs to completion.
func (h *doorHarness) evalCancelled(expr string) goja.Value {
	h.t.Helper()
	select {
	case <-h.eng.ctx.Done():
	case <-time.After(2 * time.Second):
		h.t.Fatal("evalCancelled: engine context not done")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.eng.vm.ClearInterrupt()
		v, err := h.eng.vm.RunString(expr)
		if err == nil {
			return v
		}
		var ie *goja.InterruptedError
		if !errors.As(err, &ie) || time.Now().After(deadline) {
			h.t.Fatalf("evalCancelled %q: %v", expr, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readFile returns the file's content, failing the test if unreadable.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
