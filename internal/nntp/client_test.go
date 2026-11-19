package nntp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{Server: "example.invalid:563", TLS: true, MaxArticles: 25, InitialArticles: 25, TimeoutSeconds: 2, MaxRunSeconds: 10, MaxArticleBytes: 4096}
}

func TestClientRejectsUntrustedTLSCertificate(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	c := testConfig()
	c.Server = strings.TrimPrefix(server.URL, "https://")
	if client, err := Dial(context.Background(), c); err == nil {
		client.Close()
		t.Fatal("accepted an untrusted TLS certificate")
	}
}
func fakeServer(t *testing.T, respond func(string) string) Config {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, e := ln.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprint(conn, "201 read only\r\n")
		r := bufio.NewScanner(conn)
		for r.Scan() {
			line := r.Text()
			if line == "MODE READER" {
				fmt.Fprint(conn, "201 reader\r\n")
				continue
			}
			out := respond(line)
			if out == "" {
				return
			}
			fmt.Fprint(conn, out)
		}
	}()
	c := testConfig()
	c.Server = ln.Addr().String()
	c.TLS = false
	return c
}
func TestClientOverviewArticleAndDotStuffing(t *testing.T) {
	c := fakeServer(t, func(cmd string) string {
		switch cmd {
		case "GROUP comp.sys.test":
			return "211 2 1 3 comp.sys.test\r\n"
		case "OVER 1-3":
			return "500 use XOVER\r\n"
		case "XOVER 1-3":
			return "224 overview\r\n3\tthird\tx\tdate\t<c@example.test>\t\t100\t3\r\n1\tfirst\tx\tdate\t<a@example.test>\t\t100\t3\r\n.\r\n"
		case "ARTICLE 1":
			return "220 article\r\nMessage-ID: <a@example.test>\r\n\r\n..dot\r\n.\r\n"
		case "ARTICLE 2":
			return "423 expired\r\n"
		default:
			return ""
		}
	})
	cl, err := Dial(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	low, high, err := cl.Group("comp.sys.test")
	if err != nil || low != 1 || high != 3 {
		t.Fatalf("%d %d %v", low, high, err)
	}
	rows, err := cl.Overview(low, high)
	if err != nil || len(rows) != 2 || rows[0].Number != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	raw, err := cl.Article(1)
	if err != nil || !strings.Contains(string(raw), "\r\n.dot\r\n") {
		t.Fatalf("article=%q err=%v", raw, err)
	}
	if _, err = cl.Article(2); err != ErrMissing {
		t.Fatalf("expired error=%v", err)
	}
	if _, _, err = cl.Group("test\r\nPOST"); err == nil {
		t.Fatal("accepted command injection")
	}
}
func TestClientBoundedResponses(t *testing.T) {
	c := fakeServer(t, func(string) string { return "220 article\r\n" + strings.Repeat("x", 5000) + "\r\n.\r\n" })
	cl, err := Dial(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if _, err = cl.Article(1); err == nil {
		t.Fatal("accepted oversized article")
	}
}
func TestClientCancellationClosesRead(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	accepted := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := ln.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		io.Copy(io.Discard, conn)
	}()
	c := testConfig()
	c.Server = ln.Addr().String()
	c.TLS = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := Dial(ctx, c); result <- err }()
	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was not accepted")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled connection succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled dial did not return")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not close socket")
	}
}
