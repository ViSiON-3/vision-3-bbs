package menu

import (
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// canLogonAtLevel reports whether an account at this access level is allowed to
// log in under the given configuration.
//
// Login is decided by access level alone: executor_login_auth.go denies a
// caller whose level is below logonLevel, and nothing consults the validated
// flag. Anything telling a user whether they can get on must ask this rather
// than inspecting validation, or it will say the opposite of what happens.
//
// A logonLevel of zero or below disables the check, so everyone passes.
func canLogonAtLevel(cfg config.ServerConfig, accessLevel int) bool {
	if cfg.LogonLevel <= 0 {
		return true
	}
	return accessLevel >= cfg.LogonLevel
}

// accessDeniedPause holds the access-denied message on screen before an SSH
// pre-authenticated caller is disconnected. A variable so tests need not wait.
var accessDeniedPause = time.Second

// AdmitPreAuthenticatedUser applies the post-password login checks to a caller
// whose password was already verified at the SSH layer, so that path skips
// the LOGIN prompt without also skipping what the prompt enforces: the
// required new-user intro gate, then the logon-level check.
//
// Returns admitted=false with io.EOF when the caller left the intro gate
// without sending, and admitted=false with a nil error when their level is too
// low to log on. Either way the session must end: falling back to the LOGIN
// prompt would authenticate the same account a second time, recording a
// second call for one connection, only to refuse it again.
func (e *MenuExecutor) AdmitPreAuthenticatedUser(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	u *user.User,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth, termHeight int,
) (admitted bool, err error) {
	if u == nil {
		return false, nil
	}

	if u.IntroPending {
		// No menu has run yet, so nothing has applied the idle timeout; without
		// it an idle caller would hold the node in the gate indefinitely. The
		// session handler applies the caller's own timeout before the login
		// sequence, and clears the stored value when the session ends.
		applySessionIdleTimeout(s, e.idleTimeout(nil))
		if proceed, _, gErr := e.runNewUserIntroGate(s, terminal, userManager, u, nodeNumber, outputMode, termWidth, termHeight); !proceed {
			return false, gErr
		}
	}

	if cfg := e.GetServerConfig(); !canLogonAtLevel(cfg, u.AccessLevel) {
		slog.Info("SSH pre-auth denied - insufficient access level", "node", nodeNumber, "handle", u.Handle, "has", u.AccessLevel, "needs", cfg.LogonLevel)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ExecAccessDenied)), outputMode)
		time.Sleep(accessDeniedPause)
		return false, nil
	}
	return true, nil
}
