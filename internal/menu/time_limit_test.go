package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// timeLimitEnv is execcovRunEnv with the time limit strings pinned to markers.
func timeLimitEnv(t *testing.T) (*menuEnv, *execcovMenus) {
	t.Helper()
	env, m := execcovRunEnv(t)
	execcovStrings(env, func(s *config.StringsConfig) {
		s.TimeLimitExpired = "\r\nTIME-UP\r\n"
		s.TimeLimitWarning = "\r\nWARN=%d\r\n"
		s.IdleTimeout = "\r\nIDLE\r\n"
	})
	return env, m
}

// A caller whose time has run out is logged off before the next menu is drawn.
// A sysop with the same stored limit is not, since CoSysOps and above have no
// time limit.
func TestTimeLimit_ExpiredLogsOffAtMenu(t *testing.T) {
	env, _ := timeLimitEnv(t)
	started := time.Now().Add(-2 * time.Hour)

	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r", started: started})
	if r.next != "LOGOFF" || !r.has("TIME-UP") || r.has("MAIN-SCREEN") {
		t.Errorf("caller past their limit: next=%q output:\n%s", r.next, r.text())
	}

	r = execcovRun(env, execcovCall{user: env.sysop, start: "MAIN", input: "Q\r", started: started})
	if r.has("TIME-UP") || !r.has("MAIN-SCREEN") {
		t.Errorf("sysop was timed out:\n%s", r.text())
	}
}

// In the last minutes of their time a caller is warned at each menu prompt,
// with part of a minute counted as a whole one. Earlier they are not.
func TestTimeLimit_WarnsNearTheEnd(t *testing.T) {
	env, _ := timeLimitEnv(t)

	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r",
		started: time.Now().Add(-57*time.Minute - 30*time.Second)})
	if !r.has("WARN=3") || r.has("TIME-UP") {
		t.Errorf("2.5 minutes left: output:\n%s", r.text())
	}

	r = execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "Q\r",
		started: time.Now().Add(-10 * time.Minute)})
	if r.has("WARN=") {
		t.Errorf("50 minutes left but warned:\n%s", r.text())
	}
}

// A lightbar menu shows the warning on the bottom row, with its line breaks
// dropped so the fixed-position screen does not scroll, and puts the cursor
// back afterwards.
func TestTimeLimit_WarnsOnLightbarBottomRow(t *testing.T) {
	env, m := timeLimitEnv(t)
	m.menu("BARM", MenuRecord{}, "BARM-SCREEN\r\n", CommandRecord{Keys: "A", Command: "LOGOFF"})
	m.write("bar", "BARM.BAR", execcovBar)

	r := execcovRun(env, execcovCall{user: env.caller, start: "BARM", input: "A", height: 25,
		started: time.Now().Add(-57*time.Minute - 30*time.Second)})
	if want := "\x1b[s\x1b[25;1H\x1b[2KWARN=3"; !strings.Contains(r.raw, want) {
		t.Errorf("no bottom-row warning in %q", r.raw)
	}
	if strings.Contains(r.raw, "\r\nWARN=") || strings.Contains(r.raw, "WARN=3\r\n") {
		t.Errorf("warning kept its line breaks: %q", r.raw)
	}
	if !strings.Contains(r.raw, "\x1b[u") {
		t.Errorf("cursor not restored: %q", r.raw)
	}
}

// An input loop reports an expired time limit as an idle timeout, so the
// session's deadline decides which notice the caller sees.
func TestTimeLimit_SessionTimeoutNotice(t *testing.T) {
	env, _ := timeLimitEnv(t)
	for _, tc := range []struct {
		name     string
		deadline time.Time
		want     string
	}{
		{"deadline passed", time.Now().Add(-time.Second), "TIME-UP"},
		{"deadline ahead", time.Now().Add(time.Hour), "IDLE"},
		{"no deadline", time.Time{}, "IDLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestSession("")
			t.Cleanup(func() { ClearSessionIdleTimeout(ts); resetSessionIH(ts) })
			applySessionDeadline(ts, tc.deadline)
			env.e.handleSessionTimeout(ts, newTestTerminal(ts), env.outputMode, 1, 80, 24)
			out := ts.output()
			if !strings.Contains(out, tc.want) {
				t.Errorf("output %q, want %s", out, tc.want)
			}
		})
	}
}

// CoSysOps and above, users with no limit and callers not logged in have no
// time limit; everyone else has their own.
func TestTimeLimit_Effective(t *testing.T) {
	env := newMenuEnv(t)
	coSysOp := env.e.GetServerConfig().CoSysOpLevel
	start := time.Now()
	for _, tc := range []struct {
		name string
		u    *user.User
		want int
	}{
		{"not logged in", nil, 0},
		{"no limit", &user.User{AccessLevel: 10}, 0},
		{"caller", &user.User{AccessLevel: 10, TimeLimit: 45}, 45},
		{"cosysop", &user.User{AccessLevel: coSysOp, TimeLimit: 45}, 0},
	} {
		if got := env.e.timeLimit(tc.u); got != tc.want {
			t.Errorf("%s: timeLimit = %d, want %d", tc.name, got, tc.want)
		}
		deadline := env.e.sessionDeadline(tc.u, start)
		if (tc.want == 0) != deadline.IsZero() {
			t.Errorf("%s: sessionDeadline = %v with limit %d", tc.name, deadline, tc.want)
		}
	}

	// The ACS T check agrees: a sysop long past their stored limit still has
	// time left.
	sysop := &user.User{AccessLevel: 255, TimeLimit: 60}
	if !checkACS("T10", sysop, nil, nil, time.Now().Add(-2*time.Hour)) {
		t.Error("ACS T10 failed for a sysop")
	}
	caller := &user.User{AccessLevel: 10, TimeLimit: 60}
	if checkACS("T10", caller, nil, nil, time.Now().Add(-2*time.Hour)) {
		t.Error("ACS T10 passed for a caller past their limit")
	}
}
