package sshserver

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// writeTestHostKey generates an ed25519 OpenSSH private key and writes it to a
// temp file, returning the path.
func writeTestHostKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "host_key")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

func TestNewServer_MissingHostKey(t *testing.T) {
	_, err := NewServer(Config{HostKeyPath: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Fatal("expected error for missing host key")
	}
}

func TestNewServer_InvalidHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(path, []byte("not a key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewServer(Config{HostKeyPath: path}); err == nil {
		t.Fatal("expected parse error for invalid host key")
	}
}

func TestNewServer_Success(t *testing.T) {
	srv, err := NewServer(Config{
		HostKeyPath: writeTestHostKey(t),
		Host:        "127.0.0.1",
		Port:        2222,
		Version:     "TestBanner",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if srv == nil || srv.inner == nil {
		t.Fatal("expected a configured server")
	}
	if srv.inner.Addr != "127.0.0.1:2222" {
		t.Errorf("Addr = %q, want 127.0.0.1:2222", srv.inner.Addr)
	}
	if srv.inner.Version != "TestBanner" {
		t.Errorf("Version = %q, want TestBanner", srv.inner.Version)
	}
}

func TestNewServer_LegacyAlgorithms(t *testing.T) {
	keyPath := writeTestHostKey(t)

	// Legacy off: callback returns a config without the retro cipher list.
	srv, err := NewServer(Config{HostKeyPath: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	sc := srv.inner.ServerConfigCallback(nil)
	if containsStr(sc.Config.Ciphers, "3des-cbc") {
		t.Error("non-legacy config should not enable 3des-cbc")
	}

	// Legacy on: the retro algorithms are present for old BBS clients.
	srvLegacy, err := NewServer(Config{HostKeyPath: keyPath, LegacySSHAlgorithms: true})
	if err != nil {
		t.Fatal(err)
	}
	scLegacy := srvLegacy.inner.ServerConfigCallback(nil)
	if !containsStr(scLegacy.Config.Ciphers, "3des-cbc") {
		t.Error("legacy config should enable 3des-cbc")
	}
	if !containsStr(scLegacy.Config.KeyExchanges, "diffie-hellman-group1-sha1") {
		t.Error("legacy config should enable diffie-hellman-group1-sha1")
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestBBSSession_TransferActiveFlag(t *testing.T) {
	bs := &BBSSession{}
	if bs.IsTransferActive() {
		t.Error("transfer should be inactive by default")
	}
	bs.SetTransferActive(true)
	if !bs.IsTransferActive() {
		t.Error("transfer should be active after SetTransferActive(true)")
	}
	bs.SetTransferActive(false)
	if bs.IsTransferActive() {
		t.Error("transfer should be inactive after SetTransferActive(false)")
	}
}

// mockSession is a minimal ssh.Session whose only real method is Write.
type mockSession struct {
	ssh.Session
	buf bytes.Buffer
}

func (m *mockSession) Write(p []byte) (int, error) { return m.buf.Write(p) }

func TestExtractRawChannel_NilForNonGliderlabs(t *testing.T) {
	if extractRawChannel(nil) != nil {
		t.Error("extractRawChannel(nil) should be nil")
	}
	if extractRawChannel(&mockSession{}) != nil {
		t.Error("extractRawChannel of a session without a Channel field should be nil")
	}
}

func TestWrapSession_RawWriteFallsBackToSession(t *testing.T) {
	m := &mockSession{}
	bs := WrapSession(m)
	if bs.Session != m {
		t.Fatal("WrapSession should embed the provided session")
	}
	// No raw channel on the mock, so RawWrite falls back to Session.Write.
	if _, err := bs.RawWrite([]byte("binary")); err != nil {
		t.Fatalf("RawWrite: %v", err)
	}
	if m.buf.String() != "binary" {
		t.Errorf("RawWrite fallback wrote %q, want %q", m.buf.String(), "binary")
	}
}
