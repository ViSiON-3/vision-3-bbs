//go:build !windows

package menu

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"syscall"
	"time"
)

// pollableDoorFile returns a copy of f that is registered with the runtime
// poller, so read deadlines work on it and closing it unblocks a pending Read.
// pty.Start leaves the PTY master in blocking mode, where neither holds, and
// drainDoorOutput relies on both to stop the output copier when a door leaves
// its terminal held open. f is closed once the copy is made. If the copy
// cannot be made, f is returned unchanged and the drain falls back to giving
// up on the copier after its timeout.
func pollableDoorFile(f *os.File) *os.File {
	rc, err := f.SyscallConn()
	if err != nil {
		slog.Debug("door file has no raw descriptor; output drain will not be interruptible", "file", f.Name(), "error", err)
		return f
	}
	dupFD := -1
	var dupErr error
	ctlErr := rc.Control(func(fd uintptr) {
		// Hold ForkLock so a concurrent fork cannot inherit the descriptor
		// before it is marked close-on-exec.
		syscall.ForkLock.RLock()
		defer syscall.ForkLock.RUnlock()
		dupFD, dupErr = syscall.Dup(int(fd))
		if dupErr == nil {
			syscall.CloseOnExec(dupFD)
		}
	})
	if ctlErr != nil || dupErr != nil {
		slog.Debug("cannot duplicate door file; output drain will not be interruptible", "file", f.Name(), "error", ctlErr, "dupError", dupErr)
		return f
	}
	if err := syscall.SetNonblock(dupFD, true); err != nil {
		_ = syscall.Close(dupFD) // best-effort cleanup of the unused copy
		slog.Debug("cannot make door file non-blocking; output drain will not be interruptible", "file", f.Name(), "error", err)
		return f
	}
	nf := os.NewFile(uintptr(dupFD), f.Name())
	_ = f.Close() // the copy keeps the underlying file open
	return nf
}

// withRawFD runs fn with f's descriptor. Unlike f.Fd it leaves f in
// non-blocking mode, so deadlines keep working on a pollableDoorFile.
func withRawFD(f *os.File, fn func(fd int)) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	return rc.Control(func(fd uintptr) { fn(int(fd)) })
}

// isDoorOutputEnd reports whether err, from reading a door's PTY or socket,
// only means the output has ended: end of file, EIO (how a PTY master reports
// that the door side has closed), the file being closed, or the read deadline
// drainDoorOutput sets.
func isDoorOutputEnd(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) ||
		errors.Is(err, os.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded)
}

// drainDoorOutput waits for a door's output copier (which closes outputDone
// when it returns) to relay what the door wrote before exiting. Call it after
// cmd.Wait and before closing f, the copier's source: closing f first can
// make the copier's next read fail and drop the door's final output.
//
// If the copier has not reached the end of the output within
// doorOutputDrainTimeout, the rest is abandoned: f's read deadline is expired
// so the copier's read fails, and if even that does not stop it (f is not
// pollable) the copier is left behind rather than hang the node.
func drainDoorOutput(f *os.File, outputDone <-chan struct{}, node int, door string) {
	timer := time.NewTimer(doorOutputDrainTimeout)
	defer timer.Stop()
	select {
	case <-outputDone:
		return
	case <-timer.C:
	}

	slog.Warn("door output still open after the door exited; is a child process holding its terminal? Closing it",
		"node", node, "door", door, "timeout", doorOutputDrainTimeout)
	if err := f.SetReadDeadline(time.Now()); err != nil {
		slog.Debug("cannot interrupt door output copier", "node", node, "door", door, "error", err)
	}
	timer.Reset(doorOutputDrainTimeout)
	select {
	case <-outputDone:
	case <-timer.C:
		slog.Error("door output copier did not stop; abandoning it", "node", node, "door", door)
	}
}
