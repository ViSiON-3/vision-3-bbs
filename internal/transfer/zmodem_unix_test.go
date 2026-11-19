//go:build !windows

package transfer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
)

// --- RunCommandWithPTY ---

// waitForWrite blocks until the bytes passed to Write contain want.
func (s *fakeSession) waitForWrite(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(helperTimeout)
	for !strings.Contains(s.written(), want) {
		select {
		case <-s.wrote:
		case <-deadline:
			t.Fatalf("timed out waiting for %q in session output %q", want, s.written())
		}
	}
}

func TestRunCommandWithPTY_relaysIO(t *testing.T) {
	s := newRawSession()
	s.hasPty = true
	s.winCh = make(chan ssh.Window)
	defer close(s.winCh)
	cmd := helperCommand(t, "pty")
	done := make(chan error, 1)
	go func() { done <- RunCommandWithPTY(nilCtx, s, cmd, 0) }()

	// PTY output goes through Write, never RawWrite.
	s.waitForWrite(t, "READY")
	s.in <- []byte("k") // helper reports the key and that it has a terminal
	s.waitForWrite(t, "key=k tty=true;")
	s.in <- []byte("k") // helper exits

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunCommandWithPTY: %v", err)
		}
	case <-time.After(helperTimeout):
		t.Fatal("RunCommandWithPTY did not return")
	}
	if got := s.rawWritten(); got != "" {
		t.Errorf("PTY mode used RawWrite: %q", got)
	}
	if !s.interruptCleared() {
		t.Error("read interrupt left armed after PTY command")
	}
}

