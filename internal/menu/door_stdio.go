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
	stopIdleWatch := watchDoorIdle(ctx, cmd.Process)

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
	go func() {
		defer close(inputDone)
		// Closing the pipe when the session ends gives the door end-of-file.
		defer func() { _ = stdinW.Close() }() // best-effort pipe teardown
		_, err := io.Copy(stdinW, ctx.Session)
		if err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, syscall.EPIPE) {
			if strings.Contains(err.Error(), "read interrupted") {
				slog.Debug("input goroutine interrupted for door (expected during shutdown)", "node", ctx.NodeNumber, "door", ctx.DoorName)
			} else {
				slog.Warn("error copying session stdin to door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
			}
		}
	}()

	cmdErr := cmd.Wait()
	stopIdleWatch()
	slog.Debug("door (standard I/O) process exited", "node", ctx.NodeNumber, "door", ctx.DoorName)
	if errors.Is(cmdErr, exec.ErrWaitDelay) {
		slog.Warn("door output still open after the door exited; is a child process holding it? Closed it",
			"node", ctx.NodeNumber, "door", ctx.DoorName, "timeout", doorOutputDrainTimeout)
		cmdErr = nil
	}

	close(readInterrupt)
	if hasInterrupt {
		<-inputDone
	}
	return cmdErr
}
