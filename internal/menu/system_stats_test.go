package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestSystemStats_ReportsBoardTotals pins SYSTEMSTATS against seeded data:
// board name, the sysop's handle (User #1), user and call totals, and active
// nodes out of the configured maximum.
func TestSystemStats_ReportsBoardTotals(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(&user.User{ID: 3, Handle: "Third", AccessLevel: 10, Validated: true})
	for i := 0; i < 4; i++ {
		env.um.AddCallRecord(user.CallRecord{UserID: 2, Handle: "Caller", NodeID: 1, ConnectTime: time.Now()})
	}
	whoRegister(env, 1, env.caller, "", false)
	whoRegister(env, 2, env.sysop, "", false)

	r := env.runCmd("SYSTEMSTATS", env.caller, "", "\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	cfg := env.e.GetServerConfig()
	for _, want := range []string{
		"System Statistics",
		"BBS Name:       " + cfg.BoardName,
		"SysOp:          Sysop",
		"Total Users:    3",
		"Total Calls:    4",
		"Active Nodes:   2 / 10",
	} {
		if !r.has(want) {
			t.Errorf("output lacks %q:\n%s", want, r.text())
		}
	}
	if strings.Contains(r.text(), "@TOTALUSERS@") {
		t.Error("template token left unsubstituted")
	}
}

// TestSystemStats_RequiresLogin pins that the screen is not drawn for a
// session with no user.
func TestSystemStats_RequiresLogin(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("SYSTEMSTATS", nil, "", "\r"); r.raw != "" || r.err != nil {
		t.Errorf("err = %v output: %q", r.err, r.text())
	}
}
