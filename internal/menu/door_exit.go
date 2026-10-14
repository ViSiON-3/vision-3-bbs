package menu

import (
	"errors"
	"log/slog"
	"os/exec"

	"github.com/gliderlabs/ssh"
)

// nativeDoorResult turns a native door's nonzero exit status, or the signal
// that ended it, into a normal return. The door ran and quit, and anything it
// had to say it has already shown the caller; plenty of doors exit nonzero
// as a matter of course, so this is logged rather than shown as a failure.
// Errors starting the door are still returned.
func nativeDoorResult(ctx *DoorCtx, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		slog.Warn("door exited with nonzero status", "node", ctx.NodeNumber, "door", ctx.DoorName, "status", exitErr.String())
		return nil
	}
	return err
}

// waitDoorErrorEnter waits for the Enter the door error message asks for.
// Without it the key reaches the menu, where a lightbar still on the door
// launches it again. s may be a door's wrapped session: the keys are read
// through the real session's input handler, which the menu reads next. The
// read error is returned, so a caller who idles here or hangs up is logged
// off (see isSessionFatal).
func waitDoorErrorEnter(s ssh.Session) error {
	ih := getSessionIH(unwrapSession(s))
	for {
		key, err := ih.ReadKey()
		if err != nil {
			return err
		}
		if key == '\r' || key == '\n' {
			return nil
		}
	}
}
