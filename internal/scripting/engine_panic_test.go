package scripting

import (
	"errors"
	"strings"
	"testing"
)

// A Go panic inside a host function ends the script with an error instead of
// taking the session (and the board) down, and leaves the caller's terminal
// with its cursor shown and colours reset.
func TestHostFunctionPanicIsAScriptError(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	if err := h.eng.vm.Set("boom", func() { panic("host broke") }); err != nil {
		t.Fatal(err)
	}
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Run let the panic through: %v", r)
			}
		}()
		err = h.run(`v3.console.write("\x1b[?25l"); try { boom(); } catch (e) { v3.console.write("caught"); } v3.console.write("after");`)
	}()
	if err == nil || !strings.Contains(err.Error(), "host broke") || !errors.Is(err, ErrHostPanic) {
		t.Fatalf("Run = %v, want an ErrHostPanic script error naming the panic", err)
	}
	out := h.output()
	if strings.Contains(out, "after") || strings.Contains(out, "caught") {
		t.Errorf("script went on after the panic: %q", out)
	}
	if !strings.HasSuffix(out, "\x1b[0m\x1b[?25h") {
		t.Errorf("output does not end by showing the cursor: %q", out)
	}
}
