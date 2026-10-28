package menu

import (
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
)

// sessionLoginFailures counts each session's failed passwords at the login
// prompt. The count lasts the call, however many times the LOGIN menu runs,
// and is dropped on a successful login or when the call ends.
var sessionLoginFailures sync.Map // ssh.Session -> int

// niceTryArt is shown to a caller who has used up their password tries.
const niceTryArt = "NICETRY.ANS"

// niceTryPause is how long NICETRY.ANS stays up before the call is dropped.
var niceTryPause = 2 * time.Second

// loginFailed counts a failed password for s. It reports whether the call
// must end: the caller has used up their tries (config.json's maxLoginTries,
// 3 unless set), or the failure just locked out their IP. Then it has shown
// NICETRY.ANS, or the execLoginTooManyTries string when the art is missing,
// and closed the session; the caller returns io.EOF.
func (e *MenuExecutor) loginFailed(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int, lockedOut bool, termWidth, termHeight int) bool {
	n := 1
	if v, ok := sessionLoginFailures.Load(s); ok {
		n = v.(int) + 1
	}
	sessionLoginFailures.Store(s, n)
	tries := e.GetServerConfig().LoginTries()
	if n < tries && !lockedOut {
		return false
	}
	logging.Security("login tries used up, disconnecting",
		"node", nodeNumber, "ip", remoteIPFromSession(s), "failures", n, "tries", tries, "ip_locked_out", lockedOut)
	_, _ = terminal.Write([]byte(ansi.ClearScreen())) // best-effort display
	if err := e.displayFileIfPresent(terminal, niceTryArt, outputMode, termWidth, termHeight); err != nil {
		_ = e.writeString(terminal, outputMode, e.Strings().ExecLoginTooManyTries) // best-effort display
	}
	uiPause(niceTryPause)
	sessionLoginFailures.Delete(s)
	_ = s.Close()
	return true
}

// clearLoginFailures forgets s's failed passwords, after a successful login
// or when the call ends.
func clearLoginFailures(s ssh.Session) { sessionLoginFailures.Delete(s) }
