package menu

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
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
	// acsLocation is the board's timezone, for AGE and for the dates
	// screens show (boardTime), resolved once from config.json's timezone
	// when the config is set; nil until then.
	acsLocation atomic.Pointer[time.Location]
	// acsAreaScope is the message area PERM asks a scoped permission about
	// for a user: their current area, with its conference and sponsor. nil
	// until an executor is made, when PERM reads the tags on the record.
	acsAreaScope atomic.Pointer[func(*user.User) perm.Scope]
	// acsFileScope is acsAreaScope for permissions scoped to file areas:
	// the user's current file area, with its conference.
	acsFileScope atomic.Pointer[func(*user.User) perm.Scope]
)

// setACSAreaScope sets how acsContext finds a user's current message area.
func setACSAreaScope(f func(*user.User) perm.Scope) { acsAreaScope.Store(&f) }

// setACSFileScope sets how acsContext finds a user's current file area.
func setACSFileScope(f func(*user.User) perm.Scope) { acsFileScope.Store(&f) }

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
		Location:     acsLocation.Load(),
	}
	if s != nil {
		ctx.Session = acsSession{s}
	}
	if f := acsAreaScope.Load(); f != nil && u != nil {
		ctx.Scope = func() perm.Scope { return (*f)(u) }
	}
	if f := acsFileScope.Load(); f != nil && u != nil {
		ctx.FileScope = func() perm.Scope { return (*f)(u) }
	}
	return ctx
}

// CheckUserACS evaluates an ACS string against a user outside of menu
// execution (e.g., selecting default areas on login), in session s whose
// call began at startTime. Pass the session when there is one: without it
// (s nil) a string with L or A in it fails as a whole.
func CheckUserACS(acsString string, u *user.User, s ssh.Session, startTime time.Time) bool {
	return checkACS(acsString, u, s, nil, startTime)
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
