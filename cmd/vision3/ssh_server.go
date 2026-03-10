package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/stlalpha/vision3/internal/sshserver"
	gossh "golang.org/x/crypto/ssh"
)

// Context key for storing a pre-authenticated BBS user from SSH-level auth.
type sshAuthUserKey struct{}

// startSSHServer creates, configures, and starts the pure-Go SSH server.
// Returns a cleanup function to shut down the server.
func startSSHServer(hostKeyPath, sshHost string, sshPort int, legacyAlgorithms bool) (func(), error) {
	log.Printf("INFO: Configuring SSH server on %s:%d...", sshHost, sshPort)

	server, err := sshserver.NewServer(sshserver.Config{
		HostKeyPath:         hostKeyPath,
		Host:                sshHost,
		Port:                sshPort,
		LegacySSHAlgorithms: legacyAlgorithms,
		SessionHandler:      sshSessionHandler,
		Version:             "Vision3",
		PasswordHandler: func(ctx ssh.Context, password string) bool {
			username := ctx.User()
			log.Printf("DEBUG: SSH password auth from user=%q addr=%s", username, ctx.RemoteAddr())

			// If username matches a known BBS user and password is correct,
			// stash the authenticated user for auto-login. Otherwise accept
			// the connection anyway and let the BBS LOGIN menu handle it.
			if username != "" && userMgr != nil {
				if bbsUser, found := userMgr.GetUser(username); found && bbsUser != nil {
					authedUser, ok := userMgr.Authenticate(username, password)
					if ok {
						ctx.SetValue(sshAuthUserKey{}, authedUser)
						log.Printf("INFO: SSH pre-authenticated known user %q from %s", username, ctx.RemoteAddr())
					} else {
						log.Printf("INFO: SSH password mismatch for known user %q from %s, deferring to BBS login", username, ctx.RemoteAddr())
					}
				}
			}

			return true
		},
		KeyboardInteractiveHandler: func(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) bool {
			log.Printf("DEBUG: SSH keyboard-interactive auth from user=%q addr=%s", ctx.User(), ctx.RemoteAddr())
			return true
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH server: %w", err)
	}

	cleanup := func() {
		server.Close()
		sshserver.Cleanup()
	}

	// gliderlabs/ssh handles its own accept loop — run in background
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Printf("ERROR: SSH server error: %v", err)
		}
	}()

	log.Printf("INFO: SSH server ready - connect via: ssh <username>@%s -p %d", sshHost, sshPort)
	return cleanup, nil
}
func sshSessionHandler(sess ssh.Session) {
	// Wrap the session to add SetReadInterrupt support
	wrapped := sshserver.WrapSession(sess)

	// Atomically check limits and register connection
	canAccept, reason := connectionTracker.TryAccept(wrapped.RemoteAddr())
	if !canAccept {
		log.Printf("INFO: Rejecting SSH connection from %s: %s", wrapped.RemoteAddr(), reason)
		fmt.Fprintf(wrapped, "\r\nConnection rejected: %s\r\n", reason)
		fmt.Fprintf(wrapped, "Please try again later.\r\n")
		time.Sleep(2 * time.Second)
		return
	}

	// Connection is registered; ensure it's removed when done
	defer connectionTracker.RemoveConnection(wrapped.RemoteAddr())

	// Call the existing session handler with the wrapped session
	sessionHandler(wrapped)
}
