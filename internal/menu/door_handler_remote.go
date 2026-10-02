package menu

import (
	"bytes"
	"errors"
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
	"github.com/ViSiON-3/vision-3-bbs/internal/telnetclient"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// What the remote door types share: the session deadline, the relay between
// the caller and the door server, and the notices around it. The protocols
// differ only in how the connection is made, which is each executor's own.

// remoteDoorDefaultTimeout bounds connection establishment when a door
// configures none. Every protocol's dialer defaults to the same.
const remoteDoorDefaultTimeout = 10 * time.Second

// remoteDoorAddr renders the address a remote door connects to, with the
// protocol's own port when the door names none.
func remoteDoorAddr(d config.DoorConfig) string {
	host := strings.TrimSpace(d.Host)
	if d.Type == "telnet" {
		return telnetclient.JoinHostPort(host, d.Port)
	}
	return rlogin.JoinHostPort(host, d.Port)
}

// doorDeadline works out when a remote door session must end and how long the
// connection attempt may take.
//
// One absolute deadline covers dialling and the session alike: computing a
// duration before the dial and starting the clock after it would hand the
// caller a fresh full allowance, so a slow connect would extend their time
// rather than spend it.
//
// A zero deadline means no limit, matching how the rest of the BBS reads
// TimeLimit <= 0. expired reports a caller who has no time left at all, who
// should not reach the door server in the first place. credit is the sysop
// chat time not charged to the caller.
func doorDeadline(timeLimitMin int, sessionStart time.Time, connectTimeoutSecs int, now time.Time, credit time.Duration) (deadline time.Time, timeout time.Duration, expired bool) {
	timeout = time.Duration(connectTimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = remoteDoorDefaultTimeout
	}
	if timeLimitMin <= 0 {
		return time.Time{}, timeout, false
	}

	deadline = sessionStart.Add(time.Duration(timeLimitMin)*time.Minute + credit)
	left := deadline.Sub(now)
	if left <= 0 {
		return deadline, 0, true
	}
	// Never wait longer for the door server than the caller has left. The
	// remaining time must stay positive here: the dialers read a nonpositive
	// timeout as "unset" and substitute their default, which would let the
	// dial run on past the deadline it was meant to respect.
	if left < timeout {
		timeout = left
	}
	return deadline, timeout, false
}

// relayRemoteSession pipes bytes between the caller and the door server until
// one end goes away, the user's time runs out, or the user presses the
// disconnect key. It closes conn before returning.
//
// It knows nothing of the protocol that made the connection. The caller's
// input is written to conn, and the door's output is read from remoteOut,
// which is conn itself or a reader over it that takes the protocol out.
//
// A zero deadline means the caller has no time limit, so the session lasts as
// long as the door server keeps it.
func relayRemoteSession(ctx *DoorCtx, conn net.Conn, remoteOut io.Reader, disconnectKey byte, disconnectEnabled bool, deadline time.Time) {
	proto := ctx.Config.RemoteProtocol()
	// Closing the socket is what unblocks both copies, so every exit path --
	// remote hangup, expired time, local hang-up key -- funnels through it.
	var once sync.Once
	closeConn := func() {
		once.Do(func() {
			_ = conn.Close() // best-effort: the relay is finishing either way
		})
	}
	defer closeConn()

	// The connection is closed when the BBS must end the door (see
	// door_watch.go). Once the relay ends the watch stops, so the door's
	// cleanup command isn't counted against the caller.
	relayDone := make(chan struct{})
	defer close(relayDone)
	defer ctx.watch.freeze()
	go func() {
		select {
		case <-ctx.watch.Ended():
			slog.Info("ending remote door", "node", ctx.NodeNumber, "door", ctx.DoorName,
				"protocol", proto, "reason", ctx.watch.endReason().String())
			closeConn()
		case <-relayDone:
		}
	}()

	if !deadline.IsZero() {
		timer := time.AfterFunc(time.Until(deadline), func() {
			slog.Info("time limit reached during remote door, disconnecting",
				"node", ctx.NodeNumber, "door", ctx.DoorName, "protocol", proto)
			// Record why on the watch before closing: the relay freezes the
			// watch as it finishes, and the watch's own timer for the same
			// deadline may not have fired yet.
			ctx.watch.end(doorEndTimeLimit)
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
				slog.Debug("remote door input interrupted (expected during shutdown)",
					"node", ctx.NodeNumber, "door", ctx.DoorName, "protocol", proto)
			} else {
				slog.Warn("error relaying session input to remote door",
					"node", ctx.NodeNumber, "door", ctx.DoorName, "protocol", proto, "error", err)
			}
		}
		// The user hung up or the socket died; either way the output copy
		// should stop waiting on a connection nobody is reading from.
		closeConn()
	}()

	go func() {
		defer close(outputDone)
		_, err := io.Copy(ctx.Session, remoteOut)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && !errors.Is(err, net.ErrClosed) {
			slog.Warn("error relaying remote door output to session",
				"node", ctx.NodeNumber, "door", ctx.DoorName, "protocol", proto, "error", err)
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

	slog.Info("remote door session ended",
		"node", ctx.NodeNumber, "door", ctx.DoorName, "protocol", proto)
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
//
// The result is returned as written apart from one case: a field that expands
// to nothing but whitespace is reported as empty so the caller can apply its
// default. Trimming every field would instead corrupt a value a door server
// asked for, and the handshake fields are documented as being sent as
// configured.
func substituteDoorPlaceholders(value string, subs map[string]string) string {
	for key, val := range subs {
		value = strings.ReplaceAll(value, key, val)
	}
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return value
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
