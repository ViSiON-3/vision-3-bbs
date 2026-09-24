package qwknet

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeFTP is a minimal passive-mode FTP server: one user, an in-memory
// file map, and just the verbs the client uses. It records uploads.
type fakeFTP struct {
	ln       net.Listener
	user     string
	pass     string
	files    map[string][]byte
	mu       sync.Mutex
	uploads  map[string][]byte
	noEPSV   bool
	retrCode int // non-zero overrides the RETR reply for missing files
}

func newFakeFTP(t *testing.T, user, pass string, files map[string][]byte) *fakeFTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeFTP{ln: ln, user: user, pass: pass, files: files, uploads: map[string][]byte{}}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeFTP) addr() string { return s.ln.Addr().String() }

func (s *fakeFTP) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.session(conn)
	}
}

func (s *fakeFTP) session(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	w := func(format string, a ...any) { _, _ = fmt.Fprintf(conn, format+"\r\n", a...) }
	w("220-Welcome to")
	w("220 fake hub")
	r := bufio.NewReader(conn)
	var data net.Listener
	loggedIn := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		switch strings.ToUpper(verb) {
		case "USER":
			if arg == s.user {
				w("331 password please")
			} else {
				w("530 unknown user")
			}
		case "PASS":
			if arg == s.pass {
				loggedIn = true
				w("230 logged in")
			} else {
				w("530 bad password")
			}
		case "TYPE":
			w("200 ok")
		case "EPSV":
			if s.noEPSV {
				w("500 no EPSV")
				continue
			}
			data, _ = net.Listen("tcp", "127.0.0.1:0")
			w("229 Entering Extended Passive Mode (|||%d|)", data.Addr().(*net.TCPAddr).Port)
		case "PASV":
			data, _ = net.Listen("tcp", "127.0.0.1:0")
			p := data.Addr().(*net.TCPAddr).Port
			w("227 Entering Passive Mode (127,0,0,1,%d,%d)", p>>8, p&0xff)
		case "STOR":
			if !loggedIn || data == nil {
				w("503 bad sequence")
				continue
			}
			w("150 go ahead")
			dc, _ := data.Accept()
			_ = data.Close()
			body, _ := io.ReadAll(dc)
			_ = dc.Close()
			s.mu.Lock()
			s.uploads[arg] = body
			s.mu.Unlock()
			w("226 stored")
		case "RETR":
			if !loggedIn || data == nil {
				w("503 bad sequence")
				continue
			}
			body, ok := s.files[arg]
			if !ok {
				_ = data.Close()
				code := 550
				if s.retrCode != 0 {
					code = s.retrCode
				}
				w("%d %s: no such file", code, arg)
				continue
			}
			w("150 sending")
			dc, _ := data.Accept()
			_ = data.Close()
			_, _ = dc.Write(body)
			_ = dc.Close()
			w("226 sent")
		case "QUIT":
			w("221 bye")
			return
		default:
			w("502 not implemented")
		}
	}
}

func TestFTPClient_UploadDownload(t *testing.T) {
	srv := newFakeFTP(t, "NODE", "pw", map[string][]byte{"VERT.QWK": []byte("packet-bytes")})
	ctx := context.Background()
	c, err := ftpDial(ctx, srv.addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.quit()
	if err := c.login("NODE", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := c.store(ctx, "VERT.REP", strings.NewReader("rep-bytes")); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	got := string(srv.uploads["VERT.REP"])
	srv.mu.Unlock()
	if got != "rep-bytes" {
		t.Errorf("upload = %q", got)
	}
	var buf bytes.Buffer
	n, err := c.retrieve(ctx, "VERT.QWK", &buf)
	if err != nil || n != 12 || buf.String() != "packet-bytes" {
		t.Fatalf("retrieve: n=%d err=%v body=%q", n, err, buf.String())
	}
	if _, err := c.retrieve(ctx, "MISSING.QWK", &buf); err != errNoSuchFile {
		t.Errorf("missing file: err=%v want errNoSuchFile", err)
	}
}

func TestFTPClient_PASVFallbackAndBadLogin(t *testing.T) {
	srv := newFakeFTP(t, "NODE", "pw", map[string][]byte{"X": []byte("x")})
	srv.noEPSV = true
	ctx := context.Background()
	c, err := ftpDial(ctx, srv.addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.login("NODE", "wrong"); err == nil {
		t.Fatal("bad password accepted")
	}
	c.quit()

	c, err = ftpDial(ctx, srv.addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.quit()
	if err := c.login("NODE", "pw"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := c.retrieve(ctx, "X", &buf); err != nil || buf.String() != "x" {
		t.Fatalf("PASV retrieve: %v %q", err, buf.String())
	}
}

func TestParsePASVAndEPSV(t *testing.T) {
	if h, p, ok := parsePASV("Entering Passive Mode (10,0,0,5,4,1)."); !ok || h != "10.0.0.5" || p != 1025 {
		t.Errorf("parsePASV = %q %d %v", h, p, ok)
	}
	if _, _, ok := parsePASV("nonsense"); ok {
		t.Error("nonsense accepted")
	}
	if p, ok := parseEPSV("Entering Extended Passive Mode (|||2121|)"); !ok || p != 2121 {
		t.Errorf("parseEPSV = %d %v", p, ok)
	}
}
