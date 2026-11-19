package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego/safegotest"
)

// A panic on a WFC session's authorization watcher closes that session; the
// next WFC session is served.
func TestWFCWatcherPanicEndsOnlyItsSession(t *testing.T) {
	logs := safegotest.Capture(t)
	adminSigner := newTestSigner(t)
	addr, tap, start := startWFCTestServer(t, adminSigner)
	safegotest.Inject(t, safego.KindWFC)

	c := dialWFC(t, addr, gossh.PublicKeys(adminSigner))
	if st, err := snoopOpen(t, c, 4, start); err == nil {
		// The session is closed under the stream.
		done := make(chan error, 1)
		go func() {
			buf := make([]byte, 64)
			for {
				if _, err := st.Read(buf); err != nil {
					done <- err
					return
				}
			}
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the first WFC session stayed open after its watcher panicked")
		}
		st.Close()
	}
	logs.WaitPanic(t, safego.KindWFC, "cmd/vision3/wfc_snoop.go")

	c2 := dialWFC(t, addr, gossh.PublicKeys(adminSigner))
	st2, err := snoopOpen(t, c2, 4, start)
	if err != nil {
		t.Fatalf("second WFC session: %v", err)
	}
	defer st2.Close()
	tap.Output([]byte("x"))
	buf := make([]byte, 8)
	if n, err := st2.Read(buf); err != nil || !strings.Contains(string(buf[:n]), "x") {
		t.Fatalf("second WFC session read %q %v", buf[:n], err)
	}
}

// A panic while writing one lockout to the block list loses that write only;
// the next lockout is written.
func TestBlocklistWritePanicLosesOnlyThatWrite(t *testing.T) {
	logs := safegotest.Capture(t)
	path := filepath.Join(t.TempDir(), "blocklist.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ct := NewConnectionTracker(0, 0, 1, 15, path, "")
	t.Cleanup(ct.StopWatching)
	safegotest.Inject(t, safego.KindBlocklist)

	if !ct.RecordFailedLoginAttempt("192.0.2.10") {
		t.Fatal("first IP not locked out")
	}
	logs.WaitPanic(t, safego.KindBlocklist, "cmd/vision3/main.go")
	if !ct.RecordFailedLoginAttempt("192.0.2.11") {
		t.Fatal("second IP not locked out")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(path)
		if strings.Contains(string(b), "192.0.2.11") {
			if strings.Contains(string(b), "192.0.2.10") {
				t.Errorf("the panicked write was made anyway:\n%s", b)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the second lockout was not written:\n%s", b)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A goroutine that serves a caller's session logs the node the session
// holds, seen through any wrapper around the session.
func TestNodeOfOwnerSeesThroughWrappers(t *testing.T) {
	s := &panicTestSession{addr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 9), Port: 9}}
	activeSessionsMutex.Lock()
	activeSessions[s] = 6
	activeSessionsMutex.Unlock()
	t.Cleanup(func() {
		activeSessionsMutex.Lock()
		delete(activeSessions, s)
		activeSessionsMutex.Unlock()
	})
	if n, ok := nodeOfOwner(s); !ok || n != 6 {
		t.Errorf("session: %d %v, want 6", n, ok)
	}
	if n, ok := nodeOfOwner(unwrapping{s}); !ok || n != 6 {
		t.Errorf("wrapped session: %d %v, want 6", n, ok)
	}
	if _, ok := nodeOfOwner(&panicTestSession{}); ok {
		t.Error("a session holding no node was given one")
	}
	if _, ok := nodeOfOwner("not a session"); ok {
		t.Error("a non-session was given a node")
	}
}

// unwrapping is a wrapper around a session, as a door's or script's is.
type unwrapping struct{ ssh.Session }

func (u unwrapping) Unwrap() ssh.Session { return u.Session }
