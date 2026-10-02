package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/sshserver"
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

	// If username matches a known BBS user and password is correct,
	// stash the authenticated user for auto-login. Otherwise accept
	// the connection anyway and let the BBS LOGIN menu handle it.
	if username != "" && userMgr != nil {
		if bbsUser, found := userMgr.GetUser(username); found && bbsUser != nil {
			authedUser, ok := userMgr.Authenticate(username, password)
			if ok {
				ctx.SetValue(sshAuthUserKey{}, authedUser)
				slog.Info("SSH pre-authenticated user", "user", username, "addr", ctx.RemoteAddr())
			} else {
				slog.Info("SSH password mismatch, deferring to BBS login", "user", username, "addr", ctx.RemoteAddr())
			}
		}
	}

	return true

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
