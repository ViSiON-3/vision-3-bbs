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
	var left doorInputCopy // set by the input goroutine, for after inputDone
	trailer := ctx.takeDoorEnterTrailer()

	go func() {
		defer close(inputDone)
		var err error
		left, err = copyToRemote(conn, ctx.Session, readInterrupt, disconnectKey, disconnectEnabled, trailer)
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
		left.keep(ctx.Session)
	}

	slog.Info("remote door session ended",
		"node", ctx.NodeNumber, "door", ctx.DoorName, "protocol", proto)
}

// copyToRemote forwards caller keystrokes to the door server until a read or
// a write fails, or the caller presses the local disconnect key; a user
// hanging up deliberately is not an error. It is copyDoorInput with closed,
// the relay closing the connection, as exited: it returns the keys it read
// but did not send (a chunk read once closed is closed, the unsent rest of a
// failed write, and the keys typed after the disconnect key) for the caller
// to keep for the next prompt once the copy has ended. With enterTrailer
// set, the LF or NUL of an earlier Enter is not sent.
//
// The disconnect key is never kept, nor what was typed before it in the
// same chunk: the caller pressed it to leave the door, even when the copy
// stopped before it was reached. A disconnect key of CR is the CR of an
// Enter, so the LF or NUL that follows it is not kept either, or is marked
// for the next reader to drop if it has not arrived.
func copyToRemote(dst io.Writer, src io.Reader, closed <-chan struct{}, disconnectKey byte, disconnectEnabled bool, enterTrailer bool) (doorInputCopy, error) {
	if !disconnectEnabled {
		return copyDoorInput(dst, src, closed, enterTrailer)
	}
	d := &disconnectWriter{w: dst, key: disconnectKey}
	left, err := copyDoorInput(d, src, closed, enterTrailer)
	if errors.Is(err, errDisconnectKey) {
		left.enterTrailer = d.enterTrailer
		return left, nil
	}
	// The key was read but not reached: a write ahead of it in its chunk
	// failed, or the chunk was read once closed and not written at all.
	if i := bytes.IndexByte(left.unread, disconnectKey); i >= 0 {
		end, trailer := d.keyEnd(left.unread, i)
		left = doorInputCopy{unread: left.unread[end:], enterTrailer: trailer}
	}
	return left, err
}

// errDisconnectKey is how disconnectWriter tells copyToRemote the caller
// pressed the disconnect key.
var errDisconnectKey = errors.New("disconnect key pressed")

// disconnectWriter writes to w what comes before key, then fails with
// errDisconnectKey. It counts key as written, so the keys after it are what
// copyDoorInput hands back unsent. A key of CR takes the LF or NUL after it
// too, or sets enterTrailer if the write ends with it.
type disconnectWriter struct {
	w            io.Writer
	key          byte
	enterTrailer bool
}

func (d *disconnectWriter) Write(p []byte) (int, error) {
	i := bytes.IndexByte(p, d.key)
	if i < 0 {
		return d.w.Write(p)
	}
	// Send anything typed ahead of the hang-up key, then stop.
	if i > 0 {
		n, err := d.w.Write(p[:i])
		if err == nil && n < i {
			err = io.ErrShortWrite
		}
		if err != nil {
			return n, err
		}
	}
	n, trailer := d.keyEnd(p, i)
	d.enterTrailer = trailer
	return n, errDisconnectKey
}

// keyEnd returns where the disconnect key at p[i] ends: after it, or for a
// key of CR after the LF or NUL that follows it. trailer reports a CR that
// ends p, whose LF or NUL has not arrived.
func (d *disconnectWriter) keyEnd(p []byte, i int) (end int, trailer bool) {
	end = i + 1
	if d.key == '\r' {
		switch {
		case end == len(p):
			trailer = true
		case p[end] == '\n' || p[end] == 0x00:
			end++
		}
	}
	return end, trailer
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
