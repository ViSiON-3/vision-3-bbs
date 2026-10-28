package menu

import (
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// authOnSession runs RUN:AUTHENTICATE once on ts, as each pass of the LOGIN
// menu does on one call, and returns what it returned.
func authOnSession(t *testing.T, env *menuEnv, ts *testSession) (next string, err error) {
	t.Helper()
	c := &cmdCtx{
		e: env.e, s: ts, terminal: newTestTerminal(ts), userManager: env.um,
		nodeNumber: 1, sessionStartTime: time.Now(), outputMode: env.outputMode,
		termWidth: 80, termHeight: 24,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, next, err = runAuthenticate(c, "")
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RUN:AUTHENTICATE still running 10s after its input ran out")
	}
	return next, err
}

// loginSession is a call from ip with the given keys to type.
func loginSession(t *testing.T, env *menuEnv, ip, input string) *testSession {
	t.Helper()
	ts := newTestSession(input)
	ts.addr = &net.TCPAddr{IP: net.ParseIP(ip), Port: 2222}
	SetSessionOutputMode(ts, env.outputMode)
	t.Cleanup(func() {
		resetSessionIH(ts)
		ClearSessionOutputMode(ts)
		ClearSessionIdleTimeout(ts)
	})
	return ts
}

// The BBS standard: two wrong passwords are let back to the prompt, the
// third shows NICETRY.ANS and the call is dropped.
func TestThirdWrongPasswordShowsNiceTryAndHangsUp(t *testing.T) {
	env := newMenuEnv(t)
	addLoginUser(t, env, "Alice", "secret", 20)
	lock := newFakeLockout()
	env.e.IPLockoutCheck = lock
	ts := loginSession(t, env, "198.51.100.7", strings.Repeat("Alice\rguess\r", 3))

	for try := 1; try <= 2; try++ {
		if next, err := authOnSession(t, env, ts); err != nil || next != "" || ts.closed.Load() {
			t.Fatalf("try %d: next=%q err=%v closed=%v; want back to the prompt", try, next, err, ts.closed.Load())
		}
	}
	next, err := authOnSession(t, env, ts)
	if !errors.Is(err, io.EOF) || next != "LOGOFF" {
		t.Fatalf("third try: next=%q err=%v; want LOGOFF and io.EOF", next, err)
	}
	if !ts.closed.Load() {
		t.Fatal("the call was not hung up after the third wrong password")
	}
	if out := ts.output(); !strings.Contains(out, "Try the CORRECT password next time.") {
		t.Errorf("NICETRY.ANS not shown: %q", out[max(0, len(out)-300):])
	}
	if lock.failed["198.51.100.7"] != 3 {
		t.Errorf("IP lockout counted %d failures, want 3", lock.failed["198.51.100.7"])
	}
}

// maxLoginTries changes the number of tries; a right password in time
// clears the count.
func TestLoginTriesConfigurableAndClearedOnSuccess(t *testing.T) {
	env := newMenuEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.MaxLoginTries = 2
	env.e.SetServerConfig(cfg)
	addLoginUser(t, env, "Alice", "secret", 20)
	ts := loginSession(t, env, "198.51.100.8", "Alice\rguess\rAlice\rsecret\r")

	if _, err := authOnSession(t, env, ts); err != nil || ts.closed.Load() {
		t.Fatalf("first wrong password: err=%v closed=%v", err, ts.closed.Load())
	}
	if _, err := authOnSession(t, env, ts); err != nil || ts.closed.Load() {
		t.Fatalf("right password on the second try: err=%v closed=%v", err, ts.closed.Load())
	}
	if _, ok := sessionLoginFailures.Load(ts); ok {
		t.Error("a successful login left the failure count")
	}

	ts2 := loginSession(t, env, "198.51.100.8", strings.Repeat("Alice\rguess\r", 2))
	_, _ = authOnSession(t, env, ts2)
	if _, err := authOnSession(t, env, ts2); !errors.Is(err, io.EOF) || !ts2.closed.Load() {
		t.Fatalf("second wrong password with maxLoginTries 2: err=%v closed=%v", err, ts2.closed.Load())
	}
}

// The failure that locks the IP out drops the call at once, whatever tries
// are left.
func TestLockoutDropsTheCallWithNiceTry(t *testing.T) {
	env := newMenuEnv(t)
	ts := loginSession(t, env, "198.51.100.9", "")
	if !env.e.loginFailed(ts, newTestTerminal(ts), env.outputMode, 1, true, 80, 24) {
		t.Fatal("a lockout did not end the call")
	}
	if !ts.closed.Load() || !strings.Contains(ts.output(), "Try the CORRECT password next time.") {
		t.Fatalf("closed=%v, output %q", ts.closed.Load(), ts.output())
	}
}

// Without NICETRY.ANS the execLoginTooManyTries string is shown instead.
func TestNiceTryFallsBackToTheString(t *testing.T) {
	env := newMenuEnv(t)
	env.e.MenuSetPath = filepath.Join(t.TempDir(), "menus", "v3")
	ts := loginSession(t, env, "198.51.100.10", "")
	if !env.e.loginFailed(ts, newTestTerminal(ts), env.outputMode, 1, true, 80, 24) {
		t.Fatal("a lockout did not end the call")
	}
	if !strings.Contains(ts.output(), "Nice try!") {
		t.Fatalf("no fallback string: %q", ts.output())
	}
}
