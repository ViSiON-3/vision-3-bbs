package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// notifyFixture builds an executor with the notice enabled and a registry
// holding the given sessions, keyed by node.
func notifyFixture(t *testing.T, notify bool, sessions ...*session.BbsSession) *MenuExecutor {
	t.Helper()
	reg := session.NewSessionRegistry()
	for _, s := range sessions {
		reg.Register(s)
	}
	return &MenuExecutor{
		SessionRegistry: reg,
		ServerCfg: config.ServerConfig{
			SysOpLevel:         255,
			CoSysOpLevel:       250,
			NotifySysopNewUser: notify,
		},
		LoadedStrings: config.StringsConfig{
			NewUserSysopPage: "New user: %s signed up from node %d.",
		},
	}
}

func sess(node, level int) *session.BbsSession {
	s := &session.BbsSession{ID: node, NodeID: node}
	if level >= 0 {
		s.User = &user.User{ID: node, Handle: "u", AccessLevel: level}
	}
	return s
}

func TestNotifyPagesCoSysOpAndAbove(t *testing.T) {
	sysop := sess(2, 255)
	cosysop := sess(3, 250)
	e := notifyFixture(t, true, sysop, cosysop)

	if got := e.notifySysopsOfNewUser(&user.User{Handle: "Newbie"}, 1); got != 2 {
		t.Fatalf("paged %d sessions, want 2", got)
	}
	for _, s := range []*session.BbsSession{sysop, cosysop} {
		pages := s.DrainPages()
		if len(pages) != 1 {
			t.Fatalf("node %d got %d pages, want 1", s.NodeID, len(pages))
		}
		if !strings.Contains(pages[0], "Newbie") {
			t.Errorf("page %q does not name the new user", pages[0])
		}
		if !strings.Contains(pages[0], "node 1") {
			t.Errorf("page %q does not name the node they signed up on", pages[0])
		}
	}
}

func TestNotifySkipsRegularUsers(t *testing.T) {
	regular := sess(2, 25)
	e := notifyFixture(t, true, regular)

	if got := e.notifySysopsOfNewUser(&user.User{Handle: "Newbie"}, 1); got != 0 {
		t.Errorf("paged %d sessions, want 0", got)
	}
	if pages := regular.DrainPages(); len(pages) != 0 {
		t.Errorf("a regular user was told about the signup: %q", pages)
	}
}

// The signing-up node must not page itself. Its own arrival is not news to it,
// and mid-signup it has no user record to check a level against.
func TestNotifySkipsTheSigningUpNode(t *testing.T) {
	// A sysop-level record on the signup's own node is the strongest version
	// of this: level alone would let it through.
	own := sess(1, 255)
	e := notifyFixture(t, true, own)

	if got := e.notifySysopsOfNewUser(&user.User{Handle: "Newbie"}, 1); got != 0 {
		t.Errorf("paged %d sessions, want 0", got)
	}
}

// Pre-auth sessions have no user record.
func TestNotifySkipsSessionsWithNoUser(t *testing.T) {
	preAuth := sess(2, -1)
	e := notifyFixture(t, true, preAuth)

	if got := e.notifySysopsOfNewUser(&user.User{Handle: "Newbie"}, 1); got != 0 {
		t.Errorf("paged %d sessions, want 0", got)
	}
}

func TestNotifyRespectsTheConfigFlag(t *testing.T) {
	sysop := sess(2, 255)
	e := notifyFixture(t, false, sysop)

	if got := e.notifySysopsOfNewUser(&user.User{Handle: "Newbie"}, 1); got != 0 {
		t.Errorf("paged %d sessions with notifySysopNewUser off, want 0", got)
	}
}

// The notice is deliberately independent of AutoValidateNewUsers: an
// auto-validated signup raises nothing to review, but somebody joining is still
// worth knowing at the time it happens.
func TestNotifyFiresRegardlessOfAutoValidate(t *testing.T) {
	for _, autoValidate := range []bool{false, true} {
		sysop := sess(2, 255)
		e := notifyFixture(t, true, sysop)
		e.ServerCfg.AutoValidateNewUsers = autoValidate

		newUser := &user.User{Handle: "Newbie", Validated: autoValidate}
		if got := e.notifySysopsOfNewUser(newUser, 1); got != 1 {
			t.Errorf("autoValidate=%v: paged %d sessions, want 1", autoValidate, got)
		}
	}
}

// A blank string means no notice, not a blank line appearing at a sysop's
// prompt with no explanation.
func TestNotifySkipsWhenTheStringIsEmpty(t *testing.T) {
	sysop := sess(2, 255)
	e := notifyFixture(t, true, sysop)
	e.LoadedStrings.NewUserSysopPage = ""

	if got := e.notifySysopsOfNewUser(&user.User{Handle: "Newbie"}, 1); got != 0 {
		t.Errorf("paged %d sessions with no configured string, want 0", got)
	}
	if pages := sysop.DrainPages(); len(pages) != 0 {
		t.Errorf("queued %q with no configured string", pages)
	}
}

// Nothing here may fail a signup.
func TestNotifyToleratesMissingPieces(t *testing.T) {
	e := notifyFixture(t, true)
	if got := e.notifySysopsOfNewUser(nil, 1); got != 0 {
		t.Errorf("nil user paged %d sessions", got)
	}
	noReg := &MenuExecutor{ServerCfg: config.ServerConfig{NotifySysopNewUser: true}}
	if got := noReg.notifySysopsOfNewUser(&user.User{Handle: "Newbie"}, 1); got != 0 {
		t.Errorf("nil registry paged %d sessions", got)
	}
}
