// Package bbsregression provides an MCP terminal driver for local BBS regression work.
package bbsregression

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/bbsregression/keys"
	"github.com/ViSiON-3/vision-3-bbs/internal/bbsregression/vt"
	"github.com/ViSiON-3/vision-3-bbs/internal/telnetclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/ssh"
)

// Profile describes one disposable BBS instance for the regression MCP.
type Profile struct {
	Host             string
	Port             int
	Protocol         string // "telnet" or "ssh".
	SSHUser          string
	SSHHostKeySHA256 string
	Terminal         string
	Encoding         string // "cp437" or "utf8".
	Columns, Rows    int
}

// Server owns one configured board and exposes the connect/screen/send/wait/
// disconnect tools over MCP. It deliberately has no persona state, journaling,
// pacing, or password store: this is an operator-controlled test instrument.
type Server struct {
	profile    Profile
	mu         sync.Mutex
	next       int
	connecting bool
	sessions   map[string]*terminalSession
}

type terminalSession struct {
	screen *vt.Screen
	in     io.Writer
	close  io.Closer
	mu     sync.Mutex
	last   time.Time
	open   bool
}

// Snapshot is returned by the MCP screen tools.
type Snapshot struct {
	Session   string `json:"session,omitempty"`
	Text      string `json:"text"`
	Row       int    `json:"row"`
	Column    int    `json:"column"`
	Connected bool   `json:"connected"`
	Matched   *bool  `json:"matched,omitempty"`
}

