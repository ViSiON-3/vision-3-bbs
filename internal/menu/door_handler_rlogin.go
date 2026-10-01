package menu

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/rlogin"
)

// executeRLoginDoor bridges the caller's session to a door server over RLogin.
//
// Unlike the local door executors this one is not platform-specific: there is
// no process, PTY or dropfile involved, just a socket, so the same code serves
// Windows and Unix.
func executeRLoginDoor(ctx *DoorCtx) error {
	doorConfig := ctx.Config

	host := strings.TrimSpace(doorConfig.Host)
	if host == "" {
		return fmt.Errorf("door %q has no host configured", ctx.DoorName)
	}
	addr := rlogin.JoinHostPort(host, doorConfig.Port)

	disconnectKey, disconnectEnabled, err := config.ParseDisconnectKey(doorConfig.DisconnectKey)
	if err != nil {
		return fmt.Errorf("door %q: %w", ctx.DoorName, err)
	}

	handshake := rlogin.Handshake{
		ClientUser: substituteDoorPlaceholders(doorConfig.ClientUsername, ctx.Subs),
		ServerUser: substituteDoorPlaceholders(doorConfig.ServerUsername, ctx.Subs),
		TermType:   substituteDoorPlaceholders(doorConfig.TerminalType, ctx.Subs),
	}
	// The handshake fields identify the caller to the door server, so an unset
	// one defaults to the handle rather than to empty: an empty field is a
	// silent anonymous login on servers that do not reject it.
	if handshake.ClientUser == "" {
		handshake.ClientUser = ctx.User.Handle
	}
	if handshake.ServerUser == "" {
		handshake.ServerUser = ctx.User.Handle
	}
	if handshake.TermType == "" {
		handshake.TermType = "ANSI/" + ctx.BaudStr
	}

	deadline, timeout, expired := doorDeadline(
		ctx.User.TimeLimit, ctx.SessionStartTime, doorConfig.ConnectTimeout, time.Now())
	if expired {
		// Opening a connection only to drop it immediately wastes a slot and
		// litters the door server's log.
		slog.Info("not entering rlogin door, no time left",
			"node", ctx.NodeNumber, "door", ctx.DoorName)
		return nil
	}

	// The handshake fields can carry a shared password on servers that
	// authenticate that way, so they are not written to the default log. They
	// are the first thing to check when a door server rejects a caller, so
	// they stay available at debug level.
	slog.Info("connecting to rlogin door server",
		"node", ctx.NodeNumber, "door", ctx.DoorName, "addr", addr)
	slog.Debug("rlogin handshake fields",
		"node", ctx.NodeNumber, "door", ctx.DoorName,
		"serverUser", handshake.ServerUser, "termType", handshake.TermType)

	writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteConnecting, ctx.DoorName))

	// An idle caller abandons the connection attempt too.
	dialCtx, cancelDial := idleDialContext(ctx)
	conn, err := rlogin.Dial(dialCtx, addr, handshake, timeout)
	cancelDial()
	if err != nil {
		// The caller has already been told, in the sysop's own wording, that
		// the door server is unreachable. Returning the error as well would
		// have every caller of executeDoor print "Error running door ..." on
		// top of it, so a door server being down would read as two different
		// failures. The detail is in the log, which is where a sysop looks.
		slog.Warn("rlogin door connection failed",
			"node", ctx.NodeNumber, "door", ctx.DoorName, "addr", addr, "error", err)
		writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteConnectFailed, ctx.DoorName))
		return nil
	}

	// The reader strips the protocol's acknowledgement byte from the front of
	// the door's output.
	relayRemoteSession(ctx, conn, rlogin.NewAckReader(conn), disconnectKey, disconnectEnabled, deadline)

	writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteDisconnected, ctx.DoorName))
	runDoorCleanup(ctx)
	return nil
}
