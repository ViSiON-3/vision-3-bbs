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
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
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

	// Hooks are read from the configured value before formatting, so text a
	// caller typed and the BBS formats into a string can never start one.
	t.Run("formatted arguments cannot start a hook", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		out := f.write(t, "%s", "!say")
		if !strings.Contains(out, "!say") || strings.Contains(out, "[script") {
			t.Errorf("output = %q", out)
		}
		f = newStringHookFixture(t, "")
		out = f.write(t, "%s", "%BAR.ANS")
		if !strings.Contains(out, "%BAR.ANS") || strings.Contains(out, "[bar art]") {
			t.Errorf("output = %q", out)
		}
	})

	// writeStringSubst applies a call site's own substitutions to the text
	// only, so a substituted handle cannot start a hook either.
	t.Run("substituted text cannot start a hook", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		terminal := newDoorcovTerminal(f.s.doorcovSession)
		RegisterStringHookSession(terminal, f.s, nil, f.bbs)
		defer ClearStringHookSession(terminal)
		if err := f.e.writeStringSubst(terminal, ansi.OutputModeUTF8, "|NA", "|NA", "!say"); err != nil {
			t.Fatal(err)
		}
		if err := f.e.writeStringSubst(terminal, ansi.OutputModeUTF8, "%BAR.ANS Hi |NA", "|NA", "!say"); err != nil {
			t.Fatal(err)
		}
		out := f.term()
		if strings.Contains(out, "[script") || !strings.Contains(out, "!say") || !strings.Contains(out, "[bar art]Hi !say") {
			t.Errorf("output = %q", out)
		}
	})

	// A script in place of a substituted string gets the substituted values,
	// so it can draw its own version of the message.
	t.Run("script receives substituted values", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		terminal := newDoorcovTerminal(f.s.doorcovSession)
		RegisterStringHookSession(terminal, f.s, nil, f.bbs)
		defer ClearStringHookSession(terminal)
		if err := f.e.writeStringSubst(terminal, ansi.OutputModeUTF8, "!say", "^CN", "Main", "^CT", "MAIN"); err != nil {
			t.Fatal(err)
		}
		if out := f.term(); !strings.Contains(out, "[script door,Main,MAIN]") {
			t.Errorf("output = %q", out)
		}
	})

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

	// With no InputHandler running, the script reads ahead from the session
	// itself. Keys the caller typed for the next prompt that the script did
	// not read must reach that prompt (#653).
	t.Run("hands keys typed ahead to the next prompt", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.s.whenOutput("[script", func() { f.s.send("xyz") })

		out := f.write(t, "!say key")
		if !strings.Contains(out, "[got x]") {
			t.Fatalf("script did not read its key; output = %q", out)
		}
		ih := getSessionIH(f.s)
		defer resetSessionIH(f.s)
		stop := make(chan struct{})
		timer := time.AfterFunc(time.Second, func() { close(stop) })
		defer timer.Stop()
		for _, want := range []byte("yz") {
			b, err := ih.ReadByteUntil(stop)
			if err != nil || b != want {
				t.Fatalf("next prompt read %q, %v; want %q", b, err, want)
			}
		}
	})

	// A second script run before any handler is started gets the keys the
	// first left, ahead of the session's newer input.
	t.Run("hands keys typed ahead to the next script", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.s.whenOutput("[script", func() { f.s.send("xyz") })

		if out := f.write(t, "!say key"); !strings.Contains(out, "[got x]") {
			t.Fatalf("first script did not read its key; output = %q", out)
		}
		if out := f.write(t, "!say key"); !strings.Contains(out, "[got y]") {
			t.Fatalf("second script did not read the kept key; output = %q", out)
		}
		f.s.send("w")
		ih := getSessionIH(f.s)
		defer resetSessionIH(f.s)
		stop := make(chan struct{})
		timer := time.AfterFunc(time.Second, func() { close(stop) })
		defer timer.Stop()
		for _, want := range []byte("zw") {
			b, err := ih.ReadByteUntil(stop)
			if err != nil || b != want {
				t.Fatalf("next prompt read %q, %v; want %q", b, err, want)
			}
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

// prompt runs fn, a Yes/No prompt on a terminal with the session registered,
// and returns its answer and the output.
func (f *stringHookFixture) prompt(t *testing.T, fn func(*MenuExecutor, ssh.Session, *term.Terminal) (bool, error)) (bool, string) {
	t.Helper()
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	RegisterStringHookSession(terminal, f.s, nil, f.bbs)
	defer ClearStringHookSession(terminal)
	defer resetSessionIH(f.s)
	type result struct {
		yes bool
		err error
	}
	done := make(chan result, 1)
	go func() {
		yes, err := fn(f.e, f.s, terminal)
		done <- result{yes, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("prompt: %v; output:\n%s", r.err, f.term())
		}
		return r.yes, f.term()
	case <-time.After(10 * time.Second):
		t.Fatalf("prompt still running after 10s; output:\n%s", f.term())
		return false, ""
	}
}

func TestPromptYesNoHooks(t *testing.T) {
	// termHeight 0 takes the text fallback, which reads a line.
	t.Run("string value shows its file before the question", func(t *testing.T) {
		f := newStringHookFixture(t, "y\r")
		yes, out := f.prompt(t, func(e *MenuExecutor, s ssh.Session, tm *term.Terminal) (bool, error) {
			return e.PromptYesNoString(s, tm, "%BAR.ANS Really? @", ansi.OutputModeUTF8, 3, 80, 0, false)
		})
		if !yes || !strings.Contains(out, "[bar art]Really? [y/N]? ") {
			t.Errorf("yes=%v output = %q", yes, out)
		}
	})
	t.Run("formatted value", func(t *testing.T) {
		f := newStringHookFixture(t, "n\r")
		yes, out := f.prompt(t, func(e *MenuExecutor, s ssh.Session, tm *term.Terminal) (bool, error) {
			return e.PromptYesNof(s, tm, ansi.OutputModeUTF8, 3, 80, 0, true, "%BAR.ANS Add %s?", "FidoNet")
		})
		if yes || !strings.Contains(out, "[bar art]Add FidoNet? [Y/n]? ") {
			t.Errorf("yes=%v output = %q", yes, out)
		}
	})
	t.Run("plain prompt text is never a hook", func(t *testing.T) {
		f := newStringHookFixture(t, "y\r")
		_, out := f.prompt(t, func(e *MenuExecutor, s ssh.Session, tm *term.Terminal) (bool, error) {
			return e.PromptYesNo(s, tm, "%BAR.ANS Delete?", ansi.OutputModeUTF8, 3, 80, 0, false)
		})
		if !strings.Contains(out, "%BAR.ANS Delete?") || strings.Contains(out, "[bar art]") {
			t.Errorf("output = %q", out)
		}
	})
}

// tappedHookSession is a hookSession with a snoop tap.
type tappedHookSession struct {
	hookSession
	tap *snoop.Tap
}

func (s tappedHookSession) Tap() *snoop.Tap { return s.tap }

// A script run from a screen in another snoop mode, such as chat's pickers in
// teleconference mode, leaves that mode in place; the door path alone would
// put the tap back in BBS mode.
func TestStringScriptRestoresSnoopMode(t *testing.T) {
	f := newStringHookFixture(t, "")
	tap := snoop.NewTap()
	t.Cleanup(tap.Close)
	tap.SetMode(snoop.ModeTeleconf)
	s := tappedHookSession{hookSession: f.s, tap: tap}
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	RegisterStringHookSession(terminal, s, nil, f.bbs)
	defer ClearStringHookSession(terminal)

	if err := f.e.writeString(terminal, ansi.OutputModeUTF8, "!say"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.term(), "[script") {
		t.Fatalf("script did not run; output = %q", f.term())
	}
	if m := tap.Mode(); m != snoop.ModeTeleconf {
		t.Errorf("mode after the script = %v, want teleconference", m)
	}
}

// A default prompt that shows a file continues its text where the file left
// the cursor, with placeholders expanded; a missing file prints the whole
// value as written.
func TestDefPromptFileHook(t *testing.T) {
	for _, tc := range []struct {
		value, want string
	}{
		{"%BAR.ANS Hi |MN", "[bar art]Hi MAIN"},
		{"%NOPE.ANS Hi |MN", "%NOPE.ANS Hi MAIN"},
	} {
		f := newStringHookFixture(t, "")
		f.e.SetStrings(config.StringsConfig{DefPrompt: tc.value})
		terminal := newDoorcovTerminal(f.s.doorcovSession)
		if err := f.e.displayPrompt(f.s, terminal, &MenuRecord{}, nil, nil, 3, "MAIN", time.Now(), ansi.OutputModeUTF8, ""); err != nil {
			t.Fatalf("displayPrompt(%q): %v", tc.value, err)
		}
		if out := stripPipes(f.term()); !strings.Contains(out, tc.want) {
			t.Errorf("displayPrompt(%q) output = %q, want it to contain %q", tc.value, out, tc.want)
		}
	}
}
