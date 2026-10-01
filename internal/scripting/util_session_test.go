package scripting

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestUtilFunctions table-tests the pure helpers on v3.util.
func TestUtilFunctions(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	tests := []struct{ expr, want string }{
		{`v3.util.padRight("ab", 5)`, "ab   "},
		{`v3.util.padRight("ab", 5, "*")`, "ab***"},
		{`v3.util.padRight("ab", 5, "")`, "ab   "},
		{`v3.util.padRight("ab")`, ""},
		{`v3.util.padLeft("ab", 5)`, "   ab"},
		{`v3.util.padLeft("ab", 5, "0")`, "000ab"},
		{`v3.util.padLeft("ab", 5, "")`, "   ab"},
		{`v3.util.padLeft("abcdef", 3)`, "abcdef"},
		{`v3.util.padLeft("ab")`, ""},
		{`v3.util.center("ab", 6)`, "  ab  "},
		{`v3.util.center("ab")`, ""},
		{`v3.util.stripAnsi("\x1b[1;31mred\x1b[0m")`, "red"},
		{`v3.util.stripAnsi()`, ""},
		{`v3.util.stripPipe("|09hi|07!")`, "hi!"},
		{`v3.util.stripPipe()`, ""},
		{`String(v3.util.displayLen("|09hi\x1b[0m"))`, "2"},
		{`String(v3.util.displayLen())`, "0"},
		{`String(v3.util.random())`, "0"},
		{`String(v3.util.random(0))`, "0"},
		{`String(v3.util.random(-4))`, "0"},
		{`String(v3.util.random(1))`, "0"},
		{`String(v3.util.sleep())`, "undefined"},
		{`String(v3.util.sleep(0))`, "undefined"},
		{`String(v3.util.sleep(1))`, "undefined"},
		{`v3.util.date("2006") === String(new Date().getFullYear())`, "true"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}

	for i := 0; i < 50; i++ {
		if n := h.eval(`v3.util.random(5)`).ToInteger(); n < 0 || n >= 5 {
			t.Fatalf("random(5) = %d, out of [0,5)", n)
		}
	}
	now := time.Now().Unix()
	if got := h.eval(`v3.util.time()`).ToInteger(); got < now-2 || got > now+2 {
		t.Errorf("time() = %d, want ~%d", got, now)
	}
	if got := h.eval(`v3.util.date()`).String(); len(got) != len("2006-01-02 15:04:05") {
		t.Errorf("date() = %q, want default layout", got)
	}
}

// TestSessionObject checks v3.session and v3.args reflect the session
// context the BBS passed in.
func TestSessionObject(t *testing.T) {
	start := time.Now().Add(-10 * time.Minute)
	h := newHarness(t, harnessOpts{
		args:    []string{"one", "two"},
		session: func(sc *SessionContext) { sc.SessionStartTime = start; sc.TimeLimit = 30 },
	})
	tests := []struct{ expr, want string }{
		{`String(v3.session.node)`, "3"},
		{`String(v3.session.startTime)`, strconv.FormatInt(start.Unix(), 10)},
		{`v3.session.bbs.name + "/" + v3.session.bbs.sysop + "/" + v3.session.bbs.version`, "Test BBS/Sysop/3.0-test"},
		{`[v3.session.user.id, v3.session.user.handle, v3.session.user.realName, v3.session.user.accessLevel,
		   v3.session.user.timesCalled, v3.session.user.location, v3.session.user.screenWidth,
		   v3.session.user.screenHeight].join("|")`, "7|Tester|Test User|50|12|Testville|80|24"},
		{`String(v3.session.online)`, "true"},
		{`v3.args.join(",")`, "one,two"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
	// 30-minute limit, 10 used: ~20 minutes left.
	if left := h.eval(`v3.session.timeLeft`).ToInteger(); left < 19*60 || left > 20*60 {
		t.Errorf("timeLeft = %d, want ~1200", left)
	}
}

// TestSessionTimeLeftEdges: no limit reports a steady hour, however long the
// caller has been on; an overrun limit floors at zero.
func TestSessionTimeLeftEdges(t *testing.T) {
	h := newHarness(t, harnessOpts{session: func(sc *SessionContext) {
		sc.TimeLimit = 0
		sc.SessionStartTime = time.Now().Add(-2 * time.Hour)
	}})
	if left := h.eval(`v3.session.timeLeft`).ToInteger(); left != 3600 {
		t.Errorf("no-limit timeLeft = %d, want 3600", left)
	}
	h2 := newHarness(t, harnessOpts{session: func(sc *SessionContext) {
		sc.TimeLimit = 5
		sc.SessionStartTime = time.Now().Add(-time.Hour)
	}})
	if left := h2.eval(`v3.session.timeLeft`).ToInteger(); left != 0 {
		t.Errorf("overrun timeLeft = %d, want 0", left)
	}
	if got := h2.eval(`v3.args.length`).ToInteger(); got != 0 {
		t.Errorf("args.length = %d with no args", got)
	}
}

// TestSessionOnlineFalseAfterCancel: online reflects engine cancellation.
func TestSessionOnlineFalseAfterCancel(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.eng.cancel()
	<-h.eng.ctx.Done()
	// The VM is interrupted on cancel; clear it so we can read the property.
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.eng.vm.ClearInterrupt()
		v, err := h.eng.vm.RunString(`v3.session.online`)
		if err == nil {
			if v.ToBoolean() {
				t.Fatal("online = true after cancel")
			}
			return
		}
		if !strings.Contains(err.Error(), "terminated") || time.Now().After(deadline) {
			t.Fatalf("reading online: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSessionTimeLeftIncludesChatCredit: sysop chat time is not charged, so
// timeLeft grows by the credit, read live.
func TestSessionTimeLeftIncludesChatCredit(t *testing.T) {
	credit := 0 * time.Minute
	h := newHarness(t, harnessOpts{session: func(sc *SessionContext) {
		sc.TimeLimit = 5
		sc.SessionStartTime = time.Now().Add(-4 * time.Minute)
		sc.ChatCredit = func() time.Duration { return credit }
	}})
	if left := h.eval(`v3.session.timeLeft`).ToInteger(); left > 60 {
		t.Fatalf("timeLeft = %d before credit, want <= 60", left)
	}
	credit = 10 * time.Minute
	if left := h.eval(`v3.session.timeLeft`).ToInteger(); left < 10*60 || left > 11*60 {
		t.Errorf("timeLeft = %d with credit, want ~660", left)
	}
}
