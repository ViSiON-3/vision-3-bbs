package scripting

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// v3.nodes leaves a test login out for every viewer below sysop level, a
// co-sysop who sees invisible nodes included; a sysop sees it. Ordinary
// and invisible nodes are listed as before.
func TestNodesHideTestLoginBelowSysOp(t *testing.T) {
	reg, _ := newNodeRegistry()
	reg.Register(&session.BbsSession{NodeID: 6, User: &user.User{ID: 20, Handle: "Robot", TestAccount: true}, Activity: "Deploy check", LastActivity: time.Now(), Invisible: true, TestLogin: true})
	for _, v := range []struct {
		name         string
		level        int
		sysop        bool
		count, names string
	}{
		{"caller", 10, false, "3", "Alice,Tester,"},
		{"co-sysop", 250, false, "4", "Alice,Tester,Ghost,"},
		{"sysop", 255, true, "5", "Alice,Tester,Ghost,,Robot"},
		// A board whose sysop level is below 200: the sysop sees the test
		// login, though not the other invisible node.
		{"low sysop level", 150, true, "4", "Alice,Tester,,Robot"},
	} {
		h := newHarness(t, harnessOpts{
			providers: &Providers{SessionRegistry: reg},
			session:   func(sc *SessionContext) { sc.AccessLevel, sc.SysOp = v.level, v.sysop },
		})
		if got := h.eval(`String(v3.nodes.count())`).String(); got != v.count {
			t.Errorf("%s: count = %s, want %s", v.name, got, v.count)
		}
		if got := h.eval(`v3.nodes.list().map(function(n){return n.handle}).join()`).String(); got != v.names {
			t.Errorf("%s: list = %q, want %q", v.name, got, v.names)
		}
	}
}
