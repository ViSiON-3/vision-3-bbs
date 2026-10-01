package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/sshserver"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

func newTestSigner(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// unsignedSigner offers a real public key but refuses to sign, so the server
// sees the key in a query and the client never proves ownership. The client
// then falls back to its next auth method.
type unsignedSigner struct{ gossh.Signer }

func (unsignedSigner) Sign(io.Reader, []byte) (*gossh.Signature, error) {
	return nil, errors.New("no private key")
}

// startWFCTestServer runs a real sshserver with the production auth handlers
// and WFC subsystems. It registers one admin whose key is adminSigner and one
// caller on node 4 (started at the returned time).
func startWFCTestServer(t *testing.T, adminSigner gossh.Signer) (addr string, tap *snoop.Tap, start time.Time) {
	t.Helper()
	keyLine := string(gossh.MarshalAuthorizedKey(adminSigner.PublicKey()))
	oldUM, oldMin, oldEn, oldReg := userMgr, adminMinLevel, wfcEnabled, sessionRegistry
	userMgr = user.NewUserMgrForTest(&user.User{Handle: "boss", AccessLevel: 255, PublicKeys: []string{keyLine}})
	adminMinLevel = func() int { return 250 }
	wfcEnabled = func() bool { return true }
	start = time.Unix(100, 0)
	sessionRegistry = session.NewSessionRegistry()
	tap = snoop.NewTap()
	sessionRegistry.Register(&session.BbsSession{NodeID: 4, StartTime: start, Width: 80, Height: 25, Tap: tap,
		User: &user.User{Handle: "caller"}})
	t.Cleanup(func() { userMgr, adminMinLevel, wfcEnabled, sessionRegistry = oldUM, oldMin, oldEn, oldReg })

	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	blk, err := gossh.MarshalPrivateKey(hostPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	hostKey := filepath.Join(t.TempDir(), "host_key")
	if err := os.WriteFile(hostKey, pem.EncodeToMemory(blk), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	srv, err := sshserver.NewServer(sshserver.Config{
		HostKeyPath:                hostKey,
		Host:                       "127.0.0.1",
		Port:                       port,
		SessionHandler:             func(ssh.Session) {},
		PasswordHandler:            sshPasswordHandler,
		KeyboardInteractiveHandler: sshKeyboardInteractiveHandler,
		PublicKeyHandler:           wfcPublicKeyHandler,
		SubsystemHandlers: map[string]func(ssh.Session){
			"wfc-admin": wfcAdminSubsystem,
			"wfc-snoop": wfcSnoopSubsystem,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Close() })
	addr = fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return addr, tap, start
}

func dialWFC(t *testing.T, addr string, auth ...gossh.AuthMethod) *gossh.Client {
	t.Helper()
	c, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User: "visitor", Auth: auth, HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

type subsysRWC struct {
	io.Reader
	io.WriteCloser
	sess *gossh.Session
}

func (s subsysRWC) Close() error {
	_ = s.WriteCloser.Close()
	return s.sess.Close()
}

func openSubsystem(t *testing.T, c *gossh.Client, name string) io.ReadWriteCloser {
	t.Helper()
	sess, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	in, _ := sess.StdinPipe()
	out, _ := sess.StdoutPipe()
	if err := sess.RequestSubsystem(name); err != nil {
		t.Fatalf("subsystem %s: %v", name, err)
	}
	return subsysRWC{Reader: out, WriteCloser: in, sess: sess}
}

func snoopOpen(t *testing.T, c *gossh.Client, node int, at time.Time) (*admin.SnoopStream, error) {
	t.Helper()
	return admin.ClientSnoop(openSubsystem(t, c, "wfc-snoop"), admin.SnoopRequest{NodeID: node, ConnectedAt: at})
}

// adminDenied reports whether wfc-admin refused the channel.
func adminDenied(t *testing.T, c *gossh.Client) bool {
	t.Helper()
	rwc := openSubsystem(t, c, "wfc-admin")
	defer rwc.Close()
	line, _ := bufio.NewReader(rwc).ReadString('\n')
	return strings.Contains(line, "access denied")
}

func TestWFCQueryThenPasswordIsDenied(t *testing.T) {
	adminSigner := newTestSigner(t)
	addr, _, start := startWFCTestServer(t, adminSigner)
	cases := map[string]gossh.AuthMethod{
		"password":             gossh.Password("anything"),
		"keyboard-interactive": gossh.KeyboardInteractive(func(_, _ string, _ []string, _ []bool) ([]string, error) { return nil, nil }),
	}
	for name, fallback := range cases {
		t.Run(name, func(t *testing.T) {
			c := dialWFC(t, addr, gossh.PublicKeys(unsignedSigner{adminSigner}), fallback)
			if _, err := snoopOpen(t, c, 4, start); err == nil || err.Error() != "access denied" {
				t.Fatalf("wfc-snoop err = %v, want access denied", err)
			}
			if !adminDenied(t, c) {
				t.Fatal("wfc-admin opened without proof of the key")
			}
		})
	}
}

func TestWFCSignedKeyOpensSnoop(t *testing.T) {
	adminSigner := newTestSigner(t)
	addr, tap, start := startWFCTestServer(t, adminSigner)
	c := dialWFC(t, addr, gossh.PublicKeys(adminSigner))
	st, err := snoopOpen(t, c, 4, start)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Header.Handle != "caller" || st.Header.Width != 80 {
		t.Fatalf("header %+v", st.Header)
	}
	tap.Output([]byte("x"))
	buf := make([]byte, 8)
	n, err := st.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "x") {
		t.Fatalf("read %q %v", buf[:n], err)
	}
}

func TestWFCNoStashIsDenied(t *testing.T) {
	addr, _, start := startWFCTestServer(t, newTestSigner(t))
	c := dialWFC(t, addr, gossh.Password("anything"))
	if _, err := snoopOpen(t, c, 4, start); err == nil || err.Error() != "access denied" {
		t.Fatalf("wfc-snoop err = %v, want access denied", err)
	}
	if !adminDenied(t, c) {
		t.Fatal("wfc-admin opened with no key")
	}
}

func TestWFCSnoopRefusesReusedNode(t *testing.T) {
	adminSigner := newTestSigner(t)
	addr, _, start := startWFCTestServer(t, adminSigner)
	c := dialWFC(t, addr, gossh.PublicKeys(adminSigner))
	_, err := snoopOpen(t, c, 4, start.Add(time.Second))
	if err == nil || !strings.Contains(err.Error(), "different caller") {
		t.Fatalf("err = %v, want different caller", err)
	}
}
