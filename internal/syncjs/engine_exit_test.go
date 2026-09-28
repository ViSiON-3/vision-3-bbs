package syncjs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// exitHandlerBody registers an on_exit handler that does enough JS work to
// give a stray interrupt a window to land mid-handler, then records that it
// finished (and what js.terminated reported) by writing a marker.
const exitHandlerBody = `js.on_exit(function() {
	var n = 0;
	for (var i = 0; i < 50; i++) { n += i; }
	console.write("SAVED" + (js.terminated ? "T" : "F"));
});
`

// newScriptEngine builds an engine over sess whose working directory dir
// holds test.js.
func newScriptEngine(dir string, sess *interruptibleSession) *Engine {
	return NewEngine(context.Background(), &SessionContext{
		Session:      sess,
		OutputMode:   ansi.OutputModeCP437,
		ScreenWidth:  80,
		ScreenHeight: 24,
	}, SyncJSDoorConfig{Script: "test.js", WorkingDir: dir, ExecDir: dir, DataDir: dir, NodeDir: dir})
}

// writeScript writes src as test.js in a fresh temp dir and returns the dir.
func writeScript(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (s *interruptibleSession) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.out)
}

// TestExitHandlersSurviveExitCancel is a regression test for #460: exit()
// cancels the engine context, and the context watcher used to deliver
// vm.Interrupt(ErrTerminated) while Close() was already running the on_exit
// handlers, so a game's save/cleanup handler occasionally never finished.
func TestExitHandlersSurviveExitCancel(t *testing.T) {
	dir := writeScript(t, exitHandlerBody+"exit(0);\n")
	const iterations = 200
	failures := 0
	for i := 0; i < iterations; i++ {
		sess := newInterruptibleSession("")
		eng := newScriptEngine(dir, sess)
		if err := eng.Run("test.js"); err != nil {
			t.Fatalf("iteration %d: Run: %v", i, err)
		}
		sess.closeInterrupt()
		eng.Close()
		// exit() cancels the context, so handlers observe js.terminated.
		if !strings.Contains(sess.output(), "SAVEDT") {
			failures++
		}
	}
	if failures > 0 {
		t.Fatalf("on_exit handler did not complete in %d of %d runs", failures, iterations)
	}
}

// TestExitHandlersSurviveInputShutdown is a regression test for #460's second
// trigger: at door end the read interrupt is closed, the input copier hits
// EOF and cancels the engine context, which raced the on_exit handlers run by
// Close().
func TestExitHandlersSurviveInputShutdown(t *testing.T) {
	dir := writeScript(t, exitHandlerBody+"console.getkey();\n")
	const iterations = 200
	failures := 0
	for i := 0; i < iterations; i++ {
		sess := newInterruptibleSession("A")
		eng := newScriptEngine(dir, sess)
		if err := eng.Run("test.js"); err != nil {
			t.Fatalf("iteration %d: Run: %v", i, err)
		}
		sess.closeInterrupt()
		eng.Close()
		if !strings.Contains(sess.output(), "SAVED") {
			failures++
		}
	}
	if failures > 0 {
		t.Fatalf("on_exit handler did not complete in %d of %d runs", failures, iterations)
	}
}

// TestExitHandlersRunBeforeInputShutdown checks the door handler's ordering:
// running the exit handlers before closing the read interrupt means a normal
// door end does not look like a disconnect to the handlers (js.terminated is
// false), while a real disconnect still does.
func TestExitHandlersRunBeforeInputShutdown(t *testing.T) {
	dir := writeScript(t, exitHandlerBody+"console.getkey();\n")

	sess := newInterruptibleSession("A")
	eng := newScriptEngine(dir, sess)
	if err := eng.Run("test.js"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	eng.RunExitHandlers()
	sess.closeInterrupt()
	eng.Close()
	if got := sess.output(); got != "SAVEDF" {
		t.Fatalf("normal door end: handler output %q, want exactly one SAVEDF", got)
	}

	// Disconnect: the session read fails while the script waits for a key.
	sess = newInterruptibleSession("")
	sess.closeInterrupt()
	eng = newScriptEngine(dir, sess)
	if err := eng.Run("test.js"); err != nil {
		t.Fatalf("Run after disconnect: %v", err)
	}
	eng.RunExitHandlers()
	eng.Close()
	if got := sess.output(); got != "SAVEDT" {
		t.Fatalf("disconnect: handler output %q, want SAVEDT", got)
	}
}

// TestExitHandlersBudget verifies a hung exit handler cannot hold the node
// forever: a CPU-bound loop and a blocking key read are both cut off once the
// exit-handler budget expires, and handlers not yet started are skipped.
func TestExitHandlersBudget(t *testing.T) {
	old := exitHandlerBudget
	exitHandlerBudget = 100 * time.Millisecond
	t.Cleanup(func() { exitHandlerBudget = old })

	for name, handler := range map[string]string{
		"cpu":   `while (true) {}`,
		"input": `console.getkey(); while (true) {}`,
	} {
		t.Run(name, func(t *testing.T) {
			// Handlers run in reverse registration order, so the hung one
			// runs first and the marker handler must be skipped.
			dir := writeScript(t, `js.on_exit(function() { console.write("LATE"); });
js.on_exit(function() { `+handler+` });
`)
			sess := newInterruptibleSession("")
			eng := newScriptEngine(dir, sess)
			if err := eng.Run("test.js"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				eng.RunExitHandlers()
				sess.closeInterrupt()
				eng.Close()
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("hung exit handler blocked shutdown past the budget")
			}
			if strings.Contains(sess.output(), "LATE") {
				t.Fatal("exit handler ran after the budget expired")
			}
		})
	}
}
