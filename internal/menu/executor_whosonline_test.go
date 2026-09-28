package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// whoRegister adds a session on node to the env's registry.
func whoRegister(env *menuEnv, node int, u *user.User, activity string, invisible bool) {
	now := time.Now()
	env.e.SessionRegistry.Register(&session.BbsSession{
		NodeID:       node,
		User:         u,
		Activity:     activity,
		Invisible:    invisible,
		StartTime:    now.Add(-75 * time.Minute),
		LastActivity: now,
	})
}

// whoSeed registers a visible caller, an invisible sysop and a caller still
// at the login prompt.
func whoSeed(env *menuEnv) {
	g := *env.caller
	g.GroupLocation = "Steel City"
	whoRegister(env, 1, &g, "Reading Messages", false)
	whoRegister(env, 2, env.sysop, "Lurking", true)
	whoRegister(env, 3, nil, "", false)
}

// TestWhoIsOnline_RegularCallerSkipsInvisible pins the Who's Online list for
// a regular caller: visible sessions show handle, level, location and
// activity, pre-login nodes show "Logging In...", the invisible sysop is
// omitted, and the footer counts only what was shown.
func TestWhoIsOnline_RegularCallerSkipsInvisible(t *testing.T) {
	env := newMenuEnv(t)
	whoSeed(env)
	r := env.runCmd("WHOISONLINE", env.caller, "", "\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Who's Online", "Caller", "Steel City", "Reading Messages", "Logging In...", "2 node(s) active") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.has("Lurking") {
		t.Errorf("invisible session shown to a regular caller:\n%s", r.text())
	}
	if r.user != env.caller {
		t.Errorf("returned user = %v, want the caller", r.user)
	}
}

// TestWhoIsOnline_CoSysOpSeesInvisible pins that a CoSysOp-or-above viewer
// sees invisible sessions and they are counted.
func TestWhoIsOnline_CoSysOpSeesInvisible(t *testing.T) {
	env := newMenuEnv(t)
	whoSeed(env)
	r := env.runCmd("WHOISONLINE", env.sysop, "", "\r")
	if !r.has("Lurking", "3 node(s) active") {
		t.Errorf("output:\n%s", r.text())
	}
	// Fixed-width tokens: the handle column is 20 wide.
	if !strings.Contains(r.text(), "Sysop               ") {
		t.Errorf("handle not padded to 20 columns:\n%s", r.text())
	}
}

// TestWhoIsOnline_DisconnectAtPauseLogsOff pins that EOF at the pause is a
// logoff.
func TestWhoIsOnline_DisconnectAtPauseLogsOff(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("WHOISONLINE", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("next = %q, want LOGOFF", r.next)
	}
}

// TestLoginWhosOnline_SkipsWhenAlone pins that the login step asks nothing
// when the only other sessions are pre-login or invisible to this caller.
func TestLoginWhosOnline_SkipsWhenAlone(t *testing.T) {
	env := newMenuEnv(t)
	whoRegister(env, 1, env.caller, "Logging on", false) // our own node
	whoRegister(env, 2, env.sysop, "Lurking", true)
	whoRegister(env, 3, nil, "", false)
	r := env.run(runLoginWhosOnline, env.caller, "", "Y\r")
	if r.err != nil || strings.TrimSpace(r.text()) != "" {
		t.Errorf("err = %v output: %q", r.err, r.text())
	}
	if r.user != env.caller {
		t.Errorf("returned user = %v", r.user)
	}
}

// TestLoginWhosOnline_PromptsAndShows pins the login prompt: No moves on
// without the list, Yes shows the list of other nodes.
func TestLoginWhosOnline_PromptsAndShows(t *testing.T) {
	env := newMenuEnv(t)
	whoRegister(env, 1, env.caller, "Logging on", false)
	whoRegister(env, 2, env.sysop, "Posting", false)

	r := env.run(runLoginWhosOnline, env.caller, "", "N")
	if !r.has("View users on other nodes?") || r.has("node(s) active") {
		t.Errorf("declined output:\n%s", r.text())
	}
	r = env.run(runLoginWhosOnline, env.caller, "", "Y\r")
	if !r.has("Posting", "2 node(s) active") {
		t.Errorf("accepted output:\n%s", r.text())
	}
	if r := env.run(runLoginWhosOnline, env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect at prompt: next = %q, want LOGOFF", r.next)
	}
}