// NewServer creates the MCP server for one BBS endpoint.
func NewServer(profile Profile) (*mcp.Server, error) {
	if profile.Host == "" || profile.Port < 1 || profile.Port > 65535 {
		return nil, fmt.Errorf("host and a valid port are required")
	}
	if profile.Protocol == "" {
		profile.Protocol = "telnet"
	}
	if profile.Protocol != "telnet" && profile.Protocol != "ssh" {
		return nil, fmt.Errorf("protocol must be telnet or ssh")
	}
	if profile.Protocol == "ssh" && (profile.SSHUser == "" || profile.SSHHostKeySHA256 == "") {
		return nil, fmt.Errorf("ssh_user and ssh_host_key_sha256 are required for SSH")
	}
	if profile.Terminal == "" {
		profile.Terminal = "ANSI"
	}
	if profile.Columns == 0 {
		profile.Columns = 80
	}
	if profile.Rows == 0 {
		profile.Rows = 25
	}
	if profile.Columns < 20 || profile.Columns > 240 || profile.Rows < 10 || profile.Rows > 100 {
		return nil, fmt.Errorf("terminal size must be 20-240 columns by 10-100 rows")
	}
	if profile.Encoding == "" {
		profile.Encoding = "cp437"
	}
	if profile.Encoding != "cp437" && profile.Encoding != "utf8" {
		return nil, fmt.Errorf("encoding must be cp437 or utf8")
	}
	s := &Server{profile: profile, sessions: make(map[string]*terminalSession)}
	srv := mcp.NewServer(&mcp.Implementation{Name: "vision3-regression-terminal", Version: "1"}, nil)

	mcp.AddTool(srv, &mcp.Tool{Name: "connect", Description: "Connect to the configured disposable BBS and return its initial screen."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return s.connect(ctx)
		})
	type sessionArgs struct {
		Session string `json:"session"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "screen", Description: "Read the current BBS terminal screen."},
		func(_ context.Context, _ *mcp.CallToolRequest, a sessionArgs) (*mcp.CallToolResult, any, error) {
			return s.screen(a.Session)
		})
	type sendArgs struct {
		Session string `json:"session"`
		Input   string `json:"input" jsonschema:"text and key tokens to send; supports {enter}, {esc}, arrows, {bs}, {tab}, {ctrl-a} through {ctrl-z}, and {{ for a literal {"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "send", Description: "Send text and key tokens to the BBS. Supports control keys and CP437; this can change test data, so use a disposable instance."},
		func(ctx context.Context, _ *mcp.CallToolRequest, a sendArgs) (*mcp.CallToolResult, any, error) {
			return s.send(ctx, a.Session, a.Input)
		})
	type waitArgs struct {
		Session  string `json:"session"`
		Until    string `json:"until" jsonschema:"regular expression to match on the screen, or idle"`
		TimeoutS int    `json:"timeout_s" jsonschema:"seconds to wait, 1 to 60"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "wait", Description: "Wait for a regular expression to appear on screen, or wait for the terminal to become idle."},
		func(ctx context.Context, _ *mcp.CallToolRequest, a waitArgs) (*mcp.CallToolResult, any, error) {
			return s.wait(ctx, a.Session, a.Until, a.TimeoutS)
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "disconnect", Description: "Close the terminal session."},
		func(_ context.Context, _ *mcp.CallToolRequest, a sessionArgs) (*mcp.CallToolResult, any, error) {
			return s.disconnect(a.Session)
		})
	return srv, nil
}

func (s *Server) connect(ctx context.Context) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	if s.connecting {
		s.mu.Unlock()
		return errorResult("a terminal connection is already being opened"), nil, nil
	}
	for _, old := range s.sessions {
		if old.connected() {
			s.mu.Unlock()
			return errorResult("only one terminal session is allowed at a time"), nil, nil
		}
	}
	s.connecting = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.connecting = false
		s.mu.Unlock()
	}()
	addr := net.JoinHostPort(s.profile.Host, strconv.Itoa(s.profile.Port))
	var in io.Writer
	var out io.Reader
	var closer io.Closer
	switch s.profile.Protocol {
	case "telnet":
		conn, err := telnetclient.Dial(ctx, addr, telnetclient.Options{TermType: s.profile.Terminal, Width: s.profile.Columns, Height: s.profile.Rows}, 10*time.Second)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		in, out, closer = conn, conn, conn
	case "ssh":
		cfg := &ssh.ClientConfig{User: s.profile.SSHUser, Auth: []ssh.AuthMethod{ssh.KeyboardInteractive(func(string, string, []string, []bool) ([]string, error) { return nil, nil })}, Timeout: 10 * time.Second,
			HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				got := ssh.FingerprintSHA256(key)
				if got != s.profile.SSHHostKeySHA256 {
					return fmt.Errorf("SSH host key mismatch: got %s", got)
				}
				return nil
			}}
		client, err := ssh.Dial("tcp", addr, cfg)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		channel, err := client.NewSession()
		if err != nil {
			_ = client.Close()
			return errorResult(err.Error()), nil, nil
		}
		if err := channel.RequestPty(s.profile.Terminal, s.profile.Rows, s.profile.Columns, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
			_ = client.Close()
			return errorResult(err.Error()), nil, nil
		}
		stdin, err := channel.StdinPipe()
		if err != nil {
			_ = client.Close()
			return errorResult(err.Error()), nil, nil
		}
		stdout, err := channel.StdoutPipe()
		if err != nil {
			_ = client.Close()
			return errorResult(err.Error()), nil, nil
		}
		if err := channel.Shell(); err != nil {
			_ = client.Close()
			return errorResult(err.Error()), nil, nil
		}
		in, out, closer = stdin, stdout, client
	}
	ts := &terminalSession{in: in, close: closer, last: time.Now(), open: true}
	ts.screen = vt.New(s.profile.Columns, s.profile.Rows, vt.EncodingFor(s.profile.Encoding), func(p []byte) { _, _ = in.Write(p) })
	s.mu.Lock()
	s.next++
	id := fmt.Sprintf("s%d", s.next)
	s.sessions[id] = ts
	s.mu.Unlock()
	go ts.pump(out)
	ts.settle(200*time.Millisecond, 2*time.Second)
	return jsonResult(Snapshot{Session: id, Text: ts.snapshot().Text, Row: ts.snapshot().Row, Column: ts.snapshot().Col, Connected: ts.connected()}), nil, nil
}

func (s *Server) session(id string) (*terminalSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("no session %q", id)
	}
	return ts, nil
}

func (s *Server) screen(id string) (*mcp.CallToolResult, any, error) {
	ts, err := s.session(id)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(ts.result(id)), nil, nil
}

func (s *Server) send(ctx context.Context, id, input string) (*mcp.CallToolResult, any, error) {
	ts, err := s.session(id)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	select {
	case <-ctx.Done():
		return errorResult(ctx.Err().Error()), nil, nil
	default:
	}
	data, err := keys.Parse(input, s.profile.Encoding)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	ts.mu.Lock()
	if !ts.open {
		ts.mu.Unlock()
		return errorResult("session is disconnected"), nil, nil
	}
	_, err = ts.in.Write(data)
	ts.last = time.Now()
	ts.mu.Unlock()
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	ts.settle(250*time.Millisecond, 3*time.Second)
	return jsonResult(ts.result(id)), nil, nil
}

func (s *Server) wait(ctx context.Context, id, until string, timeoutS int) (*mcp.CallToolResult, any, error) {
	ts, err := s.session(id)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	if timeoutS < 1 || timeoutS > 60 {
		return errorResult("timeout_s must be between 1 and 60"), nil, nil
	}
	var re *regexp.Regexp
	if until != "idle" {
		re, err = regexp.Compile(until)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
	}
	deadline := time.NewTimer(time.Duration(timeoutS) * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	matched := false
	for {
		snap := ts.snapshot()
		if re != nil && re.MatchString(snap.Text) {
			matched = true
			break
		}
		if re == nil && ts.idleFor() >= 250*time.Millisecond {
			matched = true
			break
		}
		select {
		case <-ctx.Done():
			return errorResult(ctx.Err().Error()), nil, nil
		case <-deadline.C:
			goto done
		case <-tick.C:
		}
	}
done:
	snap := ts.result(id)
	snap.Matched = &matched
	return jsonResult(snap), nil, nil
}

func (s *Server) disconnect(id string) (*mcp.CallToolResult, any, error) {
	ts, err := s.session(id)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	ts.mu.Lock()
	wasOpen := ts.open
	ts.open = false
	ts.mu.Unlock()
	if wasOpen {
		_ = ts.close.Close()

	}
	return jsonResult(ts.result(id)), nil, nil
}

func (ts *terminalSession) pump(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			_, _ = ts.screen.Write(buf[:n])
			ts.mu.Lock()
			ts.last = time.Now()

			ts.mu.Unlock()
		}
		if err != nil {
			ts.mu.Lock()
			ts.open = false
			ts.mu.Unlock()
			return
		}
	}
}

func (ts *terminalSession) snapshot() vt.Snapshot { return ts.screen.Snapshot() }
func (ts *terminalSession) connected() bool       { ts.mu.Lock(); defer ts.mu.Unlock(); return ts.open }
func (ts *terminalSession) idleFor() time.Duration {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return time.Since(ts.last)
}
func (ts *terminalSession) result(id string) Snapshot {
	s := ts.snapshot()
	return Snapshot{Session: id, Text: s.Text, Row: s.Row, Column: s.Col, Connected: ts.connected()}
}
func (ts *terminalSession) settle(idle, maxWait time.Duration) {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) && ts.connected() && ts.idleFor() < idle {
		time.Sleep(20 * time.Millisecond)
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	b, _ := json.Marshal(v)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}
