package menu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/rlogin"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
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

	// A door must not outlive the caller's time limit, and a caller with none
	// left should never reach the door server at all: opening a connection
	// only to drop it immediately wastes a slot and litters the far end's log.
	// TimeLimit <= 0 means unlimited, matching how the rest of the BBS reads
	// the field.
	var remaining time.Duration
	if ctx.User.TimeLimit > 0 {
		remaining = time.Duration(ctx.User.TimeLimit)*time.Minute - time.Since(ctx.SessionStartTime)
		if remaining <= 0 {
			slog.Info("not entering rlogin door, no time left",
				"node", ctx.NodeNumber, "door", ctx.DoorName)
			return nil
		}
	}

	timeout := time.Duration(doorConfig.ConnectTimeout) * time.Second

	slog.Info("connecting to rlogin door server",
		"node", ctx.NodeNumber, "door", ctx.DoorName, "addr", addr,
		"serverUser", handshake.ServerUser, "termType", handshake.TermType)

	writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteConnecting, ctx.DoorName))

	conn, err := rlogin.Dial(context.Background(), addr, handshake, timeout)
	if err != nil {
		slog.Warn("rlogin door connection failed",
			"node", ctx.NodeNumber, "door", ctx.DoorName, "addr", addr, "error", err)
		writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteConnectFailed, ctx.DoorName))
		return err
	}

	relayRLoginSession(ctx, conn, disconnectKey, disconnectEnabled, remaining)

	writeDoorMessage(ctx, fmt.Sprintf(ctx.Executor.Strings().DoorRemoteDisconnected, ctx.DoorName))
	runDoorCleanup(ctx)
	return nil
}

// relayRLoginSession pipes bytes between the caller and the door server until
// one end goes away, the user's time runs out, or the user presses the
// disconnect key. It closes conn before returning.
//
// A remaining of zero means the caller has no time limit, so the session lasts
// as long as the door server keeps it.
func relayRLoginSession(ctx *DoorCtx, conn net.Conn, disconnectKey byte, disconnectEnabled bool, remaining time.Duration) {
	// Closing the socket is what unblocks both copies, so every exit path --
	// remote hangup, expired time, local hang-up key -- funnels through it.
	var once sync.Once
	closeConn := func() {
		once.Do(func() {
			_ = conn.Close() // best-effort: the relay is finishing either way
		})
	}
	defer closeConn()

	if remaining > 0 {
		timer := time.AfterFunc(remaining, func() {
			slog.Info("time limit reached during rlogin door, disconnecting",
				"node", ctx.NodeNumber, "door", ctx.DoorName)
			closeConn()
		})
		defer timer.Stop()
	}

	// Sessions that support it get their blocked Read cancelled when the door
	// ends, so the input goroutine does not swallow the user's next keypress.
	readInterrupt := make(chan struct{})
	hasInterrupt := false
	if ri, ok := ctx.Session.(interface{ SetReadInterrupt(<-chan struct{}) }); ok {
		ri.SetReadInterrupt(readInterrupt)
		defer ri.SetReadInterrupt(nil)
		hasInterrupt = true
	}

	inputDone := make(chan struct{})
	outputDone := make(chan struct{})

	go func() {
		defer close(inputDone)
		err := copyToRemote(conn, ctx.Session, disconnectKey, disconnectEnabled)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && !errors.Is(err, net.ErrClosed) {
			if strings.Contains(err.Error(), "read interrupted") {
				slog.Debug("rlogin input interrupted (expected during shutdown)",
					"node", ctx.NodeNumber, "door", ctx.DoorName)
			} else {
				slog.Warn("error relaying session input to rlogin door",
					"node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
			}
		}
		// The user hung up or the socket died; either way the output copy
		// should stop waiting on a connection nobody is reading from.
		closeConn()
	}()

	go func() {
		defer close(outputDone)
		_, err := io.Copy(ctx.Session, rlogin.NewAckReader(conn))
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && !errors.Is(err, net.ErrClosed) {
			slog.Warn("error relaying rlogin door output to session",
				"node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
		}
	}()

	// The remote side ending is what normally finishes a door, so wait on the
	// output copy and then tear the input copy down.
	//
	// Sessions without read-interrupt support are not waited on: their input
	// goroutine is parked in a Read that nothing can cancel, and it exits by
	// itself when the next keypress fails to write to the closed socket. This
	// mirrors how the local door executors handle the same sessions.
	<-outputDone
	closeConn()
	close(readInterrupt)
	if hasInterrupt {
		<-inputDone
	}

	slog.Info("rlogin door session ended", "node", ctx.NodeNumber, "door", ctx.DoorName)
}

// copyToRemote forwards caller keystrokes to the door server, watching for the
// local disconnect key. It returns nil when that key is pressed, since a user
// hanging up deliberately is not an error.
func copyToRemote(dst io.Writer, src io.Reader, disconnectKey byte, disconnectEnabled bool) error {
	buf := make([]byte, 4096)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if disconnectEnabled {
				if i := bytes.IndexByte(chunk, disconnectKey); i >= 0 {
					// Send anything typed ahead of the hang-up key, then stop.
					if i > 0 {
						if _, err := dst.Write(chunk[:i]); err != nil {
							return err
						}
					}
					return nil
				}
			}
			if _, err := dst.Write(chunk); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// substituteDoorPlaceholders expands the {NODE}, {USERHANDLE} and friends that
// the rest of the door config supports.
func substituteDoorPlaceholders(value string, subs map[string]string) string {
	for key, val := range subs {
		value = strings.ReplaceAll(value, key, val)
	}
	return strings.TrimSpace(value)
}

// writeDoorMessage sends a pipe-coded status line to the caller. Failures are
// logged rather than returned: a door that ran is not a failure because its
// closing notice could not be printed.
func writeDoorMessage(ctx *DoorCtx, msg string) {
	if strings.TrimSpace(msg) == "" {
		return
	}
	if err := terminalio.WriteProcessedBytes(ctx.Session, ansi.ReplacePipeCodes([]byte(msg)), ctx.OutputMode); err != nil {
		slog.Warn("failed writing remote door message",
			"node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
	}
}