// TestRunCommandWithPTY_windowChanges resizes the PTY throughout a command,
// including while it shuts down, without ever closing winCh. Run under -race
// it catches a Setsize racing the final ptmx.Close; the resize goroutine must
// also have stopped listening on winCh by the time the call returns.
func TestRunCommandWithPTY_windowChanges(t *testing.T) {
	s := newRawSession()
	s.hasPty = true
	s.window = ssh.Window{Width: 80, Height: 25}
	s.winCh = make(chan ssh.Window) // never closed, like a live session's
	cmd := helperCommand(t, "pty")
	done := make(chan error, 1)
	go func() { done <- RunCommandWithPTY(nilCtx, s, cmd, 0) }()

	// Keep resizing until the call returns.
	stopResizing := make(chan struct{})
	resizerDone := make(chan int, 1)
	go func() {
		sent := 0
		for {
			select {
			case s.winCh <- ssh.Window{Width: 80 + sent%40, Height: 25 + sent%10}:
				sent++
			case <-stopResizing:
				resizerDone <- sent
				return
			}
		}
	}()

	s.waitForWrite(t, "READY")
	s.in <- []byte("k")
	s.waitForWrite(t, "key=k tty=true;")
	s.in <- []byte("k") // helper exits while resizes are still arriving

	var err error
	select {
	case err = <-done:
	case <-time.After(helperTimeout):
		t.Fatal("RunCommandWithPTY did not return")
	}
	close(stopResizing)
	if sent := <-resizerDone; sent == 0 {
		t.Error("no window changes were delivered during the command")
	}
	if err != nil {
		t.Fatalf("RunCommandWithPTY: %v", err)
	}

	// Nothing may still be receiving from winCh once the call has returned.
	select {
	case s.winCh <- ssh.Window{Width: 1, Height: 1}:
		t.Error("resize goroutine still receiving window changes after return")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunCommandWithPTY_contextCancel(t *testing.T) {
	s := newRawSession()
	s.hasPty = true
	s.winCh = make(chan ssh.Window)
	defer close(s.winCh)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := helperCommand(t, "block")
	done := make(chan error, 1)
	go func() { done <- RunCommandWithPTY(ctx, s, cmd, 0) }()

	s.waitForWrite(t, "READY")
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(helperTimeout):
		t.Fatal("cancellation did not end the PTY command")
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() == 4 {
		t.Errorf("process state = %v, want killed process", cmd.ProcessState)
	}
}

func TestRunCommandWithPTY_startError(t *testing.T) {
	s := newRawSession()
	s.hasPty = true
	cmd := exec.Command(filepath.Join(t.TempDir(), "no-such-binary"))

	err := RunCommandWithPTY(context.Background(), s, cmd, 0)
	if err == nil || !strings.Contains(err.Error(), "failed to start pty") {
		t.Fatalf("err = %v, want pty start failure", err)
	}
}

// --- ExecuteZmodemSend / ExecuteZmodemReceive ---

// fakeLrzsz puts shell-script stand-ins for sz and rz first (and alone) on
// PATH. Each prints its name, arguments and working directory, then exits
// with the given status.
func fakeLrzsz(t *testing.T, exitCode string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh available for fake sz/rz")
	}
	dir := t.TempDir()
	for _, name := range []string{"sz", "rz"} {
		script := "#!" + sh + "\nprintf '%s' \"" + name + ":$*:\"\npwd -P\nexit " + exitCode + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestExecuteZmodemSend(t *testing.T) {
	s := newRawSession()
	if err := ExecuteZmodemSend(context.Background(), s); err == nil || !strings.Contains(err.Error(), "no files provided") {
		t.Fatalf("no files: err = %v", err)
	}

	t.Run("sz missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		err := ExecuteZmodemSend(context.Background(), s, "/files/a.zip")
		if err == nil || !strings.Contains(err.Error(), "'sz' command not found") {
			t.Fatalf("err = %v, want sz not found", err)
		}
	})

	t.Run("runs sz in binary mode", func(t *testing.T) {
		fakeLrzsz(t, "0")
		s := newRawSession()
		if err := ExecuteZmodemSend(nilCtx, s, "/files/a.zip", "/files/b.zip"); err != nil {
			t.Fatalf("ExecuteZmodemSend: %v", err)
		}
		if got, want := s.rawWritten(), "sz:-b /files/a.zip /files/b.zip:"; !strings.HasPrefix(got, want) {
			t.Errorf("sz saw %q, want prefix %q", got, want)
		}
	})

	t.Run("sz failure is wrapped", func(t *testing.T) {
		fakeLrzsz(t, "2")
		s := newRawSession()
		err := ExecuteZmodemSend(context.Background(), s, "/files/a.zip")
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 || !strings.Contains(err.Error(), "zmodem send failed") {
			t.Fatalf("err = %v, want wrapped exit status 2", err)
		}
		if got := s.written(); got != zmodemAbortSeq {
			t.Errorf("abort sequence = %q, want %q", got, zmodemAbortSeq)
		}
	})
}

func TestExecuteZmodemReceive(t *testing.T) {
	s := newRawSession()
	if err := ExecuteZmodemReceive(context.Background(), s, ""); err == nil || !strings.Contains(err.Error(), "cannot be empty") {
		t.Fatalf("empty dir: err = %v", err)
	}

	t.Run("target is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		err := ExecuteZmodemReceive(context.Background(), s, file)
		if err == nil || !strings.Contains(err.Error(), "failed to create or access target directory") {
			t.Fatalf("err = %v, want directory failure", err)
		}
	})

	t.Run("rz missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		err := ExecuteZmodemReceive(context.Background(), s, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "'rz' command not found") {
			t.Fatalf("err = %v, want rz not found", err)
		}
	})

	t.Run("creates target and runs rz there", func(t *testing.T) {
		fakeLrzsz(t, "0")
		base, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(base, "incoming", "node1")
		s := newRawSession()
		if err := ExecuteZmodemReceive(nilCtx, s, dir); err != nil {
			t.Fatalf("ExecuteZmodemReceive: %v", err)
		}
		// Binary, restricted mode, run from inside the upload directory.
		if got, want := s.rawWritten(), "rz:-b -r:"+dir+"\n"; got != want {
			t.Errorf("rz saw %q, want %q", got, want)
		}
	})

	t.Run("rz failure is wrapped", func(t *testing.T) {
		fakeLrzsz(t, "1")
		err := ExecuteZmodemReceive(context.Background(), newRawSession(), t.TempDir())
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || !strings.Contains(err.Error(), "zmodem receive failed") {
			t.Fatalf("err = %v, want wrapped exit error", err)
		}
	})
}
