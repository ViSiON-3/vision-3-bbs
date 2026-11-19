package scripting

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego/safegotest"
)

// A panic on a script's input copier ends that script (as a hang-up would,
// so the menu takes over again) and is logged with the node; the next
// script reads its key.
func TestScriptInputPanicEndsOnlyItsScript(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindScriptInput)

	first := newHarness(t, harnessOpts{input: "x"})
	err := first.run(`v3.console.getkey(); while (true) {}`)
	if !errors.Is(err, ErrDisconnect) {
		t.Errorf("first script: Run = %v, want ErrDisconnect", err)
	}
	if log := logs.WaitPanic(t, safego.KindScriptInput, "scripting/engine.go"); !strings.Contains(log, "node=3") {
		t.Errorf("panic not logged with the node:\n%s", log)
	}

	second := newHarness(t, harnessOpts{input: "y"})
	second.mustRun(`v3.console.write("[" + v3.console.getkey() + "]");`)
	if out := second.output(); out != "[y]" {
		t.Errorf("second script wrote %q, want [y]", out)
	}
}

// A panic on a script's context watcher stops that script, since nothing
// else would stop it at its run time limit; the next script's limit still
// stops a busy loop.
func TestScriptWatchPanicEndsOnlyItsScript(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindScriptWatch)

	first := newHarness(t, harnessOpts{})
	logs.WaitPanic(t, safego.KindScriptWatch, "scripting/engine.go")
	done := make(chan error, 1)
	go func() { done <- first.run(`while (true) {}`) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrDisconnect) {
			t.Errorf("first script: Run = %v, want ErrDisconnect", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first script ran on after its watcher panicked")
	}

	second := newHarness(t, harnessOpts{maxRunTime: 50 * time.Millisecond})
	if err := second.run(`while (true) {}`); !errors.Is(err, ErrTimeout) {
		t.Errorf("second script: Run = %v, want ErrTimeout", err)
	}
}
