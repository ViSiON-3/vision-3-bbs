package menu

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
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
	ts := &holdSession{testSession: newTestSession(input), done: make(chan struct{})}
	t.Cleanup(func() {
		close(ts.done)
		resetSessionIH(ts)
	})
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
