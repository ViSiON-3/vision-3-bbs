//go:build !windows

package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/creack/pty"
	"golang.org/x/term"
)

// executeNativeDoor runs a native (non-DOS) door program.
// This is extracted from the original inline DOOR: handler in executor.go.
func executeNativeDoor(ctx *DoorCtx) error {
	doorConfig := ctx.Config

	// --- Dropfile Generation ---
	// Must happen before arg substitution so {DROPFILE} and {NODEDIR} are available.
	var dropfilePath string
	dropfileDir := "."
	if doorConfig.WorkingDirectory != "" {
		dropfileDir = doorConfig.WorkingDirectory
	}

	// Configurable dropfile location: "node" uses a unique per-node temp directory.
	// Uses os.MkdirTemp for unique names and defers os.RemoveAll unconditionally
	// so the directory is always cleaned up, even if no recognized dropfile is generated.
	dropfileLoc := strings.ToLower(doorConfig.DropfileLocation)
	if dropfileLoc == "node" {
		nodeDir, err := os.MkdirTemp("", fmt.Sprintf("vision3_node%d_", ctx.NodeNumber))
		if err != nil {
			return fmt.Errorf("failed to create node dropfile directory: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(nodeDir); err != nil {
				slog.Warn("failed to remove node dropfile dir", "dir", nodeDir, "error", err)
			}
		}()
		dropfileDir = nodeDir
	}

	dropfileTypeUpper := strings.ToUpper(doorConfig.DropfileType)

	if dropfileTypeUpper == "DOOR.SYS" || dropfileTypeUpper == "CHAIN.TXT" || dropfileTypeUpper == "DOOR32.SYS" || dropfileTypeUpper == "DORINFO1.DEF" {
		dropfilePath = filepath.Join(dropfileDir, dropfileTypeUpper)
		slog.Info("generating dropfile", "type", dropfileTypeUpper, "path", dropfilePath)

		// Use full-format dropfile generators (standard BBS formats)
		var genErr error
		switch dropfileTypeUpper {
		case "DOOR.SYS":
			genErr = generateDoorSys(ctx, dropfileDir)
		case "DOOR32.SYS":
			genErr = generateDoor32Sys(ctx, dropfileDir)
		case "CHAIN.TXT":
			genErr = generateChainTxt(ctx, dropfileDir)
		case "DORINFO1.DEF":
			genErr = generateDorInfo(ctx, dropfileDir)
		}

		if genErr != nil {
			slog.Error("failed to write dropfile", "path", dropfilePath, "error", genErr)
			errMsg := fmt.Sprintf(ctx.Executor.LoadedStrings.DoorDropfileError, ctx.DoorName)
			if wErr := terminalio.WriteProcessedBytes(ctx.Session.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), ctx.OutputMode); wErr != nil {
				slog.Error("failed writing dropfile creation error message", "error", wErr)
			}
			return genErr
		}

		defer func() {
			slog.Debug("cleaning up dropfile", "path", dropfilePath)
			if err := os.Remove(dropfilePath); err != nil {
				slog.Warn("failed to remove dropfile", "path", dropfilePath, "error", err)
			}
		}()
	}

	// Populate {DROPFILE} and {NODEDIR} now that dropfile generation is done
	ctx.Subs["{DROPFILE}"] = dropfilePath
	ctx.Subs["{NODEDIR}"] = dropfileDir

	// Extract command and args from Commands slice (native: [0]=executable, [1:]=args)
	if len(doorConfig.Commands) == 0 || doorConfig.Commands[0] == "" {
		return fmt.Errorf("door %q has no command configured", ctx.DoorName)
	}
	doorCommand := doorConfig.Commands[0]
	doorArgs := doorConfig.Commands[1:]

	// Substitute placeholders in command and arguments
	for key, val := range ctx.Subs {
		doorCommand = strings.ReplaceAll(doorCommand, key, val)
	}
	substitutedArgs := make([]string, len(doorArgs))
	for i, arg := range doorArgs {
		newArg := arg
		for key, val := range ctx.Subs {
			newArg = strings.ReplaceAll(newArg, key, val)
		}
		substitutedArgs[i] = newArg
	}

	// Substitute in Environment Variables
	substitutedEnv := make(map[string]string)
	if doorConfig.EnvironmentVars != nil {
		for key, val := range doorConfig.EnvironmentVars {
			newVal := val
			for subKey, subVal := range ctx.Subs {
				newVal = strings.ReplaceAll(newVal, subKey, subVal)
			}
			substitutedEnv[key] = newVal
		}
	}

	// Prepare command — optionally wrap in shell
	var cmd *exec.Cmd
	if doorConfig.UseShell {
		// Use exec "$0" "$@" to preserve argument boundaries and prevent injection
		shellArgs := append([]string{"-c", `exec "$0" "$@"`, doorCommand}, substitutedArgs...)
		cmd = exec.Command("/bin/sh", shellArgs...)
		slog.Debug("using shell execution", "node", ctx.NodeNumber, "command", doorCommand, "argCount", len(substitutedArgs))
	} else {
		cmd = exec.Command(doorCommand, substitutedArgs...)
	}

	if doorConfig.WorkingDirectory != "" {
		cmd.Dir = doorConfig.WorkingDirectory
		slog.Debug("setting working directory for door", "door", ctx.DoorName, "dir", cmd.Dir)
	}

	// Set environment variables
	cmd.Env = os.Environ()
	if len(substitutedEnv) > 0 {
		for key, val := range substitutedEnv {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, val))
		}
	}

	// Add standard BBS env vars if not already present
	envMap := make(map[string]bool)
	for _, envPair := range cmd.Env {
		envMap[strings.SplitN(envPair, "=", 2)[0]] = true
	}
	if _, exists := envMap["BBS_USERHANDLE"]; !exists {
		cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_USERHANDLE=%s", ctx.User.Handle))
	}
	if _, exists := envMap["BBS_USERID"]; !exists {
		cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_USERID=%s", ctx.UserIDStr))
	}
	if _, exists := envMap["BBS_NODE"]; !exists {
		cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_NODE=%s", ctx.NodeNumStr))
	}
	if _, exists := envMap["BBS_TIMELEFT"]; !exists {
		cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_TIMELEFT=%s", ctx.TimeLeftStr))
	}

	// Set LINES and COLUMNS from user's saved preferences (for terminal size detection).
	// Remove any existing LINES/COLUMNS entries first to ensure our values take precedence.
	screenHeight := ctx.User.ScreenHeight
	if screenHeight <= 0 {
		screenHeight = 25
	}
	screenWidth := ctx.User.ScreenWidth
	if screenWidth <= 0 {
		screenWidth = 80
	}
	filteredEnv := make([]string, 0, len(cmd.Env))
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, "LINES=") && !strings.HasPrefix(e, "COLUMNS=") {
			filteredEnv = append(filteredEnv, e)
		}
	}
	cmd.Env = append(filteredEnv, fmt.Sprintf("LINES=%d", screenHeight), fmt.Sprintf("COLUMNS=%d", screenWidth))
	slog.Debug("set door env LINES/COLUMNS", "node", ctx.NodeNumber, "lines", screenHeight, "columns", screenWidth)

	// Execute command
	_, winChOrig, isPty := ctx.Session.Pty()
	var cmdErr error

	if doorConfig.RequiresRawTerminal && isPty {
		slog.Info("starting door with PTY/Raw mode", "node", ctx.NodeNumber, "door", ctx.DoorName)

		// Set PTY size from user's saved preferences - BEFORE starting the command
		doorScreenHeight := uint16(25) // default
		if ctx.User.ScreenHeight > 0 && ctx.User.ScreenHeight <= 65535 {
			doorScreenHeight = uint16(ctx.User.ScreenHeight)
		}
		doorScreenWidth := uint16(80) // default
		if ctx.User.ScreenWidth > 0 && ctx.User.ScreenWidth <= 65535 {
			doorScreenWidth = uint16(ctx.User.ScreenWidth)
		}
		doorSize := &pty.Winsize{Rows: doorScreenHeight, Cols: doorScreenWidth}
		slog.Debug("starting door with PTY size from user preferences", "node", ctx.NodeNumber, "width", doorScreenWidth, "height", doorScreenHeight)

		ptmx, err := pty.StartWithSize(cmd, doorSize)
		if err != nil {
			cmdErr = fmt.Errorf("failed to start pty for door '%s': %w", ctx.DoorName, err)
		} else {
			ctx.Session.Signals(nil)
			ctx.Session.Break(nil)

			// Drain window resize events but don't apply them - respect user's saved preferences.
			// resizeStop is closed after cmd.Wait() to prevent this goroutine from leaking.
			resizeStop := make(chan struct{})
			go func() {
				for {
					select {
					case win, ok := <-winChOrig:
						if !ok {
							return
						}
						slog.Debug("ignoring SSH resize event, keeping user preference",
							"node", ctx.NodeNumber, "width", win.Width, "height", win.Height, "prefWidth", doorScreenWidth, "prefHeight", doorScreenHeight)
					case <-resizeStop:
						return
					}
				}
			}()

			fd := int(ptmx.Fd())
			originalState, err := term.MakeRaw(fd)
			if err != nil {
				slog.Warn("failed to put PTY into raw mode for door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
			} else {
				slog.Debug("PTY set to raw mode for door", "node", ctx.NodeNumber, "door", ctx.DoorName)
			}
			needsRestore := (err == nil)

			// Set up a read interrupt so we can cleanly stop the input goroutine
			// when the door exits, preventing it from consuming the next keypress.
			// Sessions that support SetReadInterrupt (SSH) get clean cancellation;
			// others (telnet) fall back to the old behavior where the input goroutine
			// exits on its own when ptmx is closed and the next write fails.
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
				_, err := io.Copy(ptmx, ctx.Session)
				if err != nil && err != io.EOF && !errors.Is(err, os.ErrClosed) {
					// "read interrupted" is expected when we close readInterrupt during shutdown
					if strings.Contains(err.Error(), "read interrupted") {
						slog.Debug("input goroutine interrupted for door (expected during shutdown)", "node", ctx.NodeNumber, "door", ctx.DoorName)
					} else {
						slog.Warn("error copying session stdin to PTY for door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
					}
				}
			}()
			go func() {
				defer close(outputDone)
				_, err := io.Copy(ctx.Session, ptmx)
				if err != nil && err != io.EOF && !errors.Is(err, os.ErrClosed) {
					// "input/output error" on PTY is expected when closing during active read
					if strings.Contains(err.Error(), "input/output error") {
						slog.Debug("output goroutine I/O error for door (expected during shutdown)", "node", ctx.NodeNumber, "door", ctx.DoorName)
					} else {
						slog.Warn("error copying PTY stdout to session for door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
					}
				}
			}()

			// Wait for door to exit, then cleanly shut down I/O goroutines
			cmdErr = cmd.Wait()
			close(resizeStop)
			slog.Debug("door process exited", "node", ctx.NodeNumber, "door", ctx.DoorName)

			// Interrupt the input goroutine's blocked Read() so it exits without
			// consuming the user's next keypress, then restore PTY state and close.
			close(readInterrupt)
			if hasInterrupt {
				<-inputDone
			}

			// Restore PTY state before closing the file descriptor
			if needsRestore {
				slog.Debug("restoring PTY mode after door", "node", ctx.NodeNumber, "door", ctx.DoorName)
				if err := term.Restore(fd, originalState); err != nil {
					slog.Error("failed to restore PTY state after door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
				}
			}

			_ = ptmx.Close() // best-effort PTY teardown
			<-outputDone
		}
	} else if strings.ToUpper(doorConfig.IOMode) == "SOCKET" {
		// Socket I/O: create a Unix socketpair and pass one end to the door as FD 3.
		// The other end is bridged bidirectionally to the BBS session.
		slog.Info("starting door with Socket I/O mode", "node", ctx.NodeNumber, "door", ctx.DoorName)

		fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
		if err != nil {
			cmdErr = fmt.Errorf("failed to create socketpair for door '%s': %w", ctx.DoorName, err)
		} else {
			// fds[0] = BBS side, fds[1] = door side (will become FD 3 in child)
			bbsSock := os.NewFile(uintptr(fds[0]), "bbs-socket")
			doorSock := os.NewFile(uintptr(fds[1]), "door-socket")

			cmd.ExtraFiles = []*os.File{doorSock} // child FD 3
			cmd.Env = append(cmd.Env, "DOOR_SOCKET_FD=3")

			if startErr := cmd.Start(); startErr != nil {
				_ = bbsSock.Close()  // best-effort socket teardown
				_ = doorSock.Close() // best-effort socket teardown
				cmdErr = fmt.Errorf("failed to start door '%s' with socket I/O: %w", ctx.DoorName, startErr)
			} else {
				// Parent closes the door's end
				_ = doorSock.Close() // best-effort socket teardown

				// Set up read interrupt for clean shutdown
				readInterrupt := make(chan struct{})
				hasInterrupt := false
				if ri, ok := ctx.Session.(interface{ SetReadInterrupt(<-chan struct{}) }); ok {
					ri.SetReadInterrupt(readInterrupt)
					defer ri.SetReadInterrupt(nil)
					hasInterrupt = true
				}

				// Bidirectional bridge: session <-> socketpair
				inputDone := make(chan struct{})
				outputDone := make(chan struct{})
				go func() {
					defer close(inputDone)
					_, err := io.Copy(bbsSock, ctx.Session)
					if err != nil && err != io.EOF && !errors.Is(err, os.ErrClosed) {
						if !strings.Contains(err.Error(), "read interrupted") {
							slog.Warn("socket I/O input error for door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
						}
					}
				}()
				go func() {
					defer close(outputDone)
					_, err := io.Copy(ctx.Session, bbsSock)
					if err != nil && err != io.EOF && !errors.Is(err, os.ErrClosed) {
						slog.Warn("socket I/O output error for door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
					}
				}()

				cmdErr = cmd.Wait()
				slog.Debug("door (socket I/O) process exited", "node", ctx.NodeNumber, "door", ctx.DoorName)

				close(readInterrupt)
				if hasInterrupt {
					<-inputDone
				}
				_ = bbsSock.Close() // best-effort socket teardown
				<-outputDone
			}
		}
	} else {
		if doorConfig.RequiresRawTerminal && !isPty {
			slog.Warn("door requires raw terminal, but no PTY was allocated", "node", ctx.NodeNumber, "door", ctx.DoorName)
		}
		slog.Info("starting door with standard I/O redirection", "node", ctx.NodeNumber, "door", ctx.DoorName)

		cmd.Stdout = ctx.Session
		cmd.Stderr = ctx.Session
		cmd.Stdin = ctx.Session
		cmdErr = cmd.Run()

		// Brief delay to let terminal state settle and prevent double-keypress issues
		time.Sleep(100 * time.Millisecond)
	}

	// Run cleanup while dropfiles/node dirs still exist (before deferred cleanup fires)
	executeCleanup(ctx)

	return cmdErr
}
