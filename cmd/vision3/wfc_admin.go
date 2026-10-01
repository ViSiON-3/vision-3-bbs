package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// wfcReauthInterval is how often an open admin session re-checks that its
// authorization still holds (key not revoked, level not lowered, WFC not
// disabled). Revocation therefore takes effect within this window instead of
// only at the next connection. Tests shorten it.
var wfcReauthInterval = 30 * time.Second

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

// Connection permission extensions set by wfcVerifiedKey. They exist only
// on a connection whose client signed with a WFC admin key.
const (
	wfcHandleExt = "wfc-handle"
	wfcKeyExt    = "wfc-key" // marshaled public key
)

// wfcKeyAdmin returns the admin who owns key, or a reason for refusing it:
// "unregistered", "disabled" or "level".
func wfcKeyAdmin(key gossh.PublicKey) (*user.User, string) {
	if userMgr == nil {
		return nil, "unregistered"
	}
	u, found := userMgr.FindByAuthorizedKey(key.Marshal())
	if !found || u == nil {
		return nil, "unregistered"
	}
	if !authorizeAdmin(u.Handle) {
		if wfcEnabled == nil || !wfcEnabled() {
			return u, "disabled"
		}
		return u, "level"
	}
	return u, ""
}

// wfcVerifiedKey runs once the client has signed with a key that
// wfcPublicKeyHandler accepted. It resolves the admin from that key and
// records the identity in the connection's permissions, where the WFC
// subsystems read it. wfcPublicKeyHandler also runs for unsigned key
// queries, so nothing it sees can name the admin.
func wfcVerifiedKey(_ gossh.ConnMetadata, key gossh.PublicKey, perms *gossh.Permissions, _ string) (*gossh.Permissions, error) {
	u, deny := wfcKeyAdmin(key)
	if deny != "" {
		return nil, fmt.Errorf("wfc-admin: key no longer authorized (%s)", deny)
	}
	out := &gossh.Permissions{Extensions: map[string]string{}}
	if perms != nil {
		out.CriticalOptions = perms.CriticalOptions
		out.ExtraData = perms.ExtraData
		for k, v := range perms.Extensions {
			out.Extensions[k] = v
		}
	}
	out.Extensions[wfcHandleExt] = u.Handle
	out.Extensions[wfcKeyExt] = string(key.Marshal())
	return out, nil
}

// wfcVerifiedIdentity returns the admin handle and marshaled key recorded by
// wfcVerifiedKey, or empty values when the connection did not sign with an
// admin key.
func wfcVerifiedIdentity(ctx ssh.Context) (string, []byte) {
	conn, ok := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)
	if !ok || conn == nil || conn.Permissions == nil {
		return "", nil
	}
	handle, key := conn.Permissions.Extensions[wfcHandleExt], conn.Permissions.Extensions[wfcKeyExt]
	if handle == "" || key == "" {
		return "", nil
	}
	return handle, []byte(key)
}

// wfcPublicKeyHandler is the SSH-level public-key auth handler for admin
// clients. It accepts a key registered to a BBS user with sufficient access
// level. Other keys are refused so callers fall through to password auth.
// It also runs for unsigned key queries; the identity comes from
// wfcVerifiedKey.
func wfcPublicKeyHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	u, deny := wfcKeyAdmin(key)
	switch deny {
	case "":
		slog.Info("wfc-admin: public key accepted", "user", u.Handle, "addr", ctx.RemoteAddr())
		return true
	case "unregistered":
		// Debug level: unknown keys are routine (every non-WFC pubkey offer
		// lands here), but the fingerprint makes key-scanning visible when
		// debug logging is enabled.
		slog.Debug("wfc-admin: public key not registered",
			"fingerprint", gossh.FingerprintSHA256(key), "addr", ctx.RemoteAddr())
	case "disabled":
		slog.Info("wfc-admin: public key rejected, wfc access disabled",
			"user", u.Handle, "addr", ctx.RemoteAddr())
	default:
		minLevel := 0
		if adminMinLevel != nil {
			minLevel = adminMinLevel()
		}
		slog.Info("wfc-admin: public key rejected, insufficient access level",
			"user", u.Handle, "level", u.AccessLevel, "required", minLevel)
	}
	return false
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

