package transfer_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/sshserver"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/gliderlabs/ssh"
)

type handoffSession struct {
	ssh.Session
	input   *io.PipeReader
	started chan struct{}
}

func (s *handoffSession) Read(p []byte) (int, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	return s.input.Read(p)
}
func (s *handoffSession) Write(p []byte) (int, error) { return len(p), nil }

// The helper exits only after the parent observes the transfer's blocked
// session read. This reproduces cleanup with no more protocol bytes arriving.
func TestTransferExitHelper(t *testing.T) {
	if os.Getenv("VISION3_TRANSFER_EXIT_HELPER") != "1" {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(os.Getenv("VISION3_TRANSFER_EXIT_FILE")); err == nil {
			os.Exit(0)
		}
		time.Sleep(5 * time.Millisecond)
	}
	os.Exit(2)
}

func TestSSHTransferReturnsWithoutConsumingNextEnter(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	raw := &handoffSession{input: reader, started: make(chan struct{}, 1)}
	session := sshserver.WrapSession(raw)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(t.TempDir(), "exit")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestTransferExitHelper$")
	cmd.Env = append(os.Environ(), "VISION3_TRANSFER_EXIT_HELPER=1", "VISION3_TRANSFER_EXIT_FILE="+release)
	done := make(chan error, 1)
	go func() { done <- transfer.RunCommandDirect(ctx, session, cmd, 0) }()
	select {
	case <-raw.started:
	case <-time.After(5 * time.Second):
		t.Fatal("transfer did not start reading")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("transfer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("transfer cleanup waited for another keypress")
	}
	// A single Enter must reach the menu after transfer cleanup and draining.
	key := make(chan byte, 1)
	go func() {
		var b [1]byte
		n, err := session.Read(b[:])
		if n == 1 && err == nil {
			key <- b[0]
		}
	}()
	sent := make(chan error, 1)
	go func() { _, err := writer.Write([]byte{'\r'}); sent <- err }()
	select {
	case b := <-key:
		if b != '\r' {
			t.Fatalf("got key %q", b)
		}
	case <-time.After(time.Second):
		t.Fatal("first Enter was consumed by transfer cleanup")
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
}
