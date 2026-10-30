package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func announceFixture(t *testing.T, logons, logoffs bool, level int) (*MenuExecutor, *session.BbsSession, *user.User) {
	t.Helper()
	e := &MenuExecutor{SessionRegistry: session.NewSessionRegistry()}
	e.SetServerConfig(config.ServerConfig{ToastLogons: logons, ToastLogoffs: logoffs, ToastAnnounceLevel: level})
	e.SetStrings(config.StringsConfig{ToastLogon: "on %s %d", ToastLogoff: "off %s %d"})
	u := &user.User{ID: 1, Handle: "Comer", AccessLevel: 10}
	from := &session.BbsSession{ID: 1, NodeID: 1, User: u}
	e.SessionRegistry.Register(from)
	return e, from, u
}

func announceViewer(e *MenuExecutor, node, userID, level int, ready bool) *session.BbsSession {
	s := &session.BbsSession{ID: node, NodeID: node, User: &user.User{ID: userID, AccessLevel: level}}
	s.SetToastsReady(ready)
	e.SessionRegistry.Register(s)
	return s
}

func TestAnnounceReachesOnlyAllowedCallers(t *testing.T) {
	e, from, u := announceFixture(t, true, true, 20)
	allowed := announceViewer(e, 2, 2, 20, true)
	lowLevel := announceViewer(e, 3, 3, 19, true)
	loggingIn := announceViewer(e, 4, 4, 255, false)
	sameAccount := announceViewer(e, 5, u.ID, 255, true)
	noUser := &session.BbsSession{ID: 6, NodeID: 6}
	noUser.SetToastsReady(true)
	e.SessionRegistry.Register(noUser)

	e.AnnounceLogon(from, u)

	if got := allowed.DrainPages(); len(got) != 1 || got[0] != "on Comer 1" {
		t.Errorf("allowed viewer got %q", got)
	}
	for name, s := range map[string]*session.BbsSession{
		"low level": lowLevel, "logging in": loggingIn, "same account": sameAccount, "no user": noUser, "self": from,
	} {
		if got := s.DrainPages(); len(got) != 0 {
			t.Errorf("%s got %q", name, got)
		}
	}
	e.AnnounceLogoff(from, u)
	if got := allowed.DrainPages(); len(got) != 1 || got[0] != "off Comer 1" {
		t.Errorf("allowed viewer got %q for logoff", got)
	}
}

func TestAnnounceSettings(t *testing.T) {
	e, from, u := announceFixture(t, false, true, 0)
	viewer := announceViewer(e, 2, 2, 0, true)
	e.AnnounceLogon(from, u)
	if got := viewer.DrainPages(); len(got) != 0 {
		t.Errorf("logon sent with it turned off: %q", got)
	}
	e.AnnounceLogoff(from, u)
	if got := viewer.DrainPages(); len(got) != 1 {
		t.Errorf("logoff not sent: %q", got)
	}

	e, from, u = announceFixture(t, true, false, 0)
	viewer = announceViewer(e, 2, 2, 0, true)
	e.AnnounceLogoff(from, u)
	if got := viewer.DrainPages(); len(got) != 0 {
		t.Errorf("logoff sent with it turned off: %q", got)
	}
}

func TestAnnounceSkipsInvisibleCaller(t *testing.T) {
	e, from, u := announceFixture(t, true, true, 0)
	viewer := announceViewer(e, 2, 2, 255, true)
	from.Invisible = true
	e.AnnounceLogon(from, u)
	e.AnnounceLogoff(from, u)
	if got := viewer.DrainPages(); len(got) != 0 {
		t.Errorf("invisible caller announced: %q", got)
	}
}
