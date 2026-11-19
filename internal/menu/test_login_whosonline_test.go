package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// testLoginSeed registers an ordinary caller on node 1 and a test account
// on node 2, as main.go marks it at login, and returns a co-sysop viewer.
func testLoginSeed(env *menuEnv) (coSysOp *user.User) {
	g := *env.caller
	whoRegister(env, 1, &g, "Reading Messages", false)
	now := time.Now()
	env.e.SessionRegistry.Register(&session.BbsSession{
		NodeID:       2,
		User:         &user.User{ID: 20, Handle: "Robot", AccessLevel: 10, TestAccount: true},
		Activity:     "Deploy Check",
		Invisible:    true,
		TestLogin:    true,
		StartTime:    now,
		LastActivity: now,
	})
	co := *env.sysop
	co.ID, co.Handle = 3, "CoSy"
	co.AccessLevel = env.e.GetServerConfig().CoSysOpLevel
	return &co
}

// A test account is left out of Who's Online for every viewer below sysop
// level, co-sysops included; the sysop sees it. The ordinary caller is
// shown to all of them.
func TestWhoIsOnline_TestLoginShownOnlyToSysOp(t *testing.T) {
	env := newMenuEnv(t)
	co := testLoginSeed(env)
	for _, v := range []struct {
		name string
		u    *user.User
		see  bool
		want string
	}{
		{"caller", env.caller, false, "1 node(s) active"},
		{"co-sysop", co, false, "1 node(s) active"},
		{"sysop", env.sysop, true, "2 node(s) active"},
	} {
		r := env.runCmd("WHOISONLINE", v.u, "", "\r")
		if r.err != nil {
			t.Fatalf("%s: err = %v", v.name, r.err)
		}
		if !r.has("Reading Messages", v.want) {
			t.Errorf("%s: output:\n%s", v.name, r.text())
		}
		if got := r.has("Deploy Check"); got != v.see {
			t.Errorf("%s: test account shown = %v, want %v:\n%s", v.name, got, v.see, r.text())
		}
	}
}

// The login-sequence Who's Online step does not count a test account as
// someone else online for a co-sysop.
func TestLoginWhosOnline_TestLoginNotCounted(t *testing.T) {
	env := newMenuEnv(t)
	co := testLoginSeed(env)
	// Only the test account is on another node from the caller's node 1.
	env.e.SessionRegistry.Unregister(1)
	whoRegister(env, 1, co, "Logging on", false)
	r := env.run(runLoginWhosOnline, co, "", "Y\r")
	if r.err != nil || r.has("View users on other nodes?") {
		t.Errorf("co-sysop asked to view a test account: err = %v output:\n%s", r.err, r.text())
	}
}

// A test login's logon and logoff are not announced to other nodes; an
// ordinary caller's are.
func TestAnnounceSkipsTestLogin(t *testing.T) {
	e, from, u := announceFixture(t, true, true, 0)
	viewer := announceViewer(e, 2, 2, 255, true)
	e.AnnounceLogon(from, u)
	if got := viewer.DrainPages(); len(got) != 1 {
		t.Fatalf("ordinary logon: %q, want one announcement", got)
	}
	from.TestLogin = true
	e.AnnounceLogon(from, u)
	e.AnnounceLogoff(from, u)
	if got := viewer.DrainPages(); len(got) != 0 {
		t.Errorf("test login announced: %q", got)
	}
}

// A test login sends nothing to the V3Net activity feed, even when the
// account shares logons; an ordinary session that shares them does.
func TestSendCentralAlertSkipsTestLogin(t *testing.T) {
	f := newAlertFixture(t)
	sess, u := f.caller(t, 1, "Sharer", 30, user.CentralAlertPrefs{ShareLogons: true, ShareLogoffs: true}, true)
	f.e.SendCentralLogonAlert(f.um, sess, u)
	if len(f.fake.alerts) != 1 {
		t.Fatalf("ordinary logon sent %+v, want one alert", f.fake.alerts)
	}
	f.fake.alerts = nil
	sess.TestLogin = true
	f.e.SendCentralLogonAlert(f.um, sess, u)
	f.e.SendCentralLogoffAlert(f.um, sess, u)
	if len(f.fake.alerts) != 0 {
		t.Errorf("test login sent %+v", f.fake.alerts)
	}
}

// A co-sysop neither sees a test login in the page list nor reaches it; the
// sysop does both.
func TestPageTestLoginOnlyForSysOp(t *testing.T) {
	env := newMenuEnv(t)
	co := testLoginSeed(env) // node 1 the caller, node 2 the test account

	r := env.run(runPage, co, "", "2\rboo\r")
	if r.has("Robot") || r.has("Message:") || !r.has("That node is not online.") {
		t.Errorf("co-sysop saw or reached the test login:\n%s", r.text())
	}
	if pages, _ := env.e.toastStore().Peek(20); len(pages) != 0 {
		t.Errorf("test account got pages from a co-sysop: %q", pages)
	}
	r = env.run(runPage, env.sysop, "", "2\rboo\r")
	if !r.has("Node 2: Robot", "Page sent to Node 2.") {
		t.Errorf("sysop should see and reach the test login:\n%s", r.text())
	}
}
