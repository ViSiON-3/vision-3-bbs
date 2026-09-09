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

	// nil userManager: this test is about paging online sessions only.
	if paged, _ := e.notifySysopsOfNewUser(nil, &user.User{Handle: "Newbie"}, 1); paged != 2 {
		t.Fatalf("paged %d sessions, want 2", paged)
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

	if paged, _ := e.notifySysopsOfNewUser(nil, &user.User{Handle: "Newbie"}, 1); paged != 0 {
		t.Errorf("paged %d sessions, want 0", paged)
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

	if paged, _ := e.notifySysopsOfNewUser(nil, &user.User{Handle: "Newbie"}, 1); paged != 0 {
		t.Errorf("paged %d sessions, want 0", paged)
	}
}

// Pre-auth sessions have no user record.
func TestNotifySkipsSessionsWithNoUser(t *testing.T) {
	preAuth := sess(2, -1)
	e := notifyFixture(t, true, preAuth)

	if paged, _ := e.notifySysopsOfNewUser(nil, &user.User{Handle: "Newbie"}, 1); paged != 0 {
		t.Errorf("paged %d sessions, want 0", paged)
	}
}

func TestNotifyRespectsTheConfigFlag(t *testing.T) {
	sysop := sess(2, 255)
	e := notifyFixture(t, false, sysop)

	if paged, _ := e.notifySysopsOfNewUser(nil, &user.User{Handle: "Newbie"}, 1); paged != 0 {
		t.Errorf("paged %d sessions with notifySysopNewUser off, want 0", paged)
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
		if paged, _ := e.notifySysopsOfNewUser(nil, newUser, 1); paged != 1 {
			t.Errorf("autoValidate=%v: paged %d sessions, want 1", autoValidate, paged)
		}
	}
}

// A blank string means no notice, not a blank line appearing at a sysop's
// prompt with no explanation.
func TestNotifySkipsWhenTheStringIsEmpty(t *testing.T) {
	sysop := sess(2, 255)
	e := notifyFixture(t, true, sysop)
	e.LoadedStrings.NewUserSysopPage = ""

	if paged, queued := e.notifySysopsOfNewUser(nil, &user.User{Handle: "Newbie"}, 1); paged != 0 || queued != 0 {
		t.Errorf("paged %d/queued %d with no configured string, want 0/0", paged, queued)
	}
	if pages := sysop.DrainPages(); len(pages) != 0 {
		t.Errorf("queued %q with no configured string", pages)
	}
}

// Nothing here may fail a signup.
func TestNotifyToleratesMissingPieces(t *testing.T) {
	e := notifyFixture(t, true)
	if paged, _ := e.notifySysopsOfNewUser(nil, nil, 1); paged != 0 {
		t.Errorf("nil user paged %d sessions", paged)
	}
	noReg := &MenuExecutor{ServerCfg: config.ServerConfig{NotifySysopNewUser: true}}
	if paged, _ := noReg.notifySysopsOfNewUser(nil, &user.User{Handle: "Newbie"}, 1); paged != 0 {
		t.Errorf("nil registry paged %d sessions", paged)
	}
}

// Offline sysops get a persistent notice queued for their next login, while
// online ones (paged) and non-sysops do not.
func TestNotifyQueuesForOfflineSysops(t *testing.T) {
	e := notifyFixture(t, true) // empty registry: nobody online
	e.ServerCfg.DataDir = t.TempDir()

	offlineSysop := &user.User{ID: 1, Handle: "SysOp", AccessLevel: 255}
	offlineCoSysop := &user.User{ID: 2, Handle: "Co", AccessLevel: 250}
	regular := &user.User{ID: 3, Handle: "Reg", AccessLevel: 25}
	deletedSysop := &user.User{ID: 4, Handle: "Gone", AccessLevel: 255, DeletedUser: true}
	um := user.NewUserMgrForTest(offlineSysop, offlineCoSysop, regular, deletedSysop)

	paged, queued := e.notifySysopsOfNewUser(um, &user.User{ID: 9, Handle: "Newbie"}, 1)
	if paged != 0 {
		t.Errorf("paged %d, want 0 (nobody online)", paged)
	}
	if queued != 2 {
		t.Fatalf("queued %d, want 2 (the two live sysop accounts)", queued)
	}

	path := sysopNoticesPath(e.ServerCfg.DataDir)
	for _, u := range []*user.User{offlineSysop, offlineCoSysop} {
		notices, err := drainSysopNotices(path, u.ID)
		if err != nil {
			t.Fatalf("drain for %s: %v", u.Handle, err)
		}
		if len(notices) != 1 || !strings.Contains(notices[0].Text, "Newbie") {
			t.Errorf("%s got %d notices (%v), want 1 naming the new user", u.Handle, len(notices), notices)
		}
	}
	for _, u := range []*user.User{regular, deletedSysop} {
		if notices, _ := drainSysopNotices(path, u.ID); len(notices) != 0 {
			t.Errorf("%s got a notice but should not have", u.Handle)
		}
	}
}

// An online sysop is paged, not also queued a login notice for the same event.
func TestNotifyDoesNotDoubleNotifyOnlineSysops(t *testing.T) {
	onlineSysop := sess(2, 255)
	onlineSysop.User.ID = 1
	e := notifyFixture(t, true, onlineSysop)
	e.ServerCfg.DataDir = t.TempDir()

	sysopAccount := &user.User{ID: 1, Handle: "SysOp", AccessLevel: 255}
	um := user.NewUserMgrForTest(sysopAccount)

	paged, queued := e.notifySysopsOfNewUser(um, &user.User{ID: 9, Handle: "Newbie"}, 1)
	if paged != 1 || queued != 0 {
		t.Fatalf("paged %d/queued %d, want 1/0 (paged online sysop must not also be queued)", paged, queued)
	}
	if notices, _ := drainSysopNotices(sysopNoticesPath(e.ServerCfg.DataDir), 1); len(notices) != 0 {
		t.Errorf("online sysop was also queued a login notice: %v", notices)
	}
}
