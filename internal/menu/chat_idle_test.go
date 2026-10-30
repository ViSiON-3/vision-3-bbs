package menu

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// chatIdleUnit stands in for a minute of idle timeout in these tests.
const chatIdleUnit = 50 * time.Millisecond

// chatStartWait bounds waits that span chat starting up, which opens its
// SQLite database. A slow Windows runner has taken longer than 5s to get
// through that (#792, #814). The waits poll, so this only matters on such a
// runner; a chat that never ends still fails, showing its screen.
const chatStartWait = 30 * time.Second

// chatIdleRun is a chat session started under the menus' idle timeout, as a
// caller reaches it from a menu.
type chatIdleRun struct {
	s    *chatLiveSession
	keys *io.PipeWriter
	done chan struct{}
	next string
	err  error
}

// startChatIdle sets the idle timeouts (in chatIdleUnit) and runs chat for u
// on its own goroutine.
func startChatIdle(env *menuEnv, u *user.User, sessionUnits, chatUnits int) *chatIdleRun {
	env.t.Helper()
	old := sessionIdleUnit
	sessionIdleUnit = chatIdleUnit
	env.t.Cleanup(func() { sessionIdleUnit = old })
	cfg := env.e.GetServerConfig()
	cfg.SessionIdleTimeoutMinutes, cfg.ChatIdleTimeoutMinutes = sessionUnits, chatUnits
	cfg.CoSysOpLevel, cfg.SysOpLevel = 250, 255
	env.e.SetServerConfig(cfg)

	r, w := io.Pipe()
	s := &chatLiveSession{testSession: newTestSession(""), in: r}
	s.addr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}
	SetSessionOutputMode(s, env.outputMode)
	applySessionIdleTimeout(s, env.e.idleTimeout(u))
	c := &cmdCtx{
		e: env.e, s: s, terminal: term.NewTerminal(s, ""), userManager: env.um,
		currentUser: u, nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: env.outputMode, termWidth: 80, termHeight: 24,
	}
	run := &chatIdleRun{s: s, keys: w, done: make(chan struct{})}
	go func() {
		defer close(run.done)
		_, run.next, run.err = runChat(c, "")
	}()
	env.t.Cleanup(func() {
		_ = w.Close()
		select {
		case <-run.done:
		case <-time.After(10 * time.Second):
			env.t.Errorf("chat still running after its input closed")
		}
		resetSessionIH(s)
		clearSessionIdleTimeout(s)
		ClearSessionOutputMode(s)
	})
	return run
}

// ended waits up to d for chat to return and reports whether it did.
func (r *chatIdleRun) ended(d time.Duration) bool {
	select {
	case <-r.done:
		return true
	case <-time.After(d):
		return false
	}
}

// Room traffic keeps a caller who is only reading in chat. Once the room goes
// quiet, chat's own idle limit ends the call with the idle timeout error, which
// the menu loop turns into the idle notice and log line.
func TestChatIdleRoomTrafficKeepsCallerOn(t *testing.T) {
	env := newMenuEnv(t)
	// Bob's database is opened before the caller's idle countdown starts:
	// under -race on a Windows runner, opening it can outlast the chat limit.
	bob := chatcovLocal(env, "Bob")
	run := startChatIdle(env, env.caller, 1, 10)
	waitChatOutput(t, run.s, "Joined #lobby")

	if _, _, err := bob.Join("lobby"); err != nil {
		t.Fatalf("Bob joins: %v", err)
	}
	// Bob keeps the room busy for well past both timeouts, much more often
	// than the chat limit, while the caller types nothing. He changes the
	// topic rather than posting: a topic is room traffic like a message, but
	// stays in memory, where a Post is a SQLite write that under -race on a
	// Windows runner can itself outlast the limit (#706).
	talkUntil := time.Now().Add(24 * chatIdleUnit)
	var lastTraffic time.Time
	for time.Now().Before(talkUntil) {
		if err := bob.SetTopic("lobby", "still here"); err != nil {
			t.Fatalf("Bob sets the topic: %v", err)
		}
		lastTraffic = time.Now()
		if run.ended(2 * chatIdleUnit) {
			t.Fatalf("chat ended with the room talking: next=%q err=%v", run.next, run.err)
		}
	}

	if !run.ended(5 * time.Second) {
		t.Fatal("chat still running long after the room went quiet")
	}
	// Chat's limit is 10 units and the menus' 1: anything past 2 shows chat's
	// applied, with room to spare for a slow runner.
	if took := time.Since(lastTraffic); took < 2*chatIdleUnit {
		t.Errorf("chat ended %v after the room went quiet; its own limit is %v", took, 10*chatIdleUnit)
	}
	if !errors.Is(run.err, editor.ErrIdleTimeout) || run.next != "LOGOFF" {
		t.Errorf("runChat = (%q, %v), want LOGOFF with editor.ErrIdleTimeout", run.next, run.err)
	}
	if got, _ := sessionIdleTimeouts.Load(run.s); got != chatIdleUnit {
		t.Errorf("idle timeout after chat = %v, want the menus' %v back", got, chatIdleUnit)
	}

	// The menu loop's notice logs chat's limit, the one that ran out, not
	// the menus' that is back in place by then (#701).
	var logged bytes.Buffer
	restore := captureSlog(&logged, nil)
	env.e.handleSessionTimeout(run.s, term.NewTerminal(run.s, ""), env.outputMode, 1, 80, 24)
	restore()
	if !strings.Contains(logged.String(), `msg="idle timeout, disconnecting"`) || !strings.Contains(logged.String(), "minutes=10") {
		t.Errorf("idle log = %q, want the chat limit of 10", logged.String())
	}
}

