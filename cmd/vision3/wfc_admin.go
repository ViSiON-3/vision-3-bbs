package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// adminServer is the WFC admin server instance shared across all admin sessions.
var adminServer *admin.Server

// adminMinLevel is the minimum user access level required for WFC admin access.
var adminMinLevel int

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
		return false
	}
	if !authorizeAdmin(u.Handle) {
		slog.Info("wfc-admin: public key rejected, insufficient access level",
			"user", u.Handle, "level", u.AccessLevel, "required", adminMinLevel)
		return false
	}
	ctx.SetValue(wfcAdminHandleKey{}, u.Handle)
	slog.Info("wfc-admin: public key accepted", "user", u.Handle, "addr", ctx.RemoteAddr())
	return true
}

// authorizeAdmin returns true when the user identified by handle exists and
// has an access level >= adminMinLevel.
func authorizeAdmin(handle string) bool {
	if userMgr == nil {
		return false
	}
	u, found := userMgr.GetUser(handle)
	if !found || u == nil {
		return false
	}
	return u.AccessLevel >= adminMinLevel
}

// wfcAdminSubsystem handles an SSH "wfc-admin" subsystem session by serving
// the binary admin RPC protocol over the session stream. Access is re-checked
// against the stashed handle before any data is exchanged.
func wfcAdminSubsystem(sess ssh.Session) {
	handle, _ := sess.Context().Value(wfcAdminHandleKey{}).(string)
	if handle == "" || !authorizeAdmin(handle) {
		slog.Warn("wfc-admin: subsystem access denied", "user", handle, "addr", sess.RemoteAddr())
		fmt.Fprintf(sess, "access denied\n")
		return
	}

	slog.Info("wfc-admin: session opened", "user", handle, "addr", sess.RemoteAddr())

	audit := func(cmd string) {
		slog.Info("wfc-admin: command", "user", handle, "addr", sess.RemoteAddr(), "cmd", cmd)
	}

	if adminServer == nil {
		slog.Warn("wfc-admin: subsystem requested before admin server initialized", "remote", sess.RemoteAddr())
		fmt.Fprintf(sess, "server not ready\r\n")
		return
	}

	// ServeRPC's context governs only the internal subscriber goroutine; connection
	// lifetime is enforced by the SSH session closing, which unblocks the read loop.
	if err := admin.ServeRPC(context.Background(), sess, adminServer, audit); err != nil {
		slog.Info("wfc-admin: session closed", "user", handle, "addr", sess.RemoteAddr(), "reason", err)
	} else {
		slog.Info("wfc-admin: session closed", "user", handle, "addr", sess.RemoteAddr())
	}
}
