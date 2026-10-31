package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
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

// A handle is a caller's own text: it cannot act on the screens of those told
// about it, while the configured wording keeps its colours.
func TestAnnounceEscapesTheHandle(t *testing.T) {
	for _, handle := range []string{"Comer|CL", "Comer|04"} {
		e, from, u := announceFixture(t, true, true, 0)
		e.SetStrings(config.StringsConfig{ToastLogon: "|12on %s %d", ToastLogoff: "|12off %s %d"})
		u.Handle = handle
		viewer := announceViewer(e, 2, 2, 10, true)
		e.AnnounceLogon(from, u)
		e.AnnounceLogoff(from, u)
		pages := viewer.DrainPages()
		if len(pages) != 2 {
			t.Fatalf("pages = %q", pages)
		}
		colour := string(ansi.ReplacePipeCodes([]byte("|12")))
		for _, page := range pages {
			got := string(toastPipes(page))
			if !strings.HasPrefix(got, colour) || !strings.Contains(got, " "+handle+" ") {
				t.Errorf("%q rendered as %q, want the format's colour and the handle as typed", page, got)
			}
		}
	}
}

// A sysop's promotion or demotion reaches announcements from the refreshed
// record, not the one taken at login.
func TestAnnounceChecksTheViewersPublishedLevel(t *testing.T) {
	e, from, u := announceFixture(t, true, true, 20)
	demoted := announceViewer(e, 2, 2, 255, true)
	promoted := announceViewer(e, 3, 3, 10, true)
	demoted.SetToastViewer(10, 0)
	promoted.SetToastViewer(30, 0)

	e.AnnounceLogon(from, u)
	if got := demoted.DrainPages(); len(got) != 0 {
		t.Errorf("demoted caller told: %q", got)
	}
	if got := promoted.DrainPages(); len(got) != 1 {
		t.Errorf("promoted caller told: %q", got)
	}
}

// refreshCurrentUser is where the session learns its new level and width.
func TestRefreshPublishesTheCallersRecord(t *testing.T) {
	env := newMenuEnv(t)
	sess := chatcovNode(env, 1, env.caller)
	stored := *env.caller
	stored.AccessLevel = env.caller.AccessLevel + 7
	stored.ScreenWidth = 132 // Konfig saves here, and a refresh reads it back
	if err := env.um.UpdateUser(&stored); err != nil {
		t.Fatal(err)
	}
	mine := *env.caller
	st := &runLoopState{e: env.e, s: newTestSession(""), userManager: env.um, currentUser: &mine, nodeNumber: 1}
	if !st.refreshCurrentUser() {
		t.Fatal("refresh ended the call")
	}
	level, width, ok := sess.ToastViewer()
	if !ok || level != stored.AccessLevel || width != 132 {
		t.Errorf("ToastViewer = %d, %d, %v, want %d, 132", level, width, ok, stored.AccessLevel)
	}
	if env.caller.AccessLevel == level {
		t.Error("login record changed")
	}
}

// Width changed in Konfig centres the toast on the new width.
func TestToastCenterUsesThePublishedWidth(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	cfg := env.e.GetServerConfig()
	cfg.ToastCenter = true
	env.e.SetServerConfig(cfg)
	env.u.ScreenWidth = 80
	env.sess.Width = 120
	env.sess.SetToastViewer(0, 40)
	var out strings.Builder
	if _, err := env.e.drawToastOverlay(env.s, &out, env.sess, toast.Toast{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	// ">> x" is four cells, centered on 40 columns.
	if want := ansi.MoveCursor(24, 19); !strings.Contains(out.String(), want) {
		t.Errorf("overlay = %q, want it at %q", out.String(), want)
	}
}

func TestWrapToastAnchorFollowsAutowrap(t *testing.T) {
	raw := []byte(strings.Repeat("x", 81) + "~TO")
	res, err := ansi.ProcessAnsiAndExtractCoords(raw, ansi.OutputModeCP437)
	if err != nil {
		t.Fatal(err)
	}
	wrapToastAnchor(&res, raw, 80)
	if got := res.FieldCoords[toastAnchorCode]; got.Y != 2 || got.X != 2 {
		t.Errorf("anchor at %+v, want row 2, column 2", got)
	}
}
