package menu

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// pagerRun writes n numbered lines through a pager for u on a 10-row screen
// with 2 rows reserved (8 lines a screen), feeding input as keystrokes, and
// returns the output and how many lines were written.
func pagerRun(t *testing.T, u *user.User, n int, input string) (string, int) {
	t.Helper()
	env := newMenuEnv(t)
	written := 0
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		p := c.e.newMorePager(c.s, c.terminal, c.outputMode, u, 10, 2, "<MORE>")
		for i := 1; i <= n; i++ {
			c.e.writeString(c.terminal, c.outputMode, fmt.Sprintf("line %02d\r\n", i))
			written++
			if !p.Line() {
				break
			}
		}
		return nil, "", nil
	}, u, "", input)
	return r.text(), written
}

func boolp(b bool) *bool { return &b }

// TestMorePager pins the answers to the More prompt: Y, Enter and Space a
// screen more, C the rest without stopping, N and Q stop.
func TestMorePager(t *testing.T) {
	on := &user.User{ID: 7, Handle: "Pat"}
	for _, tc := range []struct {
		name, input string
		written     int
		prompts     int
	}{
		{"Y each time", "yyy", 20, 2},
		{"Enter and Space", "\r ", 20, 2},
		{"C runs on", "c", 20, 1},
		{"N stops", "n", 8, 1},
		{"Q stops", "q", 8, 1},
		{"other keys wait", "xzy\r", 20, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, written := pagerRun(t, on, 20, tc.input)
			if written != tc.written {
				t.Errorf("wrote %d lines, want %d:\n%s", written, tc.written, out)
			}
			if got := strings.Count(out, "<MORE>"); got != tc.prompts {
				t.Errorf("%d prompts, want %d:\n%s", got, tc.prompts, out)
			}
		})
	}
}

// TestMorePagerOff checks that a caller who turned More prompts off is never
// asked, and that an unset setting (every record before this change) is on.
func TestMorePagerOff(t *testing.T) {
	out, written := pagerRun(t, &user.User{ID: 7, Handle: "Pat", MorePrompts: boolp(false)}, 20, "")
	if written != 20 || strings.Contains(out, "<MORE>") {
		t.Errorf("More prompts off: wrote %d, prompted %v", written, strings.Contains(out, "<MORE>"))
	}
	if !(&user.User{}).MorePromptsOn() || !(&user.User{MorePrompts: boolp(true)}).MorePromptsOn() {
		t.Error("unset or true should be on")
	}
}

// TestUserListPages checks that a user list longer than the screen stops at
// the More prompt, and that N ends it there.
func TestUserListPages(t *testing.T) {
	env := newMenuEnv(t)
	for i := 0; i < 40; i++ {
		if _, err := env.um.AddUser("password1", fmt.Sprintf("Caller%02d", i), fmt.Sprintf("Caller %02d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	r := env.runCmd("LISTUSERS", env.caller, "", "n")
	text := r.text()
	if !strings.Contains(text, "More?") {
		t.Fatalf("no More prompt in a 40-user list:\n%s", text)
	}
	if shown := len(regexp.MustCompile(`Caller[0-9]{2}`).FindAllString(text, -1)); shown >= 40 {
		t.Errorf("N didn't stop the list: %d of 40 shown:\n%s", shown, text)
	}
	// By user number: the first screen starts with the earliest accounts.
	if i, j := strings.Index(text, "Caller00"), strings.Index(text, "Caller01"); i < 0 || j < 0 || i > j {
		t.Errorf("list not in user-number order:\n%s", text)
	}
}

// idleAtMore runs fn for u under a one-unit session idle timeout, typing
// keys and then nothing, and returns what it returned. The caller is
// expected to be left sitting at a More prompt.
func idleAtMore(t *testing.T, env *menuEnv, u *user.User, keys string, fn func(*cmdCtx, string) (*user.User, string, error)) (string, error) {
	t.Helper()
	old := sessionIdleUnit
	sessionIdleUnit = 50 * time.Millisecond
	t.Cleanup(func() { sessionIdleUnit = old })
	cfg := env.e.GetServerConfig()
	cfg.SessionIdleTimeoutMinutes, cfg.CoSysOpLevel, cfg.SysOpLevel = 1, 250, 255
	env.e.SetServerConfig(cfg)

	r, w := io.Pipe()
	s := &chatLiveSession{testSession: newTestSession(""), in: r}
	SetSessionOutputMode(s, env.outputMode)
	applySessionIdleTimeout(s, env.e.idleTimeout(u))
	t.Cleanup(func() {
		_ = w.Close()
		resetSessionIH(s)
		clearSessionIdleTimeout(s)
		ClearSessionOutputMode(s)
	})
	c := &cmdCtx{
		e: env.e, s: s, terminal: term.NewTerminal(s, ""), userManager: env.um,
		currentUser: u, nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: env.outputMode, termWidth: 80, termHeight: 24,
	}
	go func() { _, _ = io.WriteString(w, keys) }()
	type result struct {
		next string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		_, next, err := fn(c, "")
		done <- result{next, err}
	}()
	select {
	case res := <-done:
		return res.next, res.err
	case <-time.After(handlerWait):
		t.Fatal("still waiting at the More prompt after the idle timeout")
		return "", nil
	}
}

// TestMorePagerIdleTimeoutEndsCall pins that an idle timeout at a More
// prompt ends the call, as it does anywhere else, rather than dropping the
// caller back at the menu.
func TestMorePagerIdleTimeoutEndsCall(t *testing.T) {
	t.Run("newscan", func(t *testing.T) {
		env := newMenuEnv(t)
		zero := time.Time{}
		env.caller.FileNewscanSince = &zero
		for i := 0; i < 30; i++ {
			addNewscanRecord(t, env, 1, fmt.Sprintf("IDLE%02d.ZIP", i), time.Now())
		}
		next, err := idleAtMore(t, env, env.caller, "", runFileNewscan)
		if next != "LOGOFF" || !errors.Is(err, editor.ErrIdleTimeout) {
			t.Errorf("newscan = (%q, %v), want LOGOFF with editor.ErrIdleTimeout", next, err)
		}
	})
	t.Run("search", func(t *testing.T) {
		env := newMenuEnv(t)
		names := make([]string, 30)
		for i := range names {
			names[i] = fmt.Sprintf("HIT%02d.ZIP", i)
		}
		addDownloadRecords(t, env, names...)
		next, err := idleAtMore(t, env, env.caller, "hit\r", runSearchFiles)
		if next != "LOGOFF" || !errors.Is(err, editor.ErrIdleTimeout) {
			t.Errorf("search = (%q, %v), want LOGOFF with editor.ErrIdleTimeout", next, err)
		}
	})
}

// TestMorePagerStopResult pins what a command returns once its output
// stops: the menu for a choice to stop or a hang-up, LOGOFF with any other
// read error.
func TestMorePagerStopResult(t *testing.T) {
	u := &user.User{ID: 7}
	for _, tc := range []struct {
		name     string
		err      error
		wantNext string
	}{
		{"chose to stop", nil, ""},
		{"hung up", io.EOF, ""},
		{"idle timeout", editor.ErrIdleTimeout, "LOGOFF"},
	} {
		p := &morePager{stopped: true, err: tc.err}
		got, next, err := p.stopResult(u)
		wantErr := tc.err
		if errors.Is(tc.err, io.EOF) {
			wantErr = nil
		}
		if got != u || next != tc.wantNext || !errors.Is(err, wantErr) || (wantErr == nil && err != nil) {
			t.Errorf("%s: stopResult = (%v, %q, %v)", tc.name, got, next, err)
		}
	}
}
