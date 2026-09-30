//go:build !windows

package menu

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A login door is only run when it stats cleanly and is an executable
// regular file; anything else is refused before exec (#530).
func TestLoginDoorUnrunnable(t *testing.T) {
	dir := t.TempDir()
	runnable := doorcovScript(t, "exit 0")
	plain := filepath.Join(dir, "plain.sh")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "door.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if got := loginDoorUnrunnable(runnable); got != "" {
		t.Errorf("executable script refused: %q", got)
	}
	cases := map[string]string{
		"missing":        filepath.Join(dir, "missing.sh"),
		"directory":      dir,
		"not executable": plain,
	}
	// root walks through a mode-000 directory, so only a normal user sees
	// the permission error.
	if os.Geteuid() != 0 {
		cases["permission denied"] = filepath.Join(locked, "door.sh")
	}
	for name, path := range cases {
		if got := loginDoorUnrunnable(path); got == "" {
			t.Errorf("%s: %s was accepted", name, path)
		}
	}
}

// A login door gets the caller's keystrokes while it runs and none after:
// runLoginDoor returns as soon as the script exits, without waiting for a
// key, and the next key is still there for whatever reads the session next
// (#530).
func TestRunLoginDoor_StdinStopsAtExit(t *testing.T) {
	env := newMenuEnv(t)

	// The script reads a line the caller types.
	s := newDoorcovSession()
	s.whenOutput("READY", func() { s.send("hello\n") })
	r := doorcovRun(env, s, runLoginDoor, env.caller, doorcovScript(t, `echo READY; read line; echo "GOT=$line"`))
	if r.err != nil || r.user != env.caller || !r.has("GOT=hello") {
		t.Fatalf("user=%s err=%v output:\n%s", execcovHandle(r.user), r.err, r.text())
	}

	// One that reads nothing returns without a key, and leaves the next one.
	s = newDoorcovSession()
	r = doorcovRun(env, s, runLoginDoor, env.caller, doorcovScript(t, "echo QUICK-DOOR"))
	if r.err != nil || !r.has("QUICK-DOOR") {
		t.Fatalf("err=%v output:\n%s", r.err, r.text())
	}
	s.send("x")
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 8)
		n, _ := s.Read(buf)
		got <- string(buf[:n])
	}()
	select {
	case k := <-got:
		if k != "x" {
			t.Errorf("next read = %q, want the key typed after the door", k)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the key typed after the door never reached the session's next reader")
	}
}
