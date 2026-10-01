package menu

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/telnetclient"
)

// executeTelnetDoor bridges the caller's session to a door server over Telnet.
//
// Like the RLogin executor it is not platform-specific: there is no process,
// PTY or dropfile involved, just a socket.
func executeTelnetDoor(ctx *DoorCtx) error {
	doorConfig := ctx.Config

	if strings.TrimSpace(doorConfig.Host) == "" {
		return fmt.Errorf("door %q has no host configured", ctx.DoorName)
	}
	addr := remoteDoorAddr(doorConfig)

	disconnectKey, disconnectEnabled, err := config.ParseDisconnectKey(doorConfig.DisconnectKey)
	if err != nil {
		return fmt.Errorf("door %q: %w", ctx.DoorName, err)
	}

	opts := telnetclient.Options{
		TermType: substituteDoorPlaceholders(doorConfig.TerminalType, ctx.Subs),
		// The caller's stored screen size, as the local doors use. A size
		// they never set is left for the client to default.
		Width:  ctx.User.ScreenWidth,
		Height: ctx.User.ScreenHeight,
		Send:   expandSendOnConnect(doorConfig.SendOnConnect, ctx.Subs),
		Raw:    doorConfig.RawTCP,
	}

	deadline, timeout, expired := doorDeadline(
		ctx.User.TimeLimit, ctx.SessionStartTime, doorConfig.ConnectTimeout, time.Now(),
		chatCredit(ctx.Session))
	if expired {
		// Opening a connection only to drop it immediately wastes a slot and
		// litters the door server's log.
		slog.Info("not entering telnet door, no time left",
			"node", ctx.NodeNumber, "door", ctx.DoorName)
		return nil
	}

	// What is sent on connect is normally a login, so it is not written to
	// the log at any level: unlike the rlogin handshake it is free text, with
	// no field that is known to be safe to show.
	slog.Info("connecting to telnet door server",
		"node", ctx.NodeNumber, "door", ctx.DoorName, "addr", addr, "raw", opts.Raw)

	writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteConnecting, ctx.DoorName))

	conn, err := telnetclient.Dial(context.Background(), addr, opts, timeout)
	if err != nil {
		// As for rlogin: the caller has been told in the sysop's own wording,
		// and returning the error too would print a second, different
		// failure on top of it.
		slog.Warn("telnet door connection failed",
			"node", ctx.NodeNumber, "door", ctx.DoorName, "addr", addr, "error", err)
		writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteConnectFailed, ctx.DoorName))
		return nil
	}

	// The connection takes the protocol out of what it reads, so the door's
	// output is read from it directly.
	relayRemoteSession(ctx, conn, conn, disconnectKey, disconnectEnabled, deadline)

	writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteDisconnected, ctx.DoorName))
	runDoorCleanup(ctx)
	return nil
}

// expandSendOnConnect fills the placeholders in a door's send_on_connect.
//
// It does not go through substituteDoorPlaceholders, which reports a value of
// nothing but whitespace as empty. Here that value is a real one: a bare
// carriage return is what gets a server past its "press Enter" prompt.
func expandSendOnConnect(value string, subs map[string]string) string {
	for key, val := range subs {
		value = strings.ReplaceAll(value, key, val)
	}
	return value
}
