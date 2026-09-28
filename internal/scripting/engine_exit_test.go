package scripting

import (
	"os"
	"path/filepath"
	"testing"
)

// runScriptFile writes src to a script in the engine's working directory
// and executes it through Engine.Run, the path the door handler uses.
func runScriptFile(t *testing.T, eng *Engine, src string) error {
	t.Helper()
	path := filepath.Join(eng.cfg.WorkingDir, "test.js")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return eng.Run(path)
}

// TestExitIsCleanFromRun is the regression test for #458: exit() must end
// the script and make Run return nil rather than "script error".
func TestExitIsCleanFromRun(t *testing.T) {
	for _, tc := range []struct{ name, call string }{
		{"no code", "exit()"},
		{"zero", "exit(0)"},
		{"non-zero", "exit(3)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, _, _ := newSandboxTestEngine(t)
			err := runScriptFile(t, eng, `
				var before = true;
				`+tc.call+`;
				globalThis.after = true;
				throw new Error("script kept running after exit()");
			`)
			if err != nil {
				t.Fatalf("Run = %v; want nil", err)
			}
			if eng.vm.Get("after") != nil {
				t.Fatal("statements after exit() ran")
			}
		})
	}
}

// TestExitCannotBeCaught checks that try/catch/finally cannot swallow exit(),
// including when exit() is called from a nested function.
func TestExitCannotBeCaught(t *testing.T) {
	eng, _, _ := newSandboxTestEngine(t)
	err := runScriptFile(t, eng, `
		function quit() { exit(0); }
		try {
			quit();
			globalThis.afterCall = true;
		} catch (e) {
			globalThis.caught = true;
		}
		globalThis.afterTry = true;
		throw new Error("script kept running after exit()");
	`)
	if err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
	for _, name := range []string{"afterCall", "caught", "afterTry"} {
		if eng.vm.Get(name) != nil {
			t.Errorf("%s was set: exit() did not stop the script", name)
		}
	}
}

// TestScriptErrorStillReported guards against exitStatus treating ordinary
// exceptions as clean exits.
func TestScriptErrorStillReported(t *testing.T) {
	eng, _, _ := newSandboxTestEngine(t)
	if err := runScriptFile(t, eng, `throw new Error("boom")`); err == nil {
		t.Fatal("Run = nil; want script error")
	}
}
