//go:build !windows

package menu

import (
	"errors"
	"fmt"
	"io"
	"log"
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
				log.Printf("WARN: Failed to remove node dropfile dir %s: %v", nodeDir, err)
			}
		}()
		dropfileDir = nodeDir
	}

	dropfileTypeUpper := strings.ToUpper(doorConfig.DropfileType)

	if dropfileTypeUpper == "DOOR.SYS" || dropfileTypeUpper == "CHAIN.TXT" || dropfileTypeUpper == "DOOR32.SYS" || dropfileTypeUpper == "DORINFO1.DEF" {
		dropfilePath = filepath.Join(dropfileDir, dropfileTypeUpper)
		log.Printf("INFO: Generating %s dropfile at: %s", dropfileTypeUpper, dropfilePath)

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
			log.Printf("ERROR: Failed to write dropfile %s: %v", dropfilePath, genErr)
			errMsg := fmt.Sprintf(ctx.Executor.LoadedStrings.DoorDropfileError, ctx.DoorName)
			if wErr := terminalio.WriteProcessedBytes(ctx.Session.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), ctx.OutputMode); wErr != nil {
				log.Printf("ERROR: Failed writing dropfile creation error message: %v", wErr)
			}
			return genErr
		}

		defer func() {
			log.Printf("DEBUG: Cleaning up dropfile: %s", dropfilePath)
			if err := os.Remove(dropfilePath); err != nil {
				log.Printf("WARN: Failed to remove dropfile %s: %v", dropfilePath, err)
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
		log.Printf("DEBUG: Node %d: Using shell execution for %q with %d arg(s)", ctx.NodeNumber, doorCommand, len(substitutedArgs))
	} else {
		cmd = exec.Command(doorCommand, substitutedArgs...)
	}

	if doorConfig.WorkingDirectory != "" {
		cmd.Dir = doorConfig.WorkingDirectory
		log.Printf("DEBUG: Setting working directory for door '%s' to '%s'", ctx.DoorName, cmd.Dir)
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
	log.Printf("DEBUG: Node %d: Set door env LINES=%d COLUMNS=%d", ctx.NodeNumber, screenHeight, screenWidth)

	// Execute command
	_, winChOrig, isPty := ctx.Session.Pty()
	var cmdErr error

	if doorConfig.RequiresRawTerminal && isPty {
		log.Printf("INFO: Node %d: Starting door '%s' with PTY/Raw mode", ctx.NodeNumber, ctx.DoorName)

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
		log.Printf("DEBUG: Node %d: Starting door with PTY size %dx%d (from user preferences)", ctx.NodeNumber, doorScreenWidth, doorScreenHeight)

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
						log.Printf("DEBUG: Node %d: Ignoring SSH resize event %dx%d (keeping user preference %dx%d)",
							ctx.NodeNumber, win.Width, win.Height, doorScreenWidth, doorScreenHeight)
					case <-resizeStop:
						return
					}
				}
			}()

			fd := int(ptmx.Fd())
			originalState, err := term.MakeRaw(fd)
			if err != nil {
				log.Printf("WARN: Node %d: Failed to put PTY into raw mode for door '%s': %v.", ctx.NodeNumber, ctx.DoorName, err)
			} else {
				log.Printf("DEBUG: Node %d: PTY set to raw mode for door '%s'.", ctx.NodeNumber, ctx.DoorName)
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
						log.Printf("DEBUG: Node %d: Input goroutine interrupted for door '%s' (expected during shutdown)", ctx.NodeNumber, ctx.DoorName)
					} else {
						log.Printf("WARN: Node %d: Error copying session stdin to PTY for door '%s': %v", ctx.NodeNumber, ctx.DoorName, err)
					}
				}
			}()
			go func() {
				defer close(outputDone)
				_, err := io.Copy(ctx.Session, ptmx)
				if err != nil && err != io.EOF && !errors.Is(err, os.ErrClosed) {
					// "input/output error" on PTY is expected when closing during active read
					if strings.Contains(err.Error(), "input/output error") {
						log.Printf("DEBUG: Node %d: Output goroutine I/O error for door '%s' (expected during shutdown)", ctx.NodeNumber, ctx.DoorName)
					} else {
						log.Printf("WARN: Node %d: Error copying PTY stdout to session for door '%s': %v", ctx.NodeNumber, ctx.DoorName, err)
					}
				}
			}()

			// Wait for door to exit, then cleanly shut down I/O goroutines
			cmdErr = cmd.Wait()
			close(resizeStop)
			log.Printf("DEBUG: Node %d: Door '%s' process exited", ctx.NodeNumber, ctx.DoorName)

			// Interrupt the input goroutine's blocked Read() so it exits without
			// consuming the user's next keypress, then restore PTY state and close.
			close(readInterrupt)
			if hasInterrupt {
				<-inputDone
			}

			// Restore PTY state before closing the file descriptor
			if needsRestore {
				log.Printf("DEBUG: Node %d: Restoring PTY mode after door '%s'.", ctx.NodeNumber, ctx.DoorName)
				if err := term.Restore(fd, originalState); err != nil {
					log.Printf("ERROR: Node %d: Failed to restore PTY state after door '%s': %v", ctx.NodeNumber, ctx.DoorName, err)
				}
			}

			ptmx.Close()
			<-outputDone
		}
	} else if strings.ToUpper(doorConfig.IOMode) == "SOCKET" {
		// Socket I/O: create a Unix socketpair and pass one end to the door as FD 3.
		// The other end is bridged bidirectionally to the BBS session.
		log.Printf("INFO: Node %d: Starting door '%s' with Socket I/O mode", ctx.NodeNumber, ctx.DoorName)

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
				bbsSock.Close()
				doorSock.Close()
				cmdErr = fmt.Errorf("failed to start door '%s' with socket I/O: %w", ctx.DoorName, startErr)
			} else {
				// Parent closes the door's end
				doorSock.Close()

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
							log.Printf("WARN: Node %d: Socket I/O input error for door '%s': %v", ctx.NodeNumber, ctx.DoorName, err)
						}
					}
				}()
				go func() {
					defer close(outputDone)
					_, err := io.Copy(ctx.Session, bbsSock)
					if err != nil && err != io.EOF && !errors.Is(err, os.ErrClosed) {
						log.Printf("WARN: Node %d: Socket I/O output error for door '%s': %v", ctx.NodeNumber, ctx.DoorName, err)
					}
				}()

				cmdErr = cmd.Wait()
				log.Printf("DEBUG: Node %d: Door '%s' (socket I/O) process exited", ctx.NodeNumber, ctx.DoorName)

				close(readInterrupt)
				if hasInterrupt {
					<-inputDone
				}
				bbsSock.Close()
				<-outputDone
			}
		}
	} else {
		if doorConfig.RequiresRawTerminal && !isPty {
			log.Printf("WARN: Node %d: Door '%s' requires raw terminal, but no PTY was allocated.", ctx.NodeNumber, ctx.DoorName)
		}
		log.Printf("INFO: Node %d: Starting door '%s' with standard I/O redirection", ctx.NodeNumber, ctx.DoorName)

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
