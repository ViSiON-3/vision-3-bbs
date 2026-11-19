package syncjs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Go panic inside a host function ends the door's script with an error
// instead of taking the session (and the board) down.
func TestHostFunctionPanicIsAScriptError(t *testing.T) {
	sess := newInterruptibleSession("")
	eng := newLifecycleEngine(sess)
	t.Cleanup(func() { sess.closeInterrupt(); eng.Close() })
	if err := eng.vm.Set("boom", func() { panic("host broke") }); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "door.js")
	if err := os.WriteFile(path, []byte(`console.write("\x1b[?25l"); boom();`), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Run let the panic through: %v", r)
			}
		}()
		err = eng.Run(path)
	}()
	if err == nil || !strings.Contains(err.Error(), "host broke") || !errors.Is(err, ErrHostPanic) {
		t.Fatalf("Run = %v, want an ErrHostPanic script error naming the panic", err)
	}
	sess.mu.Lock()
	out := string(sess.out)
	sess.mu.Unlock()
	if !strings.HasSuffix(out, "\x1b[0m\x1b[?25h") {
		t.Errorf("output does not end by showing the cursor: %q", out)
	}
}
