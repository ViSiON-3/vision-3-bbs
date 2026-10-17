//go:build !windows

package menu

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// hookCtx is a minimal ssh.Context: the script engine derives its own context
// from the session's, which the door test sessions report as nil.
type hookCtx struct {
	context.Context
	sync.Mutex
}

func (*hookCtx) User() string                  { return "" }
func (*hookCtx) SessionID() string             { return "" }
func (*hookCtx) ClientVersion() string         { return "" }
func (*hookCtx) ServerVersion() string         { return "" }
func (*hookCtx) RemoteAddr() net.Addr          { return doorcovAddr }
func (*hookCtx) LocalAddr() net.Addr           { return doorcovAddr }
func (*hookCtx) Permissions() *ssh.Permissions { return &ssh.Permissions{} }
func (*hookCtx) SetValue(_, _ interface{})     {}

// hookSession is a door test session with a real context.
type hookSession struct {
	*doorcovSession
	ctx *hookCtx
}

func (s hookSession) Context() ssh.Context { return s.ctx }

// stringHookFixture is an executor with a menu set holding BAR.ANS and a
// V3 script door SAY whose script prints its arguments.
type stringHookFixture struct {
	e    *MenuExecutor
	s    hookSession
	bbs  *session.BbsSession
	term func() string
}

func newStringHookFixture(t *testing.T, input string) *stringHookFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ansi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ansi", "BAR.ANS"), []byte("[bar art]"), 0o644); err != nil {
		t.Fatal(err)
	}
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	say := `v3.console.print("[script " + v3.args.join(",") + "]");
if (v3.args.indexOf("key") >= 0) { v3.console.print("[got " + v3.console.getkey() + "]"); }`
	if err := os.WriteFile(filepath.Join(scripts, "say.js"), []byte(say), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &MenuExecutor{MenuSetPath: root, RootConfigPath: root}
	e.SetStrings(config.StringsConfig{ExecFileLoadError: "Cannot load %s"})
	e.SetServerConfig(config.ServerConfig{CoSysOpLevel: 250})
	e.SetDoorRegistry(map[string]config.DoorConfig{
		"SAY":    {Type: "v3_script", Script: "say.js", WorkingDirectory: scripts, Args: []string{"door"}},
		"NATIVE": {Commands: []string{"/bin/true"}},
		"LOCKED": {Type: "v3_script", Script: "say.js", WorkingDirectory: scripts, MinAccessLevel: 200},
		"GONE":   {Type: "v3_script", Script: "gone.js", WorkingDirectory: scripts},
		"DIR":    {Type: "v3_script", Script: ".", WorkingDirectory: scripts},
	})

	ds := newDoorcovSession()
	if input != "" {
		ds.send(input)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := hookSession{doorcovSession: ds, ctx: &hookCtx{Context: ctx}}
	bbs := &session.BbsSession{NodeID: 3, StartTime: time.Now(),
		User: &user.User{ID: 7, Handle: "Neo", AccessLevel: 50, TimeLimit: 60}}
	return &stringHookFixture{e: e, s: s, bbs: bbs, term: ds.output}
}

// write writes value through writeStringf with args, with the session
// registered behind the terminal.
func (f *stringHookFixture) write(t *testing.T, value string, args ...any) string {
	t.Helper()
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	RegisterStringHookSession(terminal, f.s, nil, f.bbs)
	defer ClearStringHookSession(terminal)
	done := make(chan error, 1)
	go func() {
		if args == nil {
			done <- f.e.writeString(terminal, ansi.OutputModeUTF8, value)
			return
		}
		done <- f.e.writeStringf(terminal, ansi.OutputModeUTF8, value, args...)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write %q: %v", value, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("write %q still running after 10s; output:\n%s", value, f.term())
	}
	return f.term()
}

func TestWriteStringText(t *testing.T) {
	t.Run("verbatim value is not formatted", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		out := f.write(t, "Ratio (|15RA%|09)")
		if !strings.Contains(out, "RA%") || strings.Contains(out, "%!") {
			t.Errorf("output = %q", out)
		}
	})
	t.Run("formatted value", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := f.write(t, "%d of %d", 2, 5); !strings.Contains(out, "2 of 5") {
			t.Errorf("output = %q", out)
		}
	})
	t.Run("escape prints the bang", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := f.write(t, "!|SAY %s", "hi"); !strings.Contains(out, "!SAY hi") || strings.Contains(out, "[script") {
			t.Errorf("output = %q", out)
		}
	})
}

