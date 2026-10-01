package menu

import (
	"strings"
	"sync"
	"testing"
	"time"

	"errors"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

type fakePager struct {
	mu       sync.Mutex
	consoles int
	raised   []string
	cleared  []string
}

func (f *fakePager) Consoles() int { return f.consoles }

func (f *fakePager) RaisePage(node int, handle, reason string) {
	f.mu.Lock()
	f.raised = append(f.raised, reason)
	f.mu.Unlock()
}

func (f *fakePager) ClearPage(node int, handle, why string) {
	f.mu.Lock()
	f.cleared = append(f.cleared, why)
	f.mu.Unlock()
}

// holdSession replays its input and then blocks, so a key wait times out
// instead of seeing a disconnect.
type holdSession struct {
	*testSession
	done chan struct{}
}

func (h *holdSession) Read(p []byte) (int, error) {
	n, err := h.testSession.Read(p)
	if err != nil {
		<-h.done
	}
	return n, err
}

func runPageSysopFor(t *testing.T, env *menuEnv, handle, input string) string {
	t.Helper()
	return runPageSysopMode(t, env, handle, input, ansi.OutputModeAuto)
}

func runPageSysopMode(t *testing.T, env *menuEnv, handle, input string, mode ansi.OutputMode) string {
	t.Helper()
	ts := &holdSession{testSession: newTestSession(input), done: make(chan struct{})}
	t.Cleanup(func() {
		close(ts.done)
		resetSessionIH(ts)
		// The cooldown outlives the test; a rerun (-count=2) would be
		// refused before RaisePage. Calls within one test still share it.
		pageCooldowns.Delete(strings.ToLower(handle))
	})
	c := &cmdCtx{
		e:                env.e,
		s:                ts,
		terminal:         newTestTerminal(ts.testSession),
		currentUser:      &user.User{Handle: handle, AccessLevel: 10},
		nodeNumber:       1,
		sessionStartTime: time.Now(),
		outputMode:       mode,
		termWidth:        80,
		termHeight:       24,
	}
	if _, _, err := runPageSysop(c, ""); err != nil {
		t.Fatal(err)
	}
	return ts.output()
}

func setPageConfig(env *menuEnv, timeout, cooldown int) {
	cfg := env.e.GetServerConfig()
	cfg.PageSysopTimeoutSeconds = timeout
	cfg.PageSysopCooldownSeconds = cooldown
	env.e.SetServerConfig(cfg)
}

func TestPageSysopNoConsole(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{}
	env.e.Pager = p
	out := runPageSysopFor(t, env, "PsNoConsole", "")
	if !strings.Contains(out, "not available") {
		t.Fatalf("no unavailable notice: %q", out)
	}
	if len(p.raised) != 0 {
		t.Fatalf("raised %v", p.raised)
	}
}

func TestPageSysopTimesOut(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{consoles: 1}
	env.e.Pager = p
	setPageConfig(env, 1, 300)
	out := runPageSysopFor(t, env, "PsTimeout", "need help\r")
	if len(p.raised) != 1 || p.raised[0] != "need help" {
		t.Fatalf("raised %v", p.raised)
	}
	if len(p.cleared) != 1 || p.cleared[0] != "timeout" {
		t.Fatalf("cleared %v", p.cleared)
	}
	if !strings.Contains(out, "not available") {
		t.Fatalf("no timeout notice: %q", out)
	}
}

func TestPageSysopKeyCancels(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{consoles: 1}
	env.e.Pager = p
	setPageConfig(env, 5, 300)
	out := runPageSysopFor(t, env, "PsCancel", "why\rx")
	if len(p.cleared) != 1 || p.cleared[0] != "cancelled" {
		t.Fatalf("cleared %v", p.cleared)
	}
	if strings.Contains(out, "not available") {
		t.Fatalf("cancel printed the timeout notice: %q", out)
	}
}

func TestPageSysopEmptyReason(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{consoles: 1}
	env.e.Pager = p
	setPageConfig(env, 1, 300)
	runPageSysopFor(t, env, "PsEmpty", "\r")
	if len(p.raised) != 0 {
		t.Fatalf("raised %v", p.raised)
	}
}

func TestPageSysopCooldown(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{consoles: 1}
	env.e.Pager = p
	setPageConfig(env, 1, 300)
	runPageSysopFor(t, env, "PsCool", "x\r")
	out := runPageSysopFor(t, env, "PSCOOL", "y\r")
	if len(p.raised) != 1 {
		t.Fatalf("second page not blocked: %v", p.raised)
	}
	if !strings.Contains(out, "recently") || !strings.Contains(out, "5") {
		t.Fatalf("no cooldown notice: %q", out)
	}
}

func TestPageSysopCountdownInEveryOutputMode(t *testing.T) {
	for name, mode := range map[string]ansi.OutputMode{"utf8": ansi.OutputModeUTF8, "cp437": ansi.OutputModeCP437} {
		env := newMenuEnv(t)
		env.e.Pager = &fakePager{consoles: 1}
		setPageConfig(env, 2, 300)
		out := runPageSysopMode(t, env, "PsCount"+name, "help\r", mode)
		if !strings.Contains(out, string(pageCountdownFrame(2))) || !strings.Contains(out, string(pageCountdownFrame(1))) {
			t.Fatalf("%s: countdown frames missing from %q", name, out)
		}
	}
}

// runPageSysopWithLimit pages the sysop from a session whose time limit ends
// at limitEnds (credit is chat time already credited back), and returns the
// error runPageSysop gave and how long it took. With bypassIH the deadline is
// recorded for the session but not armed on its InputHandler, so the reason
// prompt still reads and the countdown starts, as when the limit runs out
// between key waits.
func runPageSysopWithLimit(t *testing.T, env *menuEnv, handle string, limitEnds time.Time, credit time.Duration, bypassIH bool) (time.Duration, error) {
	t.Helper()
	ts := &holdSession{testSession: newTestSession("help\r"), done: make(chan struct{})}
	t.Cleanup(func() {
		close(ts.done)
		resetSessionIH(ts)
		ClearSessionIdleTimeout(ts)
		pageCooldowns.Delete(strings.ToLower(handle))
	})
	addChatCredit(ts, credit)
	if bypassIH {
		sessionDeadlines.Store(ts, limitEnds.Add(credit))
	} else {
		applySessionDeadline(ts, limitEnds)
	}
	c := &cmdCtx{
		e:                env.e,
		s:                ts,
		terminal:         newTestTerminal(ts.testSession),
		currentUser:      &user.User{Handle: handle, AccessLevel: 10},
		nodeNumber:       1,
		sessionStartTime: time.Now(),
		outputMode:       ansi.OutputModeAuto,
		termWidth:        80,
		termHeight:       24,
	}
	start := time.Now()
	_, _, err := runPageSysop(c, "")
	return time.Since(start), err
}

func TestPageSysopStopsWhenTimeLimitRunsOutBetweenKeyWaits(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{consoles: 1}
	env.e.Pager = p
	setPageConfig(env, 10, 300)
	took, err := runPageSysopWithLimit(t, env, "PsLimitGone", time.Now().Add(300*time.Millisecond), 0, true)
	if !errors.Is(err, editor.ErrTimeLimit) {
		t.Fatalf("err = %v, want ErrTimeLimit", err)
	}
	if took > 5*time.Second {
		t.Fatalf("page waited %v after the time limit ran out", took)
	}
	if len(p.raised) != 1 || len(p.cleared) != 1 || p.cleared[0] != "cancelled" {
		t.Fatalf("raised %v cleared %v, want the page raised then cleared as cancelled", p.raised, p.cleared)
	}
}

func TestPageSysopStopsWhenTimeLimitRunsOutMidCountdown(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{consoles: 1}
	env.e.Pager = p
	setPageConfig(env, 10, 300)
	took, err := runPageSysopWithLimit(t, env, "PsLimitMid", time.Now().Add(1500*time.Millisecond), 0, false)
	if !errors.Is(err, editor.ErrTimeLimit) {
		t.Fatalf("err = %v, want ErrTimeLimit", err)
	}
	if took > 5*time.Second {
		t.Fatalf("page waited %v after the time limit ran out", took)
	}
	if len(p.cleared) != 1 || p.cleared[0] != "cancelled" {
		t.Fatalf("cleared %v", p.cleared)
	}
}

func TestPageSysopCountsDownWithTimeLeft(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{consoles: 1}
	env.e.Pager = p
	setPageConfig(env, 1, 300)
	_, err := runPageSysopWithLimit(t, env, "PsLimitLeft", time.Now().Add(time.Hour), 0, false)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(p.cleared) != 1 || p.cleared[0] != "timeout" {
		t.Fatalf("cleared %v, want the countdown to run out", p.cleared)
	}
}

func TestPageSysopChatCreditKeepsLimitOpen(t *testing.T) {
	env := newMenuEnv(t)
	p := &fakePager{consoles: 1}
	env.e.Pager = p
	setPageConfig(env, 1, 300)
	_, err := runPageSysopWithLimit(t, env, "PsLimitCredit", time.Now().Add(-time.Minute), 2*time.Minute, true)
	if err != nil {
		t.Fatalf("err = %v, chat time was charged to the caller", err)
	}
	if len(p.cleared) != 1 || p.cleared[0] != "timeout" {
		t.Fatalf("cleared %v, want the countdown to run out", p.cleared)
	}
}