// With no chat limit set, the session one applies in chat; SysOps are exempt
// from both.
func TestChatIdleTimeoutSetting(t *testing.T) {
	env := newMenuEnv(t)
	run := startChatIdle(env, env.caller, 2, 0)
	if !run.ended(chatStartWait) {
		t.Fatalf("silent caller not timed out under the session limit; screen: %q", run.s.output())
	}
	if !errors.Is(run.err, editor.ErrIdleTimeout) {
		t.Errorf("runChat err = %v, want editor.ErrIdleTimeout", run.err)
	}

	cfg := env.e.GetServerConfig()
	cfg.ChatIdleTimeoutMinutes = 3
	env.e.SetServerConfig(cfg)
	for _, tc := range []struct {
		u    *user.User
		want time.Duration
	}{
		{env.caller, 3 * chatIdleUnit},
		{env.sysop, 0},
		{&user.User{AccessLevel: 250}, 0},
	} {
		if got := env.e.chatIdleTimeout(tc.u); got != tc.want {
			t.Errorf("chatIdleTimeout(level %d) = %v, want %v", tc.u.AccessLevel, got, tc.want)
		}
	}

	sysop := startChatIdle(env, env.sysop, 1, 1)
	waitChatOutput(t, sysop.s, "Joined #lobby")
	if sysop.ended(5 * chatIdleUnit) {
		t.Errorf("SysOp timed out in chat: next=%q err=%v", sysop.next, sysop.err)
	}
}

// waitChatOutput waits until s's screen text contains marker.
func waitChatOutput(t *testing.T, s *chatLiveSession, marker string) {
	t.Helper()
	deadline := time.Now().Add(chatStartWait)
	for !strings.Contains(testAnsiEscape.ReplaceAllString(s.output(), ""), marker) {
		if time.Now().After(deadline) {
			t.Fatalf("screen never showed %q: %q", marker, s.output())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A caller who goes idle at the network or room picker is timed out there,
// rather than dropped into chat or back at the menu with a fresh idle window.
func TestChatIdleAtPicker(t *testing.T) {
	t.Run("network", func(t *testing.T) {
		env := newMenuEnv(t)
		n := chatcovFakeNet(env)
		run := startChatIdle(env, env.caller, 1, 0)
		if !run.ended(chatStartWait) {
			t.Fatalf("caller idle at the network picker not timed out; screen: %q", run.s.output())
		}
		if !errors.Is(run.err, editor.ErrIdleTimeout) || run.next != "LOGOFF" {
			t.Errorf("runChat = (%q, %v), want LOGOFF with editor.ErrIdleTimeout", run.next, run.err)
		}
		if len(n.chatted()) != 0 || !n.allClosed() {
			t.Error("a network session was joined or left open")
		}
	})
	t.Run("room", func(t *testing.T) {
		env := newMenuEnv(t)
		bob := chatcovLocal(env, "Bob")
		if _, _, err := bob.Join("lobby"); err != nil {
			t.Fatal(err)
		}
		run := startChatIdle(env, env.caller, 1, 0)
		waitChatOutput(t, run.s, "Select room")
		if !run.ended(chatStartWait) {
			t.Fatalf("caller idle at the room picker not timed out; screen: %q", run.s.output())
		}
		if !errors.Is(run.err, editor.ErrIdleTimeout) || run.next != "LOGOFF" {
			t.Errorf("runChat = (%q, %v), want LOGOFF with editor.ErrIdleTimeout", run.next, run.err)
		}
		if strings.Contains(run.s.output(), "Joined #") {
			t.Error("caller joined a room after timing out at the picker")
		}
	})
}
