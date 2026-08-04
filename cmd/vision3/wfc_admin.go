package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// wfcReauthInterval is how often an open admin session re-checks that its
// authorization still holds (key not revoked, level not lowered, WFC not
// disabled). Revocation therefore takes effect within this window instead of
// only at the next connection.
const wfcReauthInterval = 30 * time.Second

// adminServer is the WFC admin server instance shared across all admin sessions.
var adminServer *admin.Server

// adminMinLevel returns the minimum user access level required for WFC admin
// access. It is a live getter so that config hot-reloads take effect without
// a restart. Set once at startup; tests may replace it with a stub.
var adminMinLevel func() int

// wfcEnabled reports whether remote WFC admin access is allowed at all.
// Like adminMinLevel it is a live getter so config hot-reloads take effect
// without a restart. Nil (not yet wired, or a test that left it unset) denies.
var wfcEnabled func() bool

// wfcAdminHandleKey is the context key used to stash the admin handle during
// public-key authentication so wfcAdminSubsystem can re-verify it.
type wfcAdminHandleKey struct{}

// wfcPublicKeyHandler is the SSH-level public-key auth handler for admin clients.
// If the key is registered to a BBS user with sufficient access level, the
// handle is stashed in the context and the function returns true (allowing the
// connection). Otherwise it returns false so non-admin keys fall through to the
// normal caller login flow via password auth.
func wfcPublicKeyHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	if userMgr == nil {
		return false
	}
	u, found := userMgr.FindByAuthorizedKey(key.Marshal())
	if !found || u == nil {
		// Debug level: unknown keys are routine (every non-WFC pubkey offer
		// lands here), but the fingerprint makes key-scanning visible when
		// debug logging is enabled.
		slog.Debug("wfc-admin: public key not registered",
			"fingerprint", gossh.FingerprintSHA256(key), "addr", ctx.RemoteAddr())
		return false
	}
	if !authorizeAdmin(u.Handle) {
		if wfcEnabled == nil || !wfcEnabled() {
			slog.Info("wfc-admin: public key rejected, wfc access disabled",
				"user", u.Handle, "addr", ctx.RemoteAddr())
			return false
		}
		minLevel := 0
		if adminMinLevel != nil {
			minLevel = adminMinLevel()
		}
		slog.Info("wfc-admin: public key rejected, insufficient access level",
			"user", u.Handle, "level", u.AccessLevel, "required", minLevel)
		return false
	}
	ctx.SetValue(wfcAdminHandleKey{}, u.Handle)
	slog.Info("wfc-admin: public key accepted", "user", u.Handle, "addr", ctx.RemoteAddr())
	return true
}

// authorizeAdmin returns true when WFC admin access is enabled and the user
// identified by handle exists with an access level >= the live adminMinLevel
// threshold. It denies access if either live getter is nil (daemon not yet
// initialised or running in a test that deliberately left it unset).
func authorizeAdmin(handle string) bool {
	if userMgr == nil || adminMinLevel == nil || wfcEnabled == nil || !wfcEnabled() {
		return false
	}
	u, found := userMgr.GetUser(handle)
	if !found || u == nil {
		return false
	}
	return u.AccessLevel >= adminMinLevel()
}

// watchAdminAuthorization re-checks authorized(handle) every interval and
// calls kick once when it stops holding. It exits on ctx cancellation (normal
// session end) without kicking.
func watchAdminAuthorization(ctx context.Context, handle string, interval time.Duration, authorized func(string) bool, kick func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !authorized(handle) {
				kick()
				return
			}
		}
	}
}

// wfcAdminSubsystem handles an SSH "wfc-admin" subsystem session by serving
// the binary admin RPC protocol over the session stream. Access is re-checked
// against the stashed handle before any data is exchanged.
func wfcAdminSubsystem(sess ssh.Session) {
	handle, _ := sess.Context().Value(wfcAdminHandleKey{}).(string)
	if handle == "" || !authorizeAdmin(handle) {
		slog.Warn("wfc-admin: subsystem access denied", "user", handle, "addr", sess.RemoteAddr())
		_, _ = fmt.Fprintf(sess, "access denied\n") // best-effort notice to client
		return
	}

	slog.Info("wfc-admin: session opened", "user", handle, "addr", sess.RemoteAddr())

	audit := func(cmd string) {
		slog.Info("wfc-admin: command", "user", handle, "addr", sess.RemoteAddr(), "cmd", cmd)
	}

	if adminServer == nil {
		slog.Warn("wfc-admin: subsystem requested before admin server initialized", "remote", sess.RemoteAddr())
		_, _ = fmt.Fprintf(sess, "server not ready\r\n") // best-effort notice to client
		return
	}

	// Re-check authorization for the lifetime of the session so mid-session
	// revocation (key removed, level lowered, WFC disabled) kicks the client.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchAdminAuthorization(ctx, handle, wfcReauthInterval, authorizeAdmin, func() {
		slog.Warn("wfc-admin: session revoked, disconnecting", "user", handle, "addr", sess.RemoteAddr())
		_ = sess.Close() // unblocks ServeRPC's read loop
	})

	// ServeRPC's context governs only the internal subscriber goroutine; connection
	// lifetime is enforced by the SSH session closing, which unblocks the read loop.
	if err := admin.ServeRPC(ctx, sess, adminServer, audit); err != nil {
		slog.Info("wfc-admin: session closed", "user", handle, "addr", sess.RemoteAddr(), "reason", err)
	} else {
		slog.Info("wfc-admin: session closed", "user", handle, "addr", sess.RemoteAddr())
	}
}