// authorizeAdminKey reports whether the session opened by keyBytes is still
// authorized. Unlike authorizeAdmin it re-verifies the key itself, so removing
// the key — or soft-deleting the account, which the by-handle lookup does not
// catch — revokes access. handle must still own the key, guarding against a
// key that has been moved to another account mid-session.
func authorizeAdminKey(handle string, keyBytes []byte) bool {
	if userMgr == nil || adminMinLevel == nil || wfcEnabled == nil || !wfcEnabled() {
		return false
	}
	u, found := userMgr.FindByAuthorizedKey(keyBytes)
	if !found || u == nil {
		return false
	}
	if !strings.EqualFold(u.Handle, handle) {
		return false
	}
	return u.AccessLevel >= adminMinLevel()
}

// wfcReadOnly reports whether handle's WFC console is limited to watching.
// It reads the user record on every call, so a change applies to open
// sessions. An account that cannot be found is treated as read-only.
func wfcReadOnly(handle string) bool {
	if userMgr == nil {
		return true
	}
	u, found := userMgr.GetUser(handle)
	if !found || u == nil {
		return true
	}
	return u.WFCReadOnly
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
// the binary admin RPC protocol over the session stream. Access is checked
// against the admin and key the client signed with before any data is
// exchanged, and periodically for the life of the session.
func wfcAdminSubsystem(sess ssh.Session) {
	handle, keyBytes := wfcVerifiedIdentity(sess.Context())
	if handle == "" || !authorizeAdminKey(handle, keyBytes) {
		slog.Warn("wfc-admin: subsystem access denied", "user", handle, "addr", sess.RemoteAddr())
		_, _ = fmt.Fprintf(sess, "access denied\n") // best-effort notice to client
		return
	}

	slog.Info("wfc-admin: session opened", "user", handle, "addr", sess.RemoteAddr(), "readOnly", wfcReadOnly(handle))

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

	// A console that vanishes without closing (sleeping laptop, dropped
	// NAT mapping) would otherwise hold this goroutine, its subscriber, and
	// its snapshot writer until the kernel gives up on the TCP connection.
	// Keepalives bound that to roughly 25 seconds.
	if conn, ok := sess.Context().Value(ssh.ContextKeyConn).(*gossh.ServerConn); ok && conn != nil {
		stopKA := admin.KeepAlive(conn, admin.DefaultKeepAliveInterval, admin.DefaultKeepAliveTimeout, func(err error) {
			slog.Info("wfc-admin: console stopped responding, closing session",
				"user", handle, "addr", sess.RemoteAddr(), "reason", err)
			_ = sess.Close() // unblocks ServeRPC's read loop
			_ = conn.Close() // and tears down the dead transport
		})
		defer stopKA()
	}
	stillAuthorized := func(h string) bool { return authorizeAdminKey(h, keyBytes) }
	// The watcher reads the user record; it must not outlive the session.
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		watchAdminAuthorization(ctx, handle, wfcReauthInterval, stillAuthorized, func() {
			slog.Warn("wfc-admin: session revoked, disconnecting", "user", handle, "addr", sess.RemoteAddr())
			_ = sess.Close() // unblocks ServeRPC's read loop
		})
	}()
	defer func() {
		cancel()
		<-watchDone
	}()

	// ServeRPC's context governs only the internal subscriber goroutine; connection
	// lifetime is enforced by the SSH session closing, which unblocks the read loop.
	// The read-only flag is read for every command and snapshot.
	readOnly := func() bool { return wfcReadOnly(handle) }
	if err := admin.ServeRPC(ctx, sess, adminServer, handle, readOnly, audit); err != nil {
		slog.Info("wfc-admin: session closed", "user", handle, "addr", sess.RemoteAddr(), "reason", err)
	} else {
		slog.Info("wfc-admin: session closed", "user", handle, "addr", sess.RemoteAddr())
	}
}