func TestWriteStringFile(t *testing.T) {
	t.Run("file then formatted text", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		out := f.write(t, "%BAR.ANS Pick %s:", "one")
		if !strings.Contains(out, "[bar art]Pick one:") {
			t.Errorf("output = %q", out)
		}
	})
	t.Run("missing file prints the value as text", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		out := f.write(t, "%NOPE.ANS Pick %s:", "one")
		if !strings.Contains(out, "%NOPE.ANS Pick one:") {
			t.Errorf("output = %q", out)
		}
	})
}

func TestWriteStringScript(t *testing.T) {
	t.Run("runs the script with door, string and call arguments", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		out := f.write(t, "!say extra", "doorPrompt", 3)
		if !strings.Contains(out, "[script door,extra,doorPrompt,3]") {
			t.Errorf("output = %q", out)
		}
		if strings.Contains(out, "!say") {
			t.Errorf("value text printed as well as the script: %q", out)
		}
	})

	for _, tc := range []struct {
		name  string
		value string
		setup func(*stringHookFixture)
	}{
		{"no such door", "!missing", nil},
		{"not a script door", "!native", nil},
		{"below the door's access level", "!locked", nil},
		{"script file missing", "!gone", nil},
		{"script path is a directory", "!dir", nil},
		{"before login", "!say", func(f *stringHookFixture) { f.bbs.User = nil }},
	} {
		t.Run(tc.name+" prints the value as text", func(t *testing.T) {
			f := newStringHookFixture(t, "")
			if tc.setup != nil {
				tc.setup(f)
			}
			out := f.write(t, tc.value)
			if !strings.Contains(out, tc.value) || strings.Contains(out, "[script") {
				t.Errorf("output = %q", out)
			}
		})
	}

	t.Run("no registered session prints the value as text", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		terminal := newDoorcovTerminal(f.s.doorcovSession)
		if err := f.e.writeString(terminal, ansi.OutputModeUTF8, "!say"); err != nil {
			t.Fatal(err)
		}
		if out := f.term(); !strings.Contains(out, "!say") || strings.Contains(out, "[script") {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("no-script writer prints the value as text", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		terminal := newDoorcovTerminal(f.s.doorcovSession)
		RegisterStringHookSession(terminal, f.s, nil, f.bbs)
		defer ClearStringHookSession(terminal)
		if err := f.e.writeStringNoScript(terminal, ansi.OutputModeUTF8, "!say"); err != nil {
			t.Fatal(err)
		}
		if out := f.term(); !strings.Contains(out, "!say") || strings.Contains(out, "[script") {
			t.Errorf("output = %q", out)
		}
	})

	// The caller of a prompt often holds the session's InputHandler across
	// the prompt. A script run in its place must read through that handler
	// and leave it open; closing it would hand the caller end-of-file.
	t.Run("leaves a running input handler usable", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		ih := getSessionIH(f.s)
		defer resetSessionIH(f.s)
		f.s.whenOutput("[script", func() { f.s.send("xy") })

		out := f.write(t, "!say key")
		if !strings.Contains(out, "[got x]") {
			t.Fatalf("script did not read its key; output = %q", out)
		}
		if cur := getSessionIH(f.s); cur != ih {
			t.Fatal("the session's input handler was replaced")
		}
		stop := make(chan struct{})
		timer := time.AfterFunc(time.Second, func() { close(stop) })
		defer timer.Stop()
		b, err := ih.ReadByteUntil(stop)
		if err != nil || b != 'y' {
			t.Errorf("caller's next read = %q, %v; want 'y'", b, err)
		}
	})
}
