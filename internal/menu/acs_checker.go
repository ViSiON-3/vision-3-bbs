package menu

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The ACS language itself (parsing, terms, evaluation) lives in internal/acs;
// this file connects it to the menu executor's sessions and sysop levels.

// acsSysOpLevel and acsCoSysOpLevel are the thresholds behind the SYSOP and
// COSYSOP keywords. They follow sysOpLevel and coSysOpLevel in config.json
// (set via SetServerConfig) so ACS strings and handlers agree on who counts
// as a sysop. Until a config is set they hold the stock defaults.
var (
	acsSysOpLevel   atomic.Int64
	acsCoSysOpLevel atomic.Int64
)

func init() {
	setACSSysOpLevels(config.DefaultSysOpLevel, config.DefaultCoSysOpLevel)
}

// setACSSysOpLevels updates the SYSOP and COSYSOP keyword thresholds. The
// caller passes sanitized levels (see ServerConfig.SanitizeAccessLevels).
func setACSSysOpLevels(sysOp, coSysOp int) {
	acsSysOpLevel.Store(int64(sysOp))
	acsCoSysOpLevel.Store(int64(coSysOp))
}

// acsSession adapts a caller's session to acs.Session. Each method reads the
// session only when a term asks for it.
type acsSession struct{ s ssh.Session }

func (a acsSession) RemoteAddr() net.Addr      { return a.s.RemoteAddr() }
func (a acsSession) ChatCredit() time.Duration { return chatCredit(a.s) }
func (a acsSession) HasPTY() bool {
	_, _, isPty := a.s.Pty()
	return isPty
}

// acsContext is the evaluation context for u in session s (nil for none),
// whose call began at startTime.
func acsContext(u *user.User, s ssh.Session, startTime time.Time) acs.Context {
	ctx := acs.Context{
		User:         u,
		Start:        startTime,
		SysOpLevel:   int(acsSysOpLevel.Load()),
		CoSysOpLevel: int(acsCoSysOpLevel.Load()),
	}
	if s != nil {
		ctx.Session = acsSession{s}
	}
	return ctx
}

// CheckUserACS evaluates an ACS string against a user without requiring a
// session or terminal. Use this for access checks outside of menu execution
// (e.g., selecting default areas on login).
func CheckUserACS(acsString string, u *user.User) bool {
	return checkACS(acsString, u, nil, nil, time.Now())
}

// checkACS evaluates a ViSiON/2 Access Control String (ACS) against user credentials.
// Returns true if the user meets the ACS requirements, false otherwise.
func checkACS(acsString string, u *user.User, s ssh.Session, _ *term.Terminal, startTime time.Time) bool {
	return acs.Check(acsString, acsContext(u, s, startTime))
}

// evaluateCondition evaluates a single ACS term (e.g., S50, L, Fx) for u.
func evaluateCondition(condition string, u *user.User, s ssh.Session, _ *term.Terminal, startTime time.Time) bool {
	return acs.EvalTerm(condition, acsContext(u, s, startTime))
}
