package bbsregression

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolsDriveTerminal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("\x1b[2J\x1b[HTest login: "))
		buf := make([]byte, 80)
		n, _ := conn.Read(buf)
		_, _ = conn.Write([]byte("\r\nWelcome, " + strings.TrimSpace(string(buf[:n])) + "\r\nMain Menu"))
		time.Sleep(400 * time.Millisecond)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	srv, err := NewServer(Profile{Host: "127.0.0.1", Port: port, Protocol: "telnet", Encoding: "utf8"})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client, err := mcp.NewClient(&mcp.Implementation{Name: "bbsregression-test"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	call := func(name string, args map[string]any) Snapshot {
		t.Helper()
		res, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s: %s", name, res.Content[0].(*mcp.TextContent).Text)
		}
		var got Snapshot
		if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &got); err != nil {
			t.Fatalf("%s response: %v", name, err)
		}
		return got
	}
	connected := call("connect", map[string]any{})
	if connected.Session == "" || !connected.Connected || !strings.Contains(connected.Text, "Test login:") {
		t.Fatalf("connect: %+v", connected)
	}
	badKey, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "send", Arguments: map[string]any{"session": connected.Session, "input": "{bogus}"}})
	if err != nil || !badKey.IsError {
		t.Fatalf("unknown key should be rejected: result=%+v err=%v", badKey, err)
	}
	afterSend := call("send", map[string]any{"session": connected.Session, "input": "Coder{enter}"})
	if !strings.Contains(afterSend.Text, "Welcome, Coder") {
		t.Fatalf("send: %+v", afterSend)
	}
	waited := call("wait", map[string]any{"session": connected.Session, "until": "Main Menu", "timeout_s": 2})
	if waited.Matched == nil || !*waited.Matched {
		t.Fatalf("wait: %+v", waited)
	}
	closed := call("disconnect", map[string]any{"session": connected.Session})
	if closed.Connected {
		t.Fatalf("disconnect: %+v", closed)
	}
}

func TestMCPWaitTimeoutAndInvalidRequests(t *testing.T) {
	client := newIdleMCPClient(t)
	connected := callSnapshot(t, client, "connect", map[string]any{})
	t.Cleanup(func() {
		_, _ = client.CallTool(context.Background(), &mcp.CallToolParams{Name: "disconnect", Arguments: map[string]any{"session": connected.Session}})
		_ = client.Close()
	})

	unknown, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "screen", Arguments: map[string]any{"session": "missing"}})
	if err != nil || !unknown.IsError {
		t.Fatalf("unknown session should fail: result=%+v err=%v", unknown, err)
	}

	badPattern, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "wait", Arguments: map[string]any{"session": connected.Session, "until": "[", "timeout_s": 1}})
	if err != nil || !badPattern.IsError {
		t.Fatalf("invalid regular expression should fail: result=%+v err=%v", badPattern, err)
	}

	timedOut := callSnapshot(t, client, "wait", map[string]any{"session": connected.Session, "until": "never-on-screen", "timeout_s": 1})
	if timedOut.Matched == nil || *timedOut.Matched {
		t.Fatalf("expected non-match after timeout: %+v", timedOut)
	}

	idle := callSnapshot(t, client, "wait", map[string]any{"session": connected.Session, "until": "idle", "timeout_s": 2})
	if idle.Matched == nil || !*idle.Matched {
		t.Fatalf("idle wait did not match: %+v", idle)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	canceled, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "wait", Arguments: map[string]any{"session": connected.Session, "until": "still-not-on-screen", "timeout_s": 20}})
	if err == nil || canceled != nil {
		t.Fatalf("canceled wait should end with a canceled request: result=%+v err=%v", canceled, err)
	}
}

func TestMCPAllowsOnlyOneLiveSession(t *testing.T) {
	client := newIdleMCPClient(t)
	connected := callSnapshot(t, client, "connect", map[string]any{})
	defer client.Close()

	second, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "connect", Arguments: map[string]any{}})
	if err != nil || !second.IsError {
		t.Fatalf("second connect should be rejected: result=%+v err=%v", second, err)
	}

	closed, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "disconnect", Arguments: map[string]any{"session": connected.Session}})
	if err != nil || closed.IsError {
		t.Fatalf("disconnect: result=%+v err=%v", closed, err)
	}
}

func newIdleMCPClient(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("\x1b[2J\x1b[HReady for commands"))
		_, _ = io.Copy(io.Discard, conn)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	server, err := NewServer(Profile{Host: "127.0.0.1", Port: port, Protocol: "telnet", Encoding: "utf8"})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client, err := mcp.NewClient(&mcp.Implementation{Name: "bbsregression-test"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func callSnapshot(t *testing.T, client *mcp.ClientSession, name string, args map[string]any) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("%s: %s", name, result.Content[0].(*mcp.TextContent).Text)
	}
	var got Snapshot
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatalf("%s response: %v", name, err)
	}
	return got
}
