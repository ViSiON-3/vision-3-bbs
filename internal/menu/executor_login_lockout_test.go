package menu

import (
	"sync"
	"testing"
	"time"
)

// lockDuringCheck has the next LOGIN password that checks out play another
// login from the same address locking it out before it is admitted.
func lockDuringCheck(t *testing.T, lock *fakeLockout) {
	t.Helper()
	prev := loginPasswordChecked
	t.Cleanup(func() { loginPasswordChecked = prev })
	loginPasswordChecked = func(ip string) {
		lock.mu.Lock()
		lock.lockedIP, lock.until = ip, time.Now().Add(5*time.Minute)
		lock.mu.Unlock()
	}
}

// A lockout another login starts while this password is being checked
// stays: the right password doesn't clear it or get in, and the call isn't
// recorded. RUN:AUTHENTICATE.
func TestAuthenticate_LockoutDuringCheckKeepsIt(t *testing.T) {
	env := newMenuEnv(t)
	addLoginUser(t, env, "Alice", "secret", 20)
	lock := newFakeLockout()
	env.e.IPLockoutCheck = lock
	lockDuringCheck(t, lock)

	r := env.runFrom("203.0.113.5", runAuthenticate, nil, "Alice\rsecret\r")
	if r.user != nil {
		t.Fatalf("logged in as %s from an IP locked out during the check", r.user.Handle)
	}
	if !r.has("Too many failed login attempts") {
		t.Errorf("no lockout message: %q", r.text())
	}
	if len(lock.cleared) != 0 {
		t.Errorf("the lockout was cleared: %v", lock.cleared)
	}
	if u, _ := env.um.GetUser("Alice"); u.TimesCalled != 0 || !u.LastLogin.IsZero() {
		t.Errorf("call recorded for a refused login: TimesCalled=%d LastLogin=%v", u.TimesCalled, u.LastLogin)
	}
}

// The same at the LOGIN screen's own prompt.
func TestExeccovLogin_LockoutDuringCheckKeepsIt(t *testing.T) {
	env, m, lock := execcovLoginEnv(t)
	m.write("ansi", "LOGIN.ANS", "LOGIN-SCREEN\r\nHandle: |P\r\nPassword: |O\r\n")
	lockDuringCheck(t, lock)

	r := execcovRun(env, execcovCall{start: "LOGIN", input: "alice\rsecret\r"})
	if r.user != nil || r.next != "LOGOFF" || !r.has("LOCKED 5 MIN") {
		t.Errorf("next=%q user=%s output:\n%s", r.next, execcovHandle(r.user), r.text())
	}
	if len(lock.cleared) != 0 {
		t.Errorf("the lockout was cleared: %v", lock.cleared)
	}
	if u, _ := env.um.GetUser("Alice"); u.TimesCalled != 0 || !u.LastLogin.IsZero() {
		t.Errorf("call recorded for a refused login: TimesCalled=%d LastLogin=%v", u.TimesCalled, u.LastLogin)
	}
}

// An account below the logon level is refused after its password checks
// out: the call isn't recorded and the IP's failures aren't cleared, so an
// account of one's own that can't log in doesn't reset the count.
func TestExeccovLogin_BelowLogonLevelNotRecorded(t *testing.T) {
	env, _, lock := execcovLoginEnv(t)
	r := execcovRun(env, execcovCall{start: "LOGIN", input: "lowly\rsecret\r"})
	if r.user != nil || !r.has("NO-ENTRY") {
		t.Fatalf("user=%s output:\n%s", execcovHandle(r.user), r.text())
	}
	if len(lock.cleared) != 0 {
		t.Errorf("refused login cleared the IP's failures: %v", lock.cleared)
	}
	if u, _ := env.um.GetUser("Lowly"); u.TimesCalled != 0 || !u.LastLogin.IsZero() {
		t.Errorf("call recorded for a refused login: TimesCalled=%d LastLogin=%v", u.TimesCalled, u.LastLogin)
	}
}

// Wrong passwords sent at the same moment by sessions from one IP are
// checked one at a time, so no more are tried than the lockout allows: the
// rest find the IP locked out.
func TestAuthenticate_ConcurrentGuessesFromOneIP(t *testing.T) {
	env := newMenuEnv(t)
	addLoginUser(t, env, "Alice", "secret", 20)
	lock := newFakeLockout()
	lock.max = 3
	env.e.IPLockoutCheck = lock

	const ip, sessions = "198.51.100.7", 10
	var start, wg sync.WaitGroup
	start.Add(1)
	for range sessions {
		ts := loginSession(t, env, ip, "Alice\rguess\r")
		c := &cmdCtx{
			e: env.e, s: ts, terminal: newTestTerminal(ts), userManager: env.um,
			nodeNumber: 1, sessionStartTime: time.Now(), outputMode: env.outputMode,
			termWidth: 80, termHeight: 24,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			_, _, _ = runAuthenticate(c, "")
		}()
	}
	start.Done()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(handlerWait):
		t.Fatalf("logins still running after %v", handlerWait)
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()
	if got := lock.failed[ip]; got != lock.max {
		t.Errorf("%d wrong passwords tried, want %d (the lockout's limit)", got, lock.max)
	}
}
