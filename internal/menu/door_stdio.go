package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
)

// doorOutputDrainTimeout bounds how long a door's remaining output is relayed
// after the door process exits. A door that exits normally closes its end of
// the PTY, socket or output pipe, so the output copier reaches EOF (or EIO on
// a PTY) almost at once; the timeout only matters when something else, such as a background
// child the door started, still holds that end open. A variable so tests can
// shorten it.
var doorOutputDrainTimeout = 2 * time.Second

// runStdioDoor runs cmd with its standard input, output and error connected
// to the caller's session and returns once the door exits.
//
// Input reaches the door through a pipe rather than by handing exec the
// session as cmd.Stdin. exec's own input copier would stay parked in
// Session.Read after the door exited, so the caller would have to press a key
// to get back to the menu and that key would be lost. Here the read is
// interrupted when the door exits, as on the PTY and socket paths. Output is
// relayed by exec, which drains it before Wait returns; WaitDelay keeps a
// background child that still holds the door's output open from hanging the
// node.
func runStdioDoor(ctx *DoorCtx, cmd *exec.Cmd) error {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create input pipe for door '%s': %w", ctx.DoorName, err)
	}
	cmd.Stdin = stdinR
	cmd.Stdout = ctx.Session
	cmd.Stderr = ctx.Session
	cmd.WaitDelay = doorOutputDrainTimeout
	setDoorProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		_ = stdinR.Close() // best-effort pipe teardown
		_ = stdinW.Close() // best-effort pipe teardown
		return err
	}
	_ = stdinR.Close() // the door holds its own copy
	stopDoorWatch := watchDoorProcess(ctx, cmd.Process)

	// Sessions that support SetReadInterrupt (SSH, telnet) stop the input
	// goroutine cleanly when the door exits. On others it stays in Read until
	// the next keypress, which then fails to reach the exited door.
	readInterrupt := make(chan struct{})
	hasInterrupt := false
	if ri, ok := ctx.Session.(interface{ SetReadInterrupt(<-chan struct{}) }); ok {
		ri.SetReadInterrupt(readInterrupt)
		defer ri.SetReadInterrupt(nil)
		hasInterrupt = true
	}

	inputDone := make(chan struct{})
	var left doorInputCopy // set by the input goroutine, for after inputDone
	go func() {
		defer close(inputDone)
		// Closing the pipe when the session ends gives the door end-of-file.
		defer func() { _ = stdinW.Close() }() // best-effort pipe teardown
		var err error
		left, err = copyDoorInput(stdinW, ctx.Session, readInterrupt, ctx.enterTrailer)
		if err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, syscall.EPIPE) {
			if strings.Contains(err.Error(), "read interrupted") {
				slog.Debug("input goroutine interrupted for door (expected during shutdown)", "node", ctx.NodeNumber, "door", ctx.DoorName)
			} else {
				slog.Warn("error copying session stdin to door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
			}
		}
	}()

	cmdErr := cmd.Wait()
	stopDoorWatch()
	slog.Debug("door (standard I/O) process exited", "node", ctx.NodeNumber, "door", ctx.DoorName)
	if errors.Is(cmdErr, exec.ErrWaitDelay) {
		slog.Warn("door output still open after the door exited; is a child process holding it? Closed it",
			"node", ctx.NodeNumber, "door", ctx.DoorName, "timeout", doorOutputDrainTimeout)
		cmdErr = nil
	}

	close(readInterrupt)
	if hasInterrupt {
		<-inputDone
		left.keep(ctx.Session)
	}
	return cmdErr
}

// doorInputCopy is what copyDoorInput leaves for the prompt after the door.
type doorInputCopy struct {
	unread       []byte // keys read from the session but not given to the door
	enterTrailer bool   // the LF or NUL of an earlier Enter has not come yet
}

// keep hands c to the session's next reader: the keys through
// stashTypeAhead, the pending Enter trailer through keepEnterTrailer. Call it
// only once the copy has ended.
func (c doorInputCopy) keep(s ssh.Session) {
	stashTypeAhead(s, c.unread)
	keepEnterTrailer(s, c.enterTrailer)
}

// copyDoorInput copies the caller's keys from the session to w, a door's
// input, until a read or a write fails. A chunk read after exited is closed
// (the door has gone, so nothing would read it) is not written, and a write
// that fails leaves the rest of its chunk unwritten. copyDoorInput returns
// those keys, for the caller to keep for the next prompt once the copy has
// ended, and the error that ended it; end of input is not an error.
//
// With enterTrailer set, the session's last reader took the CR of an Enter
// whose LF or NUL had not arrived: a leading LF or NUL in the first input
// read is dropped, and if nothing was read the result still says so.
//
// Keys already written but not yet read by the door when it exits are in the
// PTY or pipe, out of reach, and are lost.
func copyDoorInput(w io.Writer, r io.Reader, exited <-chan struct{}, enterTrailer bool) (doorInputCopy, error) {
	buf := make([]byte, 32*1024)
	for {
		n, rerr := r.Read(buf)
		data := buf[:n]
		if enterTrailer && n > 0 {
			enterTrailer = false
			if data[0] == '\n' || data[0] == 0x00 {
				data = data[1:]
			}
		}
		if len(data) > 0 {
			select {
			case <-exited:
				return doorInputCopy{unread: append([]byte(nil), data...)}, ignoreEOF(rerr)
			default:
			}
			nw, werr := w.Write(data)
			if nw < 0 || nw > len(data) {
				nw = 0
			}
			if werr == nil && nw < len(data) {
				werr = io.ErrShortWrite
			}
			if werr != nil {
				return doorInputCopy{unread: append([]byte(nil), data[nw:]...)}, werr
			}
		}
		if rerr != nil {
			return doorInputCopy{enterTrailer: enterTrailer}, ignoreEOF(rerr)
		}
	}
}

// ignoreEOF returns err, or nil if it is io.EOF.
func ignoreEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
