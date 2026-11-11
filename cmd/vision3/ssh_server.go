package main

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/sshserver"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// Context key for storing a pre-authenticated BBS user from SSH-level auth.
type sshAuthUserKey struct{}

// sshServerConfig is the production SSH server configuration.
func sshServerConfig(hostKeyPath, host string, port int, legacyAlgorithms bool) sshserver.Config {
	return sshserver.Config{
		HostKeyPath:                hostKeyPath,
		Host:                       host,
		Port:                       port,
		LegacySSHAlgorithms:        legacyAlgorithms,
		SessionHandler:             sshSessionHandler,
		Version:                    "Vision3",
		PasswordHandler:            sshPasswordHandler,
		KeyboardInteractiveHandler: sshKeyboardInteractiveHandler,
		PublicKeyHandler:           wfcPublicKeyHandler,
		VerifiedPublicKeyCallback:  wfcVerifiedKey,
		SubsystemHandlers: map[string]func(ssh.Session){
			"wfc-admin": wfcAdminSubsystem,
			"wfc-snoop": wfcSnoopSubsystem,
		},
	}
}

// startSSHServer creates, configures, and starts the pure-Go SSH server.
// Returns a cleanup function to shut down the server.
func startSSHServer(hostKeyPath, sshHost string, sshPort int, legacyAlgorithms bool) (func(), error) {
	slog.Info("configuring SSH server", "host", sshHost, "port", sshPort)

	server, err := sshserver.NewServer(sshServerConfig(hostKeyPath, sshHost, sshPort, legacyAlgorithms))
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH server: %w", err)
	}

	cleanup := func() {
		_ = server.Close() // best-effort shutdown
		sshserver.Cleanup()
	}

	// gliderlabs/ssh handles its own accept loop — run in background
	go func() {
		if err := server.ListenAndServe(); err != nil {
			slog.Error("SSH server error", "error", err)
		}
	}()

	slog.Info("SSH server ready", "host", sshHost, "port", sshPort)
	return cleanup, nil
}

func sshPasswordHandler(ctx ssh.Context, password string) bool {
	username := ctx.User()
	slog.Debug("SSH password auth", "user", username, "addr", ctx.RemoteAddr())

	// The connection is always accepted: a caller whose SSH login doesn't
	// pre-authenticate gets the BBS LOGIN menu instead.
	if authedUser := sshPreAuthenticate(userMgr, connectionTracker, username, password, extractIP(ctx.RemoteAddr())); authedUser != nil {
		ctx.SetValue(sshAuthUserKey{}, authedUser)
	}
	return true
}

// ipLocks hands out one lock per IP address. A lock is created when first
// wanted and dropped when nobody holds or waits for it, so the map only
// holds addresses with a login in progress.
type ipLocks struct {
	mu    sync.Mutex
	locks map[string]*ipLock
}

type ipLock struct {
	mu   sync.Mutex
	refs int // holders and waiters
}

// lock takes ip's lock, waiting for it if needed, and returns its unlock.
func (l *ipLocks) lock(ip string) (unlock func()) {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*ipLock)
	}
	k := l.locks[ip]
	if k == nil {
		k = &ipLock{}
		l.locks[ip] = k
	}
	k.refs++
	l.mu.Unlock()

	k.mu.Lock()
	return func() {
		k.mu.Unlock()
		l.mu.Lock()
		k.refs--
		if k.refs == 0 {
			delete(l.locks, ip)
		}
		l.mu.Unlock()
	}
}

// sshLoginLocks serializes SSH password attempts from one IP (see
// sshPreAuthenticate).
var sshLoginLocks ipLocks

// sshPasswordChecked runs after a password from ip checks out and before the
// lockout is checked again. It does nothing; tests set it to play another
// login locking ip out at that moment.
var sshPasswordChecked = func(ip string) {}

// sshPreAuthenticate checks an SSH login's username and password against
// the BBS users, for logging the caller straight in. It returns the user on
// a match and nil otherwise. It keeps the same per-IP failed-login lockout as
// the LOGIN menu (ct may be nil): a locked-out IP is not checked at all, a
// wrong password for a known user counts as a failed attempt, and a match
// clears the count, unless the IP was locked out while the password was
// being checked (ConnectionTracker.AdmitLogin). Attempts from one IP run one
// at a time (sshLoginLocks). An empty password isn't counted: clients that
// don't store one send it, and those callers just get the LOGIN menu.
func sshPreAuthenticate(um *user.UserMgr, ct *ConnectionTracker, username, password, ip string) *user.User {
	if username == "" || um == nil {
		return nil
	}
	if bbsUser, found := um.GetUser(username); !found || bbsUser == nil {
		return nil
	}
	// One attempt at a time per IP, from the lockout check to recording
	// its outcome. Otherwise connections opened together all pass the check
	// before any failure is recorded, and get more guesses than
	// maxFailedLogins allows. Other addresses aren't held up.
	unlock := sshLoginLocks.lock(ip)
	defer unlock()
	if ct != nil {
		if locked, lockedUntil, _ := ct.IsIPLockedOut(ip); locked {
			logging.Security("SSH login from locked IP, deferring to BBS login",
				"user", username, "ip", ip, "locked_until", lockedUntil.Format(time.RFC3339))
			return nil
		}
	}
	// Only the password here: the call is recorded (BeginSession) once the
	// login is admitted below, so a caller turned away isn't counted.
	if !um.VerifyPassword(username, password) {
		slog.Info("SSH password mismatch, deferring to BBS login", "user", username, "ip", ip)
		if ct != nil && password != "" {
			ct.RecordFailedLoginAttempt(ip)
		}
		return nil
	}
	sshPasswordChecked(ip)
	// The lockout is checked again as the count is cleared: another login
	// from ip may have locked it out while this password was being checked.
	if ct != nil && !ct.AdmitLogin(ip) {
		logging.Security("SSH login from IP locked out during the password check, deferring to BBS login",
			"user", username, "ip", ip)
		return nil
	}
	authedUser, ok := um.BeginSession(username)
	if !ok {
		return nil
	}
	slog.Info("SSH pre-authenticated user", "user", username, "ip", ip)
	return authedUser
}

func sshKeyboardInteractiveHandler(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) bool {
	slog.Debug("SSH keyboard-interactive auth", "user", ctx.User(), "addr", ctx.RemoteAddr())
	return true
}

func sshSessionHandler(sess ssh.Session) {
	// Wrap the session to add SetReadInterrupt support
	wrapped := sshserver.WrapSession(sess)

	// Atomically check limits and register connection
	canAccept, reason := connectionTracker.TryAccept(wrapped.RemoteAddr())
	if !canAccept {
		slog.Info("rejecting SSH connection", "addr", wrapped.RemoteAddr(), "reason", reason)
		_, _ = fmt.Fprintf(wrapped, "\r\nConnection rejected: %s\r\n", reason) // best-effort notice to client
		_, _ = fmt.Fprintf(wrapped, "Please try again later.\r\n")             // best-effort notice to client
		time.Sleep(2 * time.Second)
		return
	}

	// Connection is registered; ensure it's removed when done
	defer connectionTracker.RemoveConnection(wrapped.RemoteAddr())

	// Call the existing session handler with the wrapped session
	sessionHandler(wrapped)
}
